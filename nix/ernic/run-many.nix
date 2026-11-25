# nix/ernic/run-many.nix
#
# ernic-run-many-<name>: one rocm-ernic server per NIC around a command,
# for VMs with several ionic NICs (upstream ernic-run serves one).
#
#   ernic-run-many-<name> --dir DIR -- CMD [ARGS...]
#
# - The servers bind DIR/ernic-<nic>.sock and log to DIR/ernic-<nic>.log.
#   CMD (the VM launcher) runs with DIR as its working directory, so the
#   relative socket paths from nics.qemuArgs resolve there.
# - When any NIC has a TAP, the servers run in a "fabric": an unprivileged
#   user + network namespace (`unshare --user --map-root-user --net`)
#   holding the TAPs and the bridges that join them, with addresses and
#   permanent neighbors from `fabric`. CMD stays in the caller's namespace,
#   so QEMU's SLiRP and host ports keep working. No host root, no host
#   network change.
# - Exit status is CMD's. It is non-zero if a server exits before its
#   socket appears or while CMD runs; every server and CMD's process group
#   are stopped on EXIT, INT and TERM.
#
# fabric = { <bridge> = { address = "10.0.4.1/24"; mac = "02:..."; neighbors = [ { address; mac; } ]; }; }
{
  pkgs,
  lib,
  server,
  startupAttempts ? 100,
  pollSeconds ? "0.1",
  shutdownTimeout ? "10s",
}:

{
  name,
  nics ? [ ],
  fabric ? { },
}:

let
  lib' = import ./nics.nix { inherit lib; };
  bridges = lib'.bridges nics;
  list =
    if lib'.problems nics == [ ] then
      nics
    else
      throw "ernic-run-many-${name}: ${lib.concatStringsSep "; " (lib'.problems nics)}";
  taps = lib.filter (t: t != null) (map (n: n.tap or null) list);
  needsNetns = taps != [ ];
  names = map (n: n.name) list;

  fabricScript = pkgs.writeShellApplication {
    name = "ernic-fabric-${name}";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.iproute2
      server
    ];
    text = ''
      dir=$1
      ${lib.concatStrings (
        lib.mapAttrsToList (
          br: brTaps:
          let
            f = fabric.${br} or { };
          in
          ''
            ip link add ${br} type bridge
            ${lib.optionalString (f ? mac) "ip link set ${br} address ${f.mac}"}
            ${lib.optionalString (f ? address) "ip addr add ${f.address} dev ${br}"}
            ip link set ${br} up
            ${lib.concatMapStrings (
              nb: "ip neigh replace ${nb.address} lladdr ${nb.mac} dev ${br} nud permanent\n"
            ) (f.neighbors or [ ])}
          ''
        ) bridges
      )}
      ${lib.concatMapStrings (
        n:
        lib.optionalString ((n.tap or null) != null) ''
          ip tuntap add dev ${n.tap} mode tap
          ${lib.optionalString ((n.bridge or null) != null) "ip link set ${n.tap} master ${n.bridge}"}
          ip link set ${n.tap} up
        ''
      ) list}
      cd "$dir"
      declare -A who
      ${lib.concatMapStrings (n: ''
        rocm-ernic ${lib.escapeShellArgs (lib'.serverArgs n)} > ernic-${n.name}.log 2>&1 &
        who[$!]=${n.name}
      '') list}
      trap 'kill "''${!who[@]}" 2>/dev/null || true; exit 143' TERM INT
      done_pid=""
      wait -n -p done_pid || true
      echo "''${who[$done_pid]:-unknown}" > ernic-failed
      kill "''${!who[@]}" 2>/dev/null || true
      exit 1
    '';
  };
in
pkgs.writeShellApplication {
  name = "ernic-run-many-${name}";
  runtimeInputs = [
    pkgs.coreutils
    pkgs.util-linux
  ];
  text = ''
    usage() {
      echo "usage: ernic-run-many-${name} --dir DIR -- CMD [ARGS...]" >&2
      exit 2
    }
    dir=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --dir) [ "$#" -ge 2 ] || usage; dir=$2; shift 2 ;;
        --) shift; break ;;
        *) usage ;;
      esac
    done
    [ -n "$dir" ] && [ "$#" -gt 0 ] || usage
    mkdir -p "$dir"
    dir=$(cd "$dir" && pwd)

    names=(${lib.escapeShellArgs names})
    for n in "''${names[@]}"; do
      if [ -e "$dir/ernic-$n.sock" ] || [ -L "$dir/ernic-$n.sock" ]; then
        echo "ernic-run-many: $dir/ernic-$n.sock already exists" >&2
        exit 1
      fi
    done
    rm -f "$dir/ernic-failed"

    fabric=""
    command_pid=""
    stop_group() {
      local pid="$1"
      kill -TERM -- "-$pid" 2>/dev/null || true
      timeout --kill-after=1s ${shutdownTimeout} tail --sleep-interval=0.05 --pid="$pid" -f /dev/null || true
      kill -KILL -- "-$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    }
    cleanup() {
      if [ -n "$command_pid" ]; then stop_group "$command_pid"; fi
      if [ -n "$fabric" ]; then stop_group "$fabric"; fi
      for n in "''${names[@]}"; do rm -f "$dir/ernic-$n.sock"; done
    }
    trap 'cleanup' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    failed() {
      cat "$dir/ernic-failed" 2>/dev/null || echo "a server"
    }

    if [ "''${#names[@]}" -gt 0 ]; then
      setsid ${lib.optionalString needsNetns "unshare --user --map-root-user --net -- "}${fabricScript}/bin/ernic-fabric-${name} "$dir" &
      fabric=$!
      up=0
      for _ in $(seq 1 ${toString startupAttempts}); do
        up=1
        for n in "''${names[@]}"; do
          [ -S "$dir/ernic-$n.sock" ] || up=0
        done
        [ "$up" = 1 ] && break
        if ! kill -0 "$fabric" 2>/dev/null; then
          echo "ernic-run-many: $(failed) exited before creating its socket; see $dir/ernic-$(failed).log" >&2
          exit 1
        fi
        sleep ${pollSeconds}
      done
      if [ "$up" != 1 ]; then
        echo "ernic-run-many: not every socket in $dir before the startup deadline" >&2
        exit 1
      fi
    fi

    cd "$dir"
    setsid "$@" <&0 &
    command_pid=$!
    while kill -0 "$command_pid" 2>/dev/null; do
      if [ -n "$fabric" ] && ! kill -0 "$fabric" 2>/dev/null; then
        echo "ernic-run-many: $(failed) exited while the command ran; see $dir/ernic-$(failed).log" >&2
        exit 1
      fi
      sleep ${pollSeconds}
    done
    rc=0
    wait "$command_pid" || rc=$?
    command_pid=""
    trap - EXIT
    cleanup
    exit "$rc"
  '';
}
