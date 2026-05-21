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
# Two flavors selected by `sink`:
#   - "minimal" (default): xtcp2 alone, JSONL configFile (currently a no-op
#                          stub; the netlink-readout check tolerates a missing
#                          file). Cheap CI smoke.
#   - "vector":            xtcp2 → unixgram UDS → Vector → parquet → MinIO,
#                          all inside the VM. Uses memVector budget. Self-test
#                          checks VECTOR/MINIO/PARQUET sentinels in addition
#                          to the rest of the suite.
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
  # Required when sink == "vector". A derivation that provides
  # share/xtcp2/xtcp_flat_record.desc. See nix/lib/mkProtoDescSet.nix.
  protoDescPackage ? null,
  # Required when sink == "tcp-stress". The OCI image (streamLayeredImage
  # script) that the in-VM container spawn unit loads via `docker load`.
  tcpStressImage ? null,
}:

let
  constants = import ./constants.nix;
  cfg = constants.architectures.${arch};

  isVector = sink == "vector";
  isCoverage = sink == "coverage" || sink == "coverage-iouring";
  isCoverageIoUring = sink == "coverage-iouring";
  isSoak = sink == "soak";
  isTcpStress = sink == "tcp-stress";
  # clickhouse-pipeline = tcp-stress + redpanda + clickhouse + kafka
  # destination. Same docker setup but two extra containers + xtcp2
  # configured with -dest kafka:localhost:19092 so the records flow
  # through the same pipeline as the production compose.
  isClickPipe = sink == "clickhouse-pipeline";
  # Anything that needs dockerd inside the VM.
  needsDocker = isTcpStress || isClickPipe;
  effectiveMem =
    if isVector then
      cfg.memVector
    else if isTcpStress || isClickPipe then
      cfg.memTcpStress
    else
      cfg.mem;

  coverDir = "/var/lib/xtcp2cov";

  selfTest =
    if isVector then
      import ./self-test-vector.nix {
        inherit pkgs;
        promPort = cfg.promPort;
        grpcPort = cfg.grpcPort;
      }
    else
      import ./self-test.nix {
        inherit pkgs lib;
        promPort = cfg.promPort;
        grpcPort = cfg.grpcPort;
        coverageEnabled = isCoverage;
        inherit coverDir;
      };

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

  vectorModules =
    assert lib.assertMsg (
      protoDescPackage != null
    ) "mkVm.nix: sink=\"vector\" requires protoDescPackage";
    [
      (import ../modules/vector-pipeline.nix {
        inherit protoDescPackage;
      })
      (import ../modules/minio-bucket-bootstrap.nix { })
      ../modules/xtcp2-vector-path.nix
    ];

  xtcp2VectorArgs = [
    "-dest"
    "unixgram:/run/xtcp2/output.sock"
    "-marshal"
    "protobufSingle"
    "-frequency"
    "2s"
    # xtcp2 requires `-timeout < -frequency`; defaults are 5 s / 10 s. With
    # frequency dropped to 2 s for fast lifecycle-test cycles, timeout must
    # come down too.
    "-timeout"
    "1s"
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
    "protobufSingle"
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
in
(nixpkgs.lib.nixosSystem {
  inherit pkgs;

  modules = [
    microvm.nixosModules.microvm
    ../modules/xtcp2-service.nix
  ]
  ++ lib.optionals isVector vectorModules
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
          lib.optionals (isTcpStress || isClickPipe) [
            9088 # xtcp2 prometheus
            8889 # xtcp2 grpc
          ]
          ++ lib.optional isTcpStress 9090 # in-VM Prometheus
          ++ lib.optionals isClickPipe [
            18123 # clickhouse HTTP
            19001 # clickhouse native
            19092 # redpanda kafka external
            19644 # redpanda admin
            18081 # schema registry
          ];

        microvm = {
          hypervisor = "qemu";
          mem = effectiveMem;
          vcpu = cfg.vcpu;
          cpu = if cfg.useKvm then null else cfg.qemuCpu;
          volumes = [ ];
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
            lib.optionals (isTcpStress || isClickPipe) [
              # xtcp2 daemon's prometheus + grpc endpoints — same on
              # every docker-enabled flavor.
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
            if isVector then
              xtcp2VectorArgs
            else if isCoverage then
              xtcp2CoverageArgs
            else if isClickPipe then
              # Phase E: produce to redpanda → clickhouse via kafka dest.
              xtcp2ClickPipeArgs
            else
              # Soak reuses the basic args (`-dest null`, fast frequency).
              # The point of soak is namespace + netlink churn, not
              # downstream destination throughput.
              xtcp2BasicArgs;
        };

        # Self-test oneshot. The self-test's check 1 retries `systemctl
        # is-active xtcp2` for 30 s, so it is robust to xtcp2 starting via
        # the systemd.path gate (vector flavor) vs. directly at boot
        # (minimal flavor). Skipped on the soak flavor (long-running churn
        # + metric scrape services replace it).
        systemd.services.xtcp2-self-test = lib.mkIf (!isSoak) {
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
        systemd.services.xtcp2-soak-churn = lib.mkIf isSoak {
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
        systemd.services.xtcp2-soak-tcp-server = lib.mkIf isSoak {
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

        systemd.services.xtcp2-soak-tcp-client = lib.mkIf isSoak {
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
        services.prometheus = lib.mkIf isTcpStress {
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
          ++ lib.optionals isVector (
            with pkgs;
            [
              vector
              minio
              minio-client
              duckdb
            ]
          )
          ++ lib.optionals isTcpStress (with pkgs; [ docker ])
          ++ [ xtcp2AllPackage ];
      }
    )
  ];
}).config.microvm.declaredRunner
