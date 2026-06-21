package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplyStructuredEdits runs the structured edits against copies of the real
// repository files so the anchors can't silently rot. It does not touch the
// network or Docker.
func TestApplyStructuredEdits(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	for _, rel := range []string{
		".github/workflows/build.yml",
		".github/dependabot.yml",
		"README.md",
		"docs/dockerhub/overview.md",
	} {
		src, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		dst := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cwd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(cwd) })
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}

	if err := applyStructuredEdits("duke"); err != nil {
		t.Fatalf("applyStructuredEdits: %v", err)
	}

	checks := []struct{ file, want string }{
		// build matrix: forky -> 14 + 14-slim, new duke + duke-slim.
		{".github/workflows/build.yml", "- dir: debian-14\n"},
		{".github/workflows/build.yml", "- dir: debian-14-slim\n"},
		{".github/workflows/build.yml", "- dir: debian-duke\n"},
		{".github/workflows/build.yml", "- dir: debian-duke-slim\n"},
		{".github/workflows/build.yml", "cacertsfriend/ca-certs-images:duke-slim"},
		// dependabot: stable dirs guarded, testing dirs digest-only.
		{".github/dependabot.yml", `directory: "/debian-14"`},
		{".github/dependabot.yml", `directory: "/debian-14-slim"`},
		{".github/dependabot.yml", "debian:14 -> debian:15"},
		{".github/dependabot.yml", "same policy as the full debian-14 image"},
		{".github/dependabot.yml", `directory: "/debian-duke"`},
		{".github/dependabot.yml", `directory: "/debian-duke-slim"`},
		// README status table.
		{"README.md", "| `debian-13`         | 13             | trixie   | oldstable                   |"},
		{"README.md", "| `debian-14`         | 14             | forky    | stable                      |"},
		{"README.md", "| `debian-14-slim`    | 14 (slim)      | forky    | stable                      |"},
		{"README.md", "| `debian-duke`       | testing        | duke     | testing                     |"},
		{"README.md", "| `debian-duke-slim`  | testing (slim) | duke     | testing                     |"},
		// README tags table.
		{"README.md", "| `debian-14`         | `forky`                          | 14             |"},
		{"README.md", "| `debian-duke-slim`  | `duke-slim`, `testing-slim`      | testing (slim) |"},
		// overview: source list, tags table, variant bullets.
		{"docs/dockerhub/overview.md", "blob/main/debian-14/Dockerfile"},
		{"docs/dockerhub/overview.md", "blob/main/debian-duke-slim/Dockerfile"},
		{"docs/dockerhub/overview.md", "| `debian-14`         | `forky`                      | 14             | forky    | stable          |"},
		{"docs/dockerhub/overview.md", "| `debian-duke`       | `duke`, `testing`            | testing        | duke     | testing         |"},
		{"docs/dockerhub/overview.md", "- **`debian-14` (`forky`)** — Debian 14, the current stable release."},
		{"docs/dockerhub/overview.md", "- **`debian-13` (`trixie`)** — Debian 13, oldstable."},
		{"docs/dockerhub/overview.md", "- **`debian-14-slim` (`forky-slim`)** — Debian 14 slim, the current stable release."},
		{"docs/dockerhub/overview.md", "- **`debian-duke-slim` (`duke-slim`, `testing-slim`)** — Debian testing slim."},
	}
	for _, c := range checks {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), c.want) {
			t.Errorf("%s missing expected content:\n%s", c.file, c.want)
		}
	}

	// The old forky anchors must be gone everywhere.
	for _, c := range []struct{ file, gone string }{
		{".github/workflows/build.yml", "- dir: debian-forky"},
		{".github/dependabot.yml", `directory: "/debian-forky"`},
		{"README.md", "debian-forky"},
		{"docs/dockerhub/overview.md", "debian-forky"},
	} {
		b, _ := os.ReadFile(c.file)
		if strings.Contains(string(b), c.gone) {
			t.Errorf("%s still contains stale anchor: %s", c.file, c.gone)
		}
	}
}
