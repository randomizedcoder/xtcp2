#!/usr/bin/env bash
# Parse guards for the go-native RDMA decoders (GO-NATIVE-RDMA-DESIGN.md,
# "Parsing rules"). Run from the repository root:
#
#   linkmonitor-parse-guards.sh OUT_DIR DECODER_FILE...
#
# Fails when the compiler keeps a bounds check (Found IsInBounds or
# IsSliceInBounds) in any listed decoder file other than the length hint, and
# records the -m escape and inlining report as OUT_DIR/escape.txt for review.
set -euo pipefail

out=$1
shift
[ "$#" -gt 0 ] || { echo "no decoder files listed" >&2; exit 2; }

declare -A pkgs=()
for file in "$@"; do
  [ -f "$file" ] || { echo "listed decoder file is missing: $file" >&2; exit 1; }
  pkgs["./$(dirname "$file")"]=1
done
mapfile -t packages < <(printf '%s\n' "${!pkgs[@]}" | sort)

# gcflags_for FLAG prints one -gcflags argument per decoder package; the go
# command applies the last matching pattern, so each package gets its own.
gcflags_for() {
  local pkg
  for pkg in "${packages[@]}"; do
    printf -- '-gcflags=%s=%s\n' "$(go list "$pkg")" "$1"
  done
}

# The one length hint, "_ = b[n-1]", is the check the rules ask for; any other
# check in a decoder file is a finding. The column points at the '['.
hint='(^|[;{])[[:space:]]*_ = [A-Za-z_][A-Za-z0-9_.]*$'
is_hint() {
  local file=$1 line=$2 col=$3 text
  text=$(sed -n "${line}p" "$file")
  text=${text:0:$((col - 1))}
  [[ $text =~ $hint ]]
}

mapfile -t bce < <(gcflags_for -d=ssa/check_bce/debug=1)
# -a so cached packages are recompiled and report their bounds checks.
go build -a "${bce[@]}" "${packages[@]}" > "$out/bce.txt" 2>&1

found=0
for file in "$@"; do
  while IFS=: read -r path line col _; do
    if ! is_hint "$file" "$line" "$col"; then
      echo "$path:$line:$col: bounds check outside the length hint"
      found=1
    fi
  done < <(grep -E "^(\./)?${file}:[0-9]+:[0-9]+: Found Is(Slice)?InBounds" "$out/bce.txt" || true)
done
if [ "$found" -ne 0 ]; then
  echo "bounds checks remain in decoder files; add one length hint before the field loads" >&2
  exit 1
fi

mapfile -t escape < <(gcflags_for -m)
go build -a "${escape[@]}" "${packages[@]}" > "$out/escape.txt" 2>&1
printf '%s\n' "$@" > "$out/decoder-files.txt"
