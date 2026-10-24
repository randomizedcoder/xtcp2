# nix/checks/nlcap-wait-linkwatch.nix
#
# Hermetic check for `xtcp2-nlcap wait-linkwatch`
# (nix/microvms/netlink-capture.nix), the wait goip-parity.exp's settle_links
# runs after each Setup so no side is captured before linkwatch has updated a
# link's operstate. The real helper runs against a fake /sys/class/net
# (NLCAP_SYS_CLASS_NET) in the root namespace (`-`). Each row asserts the
# printed line, the exit status and the elapsed wall clock. It does not
# cover whether a kernel's linkwatch behaves as the helper assumes; the
# goip-parity runs record that.
{ pkgs }:

let
  nlcap = import ../microvms/netlink-capture.nix { inherit pkgs; };
in
pkgs.runCommand "xtcp2-nlcap-wait-linkwatch"
  {
    nativeBuildInputs = with pkgs; [
      bash
      coreutils
      gnugrep
      nlcap
    ];
  }
  ''
    bash ${pkgs.writeText "wait-linkwatch-test.bash" ''
      set -u
      fails=0
      NLCAP_SYS_CLASS_NET=$(mktemp -d)
      export NLCAP_SYS_CLASS_NET
      log=$(mktemp)

      # link <name> <flags> <carrier|-> <operstate> - one fake link; `-`
      # leaves carrier absent, as the kernel's EINVAL on a down link reads.
      link() {
        local d=$NLCAP_SYS_CLASS_NET/$1
        mkdir -p "$d"
        echo "$2" >"$d/flags"
        if [ "$3" != - ]; then echo "$3" >"$d/carrier"; fi
        echo "$4" >"$d/operstate"
      }

      # later <secs> <name> <operstate> - linkwatch firing after <secs>.
      later() {
        (sleep "$1"; echo "$3" >"$NLCAP_SYS_CLASS_NET/$2/operstate") &
      }

      # row <category> <description> <want_rc> <want_regex> <min_ms> <max_ms> <args...>
      row() {
        local category=$1 desc=$2 want_rc=$3 want=$4 min_ms=$5 max_ms=$6 rc t0 ms
        shift 6
        t0=$(date +%s%N)
        rc=0
        xtcp2-nlcap wait-linkwatch "$@" >"$log" 2>&1 || rc=$?
        ms=$((($(date +%s%N) - t0) / 1000000))
        wait
        if [ "$rc" -ne "$want_rc" ] || ! grep -Eq -- "$want" "$log" ||
          [ "$ms" -lt "$min_ms" ] || [ "$ms" -gt "$max_ms" ]; then
          echo "FAIL $category: $desc: rc=$rc (want $want_rc), ''${ms}ms (want $min_ms..$max_ms), output (want /$want/):"
          cat "$log"
          fails=$((fails + 1))
        else
          echo "ok   $category: $desc (rc=$rc, ''${ms}ms): $(head -1 "$log")"
        fi
        rm -rf -- "''${NLCAP_SYS_CLASS_NET:?}"/*
      }

      # The settled states: loopback and a dummy have carrier and stay
      # UNKNOWN; a veth up with its peer down has none and goes DOWN; a
      # bond port whose peer is down reads LOWERLAYERDOWN.
      link lo 0x9 1 unknown
      link d0 0x1083 1 unknown
      link la1 0x1003 0 down
      link la2 0x1803 0 lowerlayerdown
      link bond0 0x5403 0 down
      row positive "every admin-up link already agrees" 0 '^NLCAP_LINKWATCH_OK - elapsed=0s$' 0 900 - 5

      link lo 0x9 1 unknown
      link la1 0x1003 0 unknown
      later 1 la1 down
      row positive "a veth up with its peer down reads UNKNOWN until linkwatch sets DOWN" 0 '^NLCAP_LINKWATCH_OK - elapsed=[12]s$' 900 2900 - 5

      link la1 0x1003 1 down
      later 1 la1 up
      row positive "carrier just came on and operstate has not followed yet" 0 '^NLCAP_LINKWATCH_OK' 900 2900 - 5

      link la1 0x1003 0 unknown
      row negative "a link that never settles times out and is named" 1 '^NLCAP_LINKWATCH_TIMEOUT - unsettled: la1\(carrier=0,operstate=unknown\) after 1s$' 900 2900 - 1

      link la1 0x1003 0 unknown
      link la2 0x1003 0 up
      row negative "every unsettled link is named, not only the first" 1 'la1\(carrier=0,operstate=unknown\) la2\(carrier=0,operstate=up\)' 0 1900 - 0

      row negative "a timeout that is not a number is a usage error" 2 'timeout not a number: soon' 0 900 - soon
      row negative "a missing timeout is a usage error" 2 'usage: wait-linkwatch' 0 900 -

      link lo 0x9 1 unknown
      row boundary "timeout 0 on a settled namespace succeeds at once" 0 '^NLCAP_LINKWATCH_OK - elapsed=0s$' 0 900 - 0

      link la1 0x1003 0 unknown
      row boundary "timeout 0 on an unsettled one polls once and fails" 1 '^NLCAP_LINKWATCH_TIMEOUT' 0 900 - 0

      link la1 0x1002 - unknown
      row corner "a down link is skipped: no carrier to read, and no RUNNING either way" 0 '^NLCAP_LINKWATCH_OK' 0 900 - 0

      link la1 0x1003 - unknown
      row corner "an up link whose carrier cannot be read is skipped" 0 '^NLCAP_LINKWATCH_OK' 0 900 - 0

      mkdir -p "$NLCAP_SYS_CLASS_NET/gone"
      link lo 0x9 1 unknown
      row corner "a link that vanished mid-scan (no flags) is skipped" 0 '^NLCAP_LINKWATCH_OK' 0 900 - 0

      link d0 0x1043 1 dormant
      row corner "dormant with carrier is not RUNNING, so it never settles" 1 'd0\(carrier=1,operstate=dormant\)' 0 900 - 0

      rm -rf -- "$NLCAP_SYS_CLASS_NET" "$log"
      if [ "$fails" -ne 0 ]; then
        echo "$fails row(s) failed"
        exit 1
      fi
    ''}
    touch $out
  ''
