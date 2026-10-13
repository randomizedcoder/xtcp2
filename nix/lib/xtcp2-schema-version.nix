# nix/lib/xtcp2-schema-version.nix
#
# The record format epoch, read from the Go constant the daemon stamps into
# every record's schema_version (pkg/xtcp/schema_version.go), so nix code that
# asserts the epoch cannot drift from the daemon.
#
# It did drift: self-test check SCHEMA_VERSION hard-coded epoch 1 and the
# xtcp_flat_records_v1 table, and kept failing every row as "bad" after the
# daemon and the ClickHouse DDL moved to epoch 2.
#
# Only a whole line `const XtcpFlatRecordSchemaVersion = <digits>` counts, so
# the doc comments that mention the name are ignored. Evaluation fails unless
# exactly one such line exists: a rename or a second declaration breaks the
# build instead of yielding a wrong epoch.
{ lib }:
let
  parse =
    src:
    let
      matches = lib.filter (m: m != null) (
        map (builtins.match "const XtcpFlatRecordSchemaVersion = ([0-9]+)") (lib.splitString "\n" src)
      );
    in
    if lib.length matches == 1 then
      lib.toInt (lib.head (lib.head matches))
    else
      throw "xtcp2-schema-version: want exactly one `const XtcpFlatRecordSchemaVersion = <digits>` line, found ${toString (lib.length matches)}";
in
{
  inherit parse;
  current = parse (builtins.readFile ../../pkg/xtcp/schema_version.go);
}
