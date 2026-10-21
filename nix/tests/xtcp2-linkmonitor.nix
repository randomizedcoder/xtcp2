# The link monitor inside xtcp2 (docs/design-linkmonitor-in-xtcp2.md): the
# real xtcp2 unit from nix/modules/xtcp2-service.nix in a disposable guest,
# with the monitor on by default. The guest checks, in order, each with a
# sentinel:
#   PATHS     /metrics carries the summary, the detail paths serve, /ip is off
#   BASELINE  the baseline file appears 0600 in a 0750 /var/lib/xtcp2
#   PERSIST   a restart reloads the count instead of relearning it
#   RELOAD    `systemctl reload xtcp2` (SIGUSR1) records a new baseline
#   MISSING   taking the NIC down shows on /metrics as up_links_delta -1, and
#             bringing it back returns the delta to 0
#   CORRUPT   a corrupt baseline stops only the monitor: xtcp2 stays ready,
#             the up gauge is 0, the detail paths are 503, the file is kept
#   STOP      SIGTERM stops xtcp2 cleanly within 5s
# The guest's one NIC is a virtio-net on a restricted QEMU user network: an
# eligible physical link for the monitor, with no route off the guest.
# checks.vm-xtcp2-linkmonitor runs it in the build sandbox under KVM.
{
  pkgs,
  lib,
  nixpkgs,
  xtcp2,
}:
let
  url = "http://127.0.0.1:9088";
  baseline = "/var/lib/xtcp2/link-monitor-baseline.json";
  guestTest = pkgs.writeShellApplication {
    name = "xtcp2-linkmonitor-guest";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      gnugrep
      iproute2
      jq
      systemd
    ];
    text = ''
      trap 'echo XTCP2_LINKMONITOR_FAIL; journalctl -u xtcp2 --no-pager | tail -n 80; systemctl poweroff --no-block' ERR
      code() { curl -s -o /dev/null -w '%{http_code}' "${url}$1"; }
      expect() {
        local got
        got=$(code "$1")
        if [ "$got" != "$2" ]; then
          echo "XTCP2_LINKMONITOR_ROW_FAIL GET $1 = $got, want $2"
          false
        fi
      }
      # until SECONDS COMMAND...: retry COMMAND once a second.
      until_ok() {
        local deadline=$((SECONDS + $1))
        shift
        until "$@"; do
          if [ "$SECONDS" -ge "$deadline" ]; then
            echo "XTCP2_LINKMONITOR_ROW_FAIL timed out: $*"
            return 1
          fi
          sleep 1
        done
      }
      ready() { [ "$(code /readyz)" = 200 ]; }
      # The body is read in full first: grep -q exiting early would fail curl
      # under pipefail.
      scrape() { curl -sf "${url}$1"; }
      metric() {
        local body
        body=$(scrape /metrics) || return 1
        grep -q -- "$1" <<<"$body"
      }
      recorded_at() { jq -r .recorded_at ${baseline}; }
      delta_is() { metric "^go_link_monitor_up_links_delta $1\$"; }

      until_ok 90 ready
      # PATHS
      until_ok 30 metric '^go_link_monitor_problems '
      metric '^go_link_monitor_collection_mode{mode="on-demand"} 1'
      metric '^xtcp_gauges{function="linkMonitor",type="gauge",variable="up"} 1'
      metric '^xtcp_counts'
      if metric '^go_link_monitor_link_'; then
        echo "XTCP2_LINKMONITOR_ROW_FAIL detail families on /metrics"
        false
      fi
      for path in /metrics/link /metrics/traffic /metrics/rdma /metrics/collection /metrics/link/eth0 /metrics-all /metrics-all/eth0; do
        expect "$path" 200
      done
      expect /metrics/link/nosuch0 404
      expect /ip/link 404
      expect /i/l 404
      collection=$(scrape /metrics/collection)
      grep -q '^go_link_monitor_collector_' <<<"$collection"
      # The detail scrapes above refreshed on demand, and /metrics counts them.
      metric '^go_link_monitor_on_demand_refreshes_total{result='
      echo XTCP2_LINKMONITOR_PATHS_DONE

      # BASELINE
      until_ok 60 metric '^go_link_monitor_baseline_ready 1'
      test -f ${baseline}
      [ "$(stat -c %a /var/lib/xtcp2)" = 750 ]
      [ "$(stat -c %a ${baseline})" = 600 ]
      count=$(jq -r .expected_up_links ${baseline})
      first=$(recorded_at)
      echo "baseline expected_up_links=$count recorded_at=$first"
      # eth0 is the guest's one eligible link.
      [ "$count" = 1 ]
      delta_is 0
      echo XTCP2_LINKMONITOR_BASELINE_DONE

      # PERSIST: past the settle interval, a relearned baseline would carry a
      # new recorded_at.
      systemctl restart xtcp2
      until_ok 90 ready
      until_ok 30 metric '^go_link_monitor_baseline_ready 1'
      sleep 5
      [ "$(recorded_at)" = "$first" ]
      [ "$(jq -r .expected_up_links ${baseline})" = "$count" ]
      echo XTCP2_LINKMONITOR_PERSIST_DONE

      # RELOAD
      systemctl reload xtcp2
      changed() { [ "$(recorded_at)" != "$first" ]; }
      until_ok 60 changed
      systemctl is-active --quiet xtcp2
      echo XTCP2_LINKMONITOR_RELOAD_DONE

      # MISSING
      down_at=$SECONDS
      ip link set eth0 down
      until_ok 90 delta_is -1
      problems=$(scrape /metrics | grep '^go_link_monitor_problems ' | cut -d' ' -f2)
      [ "$problems" -ge 1 ]
      echo "link down seen after $((SECONDS - down_at))s, problems=$problems"
      ip link set eth0 up
      until_ok 90 delta_is 0
      [ "$(jq -r .expected_up_links ${baseline})" = 1 ]
      echo XTCP2_LINKMONITOR_MISSING_DONE

      # CORRUPT
      systemctl stop xtcp2
      printf '{bad' > ${baseline}
      systemctl start xtcp2
      until_ok 90 ready
      until_ok 30 metric '^xtcp_gauges{function="linkMonitor",type="gauge",variable="up"} 0'
      metric '^xtcp_counts{function="linkMonitor",type="error",variable="failed"} 1'
      expect /metrics/link 503
      expect /metrics-all 503
      expect /metrics 200
      [ "$(cat ${baseline})" = '{bad' ]
      systemctl is-active --quiet xtcp2
      echo XTCP2_LINKMONITOR_CORRUPT_DONE

      # STOP
      start=$SECONDS
      systemctl stop xtcp2
      elapsed=$((SECONDS - start))
      [ "$elapsed" -le 5 ]
      [ "$(systemctl show -p Result --value xtcp2)" = success ]
      echo "stop took ''${elapsed}s"
      echo XTCP2_LINKMONITOR_STOP_DONE

      echo XTCP2_LINKMONITOR_PASS
      systemctl poweroff --no-block
    '';
  };
  vm =
    (nixpkgs.lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
        ../modules/xtcp2-service.nix
        {
          nixpkgs.pkgs = pkgs;
          system.stateVersion = "26.05";
          networking.hostName = "xtcp2-linkmonitor";
          networking.useDHCP = false;
          networking.usePredictableInterfaceNames = false;
          virtualisation = {
            graphics = false;
            memorySize = 2048;
            cores = 2;
            diskImage = null;
            # restrict=on: the guest reaches neither the host nor beyond.
            qemu.networkingOptions = lib.mkForce [
              "-netdev user,id=n0,restrict=on"
              "-device virtio-net-pci,netdev=n0"
            ];
            qemu.options = [ "-no-reboot" ];
          };
          services.xtcp2 = {
            enable = true;
            package = xtcp2;
            configFile = pkgs.writeText "xtcp2-linkmonitor.json" "{}";
            extraArgs = [
              "-dest"
              "null"
            ];
            # A short settle keeps the guest quick; everything else is the
            # module's default, which is what this check is about.
            linkMonitor.extraArgs = [ "-linkMonitorSettle=2s" ];
          };
          # eth0 is up before xtcp2 starts, so the first baseline counts it.
          systemd.services.guest-nic-up = {
            requiredBy = [ "xtcp2.service" ];
            before = [ "xtcp2.service" ];
            path = [ pkgs.iproute2 ];
            serviceConfig = {
              Type = "oneshot";
              RemainAfterExit = true;
            };
            script = ''
              ip link set eth0 up
              for _ in $(seq 50); do
                [ "$(cat /sys/class/net/eth0/operstate)" = up ] && exit 0
                sleep 0.2
              done
              echo "eth0 did not come up" >&2
              exit 1
            '';
          };
          systemd.services.xtcp2-linkmonitor = {
            wantedBy = [ "multi-user.target" ];
            after = [ "xtcp2.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/xtcp2-linkmonitor-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
  sentinels = map (s: "XTCP2_LINKMONITOR_${s}") [
    "PATHS_DONE"
    "BASELINE_DONE"
    "PERSIST_DONE"
    "RELOAD_DONE"
    "MISSING_DONE"
    "CORRUPT_DONE"
    "STOP_DONE"
    "PASS"
  ];
  # checks.vm-xtcp2-linkmonitor and test-xtcp2-linkmonitor-vm
  # (nix/microvms/mkVmCheck.nix).
  vmCheck = import ../microvms/mkVmCheck.nix { inherit pkgs lib; } {
    name = "xtcp2-linkmonitor";
    launch = "${vm}/bin/run-xtcp2-linkmonitor-vm";
    inherit sentinels;
    failPattern = "XTCP2_LINKMONITOR_(ROW_)?FAIL";
    evidence.guest = vm;
  };
in
# test-xtcp2-linkmonitor-vm [--accel=auto|kvm|tcg] [--keep=N]: evidence under
# ~/.cache/xtcp2-vm/xtcp2-linkmonitor; checks.vm-xtcp2-linkmonitor is the same run in the build sandbox.
vmCheck.app
// {
  inherit vmCheck;
}
