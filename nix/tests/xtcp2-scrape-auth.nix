# vmagent scraping xtcp2 through listener auth, the way a fleet host does
# (docs/observability.md#scraping-with-listener-auth): xtcp2 on TCP :9088
# with a raw read token and a separate raw admin token, both from files, and
# nixpkgs' vmagent with authorization.credentials_file. The guest checks, in
# order, each with a sentinel:
#   TOKENS       per-boot random tokens: xtcp2's copies 0400 root, vmagent's
#                copy 0440 to a group its DynamicUser joins
#   HTTP         a table of curl rows: no token, a wrong token and an admin
#                token used as Basic are 401 on every path; the read token
#                reads /metrics and the link monitor paths and gets 403 on
#                pprof; the admin token reaches pprof
#   VMAGENT      the credentials_file job is up; a job with no token and one
#                with a wrong token are down with 401
#   HEALTHCHECK  `xtcp2 -healthcheck` with the unit's env exits 0, with no
#                token env or a wrong token it exits 1
#   ROTATE       new tokens written for both: vmagent's job goes down against
#                the old xtcp2 within seconds, then up after `systemctl
#                restart xtcp2`, with vmagent's MainPID unchanged; the old
#                read token is then 401
# The guest's one NIC is a virtio-net on a restricted QEMU user network.
# checks.vm-xtcp2-scrape-auth runs it in the build sandbox under KVM.
{
  pkgs,
  lib,
  nixpkgs,
  xtcp2,
}:
let
  url = "http://127.0.0.1:9088";
  vmagentApi = "http://127.0.0.1:8429";
  tokenDir = "/etc/xtcp2";
  readToken = "${tokenDir}/listener_read_token";
  adminToken = "${tokenDir}/listener_admin_token";
  # vmagent's own copy, as ansible-host puts one under /opt/vmagent.
  vmagentToken = "/etc/vmagent/xtcp2_read_token";
  scrapeGroup = "xtcp2scrape";
  listenerEnv = {
    LISTENER_AUTH_MODE = "raw";
    LISTENER_RAW_TOKEN_FILE = readToken;
    LISTENER_ADMIN_AUTH_MODE = "raw";
    LISTENER_ADMIN_RAW_TOKEN_FILE = adminToken;
  };
  # mint-tokens: fresh random read and admin tokens, written for xtcp2 and
  # vmagent. vmagent's copy ends in a newline, as a hand-written file would;
  # xtcp2's do not.
  mintTokens = pkgs.writeShellApplication {
    name = "xtcp2-mint-tokens";
    runtimeInputs = with pkgs; [ coreutils ];
    text = ''
      token() { head -c 32 /dev/urandom | base64 | tr -d '\n='; }
      read=$(token)
      admin=$(token)
      install -d -m 0750 -o root -g root ${tokenDir}
      install -d -m 0750 -o root -g ${scrapeGroup} ${builtins.dirOf vmagentToken}
      umask 077
      printf '%s' "$read" >${readToken}.new
      printf '%s' "$admin" >${adminToken}.new
      printf '%s\n' "$read" >${vmagentToken}.new
      chmod 0400 ${readToken}.new ${adminToken}.new
      chgrp ${scrapeGroup} ${vmagentToken}.new
      chmod 0440 ${vmagentToken}.new
      mv ${readToken}.new ${readToken}
      mv ${adminToken}.new ${adminToken}
      mv ${vmagentToken}.new ${vmagentToken}
    '';
  };
  guestTest = pkgs.writeShellApplication {
    name = "xtcp2-scrape-auth-guest";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      gnugrep
      jq
      systemd
      xtcp2
      mintTokens
    ];
    text = ''
      trap 'echo XTCP2_SCRAPE_AUTH_FAIL; journalctl -u xtcp2 -u vmagent --no-pager | tail -n 80; systemctl poweroff --no-block' ERR
      # until SECONDS COMMAND...: retry COMMAND once a second.
      until_ok() {
        local deadline=$((SECONDS + $1))
        shift
        until "$@"; do
          if [ "$SECONDS" -ge "$deadline" ]; then
            echo "XTCP2_SCRAPE_AUTH_ROW_FAIL timed out: $*"
            return 1
          fi
          sleep 1
        done
      }
      # code PATH [CURL ARGS...]: the HTTP status of GET PATH.
      code() {
        local path=$1
        shift
        curl -s -o /dev/null -w '%{http_code}' "$@" "${url}$path"
      }
      bearer() { printf 'Authorization: Bearer %s' "$(cat "$1")"; }
      # row CATEGORY DESCRIPTION EXPECTED PATH [CURL ARGS...]
      row() {
        local category=$1 description=$2 expected=$3 path=$4 got
        shift 4
        got=$(code "$path" "$@")
        if [ "$got" != "$expected" ]; then
          echo "XTCP2_SCRAPE_AUTH_ROW_FAIL $category: $description: GET $path = $got, expected $expected"
          return 1
        fi
        echo "ok $category: $description: GET $path = $got"
      }
      ready() { [ "$(code /readyz -H "$(bearer ${readToken})")" = 200 ]; }
      # job_field JOB FIELD: FIELD of the vmagent target in job JOB.
      job_field() {
        local body
        body=$(curl -sf "${vmagentApi}/api/v1/targets") || return 1
        jq -r --arg job "$1" --arg field "$2" \
          '.data.activeTargets[] | select(.labels.job == $job) | .[$field]' <<<"$body"
      }
      job_health() { [ "$(job_field "$1" health)" = "$2" ]; }
      job_error_has() { grep -q -- "$2" <<<"$(job_field "$1" lastError)"; }

      # TOKENS
      [ "$(stat -c '%a %U %G' ${readToken})" = '400 root root' ]
      [ "$(stat -c '%a %U %G' ${adminToken})" = '400 root root' ]
      [ "$(stat -c '%a %U %G' ${vmagentToken})" = '440 root ${scrapeGroup}' ]
      [ "$(stat -c %s ${readToken})" -ge 40 ]
      if cmp -s ${readToken} ${adminToken}; then
        echo "XTCP2_SCRAPE_AUTH_ROW_FAIL read and admin tokens are equal"
        false
      fi
      until_ok 90 ready
      echo XTCP2_SCRAPE_AUTH_TOKENS_DONE

      # HTTP
      read_h=$(bearer ${readToken})
      admin_h=$(bearer ${adminToken})
      row positive "the read token scrapes /metrics" 200 /metrics -H "$read_h"
      row positive "the read token reads a link monitor detail path" 200 /metrics/link -H "$read_h"
      row positive "the read token reads /readyz" 200 /readyz -H "$read_h"
      row positive "the admin token reaches pprof" 200 /debug/pprof/ -H "$admin_h"
      row positive "the admin token also reads /metrics" 200 /metrics -H "$admin_h"
      row negative "no token on /metrics" 401 /metrics
      row negative "no token on /healthz: the whole listener is behind auth" 401 /healthz
      row negative "a wrong token" 401 /metrics -H 'Authorization: Bearer not-the-token'
      row negative "the read token on pprof is authenticated but not admin" 403 /debug/pprof/ -H "$read_h"
      row negative "the read token on a pprof profile" 403 '/debug/pprof/heap' -H "$read_h"
      row boundary "an empty bearer" 401 /metrics -H 'Authorization: Bearer '
      row boundary "the read token less its last character" 401 /metrics -H "''${read_h%?}"
      row corner "the read token as Basic, not Bearer" 401 /metrics -H "Authorization: Basic $(cat ${readToken})"
      row corner "a path that does not exist still needs a token first" 401 /nosuch
      echo XTCP2_SCRAPE_AUTH_HTTP_DONE

      # VMAGENT
      until_ok 60 job_health xtcp2 up
      until_ok 60 job_health xtcp2-noauth down
      until_ok 60 job_health xtcp2-wrong down
      job_error_has xtcp2-noauth 401
      job_error_has xtcp2-wrong 401
      echo "vmagent xtcp2 lastError: '$(job_field xtcp2 lastError)'"
      echo XTCP2_SCRAPE_AUTH_VMAGENT_DONE

      # HEALTHCHECK: the probe the container HEALTHCHECK runs, with the unit's env.
      hc() { env "$@" xtcp2 -healthcheck >/dev/null 2>&1; }
      hc ${lib.concatStringsSep " " (lib.mapAttrsToList (k: v: "${k}=${v}") listenerEnv)}
      if hc; then
        echo "XTCP2_SCRAPE_AUTH_ROW_FAIL -healthcheck with no token env exited 0"
        false
      fi
      wrong=$(mktemp)
      printf 'not-the-token' >"$wrong"
      if hc LISTENER_AUTH_MODE=raw LISTENER_RAW_TOKEN_FILE="$wrong"; then
        echo "XTCP2_SCRAPE_AUTH_ROW_FAIL -healthcheck with a wrong token exited 0"
        false
      fi
      rm -f "$wrong"
      echo XTCP2_SCRAPE_AUTH_HEALTHCHECK_DONE

      # ROTATE
      old_read_h=$read_h
      vmagent_pid=$(systemctl show -p MainPID --value vmagent)
      xtcp2-mint-tokens
      rotated_at=$SECONDS
      # vmagent re-reads credentials_file, so it sends the new token to the
      # xtcp2 still holding the old one.
      until_ok 30 job_health xtcp2 down
      job_error_has xtcp2 401
      echo "vmagent sent the new token after $((SECONDS - rotated_at))s"
      systemctl restart xtcp2
      until_ok 90 ready
      until_ok 60 job_health xtcp2 up
      [ "$(systemctl show -p MainPID --value vmagent)" = "$vmagent_pid" ]
      row negative "the old read token after rotation" 401 /metrics -H "$old_read_h"
      row positive "the new read token after rotation" 200 /metrics -H "$(bearer ${readToken})"
      echo XTCP2_SCRAPE_AUTH_ROTATE_DONE

      echo XTCP2_SCRAPE_AUTH_PASS
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
          networking.hostName = "xtcp2-scrape-auth";
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
          users.groups.${scrapeGroup} = { };
          systemd.services.xtcp2-tokens = {
            requiredBy = [
              "xtcp2.service"
              "vmagent.service"
            ];
            before = [
              "xtcp2.service"
              "vmagent.service"
            ];
            serviceConfig = {
              Type = "oneshot";
              RemainAfterExit = true;
              ExecStart = "${mintTokens}/bin/xtcp2-mint-tokens";
            };
          };
          services.xtcp2 = {
            enable = true;
            package = xtcp2;
            configFile = pkgs.writeText "xtcp2-scrape-auth.json" "{}";
            extraArgs = [
              "-dest"
              "null"
            ];
            linkMonitor.extraArgs = [ "-linkMonitorSettle=2s" ];
          };
          systemd.services.xtcp2.environment = listenerEnv;
          services.vmagent = {
            enable = true;
            # Nothing listens here: the check reads vmagent's targets API,
            # not stored samples.
            remoteWrite.url = "http://127.0.0.1:8428/api/v1/write";
            prometheusConfig = {
              global.scrape_interval = "2s";
              scrape_configs =
                let
                  job =
                    name: extra:
                    {
                      job_name = name;
                      metrics_path = "/metrics";
                      static_configs = [ { targets = [ "127.0.0.1:9088" ]; } ];
                    }
                    // extra;
                in
                [
                  (job "xtcp2" {
                    authorization = {
                      type = "Bearer";
                      credentials_file = vmagentToken;
                    };
                  })
                  (job "xtcp2-noauth" { })
                  (job "xtcp2-wrong" {
                    authorization = {
                      type = "Bearer";
                      credentials = "not-the-token";
                    };
                  })
                ];
            };
          };
          # vmagent is a DynamicUser; the group is how it reads its token copy.
          # LoadCredential would copy the file once at start and hide a
          # rotation, which is what ROTATE checks.
          systemd.services.vmagent.serviceConfig.SupplementaryGroups = [ scrapeGroup ];
          systemd.services.xtcp2-scrape-auth = {
            wantedBy = [ "multi-user.target" ];
            after = [
              "xtcp2.service"
              "vmagent.service"
            ];
            serviceConfig = {
              Type = "oneshot";
              ExecStart = "${guestTest}/bin/xtcp2-scrape-auth-guest";
              StandardOutput = "journal+console";
              StandardError = "journal+console";
            };
          };
        }
      ];
    }).config.system.build.vm;
  sentinels = map (s: "XTCP2_SCRAPE_AUTH_${s}") [
    "TOKENS_DONE"
    "HTTP_DONE"
    "VMAGENT_DONE"
    "HEALTHCHECK_DONE"
    "ROTATE_DONE"
    "PASS"
  ];
  # checks.vm-xtcp2-scrape-auth and test-xtcp2-scrape-auth-vm
  # (nix/microvms/mkVmCheck.nix).
  vmCheck = import ../microvms/mkVmCheck.nix { inherit pkgs lib; } {
    name = "xtcp2-scrape-auth";
    launch = "${vm}/bin/run-xtcp2-scrape-auth-vm";
    inherit sentinels;
    failPattern = "XTCP2_SCRAPE_AUTH_(ROW_)?FAIL";
    evidence.guest = vm;
  };
in
# test-xtcp2-scrape-auth-vm [--accel=auto|kvm|tcg] [--keep=N]: evidence under
# ~/.cache/xtcp2-vm/xtcp2-scrape-auth; checks.vm-xtcp2-scrape-auth is the same run in the build sandbox.
vmCheck.app
// {
  inherit vmCheck;
}
