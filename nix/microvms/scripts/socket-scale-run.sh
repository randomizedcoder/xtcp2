# socket-scale-run: the socket-scale microVM scenario (FP-5.7).
#
# Runs as root in the guest; every result is a SOCKSCALE_* line on the
# console, which the host runner (mkSocketScaleRunner) collects.
#
#   1. walk    A small namespace B's `ss` time against the socket count in
#              another namespace A: on the shared TCP table, then with A on
#              its own table (net.ipv4.tcp_child_ehash_entries).
#   2. gate    xtcp2 with -maxRecordsPerPoll below A's 500k sockets: A is
#              sampled, nothing is dropped, memory holds steady.
#   3. synflood half-open connections against a listener in S: sockstat
#              doesn't see them; -maxRecordsPerNetns bounds the dump.
#   4. nsflood one owner (a docker-<id>.scope) makes 2000 namespaces:
#              -maxNetnsPerOwner holds it to 64 and another owner's
#              namespace is still admitted.
#
# Sizes come from SOCKSCALE_* env (see the defaults below).

# `socket-scale-run flood N` is the namespace-flood child: run inside the
# owner's scope, it starts N sleepers, each in a new network namespace.
if [ "${1:-}" = flood ]; then
  for _ in $(seq 1 "$2"); do
    unshare -n sleep infinity &
  done
  wait
  exit 0
fi

WALK_PAIRS="${SOCKSCALE_WALK_PAIRS:-0 50000 250000}"
GATE_PAIRS="${SOCKSCALE_GATE_PAIRS:-250000}"
GATE_BUDGET="${SOCKSCALE_GATE_BUDGET:-200000}"
FLOOD_NS="${SOCKSCALE_FLOOD_NS:-2000}"
SS_ITERS="${SOCKSCALE_SS_ITERS:-15}"
PROM=127.0.0.1:9188

say() { echo "SOCKSCALE_$*"; }
fail() {
  say "FAIL $*"
  FAILED=1
}
FAILED=0

# netns_new NAME: a namespace with lo up and a wide ephemeral port range
# (sockfill puts 50k clients on each listener port).
netns_new() {
  ip netns add "$1"
  ip -n "$1" link set lo up
  ip netns exec "$1" sysctl -qw net.ipv4.ip_local_port_range="10000 65000"
}

# fill NAME PAIRS: start sockfill in NAME; sets FILL_PID. Returns non-zero
# if it didn't report ready.
fill() {
  local ns="$1" pairs="$2" log="/tmp/fill-$1.log"
  ip netns exec "$ns" sockfill -pairs "$pairs" >"$log" 2>&1 &
  FILL_PID=$!
  local i
  for i in $(seq 1 600); do
    if grep -q SOCKFILL_READY "$log"; then
      say "FILL ns=$ns $(grep -o 'pairs=.*' "$log")"
      return 0
    fi
    if grep -q SOCKFILL_ERR "$log" || ! kill -0 "$FILL_PID" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  fail "fill ns=$ns pairs=$pairs after ${i}s: $(tail -n 3 "$log" | tr '\n' ' ')"
  return 1
}

unfill() {
  if [ -n "${FILL_PID:-}" ]; then
    kill -TERM "$FILL_PID" 2>/dev/null || true
    wait "$FILL_PID" 2>/dev/null || true
    FILL_PID=""
  fi
}

# ss_us NAME: median microseconds of `ss -tan` in NAME over SS_ITERS runs
# (ss -N enters the namespace itself; the exec cost is the same every run).
ss_us() {
  local s e
  for _ in $(seq 1 "$SS_ITERS"); do
    s=$(date +%s%N)
    ss -N "$1" -tan >/dev/null
    e=$(date +%s%N)
    echo $(((e - s) / 1000))
  done | sort -n | awk '{v[NR]=$1} END {print v[int((NR+1)/2)]}'
}

# sockets NAME: TCP sockets in NAME (inuse + tw from its sockstat).
sockets() {
  ip netns exec "$1" cat /proc/net/sockstat | awk '/^TCP:/ {print $3 + $7}'
}

# metric REGEX: the sum of the /metrics samples whose line matches REGEX.
metric() {
  curl -sf "http://$PROM/metrics" | awk -v re="$1" '$0 ~ re && $0 !~ /^#/ {s += $NF} END {printf "%d\n", s}'
}

# xtcp2_start ARGS...: run xtcp2 with a loopback metrics listener; sets
# XTCP_PID once /metrics answers.
xtcp2_start() {
  LISTENER_AUTH_MODE=disabled xtcp2 -dest null: -frequency 2s -timeout 1s \
    -listener "name=prometheus,serves=health+metrics+metrics-detail,address=$PROM,network=tcp" \
    "$@" >/tmp/xtcp2-scale.log 2>&1 &
  XTCP_PID=$!
  local i
  for i in $(seq 1 60); do
    if curl -sf "http://$PROM/metrics" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail "xtcp2 $* did not serve /metrics: $(tail -n 5 /tmp/xtcp2-scale.log | tr '\n' ' ')"
  return 1
}

xtcp2_stop() {
  if [ -n "${XTCP_PID:-}" ]; then
    kill -TERM "$XTCP_PID" 2>/dev/null || true
    wait "$XTCP_PID" 2>/dev/null || true
    XTCP_PID=""
  fi
}

rss_kb() { awk '/^VmRSS:/ {print $2}' "/proc/$1/status"; }
threads() { awk '/^Threads:/ {print $2}' "/proc/$1/status"; }

# --- 1. walk ----------------------------------------------------------------

phase_walk() {
  say "PHASE walk"
  sysctl -qw net.ipv4.tcp_child_ehash_entries=0
  netns_new B
  fill B 10 || return
  B_FILL=$FILL_PID
  FILL_PID=""

  local p a us base=0 top=0
  for p in $WALK_PAIRS; do
    a="A$p"
    netns_new "$a"
    if [ "$p" -gt 0 ]; then
      fill "$a" "$p" || {
        ip netns del "$a"
        continue
      }
    fi
    us=$(ss_us B)
    say "WALK table=shared a_sockets=$(sockets "$a") b_sockets=$(sockets B) b_ss_us=$us"
    [ "$p" -eq 0 ] && base=$us
    top=$us
    unfill
    ip netns del "$a"
  done

  # A on its own table: children of a namespace with tcp_child_ehash_entries
  # set get a private ehash (tcp_set_hashinfo), so B's walk doesn't see A.
  local own=0 p_top
  p_top=$(echo "$WALK_PAIRS" | awk '{print $NF}')
  sysctl -qw net.ipv4.tcp_child_ehash_entries=65536
  netns_new Aown
  sysctl -qw net.ipv4.tcp_child_ehash_entries=0
  say "WALK_OWN_TABLE ehash_entries=$(ip netns exec Aown sysctl -n net.ipv4.tcp_ehash_entries)"
  if fill Aown "$p_top"; then
    own=$(ss_us B)
    say "WALK table=own a_sockets=$(sockets Aown) b_sockets=$(sockets B) b_ss_us=$own"
  fi
  unfill
  ip netns del Aown

  kill -TERM "$B_FILL" 2>/dev/null || true
  wait "$B_FILL" 2>/dev/null || true
  ip netns del B

  # Shared: B's dump walks A's sockets too, so it grows with A. Own table:
  # it doesn't (allow 2x + 2 ms for noise).
  if [ "$base" -gt 0 ] && [ "$top" -ge $((base * 2)) ]; then
    say "WALK_SHARED_PASS base_us=$base top_us=$top ratio=$((top / base))x"
  else
    fail "WALK_SHARED base_us=$base top_us=$top: B's dump did not grow with A"
  fi
  if [ "$own" -gt 0 ] && [ "$own" -le $((base * 2 + 2000)) ]; then
    say "WALK_OWN_PASS base_us=$base own_us=$own"
  else
    fail "WALK_OWN base_us=$base own_us=$own: B's dump grew with A on its own table"
  fi
}

# --- 2. gate ----------------------------------------------------------------

phase_gate() {
  say "PHASE gate"
  netns_new B
  fill B 10 || return
  B_FILL=$FILL_PID
  FILL_PID=""
  netns_new A
  fill A "$GATE_PAIRS" || return

  local mode budget
  for mode in on off; do
    budget=$GATE_BUDGET
    [ "$mode" = off ] && budget=0
    xtcp2_start -maxRecordsPerPoll "$budget" || continue
    sleep 20
    local rss1 thr1
    rss1=$(rss_kb "$XTCP_PID")
    thr1=$(threads "$XTCP_PID")
    sleep 40
    local rss2 thr2 sample states sockets drops polls p50
    rss2=$(rss_kb "$XTCP_PID")
    thr2=$(threads "$XTCP_PID")
    sample=$(metric '^xtcp_poll_degraded_total\{level="sample"\}')
    states=$(metric '^xtcp_poll_degraded_total\{level="states"\}')
    sockets=$(metric '^xtcp_poll_sockets ')
    drops=$(metric '^xtcp_dump_dropped_total\{reason="records"\}')
    polls=$(metric '^xtcp_histograms_count\{.*variable="pollToDoneDuration"')
    p50=$(curl -sf "http://$PROM/metrics" | awk '/pollToDoneDuration/ && /quantile="0.5"/ {print $NF; exit}')
    say "GATE mode=$mode budget=$budget a_sockets=$(sockets A) poll_sockets=$sockets sample=$sample states=$states records_dropped=$drops polls=$polls poll_p50_s=$p50 rss_kb=$rss1->$rss2 threads=$thr1->$thr2"
    xtcp2_stop
    if [ "$mode" = on ]; then
      [ "$sample" -gt 0 ] || fail "GATE A was never sampled"
      [ "$drops" -eq 0 ] || fail "GATE sampled polls still hit the record cap ($drops dropped)"
      [ "$sockets" -ge $((GATE_PAIRS * 2 * 8 / 10)) ] || fail "GATE poll_sockets $sockets below A's $((GATE_PAIRS * 2))"
      [ "$rss2" -le $((rss1 * 3 / 2)) ] || fail "GATE RSS grew $rss1 -> $rss2 kB"
      if [ "$sample" -gt 0 ] && [ "$drops" -eq 0 ]; then
        say "GATE_PASS"
      fi
    fi
  done

  unfill
  ip netns del A
  kill -TERM "$B_FILL" 2>/dev/null || true
  wait "$B_FILL" 2>/dev/null || true
  ip netns del B
}

# --- 3. synflood ------------------------------------------------------------

phase_synflood() {
  say "PHASE synflood"
  netns_new S
  netns_new F
  ip link add vS netns S type veth peer name vF netns F
  ip -n S addr add 10.250.0.1/24 dev vS
  ip -n F addr add 10.250.0.2/24 dev vF
  ip -n S link set vS up
  ip -n F link set vF up
  # SYN-ACKs to the spoofed sources leave via F, which doesn't forward them.
  ip -n S route add default via 10.250.0.2
  ip netns exec S sysctl -qw net.ipv4.tcp_syncookies=0 net.ipv4.tcp_max_syn_backlog=65535 \
    net.core.somaxconn=65535 net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.vS.rp_filter=0

  ip netns exec S sockfill -listen 10.250.0.1:8080 >/tmp/fill-S.log 2>&1 &
  local listen_pid=$!
  sleep 1

  xtcp2_start -maxRecordsPerNetns 5000 || return
  ip netns exec F timeout 20 hping3 -q -S -p 8080 --flood --rand-source 10.250.0.1 >/dev/null 2>&1 || true
  local synrecv inuse
  synrecv=$(ip netns exec S ss -Htan state syn-recv | wc -l)
  inuse=$(sockets S)
  sleep 6
  local drops
  drops=$(metric '^xtcp_dump_dropped_total\{reason="records"\}')
  say "SYNFLOOD syn_recv=$synrecv sockstat_tcp=$inuse records_dropped=$drops"
  xtcp2_stop
  kill -TERM "$listen_pid" 2>/dev/null || true
  wait "$listen_pid" 2>/dev/null || true
  ip netns del S
  ip netns del F

  if [ "$synrecv" -le 5000 ]; then
    fail "SYNFLOOD only $synrecv half-open connections; the flood didn't take"
  elif [ "$drops" -gt 0 ]; then
    say "SYNFLOOD_PASS sockstat_blind=$([ "$inuse" -lt 100 ] && echo yes || echo no)"
  else
    fail "SYNFLOOD $synrecv SYN_RECV but -maxRecordsPerNetns 5000 dropped nothing"
  fi
}

# --- 4. nsflood -------------------------------------------------------------

phase_nsflood() {
  say "PHASE nsflood"
  local owner other
  owner=$(printf 'socket-scale-flood' | sha256sum | cut -c1-64)
  other=$(printf 'socket-scale-other' | sha256sum | cut -c1-64)

  xtcp2_start || return
  sleep 4
  local before
  before=$(metric '^xtcp_gauge ')

  local start=$SECONDS
  systemd-run --quiet --scope --slice=system.slice --unit="docker-$owner" -p TasksMax=infinity \
    "$0" flood "$FLOOD_NS" &
  local flood_pid=$!
  local made=0
  for _ in $(seq 1 120); do
    made=$(pgrep -c -f '^sleep infinity$' || true)
    [ "$made" -ge "$FLOOD_NS" ] && break
    sleep 1
  done
  say "NSFLOOD spawned=$made in $((SECONDS - start))s cgroup=$(awk -F: '{print $3}' "/proc/$(pgrep -f -n '^sleep infinity$')/cgroup")"
  sleep 20

  local tracked quota timeouts
  tracked=$(metric '^xtcp_gauge ')
  quota=$(metric '^xtcp_netns_dropped_total\{reason="owner_quota"\}')
  timeouts=$(metric '^xtcp_reconcile_incomplete_total\{reason="timeout"\}')

  # A late namespace from another owner must still get in.
  systemd-run --quiet --scope --slice=system.slice --unit="docker-$other" \
    unshare -n sleep 3600 &
  local other_pid=$!
  sleep 8
  local after
  after=$(metric '^xtcp_gauge ')
  say "NSFLOOD tracked_before=$before tracked_flood=$tracked tracked_late=$after owner_quota_dropped=$quota reconcile_timeouts=$timeouts xtcp2_threads=$(threads "$XTCP_PID") rss_kb=$(rss_kb "$XTCP_PID")"

  xtcp2_stop
  systemctl stop "docker-$owner.scope" "docker-$other.scope" 2>/dev/null || true
  kill "$flood_pid" "$other_pid" 2>/dev/null || true
  wait 2>/dev/null || true

  local ok=1
  [ "$quota" -gt 0 ] || {
    fail "NSFLOOD no owner_quota drops"
    ok=0
  }
  [ "$tracked" -le $((before + 64)) ] || {
    fail "NSFLOOD tracked $tracked > $before + quota 64"
    ok=0
  }
  [ "$after" -eq $((tracked + 1)) ] || {
    fail "NSFLOOD the other owner's namespace was not admitted ($tracked -> $after)"
    ok=0
  }
  [ "$ok" -eq 1 ] && say "NSFLOOD_PASS"
  return 0
}

say "START walk_pairs='$WALK_PAIRS' gate_pairs=$GATE_PAIRS gate_budget=$GATE_BUDGET flood_ns=$FLOOD_NS kernel=$(uname -r) cpus=$(nproc) mem_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)"
for phase in ${SOCKSCALE_PHASES:-walk gate synflood nsflood}; do
  "phase_$phase" || fail "phase $phase aborted"
  unfill
  xtcp2_stop
done
if [ "$FAILED" -eq 0 ]; then
  say "OVERALL_PASS"
else
  say "OVERALL_FAIL"
fi
say "DONE"
