# The /ip paths on a loopback listener of their own (-linkMonitorIPListen):
# the real xtcp2 unit from nix/modules/xtcp2-service.nix in a disposable
# guest, with raw listener auth on the Prometheus listener and /ip moved to
# 127.0.0.1:9089. The guest checks, in order, each with a sentinel:
#   SPLIT  /ip and /i answer on 9089 without a token; 9089 serves nothing
#          else; 9088 still needs the token and no longer serves /ip
#   BIND   9089 is bound on 127.0.0.1 only
#   PEER   a neighbour on an interface with route_localnet set, as Docker
#          sets on its bridges with the userland proxy off, reaches a plain
#          loopback listener (the control) but is closed by xtcp2's, which
#          logs it
# The PEER neighbour is a network namespace on a veth pair; its own loopback
# stays down, so 127.0.0.1 routes out of the veth to the guest.
# checks.vm-xtcp2-ip-listen runs it in the build sandbox under KVM.
{
  pkgs,
  lib,
  nixpkgs,
  xtcp2,
}:
let
  prom = "http://127.0.0.1:9088";
  ipUrl = "http://127.0.0.1:9089";
  readToken = "/etc/xtcp2/listener_read_token";
  mintToken = pkgs.writeShellApplication {
    name = "xtcp2-ip-listen-token";
    runtimeInputs = with pkgs; [ coreutils ];
    text = ''
      install -d -m 0750 -o root -g root ${builtins.dirOf readToken}
      umask 077
      head -c 32 /dev/urandom | base64 | tr -d '\n=' >${readToken}.new
      mv ${readToken}.new ${readToken}
    '';
  };
  guestTest = pkgs.writeShellApplication {
    name = "xtcp2-ip-listen-guest";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      gawk
      gnugrep
      iproute2
      procps
      python3
      systemd
    ];
    text = ''
      trap 'echo XTCP2_IP_LISTEN_FAIL; journalctl -u xtcp2 --no-pager | tail -n 80; systemctl poweroff --no-block' ERR
      token=$(cat ${readToken})
      # code URL [CURL ARGS...]: the HTTP status, 000 when there is none.
      code() {
        local url=$1
        shift
        curl -s -m 5 -o /dev/null -w '%{http_code}' "$@" "$url" || true
      }
      # row URL WANT [CURL ARGS...]
      row() {
        local url=$1 want=$2
        shift 2
        local got
        got=$(code "$url" "$@")
        if [ "$got" != "$want" ]; then
          echo "XTCP2_IP_LISTEN_ROW_FAIL GET $url = $got, want $want"
          false
        fi
      }
      until_ok() {
        local deadline=$((SECONDS + $1))
        shift
        until "$@"; do
          if [ "$SECONDS" -ge "$deadline" ]; then
            echo "XTCP2_IP_LISTEN_ROW_FAIL timed out: $*"
            return 1
          fi
          sleep 1
        done
      }
      ready() { [ "$(code ${prom}/readyz -H "Authorization: Bearer $token")" = 200 ]; }
      ip_up() { [ "$(code ${ipUrl}/ip/link)" = 200 ]; }

      until_ok 90 ready
      until_ok 30 ip_up
      # SPLIT
      row ${ipUrl}/ip/link 200
      row ${ipUrl}/i/a/s 200
      body=$(curl -sf ${ipUrl}/i/a/s)
      grep -q '127.0.0.1' <<<"$body"
      row ${ipUrl}/metrics 404
      row ${ipUrl}/readyz 404
      row ${ipUrl}/metrics/link 404
      row ${prom}/metrics 401
      row ${prom}/ip/link 401
      row ${prom}/metrics 200 -H "Authorization: Bearer $token"
      row ${prom}/ip/link 404 -H "Authorization: Bearer $token"
      row ${prom}/i/l 404 -H "Authorization: Bearer $token"
      row ${prom}/metrics/link 200 -H "Authorization: Bearer $token"
      echo XTCP2_IP_LISTEN_SPLIT_DONE

      # BIND
      bound=$(ss -Hltn 'sport = :9089' | awk '{print $4}' | sort -u)
      echo "9089 bound on: $bound"
      [ "$bound" = 127.0.0.1:9089 ]
      echo XTCP2_IP_LISTEN_BIND_DONE

      # PEER
      ip netns add pod
      ip link add vhost type veth peer name vpod
      ip link set vpod netns pod
      ip addr add 10.77.0.1/24 dev vhost
      ip link set vhost up
      sysctl -qw net.ipv4.conf.vhost.route_localnet=1
      ip -n pod addr add 10.77.0.2/24 dev vpod
      ip -n pod link set vpod up
      ip netns exec pod sysctl -qw net.ipv4.conf.vpod.route_localnet=1
      ip -n pod route add 127.0.0.0/8 via 10.77.0.1 dev vpod
      mkdir -p /run/control
      echo control >/run/control/index.html
      python3 -m http.server --bind 127.0.0.1 --directory /run/control 9099 &
      control=$!
      control_up() { [ "$(code http://127.0.0.1:9099/)" = 200 ]; }
      until_ok 30 control_up
      pod_code() { ip netns exec pod curl -s -m 5 -o /dev/null -w '%{http_code}' "$1" || true; }
      reached=$(pod_code http://127.0.0.1:9099/)
      echo "the neighbour reaches the control loopback listener: $reached"
      if [ "$reached" != 200 ]; then
        echo "XTCP2_IP_LISTEN_ROW_FAIL the route_localnet path does not reach loopback; PEER would prove nothing"
        false
      fi
      got=$(pod_code ${ipUrl}/ip/link)
      echo "the neighbour on xtcp2's /ip listener: $got"
      if [ "$got" != 000 ]; then
        echo "XTCP2_IP_LISTEN_ROW_FAIL a non-loopback peer got $got from /ip"
        false
      fi
      logged() { journalctl -u xtcp2 --no-pager | grep -q 'ip listener: closed .* from non-loopback peers, last from 10.77.0.2:'; }
      until_ok 10 logged
      kill "$control"
      # Loopback still works after the rejects.
      row ${ipUrl}/ip/link 200
      echo XTCP2_IP_LISTEN_PEER_DONE

      echo XTCP2_IP_LISTEN_PASS
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
          networking.hostName = "xtcp2-ip-listen";
          networking.useDHCP = false;
          networking.usePredictableInterfaceNames = false;
          # The firewall's reverse path check would drop the neighbour's
          # packets before xtcp2 sees them.
          networking.firewall.enable = false;
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
          systemd.services.xtcp2-ip-listen-token = {
            requiredBy = [ "xtcp2.service" ];
            before = [ "xtcp2.service" ];
            serviceConfig = {
              Type = "oneshot";
              RemainAfterExit = true;
              ExecStart = "${mintToken}/bin/xtcp2-ip-listen-token";
            };
          };
          services.xtcp2 = {
            enable = true;
            package = xtcp2;
            configFile = pkgs.writeText "xtcp2-ip-listen.json" "{}";
            extraArgs = [
              "-dest"
              "null"
            ];
            linkMonitor = {
              ipPaths = true;
              ipListen = "127.0.0.1:9089";
              extraArgs = [ "-linkMonitorSettle=2s" ];
            };
          };
          systemd.services.xtcp2.environment = {
            LISTENER_AUTH_MODE = "raw";
            LISTENER_RAW_TOKEN_FILE = readToken;
          };
          systemd.services.xtcp2-ip-listen = {
            wantedBy = [ "multi-user.target" ];
            after = [ "xtcp2.service" ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/xtcp2-ip-listen-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
  sentinels = map (s: "XTCP2_IP_LISTEN_${s}") [
    "SPLIT_DONE"
    "BIND_DONE"
    "PEER_DONE"
    "PASS"
  ];
  # checks.vm-xtcp2-ip-listen and test-xtcp2-ip-listen-vm
  # (nix/microvms/mkVmCheck.nix).
  vmCheck = import ../microvms/mkVmCheck.nix { inherit pkgs lib; } {
    name = "xtcp2-ip-listen";
    launch = "${vm}/bin/run-xtcp2-ip-listen-vm";
    inherit sentinels;
    failPattern = "XTCP2_IP_LISTEN_(ROW_)?FAIL";
    evidence.guest = vm;
  };
in
# test-xtcp2-ip-listen-vm [--accel=auto|kvm|tcg] [--keep=N]: evidence under
# ~/.cache/xtcp2-vm/xtcp2-ip-listen; checks.vm-xtcp2-ip-listen is the same run in the build sandbox.
vmCheck.app
// {
  inherit vmCheck;
}
