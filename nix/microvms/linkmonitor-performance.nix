# Privileged link-monitor performance and live-netlink runner.
#
# The guest runs as root, captures rtnetlink traffic from an nlmon device, and
# returns benchmark/profile/pcap evidence over the VM console. The VM is kept
# separate from the ordinary synthetic benchmark artifact so permission and
# kernel-dependent tests cannot contaminate the normal Nix build.
{
  pkgs,
  lib,
  nixpkgs,
  artifact,
  rdmaNativeBench,
}:
let
  guest = pkgs.writeShellApplication {
    name = "linkmonitor-performance-guest";
    runtimeInputs = with pkgs; [
      coreutils
      gnutar
      gzip
      iproute2
      kmod
      procps
      tcpdump
    ];
    text = ''
      set -euo pipefail
      out=/tmp/linkmonitor-performance
      rm -rf "$out"
      mkdir -p "$out"

      echo LINKMONITOR_PERF_BOOT
      modprobe nlmon 2>/dev/null || true
      ip link add nlmon0 type nlmon
      ip link set nlmon0 up
      tcpdump --immediate-mode -i nlmon0 -w "$out/netlink.pcap" 'ether[14:2]==0' >/tmp/linkmonitor-tcpdump.log 2>&1 &
      capture_pid=$!
      trap 'kill "$capture_pid" 2>/dev/null || true; wait "$capture_pid" 2>/dev/null || true' EXIT

      # Run the root-only live tests first. A failure remains visible in the
      # transcript, while the benchmark/profile phase still produces evidence.
      set +e
      "${artifact}/bin/core.test" -test.run 'Test(IdentityLiveReadOnly|SettingsLiveReadOnly|StatisticLiveReadOnly|TrafficReadOnlyKernel)$' -test.v >"$out/live-tests.txt" 2>&1
      live_rc=$?
      set -e
      printf '%s\n' "$live_rc" > "$out/live-exit.txt"

      "${artifact}/bin/core.test" -test.run '^$' \
        -test.bench '^BenchmarkPerformance(LocalMachine|RealisticFleet)$' \
        -test.benchmem -test.benchtime="''${LINKMONITOR_BENCHTIME:-5s}" \
        -test.cpuprofile "$out/cpu.pprof" \
        -test.memprofile "$out/heap.pprof" \
        -test.blockprofile "$out/block.pprof" \
        -test.mutexprofile "$out/mutex.pprof" >"$out/bench.txt" 2>&1

      # The go-native RDMA cgo baselines (GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md
      # R0-T05): simulated input, so no RDMA device is needed.
      set +e
      ${rdmaNativeBench}/bin/bench-linkmonitor-rdma-native --output "$out/rdma-native" >"$out/rdma-native.log" 2>&1
      rdma_native_rc=$?
      set -e

      # Stop after the benchmark traffic has drained, then include the pcap
      # and tcpdump transcript in one host-readable evidence bundle.
      kill "$capture_pid" 2>/dev/null || true
      wait "$capture_pid" 2>/dev/null || true
      trap - EXIT
      ip link del nlmon0 2>/dev/null || true
      {
        echo "live_exit=$live_rc"
        echo "rdma_native_exit=$rdma_native_rc"
        echo "kernel=$(uname -r)"
        echo "arch=$(uname -m)"
      } > "$out/manifest.txt"
      tar -C /tmp -czf /tmp/linkmonitor-performance.tgz linkmonitor-performance
      echo LINKMONITOR_PERF_BUNDLE_BEGIN
      base64 -w0 /tmp/linkmonitor-performance.tgz
      echo
      echo LINKMONITOR_PERF_BUNDLE_END
      echo LINKMONITOR_PERF_DONE
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
          networking.hostName = "linkmonitor-performance";
          networking.useDHCP = false;
          boot.kernelModules = [ "nlmon" ];
          virtualisation = {
            graphics = false;
            memorySize = 4096;
            cores = 4;
            diskImage = null;
            qemu.networkingOptions = lib.mkForce [ ];
            qemu.options = [ "-no-reboot" ];
          };
          environment.systemPackages = [
            artifact
            guest
          ];
          systemd.services.linkmonitor-performance = {
            wantedBy = [ "multi-user.target" ];
            after = [ "systemd-modules-load.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guest}/bin/linkmonitor-performance-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
in
pkgs.writeShellApplication {
  name = "test-linkmonitor-performance-vm";
  runtimeInputs = with pkgs; [
    coreutils
    gnugrep
    gnutar
    gzip
    netcat-gnu
    util-linux
  ];
  text = ''
    set -euo pipefail
    output="''${1:-$(mktemp -d -t linkmonitor-performance-vm-XXXXXX)}"
    mkdir -p "$output"
    log="$output/serial.log"
    echo "Evidence: $output"
    export TMPDIR="$output"
    export QEMU_OPTS="-machine accel=''${QEMU_ACCEL:-kvm:tcg}"
    timeout --kill-after=15s "''${LINKMONITOR_VM_TIMEOUT:-20m}" \
      ${vm}/bin/run-linkmonitor-performance-vm >"$log" 2>&1 || {
        cat "$log"
        exit 1
      }
    cat "$log"
    grep -q LINKMONITOR_PERF_DONE "$log"
    sed -n '/LINKMONITOR_PERF_BUNDLE_BEGIN/,/LINKMONITOR_PERF_BUNDLE_END/{
      /LINKMONITOR_PERF_BUNDLE_BEGIN/d
      /LINKMONITOR_PERF_BUNDLE_END/d
      p
    }' "$log" \
      | sed -E 's/^.*]: //' \
      | tr -cd 'A-Za-z0-9+/=' | base64 -d | tar -xzf - -C "$output"
    grep -q '^live_exit=0$' "$output/linkmonitor-performance/manifest.txt" || {
      echo "live tests failed; see $output/linkmonitor-performance/live-tests.txt" >&2
      exit 1
    }
    grep -q '^rdma_native_exit=0$' "$output/linkmonitor-performance/manifest.txt" || {
      echo "RDMA native benchmarks failed; see $output/linkmonitor-performance/rdma-native.log" >&2
      exit 1
    }
    echo "PASS: privileged link-monitor VM evidence in $output/linkmonitor-performance"
  '';
}
