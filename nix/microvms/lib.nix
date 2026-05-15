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
  #
  # Parameters:
  #   arch         (string)  architecture key into constants.architectures
  #   vm           (drv)     microvm runner derivation
  #   suffix       (string)  optional name suffix for the wrapper binary
  #   sentinelRe   (string)  pipe-separated sentinel names to surface in the
  #                          summary grep. Default matches the minimal flavor.
  #   timeoutSec   (int)     overall scrape timeout in seconds.
  mkLifecycleFullTest =
    {
      arch,
      vm,
      suffix ? "",
      sentinelRe ? "SYSTEMD|METRICS|NETLINK|OVERALL",
      timeoutSec ? 180,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-lifecycle-full-test-${arch}${suffix}";
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
        VIRTCON_PORT=${toString cfg.virtioPort}
        TIMEOUT=${toString timeoutSec}
        LOG=$(mktemp -t xtcp2-vm-XXXX.log)

        echo "==> launching microvm (${arch}${suffix}); serial=$SERIAL_PORT virtio-console=$VIRTCON_PORT"
        echo "==> transcript: $LOG"

        # Start the VM in the background. qemu's stdout (its own diagnostics)
        # goes to a separate file; the VM's *consoles* are two TCP servers:
        #   - SERIAL_PORT  → `-serial tcp:server,nowait`  (ttyS0 / getty)
        #   - VIRTCON_PORT → virtio-console chardev        (hvc0 / systemd)
        # The kernel cmdline lists `console=ttyS0 console=hvc0` so the kernel
        # writes to both, but systemd's StandardOutput=journal+console emits
        # only to the LAST `console=` device — i.e. hvc0. Our self-test
        # sentinels therefore land on VIRTCON_PORT, not SERIAL_PORT. Capture
        # both into the same $LOG so the scrape grep sees everything.
        QEMU_LOG="''${LOG}.qemu"
        ${vm}/bin/microvm-run > "$QEMU_LOG" 2>&1 &
        vm_pid=$!

        nc_serial_pid=""
        nc_virtcon_pid=""
        for _ in $(seq 1 30); do
          if nc -z 127.0.0.1 "$SERIAL_PORT" 2>/dev/null; then
            nc 127.0.0.1 "$SERIAL_PORT" >> "$LOG" 2>&1 &
            nc_serial_pid=$!
            break
          fi
          sleep 1
        done
        for _ in $(seq 1 30); do
          if nc -z 127.0.0.1 "$VIRTCON_PORT" 2>/dev/null; then
            nc 127.0.0.1 "$VIRTCON_PORT" >> "$LOG" 2>&1 &
            nc_virtcon_pid=$!
            break
          fi
          sleep 1
        done

        # Best-effort shutdown: tell the guest to power off via the serial
        # console; if that fails (or if it does not exit within 5 s), kill
        # the qemu process directly. Inlined into the trap so the trap is
        # the only invocation site — avoids SC2329 false positives on a
        # trap-only function.
        trap '
          if kill -0 "$vm_pid" 2>/dev/null; then
            ( printf "systemctl poweroff\n" | nc -q 1 127.0.0.1 "$SERIAL_PORT" ) >/dev/null 2>&1 || true
            sleep 5
            kill "$vm_pid" 2>/dev/null || true
            wait "$vm_pid" 2>/dev/null || true
          fi
          if [ -n "$nc_serial_pid" ] && kill -0 "$nc_serial_pid" 2>/dev/null; then
            kill "$nc_serial_pid" 2>/dev/null || true
          fi
          if [ -n "$nc_virtcon_pid" ] && kill -0 "$nc_virtcon_pid" 2>/dev/null; then
            kill "$nc_virtcon_pid" 2>/dev/null || true
          fi
        ' EXIT

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
        grep -E 'XTCP2_SELF_TEST_(${sentinelRe})_(PASS|FAIL)' "$LOG" || true
        echo ""

        case "$rc" in
          0) echo "PASS: all checks passed" ;;
          1) echo "FAIL: one or more checks failed (see lines above)" ;;
          *) echo "TIMEOUT: no overall sentinel after ''${TIMEOUT}s — last 40 log lines:"; tail -n 40 "$LOG" ;;
        esac
        exit "$rc"
      '';
    };
}
