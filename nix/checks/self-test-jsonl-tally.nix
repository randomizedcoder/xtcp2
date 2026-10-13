# nix/checks/self-test-jsonl-tally.nix
#
# Hermetic check for the self-test's jsonl_tally function
# (nix/microvms/self-test-jsonl-tally.nix), run under bash with jq. Each row
# pipes its lines into jsonl_tally and compares the "<good> <bad>" it prints.
# It does not cover the daemon's jsonl itself; the lifecycle VM runs do.
{ pkgs }:

let
  jsonlTally = import ../microvms/self-test-jsonl-tally.nix;
in
pkgs.runCommand "xtcp2-self-test-jsonl-tally"
  {
    nativeBuildInputs = with pkgs; [
      bash
      coreutils
      jq
    ];
  }
  ''
    bash ${pkgs.writeText "jsonl-tally-test.bash" ''
      set -u
      ${jsonlTally}

      fails=0

      # row <description> <expected> [line...]: each line is one input line.
      row() {
        local desc=$1 want=$2 got
        shift 2
        if [ "$#" -eq 0 ]; then
          got=$(jsonl_tally </dev/null)
        else
          got=$(printf '%s\n' "$@" | jsonl_tally)
        fi
        if [ "$got" != "$want" ]; then
          echo "FAIL $desc: got '$got', want '$want'"
          fails=$((fails + 1))
        else
          echo "ok   $desc ($got)"
        fi
      }

      row "positive: all records valid" "2 0" \
        '{"hostname":"a"}' '{"hostname":"b","x":1}'
      row "negative: malformed JSON" "0 2" \
        '{"hostname":"a"' 'garbage'
      row "negative: hostname missing" "0 1" '{"x":1}'
      row "negative: hostname empty" "0 1" '{"hostname":""}'
      row "negative: hostname null" "0 1" '{"hostname":null}'
      row "negative: valid JSON that is not an object" "0 3" '[1]' '5' '"s"'
      row "boundary: no input" "0 0"
      row "boundary: one valid record" "1 0" '{"hostname":"a"}'
      row "corner: empty lines are skipped" "1 0" "" '{"hostname":"a"}' ""
      row "corner: a whitespace-only line is bad" "0 1" '   '
      row "corner: two objects on one line are bad" "0 1" \
        '{"hostname":"a"}{"hostname":"b"}'
      row "corner: one bad line among good ones is counted, not fatal" "2 1" \
        '{"hostname":"a"}' 'oops' '{"hostname":"b"}'

      # A large file in one pass. Correctness only: no timing bound, so it
      # cannot flake on a loaded builder.
      big=$(for i in $(seq 1 20000); do echo "{\"hostname\":\"vm\",\"x\":$i}"; done)
      got=$(printf '%s\n' "$big" | jsonl_tally)
      if [ "$got" != "20000 0" ]; then
        echo "FAIL corner: a large file in one pass: got '$got', want '20000 0'"
        fails=$((fails + 1))
      else
        echo "ok   corner: a large file in one pass ($got)"
      fi

      if [ "$fails" -ne 0 ]; then
        echo "$fails row(s) failed"
        exit 1
      fi
    ''}
    touch $out
  ''
