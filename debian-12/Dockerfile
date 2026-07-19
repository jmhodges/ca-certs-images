# Debian 12 (bookworm)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12@sha256:9344f8b8992482f80cba753f323adeaf17690076c095ccff6cc9536be98185dc

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
