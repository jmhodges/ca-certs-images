package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWithVersionsSnapshotOnly(t *testing.T) {
	d, err := parseDockerfile("debian-12/Dockerfile", bookworm)
	if err != nil {
		t.Fatalf("parseDockerfile: %v", err)
	}
	// A snapshot-only refresh keeps the version and rewrites just the
	// snapshot ARG, so the diff is exactly one line.
	out := d.withVersions("20270101T000000Z", d.version)
	if !strings.Contains(out, "ARG DEBIAN_SNAPSHOT=20270101T000000Z") {
		t.Errorf("snapshot ARG not rewritten:\n%s", out)
	}
	if diff := countDifferentLines(bookworm, out); diff != 1 {
		t.Errorf("expected exactly 1 changed line, got %d", diff)
	}
}

func TestDiffManifests(t *testing.T) {
	for _, tt := range []struct {
		name     string
		old, new []string
		want     []string
	}{
		{
			name: "equal",
			old:  []string{"ca-certificates 20230311+deb12u1", "openssl 3.0.11-1~deb12u2"},
			new:  []string{"ca-certificates 20230311+deb12u1", "openssl 3.0.11-1~deb12u2"},
			want: nil,
		},
		{
			name: "changed version",
			old:  []string{"ca-certificates 20230311+deb12u1", "openssl 3.0.11-1~deb12u2"},
			new:  []string{"ca-certificates 20230311+deb12u1", "openssl 3.0.16-1~deb12u1"},
			want: []string{"openssl 3.0.11-1~deb12u2 -> 3.0.16-1~deb12u1"},
		},
		{
			name: "added and removed",
			old:  []string{"ca-certificates 20250419", "libgone 1.0"},
			new:  []string{"ca-certificates 20250419", "libnew 2.0"},
			want: []string{
				"libgone 1.0 (no longer installed)",
				"libnew 2.0 (newly installed)",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := diffManifests(tt.old, tt.new); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("diffManifests = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTargetDirsCSV(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "debian-12"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "debian-12", "Dockerfile"), []byte("FROM debian:12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bumper{repoRoot: root}

	// Entries are trimmed and empties dropped.
	dirs, err := b.targetDirs(" debian-12 , ")
	if err != nil {
		t.Fatalf("targetDirs: %v", err)
	}
	if want := []string{"debian-12"}; !reflect.DeepEqual(dirs, want) {
		t.Errorf("targetDirs = %v, want %v", dirs, want)
	}

	// A named dir without a Dockerfile is an error, not a silent skip.
	if _, err := b.targetDirs("debian-12,debian-99"); err == nil {
		t.Error("targetDirs with missing dir: expected error, got nil")
	}

	// All-whitespace CSV is an error.
	if _, err := b.targetDirs(" , "); err == nil {
		t.Error("targetDirs with empty CSV entries: expected error, got nil")
	}
}
