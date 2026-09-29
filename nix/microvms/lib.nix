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
        # Per-container netns discovery. Under Method B there is no inotify
        # CREATE event to grep — xtcp2 discovers each container's netns by
        # scanning /proc/<pid>/ns/net and starts one netNamespaceInstance
        # per namespace. The prom-snapshot service exposes the running total
        # as `XTCP2_NS_INSTANCES start=<N>` (host ns + one per container);
        # take the latest.
        ns_line=$(tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_NS_INSTANCES' || true)
        ns_started=$(printf '%s' "$ns_line" | grep -oE 'start=[0-9]+' | cut -d= -f2 || true)
        ns_started=''${ns_started:-0}

        echo "  xtcp2.service started:        $started_xtcp2"
        echo "  docker.service started:       $started_docker"
        echo "  oci image loaded:             $loaded_image"
        echo "  containers spawned OK:        $spawned"
        echo "  containers FAILED to start:   $failed"
        echo "  ns instances started:         $ns_started (host + per-container)"
        echo "  panics in transcript:         $panics"

        rc=0
        [ "$started_xtcp2" -lt 1 ] && { echo "FAIL: xtcp2 didn't start"; rc=1; }
        [ "$started_docker" -lt 1 ] && { echo "FAIL: docker didn't start"; rc=1; }
        [ "$loaded_image" -lt 1 ] && { echo "FAIL: oci image never loaded"; rc=1; }
        [ "$spawned" -lt 1 ] && { echo "FAIL: no containers spawned"; rc=1; }
        # Each spawned container's netns → one netNamespaceInstance start,
        # plus the host ns, so a healthy run has ns_started >= spawned. (A 0
        # here can also mean Prometheus/the snapshot service never came up —
        # check the XTCP2_PROM_SNAPSHOT section below.)
        [ "$ns_started" -lt "$spawned" ] && { echo "FAIL: xtcp2 started $ns_started ns instances but $spawned containers spawned — per-container discovery incomplete (or metrics unavailable)"; rc=1; }
        [ "$panics" -ne 0 ] && { echo "FAIL: $panics panic(s)"; rc=1; }

        if [ "$rc" -eq 0 ]; then
          echo "PASS: $spawned containers, xtcp2 started $ns_started per-namespace instances (host + all containers)"
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

  # Build the clickhouse-pipeline stress runner for a given arch. This is
  # the full end-to-end integration stress test: boots the docker-in-VM
  # flavor that runs redpanda + clickhouse + the tcp-stress containers,
  # with xtcp2 producing inet_diag records into Kafka → ClickHouse. It
  # taps the serial console for `--duration`, printing a heartbeat with
  # the live ClickHouse row count + distinct-netns count, then asserts
  # the pipeline actually moved data over time:
  #   - the redpanda/clickhouse images pulled and the stack came up
  #   - the stress containers spawned
  #   - rows in xtcp.xtcp_flat_records grew across the run (records kept
  #     flowing end-to-end, not just a one-shot burst)
  #   - records landed from multiple distinct netns_inode values (the
  #     Method B per-container discovery proof — under Method B there is
  #     no inotify CREATE event to grep, so ClickHouse row provenance is
  #     the discovery signal)
  #   - no panics, and RSS/threads plateaued (leak check over the run)
  #
  # Usage:
  #   nix run .#microvm-x86_64-clickhouse-pipeline-stress                  # default 1h
  #   nix run .#microvm-x86_64-clickhouse-pipeline-stress -- --duration 24h
  #   nix run .#microvm-x86_64-clickhouse-pipeline-stress -- --keep-alive  # poke it by hand
  mkClickPipeStressRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-clickpipe-stress-runner-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        gawk
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        DURATION_SEC=3600  # default 1h — first boot must pull redpanda +
                           # clickhouse, bring the stack up, then let rows
                           # accumulate; 24h is the production stress run.
        KEEP_ALIVE=0
        while [ $# -gt 0 ]; do
          case "$1" in
            --duration)
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
            --duration=*) d="''${1#--duration=}"; set -- --duration "$d" "''${@:2}" ;;
            --keep-alive)
              KEEP_ALIVE=1; shift ;;
            -h|--help)
              echo "usage: $0 [--duration <Nh|Nm|Ns>] [--keep-alive]"
              echo "  --duration   how long to run before the summary (default 1h)"
              echo "  --keep-alive don't power off after the summary — leave the VM"
              echo "               up so you can 'docker exec clickhouse clickhouse-client'"
              echo "               via the serial console. Ctrl-C to terminate."
              exit 0 ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        if [ "$DURATION_SEC" -lt 60 ]; then
          echo "FATAL: --duration under 60s leaves no time for the stack to boot" >&2
          exit 1
        fi

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-clickpipe-stress-XXXX.log)

        echo "================================================"
        echo " xtcp2 clickhouse-pipeline stress — arch=${arch}"
        echo " duration:   ''${DURATION_SEC}s"
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

        # Latest ClickHouse row/netns count, read cheaply from the tail of
        # the (potentially huge) transcript — never a full scan.
        latest_rows() {
          tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_CLICKPIPE_ROWS' || true
        }

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
            line=$(latest_rows)
            rows=$(printf '%s' "$line" | grep -oE 'rows=[0-9]+' | cut -d= -f2 || true)
            nsc=$(printf '%s' "$line" | grep -oE 'netns=[0-9]+' | cut -d= -f2 || true)
            kexc=$(printf '%s' "$line" | grep -oE 'kafka_exc=[0-9]+' | cut -d= -f2 || true)
            disk=$(printf '%s' "$line" | grep -oE 'disk=[0-9]+' | cut -d= -f2 || true)
            pnc=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
            echo "  [t=$(printf %6d "$elapsed")s/$DURATION_SEC] clickhouse rows=''${rows:-?} netns=''${nsc:-?} kafka_exc=''${kexc:-?} disk=''${disk:-?}% panics=''${pnc:-0}"
          fi
        done

        echo ""
        echo "================================================"
        echo " clickhouse-pipeline stress summary"
        echo "================================================"

        pull_fatal=$(grep -cE 'FATAL: docker (pull|not ready)' "$LOG" 2>/dev/null || true)
        redpanda_up=$(grep -cE 'redpanda-0: started' "$LOG" 2>/dev/null || true)
        spawned=$(grep -cE 'stress-[0-9]+: started' "$LOG" 2>/dev/null || true)
        cfailed=$(grep -cE 'stress-[0-9]+: FAILED' "$LOG" 2>/dev/null || true)
        panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)

        first_line=$(grep -am1 'XTCP2_CLICKPIPE_ROWS' "$LOG" 2>/dev/null || true)
        last_line=$(latest_rows)
        rows_first=$(printf '%s' "$first_line" | grep -oE 'rows=[0-9]+' | cut -d= -f2 || true)
        rows_last=$(printf '%s' "$last_line" | grep -oE 'rows=[0-9]+' | cut -d= -f2 || true)
        netns_last=$(printf '%s' "$last_line" | grep -oE 'netns=[0-9]+' | cut -d= -f2 || true)
        cid_last=$(printf '%s' "$last_line" | grep -oE 'container_id=[0-9]+' | cut -d= -f2 || true)
        kexc_last=$(printf '%s' "$last_line" | grep -oE 'kafka_exc=[0-9]+' | cut -d= -f2 || true)
        disk_last=$(printf '%s' "$last_line" | grep -oE 'disk=[0-9]+' | cut -d= -f2 || true)
        # Peak disk seen across the whole run, not just the last sample —
        # a saturation event can recover (TTL delete / redpanda retention)
        # yet still have frozen ingestion earlier, so assert on the max.
        disk_peak=$(grep -oE 'disk=[0-9]+' "$LOG" 2>/dev/null | cut -d= -f2 | sort -n | tail -1 || true)
        rows_first=''${rows_first:-0}
        rows_last=''${rows_last:-0}
        netns_last=''${netns_last:-0}
        cid_last=''${cid_last:-0}
        kexc_last=''${kexc_last:-0}
        disk_last=''${disk_last:-0}
        disk_peak=''${disk_peak:-0}

        echo "  redpanda/clickhouse pull FATAL: $pull_fatal"
        echo "  redpanda started:              $redpanda_up"
        echo "  stress containers spawned:     $spawned"
        echo "  stress containers FAILED:      $cfailed"
        echo "  clickhouse rows first -> last: $rows_first -> $rows_last"
        echo "  distinct netns (last sample):  $netns_last"
        echo "  distinct container_id (last):  $cid_last"
        echo "  kafka consumer exceptions:     $kexc_last"
        echo "  docker disk % (last / peak):    $disk_last / $disk_peak"
        echo "  panics in transcript:          $panics"

        # RSS/thread trend (leak check) — first/last XTCP2_RES_SNAPSHOT.
        first_res=$(grep -am1 'XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null || true)
        last_res=$(tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_RES_SNAPSHOT' || true)
        if [ -n "$first_res" ] && [ -n "$last_res" ]; then
          rss0=$(printf '%s' "$first_res" | grep -oE '"rss_kb":[0-9]+' | cut -d: -f2 || echo 0)
          rss1=$(printf '%s' "$last_res"  | grep -oE '"rss_kb":[0-9]+' | cut -d: -f2 || echo 0)
          thr0=$(printf '%s' "$first_res" | grep -oE '"threads":[0-9]+' | cut -d: -f2 || echo 0)
          thr1=$(printf '%s' "$last_res"  | grep -oE '"threads":[0-9]+' | cut -d: -f2 || echo 0)
          echo "  rss_kb trend:                  ''${rss0:-?} -> ''${rss1:-?}"
          echo "  threads trend:                 ''${thr0:-?} -> ''${thr1:-?}"
        fi
        echo ""

        rc=0
        [ "$pull_fatal" -ne 0 ] && { echo "FAIL: redpanda/clickhouse image pull failed — pipeline never came up"; rc=1; }
        [ "$spawned" -lt 1 ] && { echo "FAIL: no stress containers spawned"; rc=1; }
        [ "$rows_last" -lt 1 ] && { echo "FAIL: 0 rows reached ClickHouse — no records made it end-to-end"; rc=1; }
        [ "$rows_last" -le "$rows_first" ] && { echo "FAIL: ClickHouse rows did not grow ($rows_first -> $rows_last) — records stopped flowing"; rc=1; }
        [ "$netns_last" -lt 2 ] && { echo "FAIL: records from only $netns_last distinct netns — per-container discovery not proven end-to-end"; rc=1; }
        [ "$cid_last" -lt 1 ] && { echo "FAIL: 0 distinct container_id — container-id enrichment (-resolveContainerId) not proven end-to-end"; rc=1; }
        [ "$kexc_last" -ne 0 ] && { echo "FAIL: $kexc_last kafka consumer exception(s) — ClickHouse ingestion stalled (likely MEMORY_LIMIT_EXCEEDED)"; rc=1; }
        # Disk saturation on /var/lib/docker freezes offset commits and back-
        # pressures the producer — ingestion plateaus while looking like a
        # decode/consumer bug. FAIL hard at >=95%; WARN early at >=85% so a
        # 24h soak that is trending toward full is visible before it stalls.
        [ "$disk_peak" -ge 95 ] && { echo "FAIL: docker disk hit ''${disk_peak}% — /var/lib/docker saturated, ingestion back-pressured (grow the volume or shorten TTL/retention)"; rc=1; }
        [ "$disk_peak" -ge 85 ] && [ "$disk_peak" -lt 95 ] && echo "WARN: docker disk peaked at ''${disk_peak}% — approaching saturation; a longer soak may stall"
        [ "$panics" -ne 0 ] && { echo "FAIL: $panics panic(s) in transcript"; rc=1; }

        if [ "$rc" -eq 0 ]; then
          echo "PASS: pipeline moved $rows_last records to ClickHouse from $netns_last netns over ''${DURATION_SEC}s, $spawned containers, 0 panics"
        fi
        echo ""
        echo "Full transcript kept at: $LOG"

        if [ "$KEEP_ALIVE" -eq 1 ]; then
          echo ""
          echo "================================================"
          echo " --keep-alive: VM is still running."
          echo "   Serial console: nc 127.0.0.1 $SERIAL_PORT"
          echo "   e.g. docker exec clickhouse clickhouse-client -q \\"
          echo "        'SELECT count() FROM xtcp.xtcp_flat_records'"
          echo "   Ctrl-C this runner to power the VM off."
          echo "================================================"
          wait "$vm_pid"
        fi

        exit "$rc"
      '';
    };

  # Host runner for the s3parquet-stress flavor — the parquet→S3 analog of
  # mkClickPipeStressRunner. Same serial-tap + disk-guard + leak-check shape,
  # but it parses the XTCP2_S3PARQUET_HOURLY sentinel (files/bytes/rows/disk
  # from xtcp2's own upload counters, which are monotonic and so unaffected by
  # the 1h retention cleanup) and asserts on uploads + the MinIO disk instead
  # of ClickHouse rows.
  mkS3ParquetStressRunner =
    {
      arch,
      vm,
      # Default --duration when the caller passes none. The stress flavor uses
      # 1h; the low-frequency flavor overrides to 2h so the ~hourly staleness
      # timer flush is reliably captured even with no explicit --duration.
      defaultDurationSec ? 3600,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-s3parquet-stress-runner-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        gawk
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        DURATION_SEC=${toString defaultDurationSec}
        KEEP_ALIVE=0
        while [ $# -gt 0 ]; do
          case "$1" in
            --duration)
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
            --duration=*) d="''${1#--duration=}"; set -- --duration "$d" "''${@:2}" ;;
            --keep-alive)
              KEEP_ALIVE=1; shift ;;
            -h|--help)
              echo "usage: $0 [--duration <Nh|Nm|Ns>] [--keep-alive]"
              echo "  --duration   how long to run before the summary (default 1h)"
              echo "  --keep-alive don't power off after the summary — leave the VM"
              echo "               up so you can inspect MinIO ('mc ls local/xtcp2-records')"
              echo "               via the serial console. Ctrl-C to terminate."
              exit 0 ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        if [ "$DURATION_SEC" -lt 60 ]; then
          echo "FATAL: --duration under 60s leaves no time for the stack to boot" >&2
          exit 1
        fi

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-s3parquet-stress-XXXX.log)

        echo "================================================"
        echo " xtcp2 s3parquet-stress — arch=${arch}"
        echo " duration:   ''${DURATION_SEC}s"
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

        # Latest upload sentinel, read cheaply from the tail of the transcript.
        latest_s3() {
          tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_S3PARQUET_HOURLY' || true
        }

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
            line=$(latest_s3)
            files=$(printf '%s' "$line" | grep -oE 'files=[0-9]+' | cut -d= -f2 || true)
            bytes=$(printf '%s' "$line" | grep -oE 'bytes=[0-9]+' | cut -d= -f2 || true)
            rows=$(printf '%s' "$line" | grep -oE 'rows=[0-9]+' | cut -d= -f2 || true)
            disk=$(printf '%s' "$line" | grep -oE 'disk=[0-9]+' | cut -d= -f2 || true)
            pnc=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
            echo "  [t=$(printf %6d "$elapsed")s/$DURATION_SEC] parquet files=''${files:-?} rows=''${rows:-?} bytes=''${bytes:-?} disk=''${disk:-?}% panics=''${pnc:-0}"
          fi
        done

        echo ""
        echo "================================================"
        echo " s3parquet-stress summary"
        echo "================================================"

        spawned=$(grep -cE 'stress-[0-9]+: started' "$LOG" 2>/dev/null || true)
        cfailed=$(grep -cE 'stress-[0-9]+: FAILED' "$LOG" 2>/dev/null || true)
        panics=$(grep -cE 'panic:|fatal error:' "$LOG" 2>/dev/null || true)
        # xtcp2 restart signal (a crash-loop would otherwise still upload
        # some files and look like a pass).
        restarts=$(grep -cE 'xtcp2\.service: Main process exited|xtcp2\.service: Start request repeated' "$LOG" 2>/dev/null || true)

        first_line=$(grep -am1 'XTCP2_S3PARQUET_HOURLY' "$LOG" 2>/dev/null || true)
        last_line=$(latest_s3)
        files_first=$(printf '%s' "$first_line" | grep -oE 'files=[0-9]+' | cut -d= -f2 || true)
        files_last=$(printf '%s' "$last_line" | grep -oE 'files=[0-9]+' | cut -d= -f2 || true)
        rows_last=$(printf '%s' "$last_line" | grep -oE 'rows=[0-9]+' | cut -d= -f2 || true)
        bytes_last=$(printf '%s' "$last_line" | grep -oE 'bytes=[0-9]+' | cut -d= -f2 || true)
        disk_last=$(printf '%s' "$last_line" | grep -oE 'disk=[0-9]+' | cut -d= -f2 || true)
        # Peak MinIO disk across the whole run (not just the last sample) — a
        # saturation event can recover after the retention sweep yet still have
        # stalled uploads earlier, so assert on the max.
        disk_peak=$(grep -oE 'disk=[0-9]+' "$LOG" 2>/dev/null | cut -d= -f2 | sort -n | tail -1 || true)
        files_first=''${files_first:-0}
        files_last=''${files_last:-0}
        rows_last=''${rows_last:-0}
        bytes_last=''${bytes_last:-0}
        disk_last=''${disk_last:-0}
        disk_peak=''${disk_peak:-0}

        echo "  stress containers spawned:     $spawned"
        echo "  stress containers FAILED:      $cfailed"
        echo "  parquet files first -> last:   $files_first -> $files_last"
        echo "  rows uploaded (last sample):   $rows_last"
        echo "  bytes uploaded (last sample):  $bytes_last"
        echo "  minio disk % (last / peak):    $disk_last / $disk_peak"
        echo "  xtcp2 restarts:                $restarts"
        echo "  panics in transcript:          $panics"

        # RSS/thread trend (leak check) — first/last XTCP2_RES_SNAPSHOT.
        first_res=$(grep -am1 'XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null || true)
        last_res=$(tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_RES_SNAPSHOT' || true)
        if [ -n "$first_res" ] && [ -n "$last_res" ]; then
          rss0=$(printf '%s' "$first_res" | grep -oE '"rss_kb":[0-9]+' | cut -d: -f2 || echo 0)
          rss1=$(printf '%s' "$last_res"  | grep -oE '"rss_kb":[0-9]+' | cut -d: -f2 || echo 0)
          thr0=$(printf '%s' "$first_res" | grep -oE '"threads":[0-9]+' | cut -d: -f2 || echo 0)
          thr1=$(printf '%s' "$last_res"  | grep -oE '"threads":[0-9]+' | cut -d: -f2 || echo 0)
          echo "  rss_kb trend:                  ''${rss0:-?} -> ''${rss1:-?}"
          echo "  threads trend:                 ''${thr0:-?} -> ''${thr1:-?}"
        fi
        echo ""

        rc=0
        [ "$spawned" -lt 1 ] && { echo "FAIL: no stress containers spawned"; rc=1; }
        [ "$files_last" -lt 1 ] && { echo "FAIL: 0 parquet files uploaded — nothing reached MinIO"; rc=1; }
        [ "$files_last" -le "$files_first" ] && { echo "FAIL: parquet uploads did not advance ($files_first -> $files_last files) — uploads stalled"; rc=1; }
        [ "$restarts" -ne 0 ] && { echo "FAIL: $restarts xtcp2 restart(s) — daemon did not stay up"; rc=1; }
        # Disk saturation on /var/lib/minio back-pressures uploads and stalls
        # the producer. FAIL hard at >=95%; WARN early at >=85%.
        [ "$disk_peak" -ge 95 ] && { echo "FAIL: minio disk hit ''${disk_peak}% — /var/lib/minio saturated, uploads back-pressured (grow the volume or shorten the retention window)"; rc=1; }
        [ "$disk_peak" -ge 85 ] && [ "$disk_peak" -lt 95 ] && echo "WARN: minio disk peaked at ''${disk_peak}% — approaching saturation; a longer soak may stall"
        [ "$panics" -ne 0 ] && { echo "FAIL: $panics panic(s) in transcript"; rc=1; }

        if [ "$rc" -eq 0 ]; then
          echo "PASS: xtcp2 uploaded $files_last parquet files ($rows_last rows) to MinIO over ''${DURATION_SEC}s under $spawned stress containers, 0 restarts, 0 panics"
        fi
        echo ""
        echo "Full transcript kept at: $LOG"

        if [ "$KEEP_ALIVE" -eq 1 ]; then
          echo ""
          echo "================================================"
          echo " --keep-alive: VM is still running."
          echo "   Serial console: nc 127.0.0.1 $SERIAL_PORT"
          echo "   MinIO API on the host: http://127.0.0.1:9000 (mc alias set …)"
          echo "   Pyroscope UI on the host: http://127.0.0.1:14040"
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
  # Host runner for the discovery-bench flavor. Boots the VM, taps the serial
  # console, waits for the in-VM discovery-bench-run service to emit its
  # DISCOBENCH_DONE sentinel (with a bounded timeout), prints the collected
  # DISCOBENCH_GRID JSON lines, powers the VM off, and exits 0 iff the grid
  # completed without a DISCOBENCH_ERR. This is a finite benchmark, not a soak.
  mkDiscoveryBenchRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-discovery-bench-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        TIMEOUT_SEC=1200
        while [ $# -gt 0 ]; do
          case "$1" in
            --timeout)   TIMEOUT_SEC="$2"; shift 2 ;;
            --timeout=*) TIMEOUT_SEC="''${1#--timeout=}"; shift ;;
            -h|--help)
              echo "usage: $0 [--timeout <seconds>]"
              echo "  Boots the discovery-bench microvm, runs the namespace-"
              echo "  discovery A/B grid (dir-scan vs /proc-scan) against a real"
              echo "  kernel, prints the per-cell JSON, then powers off."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-discovery-bench-XXXX.log)

        echo "================================================"
        echo " xtcp2 microvm discovery-bench — arch=${arch}"
        echo " timeout: $TIMEOUT_SEC s"
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

        # Wait for the grid to finish (DISCOBENCH_DONE) or the timeout.
        elapsed=0
        done_seen=0
        while [ "$elapsed" -lt "$TIMEOUT_SEC" ]; do
          if ! kill -0 "$vm_pid" 2>/dev/null; then
            echo "FATAL: qemu died at t=$elapsed s; tail of transcript:"
            tail -n 40 "$LOG"
            exit 2
          fi
          if grep -q 'DISCOBENCH_DONE' "$LOG" 2>/dev/null; then
            done_seen=1
            break
          fi
          sleep 5
          elapsed=$((elapsed + 5))
        done

        if [ "$done_seen" -ne 1 ]; then
          echo "FATAL: DISCOBENCH_DONE not seen within $TIMEOUT_SEC s"
          tail -n 40 "$LOG" 2>/dev/null || true
          exit 2
        fi

        echo ""
        echo "================================================"
        echo " discovery-bench results (per grid cell)"
        echo "================================================"
        grep -E 'DISCOBENCH_START|DISCOBENCH_GRID|DISCOBENCH_ERR' "$LOG" 2>/dev/null || true

        rc=0
        if grep -q 'DISCOBENCH_ERR' "$LOG" 2>/dev/null; then
          echo "FAIL: at least one grid cell reported DISCOBENCH_ERR"
          rc=1
        else
          cells=$(grep -cE 'DISCOBENCH_GRID' "$LOG" 2>/dev/null || true)
          echo "PASS: grid completed — $cells cell(s) measured"
        fi
        echo ""
        echo "Full transcript kept at: $LOG"
        exit "$rc"
      '';
    };

  # mkNlmonCaptureRunner — host runner for the nlmon-capture flavor.
  #
  # Same shape as mkDiscoveryBenchRunner (boot, tail both consoles, wait for a
  # DONE sentinel or --timeout, power off), plus the coverage extractor's
  # base64 scrape: the guest tars the pcap + sidecars between
  # XTCP2_NLCAP_DUMP_START/_END markers and this unpacks them into the working
  # tree so the fixtures can be committed.
  #
  # This is a runner (app), not a check, and it has to be: a nix check's
  # $TMPDIR is private, its source is a read-only store copy, and its result is
  # binary-cached — you would get a stale pcap and no way to write the real one
  # into pkg/xtcpnl/testdata/. It also needs /dev/kvm, which the sandbox lacks.
  mkNlmonCaptureRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-nlmon-capture-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gawk
        gnugrep
        gnused
        gnutar
        gzip
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        TIMEOUT_SEC=600
        OUT_DIR=""
        while [ $# -gt 0 ]; do
          case "$1" in
            --timeout)   TIMEOUT_SEC="$2"; shift 2 ;;
            --timeout=*) TIMEOUT_SEC="''${1#--timeout=}"; shift ;;
            --out)       OUT_DIR="$2"; shift 2 ;;
            --out=*)     OUT_DIR="''${1#--out=}"; shift ;;
            -h|--help)
              echo "usage: $0 [--timeout <seconds>] [--out <dir>]"
              echo "  Boots the nlmon-capture microvm, triggers a scripted"
              echo "  sequence of real kernel network events (link up/down,"
              echo "  addr add/del, route add/del, neigh add/del), captures"
              echo "  them off an nlmon device, extracts the pcap + sidecars,"
              echo "  then powers off."
              echo ""
              echo "  --out defaults to pkg/xtcpnl/testdata/<guest kernel>,"
              echo "  e.g. pkg/xtcpnl/testdata/7_1_8, and must be run from the"
              echo "  xtcp2 repo root."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        # Same repo-root guard as nix/capture-netlink-fixtures.nix: the default
        # output path is relative, so running from anywhere else would scatter
        # a pkg/ tree into the current directory.
        if [ ! -f flake.nix ] || [ ! -d pkg/xtcpnl ]; then
          echo "nlmon-capture: run from the xtcp2 repo root" >&2
          exit 2
        fi

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-nlmon-capture-XXXX.log)

        echo "================================================"
        echo " xtcp2 microvm nlmon-capture — arch=${arch}"
        echo " timeout: $TIMEOUT_SEC s"
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

        elapsed=0
        done_seen=0
        while [ "$elapsed" -lt "$TIMEOUT_SEC" ]; do
          if ! kill -0 "$vm_pid" 2>/dev/null; then
            echo "FATAL: qemu died at t=$elapsed s; tail of transcript:"
            tail -n 40 "$LOG"
            exit 2
          fi
          if grep -q 'NLCAP_DONE' "$LOG" 2>/dev/null; then
            done_seen=1
            break
          fi
          if grep -q 'NLCAP_ERR' "$LOG" 2>/dev/null; then
            echo "FATAL: guest reported NLCAP_ERR:"
            grep -E 'NLCAP_ERR' "$LOG" || true
            exit 2
          fi
          sleep 5
          elapsed=$((elapsed + 5))
        done

        if [ "$done_seen" -ne 1 ]; then
          echo "FATAL: NLCAP_DONE not seen within $TIMEOUT_SEC s"
          tail -n 40 "$LOG" 2>/dev/null || true
          exit 2
        fi

        echo ""
        echo "================================================"
        echo " trigger sequence (as executed in the guest)"
        echo "================================================"
        grep -E 'NLCAP_PHASE|NLCAP_RUN_FAIL|NLCAP_PACKETS' "$LOG" 2>/dev/null || true

        # Extract the tar|gzip|base64 blob. systemd routes the unit's
        # StandardOutput=journal+console, which prefixes every line with
        # `[TIME] <identifier>[PID]: `; strip that before decoding. The regex
        # is deliberately generic rather than hard-coding the unit name — a
        # base64 line can never begin with `[`, so it cannot over-match.
        STAGE=$(mktemp -d -t xtcp2-nlcap-XXXX)
        if grep -q 'XTCP2_NLCAP_DUMP_START' "$LOG" \
          && grep -q 'XTCP2_NLCAP_DUMP_END' "$LOG"; then
          awk '/XTCP2_NLCAP_DUMP_START/{flag=1;next} /XTCP2_NLCAP_DUMP_END/{flag=0} flag' "$LOG" \
            | sed -E 's/^\[[^]]*\] [A-Za-z0-9_.@-]+\[[0-9]+\]: //' \
            | tr -d '\r\n ' \
            | base64 -d 2>/dev/null \
            | gzip -dc 2>/dev/null \
            | tar x -C "$STAGE" 2>/dev/null || true
        else
          echo "FATAL: no XTCP2_NLCAP_DUMP block in the transcript"
          exit 2
        fi

        if [ ! -s "$STAGE/netlink_route_events.pcap" ]; then
          echo "FATAL: extracted blob has no netlink_route_events.pcap"
          echo "       staged files:"
          ls -la "$STAGE" || true
          exit 2
        fi

        # Version the output by the GUEST kernel, not the host's — the fixture
        # documents the kernel that produced it. The guest's `uname -a` sidecar
        # is the only reliable source for that.
        if [ -z "$OUT_DIR" ]; then
          VER=$(awk '{print $3}' "$STAGE/uname" | cut -d- -f1 | tr . _)
          if [ -z "$VER" ]; then
            echo "FATAL: could not derive a kernel version from the uname sidecar"
            exit 2
          fi
          OUT_DIR="pkg/xtcpnl/testdata/$VER"
        fi

        mkdir -p "$OUT_DIR"
        cp -f "$STAGE"/* "$OUT_DIR"/

        echo ""
        echo "================================================"
        echo " wrote fixtures to $OUT_DIR"
        echo "================================================"
        ls -la "$OUT_DIR"
        echo ""
        echo "Full transcript kept at: $LOG"
        echo "PASS: rtnetlink event capture complete"
        exit 0
      '';
    };

  # mkNetlinkDumpCaptureRunner — host runner for the netlink-dump-capture
  # flavor, and the one runner in this file that does not scrape a transcript.
  #
  # The other five boot a VM whose work is a baked-in systemd oneshot, tail the
  # console into a file, and poll that file with grep for a sentinel. This one
  # DRIVES the guest instead: scripts/capture-netlink-dumps.exp attaches to the
  # serial console with expect, runs the capture one command at a time, and
  # gets each command's real exit status back. That is what lets a capture that
  # came in under its datagram floor be reported as such rather than silently
  # written, and it is why there are no `sleep`s below.
  #
  # CONSEQUENCE, because it is easy to trip over: the driver OWNS the serial
  # port. qemu's chardev is `tcp:...,server,nowait`, which serves one
  # connection at a time, so this runner must not `nc` that port the way the
  # others do - the two would fight over the console. Diagnostics come off the
  # virtio console instead, which nothing else is using.
  #
  # A runner and not a check, for mkNlmonCaptureRunner's reasons (no /dev/kvm
  # in the sandbox, binary-cached results, a read-only source copy) plus one of
  # its own: the point of the run is to write files into the working tree.
  mkNetlinkDumpCaptureRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};

      # All three .exp files in one store directory, because the driver locates
      # its library and its topology with
      # `source [file dirname [info script]]/...`. Copying them individually
      # into the store would put them in three different directories and those
      # sources would fail.
      #
      # netlink-topology.exp is shared with the goip-parity driver so both
      # capture off one topology definition; see its header.
      captureScripts =
        pkgs.runCommand "xtcp2-netlink-capture-scripts"
          {
            vmLib = ./scripts/vm-lib.exp;
            topology = ./scripts/netlink-topology.exp;
            driver = ./scripts/capture-netlink-dumps.exp;
          }
          ''
            mkdir -p $out
            cp $vmLib $out/vm-lib.exp
            cp $topology $out/netlink-topology.exp
            cp $driver $out/capture-netlink-dumps.exp
            chmod +x $out/capture-netlink-dumps.exp
          '';
    in
    pkgs.writeShellApplication {
      name = "xtcp2-netlink-dump-capture-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        expect
        findutils
        gawk
        gnutar
        gzip
        netcat-gnu
      ];
      text = ''
        set -u

        # 1800 s. The capture itself is a couple of minutes; the budget is
        # dominated by a cold microvm boot on a loaded host, and by the base64
        # blob, which expect reads one line at a time.
        TIMEOUT_SEC=1800
        OUT_DIR=""
        # An array rather than a string, so the unset case passes NO argument
        # instead of an empty one. `expect $SKIP_MESH` unquoted would have done
        # that too, and would have been a shellcheck finding to suppress.
        SKIP_MESH=()
        SKIP_TUNNEL=()
        while [ $# -gt 0 ]; do
          case "$1" in
            --timeout)     TIMEOUT_SEC="$2"; shift 2 ;;
            --timeout=*)   TIMEOUT_SEC="''${1#--timeout=}"; shift ;;
            --out)         OUT_DIR="$2"; shift 2 ;;
            --out=*)       OUT_DIR="''${1#--out=}"; shift ;;
            --skip-mesh)   SKIP_MESH=(--skip-mesh); shift ;;
            --skip-tunnel) SKIP_TUNNEL=(--skip-tunnel); shift ;;
            -h|--help)
              echo "usage: $0 [--timeout <seconds>] [--out <dir>] [--skip-mesh]"
              echo "          [--skip-tunnel]"
              echo "  Boots the netlink-dump-capture microvm and drives it over"
              echo "  the serial console with expect: builds a clean topology in"
              echo "  a throwaway netns, captures each RTM_GET* dump off an"
              echo "  nlmon device in that namespace, records the ip -d / ip -j"
              echo "  sidecars those fixtures are checked against, then extracts"
              echo "  the lot and powers off."
              echo ""
              echo "  --skip-mesh omits the second, advisory capture set (bridge"
              echo "  + veth). That set is the only source of real IFLA_MASTER /"
              echo "  IFLA_LINKINFO replies, so skipping it is for iterating on"
              echo "  the clean set, not for a capture you intend to commit."
              echo ""
              echo "  --skip-tunnel omits the third capture set (ipip, sit, gre,"
              echo "  ip6tnl, ip6gre). That set is the only source of the ARPHRD"
              echo "  types ll_addr_n2a renders as addresses rather than hex, so"
              echo "  the same caveat applies."
              echo ""
              echo "  --out defaults to pkg/xtcpnl/testdata/<guest kernel>/dumps,"
              echo "  with the kernel derived from the guest's own uname"
              echo "  sidecar. Must be run from the xtcp2 repo root."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        # Same repo-root guard as the other capture paths: the default output
        # is a relative path, so running from elsewhere would scatter a pkg/
        # tree into the current directory.
        if [ ! -f flake.nix ] || [ ! -d pkg/xtcpnl ]; then
          echo "netlink-dump-capture: run from the xtcp2 repo root" >&2
          exit 2
        fi

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-netlink-dump-capture-XXXX.log)
        BLOB=$(mktemp -t xtcp2-netlink-dump-blob-XXXX.b64)

        echo "================================================"
        echo " xtcp2 microvm netlink-dump-capture — arch=${arch}"
        echo " timeout: $TIMEOUT_SEC s"
        echo " transcript: $LOG"
        echo "================================================"

        QEMU_LOG="''${LOG}.qemu"
        ${vm}/bin/microvm-run > "$QEMU_LOG" 2>&1 &
        vm_pid=$!

        # Virtio console only — the serial console belongs to the driver (see
        # the header). This is pure diagnostics: if the driver reports that the
        # guest never produced a shell, the boot messages are in here.
        nc_virtcon_pid=""
        for _ in $(seq 1 30); do
          if nc -z 127.0.0.1 "$VIRTCON_PORT" 2>/dev/null; then
            nc 127.0.0.1 "$VIRTCON_PORT" >> "$QEMU_LOG" 2>&1 &
            nc_virtcon_pid=$!
            break
          fi
          sleep 1
        done

        # The driver powers the guest off itself on every exit path, so this
        # only has to deal with the case where it did not get that far.
        trap '
          if kill -0 "$vm_pid" 2>/dev/null; then
            kill "$vm_pid" 2>/dev/null || true
            wait "$vm_pid" 2>/dev/null || true
          fi
          if [ -n "$nc_virtcon_pid" ] && kill -0 "$nc_virtcon_pid" 2>/dev/null; then
            kill "$nc_virtcon_pid" 2>/dev/null || true
          fi
        ' EXIT

        # No readiness probe on the serial port: vmlib::connect retries the
        # open itself, precisely so nothing out here has to connect to a
        # single-connection chardev just to find out whether it is up.
        #
        # `|| rc=$?` under pipefail yields expect's own status rather than
        # tee's, which is the whole reason for reading it this way: the
        # driver's exit codes are meaningful (1 = a capture missed its floor,
        # 2 = no console, 3 = a step that has no partial result failed).
        rc=0
        timeout "$TIMEOUT_SEC" \
          expect ${captureScripts}/capture-netlink-dumps.exp \
            "$SERIAL_PORT" "$BLOB" ''${SKIP_MESH[@]+"''${SKIP_MESH[@]}"} \
            ''${SKIP_TUNNEL[@]+"''${SKIP_TUNNEL[@]}"} 2>&1 \
          | tee "$LOG" || rc=$?

        if [ "$rc" -eq 124 ]; then
          echo "FATAL: the capture driver did not finish within $TIMEOUT_SEC s"
          echo "       Guest boot log: $QEMU_LOG"
          exit 2
        fi

        if [ ! -s "$BLOB" ]; then
          echo "FATAL: the driver wrote no capture blob (exit $rc)"
          echo "       Driver transcript: $LOG"
          echo "       Guest boot log:    $QEMU_LOG"
          exit 2
        fi

        # The sentinels still have to be cut. `xtcp2-nlcap pack` frames its
        # output with XTCP2_NLCAP_DUMP_START/_END because its other consumer
        # scrapes a raw console transcript and needs to find the blob in it;
        # vmlib::run_out hands back that command's output verbatim, sentinels
        # included, and base64 has no comment syntax. Unlike the nlmon runner
        # there is no systemd `[TIME] unit[PID]: ` prefix to strip as well:
        # run_out has already removed the echo, the CRs and the ANSI escapes.
        STAGE=$(mktemp -d -t xtcp2-nlcap-dumps-XXXX)
        if ! awk '
              /XTCP2_NLCAP_DUMP_START/ { flag = 1; next }
              /XTCP2_NLCAP_DUMP_END/   { flag = 0 }
              flag
            ' "$BLOB" \
          | tr -d '\r\n ' \
          | base64 -d 2>/dev/null \
          | gzip -dc 2>/dev/null \
          | tar x -C "$STAGE" 2>/dev/null; then
          echo "FATAL: the capture blob did not decode"
          echo "       $(wc -c < "$BLOB") bytes of base64 kept at $BLOB"
          exit 2
        fi

        # Two required files, chosen because between them they prove both
        # halves arrived: a pcap means the capture ran, and the ip_version
        # sidecar is what nix/upstream-pins.json's iproute2 entry is checked
        # against, so a fixture set without it is not attributable.
        for required in netlink_route_getlink.pcap ip_version; do
          if [ ! -s "$STAGE/$required" ]; then
            echo "FATAL: extracted blob has no $required"
            echo "       staged files:"
            find "$STAGE" -type f | sort || true
            exit 2
          fi
        done

        # Version the output by the GUEST kernel, not the host's. These
        # fixtures document the kernel that produced them, and the guest runs
        # pkgs.linuxPackages_latest, which is routinely a different version
        # from whatever the host booted.
        #
        # A `dumps/` SUBDIRECTORY, not the kernel directory itself, because the
        # nlmon event capture writes `ip_link_n`, `ip_addr_n`, `ip_neigh_n`,
        # `ip_route_table_all_n` and `uname` into that same directory. Those
        # five names mean something different in each set - the event capture's
        # describe a veth pair, this one's describe the dummy-only clean
        # namespace - and both are cited by line number from Go tests. Writing
        # here directly would silently repoint the event tests' citations at
        # this topology, which is a wrong-answer failure rather than a missing
        # file. Kept separate now that the two sets come from one guest kernel
        # and so no longer land in different directories by accident.
        if [ -z "$OUT_DIR" ]; then
          VER=$(awk '{print $3}' "$STAGE/uname" | cut -d- -f1 | tr . _)
          if [ -z "$VER" ]; then
            echo "FATAL: could not derive a kernel version from the uname sidecar"
            exit 2
          fi
          OUT_DIR="pkg/xtcpnl/testdata/$VER/dumps"
        fi

        mkdir -p "$OUT_DIR"
        cp -rf "$STAGE"/. "$OUT_DIR"/

        echo ""
        echo "================================================"
        echo " wrote fixtures to $OUT_DIR"
        echo "================================================"
        find "$OUT_DIR" -type f | sort
        echo ""
        echo "Driver transcript: $LOG"
        echo "Guest boot log:    $QEMU_LOG"

        if [ "$rc" -ne 0 ]; then
          echo ""
          echo "FAIL: the driver exited $rc. Fixtures above were still"
          echo "      installed - a capture that missed its floor is DISCARDED"
          echo "      in the guest rather than written, so whatever is on disk"
          echo "      for that name is unchanged. Check NLCAP_MISSED_FLOOR and"
          echo "      NLCAP_TOPO_REFUSED in the transcript before committing."
          exit "$rc"
        fi

        echo "PASS: rtnetlink dump capture complete"
        exit 0
      '';
    };

  # mkGoipParityRunner — host runner for the goip-parity flavor, Tier C of the
  # netlink parity harness.
  #
  # Shaped after mkNetlinkDumpCaptureRunner rather than the five transcript
  # scrapers, for the same reason: the guest is DRIVEN over the serial console
  # by an expect script, so the driver owns that port and this must not `nc`
  # it. Diagnostics come off the virtio console, which nothing else uses.
  #
  # ONE DIFFERENCE FROM ITS SIBLING, AND IT IS THE IMPORTANT ONE
  #
  # The dump-capture runner exists to write files into the working tree, so it
  # installs its blob and treats a non-zero driver status as advice about what
  # not to commit. This one exists to produce a VERDICT. The comparison already
  # happened in the guest - the captures never have to leave for the answer to
  # exist - so the blob here is evidence, and a run whose blob failed to decode
  # still reports the verdict it measured. Getting that backwards would let an
  # exfil problem mask a parity failure, or worse, report one that did not
  # happen.
  #
  # Not a check, for mkNlmonCaptureRunner's reasons (no /dev/kvm in the nix
  # sandbox, binary-cached results) plus the fixed SERIAL_PORT, which means it
  # cannot run concurrently with any other VM and so belongs in the sequential
  # integration list rather than in `nix flake check`.
  mkGoipParityRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};

      # All three .exp files in one store directory, because each locates its
      # siblings with `source [file dirname [info script]]/...`. Copying them
      # in individually would put them in three different store paths and
      # those sources would fail.
      parityScripts =
        pkgs.runCommand "xtcp2-goip-parity-scripts"
          {
            vmLib = ./scripts/vm-lib.exp;
            topology = ./scripts/netlink-topology.exp;
            driver = ./scripts/goip-parity.exp;
          }
          ''
            mkdir -p $out
            cp $vmLib $out/vm-lib.exp
            cp $topology $out/netlink-topology.exp
            cp $driver $out/goip-parity.exp
            chmod +x $out/goip-parity.exp
          '';
    in
    pkgs.writeShellApplication {
      name = "xtcp2-goip-parity-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        expect
        findutils
        gawk
        gnugrep
        gnutar
        gzip
        netcat-gnu
      ];
      text = ''
        set -u

        # 1800 s. The comparison itself is milliseconds and the captures are a
        # couple of minutes; the budget is dominated by a cold microvm boot on
        # a loaded host, and by the base64 blob, which expect reads one line
        # at a time.
        TIMEOUT_SEC=1800
        OUT_DIR=""
        # Arrays rather than strings, so the unset case passes NO argument
        # instead of an empty one.
        KEEP_GOING=()
        NO_ALLOWLIST=()
        while [ $# -gt 0 ]; do
          case "$1" in
            --timeout)   TIMEOUT_SEC="$2"; shift 2 ;;
            --timeout=*) TIMEOUT_SEC="''${1#--timeout=}"; shift ;;
            --out)       OUT_DIR="$2"; shift 2 ;;
            --out=*)     OUT_DIR="''${1#--out=}"; shift ;;
            --keep-going)   KEEP_GOING=(--keep-going); shift ;;
            --no-allowlist) NO_ALLOWLIST=(--no-allowlist); shift ;;
            -h|--help)
              echo "usage: $0 [--timeout <sec>] [--out <dir>] [--keep-going] [--no-allowlist]"
              echo "  Boots the goip-parity microvm and drives it over the serial"
              echo "  console with expect: builds the dummy-only topology in a"
              echo "  throwaway netns, captures an ip/goip/ip triple per command"
              echo "  off an nlmon device in that namespace, and runs"
              echo "  \`goip-parity compare\` IN THE GUEST."
              echo ""
              echo "  The verdict is the comparator's. This runner surfaces the"
              echo "  GOIP_PARITY_* sentinels and exits non-zero if the run did"
              echo "  not reach GOIP_PARITY_OVERALL_PASS."
              echo ""
              echo "  --keep-going captures every command even after one misses"
              echo "  its datagram floor, which is for diagnosing a bad capture"
              echo "  rather than for a run you intend to trust."
              echo ""
              echo "  --no-allowlist reports every accepted divergence instead"
              echo "  of suppressing it. Use it to review what the allowlist is"
              echo "  currently hiding."
              echo ""
              echo "  --out saves the capture triples + the in-guest report to a"
              echo "  directory. Optional: unlike the dump-capture runner these"
              echo "  are evidence, not fixtures, so nothing is written by"
              echo "  default and no repo root is required."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-goip-parity-XXXX.log)
        BLOB=$(mktemp -t xtcp2-goip-parity-blob-XXXX.b64)

        echo "================================================"
        echo " xtcp2 microvm goip-parity — arch=${arch}"
        echo " timeout: $TIMEOUT_SEC s"
        echo " transcript: $LOG"
        echo "================================================"

        QEMU_LOG="''${LOG}.qemu"
        ${vm}/bin/microvm-run > "$QEMU_LOG" 2>&1 &
        vm_pid=$!

        # Virtio console only — the serial console belongs to the driver. Pure
        # diagnostics: if the driver reports the guest never produced a shell,
        # the boot messages are in here.
        nc_virtcon_pid=""
        for _ in $(seq 1 30); do
          if nc -z 127.0.0.1 "$VIRTCON_PORT" 2>/dev/null; then
            nc 127.0.0.1 "$VIRTCON_PORT" >> "$QEMU_LOG" 2>&1 &
            nc_virtcon_pid=$!
            break
          fi
          sleep 1
        done

        trap '
          if kill -0 "$vm_pid" 2>/dev/null; then
            kill "$vm_pid" 2>/dev/null || true
            wait "$vm_pid" 2>/dev/null || true
          fi
          if [ -n "$nc_virtcon_pid" ] && kill -0 "$nc_virtcon_pid" 2>/dev/null; then
            kill "$nc_virtcon_pid" 2>/dev/null || true
          fi
        ' EXIT

        # `|| rc=$?` under pipefail yields expect's own status rather than
        # tee's. The driver's codes are meaningful: 1 = the comparator found
        # something, 2 = no console, 3 = a step with no partial result failed.
        rc=0
        timeout "$TIMEOUT_SEC" \
          expect ${parityScripts}/goip-parity.exp \
            "$SERIAL_PORT" "$BLOB" \
            ''${KEEP_GOING[@]+"''${KEEP_GOING[@]}"} \
            ''${NO_ALLOWLIST[@]+"''${NO_ALLOWLIST[@]}"} 2>&1 \
          | tee "$LOG" || rc=$?

        if [ "$rc" -eq 124 ]; then
          echo "FATAL: the parity driver did not finish within $TIMEOUT_SEC s"
          echo "       Guest boot log: $QEMU_LOG"
          exit 2
        fi

        # The blob is optional, so its absence is a warning rather than the
        # fatal it is in the dump-capture runner: the verdict was decided in
        # the guest and does not depend on the exfil path. Installed BEFORE
        # the verdict is read, so a failing run still leaves its evidence on
        # disk.
        if [ -n "$OUT_DIR" ]; then
          if [ ! -s "$BLOB" ]; then
            echo "WARNING: the driver wrote no capture blob; --out has nothing to install"
          else
            STAGE=$(mktemp -d -t xtcp2-goip-parity-XXXX)
            if awk '
                  /XTCP2_NLCAP_DUMP_START/ { flag = 1; next }
                  /XTCP2_NLCAP_DUMP_END/   { flag = 0 }
                  flag
                ' "$BLOB" \
              | tr -d '\r\n ' \
              | base64 -d 2>/dev/null \
              | gzip -dc 2>/dev/null \
              | tar x -C "$STAGE" 2>/dev/null; then
              mkdir -p "$OUT_DIR"
              cp -rf "$STAGE"/. "$OUT_DIR"/
              echo ""
              echo "capture triples + in-guest report installed to $OUT_DIR"
              find "$OUT_DIR" -type f | sort
            else
              echo "WARNING: the capture blob did not decode"
              echo "         $(wc -c < "$BLOB") bytes of base64 kept at $BLOB"
            fi
          fi
        fi

        echo ""
        echo "================================================"
        echo " verdict"
        echo "================================================"
        # Re-printed from the transcript rather than tracked through the shell,
        # so what is summarized here is exactly what the comparator said.
        grep -E '^GOIP_PARITY_(OVERALL|HYGIENE|CONTROL|UNGATED|NOTHING)' "$LOG" || true
        echo ""
        echo "Driver transcript: $LOG"
        echo "Guest boot log:    $QEMU_LOG"

        # OVERALL_PASS is required POSITIVELY, not inferred from the exit
        # status. A driver that died before comparing exits non-zero and would
        # be caught either way, but a driver that somehow exited 0 without
        # comparing anything must not read as a pass - which is the same
        # reasoning behind the comparator's own GOIP_PARITY_NOTHING_COMPARED.
        if ! grep -q '^GOIP_PARITY_OVERALL_PASS$' "$LOG"; then
          echo ""
          echo "FAIL: the run did not reach GOIP_PARITY_OVERALL_PASS (driver exit $rc)"
          if [ "$rc" -eq 0 ]; then
            echo "      The driver exited 0 without an OVERALL_PASS, which means it"
            echo "      never got as far as comparing. Check the transcript."
          fi
          exit 1
        fi

        if [ "$rc" -ne 0 ]; then
          echo ""
          echo "FAIL: OVERALL_PASS was printed but the driver exited $rc."
          echo "      Those disagree, so neither is trusted. Check the transcript."
          exit 1
        fi

        echo ""
        echo "PASS: goip netlink + stdout parity holds on this kernel"
        exit 0
      '';
    };

  # mkClickPipeRateRunner — host runner for the clickhouse-pipeline-rate flavor.
  # Mirrors mkDiscoveryBenchRunner (boot, tail both consoles, wait for a DONE
  # sentinel or --timeout, power off). The in-VM xtcp2-clickpipe-rate monitor
  # drives xtcp2ctl through a poll-frequency schedule and emits XTCP2_RATE_*
  # sentinels; this runner surfaces the per-phase data lines and passes only if
  # all three verdicts (INCREASE / REVERT / BURST) are PASS.
  mkClickPipeRateRunner =
    {
      arch,
      vm,
    }:
    let
      cfg = constants.architectures.${arch};
    in
    pkgs.writeShellApplication {
      name = "xtcp2-clickpipe-rate-runner-${arch}";
      runtimeInputs = with pkgs; [
        coreutils
        gnugrep
        netcat-gnu
        procps
      ];
      text = ''
        set -u

        # ~15 min schedule (3×180s windows + settles + burst) + docker/
        # ClickHouse/Redpanda first-boot pulls. Override with --timeout.
        TIMEOUT_SEC=1800
        while [ $# -gt 0 ]; do
          case "$1" in
            --timeout)   TIMEOUT_SEC="$2"; shift 2 ;;
            --timeout=*) TIMEOUT_SEC="''${1#--timeout=}"; shift ;;
            -h|--help)
              echo "usage: $0 [--timeout <seconds>]"
              echo "  Boots the clickhouse-pipeline-rate microvm, which drives"
              echo "  xtcp2ctl through a baseline→fast→revert poll-frequency"
              echo "  schedule + a poll-burst, and asserts the ClickHouse ingest"
              echo "  rate responds. Prints the per-phase XTCP2_RATE_* lines."
              exit 0
              ;;
            *) echo "unknown arg: $1" >&2; exit 1 ;;
          esac
        done

        SERIAL_PORT=${toString cfg.serialPort}
        VIRTCON_PORT=${toString cfg.virtioPort}
        LOG=$(mktemp -t xtcp2-clickpipe-rate-XXXX.log)

        echo "================================================"
        echo " xtcp2 microvm clickhouse-pipeline-rate — arch=${arch}"
        echo " timeout: $TIMEOUT_SEC s"
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

        # Wait for the schedule to finish (XTCP2_RATE_DONE) or the timeout.
        elapsed=0
        done_seen=0
        while [ "$elapsed" -lt "$TIMEOUT_SEC" ]; do
          if ! kill -0 "$vm_pid" 2>/dev/null; then
            echo "FATAL: qemu died at t=$elapsed s; tail of transcript:"
            tail -n 40 "$LOG"
            exit 2
          fi
          if grep -q 'XTCP2_RATE_DONE' "$LOG" 2>/dev/null; then
            done_seen=1
            break
          fi
          sleep 5
          elapsed=$((elapsed + 5))
        done

        if [ "$done_seen" -ne 1 ]; then
          echo "FATAL: XTCP2_RATE_DONE not seen within $TIMEOUT_SEC s"
          tail -n 40 "$LOG" 2>/dev/null || true
          exit 2
        fi

        echo ""
        echo "================================================"
        echo " clickhouse-pipeline-rate results"
        echo "================================================"
        grep -E 'XTCP2_RATE_(START|BASELINE|FAST|REVERT|BURST) ' "$LOG" 2>/dev/null || true
        grep -E 'XTCP2_RATE_(CADENCE|PREDICT|DELIVERY|SOCKETS|BURST|OVERALL)_(PASS|FAIL)' "$LOG" 2>/dev/null || true

        rc=0
        if grep -qE 'XTCP2_RATE_(CADENCE|PREDICT|DELIVERY|SOCKETS|BURST|OVERALL)_FAIL' "$LOG" 2>/dev/null; then
          echo "FAIL: at least one rate verdict failed"
          rc=1
        elif grep -q 'XTCP2_RATE_CADENCE_PASS' "$LOG" 2>/dev/null \
          && grep -q 'XTCP2_RATE_PREDICT_PASS' "$LOG" 2>/dev/null \
          && grep -q 'XTCP2_RATE_DELIVERY_PASS' "$LOG" 2>/dev/null \
          && grep -q 'XTCP2_RATE_SOCKETS_PASS' "$LOG" 2>/dev/null \
          && grep -q 'XTCP2_RATE_BURST_PASS' "$LOG" 2>/dev/null; then
          echo "PASS: predicted ClickHouse rows from the socket estimate matched actual across baseline/fast/revert + burst"
        else
          echo "FAIL: not all rate verdict sentinels present"
          rc=1
        fi
        echo ""
        echo "Full transcript kept at: $LOG"
        exit "$rc"
      '';
    };

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
          if grep -qE 'Prometheus http listener started|XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null; then
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

        # Resource-snapshot trend (XTCP2_RES_SNAPSHOT from the in-VM
        # xtcp2-resource-snapshot service): rss_kb is the memory-growth signal,
        # threads the OS-thread-leak signal. The first sample is cold-start, so
        # the first->last delta always includes ramp-up to steady state (the
        # daemon spawns per-ns netlinkers as it discovers the churn set) — a
        # healthy run ramps then PLATEAUS. The leak tell is continued growth late
        # in the run, so on a suspicious delta check whether rss_kb/threads are
        # still climbing near the end (full per-30s series is in the transcript).
        # Reported, not gated.
        snap_n=$(grep -cE 'XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null || true)
        echo "  res snapshots:    $snap_n"
        if [ "''${snap_n:-0}" -ge 2 ]; then
          # -m1 (stop at first) + tac|-m1 (first from the end) so we never scan
          # the whole transcript, which is hundreds of MB on a multi-hour soak
          # (a plain `grep|tail -1` there is slow enough to race VM teardown and
          # drop these lines from the summary). Guarded so set -e can't abort.
          first=$(grep -am1 'XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null || true)
          last=$(tac "$LOG" 2>/dev/null | grep -am1 'XTCP2_RES_SNAPSHOT' || true)
          rss0=$(printf '%s' "$first" | grep -oE 'rss_kb":[0-9]+' | grep -oE '[0-9]+' | head -1)
          rss1=$(printf '%s' "$last"  | grep -oE 'rss_kb":[0-9]+' | grep -oE '[0-9]+' | head -1)
          thr0=$(printf '%s' "$first" | grep -oE 'threads":[0-9]+' | grep -oE '[0-9]+' | head -1)
          thr1=$(printf '%s' "$last"  | grep -oE 'threads":[0-9]+' | grep -oE '[0-9]+' | head -1)
          echo "  rss_kb trend:     ''${rss0:-?} -> ''${rss1:-?}  (delta $(( ''${rss1:-0} - ''${rss0:-0} )) kb)"
          echo "  threads trend:    ''${thr0:-?} -> ''${thr1:-?}  (delta $(( ''${thr1:-0} - ''${thr0:-0} )))"
        fi

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
          if grep -qE 'Prometheus http listener started|XTCP2_RES_SNAPSHOT' "$LOG" 2>/dev/null; then
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
  #   suffix         (string)  optional name suffix for the wrapper binary
  #   extraSentinels (list)    flavor-specific sentinel tokens surfaced in the
  #                            summary grep on top of baseSentinels + OVERALL.
  #   timeoutSec     (int)     absolute backstop for the scrape, in seconds.
  #   stallSec       (int)     no-progress watchdog: fail if no new sentinel
  #                            appears for this long. This, not timeoutSec, is
  #                            the guard that catches a hang.
  # The self-test sentinels every lifecycle flavor emits (Checks 1-10 +
  # the runtime-control ones). Each flavor's summary grep is
  # `baseSentinels ++ its extraSentinels ++ [ "OVERALL" ]`, so a new
  # baseline check is added in exactly one place instead of being copied
  # into every caller's regex (where a single-token drift silently drops
  # the check from the summary). NB this only affects the DISPLAYED
  # summary — pass/fail keys solely on XTCP2_SELF_TEST_OVERALL_{PASS,FAIL}.
  baseSentinels = [
    "SYSTEMD"
    "METRICS"
    "NETLINK"
    "BINARIES_HELP"
    "GRPC_ROUNDTRIP"
    "POLL_STREAM"
    "HEALTH"
    "OUTPUT_CONTENT"
    "LISTEN_STREAM"
    "NS_INSPECT"
    "NSTEST"
    "NS_LIFECYCLE"
    "NS_TRAFFIC"
    "NS_DOCKER"
    "NS_ANONYMOUS"
    "CTL_HOT"
    "CTL_TRIGGER"
    "CTL_RESTART"
  ];

  mkLifecycleFullTest =
    {
      arch,
      vm,
      suffix ? "",
      # Flavor-specific sentinel tokens surfaced in the summary in addition
      # to baseSentinels (e.g. [ "VALKEY_CONSUME" ]). Order is irrelevant —
      # the tokens become a regex alternation.
      extraSentinels ? [ ],
      # Absolute backstop, in seconds. This is deliberately NOT the primary
      # guard — stallSec is. Keep it high enough that a slow-but-progressing
      # run can never hit it; it exists only to bound the pathological case
      # of a VM that dribbles a sentinel forever without finishing.
      timeoutSec ? 1800,
      # No-progress watchdog, in seconds. The run fails if no NEW
      # XTCP2_SELF_TEST_*_{PASS,FAIL} sentinel appears for this long. This is
      # what actually catches a hang, and it is what makes the runner
      # load-insensitive: a host under heavy load stretches every check's wall
      # clock, but it does not stop progress. Raise it per-flavor for anything
      # with a legitimately long quiet stretch (docker pulls, ClickHouse init).
      #
      # Floor: this MUST exceed the longest single in-guest check's silence,
      # or the watchdog fires mid-check and reports TIMEOUT for what is really
      # a FAIL. The longest budget in self-test.nix is 45 iterations x 3 s =
      # 135 s, multiplied by its waitScale (4) = 540 s. Hence 600.
      #
      # Measured against that floor on this host at load ~46: a full passing
      # run took 561 s of guest time across 18 sentinels, and its largest gap
      # between two sentinels was 187 s (OUTPUT_CONTENT). 600 s leaves ~3.2x
      # headroom over the worst observed quiet stretch.
      stallSec ? 600,
      # When true, after a passing OVERALL sentinel the runner also looks
      # for an XTCP2_COVERAGE_DUMP_START / _END block in the log, decodes
      # it (base64 + gzip + tar), writes the resulting Go coverage data
      # into "$XTCP2_COVERDIR" (env var, defaults to /tmp/xtcp2cov), and
      # logs the file count it extracted. Used by the coverage flavor.
      scrapeCoverage ? false,
      # Absolute path to a persistent host-backed docker disk image
      # (microvm.volumes) that must be discarded before this boot so the
      # guest's ClickHouse re-runs its /docker-entrypoint-initdb.d schema
      # scripts. ClickHouse runs initdb ONLY when its data dir is empty, so a
      # reused disk pins the table to whatever schema the FIRST boot created —
      # any later DDL change (new columns) silently never applies. Lifecycle
      # tests must be deterministic and start from the current schema, so we
      # delete the image here; the long-running soak runner keeps its own disk.
      # null (default) = no reset (flavors without a persistent docker disk).
      resetDockerImg ? null,
    }:
    let
      cfg = constants.architectures.${arch};
      sentinelRe = lib.concatStringsSep "|" (baseSentinels ++ extraSentinels ++ [ "OVERALL" ]);
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
        STALL=${toString stallSec}
        # Seconds to wait for each console TCP port to accept a connection.
        # Generous on purpose: on a loaded host qemu can take far longer than
        # the old hard 30 s to get far enough into boot to open its consoles,
        # and a missed console means an otherwise-healthy run scrapes an empty
        # log and reports a bogus timeout.
        BOOT_WAIT=180
        LOG=$(mktemp -t xtcp2-vm-XXXX.log)

        echo "==> launching microvm (${arch}${suffix}); serial=$SERIAL_PORT virtio-console=$VIRTCON_PORT"
        echo "==> transcript: $LOG"
        ${lib.optionalString (resetDockerImg != null) ''
          # Discard any stale persistent docker disk so ClickHouse re-inits its
          # schema from the current DDL (see resetDockerImg rationale above).
          if [ -e "${resetDockerImg}" ]; then
            echo "==> resetting persistent docker disk: ${resetDockerImg}"
            rm -f "${resetDockerImg}"
          fi
        ''}

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
        for _ in $(seq 1 "$BOOT_WAIT"); do
          if nc -z 127.0.0.1 "$SERIAL_PORT" 2>/dev/null; then
            nc 127.0.0.1 "$SERIAL_PORT" >> "$LOG" 2>&1 &
            nc_serial_pid=$!
            break
          fi
          sleep 1
        done
        for _ in $(seq 1 "$BOOT_WAIT"); do
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

        # Wait for the overall sentinel, for a stall, or for the absolute cap.
        #
        # This used to be a flat wall-clock budget with no notion of progress:
        # a run healthily emitting sentinel after sentinel was killed at
        # exactly the same moment as one that wedged on boot. On a busy host
        # (this one routinely sits at load ~45 on 24 cores, and the guest gets
        # 2 vCPUs) that made the check fail for reasons unrelated to the code
        # under test.
        #
        # Now the stall timer resets every time a NEW self-test sentinel lands.
        # A slow-but-progressing run survives up to the absolute backstop,
        # while a genuine hang still fails in ~STALL seconds instead of burning
        # the whole budget.
        waited=0
        stalled=0
        seen=0
        last_sentinel="(none)"
        rc=2
        while [ "$waited" -lt "$TIMEOUT" ]; do
          if grep -q 'XTCP2_SELF_TEST_OVERALL_PASS' "$LOG"; then
            rc=0; break
          fi
          if grep -q 'XTCP2_SELF_TEST_OVERALL_FAIL' "$LOG"; then
            rc=1; break
          fi

          # Progress probe. grep -c prints 0 but exits 1 when nothing matches,
          # and errexit/pipefail are on (writeShellApplication), hence || true.
          now=$(grep -cE 'XTCP2_SELF_TEST_[A-Z0-9_]+_(PASS|FAIL)' "$LOG" || true)
          if [ "$now" -gt "$seen" ]; then
            seen=$now
            stalled=0
            last_sentinel=$(grep -oE 'XTCP2_SELF_TEST_[A-Z0-9_]+_(PASS|FAIL)' "$LOG" | tail -n 1 || true)
          else
            stalled=$((stalled + 2))
            if [ "$stalled" -ge "$STALL" ]; then
              break
            fi
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
          *)
            # Say WHICH kind of timeout and how far the run actually got. The
            # difference between "this host is slow" and "this wedged in
            # GRPC_ROUNDTRIP" is the whole diagnostic value of this line.
            if [ "$stalled" -ge "$STALL" ]; then
              echo "TIMEOUT: no new sentinel for ''${stalled}s (stall watchdog, limit ''${STALL}s)"
            else
              echo "TIMEOUT: hit the absolute cap of ''${TIMEOUT}s while still making progress" \
                   "— raise timeoutSec for this flavor"
            fi
            echo "         progress: $seen sentinel(s) seen, last was $last_sentinel, elapsed ''${waited}s"
            echo "         last 40 log lines:"
            tail -n 40 "$LOG"
            ;;
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
