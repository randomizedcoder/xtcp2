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
  buf = pkgs.buf;
  protoc = pkgs.protobuf;

  # Static analysis
  golangci-lint = pkgs.golangci-lint;
  gosec = pkgs.gosec;
  nixfmt = pkgs.nixfmt-rfc-style or pkgs.nixfmt;

  # gRPC / proto inspection
  grpcurl = pkgs.grpcurl;

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

  # Go vendor hash. Update by running `nix build .#xtcp2` and pasting the
  # `got:` value from the hash mismatch error. Used by every Nix check that
  # needs deps in the sandbox (see nix/lib/goModules.nix).
  goVendorHash = "sha256-KpZrd1NhcLMEFrNehiGBwt7sKFZlwa+uankR1itYizw=";
}
