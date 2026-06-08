# nix/binaries.nix
#
# Enumerates the buildable `cmd/<name>/` entries and produces derivations for
# every {binary} × {variant} × {destination-flavor} cell relevant to its cmd.
#
# Variant axis (debug / default / stripped) is in versions.nix → buildVariants
# and affects ldflags + strip. Applies to all cmds.
#
# Destination-flavor axis (full / min / kafka / nats / nsq / valkey) is in
# versions.nix → destinationFlavors and affects build tags. Only applies to
# `xtcp2` and `ns` — the other 8 cmds don't import pkg/xtcp so destinations
# are irrelevant to them.
#
# Top-level exports (those that show up in `nix flake show .#packages`):
#   <cmd>                         default variant, full destination set
#   xtcp2-debug                   main xtcp2, debug variant, full
#   xtcp2-stripped                main xtcp2, stripped variant, full
#   xtcp2-min                     main xtcp2, default variant, stdlib only
#   xtcp2-kafka                   main xtcp2, default variant, kafka only
#   xtcp2-nats / -nsq / -valkey   ditto for nats / nsq / valkey
#   xtcp2-all                     symlinkJoin of every binary, full
#   xtcp2-all-debug               symlinkJoin, debug variant, full
#   xtcp2-all-stripped            symlinkJoin, stripped variant, full
#   byVariant / joins             internal nested attrsets used by containers/
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
  flavorNames = builtins.attrNames versions.destinationFlavors;

  # byVariant.<variant>.<cmd>: every cmd in every build variant, with the
  # default (full) destination set. Used by the OCI image fan-out and as the
  # backing store for the top-level <cmd> attrs.
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

  # xtcp2 destination flavors: only built in the default variant, since
  # debug/stripped × per-flavor would explode the eval surface for marginal
  # value. Users wanting `xtcp2-kafka-stripped` can call mkGoBinary directly.
  xtcp2ByFlavor = lib.mapAttrs (
    flavor: destList:
    mkGoBinary {
      name = "xtcp2";
      inherit
        src
        commit
        date
        version
        ;
      variant = "default";
      destinations = destList;
    }
  ) versions.destinationFlavors;

  # Joined /bin trees per build variant (full destination set). OCI images
  # and the xtcp2-all-* attrs consume these.
  joinVariant =
    variant:
    let
      suffix = versions.buildVariants.${variant}.tagSuffix;
    in
    pkgs.symlinkJoin {
      name = "xtcp2-all${suffix}-${version}";
      paths = lib.attrValues byVariant.${variant};
    };

  joins = lib.genAttrs variantNames joinVariant;

  # Per-flavor single-binary join: a derivation containing only the xtcp2
  # binary for that flavor. Used by the per-flavor OCI images.
  xtcp2OnlyByFlavor = lib.mapAttrs (
    flavor: drv:
    pkgs.symlinkJoin {
      name = "xtcp2-only-${flavor}-${version}";
      paths = [ drv ];
    }
  ) xtcp2ByFlavor;

  # Default-variant attrs (every cmd → default-variant derivation).
  defaultBinaries = byVariant.default;
in
defaultBinaries
// {
  default = defaultBinaries.xtcp2;

  # Build-variant axis for xtcp2.
  xtcp2-debug = byVariant.debug.xtcp2;
  xtcp2-stripped = byVariant.stripped.xtcp2;

  # Destination-flavor axis for xtcp2 (default build variant).
  xtcp2-min = xtcp2ByFlavor.min;
  xtcp2-kafka = xtcp2ByFlavor.kafka;
  xtcp2-nats = xtcp2ByFlavor.nats;
  xtcp2-nsq = xtcp2ByFlavor.nsq;
  xtcp2-valkey = xtcp2ByFlavor.valkey;

  # Joined builds.
  xtcp2-all = joins.default;
  xtcp2-all-debug = joins.debug;
  xtcp2-all-stripped = joins.stripped;

  # Internal nested sets for downstream consumers (containers/).
  inherit
    byVariant
    joins
    xtcp2ByFlavor
    xtcp2OnlyByFlavor
    ;
}
