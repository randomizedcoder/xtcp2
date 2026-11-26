# nix/checks/nlcap-tcmon.nix
#
# Hermetic check for `xtcp2-nl-tcmon` (nix/microvms/netlink-capture.nix),
# the helper the tcmonitor workbench rows run `tc monitor` under. The real
# helper runs a fake tc (NLCAP_TC) that "subscribes" by writing its socket
# to a fake /proc/net/netlink (NLCAP_PROC_NETLINK), "listens" by writing a
# recvmsg to a fake /proc/<pid>/task/<pid>/syscall (NLCAP_PROC), and prints
# what the trigger appends to an events file once it listens; what is
# appended before, while it "dumps", is lost, as rtnl_dump_filter drops it.
# Each row asserts the exit status, stdout and stderr. Whether tc behaves as
# the fake does is what the workbench rows themselves record.
{ pkgs }:

let
  nlcap = import ../microvms/netlink-capture.nix { inherit pkgs; };
  fakeTc = pkgs.writeShellScript "fake-tc" ''
    # fake-tc <mode>: monitor | slow-dump | dumping | queued | elsewhere |
    # never | other-group | trunc | die
    proc=$NLCAP_PROC_NETLINK
    task=$NLCAP_PROC/$$/task/$$
    # sub <groups> <rmem> <dump>: the socket's /proc/net/netlink line.
    sub() {
      printf 'sk Eth Pid Groups Rmem Wmem Dump Locks Drops Inode\nffff0001 0 4242 %s %s 0 %s 2 0 1\n' "$1" "$2" "$3" >"$proc"
    }
    # sys <nr>: the one thread's syscall, as the kernel shows a blocked one.
    sys() {
      mkdir -p "$task"
      echo "$1 0x3 0x7ffc0000 0x0 0x0 0x0 0x0 0x7ffc0000 0x7f0000000000" >"$task/syscall"
    }
    case $1 in
      monitor) sub 00000008 0 0; sys 47 ;;
      slow-dump) sub 00000008 0 1 ;;
      dumping) sub 00000008 0 1; sys 47 ;;
      queued) sub 00000008 320 0; sys 47 ;;
      elsewhere) sub 00000008 0 0; sys 7 ;;
      other-group) sub 00000001 0 0; sys 47 ;;
      trunc) sub 00000008 0 0; sys 47 ;;
      never) ;;
      die) echo 'Argument "die" is unknown, try "tc monitor help".' >&2; exit 255 ;;
    esac
    seen=0
    if [ "$1" = slow-dump ]; then
      # The dump runs for a while; whatever the trigger appends meanwhile is
      # not the dump's and is dropped. Then it listens.
      sleep 0.5
      [ -f "$EVENTS" ] && seen=$(wc -l <"$EVENTS")
      sub 00000008 0 0
      sys 47
    fi
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
      export NLCAP_PROC_NETLINK=$work/netlink NLCAP_PROC=$work/proc NLCAP_RECVMSG_NR=47 \
        NLCAP_TC=${fakeTc} EVENTS=$work/events
      out=$work/out
      err=$work/err

      # row <category> <description> <want_rc> <stdout-ERE> <stderr-ERE> <helper args...>
      # An ERE of - wants the stream empty.
      row() {
        local category=$1 desc=$2 want_rc=$3 want_out=$4 want_err=$5 rc ok=1
        shift 5
        : >"$NLCAP_PROC_NETLINK"
        rm -rf "$EVENTS" "$NLCAP_PROC"
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
      row positive "a monitor still dumping is waited for; the event raised after is seen" 0 '^qdisc noqueue 0: dev d0$' - \
        slow-dump "echo 'qdisc noqueue 0: dev d0' >>$EVENTS" '^qdisc noqueue'
      row negative "a monitor whose dump never ends is not ready" 1 - 'never finished its link dump' \
        dumping "echo x >>$EVENTS" x
      row boundary "a reply still queued (Rmem 320) is not ready" 1 - 'never finished its link dump' \
        queued "echo x >>$EVENTS" x
      row corner "a monitor blocked in another syscall than recvmsg is not ready" 1 - 'never finished its link dump' \
        elsewhere "echo x >>$EVENTS" x
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
