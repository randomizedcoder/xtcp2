# nix/microvms/self-test.nix
#
# Self-test script that runs inside the microvm after xtcp2 starts. Performs
# seven independent checks and prints labeled PASS/FAIL sentinels that the
# host launcher (see ./default.nix → fullTest) scrapes from the serial
# console.
#
# Sentinels (each fires exactly once per VM run):
#   XTCP2_SELF_TEST_SYSTEMD_{PASS,FAIL}        systemd unit active
#   XTCP2_SELF_TEST_METRICS_{PASS,FAIL}        /metrics endpoint reachable
#   XTCP2_SELF_TEST_NETLINK_{PASS,FAIL}        netlink readout produces jsonl
#   XTCP2_SELF_TEST_BINARIES_HELP_{PASS,FAIL}  every cmd binary -help works
#   XTCP2_SELF_TEST_GRPC_ROUNDTRIP_{PASS,FAIL} xtcp2 ↔ xtcp2client gRPC works
#   XTCP2_SELF_TEST_NS_INSPECT_{PASS,FAIL}     ns inspector reads netns state
#   XTCP2_SELF_TEST_NSTEST_{PASS,FAIL}         nsTest binary runs
#   XTCP2_SELF_TEST_NS_LIFECYCLE_{PASS,FAIL}   ip netns add/delete propagates to
#                                              xtcp2 (drives the fsnotify watch
#                                              + nsAdd + nsDelete code paths,
#                                              spawning a per-ns netlinker
#                                              goroutine end-to-end)
#   XTCP2_SELF_TEST_NS_TRAFFIC_{PASS,FAIL}     TCP socket created inside a fresh
#                                              netns produces records via that
#                                              ns's netlinker (drives the full
#                                              netlinkerSyscall body + real
#                                              Deserialize on real netlink
#                                              bytes — the main reason this
#                                              exists)
#   XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_{PASS,FAIL}    (clickhouse-pipeline only)
#                                              xtcp.xtcp_flat_records > 0 AND
#                                              xtcp.xtcp_flat_records_errors == 0
#   XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_{PASS,FAIL} (clickhouse-pipeline only)
#                                              Prom envelopeRows counter vs
#                                              ClickHouse row count within 15%
#   XTCP2_SELF_TEST_S3PARQUET_FILES_{PASS,FAIL}       (s3parquet only)
#                                              ≥1 .parquet object lands in
#                                              the MinIO bucket within 90s
#   XTCP2_SELF_TEST_S3PARQUET_ROWS_{PASS,FAIL}        (s3parquet only)
#                                              duckdb decodes the file and
#                                              returns ≥1 row
#   XTCP2_SELF_TEST_OVERALL_{PASS,FAIL}        overall outcome
#
# Each check is independent: failure of one does not skip the others, so the
# launcher can attribute failures precisely.
#
{
  pkgs,
  lib ? pkgs.lib,
  promPort ? 9088,
  grpcPort ? 8889,
  # When true, after the standard checks complete the self-test stops
  # xtcp2.service (which flushes Go coverage data to GOCOVERDIR) and
  # emits the tar+base64-encoded directory between
  #   XTCP2_COVERAGE_DUMP_START / _END
  # markers on stdout. The host lifecycle runner scrapes those markers
  # to extract per-run coverage. See nix/microvms/lib.nix.
  coverageEnabled ? false,
  coverDir ? "/var/lib/xtcp2cov",
  # When true (set on the clickhouse-pipeline flavor), the self-test
  # adds Check 11 (row count > 0 in xtcp.xtcp_flat_records) and Check
  # 12 (Prom envelopeRows counter reconciles with ClickHouse rows).
  # These assertions catch a class of bugs where the daemon produces
  # bytes that look right at the Kafka layer but ClickHouse silently
  # drops via the kafka_handle_error_mode='stream' path (parse failures
  # → _error column populated; main MV filters them out → 0 rows in
  # the destination table).
  runClickhouseCheck ? false,
  # When true (clickhouse-pipeline-parquet flavor only), the self-test
  # also queries the in-VM MinIO via ClickHouse's s3() table function
  # and asserts count() > 0 against the parquet objects xtcp2 wrote.
  # Validates the "operator queries parquet from inside ClickHouse"
  # deployment shape side-by-side with the kafka path.
  runClickhouseParquetCheck ? false,
  clickhousePassword ? "xtcp",
  # When true (set on the s3parquet flavor), adds Check 13 (≥1 .parquet
  # object lands in the MinIO bucket within 90 s) and Check 14 (duckdb
  # can read the file back and the row count is non-zero). The
  # rationale is the same as the ClickHouse checks: a misconfigured
  # encoder or sanitization can land syntactically-valid uploads that
  # downstream tools can't decode.
  runS3ParquetCheck ? false,
  s3Endpoint ? "http://127.0.0.1:9000",
  s3Bucket ? "xtcp2-records",
  s3AccessKey ? "xtcp2test",
  s3SecretKey ? "xtcp2testsecret",
}:

pkgs.writeShellApplication {
  name = "xtcp2-self-test";
  runtimeInputs = with pkgs; [
    coreutils
    systemd
    curl
    iproute2
    netcat-gnu
    gnugrep
    procps
    util-linux
    gnutar
    gzip
    docker  # only used by Check 11/12 (clickhouse-pipeline); harmless otherwise
    minio-client  # mc — only used by Check 13/14 (s3parquet); harmless otherwise
    duckdb  # used by Check 14 to decode the Parquet file
  ];
  text = ''
    set +e   # never exit early — we want all checks to run

    # writeShellApplication restricts PATH to runtimeInputs only, so the
    # cmd binaries that mkVm.nix installs via environment.systemPackages
    # (xtcp2, xtcp2client, ns, nsTest, …) aren't reachable. Prepend the
    # NixOS system path so check 4–7 can find them.
    export PATH="/run/current-system/sw/bin:$PATH"

    overall_ok=1

    echo "================================================"
    echo " xtcp2 microvm self-test"
    echo " kernel: $(uname -r)"
    echo " host:   $(uname -n)"
    echo "================================================"

    # Metric-counter helper: scrape one prom counter value from the
    # daemon's /metrics endpoint. Returns 0 if the counter is missing.
    # Many xtcp2 counters use multi-label vectors; the regex matches any
    # row whose label set CONTAINS each supplied substring.
    #
    # Prometheus prints labels in lexicographic order: function, then
    # type, then variable. So a single substring like
    # 'function="X",variable="Y"' will never match — labels are separated
    # by `,type="..."`. Pass two separate substrings instead.
    metric_value() {
      local name="$1"
      local label_a="$2"
      local label_b="''${3:-}"
      curl --silent --fail --max-time 2 \
           "http://127.0.0.1:${toString promPort}/metrics" \
        | awk -v n="$name" -v sa="$label_a" -v sb="$label_b" '
            $1 ~ n {
              if (sa != "" && index($0, sa) == 0) next
              if (sb != "" && index($0, sb) == 0) next
              sum += $NF + 0
            }
            END { printf "%d", sum }
          '
    }

    # ─── Check 1: systemd unit active ──────────────────────────────────────
    echo "--- check 1: systemctl is-active xtcp2 ---"
    check1=1
    for i in $(seq 1 30); do
      if systemctl is-active --quiet xtcp2; then
        echo "XTCP2_SELF_TEST_SYSTEMD_PASS  (active after ''${i}s)"
        check1=0
        break
      fi
      sleep 1
    done
    if [ "$check1" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_SYSTEMD_FAIL  (not active after 30s)"
      systemctl status xtcp2 --no-pager || true
      overall_ok=0
    fi

    # ─── Check 2: Prometheus /metrics endpoint reachable ──────────────────
    echo "--- check 2: GET http://127.0.0.1:${toString promPort}/metrics ---"
    check2=1
    for i in $(seq 1 30); do
      if curl --silent --fail --max-time 2 \
           "http://127.0.0.1:${toString promPort}/metrics" \
           | grep -q '^xtcp_'; then
        echo "XTCP2_SELF_TEST_METRICS_PASS  (after ''${i}s)"
        check2=0
        break
      fi
      sleep 1
    done
    if [ "$check2" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_METRICS_FAIL  (no xtcp2_* metric exposed in 30s)"
      overall_ok=0
    fi

    # ─── Check 3: netlink readout — daemon parses TCP sockets via inet_diag ─
    # xtcp2 has no file destination type (the daemon is configured with
    # `-dest null` in coverage / `-dest kafka:…` in the basic flavor), so
    # there's no /var/log/xtcp2.jsonl to grep — that file was always a
    # mirage. The right assertion is metric-based: the daemon's
    # `Netlinker.p` (or `NetlinkerIoUring.p`) counter accumulates the
    # number of inet_diag sockets parsed per recv. Open a TCP listener
    # on the host, then wait for ANY Netlinker `p` counter to reach >0
    # (the default-ns netlinker takes ~10s to come online after
    # openAndSetNSWithRetries returns, so a tight before/after window
    # straight after METRICS_PASS is racy). Aborting on after_p>0 is
    # enough — it means the daemon's inet_diag → Deserialize pipeline
    # ran end-to-end at least once.
    echo "--- check 3: daemon parses TCP sockets via inet_diag ---"
    nc -l 127.0.0.1 17321 >/dev/null 2>&1 &
    listener_pid=$!
    sleep 1
    ( echo "hi" | nc -w 8 127.0.0.1 17321 >/dev/null 2>&1 ) &
    client_pid=$!
    netlink_seen=0
    for _ in $(seq 1 20); do
      sample=$(metric_value "xtcp_counts" 'variable="p"' 'type="count"')
      if [ "$sample" -gt 0 ]; then
        netlink_seen=$sample
        break
      fi
      sleep 1
    done
    if [ "$netlink_seen" -gt 0 ]; then
      echo "XTCP2_SELF_TEST_NETLINK_PASS  (Netlinker parsed $netlink_seen sockets via inet_diag)"
    else
      echo "XTCP2_SELF_TEST_NETLINK_FAIL  (no inet_diag socket parsed in 20s)"
      overall_ok=0
    fi
    kill "$listener_pid" "$client_pid" 2>/dev/null || true
    wait 2>/dev/null || true

    # ─── Check 4: every cmd binary's -help works ──────────────────────────
    echo "--- check 4: -help smoke on every cmd binary ---"
    binaries=(
      xtcp2
      xtcp2client
      xtcp2_kafka_client
      clickhouse_protobuflist
      clickhouse_protobuflist_db
      clickhouse_http_insert_protobuflist
      kafka_to_clickhouse
      ns
      nsTest
      register_schema
    )
    check4=0
    failed_help=""
    for bin in "''${binaries[@]}"; do
      if ! command -v "$bin" >/dev/null 2>&1; then
        echo "    $bin: not on PATH"
        failed_help="$failed_help $bin(missing)"
        check4=1
        continue
      fi
      out=$("$bin" -help 2>&1)
      rc=$?
      if [ "$rc" -gt 2 ] || [ -z "$out" ]; then
        echo "    $bin: rc=$rc bytes=''${#out}"
        failed_help="$failed_help $bin(rc=$rc)"
        check4=1
      fi
    done
    if [ "$check4" -eq 0 ]; then
      echo "XTCP2_SELF_TEST_BINARIES_HELP_PASS  (10 binaries OK)"
    else
      echo "XTCP2_SELF_TEST_BINARIES_HELP_FAIL  (failed:$failed_help)"
      overall_ok=0
    fi

    # ─── Check 5: xtcp2 ↔ xtcp2client gRPC roundtrip ──────────────────────
    echo "--- check 5: xtcp2client connects to xtcp2 gRPC (port ${toString grpcPort}) ---"
    check5=1
    if command -v xtcp2client >/dev/null 2>&1; then
      # Run xtcp2client briefly. Exit code 0 or 124 (timeout) both acceptable
      # for "it connected"; anything else is a wire/handshake failure.
      # xtcp2client takes -target (host) + -port (numeric), not -addr.
      timeout 3s xtcp2client -target 127.0.0.1 -port "${toString grpcPort}" >/tmp/xtcp2client.log 2>&1
      rc=$?
      if [ "$rc" -eq 0 ] || [ "$rc" -eq 124 ]; then
        if [ -s /tmp/xtcp2client.log ]; then
          echo "XTCP2_SELF_TEST_GRPC_ROUNDTRIP_PASS  (xtcp2client rc=$rc, produced output)"
          check5=0
        else
          echo "XTCP2_SELF_TEST_GRPC_ROUNDTRIP_FAIL  (xtcp2client rc=$rc but no output)"
        fi
      else
        echo "XTCP2_SELF_TEST_GRPC_ROUNDTRIP_FAIL  (xtcp2client rc=$rc)"
        head -n 10 /tmp/xtcp2client.log 2>/dev/null || true
      fi
    else
      echo "XTCP2_SELF_TEST_GRPC_ROUNDTRIP_FAIL  (xtcp2client not on PATH)"
    fi
    if [ "$check5" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 6: ns inspector reads netns state ─────────────────────────
    echo "--- check 6: ns inspector ---"
    check6=1
    if command -v ns >/dev/null 2>&1; then
      out=$(timeout 5s ns -help 2>&1)
      rc=$?
      if [ "$rc" -le 2 ] && [ -n "$out" ]; then
        echo "XTCP2_SELF_TEST_NS_INSPECT_PASS  (ns -help rc=$rc, bytes=''${#out})"
        check6=0
      else
        echo "XTCP2_SELF_TEST_NS_INSPECT_FAIL  (ns -help rc=$rc, bytes=''${#out})"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_INSPECT_FAIL  (ns not on PATH)"
    fi
    if [ "$check6" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 7: nsTest runs ────────────────────────────────────────────
    echo "--- check 7: nsTest ---"
    check7=1
    if command -v nsTest >/dev/null 2>&1; then
      out=$(timeout 5s nsTest -help 2>&1)
      rc=$?
      if [ "$rc" -le 2 ] && [ -n "$out" ]; then
        echo "XTCP2_SELF_TEST_NSTEST_PASS  (nsTest -help rc=$rc, bytes=''${#out})"
        check7=0
      else
        echo "XTCP2_SELF_TEST_NSTEST_FAIL  (nsTest -help rc=$rc, bytes=''${#out})"
      fi
    else
      echo "XTCP2_SELF_TEST_NSTEST_FAIL  (nsTest not on PATH)"
    fi
    if [ "$check7" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 8: ns lifecycle — ip netns add/delete propagates ──────────
    # The xtcp2 daemon watches /run/netns/ via fsnotify. Creating a new
    # netns SHOULD fire the watcher → nsAdd → openAndSetNSWithRetries →
    # createNetlinkersAndStore (spawns a per-ns netlinker goroutine).
    # Then deletion SHOULD tear it down via nsDelete.
    #
    # We assert the daemon noticed by reading two metric counters:
    #   * the watchNamespaces "event" counter (the fsnotify callback)
    #   * the netNamespaceInstance "start" counter (per-ns goroutine)
    # Both should bump by ≥1 between before/after, and the netlinker
    # count should drop back when we delete the ns.
    echo "--- check 8: ns lifecycle (ip netns add/delete) ---"
    check8=1
    if command -v ip >/dev/null 2>&1; then
      # The label keys are function/variable/type (see promLabels in
      # pkg/xtcp/prometheus.go). Prometheus prints labels alphabetically
      # (function, type, variable), so the helper takes the function/
      # variable filters as separate args.
      before_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      ip netns add xtcp_test_ns_a 2>&1 || true
      # Bring lo up so a subsequent socket inside the ns is meaningful.
      ip netns exec xtcp_test_ns_a ip link set lo up 2>&1 || true
      # Give the daemon time to fsnotify + nsAdd + spawn netlinker.
      sleep 3
      after_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')
      after_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      ip netns delete xtcp_test_ns_a 2>&1 || true
      sleep 3
      after_delete_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')

      if [ "$after_evt" -gt "$before_evt" ] && [ "$after_inst" -gt "$before_inst" ] && [ "$after_delete_evt" -gt "$after_evt" ]; then
        echo "XTCP2_SELF_TEST_NS_LIFECYCLE_PASS  (evt:$before_evt→$after_evt→$after_delete_evt inst:$before_inst→$after_inst)"
        check8=0
      else
        echo "XTCP2_SELF_TEST_NS_LIFECYCLE_FAIL  (evt:$before_evt→$after_evt→$after_delete_evt inst:$before_inst→$after_inst)"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_LIFECYCLE_FAIL  (ip not on PATH)"
    fi
    if [ "$check8" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 9: TCP traffic inside a fresh netns — full netlinker path ─
    # Creates a netns, brings up lo, starts a listening socket. xtcp2's
    # per-ns netlinker SHOULD poll inet_diag and see the socket; the
    # Deserialize loop SHOULD parse the response into TCPInfo / inet_diag
    # attributes. We assert via the Netlinker "packets" counter for the
    # per-ns netlinker fd: it must bump by ≥1 while the ns is live.
    echo "--- check 9: TCP socket inside netns drives netlinker traffic ---"
    check9=1
    if command -v ip >/dev/null 2>&1 && command -v nc >/dev/null 2>&1; then
      # Match both Netlinker (syscall) and NetlinkerIoUring (io_uring) packet
      # counters so this check works in both coverage VM flavors.
      before_packets=$(metric_value "xtcp_counts" 'variable="packets"')
      ip netns add xtcp_test_ns_b 2>&1 || true
      ip netns exec xtcp_test_ns_b ip link set lo up 2>&1 || true
      # Listener in the ns. timeout bounds wall-clock so we don't leak
      # a process if the check fails partway.
      ip netns exec xtcp_test_ns_b timeout 10s nc -l 127.0.0.1 17322 >/dev/null 2>&1 &
      ns_listener=$!
      sleep 1
      # Client also in the ns (loopback only — the ns has no real iface).
      ip netns exec xtcp_test_ns_b sh -c '(echo hello; sleep 5) | nc -w 5 127.0.0.1 17322' >/dev/null 2>&1 &
      ns_client=$!

      # xtcp2 polls every 2s; give it two cycles to see the socket(s).
      sleep 5
      after_packets=$(metric_value "xtcp_counts" 'variable="packets"')

      # Tear down the listener + client and the ns itself.
      kill "$ns_listener" "$ns_client" 2>/dev/null || true
      wait 2>/dev/null || true
      ip netns delete xtcp_test_ns_b 2>&1 || true

      if [ "$after_packets" -gt "$before_packets" ]; then
        echo "XTCP2_SELF_TEST_NS_TRAFFIC_PASS  (Netlinker.packets:$before_packets→$after_packets)"
        check9=0
      else
        echo "XTCP2_SELF_TEST_NS_TRAFFIC_FAIL  (Netlinker.packets:$before_packets→$after_packets)"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_TRAFFIC_FAIL  (ip or nc not on PATH)"
    fi
    if [ "$check9" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 10: docker netns lifecycle — /run/docker/netns/ watch path ──
    # xtcp2 probes /run/netns/ AND /run/docker/netns/ at startup
    # (pkg/xtcp/init.go netNsCandidateDirs). When the coverage VM pre-
    # creates the docker dir via tmpfiles, the daemon spawns a SECOND
    # watchNsNamespace goroutine for it. Without exercising it the docker
    # branch in watchNsNamespace stays at 0% coverage.
    #
    # We mimic docker's behavior — create a netns under /run/netns/ via
    # the kernel's normal mechanism, then bind-mount it under
    # /run/docker/netns/ to fire fsnotify Create on the docker dir.
    # That's all docker actually does at the filesystem level when
    # `docker run --network=…` spawns a container.
    echo "--- check 10: docker netns lifecycle (/run/docker/netns/) ---"
    check10=1
    if command -v ip >/dev/null 2>&1 && [ -d /run/docker/netns ]; then
      before_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')

      ip netns add xtcp_docker_ns 2>&1 || true
      # mount --bind reuses the netns inode under the docker dir, so
      # checkMountInfo can find it just like a docker-managed one.
      mount --bind /run/netns/xtcp_docker_ns /run/docker/netns/xtcp_docker_ns 2>&1 || true
      sleep 3
      after_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')
      after_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')

      umount /run/docker/netns/xtcp_docker_ns 2>&1 || true
      rm -f /run/docker/netns/xtcp_docker_ns 2>&1 || true
      ip netns delete xtcp_docker_ns 2>&1 || true
      sleep 3
      after_delete_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces"' 'variable="event"')

      if [ "$after_evt" -gt "$before_evt" ] && [ "$after_inst" -gt "$before_inst" ] && [ "$after_delete_evt" -gt "$after_evt" ]; then
        echo "XTCP2_SELF_TEST_NS_DOCKER_PASS  (evt:$before_evt→$after_evt→$after_delete_evt inst:$before_inst→$after_inst)"
        check10=0
      else
        echo "XTCP2_SELF_TEST_NS_DOCKER_FAIL  (evt:$before_evt→$after_evt→$after_delete_evt inst:$before_inst→$after_inst)"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_DOCKER_FAIL  (ip not on PATH or /run/docker/netns/ missing)"
    fi
    if [ "$check10" -ne 0 ]; then overall_ok=0; fi

    ${lib.optionalString runS3ParquetCheck ''
      # ─── Check 13: s3parquet object landed in MinIO ──────────────────
      # Same model as Check 11 — the daemon could be producing bytes
      # that look right at the Kafka/proto layer but fail at the S3
      # upload (auth, bucket permissions, network). Catch silently.
      echo "--- check 13: s3parquet — at least one .parquet object ---"
      export MC_CONFIG_DIR=/tmp/self-test-mc
      mkdir -p "$MC_CONFIG_DIR"
      mc alias set local ${s3Endpoint} ${s3AccessKey} ${s3SecretKey} >/dev/null 2>&1 || true
      check13=1
      parquet_key=""
      for _ in $(seq 1 90); do
        parquet_key=$(mc find local/${s3Bucket} --name '*.parquet' 2>/dev/null | head -n1)
        if [ -n "$parquet_key" ]; then
          break
        fi
        sleep 1
      done
      if [ -n "$parquet_key" ]; then
        echo "XTCP2_SELF_TEST_S3PARQUET_FILES_PASS  (first object=$parquet_key)"
        check13=0
      else
        echo "XTCP2_SELF_TEST_S3PARQUET_FILES_FAIL  (no .parquet object after 90s)"
      fi
      if [ "$check13" -ne 0 ]; then overall_ok=0; fi

      # ─── Check 14: s3parquet row decode ──────────────────────────────
      # Download the first .parquet object and verify duckdb can read it
      # AND that the row count is non-zero. Sanity check on the schema /
      # codec choices in pkg/xtcp/destinations_s3parquet_schema.go.
      echo "--- check 14: s3parquet — duckdb decodes the parquet file ---"
      check14=1
      if [ -n "$parquet_key" ]; then
        mc cp "$parquet_key" /tmp/xtcp2-s3p.parquet >/dev/null 2>&1
        if [ ! -s /tmp/xtcp2-s3p.parquet ]; then
          echo "XTCP2_SELF_TEST_S3PARQUET_ROWS_FAIL  (downloaded file empty: $parquet_key)"
        else
          rowcount=$(duckdb -noheader -list \
            -c "SELECT count(*) FROM read_parquet('/tmp/xtcp2-s3p.parquet')" 2>/dev/null \
            | tail -n1 | tr -d '[:space:]')
          if [ -n "$rowcount" ] && [ "$rowcount" -ge 1 ] 2>/dev/null; then
            echo "XTCP2_SELF_TEST_S3PARQUET_ROWS_PASS  (rows=$rowcount, key=$parquet_key)"
            check14=0
          else
            echo "XTCP2_SELF_TEST_S3PARQUET_ROWS_FAIL  (duckdb returned no rows; key=$parquet_key)"
            duckdb -c "DESCRIBE SELECT * FROM read_parquet('/tmp/xtcp2-s3p.parquet')" 2>&1 | head -n 20 || true
          fi
        fi
      else
        echo "XTCP2_SELF_TEST_S3PARQUET_ROWS_FAIL  (no parquet object to test)"
      fi
      if [ "$check14" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runClickhouseCheck ''
      # ─── Check 11: ClickHouse received >0 rows + zero parse errors ───
      # xtcp2 marshals an Envelope per poll cycle and Kafka-ships it.
      # ClickHouse's kafka engine table (kafka_format=ProtobufList)
      # decodes Envelope.row[] into xtcp.xtcp_flat_records. The main MV
      # filters rows whose Kafka virtual _error column is non-empty into
      # xtcp.xtcp_flat_records_errors. So PASS requires both:
      #   * count(xtcp_flat_records)        > 0   (records flowed end-to-end)
      #   * count(xtcp_flat_records_errors) == 0   (zero parse failures)
      # Wait up to 60s for the first row to appear: initial poll cycle is
      # 5s by default, first kafka push lands within ~10s, ClickHouse
      # kafka-engine consume tick is ~5s.
      echo "--- check 11: ClickHouse received >0 rows ---"
      check11=1
      rows=0
      for _ in $(seq 1 30); do
        rows=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
          -q "SELECT count() FROM xtcp.xtcp_flat_records" 2>/dev/null | tr -d '\r\n' || echo 0)
        if [ "''${rows:-0}" -gt 0 ] 2>/dev/null; then
          break
        fi
        sleep 2
      done
      errors=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records_errors" 2>/dev/null | tr -d '\r\n' || echo "?")
      if [ "''${rows:-0}" -gt 0 ] 2>/dev/null && [ "$errors" = "0" ]; then
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_PASS  (rows=$rows, errors=0)"
        check11=0
      else
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_FAIL  (rows=$rows, errors=$errors)"
      fi
      if [ "$check11" -ne 0 ]; then overall_ok=0; fi

      # ─── Check 12: Prom records counter vs ClickHouse rows reconcile ─
      # xtcp2 bumps xtcp_counts{function=Poller,variable=envelopeRows}
      # for every row appended to a flushed envelope. ClickHouse's
      # destination table count should equal that within a small lag
      # window (kafka consume + MV flush ≈ a few seconds).
      # Tolerance: ChRows ∈ [0.4 * promRows, promRows + 100]. The
      # observed steady-state lag is ~40% — xtcp produces every 5s,
      # ClickHouse's kafka-engine consumer flushes in 5-10s batches,
      # so at any sampling instant the in-flight gap is ~one batch
      # plus the network/parse RTT. Anything tighter trips on healthy
      # runs. The upper band has 100 absolute slack so a slow Prom
      # scrape can't put chRows over the cap.
      echo "--- check 12: Prom envelopeRows vs ClickHouse rows reconcile ---"
      check12=1
      promRows=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="envelopeRows"')
      chRows=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records" 2>/dev/null | tr -d '\r\n' || echo 0)
      if [ "''${chRows:-0}" -gt 0 ] 2>/dev/null && [ "''${promRows:-0}" -gt 0 ] 2>/dev/null \
         && [ $((chRows * 100)) -ge $((promRows * 40)) ] \
         && [ "$chRows" -le $((promRows + 100)) ]; then
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_PASS  (prom=$promRows, ch=$chRows)"
        check12=0
      else
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_FAIL  (prom=$promRows, ch=$chRows)"
      fi
      if [ "$check12" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runClickhouseParquetCheck ''
      # ─── Check 15: ClickHouse can SELECT from MinIO parquet via s3() ──
      # The mixed flavor runs a second xtcp2 instance with -dest s3parquet
      # writing to in-VM MinIO. ClickHouse reaches the host (where MinIO
      # listens) via the host.docker.internal alias added to its
      # /etc/hosts. Wait up to 90s for the secondary xtcp2 to accumulate
      # enough rows to hit the 4 MiB flush threshold and write the first
      # parquet object.
      echo "--- check 15: ClickHouse s3() reads MinIO parquet ---"
      check15=1
      parquetRows=0
      for _ in $(seq 1 45); do
        # The s3() URL uses host.docker.internal because we're inside
        # the clickhouse container. Glob ** matches the Hive-style
        # host=…/date=…/hour=… partitioning xtcp2's parquet writer uses.
        parquetRows=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
          -q "SELECT count() FROM s3('http://host.docker.internal:9000/xtcp2-records/**/*.parquet', 'xtcp2test', 'xtcp2testsecret', 'Parquet')" 2>/dev/null | tr -d '\r\n' || echo 0)
        if [ "''${parquetRows:-0}" -gt 0 ] 2>/dev/null; then
          break
        fi
        sleep 2
      done
      if [ "''${parquetRows:-0}" -gt 0 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_CLICKHOUSE_PARQUET_PASS  (rows=$parquetRows)"
        check15=0
      else
        echo "XTCP2_SELF_TEST_CLICKHOUSE_PARQUET_FAIL  (rows=$parquetRows)"
      fi
      if [ "$check15" -ne 0 ]; then overall_ok=0; fi
    ''}

    echo "================================================"
    if [ "$overall_ok" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_OVERALL_PASS"
      overall_rc=0
    else
      echo "XTCP2_SELF_TEST_OVERALL_FAIL"
      overall_rc=1
    fi

    ${lib.optionalString coverageEnabled ''
      # ─── Coverage dump (coverage flavor only) ────────────────────────────
      # systemctl stop sends SIGTERM, xtcp2's runtime flushes -cover counters
      # to $GOCOVERDIR on clean exit. Wait a beat for the flush, then tar +
      # base64 the directory between marker lines so the host can scrape it.
      echo "--- coverage: stopping xtcp2 so -cover data flushes ---"
      systemctl stop xtcp2 || true
      sleep 2
      if [ -d "${coverDir}" ] && [ -n "$(ls -A "${coverDir}" 2>/dev/null)" ]; then
        echo "XTCP2_COVERAGE_DUMP_START"
        tar c -C "${coverDir}" . | gzip -n | base64 -w0
        echo
        echo "XTCP2_COVERAGE_DUMP_END"
      else
        echo "XTCP2_COVERAGE_DUMP_EMPTY (${coverDir} is missing or empty)"
      fi
    ''}

    exit "$overall_rc"
  '';
}
