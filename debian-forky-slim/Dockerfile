# Debian testing (forky), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:forky-slim@sha256:1b7f536fbed024584996bbaa59598e410f980c110dc763c4615f6b07716ea761

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
