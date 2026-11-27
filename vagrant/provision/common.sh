#!/usr/bin/env bash
# vagrant/provision/common.sh — first provisioner for BOTH guests (runs as
# root inside the Ubuntu 24.04 VM). Idempotent: `vagrant provision` re-runs it.
#
# Docker CE from Docker's apt repository (the prod shape xtcp2 deploys in —
# see TODO-SOON §29 and build/containers/docker-compose.yml), plus the
# tooling every scenario wants on both vantages. Attack tooling that is only
# the attacker's (slowhttptest) lands in provision/attacker.sh.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# The cloud image's unattended-upgrades can grab the dpkg frontend lock for
# minutes at a time, and the matrix re-provisions the same VMs for hours —
# rt-11-3..rt-11-5 all failed on it in one run (evidence 20261127T061142).
# Kill the timers, then wait out any in-flight run BEFORE the first apt
# (the docker block below needs the lock too on a fresh VM).
systemctl stop apt-daily.timer apt-daily-upgrade.timer unattended-upgrades 2>/dev/null || true
systemctl mask apt-daily.timer apt-daily-upgrade.timer unattended-upgrades 2>/dev/null || true
for _ in $(seq 1 120); do # bounded: 10 min — an upgrade can run long
  if ! fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock \
    /var/lib/apt/lists/lock /var/cache/apt/archives/lock >/dev/null 2>&1; then
    break
  fi
  sleep 5
done

if ! command -v docker >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg \
    | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg
  echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg]" \
    "https://download.docker.com/linux/ubuntu noble stable" \
    >/etc/apt/sources.list.d/docker.list
  apt-get update -qq
  apt-get install -y -qq docker-ce docker-ce-cli containerd.io
fi

apt-get install -y -qq jq socat tcpdump strace iperf3 nghttp2-client chrony
systemctl enable --now chrony
journalctl --vacuum-size=100M
apt-get clean

# The scenario scripts scp harness files to the vagrant-owned ~/xtcp2-redteam
# before `vagrant provision` runs (vagrant/lib/ssh.sh); victim.sh and
# attacker.sh consume them from there. (Not /tmp: a root provisioner cannot
# leave a dir the vagrant user can scp into.)

echo "common: done (docker $(docker --version | cut -d, -f1))"
