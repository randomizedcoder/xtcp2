# nix/binaries.nix
#
# Enumerates the buildable `cmd/<name>/` entries and produces one derivation per
# binary, plus a `xtcp2-all` joined derivation containing every binary.
#
# Not every directory under cmd/ is buildable: grpcurl is README-only, io_uring
# and io_uring_peek are stashed under .not files. The list below tracks the
# `package main` entries that actually compile.
#
{
  pkgs,
  lib,
  src,
  giouring,
  commit ? "nix",
  date ? "1970-01-01-00:00",
  version ? "0.0.0-nix",
}:

let
  mkGoBinary = import ./lib/mkGoBinary.nix { inherit pkgs lib giouring; };

  # Explicit allowlist of buildable cmd/<name>/ entries (verified at design time;
  # if you add a new cmd/<x>/ with `package main`, add it here).
  binaryNames = [
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

  mkOne =
    name:
    mkGoBinary {
      inherit
        name
        src
        commit
        date
        version
        ;
    };

  perBinary = lib.genAttrs binaryNames mkOne;

  # symlinkJoin: a single `out/bin/` containing every binary. Used by the
  # OCI image and the dev shell.
  all = pkgs.symlinkJoin {
    name = "xtcp2-all-${version}";
    paths = lib.attrValues perBinary;
  };
in
perBinary
// {
  inherit all;
  default = perBinary.xtcp2;
}
