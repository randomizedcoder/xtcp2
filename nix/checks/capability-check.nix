# nix/checks/capability-check.nix
#
# End-to-end test that xtcp2 refuses to start when a required Linux
# capability is missing, and that the diagnostic message names the
# missing cap + provides remediation. Much cheaper than the
# microvm-x86_64-capcheck-fail flavor: just spawns the binary in the
# Nix sandbox where the build user has no CAP_SYS_ADMIN (or any other
# privileged cap), reads stderr, asserts the expected substring.
#
# Sub-second per check, runs in the default `nix flake check` set.
# Catches:
#   - someone deletes the `requiredCaps` table by accident
#   - someone weakens the message format and breaks operator-facing
#     ergonomics (the test pins on the actual diagnostic text)
#   - someone makes checkCapabilities non-fatal again
#
{
  pkgs,
  lib,
  binaries,
}:

let
  xtcp2 = binaries.xtcp2;

  # Run xtcp2 with -conf so it tries to validate config + check caps,
  # but doesn't actually open netlink sockets. Exit code MUST be
  # non-zero (fatal capability error). stderr MUST contain the
  # capability name + the systemd remediation snippet.
  #
  # capsh isn't needed — the Nix builder runs as an unprivileged user
  # whose capability set is already empty, so xtcp2 will see no
  # CAP_SYS_ADMIN in /proc/self/status:CapEff and the fatal-tier
  # diagnostic fires naturally.
  mkCapCheck =
    {
      name,
      expectMissing,
      extraGrepArgs ? [ ],
    }:
    pkgs.runCommand "xtcp2-capability-check-${name}"
      {
        nativeBuildInputs = [ xtcp2 ];
      }
      ''
        set +e
        # Spawn xtcp2 with -dest null (no destination to bind) and
        # -maxLoops 1 (exit after one cycle). The cap check runs in
        # Init() before the first poll, so we expect a fatal exit
        # immediately. -frequency 1s + -timeout 0 reduces blocking
        # so the test doesn't sit on a non-responsive socket.
        output=$(${xtcp2}/bin/xtcp2 \
          -dest 'null' \
          -maxLoops 1 \
          -frequency 2s \
          -timeout 1s \
          2>&1)
        rc=$?
        set -e

        echo "----- xtcp2 stderr -----"
        echo "$output"
        echo "----- exit=$rc -----"

        if [ "$rc" -eq 0 ]; then
          echo "FAIL: xtcp2 exited 0 with no privileged caps — startup capability check is not fatal" >&2
          exit 1
        fi

        # Pin on the expected diagnostic substrings.
        for needle in "${expectMissing}: " "AmbientCapabilities" "CapabilityBoundingSet"; do
          if ! echo "$output" | grep -qF "$needle" ${lib.concatStringsSep " " extraGrepArgs}; then
            echo "FAIL: expected substring not found in stderr: $needle" >&2
            exit 1
          fi
        done

        echo "PASS: xtcp2 refused to start, diagnostic named ${expectMissing}"
        touch $out
      '';
in
{
  # The Nix sandbox lacks all elevated caps, so both required ones
  # (CAP_NET_ADMIN + CAP_SYS_ADMIN) are missing. We only need one
  # check that asserts CAP_NET_ADMIN appears first (it's listed
  # first in requiredCaps), but pinning on CAP_SYS_ADMIN explicitly
  # too gives us a guard against accidentally re-dropping it from
  # the table.
  capability-check-no-caps = mkCapCheck {
    name = "no-caps";
    expectMissing = "CAP_NET_ADMIN";
  };

  capability-check-names-sys-admin = mkCapCheck {
    name = "names-sys-admin";
    expectMissing = "CAP_SYS_ADMIN";
  };
}
