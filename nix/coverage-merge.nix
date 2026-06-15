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
#     --vm-dir /path/to/xtcp2cov [--vm-dir /path/to/xtcp2cov-iouring …] \
#     --out /tmp/merged.profile
#
# Produces a `mode: set` profile usable with `go tool cover -func`
# or `go tool cover -html`. Uses the host profile's block universe
# (so build-tag-gated destination files don't drag the total down)
# and upgrades the count when a block was also covered in the VM run.
#
# Multiple --vm-dir args are concatenated via `go tool covdata textfmt
# -i a,b,…` so the merge picks up every block any VM run covered.
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
    VMDIRS=""
    OUT=""
    while [ $# -gt 0 ]; do
      case "$1" in
        --host)    HOST="$2"; shift 2 ;;
        --vm-dir)
          # Accumulate as a comma-separated list for go tool covdata's -i.
          if [ -z "$VMDIRS" ]; then VMDIRS="$2"; else VMDIRS="$VMDIRS,$2"; fi
          shift 2
          ;;
        --out)     OUT="$2"; shift 2 ;;
        -h|--help)
          echo "usage: $0 --host <host.out> --vm-dir <covdir> [--vm-dir <covdir> …] --out <merged.out>"
          exit 0
          ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
      esac
    done
    if [ -z "$HOST" ] || [ -z "$VMDIRS" ] || [ -z "$OUT" ]; then
      echo "usage: $0 --host <host.out> --vm-dir <covdir> [--vm-dir <covdir> …] --out <merged.out>" >&2
      exit 1
    fi
    if [ ! -s "$HOST" ]; then echo "host profile missing: $HOST" >&2; exit 1; fi
    # Validate each VMDIR exists.
    IFS=, read -ra _dirs <<< "$VMDIRS"
    for d in "''${_dirs[@]}"; do
      if [ ! -d "$d" ]; then echo "vm dir missing: $d" >&2; exit 1; fi
    done

    VM_PROFILE=$(mktemp)
    trap 'rm -f "$VM_PROFILE"' EXIT

    go tool covdata textfmt -i "$VMDIRS" -o "$VM_PROFILE"

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
