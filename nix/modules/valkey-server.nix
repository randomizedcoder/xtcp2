# nix/modules/valkey-server.nix
#
# NixOS module: runs a single-node Valkey (Redis-protocol) server inside the
# xtcp2 microvm, plus a long-lived subscriber that the self-test uses to prove
# records actually flow through the valkey destination end-to-end.
#
# Why a subscriber and not just a post-hoc query: the valkey destination
# PUBLISHes each marshalled payload to a pub/sub channel (config.Topic), and
# pub/sub has no retention — a message published with no subscriber is dropped.
# So valkey-subscriber must be listening BEFORE xtcp2 starts publishing. It is
# ordered Before xtcp2.service, and xtcp2 is ordered After it; the daemon then
# polls repeatedly, so the subscriber catches a steady stream regardless of the
# exact first-publish/first-subscribe race. It logs every received message to
# the journal, which the self-test counts (journalctl -u valkey-subscriber).
#
# Everything here is a throwaway test fixture: no auth (protected-mode off), no
# persistence (save '' / appendonly no), storage only in RAM for the VM's life.
#
{
  channel ? "xtcp2-records",
  port ? 6379,
}:

{
  config,
  lib,
  pkgs,
  ...
}:

let
  subscriberScript = pkgs.writeShellScript "xtcp2-valkey-subscriber" ''
    set -u
    # Wait for the server to accept connections before subscribing.
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if ${pkgs.valkey}/bin/valkey-cli -h 127.0.0.1 -p ${toString port} ping >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done
    # Subscribe; every received message is a 3-element reply whose first element
    # is the literal "message" type marker. Output goes to the journal (and the
    # serial console) so the self-test counts deliveries via journalctl.
    #
    # Run valkey-cli under a PTY via `unbuffer`: valkey-cli block-buffers stdout
    # when it isn't a terminal (a plain pipe to journald), so messages never
    # surface — even stdbuf can't defeat its internal buffering. Under a PTY it
    # detects "interactive" and flushes per reply.
    exec ${pkgs.expect}/bin/unbuffer \
      ${pkgs.valkey}/bin/valkey-cli -h 127.0.0.1 -p ${toString port} \
      subscribe ${channel}
  '';
in
{
  # Native valkey-server as a plain systemd unit (full control, no auth/persist).
  # Bound on all interfaces so a host QEMU hostfwd could reach it if needed;
  # xtcp2 talks to it via 127.0.0.1 inside the VM.
  systemd.services.valkey-server = {
    description = "Valkey server for the xtcp2 valkey-destination integration test";
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      Type = "simple";
      ExecStart = ''
        ${pkgs.valkey}/bin/valkey-server \
          --bind 0.0.0.0 --port ${toString port} \
          --protected-mode no --save "" --appendonly no
      '';
      Restart = "on-failure";
      StandardOutput = "journal+console";
      StandardError = "journal+console";
    };
  };

  systemd.services.valkey-subscriber = {
    description = "Subscribes to the xtcp2 valkey channel and logs messages for the self-test to count";
    after = [ "valkey-server.service" ];
    requires = [ "valkey-server.service" ];
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

  # xtcp2 must publish only after the subscriber is up (pub/sub has no replay),
  # and only once the server is reachable. This merges with the services.xtcp2
  # unit that mkVm.nix generates.
  systemd.services.xtcp2 = {
    after = [ "valkey-subscriber.service" ];
    requires = [ "valkey-server.service" ];
  };
}
