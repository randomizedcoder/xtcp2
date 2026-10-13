# nix/checks/xtcp2-schema-version.nix
#
# Pure-evaluation check for nix/lib/xtcp2-schema-version.nix: the epoch is
# read from the Go constant, and only from its declaration line. The last row
# checks the ClickHouse initdb DDL has the current epoch's table and MV, so an
# epoch bump without the matching DDL fails here rather than in a VM run.
{ pkgs, lib }:

let
  sv = import ../lib/xtcp2-schema-version.nix { inherit lib; };
  sqlDir = ../../build/containers/clickhouse/initdb.d/sql;
  tableSql = builtins.readFile "${sqlDir}/xtcp_xtcp_flat_records.sql";
  mvSql = builtins.readFile "${sqlDir}/xtcp_xtcp_flat_records_mv.sql";
  n = toString sv.current;
  throws = src: !(builtins.tryEval (sv.parse src)).success;
  failures = lib.runTests {
    "test positive: the real Go file" = {
      expr = sv.current;
      expected = 2;
    };
    "test positive: a minimal source" = {
      expr = sv.parse "package xtcp\nconst XtcpFlatRecordSchemaVersion = 7\n";
      expected = 7;
    };
    "test boundary: a multi-digit epoch" = {
      expr = sv.parse "const XtcpFlatRecordSchemaVersion = 12";
      expected = 12;
    };
    "test corner: doc comments that mention the name are ignored" = {
      expr = sv.parse ''
        // Bump XtcpFlatRecordSchemaVersion = 9 when the format changes.
        //	const XtcpFlatRecordSchemaVersion = 8
        const XtcpFlatRecordSchemaVersion = 3
      '';
      expected = 3;
    };
    "test negative: the constant is missing" = {
      expr = throws "package xtcp\n";
      expected = true;
    };
    "test negative: declared twice" = {
      expr = throws "const XtcpFlatRecordSchemaVersion = 2\nconst XtcpFlatRecordSchemaVersion = 3\n";
      expected = true;
    };
    "test negative: not a number" = {
      expr = throws "const XtcpFlatRecordSchemaVersion = schemaV2\n";
      expected = true;
    };
    "test corner: the DDL has the current epoch's table and MV" = {
      expr = {
        table = lib.hasInfix "CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v${n}\n" tableSql;
        mv = lib.hasInfix "CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_v${n}_mv TO xtcp.xtcp_flat_records_v${n}\n" mvSql;
        routes = lib.hasInfix "schema_version = ${n};" mvSql;
      };
      expected = {
        table = true;
        mv = true;
        routes = true;
      };
    };
  };
in
if failures != [ ] then
  throw "xtcp2-schema-version: ${builtins.toJSON failures}"
else
  pkgs.runCommand "xtcp2-schema-version" { } "touch $out"
