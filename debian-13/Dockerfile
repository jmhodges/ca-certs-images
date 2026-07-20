# Debian 13 (trixie)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:13@sha256:fac46bff2e02f51425b6e33b0e1169f55dfb053d83511ca28aa50c09fd5ed7a4

# Managed by tools/ccbump -- do not edit by hand.
ARG DEBIAN_SNAPSHOT=20260618T000000Z
ARG CA_CERTIFICATES_VERSION=20250419

# Pin apt at a snapshot.debian.org timestamp so the exact ca-certificates
# version above stays fetchable forever, even after Debian's main mirror
# garbage-collects superseded versions from the pool.
RUN printf '%s\n' \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ trixie main" \
      "deb http://snapshot.debian.org/archive/debian-security/${DEBIAN_SNAPSHOT}/ trixie-security main" \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ trixie-updates main" \
      > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false \
               -o Acquire::Retries=5 update \
    && apt-get install -y --no-install-recommends \
        ca-certificates=${CA_CERTIFICATES_VERSION} \
    && rm -rf /var/lib/apt/lists/*
