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
  xdp2,
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
      src
      giouring
      ;
    vendorHash = versions.goVendorHash;
  };
  inherit (goMods) vendoredSource;

  # OCI image(s) — three variants in lockstep with the Go build variants.
  containers = import ./containers {
    inherit
      pkgs
      lib
      src
      binaries
      ;
  };
  ipmetaBootstrapArtifact = import ./ipmeta-bootstrap {
    inherit pkgs lib;
    ipmetaBootstrapTool = binaries.ipmeta-bootstrap;
    lockFile = "${toString src}/nix/ipmeta-bootstrap-lock.json";
  };
  containersWithIpmetaBootstrap =
    let
      allBootstrapContainers = import ./containers {
        inherit
          pkgs
          lib
          src
          binaries
          ipmetaBootstrapArtifact
          ;
        daemonAttrSuffix = "-bootstrap";
        daemonTagSuffix = "-bootstrap";
      };
      isUsefulBootstrapImage =
        n:
        n == "oci-xtcp2-bootstrap"
        || n == "oci-xtcp2-debug-bootstrap"
        || n == "oci-xtcp2-stripped-bootstrap"
        || lib.hasSuffix "-asn-bootstrap" n
        || lib.hasSuffix "-enrich-bootstrap" n;
    in
    lib.filterAttrs (n: _v: isUsefulBootstrapImage n) allBootstrapContainers;

  # Protobuf FileDescriptorSet for the XtcpFlatRecord schema. Kept for
  # external consumers that want the .desc without standing up the whole
  # microvm (built and exposed below as the `xtcp-flat-record-desc`
  # package).
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
    ipfeedCollectorPackage = binaries."ipfeed-collector";
    ipmetaBootstrapTool = binaries.ipmeta-bootstrap;
    xtcp2CoverPackage = binaries.xtcp2-cover;
    tcpStressImage = containers.oci-xtcp2-tcp-stress;
  };

  # Static analysis + audit checks
  checks = import ./checks {
    inherit
      pkgs
      lib
      src
      vendoredSource
      binaries
      xdp2
      ;
  };

  # Behavioral test runners
  tests = import ./tests {
    inherit
      pkgs
      lib
      src
      vendoredSource
      binaries
      microvms
      ;
  };

  # Dev shell
  devshell = import ./devshell.nix { inherit pkgs; };

  # Proto plumbing
  protos = import ./protos { inherit pkgs src; };

  # Pedantic code-quality aggregator: runs every static-analysis tool +
  # custom audit, never short-circuits, emits a single markdown report.
  qualityReport = import ./quality-report {
    inherit
      pkgs
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

  # Reproducible nlmon-based capture of real rtnetlink DUMP replies for the
  # pkg/xtcpnl testdata harness. Invoked via
  # `nix run .#capture-netlink-fixtures` from the repo root; see the file
  # header for the filtering/versioning rationale.
  captureNetlinkFixtures = import ./capture-netlink-fixtures.nix { inherit pkgs; };

  # Asks the upstream remotes where `main` actually is and reports how far
  # behind each pin in nix/upstream-pins.json has fallen. A RUNNER rather than
  # a check for the same reason captureNetlinkFixtures is one: the `nix flake
  # check` sandbox has no network, so it cannot answer "has upstream moved?".
  # Its hermetic counterpart, checks.upstream-pins, keeps the manifest honest.
  checkUpstreamPins = import ./check-upstream-pins.nix { inherit pkgs; };

  # The five golangci-lint tier helpers (lint-quick / lint /
  # lint-comprehensive / lint-fix / lint-new). They live in their own file
  # because nix/devshell.nix puts the very same derivations on the dev
  # shell's PATH — one definition, so the shell and the flake cannot drift.
  lintTiers = import ./lint-tiers.nix { inherit pkgs; };

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

  focusedQuality = pkgs.writeShellApplication {
    name = "xtcp2-focused-quality";
    runtimeInputs = [
      pkgs.nix
      versions.go
    ];
    text = ''
      set -eu

      if [ ! -f flake.nix ]; then
        echo "focused-quality: must be run from the xtcp2 repo root" >&2
        exit 2
      fi

      # Use path:. for local focused iteration so WIP files that are not yet
      # tracked by git still enter the Nix source. Override for CI/release
      # parity with: XTCP2_FLAKE_REF=. nix run .#focused-quality
      FLAKE_REF=''${XTCP2_FLAKE_REF:-path:.}

      echo "==> building cacheable focused test derivations"
      nix build --accept-flake-config \
        "$FLAKE_REF#test-focused-asn-locality" \
        "$FLAKE_REF#test-focused-goip" \
        "$FLAKE_REF#test-focused-xtcp-enrich"

      echo "==> focused quality checks passed"
    '';
  };

  ociImageSizeReport = pkgs.writeShellApplication {
    name = "xtcp2-oci-size-report";
    runtimeInputs = with pkgs; [
      coreutils
      gawk
      nix
    ];
    text = ''
            set -eu

            mode="default"
            while [ $# -gt 0 ]; do
              case "$1" in
                --default|--slim) mode="default"; shift ;;
                --fat) mode="fat"; shift ;;
                --all) mode="all"; shift ;;
                -h|--help)
                  cat <<'EOF'
      usage: oci-size-report [--default|--slim|--fat|--all]

      Streams docker-loadable OCI tarballs and prints CSV:
        attr,bytes,mib

      Default measures the slim daemon matrix plus standalone images. Use --fat for
      oci-xtcp2{,-debug,-stripped}; use --all for both sets.

      Set XTCP2_FLAKE_REF=. for committed-source parity, or leave it unset to use
      path:. so local WIP files are included.
      EOF
                  exit 0
                  ;;
                *) echo "unknown arg: $1" >&2; exit 2 ;;
              esac
            done

            if [ ! -f flake.nix ]; then
              echo "oci-size-report: must be run from the xtcp2 repo root" >&2
              exit 2
            fi

            FLAKE_REF=''${XTCP2_FLAKE_REF:-path:.}

            fat_attrs=(
              oci-xtcp2 oci-xtcp2-debug oci-xtcp2-stripped
            )
            slim_attrs=(
              oci-xtcp2-min oci-xtcp2-min-asn oci-xtcp2-min-locality oci-xtcp2-min-enrich
              oci-xtcp2-kafka oci-xtcp2-kafka-asn oci-xtcp2-kafka-locality oci-xtcp2-kafka-enrich
              oci-xtcp2-nats oci-xtcp2-nats-asn oci-xtcp2-nats-locality oci-xtcp2-nats-enrich
              oci-xtcp2-nsq oci-xtcp2-nsq-asn oci-xtcp2-nsq-locality oci-xtcp2-nsq-enrich
              oci-xtcp2-valkey oci-xtcp2-valkey-asn oci-xtcp2-valkey-locality oci-xtcp2-valkey-enrich
              oci-xtcp2-s3parquet oci-xtcp2-s3parquet-asn oci-xtcp2-s3parquet-locality oci-xtcp2-s3parquet-enrich
              oci-ipfeed-collector oci-xtcp2client oci-xtcp2ctl oci-xtcp2-tcp-stress
            )

            attrs=()
            case "$mode" in
              default) attrs=("''${slim_attrs[@]}") ;;
              fat) attrs=("''${fat_attrs[@]}") ;;
              all) attrs=("''${fat_attrs[@]}" "''${slim_attrs[@]}") ;;
            esac

            printf 'attr,bytes,mib\n'
            for attr in "''${attrs[@]}"; do
              path=$(nix build --no-link --print-out-paths --accept-flake-config "$FLAKE_REF#$attr")
              bytes=$("$path" | wc -c)
              mib=$(awk -v b="$bytes" 'BEGIN { printf "%.1f", b / 1024 / 1024 }')
              printf '%s,%s,%s\n' "$attr" "$bytes" "$mib"
            done
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
        # Build and run the binary rather than `go run`: `go run` reports
        # "exit status N" and exits 1 regardless (golang/go#26139), which
        # would collapse a coverage-ratchet breach (3) into the generic
        # failure path and print "report may be incomplete" for a report
        # that is in fact complete — the aggregator emits the whole
        # markdown before it evaluates the ratchet. Same fix as
        # nix/quality-report/default.nix.
        QR_BIN=$(mktemp -t quality-report-bin-XXXXXX)
        go build -o "$QR_BIN" ./tools/quality-report
        qr_rc=0
        "$QR_BIN" \
          -raw-dir "$MERGED_RAW" \
          -repo-root . \
          -known-failures ./tools/quality-report/known-failures.txt \
          -coverage-baseline ./docs/coverage-baseline.txt \
          -coverage-max-drop 0.5 \
          -coverage-out "$MERGED" \
          > docs/quality-report.md || qr_rc=$?
        rm -f "$QR_BIN"
        if [ "$qr_rc" -eq 3 ]; then
          echo "WARNING: coverage ratchet breach; report is complete, but the" \
               "merged total is below docs/coverage-baseline.txt"
        elif [ "$qr_rc" -ne 0 ]; then
          echo "WARNING: aggregator exited $qr_rc; report may be incomplete"
        fi
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

  # Aggregator that drives the whole microVM integration suite from one
  # command: `nix run .#integration-all`. KVM is a single slot, so every
  # flavor runs SEQUENTIALLY.
  #
  #   default        the ~12 lifecycle flavors — each self-terminates on
  #                  its XTCP2_SELF_TEST_OVERALL sentinel (minutes each) —
  #                  then the verdict runners, which terminate on a verdict
  #                  of their own rather than on that sentinel.
  #   --soak         additionally run the 6 duration-bounded runners at
  #                  --duration each (soak + the stress/long flavors).
  #   --duration D   per-soak duration (default 1h); ignored without --soak.
  #
  # Each runner already exits 0/1/2 = PASS/FAIL/TIMEOUT, so we just capture
  # the code, keep going, and fold them into a summary + aggregate exit.
  # Binaries are referenced by store path (lib.getExe), so building this
  # derivation builds every VM it drives — no re-entrant `nix run`.
  integrationAll =
    let
      # label → runner derivation. Order = run order.
      lifecycleFlavors = [
        {
          label = "lifecycle";
          drv = microvms.lifecycle.x86_64.fullTest;
        }
        {
          label = "uds-security";
          drv = microvms.lifecycleUdsSecurity.x86_64.fullTest;
        }
        {
          label = "s3parquet";
          drv = microvms.lifecycleS3Parquet.x86_64.fullTest;
        }
        {
          label = "clickhouse-http";
          drv = microvms.lifecycleClickHttp.x86_64.fullTest;
        }
        {
          label = "clickhouse-pipeline";
          drv = microvms.lifecycleClickPipe.x86_64.fullTest;
        }
        {
          label = "valkey";
          drv = microvms.lifecycleValkey.x86_64.fullTest;
        }
        {
          label = "nats";
          drv = microvms.lifecycleNats.x86_64.fullTest;
        }
        {
          label = "nsq";
          drv = microvms.lifecycleNsq.x86_64.fullTest;
        }
        {
          label = "tcp-sink";
          drv = microvms.lifecycleTcpSink.x86_64.fullTest;
        }
        {
          label = "udp-sink";
          drv = microvms.lifecycleUdpSink.x86_64.fullTest;
        }
        {
          label = "unix-sink";
          drv = microvms.lifecycleUnixSink.x86_64.fullTest;
        }
        {
          label = "unixgram-sink";
          drv = microvms.lifecycleUnixgramSink.x86_64.fullTest;
        }
        {
          label = "coverage";
          drv = microvms.lifecycleCoverage.x86_64.fullTest;
        }
        {
          label = "coverage-iouring";
          drv = microvms.lifecycleCoverageIoUring.x86_64.fullTest;
        }
      ];
      soakFlavors = [
        {
          label = "soak";
          drv = microvms.soak.x86_64.runner;
        }
        {
          label = "tcp-stress";
          drv = microvms.tcpStress.x86_64.runner;
        }
        {
          label = "clickhouse-pipeline-stress";
          drv = microvms.clickPipeStress.x86_64.runner;
        }
        {
          label = "s3parquet-long";
          drv = microvms.s3parquetLong.x86_64.runner;
        }
        {
          label = "s3parquet-stress";
          drv = microvms.s3ParquetStress.x86_64.runner;
        }
        {
          label = "s3parquet-lowfreq";
          drv = microvms.s3ParquetLowfreq.x86_64.runner;
        }
      ];
      # Neither a lifecycle flavor nor a soak: it runs to completion in minutes
      # like the first list, but it self-terminates on a parity verdict rather
      # than on XTCP2_SELF_TEST_OVERALL, and there is no xtcp2 daemon in the
      # guest to emit that sentinel. Its own list, so the lifecycle sweep's
      # description stays true of every member of it.
      #
      # It belongs in `integration-all` for the same reason everything else
      # here does: SERIAL_PORT is fixed per arch, so it cannot run concurrently
      # with any other VM, and this is the one command that runs them one at a
      # time. Same 0/1/2 = PASS/FAIL/TIMEOUT contract, so run_job needs no
      # special case.
      verdictFlavors = [
        {
          label = "goip-parity";
          drv = microvms.goipParity.x86_64.runner;
        }
      ];
      toLine = f: "${f.label}\t${lib.getExe f.drv}";
      lifecycleLines = lib.concatStringsSep "\n" (map toLine lifecycleFlavors);
      soakLines = lib.concatStringsSep "\n" (map toLine soakFlavors);
      verdictLines = lib.concatStringsSep "\n" (map toLine verdictFlavors);
    in
    pkgs.writeShellApplication {
      name = "xtcp2-integration-all";
      runtimeInputs = with pkgs; [ coreutils ];
      text = ''
        SOAK=0
        DURATION="1h"
        while [ $# -gt 0 ]; do
          case "$1" in
            --soak) SOAK=1; shift ;;
            --duration) DURATION="$2"; shift 2 ;;
            --duration=*) DURATION="''${1#--duration=}"; shift ;;
            -h|--help)
              printf '%s\n' \
                "usage: integration-all [--soak] [--duration <1h|30m|...>]" \
                "" \
                "  Runs the xtcp2 microVM integration suite SEQUENTIALLY (KVM is single-slot)." \
                "" \
                "  default          the ~12 lifecycle flavors; each self-terminates on its" \
                "                   XTCP2_SELF_TEST_OVERALL sentinel (minutes each; the" \
                "                   clickhouse-http one can take up to ~20m), then the" \
                "                   verdict runners: goip-parity, which captures an" \
                "                   ip/goip/ip triple per command and compares them in" \
                "                   the guest (~5m)." \
                "  --soak           additionally run the 6 duration-bounded runners:" \
                "                   soak, tcp-stress, clickhouse-pipeline-stress," \
                "                   s3parquet-long, s3parquet-stress, s3parquet-lowfreq." \
                "  --duration <D>   per-soak duration (default 1h). Ignored without --soak." \
                "" \
                "  Prints a PASS/FAIL/TIMEOUT summary; exits non-zero if any flavor failed." \
                "" \
                "  NOT included (finite, not soaks - run directly if wanted):" \
                "    .#microvm-x86_64-clickhouse-pipeline-rate-runner, .#microvm-x86_64-discovery-bench"
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 2 ;;
          esac
        done

        LIFECYCLE_JOBS='${lifecycleLines}'
        SOAK_JOBS='${soakLines}'
        VERDICT_JOBS='${verdictLines}'

        results=""
        overall_rc=0

        run_job() {
          label="$1"
          bin="$2"
          shift 2
          echo ""
          echo "########################################################"
          echo "# integration-all: $label"
          echo "########################################################"
          rc=0
          "$bin" "$@" || rc=$?
          case "$rc" in
            0) verdict="PASS" ;;
            1) verdict="FAIL" ;;
            2) verdict="TIMEOUT" ;;
            *) verdict="ERROR($rc)" ;;
          esac
          if [ "$rc" -ne 0 ]; then overall_rc=1; fi
          results="''${results}''${label}\t''${verdict}\n"
          echo "==> $label: $verdict (rc=$rc)"
        }

        echo "==> integration-all: lifecycle sweep (sequential)"
        while IFS=$'\t' read -r label bin; do
          [ -z "$label" ] && continue
          run_job "$label" "$bin"
        done < <(printf '%s\n' "$LIFECYCLE_JOBS")

        echo ""
        echo "==> integration-all: verdict runners (sequential)"
        while IFS=$'\t' read -r label bin; do
          [ -z "$label" ] && continue
          run_job "$label" "$bin"
        done < <(printf '%s\n' "$VERDICT_JOBS")

        if [ "$SOAK" = "1" ]; then
          echo ""
          echo "==> integration-all: soak/stress runners @ --duration $DURATION each"
          while IFS=$'\t' read -r label bin; do
            [ -z "$label" ] && continue
            run_job "$label" "$bin" --duration "$DURATION"
          done < <(printf '%s\n' "$SOAK_JOBS")
        fi

        echo ""
        echo "================================================"
        echo " integration-all summary"
        echo "================================================"
        printf '%b' "$results" | while IFS=$'\t' read -r label verdict; do
          [ -z "$label" ] && continue
          printf '  %-32s %s\n' "$label" "$verdict"
        done

        echo ""
        if [ "$overall_rc" -eq 0 ]; then
          echo "ALL PASS"
        else
          echo "SOME FAILED"
        fi
        exit "$overall_rc"
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
    # Every OCI image, by prefix rather than by hand. `containers` exports
    # nothing but `oci-*` attrs, so filtering instead of enumerating stops a
    # new flavor from having to be declared both there and here. Covers:
    #   oci-xtcp2{,-debug,-stripped}   fat, every cmd binary
    #   oci-xtcp2-<dest>[-<enrich>]    24 slim single-binary daemons
    #   oci-xtcp2client / oci-xtcp2ctl slim gRPC clients
    #   oci-ipfeed-collector           ASN artifact builder
    #   oci-xtcp2-tcp-stress           TCP_MODE-dispatched stress image
    // (lib.filterAttrs (n: _v: lib.hasPrefix "oci-" n) containers)
    // containersWithIpmetaBootstrap
    # lint-quick / lint / lint-comprehensive / lint-fix / lint-new. `all` is
    # a convenience list for nix/devshell.nix, not a package, so drop it.
    // (removeAttrs lintTiers [ "all" ])
    // {
      regen-protos = protos.regenerate;
      microvm-x86_64 = microvms.vms.x86_64;
      microvm-x86_64-coverage = microvms.vmsCoverage.x86_64;
      microvm-x86_64-coverage-iouring = microvms.vmsCoverageIoUring.x86_64;
      microvm-x86_64-soak = microvms.vmsSoak.x86_64;
      microvm-x86_64-tcp-stress = microvms.vmsTcpStress.x86_64;
      microvm-x86_64-interface-naming = microvms.vmsInterfaceNaming.x86_64;
      microvm-x86_64-ipmeta-bootstrap = microvms.vmsIpmetaBootstrap.x86_64;
      microvm-x86_64-clickhouse-pipeline = microvms.vmsClickPipe.x86_64;
      microvm-x86_64-clickhouse-http = microvms.vmsClickHttp.x86_64;
      microvm-x86_64-clickhouse-pipeline-rate = microvms.vmsClickPipeRate.x86_64;
      microvm-x86_64-clickhouse-pipeline-stress = microvms.vmsClickPipeStress.x86_64;
      microvm-x86_64-clickhouse-pipeline-parquet = microvms.vmsClickPipeParquet.x86_64;
      microvm-x86_64-s3parquet-pipeline = microvms.vmsS3Parquet.x86_64;
      microvm-x86_64-valkey = microvms.vmsValkey.x86_64;
      microvm-x86_64-tcp-sink = microvms.vmsTcpSink.x86_64;
      microvm-x86_64-udp-sink = microvms.vmsUdpSink.x86_64;
      microvm-x86_64-unix-sink = microvms.vmsUnixSink.x86_64;
      microvm-x86_64-unixgram-sink = microvms.vmsUnixgramSink.x86_64;
      microvm-x86_64-nats = microvms.vmsNats.x86_64;
      microvm-x86_64-nsq = microvms.vmsNsq.x86_64;
      microvm-x86_64-s3parquet-long = microvms.vmsS3ParquetLong.x86_64;
      microvm-x86_64-s3parquet-stress = microvms.vmsS3ParquetStress.x86_64;
      microvm-x86_64-s3parquet-lowfreq = microvms.vmsS3ParquetLowfreq.x86_64;
      microvm-x86_64-capcheck-fail = microvms.vmsCapCheckFail.x86_64;
      microvm-x86_64-nlmon-capture = microvms.vmsNlmonCapture.x86_64;
      microvm-x86_64-netlink-dump-capture = microvms.vmsNetlinkDumpCapture.x86_64;
      # Boots nothing under `nix build`: mkVm.nix returns
      # config.microvm.declaredRunner, so this builds the guest closure and
      # runs shellcheck over the runner's script, and that is the whole value
      # of having it here. The VM itself only starts under
      # `nix run .#microvm-x86_64-goip-parity`, which needs /dev/kvm.
      microvm-x86_64-goip-parity = microvms.vmsGoipParity.x86_64;

      # Whole-suite aggregator (see `apps.integration-all`). Buildable so
      # `nix build .#integration-all` builds every VM it drives.
      integration-all = integrationAll;
      oci-size-report = ociImageSizeReport;
      ipmeta-bootstrap-artifact = ipmetaBootstrapArtifact;

      # Protobuf FileDescriptorSet — buildable so users can grab the .desc
      # without standing up the whole microvm.
      xtcp-flat-record-desc = xtcpFlatRecordDescPackage;

      # The netlink layout oracle's binary, re-exported at the pin this repo
      # actually audits with. `checks.proto-audit-netlink` runs it in a fixed
      # shape; this is for reading individual answers out of it by hand, which
      # is how the TCPInfo6_10_3 registry pin was diagnosed:
      #
      #   PROTO_AUDIT_XTCP2_SRC=$PWD nix run .#proto-audit -- \
      #     extract --source xtcp2 --proto NL_Diag_TCPInfo --json
      #
      # Free at eval time and already in the check's closure, so exposing it
      # costs nothing beyond this comment. Both env vars override the stale
      # defaults baked into xdp2's wrapper — see nix/upstream-pins.json.
      proto-audit = xdp2.packages.${pkgs.stdenv.hostPlatform.system}.proto-audit;

      # Test runners exposed as packages so they can be built via
      # `nix build .#test-go-unit`, etc.
      test-go-unit = tests.go-unit;
      test-go-bench = tests.go-bench;
      test-go-race = tests.go-race;
      test-listener-security = tests.listener-security;
      test-proto-deserialize-golden = tests.proto-deserialize-golden;
      test-ipmeta-bootstrap-artifact = tests.ipmeta-bootstrap-artifact;
      test-oci-ipmeta-bootstrap-contents = tests.oci-ipmeta-bootstrap-contents;
      test-focused-asn-locality = tests.focused.focused-asn-locality;
      test-focused-goip = tests.focused.focused-goip;
      test-focused-xtcp-enrich = tests.focused.focused-xtcp-enrich;
      test-microvm-lifecycle-x86_64 = tests.microvm-lifecycle.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-ipmeta-bootstrap = tests.microvm-lifecycle-ipmeta-bootstrap.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-uds-security = microvms.lifecycleUdsSecurity.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-s3parquet = microvms.lifecycleS3Parquet.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-clickhouse-http = microvms.lifecycleClickHttp.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-clickhouse-pipeline = microvms.lifecycleClickPipe.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-valkey = microvms.lifecycleValkey.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-tcp-sink = microvms.lifecycleTcpSink.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-udp-sink = microvms.lifecycleUdpSink.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-unix-sink = microvms.lifecycleUnixSink.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-unixgram-sink = microvms.lifecycleUnixgramSink.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-nats = microvms.lifecycleNats.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-nsq = microvms.lifecycleNsq.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-interface-naming = microvms.lifecycleInterfaceNaming.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-coverage = microvms.lifecycleCoverage.x86_64.fullTest;
      test-microvm-lifecycle-x86_64-coverage-iouring = microvms.lifecycleCoverageIoUring.x86_64.fullTest;

      # Pedantic code-quality report — aggregates every tool's findings.
      quality-report = qualityReport;
      focused-quality = focusedQuality;
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
    capture-netlink-fixtures = {
      type = "app";
      program = "${captureNetlinkFixtures}/bin/xtcp2-capture-netlink-fixtures";
    };
    # Warns when an upstream pin's `main` has moved. Needs network, so it is an
    # app and not a check — see nix/upstream-pins.json for the split.
    check-upstream-pins = {
      type = "app";
      program = "${checkUpstreamPins}/bin/xtcp2-check-upstream-pins";
    };
    # Run the whole microVM integration suite sequentially. Lifecycle sweep
    # by default; `-- --soak [--duration 1h]` adds the duration runners.
    integration-all = {
      type = "app";
      program = "${integrationAll}/bin/xtcp2-integration-all";
    };
    microvm-x86_64-lifecycle = {
      type = "app";
      program = "${microvms.lifecycle.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64";
    };
    microvm-x86_64-lifecycle-uds-security = {
      type = "app";
      program = "${microvms.lifecycleUdsSecurity.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-uds-security";
    };
    microvm-x86_64-lifecycle-s3parquet = {
      type = "app";
      program = "${microvms.lifecycleS3Parquet.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-s3parquet";
    };
    microvm-x86_64-lifecycle-clickhouse-http = {
      type = "app";
      program = "${microvms.lifecycleClickHttp.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-clickhouse-http";
    };
    microvm-x86_64-lifecycle-clickhouse-pipeline = {
      type = "app";
      program = "${microvms.lifecycleClickPipe.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-clickhouse-pipeline";
    };
    microvm-x86_64-lifecycle-valkey = {
      type = "app";
      program = "${microvms.lifecycleValkey.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-valkey";
    };
    microvm-x86_64-lifecycle-tcp-sink = {
      type = "app";
      program = "${microvms.lifecycleTcpSink.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-tcp-sink";
    };
    microvm-x86_64-lifecycle-udp-sink = {
      type = "app";
      program = "${microvms.lifecycleUdpSink.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-udp-sink";
    };
    microvm-x86_64-lifecycle-unix-sink = {
      type = "app";
      program = "${microvms.lifecycleUnixSink.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-unix-sink";
    };
    microvm-x86_64-lifecycle-unixgram-sink = {
      type = "app";
      program = "${microvms.lifecycleUnixgramSink.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-unixgram-sink";
    };
    microvm-x86_64-lifecycle-nats = {
      type = "app";
      program = "${microvms.lifecycleNats.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-nats";
    };
    microvm-x86_64-lifecycle-nsq = {
      type = "app";
      program = "${microvms.lifecycleNsq.x86_64.fullTest}/bin/xtcp2-lifecycle-full-test-x86_64-nsq";
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
    # Phase C: docker-in-VM tcp-stress smoke. Boots a microvm with
    # dockerd, loads oci-xtcp2-tcp-stress, and spawns N containers
    # (default 5, configurable via tcpStressNumContainers in mkVm.nix)
    # each running tcp_server + tcp_client. Each container's sockets
    # live in their own /run/docker/netns/ entry — xtcp2 watches that
    # directory and discovers all of them. The runner sleeps for
    # `--duration` (default 180s) then powers off with a summary.
    microvm-x86_64-tcp-stress = {
      type = "app";
      program = "${microvms.tcpStress.x86_64.runner}/bin/xtcp2-tcp-stress-runner-x86_64";
    };
    # Phase E: boots a microvm that runs redpanda + clickhouse as docker
    # containers, with xtcp2 producing inet_diag records into the kafka
    # topic, clickhouse_kafka_engine consuming them, and a materialized
    # view writing them into xtcp.xtcp_flat_records. The microvm exposes
    # /bin/microvm-run directly so users can poke clickhouse via:
    #   docker exec clickhouse clickhouse-client -q 'SELECT count() FROM xtcp.xtcp_flat_records'
    microvm-x86_64-clickhouse-pipeline = {
      type = "app";
      program = "${microvms.vmsClickPipe.x86_64}/bin/microvm-run";
    };

    # Full end-to-end integration stress test over the clickhouse-pipeline
    # stack. Boots the same VM as `-clickhouse-pipeline` but wraps it in a
    # duration-bounded runner that taps the serial console, prints a live
    # ClickHouse row/netns heartbeat, and asserts records kept flowing
    # end-to-end (rows grew, from ≥2 distinct netns_inode) with no panics
    # and a bounded RSS/thread trend. Default 1h; pass `--duration 24h` for
    # the production stress run or `--keep-alive` to poke it by hand. Not in
    # `nix flake check` — holds a KVM slot for the full duration.
    microvm-x86_64-clickhouse-pipeline-stress = {
      type = "app";
      program = "${microvms.clickPipeStress.x86_64.runner}/bin/xtcp2-clickpipe-stress-runner-x86_64";
    };

    # Runtime-control rate test. Boots the clickhouse-pipeline-rate VM, whose
    # in-VM monitor drives xtcp2ctl through a baseline→fast→revert poll-frequency
    # schedule plus a poll-burst, and asserts the ClickHouse ingest RATE responds
    # and returns. Duration-bounded runner (~9 min); prints the per-phase
    # XTCP2_RATE_* lines and passes only if every verdict is PASS. Not in
    # `nix flake check` — holds a KVM slot.
    microvm-x86_64-clickhouse-pipeline-rate-runner = {
      type = "app";
      program = "${microvms.clickPipeRate.x86_64.runner}/bin/xtcp2-clickpipe-rate-runner-x86_64";
    };

    # Mixed: clickpipe stack (redpanda + clickhouse) plus MinIO and a
    # second xtcp2 instance writing parquet. ClickHouse can then query
    # both the kafka path (xtcp.xtcp_flat_records) and the parquet
    # path (via s3() table function against MinIO at 127.0.0.1:9000).
    # Same boot model as clickhouse-pipeline — `nix run` boots the VM
    # directly; no host-side runner.
    microvm-x86_64-clickhouse-pipeline-parquet = {
      type = "app";
      program = "${microvms.vmsClickPipeParquet.x86_64}/bin/microvm-run";
    };

    # s3parquet flavor: xtcp2 produces Parquet directly into MinIO via the
    # in-VM minio-go client. No Vector. After boot, query the bucket from
    # the host with `mc ls --json local/xtcp2-records --recursive` (or
    # `duckdb` against s3://xtcp2-records/**/*.parquet) on the forwarded
    # MinIO endpoint at http://127.0.0.1:9000.
    microvm-x86_64-s3parquet-pipeline = {
      type = "app";
      program = "${microvms.vmsS3Parquet.x86_64}/bin/microvm-run";
    };

    # On-demand long soak for the s3parquet path. Default 1h with hourly
    # XTCP2_S3PARQUET_HOURLY sentinels; pass `--duration 12h` for the
    # production soak or `--report-interval 60 --duration 5m` for a
    # wiring smoke. Not in `nix flake check` — runs out-of-band like
    # the soak / tcp-stress / clickhouse-pipeline flavors.
    microvm-x86_64-s3parquet-runner = {
      type = "app";
      program = "${microvms.s3parquetLong.x86_64.runner}/bin/xtcp2-s3parquet-runner-x86_64";
    };

    # Parquet→S3 upload stress soak: the s3parquet-long harness (in-VM MinIO +
    # xtcp2 writing parquet) PLUS the tcp-stress load containers (20 × 250
    # sockets) and a ~1h object-retention cleanup. Duration-bounded runner taps
    # the serial console, prints an upload/disk heartbeat, and asserts uploads
    # kept advancing, the MinIO disk stayed healthy, and RSS/threads stayed
    # bounded, with no restarts/panics. Default 1h; pass `--duration 24h` for
    # the production soak or `--keep-alive` to inspect MinIO by hand. Analog of
    # `microvm-x86_64-clickhouse-pipeline-stress`. Not in `nix flake check`.
    microvm-x86_64-s3parquet-stress = {
      type = "app";
      program = "${microvms.s3ParquetStress.x86_64.runner}/bin/xtcp2-s3parquet-stress-runner-x86_64";
    };

    # Parquet→S3 LOW-activity verification: same MinIO + container harness as
    # -s3parquet-stress, but xtcp2 polls once an hour with only 2 sockets per
    # container, so the 63 MiB byte cap is never reached and parquet files
    # finalize purely on the staleness TIMER. Confirms the bucket still fills
    # under low poll frequency + low socket count. Reuses the stress runner;
    # run ~2h (`--duration 2h`) so the ~hourly timer flush is captured (first
    # file typically lands ~30 min in). Not in `nix flake check`.
    microvm-x86_64-s3parquet-lowfreq = {
      type = "app";
      program = "${microvms.s3ParquetLowfreq.x86_64.runner}/bin/xtcp2-s3parquet-stress-runner-x86_64";
    };

    # Namespace-discovery A/B benchmark: boots a root microvm that runs the
    # discovery-bench grid (dir-scan vs /proc-scan, over an N×P sweep) against a
    # real kernel, prints the per-cell JSON, then powers off. Pass
    # `--timeout <sec>` to bound the wait. Not in `nix flake check` — holds a KVM
    # slot for the sweep. Override the grid via DISCO_NS_GRID / DISCO_PID_GRID /
    # DISCO_ITERS on the in-VM service if needed.
    microvm-x86_64-discovery-bench = {
      type = "app";
      program = "${microvms.discoveryBench.x86_64.runner}/bin/xtcp2-discovery-bench-x86_64";
    };

    # rtnetlink EVENT capture: boots a quiet root microvm (no xtcp2 daemon),
    # triggers link up/down + addr add/del + route add/del + neigh add/del on a
    # veth pair, records them off an nlmon device, and writes the pcap plus the
    # `ip -d` sidecars into pkg/xtcpnl/testdata/<guest kernel>/. Run from the
    # repo root. Pass `--timeout <sec>` to bound the wait or `--out <dir>` to
    # override the destination. Not in `nix flake check` — it needs /dev/kvm and
    # it writes to the working tree, neither of which a check can do.
    #
    # Its sibling below captures DUMPS in the same controlled guest. Between
    # them they are the fixture source; `nix run .#capture-netlink-fixtures`
    # captures dumps on the HOST and is now only a diagnostic fallback.
    microvm-x86_64-nlmon-capture = {
      type = "app";
      program = "${microvms.nlmonCapture.x86_64.runner}/bin/xtcp2-nlmon-capture-x86_64";
    };

    # rtnetlink DUMP capture: boots the same quiet root microvm, then drives it
    # over the serial console with expect rather than running a baked-in
    # oneshot. Builds a dummy-only topology in a throwaway netns — chosen so
    # iproute2 issues no side `ll_link_get`, which is what lets the parity
    # harness compare transaction counts positionally — captures each RTM_GET*
    # dump off an nlmon device in that namespace, records the matching `ip -d`
    # and `ip -j` sidecars, and writes the lot into
    # pkg/xtcpnl/testdata/<guest kernel>/dumps/ — a subdirectory, because five
    # sidecar names collide with the event set above and mean something
    # different in each. A second, advisory set (bridge + veth, under
    # dumps/mesh/) carries the IFLA_MASTER / IFLA_LINKINFO replies the clean
    # set deliberately cannot produce; `--skip-mesh` omits it.
    #
    # Run from the repo root. `--timeout <sec>` bounds the wait, `--out <dir>`
    # overrides the destination. Not in `nix flake check`: /dev/kvm plus a write
    # to the working tree. The expect library underneath it IS checked, by
    # `nix build .#checks.x86_64-linux.vm-lib-exp`, against a local pty shell.
    microvm-x86_64-netlink-dump-capture = {
      type = "app";
      program = "${microvms.netlinkDumpCapture.x86_64.runner}/bin/xtcp2-netlink-dump-capture-x86_64";
    };

    # goip/ip netlink parity, Tier C: boots the same quiet root microvm on the
    # same clean topology as the capture above (one definition, in
    # scripts/netlink-topology.exp — D_control only means what it claims if
    # both come off the same objects), captures an ip → goip → ip triple per
    # command with each side's stdout beside its pcap, then runs `goip-parity
    # compare` IN THE GUEST and reports the verdict it reached.
    #
    # The comparison happening in the guest is the point: the captures never
    # have to leave for the answer to exist, so a failure to exfiltrate the
    # evidence cannot mask a parity failure. `--out <dir>` installs the blob
    # anyway, for inspecting a run that went red.
    #
    # `--keep-going` captures every command even after one comes in short;
    # `--no-allowlist` reports suppressed divergences too. Not in `nix flake
    # check`: /dev/kvm, and SERIAL_PORT is fixed so it cannot run concurrently
    # with any other VM.
    microvm-x86_64-goip-parity = {
      type = "app";
      program = "${microvms.goipParity.x86_64.runner}/bin/xtcp2-goip-parity-x86_64";
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
    focused-quality = {
      type = "app";
      program = "${focusedQuality}/bin/xtcp2-focused-quality";
    };
    oci-size-report = {
      type = "app";
      program = "${ociImageSizeReport}/bin/xtcp2-oci-size-report";
    };
  }
  # The five tiers as apps too, so `nix run .#lint-quick` works without
  # entering the dev shell. Each derivation's single binary is named for the
  # attr, so the program path is derivable rather than spelled out.
  // (
    let
      tiers = removeAttrs lintTiers [ "all" ];
    in
    lib.mapAttrs (name: drv: {
      type = "app";
      program = "${drv}/bin/${name}";
    }) tiers
  );

  inherit tests;
}
