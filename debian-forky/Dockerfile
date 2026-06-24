# Debian testing (forky)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:forky@sha256:55ae88e985e811b8539ad4bb86bff6e8acc163b324296d9863547758e769ef7c

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
