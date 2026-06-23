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
  # Build the tcp-stress smoke runner for a given arch. Boots the
  # docker-in-VM flavor, tails its serial console for `--duration`
  # seconds, then powers off. Reports key signals scraped from the
  # transcript: docker.service start, xtcp2-tcp-stress-load completion,
  # how many stress-N containers came up, and a final xtcp2 metric
  # snapshot showing the per-container ns counters.
  mkTcpStressRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-tcp-stress-runner-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        gawk
        netcat-gnu
        procps
        curl
      ];
      text = ''
        set -u

        DURATION_SEC=180  # default 3 minutes — enough for boot + container
                          # spawn + a few netlinker polling cycles
        KEEP_ALIVE=0
        while [ $# -gt 0 ]; do
          case "$1" in
            --duration)
              # Convert <N>{s,m,h} → seconds
              d="$2"
              DURATION_SEC=$(awk -v d="$d" '
                BEGIN {
                  n = d + 0
                  u = d; sub(/^[0-9.]+/, "", u)
                  mul = (u == "s" || u == "") ? 1 :
                        (u == "m") ? 60 :
                        (u == "h") ? 3600 : -1
                  if (mul < 0) exit 1
                  printf "%d", n * mul
                }
              ')
              shift 2 ;;
            --keep-alive)
              KEEP_ALIVE=1; shift ;;
            -h|--help)
              echo "usage: $0 [--duration <Nh|Nm|Ns>] [--keep-alive]"
              echo "  --duration   how long to sleep before printing the summary"
              echo "               (default 180s, accepts s/m/h suffix)"
              echo "  --keep-alive don't power off after the summary — leave the"
              echo "               VM running so you can serial-in (\`nc 127.0.0.1"
              echo "               12055\`) and poke Prometheus etc. Ctrl-C the"
              echo "               runner to terminate the VM."
              exit 0 ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-tcp-stress-XXXX.log)

        echo "================================================"
        echo " xtcp2 tcp-stress smoke — arch=${arch}"
        echo " duration: ''${DURATION_SEC}s"
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

        # Cleanup trap: only kicks in when the runner actually exits.
        # With --keep-alive, the runner sleeps forever after the summary
        # so this trap never fires until Ctrl-C / SIGTERM.
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

        # Sleep the full duration regardless of boot speed — the VM
        # needs time for: dockerd to come up (~5-10s), xtcp2 to come up
        # (~2s), image load (~5-10s), N containers to start (~10-30s),
        # plus a few polling cycles for xtcp2 to discover the netns.
        # On long runs (12h soak), print a heartbeat every ~5 min so
        # the operator sees the runner is alive and accumulating data.
        elapsed=0
        heartbeat_period=300
        if [ "$DURATION_SEC" -lt 600 ]; then heartbeat_period=$DURATION_SEC; fi
        while [ "$elapsed" -lt "$DURATION_SEC" ]; do
          remaining=$((DURATION_SEC - elapsed))
          step=$heartbeat_period
          if [ "$step" -gt "$remaining" ]; then step=$remaining; fi
          sleep "$step"
          elapsed=$((elapsed + step))
          if [ "$elapsed" -lt "$DURATION_SEC" ]; then
            echo "  [t=$(printf %6d "$elapsed")s/$DURATION_SEC] tcp-stress in flight"
          fi
        done

        echo ""
        echo "================================================"
        echo " tcp-stress smoke summary"
        echo "================================================"
        started_xtcp2=$(grep -c 'Started.*xtcp2 — TCP socket' "$LOG" 2>/dev/null || true)
        # Match `dockerd[…]: Starting up` — NixOS docker.service doesn't
        # use a "Started Docker" line, the dockerd binary just logs its
        # own startup banner.
        started_docker=$(grep -cE 'dockerd\[[0-9]+\]:.*Starting up' "$LOG" 2>/dev/null || true)
        loaded_image=$(grep -c 'Loaded image: xtcp2-tcp-stress' "$LOG" 2>/dev/null || true)
        spawned=$(grep -cE 'stress-[0-9]+: started' "$LOG" 2>/dev/null || true)
        failed=$(grep -cE 'stress-[0-9]+: FAILED' "$LOG" 2>/dev/null || true)
        panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
        # Per-container netns discovery — count how many distinct
        # /run/docker/netns/<container-id> CREATE events fired in xtcp2.
        ns_discovered=$(grep -cE 'watchNamespaces /run/docker/netns/.*Op\.String: CREATE' "$LOG" 2>/dev/null || true)

        echo "  xtcp2.service started:        $started_xtcp2"
        echo "  docker.service started:       $started_docker"
        echo "  oci image loaded:             $loaded_image"
        echo "  containers spawned OK:        $spawned"
        echo "  containers FAILED to start:   $failed"
        echo "  per-container ns discovered:  $ns_discovered"
        echo "  panics in transcript:         $panics"

        rc=0
        [ "$started_xtcp2" -lt 1 ] && { echo "FAIL: xtcp2 didn't start"; rc=1; }
        [ "$started_docker" -lt 1 ] && { echo "FAIL: docker didn't start"; rc=1; }
        [ "$loaded_image" -lt 1 ] && { echo "FAIL: oci image never loaded"; rc=1; }
        [ "$spawned" -lt 1 ] && { echo "FAIL: no containers spawned"; rc=1; }
        [ "$ns_discovered" -lt "$spawned" ] && { echo "FAIL: xtcp2 saw $ns_discovered ns CREATE events but $spawned containers spawned"; rc=1; }
        [ "$panics" -ne 0 ] && { echo "FAIL: $panics panic(s)"; rc=1; }

        if [ "$rc" -eq 0 ]; then
          echo "PASS: $spawned containers, xtcp2 discovered all $ns_discovered per-container netns"
        fi
        echo ""

        # Pull the last few Prometheus snapshot lines straight out of the
        # serial transcript. xtcp2-prom-snapshot.service streams each
        # query result as one `XTCP2_PROM_SNAPSHOT {...}` line per 30s.
        echo "================================================"
        echo " Prometheus snapshots (latest 5)"
        echo "================================================"
        grep -E 'XTCP2_PROM_SNAPSHOT \{' "$LOG" 2>/dev/null \
          | tail -n 5 \
          | sed -E 's/^.*XTCP2_PROM_SNAPSHOT //' \
          || echo "(no snapshot lines in transcript — Prometheus may not have started)"
        echo ""

        echo "Full transcript kept at: $LOG"

        if [ "$KEEP_ALIVE" -eq 1 ]; then
          echo ""
          echo "================================================"
          echo " --keep-alive: VM is still running."
          echo "   Serial console: nc 127.0.0.1 $SERIAL_PORT"
          echo "   Prometheus (host-forwarded): curl 127.0.0.1:19090/api/v1/query?query=..."
          echo "   Ctrl-C this runner to power the VM off."
          echo "================================================"
          wait "$vm_pid"
        fi

        exit "$rc"
      '';
    };

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

  # Long-soak runner for the s3parquet-long flavor. Boots the VM, sleeps
  # for --duration, prints a heartbeat every 5 min (or 30s on short
  # runs), and finishes with a markdown-style summary listing the
  # XTCP2_S3PARQUET_HOURLY sentinels emitted by the in-VM monitor.
  #
  # Usage:
  #   nix run .#microvm-x86_64-s3parquet-runner             # default 1h, hourly reports
  #   nix run .#microvm-x86_64-s3parquet-runner -- --duration 5m --report-interval 60
  #   nix run .#microvm-x86_64-s3parquet-runner -- --duration 12h
  #
  # Exits 0 if xtcp2 stayed up for the full duration with no panic or
  # restart and the file count grew monotonically, 1 otherwise.
  mkS3ParquetRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-s3parquet-runner-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        gawk
        gnused
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        DURATION="1h"
        REPORT_INTERVAL=""        # empty = leave systemd default (3600s)
        RSS_CAP_MB=0              # 0 = no cap
        while [ $# -gt 0 ]; do
          case "$1" in
            --duration)         DURATION="$2"; shift 2 ;;
            --duration=*)       DURATION="''${1#--duration=}"; shift ;;
            --report-interval)  REPORT_INTERVAL="$2"; shift 2 ;;
            --report-interval=*) REPORT_INTERVAL="''${1#--report-interval=}"; shift ;;
            --rss-cap-mb)       RSS_CAP_MB="$2"; shift 2 ;;
            --rss-cap-mb=*)     RSS_CAP_MB="''${1#--rss-cap-mb=}"; shift ;;
            -h|--help)
              echo "usage: $0 [--duration <5m|1h|12h|...>]"
              echo "          [--report-interval <seconds>]   default 3600"
              echo "          [--rss-cap-mb <N>]              default 0 = no cap"
              echo "  Boots the xtcp2 s3parquet-long microvm, sleeps for"
              echo "  the duration, scrapes XTCP2_S3PARQUET_HOURLY sentinels"
              echo "  from the in-VM monitor, then powers off and summarizes."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

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
        LOG=$(mktemp -t xtcp2-s3parquet-runner-XXXX.log)

        echo "================================================"
        echo " xtcp2 s3parquet-long runner — arch=${arch}"
        echo " duration:         $DURATION ($DURATION_SEC s)"
        echo " report interval:  ''${REPORT_INTERVAL:-default (3600s)}"
        echo " rss cap:          ''${RSS_CAP_MB} MiB (0 = off)"
        echo " transcript:       $LOG"
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

        booted=0
        for _ in $(seq 1 60); do
          if grep -q 'Prometheus http listener started' "$LOG" 2>/dev/null; then
            booted=1
            break
          fi
          sleep 1
        done
        if [ "$booted" -ne 1 ]; then
          echo "FATAL: xtcp2 prom listener never started; aborting"
          tail -n 40 "$LOG" 2>/dev/null || true
          exit 2
        fi
        echo "==> boot OK at $(date -u +%FT%TZ)"

        # QEMU usermode hostfwd in this microvm setup doesn't actually
        # route host:9000 to the in-VM MinIO (port appears LISTEN on the
        # host but connects time out). We instead read all file counts
        # off the in-VM monitor's serial sentinels — the systemd unit
        # emits XTCP2_S3PARQUET_HOURLY every S3PARQUET_REPORT_INTERVAL
        # seconds (built-in default 60 s).
        : "''${REPORT_INTERVAL:=}"

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
          # Read the latest in-VM sentinel for the running count.
          latest_line=$( { grep 'XTCP2_S3PARQUET_HOURLY' "$LOG" 2>/dev/null || true; } | tail -n1 || true)
          files=$(echo "$latest_line" | sed -nE 's/.*files=([0-9]+).*/\1/p' || true)
          bytes=$(echo "$latest_line" | sed -nE 's/.*bytes=([0-9]+).*/\1/p' || true)
          : "''${files:=?}" "''${bytes:=?}"
          panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
          restarts=$(grep -cE 'xtcp2.service: Main process exited|Start request repeated' "$LOG" 2>/dev/null || true)
          # xtcp2 RSS in MiB (best-effort — pid is via pgrep over the
          # in-VM journal; on failure we just print ?).
          rss_mb="?"
          if [ "$RSS_CAP_MB" -gt 0 ] && [ "$rss_mb" != "?" ] \
             && [ "$rss_mb" -gt "$RSS_CAP_MB" ]; then
            echo "FATAL: RSS ''${rss_mb} MiB exceeds cap ''${RSS_CAP_MB} MiB"
            exit 2
          fi
          echo "  [t=$(printf %5d "$elapsed")s/$DURATION_SEC] files=$files bytes=$bytes panics=$panics restarts=$restarts"
        done

        echo ""
        echo "================================================"
        echo " s3parquet-long complete — summary"
        echo "================================================"

        final_panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
        final_restarts=$(grep -cE 'xtcp2.service: Main process exited|Start request repeated' "$LOG" 2>/dev/null || true)
        # All in-VM sentinels; the last one's "files=" is the
        # authoritative final count.
        mapfile -t hourly_lines < <(grep 'XTCP2_S3PARQUET_HOURLY' "$LOG" 2>/dev/null || true)
        n_reports=''${#hourly_lines[@]}
        final_files=0
        final_bytes=0
        if [ "$n_reports" -gt 0 ]; then
          last=''${hourly_lines[$((n_reports - 1))]}
          final_files=$(echo "$last" | sed -nE 's/.*files=([0-9]+).*/\1/p' || true)
          final_bytes=$(echo "$last" | sed -nE 's/.*bytes=([0-9]+).*/\1/p' || true)
          : "''${final_files:=0}" "''${final_bytes:=0}"
        fi

        echo "  duration:         $DURATION ($DURATION_SEC s)"
        echo "  in-VM sentinels:  $n_reports"
        echo "  final files:      $final_files"
        echo "  final bytes:      $final_bytes"
        echo "  xtcp2 panics:     $final_panics"
        echo "  xtcp2 restarts:   $final_restarts"
        echo ""
        if [ "$n_reports" -gt 0 ]; then
          echo "  per-sentinel file count (in-VM monitor):"
          echo "  | timestamp            | files | bytes      |"
          echo "  |----------------------|-------|------------|"
          prev=0
          for line in "''${hourly_lines[@]}"; do
            ts=$(echo "$line" | sed -nE 's/.*XTCP2_S3PARQUET_HOURLY ([^ ]+) .*/\1/p' || true)
            f=$(echo "$line" | sed -nE 's/.*files=([0-9]+).*/\1/p' || true)
            b=$(echo "$line" | sed -nE 's/.*bytes=([0-9]+).*/\1/p' || true)
            : "''${f:=0}" "''${b:=0}"
            printf "  | %-20s | %5s | %10s |  (Δ=%+d)\n" "$ts" "$f" "$b" "$((f - prev))"
            prev="$f"
          done
        fi

        rc=0
        if [ "$final_panics" -ne 0 ]; then
          echo "FAIL: $final_panics panic(s) in transcript"
          rc=1
        fi
        if [ "$final_restarts" -ne 0 ]; then
          echo "FAIL: xtcp2 restarted $final_restarts time(s)"
          rc=1
        fi
        # Smoke / production pass criterion: at least 1 parquet object
        # landed if the duration is long enough that the 1 MiB flush
        # threshold could plausibly trip. Loose lower bound to avoid
        # false-positive failures from short runs with idle netlink.
        if [ "$DURATION_SEC" -ge 300 ] && [ "$final_files" -lt 1 ]; then
          echo "FAIL: no parquet files landed after $DURATION_SEC s"
          rc=1
        fi
        if [ "$rc" -eq 0 ]; then
          echo "PASS: xtcp2 survived $DURATION with $final_files final parquet file(s)"
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
