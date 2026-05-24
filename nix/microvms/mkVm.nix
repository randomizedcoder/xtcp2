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
  isTcpStress = sink == "tcp-stress";
  # clickhouse-pipeline = tcp-stress + redpanda + clickhouse + kafka
  # destination. Same docker setup but two extra containers + xtcp2
  # configured with -dest kafka:localhost:19092 so the records flow
  # through the same pipeline as the production compose.
  isClickPipe = sink == "clickhouse-pipeline";
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
  # Convenience predicate — most plumbing (minio module, port forwards,
  # mem budget, daemon args base) is shared.
  isAnyS3Parquet = isS3Parquet || isS3ParquetLong || isCapCheckFail;
  # Anything that needs dockerd inside the VM.
  needsDocker = isTcpStress || isClickPipe;
  effectiveMem =
    if isClickPipe then
      cfg.memClickPipe
    else if isAnyS3Parquet then
      cfg.memClickPipe
    else if isTcpStress then
      cfg.memTcpStress
    else
      cfg.mem;

  coverDir = "/var/lib/xtcp2cov";

  selfTest = import ./self-test.nix {
    inherit pkgs lib;
    promPort = cfg.promPort;
    grpcPort = cfg.grpcPort;
    coverageEnabled = isCoverage;
    inherit coverDir;
    runClickhouseCheck = isClickPipe;
    clickhousePassword = clickPipeChPassword;
    runS3ParquetCheck = isS3Parquet;
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
  # /run/docker/netns/ fsnotify watch under real socket load. Start
  # small (numContainers=5 default) so the per-VM resource budget
  # stays sane; bump up to 20+ once you've validated end-to-end.
  tcpStressNumContainers = 20;
  tcpStressSocketsPerContainer = 100;
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

  clickPipeRedpandaImage = "docker.redpanda.com/redpandadata/redpanda:v25.1.7";
  # ClickHouse uses MAJOR.MINOR.PATCH.SUBPATCH versioning; the precise
  # numeric tag for the LTS 25.x line at any given point is hard to
  # predict, so we use the floating "25.3-alpine" tag which Docker Hub
  # repoints at the latest 25.3 LTS patch. Pin to a precise tag for
  # reproducibility once you've validated which patch works.
  clickPipeClickhouseImage = "clickhouse/clickhouse-server:25.3-alpine";
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
    # The init scripts write tracking files into out/; pre-create it
    # so they don't fail on the first run. Same as the compose flow.
    mkdir -p $out/out
    chmod -R a+rX $out
  '';

  # nsTest churn parameters tuned for soak runs. Production nsTest defaults
  # are 1000 initial namespaces + 100ms sleep — which inside a microvm
  # creates an explosive boot-time spike (1000 × `ip netns add` back-to-back
  # before any churn). Soak runs benefit from a smaller initial fill and a
  # bit more breathing room between iterations so the daemon's fsnotify
  # watcher + nsAdd path runs continuously without ever being completely
  # idle. Sized empirically — increase if you want harsher loading.
  soakInitialNs = 50;
  soakChurnSleep = "250ms";
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
      # Run nsTest with reduced initial-fill + slightly longer churn sleep
      # so a 1h / 24h run doesn't drown the journal in `ip netns add` lines
      # before any actual churn happens.
      exec ${xtcp2AllPackage}/bin/nsTest -initial ${toString soakInitialNs} -sleep ${soakChurnSleep}
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
      ${if tcpStressImage != null then "${tcpStressImage} | docker load" else "echo 'no image provided'; exit 1"}
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
      # The point is for xtcp2 to discover each container's netns via
      # /run/docker/netns/ fsnotify and observe its sockets via inet_diag.
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
      docker run --detach \
        --name redpanda-0 \
        --network xtcp \
        --hostname redpanda-0 \
        -p 19092:19092 -p 19644:9644 -p 18081:8081 \
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
      docker rm -f clickhouse 2>/dev/null || true
      docker run --detach \
        --name clickhouse \
        --network xtcp \
        --hostname clickhouse \
        -p 18123:8123 -p 19001:9000 \
        --ulimit nofile=262144:262144 \
        --memory=3500m \
        --cap-add CAP_NET_ADMIN --cap-add CAP_SYS_NICE \
        --cap-add CAP_IPC_LOCK --cap-add CAP_SYS_PTRACE \
        --env CLICKHOUSE_ALWAYS_RUN_INITDB_SCRIPTS=true \
        --env CLICKHOUSE_PASSWORD=${clickPipeChPassword} \
        -v clickhouse_db:/var/lib/clickhouse \
        -v "$initdbRw":/docker-entrypoint-initdb.d:rw \
        -v "$schemasRw":/var/lib/clickhouse/format_schemas:rw \
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
      # without ambiguity.
      while true; do
        rows=$(docker exec clickhouse clickhouse-client --password ${clickPipeChPassword} \
          -q 'SELECT count() FROM xtcp.xtcp_flat_records' 2>/dev/null || echo 0)
        echo "XTCP2_CLICKPIPE_ROWS $(date -u +%FT%TZ) rows=$rows"
        sleep 30
      done
    '';
  };

  # s3parquet flavor: in-VM MinIO + bucket bootstrap. The xtcp2 daemon
  # talks to MinIO directly via the minio-go client; no proto-desc file
  # or unixgram socket required. The long-soak variant additionally
  # brings up a local Pyroscope server so xtcp2 can stream profiles
  # for goroutine/thread-leak diagnosis without an external dependency.
  s3ParquetModules =
    [ (import ../modules/minio-bucket-bootstrap.nix { }) ]
    ++ lib.optionals isS3ParquetLong [
      (import ../modules/pyroscope-server.nix { })
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
        echo "XTCP2_S3PARQUET_HOURLY $(date -u +%FT%TZ) files=''${files} bytes=''${bytes} rows=''${rows} goroutines=''${gor} threads=''${thr}"
      done
    '';
  };

  # Args for the long-soak flavor. Production-sized 63 MiB flush
  # threshold — at the steady ~1 MB/min raw-row rate seen in the 30 min
  # smoke, a 12 h run produces ~12 finalized objects (multiple files in
  # 12 h, matching the user's stated expectation). Drop to 1048576 for
  # smoke runs that need a visible file count growing every minute.
  # Poll rate 10 s keeps the daemon CPU-cheap over multi-hour runs.
  xtcp2S3ParquetLongArgs = [
    "-dest"
    "s3parquet:http://127.0.0.1:9000"
    "-marshal"
    "protobufList"
    "-frequency"
    "10s"
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
    # Stream profile data to the in-VM Pyroscope server. Empty value
    # would disable the agent — kept on for long soaks because that's
    # where leak diagnosis lives.
    "-pyroscopeUrl"
    "http://127.0.0.1:14040"
    "-pyroscopeAppName"
    "xtcp2.s3parquet-long"
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

  xtcp2CoverageArgs = xtcp2BasicArgs
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
in
(nixpkgs.lib.nixosSystem {
  inherit pkgs;

  modules = [
    microvm.nixosModules.microvm
    ../modules/xtcp2-service.nix
  ]
  ++ lib.optionals isAnyS3Parquet s3ParquetModules
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
          lib.optionals (isTcpStress || isClickPipe || isAnyS3Parquet) [
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
          ++ lib.optionals isClickPipe [
            18123 # clickhouse HTTP
            19001 # clickhouse native
            19092 # redpanda kafka external
            19644 # redpanda admin
            18081 # schema registry
            3000  # grafana
            # 9090 (prometheus) intentionally not in forwardPorts —
            # see comment in microvm.forwardPorts.
            9090  # still open the firewall so grafana's internal call works
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
          # gets real (not RAM) bytes. 8 GiB covers a 12h soak with
          # MergeTree compression at ~3 rows/s × ~1 KiB/row + dockerd
          # working set + redpanda topic data.
          volumes =
            lib.optionals isClickPipe [
              {
                # User-writable path so microvm-run can autoCreate the
                # image without sudo. /tmp is RAM-backed on most distros
                # but big enough for the 8 GiB image; if you want
                # cross-boot persistence move this to ~/.cache or a
                # mounted disk and add `microvm.preStart` to mkdir.
                image = "/tmp/xtcp2-microvm-clickhouse-pipeline-docker.img";
                mountPoint = "/var/lib/docker";
                size = 8192;
                autoCreate = true;
                fsType = "ext4";
                label = "xtcp2dock";
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
            lib.optionals (isTcpStress || isClickPipe || isAnyS3Parquet) [
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
            ++ lib.optionals isS3ParquetLong [
              # Pyroscope UI on the long-soak flavor so operators can
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
              # in-VM Prometheus server for the tcp-stress flavor.
              {
                from = "host";
                host.port = 9090;
                guest.port = 9090;
              }
            ]
            ++ lib.optionals isClickPipe [
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
              # 127.0.0.1:9090 internally — no host forward by default,
              # and :9090 frequently clashes. Use host:19090 if you
              # want host-side browsing (commented out — uncomment +
              # add 19090 to firewall list).
              # {
              #   from = "host"; host.port = 19090; guest.port = 9090;
              # }
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

        # xtcp2 enumerates network namespaces by listing /run/netns/ and
        # /run/docker/netns/. If neither exists it fatal-exits with
        # "neither network namespace directory exists.  ??!"
        # (pkg/xtcp/init.go:130). Pre-create BOTH in every flavor so the
        # daemon watches both fsnotify paths and the self-test's
        # Check 10 (NS_DOCKER) has a target to bind-mount into. Creating
        # an empty /run/docker/netns/ doesn't pull docker in — the
        # daemon just sees an empty dir and starts a watcher on it.
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
        systemd.services.xtcp2 = lib.mkIf isCoverage {
          environment.GOCOVERDIR = coverDir;
        };

        # Pre-create a test network namespace before xtcp2 starts. This
        # makes the fsnotify-watch path fire a Create event for an actual
        # namespace, which spawns netNamespaceInstance →
        # openAndSetNSWithRetries → openDefaultNetLinkSocket inside that
        # namespace. Otherwise those code paths stay at 0% even with
        # coverage instrumentation.
        systemd.services.create-test-netns = lib.mkIf isCoverage {
          description = "Create a test network namespace for xtcp2 coverage";
          wantedBy = [ "xtcp2.service" ];
          before = [ "xtcp2.service" ];
          after = [ "local-fs.target" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            ExecStart = "${pkgs.iproute2}/bin/ip netns add xtcpcovns";
            ExecStop = "${pkgs.iproute2}/bin/ip netns delete xtcpcovns";
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
            else if isClickPipe then
              # Phase E: produce to redpanda → clickhouse via kafka dest.
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

        # Self-test oneshot. The self-test's check 1 retries `systemctl
        # is-active xtcp2` for 30 s, robust to xtcp2 starting directly at
        # boot or via a systemd.path gate. Skipped on long-running flavors
        # (soak / s3parquet-long), which run heartbeat services instead.
        systemd.services.xtcp2-self-test = lib.mkIf (!isSoak && !isS3ParquetLong) {
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

        # Soak flavor: long-running services that churn namespaces + scrape
        # /metrics into a file inside the VM. The host-side soak runner
        # (see nix/microvms/lib.nix mkSoakRunner) boots the VM, sleeps for
        # the configured -duration, then powers it off and inspects the
        # metric log + journal for crashes/restarts.
        systemd.services.xtcp2-soak-churn = lib.mkIf (isSoak || isS3ParquetLong) {
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

        # s3parquet-long: hourly file-count monitor. Sentinel format
        # mirrors XTCP2_CLICKPIPE_ROWS so the host-side runner can grep
        # for it with the same idiom. Cadence is S3PARQUET_REPORT_INTERVAL
        # (seconds) — the runner overrides per phase.
        systemd.services.xtcp2-s3parquet-monitor = lib.mkIf isS3ParquetLong {
          description = "xtcp2 s3parquet-long — hourly MinIO file-count reporter";
          after = [
            "xtcp2.service"
            "multi-user.target"
          ];
          wants = [ "xtcp2.service" ];
          wantedBy = [ "multi-user.target" ];
          environment.S3PARQUET_REPORT_INTERVAL = toString s3ParquetReportIntervalDefault;
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
        systemd.services.xtcp2-soak-tcp-server = lib.mkIf (isSoak || isS3ParquetLong) {
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

        systemd.services.xtcp2-soak-tcp-client = lib.mkIf (isSoak || isS3ParquetLong) {
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
            ExecStart = ''${xtcp2AllPackage}/bin/tcp_client -count ${toString soakTcpClientCount} -connect ${soakTcpConnect} -sleep ${soakTcpClientSleep} -pads ${toString soakTcpPads}'';
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
        # time: per-ns Netlinker.p / .packets / start, watchNamespaces
        # event/for, GC behaviour, etc. The server listens on
        # 127.0.0.1:9090; the runner also includes a periodic snapshot
        # service that curls Prometheus and writes per-query JSON lines
        # to a file so the user sees concrete data even if they don't
        # log into the VM to browse the web UI.
        services.prometheus = lib.mkIf (isTcpStress || isClickPipe) {
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
                  labels.instance = "xtcp2-vm";
                }
              ];
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
        services.grafana = lib.mkIf isClickPipe {
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
                {
                  printf 'XTCP2_PROM_SNAPSHOT {"t":"%s"' "$ts"
                  for q in \
                    'sum(xtcp_counts{variable="p"})' \
                    'sum(xtcp_counts{variable="packets"})' \
                    'sum(xtcp_counts{function="netNamespaceInstance",variable="start"})' \
                    'sum(xtcp_counts{function="watchNamespaces",variable="event"})' \
                    'sum(xtcp_counts{function="nsAdd",variable="store"})' \
                    'sum(xtcp_counts{variable="OrphanCQE"})' ; do
                    v=$(${pkgs.curl}/bin/curl --silent --fail --max-time 2 \
                      --data-urlencode "query=$q" \
                      http://127.0.0.1:9090/api/v1/query 2>/dev/null \
                      | ${pkgs.jq}/bin/jq -r '.data.result[0].value[1] // "0"' 2>/dev/null \
                      || echo "0")
                    printf ',"%s":%s' "$q" "$v"
                  done
                  printf '}\n'
                }
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

        systemd.services.xtcp2-tcp-stress-load = lib.mkIf isTcpStress {
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

        systemd.services.xtcp2-tcp-stress-spawn = lib.mkIf isTcpStress {
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
        systemd.services.xtcp2-clickpipe-up = lib.mkIf isClickPipe {
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
        systemd.services.xtcp2-clickpipe-monitor = lib.mkIf isClickPipe {
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

        # Phase E: ship the xtcp_flat_record.proto so the kafka destination
        # factory can read it (registerProtobufSchema is the first thing
        # newKafkaDest does — without the file the daemon crashes during
        # init, restart-loops, and never gets the prom listener up long
        # enough to scrape). NixOS drops it at /etc/xtcp2/xtcp_flat_record.proto
        # and the -xtcpProtoFile arg in xtcp2ClickPipeArgs points at that
        # path.
        environment.etc."xtcp2/xtcp_flat_record.proto" = lib.mkIf isClickPipe {
          source = ../../proto/xtcp_flat_record/v1/xtcp_flat_record.proto;
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
