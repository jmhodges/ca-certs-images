package main

import (
	"strings"
	"testing"
)

const bookworm = `# Debian 12 (bookworm)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12@sha256:49ba348354a28e39c70beffd6cf43bdb8d55d81ce4b746b0428717d054b8bbc4

# Managed by tools/ccbump -- do not edit by hand.
ARG DEBIAN_SNAPSHOT=20260618T000000Z
ARG CA_CERTIFICATES_VERSION=20230311+deb12u1

RUN printf '%s\n' \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ bookworm main" \
      "deb http://snapshot.debian.org/archive/debian-security/${DEBIAN_SNAPSHOT}/ bookworm-security main" \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ bookworm-updates main" \
      > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false \
               -o Acquire::Retries=5 update \
    && apt-get install -y --no-install-recommends \
        ca-certificates=${CA_CERTIFICATES_VERSION} \
    && rm -rf /var/lib/apt/lists/*
`

func TestParseDockerfile(t *testing.T) {
	d, err := parseDockerfile("debian-12/Dockerfile", bookworm)
	if err != nil {
		t.Fatalf("parseDockerfile: %v", err)
	}
	if got, want := d.image, "debian:12@sha256:49ba348354a28e39c70beffd6cf43bdb8d55d81ce4b746b0428717d054b8bbc4"; got != want {
		t.Errorf("image = %q, want %q", got, want)
	}
	if got, want := d.suite, "bookworm"; got != want {
		t.Errorf("suite = %q, want %q", got, want)
	}
	if got, want := d.snapshot, "20260618T000000Z"; got != want {
		t.Errorf("snapshot = %q, want %q", got, want)
	}
	if got, want := d.version, "20230311+deb12u1"; got != want {
		t.Errorf("version = %q, want %q", got, want)
	}
}

func TestParseDockerfileMissingField(t *testing.T) {
	for _, missing := range []string{"FROM", "ARG DEBIAN_SNAPSHOT", "ARG CA_CERTIFICATES_VERSION"} {
		var b strings.Builder
		for line := range strings.SplitSeq(bookworm, "\n") {
			if strings.HasPrefix(line, missing) {
				continue
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		if _, err := parseDockerfile("x", b.String()); err == nil {
			t.Errorf("removing %q: expected parse error, got nil", missing)
		}
	}
}

func TestWithVersions(t *testing.T) {
	d, err := parseDockerfile("debian-12/Dockerfile", bookworm)
	if err != nil {
		t.Fatalf("parseDockerfile: %v", err)
	}
	out := d.withVersions("20270101T000000Z", "20230311+deb12u2")

	if !strings.Contains(out, "ARG DEBIAN_SNAPSHOT=20270101T000000Z") {
		t.Errorf("snapshot ARG not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "ARG CA_CERTIFICATES_VERSION=20230311+deb12u2") {
		t.Errorf("version ARG not rewritten:\n%s", out)
	}
	// Only the two ARG lines should change; everything else is preserved byte
	// for byte, so the resulting diff is exactly two lines.
	if diff := countDifferentLines(bookworm, out); diff != 2 {
		t.Errorf("expected exactly 2 changed lines, got %d", diff)
	}
	// The rewritten file must re-parse to the new values.
	d2, err := parseDockerfile("debian-12/Dockerfile", out)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if d2.snapshot != "20270101T000000Z" || d2.version != "20230311+deb12u2" {
		t.Errorf("re-parsed values wrong: snapshot=%q version=%q", d2.snapshot, d2.version)
	}
}

func countDifferentLines(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	n := 0
	for i := range al {
		if i >= len(bl) {
			n++
			continue
		}
		if al[i] != bl[i] {
			n++
		}
	}
	return n
}
