# ca-certs-images

Dockerfiles that install `ca-certificates` on top of each currently-supported
major version of the official Debian image.

Each `FROM` is pinned to **both** the tag and the sha256 digest of the
multi-arch image index, so builds are reproducible while still being readable.

The installed `ca-certificates` version is pinned too. apt is pointed at a
[`snapshot.debian.org`](https://snapshot.debian.org) timestamp, and both the
snapshot and the exact `ca-certificates` version live in two managed `ARG`s at
the top of each Dockerfile:

```dockerfile
# Managed by tools/ccbump -- do not edit by hand.
ARG DEBIAN_SNAPSHOT=20260618T000000Z
ARG CA_CERTIFICATES_VERSION=20230311+deb12u1
```

Pinning apt to a snapshot is what makes the version pin durable. Debian's main
mirror garbage-collects superseded package versions — when `…u2` ships, `…u1` is
deleted from the pool — so a bare version pin would become unresolvable the
moment a new release lands and break `docker build` of `main`. `snapshot.debian.org`
archives the whole history by timestamp, so a version that existed at a given
snapshot stays fetchable from it forever. The only cost is higher latency, and it
falls entirely on CI; downstream consumers pull the prebuilt image and never
touch snapshot.

| Directory           | Debian version | Codename | Status                      |
|---------------------|----------------|----------|-----------------------------|
| `debian-12`         | 12             | bookworm | oldstable / LTS             |
| `debian-12-slim`    | 12 (slim)      | bookworm | oldstable / LTS             |
| `debian-13`         | 13             | trixie   | stable                      |
| `debian-13-slim`    | 13 (slim)      | trixie   | stable                      |
| `debian-forky`      | testing        | forky    | testing                     |
| `debian-forky-slim` | testing (slim) | forky    | testing                     |

Each `-slim` directory builds on the matching official `debian:<tag>-slim`
image, which drops files (man pages, locales, etc.) most container workloads
don't need.

EOL versions (Debian 10 "buster" and earlier) are intentionally excluded.

## Images

Pull requests build every Dockerfile (for `linux/amd64` and `linux/arm64`)
without pushing. Pushes to `main` build every image, but only **push** (and
sign/attest) the images whose inputs actually changed — a commit that only
touches a README or an unrelated directory will not republish an image. Images
are pushed to Docker Hub under
[`cacertsfriend/ca-certs-images`](https://hub.docker.com/r/cacertsfriend/ca-certs-images):

| Tag                 | Aliases                          | Debian version |
|---------------------|----------------------------------|----------------|
| `debian-12`         | `bookworm`                       | 12             |
| `debian-12-slim`    | `bookworm-slim`                  | 12 (slim)      |
| `debian-13`         | `trixie`                         | 13             |
| `debian-13-slim`    | `trixie-slim`                    | 13 (slim)      |
| `debian-forky`      | `forky`, `testing`               | testing        |
| `debian-forky-slim` | `forky-slim`, `testing-slim`     | testing (slim) |

```sh
docker pull cacertsfriend/ca-certs-images:debian-13
```

Publishing requires the `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository
secrets to be configured.

## Verifying images

Pushed images are keylessly signed with [cosign](https://github.com/sigstore/cosign)
using the GitHub Actions OIDC identity, and carry SLSA provenance and SBOM
attestations. Verify a signature against the workflow identity:

```sh
cosign verify \
  --certificate-identity-regexp '^https://github.com/jmhodges/ca-certs-images/\.github/workflows/build\.yml@refs/heads/main$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  cacertsfriend/ca-certs-images:debian-13
```

Inspect the provenance / SBOM attestations:

```sh
docker buildx imagetools inspect cacertsfriend/ca-certs-images:debian-13 --format '{{ json .Provenance }}'
docker buildx imagetools inspect cacertsfriend/ca-certs-images:debian-13 --format '{{ json .SBOM }}'
```

## Building

```sh
docker build -t ca-certs:debian-13 debian-13/
```

## Updating the `ca-certificates` version

This is automated by [`tools/ccbump`](tools/ccbump), run daily by the
[`ccbump`](.github/workflows/ccbump.yml) workflow. For each Dockerfile it:

1. runs the exact pinned base image and points apt at a current snapshot,
2. reads the latest `ca-certificates` available there,
3. compares it against the pinned version with `dpkg --compare-versions`
   (run inside the container — no host-side comparator), and
4. if it is newer, opens or updates a per-Dockerfile, auto-merging PR that
   rewrites the two managed `ARG`s.

Each bump PR is built by the [`build`](.github/workflows/build.yml) workflow on
both `linux/amd64` and `linux/arm64`. The `build-all` check (the required status
check on `main`) gates auto-merge, so a bump can only ever change how fresh the
image is — never whether `main` builds.

Run it yourself to inspect what it would do:

```sh
cd tools/ccbump
go test ./...
go run . -repo-root ../.. -detect-only   # print latest detected version per Dockerfile
go run . -repo-root ../.. -dry-run        # also compare + report bumps, without touching git/GitHub
```

The workflow needs the `APP_CLIENT_ID` and `APP_PRIVATE_KEY` secrets for a
GitHub App installed on this repo with **contents: write** and
**pull-requests: write** permissions. The App token (not the default
`GITHUB_TOKEN`) is required so the bump PR triggers the `build` run that gates
auto-merge.

## Updating the `FROM` digest

The base-image digests are bumped automatically by Dependabot (see
[`.github/dependabot.yml`](.github/dependabot.yml)); it is intentionally out of
ccbump's scope. To do it by hand:

```sh
docker buildx imagetools inspect debian:13 | grep '^Digest:'
```

then update the `FROM` line in the matching directory.
