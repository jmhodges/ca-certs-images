# ca-certs-images

Dockerfiles that install `ca-certificates` on top of each currently-supported
major version of the official Debian image.

Each `FROM` is pinned to **both** the tag and the sha256 digest of the
multi-arch image index, so builds are reproducible while still being readable.

| Directory   | Debian version | Codename | Status                              |
|-------------|----------------|----------|-------------------------------------|
| `debian-12` | 12             | bookworm | oldstable / LTS                     |
| `debian-13` | 13             | trixie   | stable                              |

EOL versions (Debian 10 "buster" and earlier) are intentionally excluded.

## Building

```sh
docker build -t ca-certs:debian-13 debian-13/
```

## Updating digests

```sh
docker buildx imagetools inspect debian:13 | grep '^Digest:'
```

then update the `FROM` line in the matching directory.
