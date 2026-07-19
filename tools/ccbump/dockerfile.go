package main

import (
	"fmt"
	"regexp"
)

// A dockerfile is the parsed view of one Dockerfile that ccbump manages. Only
// the two managed ARG anchors (the snapshot timestamp and the ca-certificates
// version) ever change; everything else is read to drive detection.
type dockerfile struct {
	// path is the repo-relative path, e.g. "debian-12/Dockerfile".
	path string
	// content is the full, unmodified file text.
	content string
	// image is the FROM reference, pinned to a digest,
	// e.g. "debian:12@sha256:49ba...".
	image string
	// suite is the Debian suite the snapshot sources point at,
	// e.g. "bookworm" or "trixie".
	suite string
	// snapshot is the current ARG DEBIAN_SNAPSHOT value, e.g. "20260618T000000Z".
	snapshot string
	// version is the current ARG CA_CERTIFICATES_VERSION value,
	// e.g. "20230311+deb12u1".
	version string
}

var (
	fromRe     = regexp.MustCompile(`(?m)^FROM\s+(\S+)`)
	snapshotRe = regexp.MustCompile(`(?m)^ARG\s+DEBIAN_SNAPSHOT=(\S+)`)
	versionRe  = regexp.MustCompile(`(?m)^ARG\s+CA_CERTIFICATES_VERSION=(\S+)`)
	// suiteRe pulls the suite out of the first snapshot "deb" source line, e.g.
	// the "bookworm" in `.../debian/${DEBIAN_SNAPSHOT}/ bookworm main`. Reading
	// it from the file (rather than a dir->suite table) keeps the suite a single
	// source of truth that lives next to the sources it configures.
	suiteRe = regexp.MustCompile(`archive/debian/\$\{DEBIAN_SNAPSHOT\}/\s+(\S+)\s+main`)
)

// parseDockerfile extracts the managed anchors and detection inputs from a
// Dockerfile's text. It returns an error if any required field is missing,
// which means the file is not in the shape ccbump manages.
func parseDockerfile(path, content string) (*dockerfile, error) {
	d := &dockerfile{path: path, content: content}

	for _, f := range []struct {
		name string
		re   *regexp.Regexp
		dst  *string
	}{
		{"FROM", fromRe, &d.image},
		{"ARG DEBIAN_SNAPSHOT", snapshotRe, &d.snapshot},
		{"ARG CA_CERTIFICATES_VERSION", versionRe, &d.version},
		{"snapshot deb source", suiteRe, &d.suite},
	} {
		m := f.re.FindStringSubmatch(content)
		if m == nil {
			return nil, fmt.Errorf("%s: could not find %s line", path, f.name)
		}
		*f.dst = m[1]
	}
	return d, nil
}

// withVersions returns the file content with the two managed ARG anchors
// rewritten to the given snapshot and version. All other bytes are preserved
// exactly, so the diff a bump produces is the two ARG lines and nothing else.
func (d *dockerfile) withVersions(snapshot, version string) string {
	out := snapshotRe.ReplaceAllLiteralString(d.content, "ARG DEBIAN_SNAPSHOT="+snapshot)
	out = versionRe.ReplaceAllLiteralString(out, "ARG CA_CERTIFICATES_VERSION="+version)
	return out
}
