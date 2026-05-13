# nix/default.nix
#
# Aggregator. Returns the per-system attribute set consumed by flake.nix.
#
{
  pkgs,
  lib,
  src,
  giouring,
  microvm,
  nixpkgs,
}:

let
  versions = import ./versions.nix { inherit pkgs; };

  # Per-binary derivations + xtcp2-all join + default = xtcp2.
  binaries = import ./binaries.nix {
    inherit
      pkgs
      lib
      src
      giouring
      ;
  };

  # Vendored source (used by every check that needs Go deps inside the sandbox).
  goMods = import ./lib/goModules.nix {
    inherit
      pkgs
      lib
      src
      giouring
      ;
    vendorHash = versions.goVendorHash;
  };
  vendoredSource = goMods.vendoredSource;

  # OCI image(s) — three variants in lockstep with the Go build variants.
  containers = import ./containers {
    inherit
      pkgs
      lib
      src
      binaries
      ;
  };

  # MicroVM infrastructure (per supported arch)
  microvms = import ./microvms {
    inherit
      pkgs
      lib
      microvm
      nixpkgs
      ;
    xtcp2Package = binaries.xtcp2;
    xtcp2AllPackage = binaries.xtcp2-all;
  };

  # Static analysis + audit checks
  checks = import ./checks {
    inherit
      pkgs
      lib
      src
      vendoredSource
      binaries
      ;
  };

  # Behavioral test runners
  tests = import ./tests {
    inherit
      pkgs
      lib
      src
      vendoredSource
      microvms
      ;
  };

  # Dev shell
  devshell = import ./devshell.nix { inherit pkgs lib; };

  # Proto plumbing
  protos = import ./protos { inherit pkgs lib src; };
in
{
  packages =
    # Per-binary default-variant attrs (xtcp2, clickhouse_protobuflist, …).
    (removeAttrs binaries [
      "byVariant"
      "joins"
      "xtcp2ByFlavor"
      "xtcp2OnlyByFlavor"
    ])
    // {
      # Build-variant OCI images (fat: every cmd binary).
      inherit (containers)
        oci-xtcp2
        oci-xtcp2-debug
        oci-xtcp2-stripped
        ;
      # Per-flavor OCI images (slim: single xtcp2 binary for one destination).
      inherit (containers)
        oci-xtcp2-min
        oci-xtcp2-kafka
        oci-xtcp2-nats
        oci-xtcp2-nsq
        oci-xtcp2-valkey
        ;

      regen-protos = protos.regenerate;
      microvm-x86_64 = microvms.vms.x86_64;

      # Test runners exposed as packages so they can be built via
      # `nix build .#test-go-unit`, etc.
      test-go-unit = tests.go-unit;
      test-go-bench = tests.go-bench;
      test-proto-deserialize-golden = tests.proto-deserialize-golden;
      test-microvm-lifecycle-x86_64 = tests.microvm-lifecycle.x86_64.fullTest;
    };

  devShells = {
    default = devshell;
  };

  checks = checks // {
    # Microvm lifecycle per arch shows up alongside the rest of the checks.
    microvm-lifecycle-x86_64 = microvms.checks.x86_64;
  };

  apps = {
    regen-protos = {
      type = "app";
      program = "${protos.regenerate}/bin/regen-protos";
    };
    microvm-x86_64-lifecycle = {
      type = "app";
      program = "${microvms.lifecycle.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64";
    };
  };

  inherit tests;
}
