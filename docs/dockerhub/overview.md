# ca-certs-images

Official-Debian base images with the `ca-certificates` package already installed.

## Quick reference

- **Maintained by:** [jmhodges](https://github.com/jmhodges) — see [jmhodges/ca-certs-images](https://github.com/jmhodges/ca-certs-images)
- **Where to file issues:** https://github.com/jmhodges/ca-certs-images/issues
- **Supported architectures:** `amd64`, `arm64`
- **Source Dockerfiles:** [`debian-12`](https://github.com/jmhodges/ca-certs-images/blob/main/debian-12/Dockerfile), [`debian-13`](https://github.com/jmhodges/ca-certs-images/blob/main/debian-13/Dockerfile), [`debian-forky`](https://github.com/jmhodges/ca-certs-images/blob/main/debian-forky/Dockerfile)
- **Signing & provenance:** keyless [cosign](https://github.com/sigstore/cosign) signatures, plus SLSA provenance and SBOM attestations

## Supported tags

| Tag            | Aliases            | Debian version | Codename | Status          |
|----------------|--------------------|----------------|----------|-----------------|
| `debian-12`    | `bookworm`         | 12             | bookworm | oldstable / LTS |
| `debian-13`    | `trixie`           | 13             | trixie   | stable          |
| `debian-forky` | `forky`, `testing` | testing        | forky    | testing         |

End-of-life Debian releases (10 "buster" and earlier) are not built.

## What is this?

Plenty of programs need the system trust store to make TLS connections, and the
stock `debian` images don't ship one. The usual fix is to start every
Dockerfile with an `apt-get install ca-certificates` dance. These images do that
once so you don't have to: each is an official `debian` image with
`ca-certificates` installed and the apt lists cleaned up, and nothing else.

Every `FROM` is pinned to both its tag and the sha256 digest of the upstream
multi-arch index, so a given tag rebuilds to the same Debian base until the
digest is bumped on purpose.

## How to use this image

Pull a release by version or codename:

```console
$ docker pull cacertsfriend/ca-certs-images:debian-13
```

Use it as a base in your own Dockerfile:

```dockerfile
FROM cacertsfriend/ca-certs-images:debian-13

# your TLS-using program can now verify certificates out of the box
COPY myapp /usr/local/bin/myapp
ENTRYPOINT ["myapp"]
```

### Verifying what you pulled

Images are signed keylessly with cosign using the GitHub Actions OIDC identity.
Verify a signature against the workflow that built it:

```console
$ cosign verify \
    --certificate-identity-regexp '^https://github.com/jmhodges/ca-certs-images/\.github/workflows/build\.yml@' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    cacertsfriend/ca-certs-images:debian-13
```

Inspect the SLSA provenance or SBOM attached to an image:

```console
$ docker buildx imagetools inspect cacertsfriend/ca-certs-images:debian-13 --format '{{ json .Provenance }}'
$ docker buildx imagetools inspect cacertsfriend/ca-certs-images:debian-13 --format '{{ json .SBOM }}'
```

## Image variants

Each variant tracks one currently-supported Debian release and is built for
`linux/amd64` and `linux/arm64`:

- **`debian-12` (`bookworm`)** — Debian 12, oldstable / LTS.
- **`debian-13` (`trixie`)** — Debian 13, the current stable release.
- **`debian-forky` (`forky`, `testing`)** — Debian testing. Moves often; pin to a
  digest if you need it to hold still.

Tags are mutable: each push to `main` rebuilds them against the latest Debian
base, so `:debian-13` follows Debian 13's security updates over time. Pin to a
digest (`cacertsfriend/ca-certs-images@sha256:...`) if you want a fixed image.

## License

Distributed under the [MIT License](https://github.com/jmhodges/ca-certs-images/blob/main/LICENSE).
The contents of the images are governed by the licenses of Debian and the
packages installed in them.
