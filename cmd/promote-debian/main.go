// Command promote-debian detects when Debian "forky" has been released as
// stable and, if so, prepares the promotion of the testing directories (both the
// full and -slim variants) to the new stable major and seeds fresh testing
// directories for whatever codename Debian rolled testing on to.
//
// It only mutates the working tree and creates a local commit on a new branch.
// Pushing the branch and opening the pull request is left to the calling
// workflow (which holds the GitHub App token).
//
// It is deliberately idempotent and conservative: it does nothing (exits 0 with
// pr=false) unless every precondition is met, and it hard-fails if any expected
// anchor text is missing so a partial, silent edit can never be committed.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// What we are promoting. forky is testing today; when released it becomes
// Debian 14. These are the only task-specific constants; the *next* testing
// codename is discovered at runtime.
const (
	targetCodename = "forky"
	newMajor       = "14"
	branch         = "promote/debian-" + newMajor

	stableURL  = "https://deb.debian.org/debian/dists/stable/Release"
	testingURL = "https://deb.debian.org/debian/dists/testing/Release"

	blobBase = "https://github.com/jmhodges/ca-certs-images/blob/main/"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := cmdOutput("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if err := os.Chdir(strings.TrimSpace(root)); err != nil {
		return err
	}

	// --- Detection ---
	stableCodename, err := releaseField(stableURL, "Codename")
	if err != nil {
		return err
	}
	stableVersion, err := releaseField(stableURL, "Version")
	if err != nil {
		return err
	}
	fmt.Printf("stable suite: Codename=%s Version=%s\n", orQ(stableCodename), orQ(stableVersion))
	if stableCodename != targetCodename {
		skip("stable is still %q, not %q.", stableCodename, targetCodename)
	}

	newTesting, err := releaseField(testingURL, "Codename")
	if err != nil {
		return err
	}
	fmt.Printf("testing suite: Codename=%s\n", orQ(newTesting))
	if newTesting == "" || newTesting == targetCodename {
		skip("could not determine the new testing codename (got %q).", newTesting)
	}

	// --- Idempotency guards ---
	if dirExists("debian-" + newMajor) {
		skip("debian-%s/ already exists; promotion already merged.", newMajor)
	}
	if !dirExists("debian-" + targetCodename) {
		skip("debian-%s/ is gone; nothing to promote.", targetCodename)
	}
	if cmdOK("git", "ls-remote", "--exit-code", "--heads", "origin", branch) {
		skip("branch %q already exists on origin; a promotion PR is already open.", branch)
	}
	// Wait for the real debian:<newMajor> tags (full and slim) to be published
	// before pinning to them.
	for _, tag := range []string{"debian:" + newMajor, "debian:" + newMajor + "-slim"} {
		if !cmdOK("docker", "buildx", "imagetools", "inspect", tag) {
			skip("forky is stable but the %s Docker tag is not published yet; will retry.", tag)
		}
	}

	// --- Resolve the multi-arch index digests we will pin ---
	digests := map[string]string{}
	for _, ref := range []string{
		"debian:" + newMajor,
		"debian:" + newMajor + "-slim",
		"debian:" + newTesting,
		"debian:" + newTesting + "-slim",
	} {
		d, err := resolveDigest(ref)
		if err != nil {
			return err
		}
		digests[ref] = d
		fmt.Printf("%s -> %s\n", ref, d)
	}

	// --- Mutate the tree ---
	// 1. forky -> the new stable major (full and slim).
	if _, err := cmdOutput("git", "mv", "debian-"+targetCodename, "debian-"+newMajor); err != nil {
		return err
	}
	if _, err := cmdOutput("git", "mv", "debian-"+targetCodename+"-slim", "debian-"+newMajor+"-slim"); err != nil {
		return err
	}
	if err := writeDockerfile("debian-"+newMajor+"/Dockerfile",
		fmt.Sprintf("Debian %s (%s)", newMajor, targetCodename),
		"debian:"+newMajor, digests["debian:"+newMajor]); err != nil {
		return err
	}
	if err := writeDockerfile("debian-"+newMajor+"-slim/Dockerfile",
		fmt.Sprintf("Debian %s (%s), slim", newMajor, targetCodename),
		"debian:"+newMajor+"-slim", digests["debian:"+newMajor+"-slim"]); err != nil {
		return err
	}
	// 2. Seed the new testing directories (full and slim).
	for _, slim := range []bool{false, true} {
		suffix := ""
		comment := fmt.Sprintf("Debian testing (%s)", newTesting)
		if slim {
			suffix = "-slim"
			comment = fmt.Sprintf("Debian testing (%s), slim", newTesting)
		}
		dir := "debian-" + newTesting + suffix
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		ref := "debian:" + newTesting + suffix
		if err := writeDockerfile(dir+"/Dockerfile", comment, ref, digests[ref]); err != nil {
			return err
		}
	}
	// 3. Structured edits to the workflow matrix, dependabot config, README, and
	//    the Docker Hub overview.
	if err := applyStructuredEdits(newTesting); err != nil {
		return err
	}

	// --- Commit on a fresh branch ---
	if _, err := cmdOutput("git", "checkout", "-b", branch); err != nil {
		return err
	}
	if _, err := cmdOutput("git", "add", "-A"); err != nil {
		return err
	}
	commitMsg := fmt.Sprintf("Promote Debian %s to stable (debian-%s); add testing (%s)",
		targetCodename, newMajor, newTesting)
	if _, err := cmdOutput("git",
		"-c", "user.name=ca-certs-bot[bot]",
		"-c", "user.email=ca-certs-bot[bot]@users.noreply.github.com",
		"commit", "-m", commitMsg); err != nil {
		return err
	}

	// --- PR metadata for the workflow ---
	if err := writePRBody(stableVersion, newTesting, digests); err != nil {
		return err
	}
	title := fmt.Sprintf("Promote Debian %s to stable (debian-%s); add debian-%s testing",
		targetCodename, newMajor, newTesting)
	fmt.Printf("Prepared promotion on branch %s\n", branch)
	setOutput("pr", "true")
	setOutput("branch", branch)
	setOutput("title", title)
	return nil
}

// applyStructuredEdits rewrites the build matrix, Dependabot config, README
// tables, and Docker Hub overview. Each replacement asserts its anchor exists so
// we never commit a half-edit. forky and its -slim variant become the new stable
// major; the discovered codename and its -slim variant become the new testing.
func applyStructuredEdits(testing string) error {
	major := newMajor
	forky := targetCodename
	mi, err := strconv.Atoi(major)
	if err != nil {
		return err
	}
	nextMajor := strconv.Itoa(mi + 1)

	// --- .github/workflows/build.yml matrix ---
	matrixEntry := func(dir string, tags ...string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "          - dir: %s\n            tags: |\n", dir)
		for _, t := range tags {
			fmt.Fprintf(&b, "              cacertsfriend/ca-certs-images:%s\n", t)
		}
		return b.String()
	}
	if err := editFile(".github/workflows/build.yml",
		matrixEntry("debian-"+forky, "debian-"+forky, forky, "testing")+
			matrixEntry("debian-"+forky+"-slim", "debian-"+forky+"-slim", forky+"-slim", "testing-slim"),
		matrixEntry("debian-"+major, "debian-"+major, forky)+
			matrixEntry("debian-"+major+"-slim", "debian-"+major+"-slim", forky+"-slim")+
			matrixEntry("debian-"+testing, "debian-"+testing, testing, "testing")+
			matrixEntry("debian-"+testing+"-slim", "debian-"+testing+"-slim", testing+"-slim", "testing-slim"),
	); err != nil {
		return err
	}

	// --- .github/dependabot.yml ---
	// The promoted directories now track a major version, so guard against
	// semver-major jumps like the other stable directories. The new testing
	// directories track a codename tag and stay digest-only.
	stableDep := func(comment, dir string) string {
		return fmt.Sprintf(`  # %s
  - package-ecosystem: "docker"
    directory: "/%s"
    schedule:
      interval: "daily"
    ignore:
      - dependency-name: "debian"
        update-types:
          - "version-update:semver-major"
`, comment, dir)
	}
	testingDep := func(comment, dir string) string {
		return fmt.Sprintf(`  # %s
  - package-ecosystem: "docker"
    directory: "/%s"
    schedule:
      interval: "daily"
`, comment, dir)
	}
	if err := editFile(".github/dependabot.yml",
		testingDep(fmt.Sprintf("Debian testing (%[1]s): tracks the codename tag, so updates are digest-only\n  # and there is no semver major to guard against.", forky), "debian-"+forky)+
			testingDep(fmt.Sprintf("Debian testing (%[1]s), slim: digest-only updates, like the full image.", forky), "debian-"+forky+"-slim"),
		stableDep(fmt.Sprintf("Debian %[2]s (%[1]s): allow minor/point-release and digest (sha) bumps,\n  # but never jump the major version (e.g. debian:%[2]s -> debian:%[3]s).", forky, major, nextMajor), "debian-"+major)+
			stableDep(fmt.Sprintf("Debian %[2]s (%[1]s), slim: same policy as the full debian-%[2]s image.", forky, major), "debian-"+major+"-slim")+
			testingDep(fmt.Sprintf("Debian testing (%[1]s): tracks the codename tag, so updates are digest-only\n  # and there is no semver major to guard against.", testing), "debian-"+testing)+
			testingDep(fmt.Sprintf("Debian testing (%[1]s), slim: digest-only updates, like the full image.", testing), "debian-"+testing+"-slim"),
	); err != nil {
		return err
	}

	// --- README.md: status table (debian-13 stable -> oldstable; forky -> 14) ---
	statusRow := func(dir, ver, code, status string) string {
		return fmt.Sprintf("| %-19s | %-14s | %-8s | %-27s |\n", dir, ver, code, status)
	}
	if err := editFile("README.md",
		statusRow("`debian-13`", "13", "trixie", "stable")+
			statusRow("`debian-13-slim`", "13 (slim)", "trixie", "stable")+
			statusRow("`debian-"+forky+"`", "testing", forky, "testing")+
			statusRow("`debian-"+forky+"-slim`", "testing (slim)", forky, "testing"),
		statusRow("`debian-13`", "13", "trixie", "oldstable")+
			statusRow("`debian-13-slim`", "13 (slim)", "trixie", "oldstable")+
			statusRow("`debian-"+major+"`", major, forky, "stable")+
			statusRow("`debian-"+major+"-slim`", major+" (slim)", forky, "stable")+
			statusRow("`debian-"+testing+"`", "testing", testing, "testing")+
			statusRow("`debian-"+testing+"-slim`", "testing (slim)", testing, "testing"),
	); err != nil {
		return err
	}

	// --- README.md: published tags table (forky rows -> 14 + new testing) ---
	tagsRow := func(dir, aliases, ver string) string {
		return fmt.Sprintf("| %-19s | %-32s | %-14s |\n", dir, aliases, ver)
	}
	if err := editFile("README.md",
		tagsRow("`debian-"+forky+"`", "`"+forky+"`, `testing`", "testing")+
			tagsRow("`debian-"+forky+"-slim`", "`"+forky+"-slim`, `testing-slim`", "testing (slim)"),
		tagsRow("`debian-"+major+"`", "`"+forky+"`", major)+
			tagsRow("`debian-"+major+"-slim`", "`"+forky+"-slim`", major+" (slim)")+
			tagsRow("`debian-"+testing+"`", "`"+testing+"`, `testing`", "testing")+
			tagsRow("`debian-"+testing+"-slim`", "`"+testing+"-slim`, `testing-slim`", "testing (slim)"),
	); err != nil {
		return err
	}

	// --- docs/dockerhub/overview.md ---
	if err := editOverview(testing, forky, major); err != nil {
		return err
	}

	return nil
}

// editOverview updates the Docker Hub overview: the source Dockerfile list, the
// supported-tags table, and the image-variant bullets.
func editOverview(testing, forky, major string) error {
	const path = "docs/dockerhub/overview.md"

	srcLink := func(name string) string {
		return fmt.Sprintf("[`debian-%[1]s`](%[2]sdebian-%[1]s/Dockerfile)", name, blobBase)
	}
	if err := editFile(path,
		srcLink(forky)+", "+srcLink(forky+"-slim"),
		strings.Join([]string{
			srcLink(major), srcLink(major + "-slim"),
			srcLink(testing), srcLink(testing + "-slim"),
		}, ", "),
	); err != nil {
		return err
	}

	// Supported tags table.
	ovRow := func(dir, aliases, ver, code, status string) string {
		return fmt.Sprintf("| %-19s | %-28s | %-14s | %-8s | %-15s |\n", dir, aliases, ver, code, status)
	}
	if err := editFile(path,
		ovRow("`debian-13`", "`trixie`", "13", "trixie", "stable")+
			ovRow("`debian-13-slim`", "`trixie-slim`", "13 (slim)", "trixie", "stable")+
			ovRow("`debian-"+forky+"`", "`"+forky+"`, `testing`", "testing", forky, "testing")+
			ovRow("`debian-"+forky+"-slim`", "`"+forky+"-slim`, `testing-slim`", "testing (slim)", forky, "testing"),
		ovRow("`debian-13`", "`trixie`", "13", "trixie", "oldstable")+
			ovRow("`debian-13-slim`", "`trixie-slim`", "13 (slim)", "trixie", "oldstable")+
			ovRow("`debian-"+major+"`", "`"+forky+"`", major, forky, "stable")+
			ovRow("`debian-"+major+"-slim`", "`"+forky+"-slim`", major+" (slim)", forky, "stable")+
			ovRow("`debian-"+testing+"`", "`"+testing+"`, `testing`", "testing", testing, "testing")+
			ovRow("`debian-"+testing+"-slim`", "`"+testing+"-slim`, `testing-slim`", "testing (slim)", testing, "testing"),
	); err != nil {
		return err
	}

	// Image variants: full bullets.
	if err := editFile(path,
		"- **`debian-13` (`trixie`)** — Debian 13, the current stable release.\n"+
			"- **`debian-"+forky+"` (`"+forky+"`, `testing`)** — Debian testing. Moves often; pin to a\n",
		"- **`debian-13` (`trixie`)** — Debian 13, oldstable.\n"+
			"- **`debian-"+major+"` (`"+forky+"`)** — Debian "+major+", the current stable release.\n"+
			"- **`debian-"+testing+"` (`"+testing+"`, `testing`)** — Debian testing. Moves often; pin to a\n",
	); err != nil {
		return err
	}

	// Image variants: slim bullets.
	if err := editFile(path,
		"- **`debian-13-slim` (`trixie-slim`)** — Debian 13 slim, the current stable release.\n"+
			"- **`debian-"+forky+"-slim` (`"+forky+"-slim`, `testing-slim`)** — Debian testing slim.\n",
		"- **`debian-13-slim` (`trixie-slim`)** — Debian 13 slim, oldstable.\n"+
			"- **`debian-"+major+"-slim` (`"+forky+"-slim`)** — Debian "+major+" slim, the current stable release.\n"+
			"- **`debian-"+testing+"-slim` (`"+testing+"-slim`, `testing-slim`)** — Debian testing slim.\n",
	); err != nil {
		return err
	}

	return nil
}

func writePRBody(stableVersion, testing string, digests map[string]string) error {
	body := fmt.Sprintf("Debian `%[1]s` has been released as stable (Debian %[2]s, version %[3]s). "+
		"This PR was opened automatically by the scheduled `promote-debian` workflow.\n\n"+
		"## Changes\n"+
		"- Moved `debian-%[1]s/` → `debian-%[2]s/` (`%[4]s`) and `debian-%[1]s-slim/` → `debian-%[2]s-slim/` (`%[5]s`), repinned to `debian:%[2]s` / `debian:%[2]s-slim`.\n"+
		"- Added `debian-%[6]s/` (`%[7]s`) and `debian-%[6]s-slim/` (`%[8]s`) for the new testing codename.\n"+
		"- Updated the build matrix, Dependabot config (new stable dirs guard the major; new testing dirs are digest-only), the README tables, and the Docker Hub overview.\n\n"+
		"## Maintainer follow-ups (intentionally not automated)\n"+
		"- Re-tier Debian 12 (`bookworm`) per its LTS/EOL status.\n"+
		"- Confirm the Docker tag aliases (`%[1]s`, `testing`) read the way you want now that `%[1]s` == stable.\n"+
		"- The `build` workflow validates every Dockerfile builds for amd64+arm64 on this PR.\n",
		targetCodename, newMajor, stableVersion,
		digests["debian:"+newMajor], digests["debian:"+newMajor+"-slim"],
		testing, digests["debian:"+testing], digests["debian:"+testing+"-slim"])
	return os.WriteFile("pr-body.md", []byte(body), 0o644)
}

// --- helpers ---

// setOutput emits a key=value pair to the workflow's step outputs (no-op when
// run locally).
func setOutput(key, val string) {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot write GITHUB_OUTPUT:", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s=%s\n", key, val)
}

// skip reports why we are not opening a PR and exits cleanly.
func skip(format string, args ...any) {
	fmt.Printf("No promotion this run: "+format+"\n", args...)
	setOutput("pr", "false")
	os.Exit(0)
}

// releaseField fetches an apt Release file and returns the value of the named
// field (e.g. "Codename"), or "" if the field is absent.
func releaseField(url, field string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	prefix := field + ":"
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), nil
		}
	}
	return "", sc.Err()
}

// resolveDigest returns the sha256 multi-arch index digest for an image ref.
func resolveDigest(ref string) (string, error) {
	out, err := cmdOutput("docker", "buildx", "imagetools", "inspect", ref)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Digest:") {
			d := strings.TrimSpace(strings.TrimPrefix(line, "Digest:"))
			if !strings.HasPrefix(d, "sha256:") {
				return "", fmt.Errorf("unexpected digest for %s: %q", ref, d)
			}
			return d, nil
		}
	}
	return "", fmt.Errorf("no Digest line in imagetools output for %s", ref)
}

func writeDockerfile(path, comment, from, digest string) error {
	content := fmt.Sprintf(`# %s
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM %s@%s

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
`, comment, from, digest)
	return os.WriteFile(path, []byte(content), 0o644)
}

// editFile replaces the first occurrence of old with new, failing if old is
// absent so a partial edit is never written.
func editFile(path, old, new string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(b)
	if !strings.Contains(text, old) {
		return fmt.Errorf("anchor not found in %s:\n%s", path, old)
	}
	return os.WriteFile(path, []byte(strings.Replace(text, old, new, 1)), 0o644)
}

// cmdOutput runs a command and returns its stdout, including stderr in any error.
func cmdOutput(name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.Command(name, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// cmdOK reports whether a command exits successfully, discarding its output.
func cmdOK(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func orQ(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
