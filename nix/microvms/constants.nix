# nix/microvms/constants.nix
#
# Architecture and VM-runtime constants.
#
# To add a new architecture later: append to `supportedArchs` and add a matching
# entry to `architectures`. The rest of the microvm/ tree consumes this purely
# data-driven.
#
{
  # v1: x86_64-linux only. io_uring (kernel 6.0+) and netlink work on all three
  # arches; adding aarch64/riscv64 is one line here + an architectures entry.
  supportedArchs = [ "x86_64" ];

  # Polling cadence used by lifecycle scripts (seconds between probes)
  pollInterval = 2;

  architectures = {
    x86_64 = {
      hostname = "xtcp2-vm-x86_64";
      qemuMachine = "pc";
      qemuCpu = null; # null => microvm.nix selects -enable-kvm -cpu host
      useKvm = true;
      mem = 1024;
      # memVector is used by the "vector" flavor of the microvm. Vector
      # (~120 MB RSS) plus MinIO (~80 MB) plus the Arrow/parquet working set
      # require headroom above the 1 GiB baseline.
      #
      # Avoid exactly 2048 — microvm.nix #171: QEMU hangs at boot when memory
      # is exactly 2 GiB. 2304 (2.25 GiB) sidesteps that and leaves slack.
      memVector = 2304;
      # memTcpStress is used by sink="tcp-stress". The flavor runs
      # dockerd + N container instances of oci-xtcp2-tcp-stress + xtcp2
      # + an in-VM Prometheus server scraping xtcp2's /metrics. With
      # 20 containers + Prometheus's ~150 MiB RSS + 12h of TSDB working
      # set, 3072 MiB leaves clear headroom for a long-running session.
      memTcpStress = 3072;
      # memClickPipe is used by sink="clickhouse-pipeline". ClickHouse
      # 25.x easily consumes 2 GiB just to handle the kafka-engine
      # consume + parse + materialize-view path during a soak. With
      # 3 GiB total (the tcp-stress budget) ClickHouse gets OOM-killed
      # within a few minutes. 6144 MiB gives headroom for: ClickHouse
      # (~2.5 GiB peak), Redpanda (~700 MiB), dockerd (~150 MiB),
      # xtcp2 (~150 MiB), and the kernel/page cache.
      memClickPipe = 6144;
      # memClickPipeParquet is used by sink="clickhouse-pipeline-parquet"
      # (mixed flavor). Adds to memClickPipe's footprint:
      #   * a SECOND xtcp2 instance (~500 MiB; tracks the same ns set
      #     as the primary independently)
      #   * MinIO server + bucket data (~300 MiB for the 2h soak's
      #     8 k×60 KiB working set; grows with time)
      # The first 2h run with 6144 MiB peaked ClickHouse against its
      # 3500 MiB container cap (222 MEMORY_LIMIT_EXCEEDED errors,
      # kafka_engine MV blocked). 12288 MiB lets ClickHouse breathe
      # while keeping headroom for MinIO accumulation over multi-hour
      # runs. Pairs with a higher `--memory=` on the clickhouse
      # container below.
      memClickPipeParquet = 12288;
      vcpu = 2;
      serialPort = 12055;
      virtioPort = 12056;
      promPort = 9088;
      grpcPort = 8889;
    };
  };
}
