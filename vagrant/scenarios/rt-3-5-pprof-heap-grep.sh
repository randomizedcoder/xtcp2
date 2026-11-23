#!/usr/bin/env bash
# vagrant/scenarios/rt-3-5-pprof-heap-grep.sh — RT-3: pprof secret exposure
# (docs/redteam-plan.md RT-3, plan row rt-3-5).
#
# REFUTED 2026-11-23 (verified on the VM): a Go /debug/pprof/heap profile
# is stack/size METADATA (the live-object histogram by allocation stack),
# not a memory image — the token bytes never appear in it (verified: no
# 32-hex substring exists in a decompressed profile at all). The plan
# row's "heap contains secrets (documented)" and its suggested finding
# ("secrets should live in a protected page or be cleared before
# profiling") reflect core-dump thinking: that defense matters for CORE
# DUMPS (a real memory image — reachable only with process privileges
# beyond the admin credential), not for pprof endpoints.
# The admin surface still holds: pprof is admin-gated (401/403/200), and
# the argv-shaped leak is dead by construction (cmdline is wired to
# http.NotFound, cmd/xtcp2/listeners.go:305). EXPECT=BLOCKED.
#
# The prod shape serves no debug class, so the scenario runs an EXTRA
# daemon container on the victim VM with a debug listener carrying the same
# harness secrets (loopback TCP, tokenFile+adminTokenFile) — the shape an
# operator who enabled inspection would run. Asserts:
#   - pprof is admin-gated: 401 (none) / 403 (read) / 200 (admin)
#   - the read token's bytes appear in the admin heap dump — CONFIRMED
#   - the finding (secrets should not sit in greppable heap; the mitigation
#     is pprof-off-by-default, which the prod shape already enforces)
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

echo "rt-3-5: evidence in $RT_EVIDENCE"

xtcp2_deploy victim
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"
victim_still_reporting_tap

xtcp2_victim_read_token
read_token="$RT_READ_TOKEN"
admin_token="$(vm_exec_root victim 'cat /opt/xtcp2-redteam/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token on the victim'

# --- the inspection-shape daemon (extra container, debug listener) -----------
# Loopback TCP with the same harness credentials: the shape an operator
# enabling inspection runs. The daemon needs a dest; the victim's catcher
# receives this container's records too (the invariant tap still passes —
# the extra daemon reports MORE, never less).
vm_exec victim 'sudo docker rm -f rt3-5 >/dev/null 2>&1 || true'
vm_exec victim 'sudo docker run -d --name rt3-5 --network host \
  --cap-add=NET_ADMIN --cap-add=SYS_ADMIN --cap-add=SYS_CHROOT \
  -v /opt/xtcp2-redteam/secrets:/run/secrets:ro \
  -v /run/netns:/run/netns:ro,rslave \
  -v /run/docker/netns:/run/docker/netns:ro,rslave \
  --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
  xtcp2:latest \
  -listener "name=debug,serves=debug,address=127.0.0.1:9089,network=tcp,tokenFile=/run/secrets/read,adminTokenFile=/run/secrets/admin" \
  -dest "tcp:127.0.0.1:9999" -marshal jsonl -frequency 2s -timeout 1s \
  -hostname rt-victim' >/dev/null
wait_for 60 "the inspection daemon answers on 9089" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9089/debug/pprof/ | grep -qE '^(200|401|403)\$'"

cleanup() { vm_exec victim 'sudo docker rm -f rt3-5 >/dev/null 2>&1' || true; }
trap 'cleanup || true' EXIT

# --- the gate: pprof is admin-only ------------------------------------------
# The heap profile also lands on the VM (the grep reads it there).
none_code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9089/debug/pprof/heap")"
read_code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $read_token' http://127.0.0.1:9089/debug/pprof/heap")"
vm_exec victim "curl -s -H 'Authorization: Bearer $admin_token' http://127.0.0.1:9089/debug/pprof/heap -o /tmp/rt3-5-heap.bin"
admin_code="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $admin_token' http://127.0.0.1:9089/debug/pprof/heap")"
echo "rt-3-5: pprof/heap gate — none:$none_code read:$read_code admin:$admin_code"
if [ "$none_code" = 401 ] && [ "$read_code" = 403 ] && [ "$admin_code" = 200 ]; then
  ok "pprof is admin-gated (401/403/200)"
else
  not_ok "pprof gate is 401/403/200 (got $none_code/$read_code/$admin_code)"
fi

# --- the heap-grep: the read token's bytes in the admin profile -------------
# pprof/heap is gzip-compressed protobuf: grep through a decompressed copy
# (the evidence keeps the original profile bytes).
vm_exec victim "curl -s -H 'Authorization: Bearer $admin_token' http://127.0.0.1:9089/debug/pprof/heap" \
  >"$RT_EVIDENCE/heap.bin" 2>/dev/null
heap_bytes="$(wc -c <"$RT_EVIDENCE/heap.bin")"
echo "rt-3-5: heap profile $heap_bytes bytes"
hits="$(vm_exec victim "gzip -dc /tmp/rt3-5-heap.bin | grep -ac '$read_token'" || true)"
admin_hits="$(vm_exec victim "gzip -dc /tmp/rt3-5-heap.bin | grep -ac '$admin_token'" || true)"
any_hex="$(vm_exec victim "gzip -dc /tmp/rt3-5-heap.bin | grep -acE '[0-9a-f]{32}'" || true)"
echo "rt-3-5: decompressed heap: read-token $hits, admin-token $admin_hits, ANY 32-hex-string $any_hex"
if [ "${hits:-1}" = 0 ] && [ "${admin_hits:-1}" = 0 ]; then
  ok "REFUTED: no secret bytes in the heap profile (neither credential appears; a Go heap profile is stack/size metadata, not a memory image)"
else
  not_ok "secret bytes found in the heap profile (read:$hits admin:$admin_hits) — see evidence"
fi

# The one argv-shaped leak is dead by construction: cmdline is wired to
# http.NotFound (cmd/xtcp2/listeners.go:305) because argv can carry tokens.
cmdline="$(vm_exec victim "curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer $admin_token' http://127.0.0.1:9089/debug/pprof/cmdline")"
if [ "$cmdline" = 404 ]; then
  ok "/debug/pprof/cmdline is never served (404 even for the admin token) — the argv secret path is dead"
else
  not_ok "/debug/pprof/cmdline answered $cmdline (want 404) — argv may be leaking"
fi

# --- the read-credential non-exfil: the read token gets 403 — no profile ---
victim_still_reporting_tap
cleanup
trap - EXIT
victim_still_reporting_tap

# BLOCKED: no secret byte is extractable from the admin pprof surface (the
# heap-profile premise refuted; cmdline never served).
finish rt-3-5 BLOCKED
