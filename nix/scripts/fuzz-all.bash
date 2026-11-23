#!/usr/bin/env bash
# fuzz-all: find every Go fuzz target in the module and fuzz each one in turn.
#
# Discovery uses `go test -list`, so a new Fuzz* function is picked up without
# editing a list. Every target runs even after one fails; the summary at the
# end names the failures and the crasher files Go wrote under
# <pkg>/testdata/fuzz/<Target>/. Commit a crasher (and add it as a table row)
# once the bug is fixed, so it stays a regression seed.
#
# Environment:
#   FUZZTIME          per-target budget (default 10s)
#   FUZZ_PARALLEL     fuzz workers per target (default 2)
#   FUZZ_MINIMIZE     -fuzzminimizetime (default 5s). Go minimizes each new
#                     interesting input for up to 60s by default, and those
#                     runs are not counted as execs: with a 10s budget and a
#                     slow target (FuzzRowFromProto writes Parquet) the whole
#                     budget went on minimizing.
#   FUZZ_TAGS         build tags (default: dest_s3parquet enrich_asn enrich_locality)
#   FUZZ_FILTER       extended regex on "pkg:target"; only matches run
#   FUZZ_MIN_TARGETS  fail if discovery finds fewer (default 50)
#   FUZZ_LOG_DIR      per-target logs (default: a new temporary directory)
#
# Run from the repository root: nix develop -c fuzz-all
set -euo pipefail

fuzztime=${FUZZTIME:-10s}
parallel=${FUZZ_PARALLEL:-2}
minimize=${FUZZ_MINIMIZE:-5s}
tags=${FUZZ_TAGS-dest_s3parquet enrich_asn enrich_locality}
filter=${FUZZ_FILTER:-}
min_targets=${FUZZ_MIN_TARGETS:-50}
log_dir=${FUZZ_LOG_DIR:-$(mktemp -d -t fuzz-all.XXXXXX)}

if [[ ! -f go.mod ]]; then
  echo "fuzz-all: run from the repository root (no go.mod here)" >&2
  exit 2
fi
mkdir -p "$log_dir"

# `go test -list` prints a package's matching names, then its "ok <pkg>" line.
list_out=$(go test -list '^Fuzz' -tags "$tags" ./...)
mapfile -t all_targets < <(
  awk '
    /^Fuzz/ { names[++n] = $1; next }
    /^ok[ \t]/ { for (i = 1; i <= n; i++) print $2 ":" names[i]; n = 0 }
  ' <<<"$list_out"
)

echo "fuzz-all: discovered ${#all_targets[@]} targets (tags: ${tags:-none})"
if ((${#all_targets[@]} < min_targets)); then
  echo "fuzz-all: expected at least $min_targets targets; discovery is broken or targets were removed" >&2
  exit 1
fi

targets=()
for entry in "${all_targets[@]}"; do
  if [[ -z $filter || $entry =~ $filter ]]; then
    targets+=("$entry")
  fi
done
if ((${#targets[@]} == 0)); then
  echo "fuzz-all: 0 targets matched FUZZ_FILTER=$filter" >&2
  exit 1
fi

echo "fuzz-all: running ${#targets[@]} targets for $fuzztime each; logs in $log_dir"
failed=()
start=$SECONDS
start_epoch=$(date +%s)
for entry in "${targets[@]}"; do
  pkg=${entry%%:*}
  target=${entry#*:}
  log="$log_dir/${pkg//\//_}.$target.log"
  t0=$SECONDS
  if go test "$pkg" -tags "$tags" -run '^$' -fuzz "^${target}\$" \
    -fuzztime="$fuzztime" -fuzzminimizetime="$minimize" \
    -parallel="$parallel" -timeout=30m >"$log" 2>&1; then
    printf 'PASS %-70s %4ds\n' "$entry" $((SECONDS - t0))
  else
    printf 'FAIL %-70s %4ds\n' "$entry" $((SECONDS - t0))
    tail -n 30 "$log"
    failed+=("$entry")
  fi
done

echo "fuzz-all: ${#targets[@]} targets in $((SECONDS - start))s, ${#failed[@]} failed"
if ((${#failed[@]} > 0)); then
  for entry in "${failed[@]}"; do
    pkg=${entry%%:*}
    target=${entry#*:}
    echo "  $entry"
    dir=$(go list -tags "$tags" -f '{{.Dir}}' "$pkg")/testdata/fuzz/$target
    if [[ -d $dir ]]; then
      find "$dir" -type f -newermt "@$start_epoch" -printf '    crasher: %p\n'
    fi
  done
  exit 1
fi
