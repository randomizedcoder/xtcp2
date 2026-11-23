# Golden-record capture workload (the s3parquet-golden flavor). Starts a
# fixed set of sockets and returns only once every one is established, so
# xtcp2, ordered after this unit, sees the same workload every run:
#
#   golden-1..3  Docker containers, each TCP_COUNT=$SOCKETS listeners plus
#                as many loopback connections, idle (TCP_SLEEP=1h)
#   golden-ns    a named netns with one listener and one loopback connection
#   the host     as it is
#
# Expects IMAGE, CONTAINERS, SOCKETS, TCP_SERVER and TCP_CLIENT from the
# wrapper. A failure prints GOLDEN_ERR, which the host runner reports.
# Any unexpected exit reports itself, so the runner fails fast instead of
# waiting out its timeout.
trap 'echo "GOLDEN_ERR $0: line $LINENO exited $?"' ERR

READY_SEC="${GOLDEN_READY_SEC:-60}"

fail() {
  echo "GOLDEN_ERR $*"
  exit 1
}

for _ in $(seq 1 30); do
  if docker info >/dev/null 2>&1; then break; fi
  sleep 1
done
docker info >/dev/null 2>&1 || fail "docker not ready"
docker image inspect "$IMAGE" >/dev/null 2>&1 || fail "image $IMAGE not loaded"

for i in $(seq 1 "$CONTAINERS"); do
  docker rm --force "golden-$i" >/dev/null 2>&1 || true
  docker run --detach --name "golden-$i" \
    --env TCP_MODE=both --env "TCP_COUNT=$SOCKETS" --env TCP_SLEEP=1h --env TCP_PADS=0 \
    "$IMAGE" >/dev/null || fail "golden-$i did not start"
done

ip netns del golden-ns 2>/dev/null || true
ip netns add golden-ns
ip -n golden-ns link set lo up
ip_bin=$(command -v ip)
# Transient units, so the pair outlives this oneshot.
systemd-run --unit=xtcp2-golden-ns-server --collect \
  "$ip_bin" netns exec golden-ns "$TCP_SERVER" -count 1 -bind 127.0.0.1
sleep 1
systemd-run --unit=xtcp2-golden-ns-client --collect \
  "$ip_bin" netns exec golden-ns "$TCP_CLIENT" -count 1 -connect 127.0.0.1 -sleep 1h -pads 0

# Each container's connections show twice, once per end.
need=$((2 * SOCKETS))
deadline=$((SECONDS + READY_SEC))
while :; do
  pending=""
  for i in $(seq 1 "$CONTAINERS"); do
    pid=$(docker inspect --format '{{.State.Pid}}' "golden-$i" 2>/dev/null || echo 0)
    n=0
    if [ "$pid" -gt 0 ]; then
      n=$(nsenter -t "$pid" -n ss -Htn state established | wc -l)
    fi
    if [ "$n" -lt "$need" ]; then pending="$pending golden-$i($n/$need)"; fi
  done
  n=$(ip netns exec golden-ns ss -Htn state established | wc -l)
  if [ "$n" -lt 2 ]; then pending="$pending golden-ns($n/2)"; fi
  if [ -z "$pending" ]; then break; fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    fail "workload not ready after ${READY_SEC}s:$pending"
  fi
  sleep 1
done
echo "GOLDEN_WORKLOAD_READY"
