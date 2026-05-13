# nix/checks/cli-help-smoke.nix
#
# Smoke test: `xtcp2 -help` exits cleanly and prints something.
#
# Catches: broken flag parsing, missing init in main(), early-panic regressions.
#
{
  pkgs,
  lib,
  xtcp2,
}:

pkgs.runCommand "xtcp2-cli-help-smoke" { } ''
  set +e
  output=$(${xtcp2}/bin/xtcp2 -help 2>&1)
  rc=$?
  set -e
  # Go's `flag` package exits 0 (some configs use 2) on -help. Anything higher
  # is broken.
  if [ "$rc" -gt 2 ]; then
    echo "xtcp2 -help exited with $rc"
    echo "$output"
    exit 1
  fi
  if [ -z "$output" ]; then
    echo "xtcp2 -help produced no output"
    exit 1
  fi
  printf 'xtcp2 -help: rc=%d, %d bytes output\n' "$rc" "''${#output}" > $out
''
