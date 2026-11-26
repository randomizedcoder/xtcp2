# nix/checks/nlcap-tcmon.nix
#
# Hermetic check for `xtcp2-nl-tcmon` (nix/microvms/netlink-capture.nix),
# the helper the tcmonitor workbench rows run `tc monitor` under. The real
# helper runs a fake tc (NLCAP_TC) that "subscribes" by writing a line to a
# fake /proc/net/netlink (NLCAP_PROC_NETLINK) and prints what the trigger
# appends to an events file. Each row asserts the exit status, stdout and
# stderr. Whether tc behaves as the fake does is what the workbench rows
# themselves record.
{ pkgs }:

let
  nlcap = import ../microvms/netlink-capture.nix { inherit pkgs; };
  fakeTc = pkgs.writeShellScript "fake-tc" ''
    # fake-tc <mode>: monitor | never | other-group | trunc | die
    proc=$NLCAP_PROC_NETLINK
    sub() { printf 'sk Eth Pid Groups\nffff0001 0 4242 %s\n' "$1" >"$proc"; }
    case $1 in
      monitor) sub 00000008 ;;
      other-group) sub 00000001 ;;
      never) ;;
      trunc) sub 00000008 ;;
      die) echo 'Argument "die" is unknown, try "tc monitor help".' >&2; exit 255 ;;
    esac
    seen=0
    while :; do
      if [ -f "$EVENTS" ]; then
        n=$(wc -l <"$EVENTS")
        if [ "$n" -gt "$seen" ]; then
          if [ "$1" = trunc ]; then
            echo "Truncated message" >&2
            exit 2
          fi
          tail -n +"$((seen + 1))" "$EVENTS" | head -n "$((n - seen))"
          seen=$n
        fi
      fi
      sleep 0.02
    done
  '';
in
pkgs.runCommand "xtcp2-nlcap-tcmon"
  {
    nativeBuildInputs = with pkgs; [
      bash
      coreutils
      gnugrep
      nlcap.tcmon
    ];
  }
  ''
    bash ${pkgs.writeText "tcmon-test.bash" ''
      set -u
      fails=0
      work=$(mktemp -d)
      export NLCAP_PROC_NETLINK=$work/netlink NLCAP_TC=${fakeTc} EVENTS=$work/events
      out=$work/out
      err=$work/err

      # row <category> <description> <want_rc> <stdout-ERE> <stderr-ERE> <helper args...>
      # An ERE of - wants the stream empty.
      row() {
        local category=$1 desc=$2 want_rc=$3 want_out=$4 want_err=$5 rc ok=1
        shift 5
        : >"$NLCAP_PROC_NETLINK"
        rm -f "$EVENTS"
        rc=0
        xtcp2-nl-tcmon "$@" >"$out" 2>"$err" || rc=$?
        [ "$rc" -eq "$want_rc" ] || ok=0
        if [ "$want_out" = - ]; then [ ! -s "$out" ] || ok=0; else grep -Eq -- "$want_out" "$out" || ok=0; fi
        if [ "$want_err" = - ]; then [ ! -s "$err" ] || ok=0; else grep -Eq -- "$want_err" "$err" || ok=0; fi
        if [ "$ok" -eq 1 ]; then
          echo "ok   $category: $desc (rc=$rc)"
        else
          echo "FAIL $category: $desc: rc=$rc (want $want_rc); stdout (want /$want_out/):"
          cat "$out"
          echo "stderr (want /$want_err/):"
          cat "$err"
          fails=$((fails + 1))
        fi
      }

      row positive "one event, matched, then SIGTERM" 0 '^qdisc noqueue 0: dev d0$' - \
        monitor "echo 'qdisc noqueue 0: dev d0' >>$EVENTS" '^qdisc noqueue'
      row positive "two events, both waited for" 0 '^deleted qdisc htb' - \
        monitor "echo 'qdisc htb 1: dev d0' >>$EVENTS; echo 'deleted qdisc htb 1: dev d0' >>$EVENTS" '^qdisc htb' '^deleted'
      row negative "a monitor that never subscribes" 1 - 'never subscribed' \
        never "echo x >>$EVENTS" x
      row negative "a socket on other groups is not the monitor" 1 - 'never subscribed' \
        other-group "echo x >>$EVENTS" x
      row negative "a failing trigger is shown, with its output" 1 - 'the trigger failed: boom' \
        monitor "echo boom; false"
      row negative "an event that never comes" 1 - 'no match for .never' \
        monitor "true" '^never$'
      row boundary "a monitor that ends by itself passes its status on" 2 - '^Truncated message$' \
        trunc "echo big >>$EVENTS" 'Truncated message'
      row boundary "and needs no ERE" 2 - '^Truncated message$' \
        trunc "echo big >>$EVENTS"
      row corner "a monitor refusing its arguments exits with its own status, no complaint" 255 - '^Argument "die" is unknown' \
        die "true"
      row corner "usage: fewer than two arguments" 2 - '^usage: xtcp2-nl-tcmon' monitor

      rm -rf "$work"
      if [ "$fails" -ne 0 ]; then
        echo "$fails row(s) failed"
        exit 1
      fi
    ''}
    touch $out
  ''
