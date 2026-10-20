# VM parity for the `ip` command-line paths (IP-ARGV-PATHS-IMPLEMENTATION-PLAN.md
# IA4-T01): go-link-monitor -ip.paths in a disposable guest, every design
# example and every IA2-T02 positive row fetched with curl and compared with
# the goip CLI (byte-identical) and with ip (identical apart from the named
# gaps below). The guest has no network; QEMU's launcher supports KVM and TCG.
{
  pkgs,
  lib,
  nixpkgs,
  monitor,
  goip,
}:
let
  port = "9101";
  # TestHandler's positive GET rows (IA2-T02 and the query form it kept),
  # against the guest's goip0 rather than the fixtures'.
  handlerRows = [
    "/ip/addr"
    "/ip/addr/goip0?format=text"
    "/ip/rule"
    "/ip/link/show"
    "/i/a/s"
    "/i/ad/s"
    "/i/a/show"
    "/i/l/sh"
    "/ip/-j/addr/show"
    "/ip/-br/a"
    "/i/-4/r/s/table/all"
    "/ip/addr?dev=goip0"
    "/i/a?dev=goip0"
    "/i/a/lst"
    "/i/ntbl/show"
  ];
  # Paths whose body differs from `ip`'s output by a known goip gap, each by
  # name. A listed path that no longer differs fails too, so the list stays
  # true.
  # ntable_dev_star (`dev *` for NDTPA_IFINDEX 0) was the only one; goip
  # now prints it (xtcpnl.NeighTblParms.HasIfindex).
  ipGaps = { };
  guestTest = pkgs.writeShellApplication {
    name = "goip-argv-paths-guest";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.curl
      pkgs.diffutils
      pkgs.gnugrep
      pkgs.gnused
      pkgs.iproute2
      pkgs.jq
      pkgs.procps
      pkgs.systemd
      monitor
      goip
    ];
    text = ''
      trap 'echo GOIP_ARGV_PATHS_FAIL; systemctl poweroff --no-block' ERR
      url=http://127.0.0.1:${port}
      work=$(mktemp -d)
      failed=0
      declare -A gaps=(${
        lib.concatStrings (
          lib.mapAttrsToList (p: g: "[${lib.escapeShellArg p}]=${lib.escapeShellArg g} ") ipGaps
        )
      })

      # No IPv6 on the new links: no DAD flags or router solicitations moving
      # the output, or a dummy's counters, between curl and the CLI.
      sysctl -qw net.ipv6.conf.default.disable_ipv6=1
      ip link set lo up
      ip -br link
      # The examples name eth0; a guest NIC by that name moves aside for a
      # dummy whose counters stay still.
      if ip link show dev eth0 > /dev/null 2>&1; then
        ip link set dev eth0 down
        ip link set dev eth0 name guestnic0
      fi
      ip link add eth0 type dummy
      ip addr add 192.0.2.1/24 dev eth0
      ip link set eth0 up
      ip link add goip0 type dummy
      ip addr add 198.51.100.1/24 dev goip0
      ip link set goip0 up
      # iface_named_show_live and prefix_join_live.
      ip link add name show type dummy
      ip link set show up
      ip route add 10.0.0.0/8 dev show
      ip route add 10.1.0.0/16 dev show
      ip route add default dev eth0

      go-link-monitor -metrics-addr 127.0.0.1:${port} -baseline-file "$work/baseline.json" \
        -admin.address= -ip.paths > "$work/monitor.log" 2>&1 &
      for _ in $(seq 1 60); do
        if curl -sf "$url/healthz" > /dev/null; then
          break
        fi
        sleep 1
      done
      curl -sf "$url/healthz" > /dev/null

      # fetch PATH: the status in $status, the body in $work/body and the
      # X-Goip-Argv words (without "ip") in the argv array.
      status=
      argv=()
      fetch() {
        local header
        status=$(curl -s -o "$work/body" -D "$work/headers" -w '%{http_code}' "$url$1")
        header=$(grep -i '^x-goip-argv:' "$work/headers" | sed 's/^[^:]*: *//' | tr -d '\r') || true
        read -ra argv <<< "''${header#ip }"
      }
      fail() {
        echo "GOIP_ARGV_PATHS_ROW_FAIL $*"
        failed=1
      }

      # check PATH WANT_STATUS: curl_eq_goip and curl_eq_ip for one path.
      check() {
        local path=$1 want=$2 gap
        fetch "$path"
        if [ "$status" != "$want" ]; then
          fail "$path: status $status, want $want: $(head -c 300 "$work/body")"
          return
        fi
        if [ "$want" != 200 ]; then
          echo "GOIP_ARGV_PATHS_ROW_PASS $path ($status)"
          return
        fi
        if [ "''${#argv[@]}" -eq 0 ]; then
          fail "$path: no X-Goip-Argv"
          return
        fi
        goip "''${argv[@]}" > "$work/goip" 2>&1 || true
        if ! cmp -s "$work/body" "$work/goip"; then
          fail "$path: differs from goip ''${argv[*]}"
          diff "$work/goip" "$work/body" | head -40 || true
          return
        fi
        ip "''${argv[@]}" > "$work/ip" 2>&1 || true
        gap=''${gaps[$path]:-}
        if cmp -s "$work/body" "$work/ip"; then
          if [ -n "$gap" ]; then
            fail "$path: listed gap '$gap' no longer differs from ip"
            return
          fi
        elif [ -n "$gap" ]; then
          echo "GOIP_ARGV_PATHS_GAP $path: $gap"
        else
          fail "$path: differs from ip ''${argv[*]}"
          diff "$work/ip" "$work/body" | head -40 || true
          return
        fi
        echo "GOIP_ARGV_PATHS_ROW_PASS $path"
      }

      # Every design example, from the index itself; a not_served one is a
      # 501 until goip parses its keyword.
      curl -sf "$url/ip" | jq -r '.examples[] | [.path, (.not_served // "")] | @tsv' > "$work/examples"
      [ -s "$work/examples" ]
      while IFS=$'\t' read -r path gap; do
        if [ -n "$gap" ]; then
          check "$path" 501
        else
          check "$path" 200
        fi
      done < "$work/examples"
      echo GOIP_ARGV_PATHS_EXAMPLES_DONE

      for path in ${lib.escapeShellArgs handlerRows}; do
        check "$path" 200
      done
      echo GOIP_ARGV_PATHS_HANDLER_ROWS_DONE

      # write_refused_live: refused, and nothing changed.
      before=$(ip -d link show; ip addr show; ip route show table all)
      check /i/l/s 400
      check /ip/addr/flush 400
      check /ip/link/set/dev/eth0/down 400
      after=$(ip -d link show; ip addr show; ip route show table all)
      if [ "$before" != "$after" ]; then
        fail "write_refused_live: the guest's links, addresses or routes changed"
        diff <(echo "$before") <(echo "$after") || true
      fi
      echo GOIP_ARGV_PATHS_WRITE_REFUSED_DONE

      # prefix_join_live: the joined prefix is one token and lists the
      # route; ip's root, match and exact forms, and a host address that
      # equals no route, each compared with ip.
      check /ip/route/show/10.0.0.0/8 200
      grep -q '^10\.0\.0\.0/8 dev show' "$work/body" || fail "prefix_join_live: 10.0.0.0/8 not listed"
      check /ip/-json/route/show/0.0.0.0/0 200
      grep -q '"dst":"default"' "$work/body" || fail "prefix_join_live: 0.0.0.0/0 is not the default route"
      check /ip/route/show/0.0.0.0 200
      check /i/r/s/root/10.0.0.0/8 200
      check /i/r/s/match/10.1.2.3 200
      check /i/r/s/to/exact/10.0.0.0%2F8 200
      check /ip/route/show/10.0.0.0%2F33 400
      echo GOIP_ARGV_PATHS_PREFIX_JOIN_DONE

      # iface_named_show_live: a link named like a verb.
      check /ip/link/show/dev/show 200
      grep -q ': show: ' "$work/body" || fail "iface_named_show_live: link show not listed"
      # ip link show NAME is dev NAME, as for address show.
      check /ip/link/show/goip0 200
      grep -q ': goip0: ' "$work/body" || fail "link_bare_device: goip0 not listed"
      check /i/l/sh/eth0 200
      check /ip/link/show/nosuch0 404
      echo GOIP_ARGV_PATHS_IFACE_SHOW_DONE

      # selectors_live: link show type, route show proto and neighbor show
      # nud, each with a selection that lists something.
      check /ip/-d/link/show/type/dummy 200
      grep -q ': goip0: ' "$work/body" || fail "selectors_live: type dummy omits goip0"
      check /ip/-j/link/show/type/dummy 200
      check /i/l/sh/goip0/type/dummy 200
      check /i/r/s/proto/kernel 200
      grep -q 'proto' "$work/body" && fail "selectors_live: proto filter still prints proto"
      check /i/r/s/table/all/p/boot 200
      check /i/n/s/nud/all 200
      check /i/n/s/nud/none/nud/noarp 200
      check /i/n/s/nud/bogus 400
      echo GOIP_ARGV_PATHS_SELECTORS_DONE

      # A missing device on a live kernel: the get answers ENODEV first, so
      # it is a 404, never the dumps-only 500.
      check /ip/addr/show/dev/nosuch0 404
      check /i/-j/l/sh/dev/nosuch0 404
      echo GOIP_ARGV_PATHS_MISSING_DEV_DONE

      if [ "$failed" -ne 0 ]; then
        echo "--- go-link-monitor log"
        cat "$work/monitor.log"
        false
      fi
      echo GOIP_ARGV_PATHS_PASS
      systemctl poweroff --no-block
    '';
  };
  vm =
    (nixpkgs.lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          networking.hostName = "goip-argv-paths";
          networking.useDHCP = false;
          boot.kernelModules = [ "dummy" ];
          virtualisation = {
            graphics = false;
            memorySize = 2048;
            cores = 2;
            diskImage = null;
            qemu.networkingOptions = lib.mkForce [ ];
            qemu.options = [ "-no-reboot" ];
          };
          systemd.services.goip-argv-paths = {
            wantedBy = [ "multi-user.target" ];
            after = [ "systemd-modules-load.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/goip-argv-paths-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
  sentinels = map (s: "GOIP_ARGV_PATHS_${s}") [
    "EXAMPLES_DONE"
    "HANDLER_ROWS_DONE"
    "WRITE_REFUSED_DONE"
    "PREFIX_JOIN_DONE"
    "IFACE_SHOW_DONE"
    "SELECTORS_DONE"
    "MISSING_DEV_DONE"
    "PASS"
  ];
  # checks.vm-goip-argv-paths and test-goip-argv-paths-vm-check
  # (nix/microvms/mkVmCheck.nix).
  vmCheck = import ../microvms/mkVmCheck.nix { inherit pkgs lib; } {
    name = "goip-argv-paths";
    launch = "${vm}/bin/run-goip-argv-paths-vm";
    inherit sentinels;
    failPattern = "GOIP_ARGV_PATHS_(ROW_)?FAIL";
    evidence.guest = vm;
  };
in
pkgs.writeShellApplication {
  name = "test-goip-argv-paths-vm";
  runtimeInputs = with pkgs; [
    coreutils
    gnugrep
  ];
  text = ''
    accel=auto
    case "''${1:---accel=auto}" in
      --accel=auto) ;;
      --accel=kvm) accel=kvm ;;
      --accel=tcg) accel=tcg ;;
      *) echo 'usage: test-goip-argv-paths-vm [--accel=auto|kvm|tcg]' >&2; exit 2 ;;
    esac
    if [ "$#" -gt 1 ]; then echo 'too many arguments' >&2; exit 2; fi
    run_dir=$(mktemp -d -t goip-argv-paths-XXXXXX)
    echo "Evidence: $run_dir"
    printf '%s\n' '${vm}' > "$run_dir/guest.txt"
    export TMPDIR="$run_dir"
    cd "$run_dir"
    case "$accel" in
      auto) export QEMU_OPTS='-machine accel=kvm:tcg' ;;
      kvm) export QEMU_OPTS='-machine accel=kvm' ;;
      tcg) export QEMU_OPTS='-machine accel=tcg' ;;
    esac
    unset QEMU_KERNEL_PARAMS QEMU_NET_OPTS NIX_DISK_IMAGE
    # stdin from /dev/null: timeout runs qemu in its own process group, and
    # a -nographic qemu reading an interactive terminal from there is
    # stopped (SIGTTIN) before it prints anything, until the timeout.
    timeout --kill-after=10s 15m ${vm}/bin/run-goip-argv-paths-vm < /dev/null > "$run_dir/serial.log" 2>&1 || {
      cat "$run_dir/serial.log"
      exit 1
    }
    cat "$run_dir/serial.log"
    if [ ! -s "$run_dir/serial.log" ]; then echo 'FAIL: the guest wrote nothing' >&2; exit 1; fi
    for sentinel in ${lib.escapeShellArgs sentinels}; do
      grep -q "$sentinel" "$run_dir/serial.log"
    done
    if grep -E 'GOIP_ARGV_PATHS_(ROW_)?FAIL' "$run_dir/serial.log"; then exit 1; fi
    echo "PASS: goip argv paths VM; evidence $run_dir"
  '';
}
// {
  inherit vmCheck;
}
