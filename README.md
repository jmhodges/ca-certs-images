# ca-certs-images

Dockerfiles that install `ca-certificates` on top of each currently-supported
major version of the official Debian image.

These exist for Debian-based services that need `ca-certificates` to make
outbound TLS connections (calling APIs, fetching dependencies, talking to a
database over TLS) but still want a small image. They're handy as the final
stage of a multistage build. For example, the `golang` build image runs around
1&nbsp;GB, so copying your Go binary from it into one of these `-slim` images
keeps the shipped image tiny while still having the trusted CA roots your binary
needs. The `-slim` images average roughly 30&nbsp;MB compressed, or about
95&nbsp;MB on disk.

Each `FROM` is pinned to **both** the tag and the sha256 digest of the
multi-arch image index, so builds are reproducible while still being readable.

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

## Updating digests

```sh
docker buildx imagetools inspect debian:13 | grep '^Digest:'
```

then update the `FROM` line in the matching directory.
