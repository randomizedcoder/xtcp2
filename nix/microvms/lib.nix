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
  # Build the soak runner for a given arch. Long-running on-demand test:
  # boots the soak microvm (xtcp2 + nsTest churn + /metrics scraper),
  # waits for --duration to elapse, then powers off and prints a summary
  # (uptime, restart count, last few metric samples, panic check).
  #
  # Usage:
  #   nix run .#microvm-x86_64-soak                 # default 1h
  #   nix run .#microvm-x86_64-soak -- --duration 24h
  #   nix run .#microvm-x86_64-soak -- --duration 5m
  #
  # Exits 0 if xtcp2 stayed up for the full duration with no panic or
  # restart in the journal, 1 otherwise.
  mkSoakRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-soak-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        gawk
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        DURATION="1h"
        while [ $# -gt 0 ]; do
          case "$1" in
            --duration)  DURATION="$2"; shift 2 ;;
            --duration=*) DURATION="''${1#--duration=}"; shift ;;
            -h|--help)
              echo "usage: $0 [--duration <1h|24h|5m|...>]"
              echo "  Boots the xtcp2 soak microvm, runs nsTest churn +"
              echo "  /metrics scrape for the given duration, then powers"
              echo "  off and reports pass/fail."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        # Convert <N>{s,m,h,d} → seconds. coreutils' sleep accepts the
        # suffix directly but we also want to enforce a bounded grep loop
        # for the heartbeat check.
        DURATION_SEC=$(awk -v d="$DURATION" '
          BEGIN {
            n = d + 0
            u = d
            sub(/^[0-9.]+/, "", u)
            mul = (u == "s" || u == "") ? 1 :
                  (u == "m") ? 60 :
                  (u == "h") ? 3600 :
                  (u == "d") ? 86400 : -1
            if (mul < 0) { print "ERR"; exit 1 }
            printf "%d", n * mul
          }
        ')
        if [ "$DURATION_SEC" = "ERR" ] || [ "$DURATION_SEC" -lt 60 ]; then
          echo "FATAL: --duration $DURATION not parseable or under 60s" >&2
          exit 2
        fi

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-soak-XXXX.log)

        echo "================================================"
        echo " xtcp2 microvm soak — arch=${arch}"
        echo " duration: $DURATION ($DURATION_SEC s)"
        echo " transcript: $LOG"
        echo "================================================"

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

        trap '
          if kill -0 "$vm_pid" 2>/dev/null; then
            ( printf "systemctl poweroff\n" | nc -q 1 127.0.0.1 "$SERIAL_PORT" ) >/dev/null 2>&1 || true
            sleep 10
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

        # Wait for xtcp2 to be up.
        booted=0
        for _ in $(seq 1 60); do
          if grep -q 'Prometheus http listener started' "$LOG" 2>/dev/null; then
            booted=1
            break
          fi
          sleep 1
        done
        if [ "$booted" -ne 1 ]; then
          echo "FATAL: xtcp2 prom listener never started; aborting soak"
          tail -n 40 "$LOG" 2>/dev/null || true
          exit 2
        fi
        echo "==> boot OK — soak starting at $(date -u +%FT%TZ)"

        # Heartbeat: every 5 minutes (or 30s on short runs) print a one-
        # liner to the host stdout so a watching operator sees progress.
        heartbeat_period=300
        if [ "$DURATION_SEC" -lt 600 ]; then heartbeat_period=30; fi

        elapsed=0
        while [ "$elapsed" -lt "$DURATION_SEC" ]; do
          if ! kill -0 "$vm_pid" 2>/dev/null; then
            echo "FATAL: qemu died at t=$elapsed s; tail of transcript:"
            tail -n 40 "$LOG"
            exit 2
          fi
          sleep "$heartbeat_period"
          elapsed=$((elapsed + heartbeat_period))
                    # grep -c always prints 0 to stdout when there are no matches
          # (and exits 1). Don't chain `|| echo 0` — that would emit "0"
          # twice and break the arithmetic in `[ "$panics" -ne 0 ]`.
          churn=$(grep -cE 'Added namespace|Removed namespace' "$LOG" 2>/dev/null || true)
          panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
          restarts=$(grep -cE 'xtcp2.service: Main process exited|Start request repeated' "$LOG" 2>/dev/null || true)
          echo "  [t=$(printf %5d "$elapsed")s/$DURATION_SEC] churn_lines=$churn panics=$panics xtcp2_restarts=$restarts"
        done

        echo ""
        echo "================================================"
        echo " soak complete — summary"
        echo "================================================"

        final_churn=$(grep -cE 'Added namespace|Removed namespace' "$LOG" 2>/dev/null || true)
        final_panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
        final_restarts=$(grep -cE 'xtcp2.service: Main process exited|Start request repeated' "$LOG" 2>/dev/null || true)
        echo "  duration:         $DURATION ($DURATION_SEC s)"
        echo "  ns-churn events:  $final_churn"
        echo "  xtcp2 panics:     $final_panics"
        echo "  xtcp2 restarts:   $final_restarts"

        rc=0
        if [ "$final_panics" -ne 0 ]; then
          echo "FAIL: $final_panics panic(s) in transcript"
          rc=1
        fi
        if [ "$final_restarts" -ne 0 ]; then
          echo "FAIL: xtcp2 restarted $final_restarts time(s) during soak"
          rc=1
        fi
        if [ "$final_churn" -lt 10 ]; then
          echo "FAIL: only $final_churn ns-churn events seen — nsTest may have hung"
          rc=1
        fi
        if [ "$rc" -eq 0 ]; then
          echo "PASS: xtcp2 survived $DURATION soak with $final_churn ns-churn events"
        fi
        echo ""
        echo "Full transcript kept at: $LOG"
        exit "$rc"
      '';
    };

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
      # When true, after a passing OVERALL sentinel the runner also looks
      # for an XTCP2_COVERAGE_DUMP_START / _END block in the log, decodes
      # it (base64 + gzip + tar), writes the resulting Go coverage data
      # into "$XTCP2_COVERDIR" (env var, defaults to /tmp/xtcp2cov), and
      # logs the file count it extracted. Used by the coverage flavor.
      scrapeCoverage ? false,
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
        gnutar
        gzip
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
        ${
          if scrapeCoverage then
            ''
              # Coverage scrape: extract the base64+gzip+tar blob between markers
              # and unpack into $XTCP2_COVERDIR. Wait briefly for the dump to
              # complete before scraping (the VM may still be flushing).
              COVERDIR="''${XTCP2_COVERDIR:-/tmp/xtcp2cov}"
              mkdir -p "$COVERDIR"
              for _ in $(seq 1 30); do
                if grep -q 'XTCP2_COVERAGE_DUMP_END' "$LOG"; then
                  break
                fi
                sleep 1
              done
              if grep -q 'XTCP2_COVERAGE_DUMP_START' "$LOG" \
                && grep -q 'XTCP2_COVERAGE_DUMP_END' "$LOG"; then
                # systemd routes the self-test's StandardOutput=journal+console
                # which prefixes every line with `[TIME] xtcp2-self-test[PID]: `.
                # Strip that prefix before base64-decoding.
                awk '/XTCP2_COVERAGE_DUMP_START/{flag=1;next} /XTCP2_COVERAGE_DUMP_END/{flag=0} flag' "$LOG" \
                  | sed -E 's/^\[[^]]*\] xtcp2-self-test\[[0-9]+\]: //' \
                  | tr -d '\r\n ' \
                  | base64 -d 2>/dev/null \
                  | gzip -dc 2>/dev/null \
                  | tar x -C "$COVERDIR" 2>/dev/null || true
                n=$(find "$COVERDIR" -type f | wc -l)
                echo "coverage: extracted $n file(s) into $COVERDIR"
              else
                echo "coverage: no XTCP2_COVERAGE_DUMP block found in transcript"
              fi
            ''
          else
            ""
        }
        exit "$rc"
      '';
    };
}
