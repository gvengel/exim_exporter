#!/bin/bash
set -ex
dpkg --add-architecture arm64
find /etc/apt/sources.list.d -name '*.list' -delete
. /etc/lsb-release
cat <<EOT >/etc/apt/sources.list
# amd64
deb [arch=amd64] http://us.archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME} main restricted universe multiverse
deb [arch=amd64] http://us.archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-updates main restricted universe multiverse
deb [arch=amd64] http://us.archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-backports main restricted universe multiverse
deb [arch=amd64] http://us.archive.ubuntu.com/ubuntu ${DISTRIB_CODENAME}-security main restricted universe multiverse
# arm64
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports ${DISTRIB_CODENAME} main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports ${DISTRIB_CODENAME}-updates main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports ${DISTRIB_CODENAME}-backports main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports ${DISTRIB_CODENAME}-security main restricted universe multiverse
EOT
apt-get update
apt-get install -fy \
  build-essential \
  libsystemd-dev \
  gcc-aarch64-linux-gnu \
  libc6-dev-arm64-cross \
  libsystemd-dev:arm64
