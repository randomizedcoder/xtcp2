# tcp-families check: does xtcp2 record every socket of each address
# family? After poll 2 (see golden-common.sh), each unit's rows per
# inet_diag_msg_family are compared with ss -4 / ss -6 in that namespace.
# Each table row prints "ROW <name> <category> PASS|FAIL <detail>"; the host
# runner fails on any FAIL. FAMILIES_DONE ends the run.
#
# Expects PROM_SOCKET, TOKEN_FILE, BUCKET and SOCKETS from the wrapper,
# which prepends golden-common.sh (ERR_TAG=FAMILIES_ERR).
trap 'echo "FAMILIES_ERR $0: line $LINENO exited $?"' ERR

OUT=/tmp/families
FLUSH_SEC="${FAMILIES_FLUSH_SEC:-300}"
rm -rf "$OUT"
mkdir -p "$OUT"

failed=0
row() { # name category PASS|FAIL detail
  echo "ROW $1 $2 $3 $4"
  if [ "$3" != PASS ]; then failed=$((failed + 1)); fi
}
check() { # name category description: condition as a command
  local name=$1 category=$2 detail=$3
  shift 3
  if "$@"; then row "$name" "$category" PASS "$detail"; else row "$name" "$category" FAIL "$detail"; fi
}

echo "FAMILIES_START"
# no_poll_end (negative): without two poll_end flushes nothing below is
# compared; wait_second_poll fails the run naming the metric.
wait_second_poll "$FLUSH_SEC"
row no_poll_end negative PASS "poll_end reached 2"

# The sockets each namespace holds now, per family.
ss_count() { # nsenter-or-netns args... -- family flag
  local fam=$1
  shift
  "$@" ss "$fam" -Htan | grep -c . || true
}
declare -A ss4 ss6
for c in fam-v4 fam-v6 fam-dual; do
  pid=$(docker inspect --format '{{.State.Pid}}' "$c")
  ss4[$c]=$(ss_count -4 nsenter -t "$pid" -n)
  ss6[$c]=$(ss_count -6 nsenter -t "$pid" -n)
done
ss4[fam-ns]=$(ss_count -4 ip netns exec fam-ns)
ss6[fam-ns]=$(ss_count -6 ip netns exec fam-ns)
ss4[host]=$(ss_count -4 env)
ss6[host]=$(ss_count -6 env)
ends=$(metric envelopeFlush poll_end)
timeouts=$(metric envelopeFlush poll_timeout)

stop_and_fetch_second_poll "$OUT/raw.parquet"
P="read_parquet('$OUT/raw.parquet')"

# rows UNIT FAMILY: the unit's rows of one family.
rows() {
  local where
  case "$1" in
    fam-ns) where="netns = 'fam-ns'" ;;
    host) where="container_name = '' AND netns <> 'fam-ns'" ;;
    *) where="container_name = '$1'" ;;
  esac
  q "SELECT count(*) FROM $P WHERE $where AND inet_diag_msg_family = $2"
}
counts() { # unit: "v4=<rows>/<ss -4> v6=<rows>/<ss -6>"
  echo "v4=$(rows "$1" 2)/${ss4[$1]} v6=$(rows "$1" 10)/${ss6[$1]}"
}
equal() { # unit: both families' rows equal their ss counts
  [ "$(rows "$1" 2)" -eq "${ss4[$1]}" ] && [ "$(rows "$1" 10)" -eq "${ss6[$1]}" ]
}

v4_ok() { equal fam-v4 && [ "${ss6[fam-v4]}" -eq 0 ]; }
v6_ok() { equal fam-v6 && [ "${ss4[fam-v6]}" -eq 0 ] && [ "${ss6[fam-v6]}" -gt 0 ]; }
dual_ok() {
  equal fam-dual && [ "${ss4[fam-dual]}" -gt 0 ] && [ "${ss6[fam-dual]}" -gt 0 ] && [ "$mapped" -eq "$SOCKETS" ]
}
ns_ok() { equal fam-ns && [ "${ss6[fam-ns]}" -gt 0 ]; }
check v4_counts positive "fam-v4 $(counts fam-v4)" v4_ok
check v6_counts positive "fam-v6 $(counts fam-v6)" v6_ok
mapped=$(q "SELECT count(*) FROM $P WHERE container_name = 'fam-dual' AND inet_diag_msg_family = 10 AND inet_diag_msg_state = 1 AND hex(inet_diag_msg_socket_destination) = '00000000000000000000FFFF7F000001'")
check dual_counts positive "fam-dual $(counts fam-dual) accepted-v4-mapped=$mapped/$SOCKETS" dual_ok
check netns_v6 positive "fam-ns $(counts fam-ns)" ns_ok

# family_mismatch (negative): an AF_INET row's address is 4 bytes, or 16
# with only the first 4 set.
mismatch=$(q "SELECT count(*) FROM $P WHERE inet_diag_msg_family = 2 AND NOT (length(hex(inet_diag_msg_socket_source)) = 8 OR (length(hex(inet_diag_msg_socket_source)) = 32 AND ends_with(hex(inet_diag_msg_socket_source), repeat('0', 24))))")
check family_mismatch negative "AF_INET rows with a non-IPv4 source: $mismatch" \
  test "$mismatch" -eq 0

polls_ok() { [ "$ends" -eq 2 ] && [ "$timeouts" -eq 0 ]; }
check two_polls boundary "poll_end=$ends poll_timeout=$timeouts" polls_ok

# host_ns (corner): the host's sockets change as xtcp2 uploads, so its
# counts are not pinned; each family ss lists must have rows.
host_ok() {
  { [ "${ss4[host]}" -eq 0 ] || [ "$(rows host 2)" -gt 0 ]; } &&
    { [ "${ss6[host]}" -eq 0 ] || [ "$(rows host 10)" -gt 0 ]; }
}
check host_ns corner "host $(counts host)" host_ok

echo "FAMILIES_RESULT failed=$failed"
echo "FAMILIES_DONE"
