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

  # Protobuf FileDescriptorSet for the XtcpFlatRecord schema. Vector loads
  # this at runtime to decode protobuf bytes streamed over the unixgram
  # destination. Built once here so every consumer (vector module, smoke
  # tests, future tooling) reuses the same derivation.
  mkProtoDescSet = import ./lib/mkProtoDescSet.nix { inherit pkgs lib src; };
  xtcpFlatRecordDescPackage = mkProtoDescSet {
    name = "xtcp_flat_record";
    protoFile = "proto/xtcp_flat_record/v1/xtcp_flat_record.proto";
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
    protoDescPackage = xtcpFlatRecordDescPackage;
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

  # Pedantic code-quality aggregator: runs every static-analysis tool +
  # custom audit, never short-circuits, emits a single markdown report.
  qualityReport = import ./quality-report {
    inherit
      pkgs
      lib
      vendoredSource
      src
      ;
  };

  # Per-linter auto-fix helper: lets a fix-sweep produce one commit per
  # linter category instead of one giant mixed-bag commit. Invoked via
  # `nix run .#lint-fix-one -- <linter>` from the repo root.
  #
  # Uses the comprehensive config so Tier-2-only auto-fixable linters
  # (misspell, nakedret) are reachable; `--enable-only` scopes the run
  # to just the requested linter so the diff is clean.
  #
  # `--modules-download-mode=mod` overrides the config's `vendor` setting
  # (which exists for the Nix sandbox's vendoredSource path). Locally the
  # repo has no committed vendor/ tree, so we fall back to module-mode
  # against the user's GOMODCACHE.
  lintFixOne = pkgs.writeShellApplication {
    name = "xtcp2-lint-fix-one";
    runtimeInputs = [ versions.golangci-lint ];
    text = ''
      set -eu
      if [ $# -lt 1 ]; then
        echo "usage: lint-fix-one <linter>" >&2
        echo "  e.g. lint-fix-one gocritic" >&2
        exit 2
      fi
      if [ ! -f flake.nix ]; then
        echo "lint-fix-one: must be run from the xtcp2 repo root" >&2
        exit 2
      fi
      exec golangci-lint run \
        --config .golangci-comprehensive.yml \
        --modules-download-mode=mod \
        --max-issues-per-linter=0 --max-same-issues=0 \
        --enable-only="$1" \
        --fix ./...
    '';
  };

  # User-facing wrapper that refreshes docs/quality-report.md from the
  # current source tree. Invoked via `nix run .#update-quality-report`.
  updateQualityReport = pkgs.writeShellApplication {
    name = "xtcp2-update-quality-report";
    runtimeInputs = with pkgs; [
      coreutils
      git
    ];
    text = ''
      set -eu

      if [ ! -f flake.nix ]; then
        echo "update-quality-report: must be run from the xtcp2 repo root" >&2
        exit 2
      fi

      echo "==> building .#quality-report (Tier 2 takes ~10 min on a cold cache;"
      echo "    Nix-cached on subsequent runs)"
      result=$(nix build --no-link --print-out-paths --accept-flake-config .#quality-report)

      mkdir -p docs
      cp "$result/quality-report.md" docs/quality-report.md
      chmod +w docs/quality-report.md

      echo
      echo "==> wrote docs/quality-report.md"

      if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
        echo
        echo "==> git diff --stat docs/quality-report.md"
        git diff --stat docs/quality-report.md || true
      fi
    '';
  };
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
      microvm-x86_64-vector = microvms.vmsVector.x86_64;

      # Protobuf FileDescriptorSet — buildable so users can grab the .desc
      # without standing up the whole microvm.
      xtcp-flat-record-desc = xtcpFlatRecordDescPackage;

      # Test runners exposed as packages so they can be built via
      # `nix build .#test-go-unit`, etc.
      test-go-unit = tests.go-unit;
      test-go-bench = tests.go-bench;
      test-proto-deserialize-golden = tests.proto-deserialize-golden;
      test-microvm-lifecycle-x86_64 = tests.microvm-lifecycle.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-vector = microvms.lifecycleVector.x86_64.fullTest;

      # Pedantic code-quality report — aggregates every tool's findings.
      quality-report = qualityReport;
    };

  devShells = {
    default = devshell;
  };

  checks = checks // {
    # Microvm lifecycle per arch shows up alongside the rest of the checks.
    microvm-lifecycle-x86_64 = microvms.checks.x86_64;
    microvm-lifecycle-x86_64-vector = microvms.checksVector.x86_64;
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
    microvm-x86_64-lifecycle-vector = {
      type = "app";
      program = "${microvms.lifecycleVector.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-vector";
    };
    quality-report = {
      type = "app";
      program = "${qualityReport}/bin/quality-report";
    };
    update-quality-report = {
      type = "app";
      program = "${updateQualityReport}/bin/xtcp2-update-quality-report";
    };
    lint-fix-one = {
      type = "app";
      program = "${lintFixOne}/bin/xtcp2-lint-fix-one";
    };
  };

  inherit tests;
}
