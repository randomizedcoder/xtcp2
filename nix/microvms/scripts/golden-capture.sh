# Golden-record capture (the s3parquet-golden flavor): keep xtcp2's second
# poll, check it, then emit it with a manifest between
# XTCP2_GOLDEN_DUMP_START/_END on the console for the host runner, which
# scrubs it. The raw object holds the guest's real addresses; it never
# leaves the runner's temporary directory.
#
# Why the second poll: the first is immediate, and a namespace whose
# netlink socket is still opening is left to the next cycle, so the first
# poll can miss golden-ns. xtcp2 polls every 30 s and flushes Parquet every
# 5 s, so poll 1 lands in its own object; poll 2 is the object that appears
# after it (a timed flush or Close on stop).
#
# Nothing here opens a TCP connection in the host namespace before poll 2:
# its TIME_WAIT sockets would be recorded, as many as the timing made.
# Metrics come over xtcp2's unix socket; rclone runs only after the stop.
#
# Expects PROM_SOCKET, TOKEN_FILE, BUCKET, CONTAINERS, SOCKETS and FLAVOR from
# the wrapper.
# Any unexpected exit reports itself, so the runner fails fast instead of
# waiting out its timeout.
trap 'echo "GOLDEN_ERR $0: line $LINENO exited $?"' ERR

OUT=/tmp/golden
# From boot: the workload (image load, containers) comes up before xtcp2.
FLUSH_SEC="${GOLDEN_FLUSH_SEC:-300}"
rm -rf "$OUT"
mkdir -p "$OUT"

fail() {
  echo "GOLDEN_ERR $*"
  exit 1
}

export RCLONE_CONFIG=/dev/null
export RCLONE_CONFIG_S3_TYPE=s3
export RCLONE_CONFIG_S3_PROVIDER=Other
export RCLONE_CONFIG_S3_ENDPOINT=http://127.0.0.1:9000
export RCLONE_CONFIG_S3_REGION=us-east-1
export RCLONE_CONFIG_S3_FORCE_PATH_STYLE=true
export RCLONE_CONFIG_S3_ACCESS_KEY_ID=xtcp2test
export RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=xtcp2testsecretkey

# metric prints one xtcp_counts value, by its variable and type labels; 0
# until xtcp2's listener answers.
metric() {
  { curl -sf -m 5 --unix-socket "$PROM_SOCKET" -H "Authorization: Bearer $(cat "$TOKEN_FILE")" http://localhost/metrics 2>/dev/null || true; } |
    awk -v variable="$1" -v type="$2" '
      /^xtcp_counts\{/ && index($1, "variable=\"" variable "\"") && index($1, "type=\"" type "\"") { n = $2 }
      END { print n + 0 }'
}

# wait_for waits up to $4 seconds for metric $1 $2 to reach $3.
wait_for() {
  local n=0
  for _ in $(seq 1 "$4"); do
    n=$(metric "$1" "$2")
    if [ "$n" -ge "$3" ]; then return 0; fi
    sleep 1
  done
  fail "$1 $2: $n of $3 within $4s"
}

echo "GOLDEN_START"
wait_for envelopeFlush poll_end 1 "$FLUSH_SEC"
wait_for upload count 1 20
wait_for envelopeFlush poll_end 2 60
ends=$(metric envelopeFlush poll_end)
timeouts=$(metric envelopeFlush poll_timeout)
[ "$ends" -eq 2 ] && [ "$timeouts" -eq 0 ] ||
  fail "expected two poll_end and no poll_timeout, got $ends and $timeouts"

# The AF_INET sockets each container holds now, for the row-count check
# below. xtcp2 dumps AF_INET only, so the Go servers' dual-stack listeners
# (0.0.0.0 opens an AF_INET6 socket) and the connections they accept are
# not recorded; ss -4 leaves them out too.
for i in $(seq 1 "$CONTAINERS"); do
  pid=$(docker inspect --format '{{.State.Pid}}' "golden-$i")
  nsenter -t "$pid" -n ss -4 -Htan >"$OUT/ss-golden-$i"
done
caps=$(systemctl show xtcp2 -p CapabilityBoundingSet --value)

systemctl stop xtcp2
# Two objects, poll 1's and poll 2's; keep the newer.
listing=""
for _ in $(seq 1 30); do
  listing=$(rclone lsf -R --files-only --include '*.parquet' --format tp --separator ' ' "s3:$BUCKET" 2>/dev/null || true)
  if [ "$(printf '%s' "$listing" | grep -c . || true)" -ge 2 ]; then break; fi
  sleep 1
done
count=$(printf '%s' "$listing" | grep -c . || true)
[ "$count" -eq 2 ] || fail "expected 2 objects (polls 1 and 2), got $count"
second=$(printf '%s\n' "$listing" | sort | tail -n1 | awk '{print $NF}')
rclone copyto "s3:$BUCKET/$second" "$OUT/raw.parquet" || fail "download failed"

q() { duckdb -noheader -list -c "$1" 2>&1 | tail -n1 | tr -d '[:space:]'; }
P="read_parquet('$OUT/raw.parquet')"
rows=$(q "SELECT count(*) FROM $P")
for i in $(seq 1 "$CONTAINERS"); do
  c="golden-$i"
  n=$(q "SELECT count(*) FROM $P WHERE container_name = '$c'")
  [ "$n" -ge 1 ] || fail "$c: no rows"
  est=$(q "SELECT count(*) FROM $P WHERE container_name = '$c' AND inet_diag_msg_state = 1")
  [ "$est" -ge "$SOCKETS" ] || fail "$c: $est established rows, expected at least $SOCKETS"
  bad=$(q "SELECT count(*) FROM $P WHERE container_name = '$c' AND (container_runtime <> 'docker' OR NOT regexp_full_match(container_id, '[0-9a-f]{64}'))")
  [ "$bad" -eq 0 ] || fail "$c: $bad rows without docker runtime and a 64-hex id"
  want=$(grep -c . "$OUT/ss-golden-$i" || true)
  [ "$n" -eq "$want" ] || fail "$c: $n rows, ss -4 lists $want sockets"
done
ns=$(q "SELECT count(*) FROM $P WHERE netns = 'golden-ns'")
[ "$ns" -ge 1 ] || fail "golden-ns: no rows"
host=$(q "SELECT count(*) FROM $P WHERE container_name = '' AND netns <> 'golden-ns'")
[ "$host" -ge 1 ] || fail "host namespace: no rows"
version=$(q "SELECT any_value(daemon_version) FROM $P")

uname -r >"$OUT/uname"
jq -n \
  --arg flavor "$FLAVOR" --arg version "$version" --arg kernel "$(uname -r)" \
  --arg caps "$caps" --argjson containers "$CONTAINERS" --argjson sockets "$SOCKETS" \
  --argjson rows "$rows" --arg captured "$(date -u +%Y-%m-%d)" \
  '{flavor: $flavor, daemon_version: $version, kernel: $kernel,
    capabilities: ($caps | split(" ")),
    workload: {containers: $containers, sockets_per_container: $sockets, named_netns: ["golden-ns"]},
    rows: $rows, captured: $captured}' >"$OUT/manifest.json"
rm -f "$OUT"/ss-golden-*

echo "GOLDEN_ROWS $rows"
echo "XTCP2_GOLDEN_DUMP_START"
tar c -C "$OUT" . | gzip -n | base64 -w0
echo
echo "XTCP2_GOLDEN_DUMP_END"
echo "GOLDEN_DONE"
