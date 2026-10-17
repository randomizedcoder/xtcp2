# rdma-native-capture: record the bytes the go-native RDMA decoders parse.
#
# Usage:
#   rdma-native-capture --out DIR --dev DEV [--dev DEV]... [--values FILE]
#                       [--triggers gid[,flap] --iface IFACE] [--umad]
#                       [--driver NAME] [--dry-run]
#
# Writes one capture stage, in the layout of
# cmd/go-link-monitor/GO-NATIVE-RDMA-ASSESSMENT.md "Captured testdata":
#
#   nldev/<row>-<dev>.strace|.out|.err|.rc  ibv_devinfo (GET_CHARDEV) and rdma
#                                           dev/link under the nlcap strace args
#   sysfs/<dev>/...                         port files, byte for byte
#   nlmon/<trigger>.pcap                    NETLINK_ROUTE around each trigger
#   async/<dev>-<trigger>.strace|.out|...   ibv_asyncwatch reads (-xx)
#   umad/<row>-<dev>-<port>.strace|...      smpquery and perfquery (--umad)
#   meta/uname|driver|firmware|rdma_core    for the manifest
#   index.tsv                               each row's command and trigger
#
# rdma-capture-manifest then redacts the stage and writes manifest.json.
#
# Nothing here changes the host unless asked. Without --triggers and --umad it
# only reads: strace of read-only tools, and sysfs. --triggers gid adds and
# removes the documentation address 2001:db8:5300::1/128 on IFACE; --triggers
# flap takes IFACE down and up, which only the VM guests use. Each trigger
# window creates and deletes one nlmon interface. --umad needs ib_umad loaded
# already: this script never loads a module.
#
# --values FILE receives the real identifiers the stage holds (GUIDs, GIDs,
# MACs, addresses), for the redaction. Keep it out of the repository.
#
# --dry-run writes the same layout with placeholder contents and no device
# access, so the runners and the manifest tool can be tested anywhere.

set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
usage: rdma-native-capture --out DIR --dev DEV [--dev DEV]... [--values FILE]
                           [--triggers gid[,flap] --iface IFACE] [--umad]
                           [--driver NAME] [--dry-run]
USAGE
  exit 2
}

OUT=""
DEVS=()
VALUES=""
TRIGGERS=""
IFACE=""
UMAD=0
DRY_RUN=0
DRIVER=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=$2; shift 2 ;;
    --dev) DEVS+=("$2"); shift 2 ;;
    --values) VALUES=$2; shift 2 ;;
    --triggers) TRIGGERS=$2; shift 2 ;;
    --iface) IFACE=$2; shift 2 ;;
    --umad) UMAD=1; shift ;;
    --driver) DRIVER=$2; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h | --help) usage ;;
    *) echo "rdma-native-capture: unknown argument $1" >&2; usage ;;
  esac
done
if [ -z "$OUT" ] || [ "${#DEVS[@]}" -eq 0 ]; then
  echo "rdma-native-capture: --out and at least one --dev are required" >&2
  usage
fi
if [ -n "$TRIGGERS" ] && [ -z "$IFACE" ]; then
  echo "rdma-native-capture: --triggers needs --iface" >&2
  exit 2
fi
for t in ${TRIGGERS//,/ }; do
  case "$t" in
    gid | flap) ;;
    *) echo "rdma-native-capture: unknown trigger $t (gid, flap)" >&2; exit 2 ;;
  esac
done
if [ "$DRY_RUN" -eq 0 ] && { [ -n "$TRIGGERS" ] || [ "$UMAD" -eq 1 ]; } && [ "$(id -u)" -ne 0 ]; then
  echo "rdma-native-capture: --triggers and --umad need root" >&2
  exit 2
fi

# The nlcap helper's arguments (nix/microvms/netlink-capture.nix), so the
# NLDEV rows read like the existing pkg/xtcpnl RDMA corpus.
NLDEV_TRACE=(-qq -f -e "trace=socket,sendto,sendmsg,recvfrom,recvmsg" -e write=all -e read=all)
ASYNC_TRACE=(-qq -f -e trace=read -e read=all -xx)
UMAD_TRACE=(-qq -f -e "trace=socket,sendto,sendmsg,recvfrom,recvmsg,openat,ioctl,write,read,poll" -e write=all -e read=all -xx)
GID_ADDR=2001:db8:5300::1/128
NLMON=rdmacap0
SHORT=()

mkdir -p "$OUT/meta"
: >"$OUT/index.tsv"
if [ -n "$VALUES" ]; then
  : >"$VALUES"
fi

record() {
  printf '%s\t%s\t%s\n' "$1" "$2" "$3" >>"$OUT/index.tsv"
}

value() {
  if [ -n "$VALUES" ]; then
    printf '%s %s\n' "$1" "$2" >>"$VALUES"
  fi
}

trace_args() {
  case "$1" in
    nldev) printf '%s\n' "${NLDEV_TRACE[@]}" ;;
    umad) printf '%s\n' "${UMAD_TRACE[@]}" ;;
  esac
}

# row <stream> <name> <command...>: one command under its stream's strace
# arguments. A non-zero exit is an observation and is kept; a row whose strace
# recorded no buffer at all never reached the kernel, and is reported short.
row() {
  local stream=$1 name=$2
  shift 2
  local stem="$stream/$name" rc=0 trace
  mapfile -t trace < <(trace_args "$stream")
  mkdir -p "$OUT/$stream"
  record "$stem" "$*" ""
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '# dry run: strace %s %s\n' "${trace[*]}" "$*" >"$OUT/$stem.strace"
    : >"$OUT/$stem.out"
    : >"$OUT/$stem.err"
    echo 0 >"$OUT/$stem.rc"
    return 0
  fi
  strace "${trace[@]}" -o "$OUT/$stem.strace" "$@" >"$OUT/$stem.out" 2>"$OUT/$stem.err" || rc=$?
  echo "$rc" >"$OUT/$stem.rc"
  if ! grep -q '^ | 00000 ' "$OUT/$stem.strace"; then
    SHORT+=("$stem")
  fi
  echo "RDMACAP_ROW $stem rc=$rc"
}

copy_file() {
  local src=$1 dst=$OUT/$2
  mkdir -p "$(dirname "$dst")"
  if ! cat "$src" >"$dst" 2>/dev/null; then
    rm -f -- "$dst"
  fi
}

ports_of() {
  local dev=$1 p
  if [ "$DRY_RUN" -eq 1 ]; then
    echo 1
    return 0
  fi
  for p in "/sys/class/infiniband/$dev/ports/"*; do
    echo "${p##*/}"
  done
}

copy_sysfs() {
  local dev=$1 base=/sys/class/infiniband/$1 f port portdir g i gid
  if [ "$DRY_RUN" -eq 1 ]; then
    mkdir -p "$OUT/sysfs/$dev/ports/1"
    echo "9803:9b03:0012:3456" >"$OUT/sysfs/$dev/node_guid"
    echo "ACTIVE" >"$OUT/sysfs/$dev/ports/1/state"
    value guid 9803:9b03:0012:3456
    value mac 98:03:9b:12:34:56
    return 0
  fi
  # node_desc is left out: it is usually the hostname, which no redaction covers.
  for f in node_guid sys_image_guid fw_ver node_type hca_type hw_rev board_id; do
    copy_file "$base/$f" "sysfs/$dev/$f"
  done
  for f in node_guid sys_image_guid; do
    g=$(cat "$base/$f" 2>/dev/null || true)
    if [ -n "$g" ] && [ "$g" != "0000:0000:0000:0000" ]; then
      value guid "$g"
    fi
  done
  for portdir in "$base"/ports/*; do
    port=${portdir##*/}
    for f in rate cap_mask state phys_state link_layer lid sm_lid lid_mask_count sm_sl; do
      copy_file "$portdir/$f" "sysfs/$dev/ports/$port/$f"
    done
    for f in "$portdir"/counters/* "$portdir"/hw_counters/*; do
      if [ -f "$f" ]; then
        copy_file "$f" "sysfs/$dev/ports/$port/${f#"$portdir"/}"
      fi
    done
    for g in "$portdir"/gids/*; do
      i=${g##*/}
      gid=$(cat "$g" 2>/dev/null || true)
      if [ -z "$gid" ] || [ "$gid" = "0000:0000:0000:0000:0000:0000:0000:0000" ]; then
        continue
      fi
      copy_file "$g" "sysfs/$dev/ports/$port/gids/$i"
      copy_file "$portdir/gid_attrs/types/$i" "sysfs/$dev/ports/$port/gid_attrs/types/$i"
      copy_file "$portdir/gid_attrs/ndevs/$i" "sysfs/$dev/ports/$port/gid_attrs/ndevs/$i"
      value gid "$gid"
    done
  done
}

# netdev_values <netdev>: its MAC and addresses, for the redaction.
netdev_values() {
  local nd=$1 mac family addr
  mac=$(cat "/sys/class/net/$nd/address" 2>/dev/null || true)
  if [ -n "$mac" ]; then
    value mac "$mac"
  fi
  while read -r family addr; do
    case "$family" in
      inet) value ipv4 "${addr%/*}" ;;
      inet6) value ipv6 "${addr%/*}" ;;
    esac
  done < <(ip -o addr show dev "$nd" 2>/dev/null | awk '{ print $3, $4 }')
}

netdevs_of() {
  local dev=$1
  cat "/sys/class/infiniband/$dev/ports/"*/gid_attrs/ndevs/* 2>/dev/null | sort -u
}

write_meta() {
  local dev drivers="" firmware="" d
  # Every uname field but the nodename, which is the hostname.
  uname -srvmo >"$OUT/meta/uname"
  for dev in "${DEVS[@]}"; do
    if [ -n "$DRIVER" ]; then
      d=$DRIVER
    elif [ -e "/sys/class/infiniband/$dev/device/driver" ]; then
      d=$(basename "$(readlink -f "/sys/class/infiniband/$dev/device/driver")")
    else
      d=unknown
    fi
    drivers="$drivers${drivers:+, }$dev=$d"
    firmware="$firmware${firmware:+, }$dev=$(cat "/sys/class/infiniband/$dev/fw_ver" 2>/dev/null || echo none)"
  done
  echo "$drivers" >"$OUT/meta/driver"
  echo "$firmware" >"$OUT/meta/firmware"
  echo "${RDMA_CORE_VERSION:-unknown}" >"$OUT/meta/rdma_core"
}

wait_active() {
  local dev port state
  for _ in $(seq 1 60); do
    state=ACTIVE
    for dev in "${DEVS[@]}"; do
      for port in $(ports_of "$dev"); do
        if ! grep -q ACTIVE "/sys/class/infiniband/$dev/ports/$port/state" 2>/dev/null; then
          state=down
        fi
      done
    done
    if [ "$state" = ACTIVE ]; then
      return 0
    fi
    sleep 0.5
  done
  echo "RDMACAP_NOT_ACTIVE after 30s" >&2
  return 1
}

# wait_for <file> <pattern>: poll until the file shows the pattern, 10s at most.
wait_for() {
  for _ in $(seq 1 40); do
    if grep -q "$2" "$1" 2>/dev/null; then
      return 0
    fi
    sleep 0.25
  done
  echo "RDMACAP_WAIT_TIMEOUT $1 never showed '$2'" >&2
  return 1
}

# window <trigger> <command>: nlmon and one ibv_asyncwatch per device record
# while the command runs. tcpdump keeps only NETLINK_ROUTE (family 0 at
# offset 14 of the cooked header), as capture-netlink-fixtures does.
window() {
  local t=$1 command=$2 dev tpid spid child
  local -a watchers=()
  mkdir -p "$OUT/nlmon" "$OUT/async"
  record "nlmon/$t" "tcpdump -i $NLMON 'ether[14:2]==0'" "$command"
  for dev in "${DEVS[@]}"; do
    record "async/$dev-$t" "ibv_asyncwatch -d $dev" "$command"
  done
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '%s\n' "# dry run: tcpdump around: $command" >"$OUT/nlmon/$t.txt"
    for dev in "${DEVS[@]}"; do
      printf '# dry run: strace %s ibv_asyncwatch -d %s\n' "${ASYNC_TRACE[*]}" "$dev" >"$OUT/async/$dev-$t.strace"
    done
    return 0
  fi
  ip link add "$NLMON" type nlmon
  ip link set "$NLMON" up
  tcpdump -i "$NLMON" -U -w "$OUT/nlmon/$t.pcap" 'ether[14:2]==0' 2>"$OUT/nlmon/$t.err" &
  tpid=$!
  wait_for "$OUT/nlmon/$t.err" 'listening on' || true
  for dev in "${DEVS[@]}"; do
    strace "${ASYNC_TRACE[@]}" -o "$OUT/async/$dev-$t.strace" \
      stdbuf -oL ibv_asyncwatch -d "$dev" >"$OUT/async/$dev-$t.out" 2>"$OUT/async/$dev-$t.err" &
    watchers+=($!)
    wait_for "$OUT/async/$dev-$t.out" 'async event FD' || true
  done
  echo "RDMACAP_TRIGGER $t: $command"
  sh -c "$command"
  wait_active || true
  # Port events can follow the netdev's by a moment.
  sleep 2
  for spid in "${watchers[@]}"; do
    for child in $(pgrep -P "$spid" || true); do
      kill -TERM "$child" 2>/dev/null || true
    done
    wait "$spid" 2>/dev/null || true
  done
  kill -INT "$tpid" 2>/dev/null || true
  wait "$tpid" 2>/dev/null || true
  rm -f -- "$OUT/nlmon/$t.err"
  ip link del "$NLMON"
  for dev in "${DEVS[@]}"; do
    rm -f -- "$OUT/async/$dev-$t.err"
    if ! grep -q '= 16$' "$OUT/async/$dev-$t.strace"; then
      SHORT+=("async/$dev-$t")
    fi
  done
}

umad_loaded() {
  local dev=$1 d
  for d in /sys/class/infiniband_mad/umad*; do
    if [ "$(cat "$d/ibdev" 2>/dev/null)" = "$dev" ]; then
      return 0
    fi
  done
  return 1
}

if [ "$UMAD" -eq 1 ] && [ "$DRY_RUN" -eq 0 ]; then
  for dev in "${DEVS[@]}"; do
    if ! umad_loaded "$dev"; then
      echo "rdma-native-capture: no umad device for $dev; load ib_umad first (this tool never loads modules)" >&2
      exit 2
    fi
  done
fi

write_meta
for dev in "${DEVS[@]}"; do
  copy_sysfs "$dev"
  if [ "$DRY_RUN" -eq 0 ]; then
    for nd in $(netdevs_of "$dev"); do
      netdev_values "$nd"
    done
  fi
  row nldev "devinfo-$dev" ibv_devinfo -d "$dev"
  row nldev "devinfo_v-$dev" ibv_devinfo -v -d "$dev"
  row nldev "dev_show-$dev" rdma dev show "$dev"
  row nldev "link_show-$dev" rdma link show "$dev"
done

for t in ${TRIGGERS//,/ }; do
  case "$t" in
    gid) window gid "ip -6 addr add $GID_ADDR dev $IFACE nodad && sleep 1 && ip -6 addr del $GID_ADDR dev $IFACE" ;;
    flap) window flap "ip link set $IFACE down && sleep 1 && ip link set $IFACE up" ;;
  esac
done

if [ "$UMAD" -eq 1 ]; then
  for dev in "${DEVS[@]}"; do
    for port in $(ports_of "$dev"); do
      row umad "smpquery_nodeinfo-$dev-$port" smpquery -C "$dev" -P "$port" nodeinfo
      row umad "perfquery-$dev-$port" perfquery -C "$dev" -P "$port"
    done
  done
fi

if [ "${#SHORT[@]}" -gt 0 ]; then
  echo "RDMACAP_SHORT ${#SHORT[@]}: ${SHORT[*]}" >&2
  exit 1
fi
echo "RDMACAP_PASS $OUT"
