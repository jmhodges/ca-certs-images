// Command ccbump keeps each Dockerfile's pinned ca-certificates version fresh.
//
// For every managed Dockerfile it runs the exact pinned base image, points apt
// at a current snapshot.debian.org timestamp, and reads the latest
// ca-certificates version available there. If that version is newer than the
// one pinned on main (compared with `dpkg --compare-versions` inside the
// container, never on the host), it opens or updates a per-Dockerfile,
// auto-merging pull request that rewrites the two managed ARG anchors.
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

// targetDirs returns the Dockerfile directories to process. An explicit CSV
// wins; otherwise it auto-discovers every "debian-*" dir that has a Dockerfile.
func (b *bumper) targetDirs(csv string) ([]string, error) {
	if csv != "" {
		return strings.Split(csv, ","), nil
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

	candidate, err := b.detect(d)
	if err != nil {
		return fmt.Errorf("detecting latest ca-certificates: %w", err)
	}

	if b.detectOnly {
		fmt.Printf("%s\tsnapshot=%s\tcurrent=%s\tcandidate=%s\n", dir, b.snapshot, d.version, candidate)
		return nil
	}

	newer, err := b.versionGreater(d.image, candidate, d.version)
	if err != nil {
		return fmt.Errorf("comparing versions: %w", err)
	}
	if !newer {
		fmt.Printf("%s: up to date (pinned %s, latest %s)\n", dir, d.version, candidate)
		return nil
	}

	fmt.Printf("%s: %s -> %s (snapshot %s)\n", dir, d.version, candidate, b.snapshot)
	if b.dryRun {
		return nil
	}
	return b.openOrUpdatePR(d, candidate)
}

// parseTarget reads and parses a Dockerfile, preferring the copy committed on
// the base branch -- the source of truth for "what main builds", independent of
// a dirty working tree. It falls back to the working tree when the base copy is
// missing or not yet in the managed shape (e.g. before the refactor has landed
// on main, or during local testing on a branch).
func (b *bumper) parseTarget(path string) (*dockerfile, error) {
	if content, err := b.fileOnBase(path); err == nil {
		if d, err := parseDockerfile(path, string(content)); err == nil {
			return d, nil
		}
	}
	content, err := os.ReadFile(filepath.Join(b.repoRoot, path))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseDockerfile(path, string(content))
}

// detect runs the pinned base image, points apt at b.snapshot, and returns the
// candidate (latest available) ca-certificates version at that snapshot.
func (b *bumper) detect(d *dockerfile) (string, error) {
	script := fmt.Sprintf(`set -e
printf '%%s\n' \
  "deb http://snapshot.debian.org/archive/debian/%[1]s/ %[2]s main" \
  "deb http://snapshot.debian.org/archive/debian-security/%[1]s/ %[2]s-security main" \
  "deb http://snapshot.debian.org/archive/debian/%[1]s/ %[2]s-updates main" \
  > /etc/apt/sources.list
rm -f /etc/apt/sources.list.d/*
apt-get -o Acquire::Check-Valid-Until=false -o Acquire::Retries=5 update >/dev/null
apt-cache policy ca-certificates | sed -n 's/^  Candidate: //p'
`, b.snapshot, d.suite)

	out, err := b.run("docker", "run", "--rm", "--platform", b.platform, d.image, "sh", "-c", script)
	if err != nil {
		return "", err
	}
	candidate := strings.TrimSpace(out)
	if candidate == "" || candidate == "(none)" {
		return "", fmt.Errorf("no ca-certificates candidate at snapshot %s for %s", b.snapshot, d.suite)
	}
	return candidate, nil
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
// and ensures an auto-merging PR exists. It is idempotent: if the branch already
// pins the candidate version it skips the push and only re-asserts auto-merge.
func (b *bumper) openOrUpdatePR(d *dockerfile, candidate string) error {
	dir := filepath.Dir(d.path)
	branch := "ccbump/" + dir
	newContent := d.withVersions(b.snapshot, candidate)

	if existing, err := b.fileOnRef("origin/"+branch, d.path); err == nil {
		if pd, err := parseDockerfile(d.path, string(existing)); err == nil && pd.version == candidate {
			fmt.Printf("%s: branch %s already pins %s; re-asserting auto-merge\n", dir, branch, candidate)
			return b.ensurePR(branch, dir, d.version, candidate)
		}
	}

	if err := b.commitToBranch(branch, d.path, newContent, candidate); err != nil {
		return err
	}
	return b.ensurePR(branch, dir, d.version, candidate)
}

// commitToBranch resets branch to base, writes newContent, and force-pushes a
// single bump commit. Force-push keeps the branch a clean one-commit delta from
// base even after the base branch advances.
func (b *bumper) commitToBranch(branch, path, newContent, candidate string) error {
	if _, err := b.git("switch", "-C", branch, "origin/"+b.baseBranch); err != nil {
		return fmt.Errorf("creating branch %s: %w", branch, err)
	}
	if err := os.WriteFile(filepath.Join(b.repoRoot, path), []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err := b.git("add", path); err != nil {
		return err
	}
	msg := fmt.Sprintf("%s: bump ca-certificates to %s", filepath.Dir(path), candidate)
	if _, err := b.git("-c", "user.name="+b.gitName, "-c", "user.email="+b.gitEmail,
		"commit", "-m", msg); err != nil {
		return fmt.Errorf("committing %s: %w", path, err)
	}
	if _, err := b.git("push", "-f", "origin", branch); err != nil {
		return fmt.Errorf("pushing %s: %w", branch, err)
	}
	return nil
}

// ensurePR makes sure an open PR exists for branch and that auto-merge is on.
// A failure to enable auto-merge is a warning, not fatal: the PR is the durable
// artifact, and auto-merge can be re-asserted on the next run.
func (b *bumper) ensurePR(branch, dir, oldVersion, candidate string) error {
	num, err := b.prNumberForBranch(branch)
	if err != nil {
		return err
	}
	if num == "" {
		title := fmt.Sprintf("%s: bump ca-certificates to %s", dir, candidate)
		body := prBody(dir, oldVersion, candidate, b.snapshot)
		if _, err := b.run("gh", "pr", "create", "--base", b.baseBranch, "--head", branch,
			"--title", title, "--body", body); err != nil {
			return fmt.Errorf("creating PR for %s: %w", branch, err)
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

func prBody(dir, oldVersion, candidate, snapshot string) string {
	return fmt.Sprintf(`Automated bump by `+"`tools/ccbump`"+`.

| | |
|---|---|
| Dockerfile | `+"`%s/Dockerfile`"+` |
| ca-certificates | `+"`%s`"+` → `+"`%s`"+` |
| Debian snapshot | `+"`%s`"+` |

The new version was detected by running the pinned base image and reading the
latest `+"`ca-certificates`"+` available at the snapshot above. The apt sources are
pinned to `+"`snapshot.debian.org`"+`, so this version stays fetchable forever.

This PR auto-merges once the `+"`build`"+` workflow proves the new pin builds on
both `+"`linux/amd64`"+` and `+"`linux/arm64`"+`. Auto-merge only affects how fresh the
image is; it can never make `+"`main`"+` fail to build.`,
		dir, oldVersion, candidate, snapshot)
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
