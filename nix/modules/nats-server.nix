# nix/modules/nats-server.nix
#
# NixOS module: runs a native NATS server inside the xtcp2 microvm plus a
# long-lived subscriber the self-test uses to prove records flow through the
# nats destination end-to-end.
#
# The nats destination PUBLISHes each marshalled payload to a NATS subject
# (config.Topic). Core NATS is fire-and-forget (no retention), so the subscriber
# must be listening before xtcp2 publishes; it is ordered Before xtcp2.service.
# The nats client uses RetryOnFailedConnect, so xtcp2 tolerates a cold server and
# keeps publishing as it reconnects.
#
# Test fixture: no auth, storage in RAM for the VM's life. Subscriber output goes
# to the journal (which the self-test counts) and the console (for debugging).
#
{
  subject ? "xtcp2-records",
  port ? 4222,
}:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  subscriberScript = pkgs.writeShellScript "xtcp2-nats-subscriber" ''
    set -u
    # Wait for the server to accept TCP connections before subscribing
    # (bash /dev/tcp — no extra tools, version-independent).
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if (exec 3<>/dev/tcp/127.0.0.1/${toString port}) 2>/dev/null; then
        exec 3>&- 3<&-
        break
      fi
      sleep 1
    done
    # Subscribe; natscli prints a "Received on" header per message. Run it under a
    # PTY via `unbuffer` so it flushes per message (it block-buffers otherwise).
    exec ${pkgs.expect}/bin/unbuffer \
      ${pkgs.natscli}/bin/nats --server nats://127.0.0.1:${toString port} \
      sub ${subject}
  '';
in
{
  systemd.services.nats-server = {
    description = "NATS server for the xtcp2 nats-destination integration test";
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = "${pkgs.nats-server}/bin/nats-server --addr 0.0.0.0 --port ${toString port}";
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  systemd.services.nats-subscriber = {
    description = "Subscribes to the xtcp2 nats subject and logs messages for the self-test to count";
    after = [ "nats-server.service" ];
    requires = [ "nats-server.service" ];
    before = [ "xtcp2.service" ];
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = "${subscriberScript}";
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  # xtcp2 publishes only after the subscriber is up (core NATS has no replay).
  systemd.services.xtcp2 = {
    after = [ "nats-subscriber.service" ];
    requires = [ "nats-server.service" ];
  };
}
