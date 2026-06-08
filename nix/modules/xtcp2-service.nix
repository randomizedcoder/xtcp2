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
      description = "Path to the xtcp2 JSON config file.";
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
  };

  config = lib.mkIf cfg.enable {
    systemd.services.xtcp2 = {
      description = "xtcp2 — TCP socket introspection via netlink";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "simple";
        ExecStart =
          "${cfg.package}/bin/xtcp2 -config ${cfg.configFile}"
          + lib.optionalString (cfg.extraArgs != [ ]) " ${lib.concatStringsSep " " cfg.extraArgs}";
        Restart = "on-failure";
        RestartSec = "2s";
        User = cfg.user;
        # netlink inet_diag and io_uring need elevated capabilities
        AmbientCapabilities = [
          "CAP_NET_ADMIN"
          "CAP_NET_RAW"
          "CAP_SYS_RESOURCE"
        ];
        CapabilityBoundingSet = [
          "CAP_NET_ADMIN"
          "CAP_NET_RAW"
          "CAP_SYS_RESOURCE"
        ];
      };
    };
  };
}
