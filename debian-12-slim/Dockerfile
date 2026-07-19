# Debian 12 (bookworm), slim
# Pinned to both the tag and the sha256 digest of the multi-arch image index.
FROM debian:12-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

# Managed by tools/ccbump -- do not edit by hand.
ARG DEBIAN_SNAPSHOT=20260618T000000Z
ARG CA_CERTIFICATES_VERSION=20230311+deb12u1

# Pin apt at a snapshot.debian.org timestamp so the exact ca-certificates
# version above stays fetchable forever, even after Debian's main mirror
# garbage-collects superseded versions from the pool.
RUN printf '%s\n' \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ bookworm main" \
      "deb http://snapshot.debian.org/archive/debian-security/${DEBIAN_SNAPSHOT}/ bookworm-security main" \
      "deb http://snapshot.debian.org/archive/debian/${DEBIAN_SNAPSHOT}/ bookworm-updates main" \
      > /etc/apt/sources.list \
    && rm -f /etc/apt/sources.list.d/* \
    && apt-get -o Acquire::Check-Valid-Until=false \
               -o Acquire::Retries=5 update \
    && apt-get install -y --no-install-recommends \
        ca-certificates=${CA_CERTIFICATES_VERSION} \
    && rm -rf /var/lib/apt/lists/*
