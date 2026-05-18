# nix/coverage-merge.nix
#
# Helper that combines a host `go test` coverage profile with the VM
# coverage data scraped by the microvm coverage harness (see
# `nix/microvms/lib.nix`'s `scrapeCoverage`). Both inputs are expected
# to be on disk before this script runs; it doesn't drive either
# collection step.
#
# Usage:
#   nix run .#coverage-merge -- \
#     --host /path/to/host-coverage.out \
#     --vm-dir /path/to/xtcp2cov \
#     --out /tmp/merged.profile
#
# Produces a `mode: set` profile usable with `go tool cover -func`
# or `go tool cover -html`. Uses the host profile's block universe
# (so build-tag-gated destination files don't drag the total down)
# and upgrades the count when a block was also covered in the VM run.
#
{ pkgs }:

pkgs.writeShellApplication {
  name = "xtcp2-coverage-merge";
  runtimeInputs = with pkgs; [
    coreutils
    gawk
    gnugrep
    go
  ];
  text = ''
    set -euo pipefail

    HOST=""
    VMDIR=""
    OUT=""
    while [ $# -gt 0 ]; do
      case "$1" in
        --host)    HOST="$2"; shift 2 ;;
        --vm-dir)  VMDIR="$2"; shift 2 ;;
        --out)     OUT="$2"; shift 2 ;;
        -h|--help)
          echo "usage: $0 --host <host.out> --vm-dir <covdir> --out <merged.out>"
          exit 0
          ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
      esac
    done
    if [ -z "$HOST" ] || [ -z "$VMDIR" ] || [ -z "$OUT" ]; then
      echo "usage: $0 --host <host.out> --vm-dir <covdir> --out <merged.out>" >&2
      exit 1
    fi
    if [ ! -s "$HOST" ]; then echo "host profile missing: $HOST" >&2; exit 1; fi
    if [ ! -d "$VMDIR" ]; then echo "vm dir missing: $VMDIR" >&2; exit 1; fi

    VM_PROFILE=$(mktemp)
    trap 'rm -f "$VM_PROFILE"' EXIT

    go tool covdata textfmt -i "$VMDIR" -o "$VM_PROFILE"

    skipPkg='github.com/randomizedcoder/xtcp2/pkg/xtcp_config|github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record|github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist'
    VM_FILTERED=$(mktemp)
    trap 'rm -f "$VM_PROFILE" "$VM_FILTERED"' EXIT
    grep -vE "$skipPkg" "$VM_PROFILE" > "$VM_FILTERED" || true

    gawk '
      BEGIN {
        print "mode: set"
        file_idx = 0
      }
      FNR == 1 { file_idx++ }
      $1 == "mode:" { next }
      NF == 3 {
        # path:range numStmt count
        key = $1
        numStmt = $2 + 0
        count = $3 + 0
        if (file_idx == 1) {
          universe[key] = numStmt
          if (count > merged[key]) merged[key] = count
        } else {
          if (key in universe && count > merged[key]) merged[key] = count
        }
      }
      END {
        for (key in universe) {
          print key, universe[key], (merged[key] > 0 ? 1 : 0)
        }
      }
    ' "$HOST" "$VM_FILTERED" > "$OUT"

    echo "merged profile: $OUT"
    go tool cover -func="$OUT" | tail -1
  '';
}
