# Shared by the golden-record capture and the tcp-families check: both keep
# xtcp2's second poll (the first is immediate, and a namespace whose netlink
# socket is still opening is left to the next cycle), so both need the same
# metric waits and the same object fetch.
#
# Expects PROM_SOCKET, TOKEN_FILE and BUCKET, and ERR_TAG, the console
# marker a failure prints (the host runner watches for it).
#
# Nothing here opens a TCP connection in the host namespace before the
# second poll: its TIME_WAIT sockets would be recorded. Metrics come over
# xtcp2's unix socket; rclone runs only after xtcp2 is stopped.

fail() {
  echo "$ERR_TAG $*"
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

# wait_second_poll waits for poll 2 to end, $1 seconds at most for poll 1
# (from boot: the workload comes up before xtcp2), and checks no poll timed
# out. A second argument names a function run between the polls.
wait_second_poll() {
  wait_for envelopeFlush poll_end 1 "$1"
  if [ -n "${2:-}" ]; then "$2"; fi
  wait_for upload count 1 20
  wait_for envelopeFlush poll_end 2 60
  local ends timeouts
  ends=$(metric envelopeFlush poll_end)
  timeouts=$(metric envelopeFlush poll_timeout)
  [ "$ends" -eq 2 ] && [ "$timeouts" -eq 0 ] ||
    fail "expected two poll_end and no poll_timeout, got $ends and $timeouts"
}

# stop_and_fetch_second_poll stops xtcp2 (Close uploads poll 2's object)
# and downloads that object, the newer of the two, to $1.
stop_and_fetch_second_poll() {
  systemctl stop xtcp2
  local listing="" count second
  for _ in $(seq 1 30); do
    listing=$(rclone lsf -R --files-only --include '*.parquet' --format tp --separator ' ' "s3:$BUCKET" 2>/dev/null || true)
    if [ "$(printf '%s' "$listing" | grep -c . || true)" -ge 2 ]; then break; fi
    sleep 1
  done
  count=$(printf '%s' "$listing" | grep -c . || true)
  [ "$count" -eq 2 ] || fail "expected 2 objects (polls 1 and 2), got $count"
  second=$(printf '%s\n' "$listing" | sort | tail -n1 | awk '{print $NF}')
  rclone copyto "s3:$BUCKET/$second" "$1" || fail "download failed"
}

# q runs one duckdb query and prints its single value.
q() { duckdb -noheader -list -c "$1" 2>&1 | tail -n1 | tr -d '[:space:]'; }
