# nix/modules/broker-server.nix
#
# Parameterized NixOS module for the broker-destination integration tests
# (valkey / nats / nsq). For a broker flavor it runs, inside the xtcp2 microvm:
#   * a native broker server unit, and
#   * a long-lived consumer ordered *before* xtcp2.service.
#
# The brokers are fire-and-forget (pub/sub, or publish-time channel fan-out)
# with no replay, so the consumer must be listening BEFORE xtcp2 starts
# publishing — hence `before = [ "xtcp2.service" ]` on the consumer and the
# matching after/requires added onto services.xtcp2. The daemon then polls
# repeatedly, so the consumer catches a steady stream regardless of the exact
# first-publish/first-subscribe race. The self-test counts what the consumer
# received (journal lines, or nsqd's per-channel finish_count).
#
# This collapses the three previously byte-for-byte-parallel modules
# (valkey-server.nix / nats-server.nix / nsq-server.nix) into one; callers
# supply only the per-broker deltas. Everything here is a throwaway fixture:
# no auth, storage in RAM/tmpfs for the VM's life.
#
# The command/probe parameters are functions of `pkgs` so their store paths
# resolve against the VM module's own package set (as the originals did).
#
# Parameters:
#   serverName          server systemd unit name (e.g. "valkey-server", "nsqd")
#   consumerName        consumer systemd unit name (e.g. "valkey-subscriber")
#   label               human label used in the unit descriptions
#   serverExecStart     pkgs -> the server unit's ExecStart string
#   readyCheck          pkgs -> a shell command that exits 0 once the broker
#                       accepts connections (polled up to 60×1s before the
#                       consumer starts)
#   consumerExec        pkgs -> the consumer's exec line (should `exec …`)
#   serverServiceConfig extra serviceConfig merged into the server unit
#                       (e.g. { StateDirectory = "nsqd"; }); default { }
{
  serverName,
  consumerName,
  label,
  serverExecStart,
  readyCheck,
  consumerExec,
  serverServiceConfig ? { },
}:

{ pkgs, ... }:

let
  consumerScript = pkgs.writeShellScript "xtcp2-${consumerName}" ''
    set -u
    # Wait for the server to accept connections before subscribing.
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if ${readyCheck pkgs}; then
        break
      fi
      sleep 1
    done
    ${consumerExec pkgs}
  '';
in
{
  # Native broker server as a plain systemd unit (full control, no auth/persist).
  # Bound on all interfaces so a host QEMU hostfwd could reach it if needed;
  # xtcp2 talks to it via 127.0.0.1 inside the VM.
  systemd.services.${serverName} = {
    description = "${label} server for the xtcp2 ${label}-destination integration test";
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = serverExecStart pkgs;
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    }
    // serverServiceConfig;
  };

  systemd.services.${consumerName} = {
    description = "Consumes the xtcp2 ${label} topic and logs deliveries for the self-test to count";
    after = [ "${serverName}.service" ];
    requires = [ "${serverName}.service" ];
    before = [ "xtcp2.service" ];
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = "${consumerScript}";
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  # xtcp2 publishes only after the consumer is up (no replay) and the server is
  # reachable. Merges with the services.xtcp2 unit that mkVm.nix generates.
  systemd.services.xtcp2 = {
    after = [ "${consumerName}.service" ];
    requires = [ "${serverName}.service" ];
  };
}
