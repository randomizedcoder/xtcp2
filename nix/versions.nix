# nix/versions.nix
#
# Pinned tool versions for the xtcp2 Nix flake.
#
# Single source of truth — every other module reads from here.
# Changing a version here propagates to dev shell, build derivations, and checks.
#
{ pkgs }:

{
  # Go toolchain — pinned to 1.26.5 for its security fixes. The pinned nixpkgs
  # only packages go_1_26 = 1.26.2, so override the version + source to 1.26.5
  # until nixpkgs catches up (then this can drop back to plain `pkgs.go_1_26`).
  go = pkgs.go_1_26.overrideAttrs (_old: rec {
    version = "1.26.5";
    src = pkgs.fetchurl {
      url = "https://go.dev/dl/go${version}.src.tar.gz";
      hash = "sha256-SVvkvIcXasVnOS5bQRar2YRm0z17SdQedkzMaXay3EI=";
    };
  });

  # protobuf tooling
  inherit (pkgs) buf;
  protoc = pkgs.protobuf; # also provides the protoc builtins (python/pyi/cpp)

  # Local buf codegen plugins (nix-pinned) — used by nix/protos/buf-generate.nix
  # so `buf generate` runs fully offline (no buf-cloud remote plugins). See
  # buf.gen.yaml. grpc-gateway ships protoc-gen-grpc-gateway + protoc-gen-openapiv2;
  # grpc ships grpc_python_plugin + grpc_cpp_plugin.
  inherit (pkgs)
    protoc-gen-go
    protoc-gen-go-grpc
    protoc-gen-go-vtproto
    grpc-gateway
    protoc-gen-dart
    grpc
    ;

  # Static analysis. deadnix (dead Nix bindings) and statix (Nix antipatterns)
  # back nix/checks/{deadnix,statix}.nix — added 2026-09-23 because nothing
  # linted the Nix tree, which is how the dead nix/containers/oci-xtcp2.nix
  # survived unnoticed for months.
  inherit (pkgs)
    golangci-lint
    gosec
    deadnix
    statix
    ;
  nixfmt = pkgs.nixfmt-rfc-style or pkgs.nixfmt;

  # gRPC / proto inspection
  inherit (pkgs) grpcurl;

  # Per-variant build configuration. mkGoBinary picks one by name.
  #
  # Reference: https://words.filippo.io/shrink-your-go-binaries-with-this-one-weird-trick/
  #
  #   debug    — plain `go build` output. Keeps the symbol table and DWARF
  #              debug info. Largest; works directly with delve / `go tool
  #              pprof -symbolize`. Use for development and post-mortems.
  #   default  — `-ldflags "-s -w"`. Drops the symbol table (-s) and DWARF
  #              info (-w). ~25% smaller. Production default; matches the
  #              existing Containerfile.
  #   stripped — default + binutils `strip` over the build outputs. A few
  #              more % off. Smallest. Loses the Go buildid (still readable
  #              via `go version <bin>` because that's a separate note
  #              section preserved by strip).
  buildVariants = {
    debug = {
      extraLdflags = [ ];
      doStrip = false;
      tagSuffix = "-debug";
    };
    default = {
      extraLdflags = [
        "-s"
        "-w"
      ];
      doStrip = false;
      tagSuffix = "";
    };
    stripped = {
      extraLdflags = [
        "-s"
        "-w"
      ];
      doStrip = true;
      tagSuffix = "-stripped";
    };
  };

  buildTags = [
    "netgo"
    "osusergo"
  ];
  cgoEnabled = false;

  # Destination flavors. Each maps to a list of `dest_<scheme>` build tags
  # appended to the binary's build. `null` means "all" — backward-compat
  # default that pulls in every library destination (kafka/nats/nsq/valkey/s3parquet).
  # Stdlib destinations (null/udp/unix/unixgram) are always compiled
  # regardless of this list.
  #
  # See nix/binaries.nix for which flavors are surfaced as top-level attrs.
  destinationFlavors = {
    full = null;
    min = [ ];
    kafka = [ "kafka" ];
    nats = [ "nats" ];
    nsq = [ "nsq" ];
    valkey = [ "valkey" ];
    s3parquet = [ "s3parquet" ];
  };

  # The full destination set, expanded explicitly. mkGoBinary uses this when
  # `destinations = null` is passed (the "full" flavor) so the build tag
  # surface is identical to the explicit `destinations = [ "kafka" "nats" "nsq" "valkey" ]` form.
  allLibraryDestinations = [
    "kafka"
    "nats"
    "nsq"
    "valkey"
    "s3parquet"
  ];

  # Enrichment flavors — the third build axis, alongside buildVariants and
  # destinationFlavors. Each maps to a list of `enrich_<feature>` build tags.
  #
  # These two enrichers are opt-in at COMPILE time, unlike the container /
  # lldp / nic / nsid enrichers which are always compiled and toggled only at
  # runtime. The reason is weight: pkg/ipasn pulls in parquet-go and bart, and
  # without a tag it links into every flavor including `min` — which defeats
  # the whole point of a stdlib-only build. Before the enrichment work,
  # parquet-go reached the daemon only via `//go:build dest_s3parquet`.
  #
  #   none     — neither enricher. The plain slim flavors.
  #   asn      — IP->ASN lookup (pkg/ipasn: parquet-go + bart).
  #   locality — network-locality classification (pkg/localnet: bart).
  #   enrich   — both.
  #
  # Both still default to OFF at runtime (-enrichAsn / -enrichLocality); the
  # tag only decides whether the code is in the binary at all. Asking for an
  # enricher that was not compiled in is a fatal startup error, not a silent
  # no-op — see pkg/xtcp/enrich_core.go.
  enrichmentFlavors = {
    none = [ ];
    asn = [ "asn" ];
    locality = [ "locality" ];
    enrich = [
      "asn"
      "locality"
    ];
  };

  # The full enrichment set, expanded explicitly. mkGoBinary uses this when
  # `enrichments = null` is passed, which is the default — so every existing
  # call site (the per-cmd attrs and the three fat images) keeps every
  # enricher and nothing about them changes.
  allEnrichmentFeatures = [
    "asn"
    "locality"
  ];

  # Go vendor hash. Update by running `nix build .#xtcp2` and pasting the
  # `got:` value from the hash mismatch error. Used by every Nix check that
  # needs deps in the sandbox (see nix/lib/goModules.nix).
  goVendorHash = "sha256-UbE22AXaeUHZ9Y696oamvsbc7GwdeGqX3j+xOoFoo3g=";
}
