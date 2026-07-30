# nix/modules/nsq-server.nix
#
# NixOS module: runs a native nsqd inside the xtcp2 microvm plus an nsq_tail
# consumer, so the self-test can prove records flow through the nsq destination
# end-to-end.
#
# nsq is a queue: a message published to a topic is copied to every EXISTING
# channel at publish time, so — like the pub/sub brokers — the consumer must
# exist before xtcp2 publishes. nsq_tail creates the `${channel}` channel on the
# `${topic}` topic and FINISHes each message it receives, so nsqd's per-channel
# `finish_count` (exposed at /stats) is a deterministic count of consumed
# records — no output parsing needed. nsq-consumer is ordered Before xtcp2.
#
# Test fixture: data in a tmpfs-backed StateDirectory for the VM's life.
#
{
  topic ? "xtcp2-records",
  channel ? "selftest",
  tcpPort ? 4150,
  httpPort ? 4151,
}:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  consumerScript = pkgs.writeShellScript "xtcp2-nsq-consumer" ''
    set -u
    # Wait for nsqd's TCP port before subscribing (bash /dev/tcp — no extra tools).
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if (exec 3<>/dev/tcp/127.0.0.1/${toString tcpPort}) 2>/dev/null; then
        exec 3>&- 3<&-
        break
      fi
      sleep 1
    done
    # Consume the topic on a named channel; nsq_tail finishes each message, so
    # nsqd's channel finish_count reflects real consumption.
    exec ${pkgs.nsq}/bin/nsq_tail \
      --topic=${topic} --channel=${channel} \
      --nsqd-tcp-address=127.0.0.1:${toString tcpPort}
  '';
in
{
  systemd.services.nsqd = {
    description = "nsqd for the xtcp2 nsq-destination integration test";
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = ''
        ${pkgs.nsq}/bin/nsqd \
          --tcp-address 0.0.0.0:${toString tcpPort} \
          --http-address 0.0.0.0:${toString httpPort} \
          --data-path /var/lib/nsqd
      '';
      StateDirectory = "nsqd";
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  systemd.services.nsq-consumer = {
    description = "Consumes the xtcp2 nsq topic so nsqd records finished messages";
    after = [ "nsqd.service" ];
    requires = [ "nsqd.service" ];
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

  # xtcp2 publishes only after the consumer channel exists (nsq copies messages
  # to existing channels at publish time).
  systemd.services.xtcp2 = {
    after = [ "nsq-consumer.service" ];
    requires = [ "nsqd.service" ];
  };
}
