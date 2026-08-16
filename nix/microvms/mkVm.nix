# nix/microvms/mkVm.nix
#
# Parameterized NixOS-microvm definition for xtcp2 lifecycle testing.
#
# Mirrors xdp2's mkVm pattern but slimmed for v1:
#   - x86_64-linux only (KVM accelerated)
#   - imports modules/xtcp2-service.nix as the single systemd-unit source
#   - bundles the self-test as a oneshot service triggered after xtcp2
#   - shares /nix/store with the host via 9p
#
# Flavors selected by `sink`:
#   - "minimal" (default): xtcp2 alone, JSONL configFile (currently a no-op
#                          stub; the netlink-readout check tolerates a missing
#                          file). Cheap CI smoke.
#   - "s3parquet":         xtcp2 → MinIO Parquet upload, all inside the VM.
#                          Reuses the minio-bucket-bootstrap module; the xtcp2
#                          daemon talks to MinIO directly via the minio-go
#                          client. Self-test scrapes a single .parquet object
#                          and exits. Lifecycle smoke for CI.
#   - "s3parquet-long":    Same plumbing as "s3parquet" but no self-test
#                          oneshot. A monitor service emits a heartbeat
#                          sentinel each `S3PARQUET_REPORT_INTERVAL` seconds
#                          (default 3600). Pairs with mkS3ParquetRunner for
#                          multi-hour soak runs.
#   - "clickhouse-pipeline", "soak", "tcp-stress", "coverage[-iouring]".
#
{
  pkgs,
  lib,
  microvm,
  nixpkgs,
  arch,
  xtcp2Package,
  xtcp2AllPackage,
  sink ? "minimal",
  # Required when sink == "tcp-stress". The OCI image (streamLayeredImage
  # script) that the in-VM container spawn unit loads via `docker load`.
  tcpStressImage ? null,
}:

let
  constants = import ./constants.nix;
  cfg = constants.architectures.${arch};

  isCoverage = sink == "coverage" || sink == "coverage-iouring";
  isCoverageIoUring = sink == "coverage-iouring";
  isSoak = sink == "soak";
  # tcp-sink = a lightweight lifecycle flavor that proves the raw `tcp`
  # destination end-to-end: xtcp2 streams jsonl records over TCP to an in-VM
  # ncat receiver (dual-stack), and the self-test validates the received
  # records + the destTCP send counter. No docker, no broker.
  # socket-sink flavors: <scheme>-sink proves the raw tcp/udp/unix/unixgram
  # destination end-to-end. xtcp2 streams jsonl records over the dest to a
  # dual-stack ncat receiver, which writes everything to socketSinkFile for the
  # RAW_SOCKET self-test check to validate. No docker, no broker.
  socketSinkScheme =
    if sink == "tcp-sink" then
      "tcp"
    else if sink == "udp-sink" then
      "udp"
    else if sink == "unix-sink" then
      "unix"
    else if sink == "unixgram-sink" then
      "unixgram"
    else
      "";
  isSocketSink = socketSinkScheme != "";
  socketSinkPort = 13001; # inet dests (tcp/udp)
  socketSinkPath = "/run/xtcp2-sink.sock"; # unix dests
  socketSinkFile = "/tmp/xtcp2-socket-sink.out";
  # -dest value per scheme. `localhost` (dual-stack) for inet dests.
  socketSinkDest =
    if socketSinkScheme == "tcp" then
      "tcp:localhost:${toString socketSinkPort}"
    else if socketSinkScheme == "udp" then
      "udp:localhost:${toString socketSinkPort}"
    else
      "${socketSinkScheme}:${socketSinkPath}"; # unix:/path or unixgram:/path
  # ncat receiver invocation per scheme (its stdout is redirected to
  # socketSinkFile by the service). inet dests listen dual-stack on ::
  # (bindv6only=0 also accepts IPv4-mapped); unix dests bind the socket path.
  socketSinkReceiverCmd =
    if socketSinkScheme == "tcp" then
      "${pkgs.nmap}/bin/ncat --listen --keep-open :: ${toString socketSinkPort}"
    else if socketSinkScheme == "udp" then
      "${pkgs.nmap}/bin/ncat --udp --listen --keep-open :: ${toString socketSinkPort}"
    else if socketSinkScheme == "unix" then
      "${pkgs.nmap}/bin/ncat --unixsock --listen --keep-open ${socketSinkPath}"
    else
      # unixgram: ncat can't LISTEN on a unix DATAGRAM socket (`--unixsock
      # --udp --listen` fails with "connect: Invalid argument"), so use socat's
      # UNIX-RECV, which binds a unix datagram socket and streams received
      # datagrams to stdout (-u = unidirectional recv→stdout).
      "${pkgs.socat}/bin/socat -u UNIX-RECV:${socketSinkPath} -";
  # Send-side Writes counter (function, variable) per scheme. udp is labelled
  # under the legacy Inetdiager/udpWrites; the rest are destXxx/Writes.
  socketSinkMetricFn =
    if socketSinkScheme == "udp" then
      "Inetdiager"
    else if socketSinkScheme == "tcp" then
      "destTCP"
    else if socketSinkScheme == "unix" then
      "destUnix"
    else
      "destUnixGram";
  socketSinkMetricVar = if socketSinkScheme == "udp" then "udpWrites" else "Writes";
  # minimal = the lifecycle correctness gate. Unlike soak (which shares the
  # basic `-dest null` args), minimal writes jsonl to a file so the self-test
  # can validate the daemon's serialized output content (OUTPUT_CONTENT check).
  isMinimal = sink == "minimal";
  isTcpStress = sink == "tcp-stress";
  # clickhouse-pipeline = tcp-stress + redpanda + clickhouse + kafka
  # destination. Same docker setup but two extra containers + xtcp2
  # configured with -dest kafka:localhost:19092 so the records flow
  # through the same pipeline as the production compose.
  isClickPipe = sink == "clickhouse-pipeline";
  # clickhouse-http = the same redpanda+clickhouse stack, but xtcp2 inserts
  # DIRECTLY into ClickHouse over its HTTP interface (localhost:18123),
  # bypassing Kafka entirely — the non-Kafka ingestion path, which had no e2e
  # coverage. redpanda stays up (idle) so the kafka-engine table is happy.
  isClickHttp = sink == "clickhouse-http";
  # clickhouse-pipeline + s3parquet mixed: existing redpanda + clickhouse
  # stack PLUS in-VM MinIO + a second xtcp2 instance writing parquet.
  # ClickHouse can query the parquet files via the s3() table function /
  # an S3-engine table — same VM that runs the kafka path, validating
  # the "operator wants both pipelines on one host" deployment shape.
  isClickPipeParquet = sink == "clickhouse-pipeline-parquet";
  # clickhouse-pipeline-stress = the full end-to-end integration stress
  # test: the clickhouse-pipeline stack (redpanda + clickhouse + kafka +
  # xtcp2) PLUS the tcp-stress load containers (20 × 250 sockets). Unlike
  # the plain clickhouse-pipeline flavor (which only sees the redpanda/
  # clickhouse infra netns), this drives real socket load through the
  # discovery path and validates those records reach ClickHouse.
  isClickPipeStress = sink == "clickhouse-pipeline-stress";
  # s3parquet = MinIO + xtcp2 writing Parquet directly to S3 (lifecycle).
  isS3Parquet = sink == "s3parquet";
  # s3parquet-long = same destination, no self-test, monitor service emits
  # hourly file-count sentinels. Long-soak runner consumes them.
  isS3ParquetLong = sink == "s3parquet-long";
  # capcheck-fail = a deliberately-misconfigured s3parquet-long VM that
  # drops CAP_SYS_ADMIN from the service. xtcp2's startup capability
  # check should refuse to start; the lifecycle test verifies the
  # expected error appears on the serial console.
  isCapCheckFail = sink == "capcheck-fail";
  # s3parquet-stress = the parquet→S3 upload analog of clickhouse-pipeline-stress:
  # xtcp2 writes Parquet to an in-VM MinIO under the SAME tcp-stress load
  # (20 × 250 sockets), bounded to ~1h of retained objects (periodic mc rm),
  # with the disk guard + RSS/thread leak check + a repeatable soak runner.
  isS3ParquetStress = sink == "s3parquet-stress";
  # s3parquet-lowfreq = the LOW-activity counterpart of s3parquet-stress: same
  # parquet→MinIO sink and container load, but xtcp2 polls once an HOUR
  # (-frequency 1h) with only 2 sockets/container. The 63 MiB byte cap is never
  # reached, so parquet files finalize purely on the staleness TIMER
  # (max(1h,30m)=1h) — this flavor verifies the bucket still fills under low
  # poll frequency + low socket count. Lighter than stress (no dedicated disks,
  # no retention).
  isS3ParquetLowfreq = sink == "s3parquet-lowfreq";
  # clickhouse-pipeline-rate = the runtime-control behavioral test: the
  # clickhouse-pipeline stack (redpanda + clickhouse + kafka + xtcp2) PLUS a
  # steady 100-connection tcp_server/tcp_client load (a stable rate
  # denominator). A dedicated in-VM monitor drives xtcp2ctl through a
  # baseline→fast→revert poll-frequency schedule plus a poll-burst, and asserts
  # the RATE of rows arriving in xtcp.xtcp_flat_records responds and returns —
  # end-to-end proof the operator-control CLI changes live daemon behavior.
  isClickPipeRate = sink == "clickhouse-pipeline-rate";
  # discovery-bench = a root VM that runs the namespace-discovery A/B grid
  # (tools/discovery-bench -mode grid) against a real kernel, then powers off.
  # No downstream/dockerd — it only needs ip netns + many cheap processes.
  isDiscoveryBench = sink == "discovery-bench";
  # valkey = a native in-VM Valkey (Redis-protocol) server + a pre-subscribed
  # consumer; xtcp2 PUBLISHes each record to the pub/sub channel and the
  # self-test proves records flow through end-to-end. No docker, no persistence;
  # a lightweight lifecycle flavor (falls through to the default mem budget).
  isValkey = sink == "valkey";
  # nats = a native in-VM NATS server + a pre-subscribed consumer; xtcp2
  # PUBLISHes each record to a NATS subject and the self-test proves records
  # flow through end-to-end. Same shape as the valkey flavor.
  isNats = sink == "nats";
  # nsq = a native in-VM nsqd + an nsq_tail consumer; xtcp2 PUBLISHes each record
  # to an nsq topic and the self-test proves records are consumed (via nsqd's
  # per-channel finish_count in /stats). Same shape as valkey/nats.
  isNsq = sink == "nsq";
  # Convenience predicate — most plumbing (minio module, port forwards,
  # mem budget, daemon args base) is shared.
  isAnyS3Parquet =
    isS3Parquet
    || isS3ParquetLong
    || isCapCheckFail
    || isClickPipeParquet
    || isS3ParquetStress
    || isS3ParquetLowfreq;
  # All flavors that bring up the redpanda + clickhouse docker stack.
  isAnyClickPipe =
    isClickPipe || isClickPipeParquet || isClickPipeStress || isClickPipeRate || isClickHttp;
  # Flavors that spawn the tcp-stress socket-load containers.
  isAnyTcpStressLoad = isTcpStress || isClickPipeStress || isS3ParquetStress || isS3ParquetLowfreq;
  # Anything that needs dockerd inside the VM (the tcp-stress load containers
  # need it too, so the s3parquet stress/lowfreq flavors pull docker in even
  # though their sink is MinIO).
  needsDocker = isTcpStress || isAnyClickPipe || isS3ParquetStress || isS3ParquetLowfreq;
  effectiveMem =
    if isClickPipeParquet then
      # Mixed flavor needs more — clickhouse + redpanda + 2× xtcp2 +
      # MinIO + Pyroscope all in one VM.
      cfg.memClickPipeParquet
    else if isClickPipeStress then
      # Pipeline stack + the tcp-stress load containers in one VM.
      cfg.memClickPipeStress
    else if isS3ParquetStress then
      # MinIO + xtcp2 (parquet) + the tcp-stress load containers, no CH/Redpanda.
      cfg.memS3ParquetStress
    else if isAnyClickPipe then
      cfg.memClickPipe
    else if isAnyS3Parquet then
      cfg.memClickPipe
    else if isTcpStress then
      cfg.memTcpStress
    else if isSoak then
      # nsTest's churn working set (~320 MiB) OOM-loops at the 1024 MiB
      # baseline; give the soak its own larger budget.
      cfg.memSoak
    else if isDiscoveryBench then
      # The grid sweep spawns up to a few thousand processes per cell.
      cfg.memDiscoveryBench
    else
      cfg.mem;

  coverDir = "/var/lib/xtcp2cov";

  selfTest = import ./self-test.nix {
    inherit pkgs lib;
    promPort = cfg.promPort;
    grpcPort = cfg.grpcPort;
    coverageEnabled = isCoverage;
    inherit coverDir;
    # The kafka-oriented rows+errors check (Check 11) runs on the kafka
    # flavors; the http-direct flavor runs its own CLICKHOUSE_HTTP check.
    runClickhouseCheck = isAnyClickPipe && !isClickHttp;
    runClickhouseHttpCheck = isClickHttp;
    runClickhouseParquetCheck = isClickPipeParquet;
    # Enrichment end-to-end checks (ENRICH_NIC / ENRICH_CONTAINER / ENRICH_LLDP
    # / ENRICH_BESTEFFORT_NEGATIVE) run on the plain clickhouse-pipeline flavor,
    # whose xtcp2 carries -enrichContainer/-enrichNic/-enrichLldp and which has
    # dockerd + lldpd + the kafka→ClickHouse path the checks query against.
    runEnrichCheck = isClickPipe;
    # idiag_ext request/response contract probe against the real kernel. Gated to
    # the plain clickhouse-pipeline flavor (same as the enrich checks) so it runs
    # in the routinely-exercised lifecycle-clickhouse-pipeline test.
    runNlProbeCheck = isClickPipe;
    clickhousePassword = clickPipeChPassword;
    runS3ParquetCheck = isS3Parquet;
    runValkeyCheck = isValkey;
    valkeyChannel = valkeyTopic;
    runNatsCheck = isNats;
    natsSubject = natsTopic;
    runNsqCheck = isNsq;
    nsqTopic = nsqTopicName;
    nsqChannel = nsqChannelName;
    # minimal flavor only: validate the daemon's jsonl file-dest output.
    runFileOutputCheck = isMinimal;
    inherit fileOutputPath;
    # tcp-sink flavor only: validate records received over the raw TCP dest.
    runRawSocketCheck = isSocketSink;
    inherit
      socketSinkScheme
      socketSinkFile
      socketSinkMetricFn
      socketSinkMetricVar
      ;
  };

  # Default monitor cadence for the s3parquet-long flavor. 60 s is fast
  # enough for short smoke runs to see file growth, and the host-side
  # runner aggregates the per-minute sentinels into hourly summaries for
  # long-running tests. Override via the systemd env at boot if you want
  # genuine hourly cadence (e.g. for a 12 h soak that doesn't need
  # per-minute resolution).
  s3ParquetReportIntervalDefault = 60;

  # tcp_server/tcp_client tunables for the soak flavor. They share the
  # same port base (cmd/tcp_server/tcp_server.go startPort = 4000), so
  # `tcpServerCount` listeners → 4000..4000+N-1, and `tcpClientCount`
  # clients dial those same ports. Setting client < server is fine
  # (extra listeners stay idle); setting client > server means the
  # excess clients fail to dial.
  soakTcpServerCount = 100;
  soakTcpClientCount = 100;
  soakTcpClientSleep = "5s";
  soakTcpPads = 2048;
  soakTcpConnect = "127.0.0.1";

  # Phase C tcp-stress tunables. N containers each running TCP_MODE=both
  # (server + client) with M sockets, so the total visible-to-xtcp2
  # socket count is roughly numContainers * socketsPerContainer * 2
  # (server + accepted-conn pair per port). Each container gets its
  # own netns courtesy of docker's bridge network, exercising xtcp2's
  # /proc/<pid>/ns/net scan discovery (Method B) under real socket load.
  # Keep numContainers modest so the per-VM resource budget stays sane.
  tcpStressNumContainers = 20;
  # 250/container × 20 containers × 2 (server + accepted-conn) ≈ 10k sockets
  # visible to xtcp2 — a 2.5× bump from the original 100 for the 24h
  # clickhouse-pipeline stress run. Stays within the clickhouse-pipeline
  # memory budget (see clickPipeClickhouseMemory / memClickPipe).
  # The s3parquet-lowfreq flavor drops to 2/container (~40 sockets across the
  # 20 netns) so the parquet byte cap is never reached and the staleness TIMER
  # is the sole flush driver — the whole point of that flavor.
  tcpStressSocketsPerContainer = if isS3ParquetLowfreq then 2 else 250;
  tcpStressClientSleep = "5s";
  tcpStressPads = 1024;

  # Phase E clickhouse-pipeline tunables. Image tags are deliberately
  # exposed here so a future tag bump doesn't require touching the
  # ExecStart strings deep in the systemd unit defs.
  # ClickHouse 25.x disables network access for the `default` user
  # when no password is configured (returns code 194 REQUIRED_PASSWORD).
  # Set a known password so host-side queries via the forwarded :18123
  # work without further setup. Override at deploy time if you don't
  # want a hardcoded local-dev password.
  clickPipeChPassword = "xtcp";
  # ClickHouse container memory cap. Default 3500m for the plain
  # clickpipe flavor (12h-validated). The mixed flavor adds MinIO +
  # a second xtcp2 + nsTest churn and needs more — see constants.nix
  # `memClickPipeParquet` for the OOM history. Bumped 12000m → 14000m
  # after the 4h soak showed CH parked at ~10.45 GiB MemoryTracking
  # against the internal cap derived from the container limit (88 %
  # of 12000m = 10.55 GiB) and the kafka_engine's per-batch 131 MiB
  # decode buffer allocation getting rejected ~2 %/min. 14000m raises
  # the internal cap to ~12.3 GiB; VM at 16 GiB leaves ~2 GiB headroom
  # for the rest of the stack.
  clickPipeClickhouseMemory = if isClickPipeParquet then "14000m" else "3500m";

  clickPipeRedpandaImage = "docker.redpanda.com/redpandadata/redpanda:v25.1.7";
  # ClickHouse uses MAJOR.MINOR.PATCH.SUBPATCH versioning; the precise
  # numeric tag for an LTS line at any given point is hard to predict, so
  # we use a floating "<major>.<minor>-alpine" tag which Docker Hub
  # repoints at the latest patch. Pin to a precise tag for reproducibility
  # once you've validated which patch works.
  #
  # Kafka-engine consumer-stall history under heavy ingest:
  #   * 25.3  → stalls at ~19-21k rows (max.poll.interval.ms rebalance loop)
  #   * 25.8  → ~3x better (~60k) but the consumer still silently detaches
  #   * 26.7  → trying the latest stable to see if the detach is fixed
  clickPipeClickhouseImage = "clickhouse/clickhouse-server:26.7-alpine";
  clickPipeKafkaTopic = "xtcp";
  # Bind the SQL initdb scripts from the repo into a nix-store path that
  # the clickhouse container can mount read-only. Anchored to the build/
  # tree the existing docker-compose uses, so the same SQL drives both.
  # The full directory is needed — script 020 creates the xtcp database
  # which scripts 035/040/050 depend on. Skipping it produces
  # `Database xtcp does not exist (UNKNOWN_DATABASE)` and code 81 exit.
  # ClickHouse's kafka_engine table is declared with
  # kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord' — clickhouse
  # looks for that file under /var/lib/clickhouse/format_schemas/. Mirror
  # the production Dockerfile by mounting a tiny derivation containing
  # just the .proto in there.
  clickPipeProtoSchemas = pkgs.runCommand "xtcp2-clickhouse-format-schemas" { } ''
    mkdir -p $out
    cp ${../../proto/xtcp_flat_record/v1/xtcp_flat_record.proto} \
       $out/xtcp_flat_record.proto
    chmod -R a+rX $out
  '';

  clickPipeInitdb = pkgs.runCommand "xtcp2-clickhouse-initdb" { } ''
    mkdir -p $out
    # Copy 005..050 plus the sql/ subdir. The README script
    # `000_clickhouse_runs_all_dot_sh_and_dot_sql_files` is just a
    # comment artifact, but copying everything is simpler than picking.
    cp -r ${../../build/containers/clickhouse/initdb.d}/005_start.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/010_clear_tracking_files.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/020_drop_database_xtcp.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/035_recreate_xtcp_xtcp_flat_records.sql.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/040_recreate_xtcp_xtcp_flat_records_kafka.sql.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/050_recreate_xtcp_xtcp_flat_records_mv.sql.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/055_recreate_xtcp_xtcp_flat_records_errors_mv.sql.sh $out/
    cp -r ${../../build/containers/clickhouse/initdb.d}/sql $out/sql
    ${lib.optionalString isClickPipeStress ''
      # Stress flavor ONLY: shrink the records-table TTL from the canonical
      # 1 MONTH (in sql/xtcp_xtcp_flat_records.sql, unchanged for prod) to
      # 1 HOUR, so a 24h stress run keeps the MergeTree bounded to ~1h of
      # data instead of growing unbounded under heavy ingest. Runs after the
      # table is created (035) and before the MV (050). The clickhouse
      # entrypoint executes *.sql files in the initdb dir in name order.
      printf '%s\n' "ALTER TABLE xtcp.xtcp_flat_records MODIFY TTL toDateTime(timestamp_ns) + INTERVAL 1 HOUR DELETE;" \
        > $out/045_stress_ttl_1h.sql
    ''}
    # The init scripts write tracking files into out/; pre-create it
    # so they don't fail on the first run. Same as the compose flow.
    mkdir -p $out/out
    chmod -R a+rX $out
  '';

  # config.d overrides mounted into /etc/clickhouse-server/config.d/.
  # Disables the chatty internal observability tables (latency_log,
  # metric_log, etc.) whose background merges trip the per-server
  # max-memory cap under heavy ingest. See the XML for details.
  clickPipeConfigD = pkgs.runCommand "xtcp2-clickhouse-config-d" { } ''
    mkdir -p $out
    cp ${../../build/containers/clickhouse/config.d/disable_chatty_logs.xml} \
       $out/disable_chatty_logs.xml
    cp ${../../build/containers/clickhouse/config.d/limit_memory.xml} \
       $out/limit_memory.xml
    cp ${../../build/containers/clickhouse/config.d/kafka_client_tuning.xml} \
       $out/kafka_client_tuning.xml
    cp ${../../build/containers/clickhouse/config.d/listen.xml} \
       $out/listen.xml
    chmod -R a+rX $out
  '';

  # users.d override: allow the `default` user to connect from any source
  # network. The image restricts it to localhost-inside-container, which blocks
  # the clickhouse-http flavor's host-side xtcp2 (host → docker -p proxy → the
  # container sees the docker gateway IP). Auth is still enforced by
  # CLICKHOUSE_PASSWORD. Mounted read-WRITE (the entrypoint writes the password
  # file default-user.xml into users.d; a read-only mount makes it exit).
  clickPipeUsersD = pkgs.runCommand "xtcp2-clickhouse-users-d" { } ''
    mkdir -p $out
    cp ${../../build/containers/clickhouse/users.d/allow-host.xml} \
       $out/allow-host.xml
    chmod -R a+rX $out
  '';

  # nsTest churn parameters tuned for soak runs. Production nsTest defaults
  # are 1000 initial namespaces + 100ms sleep — which inside a microvm
  # creates an explosive boot-time spike (1000 × `ip netns add` back-to-back
  # before any churn). Soak runs benefit from a smaller initial fill and a
  # bit more breathing room between iterations so the daemon's /proc-scan
  # discovery + reconcile + nsAdd path runs continuously without ever being
  # completely idle. Sized empirically — increase if you want harsher loading.
  # Soak workload sizing. The mixed clickpipe-parquet flavor runs
  # TWO xtcp2 instances tracking the same namespaces independently
  # (kafka path + parquet path), so each in-flight ns handler costs
  # ~2× the OS threads vs a single-xtcp2 flavor. Cut both knobs
  # roughly in half to keep each instance well under its 2000-thread
  # cap with headroom for the inevitable cleanup lag from the
  # persistent-connection model.
  soakInitialNs = if isClickPipeParquet then 100 else 200;
  soakChurnSleep = if isClickPipeParquet then "250ms" else "100ms";
  # Per-ns persistent loopback connections. 100 conns × 200 ns =
  # 20,000 ESTABLISHED sockets across the working set. With 5 payload
  # sizes × 4 send intervals = 20 distinct io profiles, the TCPInfo
  # readout xtcp2 sees has real spread instead of a single shape.
  # Mixed flavor uses 25 (matched smaller ns count + slower churn
  # for the two-xtcp2-instance overhead).
  soakConnsPerNs = if isClickPipeParquet then 25 else 100;
  # Period (seconds) between /metrics scrapes. 60s lines up with most
  # default Prometheus scrape intervals.
  soakScrapePeriodSec = 60;
  soakMetricsLog = "/var/log/xtcp2-soak-metrics.log";

  soakChurnScript = pkgs.writeShellApplication {
    name = "xtcp2-soak-churn";
    runtimeInputs = with pkgs; [
      coreutils
      iproute2
    ];
    text = ''
      # Run nsTest with reduced initial-fill + slightly longer churn
      # sleep so a 1h / 24h run doesn't drown the journal in
      # `ip netns add` lines before any actual churn happens.
      #
      # -conns ${toString soakConnsPerNs}: after each `ip netns add`,
      # nsTest enters the new ns, brings lo UP, opens N persistent
      # loopback TCP connections with varied io profiles, and keeps
      # them running for the ns's lifetime. xtcp2 then sees 2N
      # ESTABLISHED sockets per ns in every poll with real spread
      # across TCPInfo segs/bytes/rtt (different payload sizes +
      # send intervals per conn). When the churn loop deletes the
      # ns, nsTest signals the per-ns generator to close cleanly
      # before `ip netns del` runs.
      exec ${xtcp2AllPackage}/bin/nsTest \
        -initial ${toString soakInitialNs} \
        -sleep ${soakChurnSleep} \
        -conns ${toString soakConnsPerNs}
    '';
  };

  # (Retired) Shell-based ns-traffic driver. Replaced by the
  # in-process `-traffic` flag on nsTest (cmd/nsTest/nsTest.go),
  # which avoids the `ip netns exec` race that left this version
  # producing files=0 over a 12h soak. Kept around as a reference
  # for future ad-hoc injectors but no longer wired up.
  soakNsTrafficScript_UNUSED = pkgs.writeShellApplication {
    name = "xtcp2-soak-ns-traffic";
    runtimeInputs = with pkgs; [
      bash # ip netns exec resolves `bash` via PATH; must be in runtimeInputs
      coreutils
      iproute2
      nmap # provides ncat
      util-linux
    ];
    text = ''
      # Picks a single ns and runs a quick listener+connect pair inside
      # its loopback. The listener exits when the client disconnects
      # (-l --recv-only --send-only style), so the function returns
      # cleanly without leaving orphans even if a process gets stuck —
      # the outer `timeout` is the backstop.
      # Single-quoted heredoc-style body for `bash -c '…'`: the inner
      # script intentionally does NOT expand $vars in the parent shell;
      # it runs inside `ip netns exec` and only references its own
      # locals. Annotated so shellcheck doesn't flag it.
      # shellcheck disable=SC2016
      inject_one() {
        local nsname=$1
        timeout 3 ip netns exec "$nsname" bash -c '
          # Bring up lo so 127.0.0.1 is routable inside the ns. (Most
          # nsTest-created namespaces have lo DOWN by default; without
          # this every connection would EHOSTUNREACH.) Surface errors
          # to stderr (which is journal+console for this service) so
          # cap/perms problems become visible.
          if ! ip link set lo up 2>&1; then
            echo "ns=$0 ip link set lo up FAILED"
            exit 1
          fi
          # One-shot listener that accepts one connection and exits.
          ncat -l 127.0.0.1 5000 --recv-only --no-shutdown >/dev/null 2>&1 &
          server_pid=$!
          # Brief delay so the listener has socket() + bind() done.
          sleep 0.1
          # Fire a payload at it; this produces ESTABLISHED on both
          # sides for ~50-100 ms, then TIME_WAIT — both visible to xtcp2.
          if ! ncat --send-only -w 1 127.0.0.1 5000 < /etc/hostname >/dev/null 2>&1; then
            echo "ns=$0 ncat client FAILED"
            kill $server_pid 2>/dev/null || true
            exit 1
          fi
          wait $server_pid 2>/dev/null || true
        ' "$nsname"
      }

      max_inflight=30
      while true; do
        # Snapshot the current ns list — /run/netns/ can churn out from
        # under a long-running loop, so re-read every cycle. Glob
        # expansion (not ls|grep) keeps shellcheck happy.
        namespaces=()
        for f in /run/netns/ns*; do
          [ -e "$f" ] || continue
          namespaces+=("$(basename "$f")")
        done
        if [ "''${#namespaces[@]}" -eq 0 ]; then
          sleep 0.5
          continue
        fi
        for nsname in "''${namespaces[@]}"; do
          # Block until we have a slot — keeps total fork pressure
          # bounded regardless of ns population.
          while [ "$(jobs -r 2>/dev/null | wc -l)" -ge "$max_inflight" ]; do
            wait -n 2>/dev/null || true
          done
          inject_one "$nsname" &
        done
        wait
        # Brief gap so we don't busy-loop when ns count is small.
        sleep 0.2
      done
    '';
  };

  soakScrapeScript = pkgs.writeShellApplication {
    name = "xtcp2-soak-scrape";
    runtimeInputs = with pkgs; [
      coreutils
      curl
    ];
    text = ''
      # Scrape /metrics on a fixed cadence so the soak run leaves a
      # historical trail of every xtcp_counts / xtcp_histograms value.
      # Each scrape is a JSON-shaped record so jq can post-process later.
      while true; do
        ts=$(date -u +%FT%TZ)
        body=$(curl --silent --fail --max-time 5 \
          "http://127.0.0.1:${toString cfg.promPort}/metrics" \
          | grep '^xtcp_' || true)
        if [ -z "$body" ]; then
          echo "{\"t\":\"$ts\",\"err\":\"scrape_empty\"}"
        else
          # Wrap the raw text in a JSON envelope keyed on the scrape ts.
          printf '{"t":"%s","metrics":' "$ts"
          # Encode the prom text exposition as a JSON string array so the
          # whole record is one valid JSON line per scrape — easy to tail
          # with jq, easy to split.
          printf '%s' "$body" | awk '
            BEGIN { printf "[" }
            { gsub(/\\/, "\\\\"); gsub(/"/, "\\\""); printf (NR>1?",":"") "\"" $0 "\"" }
            END { print "]}" }
          '
        fi
        sleep ${toString soakScrapePeriodSec}
      done
    '';
  };

  vmConfig = ./xtcp2-vm-config.json;

  # Phase C scripts: load the OCI image into the VM's docker daemon at
  # boot, then spin up N containers each running tcp_server + tcp_client.
  # The image arrives as a streamLayeredImage script — pipe it into
  # docker load to materialize it inside the daemon.
  tcpStressLoadScript = pkgs.writeShellApplication {
    name = "xtcp2-tcp-stress-load";
    runtimeInputs = with pkgs; [
      coreutils
      docker
    ];
    text = ''
      # Wait for dockerd's socket to be ready. NixOS' docker.service
      # ordering should already gate us, but a brief readiness loop
      # keeps the boot ordering robust if Type=notify isn't honored.
      for _ in $(seq 1 30); do
        if docker info >/dev/null 2>&1; then break; fi
        sleep 1
      done
      docker info >/dev/null 2>&1 || { echo "FATAL: docker not ready"; exit 1; }

      # The image is a streamLayeredImage script in the nix store. Run
      # it; it streams a tar of the image to stdout, which `docker load`
      # consumes directly.
      ${
        if tcpStressImage != null then
          "${tcpStressImage} | docker load"
        else
          "echo 'no image provided'; exit 1"
      }
    '';
  };

  tcpStressSpawnScript = pkgs.writeShellApplication {
    name = "xtcp2-tcp-stress-spawn";
    runtimeInputs = with pkgs; [
      coreutils
      docker
    ];
    text = ''
      # Spawn N containers, each running TCP_MODE=both with M sockets.
      # No port publishing — each container has its own bridge netns,
      # so the in-container client just dials 127.0.0.1 inside that ns.
      # The point is for xtcp2 to discover each container's netns via the
      # /proc/<pid>/ns/net scan (Method B) and observe its sockets via inet_diag.
      n=${toString tcpStressNumContainers}
      m=${toString tcpStressSocketsPerContainer}
      sleep_dur=${tcpStressClientSleep}
      pads=${toString tcpStressPads}

      echo "spawning $n containers, each with TCP_MODE=both TCP_COUNT=$m"
      for i in $(seq 1 "$n"); do
        # --detach because we want them all live concurrently. Reusing
        # the same image name from `docker load` (xtcp2-tcp-stress:latest).
        # Names stress-1, stress-2, … so cleanup is scriptable.
        if docker run --detach \
            --name "stress-$i" \
            --restart on-failure \
            --env TCP_MODE=both \
            --env "TCP_COUNT=$m" \
            --env "TCP_SLEEP=$sleep_dur" \
            --env "TCP_PADS=$pads" \
            xtcp2-tcp-stress:latest >/dev/null 2>&1; then
          echo "  stress-$i: started"
        else
          echo "  stress-$i: FAILED to start"
        fi
      done
      # Keep the unit alive — it's Type=simple. Tail the logs of one
      # representative container so this service's journal has signal.
      sleep infinity
    '';
  };

  # Phase E clickhouse-pipeline scripts: docker pull → bring up the
  # `xtcp` network + redpanda + clickhouse → connect them. xtcp2 (running
  # on the microvm host, NOT in a container) connects to redpanda via
  # the published external port (localhost:19092) so it can see netns
  # outside the container.
  clickPipeUpScript = pkgs.writeShellApplication {
    name = "xtcp2-clickpipe-up";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      docker
    ];
    text = ''
      # Wait for dockerd's socket to be ready.
      for _ in $(seq 1 30); do
        if docker info >/dev/null 2>&1; then break; fi
        sleep 1
      done
      docker info >/dev/null 2>&1 || { echo "FATAL: docker not ready"; exit 1; }

      # 1) shared network so redpanda + clickhouse see each other by name
      docker network create xtcp --subnet 10.20.0.0/24 2>/dev/null || true

      # 1b) Named volumes — mirror the compose stack so the dirs that
      # the entrypoint chowns are docker-managed (and thus writable
      # with the right ownership from the start). Survives container
      # restarts inside one VM boot; gets wiped on VM reboot because
      # /var/lib/docker is tmpfs-backed in this microvm.
      docker volume create redpanda-0  2>/dev/null || true
      docker volume create clickhouse_db 2>/dev/null || true

      # 2) Pull both images. First boot needs internet (qemu user-mode
      # NAT). After the layers are cached in /var/lib/docker the runner
      # comes up offline.
      echo "pulling ${clickPipeRedpandaImage}"
      docker pull ${clickPipeRedpandaImage} || \
        { echo "FATAL: docker pull redpanda failed"; exit 1; }
      echo "pulling ${clickPipeClickhouseImage}"
      docker pull ${clickPipeClickhouseImage} || \
        { echo "FATAL: docker pull clickhouse failed"; exit 1; }

      # 3) Start redpanda. Mirrors the production compose: internal kafka
      # addr inside the docker net, external kafka addr published as
      # localhost:19092 on the VM host so xtcp2 can dial it.
      docker rm -f redpanda-0 2>/dev/null || true
      # docker --memory=2G enforces a hard cgroup ceiling. The redpanda
      # `start --memory=1G` flag below only sets the seastar data plane
      # reservation — it does NOT bound the rest of the process. A 21h
      # soak observed redpanda triggering the system OOM-killer with a
      # 12.9 GiB folio_prealloc allocation, killing the unrelated CH
      # container as collateral. The docker cgroup limit catches that.
      docker run --detach \
        --name redpanda-0 \
        --network xtcp \
        --hostname redpanda-0 \
        -p 19092:19092 -p 19644:9644 -p 18081:8081 \
        --memory=2G \
        -v redpanda-0:/var/lib/redpanda/data \
        --restart on-failure \
        ${clickPipeRedpandaImage} \
        redpanda start \
          --kafka-addr=internal://0.0.0.0:9092,external://0.0.0.0:19092 \
          --advertise-kafka-addr=internal://redpanda-0:9092,external://localhost:19092 \
          --schema-registry-addr=internal://0.0.0.0:8081,external://0.0.0.0:18081 \
          --rpc-addr=redpanda-0:33145 \
          --advertise-rpc-addr=redpanda-0:33145 \
          --mode=dev-container \
          --smp=1 \
          --memory=1G \
          --reserve-memory=0M \
          --default-log-level=info >/dev/null
      echo "redpanda-0: started"

      # 4) Wait for the Kafka API to be up (a few seconds), then create
      # the topic xtcp2 will produce to. Idempotent — running it again
      # is a noop after the first time.
      for _ in $(seq 1 30); do
        if docker exec redpanda-0 rpk cluster health 2>/dev/null \
            | grep -q 'Healthy.*true'; then
          break
        fi
        sleep 1
      done
      docker exec redpanda-0 rpk topic create ${clickPipeKafkaTopic} \
        --partitions 1 --replicas 1 2>/dev/null || true
      echo "topic ${clickPipeKafkaTopic}: ready"

      # Wait for the schema registry to start listening too — xtcp2's
      # newKafkaDest calls registerProtobufSchema during init, which
      # POSTs to the schema registry. If it isn't up yet the daemon
      # crashes and systemd restart-loops it. Schema registry binds on
      # localhost:18081 via the docker run -p mapping.
      for _ in $(seq 1 30); do
        if curl --silent --fail --max-time 2 \
            http://localhost:18081/subjects >/dev/null 2>&1; then
          break
        fi
        sleep 1
      done
      echo "schema-registry: ready"

      # 5) Start clickhouse with the initdb scripts mounted from a
      # writable tmpfs copy of clickPipeInitdb. The init scripts
      # 005_start.sh / 010_clear_tracking_files.sh + the *_recreate_*
      # ones write tracking files into out/ — they can't run from a
      # read-only /nix/store mount. We also patch any `rm --recursive`
      # to `rm -r` since alpine's busybox `rm` doesn't accept the long
      # option (the original compose used the full-coreutils alpine
      # which did).
      initdbRw=/var/lib/xtcp2-clickhouse-initdb
      rm -rf "$initdbRw"
      mkdir -p "$initdbRw"
      cp -r ${clickPipeInitdb}/. "$initdbRw"/
      chmod -R u+w "$initdbRw"
      # Replace long --recursive flags with -r (busybox-compatible).
      # Done in-place because the source dir is a writable copy now.
      find "$initdbRw" -type f -name '*.sh' -exec \
        sed -i 's/rm --recursive --force/rm -rf/g' {} +
      # The initdb shell scripts invoke `clickhouse-client` without a
      # --password. With CLICKHOUSE_PASSWORD set on the container, the
      # default user requires auth even over the local TCP loopback, so
      # the bare invocations fail with code 194 and the container exits.
      # Patch the variable definition to include the password.
      find "$initdbRw" -type f -name '*.sh' -exec \
        sed -i 's|CLICKHOUSE_CLIENT="clickhouse-client";|CLICKHOUSE_CLIENT="clickhouse-client --password ${clickPipeChPassword}";|g' {} +
      # The 020 script uses a heredoc into `clickhouse-client -n` rather
      # than the CLICKHOUSE_CLIENT variable — patch that directly too.
      find "$initdbRw" -type f -name '*.sh' -exec \
        sed -i 's|clickhouse-client -n <<-EOSQL|clickhouse-client --password ${clickPipeChPassword} -n <<-EOSQL|g' {} +
      # Same writable-copy pattern for format_schemas: clickhouse's
      # entrypoint chowns the mountpoint, which fails on a read-only
      # /nix/store bind. tmpfs the .proto file so the chown succeeds.
      schemasRw=/var/lib/xtcp2-clickhouse-schemas
      rm -rf "$schemasRw"
      mkdir -p "$schemasRw"
      cp ${clickPipeProtoSchemas}/* "$schemasRw"/
      chmod -R u+w "$schemasRw"
      # config.d mount: read-only is fine (no chown required by entrypoint).
      configDRo=/var/lib/xtcp2-clickhouse-config-d
      rm -rf "$configDRo"
      mkdir -p "$configDRo"
      cp ${clickPipeConfigD}/* "$configDRo"/
      # users.d mount: read-WRITE — our allow-host.xml lives here AND the image
      # entrypoint writes default-user.xml (the CLICKHOUSE_PASSWORD file) here,
      # so a read-only mount makes the container exit.
      usersDRw=/var/lib/xtcp2-clickhouse-users-d
      rm -rf "$usersDRw"
      mkdir -p "$usersDRw"
      cp ${clickPipeUsersD}/* "$usersDRw"/
      chmod -R u+w "$usersDRw"
      docker rm -f clickhouse 2>/dev/null || true
      # --add-host host.docker.internal:host-gateway gives ClickHouse a
      # routable name for the VM host (where the in-VM MinIO listens
      # for the mixed clickpipe-parquet flavor). The mapping is
      # harmless for the plain clickpipe flavor too: it's just an
      # /etc/hosts entry that nothing references unless an s3() table
      # function asks for it.
      docker run --detach \
        --name clickhouse \
        --network xtcp \
        --hostname clickhouse \
        --add-host host.docker.internal:host-gateway \
        -p 18123:8123 -p 19001:9000 \
        --ulimit nofile=262144:262144 \
        --memory=${clickPipeClickhouseMemory} \
        --cap-add CAP_NET_ADMIN --cap-add CAP_SYS_NICE \
        --cap-add CAP_IPC_LOCK --cap-add CAP_SYS_PTRACE \
        --env CLICKHOUSE_PASSWORD=${clickPipeChPassword} \
        --env "MALLOC_CONF=background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:1000" \
        -v clickhouse_db:/var/lib/clickhouse \
        -v "$initdbRw":/docker-entrypoint-initdb.d:rw \
        -v "$schemasRw":/var/lib/clickhouse/format_schemas:rw \
        -v "$configDRo":/etc/clickhouse-server/config.d:ro \
        -v "$usersDRw":/etc/clickhouse-server/users.d:rw \
        --restart on-failure \
        ${clickPipeClickhouseImage} >/dev/null
      echo "clickhouse: started"

      # 6) Wait for clickhouse to accept queries (~10-20s on first boot
      # because the initdb scripts run synchronously before HTTP comes up).
      for _ in $(seq 1 60); do
        if docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} -q 'SELECT 1' >/dev/null 2>&1; then
          break
        fi
        sleep 1
      done
      echo "clickhouse: ready"

      # All ready — exit so the next oneshot/service ordered After=us
      # can start. The monitor service tails the row count after xtcp2
      # has had a chance to produce.
      echo "clickpipe-up: complete"
    '';
  };

  # Companion service that tails the row count every 30s once xtcp2 +
  # the clickpipe stack are up. Decoupled from clickpipe-up so the
  # oneshot can exit cleanly and let xtcp2.service start.
  clickPipeMonitorScript = pkgs.writeShellApplication {
    name = "xtcp2-clickpipe-monitor";
    runtimeInputs = with pkgs; [
      coreutils
      docker
    ];
    text = ''
      # Wait for the table to exist (initdb runs async during clickhouse
      # first start).
      for _ in $(seq 1 60); do
        if docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
            -q 'EXISTS TABLE xtcp.xtcp_flat_records' 2>/dev/null \
            | grep -q '^1$'; then
          break
        fi
        sleep 2
      done
      # Periodic snapshot — sentinel prefix lets the host runner grep
      # without ambiguity. prev_msgs drives the "only dump deep diagnostics
      # when something looks wrong" gate at the bottom of the loop.
      #
      # NB: there is intentionally NO periodic `SYSTEM DROP FORMAT SCHEMA
      # CACHE FOR Protobuf` here. An earlier version flushed the cache every
      # loop on a "poisoned cache" theory — that theory was wrong. The real
      # stall was a package-qualified kafka_schema message name that never
      # resolves (fixed in sql/xtcp_xtcp_flat_records_kafka.sql). The cache
      # is fine; do not reintroduce the flush.
      prev_msgs=-1
      while true; do
        rows=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT count() FROM xtcp.xtcp_flat_records' 2>/dev/null || echo 0)
        # Distinct netns_inode = how many network namespaces have landed
        # records end-to-end. This is the Method B discovery proof: each
        # tcp-stress container's netns should show up as a distinct inode
        # once xtcp2 discovers it and its records reach ClickHouse.
        nsc=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT uniqExact(netns_inode) FROM xtcp.xtcp_flat_records' 2>/dev/null || echo 0)
        # Distinct non-empty container_id = per-container ENRICHMENT proof.
        # With -resolveContainerId (the stress flavor), records from the
        # tcp-stress load containers' sockets carry the owning docker
        # container_id, resolved from the socket's cgroup v2 id (pkg/cgroupid).
        # 0 when resolution is disabled (the other clickpipe flavors).
        cid=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT uniqExact(container_id) FROM xtcp.xtcp_flat_records WHERE length(container_id) > 0' 2>/dev/null || echo 0)
        # Kafka consumer health: a non-zero count here means the Kafka-engine
        # consumer hit an exception (e.g. MEMORY_LIMIT_EXCEEDED) — the signal
        # that ingestion has stalled. 0 = healthy end-to-end ingest.
        kexc=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT count() FROM system.kafka_consumers WHERE length(last_exception) > 0' 2>/dev/null || echo 0)
        # Rebalance-loop diagnostics. reb = cumulative consumer-group
        # rebalance revocations; if this climbs while rows stall, the
        # consumer is in the documented max.poll.interval.ms rebalance
        # death loop (see kafka_client_tuning.xml). msgs = kafka messages
        # the consumer has actually read; if this freezes near one poll
        # batch (~kafka_poll_max_batch_size) the consumer stopped polling.
        reb=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT sum(num_rebalance_revocations) FROM system.kafka_consumers' 2>/dev/null || echo 0)
        msgs=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT sum(num_messages_read) FROM system.kafka_consumers' 2>/dev/null || echo 0)
        # Disk utilization of the persistent docker volume (/var/lib/docker,
        # the 16 GiB ext4 disk that backs the clickhouse_db MergeTree +
        # redpanda segment logs). This grows over a soak; if it saturates,
        # the kafka_engine can't commit offsets, xtcp2's producer back-
        # pressures, and ingestion silently plateaus — the exact failure
        # that froze a 12h run at ~18k rows and reads like a decode/consumer
        # bug when it is really disk-full. Surface it every heartbeat so the
        # host runner can assert on it. Integer percent (no % sign).
        disk=$(df --output=pcent /var/lib/docker 2>/dev/null | tail -1 | tr -dc '0-9')
        disk=''${disk:-0}
        echo "XTCP2_CLICKPIPE_ROWS $(date -u +%FT%TZ) rows=$rows netns=$nsc container_id=$cid kafka_exc=$kexc reb=$reb msgs=$msgs disk=$disk"
        # Deep-dive diagnostics for the consumer-detach stall — ONLY when
        # something looks wrong, so a healthy multi-hour soak stays quiet.
        # Trouble = a consumer exception, or the cumulative messages-read
        # count (monotonic) not advancing since the last sample = the
        # consumer stopped polling. We gate on msgs, not rows, because the
        # stress flavor's 1h TTL legitimately shrinks the row count at steady
        # state and must not be mistaken for a stall. When it fires it dumps
        # the full consumer row (incl. the exceptions array, which keeps the
        # last ~10 errors even after last_exception clears) + the ClickHouse
        # server-log Kafka lines (the actual reason the stream engine stops).
        if [ "$kexc" -ne 0 ] || [ "$msgs" -le "$prev_msgs" ]; then
          docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} -q \
            "SELECT concat('used=', toString(is_currently_used), ' msgs=', toString(num_messages_read), ' commits=', toString(num_commits), ' reb=', toString(num_rebalance_revocations), ' lastpoll=', toString(last_poll_time), ' lastcommit=', toString(last_commit_time), ' exc=[', arrayStringConcat(arrayMap(x -> replaceAll(x, '\n', ' '), exceptions.text), ' || '), ']') FROM system.kafka_consumers" 2>/dev/null \
            | sed 's/^/XTCP2_CH_CONSUMER /' || true
          docker exec clickhouse sh -c \
            "grep -iE 'StorageKafka|rdkafka|rebalance|assign|Committed|Stalled|Polled|DirectKafka' /var/log/clickhouse-server/clickhouse-server.log 2>/dev/null | tail -12" 2>/dev/null \
            | sed 's/^/XTCP2_CH_KAFKALOG /' || true
        fi
        prev_msgs=$msgs
        sleep 30
      done
    '';
  };

  # clickhouse-pipeline-rate flavor: the runtime-control behavioral test.
  # A dedicated monitor drives xtcp2ctl through a poll-frequency schedule and
  # asserts the RATE of rows arriving in xtcp.xtcp_flat_records responds:
  #   BASELINE(10s) → FAST(1s, ~10× faster) → REVERT(10s, back down)
  # then a poll-burst (6 polls, 10s apart) with the ticker parked. Rate is the
  # ClickHouse row-count delta over a fixed window (default 60s); a steady
  # 100-connection tcp load keeps rows-per-poll stable so rate tracks only the
  # frequency. Emits per-phase data + PASS/FAIL verdict sentinels + a terminal
  # XTCP2_RATE_DONE the host runner (lib.nix mkClickPipeRateRunner) waits on.
  clickPipeRateScript = pkgs.writeShellApplication {
    name = "xtcp2-clickpipe-rate";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      docker
      gawk
      gnugrep
      xtcp2AllPackage # provides xtcp2ctl
    ];
    text = ''
      # Never abort mid-schedule — we want to reach XTCP2_RATE_DONE so the host
      # runner never blocks to timeout on a single transient hiccup.
      set +e

      WINDOW=''${RATE_WINDOW_SEC:-180}  # per-phase measurement window (seconds)
      SETTLE=''${RATE_SETTLE_SEC:-30}   # settle after a frequency change before measuring
      TOL=''${RATE_TOL_PCT:-10}         # ± tolerance percent for the quantitative asserts
      GPORT=${toString cfg.grpcPort}
      PROM="http://127.0.0.1:${toString cfg.promPort}/metrics"

      ch_rows() {
        docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT count() FROM xtcp.xtcp_flat_records' 2>/dev/null | tr -dc '0-9'
      }
      # Sum a Poller counter row (function="Poller",variable=$1) from /metrics.
      # Handles integer or scientific-notation values (awk coerces both).
      poller_counter() {
        curl --silent --fail --max-time 3 "$PROM" 2>/dev/null \
          | awk -v v="variable=\"$1\"" '
              /^xtcp_counts/ && index($0,"function=\"Poller\"")>0 && index($0,v)>0 { s += $NF + 0 }
              END { printf "%d", s+0 }'
      }
      setfreq() { xtcp2ctl set-poll-frequency -frequency "$1" -timeout "$2" -target 127.0.0.1 -port "$GPORT" >/dev/null 2>&1; }
      # within_tol ACTUAL EXPECTED → true when |actual-expected|*100/expected <= TOL.
      within_tol() {
        local a="$1" e="$2" d
        [ "$e" -le 0 ] && return 1
        d=$(( a >= e ? a - e : e - a ))
        [ $(( d * 100 / e )) -le "$TOL" ]
      }
      verdict() { # NAME OK MSG
        if [ "$2" -eq 1 ]; then echo "XTCP2_RATE_''${1}_PASS ($3)"; else echo "XTCP2_RATE_''${1}_FAIL ($3)"; fi
      }

      # measure PHASE FREQ_SECONDS → samples (ClickHouse rows, produced rows,
      # poll count) across one window and sets the globals M_CH / M_PRODUCED /
      # M_POLLS / M_EXPECTED / M_N. N = produced/polls = sockets-per-poll (the
      # daemon's own socket estimate). Jitter is disabled, so M_POLLS ≈ W/f.
      measure() {
        local phase="$1" fsec="$2" t0 e0 k0 t1 e1 k1
        t0=$(ch_rows); t0=''${t0:-0}; e0=$(poller_counter envelopeRows); k0=$(poller_counter ticker)
        sleep "$WINDOW"
        t1=$(ch_rows); t1=''${t1:-0}; e1=$(poller_counter envelopeRows); k1=$(poller_counter ticker)
        M_CH=$(( t1 - t0 )); M_PRODUCED=$(( e1 - e0 )); M_POLLS=$(( k1 - k0 ))
        M_EXPECTED=$(( WINDOW / fsec ))
        if [ "$M_POLLS" -gt 0 ]; then M_N=$(( M_PRODUCED / M_POLLS )); else M_N=0; fi
        echo "XTCP2_RATE_$phase $(date -u +%FT%TZ) freq=''${fsec}s window=''${WINDOW}s polls=$M_POLLS expected_polls=$M_EXPECTED sockets_per_poll=$M_N produced=$M_PRODUCED ch_delta=$M_CH"
      }

      echo "XTCP2_RATE_START $(date -u +%FT%TZ) window=''${WINDOW}s settle=''${SETTLE}s tol=''${TOL}% grpc=$GPORT"

      # ── Ready gate: xtcp2 metrics up AND rows already flowing into ClickHouse.
      ready=0
      for _ in $(seq 1 90); do
        if curl --silent --fail --max-time 3 "$PROM" 2>/dev/null | grep -q '^xtcp_'; then
          r=$(ch_rows); r=''${r:-0}
          if [ "$r" -gt 0 ]; then ready=1; break; fi
        fi
        sleep 2
      done
      if [ "$ready" -ne 1 ]; then
        echo "XTCP2_RATE_OVERALL_FAIL (pipeline not ready: no rows in ClickHouse within 180s)"
        echo "XTCP2_RATE_DONE"
        exit 1
      fi

      # ── Phase 1: BASELINE (10s) — defines the reference socket estimate.
      setfreq 10s 2s;   sleep "$SETTLE"; measure BASELINE 10
      base_polls=$M_POLLS; base_exp=$M_EXPECTED; base_ch=$M_CH; base_prod=$M_PRODUCED; NCALIB=$M_N
      # ── Phase 2: FAST (1s) — ~10× the poll rate.
      setfreq 1s 500ms; sleep "$SETTLE"; measure FAST 1
      fast_polls=$M_POLLS; fast_exp=$M_EXPECTED; fast_ch=$M_CH; fast_prod=$M_PRODUCED; fast_n=$M_N
      # ── Phase 3: REVERT (10s) — rate returns to baseline.
      setfreq 10s 2s;   sleep "$SETTLE"; measure REVERT 10
      rev_polls=$M_POLLS; rev_exp=$M_EXPECTED; rev_ch=$M_CH; rev_prod=$M_PRODUCED; rev_n=$M_N

      # CADENCE: the actual poll count each window matched window/frequency —
      # i.e. set-poll-frequency really changed the poll rate (quantitatively).
      cad_ok=1
      within_tol "$base_polls" "$base_exp" || cad_ok=0
      within_tol "$fast_polls" "$fast_exp" || cad_ok=0
      within_tol "$rev_polls" "$rev_exp"  || cad_ok=0
      verdict CADENCE "$cad_ok" "polls vs W/f: base=$base_polls/$base_exp fast=$fast_polls/$fast_exp revert=$rev_polls/$rev_exp"

      # PREDICT (the headline): ClickHouse rows in the FAST and REVERT windows
      # matched the prediction from the socket estimate + the known frequency:
      # expected = NCALIB × (window/freq).
      fast_pred=$(( NCALIB * fast_exp )); rev_pred=$(( NCALIB * rev_exp ))
      pred_ok=1
      within_tol "$fast_ch" "$fast_pred" || pred_ok=0
      within_tol "$rev_ch" "$rev_pred"  || pred_ok=0
      verdict PREDICT "$pred_ok" "ch vs N*W/f (N=$NCALIB): fast=$fast_ch/$fast_pred revert=$rev_ch/$rev_pred"

      # DELIVERY: every row the daemon produced reached ClickHouse (no loss /
      # consumer stall) — ch_delta ≈ produced each phase.
      del_ok=1
      within_tol "$base_ch" "$base_prod" || del_ok=0
      within_tol "$fast_ch" "$fast_prod" || del_ok=0
      within_tol "$rev_ch" "$rev_prod"  || del_ok=0
      verdict DELIVERY "$del_ok" "ch vs produced: base=$base_ch/$base_prod fast=$fast_ch/$fast_prod revert=$rev_ch/$rev_prod"

      # SOCKETS: the socket estimate stayed stable across phases, so the rate
      # change is attributable to frequency, not socket drift.
      soc_ok=1
      within_tol "$fast_n" "$NCALIB" || soc_ok=0
      within_tol "$rev_n" "$NCALIB"  || soc_ok=0
      verdict SOCKETS "$soc_ok" "sockets/poll stable: calib=$NCALIB fast=$fast_n revert=$rev_n"

      # ── Phase 4: BURST. Park the ticker at 1h (1s timeout lets a 10s burst
      # interval satisfy interval>poll_timeout). Measure one poll's rows via the
      # daemon counter (no lag), then fire 6 polls 10s apart and confirm the
      # poll counter advanced by >=6 and 6× the rows were produced AND delivered.
      setfreq 3600s 1s; sleep "$SETTLE"
      pe0=$(poller_counter envelopeRows)
      xtcp2ctl trigger-poll -target 127.0.0.1 -port "$GPORT" >/dev/null 2>&1
      sleep 8
      pe1=$(poller_counter envelopeRows)
      perpoll=$(( pe1 - pe0 ))

      pr0=$(poller_counter pollRequestCh); be0=$(poller_counter envelopeRows)
      bc0=$(ch_rows); bc0=''${bc0:-0}
      xtcp2ctl poll-burst -count 6 -interval 10s -target 127.0.0.1 -port "$GPORT" >/dev/null 2>&1
      sleep 80   # 6 × 10s spacing + ClickHouse consume lag
      pr1=$(poller_counter pollRequestCh); be1=$(poller_counter envelopeRows)
      bc1=$(ch_rows); bc1=''${bc1:-0}
      pollreq_delta=$(( pr1 - pr0 )); burst_prod=$(( be1 - be0 )); burst_ch=$(( bc1 - bc0 ))
      burst_exp=$(( 6 * perpoll ))
      echo "XTCP2_RATE_BURST $(date -u +%FT%TZ) perpoll=$perpoll pollreq_delta=$pollreq_delta produced=$burst_prod ch_delta=$burst_ch expected=$burst_exp"

      burst_ok=1
      [ "$pollreq_delta" -ge 6 ] || burst_ok=0
      [ "$perpoll" -gt 0 ] || burst_ok=0
      within_tol "$burst_prod" "$burst_exp" || burst_ok=0
      within_tol "$burst_ch" "$burst_prod"  || burst_ok=0
      verdict BURST "$burst_ok" "6 polls: pollreq_delta=$pollreq_delta produced=$burst_prod/~$burst_exp ch=$burst_ch"

      if [ "$cad_ok" -eq 1 ] && [ "$pred_ok" -eq 1 ] && [ "$del_ok" -eq 1 ] \
         && [ "$soc_ok" -eq 1 ] && [ "$burst_ok" -eq 1 ]; then
        echo "XTCP2_RATE_OVERALL_PASS"
      else
        echo "XTCP2_RATE_OVERALL_FAIL (cadence=$cad_ok predict=$pred_ok delivery=$del_ok sockets=$soc_ok burst=$burst_ok)"
      fi
      echo "XTCP2_RATE_DONE"
    '';
  };

  # s3parquet flavor: in-VM MinIO + bucket bootstrap. The xtcp2 daemon
  # talks to MinIO directly via the minio-go client; no proto-desc file
  # or unixgram socket required. The long-soak variant additionally
  # brings up a local Pyroscope server so xtcp2 can stream profiles
  # for goroutine/thread-leak diagnosis without an external dependency.
  s3ParquetModules = [
    (import ../modules/minio-bucket-bootstrap.nix {
      # The clickpipe-parquet AND s3parquet-stress flavors mount a dedicated
      # ext4 disk at /var/lib/minio via microvm.volumes (see below) — tell the
      # bootstrap module not to also declare a tmpfs there. The short-run
      # s3parquet / s3parquet-long flavors keep the 512 MiB tmpfs.
      useTmpfs = !isClickPipeParquet && !isS3ParquetStress;
    })
  ]
  ++ lib.optionals (isS3ParquetLong || isS3ParquetStress || isS3ParquetLowfreq) [
    (import ../modules/pyroscope-server.nix { })
  ];

  # The broker flavors (valkey/nats/nsq) share one parameterized module
  # (broker-server.nix): a native server + a pre-subscribed consumer whose
  # delivery count the self-test reads. Ports and topics/channels must match
  # the matching xtcp2*Args' -dest/-topic (and the self-test's queries).

  # valkey: pub/sub. valkey-cli block-buffers stdout to a pipe, so both the
  # readiness ping and the subscriber run bare / under a PTY (`unbuffer`)
  # respectively — stdbuf can't defeat valkey-cli's internal buffering.
  valkeyModules = [
    (import ../modules/broker-server.nix {
      serverName = "valkey-server";
      consumerName = "valkey-subscriber";
      label = "valkey";
      serverExecStart = pkgs: ''
        ${pkgs.valkey}/bin/valkey-server \
          --bind 0.0.0.0 --port 6379 \
          --protected-mode no --save "" --appendonly no
      '';
      readyCheck = pkgs: "${pkgs.valkey}/bin/valkey-cli -h 127.0.0.1 -p 6379 ping >/dev/null 2>&1";
      consumerExec = pkgs: ''
        exec ${pkgs.expect}/bin/unbuffer \
          ${pkgs.valkey}/bin/valkey-cli -h 127.0.0.1 -p 6379 \
          subscribe ${valkeyTopic}
      '';
    })
  ];

  # nats: core NATS is fire-and-forget. Readiness via bash /dev/tcp (no extra
  # tools); the subscriber (natscli) runs under a PTY so it flushes per message.
  natsModules = [
    (import ../modules/broker-server.nix {
      serverName = "nats-server";
      consumerName = "nats-subscriber";
      label = "nats";
      serverExecStart = pkgs: "${pkgs.nats-server}/bin/nats-server --addr 0.0.0.0 --port 4222";
      readyCheck = _pkgs: "(exec 3<>/dev/tcp/127.0.0.1/4222) 2>/dev/null";
      consumerExec = pkgs: ''
        exec ${pkgs.expect}/bin/unbuffer \
          ${pkgs.natscli}/bin/nats --server nats://127.0.0.1:4222 \
          sub ${natsTopic}
      '';
    })
  ];

  # nsq: nsqd + an nsq_tail consumer that FINISHes each message, so nsqd's
  # per-channel finish_count (/stats) is a deterministic consumed count.
  nsqModules = [
    (import ../modules/broker-server.nix {
      serverName = "nsqd";
      consumerName = "nsq-consumer";
      label = "nsq";
      serverExecStart = pkgs: ''
        ${pkgs.nsq}/bin/nsqd \
          --tcp-address 0.0.0.0:4150 \
          --http-address 0.0.0.0:4151 \
          --data-path /var/lib/nsqd
      '';
      serverServiceConfig = {
        StateDirectory = "nsqd";
      };
      readyCheck = _pkgs: "(exec 3<>/dev/tcp/127.0.0.1/4150) 2>/dev/null";
      consumerExec = pkgs: ''
        exec ${pkgs.nsq}/bin/nsq_tail \
          --topic=${nsqTopicName} --channel=${nsqChannelName} \
          --nsqd-tcp-address=127.0.0.1:4150
      '';
    })
  ];

  # Long-soak monitor: emit one sentinel line per
  # S3PARQUET_REPORT_INTERVAL seconds. The numbers come from xtcp2's
  # own Prometheus counters (destS3Parquet/upload + uploadBytes)
  # rather than `mc find` — under nsTest load the mc commands are too
  # slow to complete inside the cadence window.
  s3ParquetMonitorScript = pkgs.writeShellApplication {
    name = "xtcp2-s3parquet-monitor";
    runtimeInputs = with pkgs; [
      coreutils
      curl
      gawk
      gnugrep
      gnused
    ];
    text = ''
      # Wait for xtcp2's /metrics endpoint to come up before reporting.
      # No mc/MinIO probe — xtcp2 itself owns the upload counter we
      # rely on, so the metrics endpoint is the right readiness gate.
      for _ in $(seq 1 60); do
        if curl --silent --fail --max-time 2 \
             http://127.0.0.1:9088/metrics >/dev/null 2>&1; then
          break
        fi
        sleep 2
      done

      interval="''${S3PARQUET_REPORT_INTERVAL:-3600}"
      echo "XTCP2_S3PARQUET_MONITOR_START interval=''${interval}s"

      # Extract a single Prometheus counter value by full label match.
      # Returns "0" when the counter hasn't been emitted yet (e.g.
      # before the first finalize), so smoke runs see a clean
      # files=0 line. The `|| true` swallows pipefail when grep
      # finds nothing — without it set -e (from
      # writeShellApplication) kills the whole monitor on the first
      # cold-start scrape, causing a systemd restart loop.
      get_counter() {
        local metrics="$1" pattern="$2"
        local out
        out=$( { echo "$metrics" \
                 | grep -E "^xtcp_counts\\{[^}]*''${pattern}[^}]*\\}" \
                 | sed -nE 's/.*\}[[:space:]]+([0-9.+e-]+).*/\1/p' \
                 | head -n1; } || true )
        echo "''${out:-0}"
      }

      # Pull the simple Go runtime metrics by their bare name (no
      # label prefix). Used for goroutine / thread leak diagnosis.
      get_simple() {
        local metrics="$1" name="$2"
        local out
        out=$( { echo "$metrics" \
                 | grep -E "^''${name}[[:space:]]" \
                 | sed -nE 's/[^[:space:]]+[[:space:]]+([0-9.+e-]+).*/\1/p' \
                 | head -n1; } || true )
        echo "''${out:-0}"
      }

      while true; do
        sleep "$interval"
        metrics=$(curl --silent --fail --max-time 5 \
                       http://127.0.0.1:9088/metrics 2>/dev/null || echo "")
        files=$(get_counter "$metrics" 'variable="upload"')
        bytes=$(get_counter "$metrics" 'variable="uploadBytes"')
        rows=$(get_counter "$metrics" 'variable="uploadRows"')
        gor=$(get_simple "$metrics" 'go_goroutines')
        thr=$(get_simple "$metrics" 'go_threads')
        : "''${files:=0}" "''${bytes:=0}" "''${rows:=0}" "''${gor:=0}" "''${thr:=0}"
        # Prometheus client may print "5.4e+07"; convert through awk so
        # the sentinel shows the integer rather than the scientific-
        # notation prefix (a previous attempt used "''${var%.*}" which
        # strips after the last `.` and turned "5.4e+07" into "5").
        files=$(awk -v n="$files" 'BEGIN { printf "%.0f", n+0 }')
        bytes=$(awk -v n="$bytes" 'BEGIN { printf "%.0f", n+0 }')
        rows=$(awk -v n="$rows" 'BEGIN { printf "%.0f", n+0 }')
        gor=$(awk -v n="$gor" 'BEGIN { printf "%.0f", n+0 }')
        thr=$(awk -v n="$thr" 'BEGIN { printf "%.0f", n+0 }')
        # Disk utilization of the MinIO data dir (integer percent, no % sign).
        # For s3parquet-stress this is the dedicated ext4 disk that holds the
        # parquet objects; the runner asserts it never saturates (a full disk
        # would back-pressure uploads and stall ingest). For s3parquet-long it
        # is the 512 MiB tmpfs — harmless, just reported.
        disk=$(df --output=pcent /var/lib/minio 2>/dev/null | tail -1 | tr -dc '0-9')
        disk=''${disk:-0}
        echo "XTCP2_S3PARQUET_HOURLY $(date -u +%FT%TZ) files=''${files} bytes=''${bytes} rows=''${rows} goroutines=''${gor} threads=''${thr} disk=''${disk}"
      done
    '';
  };

  # s3parquet-stress retention: the parquet path has no TTL of its own, so a
  # long soak would grow the MinIO disk without bound. This is the direct
  # analog of the clickhouse-pipeline-stress table's 1h TTL — every 10 min it
  # deletes objects older than 1h, keeping ~1h of parquet retained so the disk
  # sits at steady state and the disk guard stays meaningful at any duration.
  s3ParquetRetentionScript = pkgs.writeShellApplication {
    name = "xtcp2-s3parquet-retention";
    runtimeInputs = with pkgs; [
      coreutils
      minio-client
    ];
    text = ''
      # Address MinIO via the MC_HOST_<alias> env var — no `mc alias set` /
      # persisted config needed. Also set HOME + MC_CONFIG_DIR explicitly:
      # writeShellApplication runs with a minimal PATH that lacks `getent`, and
      # with HOME unset mc shells out to `getent` to locate its config dir and
      # aborts ("Unable to get mcConfigDir. exec: getent: not found") — which
      # silently failed EVERY mc call (that was the real retention bug). With
      # MC_CONFIG_DIR set, mc uses it directly and never calls getent.
      export MC_HOST_local="http://xtcp2test:xtcp2testsecret@127.0.0.1:9000"
      export HOME=/tmp/xtcp2-s3parquet-mc
      export MC_CONFIG_DIR="$HOME/.mc"
      mkdir -p "$MC_CONFIG_DIR"
      # Retention window + sweep cadence are overridable (defaults = the 1h
      # analog of the clickhouse-stress table TTL, swept every 10 min); a
      # verification run can shorten both to see deletions without waiting 1h.
      AGE="''${S3PARQUET_RETENTION_AGE:-1h}"
      IVAL="''${S3PARQUET_RETENTION_INTERVAL:-600}"
      # Wait for MinIO to answer (a credentialed list) before the loop.
      for _ in $(seq 1 60); do
        if mc ls local/ >/dev/null 2>&1; then break; fi
        sleep 2
      done
      echo "XTCP2_S3PARQUET_RETENTION start: deleting objects older than $AGE every ''${IVAL}s"
      while true; do
        sleep "$IVAL"
        # Capture stdout+stderr so a failure is visible on the console (as
        # err=[...]) instead of a silent expired=0. `--force` is required for a
        # non-interactive recursive delete; mc prints one "Removed ..." line
        # per deleted object. `|| true` keeps the loop alive across a transient
        # MinIO blip.
        out=$(mc rm --recursive --force --older-than "$AGE" local/xtcp2-records/ 2>&1) || true
        n=$(printf '%s\n' "$out" | grep -c '^Removed ' || true)
        err=$(printf '%s\n' "$out" | grep -iE 'error|unable|fail' | head -1 || true)
        echo "XTCP2_S3PARQUET_RETENTION $(date -u +%FT%TZ) expired=''${n}''${err:+ err=[''${err}]}"
      done
    '';
  };

  # The three long-running s3parquet flavors (long/stress/lowfreq) share one
  # production parquet→MinIO config — 64 MiB flush, 5s timeout, Pyroscope leak
  # diagnosis streaming to the in-VM server — and differ ONLY in poll frequency
  # and the Pyroscope app name. -s3FlushInterval is intentionally left unset so
  # it derives to the out-of-the-box staleness ceiling.
  #
  # (The lifecycle s3parquet flavor is a different shape — 1 MiB flush, no
  # Pyroscope — so it stays a separate list, xtcp2S3ParquetArgs below.)
  mkS3ParquetArgs =
    {
      frequency,
      pyroscopeAppName,
    }:
    [
      "-dest"
      "s3parquet:http://127.0.0.1:9000"
      "-marshal"
      "protobufList"
      "-frequency"
      frequency
      "-timeout"
      "5s"
      "-s3Bucket"
      "xtcp2-records"
      "-s3AccessKey"
      "xtcp2test"
      "-s3SecretKey"
      "xtcp2testsecret"
      "-s3ParquetFlushBytes"
      "67108864"
      "-pyroscopeUrl"
      "http://127.0.0.1:14040"
      "-pyroscopeAppName"
      pyroscopeAppName
    ];

  # long-soak: at the steady ~1 MB/min raw-row rate seen in the 30 min smoke, a
  # 12 h run produces ~12 finalized objects. 10s poll keeps the daemon CPU-cheap.
  xtcp2S3ParquetLongArgs = mkS3ParquetArgs {
    frequency = "10s";
    pyroscopeAppName = "xtcp2.s3parquet-long";
  };

  # stress: same config as long, but this flavor ALSO runs the tcp-stress load
  # containers (20 × 250 sockets), so xtcp2 discovers many container netns and
  # reads real socket load while uploading (+ ~1h retention cleanup + disk guard).
  xtcp2S3ParquetStressArgs = mkS3ParquetArgs {
    frequency = "10s";
    pyroscopeAppName = "xtcp2.s3parquet-stress";
  };

  # lowfreq: low-activity counterpart — same sink + container load as stress, but
  # poll ONCE AN HOUR with only 2 sockets/container. The 64 MiB byte cap is never
  # reached (~40 sockets), so the staleness TIMER is the only thing that finalizes
  # a parquet object — verifies the bucket still fills under low poll + low count.
  xtcp2S3ParquetLowfreqArgs = mkS3ParquetArgs {
    frequency = "1h";
    pyroscopeAppName = "xtcp2.s3parquet-lowfreq";
  };

  # Args for the SECOND xtcp2 instance in the clickhouse-pipeline-parquet
  # flavor. The primary instance writes to kafka (xtcp2ClickPipeArgs);
  # this one writes parquet to the same in-VM MinIO so ClickHouse can
  # read both paths. Different prom + grpc ports so the two instances
  # don't clash. 256 KiB flush threshold gives parquet turnover within
  # the 5-10 min boot exercise window (production deployments would
  # raise this to the 63 MiB default).
  xtcp2ClickPipeParquetArgs = [
    "-dest"
    "s3parquet:http://127.0.0.1:9000"
    "-marshal"
    "protobufList"
    "-frequency"
    "5s"
    "-timeout"
    "2s"
    "-s3Bucket"
    "xtcp2-records"
    "-s3AccessKey"
    "xtcp2test"
    "-s3SecretKey"
    "xtcp2testsecret"
    "-s3ParquetFlushBytes"
    "262144"
    "-promListen"
    ":9089"
    "-grpcPort"
    "8890"
  ];

  # Both the basic and coverage flavors override the default dest. The
  # default in cmd/xtcp2 is `kafka:redpanda-0:9092` which makes the kafka
  # destination factory read /xtcp_flat_record.proto — that file lives
  # in the source tree, never inside the stripped VM, so the daemon
  # crashes during init and systemd never lets the prom listener stay up
  # long enough for the self-test to scrape it. `-dest null` sidesteps
  # the proto read entirely.
  xtcp2BasicArgs = [
    "-dest"
    "null"
    "-frequency"
    "2s"
    "-timeout"
    "1s"
  ];

  # Where the minimal flavor writes its jsonl records (daemon file dest) and
  # where the OUTPUT_CONTENT self-test check reads them back. Both xtcp2 and
  # the self-test run as root, so a 0600 file under /var/log is readable.
  fileOutputPath = "/var/log/xtcp2.jsonl";

  # socket-sink flavors: stream jsonl records over the raw socket destination
  # (tcp/udp/unix/unixgram) to the in-VM ncat receiver. For inet dests,
  # `localhost` (dual-stack) resolves to 127.0.0.1 AND ::1 and the sink listens
  # on [::], so whichever family Go's resolver picks reaches it.
  xtcp2SocketSinkArgs = [
    "-dest"
    socketSinkDest
    "-marshal"
    "jsonl"
    "-frequency"
    "2s"
    "-timeout"
    "1s"
  ];

  # minimal (lifecycle) flavor: same fast cadence as basic, but write records
  # as jsonl to a file so the self-test's OUTPUT_CONTENT check can validate the
  # daemon's serialized output (jsonl marshaller → file destination → record
  # field population). Soak deliberately stays on `-dest null` (a file dest
  # would grow unbounded over a multi-hour soak). The file is small over the
  # ~90 s self-test (2 s cadence, tiny records).
  xtcp2FileArgs = [
    "-dest"
    "file:${fileOutputPath}"
    "-marshal"
    "jsonl"
    "-frequency"
    "2s"
    "-timeout"
    "1s"
  ];

  # Phase E: xtcp2 produces directly into the in-VM redpanda. external
  # advertise addr is localhost:19092 so we dial that. -topic matches
  # the clickhouse kafka-engine table's kafka_topic_list. -xtcpProtoFile
  # overrides the hardcoded /xtcp_flat_record.proto default so we can
  # point at the proto NixOS dropped under /etc (see environment.etc
  # block below).
  xtcp2ClickPipeArgs = [
    "-dest"
    "kafka:localhost:19092"
    "-topic"
    clickPipeKafkaTopic
    "-marshal"
    "protobufList"
    "-xtcpProtoFile"
    "/etc/xtcp2/xtcp_flat_record.proto"
    "-frequency"
    "5s"
    "-timeout"
    "2s"
    "-kafkaSchemaUrl"
    "http://localhost:18081"
  ];

  # Metadata-enrichment flags (best-effort, non-fatal). Turned on for the plain
  # clickhouse-pipeline flavor so the ENRICH_* self-test checks can assert the
  # enrichers reach ClickHouse end-to-end (see self-test.nix runEnrichCheck):
  #   -enrichContainer : map each socket's netns → owning docker container via
  #                      the Docker Engine API (/run/docker.sock), stamping
  #                      container_id/name/image. dockerd is already up on this
  #                      flavor (needsDocker), and the redpanda/clickhouse
  #                      containers provide real container netns to discover.
  #   -enrichNic       : read the uplink NIC identity (driver/model/pci/…) from
  #                      sysfs+ethtool. The VM's virtio_net primary NIC populates
  #                      uplink1_nic_driver — the most reliable enrichment check.
  #   -enrichLldp      : read LLDP neighbors from lldpd's control socket
  #                      (/run/lldpd.socket) once at startup. A single VM has no
  #                      neighbor, so this only proves the read is wired + non-
  #                      fatal; -lldpdVersionHint pins the 1.0.22 struct layout
  #                      (pkg/lldp Layout1_0_22) matching the pinned nixpkgs
  #                      lldpd so auto-detect can't guess wrong.
  #   -populateNsid    : best-effort NETNSA_NSID into the nsid column.
  # Socket paths are left at their defaults (docker /run/docker.sock, lldpd
  # /run/lldpd.socket) which match the NixOS services below.
  xtcp2EnrichArgs = [
    "-enrichContainer"
    "-enrichNic"
    "-enrichLldp"
    "-lldpdVersionHint"
    "1.0.22"
    "-populateNsid"
  ];

  # Plain clickhouse-pipeline flavor: kafka pipeline + metadata enrichment.
  xtcp2ClickPipeEnrichArgs = xtcp2ClickPipeArgs ++ xtcp2EnrichArgs;

  # clickhouse-pipeline-rate flavor: same kafka pipeline, but poll jitter is
  # DISABLED so each measurement window's poll count is exactly window/frequency.
  # The rate monitor predicts expected rows = sockets_per_poll × (window/freq)
  # and asserts ClickHouse matches within tolerance — jitter's ±20% cadence
  # variance would defeat that tight prediction. The rate *behavior* is
  # identical; only the variance is removed. The monitor overrides -frequency
  # at runtime via xtcp2ctl, so the 5s here is just the pre-baseline cadence.
  xtcp2ClickPipeRateArgs = xtcp2ClickPipeArgs ++ [
    "-pollJitterPct"
    "0"
  ];

  # clickhouse-pipeline-stress: same kafka pipeline, but enable container-id
  # resolution so records from the tcp-stress load containers carry their
  # docker container_id/runtime (resolved from the socket's cgroup v2 id via
  # pkg/cgroupid). The stress monitor asserts distinct container_ids land in
  # ClickHouse — the enrichment analog of the netns-diversity check.
  xtcp2ClickPipeStressArgs = xtcp2ClickPipeArgs ++ [
    "-resolveContainerId"
  ];

  # clickhouse-http flavor: xtcp2 inserts DIRECTLY into ClickHouse over HTTP,
  # no Kafka. The http dest POSTs the marshalled batch to the VERBATIM -dest
  # URL (destinations_http.go), so the whole ClickHouse insert is baked into
  # -dest: the INSERT query, FORMAT ProtobufList, the existing format_schema
  # mount (same .proto the kafka-engine table uses), and auth via &password.
  # Spaces are '+'-encoded, NOT %20: ExecStart is subject to systemd specifier
  # expansion, and a bare '%' would be mangled/rejected; ClickHouse's HTTP
  # interface decodes '+' as space in the query param, and Go's url.Parse keeps
  # '+' literal in the raw query, so it round-trips. protobufList matches by
  # field NUMBER via the schema (avoids JSONEachRow column-name mismatch).
  # Inserts land in the base MergeTree xtcp.xtcp_flat_records (independent of
  # the idle kafka table + MV). CH HTTP is published at localhost:18123.
  # `localhost` (dual-stack) resolves to both 127.0.0.1 and ::1; ClickHouse
  # binds 0.0.0.0 + :: (config.d/listen.xml), so whichever family Go's resolver
  # picks reaches ClickHouse through the docker -p forward.
  xtcp2ClickHttpArgs = [
    "-dest"
    "http://localhost:18123/?query=INSERT+INTO+xtcp.xtcp_flat_records+FORMAT+ProtobufList&format_schema=xtcp_flat_record.proto:XtcpFlatRecord&password=${clickPipeChPassword}"
    "-marshal"
    "protobufList"
    "-frequency"
    "5s"
    "-timeout"
    "2s"
  ];

  xtcp2CoverageArgs =
    xtcp2BasicArgs
    # sink=coverage-iouring adds -ioUring so the netlinkerIoUring code
    # path runs (otherwise 0% covered; the syscall variant runs by default).
    ++ lib.optionals isCoverageIoUring [ "-ioUring" ];

  # s3parquet flavor: write Parquet straight to MinIO. Lifecycle-test
  # threshold dropped to 1 MiB so a 90 s boot exercise actually triggers
  # a finalize+upload; production default (set via
  # S3_PARQUET_FLUSH_BYTES=0) is 63 MiB.
  xtcp2S3ParquetArgs = [
    "-dest"
    "s3parquet:http://127.0.0.1:9000"
    "-marshal"
    "protobufList"
    "-frequency"
    "2s"
    "-timeout"
    "1s"
    "-s3Bucket"
    "xtcp2-records"
    "-s3AccessKey"
    "xtcp2test"
    "-s3SecretKey"
    "xtcp2testsecret"
    "-s3ParquetFlushBytes"
    "1048576"
  ];

  # The three broker flavors (valkey/nats/nsq) share one arg shape: publish
  # each poll's records to <dest> on <topic> as protobufList, 2s poll / 1s
  # timeout. protobufList keeps them consistent with the other flavors; the
  # self-test counts delivered messages, independent of payload encoding.
  # -topic maps to config.Topic (the pub/sub channel / subject / nsq topic).
  mkBrokerArgs =
    {
      dest,
      topic,
    }:
    [
      "-dest"
      dest
      "-marshal"
      "protobufList"
      "-topic"
      topic
      "-frequency"
      "2s"
      "-timeout"
      "1s"
    ];

  # valkey: xtcp2 PUBLISHes to the Valkey pub/sub channel `valkeyTopic`.
  valkeyTopic = "xtcp2-records";
  xtcp2ValkeyArgs = mkBrokerArgs {
    dest = "valkey:127.0.0.1:6379";
    topic = valkeyTopic;
  };

  # nats: xtcp2 PUBLISHes to the NATS subject `natsTopic`.
  natsTopic = "xtcp2-records";
  xtcp2NatsArgs = mkBrokerArgs {
    dest = "nats:127.0.0.1:4222";
    topic = natsTopic;
  };

  # nsq: xtcp2 PUBLISHes to the nsq topic `nsqTopicName`; the in-VM nsq_tail
  # consumer reads them on `nsqChannelName`.
  nsqTopicName = "xtcp2-records";
  nsqChannelName = "selftest";
  xtcp2NsqArgs = mkBrokerArgs {
    dest = "nsq:127.0.0.1:4150";
    topic = nsqTopicName;
  };
in
(nixpkgs.lib.nixosSystem {
  inherit pkgs;

  modules = [
    microvm.nixosModules.microvm
    ../modules/xtcp2-service.nix
  ]
  ++ lib.optionals isAnyS3Parquet s3ParquetModules
  ++ lib.optionals isValkey valkeyModules
  ++ lib.optionals isNats natsModules
  ++ lib.optionals isNsq nsqModules
  ++ [
    (
      { config, ... }:
      {
        system.stateVersion = "26.05";
        networking.hostName = cfg.hostname;

        # Trim VM surface area
        documentation.enable = false;
        documentation.man.enable = false;
        documentation.doc.enable = false;
        documentation.info.enable = false;
        documentation.nixos.enable = false;
        security.polkit.enable = false;
        services.udisks2.enable = false;
        programs.command-not-found.enable = false;
        fonts.fontconfig.enable = false;
        nix.enable = false;
        xdg.mime.enable = false;
        hardware.enableRedistributableFirmware = false;
        boot.supportedFilesystems = lib.mkForce [
          "vfat"
          "ext4"
        ];

        # When microvm.forwardPorts maps host → guest, the NixOS
        # firewall on the guest still has to allow the inbound packet.
        # Open the same set of ports that forwardPorts above covers,
        # gated by the same flavor predicates. Default firewall in
        # NixOS is enabled and blocks everything but ssh, so without
        # these `curl 127.0.0.1:18123` from the host gets a TCP RST.
        networking.firewall.allowedTCPPorts =
          lib.optionals (isTcpStress || isAnyClickPipe || isAnyS3Parquet) [
            9088 # xtcp2 prometheus
            8889 # xtcp2 grpc
          ]
          ++ lib.optional isTcpStress 9090 # in-VM Prometheus
          ++ lib.optionals isAnyS3Parquet [
            9000 # MinIO API
            9001 # MinIO console
          ]
          ++ lib.optionals isS3ParquetLong [
            14040 # Pyroscope OSS UI + ingest
          ]
          ++ lib.optionals isAnyClickPipe [
            18123 # clickhouse HTTP
            19001 # clickhouse native
            19092 # redpanda kafka external
            19644 # redpanda admin
            18081 # schema registry
            3000 # grafana
            9090 # prometheus (host accesses via :19090 → guest :9090)
          ]
          ++ lib.optionals isClickPipeParquet [
            # Second xtcp2 instance's prom + grpc endpoints (parquet path).
            9089
            8890
          ];

        microvm = {
          hypervisor = "qemu";
          mem = effectiveMem;
          vcpu = cfg.vcpu;
          cpu = if cfg.useKvm then null else cfg.qemuCpu;
          # Default: no disk. /var/lib/docker lives on the root tmpfs.
          # For clickhouse-pipeline this proved a problem at hour ~1
          # of a 12h run: clickhouse_db's MergeTree storage saturated
          # the tmpfs cap, threw NOT_ENOUGH_SPACE 700+ times, the
          # kafka_engine couldn't commit offsets, back-pressure froze
          # xtcp2's producer, row count plateaued at ~18k. Fix: give
          # docker its own ext4 disk on the host so /var/lib/docker
          # gets real (not RAM) bytes. 16 GiB covers a 24h soak with
          # MergeTree compression (~3.6 GiB / 24h) + dockerd working
          # set + redpanda topic data + redpanda segment log (uncapped
          # by default). The earlier 8 GiB hit 99 % at T+22h of a 24h
          # soak.
          volumes =
            lib.optionals isAnyClickPipe [
              {
                # User-writable path so microvm-run can autoCreate the
                # image without sudo. /tmp is RAM-backed on most distros
                # but big enough for the 16 GiB image; if you want
                # cross-boot persistence move this to ~/.cache or a
                # mounted disk and add `microvm.preStart` to mkdir.
                image = "/tmp/xtcp2-microvm-clickhouse-pipeline-docker.img";
                mountPoint = "/var/lib/docker";
                size = 16384;
                autoCreate = true;
                fsType = "ext4";
                label = "xtcp2dock";
              }
            ]
            ++ lib.optionals isClickPipeParquet [
              {
                # Dedicated disk for MinIO data in the mixed
                # clickhouse-pipeline-parquet flavor. Default
                # minio-bucket-bootstrap.nix puts /var/lib/minio on
                # a 512 MiB tmpfs — fine for short smokes, ran out
                # at T+22h of a 24h soak (the parquet path uploads
                # ~10 MiB/min sustained → 14 GiB over 24h). 16 GiB
                # ext4 disk covers a full 24h with margin; sparse
                # file so disk space on the host is consumed
                # incrementally.
                image = "/tmp/xtcp2-microvm-clickhouse-pipeline-minio.img";
                mountPoint = "/var/lib/minio";
                size = 16384;
                autoCreate = true;
                fsType = "ext4";
                label = "xtcp2minio";
              }
            ]
            ++ lib.optionals isS3ParquetStress [
              {
                # s3parquet-stress spawns the tcp-stress load containers, so it
                # needs dockerd on a real disk for /var/lib/docker (the load
                # image + 20 containers) — same rationale as the clickpipe
                # docker disk above. Own image path so flavors never share
                # state (a reused image once skipped ClickHouse initdb and
                # corrupted the docker image store).
                image = "/tmp/xtcp2-microvm-s3parquet-stress-docker.img";
                mountPoint = "/var/lib/docker";
                size = 16384;
                autoCreate = true;
                fsType = "ext4";
                label = "xtcp2s3pdock";
              }
              {
                # Dedicated disk for the MinIO parquet objects. Bounded by the
                # ~1h retention cleanup (xtcp2-s3parquet-retention.service), so
                # 16 GiB is ample steady-state headroom; sparse file, grown
                # incrementally. The disk guard asserts it never saturates.
                image = "/tmp/xtcp2-microvm-s3parquet-stress-minio.img";
                mountPoint = "/var/lib/minio";
                size = 16384;
                autoCreate = true;
                fsType = "ext4";
                label = "xtcp2s3pmin";
              }
            ];
          interfaces = [
            {
              type = "user";
              id = "eth0";
              mac = "02:00:00:00:10:01";
            }
          ];
          # Host → guest port forwards via qemu's SLiRP hostfwd. Only
          # applies when the interface is `type = "user"` (which it is
          # for every flavor here). Each entry maps a host port to the
          # SAME guest port — so e.g. `curl 127.0.0.1:18123` on the
          # host hits clickhouse's HTTP endpoint inside the VM, which
          # the docker `-p 18123:8123` mapping then routes into the
          # clickhouse container.
          forwardPorts =
            lib.optionals (isTcpStress || isAnyClickPipe || isAnyS3Parquet) [
              # xtcp2 daemon's prometheus + grpc endpoints — same on
              # every flavor that runs xtcp2 with networking surface.
              {
                from = "host";
                host.port = 9088;
                guest.port = 9088;
              }
              {
                from = "host";
                host.port = 8889;
                guest.port = 8889;
              }
            ]
            ++ lib.optionals isAnyS3Parquet [
              # MinIO API (9000) and console (9001) — lets host-side
              # `mc ls` and a browser hit the in-VM MinIO from the dev box.
              {
                from = "host";
                host.port = 9000;
                guest.port = 9000;
              }
              {
                from = "host";
                host.port = 9001;
                guest.port = 9001;
              }
            ]
            ++ lib.optionals (isS3ParquetLong || isS3ParquetStress || isS3ParquetLowfreq) [
              # Pyroscope UI on the long-soak / stress flavors so operators can
              # open http://127.0.0.1:14040 from the host and inspect
              # the live profile. Port shifted off the canonical 4040
              # because pyroscope was failing to bind it inside the
              # VM (still investigating; alternate port lets the run
              # proceed).
              {
                from = "host";
                host.port = 14040;
                guest.port = 14040;
              }
            ]
            ++ lib.optionals isTcpStress [
              # in-VM Prometheus server for the tcp-stress flavor. Host side
              # shifted to 19090 (guest stays 9090) so it doesn't collide with
              # a Prometheus already running on the dev box's :9090 — qemu
              # refuses to start if the hostfwd port is taken. Matches the
              # clickpipe convention (host :19090 → guest :9090).
              {
                from = "host";
                host.port = 19090;
                guest.port = 9090;
              }
            ]
            ++ lib.optionals isAnyClickPipe [
              # ClickHouse HTTP (clickhouse-client uses it via 8123,
              # native via 9000; the docker run publishes them on 18123
              # and 19001 respectively to avoid clashing with anything
              # else on the VM).
              {
                from = "host";
                host.port = 18123;
                guest.port = 18123;
              }
              {
                from = "host";
                host.port = 19001;
                guest.port = 19001;
              }
              # Redpanda external Kafka API + admin + schema registry.
              {
                from = "host";
                host.port = 19092;
                guest.port = 19092;
              }
              {
                from = "host";
                host.port = 19644;
                guest.port = 19644;
              }
              {
                from = "host";
                host.port = 18081;
                guest.port = 18081;
              }
              # Grafana on the VM host (not in docker). Use host:13000
              # (instead of :3000) because :3000 is a popular dev-server
              # default that often clashes — your host may have its
              # own Grafana / next.js / etc. already there.
              {
                from = "host";
                host.port = 13000;
                guest.port = 3000;
              }
              # Prometheus inside the VM is reachable to Grafana via
              # 127.0.0.1:9090 internally — host-side access via
              # 19090 (avoiding the common :9090 clash).
              {
                from = "host";
                host.port = 19090;
                guest.port = 9090;
              }
            ]
            ++ lib.optionals isClickPipeParquet [
              # Second xtcp2 instance's prom + grpc — the secondary
              # parquet-writing instance binds these (encoded in
              # xtcp2ClickPipeParquetArgs). Host curl :9089/metrics
              # shows the s3parquet upload counter directly.
              {
                from = "host";
                host.port = 9089;
                guest.port = 9089;
              }
              {
                from = "host";
                host.port = 8890;
                guest.port = 8890;
              }
            ];
          shares = [
            {
              source = "/nix/store";
              mountPoint = "/nix/store";
              tag = "nix-store";
              proto = "9p";
            }
          ];
          qemu = {
            serialConsole = false;
            machine = cfg.qemuMachine;
            package = pkgs.qemu_kvm;
            extraArgs = [
              "-name"
              "${cfg.hostname},process=${cfg.hostname}"
              "-serial"
              "tcp:127.0.0.1:${toString cfg.serialPort},server,nowait"
              "-device"
              "virtio-serial-pci"
              "-chardev"
              "socket,id=virtcon,port=${toString cfg.virtioPort},host=127.0.0.1,server=on,wait=off"
              "-device"
              "virtconsole,chardev=virtcon"
              "-append"
              (builtins.concatStringsSep " " (
                [
                  "console=ttyS0,115200"
                  "console=hvc0"
                  "reboot=t"
                  "panic=-1"
                  "loglevel=4"
                  "init=${config.system.build.toplevel}/init"
                ]
                ++ config.boot.kernelParams
              ))
            ];
          };
        };

        boot.kernelPackages = pkgs.linuxPackages_latest;
        boot.kernelParams = [
          "console=ttyS0,115200"
          "console=hvc0"
          "systemd.default_standard_error=journal+console"
          "systemd.show_status=true"
        ];
        boot.initrd.availableKernelModules = [
          "9p"
          "9pnet"
          "9pnet_virtio"
          "virtio_pci"
          "virtio_console"
        ];
        boot.initrd.systemd.emergencyAccess = true;

        # netlink + io_uring sysctls
        boot.kernel.sysctl = {
          # io_uring availability is gated by kernel.io_uring_disabled on newer kernels
          "kernel.io_uring_disabled" = 0;
        };

        # xtcp2 discovers namespaces via the /proc/<pid>/ns/net scan
        # (Method B), but its name resolver still reads /run/netns/ and
        # /run/docker/netns/ to map a discovered inode to a best-effort
        # bind-mount NAME (xtcp2 now tolerates these dirs being absent —
        # pkg/xtcp/init.go). Pre-create BOTH in every flavor so name
        # resolution works and the self-test's Check 10 (NS_DOCKER) has a
        # target to bind-mount into. Creating an empty /run/docker/netns/
        # doesn't pull docker in — the daemon just reads an empty dir.
        systemd.tmpfiles.rules = [
          "d /run/netns 0755 root root -"
          "d /run/docker 0755 root root -"
          "d /run/docker/netns 0755 root root -"
        ]
        ++ lib.optionals isCoverage [
          "d ${coverDir} 0755 root root -"
        ];

        # GOCOVERDIR for the coverage-instrumented xtcp2 build. The runtime
        # writes covcounters.* + covmeta files into this directory on clean
        # exit (SIGTERM via systemctl stop). The self-test scrapes those
        # files between XTCP2_COVERAGE_DUMP_{START,END} markers.
        # GOCOVERDIR for coverage flavors + (clickhouse-pipeline) ordering after
        # the enricher sockets. Merged because isCoverage and isClickPipe are
        # mutually exclusive but both target systemd.services.xtcp2.
        systemd.services.xtcp2 = lib.mkMerge [
          (lib.mkIf isCoverage {
            environment.GOCOVERDIR = coverDir;
          })
          # Order xtcp2 after lldpd + docker so the enricher sockets exist by the
          # time xtcp2 reads LLDP/container metadata at startup. Both reads are
          # best-effort, so this is about making the positive checks meaningful,
          # not correctness.
          (lib.mkIf isClickPipe {
            after = [
              "lldpd.service"
              "docker.service"
            ];
            wants = [ "lldpd.service" ];
          })
        ];

        # clickhouse-pipeline flavor: run lldpd so xtcp2's -enrichLldp has a real
        # control socket (/run/lldpd.socket) to read. The NixOS lldpd module
        # ships lldpd 1.0.22 (matching pkg/lldp Layout1_0_22 / -lldpdVersionHint
        # above). A single VM has no LLDP peer, so this only proves the read path
        # is wired and non-fatal — the ENRICH_LLDP self-test check asserts the
        # best-effort contract (socket present + daemon healthy + records
        # flowing), and ENRICH_BESTEFFORT_NEGATIVE proves a MISSING socket is
        # also non-fatal.
        services.lldpd.enable = lib.mkIf isClickPipe true;

        # Hold a live process inside a test network namespace before xtcp2
        # starts, so xtcp2's /proc/<pid>/ns/net scan (Method B) discovers it
        # and exercises netNamespaceInstance → openAndSetNSWithRetries →
        # openDefaultNetLinkSocket. NOTE: under Method B an empty
        # `ip netns add` (no process in the ns) is invisible to the /proc
        # scan — the namespace must hold a live process to be discovered — so
        # this keeps a `sleep infinity` running inside it, not a bare oneshot.
        # Otherwise those code paths stay at 0% even with coverage instrumentation.
        systemd.services.create-test-netns = lib.mkIf isCoverage {
          description = "Hold a live process in a test netns for xtcp2 coverage";
          wantedBy = [ "xtcp2.service" ];
          before = [ "xtcp2.service" ];
          after = [ "local-fs.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStartPre = "${pkgs.iproute2}/bin/ip netns add xtcpcovns";
            ExecStart = "${pkgs.iproute2}/bin/ip netns exec xtcpcovns ${pkgs.coreutils}/bin/sleep infinity";
            ExecStopPost = "${pkgs.iproute2}/bin/ip netns delete xtcpcovns";
          };
        };

        services.getty.autologinUser = "root";
        systemd.enableEmergencyMode = false;

        # The reason we're here: xtcp2 as a systemd unit
        services.xtcp2 = {
          enable = true;
          package = xtcp2Package;
          configFile = vmConfig;
          extraArgs =
            if isCoverage then
              xtcp2CoverageArgs
            else if isClickPipeRate then
              # Rate test: kafka pipeline with poll jitter disabled for a
              # deterministic per-window poll count (see xtcp2ClickPipeRateArgs).
              xtcp2ClickPipeRateArgs
            else if isClickHttp then
              # Direct HTTP→ClickHouse: insert straight into CH over HTTP,
              # bypassing Kafka (must come before isAnyClickPipe, which it is
              # a member of, so it doesn't fall into the kafka-dest branch).
              xtcp2ClickHttpArgs
            else if isClickPipeStress then
              # Kafka pipeline + container-id resolution (before isAnyClickPipe).
              xtcp2ClickPipeStressArgs
            else if isClickPipe then
              # Plain kafka pipeline + metadata enrichment (container/nic/lldp/
              # nsid). Enrichers are best-effort; the ENRICH_* self-test checks
              # assert they reach ClickHouse. Must precede isAnyClickPipe (of
              # which isClickPipe is a member).
              xtcp2ClickPipeEnrichArgs
            else if isAnyClickPipe then
              # Phase E: produce to redpanda → clickhouse via kafka dest.
              # The mixed flavor uses these args for its primary xtcp2
              # instance (kafka path); a second instance writing parquet
              # is declared separately below.
              xtcp2ClickPipeArgs
            else if isS3Parquet then
              # s3parquet lifecycle flavor: 1 MiB flush threshold so the
              # 90 s boot exercise triggers a finalize+upload.
              xtcp2S3ParquetArgs
            else if isS3ParquetLong || isCapCheckFail then
              # s3parquet-long flavor: production 63 MiB flush threshold,
              # 10 s polling. Pairs with mkS3ParquetRunner.
              # capcheck-fail reuses the same args (so the daemon's
              # config is otherwise valid; the capability check is the
              # only thing that fails).
              xtcp2S3ParquetLongArgs
            else if isS3ParquetStress then
              # s3parquet-stress flavor: production parquet config under the
              # tcp-stress load containers. Pairs with mkS3ParquetStressRunner.
              xtcp2S3ParquetStressArgs
            else if isS3ParquetLowfreq then
              # s3parquet-lowfreq: 1h poll + 2 sockets/container — timer-only
              # parquet flush. Reuses mkS3ParquetStressRunner.
              xtcp2S3ParquetLowfreqArgs
            else if isValkey then
              # valkey flavor: PUBLISH each poll's records to the in-VM Valkey
              # pub/sub channel; the self-test's subscriber counts deliveries.
              xtcp2ValkeyArgs
            else if isNats then
              # nats flavor: PUBLISH each poll's records to the in-VM NATS
              # subject; the self-test's subscriber counts deliveries.
              xtcp2NatsArgs
            else if isNsq then
              # nsq flavor: PUBLISH each poll's records to the in-VM nsqd topic;
              # the self-test reads nsqd's per-channel finish_count.
              xtcp2NsqArgs
            else if isMinimal then
              # minimal (lifecycle) writes jsonl to a file so OUTPUT_CONTENT
              # can validate the daemon's serialized output.
              xtcp2FileArgs
            else if isSocketSink then
              # socket-sink: stream jsonl over the raw tcp/udp/unix/unixgram
              # dest to the ncat sink.
              xtcp2SocketSinkArgs
            else
              # Soak reuses the basic args (`-dest null`, fast frequency).
              # The point of soak is namespace + netlink churn, not
              # downstream destination throughput.
              xtcp2BasicArgs;
          # capcheck-fail intentionally drops CAP_SYS_ADMIN. Anything
          # else gets the default full set.
          capabilities = lib.mkIf isCapCheckFail [
            "CAP_NET_ADMIN"
            "CAP_NET_RAW"
            "CAP_SYS_RESOURCE"
            # CAP_SYS_ADMIN omitted on purpose — startup capability
            # check should refuse to start with a clear diagnostic.
          ];
        };

        # Second xtcp2 instance for the mixed flavor: writes parquet
        # to MinIO in parallel with the kafka-producing primary
        # instance above. Same caps, different prom + grpc ports
        # (encoded in xtcp2ClickPipeParquetArgs), no extra docker /
        # MinIO setup needed (the bucket bootstrap module is already
        # imported by s3ParquetModules under isAnyS3Parquet).
        systemd.services.xtcp2-parquet = lib.mkIf isClickPipeParquet {
          description = "xtcp2 — TCP socket introspection (parquet sink, secondary instance)";
          after = [
            "network-online.target"
            "xtcp2-bucket-bootstrap.service"
          ];
          wants = [
            "network-online.target"
            "xtcp2-bucket-bootstrap.service"
          ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = "${xtcp2Package}/bin/xtcp2 ${lib.concatStringsSep " " xtcp2ClickPipeParquetArgs}";
            Restart = "on-failure";
            RestartSec = "2s";
            User = "root";
            AmbientCapabilities = [
              "CAP_NET_ADMIN"
              "CAP_NET_RAW"
              "CAP_SYS_RESOURCE"
              "CAP_SYS_ADMIN"
            ];
            CapabilityBoundingSet = [
              "CAP_NET_ADMIN"
              "CAP_NET_RAW"
              "CAP_SYS_RESOURCE"
              "CAP_SYS_ADMIN"
            ];
            TasksMax = 8192;
            LimitNPROC = 8192;
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # Self-test oneshot. The self-test's check 1 retries `systemctl
        # is-active xtcp2` for 30 s, robust to xtcp2 starting directly at
        # boot or via a systemd.path gate. Skipped on long-running flavors
        # (soak / s3parquet-long), which run heartbeat services instead.
        systemd.services.xtcp2-self-test = lib.mkIf (!isSoak && !isS3ParquetLong && !isClickPipeRate) {
          description = "xtcp2 microvm self-test";
          after = [
            "xtcp2.service"
            "multi-user.target"
          ];
          wants = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${selfTest}/bin/xtcp2-self-test";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # socket-sink flavors: an ncat receiver that appends everything xtcp2
        # streams over the raw socket destination (tcp/udp/unix/unixgram) to
        # socketSinkFile, for the RAW_SOCKET self-test check to count +
        # validate. inet dests listen dual-stack on [::] (bindv6only=0 → also
        # accepts IPv4-mapped) so `localhost` on the daemon side reaches it on
        # either family. Ordered before xtcp2 so the dest dial doesn't race a
        # missing listener (xtcp2's Restart=on-failure also covers the race).
        systemd.services.xtcp2-socket-sink = lib.mkIf isSocketSink {
          description = "xtcp2 socket-sink — ncat receiver for the raw ${socketSinkScheme} dest";
          before = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = "${pkgs.writeShellScript "xtcp2-socket-sink" ''
              # A stale unix socket path would make ncat fail to bind on restart.
              rm -f ${socketSinkPath} 2>/dev/null || true
              exec ${socketSinkReceiverCmd} > ${socketSinkFile}
            ''}";
            Restart = "on-failure";
            RestartSec = "1s";
            StandardError = "journal+console";
          };
        };

        # Soak flavor: long-running services that churn namespaces + scrape
        # /metrics into a file inside the VM. The host-side soak runner
        # (see nix/microvms/lib.nix mkSoakRunner) boots the VM, sleeps for
        # the configured -duration, then powers it off and inspects the
        # metric log + journal for crashes/restarts.
        systemd.services.xtcp2-soak-churn = lib.mkIf (isSoak || isS3ParquetLong || isClickPipeParquet) {
          description = "xtcp2 soak — nsTest namespace churn driver";
          after = [
            "xtcp2.service"
            "multi-user.target"
          ];
          wants = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = "${soakChurnScript}/bin/xtcp2-soak-churn";
            # Soak runs are open-ended. If nsTest itself crashes we want
            # systemd to restart it so the soak workload keeps generating
            # load even across an `ip netns` blip.
            Restart = "on-failure";
            RestartSec = "2s";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # s3parquet-long / s3parquet-stress: MinIO upload monitor. Sentinel
        # format mirrors XTCP2_CLICKPIPE_ROWS so the host-side runner can grep
        # for it with the same idiom. Cadence is S3PARQUET_REPORT_INTERVAL
        # (seconds); the stress flavor uses a tight 30 s so the soak heartbeat
        # and disk-guard track closely, the long flavor keeps the 60 s default.
        systemd.services.xtcp2-s3parquet-monitor =
          lib.mkIf (isS3ParquetLong || isS3ParquetStress || isS3ParquetLowfreq)
            {
              description = "xtcp2 s3parquet — MinIO upload-count + disk reporter";
              after = [
                "xtcp2.service"
                "multi-user.target"
              ];
              wants = [ "xtcp2.service" ];
              wantedBy = [ "multi-user.target" ];
              environment.S3PARQUET_REPORT_INTERVAL = toString (
                if isS3ParquetStress then 30 else s3ParquetReportIntervalDefault
              );
              serviceConfig = {
                Type = "simple";
                ExecStart = "${s3ParquetMonitorScript}/bin/xtcp2-s3parquet-monitor";
                # Crash-loop here would silently hide xtcp2's progress; restart
                # so a brief mc/MinIO blip doesn't permanently silence the
                # sentinel stream.
                Restart = "on-failure";
                RestartSec = "5s";
                StandardOutput = "journal+console";
                StandardError = "journal+console";
              };
            };

        # s3parquet-stress: bounded ~1h object retention (analog of the
        # clickhouse-stress 1h table TTL). Keeps the MinIO disk at steady state.
        systemd.services.xtcp2-s3parquet-retention = lib.mkIf isS3ParquetStress {
          description = "xtcp2 s3parquet-stress — delete parquet objects older than 1h";
          after = [
            "minio.service"
            "xtcp2-bucket-bootstrap.service"
          ];
          wants = [ "minio.service" ];
          wantedBy = [ "multi-user.target" ];
          # Production window: keep ~1h of objects, swept every 10 min (the
          # script defaults). Override S3PARQUET_RETENTION_AGE /
          # S3PARQUET_RETENTION_INTERVAL here to verify deletions in a short
          # run (e.g. "10m" / "120" fires within a 1h run — validated 2026-07-28).
          serviceConfig = {
            Type = "simple";
            ExecStart = "${s3ParquetRetentionScript}/bin/xtcp2-s3parquet-retention";
            Restart = "on-failure";
            RestartSec = "10s";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        systemd.services.xtcp2-soak-scrape = lib.mkIf isSoak {
          description = "xtcp2 soak — periodic /metrics scraper";
          after = [
            "xtcp2.service"
            "multi-user.target"
          ];
          wants = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            # Use shell redirect so each line is JSON. /var/log is tmpfs in
            # microvm — the host runner tar-scrapes this path before the
            # poweroff completes.
            ExecStart = "${pkgs.bash}/bin/bash -c '${soakScrapeScript}/bin/xtcp2-soak-scrape >> ${soakMetricsLog}'";
            Restart = "on-failure";
            RestartSec = "2s";
            StandardOutput = "journal";
            StandardError = "journal+console";
          };
        };

        # Phase A — native TCP stress: spin up N echo-listeners + N clients
        # in the VM's default netns. Gives xtcp2's inet_diag readout a
        # known population of ESTABLISHED sockets with measurable RTT /
        # bytes-sent / segs-out for the parser to chew on. The two units
        # below run alongside the nsTest churn for the soak flavor.
        systemd.services.xtcp2-soak-tcp-server =
          lib.mkIf (isSoak || isS3ParquetLong || isClickPipeParquet || isClickPipeRate)
            {
              description = "xtcp2 soak — tcp_server echo listeners";
              after = [ "network-online.target" ];
              wants = [ "network-online.target" ];
              wantedBy = [ "multi-user.target" ];
              serviceConfig = {
                Type = "simple";
                ExecStart = "${xtcp2AllPackage}/bin/tcp_server -count ${toString soakTcpServerCount} -bind 0.0.0.0";
                Restart = "on-failure";
                RestartSec = "2s";
                # Need enough fd headroom for `tcpServerCount` listeners +
                # `tcpClientCount` accepted conns. Default nofile is 1024;
                # bump it explicitly.
                LimitNOFILE = 65536;
                StandardOutput = "journal";
                StandardError = "journal+console";
              };
            };

        # Inject brief loopback TCP traffic INSIDE each ns. The
        # tcp_server/tcp_client pair above lives in the default ns
        # only — without this service the per-namespace netlink reads
        # would be empty and parquet would build nothing.
        #
        # NOTE: replaced by nsTest's in-process -traffic flag (see
        # soakChurnScript). This unit is left guarded behind `false`
        # so callers / debug references still resolve but the broken
        # shell-loop variant doesn't try to run.
        systemd.services.xtcp2-soak-ns-traffic = lib.mkIf false {
          description = "xtcp2 soak — in-namespace TCP loopback injector";
          # No After/Wants on xtcp2-soak-churn — that creates a
          # systemd ordering cycle (caught it in the first
          # aggressive 12 h: the unit got SKIPped with
          # "Ordering cycle found"). The driver script already
          # idles when /run/netns/ is empty, so racing churn at
          # boot is fine.
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          wantedBy = [ "multi-user.target" ];
          # The first aggressive 12 h soak ran but produced
          # files=0 / envelopeRows=72 across the whole 12 h. The
          # `ip link set lo up` inside the entered netns was
          # silently failing (script swallowed errors) because
          # systemd's default service caps don't cover what
          # `ip netns exec` needs to manipulate interfaces in the
          # new ns. Grant the same set xtcp2 itself uses + put
          # them in Ambient so child processes (ip, ncat) inherit.
          serviceConfig = {
            Type = "simple";
            ExecStart = "${soakNsTrafficScript_UNUSED}/bin/xtcp2-soak-ns-traffic";
            Restart = "on-failure";
            RestartSec = "2s";
            AmbientCapabilities = [
              "CAP_NET_ADMIN"
              "CAP_NET_RAW"
              "CAP_SYS_ADMIN"
            ];
            CapabilityBoundingSet = [
              "CAP_NET_ADMIN"
              "CAP_NET_RAW"
              "CAP_SYS_ADMIN"
            ];
            # Lots of short-lived processes per cycle.
            TasksMax = 8192;
            LimitNOFILE = 65536;
            # Errors from the inject helper must reach console so
            # cap/perms regressions don't silently produce
            # files=0 runs again.
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        systemd.services.xtcp2-soak-tcp-client =
          lib.mkIf (isSoak || isS3ParquetLong || isClickPipeParquet || isClickPipeRate)
            {
              description = "xtcp2 soak — tcp_client traffic generators";
              # tcp_server takes a moment to bind all N ports — gate the
              # clients behind its readiness so the dial-retry loop in
              # tcp_client doesn't burn through its budget at boot.
              after = [
                "xtcp2-soak-tcp-server.service"
                "network-online.target"
              ];
              wants = [
                "xtcp2-soak-tcp-server.service"
                "network-online.target"
              ];
              wantedBy = [ "multi-user.target" ];
              serviceConfig = {
                Type = "simple";
                # Brief delay so the server's Accept loop is up. tcp_client
                # also retries dial up to -dialr times so this is belt+suspenders.
                ExecStartPre = "${pkgs.coreutils}/bin/sleep 2";
                ExecStart = "${xtcp2AllPackage}/bin/tcp_client -count ${toString soakTcpClientCount} -connect ${soakTcpConnect} -sleep ${soakTcpClientSleep} -pads ${toString soakTcpPads}";
                Restart = "on-failure";
                RestartSec = "2s";
                LimitNOFILE = 65536;
                StandardOutput = "journal";
                StandardError = "journal+console";
              };
            };

        # Enable docker daemon for any flavor that needs it. Adds
        # ~150 MiB to the VM image (dockerd + containerd) but keeps the
        # rest of the surface minimal — no docker-buildx, no compose.
        virtualisation.docker = lib.mkIf needsDocker {
          enable = true;
          # Disable docker's bridge auto-configuration via iptables to
          # avoid microvm-vs-host iptables-version drift. Containers
          # still get bridge networking via dockerd's default bridge.
          enableOnBoot = true;
        };

        # Phase D: Prometheus server inside the tcp-stress VM, scraping
        # xtcp2's /metrics endpoint every 15s. Lets us run a long-form
        # session (300s smoke → 12h) and inspect what counters did over
        # time: per-ns Netlinker.p / .packets / start, netNamespaceInstance
        # start, reconcile start/stores, GC behaviour, etc. The server listens on
        # 127.0.0.1:9090; the runner also includes a periodic snapshot
        # service that curls Prometheus and writes per-query JSON lines
        # to a file so the user sees concrete data even if they don't
        # log into the VM to browse the web UI.
        services.prometheus = lib.mkIf (isTcpStress || isAnyClickPipe) {
          enable = true;
          port = 9090;
          listenAddress = "0.0.0.0";
          globalConfig = {
            scrape_interval = "15s";
            evaluation_interval = "15s";
          };
          scrapeConfigs = [
            {
              job_name = "xtcp2";
              static_configs = [
                {
                  targets = [ "127.0.0.1:${toString cfg.promPort}" ];
                  labels.instance = "xtcp2-primary";
                }
              ]
              ++ lib.optional isClickPipeParquet {
                # The mixed flavor runs a second xtcp2 instance for the
                # parquet path on port 9089. Scrape both so we can
                # compare goroutine/memory/GC trends across the two
                # backends side-by-side in Grafana / promql.
                targets = [ "127.0.0.1:9089" ];
                labels.instance = "xtcp2-parquet";
              };
            }
            {
              job_name = "prometheus-self";
              static_configs = [
                {
                  targets = [ "127.0.0.1:9090" ];
                  labels.instance = "prometheus-vm";
                }
              ];
            }
          ];
          # Keep retention well above the longest planned soak (12h).
          # Storage lives in /var/lib/prometheus2 which is tmpfs in this
          # VM — a 12h run with 15s scrape ≈ 2880 samples per series,
          # well under the default ~16 GiB block budget.
          retentionTime = "48h";
        };

        # Phase F: Grafana on the clickhouse-pipeline flavor. Browses
        # both data sources we already have inside the VM:
        #   1. ClickHouse @ localhost:19001 (docker bridge maps 9000 of
        #      the container → host port 19001). The grafana-clickhouse-
        #      datasource plugin from nixpkgs handles wire protocol.
        #   2. Prometheus @ localhost:9090 (in-VM TSDB scraping xtcp2:9088).
        # Grafana itself listens on 0.0.0.0:3000; microvm.forwardPorts
        # (below) opens that to the host so the operator can browse
        # http://127.0.0.1:3000 directly. Default admin/admin login —
        # change via grafana UI on first browse, or set a password via
        # services.grafana.settings.security.admin_password.
        services.grafana = lib.mkIf isAnyClickPipe {
          enable = true;
          declarativePlugins = with pkgs.grafanaPlugins; [
            grafana-clickhouse-datasource
          ];
          settings = {
            server = {
              http_addr = "0.0.0.0";
              http_port = 3000;
              root_url = "http://127.0.0.1:3000/";
            };
            "auth.anonymous" = {
              enabled = true;
              org_role = "Viewer";
            };
            analytics.reporting_enabled = false;
            # NixOS module asserts secret_key is set explicitly so a
            # silent upgrade can't lose access to encrypted secrets in
            # the DB. This is a local-dev microvm so a hardcoded key
            # is fine — change for production deployments.
            security.secret_key = "xtcp2-local-dev-microvm-secret-key";
          };
          provision = {
            enable = true;
            datasources.settings = {
              apiVersion = 1;
              datasources = [
                {
                  name = "xtcp2-clickhouse";
                  type = "grafana-clickhouse-datasource";
                  uid = "xtcp2-clickhouse";
                  access = "proxy";
                  # Docker -p 19001:9000 exposes ClickHouse native protocol
                  # on the VM host's localhost. Grafana runs on the VM
                  # host (not in docker) so it connects there.
                  jsonData = {
                    host = "127.0.0.1";
                    port = 19001;
                    username = "default";
                    protocol = "native";
                    defaultDatabase = "xtcp";
                    secure = false;
                  };
                  secureJsonData.password = clickPipeChPassword;
                  isDefault = true;
                }
                {
                  name = "xtcp2-prometheus";
                  type = "prometheus";
                  uid = "xtcp2-prometheus";
                  access = "proxy";
                  url = "http://127.0.0.1:9090";
                  isDefault = false;
                }
              ];
            };
          };
        };

        # Snapshot service: every 30s, query Prometheus for a handful of
        # key xtcp2 metrics and append a JSON line to a tmpfs log file.
        # On exit the runner prints the last few lines so the user has
        # concrete evidence Prometheus collected data without needing
        # to log into the VM.
        systemd.services.xtcp2-prom-snapshot = lib.mkIf isTcpStress {
          description = "xtcp2 tcp-stress — periodic Prometheus query snapshots";
          after = [ "prometheus.service" ];
          wants = [ "prometheus.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = pkgs.writeShellScript "xtcp2-prom-snapshot" ''
              set -u
              # Wait for Prometheus to come up.
              for _ in $(seq 1 30); do
                if ${pkgs.curl}/bin/curl --silent --fail --max-time 2 \
                    http://127.0.0.1:9090/-/ready >/dev/null 2>&1; then
                  break
                fi
                sleep 1
              done
              while true; do
                ts=$(date -u +%FT%TZ)
                # Use Prometheus's instant-query API. Each query gives
                # the current value of one summable counter. Prefix each
                # line with a sentinel so the host runner can grep it
                # out of the serial transcript without ambiguity.
                ns_start=0
                {
                  printf 'XTCP2_PROM_SNAPSHOT {"t":"%s"' "$ts"
                  # netNamespaceInstance/start = per-namespace instances
                  # started (Method B discovery signal). reconcile/start =
                  # the pre-poll reconcile firing (replaced the removed
                  # watchNamespaces inotify counter, which is dead under
                  # Method B).
                  for q in \
                    'sum(xtcp_counts{variable="p"})' \
                    'sum(xtcp_counts{variable="packets"})' \
                    'sum(xtcp_counts{function="netNamespaceInstance",variable="start"})' \
                    'sum(xtcp_counts{function="reconcile",variable="start"})' \
                    'sum(xtcp_counts{function="nsAdd",variable="store"})' \
                    'sum(xtcp_counts{variable="OrphanCQE"})' ; do
                    v=$(${pkgs.curl}/bin/curl --silent --fail --max-time 2 \
                      --data-urlencode "query=$q" \
                      http://127.0.0.1:9090/api/v1/query 2>/dev/null \
                      | ${pkgs.jq}/bin/jq -r '.data.result[0].value[1] // "0"' 2>/dev/null \
                      || echo "0")
                    printf ',"%s":%s' "$q" "$v"
                    case "$q" in *netNamespaceInstance*) ns_start="$v" ;; esac
                  done
                  printf '}\n'
                }
                # Clean, unambiguous per-namespace discovery signal for the
                # host runner. Under Method B there is no inotify CREATE
                # event to grep — xtcp2 starts one netNamespaceInstance per
                # netns it discovers via /proc (host + each container). The
                # tcp-stress runner asserts this is >= the container count.
                printf 'XTCP2_NS_INSTANCES start=%s\n' "''${ns_start:-0}"
                sleep 30
              done
            '';
            Restart = "on-failure";
            RestartSec = "5s";
            # journal+console so the lines also stream out the serial
            # console — the host runner greps them from the transcript.
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # Periodic /proc-based resource snapshots for the leak-soak (and the
        # io_uring-vs-syscall comparison). Every 30s emit xtcp2's cumulative
        # user/kernel CPU (utime/stime from /proc/PID/stat), voluntary and
        # nonvoluntary context switches, RSS, and thread count (/proc/PID/status)
        # as one XTCP2_RES_SNAPSHOT json line. Diff consecutive lines host-side
        # to see long-term trends: rss_kb is the memory-growth signal, threads
        # the OS-thread-leak signal, CPU + ctxt round it out. Gated to the load
        # flavors (soak / tcp-stress).
        systemd.services.xtcp2-resource-snapshot =
          lib.mkIf (isTcpStress || isSoak || isAnyClickPipe || isS3ParquetStress || isS3ParquetLowfreq)
            {
              description = "xtcp2 — periodic CPU/ctxt/RSS/thread snapshots (leak-soak)";
              after = [ "xtcp2.service" ];
              wants = [ "xtcp2.service" ];
              wantedBy = [ "multi-user.target" ];
              path = [
                pkgs.procps
                pkgs.gnugrep
                pkgs.coreutils
              ];
              serviceConfig = {
                Type = "simple";
                ExecStart = pkgs.writeShellScript "xtcp2-resource-snapshot" ''
                  set -u
                  clk=$(getconf CLK_TCK 2>/dev/null || echo 100)
                  while true; do
                    pid=$(pgrep -x xtcp2 | head -1 || true)
                    if [ -n "''${pid:-}" ] && [ -r "/proc/$pid/stat" ]; then
                      # /proc/PID/stat: utime=14, stime=15 (clock ticks).
                      read -r -a st < "/proc/$pid/stat"
                      utime=''${st[13]}; stime=''${st[14]}
                      vctx=$(grep -m1 '^voluntary_ctxt_switches' "/proc/$pid/status" | grep -oE '[0-9]+' || echo 0)
                      nvctx=$(grep -m1 '^nonvoluntary_ctxt_switches' "/proc/$pid/status" | grep -oE '[0-9]+' || echo 0)
                      rss=$(grep -m1 '^VmRSS' "/proc/$pid/status" | grep -oE '[0-9]+' || echo 0)
                      threads=$(grep -m1 '^Threads' "/proc/$pid/status" | grep -oE '[0-9]+' || echo 0)
                      printf 'XTCP2_RES_SNAPSHOT {"t":"%s","clk":%s,"utime":%s,"stime":%s,"vctx":%s,"nvctx":%s,"rss_kb":%s,"threads":%s}\n' \
                        "$(date -u +%FT%TZ)" "$clk" "''${utime:-0}" "''${stime:-0}" "$vctx" "$nvctx" "$rss" "''${threads:-0}"
                    fi
                    sleep 30
                  done
                '';
                Restart = "on-failure";
                RestartSec = "5s";
                StandardOutput = "journal+console";
                StandardError = "journal+console";
              };
            };

        systemd.services.xtcp2-tcp-stress-load = lib.mkIf isAnyTcpStressLoad {
          description = "xtcp2 tcp-stress — load OCI image into docker";
          after = [ "docker.service" ];
          requires = [ "docker.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${tcpStressLoadScript}/bin/xtcp2-tcp-stress-load";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        systemd.services.xtcp2-tcp-stress-spawn = lib.mkIf isAnyTcpStressLoad {
          description = "xtcp2 tcp-stress — spawn N stress containers";
          after = [
            "xtcp2-tcp-stress-load.service"
            "xtcp2.service"
          ];
          requires = [ "xtcp2-tcp-stress-load.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = "${tcpStressSpawnScript}/bin/xtcp2-tcp-stress-spawn";
            Restart = "on-failure";
            RestartSec = "5s";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # Phase E: docker network + redpanda + clickhouse + topic + initdb.
        # The xtcp2 daemon (on the VM host) connects to redpanda's
        # external advertised addr localhost:19092. Records flow through:
        #   xtcp2 → kafka (redpanda) → kafka-engine-table → MV → MergeTree.
        # The script's tail loop also prints XTCP2_CLICKPIPE_ROWS every 30s
        # so the host runner can grep current row count out of the
        # transcript without docker exec.
        systemd.services.xtcp2-clickpipe-up = lib.mkIf isAnyClickPipe {
          description = "xtcp2 clickhouse-pipeline — redpanda + clickhouse + topic + initdb";
          after = [ "docker.service" ];
          requires = [ "docker.service" ];
          # before xtcp2.service so the kafka broker + topic + schema
          # registry are all live by the time newKafkaDest tries to
          # registerProtobufSchema.
          before = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            # oneshot + RemainAfterExit so units ordered After=us can
            # start only after the script returns 0. Type=simple would
            # let xtcp2.service kick in immediately and crash-loop while
            # the docker pulls were still going.
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${clickPipeUpScript}/bin/xtcp2-clickpipe-up";
            # First-boot image pulls can be slow; give the up-script up
            # to 10 min to settle before systemd considers it a failure.
            TimeoutStartSec = "600";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # Companion monitor: tail row count from xtcp.xtcp_flat_records
        # every 30s so the operator can see records arriving without
        # logging in.
        systemd.services.xtcp2-clickpipe-monitor = lib.mkIf isAnyClickPipe {
          description = "xtcp2 clickhouse-pipeline — periodic row count monitor";
          after = [
            "xtcp2-clickpipe-up.service"
            "xtcp2.service"
          ];
          requires = [ "xtcp2-clickpipe-up.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            Type = "simple";
            ExecStart = "${clickPipeMonitorScript}/bin/xtcp2-clickpipe-monitor";
            Restart = "on-failure";
            RestartSec = "10s";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # clickhouse-pipeline-rate flavor: the runtime-control rate test driver.
        # Runs the baseline→fast→revert→burst schedule against the live daemon
        # via xtcp2ctl and prints XTCP2_RATE_* sentinels the host runner scrapes.
        # Ordered after the pipeline is up + xtcp2 is serving gRPC.
        systemd.services.xtcp2-clickpipe-rate = lib.mkIf isClickPipeRate {
          description = "xtcp2 clickhouse-pipeline-rate — runtime-control rate test";
          after = [
            "xtcp2-clickpipe-up.service"
            "xtcp2.service"
            "xtcp2-soak-tcp-client.service"
          ];
          requires = [ "xtcp2-clickpipe-up.service" ];
          wantedBy = [ "multi-user.target" ];
          serviceConfig = {
            # oneshot: the schedule runs once to XTCP2_RATE_DONE and exits.
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${clickPipeRateScript}/bin/xtcp2-clickpipe-rate";
            # ~15 min schedule (3×180s windows + settles + burst) + readiness
            # headroom. The host runner's --timeout bounds the overall run.
            TimeoutStartSec = "1900";
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        # Phase E: ship the xtcp_flat_record.proto so the kafka destination
        # factory can read it (registerProtobufSchema is the first thing
        # newKafkaDest does — without the file the daemon crashes during
        # init, restart-loops, and never gets the prom listener up long
        # enough to scrape). NixOS drops it at /etc/xtcp2/xtcp_flat_record.proto
        # and the -xtcpProtoFile arg in xtcp2ClickPipeArgs points at that
        # path.
        environment.etc."xtcp2/xtcp_flat_record.proto" = lib.mkIf isAnyClickPipe {
          source = ../../proto/xtcp_flat_record/v1/xtcp_flat_record.proto;
        };

        # discovery-bench flavor: run the namespace-discovery A/B grid on boot,
        # emit DISCOBENCH_GRID JSON lines + a DISCOBENCH_DONE sentinel to the
        # serial console, then let the host runner power the VM off. Runs as root
        # inside the VM, so Method B's /proc scan sees every namespace (no
        # ptrace-gated skips) and `ip netns` can build the controlled N×P grid.
        systemd.services.discovery-bench-run = lib.mkIf isDiscoveryBench {
          description = "xtcp2 — namespace-discovery A/B grid benchmark";
          after = [ "network.target" ];
          wantedBy = [ "multi-user.target" ];
          path = with pkgs; [
            iproute2
            coreutils
            util-linux
          ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            # The largest grid cell spawns thousands of `sleep` procs in this
            # service's cgroup; lift the per-service task cap to fit them.
            TasksMax = 16384;
            ExecStart = pkgs.writeShellScript "discovery-bench-run" ''
              set -u
              NS_GRID="''${DISCO_NS_GRID:-1,10,50,100}"
              PID_GRID="''${DISCO_PID_GRID:-100,500,1000,3000}"
              ITERS="''${DISCO_ITERS:-30}"
              echo "DISCOBENCH_START ns_grid=$NS_GRID pid_grid=$PID_GRID iters=$ITERS"
              ${xtcp2AllPackage}/bin/discovery-bench \
                -mode grid -json \
                -nsGrid "$NS_GRID" -pidGrid "$PID_GRID" -iters "$ITERS" \
                || echo "DISCOBENCH_ERR grid exited non-zero"
              echo "DISCOBENCH_DONE"
            '';
            StandardOutput = "journal+console";
            StandardError = "journal+console";
          };
        };

        environment.systemPackages =
          (with pkgs; [
            coreutils
            iproute2
            netcat-gnu
            tcpdump
            curl
            jq
            procps
            util-linux
            systemd
          ])
          ++ lib.optionals isTcpStress (with pkgs; [ docker ])
          ++ [ xtcp2AllPackage ];
      }
    )
  ];
}).config.microvm.declaredRunner
