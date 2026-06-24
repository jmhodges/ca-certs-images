# Debian 13 (trixie)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:13@sha256:d07d1b51c39f51188e60be9b64e6bf769fa94e187f092bc32b91305cfa34ba5a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
