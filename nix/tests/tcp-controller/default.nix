# Go-driven, rootless seven-guest integration using the existing TCP load tools.
{
  pkgs,
  lib,
  nixpkgs,
  binaries,
  vendoredSource,
  soak ? false,
}:
let
  name = if soak then "tcp-controller-soak" else "tcp-controller";
  agents = lib.genList (i: "agent${toString (i + 1)}") 6;
  controllerAddress = "192.168.1.254";
  versions = import ../../versions.nix { inherit pkgs; };
  harness = pkgs.runCommand "tcp-controller-integration" { nativeBuildInputs = [ versions.go ]; } ''
    cp -r ${vendoredSource} source
    chmod -R u+w source
    cd source
    export GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0
    export GOCACHE="$TMPDIR/go-cache" GOMODCACHE="$TMPDIR/go-modcache"
    mkdir -p "$out/bin"
    go build -trimpath -o "$out/bin/tcp-controller-integration" ./tools/tcp-controller-integration
  '';
  pki = pkgs.writeShellApplication {
    name = "controller-test-pki";
    runtimeInputs = [
      pkgs.openssl
      pkgs.coreutils
    ];
    text = ''
      mkdir -p /var/lib/controller-test/pki
      cd /var/lib/controller-test/pki
      umask 077
      openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
        -keyout ca.key -out ca.crt -days 2 -subj /CN=xtcp-integration-CA
      issue() {
        local name=$1 san=$2 usage=$3
        openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
          -keyout "$name.key" -out "$name.csr" -subj "/CN=$name"
        printf 'subjectAltName=%s\nextendedKeyUsage=%s\nkeyUsage=digitalSignature\n' "$san" "$usage" > "$name.ext"
        openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
          -out "$name.crt" -days 2 -extfile "$name.ext"
      }
      issue controller 'DNS:controller,IP:${controllerAddress}' serverAuth
      issue operator 'URI:spiffe://xtcp/operator/integration' clientAuth
      for agent in ${lib.escapeShellArgs agents}; do
        issue "$agent" "URI:spiffe://xtcp/agent/test-dc/$agent" clientAuth
      done
    '';
  };
  fixturePath = [
    pkgs.iproute2
    pkgs.coreutils
    pkgs.systemd
    pkgs.procps
    binaries.xtcp2-treatmentctl
  ];
  base = {
    system.stateVersion = "26.05";
    networking.firewall.enable = false;
    networking.useDHCP = false;
    networking.usePredictableInterfaceNames = false;
    services.timesyncd.enable = false;
    virtualisation = {
      graphics = false;
      memorySize = 768;
      cores = 1;
      diskImage = null;
      qemu.options = [ "-no-reboot" ];
    };
    boot.kernelModules = [
      "sch_netem"
      "sch_prio"
      "cls_u32"
      "tcp_cubic"
    ];
    environment.etc."xtcp-controller-integration".text = "Disposable VM fixture only.\n";
    environment.systemPackages = fixturePath;
  };
  # Point-to-point UNIX datagram NICs form a star, bridged inside the control
  # guest. No host bridge, tap, root privileges or external network is needed.
  nic =
    index: controller:
    let
      localName = if controller then "controller" else "agent";
      remoteName = if controller then "agent" else "controller";
      macPrefix = if controller then "01" else "02";
    in
    [
      "-netdev dgram,id=link${toString index},local.type=unix,local.path=$TEST_NETWORK/${localName}${toString index},remote.type=unix,remote.path=$TEST_NETWORK/${remoteName}${toString index}"
      "-device virtio-net-pci,netdev=link${toString index},mac=02:00:00:${macPrefix}:00:0${toString index}"
    ];
  mkVM =
    name: config:
    (nixpkgs.lib.nixosSystem {
      system = pkgs.stdenv.hostPlatform.system;
      modules = [
        "${nixpkgs}/nixos/modules/virtualisation/qemu-vm.nix"
        base
        {
          nixpkgs.pkgs = pkgs;
          networking.hostName = name;
        }
        config
      ];
    }).config.system.build.vm;
  agentVM =
    index:
    let
      name = "agent${toString index}";
      host = toString (10 + index);
    in
    mkVM name {
      imports = [ ../../modules/xtcp2-service.nix ];
      virtualisation.qemu.networkingOptions = lib.mkForce (nic index false);
      networking.interfaces.eth0.ipv4.addresses = [
        {
          address = "192.168.1.${host}";
          prefixLength = 24;
        }
        {
          address = "198.18.0.${host}";
          prefixLength = 24;
        }
      ];
      boot.kernel.sysctl."net.ipv4.tcp_congestion_control" = "reno";
      environment.etc."xtcp2/controller-agent.json".text = builtins.toJSON {
        agent_id = name;
        location = "test-dc";
        controller = "${controllerAddress}:9443";
        server_name = "controller";
        certificate = "/var/lib/controller-test/pki/agent.crt";
        private_key = "/var/lib/controller-test/pki/agent.key";
        ca = "/var/lib/controller-test/pki/ca.crt";
        enable_apply = true;
        protected_prefixes = [ "192.168.1.0/24" ];
      };
      services.xtcp2 = {
        enable = true;
        # The slim daemon does not embed the agent, so this fixture runs the
        # standalone arrangement: xtcp2-treatment-agent owns the journal and
        # the routes, xtcp2 reads the journal to stamp provenance.
        package = binaries.xtcp2-min;
        configFile = "/etc/xtcp2/unused.json";
        linkMonitor.enable = false;
        treatments = {
          enable = true;
          mode = "standalone";
          agentPackage = binaries.xtcp2-treatment-agent;
          watchdogPackage = binaries.xtcp2-treatmentctl;
        };
        extraArgs = [
          "-frequency=2s"
          "-timeout=1s"
          "-pollJitterPct=0"
          "-marshal=protobufList"
          "-dest=http://${controllerAddress}:8123/?query=INSERT+INTO+xtcp.ingest+FORMAT+ProtobufList&format_schema=xtcp_flat_record.proto:XtcpFlatRecord"
        ];
      };
      # The Go fixture installs runtime credentials before starting the daemon.
      systemd.services.xtcp2.wantedBy = lib.mkForce [ ];
      systemd.services.xtcp2-treatment-agent.wantedBy = lib.mkForce [ ];
      systemd.services.xtcp2-treatment-watchdog.wantedBy = lib.mkForce [ ];
      systemd.services.controller-test-agent = {
        wantedBy = [ "multi-user.target" ];
        after = [ "network-online.target" ];
        wants = [ "network-online.target" ];
        path = fixturePath;
        serviceConfig.ExecStart = "${harness}/bin/tcp-controller-integration -role=agent -listen=192.168.1.${host}:9009 -agentUnit=xtcp2-treatment-agent";
      };
      systemd.services.load-near.serviceConfig = {
        EnvironmentFile = "/var/lib/controller-test/load.env";
        ExecStart = "${binaries.tcp_client}/bin/tcp_client -count 32 -connect $TCP_TEST_DESTINATION -sleep 50ms -startsleep 5ms -pads 1024 -rto 3s -wto 3s";
        Restart = "always";
        RestartSec = 1;
        StandardOutput = "null";
        StandardError = "null";
      };
      systemd.services.load-far.serviceConfig = {
        ExecStart = "${binaries.tcp_client}/bin/tcp_client -count 32 -connect 198.18.0.2 -sleep 50ms -startsleep 5ms -pads 1024 -rto 3s -wto 3s";
        Restart = "always";
        RestartSec = 1;
        StandardOutput = "null";
        StandardError = "null";
      };
    };
  agentVMs = lib.genList (i: agentVM (i + 1)) 6;
  controllerVM = mkVM "controller" {
    virtualisation.memorySize = lib.mkForce 4096;
    virtualisation.cores = lib.mkForce 2;
    virtualisation.qemu.networkingOptions = lib.mkForce (lib.concatMap (i: nic i true) (lib.range 1 6));
    virtualisation.sharedDirectories.evidence = {
      source = "$VM_EVIDENCE/artifacts";
      target = "/var/lib/controller-test/evidence";
      writable = true;
    };
    networking.bridges.br0.interfaces = lib.genList (i: "eth${toString i}") 6;
    networking.interfaces.br0.ipv4.addresses = [
      {
        address = controllerAddress;
        prefixLength = 24;
      }
      {
        address = "198.18.0.1";
        prefixLength = 24;
      }
      {
        address = "198.18.0.2";
        prefixLength = 24;
      }
    ]
    ++ lib.optionals soak (
      lib.genList (i: {
        address = "198.18.0.${toString (101 + i)}";
        prefixLength = 24;
      }) 6
    );
    services.postgresql = {
      enable = true;
      package = pkgs.postgresql_15;
      ensureDatabases = [ "xtcp" ];
      ensureUsers = [
        {
          name = "root";
          ensureClauses.superuser = true;
        }
      ];
    };
    services.clickhouse = {
      enable = true;
      serverConfig = {
        listen_host = "0.0.0.0";
        format_schema_path = "/etc/xtcp-test/schemas/";
        max_server_memory_usage = 1500000000;
        background_pool_size = 4;
        background_merges_mutations_concurrency_ratio = 8;
      };
      usersConfig.profiles.default = {
        max_threads = 2;
        max_memory_usage = 1000000000;
      };
    };
    systemd.services.clickhouse.serviceConfig.TimeoutStopSec = "10s";
    environment.etc."xtcp-test/schemas/xtcp_flat_record.proto".source =
      ../../../proto/xtcp_flat_record/v1/xtcp_flat_record.proto;
    environment.etc."xtcp-test/schema.sql".source =
      ../../../build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records.sql;
    environment.etc."xtcp-test/discovery.json".text = builtins.toJSON [
      {
        id = "soak-nearby-cubic";
        location = "test-dc";
        congestion_control = "cubic";
        rto_min_ms = 5;
        quick_ack = true;
        initial_congestion_window = 100;
        initial_receive_window = 100;
        # A 10% guard at 5ms allows only ~0.5ms of VM scheduling variation.
        # Keep this fixture tolerant of small changes but require withdrawal
        # for the deliberate 5ms -> 180ms regression. Production stays at 10%.
        maximum_rtt_increase = 1.0;
        minimum_rtt_ms = 0;
        maximum_rtt_ms = 15;
        minimum_retransmission_ratio = 0;
        prefixes = lib.genList (i: "198.18.0.${toString (101 + i)}/32") 6;
      }
    ];
    systemd.services.controller-test-pki = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        Type = "oneshot";
        RemainAfterExit = true;
        ExecStart = lib.getExe pki;
      };
    };
    systemd.services.xtcp-controller = {
      after = [
        "postgresql.service"
        "postgresql-setup.service"
        "controller-test-pki.service"
      ];
      requires = [
        "postgresql.service"
        "controller-test-pki.service"
      ];
      environment.XTCP_CONTROLLER_DATABASE_DSN = "host=/run/postgresql dbname=xtcp user=root sslmode=disable";
      serviceConfig = {
        ExecStart =
          "${binaries.xtcp2-controller}/bin/xtcp2-controller -migrate -cert=/var/lib/controller-test/pki/controller.crt -key=/var/lib/controller-test/pki/controller.key -ca=/var/lib/controller-test/pki/ca.crt -clickhouse=http://127.0.0.1:8123"
          + lib.optionalString soak " -discoveryTemplates=/etc/xtcp-test/discovery.json -discoveryInterval=1m -discoveryWindow=1m";
        Restart = "on-failure";
      };
    };
    systemd.services.tcp-echo = {
      wantedBy = [ "multi-user.target" ];
      serviceConfig.ExecStart = "${binaries.tcp_server}/bin/tcp_server -count 32 -bind 0.0.0.0";
      serviceConfig.StandardOutput = "null";
      serviceConfig.StandardError = "null";
    };
    systemd.services.controller-integration = {
      wantedBy = [ "multi-user.target" ];
      after = [
        "network-online.target"
        "postgresql.service"
        "postgresql-setup.service"
        "clickhouse.service"
        "controller-test-pki.service"
        "tcp-echo.service"
      ];
      wants = [ "network-online.target" ];
      requires = [
        "postgresql.service"
        "clickhouse.service"
        "controller-test-pki.service"
        "tcp-echo.service"
      ];
      path = fixturePath ++ [
        pkgs.clickhouse
        pkgs.postgresql_15
      ];
      script = ''
        trap 'systemctl poweroff --no-block' EXIT
        ${harness}/bin/tcp-controller-integration -role=controller ${lib.optionalString soak "-soak"}
      '';
      serviceConfig = {
        Type = "oneshot";
        StandardOutput = "journal+console";
        StandardError = "journal+console";
        TimeoutStartSec = if soak then "115min" else "30min";
      };
    };
  };
  vmCheck = import ../../microvms/mkVmCheck.nix { inherit pkgs lib; } {
    inherit name;
    timeout = if soak then "120m" else "35m";
    launch = ''
      mkdir -p links "$VM_EVIDENCE/artifacts"
      ${lib.optionalString soak ''
        printf '{"cycles":%s,"interval_seconds":%s}\n' \
          "''${XTCP_SOAK_CYCLES:-3}" "''${XTCP_SOAK_INTERVAL_SECONDS:-180}" > "$VM_EVIDENCE/artifacts/soak-config.json"
      ''}
      export TEST_NETWORK="$PWD/links"
      pids=()
      cleanup() {
        kill "''${pids[@]}" 2>/dev/null || true
        wait "''${pids[@]}" 2>/dev/null || true
      }
      trap cleanup EXIT
      ${lib.concatImapStringsSep "\n" (i: vm: ''
        mkdir agent${toString i}
        (cd agent${toString i}; exec ${vm}/bin/run-agent${toString i}-vm) > "$VM_EVIDENCE/agent${toString i}.log" 2>&1 &
        pids+=("$!")
      '') agentVMs}
      mkdir controller
      cd controller
      ${controllerVM}/bin/run-controller-vm
    '';
    sentinels =
      if soak then
        [
          "TCP_CONTROLLER_SOAK_CYCLE_1_PASS"
          "TCP_CONTROLLER_SOAK_CYCLE_2_PASS"
          "TCP_CONTROLLER_SOAK_COOLDOWN_PASS"
          "TCP_CONTROLLER_SOAK_PASS"
        ]
      else
        [
          "TCP_CONTROLLER_RTT_PASS"
          "TCP_CONTROLLER_CANARY_PASS"
          "TCP_CONTROLLER_PERSIST_PASS"
          "TCP_CONTROLLER_HOLD_PASS"
          "TCP_CONTROLLER_ROLLBACK_PASS"
          "TCP_CONTROLLER_WATCHDOG_PASS"
          "TCP_CONTROLLER_PASS"
        ];
    failPattern = "TCP_CONTROLLER_FAIL";
    evidence = {
      inherit harness;
      controller = controllerVM;
    };
  };
  soakApp = pkgs.writeShellApplication {
    name = "test-tcp-controller-soak-vm";
    text = ''
      export XTCP_SOAK_CYCLES=3 XTCP_SOAK_INTERVAL_SECONDS=180
      args=()
      for arg in "$@"; do
        case "$arg" in
          --cycles=[2-6]) XTCP_SOAK_CYCLES=''${arg#--cycles=} ;;
          --interval=3m) XTCP_SOAK_INTERVAL_SECONDS=180 ;;
          --interval=4m) XTCP_SOAK_INTERVAL_SECONDS=240 ;;
          --interval=5m) XTCP_SOAK_INTERVAL_SECONDS=300 ;;
          --accel=*|--keep=*) args+=("$arg") ;;
          *) echo 'usage: test-tcp-controller-soak-vm [--cycles=2..6] [--interval=3m|4m|5m] [--accel=auto|kvm|tcg] [--keep=N]' >&2; exit 2 ;;
        esac
      done
      exec ${vmCheck.app}/bin/test-tcp-controller-soak-vm "''${args[@]}"
    '';
  };
in
(if soak then soakApp else vmCheck.app) // { inherit vmCheck; }
