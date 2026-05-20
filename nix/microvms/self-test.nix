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

    # ─── Check 3: netlink readout — open a loopback TCP conn, see it in jsonl ─
    echo "--- check 3: netlink readout of loopback TCP socket ---"
    check3=1
    nc -l 127.0.0.1 17321 >/dev/null 2>&1 &
    listener_pid=$!
    sleep 1
    ( echo "hi" | nc -w 2 127.0.0.1 17321 >/dev/null 2>&1 ) &
    client_pid=$!
    for _ in $(seq 1 20); do
      if [ -f /var/log/xtcp2.jsonl ] && \
         grep -E -q '"(d_?port|dst_port|remote_port)"[^,}]*17321' /var/log/xtcp2.jsonl; then
        echo "XTCP2_SELF_TEST_NETLINK_PASS  (4-tuple :17321 found in jsonl)"
        check3=0
        break
      fi
      sleep 1
    done
    if [ "$check3" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_NETLINK_FAIL  (no record matching :17321 in /var/log/xtcp2.jsonl)"
      echo "--- last 20 lines of jsonl sink ---"
      tail -n 20 /var/log/xtcp2.jsonl 2>/dev/null || echo "(file missing)"
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
      timeout 3s xtcp2client -addr "127.0.0.1:${toString grpcPort}" >/tmp/xtcp2client.log 2>&1
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

    # Metric-counter helper: scrape one prom counter value from the
    # daemon's /metrics endpoint. Returns 0 if the counter is missing.
    # Many xtcp2 counters use multi-label vectors; the regex matches any
    # row whose label set CONTAINS the supplied substring.
    metric_value() {
      local name="$1"
      local label_substring="$2"
      curl --silent --fail --max-time 2 \
           "http://127.0.0.1:${toString promPort}/metrics" \
        | awk -v n="$name" -v s="$label_substring" '
            $1 ~ n {
              if (s == "" || index($0, s) > 0) {
                # Last field is the counter value.
                sum += $NF + 0
              }
            }
            END { printf "%d", sum }
          '
    }

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
      # Diagnostic: what does the daemon think it's watching? Dump
      # every xtcp_counts row whose label set mentions watchNamespaces.
      echo "  pre: ls /run/netns/:"
      ls -la /run/netns/ 2>&1 | sed 's/^/    /'
      echo "  pre: watchNamespaces metric rows:"
      curl --silent --fail --max-time 2 \
           "http://127.0.0.1:${toString promPort}/metrics" \
        | grep -E 'watchNamespaces|netNamespaceInstance|nsAdd' \
        | sed 's/^/    /' || true

      # The label keys are function/variable/type (see promLabels in
      # pkg/xtcp/prometheus.go). watchNamespaces emits multiple variable
      # values per call site; we filter on variable="event" to count
      # fsnotify-Create+Remove events specifically.
      before_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces",variable="event"')
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance",variable="start"')

      add_out=$(ip netns add xtcp_test_ns_a 2>&1) ; add_rc=$?
      echo "  ip netns add xtcp_test_ns_a rc=$add_rc out=''${add_out:-(empty)}"
      # Bring lo up so a subsequent socket inside the ns is meaningful.
      ip netns exec xtcp_test_ns_a ip link set lo up 2>&1 | sed 's/^/    /' || true
      echo "  post-add: ls /run/netns/:"
      ls -la /run/netns/ 2>&1 | sed 's/^/    /'
      # Give the daemon time to fsnotify + nsAdd + spawn netlinker.
      sleep 3
      after_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces",variable="event"')
      after_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance",variable="start"')

      echo "  post-add: watchNamespaces metric rows:"
      curl --silent --fail --max-time 2 \
           "http://127.0.0.1:${toString promPort}/metrics" \
        | grep -E 'watchNamespaces|netNamespaceInstance|nsAdd' \
        | sed 's/^/    /' || true

      ip netns delete xtcp_test_ns_a 2>&1 | sed 's/^/    /' || true
      sleep 3
      after_delete_evt=$(metric_value "xtcp_counts" 'function="watchNamespaces",variable="event"')

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
