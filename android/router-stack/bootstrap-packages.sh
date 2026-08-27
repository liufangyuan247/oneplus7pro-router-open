#!/data/data/com.termux/files/usr/bin/bash
set -euo pipefail

export HOME=/data/data/com.termux/files/home
export PREFIX=/data/data/com.termux/files/usr
export PATH="$PREFIX/bin:$PREFIX/bin/applets"
export TMPDIR="$PREFIX/tmp"
export DEBIAN_FRONTEND=noninteractive

pkg update -y
apt-get \
  -o Dpkg::Options::=--force-confold \
  -o Dpkg::Options::=--force-confdef \
  -y --fix-broken install
apt-get \
  -o Dpkg::Options::=--force-confold \
  -o Dpkg::Options::=--force-confdef \
  -y dist-upgrade

# Native service/runtime dependencies. OpenList is staged
# separately because they are distributed as upstream release binaries.
apt-get \
  -o Dpkg::Options::=--force-confold \
  -o Dpkg::Options::=--force-confdef \
  install -y \
  aria2 \
  caddy \
  coreutils \
  curl \
  iproute2 \
  jq \
  openssh \
  procps \
  root-repo \
  runit \
  termux-services \
  unzip \
  wget \
  x11-repo

pkg update -y

# Router/firewall utilities are supplied by the Termux root repository.
# qBittorrent availability differs between repository snapshots and is
# handled by the desktop deployment after this bootstrap step.
apt-get \
  -o Dpkg::Options::=--force-confold \
  -o Dpkg::Options::=--force-confdef \
  install -y dnsmasq iptables

echo 'TERMUX_PACKAGES_READY'
