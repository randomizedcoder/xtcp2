# demo-linkmonitor-ionic-profiles: the same ionic guest as
# test-linkmonitor-ionic-vm, booted once per rocm-ernic NIC profile with a TAP
# peer, so go-link-monitor's /metrics and /metrics-all can be compared across
# profiles. Each guest pings the TAP from every vCPU, sends TCP SYNs that the
# host answers with RSTs (one flow per port, so the RSTs spread over the RX
# queues), runs an RDMA ping-pong and, where rocm-ernic provides it,
# ernic-rdma-errors, and then scrapes the monitor. The runner
# collects every guest's scrapes and driver views and writes report.md.
#
# This is evidence for review, not a pass/fail test: a scenario that cannot
# capture is reported as such, and the others still run.
{
  pkgs,
  lib,
  nixpkgs,
  monitor,
  ernicModule,
  ernicRun,
}:
let
  name = "linkmonitor-ionic-demo";
  metricsAddress = "127.0.0.1:9101";
  # RFC 5737 TEST-NET-1, as in linkmonitor-ionic.nix.
  tap = {
    ifname = "ernic0";
    host = "192.0.2.1";
    guest = "192.0.2.2";
    prefix = "24";
  };
  # Most scenarios run on the runner's --vcpus (8), so the differences between
  # them come from the profile alone: ionic uses min(profile queues, vCPUs).
  # The two 20-vCPU scenarios show the profile's limit instead of the guest's.
  scenarios = [
    {
      scenario = "compat";
      profile = "compat";
      injects = [ ];
    }
    {
      scenario = "small-vm";
      profile = "small-vm";
      injects = [ ];
    }
    {
      scenario = "generic-100g";
      profile = "generic-100g";
      injects = [ ];
    }
    {
      scenario = "generic-400g";
      profile = "generic-400g";
      injects = [ ];
    }
    {
      scenario = "generic-100g-20vcpu";
      profile = "generic-100g";
      vcpus = 20;
      injects = [ ];
    }
    {
      scenario = "generic-400g-20vcpu";
      profile = "generic-400g";
      vcpus = 20;
      injects = [ ];
    }
    # The same NIC losing every 10th frame each way, as the fault test does.
    {
      scenario = "generic-100g-faults";
      profile = "generic-100g";
      injects = [
        "eth_rx: every 10 effect nobuf"
        "eth_tx: every 10 effect drop"
      ];
    }
  ];
  scenarioNames = map (s: s.scenario) scenarios;
  scenarioCase = lib.concatMapStrings (s: ''
    ${s.scenario})
      ernic_args=(${
        lib.escapeShellArgs (
          [
            "--nic-profile"
            s.profile
          ]
          ++ lib.concatMap (spec: [
            "--inject"
            spec
          ]) s.injects
        )
      })
      scenario_vcpus=${if s ? vcpus then toString s.vcpus else "$vcpus"}
      ;;
  '') scenarios;

  netnsTap = pkgs.writeShellApplication {
    name = "ernic-netns-tap";
    runtimeInputs = [ pkgs.iproute2 ];
    text = ''
      ip link set lo up
      ip tuntap add dev ${tap.ifname} mode tap
      ip addr add ${tap.host}/${tap.prefix} dev ${tap.ifname}
      ip link set ${tap.ifname} up
      exec "$@"
    '';
  };

  guestDemo = pkgs.writeShellApplication {
    name = "${name}-guest";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      ethtool
      gawk
      gnugrep
      iproute2
      iputils
      kmod
      rdma-core
      systemd
      util-linux
    ];
    text = ''
      finish() {
        rc=$?
        if [ "$rc" -ne 0 ]; then
          echo "LINKMONITOR_IONIC_DEMO_FAIL rc=$rc"
        fi
        systemctl poweroff --no-block
      }
      trap finish EXIT
      ionic_netdev() {
        for path in /sys/bus/pci/drivers/ionic/*/net/*; do
          if [ -e "$path" ]; then
            echo "''${path##*/}"
            return 0
          fi
        done
        return 1
      }
      rdma_device() {
        for _ in $(seq 1 60); do
          device=$(rdma link show | awk -v netdev="$1" '$NF == netdev && /state ACTIVE/ { split($2, a, "/"); print a[1] }')
          if [ -n "$device" ]; then
            echo "$device"
            return 0
          fi
          sleep 1
        done
        rdma link show >&2
        return 1
      }
      scrape() {
        curl -sf "http://${metricsAddress}/$1"
      }
      # One ping and one burst of SYNs per vCPU: XPS maps each CPU to its own
      # TX queue, and the host's RSTs carry 32 different port pairs each, so
      # the emulator's flow hash spreads them over the RX queues.
      ethernet_traffic() {
        for cpu in $(seq 0 $(($(nproc) - 1))); do
          (
            taskset -cp "$cpu" "$BASHPID" > /dev/null
            ping -c 25 -i 0.01 -q ${tap.host} > /dev/null || true
            for port in $(seq $((40000 + cpu * 32)) $((40031 + cpu * 32))); do
              curl -s -o /dev/null --connect-timeout 1 "http://${tap.host}:$port/" || true
            done
          )
        done
      }
      # An RC ping-pong between two QPs on the one device, over the RoCE v2
      # GID of the TAP address. Not every emulator build loops back, so a
      # failure is recorded rather than fatal.
      rdma_traffic() {
        port=/sys/class/infiniband/$rdmadev/ports/1
        gid_index=""
        for path in "$port"/gids/*; do
          index=''${path##*/}
          if grep -q 'ffff:c000:0202$' "$path" 2> /dev/null \
            && grep -q 'RoCE v2' "$port/gid_attrs/types/$index" 2> /dev/null; then
            gid_index=$index
            break
          fi
        done
        if [ -z "$gid_index" ]; then
          echo "no RoCE v2 GID for ${tap.guest}"
          return 0
        fi
        timeout 30 ibv_rc_pingpong -d "$rdmadev" -g "$gid_index" -n 500 -s 4096 &
        server=$!
        sleep 1
        timeout 30 ibv_rc_pingpong -d "$rdmadev" -g "$gid_index" -n 500 -s 4096 127.0.0.1 || echo "ibv_rc_pingpong client rc=$?"
        wait "$server" || echo "ibv_rc_pingpong server rc=$?"
      }

      systemctl is-active --quiet ernic-rdma-up.service
      netdev=$(ionic_netdev)
      rdmadev=$(rdma_device "$netdev")
      echo "ionic netdev $netdev, RDMA device $rdmadev"
      ip addr add ${tap.guest}/${tap.prefix} dev "$netdev"
      systemctl start go-link-monitor.service

      # The runner's SHARED_DIR: the captures go straight to the host over
      # 9p, since journald splits console lines longer than 48K.
      out=/tmp/shared
      healthy=0
      for _ in $(seq 1 60); do
        if scrape metrics-all 2> /dev/null | grep -q '^go_link_monitor_collection_healthy 1'; then
          healthy=1
          break
        fi
        sleep 1
      done
      echo "$healthy" > "$out/healthy.txt"
      scrape metrics-all > "$out/metrics-all-before.txt" || true

      ethernet_traffic
      rdma_traffic > "$out/rdma-traffic.txt" 2>&1 || true
      # rocm-ernic's guest module installs ernic-rdma-errors once the RDMA
      # error counters are in; each of its rows makes one error happen and
      # checks hw_counters moved by exactly that.
      if [ -x /run/current-system/sw/bin/ernic-rdma-errors ]; then
        rc=0
        /run/current-system/sw/bin/ernic-rdma-errors -d "$rdmadev" > "$out/rdma-errors.txt" 2>&1 || rc=$?
        echo "exit $rc" >> "$out/rdma-errors.txt"
      fi
      # Two emulator publish intervals and two monitor stats intervals.
      sleep 6

      scrape metrics > "$out/metrics.txt"
      scrape metrics-all > "$out/metrics-all.txt"
      nproc > "$out/nproc.txt"
      echo "$netdev" > "$out/netdev.txt"
      echo "$rdmadev" > "$out/rdmadev.txt"
      uname -r > "$out/kernel.txt"
      ethtool -i "$netdev" > "$out/ethtool-i.txt"
      ethtool "$netdev" > "$out/ethtool.txt"
      ethtool -l "$netdev" > "$out/ethtool-l.txt"
      ethtool -S "$netdev" > "$out/ethtool-S.txt"
      ip -s -s link show dev "$netdev" > "$out/ip-link.txt"
      rdma link show > "$out/rdma-link.txt"
      for path in "/sys/class/infiniband/$rdmadev/ports/1/hw_counters"/*; do
        printf '%s %s\n' "''${path##*/}" "$(cat "$path" 2> /dev/null || echo unreadable)"
      done > "$out/hw_counters.txt"
      journalctl -k --no-pager | grep -i ionic > "$out/dmesg-ionic.txt" || true
      sync
      echo LINKMONITOR_IONIC_DEMO_PASS
    '';
  };

  system = nixpkgs.lib.nixosSystem {
    system = pkgs.stdenv.hostPlatform.system;
    modules = [
      "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
      ernicModule
      {
        nixpkgs.pkgs = pkgs;
        system.stateVersion = "26.05";
        networking.hostName = name;
        networking.useDHCP = false;
        boot.kernelPackages = pkgs.linuxPackages_latest;
        virtualisation = {
          ernic.enable = true;
          graphics = false;
          memorySize = 4096;
          # The runner's -smp overrides this; QEMU keeps the last one.
          cores = 8;
          diskImage = null;
          qemu.networkingOptions = lib.mkForce [ ];
          # No default e1000e: the ionic netdev is the guest's only NIC.
          qemu.options = [
            "-no-reboot"
            "-nic"
            "none"
          ];
        };
        systemd.services.go-link-monitor = {
          description = "go-link-monitor under demo";
          after = [ "ernic-rdma-up.service" ];
          serviceConfig = {
            ExecStart = lib.escapeShellArgs [
              "${monitor}/bin/go-link-monitor"
              "-settle"
              "5s"
              "-collection.mode"
              "poll"
              "-stats-interval"
              "2s"
              "-metrics-addr"
              metricsAddress
              "-baseline-file"
              "/var/lib/go-link-monitor/baseline.json"
            ];
            StateDirectory = "go-link-monitor";
          };
        };
        systemd.services."${name}" = {
          wantedBy = [ "multi-user.target" ];
          wants = [ "ernic-rdma-up.service" ];
          after = [ "ernic-rdma-up.service" ];
          serviceConfig = {
            Type = "oneshot";
            ExecStart = "${guestDemo}/bin/${name}-guest";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };
      }
    ];
  };
  vm = system.config.system.build.vm;

  # The report generator, with its table-driven tests run at build time.
  report = pkgs.runCommand "${name}-report" { nativeBuildInputs = [ pkgs.python3 ]; } ''
    cp ${./linkmonitor_ionic_demo_report.py} linkmonitor_ionic_demo_report.py
    cp ${./linkmonitor_ionic_demo_report_test.py} linkmonitor_ionic_demo_report_test.py
    python3 -m unittest -v linkmonitor_ionic_demo_report_test
    install -D -m 644 linkmonitor_ionic_demo_report.py "$out/linkmonitor_ionic_demo_report.py"
  '';

  launcher = pkgs.writeShellApplication {
    name = "${name}-launcher";
    text = ''
      exec ${vm}/bin/run-${name}-vm > "$1/serial.log" 2>&1
    '';
  };
in
pkgs.writeShellApplication {
  name = "demo-linkmonitor-ionic-profiles";
  runtimeInputs = with pkgs; [
    coreutils
    util-linux
    gawk
    gnugrep
    python3
  ];
  text = ''
    usage() {
      echo 'usage: demo-linkmonitor-ionic-profiles [--accel=auto|kvm|tcg] [--vcpus=N] [--out=DIR] [SCENARIO...]' >&2
      echo '  scenarios: ${lib.concatStringsSep " " scenarioNames} (default: all)' >&2
      echo '  --vcpus sets the guest size for scenarios without their own (default 8)' >&2
      exit 2
    }
    accel='-machine accel=kvm:tcg'
    vcpus=8
    out=""
    selected=()
    for arg in "$@"; do
      case "$arg" in
        --accel=auto) accel='-machine accel=kvm:tcg' ;;
        --accel=kvm) accel='-machine accel=kvm' ;;
        --accel=tcg) accel='-machine accel=tcg' ;;
        --vcpus=*) vcpus=''${arg#--vcpus=} ;;
        --out=*) out=''${arg#--out=} ;;
        -*) usage ;;
        *) selected+=("$arg") ;;
      esac
    done
    case "$vcpus" in
      "" | *[!0-9]*) usage ;;
    esac
    if [ "''${#selected[@]}" -eq 0 ]; then
      selected=(${lib.escapeShellArgs scenarioNames})
    fi
    for scenario in "''${selected[@]}"; do
      case "$scenario" in
        ${lib.concatStringsSep " | " scenarioNames}) ;;
        *) usage ;;
      esac
    done
    if [ -z "$out" ]; then
      out=$(mktemp -d -t ${name}-XXXXXX)
    fi
    mkdir -p "$out"
    out=$(cd "$out" && pwd)
    echo "Output: $out"
    printf '%s\n' '${monitor}' > "$out/monitor.txt"
    printf '%s\n' '${ernicRun}' > "$out/ernic-run.txt"
    printf '%s\n' '${vm}' > "$out/guest.txt"
    unset QEMU_KERNEL_PARAMS QEMU_NET_OPTS NIX_DISK_IMAGE

    captured=()
    for scenario in "''${selected[@]}"; do
      case "$scenario" in
        ${scenarioCase}
      esac
      run_dir=$out/$scenario
      rm -rf "$run_dir"
      mkdir -p "$run_dir/guest"
      printf '%s\n' "''${ernic_args[*]}" > "$run_dir/ernic-args.txt"
      echo "== $scenario: ernic-run ''${ernic_args[*]}, $scenario_vcpus vCPUs"
      rc=0
      (cd "$run_dir" && QEMU_OPTS="$accel -smp $scenario_vcpus" TMPDIR="$run_dir" SHARED_DIR="$run_dir/guest" timeout --kill-after=10s 15m \
        unshare --user --map-root-user --net -- ${netnsTap}/bin/ernic-netns-tap \
        ${ernicRun}/bin/ernic-run --socket "$run_dir/ernic.sock" --log-file "$run_dir/ernic.log" \
        "''${ernic_args[@]}" --tap ${tap.ifname} -- \
        ${launcher}/bin/${name}-launcher "$run_dir") || rc=$?
      if [ "$rc" -ne 0 ] || ! grep -q LINKMONITOR_IONIC_DEMO_PASS "$run_dir/serial.log"; then
        echo "$scenario: no capture (rc=$rc); see $run_dir/serial.log" >&2
        continue
      fi
      captured+=("$scenario")
    done
    if [ "''${#captured[@]}" -eq 0 ]; then
      echo "FAIL: no scenario captured; see $out" >&2
      exit 1
    fi
    python3 -I ${report}/linkmonitor_ionic_demo_report.py "$out" "''${captured[@]}"
    echo "Report: $out/report.md"
    if [ "''${#captured[@]}" -ne "''${#selected[@]}" ]; then
      echo "captured ''${#captured[@]} of ''${#selected[@]} scenarios" >&2
      exit 1
    fi
  '';
}
