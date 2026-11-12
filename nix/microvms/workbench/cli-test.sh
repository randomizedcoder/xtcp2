# shellcheck shell=bash
# Hermetic table test for xtcp2-nlcap-ws's argument handling and row-json
# (run by checks.nlcap-ws). Every case here must be decided before the CLI
# needs a VM, so none of them boots one. NLCAP_WS_STATE points at an empty
# directory, so `need_up` would fail any case that got that far.
set -u

export NLCAP_WS_STATE=$PWD/state
fails=0

# check <description> <want-rc> <want-substring> <args...>
check() {
  local desc=$1 want_rc=$2 want=$3 out rc=0
  shift 3
  out=$(xtcp2-nlcap-ws "$@" 2>&1) || rc=$?
  if [ "$rc" -ne "$want_rc" ] || [[ $out != *"$want"* ]]; then
    echo "FAIL $desc: rc=$rc (want $want_rc), output: $out"
    fails=$((fails + 1))
  else
    echo "ok   $desc"
  fi
}

check "negative: no subcommand" 2 "usage:"
check "negative: an unknown subcommand" 2 "usage:" nosuch
check "negative: run without --slug" 2 "--slug F/N is required" run -- ip link
check "negative: run with no command" 2 "no command after --" run --slug a/b --
check "negative: run with a bad floor" 2 "--floor wants a number" \
  run --slug a/b --floor x -- ip link
check "negative: run with a bad --expect-rc" 2 "--expect-rc wants a number" \
  run --slug a/b --expect-rc -1 -- ip link
check "negative: a readback without =" 2 "--readback wants name=cmd" \
  run --slug a/b --readback nope -- ip link
check "negative: batch with no file" 2 "no rows file" batch
check "negative: batch with a missing file" 2 "no such file" batch /nonexistent
check "negative: batch -j not a number" 2 "-j wants a number" batch cli-test.sh -j x
check "negative: run while down" 2 "the workbench is down" run --slug a/b -- ip link
check "negative: batch --ip with no bin/ip" 2 "no ip in /nonexistent/bin" \
  batch cli-test.sh --ip /nonexistent
check "negative: batch --ip with no value" 2 "--ip wants an iproute2 store path" \
  batch cli-test.sh --ip
check "negative: run --ip with no value" 2 "run: --ip wants a value" \
  run --slug a/b --ip
mkdir -p fake-iproute2/bin && printf '#!/bin/sh\n' >fake-iproute2/bin/ip &&
  chmod +x fake-iproute2/bin/ip
check "positive: batch --ip with a bin/ip gets as far as need_up" 2 \
  "the workbench is down" batch cli-test.sh --ip "$PWD/fake-iproute2"
check "corner: --helper and --ip together get as far as need_up" 2 \
  "the workbench is down" batch cli-test.sh --helper /x --ip "$PWD/fake-iproute2"
check "boundary: status while down" 1 "down" status

# row-json: what `run` would send, checked by the batch runner's own parser.
row=$(xtcp2-nlcap-ws row-json t/x "positive: x" "tc qdisc add dev d0 root fq" route 0 0 \
  sch_fq,sch_htb false 2 "ip link add d0 type dummy" "ip link set d0 up" 'q=tc -j qdisc show dev d0')
want='{"id":"t/x","desc":"positive: x","cmd":"tc qdisc add dev d0 root fq","proto":"route","floor":0,"pre":["ip link add d0 type dummy","ip link set d0 up"],"readback":[{"name":"q","cmd":"tc -j qdisc show dev d0"}],"serial":false,"modules":["sch_fq","sch_htb"],"expect":{"rc":0}}'
if [ "$row" = "$want" ]; then
  echo "ok   positive: row-json carries every field"
else
  echo "FAIL positive: row-json: $row"
  fails=$((fails + 1))
fi
echo "$row" >row.jsonl
if python3 -I -c 'import sys; sys.path.insert(0, "."); import nlcap_batch; nlcap_batch.load_rows("row.jsonl", [])'; then
  echo "ok   positive: row-json passes the batch runner's checks"
else
  echo "FAIL positive: row-json rejected by nlcap_batch"
  fails=$((fails + 1))
fi

row=$(xtcp2-nlcap-ws row-json t/y "boundary: y" "ip link" route 0 "" "" false 0)
want='{"id":"t/y","desc":"boundary: y","cmd":"ip link","proto":"route","floor":0,"pre":[],"readback":[],"serial":false,"modules":[],"expect":{}}'
if [ "$row" = "$want" ]; then
  echo "ok   boundary: row-json with nothing optional"
else
  echo "FAIL boundary: row-json minimal: $row"
  fails=$((fails + 1))
fi

[ "$fails" -eq 0 ] || { echo "$fails failure(s)"; exit 1; }
echo "all cli cases passed"
