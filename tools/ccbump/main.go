// Command ccbump keeps each Dockerfile's pinned ca-certificates install fresh.
//
// For every managed Dockerfile it runs the exact pinned base image, points apt
// at a current snapshot.debian.org timestamp, and reads the latest
// ca-certificates version available there. It opens or updates a
// per-Dockerfile, auto-merging pull request that rewrites the two managed ARG
// anchors when either:
//
//   - that version is newer than the one pinned on main (compared with
//     `dpkg --compare-versions` inside the container, never on the host), or
//   - the version is unchanged but refreshing the snapshot would change some
//     package the install resolves. Dependencies like openssl come from the
//     pinned snapshot too, so without this they would never see security
//     updates between ca-certificates releases.
//
// Correctness of a bump is guaranteed by the build.yml PR run, which builds the
// new pin on both arches before auto-merge can land it; ccbump only affects
// freshness, never whether main builds.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	var (
		repoRoot   = flag.String("repo-root", ".", "path to the repository root")
		dirsCSV    = flag.String("dirs", "", "comma-separated Dockerfile dirs to process (default: auto-discover debian-*)")
		snapshot   = flag.String("snapshot", "", "snapshot timestamp to detect against (default: today at 00:00:00 UTC)")
		platform   = flag.String("platform", "linux/amd64", "docker platform to detect on (ca-certificates is Architecture: all)")
		baseBranch = flag.String("base", "main", "base branch the bump PRs target")
		gitName    = flag.String("git-name", "ca-certs-images-bot", "git committer name for bump commits")
		gitEmail   = flag.String("git-email", "ca-certs-images-bot@users.noreply.github.com", "git committer email for bump commits")
		dryRun     = flag.Bool("dry-run", false, "detect and report, but do not touch git or GitHub")
		detectOnly = flag.Bool("detect-only", false, "only print detected snapshot+version per Dockerfile, then exit")
	)
	flag.Parse()

	if *snapshot == "" {
		*snapshot = time.Now().UTC().Format("20060102") + "T000000Z"
	}

	b := &bumper{
		repoRoot:   *repoRoot,
		snapshot:   *snapshot,
		platform:   *platform,
		baseBranch: *baseBranch,
		gitName:    *gitName,
		gitEmail:   *gitEmail,
		dryRun:     *dryRun || *detectOnly,
		detectOnly: *detectOnly,
	}

	dirs, err := b.targetDirs(*dirsCSV)
	if err != nil {
		fatalf("%v", err)
	}

	var failed bool
	for _, dir := range dirs {
		if err := b.process(dir); err != nil {
			fmt.Fprintf(os.Stderr, "ccbump: %s: %v\n", dir, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

type bumper struct {
	repoRoot   string
	snapshot   string
	platform   string
	baseBranch string
	gitName    string
	gitEmail   string
	dryRun     bool
	detectOnly bool
}

// A change is one pending bump for one Dockerfile: either a new
// ca-certificates version, or a snapshot-only refresh that updates the
// packages the install resolves while the pinned version stays the same.
type change struct {
	d *dockerfile
	// candidate is the ca-certificates version the bump will pin. For a
	// snapshot-only refresh it equals what the new snapshot resolves, which is
	// normally d.version unchanged.
	candidate string
	// snapshot is the new DEBIAN_SNAPSHOT value.
	snapshot string
	// versionBump is true when candidate is strictly newer than d.version.
	versionBump bool
	// depDiff describes how the resolved install set changes, one line per
	// package. Only populated for snapshot-only refreshes.
	depDiff []string
}

func (c *change) title() string {
	dir := filepath.Dir(c.d.path)
	if c.versionBump {
		return fmt.Sprintf("%s: bump ca-certificates to %s", dir, c.candidate)
	}
	return fmt.Sprintf("%s: refresh Debian snapshot to %s", dir, c.snapshot)
}

// targetDirs returns the Dockerfile directories to process. An explicit CSV
// wins; otherwise it auto-discovers every "debian-*" dir that has a Dockerfile.
func (b *bumper) targetDirs(csv string) ([]string, error) {
	if csv != "" {
		var dirs []string
		for dir := range strings.SplitSeq(csv, ",") {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(b.repoRoot, dir, "Dockerfile")); err != nil {
				return nil, fmt.Errorf("-dirs entry %q has no Dockerfile: %w", dir, err)
			}
			dirs = append(dirs, dir)
		}
		if len(dirs) == 0 {
			return nil, fmt.Errorf("-dirs %q names no directories", csv)
		}
		return dirs, nil
	}
	entries, err := os.ReadDir(b.repoRoot)
	if err != nil {
		return nil, fmt.Errorf("reading repo root: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "debian-") {
			continue
		}
		if _, err := os.Stat(filepath.Join(b.repoRoot, e.Name(), "Dockerfile")); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no debian-* Dockerfile directories found in %s", b.repoRoot)
	}
	return dirs, nil
}

// process runs the full detect-compare-bump pipeline for one Dockerfile dir.
func (b *bumper) process(dir string) error {
	path := filepath.Join(dir, "Dockerfile")

	d, err := b.parseTarget(path)
	if err != nil {
		return err
	}

	cur, err := b.stateAt(d, b.snapshot)
	if err != nil {
		return fmt.Errorf("detecting at snapshot %s: %w", b.snapshot, err)
	}

	if b.detectOnly {
		fmt.Printf("%s\tsnapshot=%s\tcurrent=%s\tcandidate=%s\n", dir, b.snapshot, d.version, cur.candidate)
		return nil
	}

	newer, err := b.versionGreater(d.image, cur.candidate, d.version)
	if err != nil {
		return fmt.Errorf("comparing versions: %w", err)
	}

	c := &change{d: d, candidate: cur.candidate, snapshot: b.snapshot, versionBump: newer}
	if newer {
		fmt.Printf("%s: ca-certificates %s -> %s (snapshot %s -> %s)\n",
			dir, d.version, cur.candidate, d.snapshot, b.snapshot)
	} else {
		// Same ca-certificates version, but the install also pulls in
		// dependencies (openssl and friends) resolved at the pinned snapshot.
		// If resolving at the current snapshot would change any of them --
		// e.g. an openssl security update -- refresh the snapshot so the fix
		// reaches the images without waiting for the next ca-certificates
		// release.
		pinned, err := b.stateAt(d, d.snapshot)
		if err != nil {
			return fmt.Errorf("detecting at pinned snapshot %s: %w", d.snapshot, err)
		}
		c.depDiff = diffManifests(pinned.manifest, cur.manifest)
		if len(c.depDiff) == 0 {
			fmt.Printf("%s: up to date (pinned %s at %s; no dependency changes at %s)\n",
				dir, d.version, d.snapshot, b.snapshot)
			return nil
		}
		fmt.Printf("%s: snapshot %s -> %s changes resolved dependencies:\n", dir, d.snapshot, b.snapshot)
		for _, l := range c.depDiff {
			fmt.Printf("  %s\n", l)
		}
	}

	if b.dryRun {
		return nil
	}
	return b.openOrUpdatePR(c)
}

// parseTarget reads and parses a Dockerfile, preferring the copy committed on
// the base branch -- the source of truth for "what main builds", independent of
// a dirty working tree. It falls back to the working tree when the base copy is
// missing or not yet in the managed shape (e.g. before the refactor has landed
// on main, or during local testing on a branch), warning so a regression of
// main's copy is visible in the logs rather than silently masked.
func (b *bumper) parseTarget(path string) (*dockerfile, error) {
	if content, err := b.fileOnBase(path); err != nil {
		fmt.Fprintf(os.Stderr, "ccbump: warning: %s: could not read copy on %s (%v); falling back to the working tree\n",
			path, b.baseBranch, err)
	} else if d, err := parseDockerfile(path, string(content)); err != nil {
		fmt.Fprintf(os.Stderr, "ccbump: warning: %s: copy on %s is not in the managed shape (%v); falling back to the working tree\n",
			path, b.baseBranch, err)
	} else {
		return d, nil
	}
	content, err := os.ReadFile(filepath.Join(b.repoRoot, path))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseDockerfile(path, string(content))
}

// A snapshotState is what one snapshot timestamp resolves for one Dockerfile:
// the candidate (latest available) ca-certificates version, and the full set
// of packages installing it would pull in at that snapshot.
type snapshotState struct {
	candidate string
	// manifest is the sorted "pkg version" lines the install resolves.
	manifest []string
}

// stateAt runs the pinned base image with apt pointed at the given snapshot
// and reads back the candidate ca-certificates version plus the resolved
// install manifest.
//
// The sources.list template here mirrors the RUN block in each managed
// Dockerfile; keep the two in sync or detection will drift from what the
// images actually build against.
func (b *bumper) stateAt(d *dockerfile, snapshot string) (*snapshotState, error) {
	script := fmt.Sprintf(`set -e
printf '%%s\n' \
  "deb http://snapshot.debian.org/archive/debian/%[1]s/ %[2]s main" \
  "deb http://snapshot.debian.org/archive/debian-security/%[1]s/ %[2]s-security main" \
  "deb http://snapshot.debian.org/archive/debian/%[1]s/ %[2]s-updates main" \
  > /etc/apt/sources.list
rm -f /etc/apt/sources.list.d/*
apt-get -o Acquire::Check-Valid-Until=false -o Acquire::Retries=5 update >/dev/null
cand="$(apt-cache policy ca-certificates | sed -n 's/^  Candidate: //p')"
printf '%%s\n' "$cand"
[ -n "$cand" ] && [ "$cand" != "(none)" ] || exit 0
apt-get install -s --no-install-recommends ca-certificates \
  | sed -n 's/^Inst \([^ ]*\) (\([^ ]*\).*/\1 \2/p' | sort
`, snapshot, d.suite)

	out, err := b.run("docker", "run", "--rm", "--platform", b.platform, d.image, "sh", "-c", script)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	candidate := strings.TrimSpace(lines[0])
	if candidate == "" || candidate == "(none)" {
		return nil, fmt.Errorf("no ca-certificates candidate at snapshot %s for %s", snapshot, d.suite)
	}
	var manifest []string
	for _, l := range lines[1:] {
		if l = strings.TrimSpace(l); l != "" {
			manifest = append(manifest, l)
		}
	}
	if len(manifest) == 0 {
		// The base images never preinstall ca-certificates, so an empty
		// manifest means the simulated-install output was not parseable --
		// fail loudly rather than silently never seeing dependency changes.
		return nil, fmt.Errorf("empty install manifest at snapshot %s for %s", snapshot, d.suite)
	}
	return &snapshotState{candidate: candidate, manifest: manifest}, nil
}

// diffManifests returns one human-readable line per package whose resolved
// version differs between two "pkg version" manifests, or nil if they match.
func diffManifests(old, new []string) []string {
	parse := func(m []string) map[string]string {
		vers := make(map[string]string, len(m))
		for _, l := range m {
			pkg, ver, _ := strings.Cut(l, " ")
			vers[pkg] = ver
		}
		return vers
	}
	ov, nv := parse(old), parse(new)
	var pkgs []string
	for p := range ov {
		pkgs = append(pkgs, p)
	}
	for p := range nv {
		if _, ok := ov[p]; !ok {
			pkgs = append(pkgs, p)
		}
	}
	sort.Strings(pkgs)
	var diff []string
	for _, p := range pkgs {
		o, oOK := ov[p]
		n, nOK := nv[p]
		switch {
		case oOK && nOK && o != n:
			diff = append(diff, fmt.Sprintf("%s %s -> %s", p, o, n))
		case oOK && !nOK:
			diff = append(diff, fmt.Sprintf("%s %s (no longer installed)", p, o))
		case !oOK && nOK:
			diff = append(diff, fmt.Sprintf("%s %s (newly installed)", p, n))
		}
	}
	return diff
}

// versionGreater reports whether candidate is strictly newer than current,
// decided by dpkg inside the container so there is no host-side comparator to
// keep in sync with Debian's version semantics.
func (b *bumper) versionGreater(image, candidate, current string) (bool, error) {
	cmd := exec.Command("docker", "run", "--rm", "--platform", b.platform, image,
		"dpkg", "--compare-versions", candidate, "gt", current)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// dpkg exits 1 when the comparison is simply false; that is not an error.
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("dpkg --compare-versions %q gt %q: %w", candidate, current, err)
}

// openOrUpdatePR rewrites the managed anchors on a stable per-Dockerfile branch
// and ensures an auto-merging PR exists. It is idempotent: if the branch
// already holds exactly the content this bump would write -- same version,
// same snapshot, same everything-else from base -- it skips the force-push so
// an open PR's checks are not needlessly restarted, and only re-asserts the
// PR metadata and auto-merge.
func (b *bumper) openOrUpdatePR(c *change) error {
	d := c.d
	dir := filepath.Dir(d.path)
	branch := "ccbump/" + dir
	newContent := d.withVersions(c.snapshot, c.candidate)

	if existing, err := b.fileOnRef("origin/"+branch, d.path); err == nil && string(existing) == newContent {
		fmt.Printf("%s: branch %s already up to date; re-asserting PR and auto-merge\n", dir, branch)
		return b.ensurePR(branch, c)
	}

	if err := b.commitToBranch(branch, d.path, newContent, c.title()); err != nil {
		return err
	}
	return b.ensurePR(branch, c)
}

// commitToBranch resets branch to base, writes newContent, and force-pushes a
// single bump commit. Force-push keeps the branch a clean one-commit delta from
// base even after the base branch advances.
func (b *bumper) commitToBranch(branch, path, newContent, msg string) error {
	if _, err := b.git("switch", "-C", branch, "origin/"+b.baseBranch); err != nil {
		return fmt.Errorf("creating branch %s: %w", branch, err)
	}
	if err := os.WriteFile(filepath.Join(b.repoRoot, path), []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := b.git("add", path); err != nil {
		return err
	}
	if _, err := b.git("-c", "user.name="+b.gitName, "-c", "user.email="+b.gitEmail,
		"commit", "-m", msg); err != nil {
		return fmt.Errorf("committing %s: %w", path, err)
	}
	if _, err := b.git("push", "-f", "origin", branch); err != nil {
		return fmt.Errorf("pushing %s: %w", branch, err)
	}
	return nil
}

// ensurePR makes sure an open PR exists for branch, that its title and body
// describe what the branch pins right now, and that auto-merge is on. Keeping
// the title current matters beyond cosmetics: the squash merge uses the PR
// title as the commit subject, so a stale title would record the wrong bump in
// main's history. A failure to enable auto-merge is a warning, not fatal: the
// PR is the durable artifact, and auto-merge can be re-asserted on the next
// run.
func (b *bumper) ensurePR(branch string, c *change) error {
	dir := filepath.Dir(c.d.path)
	num, err := b.prNumberForBranch(branch)
	if err != nil {
		return err
	}
	title, body := c.title(), prBody(c)
	if num == "" {
		if _, err := b.run("gh", "pr", "create", "--base", b.baseBranch, "--head", branch,
			"--title", title, "--body", body); err != nil {
			return fmt.Errorf("creating PR for %s: %w", branch, err)
		}
	} else {
		if _, err := b.run("gh", "pr", "edit", num, "--title", title, "--body", body); err != nil {
			return fmt.Errorf("updating PR #%s for %s: %w", num, branch, err)
		}
	}
	if _, err := b.run("gh", "pr", "merge", "--auto", "--squash", branch); err != nil {
		fmt.Fprintf(os.Stderr, "ccbump: %s: warning: could not enable auto-merge: %v\n", dir, err)
	}
	return nil
}

// prNumberForBranch returns the number of the open PR whose head is branch, or
// "" if there is none.
func (b *bumper) prNumberForBranch(branch string) (string, error) {
	out, err := b.run("gh", "pr", "list", "--head", branch, "--state", "open",
		"--json", "number", "--jq", ".[0].number // \"\"")
	if err != nil {
		return "", fmt.Errorf("listing PRs for %s: %w", branch, err)
	}
	return strings.TrimSpace(out), nil
}

func prBody(c *change) string {
	dir := filepath.Dir(c.d.path)
	var sb strings.Builder
	fmt.Fprintf(&sb, "Automated bump by `tools/ccbump`.\n\n")
	fmt.Fprintf(&sb, "| | |\n|---|---|\n")
	fmt.Fprintf(&sb, "| Dockerfile | `%s/Dockerfile` |\n", dir)
	if c.versionBump {
		fmt.Fprintf(&sb, "| ca-certificates | `%s` → `%s` |\n", c.d.version, c.candidate)
	} else {
		fmt.Fprintf(&sb, "| ca-certificates | `%s` (unchanged) |\n", c.candidate)
	}
	fmt.Fprintf(&sb, "| Debian snapshot | `%s` → `%s` |\n", c.d.snapshot, c.snapshot)
	if len(c.depDiff) > 0 {
		fmt.Fprintf(&sb, "\nRefreshing the snapshot changes the packages the `ca-certificates`\ninstall resolves:\n\n```\n%s\n```\n",
			strings.Join(c.depDiff, "\n"))
	}
	fmt.Fprintf(&sb, `
Detected by running the pinned base image and resolving `+"`ca-certificates`"+`
at the snapshot above. The apt sources are pinned to `+"`snapshot.debian.org`"+`,
so the pinned versions stay fetchable forever.

This PR auto-merges once the `+"`build`"+` workflow proves the new pin builds on
both `+"`linux/amd64`"+` and `+"`linux/arm64`"+`. Auto-merge only affects how fresh the
image is; it can never make `+"`main`"+` fail to build.`)
	return sb.String()
}

// fileOnBase returns the contents of path as committed on the base branch.
func (b *bumper) fileOnBase(path string) ([]byte, error) {
	return b.fileOnRef("origin/"+b.baseBranch, path)
}

// fileOnRef returns the contents of path at the given git ref.
func (b *bumper) fileOnRef(ref, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = b.repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git show %s:%s: %v: %s", ref, path, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// git runs a git subcommand in the repo root and returns its stdout.
func (b *bumper) git(args ...string) (string, error) {
	return b.run("git", append([]string{"-C", b.repoRoot}, args...)...)
}

// run executes a command, streaming stderr through, and returns trimmed stdout.
func (b *bumper) run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = b.repoRoot
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ccbump: "+format+"\n", args...)
	os.Exit(1)
}
