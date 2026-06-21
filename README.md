# ca-certs-images

Dockerfiles that install `ca-certificates` on top of each currently-supported
major version of the official Debian image.

Each `FROM` is pinned to **both** the tag and the sha256 digest of the
multi-arch image index, so builds are reproducible while still being readable.

| Directory      | Debian version | Codename | Status                           |
|----------------|----------------|----------|----------------------------------|
| `debian-12`    | 12             | bookworm | oldstable / LTS                  |
| `debian-13`    | 13             | trixie   | stable                           |
| `debian-forky` | testing        | forky    | testing                          |

EOL versions (Debian 10 "buster" and earlier) are intentionally excluded.

## Images

Pull requests build every Dockerfile (for `linux/amd64` and `linux/arm64`)
without pushing. Pushes to `main` build every image, but only **push** (and
sign/attest) the images whose inputs actually changed — a commit that only
touches a README or an unrelated directory will not republish an image. Images
are pushed to Docker Hub under
[`cacertsfriend/ca-certs-images`](https://hub.docker.com/r/cacertsfriend/ca-certs-images):

| Tag            | Aliases              | Debian version |
|----------------|----------------------|----------------|
| `debian-12`    | `bookworm`           | 12             |
| `debian-13`    | `trixie`             | 13             |
| `debian-forky` | `forky`, `testing`   | testing        |

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
  --certificate-identity-regexp '^https://github.com/jmhodges/ca-certs-images/\.github/workflows/build\.yml@' \
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
