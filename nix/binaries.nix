# nix/binaries.nix
#
# Enumerates the buildable `cmd/<name>/` entries and produces derivations for
# every {binary} × {variant} × {destination-flavor} × {enrichment-flavor} cell
# relevant to its cmd.
#
# Variant axis (debug / default / stripped) is in versions.nix → buildVariants
# and affects ldflags + strip. Applies to all cmds.
#
# Destination-flavor axis (full / min / kafka / nats / nsq / valkey / s3parquet)
# is in versions.nix → destinationFlavors and affects build tags. Only applies to
# `xtcp2` and `ns` — the other 8 cmds don't import pkg/xtcp so destinations
# are irrelevant to them.
#
# Enrichment-flavor axis (none / asn / locality / enrich) is in versions.nix →
# enrichmentFlavors and likewise affects build tags. Same scope as the
# destination axis. These are the two heavyweight enrichers (pkg/ipasn drags in
# parquet-go; pkg/localnet drags in bart), gated so the slim flavors stay slim.
#
# Top-level exports (those that show up in `nix flake show .#packages`):
#   <cmd>                         default variant, full destinations, all enrichers
#   xtcp2-debug                   main xtcp2, debug variant, full, all enrichers
#   xtcp2-stripped                main xtcp2, stripped variant, full, all enrichers
#   xtcp2-min                     main xtcp2, default variant, stdlib only, no enrichers
#   xtcp2-kafka                   main xtcp2, default variant, kafka only, no enrichers
#   xtcp2-nats / -nsq / -valkey   ditto for nats / nsq / valkey
#   xtcp2-s3parquet               main xtcp2, default variant, s3parquet only
#   xtcp2-<dest>-asn              …plus the ASN enricher
#   xtcp2-<dest>-locality         …plus the locality enricher
#   xtcp2-<dest>-enrich           …plus both
#   xtcp2-full-<enrich>           full destination set × each enrichment set
#   xtcp2-all                     symlinkJoin of every binary, full, all enrichers
#   xtcp2-all-debug               symlinkJoin, debug variant, full
#   xtcp2-all-stripped            symlinkJoin, stripped variant, full
#   byVariant / joins             internal nested attrsets used by containers/
#
# Note the deliberate asymmetry: the fat `xtcp2-all*` joins and every plain
# `<cmd>` attr keep ALL enrichers (mkGoBinary's `enrichments ? null` default),
# while the slim per-destination attrs default to NONE. The fat images are the
# "everything" images; the slim ones are where opting in matters.
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
  # Release version stamped into main.version → the record's daemon_version.
  # Sourced from the repo-root ./VERSION file (hand-bumped semver) so a pure
  # nix build has a real version without needing git in the sandbox. The fleet
  # build (runpod/xtcp2) can still override commit/date/version explicitly.
  version ? lib.fileContents ../VERSION,
}:

let
  versions = import ./versions.nix { inherit pkgs; };
  mkGoBinary = import ./lib/mkGoBinary.nix { inherit pkgs lib giouring; };

  binaryNames = [
    "clickhouse_http_insert_protobuflist"
    "clickhouse_protobuflist"
    "clickhouse_protobuflist_db"
    # goip is not a fleet binary: it is a read-only ip(8) subset that exists to
    # exercise pkg/xtcpnl's breadth (docs/netlink/coverage-expansion.md). It is
    # listed here anyway, because this list is what puts a name into xtcp2-all
    # below and therefore into every microVM flavor via mkVm.nix — which is
    # where the goip-parity harness has to run it, beside the pinned `ip`.
    "goip"
    "ipfeed-collector"
    "kafka_to_clickhouse"
    "ns"
    "nsTest"
    "register_schema"
    "xtcp2"
    "xtcp2client"
    "xtcp2ctl"
    "xtcp2_kafka_client"
  ];

  variantNames = builtins.attrNames versions.buildVariants;

  # Attr-name suffix for an enrichment flavor. The `none` cell keeps the
  # historical unsuffixed name (xtcp2-min, xtcp2-s3parquet, …) so existing
  # references stay valid; the other cells append their flavor name.
  flavorSuffix = enrich: lib.optionalString (enrich != "none") "-${enrich}";

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

  # xtcp2 destination × enrichment flavors: only built in the default variant,
  # since debug/stripped × per-flavor would explode the eval surface for
  # marginal value. Users wanting `xtcp2-kafka-stripped` can call mkGoBinary
  # directly.
  #
  # Two-level: xtcp2ByFlavor.<dest>.<enrich>. The `none` enrichment cell is
  # what the historical `xtcp2-<dest>` attrs point at, so the slim flavors are
  # slim again — before the enrichment build tags, pkg/ipasn dragged parquet-go
  # into every one of them including `min`.
  xtcp2ByFlavor = lib.mapAttrs (
    _dest: destList:
    lib.mapAttrs (
      _enrich: enrichList:
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
        enrichments = enrichList;
      }
    ) versions.enrichmentFlavors
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
      # Include the tools/ helpers (tcp_server, tcp_client) only in the
      # default variant join — they're test utilities, not production.
      # Building them in every variant just for the sake of join symmetry
      # would explode the eval surface for no benefit.
      #
      # ipfeed-collector is excluded on the same opt-in principle as the
      # enrichment axis: it is a standalone daemon that builds the ASN Parquet
      # artifact on its own schedule, not something the xtcp2 daemon invokes,
      # and at ~24.9 MB it was 12.7% of the fat image. Anyone who wants it has
      # `nix build .#ipfeed-collector` or the slim `oci-ipfeed-collector`
      # image, which is how it is meant to be deployed (sidecar / separate
      # unit). It stays in binaryNames, so that attr and the cli-help-smoke
      # check are unaffected.
      paths =
        lib.attrValues (removeAttrs byVariant.${variant} [ "ipfeed-collector" ])
        ++ lib.optionals (variant == "default") (lib.attrValues toolBinaries);
    };

  joins = lib.genAttrs variantNames joinVariant;

  # Per-flavor single-binary join: a derivation containing only the xtcp2
  # binary for that {destination, enrichment} cell. Used by the per-flavor
  # OCI images. Same two-level shape as xtcp2ByFlavor.
  xtcp2OnlyByFlavor = lib.mapAttrs (
    dest: byEnrich:
    lib.mapAttrs (
      enrich: drv:
      pkgs.symlinkJoin {
        name = "xtcp2-only-${dest}${flavorSuffix enrich}-${version}";
        paths = [ drv ];
      }
    ) byEnrich
  ) xtcp2ByFlavor;

  # Test-utility binaries that live under tools/ rather than cmd/. Built
  # through the same mkGoBinary machinery via the subPath override so the
  # microvm soak / tcp-stress flavors can pull them in via xtcp2AllPackage.
  toolBinaryNames = [
    "tcp_server"
    "tcp_client"
    # discovery-bench: namespace-discovery A/B benchmark (dir-scan vs /proc
    # inode scan). Rides xtcp2AllPackage into the soak/tcp-stress/discovery-bench
    # microvms so its root-only `grid` mode can run against a real kernel.
    "discovery-bench"
    # idiag-extprobe: verifies against the real kernel that inet_diag_req_v2's
    # idiag_ext bitmask gates which INET_DIAG_* attributes are returned ("what we
    # ask for is what we get"). Rides xtcp2AllPackage into the clickhouse-pipeline
    # microvm, where the self-test runs it as the IDIAG_EXT_PROBE check.
    "idiag-extprobe"
  ];
  toolBinaries = lib.genAttrs toolBinaryNames (
    name:
    mkGoBinary {
      inherit
        name
        src
        commit
        date
        version
        ;
      subPath = "tools/${name}";
      variant = "default";
    }
  );

  # Default-variant attrs (every cmd → default-variant derivation).
  defaultBinaries = byVariant.default;

  # Flatten the two-level {dest}.{enrich} matrix into top-level attrs:
  #   xtcp2-min, xtcp2-min-asn, xtcp2-min-locality, xtcp2-min-enrich, …
  #
  # The `none` enrichment cell is elided from the name so the historical slim
  # attrs (xtcp2-min, xtcp2-kafka, …) keep pointing at a no-enrichment build.
  # `full` is the exception: it always spells its enrichment out, because a
  # bare `xtcp2-full` would read as a synonym for the plain `xtcp2` attr when
  # in fact they differ (`xtcp2` has every enricher, dest=full/enrich=none has
  # none). 7 destinations × 4 enrichment sets = 28 attrs.
  flavorAttrs = lib.listToAttrs (
    lib.concatMap (
      dest:
      map (enrich: {
        name = if dest == "full" then "xtcp2-full-${enrich}" else "xtcp2-${dest}${flavorSuffix enrich}";
        value = xtcp2ByFlavor.${dest}.${enrich};
      }) (builtins.attrNames versions.enrichmentFlavors)
    ) (builtins.attrNames versions.destinationFlavors)
  );

  # Coverage-instrumented xtcp2: `-cover` build flag plus `-coverpkg` set
  # to the in-scope namespace. Writes Go coverage data to $GOCOVERDIR on
  # clean exit. Consumed by the wave 10 microvm coverage harness; not
  # exposed by default for production use.
  #
  # `destinations = [ ]` builds the stdlib-only flavor (null/udp/unix/
  # unixgram) — same as host `go test ./...` without dest_kafka/dest_nats/
  # dest_nsq/dest_valkey build tags. Keeping the block universe in sync
  # with host tests lets the VM profile merge cleanly with host coverage
  # without introducing build-tag-gated blocks that drag the total down.
  #
  # `enrichments = [ ]` is there for exactly the same reason: untagged host
  # tests don't compile the enrich_asn / enrich_locality files either, so
  # including them here would add blocks the host profile can never cover.
  xtcp2-cover = mkGoBinary {
    name = "xtcp2";
    inherit
      src
      commit
      date
      version
      ;
    variant = "default";
    destinations = [ ];
    enrichments = [ ];
    coverage = true;
    coverPkg = "github.com/randomizedcoder/xtcp2/...";
  };
in
defaultBinaries
// flavorAttrs
// {
  default = defaultBinaries.xtcp2;

  # Build-variant axis for xtcp2.
  xtcp2-debug = byVariant.debug.xtcp2;
  xtcp2-stripped = byVariant.stripped.xtcp2;

  # Coverage-instrumented xtcp2 for the microvm coverage harness.
  inherit xtcp2-cover;

  # Joined builds.
  xtcp2-all = joins.default;
  xtcp2-all-debug = joins.debug;
  xtcp2-all-stripped = joins.stripped;

  # tools/ helper binaries (test utilities, default variant only).
  inherit (toolBinaries)
    tcp_server
    tcp_client
    discovery-bench
    ;

  # Internal nested sets for downstream consumers (containers/).
  inherit
    byVariant
    joins
    xtcp2ByFlavor
    xtcp2OnlyByFlavor
    ;
}
