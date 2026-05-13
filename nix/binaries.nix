# nix/binaries.nix
#
# Enumerates the buildable `cmd/<name>/` entries and produces derivations for
# every {binary} × {variant} cell, where variant ∈ {debug, default, stripped}
# (see versions.nix → buildVariants).
#
# Top-level exports:
#   <cmd>                  — default variant of every binary
#   xtcp2-debug            — main xtcp2 binary, debug variant
#   xtcp2-stripped         — main xtcp2 binary, stripped variant
#   xtcp2-all              — symlinkJoin of every binary, default variant
#   xtcp2-all-debug        — symlinkJoin of every binary, debug variant
#   xtcp2-all-stripped     — symlinkJoin of every binary, stripped variant
#   byVariant              — internal nested set { <variant>.<cmd> = drv }
#                            consumed by containers/ to avoid double work.
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
  versions = import ./versions.nix { inherit pkgs; };
  mkGoBinary = import ./lib/mkGoBinary.nix { inherit pkgs lib giouring; };

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

  variantNames = builtins.attrNames versions.buildVariants;

  # byVariant.<variant>.<cmd> = derivation
  byVariant = lib.genAttrs variantNames (
    variant:
    lib.genAttrs binaryNames (
      name:
      mkGoBinary {
        inherit
          name
          src
          variant
          commit
          date
          version
          ;
      }
    )
  );

  # Joined /bin trees per variant. Used by containers/ + xtcp2-all-* exports.
  join =
    variant:
    let
      suffix = versions.buildVariants.${variant}.tagSuffix;
    in
    pkgs.symlinkJoin {
      name = "xtcp2-all${suffix}-${version}";
      paths = lib.attrValues byVariant.${variant};
    };

  joins = lib.genAttrs variantNames join;

  # Surface the default variant of every binary at the top level (existing
  # `nix build .#xtcp2`, .#clickhouse_protobuflist, etc. keep working).
  defaultBinaries = byVariant.default;
in
defaultBinaries
// {
  default = defaultBinaries.xtcp2;

  # Explicit xtcp2 variants (the user's "x3 builds" — single-binary form).
  xtcp2-debug = byVariant.debug.xtcp2;
  xtcp2-stripped = byVariant.stripped.xtcp2;

  # Joined builds, one per variant. OCI images are built from these.
  xtcp2-all = joins.default;
  xtcp2-all-debug = joins.debug;
  xtcp2-all-stripped = joins.stripped;

  # Nested set for downstream consumers (containers/, anyone else who needs
  # to operate on a single variant uniformly).
  inherit byVariant joins;
}
