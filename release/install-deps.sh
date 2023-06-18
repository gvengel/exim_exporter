#!/bin/bash
set -ex
dpkg --add-architecture arm64
sed 's/^deb http/deb \[arch=amd64\] http/' -i /etc/apt/sources.list
. /etc/lsb-release
cat <<EOT >/etc/apt/sources.list.d/arm64.list
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports/ ${DISTRIB_CODENAME} main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports/ ${DISTRIB_CODENAME}-updates main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports/ ${DISTRIB_CODENAME}-backports main restricted universe multiverse
deb [arch=arm64] http://ports.ubuntu.com/ubuntu-ports/ ${DISTRIB_CODENAME}-security main restricted universe multiverse
EOT
apt-get update
apt-get install -y --no-install-recommends \
  build-essential \
  libsystemd-dev \
  gcc-aarch64-linux-gnu \
  libc6-dev-arm64-cross \
  libsystemd-dev:arm64
