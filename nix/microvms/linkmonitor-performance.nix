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

      # A step that fails outside the tolerated ones below stops the run
      # with a line the host fails on, rather than at the host's timeout.
      trap 'echo LINKMONITOR_PERF_FAIL; systemctl poweroff --no-block' ERR
      echo LINKMONITOR_PERF_BOOT
      modprobe nlmon 2>/dev/null || true
      ip link add nlmon0 type nlmon
      ip link set nlmon0 up
      tcpdump --immediate-mode -i nlmon0 -w "$out/netlink.pcap" 'ether[14:2]==0' >/tmp/linkmonitor-tcpdump.log 2>&1 &
      capture_pid=$!
      trap 'kill "$capture_pid" 2>/dev/null || true; wait "$capture_pid" 2>/dev/null || true' EXIT

      # Run the root-only live tests first. A failure remains visible in the
      # transcript, while the benchmark/profile phase still produces evidence.
      live_rc=0
      "${artifact}/bin/core.test" -test.run 'Test(IdentityLiveReadOnly|SettingsLiveReadOnly|StatisticLiveReadOnly|TrafficReadOnlyKernel)$' -test.v >"$out/live-tests.txt" 2>&1 || live_rc=$?
      printf '%s\n' "$live_rc" > "$out/live-exit.txt"

      "${artifact}/bin/core.test" -test.run '^$' \
        -test.bench '^BenchmarkPerformance(LocalMachine|RealisticFleet)$' \
        -test.benchmem -test.benchtime="''${LINKMONITOR_BENCHTIME:-5s}" \
        -test.cpuprofile "$out/cpu.pprof" \
        -test.memprofile "$out/heap.pprof" \
        -test.blockprofile "$out/block.pprof" \
        -test.mutexprofile "$out/mutex.pprof" >"$out/bench.txt" 2>&1

      # The go-native RDMA cgo baselines (GO-NATIVE-RDMA-IMPLEMENTATION-PLAN.md
      # R0-T05) and the native decoders beside them (R1-T03, R1-T06):
      # simulated input, so no RDMA device is needed.
      rdma_native_rc=0
      ${rdmaNativeBench}/bin/bench-linkmonitor-rdma-native --native --output "$out/rdma-native" >"$out/rdma-native.log" 2>&1 || rdma_native_rc=$?

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
  # The bundle reaches the console through journald, so systemd's shutdown
  # status lines can land between its lines; keep only the guest's own.
  sentinels = [
    "LINKMONITOR_PERF_BOOT"
    "LINKMONITOR_PERF_BUNDLE_END"
    "LINKMONITOR_PERF_DONE"
  ];
  failPattern = "LINKMONITOR_PERF_FAIL";
  bundlePostCheck = ''
    if ! sed -n '/LINKMONITOR_PERF_BUNDLE_BEGIN/,/LINKMONITOR_PERF_BUNDLE_END/{
      /LINKMONITOR_PERF_BUNDLE_BEGIN/d
      /LINKMONITOR_PERF_BUNDLE_END/d
      /linkmonitor-performance-guest\[[0-9]*\]: /p
    }' "$run_dir/serial.log" |
      sed -E 's/^.*]: //' | tr -cd 'A-Za-z0-9+/=' | base64 -d |
      ${pkgs.gzip}/bin/gzip -dc | ${pkgs.gnutar}/bin/tar -x -C "$run_dir"; then
      fail 'the evidence bundle does not unpack'
    fi
    manifest="$run_dir/linkmonitor-performance/manifest.txt"
    if ! grep -qx 'live_exit=0' "$manifest"; then
      fail 'live tests failed; see linkmonitor-performance/live-tests.txt'
    fi
    if ! grep -qx 'rdma_native_exit=0' "$manifest"; then
      fail 'RDMA native benchmarks failed; see linkmonitor-performance/rdma-native.log'
    fi
  '';
  # checks.vm-linkmonitor-performance proves the guest's live tests pass and
  # every benchmark and profile completes; its timings are evidence of that
  # build, not a fresh measurement. For a measurement, run the app on a quiet
  # host: test-linkmonitor-performance-vm [--accel=…] [--keep=N] [DIR]
  # (DIR, if given, instead of ~/.cache/xtcp2-vm), then read or promote
  # DIR/linkmonitor-performance.
  vmCheck = import ./mkVmCheck.nix { inherit pkgs lib; } {
    name = "linkmonitor-performance";
    launch = "${vm}/bin/run-linkmonitor-performance-vm";
    inherit sentinels failPattern;
    timeout = "20m";
    evidence = {
      guest = vm;
      inherit artifact;
    };
    postCheck = bundlePostCheck;
  };
  # checks.vm-linkmonitor-performance-bundle: bundlePostCheck through
  # mkVmCheck's runner, with shell stand-ins for the guest (no VM).
  bundleTest =
    let
      # emit LIVE RDMA [half]: a manifest bundle in the guest's journald
      # form, split across lines with systemd status lines between them.
      emit = ''
        emit() {
          d=$(mktemp -d)
          mkdir "$d/linkmonitor-performance"
          printf 'live_exit=%s\nrdma_native_exit=%s\n' "$1" "$2" > "$d/linkmonitor-performance/manifest.txt"
          tar -C "$d" -czf - linkmonitor-performance | base64 -w0 > "$d/b64"
          if [ "''${3:-}" = half ]; then head -c 20 "$d/b64" > "$d/b64.half"; mv "$d/b64.half" "$d/b64"; fi
          echo '[    1.0] linkmonitor-performance-guest[42]: LINKMONITOR_PERF_BOOT'
          echo '[    1.1] linkmonitor-performance-guest[42]: LINKMONITOR_PERF_BUNDLE_BEGIN'
          fold -w 16 "$d/b64" | while read -r chunk || [ -n "$chunk" ]; do
            echo "[    1.2] linkmonitor-performance-guest[42]: $chunk"
            echo '[  OK  ] Stopped target Timers.'
          done
          echo '[    1.3] linkmonitor-performance-guest[42]: LINKMONITOR_PERF_BUNDLE_END'
          echo '[    1.4] linkmonitor-performance-guest[42]: LINKMONITOR_PERF_DONE'
        }
      '';
      rows = [
        {
          id = "pass";
          category = "positive";
          description = "a bundle whose manifest has both exits 0 passes and is unpacked";
          launch = "emit 0 0";
          want = "PASS: t-perf-pass";
          wantRc = 0;
        }
        {
          id = "live";
          category = "negative";
          description = "a failed live test in the manifest fails the run";
          launch = "emit 1 0";
          want = "live tests failed";
          wantRc = 1;
        }
        {
          id = "rdma";
          category = "negative";
          description = "a failed RDMA native benchmark in the manifest fails the run";
          launch = "emit 0 3";
          want = "RDMA native benchmarks failed";
          wantRc = 1;
        }
        {
          id = "nobundle";
          category = "boundary";
          description = "a guest that finishes without a bundle fails";
          launch = "echo LINKMONITOR_PERF_BOOT; echo LINKMONITOR_PERF_BUNDLE_END; echo LINKMONITOR_PERF_DONE";
          want = "the evidence bundle does not unpack";
          wantRc = 1;
        }
        {
          id = "truncated";
          category = "corner";
          description = "a bundle cut short on the console fails";
          launch = "emit 0 0 half";
          want = "the evidence bundle does not unpack";
          wantRc = 1;
        }
      ];
      runnerOf =
        row:
        (import ./mkVmCheck.nix { inherit pkgs lib; } {
          name = "t-perf-${row.id}";
          runtimeInputs = with pkgs; [
            coreutils
            gnutar
            gzip
          ];
          launch = emit + row.launch;
          inherit sentinels failPattern;
          postCheck = bundlePostCheck;
          timeout = "20s";
        }).runner;
    in
    pkgs.runCommand "xtcp2-vm-linkmonitor-performance-bundle"
      {
        nativeBuildInputs = with pkgs; [
          coreutils
          gnugrep
        ];
      }
      ''
        fails=0
        row() {
          local cat=$1 desc=$2 runner=$3 want_rc=$4 want=$5 dir rc=0
          dir=$(mktemp -d)/evidence
          "$runner" "$dir" kvm > "$dir.out" 2>&1 || rc=$?
          if [ "$rc" -ne "$want_rc" ] || ! grep -qF -- "$want" "$dir.out"; then
            echo "FAIL $cat: $desc: rc=$rc (want $want_rc, '$want')"; cat "$dir.out"; fails=$((fails + 1))
          elif [ "$want_rc" -eq 0 ] && ! grep -qx live_exit=0 "$dir/linkmonitor-performance/manifest.txt"; then
            echo "FAIL $cat: $desc: no unpacked manifest"; fails=$((fails + 1))
          else
            echo "ok   $cat: $desc"
          fi
        }
        ${lib.concatMapStrings (row: ''
          row ${
            lib.escapeShellArgs [
              row.category
              row.description
              "${runnerOf row}/bin/vm-run-t-perf-${row.id}"
              (toString row.wantRc)
              row.want
            ]
          }
        '') rows}
        if [ "$fails" -ne 0 ]; then
          echo "$fails row(s) failed"
          exit 1
        fi
        touch "$out"
      '';
  usage = "usage: test-linkmonitor-performance-vm [--accel=auto|kvm|tcg] [--keep=N] [DIR]";
in
pkgs.writeShellApplication {
  name = "test-linkmonitor-performance-vm";
  runtimeInputs = [ pkgs.coreutils ];
  text = ''
    accel=auto
    dir=""
    flags=()
    for arg in "$@"; do
      case "$arg" in
        --accel=auto | --accel=kvm | --accel=tcg)
          accel=''${arg#--accel=}
          flags+=("$arg")
          ;;
        --keep=*) flags+=("$arg") ;;
        -*) echo '${usage}' >&2; exit 2 ;;
        *)
          if [ -n "$dir" ]; then echo '${usage}' >&2; exit 2; fi
          dir=$arg
          ;;
      esac
    done
    if [ -z "$dir" ]; then
      exec ${vmCheck.app}/bin/test-linkmonitor-performance-vm "''${flags[@]}"
    fi
    rc=0
    ${vmCheck.runner}/bin/vm-run-linkmonitor-performance "$dir" "$accel" || rc=$?
    if [ "$rc" -ne 0 ] && [ -s "$dir/serial.log" ]; then
      echo '--- the last 40 lines of serial.log ---'
      tail -n 40 "$dir/serial.log"
    fi
    echo "evidence: $dir"
    exit "$rc"
  '';
}
// {
  inherit vmCheck bundleTest;
}
