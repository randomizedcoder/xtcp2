# nix/modules/xtcp2-service.nix
#
# NixOS module: declares `services.xtcp2` and wires it to a systemd unit.
#
# Single source of truth for how xtcp2 runs as a long-lived service. Imported
# both by microvms/mkVm.nix and by any future bare-metal NixOS host.
#
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.xtcp2;
  lm = cfg.linkMonitor;
  xtcp2Exec = import ../lib/xtcp2-exec-start.nix { inherit pkgs lib; };
  # The link monitor flags come before extraArgs, so an -linkMonitor* flag in
  # extraArgs still wins: the Go flag package keeps the last value.
  linkMonitorArgs = [
    "-linkMonitor=${lib.boolToString lm.enable}"
  ]
  ++ lib.optionals lm.enable (
    [
      "-linkMonitorBaselineFile=${lm.baselineFile}"
      "-linkMonitorCollectionMode=${lm.collectionMode}"
      "-linkMonitorIPPaths=${lib.boolToString lm.ipPaths}"
    ]
    ++ lm.extraArgs
  );
in
{
  options.services.xtcp2 = {
    enable = lib.mkEnableOption "the xtcp2 TCP socket introspection daemon";

    package = lib.mkOption {
      type = lib.types.package;
      description = "The xtcp2 package providing /bin/xtcp2.";
    };

    configFile = lib.mkOption {
      type = lib.types.path;
      description = ''
        Path to the xtcp2 JSON config file. Reserved for a future on-disk
        config format — xtcp2's `cmd/xtcp2/xtcp2.go` does not yet define a
        `-config` flag, so this path is not currently passed on the command
        line. Configure the daemon via `extraArgs` (CLI flags) in the
        meantime.
      '';
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "root";
      description = ''
        User to run xtcp2 as. Must be root (or have CAP_NET_ADMIN) since
        netlink inet_diag requires elevated privileges.
      '';
    };

    extraArgs = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      description = ''
        Additional CLI flags appended to the xtcp2 invocation, one argv entry
        per element, passed verbatim: an empty string, a space, `%` or `$`
        reaches the daemon unchanged (see nix/lib/xtcp2-exec-start.nix).
      '';
    };

    linkMonitor = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = ''
          Run the embedded link monitor: the expected-up-link baseline,
          per-interface and RDMA checks, the problem summary on /metrics, the
          detail paths (/metrics/<category>, /metrics-all) and
          LinkMonitorService. `systemctl reload xtcp2` (SIGUSR1) relearns the
          baseline.
        '';
      };

      baselineFile = lib.mkOption {
        type = lib.types.str;
        default = "/var/lib/xtcp2/link-monitor-baseline.json";
        description = ''
          Baseline file recording how many links should be up, so the count
          survives restarts and reboots. The default is under the unit's
          StateDirectory (/var/lib/xtcp2, mode 0750); it must differ from any
          standalone go-link-monitor's baseline file.
        '';
      };

      collectionMode = lib.mkOption {
        type = lib.types.enum [
          "on-demand"
          "poll"
        ];
        default = "on-demand";
        description = ''
          on-demand polls errors and state each minute and settings every five,
          and collects the rest when a detail path is scraped; poll collects
          everything each stats interval.
        '';
      };

      ipPaths = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = ''
          Serve the read-only `ip … show` paths under /ip and /i on the
          Prometheus listener. They expose host addresses, so they are off by
          default.
        '';
      };

      extraArgs = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [
          "-linkMonitorMetricsCategories=link,rdma"
          "-linkMonitorErrorsClasses=link,rdma,drop"
        ];
        description = ''
          Further -linkMonitor* flags, passed only when the monitor is enabled.
          Run `xtcp2 -help` for the full set.
        '';
      };
    };

    capabilities = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "CAP_NET_ADMIN"
        "CAP_NET_RAW"
        "CAP_SYS_RESOURCE"
        "CAP_SYS_ADMIN"
        # Method B discovery reads /proc/<pid>/ns/net of OTHER processes to
        # enumerate live network namespaces; the kernel gates that behind
        # ptrace_may_access, which denies non-dumpable targets (system daemons,
        # `ip netns exec` children) even to root without CAP_SYS_PTRACE. Without
        # it the /proc scan sees only xtcp2's own ns and discovers nothing.
        "CAP_SYS_PTRACE"
      ];
      description = ''
        Linux capabilities granted to xtcp2 via AmbientCapabilities +
        CapabilityBoundingSet. Override in a test flavor (e.g. drop
        CAP_SYS_ADMIN) to validate the daemon's startup capability
        check. The default set is what production deployments need:
        see pkg/xtcp/init_capabilities.go for the full justification
        of each entry.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.xtcp2 = {
      description = "xtcp2 — TCP socket introspection via netlink";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "simple";
        RuntimeDirectory = "xtcp2";
        # /var/lib/xtcp2 holds the link monitor baseline. The daemon creates
        # it 0750 when missing; systemd creating it first keeps the mode and
        # owner right for a non-root User too.
        StateDirectory = "xtcp2";
        StateDirectoryMode = "0750";
        # NOTE: xtcp2 does not yet implement `-config <path>`. Drive it via
        # `extraArgs` (CLI flags) instead. configFile is kept in the option
        # surface for forward-compatibility — when the daemon learns to
        # parse a JSON config, flip this back to `-config ${cfg.configFile}`.
        ExecStart = xtcp2Exec.mkExecStart cfg.package (linkMonitorArgs ++ cfg.extraArgs);
        # `systemctl reload xtcp2` asks the link monitor to relearn the
        # expected up-link count, as SIGUSR1 does for go-link-monitor.
        ExecReload = "${pkgs.coreutils}/bin/kill -USR1 $MAINPID";
        # TODO(linkmonitor-in-xtcp2): ProtectSystem=strict with
        # ReadWritePaths=/var/lib/xtcp2 once the VM checks cover every
        # path xtcp2 writes (-capturePath, unix sockets, dest files).
        Restart = "on-failure";
        RestartSec = "2s";
        User = cfg.user;
        # netlink inet_diag needs CAP_NET_ADMIN; io_uring needs
        # CAP_SYS_RESOURCE for the locked-memory budget; CAP_SYS_ADMIN
        # is required for setns(CLONE_NEWNET) into per-namespace netlink
        # sockets. The set is exposed via the cfg.capabilities option
        # so test flavors can drop one and verify the daemon's startup
        # capability check fails cleanly. See
        # pkg/xtcp/init_capabilities.go for per-cap justification.
        AmbientCapabilities = cfg.capabilities;
        CapabilityBoundingSet = cfg.capabilities;
        # Default systemd TasksMax is 15% of kernel.pid_max which in a
        # microvm works out to ~1100. The 1h soak with 4-per-sec ns churn
        # hit exactly that ceiling: `runtime: failed to create new OS
        # thread (have 1121 already; errno=11)`. The Go runtime's
        # SetMaxThreads cap (xtcp2's -maxThreads, default 2000) only
        # bounds Go's internal pool; systemd's cgroup pids.max is the
        # outer wall and what was actually killing us. Raise both
        # explicitly so legitimate burst load (per-ns netlinkers ×
        # blocked syscalls) has headroom.
        TasksMax = 8192;
        LimitNPROC = 8192;
      };
    };
  };
}
