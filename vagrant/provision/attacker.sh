#!/usr/bin/env bash
# vagrant/provision/attacker.sh — runs as root inside the attacker VM, after
# common.sh. The host-side attacker vantage: the repo's static client binaries
# (scp'd by the scenario app as xtcp2-redteam-attacker-tools.tar — built by
# nix/redteam.nix with a static-linkage assertion) plus the load/DoS tooling
# Ubuntu packages. Missing tar is a warning, not an error: rt-1-1 attacks from
# the co-tenant container and never touches this VM.
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Application-layer DoS simulator (universe) + the HTTP/2 storm driver
# (nghttp2-client ships h2load). slowhttptest is attack-only: it stays off the
# victim so the victim's base-OS remains the monitored prod shape.
apt-get install -y -qq slowhttptest

mkdir -p /opt/xtcp2-redteam
if [ -f /home/vagrant/xtcp2-redteam/xtcp2-redteam-attacker-tools.tar ]; then
  tar -xf /home/vagrant/xtcp2-redteam/xtcp2-redteam-attacker-tools.tar -C /opt/xtcp2-redteam
  ls -l /opt/xtcp2-redteam/bin
else
  echo "attacker.sh: /home/vagrant/xtcp2-redteam/xtcp2-redteam-attacker-tools.tar missing — the scenario app scps it before provisioning; run via nix run .#redteam-*" >&2
fi

echo "attacker: done (slowhttptest $(slowhttptest --help 2>&1 | head -1 || true))"
