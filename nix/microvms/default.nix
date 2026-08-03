# nix/microvms/default.nix
#
# Entry point for xtcp2 microvm infrastructure.
#
# Exports per-arch attribute sets:
#   vms.${arch}                          the runnable minimal microvm
#   lifecycle.${arch}.fullTest           host-side launcher (minimal)
#   checks.${arch}.lifecycle             flake-check-compatible (minimal)
#
# Currently supportedArchs = [ "x86_64" ]. To add another, edit constants.nix.
#
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  xtcp2Package,
  xtcp2AllPackage,
  # Optional: the streamLayeredImage script for oci-xtcp2-tcp-stress.
  # Phase C ("tcp-stress" sink) loads this into the in-VM docker daemon
  # at boot and spawns N containers from it. When null, the tcp-stress
  # flavor attrs are not exposed.
  tcpStressImage ? null,
  # Optional: a coverage-instrumented xtcp2 build (see nix/binaries.nix
  # xtcp2-cover). When non-null, the coverage flavor is exposed. The
  # microvm runs the cover binary with GOCOVERDIR set to a tmpfs path,
  # then the self-test stops xtcp2 to flush counter data and tar+base64s
  # it out via the serial console for the host lifecycle runner to scrape.
  xtcp2CoverPackage ? null,
}:

let
  constants = import ./constants.nix;
  microvmLib = import ./lib.nix { inherit pkgs lib constants; };

  mkOne =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "minimal";
    };

  mkOneCoverage =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2AllPackage
        ;
      xtcp2Package = xtcp2CoverPackage;
      sink = "coverage";
    };

  mkOneCoverageIoUring =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2AllPackage
        ;
      xtcp2Package = xtcp2CoverPackage;
      sink = "coverage-iouring";
    };

  mkOneSoak =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "soak";
    };

  mkOneTcpStress =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        tcpStressImage
        ;
      sink = "tcp-stress";
    };

  mkOneClickPipe =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "clickhouse-pipeline";
    };

  # Same redpanda+clickhouse stack as clickhouse-pipeline, but xtcp2 inserts
  # DIRECTLY into ClickHouse over HTTP (bypassing Kafka) — the non-Kafka
  # ingestion path. Self-test emits CLICKHOUSE_HTTP.
  mkOneClickHttp =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "clickhouse-http";
    };

  # Runtime-control rate test: the clickhouse-pipeline stack + a steady
  # 100-connection tcp load + an in-VM monitor that drives xtcp2ctl and
  # asserts the ClickHouse ingest rate responds. Host runner is duration-
  # bounded (mkClickPipeRateRunner). No tcp-stress image needed.
  mkOneClickPipeRate =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "clickhouse-pipeline-rate";
    };

  # Full end-to-end integration stress test: clickhouse-pipeline stack +
  # the tcp-stress socket-load containers. Needs tcpStressImage (to
  # docker-load and spawn the load containers) in addition to the pipeline.
  mkOneClickPipeStress =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        tcpStressImage
        ;
      sink = "clickhouse-pipeline-stress";
    };

  # Mixed: clickhouse-pipeline + MinIO + a second xtcp2 instance
  # writing parquet so ClickHouse can query both paths.
  mkOneClickPipeParquet =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "clickhouse-pipeline-parquet";
    };

  mkOneS3Parquet =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "s3parquet";
    };

  # valkey lifecycle flavor: native in-VM Valkey server + pre-subscribed
  # consumer; xtcp2 PUBLISHes to the pub/sub channel and the self-test proves
  # records are consumed back.
  mkOneValkey =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "valkey";
    };

  # socket-sink lifecycle flavors: xtcp2 streams jsonl over the raw
  # tcp/udp/unix/unixgram dest to an in-VM ncat receiver; the self-test
  # validates the received records + the per-scheme send counter (RAW_SOCKET).
  # Native, no docker/broker.
  mkOneSocketSink =
    sinkName: arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = sinkName;
    };
  mkOneTcpSink = mkOneSocketSink "tcp-sink";
  mkOneUdpSink = mkOneSocketSink "udp-sink";
  mkOneUnixSink = mkOneSocketSink "unix-sink";
  mkOneUnixgramSink = mkOneSocketSink "unixgram-sink";

  # nats lifecycle flavor: native in-VM NATS server + pre-subscribed consumer.
  mkOneNats =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "nats";
    };

  # nsq lifecycle flavor: native in-VM nsqd + an nsq_tail consumer.
  mkOneNsq =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "nsq";
    };

  mkOneS3ParquetLong =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "s3parquet-long";
    };

  # s3parquet-long + the tcp-stress socket-load containers = the parquet→S3
  # upload analog of clickhouse-pipeline-stress. Needs tcpStressImage (to
  # docker-load and spawn the load containers).
  mkOneS3ParquetStress =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        tcpStressImage
        ;
      sink = "s3parquet-stress";
    };

  # Low-activity counterpart of s3parquet-stress: 1h poll + 2 sockets/container,
  # so parquet files flush on the staleness timer, not the byte cap. Same
  # tcpStressImage (the load containers), reuses mkS3ParquetStressRunner.
  mkOneS3ParquetLowfreq =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        tcpStressImage
        ;
      sink = "s3parquet-lowfreq";
    };

  # Deliberately misconfigured: drops CAP_SYS_ADMIN from xtcp2's
  # capability set so the startup capability check refuses to start
  # the daemon. Used to validate the fail-early diagnostic.
  mkOneCapCheckFail =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "capcheck-fail";
    };

  # discovery-bench: root VM that runs the namespace-discovery A/B grid
  # (tools/discovery-bench -mode grid) against a real kernel, then powers off.
  mkOneDiscoveryBench =
    arch:
    import ./mkVm.nix {
      inherit
        pkgs
        lib
        microvm
        nixpkgs
        arch
        xtcp2Package
        xtcp2AllPackage
        ;
      sink = "discovery-bench";
    };

  vms = lib.genAttrs constants.supportedArchs mkOne;

  vmsCoverage = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneCoverage
  );

  vmsCoverageIoUring = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs mkOneCoverageIoUring
  );

  vmsSoak = lib.genAttrs constants.supportedArchs mkOneSoak;

  vmsTcpStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs mkOneTcpStress
  );

  vmsClickPipe = lib.genAttrs constants.supportedArchs mkOneClickPipe;

  vmsClickHttp = lib.genAttrs constants.supportedArchs mkOneClickHttp;

  vmsClickPipeRate = lib.genAttrs constants.supportedArchs mkOneClickPipeRate;

  vmsClickPipeStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs mkOneClickPipeStress
  );

  vmsClickPipeParquet = lib.genAttrs constants.supportedArchs mkOneClickPipeParquet;

  vmsS3ParquetLong = lib.genAttrs constants.supportedArchs mkOneS3ParquetLong;

  vmsS3ParquetStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs mkOneS3ParquetStress
  );

  vmsS3ParquetLowfreq = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs mkOneS3ParquetLowfreq
  );

  vmsCapCheckFail = lib.genAttrs constants.supportedArchs mkOneCapCheckFail;

  vmsS3Parquet = lib.genAttrs constants.supportedArchs mkOneS3Parquet;

  vmsValkey = lib.genAttrs constants.supportedArchs mkOneValkey;

  vmsTcpSink = lib.genAttrs constants.supportedArchs mkOneTcpSink;
  vmsUdpSink = lib.genAttrs constants.supportedArchs mkOneUdpSink;
  vmsUnixSink = lib.genAttrs constants.supportedArchs mkOneUnixSink;
  vmsUnixgramSink = lib.genAttrs constants.supportedArchs mkOneUnixgramSink;

  vmsNats = lib.genAttrs constants.supportedArchs mkOneNats;

  vmsNsq = lib.genAttrs constants.supportedArchs mkOneNsq;

  vmsDiscoveryBench = lib.genAttrs constants.supportedArchs mkOneDiscoveryBench;

  lifecycle = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vms.${arch};
      # No flavor-specific tokens; baseSentinels already surfaces every
      # check the minimal self-test emits (Check 4+ breadcrumbs included).
    };
  });

  lifecycleValkey = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vmsValkey.${arch};
      suffix = "-valkey";
      # Baseline sentinels plus the valkey consume-back verdict. Valkey boots
      # fast (native server, no docker), but the self-test waits up to ~60 s for
      # the subscriber to accumulate messages, so keep a generous timeout.
      extraSentinels = [ "VALKEY_CONSUME" ];
      timeoutSec = 240;
    };
  });

  # Lifecycle runner for a socket-sink flavor (tcp/udp/unix/unixgram). Baseline
  # sentinels plus the raw-socket verdict. Native (no docker), but the self-test
  # waits for records to stream over the socket + reach the sink.
  mkLifecycleSocketSink =
    vmsAttr: suffixName:
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsAttr.${arch};
        suffix = suffixName;
        extraSentinels = [ "RAW_SOCKET" ];
        timeoutSec = 240;
      };
    });
  lifecycleTcpSink = mkLifecycleSocketSink vmsTcpSink "-tcp-sink";
  lifecycleUdpSink = mkLifecycleSocketSink vmsUdpSink "-udp-sink";
  lifecycleUnixSink = mkLifecycleSocketSink vmsUnixSink "-unix-sink";
  lifecycleUnixgramSink = mkLifecycleSocketSink vmsUnixgramSink "-unixgram-sink";

  lifecycleNats = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vmsNats.${arch};
      suffix = "-nats";
      extraSentinels = [ "NATS_CONSUME" ];
      timeoutSec = 240;
    };
  });

  lifecycleNsq = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vmsNsq.${arch};
      suffix = "-nsq";
      extraSentinels = [ "NSQ_CONSUME" ];
      timeoutSec = 240;
    };
  });

  lifecycleS3Parquet = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vmsS3Parquet.${arch};
      suffix = "-s3parquet";
      # The two s3parquet-specific sentinels alongside the baseline set.
      # 240 s timeout because the worker accumulates rows for several
      # poll cycles before triggering the 1 MiB-threshold finalize.
      extraSentinels = [
        "S3PARQUET_FILES"
        "S3PARQUET_ROWS"
      ];
      timeoutSec = 240;
    };
  });

  # Direct HTTP→ClickHouse lifecycle test: boots the clickhouse-http VM
  # (redpanda + clickhouse + xtcp2 inserting over HTTP) and greps the
  # self-test's CLICKHOUSE_HTTP verdict. Generous timeout: docker image pulls +
  # ClickHouse init + the first HTTP insert landing rows takes several minutes.
  lifecycleClickHttp = lib.genAttrs constants.supportedArchs (arch: {
    fullTest = microvmLib.mkLifecycleFullTest {
      inherit arch;
      vm = vmsClickHttp.${arch};
      suffix = "-clickhouse-http";
      extraSentinels = [ "CLICKHOUSE_HTTP" ];
      timeoutSec = 1200;
    };
  });

  lifecycleCoverage = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsCoverage.${arch};
        suffix = "-coverage";
        scrapeCoverage = true;
        # baseSentinels already surfaces NS_LIFECYCLE + NS_TRAFFIC (Checks
        # 8+9) and every other check, so no extraSentinels needed here.
      };
    })
  );

  lifecycleCoverageIoUring = lib.optionalAttrs (xtcp2CoverPackage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      fullTest = microvmLib.mkLifecycleFullTest {
        inherit arch;
        vm = vmsCoverageIoUring.${arch};
        suffix = "-coverage-iouring";
        scrapeCoverage = true;
      };
    })
  );

  soak = lib.genAttrs constants.supportedArchs (arch: {
    runner = microvmLib.mkSoakRunner {
      inherit arch;
      vm = vmsSoak.${arch};
    };
  });

  s3parquetLong = lib.genAttrs constants.supportedArchs (arch: {
    runner = microvmLib.mkS3ParquetRunner {
      inherit arch;
      vm = vmsS3ParquetLong.${arch};
    };
  });

  tcpStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      runner = microvmLib.mkTcpStressRunner {
        inherit arch;
        vm = vmsTcpStress.${arch};
      };
    })
  );

  discoveryBench = lib.genAttrs constants.supportedArchs (arch: {
    runner = microvmLib.mkDiscoveryBenchRunner {
      inherit arch;
      vm = vmsDiscoveryBench.${arch};
    };
  });

  # Runtime-control rate test runner: boots the clickhouse-pipeline-rate VM,
  # waits for the in-VM monitor's XTCP2_RATE_DONE (or --timeout), and passes
  # only if the ingest rate responded to set-poll-frequency + poll-burst.
  # No tcpStressImage needed (the steady load is the default-ns tcp_server/
  # tcp_client pair, not the load containers).
  clickPipeRate = lib.genAttrs constants.supportedArchs (arch: {
    runner = microvmLib.mkClickPipeRateRunner {
      inherit arch;
      vm = vmsClickPipeRate.${arch};
    };
  });

  # Full end-to-end integration stress test: the dedicated
  # clickhouse-pipeline-stress VM (redpanda + clickhouse + the tcp-stress
  # socket-load containers + xtcp2), wrapped in a duration-bounded host
  # runner that asserts records keep reaching ClickHouse over time from the
  # load containers' namespaces. Gated on tcpStressImage (the load
  # containers need the OCI image). Leaves the interactive
  # `microvm-x86_64-clickhouse-pipeline` boot available unchanged.
  clickPipeStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      runner = microvmLib.mkClickPipeStressRunner {
        inherit arch;
        vm = vmsClickPipeStress.${arch};
      };
    })
  );

  # Parquet→S3 upload stress soak: the s3parquet-stress VM (in-VM MinIO +
  # xtcp2 writing parquet + the tcp-stress load containers), wrapped in a
  # duration-bounded host runner that asserts uploads keep advancing and the
  # MinIO disk stays healthy. Gated on tcpStressImage like clickPipeStress.
  s3ParquetStress = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      runner = microvmLib.mkS3ParquetStressRunner {
        inherit arch;
        vm = vmsS3ParquetStress.${arch};
      };
    })
  );

  # Parquet→S3 low-activity verification: the s3parquet-lowfreq VM (1h poll,
  # 2 sockets/container) wrapped in the SAME host runner as the stress flavor —
  # it asserts uploads still advance (driven by the staleness timer, since the
  # byte cap can't be reached with so few sockets).
  s3ParquetLowfreq = lib.optionalAttrs (tcpStressImage != null) (
    lib.genAttrs constants.supportedArchs (arch: {
      runner = microvmLib.mkS3ParquetStressRunner {
        inherit arch;
        vm = vmsS3ParquetLowfreq.${arch};
        # 2h default so a no-arg run reliably captures the ~hourly timer flush.
        defaultDurationSec = 7200;
      };
    })
  );

  # nix flake check compatible derivations. Builds the launcher (cheap) and
  # invokes the VM. Note: requires KVM access — CI runners without /dev/kvm
  # will need to mark this check as host-only or use --keep-going.
  checks = lib.genAttrs constants.supportedArchs (
    arch:
    pkgs.runCommand "xtcp2-microvm-lifecycle-${arch}"
      {
        nativeBuildInputs = [ lifecycle.${arch}.fullTest ];
      }
      ''
        xtcp2-lifecycle-full-test-${arch} > $out 2>&1 || (cat $out && exit 1)
      ''
  );

in
{
  inherit
    vms
    vmsCoverage
    vmsCoverageIoUring
    vmsSoak
    vmsTcpStress
    vmsClickPipe
    vmsClickHttp
    vmsClickPipeRate
    vmsClickPipeStress
    vmsClickPipeParquet
    vmsS3Parquet
    vmsValkey
    vmsTcpSink
    vmsUdpSink
    vmsUnixSink
    vmsUnixgramSink
    vmsNats
    vmsNsq
    vmsS3ParquetLong
    vmsS3ParquetStress
    vmsS3ParquetLowfreq
    vmsCapCheckFail
    vmsDiscoveryBench
    s3parquetLong
    discoveryBench
    clickPipeRate
    clickPipeStress
    s3ParquetStress
    s3ParquetLowfreq
    lifecycle
    lifecycleClickHttp
    lifecycleS3Parquet
    lifecycleValkey
    lifecycleTcpSink
    lifecycleUdpSink
    lifecycleUnixSink
    lifecycleUnixgramSink
    lifecycleNats
    lifecycleNsq
    lifecycleCoverage
    lifecycleCoverageIoUring
    soak
    tcpStress
    checks
    ;
}
