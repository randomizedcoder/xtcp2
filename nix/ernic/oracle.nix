# nix/ernic/oracle.nix
#
# ernic-oracle: checks the guest's NIC counters against the emulator's own
# fault counts. rocm-ernic counts frames, never time, and at the end of a
# clean shutdown prints, per fault site,
#   Fault eth_tx: eligible <frames seen>, fired <frames dropped>
# (docs/ionic.rst upstream). The guest prints its counters on the console:
#   GOLDEN_ERNIC_COUNTERS <nic>_tx=N <nic>_tx_dropped=N <nic>_rx=N ...
#
#   ernic-oracle --log CONSOLE_LOG --dir ERNIC_DIR [--tx NIC]... [--rx NIC]...
#                [--segs-out NIC]... [--segs-in NIC]...
#
# --tx NIC (an eth_tx drop fault): eligible == tx + tx_dropped, fired == tx_dropped.
#   A dropped frame still completes, so the guest counts it in tx_dropped only.
# --rx NIC (an eth_rx drop fault): eligible == rx + fired. A dropped frame
#   never reaches the NIC, so no guest counter moves.
# --segs-out NIC: eligible (eth_tx) == <nic>_segs_out, the sum of
#   tcp_info_segs_out over the sockets sending out of NIC, as xtcp2 recorded
#   it. With TSO/GSO off one segment is one frame, and segs_out counts every
#   segment a socket transmits (__tcp_transmit_skb, net/ipv4/tcp_output.c).
# --segs-in NIC: <nic>_rx == <nic>_segs_in, the frames delivered to NIC
#   against tcp_info_segs_in of the sockets receiving on it (tcp_segs_in,
#   include/net/tcp.h; GRO off).
# A site that saw no frames fails: a vacuous match proves nothing.
# Prints ROW lines like the guest's checks; exits 1 if any fails.
# Counts are compared with bc, so any u64 is exact.
{ pkgs }:

pkgs.writeShellApplication {
  name = "ernic-oracle";
  runtimeInputs = [
    pkgs.coreutils
    pkgs.gnugrep
    pkgs.bc
  ];
  text = ''
    usage() {
      echo "usage: ernic-oracle --log CONSOLE_LOG --dir ERNIC_DIR [--tx NIC]... [--rx NIC]... [--segs-out NIC]... [--segs-in NIC]..." >&2
      exit 2
    }
    log="" dir=""
    tx=() rx=() segs_out=() segs_in=()
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --log) [ "$#" -ge 2 ] || usage; log=$2; shift 2 ;;
        --dir) [ "$#" -ge 2 ] || usage; dir=$2; shift 2 ;;
        --tx) [ "$#" -ge 2 ] || usage; tx+=("$2"); shift 2 ;;
        --rx) [ "$#" -ge 2 ] || usage; rx+=("$2"); shift 2 ;;
        --segs-out) [ "$#" -ge 2 ] || usage; segs_out+=("$2"); shift 2 ;;
        --segs-in) [ "$#" -ge 2 ] || usage; segs_in+=("$2"); shift 2 ;;
        *) usage ;;
      esac
    done
    [ -n "$log" ] && [ -n "$dir" ] || usage

    fail=0
    row() { # name result detail
      [ "$2" = PASS ] || fail=1
      echo "ROW $1 corner $2 $3"
    }

    counters=$(tr -d '\r' < "$log" | grep -o 'GOLDEN_ERNIC_COUNTERS.*' | tail -n 1 || true)
    if [ -z "$counters" ]; then
      row oracle FAIL "no GOLDEN_ERNIC_COUNTERS line in $log"
      exit 1
    fi

    # counter KEY: the guest's value, checked to be a decimal number.
    counter() {
      local v="" w
      for w in $counters; do
        if [[ $w == "$1="* ]]; then v=''${w#*=}; fi
      done
      if ! [[ $v =~ ^[0-9]+$ ]]; then
        echo "$1 is not a number: '$v'"
        return 1
      fi
      echo "$v"
    }
    # fault NIC SITE: "eligible fired" from the server's shutdown report.
    fault() {
      local f="$dir/ernic-$1.log" line
      line=$(tr -d '\r' < "$f" 2>/dev/null | grep -o "Fault $2: eligible [0-9]*, fired [0-9]*" | tail -n 1 || true)
      if [ -z "$line" ]; then
        echo "no Fault $2 line for $1 in $f"
        return 1
      fi
      [[ $line =~ eligible\ ([0-9]+),\ fired\ ([0-9]+) ]]
      echo "''${BASH_REMATCH[1]} ''${BASH_REMATCH[2]}"
    }
    eq() { [ "$(bc <<< "$1")" = "$(bc <<< "$2")" ]; }

    for nic in "''${tx[@]}"; do
      if ! f=$(fault "$nic" eth_tx); then row oracle_tx FAIL "$f"; continue; fi
      read -r eligible fired <<< "$f"
      if ! t=$(counter "''${nic}_tx"); then row oracle_tx FAIL "$t"; continue; fi
      if ! d=$(counter "''${nic}_tx_dropped"); then row oracle_tx FAIL "$d"; continue; fi
      if eq "$eligible" 0; then
        row oracle_tx FAIL "$nic saw no frames"
      elif ! eq "$fired" "$d"; then
        row oracle_tx FAIL "$nic fired $fired != tx_dropped $d"
      elif ! eq "$eligible" "$t + $d"; then
        row oracle_tx FAIL "$nic eligible $eligible != tx $t + tx_dropped $d"
      else
        row oracle_tx PASS "$nic eth_tx eligible $eligible fired $fired, guest tx $t tx_dropped $d"
      fi
    done

    for nic in "''${rx[@]}"; do
      if ! f=$(fault "$nic" eth_rx); then row oracle_rx FAIL "$f"; continue; fi
      read -r eligible fired <<< "$f"
      if ! r=$(counter "''${nic}_rx"); then row oracle_rx FAIL "$r"; continue; fi
      if eq "$eligible" 0; then
        row oracle_rx FAIL "$nic saw no frames"
      elif ! eq "$eligible" "$r + $fired"; then
        row oracle_rx FAIL "$nic eligible $eligible != rx $r + fired $fired"
      else
        row oracle_rx PASS "$nic eth_rx eligible $eligible fired $fired, guest rx $r"
      fi
    done

    for nic in "''${segs_out[@]}"; do
      if ! f=$(fault "$nic" eth_tx); then row oracle_segs_out FAIL "$f"; continue; fi
      read -r eligible _ <<< "$f"
      if ! n=$(counter "''${nic}_segs_out"); then row oracle_segs_out FAIL "$n"; continue; fi
      if eq "$eligible" "$n"; then
        row oracle_segs_out PASS "$nic eligible $eligible == segs_out $n"
      else
        row oracle_segs_out FAIL "$nic eligible $eligible != segs_out $n"
      fi
    done

    for nic in "''${segs_in[@]}"; do
      if ! r=$(counter "''${nic}_rx"); then row oracle_segs_in FAIL "$r"; continue; fi
      if ! n=$(counter "''${nic}_segs_in"); then row oracle_segs_in FAIL "$n"; continue; fi
      if eq "$r" "$n"; then
        row oracle_segs_in PASS "$nic rx $r == segs_in $n"
      else
        row oracle_segs_in FAIL "$nic rx $r != segs_in $n"
      fi
    done

    exit "$fail"
  '';
}
