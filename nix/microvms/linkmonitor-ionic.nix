# Real-driver guest: the in-tree ionic/ionic_rdma drivers bound to a rocm-ernic
# emulated NIC (vfio-user), so linuxio and the full monitor see a real driver's
# netlink and ethtool replies. Hot-unplug and replug are a guest PCI
# remove and rescan: QMP device_del of a vfio-user-pci device aborts QEMU 11.0.2
# (memory_region_del_subregion assertion), while the guest's driver removal and
# re-probe paths, which the monitor has to survive, are the same either way.
#
# With faults = true it is test-linkmonitor-ionic-faults-vm instead: the
# generic-100g NIC profile (16 queues, 8 used on 8 vCPUs), injected eth_rx, eth_tx and
# stats_vals faults, and a TAP in an unprivileged user and network namespace,
# so the guest has a peer to ping and the emulator has frames to drop. The
# monitor's interface counters must then agree with sysfs, ethtool and the
# fault pattern, frame for frame.
#
# --capture records the go-native RDMA corpus from the real driver as well
# (GO-NATIVE-RDMA-DESIGN.md "Testdata"): the guest runs rdma-native-capture
# before the monitor starts, and the runner writes its manifest and, with
# --promote or --native-out, puts it in
# pkg/linkmonitor/internal/rdmanative/testdata/<kernel>/vm-ionic.
# --capture --dry-run --native-out=DIR does that from placeholder contents,
# without a VM.
{
  pkgs,
  lib,
  nixpkgs,
  artifact,
  monitor,
  ernicModule,
  ernicRun,
  rdmaCaptureManifest,
  faults ? false,
}:
let
  rdmaNativeCapture = import ../tests/rdma-native-capture.nix { inherit pkgs; };
  metricsAddress = "127.0.0.1:9101";
  name = if faults then "linkmonitor-ionic-faults" else "linkmonitor-ionic";
  # The host end of the TAP lives in the namespace ernic-run starts in; the
  # guest gives its ionic netdev the other address. RFC 5737 TEST-NET-1.
  tap = {
    ifname = "ernic0";
    host = "192.0.2.1";
    guest = "192.0.2.2";
    prefix = "24";
  };
  # Every 10th frame each way, so the guest's drop counters are an exact
  # function of its packet counters. stats_vals event 1 is ionic_rdma's read
  # at registration, which must succeed or the RDMA device is torn down.
  injects = [
    "eth_rx: every 10 effect nobuf"
    "eth_tx: every 10 effect drop"
    "stats_vals: after 2 effect fail"
  ];
  ernicArgs = lib.optionals faults (
    [
      "--nic-profile"
      "generic-100g"
      "--tap"
      tap.ifname
    ]
    ++ lib.concatMap (spec: [
      "--inject"
      spec
    ]) injects
  );
  # Run inside `unshare --user --map-root-user --net`: root there may create
  # the TAP, and the server attaches to it after creating its socket.
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
  # The same capture that produced the host fixtures in
  # pkg/linkmonitor/testdata/rdmasysfs, for TestRDMACountersRealSysfs.
  sysfsCapture = pkgs.writeShellApplication {
    name = "rdmasysfs-capture";
    runtimeInputs = with pkgs; [
      coreutils
      findutils
    ];
    text = builtins.readFile ../../pkg/linkmonitor/testdata/rdmasysfs/capture.sh;
  };
  healthySteps = ''
    # ernic-rdma-up (guest module) opened the netdev, then loaded ionic_rdma.
    systemctl is-active --quiet ernic-rdma-up.service
    discover
    run_test TestIonicDiscovery
    echo LINKMONITOR_IONIC_DISCOVERY_PASS

    nlrec=$(mktemp -d)
    ${sysfsCapture}/bin/rdmasysfs-capture ionic-vm "$rdmadev" > "$nlrec/rdmasysfs-$rdmadev.txt"
    LINKMONITOR_NLRECORD="$nlrec" LINKMONITOR_LIVE_INTERFACE="$netdev" \
      ${artifact}/bin/linuxio-nlrecord.test -test.run '^TestLiveRequestShapes$' -test.v
    echo LINKMONITOR_IONIC_CONTRACTS_PASS

    # --capture puts xtcp2.rdmacapture=1 on the kernel command line. Before
    # the monitor starts, so the flap and the GID are the only events, and
    # discover runs again because the flap takes the port down.
    if grep -qw xtcp2.rdmacapture=1 /proc/cmdline; then
      modprobe ib_umad
      ${rdmaNativeCapture}/bin/rdma-native-capture --out "$nlrec/native" --dev "$rdmadev" \
        --triggers flap,gid --iface "$netdev" --umad --driver ionic_rdma
      discover
      echo LINKMONITOR_IONIC_NATIVE_CAPTURE_PASS
    fi

    systemctl start go-link-monitor.service
    run_test TestIonicMonitorHealthy
    # The single CGO_ENABLED=0 build reports its netlink events backend.
    run_test TestIonicAutoBackend
    echo LINKMONITOR_IONIC_MONITOR_PASS
    run_test TestIonicCLIDifferential
    echo LINKMONITOR_IONIC_CLI_PASS
    run_test TestIonicEvents
    echo LINKMONITOR_IONIC_EVENTS_PASS
    # The netlink source, through the monitor's own openRDMAEvents, against
    # the events the verbs source recorded over the same triggers (R1-T05,
    # R4-T02).
    run_test TestRDMAEventsRecorded
    run_test TestRDMAEventsFaults
    echo LINKMONITOR_IONIC_RECORDED_PASS

    # Removal unregisters the RDMA device while the monitor holds its netlink
    # event source, and while TestRDMAEventsRemove holds another, which must
    # report it as Fatal. remove_ionic shows where removal blocks instead of
    # waiting for the timeout.
    LINKMONITOR_EVENTS_REMOVE=remove_ionic run_test TestRDMAEventsRemove
    echo LINKMONITOR_IONIC_REMOVE_PASS
    run_test TestIonicRemoved
    echo LINKMONITOR_IONIC_UNPLUG_PASS

    # Loaded first, ionic_rdma takes every interrupt for EQs and the netdev's
    # rx queues fail with -ENOSPC; unload it so ernic-rdma-up can order them.
    modprobe -r ionic_rdma
    echo 1 > /sys/bus/pci/rescan
    for _ in $(seq 1 90); do
      if ionic_netdev > /dev/null; then
        break
      fi
      sleep 1
    done
    systemctl restart ernic-rdma-up.service
    discover
    run_test TestIonicRecovered
    echo LINKMONITOR_IONIC_HOTPLUG_PASS
    # Coverage must be live again, not just reported: the same transitions
    # and renames have to be observed on the replugged device.
    run_test TestIonicEvents
    run_test TestRDMAEventsRecorded
    echo LINKMONITOR_IONIC_RECOVERED_EVENTS_PASS

    on_demand
    run_test TestIonicOnDemand
    echo LINKMONITOR_IONIC_ON_DEMAND_PASS

    uname -r > "$nlrec/kernel.txt"
    echo XTCP2_NLREC_DUMP_START
    tar c -C "$nlrec" . | gzip -n | base64 -w0
    echo
    echo XTCP2_NLREC_DUMP_END
  '';
  faultSteps = ''
    systemctl is-active --quiet ernic-rdma-up.service
    discover
    # Before the monitor starts, so the guest's own reads are events 2 and 3.
    run_test TestIonicFaultStatsVals
    echo LINKMONITOR_IONIC_FAULT_STATS_VALS_PASS
    ip addr add ${tap.guest}/${tap.prefix} dev "$netdev"
    export LINKMONITOR_IONIC_PEER=${tap.host}
    systemctl start go-link-monitor.service
    run_test TestIonicFaultCounters
    echo LINKMONITOR_IONIC_FAULT_COUNTERS_PASS
    on_demand
    run_test TestIonicOnDemandErrors
    echo LINKMONITOR_IONIC_ON_DEMAND_ERRORS_PASS
  '';
  guestTest = pkgs.writeShellApplication {
    name = "${name}-guest";
    runtimeInputs = with pkgs; [
      bash
      coreutils
      ethtool
      gawk
      gnugrep
      gnutar
      gzip
      iproute2
      iputils
      kmod
      rdma-core
      systemd
    ];
    text = ''
      # EXIT, not ERR: errexit inside a function (run_test) does not run an ERR
      # trap without errtrace, and the guest would then idle at the login prompt.
      finish() {
        rc=$?
        if [ "$rc" -ne 0 ]; then
          echo "LINKMONITOR_IONIC_FAIL rc=$rc"
        fi
        systemctl poweroff --no-block
      }
      trap finish EXIT
      ionic_pci() {
        for path in /sys/bus/pci/drivers/ionic/0000:*; do
          if [ -e "$path" ]; then
            echo "''${path##*/}"
            return 0
          fi
        done
        return 1
      }
      ionic_netdev() {
        for path in /sys/bus/pci/drivers/ionic/*/net/*; do
          if [ -e "$path" ]; then
            echo "''${path##*/}"
            return 0
          fi
        done
        return 1
      }
      # The RDMA device whose port 1 is ACTIVE on the given netdev.
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
      run_test() {
        ${artifact}/bin/ionic-vm.test -test.run "^$1\$" -test.v -test.timeout=10m
      }
      # The same monitor on demand, on the same address and baseline.
      on_demand() {
        systemctl stop go-link-monitor.service
        systemctl start go-link-monitor-on-demand.service
      }
      discover() {
        netdev=$(ionic_netdev)
        rdmadev=$(rdma_device "$netdev")
        export LINKMONITOR_IONIC_NETDEV="$netdev" LINKMONITOR_IONIC_RDMA="$rdmadev"
        export LINKMONITOR_EVENTS_NETDEV="$netdev" LINKMONITOR_EVENTS_RDMA="$rdmadev"
        echo "ionic netdev $netdev, RDMA device $rdmadev"
      }
      # Run by TestRDMAEventsRemove through bash -c, where errexit is
      # off, so every failure returns explicitly.
      remove_ionic() {
        local pci remover
        pci=$(ionic_pci) || return 1
        echo "removing ionic PCI function $pci"
        echo 1 > "/sys/bus/pci/devices/$pci/remove" &
        remover=$!
        for _ in $(seq 1 90); do
          if [ ! -e "/sys/class/net/$LINKMONITOR_IONIC_NETDEV" ] && ! kill -0 "$remover" 2> /dev/null; then
            break
          fi
          sleep 1
        done
        if kill -0 "$remover" 2> /dev/null; then
          echo "PCI remove of $pci still blocked after 90s; kernel stack:" >&2
          cat "/proc/$remover/stack" >&2 || true
          echo w > /proc/sysrq-trigger
          return 1
        fi
        wait "$remover" || return 1
        if [ -e "/sys/class/net/$LINKMONITOR_IONIC_NETDEV" ]; then
          echo "ionic netdev $LINKMONITOR_IONIC_NETDEV still present after PCI remove" >&2
          return 1
        fi
      }
      export -f ionic_pci remove_ionic
      export LINKMONITOR_METRICS_URL=http://${metricsAddress}/metrics-all

      ${if faults then faultSteps else healthySteps}
      echo LINKMONITOR_IONIC_PASS
    '';
  };

  mkMonitorService = package: description: flags: {
    inherit description;
    after = [ "ernic-rdma-up.service" ];
    serviceConfig = {
      ExecStart = lib.escapeShellArgs (
        [
          "${package}/bin/go-link-monitor"
          "-settle"
          "5s"
          "-metrics-addr"
          metricsAddress
          "-baseline-file"
          "/var/lib/go-link-monitor/baseline.json"
        ]
        ++ flags
      );
      StateDirectory = "go-link-monitor";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };
  monitorService = mkMonitorService monitor;
  # Poll mode, so the counter checks see a fresh sample every 2s.
  pollFlags = [
    "-collection.mode"
    "poll"
    "-stats-interval"
    "2s"
  ];

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
          # generic-100g offers 16 queues; ionic uses one per online CPU, so 8.
          cores = if faults then 8 else 4;
          diskImage = null;
          qemu.networkingOptions = lib.mkForce [ ];
          qemu.options = [ "-no-reboot" ];
        };
        systemd.services.go-link-monitor = monitorService "go-link-monitor under test" pollFlags;
        # Intervals match production_ionic_ondemand_vm_linux_test.go.
        systemd.services.go-link-monitor-on-demand =
          monitorService "go-link-monitor under test, on demand" [
            "-collection.mode"
            "on-demand"
            "-watch.interval"
            "2s"
            "-watch.slow-interval"
            "4s"
            "-on-demand.ttl"
            "5s"
          ]
          // {
            # Conflicts= works both ways: starting either unit stops the other.
            conflicts = [ "go-link-monitor.service" ];
          };
        systemd.services."${name}-test" = {
          wantedBy = [ "multi-user.target" ];
          wants = [ "ernic-rdma-up.service" ];
          after = [ "ernic-rdma-up.service" ];
          serviceConfig = {
            Type = "oneshot";
            ExecStart = "${guestTest}/bin/${name}-guest";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };
      }
    ];
  };
  vm = system.config.system.build.vm;

  # Runs under ernic-run, which owns the emulator for the VM's lifetime.
  launcher = pkgs.writeShellApplication {
    name = "${name}-launcher";
    text = ''
      exec ${vm}/bin/run-${name}-vm > "$1/serial.log" 2>&1
    '';
  };
  fixtureSteps = ''
    sysfsdata=pkg/linkmonitor/testdata/rdmasysfs
    mkdir -p "$run_dir/fixtures"
    awk '/XTCP2_NLREC_DUMP_START/{flag=1;next} /XTCP2_NLREC_DUMP_END/{flag=0} flag' "$run_dir/serial.log" \
      | sed -E 's/^\[[^]]*\] linkmonitor-ionic-guest\[[0-9]+\]: //' \
      | tr -d '\r\n ' \
      | base64 -d \
      | gzip -dc \
      | tar x -C "$run_dir/fixtures"
    kernel=$(cat "$run_dir/fixtures/kernel.txt")
    echo "fixtures: $(find "$run_dir/fixtures" -name '*.pcap' | wc -l) recording(s) from kernel $kernel in $run_dir/fixtures"
    if [ "$promote" -eq 1 ]; then
      dest="$testdata/ionic-vm/$kernel"
      mkdir -p "$dest"
      for path in "$run_dir"/fixtures/*.pcap; do
        name=''${path##*/}
        install -m 644 "$path" "$dest/$(echo "$name" | sed -E 's/^([a-z]+)-[0-9]+-([0-9]+)\.pcap$/\1-\2.pcap/')"
      done
      echo "promoted into $dest; regenerate goldens with: go test ./pkg/linkmonitor/internal/linuxio -run TestRecordedContracts -update"
      for path in "$run_dir"/fixtures/rdmasysfs-*.txt; do
        name=''${path##*/rdmasysfs-}
        install -m 644 "$path" "$sysfsdata/ionic-vm-''${name%.txt}-$kernel.txt"
        echo "promoted $sysfsdata/ionic-vm-''${name%.txt}-$kernel.txt"
      done
    fi
    if [ "$capture" -eq 1 ]; then
      if [ ! -d "$run_dir/fixtures/native" ]; then
        echo "FAIL: --capture but the guest recorded no native/ stage; evidence $run_dir" >&2
        exit 1
      fi
      if [ -z "$native_out" ] && [ "$promote" -eq 1 ]; then
        # The rxe runner's kernel directory naming: 7.2.9 is 7_2_9.
        native_out="pkg/linkmonitor/internal/rdmanative/testdata/$(echo "$kernel" | sed -E 's/-(rc[0-9]+).*/_\1/; s/-.*//' | tr . _)/vm-ionic"
      fi
      native_promote "$run_dir/fixtures/native" "$native_out"
    fi
  '';
  # The server prints each fault's counts at shutdown. Each must have fired:
  # a fault that never fired proves nothing about the counters.
  faultSummary = ''
    for site in eth_rx eth_tx stats_vals; do
      line=$(grep -m1 "Fault $site: eligible" "$run_dir/ernic.log" || true)
      echo "server: ''${line# }"
      fired=''${line##*fired }
      if [ -z "$line" ] || [ "$fired" = 0 ]; then
        echo "FAIL: fault $site did not fire; evidence $run_dir" >&2
        exit 1
      fi
    done
  '';
in
pkgs.writeShellApplication {
  name = "test-${name}-vm";
  runtimeInputs = with pkgs; [
    coreutils
    util-linux
    findutils
    gawk
    gnugrep
    gnused
    gnutar
    gzip
    rdmaNativeCapture
    rdmaCaptureManifest
  ];
  text = ''
    usage() {
      echo 'usage: test-${name}-vm [--accel=auto|kvm|tcg] [--promote] [--capture [--native-out=DIR] [--dry-run]]' >&2
      echo '  --promote copies the recorded fixtures into ./pkg/linkmonitor/internal/linuxio/testdata/nlrecord/ionic-vm/<kernel>/' >&2
      echo '  and the sysfs capture into ./pkg/linkmonitor/testdata/rdmasysfs/' >&2
      echo '  --capture records the go-native RDMA corpus too; with --promote it goes to' >&2
      echo '  ./pkg/linkmonitor/internal/rdmanative/testdata/<kernel>/vm-ionic/, or to --native-out' >&2
      echo '  --dry-run (with --capture and --native-out) writes placeholder contents, no VM' >&2
      exit 2
    }
    # native_promote SRC DEST: write SRC's manifest, then put it at DEST,
    # replacing what was there; an empty DEST leaves it in the evidence. The
    # GUIDs come from the emulated NIC, so there is nothing real to redact.
    native_promote() {
      rdma-capture-manifest write -dir "$1" -source vm-ionic
      if [ -n "$2" ]; then
        rm -rf -- "$2"
        mkdir -p "$2"
        cp -r "$1"/. "$2"/
        rdma-capture-manifest verify "$2"
        echo "promoted the native capture into $2"
      fi
    }
    accel='-machine accel=kvm:tcg'
    promote=0
    capture=0
    dry_run=0
    native_out=""
    for arg in "$@"; do
      case "$arg" in
        --accel=auto) accel='-machine accel=kvm:tcg' ;;
        --accel=kvm) accel='-machine accel=kvm' ;;
        --accel=tcg) accel='-machine accel=tcg' ;;
        --promote) promote=1 ;;
        --capture) capture=1 ;;
        --dry-run) dry_run=1 ;;
        --native-out=*) native_out=''${arg#--native-out=} ;;
        *) usage ;;
      esac
    done
    ${lib.optionalString faults "[ \"$promote\" -eq 0 ] && [ \"$capture\" -eq 0 ] || usage # no recordings in the fault run"}
    if { [ "$dry_run" -eq 1 ] || [ -n "$native_out" ]; } && [ "$capture" -eq 0 ]; then
      usage
    fi
    if [ "$dry_run" -eq 1 ]; then
      [ -n "$native_out" ] || usage
      stage=$(mktemp -d -t ${name}-dry-XXXXXX)
      # The arguments healthySteps gives the guest, with typical names.
      rdma-native-capture --dry-run --out "$stage/native" --dev ionic_0 \
        --triggers flap,gid --iface enp0s3 --umad --driver ionic_rdma
      native_promote "$stage/native" "$native_out"
      rm -rf -- "$stage"
      echo "PASS: ${name} capture dry run"
      exit 0
    fi
    testdata=pkg/linkmonitor/internal/linuxio/testdata/nlrecord
    if [ "$promote" -eq 1 ] && [ ! -d "$testdata" ]; then
      echo "--promote must run from the xtcp2 checkout root ($testdata missing)" >&2
      exit 2
    fi
    run_dir=$(mktemp -d -t ${name}-XXXXXX)
    echo "Evidence: $run_dir"
    printf '%s\n' '${artifact}' > "$run_dir/artifact.txt"
    printf '%s\n' '${monitor}' > "$run_dir/monitor.txt"
    printf '%s\n' '${vm}' > "$run_dir/guest.txt"
    export QEMU_OPTS="$accel"
    printf '%s\n' "$QEMU_OPTS" > "$run_dir/qemu-options.txt"
    export TMPDIR="$run_dir"
    unset QEMU_KERNEL_PARAMS QEMU_NET_OPTS NIX_DISK_IMAGE
    if [ "$capture" -eq 1 ]; then
      export QEMU_KERNEL_PARAMS=xtcp2.rdmacapture=1
    fi
    rc=0
    (cd "$run_dir" && timeout --kill-after=10s 30m \
      ${lib.optionalString faults "unshare --user --map-root-user --net -- ${netnsTap}/bin/ernic-netns-tap"} \
      ${ernicRun}/bin/ernic-run --socket "$run_dir/ernic.sock" --log-file "$run_dir/ernic.log" \
      ${lib.escapeShellArgs ernicArgs} -- \
      ${launcher}/bin/${name}-launcher "$run_dir") || rc=$?
    cat "$run_dir/serial.log"
    if [ "$rc" -ne 0 ]; then
      echo "FAIL: launcher exited $rc; evidence $run_dir" >&2
      exit 1
    fi
    for sentinel in ${
      if faults then
        "FAULT_STATS_VALS FAULT_COUNTERS"
      else
        "DISCOVERY CONTRACTS MONITOR CLI EVENTS RECORDED REMOVE UNPLUG HOTPLUG RECOVERED_EVENTS"
    }; do
      if ! grep -q "LINKMONITOR_IONIC_''${sentinel}_PASS" "$run_dir/serial.log"; then
        echo "FAIL: missing LINKMONITOR_IONIC_''${sentinel}_PASS; evidence $run_dir" >&2
        exit 1
      fi
    done
    if [ "$capture" -eq 1 ] && ! grep -q LINKMONITOR_IONIC_NATIVE_CAPTURE_PASS "$run_dir/serial.log"; then
      echo "FAIL: missing LINKMONITOR_IONIC_NATIVE_CAPTURE_PASS; evidence $run_dir" >&2
      exit 1
    fi
    if grep -q LINKMONITOR_IONIC_FAIL "$run_dir/serial.log" || ! grep -q 'LINKMONITOR_IONIC_PASS' "$run_dir/serial.log"; then
      echo "FAIL: guest test failed; evidence $run_dir" >&2
      exit 1
    fi
    ${if faults then faultSummary else fixtureSteps}
    echo "PASS: ${name} VM; evidence $run_dir"
  '';
}
