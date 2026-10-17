# nix/capture-rdma-testdata.nix
#
# `nix run .#capture-rdma-testdata` - capture a real host's RDMA bytes for the
# go-native decoders (cmd/go-link-monitor/GO-NATIVE-RDMA-DESIGN.md "Testdata").
# The VM sources come from the rxe and ionic runners; this is the one for real
# hardware, written for host `l` (mlx5_0 and mlx5_1).
#
#   capture-rdma-testdata [--dev DEV]... [--source NAME] [--out ROOT]
#                         [--gid-trigger IFACE] [--umad] [--dry-run]
#
# Read-only by default: GET_CHARDEV and NLDEV under strace, and sysfs copies.
# The two opt-ins change the host and need root and the operator's approval:
#
#   --gid-trigger IFACE  adds and removes 2001:db8:5300::1/128 on IFACE while
#                        nlmon and ibv_asyncwatch record (GID change events)
#   --umad               smpquery and perfquery under strace; ib_umad must
#                        already be loaded, this never loads a module
#
# The stage is redacted (GUIDs, GIDs, MACs and addresses become documentation
# values) and written with its manifest to ROOT/<kernel>/<source>/, replacing
# what was there. ROOT defaults to pkg/linkmonitor/internal/rdmanative/testdata
# and the source to <hostname>-<driver>, so `l` writes .../<kernel>/l-mlx5/.
# The real values are only ever in a temporary file, removed on exit.
{
  pkgs,
  rdmaNativeCapture,
  rdmaCaptureManifest,
}:
pkgs.writeShellApplication {
  name = "capture-rdma-testdata";
  runtimeInputs = with pkgs; [
    coreutils
    gnused
    hostname
    rdmaNativeCapture
    rdmaCaptureManifest
  ];
  text = ''
    usage() {
      echo "usage: capture-rdma-testdata [--dev DEV]... [--source NAME] [--out ROOT]" >&2
      echo "                             [--gid-trigger IFACE] [--umad] [--dry-run]" >&2
      exit 2
    }

    devs=()
    source_name=""
    root=""
    gid_iface=""
    umad=0
    dry_run=0
    while [ $# -gt 0 ]; do
      case "$1" in
        --dev) devs+=("$2"); shift 2 ;;
        --source) source_name=$2; shift 2 ;;
        --out) root=$2; shift 2 ;;
        --gid-trigger) gid_iface=$2; shift 2 ;;
        --umad) umad=1; shift ;;
        --dry-run) dry_run=1; shift ;;
        -h | --help) usage ;;
        *) echo "capture-rdma-testdata: unknown argument $1" >&2; usage ;;
      esac
    done

    if [ -z "$root" ]; then
      root=pkg/linkmonitor/internal/rdmanative/testdata
      if [ ! -d pkg/linkmonitor/internal/rdmanative ]; then
        echo "capture-rdma-testdata: run from the xtcp2 repo root, or pass --out" >&2
        exit 2
      fi
    fi
    if [ "''${#devs[@]}" -eq 0 ]; then
      if [ "$dry_run" -eq 1 ]; then
        devs=(mlx5_0)
      else
        for d in /sys/class/infiniband/*; do
          if [ -e "$d" ]; then
            devs+=("''${d##*/}")
          fi
        done
      fi
    fi
    if [ "''${#devs[@]}" -eq 0 ]; then
      echo "capture-rdma-testdata: no RDMA devices in /sys/class/infiniband" >&2
      exit 2
    fi
    if [ -z "$source_name" ]; then
      if [ "$dry_run" -eq 1 ]; then
        source_name=dry-run
      else
        driver=$(basename "$(readlink -f "/sys/class/infiniband/''${devs[0]}/device/driver")")
        source_name="$(hostname -s)-''${driver%_core}"
      fi
    fi
    # The kernel directory naming of the rxe runner: 7.1.8 is 7_1_8 and a
    # release candidate keeps its rc.
    kernel=$(uname -r | sed -E 's/-(rc[0-9]+).*/_\1/; s/-.*//' | tr . _)

    stage=$(mktemp -d -t capture-rdma-testdata-XXXXXX)
    values=$(mktemp -t capture-rdma-values-XXXXXX)
    cleanup() {
      rm -rf -- "$stage"
      rm -f -- "$values"
    }
    trap cleanup EXIT

    args=(--out "$stage" --values "$values")
    for d in "''${devs[@]}"; do
      args+=(--dev "$d")
    done
    if [ -n "$gid_iface" ]; then
      args+=(--triggers gid --iface "$gid_iface")
    fi
    if [ "$umad" -eq 1 ]; then
      args+=(--umad)
    fi
    if [ "$dry_run" -eq 1 ]; then
      args+=(--dry-run)
    fi

    echo "capture-rdma-testdata: devices ''${devs[*]}, source $source_name, kernel $kernel"
    rdma-native-capture "''${args[@]}"
    rdma-capture-manifest write -dir "$stage" -source "$source_name" -values "$values"

    dest="$root/$kernel/$source_name"
    rm -rf -- "$dest"
    mkdir -p "$dest"
    cp -r "$stage"/. "$dest"/
    # Under sudo, hand the files back to the operator who owns the checkout.
    if [ -n "''${SUDO_UID:-}" ] && [ -n "''${SUDO_GID:-}" ]; then
      chown -R "$SUDO_UID:$SUDO_GID" "$root/$kernel"
    fi
    rdma-capture-manifest verify "$dest"
    echo "PASS: wrote $dest"
  '';
}
