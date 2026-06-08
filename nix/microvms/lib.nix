# nix/microvms/lib.nix
#
# Helpers for the xtcp2 microvm lifecycle.
#
# Currently provides:
#   - mkLifecycleFullTest: launches the VM, scrapes its serial console for the
#     XTCP2_SELF_TEST_* sentinels, returns pass/fail with labeled output.
#
{
  pkgs,
  lib,
  constants,
}:

rec {
  # Build the lifecycle-full-test runner for a given arch.
  mkLifecycleFullTest =
    { arch, vm }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-lifecycle-full-test-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        netcat-gnu
        gawk
        procps
      ];
      text = ''
        set -u
        SERIAL_PORT=${toString cfg.serialPort}
        TIMEOUT=180   # seconds; v1 target is <90s but allow headroom
        LOG=$(mktemp -t xtcp2-vm-XXXX.log)

        echo "==> launching microvm (${arch}); serial port = $SERIAL_PORT"
        echo "==> serial transcript: $LOG"

        # Start the VM in the background; its qemu binds the serial port.
        ${vm}/bin/microvm-run > "$LOG" 2>&1 &
        vm_pid=$!

        cleanup() {
          # Try graceful shutdown first via serial
          if kill -0 "$vm_pid" 2>/dev/null; then
            # Best-effort: tell guest to power off; if that fails, kill QEMU
            ( printf 'systemctl poweroff\n' | nc -q 1 127.0.0.1 "$SERIAL_PORT" ) \
              >/dev/null 2>&1 || true
            sleep 5
            kill "$vm_pid" 2>/dev/null || true
            wait "$vm_pid" 2>/dev/null || true
          fi
        }
        trap cleanup EXIT

        # Wait for the overall sentinel or for timeout
        waited=0
        rc=2
        while [ "$waited" -lt "$TIMEOUT" ]; do
          if grep -q 'XTCP2_SELF_TEST_OVERALL_PASS' "$LOG"; then
            rc=0; break
          fi
          if grep -q 'XTCP2_SELF_TEST_OVERALL_FAIL' "$LOG"; then
            rc=1; break
          fi
          sleep 2
          waited=$((waited + 2))
        done

        echo ""
        echo "================================================"
        echo " xtcp2 microvm lifecycle result"
        echo "================================================"
        grep -E 'XTCP2_SELF_TEST_(SYSTEMD|METRICS|NETLINK|OVERALL)_(PASS|FAIL)' "$LOG" || true
        echo ""

        case "$rc" in
          0) echo "PASS: all three checks passed" ;;
          1) echo "FAIL: one or more checks failed (see lines above)" ;;
          *) echo "TIMEOUT: no overall sentinel after ''${TIMEOUT}s — last 40 log lines:"; tail -n 40 "$LOG" ;;
        esac
        exit "$rc"
      '';
    };
}
