# nix/modules/xtcp2-vector-path.nix
#
# Race-avoidance module for the Vector flavor.
#
# Background:
#   xtcp2's unixgram destination calls os.Stat(path) at startup
#   (pkg/xtcp/destinations_unixgram.go:32) and fails loudly if the peer
#   socket does not exist. Vector binds /run/xtcp2/output.sock
#   asynchronously, AFTER the topology loads — so plain After=vector.service
#   on xtcp2 still races (systemd Type=simple returns when the process
#   forks, not when the source has bound).
#
# Why not systemd.path:
#   The natural fit is a `systemd.paths.xtcp2` unit with
#   `PathExists=/run/xtcp2/output.sock`. But anchoring that path unit
#   with `After=vector.service` (so the path unit itself starts late)
#   produces an ordering cycle through basic.target/paths.target that
#   systemd resolves by deleting the path unit, defeating the purpose.
#
# What we do instead:
#   Inject an `ExecStartPre` into xtcp2.service that busy-waits for the
#   socket to appear (up to 60 s). The unit can be ordered after Vector
#   (or auto-started by `wants` from the self-test) without any cycle —
#   it just won't enter ExecStart until Vector has bound the socket.
#
{ pkgs, lib, ... }:

let
  waitForSocket = pkgs.writeShellScript "xtcp2-wait-for-vector-sock" ''
    set -eu
    for _ in $(${pkgs.coreutils}/bin/seq 1 60); do
      if [ -S /run/xtcp2/output.sock ]; then
        exit 0
      fi
      sleep 1
    done
    echo "xtcp2: /run/xtcp2/output.sock never appeared after 60 s" >&2
    exit 1
  '';
in
{
  systemd.services.xtcp2.serviceConfig.ExecStartPre = lib.mkBefore [
    "${waitForSocket}"
  ];
}
