# xtcp2-nlcap-ws: the host side of the nl_mon capture workbench.
#
# docs/netlink/coverage-status.md, "Workbench and generated rows". Boots the
# nlcap-workbench microVM once and keeps it up; every capture after that is
# an ssh call, not a rebuild and a cold boot. The guest half is
# nlcap_batch.py, run by store path: the guest mounts the host's /nix/store,
# so the helper and the runner can change without a reboot.
#
# Substituted by default.nix: @VM_RUN@ @NLCAP@ @BATCH@ @SSH_PORT@
# @SERIAL_PORT@ @VIRTIO_PORT@ @HOSTNAME@.

set -euo pipefail

VM_RUN=@VM_RUN@
NLCAP=@NLCAP@
BATCH=@BATCH@
SSH_PORT=@SSH_PORT@
SERIAL_PORT=@SERIAL_PORT@
VIRTIO_PORT=@VIRTIO_PORT@
VM_NAME=@HOSTNAME@

STATE=${NLCAP_WS_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/xtcp2-nlcap-ws}
GUEST_DIR=/tmp/nlcap-ws

die() {
  echo "NLCAP_WS_ERR $*" >&2
  exit 2
}

usage() {
  cat <<EOF
usage: xtcp2-nlcap-ws <command> [args]

  up [--timeout S]        boot the workbench VM in the background, wait for ssh
  down                    power it off; delete the ephemeral key and state
  status                  up or down; kernel, iproute2, ports
  sh [cmd...]             a shell (or one command) in the guest's root namespace
  run --slug F/N [opts] -- <cmd...>
                          capture one ad-hoc row
      --desc 'positive: ...'   (default "positive: ad-hoc")
      --pre '<cmd>'            a step before the capture window (repeatable)
      --readback name='<cmd>'  a captured readback after it (repeatable)
      --proto route|generic|xfrm|sock_diag   --floor N   --expect-rc N
      --modules m1,m2          --serial
      --out DIR  --helper PATH  --ip PATH   (as for batch)
  batch <rows.jsonl> [-j N] [--only id,id] [--out DIR] [--helper PATH]
        [--ip PATH]       run a rows file; rows land in
                          DIR (default pkg/xtcpnl/testdata/<kernel>/ws).
                          --ip: an iproute2 store path whose bin/ip every
                          row runs, e.g. nix build --print-out-paths
                          '.#iproute2_7_3_0^out' (^out: not the man output)

  Rows files: nix/microvms/workbench/*.rows.jsonl. State: $STATE
EOF
}

ssh_opts() {
  printf '%s\n' -i "$STATE/id" -p "$SSH_PORT" \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR -o BatchMode=yes -o ConnectTimeout=5 \
    -o ControlMaster=auto -o "ControlPath=$STATE/cm" -o ControlPersist=600
}

# gssh <remote command...> - one command in the guest's root namespace. The
# remote words are joined and run by the guest's shell, as ssh always does;
# callers quote for that shell.
gssh() {
  local argv
  mapfile -t argv < <(ssh_opts)
  argv+=(root@127.0.0.1 "$@")
  ssh "${argv[@]}"
}

vm_pid() {
  local pid
  [ -f "$STATE/pid" ] || return 1
  pid=$(cat "$STATE/pid")
  kill -0 "$pid" 2>/dev/null || return 1
  echo "$pid"
}

port_busy() {
  ss -Hltn "sport = :$1" | grep -q .
}

cmd_up() {
  local timeout=900 deadline
  while [ $# -gt 0 ]; do
    case "$1" in
      --timeout) timeout=$2; shift 2 ;;
      *) die "up: unknown arg $1" ;;
    esac
  done
  if vm_pid >/dev/null; then
    echo "NLCAP_WS_UP already (pid $(vm_pid))"
    return 0
  fi
  local p
  for p in "$SSH_PORT" "$SERIAL_PORT" "$VIRTIO_PORT"; do
    if port_busy "$p"; then
      die "port $p is in use; another VM (or a stale workbench) holds it"
    fi
  done
  mkdir -p "$STATE"
  chmod 700 "$STATE"
  rm -f "$STATE/id" "$STATE/id.pub" "$STATE/authorized_keys" "$STATE/cm"
  ssh-keygen -q -t ed25519 -N '' -C nlcap-ws -f "$STATE/id"
  cp "$STATE/id.pub" "$STATE/authorized_keys"
  # qemu resolves the fw_cfg key file relative to its working directory,
  # and microvm.nix puts the QMP socket there too.
  (cd "$STATE" && exec setsid "$VM_RUN" >"$STATE/qemu.log" 2>&1 </dev/null) &
  echo $! >"$STATE/pid"
  echo "NLCAP_WS_BOOTING $VM_NAME pid $(cat "$STATE/pid") ssh 127.0.0.1:$SSH_PORT"
  deadline=$(($(date +%s) + timeout))
  until gssh true 2>/dev/null; do
    if ! vm_pid >/dev/null; then
      tail -20 "$STATE/qemu.log" >&2 || true
      die "the VM exited before ssh came up; see $STATE/qemu.log"
    fi
    if [ "$(date +%s)" -ge "$deadline" ]; then
      die "no ssh after ${timeout}s; see $STATE/qemu.log"
    fi
    sleep 3
  done
  gssh 'uname -r; ip -V' >"$STATE/env"
  echo "NLCAP_WS_UP $(tr '\n' ' ' <"$STATE/env")"
}

cmd_down() {
  local pid i
  if pid=$(vm_pid); then
    gssh 'systemctl poweroff' >/dev/null 2>&1 || true
    for i in $(seq 1 60); do
      kill -0 "$pid" 2>/dev/null || break
      [ "$i" -eq 60 ] && kill "$pid" 2>/dev/null
      sleep 1
    done
  fi
  ssh -O exit -o "ControlPath=$STATE/cm" root@127.0.0.1 >/dev/null 2>&1 || true
  rm -f "$STATE/id" "$STATE/id.pub" "$STATE/authorized_keys" "$STATE/cm" \
    "$STATE/pid" "$STATE/env" "$STATE"/*.sock
  echo "NLCAP_WS_DOWN"
}

cmd_status() {
  if vm_pid >/dev/null; then
    echo "up pid=$(vm_pid) ssh=127.0.0.1:$SSH_PORT"
    cat "$STATE/env" 2>/dev/null || true
  else
    echo "down"
    return 1
  fi
}

need_up() {
  vm_pid >/dev/null || die "the workbench is down; run: nix run .#nlcap-ws -- up"
}

cmd_sh() {
  need_up
  if [ $# -eq 0 ]; then
    local opts
    mapfile -t opts < <(ssh_opts)
    exec ssh -t "${opts[@]}" root@127.0.0.1
  fi
  gssh "$@"
}

# kver_dir: the guest kernel as a testdata directory name, the rule the
# capture runners use (lib.nix): 7.3.0 is 7_3_0, 7.2.0-rc3 is 7_2_0_rc3.
kver_dir() {
  gssh 'uname -r' | sed -E 's/-(rc[0-9]+).*/_\1/; s/-.*//' | tr . _
}

cmd_batch() {
  local rows="" jobs=8 only="" out="" helper="$NLCAP" ipdir=""
  while [ $# -gt 0 ]; do
    case "$1" in
      -j) jobs=$2; shift 2 ;;
      --only) only=$2; shift 2 ;;
      --out) out=$2; shift 2 ;;
      --helper) helper=$2; shift 2 ;;
      --ip)
        [ $# -ge 2 ] || die "batch: --ip wants an iproute2 store path"
        ipdir=$2; shift 2 ;;
      -*) die "batch: unknown arg $1" ;;
      *) [ -z "$rows" ] || die "batch: one rows file"; rows=$1; shift ;;
    esac
  done
  [ -n "$rows" ] || die "batch: no rows file"
  [ -f "$rows" ] || die "batch: no such file $rows"
  case "$jobs" in '' | *[!0-9]*) die "batch: -j wants a number" ;; esac
  # The guest sees the host's /nix/store, so checking here checks there.
  if [ -n "$ipdir" ] && [ ! -x "$ipdir/bin/ip" ]; then
    die "batch: no ip in $ipdir/bin"
  fi
  need_up
  if [ -z "$out" ]; then
    [ -d pkg/xtcpnl/testdata ] || die "batch: run from the xtcp2 repo root, or pass --out"
    out="pkg/xtcpnl/testdata/$(kver_dir)/ws"
  fi
  local run rdir
  run=$(date +%Y%m%d-%H%M%S)-$$
  rdir="$GUEST_DIR/$run"
  gssh "mkdir -p $rdir/out && cat > $rdir/rows.jsonl" <"$rows"
  local args=(-j "$jobs" --nlcap "$helper/bin/xtcp2-nlcap")
  [ -z "$only" ] || args+=(--only "$only")
  [ -z "$ipdir" ] || args+=(--ip-bin "$ipdir/bin")
  local rc=0
  gssh "$BATCH/bin/xtcp2-nlcap-batch ${args[*]} $rdir/rows.jsonl $rdir/out" || rc=$?
  if [ "$rc" -eq 2 ]; then
    die "batch: the guest runner refused the rows file (above)"
  fi
  mkdir -p "$out"
  gssh "tar -cz -C $rdir/out --exclude=.batch-results.jsonl ." | tar -xz -C "$out"
  gssh "cat $rdir/out/.batch-results.jsonl" >"$STATE/last-results.jsonl"
  gssh "rm -rf $rdir"
  echo "NLCAP_WS_BATCH rc=$rc out=$out results=$STATE/last-results.jsonl"
  return "$rc"
}

cmd_run() {
  local slug="" desc="positive: ad-hoc" proto=route floor=0 rc_want="" mods="" serial=false
  local pre=() rbs=() bargs=(-j 1)
  while [ $# -gt 0 ]; do
    case "$1" in
      --slug) slug=$2; shift 2 ;;
      --desc) desc=$2; shift 2 ;;
      --pre) pre+=("$2"); shift 2 ;;
      --readback) rbs+=("$2"); shift 2 ;;
      --proto) proto=$2; shift 2 ;;
      --floor) floor=$2; shift 2 ;;
      --expect-rc) rc_want=$2; shift 2 ;;
      --modules) mods=$2; shift 2 ;;
      --serial) serial=true; shift ;;
      --out | --helper | --ip)
        [ $# -ge 2 ] || die "run: $1 wants a value"
        bargs+=("$1" "$2"); shift 2 ;;
      --) shift; break ;;
      *) die "run: unknown arg $1 (the command goes after --)" ;;
    esac
  done
  [ -n "$slug" ] || die "run: --slug F/N is required"
  [ $# -gt 0 ] || die "run: no command after --"
  case "$floor" in '' | *[!0-9]*) die "run: --floor wants a number" ;; esac
  case "$rc_want" in *[!0-9]*) die "run: --expect-rc wants a number" ;; esac
  local tmp rc=0
  tmp=$(mktemp)
  row_json "$slug" "$desc" "$*" "$proto" "$floor" "$rc_want" "$mods" "$serial" \
    "${#pre[@]}" "${pre[@]}" "${rbs[@]}" >"$tmp" || { rm -f "$tmp"; exit 2; }
  cmd_batch "$tmp" "${bargs[@]}" || rc=$?
  rm -f "$tmp"
  return "$rc"
}

# row_json <slug> <desc> <cmd> <proto> <floor> <rc|""> <mods> <serial>
#          <npre> <pre...> <name=cmd...>
row_json() {
  local slug=$1 desc=$2 cmd=$3 proto=$4 floor=$5 rc=$6 mods=$7 serial=$8 npre=$9
  shift 9
  local pre=("${@:1:npre}") rbs=("${@:npre+1}")
  local rb_json='[]' r
  for r in "${rbs[@]}"; do
    case "$r" in *=*) ;; *) die "run: --readback wants name=cmd" ;; esac
    rb_json=$(jq -c --arg n "${r%%=*}" --arg c "${r#*=}" '. + [{name: $n, cmd: $c}]' <<<"$rb_json")
  done
  jq -nc --arg id "$slug" --arg desc "$desc" --arg cmd "$cmd" --arg proto "$proto" \
    --argjson floor "$floor" --arg rc "$rc" --arg mods "$mods" \
    --argjson serial "$serial" --argjson rb "$rb_json" \
    --args '{id: $id, desc: $desc, cmd: $cmd, proto: $proto, floor: $floor,
      pre: $ARGS.positional, readback: $rb, serial: $serial,
      modules: ($mods | split(",") | map(select(. != ""))),
      expect: (if $rc == "" then {} else {rc: ($rc | tonumber)} end)}' \
    "${pre[@]}"
}

main() {
  [ $# -gt 0 ] || { usage; exit 2; }
  local sub=$1
  shift
  case "$sub" in
    up) cmd_up "$@" ;;
    down) cmd_down "$@" ;;
    status) cmd_status "$@" ;;
    sh) cmd_sh "$@" ;;
    run) cmd_run "$@" ;;
    batch) cmd_batch "$@" ;;
    row-json) row_json "$@" ;;
    -h | --help | help) usage ;;
    *) usage; exit 2 ;;
  esac
}

main "$@"
