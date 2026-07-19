# Debian 13 (trixie)
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:13@sha256:d07d1b51c39f51188e60be9b64e6bf769fa94e187f092bc32b91305cfa34ba5a

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
