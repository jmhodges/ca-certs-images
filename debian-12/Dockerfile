# Debian 12 (bookworm)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:13@sha256:fe7312b5f05bf5f43fad76bcd8945642e4e47a68aefd1b73f447615899d0fac1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
