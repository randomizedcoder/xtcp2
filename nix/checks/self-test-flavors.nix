# nix/checks/self-test-flavors.nix
#
# Builds the lifecycle self-test script (nix/microvms/self-test.nix) once per
# flavor's set of run*Check flags, mirroring mkVm.nix. writeShellApplication
# runs shellcheck at build time, so a helper that one flavor never calls
# (SC2329) fails here in seconds instead of in that flavor's VM build. That is
# how #206 broke every flavor except minimal and the socket sinks: jsonl_tally
# was defined unconditionally, and only those flavors run checks 5d or 5f.
#
# There is no in-check negative row: shellcheck 0.11 does not report SC2329
# for an unused function in a small script, only in context in the full
# self-test, so a toy stand-in builds fine. The negative case was a mutation
# check instead: with the jsonl_tally gate removed, this check fails.
{ pkgs, lib }:

let
  selfTest = flags: import ../microvms/self-test.nix ({ inherit pkgs; } // flags);

  rows = [
    {
      description = "positive: no optional checks (soak, tcp-stress, coverage)";
      flags = { };
    }
    {
      description = "positive: minimal (5d, daemon jsonl output)";
      flags.runFileOutputCheck = true;
    }
    {
      description = "positive: uds-security";
      flags.runUdsSecurityCheck = true;
    }
    {
      description = "positive: socket sinks (5f, raw socket)";
      flags.runRawSocketCheck = true;
    }
    {
      description = "positive: clickhouse-pipeline";
      flags = {
        runClickhouseCheck = true;
        runEnrichCheck = true;
        runNlProbeCheck = true;
      };
    }
    {
      description = "positive: clickhouse-pipeline-parquet";
      flags = {
        runClickhouseCheck = true;
        runClickhouseParquetCheck = true;
      };
    }
    {
      description = "positive: clickhouse-http";
      flags.runClickhouseHttpCheck = true;
    }
    {
      description = "positive: s3parquet";
      flags.runS3ParquetCheck = true;
    }
    {
      description = "positive: valkey";
      flags.runValkeyCheck = true;
    }
    {
      description = "positive: nats";
      flags.runNatsCheck = true;
    }
    {
      description = "positive: nsq";
      flags.runNsqCheck = true;
    }
    {
      description = "positive: interface-naming";
      flags = {
        runInterfaceNamingCheck = true;
        runAsnCheck = true;
        runLocalityEventsCheck = true;
      };
    }
    {
      description = "positive: ipmeta-bootstrap";
      flags = {
        runIpmetaBootstrapCheck = true;
        runNsInspectCheck = false;
      };
    }
    {
      description = "boundary: the smallest script (ns-inspect and rtnl-events off too)";
      flags = {
        runNsInspectCheck = false;
        runRtnlEventsCheck = false;
      };
    }
  ];

in
pkgs.runCommand "xtcp2-self-test-flavors" { } ''
  ${lib.concatMapStrings (r: ''
    echo "ok   ${r.description} (${selfTest r.flags})"
  '') rows}
  touch $out
''
