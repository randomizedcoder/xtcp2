# tcp-families workload: sockets of each address family, ready before xtcp2
# starts. Each container runs SOCKETS listeners and as many loopback
# connections, idle (TCP_SLEEP=1h):
#
#   fam-v4    tcp4 on 127.0.0.1              AF_INET only
#   fam-v6    tcp6 on ::1                    AF_INET6 only
#   fam-dual  tcp on 0.0.0.0 (dual-stack),   AF_INET6 listeners and accepted
#             clients dial 127.0.0.1         sockets (v4-mapped), AF_INET clients
#   fam-ns    a named netns, one tcp6 pair on ::1
#
# Docker turns IPv6 off in a container unless its network has it, which
# would leave no ::1; the sysctls turn it back on for loopback.
#
# Expects IMAGE, SOCKETS, TCP_SERVER and TCP_CLIENT from the wrapper. A
# failure prints FAMILIES_ERR, which the host runner reports.
trap 'echo "FAMILIES_ERR $0: line $LINENO exited $?"' ERR

READY_SEC="${FAMILIES_READY_SEC:-60}"

fail() {
  echo "FAMILIES_ERR $*"
  exit 1
}

for _ in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then break; fi
  sleep 1
done
docker info >/dev/null 2>&1 || fail "docker not ready"
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "image $IMAGE not loaded"

run() {
  local name=$1 network=$2 bind=$3 connect=$4
  docker rm --force "$name" >/dev/null 2>&1 || true
  docker run --detach --name "$name" \
    --sysctl net.ipv6.conf.all.disable_ipv6=0 --sysctl net.ipv6.conf.lo.disable_ipv6=0 \
    --env TCP_MODE=both --env "TCP_COUNT=$SOCKETS" --env TCP_SLEEP=1h --env TCP_PADS=0 \
    --env "TCP_NETWORK=$network" --env "TCP_BIND=$bind" --env "TCP_CONNECT=$connect" \
    "$IMAGE" >/dev/null || fail "$name did not start"
}
run fam-v4 tcp4 127.0.0.1 127.0.0.1
run fam-v6 tcp6 ::1 ::1
run fam-dual tcp 0.0.0.0 127.0.0.1

ip netns del fam-ns 2>/dev/null || true
ip netns add fam-ns
ip -n fam-ns link set lo up
ip_bin=$(command -v ip)
# Transient units, so the pair outlives this oneshot.
systemd-run --unit=xtcp2-fam-ns-server --collect \
  "$ip_bin" netns exec fam-ns "$TCP_SERVER" -network tcp6 -count 1 -bind ::1
sleep 1
systemd-run --unit=xtcp2-fam-ns-client --collect \
  "$ip_bin" netns exec fam-ns "$TCP_CLIENT" -network tcp6 -count 1 -connect ::1 -sleep 1h -pads 0

# Each connection shows twice, once per end.
need=$((2 * SOCKETS))
deadline=$((SECONDS + READY_SEC))
while :; do
  pending=""
  for c in fam-v4 fam-v6 fam-dual; do
    pid=$(docker inspect --format '{{.State.Pid}}' "$c" 2>/dev/null || echo 0)
    n=0
    if [ "$pid" -gt 0 ]; then
      n=$(nsenter -t "$pid" -n ss -Htn state established | wc -l)
    fi
    if [ "$n" -lt "$need" ]; then pending="$pending $c($n/$need)"; fi
  done
  n=$(ip netns exec fam-ns ss -Htn state established | wc -l)
  if [ "$n" -lt 2 ]; then pending="$pending fam-ns($n/2)"; fi
  if [ -z "$pending" ]; then break; fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    fail "workload not ready after ${READY_SEC}s:$pending"
  fi
  sleep 1
done
echo "FAMILIES_WORKLOAD_READY"
