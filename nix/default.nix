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
    xtcp2CoverPackage = binaries.xtcp2-cover;
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
  coverageMerge = import ./coverage-merge.nix { inherit pkgs; };

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
  #
  # With --with-microvm, additionally:
  #   1. Boot the coverage-instrumented microvm via
  #      `nix run .#microvm-x86_64-lifecycle-coverage` and scrape the
  #      Go coverage data dump from its serial console.
  #   2. Merge the VM profile with the host-only profile produced by
  #      .#quality-report via `nix run .#coverage-merge`.
  #   3. Re-run the quality-report aggregator binary with the merged
  #      profile through the new -coverage-out flag (no Nix rebuild
  #      needed for the merge step).
  # Result: the headline coverage % in docs/quality-report.md
  # reflects io_uring + real netlink + namespace paths the host
  # sandbox can't exercise.
  updateQualityReport = pkgs.writeShellApplication {
    name = "xtcp2-update-quality-report";
    runtimeInputs = with pkgs; [
      coreutils
      git
      versions.go
    ];
    text = ''
      set -eu

      WITH_MICROVM=0
      while [ $# -gt 0 ]; do
        case "$1" in
          --with-microvm) WITH_MICROVM=1; shift ;;
          -h|--help)
            echo "usage: update-quality-report [--with-microvm]"
            exit 0
            ;;
          *) echo "unknown arg: $1" >&2; exit 2 ;;
        esac
      done

      if [ ! -f flake.nix ]; then
        echo "update-quality-report: must be run from the xtcp2 repo root" >&2
        exit 2
      fi

      # Step 1: optionally run both microvm-coverage lifecycles
      # (stdlib + iouring) and collect their coverage scrape dirs.
      # Each variant exercises different code paths inside the daemon
      # — the iouring one is the only way to reach the netlinkerIoUring
      # body without a real io_uring-capable kernel.
      VMDIR_STD=""
      VMDIR_IOU=""
      if [ "$WITH_MICROVM" = "1" ]; then
        VMDIR_STD="$(mktemp -d -t xtcp2cov-std-XXXXXX)"
        echo "==> running .#microvm-x86_64-lifecycle-coverage (stdlib)"
        echo "    scrape dir: $VMDIR_STD"
        XTCP2_COVERDIR="$VMDIR_STD" \
          nix run --accept-flake-config .#microvm-x86_64-lifecycle-coverage \
          || echo "WARNING: stdlib microvm lifecycle exited non-zero; coverage may be partial"

        VMDIR_IOU="$(mktemp -d -t xtcp2cov-iou-XXXXXX)"
        echo "==> running .#microvm-x86_64-lifecycle-coverage-iouring"
        echo "    scrape dir: $VMDIR_IOU"
        XTCP2_COVERDIR="$VMDIR_IOU" \
          nix run --accept-flake-config .#microvm-x86_64-lifecycle-coverage-iouring \
          || echo "WARNING: iouring microvm lifecycle exited non-zero; coverage may be partial"

        n_std=$(find "$VMDIR_STD" -type f 2>/dev/null | wc -l)
        n_iou=$(find "$VMDIR_IOU" -type f 2>/dev/null | wc -l)
        echo "==> microvm coverage files: stdlib=$n_std iouring=$n_iou"
        if [ "$n_std" -eq 0 ] && [ "$n_iou" -eq 0 ]; then
          echo "WARNING: no coverage files scraped from either VM; falling back to host-only"
          WITH_MICROVM=0
        fi
      fi

      echo "==> building .#quality-report (Tier 2 takes ~10 min on a cold cache;"
      echo "    Nix-cached on subsequent runs)"
      result=$(nix build --no-link --print-out-paths --accept-flake-config .#quality-report)

      mkdir -p docs

      if [ "$WITH_MICROVM" = "1" ]; then
        echo "==> merging host + microvm coverage profiles"
        MERGED=$(mktemp -t merged-cov-XXXXXX.out)
        # nix run .#coverage-merge handles host+VM merge: produces a
        # mode-set profile keyed on the host's block universe with
        # counts upgraded where any VM run also covered the block.
        # Multiple --vm-dir flags are union-merged via covdata textfmt.
        MERGE_ARGS=(--host "$result/raw/coverage.out" --out "$MERGED")
        n_std=$(find "$VMDIR_STD" -type f 2>/dev/null | wc -l)
        n_iou=$(find "$VMDIR_IOU" -type f 2>/dev/null | wc -l)
        if [ "$n_std" -gt 0 ]; then MERGE_ARGS+=(--vm-dir "$VMDIR_STD"); fi
        if [ "$n_iou" -gt 0 ]; then MERGE_ARGS+=(--vm-dir "$VMDIR_IOU"); fi
        nix run --accept-flake-config .#coverage-merge -- "''${MERGE_ARGS[@]}" >&2

        # Copy raw/ to a writable temp dir so we can re-run the
        # aggregator with the merged profile in-place. The Nix store
        # path is read-only; we need a writable rawDir for the
        # -coverage-out regeneration step.
        MERGED_RAW=$(mktemp -d -t merged-raw-XXXXXX)
        cp -r "$result/raw/." "$MERGED_RAW/"
        chmod -R +w "$MERGED_RAW"

        echo "==> re-running quality-report with merged profile"
        go run ./tools/quality-report \
          -raw-dir "$MERGED_RAW" \
          -repo-root . \
          -known-failures ./tools/quality-report/known-failures.txt \
          -coverage-baseline ./docs/coverage-baseline.txt \
          -coverage-max-drop 0.5 \
          -coverage-out "$MERGED" \
          > docs/quality-report.md \
          || echo "WARNING: aggregator exited non-zero; report may be incomplete"
      else
        cp "$result/quality-report.md" docs/quality-report.md
      fi

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

      # Phase B: TCP-stress container for the multi-container test
      # harness. Run with TCP_MODE=server|client|both, TCP_COUNT,
      # TCP_SLEEP, TCP_PADS, TCP_CONNECT, TCP_BIND env vars.
      inherit (containers) oci-xtcp2-tcp-stress;

      regen-protos = protos.regenerate;
      microvm-x86_64 = microvms.vms.x86_64;
      microvm-x86_64-vector = microvms.vmsVector.x86_64;
      microvm-x86_64-coverage = microvms.vmsCoverage.x86_64;
      microvm-x86_64-coverage-iouring = microvms.vmsCoverageIoUring.x86_64;
      microvm-x86_64-soak = microvms.vmsSoak.x86_64;

      # Protobuf FileDescriptorSet — buildable so users can grab the .desc
      # without standing up the whole microvm.
      xtcp-flat-record-desc = xtcpFlatRecordDescPackage;

      # Test runners exposed as packages so they can be built via
      # `nix build .#test-go-unit`, etc.
      test-go-unit = tests.go-unit;
      test-go-bench = tests.go-bench;
      test-go-race = tests.go-race;
      test-proto-deserialize-golden = tests.proto-deserialize-golden;
      test-microvm-lifecycle-x86_64 = tests.microvm-lifecycle.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-vector = microvms.lifecycleVector.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-coverage = microvms.lifecycleCoverage.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-coverage-iouring = microvms.lifecycleCoverageIoUring.x86_64.fullTest;

      # Pedantic code-quality report — aggregates every tool's findings.
      quality-report = qualityReport;
    }
    # Per-flavor + per-package test targets. The two imports above each
    # return an attrset whose keys already start with `test-` so they
    # merge straight into the flake's packages namespace.
    // (lib.filterAttrs (n: _v: lib.hasPrefix "test-" n) tests);

  devShells = {
    default = devshell;
  };

  checks =
    checks
    // {
      # Microvm lifecycle per arch shows up alongside the rest of the checks.
      microvm-lifecycle-x86_64 = microvms.checks.x86_64;
      microvm-lifecycle-x86_64-vector = microvms.checksVector.x86_64;

      # Race-detector + per-flavor builds. These run as part of
      # `nix flake check` so a flavor-tag regression (e.g. dest_kafka
      # stops compiling because of a new import cycle) or a fresh
      # data race fails CI immediately. The per-package targets are
      # NOT here — quality-report already runs the all-default-tags
      # case, so per-package would be duplicate work.
      test-go-race = tests.go-race;
    }
    // (lib.filterAttrs (n: _v: lib.hasPrefix "test-go-flavor-" n) tests);

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
    microvm-x86_64-lifecycle-coverage = {
      type = "app";
      program = "${microvms.lifecycleCoverage.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-coverage";
    };
    microvm-x86_64-lifecycle-coverage-iouring = {
      type = "app";
      program = "${microvms.lifecycleCoverageIoUring.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-coverage-iouring";
    };
    # On-demand long-running soak. Default 1h; pass --duration 24h (or
    # 5m for a smoke run) to override. Not wired into `nix flake check`
    # because it holds a KVM slot for the full duration.
    microvm-x86_64-soak = {
      type = "app";
      program = "${microvms.soak.x86_64.runner}/bin/xtcp2-soak-x86_64";
    };
    quality-report = {
      type = "app";
      program = "${qualityReport}/bin/quality-report";
    };
    update-quality-report = {
      type = "app";
      program = "${updateQualityReport}/bin/xtcp2-update-quality-report";
    };
    coverage-merge = {
      type = "app";
      program = "${coverageMerge}/bin/xtcp2-coverage-merge";
    };
    lint-fix-one = {
      type = "app";
      program = "${lintFixOne}/bin/xtcp2-lint-fix-one";
    };
  };

  inherit tests;
}
