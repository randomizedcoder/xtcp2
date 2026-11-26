# nix/checks/ernic-oracle.nix
#
# Table test for nix/ernic/oracle.nix (ernic-oracle): the emulator's own
# fault counts, from each server's clean-shutdown report, against the
# guest's NIC counters from the console line GOLDEN_ERNIC_COUNTERS. Each
# row builds a console log and server logs, runs the oracle, and prints
#   ROW <name> <category> PASS|FAIL <description>: <detail>
# and any FAIL fails the check.
{ pkgs, lib }:

let
  oracle = import ../ernic/oracle.nix { inherit pkgs lib; };
in
pkgs.runCommand "xtcp2-ernic-oracle"
  {
    nativeBuildInputs = [ pkgs.coreutils ];
  }
  ''
    fail=0
    row() { # name category description ok detail
      if [ "$4" = 1 ]; then r=PASS; else r=FAIL; fail=1; fi
      echo "ROW $1 $2 $r $3: $5"
    }
    ORACLE=${oracle}/bin/ernic-oracle

    # case COUNTERS-LINE IONIC0-LOG IONIC1-LOG: sets rc and out
    case_() {
      d=$(mktemp -d)
      printf '%b' "$1" > "$d/console.log"
      printf '%b' "$2" > "$d/ernic-ionic0.log"
      printf '%b' "$3" > "$d/ernic-ionic1.log"
      rc=0
      out=$($ORACLE --log "$d/console.log" --dir "$d" --tx ionic0 --rx ionic1 2>&1) || rc=$?
    }
    has() { [[ $out == *"$1"* ]]; }
    ok() { [ "$1" ] && echo 1; }

    C='GOLDEN_ERNIC_COUNTERS ionic0_tx=14542 ionic0_tx_dropped=296 ionic1_rx=8860\n'
    TX='rocm-ernic: Shutting down\n  Fault eth_tx: eligible 14838, fired 296\n'
    RX='rocm-ernic: Shutting down\n  Fault eth_rx: eligible 9000, fired 140\n'

    case_ "$C" "$TX" "$RX"
    row tx_match positive "the emulator's tx frames equal the guest's sent plus dropped" \
      "$(ok "$([ $rc = 0 ] && has 'ROW oracle_tx corner PASS' && echo y)")" "rc=$rc out=$out"

    row rx_match positive "the emulator's rx frames equal the guest's received plus dropped" \
      "$(ok "$([ $rc = 0 ] && has 'ROW oracle_rx corner PASS' && echo y)")" "rc=$rc out=$out"

    case_ 'GOLDEN_ERNIC_COUNTERS ionic0_tx=14543 ionic0_tx_dropped=295 ionic1_rx=8860\n' "$TX" "$RX"
    row fired_mismatch negative "a guest drop counter that disagrees fails" \
      "$(ok "$([ $rc = 1 ] && has 'ionic0 fired 296 != tx_dropped 295' && echo y)")" "rc=$rc out=$out"

    case_ "$C" 'rocm-ernic: Shutting down\n' "$RX"
    row no_fault_line negative "a server log without its report (no clean shutdown) fails" \
      "$(ok "$([ $rc = 1 ] && has 'no Fault eth_tx line for ionic0' && echo y)")" "rc=$rc out=$out"

    case_ 'boot\n' "$TX" "$RX"
    row no_counters negative "a console log without the guest's counters fails" \
      "$(ok "$([ $rc = 1 ] && has 'no GOLDEN_ERNIC_COUNTERS line' && echo y)")" "rc=$rc out=$out"

    case_ 'GOLDEN_ERNIC_COUNTERS ionic0_tx=0 ionic0_tx_dropped=0 ionic1_rx=8860\n' '  Fault eth_tx: eligible 0, fired 0\n' "$RX"
    row zero_eligible boundary "a site that saw no frames is a failure, not a vacuous pass" \
      "$(ok "$([ $rc = 1 ] && has 'ionic0 saw no frames' && echo y)")" "rc=$rc out=$out"

    case_ 'GOLDEN_ERNIC_COUNTERS ionic0_tx=0 ionic0_tx_dropped=50 ionic1_rx=8860\n' '  Fault eth_tx: eligible 50, fired 50\n' "$RX"
    row fired_equals_eligible boundary "every frame dropped is still consistent" \
      "$(ok "$([ $rc = 0 ] && echo y)")" "rc=$rc out=$out"

    case_ "$C" '  Fault eth_rx: eligible 5, fired 5\n  Fault eth_tx: eligible 14838, fired 296\n' '  Fault eth_tx: eligible 3, fired 0\n  Fault eth_rx: eligible 9000, fired 140\n'
    row two_sites corner "each NIC is compared on its own site, whatever else its log reports" \
      "$(ok "$([ $rc = 0 ] && echo y)")" "rc=$rc out=$out"

    case_ '[  113.3] xtcp2-golden-capture[555]: GOLDEN_ERNIC_COUNTERS ionic0_tx=1 ionic0_tx_dropped=1 ionic1_rx=1\r\n[  114.0] xtcp2-golden-capture[555]: GOLDEN_ERNIC_COUNTERS ionic0_tx=14542 ionic0_tx_dropped=296 ionic1_rx=8860\r\n' "$TX" "$RX"
    row console_prefix corner "a console line with a journal prefix and CR is read, and the last one wins" \
      "$(ok "$([ $rc = 0 ] && echo y)")" "rc=$rc out=$out"

    case_ 'GOLDEN_ERNIC_COUNTERS ionic0_tx=18446744073709551614 ionic0_tx_dropped=1 ionic1_rx=8860\n' '  Fault eth_tx: eligible 18446744073709551615, fired 1\n' "$RX"
    row u64 corner "the largest u64 count is compared exactly" \
      "$(ok "$([ $rc = 0 ] && echo y)")" "rc=$rc out=$out"

    case_ 'GOLDEN_ERNIC_COUNTERS ionic0_tx=1x ionic0_tx_dropped=296 ionic1_rx=8860\n' "$TX" "$RX"
    row not_a_number corner "a counter that is not a decimal number fails" \
      "$(ok "$([ $rc = 1 ] && has 'ionic0_tx is not a number' && echo y)")" "rc=$rc out=$out"

    [ "$fail" = 0 ] || exit 1
    touch $out
  ''
