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
  effectiveMem =
    if isVector then
      cfg.memVector
    else if isTcpStress then
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

        # Phase C: enable docker daemon for the tcp-stress sink. Adds
        # ~150 MiB to the VM image (dockerd + containerd) but keeps the
        # rest of the surface minimal — no docker-buildx, no compose.
        virtualisation.docker = lib.mkIf isTcpStress {
          enable = true;
          # Disable docker's bridge auto-configuration via iptables to
          # avoid microvm-vs-host iptables-version drift. Containers
          # still get bridge networking via dockerd's default bridge.
          enableOnBoot = true;
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
