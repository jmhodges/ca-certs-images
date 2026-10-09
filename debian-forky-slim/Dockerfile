# Debian testing (forky), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:forky-slim@sha256:7bb96d9b4ccd7f46f54853c258a56df0c6b628a63e65c2317959da18fd36932c

# Managed by tools/ccbump -- do not edit by hand.
ARG DEBIAN_SNAPSHOT=20261009T000000Z
ARG CA_CERTIFICATES_VERSION=20260816

# Pin apt at a snapshot.debian.org timestamp so the exact ca-certificates
# version above stays fetchable forever, even after Debian's main mirror
# garbage-collects superseded versions from the pool.
RUN printf '%s\n' \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ forky main" \
      "deb http://snapshot.debian.org/archive/debian-security/${DEBIAN_SNAPSHOT}/ forky-security main" \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ forky-updates main" \
      > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false \
               -o Acquire::Retries=5 update \
    && apt-get install -y --no-install-recommends \
        ca-certificates=${CA_CERTIFICATES_VERSION} \
    && rm -rf /var/lib/apt/lists/*
