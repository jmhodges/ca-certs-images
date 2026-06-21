# Debian 12 (bookworm), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12-slim@sha256:96e378d7e6531ac9a15ad505478fcc2e69f371b10f5cdf87857c4b8188404716

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
