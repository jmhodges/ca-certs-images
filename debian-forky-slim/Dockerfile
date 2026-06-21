# Debian testing (forky), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:forky-slim@sha256:484c506e686dd6a8e2977a16d2fbab99763486c1c05f93e1dd29e6a26efe6e03

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
