# Debian testing (forky)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:forky@sha256:cf1620dec3829671cde00e83c054c4a2e8f7462a059af809f1e252da3de04c1d

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
