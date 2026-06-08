# nix/checks/cli-help-smoke.nix
#
# Smoke matrix: for every cmd binary, run `-help` and verify exit code ≤ 2
# (Go's `flag` package exits with 0 or 2 on -help/-h) and non-empty output.
#
# Catches: panic-on-init, broken flag declarations, missing dependency that
# only manifests at runtime, accidental removal of a binary's `-help` flag.
#
# Wired into nix/checks/default.nix as one attribute per binary
# (`cli-help-smoke-xtcp2`, `cli-help-smoke-clickhouse_protobuflist`, …) so
# CI logs name the failing binary cleanly.
#
{
  pkgs,
  lib,
  binaries,
}:

let
  # `binaries` is the result of `nix/binaries.nix`. We pluck the default-
  # variant attr for every cmd by name; the keys must match the list in
  # binaries.nix. Keep these two in sync.
  cmdNames = [
    "clickhouse_http_insert_protobuflist"
    "clickhouse_protobuflist"
    "clickhouse_protobuflist_db"
    "kafka_to_clickhouse"
    "ns"
    "nsTest"
    "register_schema"
    "xtcp2"
    "xtcp2client"
    "xtcp2_kafka_client"
  ];

  mkSmoke =
    cmd:
    pkgs.runCommand "xtcp2-cli-help-smoke-${cmd}"
      {
        nativeBuildInputs = [ binaries.${cmd} ];
      }
      ''
        set +e
        output=$(${binaries.${cmd}}/bin/${cmd} -help 2>&1)
        rc=$?
        set -e
        # Go's `flag` package exits with 0 on -help in some configs, 2 in
        # others (depending on which API the cmd uses). Either is fine.
        # Anything higher means a panic or broken init.
        if [ "$rc" -gt 2 ]; then
          echo "cli-help-smoke-${cmd}: -help exited with $rc" >&2
          echo "$output" >&2
          exit 1
        fi
        if [ -z "$output" ]; then
          echo "cli-help-smoke-${cmd}: -help produced no output" >&2
          exit 1
        fi
        printf 'cli-help-smoke-%s: rc=%d, %d bytes output\n' "${cmd}" "$rc" "''${#output}" > $out
      '';
in
lib.listToAttrs (
  map (cmd: {
    name = "cli-help-smoke-${cmd}";
    value = mkSmoke cmd;
  }) cmdNames
)
