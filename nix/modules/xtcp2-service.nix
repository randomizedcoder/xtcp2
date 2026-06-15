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
      description = "Additional CLI flags appended to the xtcp2 invocation.";
    };

    capabilities = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "CAP_NET_ADMIN"
        "CAP_NET_RAW"
        "CAP_SYS_RESOURCE"
        "CAP_SYS_ADMIN"
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
        # NOTE: xtcp2 does not yet implement `-config <path>`. Drive it via
        # `extraArgs` (CLI flags) instead. configFile is kept in the option
        # surface for forward-compatibility — when the daemon learns to
        # parse a JSON config, flip this back to `-config ${cfg.configFile}`.
        ExecStart =
          "${cfg.package}/bin/xtcp2"
          + lib.optionalString (cfg.extraArgs != [ ]) " ${lib.concatStringsSep " " cfg.extraArgs}";
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
