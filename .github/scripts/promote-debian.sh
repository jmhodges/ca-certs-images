#!/usr/bin/env bash
# Detect when Debian "forky" has been released as stable and, if so, prepare the
# promotion of the testing directory to the new stable major and seed a fresh
# testing directory for whatever codename Debian rolled testing on to.
#
# This script only mutates the working tree and creates a local commit on a new
# branch. Pushing the branch and opening the pull request is left to the calling
# workflow (which holds the GitHub App token).
#
# It is deliberately idempotent and conservative: it does nothing (exits 0 with
# pr=false) unless every precondition is met, and it hard-fails if any expected
# anchor text is missing so a partial, silent edit can never be committed.
set -euo pipefail

# --- What we are promoting -------------------------------------------------
# forky is testing today; when released it becomes Debian 14. These are the only
# task-specific constants; the *next* testing codename is discovered at runtime.
TARGET_CODENAME="forky"
NEW_MAJOR="14"
BRANCH="promote/debian-${NEW_MAJOR}"

STABLE_URL="https://deb.debian.org/debian/dists/stable/Release"
TESTING_URL="https://deb.debian.org/debian/dists/testing/Release"

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

# Emit a key=value pair to the workflow's step outputs (no-op when run locally).
set_output() {
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    echo "$1=$2" >>"$GITHUB_OUTPUT"
  fi
}

# Report why we are not opening a PR and exit cleanly.
skip() {
  echo "No promotion this run: $1"
  set_output pr false
  exit 0
}

release_field() {
  # release_field <url> <Field>
  curl -fsSL "$1" | awk -v f="^$2:" '$0 ~ f {print $2; exit}'
}

# --- Detection -------------------------------------------------------------
stable_codename="$(release_field "$STABLE_URL" Codename || true)"
stable_version="$(release_field "$STABLE_URL" Version || true)"
echo "stable suite: Codename=${stable_codename:-?} Version=${stable_version:-?}"

if [[ "$stable_codename" != "$TARGET_CODENAME" ]]; then
  skip "stable is still '${stable_codename:-unknown}', not '${TARGET_CODENAME}'."
fi

new_testing_codename="$(release_field "$TESTING_URL" Codename || true)"
echo "testing suite: Codename=${new_testing_codename:-?}"
if [[ -z "$new_testing_codename" || "$new_testing_codename" == "$TARGET_CODENAME" ]]; then
  skip "could not determine the new testing codename (got '${new_testing_codename:-empty}')."
fi

# --- Idempotency guards ----------------------------------------------------
if [[ -d "debian-${NEW_MAJOR}" ]]; then
  skip "debian-${NEW_MAJOR}/ already exists; promotion already merged."
fi
if [[ ! -d "debian-${TARGET_CODENAME}" ]]; then
  skip "debian-${TARGET_CODENAME}/ is gone; nothing to promote."
fi
if git ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
  skip "branch '$BRANCH' already exists on origin; a promotion PR is already open."
fi

# Wait for the real debian:${NEW_MAJOR} tag to be published before pinning to it.
if ! docker buildx imagetools inspect "debian:${NEW_MAJOR}" >/dev/null 2>&1; then
  skip "forky is stable but the debian:${NEW_MAJOR} Docker tag is not published yet; will retry."
fi

# --- Resolve the multi-arch index digests we will pin ----------------------
resolve_digest() {
  # resolve_digest <ref> -> sha256:...
  local d
  d="$(docker buildx imagetools inspect "$1" | awk '/^Digest:/ {print $2; exit}')"
  if [[ "$d" != sha256:* ]]; then
    echo "could not resolve digest for $1 (got '$d')" >&2
    exit 1
  fi
  echo "$d"
}

stable_digest="$(resolve_digest "debian:${NEW_MAJOR}")"
testing_digest="$(resolve_digest "debian:${new_testing_codename}")"
echo "debian:${NEW_MAJOR} -> ${stable_digest}"
echo "debian:${new_testing_codename} -> ${testing_digest}"

# --- Mutate the tree -------------------------------------------------------
write_dockerfile() {
  # write_dockerfile <path> <comment> <from-ref> <digest>
  cat >"$1" <<EOF
# $2
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM $3@$4

RUN apt-get update \\
    && apt-get install -y --no-install-recommends ca-certificates \\
    && rm -rf /var/lib/apt/lists/*
EOF
}

# 1. forky -> the new stable major.
git mv "debian-${TARGET_CODENAME}" "debian-${NEW_MAJOR}"
write_dockerfile "debian-${NEW_MAJOR}/Dockerfile" \
  "Debian ${NEW_MAJOR} (${TARGET_CODENAME})" "debian:${NEW_MAJOR}" "$stable_digest"

# 2. Seed the new testing directory.
mkdir -p "debian-${new_testing_codename}"
write_dockerfile "debian-${new_testing_codename}/Dockerfile" \
  "Debian testing (${new_testing_codename})" "debian:${new_testing_codename}" "$testing_digest"

# 3. Structured edits to the workflow matrix, dependabot config, and README.
#    Each replacement asserts its anchor exists so we never commit a half-edit.
NEW_MAJOR="$NEW_MAJOR" TARGET_CODENAME="$TARGET_CODENAME" \
NEW_TESTING="$new_testing_codename" python3 - <<'PY'
import os, sys

major = os.environ["NEW_MAJOR"]
forky = os.environ["TARGET_CODENAME"]
testing = os.environ["NEW_TESTING"]

def edit(path, old, new):
    with open(path, encoding="utf-8") as f:
        text = f.read()
    if old not in text:
        sys.exit(f"anchor not found in {path}:\n{old}")
    with open(path, "w", encoding="utf-8") as f:
        f.write(text.replace(old, new, 1))

# --- .github/workflows/build.yml matrix ---
edit(
    ".github/workflows/build.yml",
    f"""          - dir: debian-{forky}
            tags: |
              cacertsfriend/ca-certs-images:debian-{forky}
              cacertsfriend/ca-certs-images:{forky}
              cacertsfriend/ca-certs-images:testing
""",
    f"""          - dir: debian-{major}
            tags: |
              cacertsfriend/ca-certs-images:debian-{major}
              cacertsfriend/ca-certs-images:{forky}
          - dir: debian-{testing}
            tags: |
              cacertsfriend/ca-certs-images:debian-{testing}
              cacertsfriend/ca-certs-images:{testing}
              cacertsfriend/ca-certs-images:testing
""",
)

# --- .github/dependabot.yml ---
# The promoted directory now tracks a major version, so guard against
# semver-major jumps like the other stable directories. The new testing
# directory tracks a codename tag and stays digest-only.
edit(
    ".github/dependabot.yml",
    f"""  # Debian testing (forky): tracks the codename tag, so updates are digest-only
  # and there is no semver major to guard against.
  - package-ecosystem: "docker"
    directory: "/debian-{forky}"
    schedule:
      interval: "daily"
""",
    f"""  # Debian {major} ({forky}): allow minor/point-release and digest (sha) bumps,
  # but never jump the major version (e.g. debian:{major} -> debian:{int(major)+1}).
  - package-ecosystem: "docker"
    directory: "/debian-{major}"
    schedule:
      interval: "daily"
    ignore:
      - dependency-name: "debian"
        update-types:
          - "version-update:semver-major"
  # Debian testing ({testing}): tracks the codename tag, so updates are
  # digest-only and there is no semver major to guard against.
  - package-ecosystem: "docker"
    directory: "/debian-{testing}"
    schedule:
      interval: "daily"
""",
)

# --- README.md: status table ---
edit(
    "README.md",
    f"| `debian-13`    | 13             | trixie   | stable                           |\n"
    f"| `debian-{forky}` | testing        | {forky}    | testing                          |\n",
    f"| `debian-13`    | 13             | trixie   | oldstable                        |\n"
    f"| `debian-{major}`    | {major}             | {forky}    | stable                           |\n"
    f"| `debian-{testing}`  | testing        | {testing}     | testing                          |\n",
)

# --- README.md: published tags table ---
edit(
    "README.md",
    f"| `debian-13`    | `trixie`             | 13             |\n"
    f"| `debian-{forky}` | `{forky}`, `testing`   | testing        |\n",
    f"| `debian-13`    | `trixie`             | 13             |\n"
    f"| `debian-{major}`    | `{forky}`              | {major}             |\n"
    f"| `debian-{testing}`  | `{testing}`, `testing`    | testing        |\n",
)
PY

# --- Commit on a fresh branch ----------------------------------------------
git checkout -b "$BRANCH"
git add -A
git -c user.name="ca-certs-bot[bot]" \
    -c user.email="ca-certs-bot[bot]@users.noreply.github.com" \
    commit -m "Promote Debian ${TARGET_CODENAME} to stable (debian-${NEW_MAJOR}); add testing (${new_testing_codename})"

# --- PR metadata for the workflow ------------------------------------------
title="Promote Debian ${TARGET_CODENAME} to stable (debian-${NEW_MAJOR}); add debian-${new_testing_codename} testing"
cat >pr-body.md <<EOF
Debian \`${TARGET_CODENAME}\` has been released as stable (Debian ${NEW_MAJOR}, version ${stable_version}). This PR was opened automatically by the scheduled \`promote-debian\` workflow.

## Changes
- Moved \`debian-${TARGET_CODENAME}/\` → \`debian-${NEW_MAJOR}/\` and repinned \`FROM debian:${NEW_MAJOR}\` (\`${stable_digest}\`).
- Added \`debian-${new_testing_codename}/\` for the new testing codename, pinned \`FROM debian:${new_testing_codename}\` (\`${testing_digest}\`).
- Updated the build matrix, Dependabot config (new stable dir guards the major; new testing dir is digest-only), and the README tables.

## Maintainer follow-ups (intentionally not automated)
- Re-tier Debian 12 (\`bookworm\`) per its LTS/EOL status.
- Confirm the Docker tag aliases (\`forky\`, \`testing\`) read the way you want now that \`forky\` == stable.
- The \`build\` workflow validates both new Dockerfiles build for amd64+arm64 on this PR.
EOF

echo "Prepared promotion on branch $BRANCH"
set_output pr true
set_output branch "$BRANCH"
set_output title "$title"
