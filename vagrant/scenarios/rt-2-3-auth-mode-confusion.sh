#!/usr/bin/env bash
# vagrant/scenarios/rt-2-3-auth-mode-confusion.sh — RT-2: the auth-mode
# confusion footguns (docs/redteam-plan.md RT-2, plan row rt-2-3).
#
# The plan row's premise: a typo'd mode flag, an unsupported env mode, or an
# unreadable _FILE leaves the daemon unauthenticated (documented-broken,
# EXPECT=CONFIRMED with a hardening issue).
#
# REFUTED 2026-11-23 (verified on the VM): the listener-security-hardening
# made every confusion path fail closed —
#   (a)  -listenerAuthMode rawy (CLI typo) + a token: the typo equals unset,
#        the mode is INFERRED from the credential (auth raw), the daemon
#        serves authenticated;
#   (a2) the same typo with no credential anywhere: exit 64 ("listener auth
#        raw mode requires a token") — refuses to start;
#   (b)  LISTENER_AUTH_MODE=unsupported (invalid env) + a token: the invalid
#        value is ignored, the credential infers raw — authenticated;
#   (c)  LISTENER_RAW_TOKEN_FILE=<unreadable>: exit 64 — refuses to start.
#
# So EXPECT=BLOCKED: no confusion path yields an unauthenticated daemon.
# The residual gap (worth the hardening issue): the CLI typo is silently
# treated as unset — the daemon tells the operator only in the startup
# print, not at the flag.
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
XTCP2_EXPECT="${XTCP2_EXPECT:-BLOCKED}"

# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/matrix.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/evidence.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/ssh.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/deploy.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/invariant.sh"
# shellcheck source=/dev/null
source "$(dirname "${BASH_SOURCE[0]}")/../lib/assert.sh"

xtcp2_matrix_defaults
xtcp2_preflight
evidence_init

echo "rt-2-3: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
victim_still_reporting_tap
xtcp2_victim_read_token
token="$RT_READ_TOKEN"

# The confusion probes run as EXTRA daemon containers next to the prod-shape
# one (the default-listener shape: UDS metrics, env/flag credentials) — the
# daemon image is scratch, so every assertion reads logs + docker inspect,
# never exec.
CAPS="--cap-add=NET_ADMIN --cap-add=SYS_ADMIN --cap-add=SYS_CHROOT"
cleanup() {
  vm_exec victim 'sudo docker rm -f rt2a rt2b rt2c rt2d >/dev/null 2>&1 || true'
}
trap 'cleanup || true' EXIT
cleanup

start() { # start <name> <run-tail>: docker args AND the command (docker run
  # takes its options before the image, the daemon's flags after it)
  local name="$1" tail="$2"
  vm_exec victim "sudo docker run -d --name $name $CAPS $tail" \
    >>"$RT_EVIDENCE/$name.run" 2>&1
  sleep 4
}

state() { # state <name>: "running" or "exited:<code>"
  local st ec
  st="$(vm_exec victim "sudo docker inspect $1 --format '{{.State.Status}}'")"
  ec="$(vm_exec victim "sudo docker inspect $1 --format '{{.State.ExitCode}}'")"
  if [ "$st" = running ]; then
    echo "running"
  else
    echo "exited:$ec"
  fi
}

authed() { # authed <name>: 1 when the log line says "(auth raw)"
  vm_exec victim "sudo docker logs $1 2>&1 | grep -c '(auth raw)'" || echo 0
}

# --- (a) CLI typo + a token: the typo equals unset, the mode is inferred ---
start rt2a "-e LISTENER_RAW_TOKEN=$token xtcp2:latest -listenerAuthMode rawy"
vm_exec victim "sudo docker logs rt2a 2>&1" >"$RT_EVIDENCE/rt2a.log" 2>&1
s="$(state rt2a)"
a="$(authed rt2a)"
echo "rt-2-3: (a) CLI typo + token -> $s, auth-raw lines: $a"
if [ "$s" = running ] && [ "$a" -ge 1 ]; then
  ok "(a) CLI typo + token: daemon stays authenticated (mode inferred from the credential)"
else
  not_ok "(a) CLI typo + token: daemon $s auth-raw:$a — see evidence"
fi

# --- (a2) the same typo with NO credential: refuse to start (exit 64) -----
start rt2b "xtcp2:latest -listenerAuthMode rawy"
vm_exec victim "sudo docker logs rt2b 2>&1" >"$RT_EVIDENCE/rt2b.log" 2>&1
s="$(state rt2b)"
echo "rt-2-3: (a2) CLI typo, no credential -> $s"
if [ "$s" = "exited:64" ]; then
  ok "(a2) CLI typo, no credential: daemon refuses to start (exit 64, fail closed)"
else
  not_ok "(a2) CLI typo, no credential: daemon $s (want exited:64 — see evidence)"
fi

# --- (b) invalid env mode + a token: the invalid value is ignored --------
start rt2c "-e LISTENER_AUTH_MODE=unsupported -e LISTENER_RAW_TOKEN=$token xtcp2:latest"
vm_exec victim "sudo docker logs rt2c 2>&1" >"$RT_EVIDENCE/rt2c.log" 2>&1
s="$(state rt2c)"
a="$(authed rt2c)"
echo "rt-2-3: (b) invalid env mode + token -> $s, auth-raw lines: $a"
if [ "$s" = running ] && [ "$a" -ge 1 ]; then
  ok "(b) invalid env mode + token: ignored; the credential keeps auth on"
else
  not_ok "(b) invalid env mode + token: daemon $s auth-raw:$a — see evidence"
fi

# --- (c) unreadable _FILE: refuse to start (exit 64) ----------------------
start rt2d "-e LISTENER_RAW_TOKEN_FILE=/run/secrets/nosuchfile xtcp2:latest"
vm_exec victim "sudo docker logs rt2d 2>&1" >"$RT_EVIDENCE/rt2d.log" 2>&1
s="$(state rt2d)"
echo "rt-2-3: (c) unreadable token file -> $s"
if [ "$s" = "exited:64" ]; then
  ok "(c) unreadable token file: daemon refuses to start (exit 64, fail closed)"
else
  not_ok "(c) unreadable token file: daemon $s (want exited:64 — see evidence)"
fi

# The one daemon that runs (a/b) must not be unauthenticated on the wire:
# with no token on the prod listener the scrape 401s (the default-listener
# shape (a) uses the SAME token).
victim_still_reporting_tap
cleanup
trap - EXIT

# BLOCKED = no confusion path produced an unauthenticated daemon.
finish rt-2-3 BLOCKED
