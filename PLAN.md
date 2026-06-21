# Plan: automated `ca-certificates` version bumper (`tools/ccbump`)

A Go tool, run from GitHub Actions, that for each Dockerfile finds the latest
`ca-certificates` available for that exact pinned base image and opens (or
updates) an auto-merging PR bumping the pinned version. A one-time refactor
makes the Dockerfiles machine-updatable **and** immune to build breakage when
Debian publishes a new `ca-certificates`.

## Current repo state (after rebase on `main`)

- `README.md` — describes the Dockerfiles; `FROM` pinned to tag + sha256 digest.
- `debian-12/Dockerfile`, `debian-13/Dockerfile` — install `ca-certificates`,
  currently **unpinned**.
- `.github/workflows/build.yml` — already present:
  - On `pull_request`: builds every Dockerfile for `linux/amd64,linux/arm64`,
    **no push**, no secrets.
  - On push to `main`: builds and pushes the multi-arch images to Docker Hub
    (`cacertsfriend/ca-certs-images`) using the `docker-pusher` environment.
- No Go code yet.

This existing `build.yml` is the CI gate for the bump PRs: a bump PR triggers a
build-only run that proves the new pin actually builds on both arches before
auto-merge lands it.

## Decisions (locked)

1. **Detect the latest version by running the real pinned image** in CI, not by
   parsing a package index over HTTP.
2. **Scope: `ca-certificates` version (+ the snapshot it is pinned against).**
   The `FROM` sha256 digest is intentionally **out of scope**.
3. **One PR per Dockerfile**, separately and independently mergeable.
4. **PRs auto-merge** without human review. Correctness is guaranteed by the
   `build.yml` PR run; auto-merge only affects *freshness*, never whether `main`
   builds.
5. **Pin against `snapshot.debian.org`** so a pinned version can never disappear
   from the archive (see rationale below). Both the snapshot timestamp and the
   `ca-certificates` version are managed anchors.
6. **Version comparison via `dpkg --compare-versions`, run inside the container**
   — no host dependency, no hand-rolled comparator to maintain.
7. **Auth: GitHub App** via `actions/create-github-app-token@v1`, using the
   `APP_CLIENT_ID` and `APP_PRIVATE_KEY` secrets. The App token (not the default
   `GITHUB_TOKEN`) is required so the bump PR triggers `build.yml`.

## Why `snapshot.debian.org` (the key design point)

Debian's main mirror garbage-collects superseded package versions: when
`…u2` is published, `…u1` is deleted from the pool. A bare
`ca-certificates=…u1` pin therefore becomes unresolvable the moment a new
point/security release lands, breaking `docker build` of `main` until the bot's
next run merges — an unacceptable window.

`snapshot.debian.org` archives the complete history of the Debian archive,
addressed by timestamp. A version that existed at a given snapshot is
retrievable from that snapshot **forever**. Pinning the apt sources to a
snapshot timestamp makes the version pin permanently resolvable, so builds never
break. Its only cost is higher latency, which lands **entirely on CI** — both
the bumper and `build.yml`. Downstream consumers pull the prebuilt image from
Docker Hub and never touch snapshot, so they feel nothing.

---

## 1. One-time Dockerfile refactor

Two managed `ARG` anchors and a rewritten `sources.list` pointing at a pinned
snapshot:

```dockerfile
# Debian 12 (bookworm)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12@sha256:<existing-digest-unchanged>

# Managed by tools/ccbump — do not edit by hand.
ARG DEBIAN_SNAPSHOT=<seeded-current-timestamp>          # e.g. 20260615T000000Z
ARG CA_CERTIFICATES_VERSION=<seeded-current-version>    # e.g. 20230311+deb12u1

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
```

Notes:
- `debian-13` uses the `trixie` suite in the three source lines; everything else
  is identical.
- `Acquire::Check-Valid-Until=false` is required because old snapshots' `Release`
  files are past their `Valid-Until`. `Acquire::Retries=5` absorbs snapshot's
  occasional flakiness.
- Pinning **both** anchors keeps the human-readable "we're on version X" in the
  diff and commit message while the snapshot guarantees X is always fetchable.
- `ca-certificates` is `Architecture: all`, so one version is correct across all
  arches of the multi-arch image; detecting on the amd64 runner is sufficient.
- The `ARG`s sit **after** `FROM` so they are in scope for the `RUN`.
- Seed both defaults with REAL current values by running detection (§3) during
  implementation, so the refactor lands building, not with placeholders.

## 2. Repo layout (single small Go module; stdlib + go-github)

```
go.mod
tools/ccbump/
  main.go            # CLI: detect -> compare -> rewrite -> branch/commit/PR
  dockerfile.go      # locate FROM ref + the two ARG lines; parse/rewrite (only ARG lines change)
  dockerfile_test.go # golden-file tests (idempotency; only the ARG lines change)
  detect.go          # run the pinned image at a snapshot; read apt candidate; dpkg compare
  github.go          # go-github: open-or-update PR, enable auto-merge
```

No native version comparator and no `version_test.go`: comparison is delegated
to `dpkg` inside the container (decision 6).

## 3. Detection (run the real image, against the snapshot we will pin)

For each directory:
1. Parse the `FROM` line for the exact `debian:N@sha256:...` reference and read
   the current `DEBIAN_SNAPSHOT` / `CA_CERTIFICATES_VERSION` ARG values.
2. Choose the candidate snapshot timestamp = now (UTC, `YYYYMMDDThhmmssZ`).
3. Run the pinned image with that snapshot's sources and ask apt for the
   candidate version:
   ```sh
   docker run --rm <pinned-ref> sh -c '
     set -e
     printf "%s\n" \
       "deb http://snapshot.debian.org/archive/debian/<SNAP>/ bookworm main" \
       "deb http://snapshot.debian.org/archive/debian-security/<SNAP>/ bookworm-security main" \
       "deb http://snapshot.debian.org/archive/debian/<SNAP>/ bookworm-updates main" \
       > /etc/apt/sources.list
     rm -f /etc/apt/sources.list.d/*
     apt-get -o Acquire::Check-Valid-Until=false -o Acquire::Retries=5 update -qq
     apt-cache policy ca-certificates | awk "/Candidate:/{print \$2}"
   '
   ```
   Because the candidate is read against the **same snapshot** the tool is about
   to pin, the resulting pin is guaranteed resolvable. This is authoritative —
   exactly what `apt-get install` would resolve for that base image/suite.
4. **Validate** the parsed candidate: reject empty / `(none)`, and require it to
   match `^[A-Za-z0-9.+:~-]+$` before it is ever written into the Dockerfile or
   a commit.

## 4. Compare + rewrite

- Compare detected candidate vs. current `CA_CERTIFICATES_VERSION` with
  `dpkg --compare-versions <current> lt <candidate>`, run **inside the
  container** (decision 6) so there is no host `dpkg` dependency.
- If the candidate is strictly newer, rewrite **only** the two `ARG` lines
  (`DEBIAN_SNAPSHOT` and `CA_CERTIFICATES_VERSION`), preserving every other byte.
- If not newer, leave the file untouched (do **not** advance `DEBIAN_SNAPSHOT` on
  its own) so unchanged runs are true no-ops and avoid needless rebuilds / image
  churn.

## 5. PR creation — one per Dockerfile, idempotent, auto-merge

- Deterministic head branch per release, e.g. `ccbump/debian-12`.
- Each run: reset that branch from the default branch, apply the edit, commit
  (`debian-12: bump ca-certificates to <ver>`), push.
- **Skip the push** when the recomputed file content equals what is already on
  the head branch, to avoid re-triggering `build.yml` and PR churn.
- Open-or-update: if an open PR for that head exists, the push updates it; else
  create one and **enable auto-merge** (squash). The PR merges itself once the
  `build.yml` PR run is green.
- If the default branch is already at the latest, no-op and leave no stale
  branch.
- Net guarantee: at most one open bump PR per Debian release; reruns are safe.

## 6. GitHub App authentication

- A GitHub App with **Contents: write** + **Pull requests: write**, installed on
  the repo. Secrets `APP_CLIENT_ID` and `APP_PRIVATE_KEY` are configured.
- The workflow mints a short-lived installation token via
  `actions/create-github-app-token@v1` (its `app-id` input accepts the client
  ID) and passes it to the Go tool as `GITHUB_TOKEN`.
- Using the App token (not the default `GITHUB_TOKEN`) is **required**: the
  default token deliberately cannot trigger other workflows, so a PR opened with
  it would not run `build.yml` and auto-merge would have nothing to gate on.

## 7. Workflow `.github/workflows/update-ca-certificates.yml`

- Triggers: `schedule` (daily cron) + `workflow_dispatch`. Daily keeps the
  images fresh; correctness never depends on cadence because the snapshot pin
  always builds.
- Matrix over `[debian-12, debian-13]` so each release is independent
  (`fail-fast: false`).
- Per job: `actions/checkout`, `actions/setup-go`,
  `actions/create-github-app-token`, then `go run ./tools/ccbump --dir debian-NN`.
- Minimal top-level `permissions:` (`contents: read`); the App token performs
  the privileged Contents/PR writes.
- The tool reads owner/repo from `GITHUB_REPOSITORY`, so the repo rename needs no
  hardcoded name.

## 8. Tests & docs

- Golden-file tests for parse/rewrite: only the two `ARG` lines change; rewriting
  an already-current file is a byte-for-byte no-op (idempotency).
- Validation tests for the candidate-version guard (`(none)`, empty, junk
  rejected).
- No version-comparator unit tests — comparison is `dpkg`'s job (decision 6).
- `README.md`: add an "Automated ca-certificates updates" section describing the
  bot and, importantly, the `snapshot.debian.org` pinning rationale (reproducible
  + never breaks the build; latency lands only on CI, not on consumers who pull
  the prebuilt image).

## Assumptions

- Detection requires Docker in CI (fine on `ubuntu-latest`).
- The refactor PR and the tool land together; the bot maintains the two `ARG`
  values afterward.
- The `FROM` digest is intentionally out of scope.
- The build/publish job (`build.yml`) already exists and serves as the bump PRs'
  CI gate.
