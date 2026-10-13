#!/usr/bin/env bash
# Capture one RDMA port's sysfs attributes and counters as a fixture for
# TestRDMACountersRealSysfs. Read-only: it only cats regular files.
#
#   capture.sh SOURCE DEVICE [PORT] > SOURCE-DEVICE-$(uname -r).txt
#
# Output is one "path=value" line per file, relative to
# /sys/class/infiniband/DEVICE, after a "#" header naming the source,
# device, kernel and driver. Symlinks are not followed or recorded; the test
# loader recreates the device and netdev links itself.
set -euo pipefail

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
  echo "usage: $0 SOURCE DEVICE [PORT]" >&2
  exit 2
fi
source_name=$1
device=$2
port=${3:-1}
root=/sys/class/infiniband/$device
if [ ! -d "$root/ports/$port" ]; then
  echo "$0: no port $port on $device" >&2
  exit 1
fi

driver=unknown
if [ -L "$root/device/driver" ]; then
  driver=$(basename "$(readlink "$root/device/driver")")
fi

emit() {
  local rel=$1 value
  if ! value=$(cat "$root/$rel"); then
    echo "$0: cannot read $root/$rel" >&2
    exit 1
  fi
  case $value in
    *$'\n'*)
      echo "$0: $rel has more than one line" >&2
      exit 1
      ;;
  esac
  printf '%s=%s\n' "$rel" "$value"
}

printf '# source: %s\n# device: %s port %s\n# kernel: %s\n# driver: %s\n' \
  "$source_name" "$device" "$port" "$(uname -r)" "$driver"
for name in board_id fw_ver hca_type node_type; do
  if [ -f "$root/$name" ]; then
    emit "$name"
  fi
done
for name in link_layer phys_state rate state; do
  emit "ports/$port/$name"
done
for group in counters counters_ext hw_counters; do
  dir=$root/ports/$port/$group
  if [ ! -d "$dir" ]; then
    continue
  fi
  while IFS= read -r file; do
    emit "ports/$port/$group/$file"
  done < <(find "$dir" -mindepth 1 -maxdepth 1 -type f -printf '%f\n' | LC_ALL=C sort)
done
