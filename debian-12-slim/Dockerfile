# Debian 12 (bookworm), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
