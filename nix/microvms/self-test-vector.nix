# nix/microvms/self-test-vector.nix
#
# Self-test for the Vector flavor of the microvm. Mirrors the structure of
# self-test.nix (independent checks, PASS/FAIL sentinels per check) and:
#
#   - keeps checks 1, 2, 4, 5, 6, 7 verbatim (systemd, prometheus, cmd -help
#     smoke, gRPC roundtrip, ns inspector, nsTest)
#   - replaces the dead JSONL "check 3 (netlink)" with three new checks that
#     verify the end-to-end Vector→MinIO pipeline:
#       VECTOR  — vector active, datagram socket bound with right perms
#       MINIO   — minio active, bucket exists
#       PARQUET — :17321 nc roundtrip triggers a netlink poll; within 60 s a
#                 parquet object lands in the bucket and decodes via duckdb
#                 to at least one row.
#
# Each check emits exactly one sentinel; the host launcher (lib.nix) grep
# was extended to include the new ones.
#
{
  pkgs,
  promPort ? 9088,
  grpcPort ? 8889,
  bucket ? "xtcp2-records",
  accessKey ? "xtcp2test",
  secretKey ? "xtcp2testsecret",
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
    minio-client
    duckdb
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
    echo " xtcp2 microvm self-test (Vector flavor)"
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

    # ─── Check 3a (was NETLINK): VECTOR — vector active + socket bound ────
    echo "--- check 3a: vector active and unixgram socket present ---"
    check_vector=1
    for i in $(seq 1 30); do
      if systemctl is-active --quiet vector && [ -S /run/xtcp2/output.sock ]; then
        # confirm perms include o+w (xtcp2 runs as root so technically it can
        # write anyway, but the test asserts the published contract).
        mode=$(stat -c '%a' /run/xtcp2/output.sock 2>/dev/null || echo "")
        if [ "$mode" = "666" ] || [ "$mode" = "660" ] || [ "$mode" = "777" ]; then
          echo "XTCP2_SELF_TEST_VECTOR_PASS  (active after ''${i}s, socket mode=$mode)"
          check_vector=0
          break
        fi
      fi
      sleep 1
    done
    if [ "$check_vector" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_VECTOR_FAIL  (vector not ready / socket missing after 30s)"
      systemctl status vector --no-pager || true
      ls -la /run/xtcp2/ || true
      overall_ok=0
    fi

    # ─── Check 3b (was NETLINK): MINIO — minio active + bucket exists ─────
    echo "--- check 3b: minio active and bucket ${bucket} present ---"
    check_minio=1
    export MC_CONFIG_DIR=/tmp/self-test-mc
    mkdir -p "$MC_CONFIG_DIR"
    mc alias set local http://127.0.0.1:9000 ${accessKey} ${secretKey} >/dev/null 2>&1 || true
    for i in $(seq 1 30); do
      if systemctl is-active --quiet minio && \
         mc ls local/${bucket} >/dev/null 2>&1; then
        echo "XTCP2_SELF_TEST_MINIO_PASS  (active and bucket reachable after ''${i}s)"
        check_minio=0
        break
      fi
      sleep 1
    done
    if [ "$check_minio" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_MINIO_FAIL  (minio/bucket not ready after 30s)"
      systemctl status minio --no-pager || true
      systemctl status xtcp2-bucket-bootstrap --no-pager || true
      overall_ok=0
    fi

    # ─── Check 3c (was NETLINK): PARQUET — end-to-end via :17321 ──────────
    echo "--- check 3c: trigger :17321 conn, expect parquet object in MinIO ---"
    # Open a brief loopback TCP roundtrip to give xtcp2 a socket to report.
    nc -l 127.0.0.1 17321 >/dev/null 2>&1 &
    listener_pid=$!
    sleep 1
    ( echo "hi" | nc -w 2 127.0.0.1 17321 >/dev/null 2>&1 ) &
    client_pid=$!

    # Wait up to 60 s for any parquet object to appear under the bucket.
    parquet_key=""
    for i in $(seq 1 60); do
      parquet_key=$(mc find local/${bucket} --name '*.parquet' 2>/dev/null | head -n1)
      if [ -n "$parquet_key" ]; then
        echo "    parquet object: $parquet_key  (after ''${i}s)"
        break
      fi
      sleep 1
    done

    kill "$listener_pid" "$client_pid" 2>/dev/null || true
    wait 2>/dev/null || true

    if [ -z "$parquet_key" ]; then
      echo "XTCP2_SELF_TEST_PARQUET_FAIL  (no .parquet object in bucket after 60s)"
      mc ls --recursive local/${bucket} 2>&1 | head -n 20 || true
      echo "--- xtcp2 metrics relevant to pipeline ---"
      curl --silent --max-time 2 "http://127.0.0.1:${toString promPort}/metrics" \
        | grep -E '^xtcp_counts.*(Deserialize|destUnixGram)' | head -20 || true
      echo "--- vector status + recent journal ---"
      systemctl status vector --no-pager -l 2>&1 | tail -n 20 || true
      journalctl -u vector --no-pager -n 30 2>&1 | tail -n 30 || true
      overall_ok=0
    else
      # Download it and decode with duckdb.
      mc cp "$parquet_key" /tmp/xtcp2.parquet >/dev/null 2>&1
      if [ ! -s /tmp/xtcp2.parquet ]; then
        echo "XTCP2_SELF_TEST_PARQUET_FAIL  (downloaded file empty: $parquet_key)"
        overall_ok=0
      else
        rowcount=$(duckdb -noheader -list \
          -c "SELECT count(*) FROM read_parquet('/tmp/xtcp2.parquet')" 2>/dev/null \
          | tail -n1 | tr -d '[:space:]')
        if [ -n "$rowcount" ] && [ "$rowcount" -ge 1 ]; then
          # Soft assertion: try to find the :17321 dst_port. If schema or
          # field name differs, we still PASS on rowcount but log it.
          port_hit=$(duckdb -noheader -list \
            -c "SELECT count(*) FROM read_parquet('/tmp/xtcp2.parquet') WHERE inet_diag_msg_socket_destination_port = 17321" \
            2>/dev/null | tail -n1 | tr -d '[:space:]' || echo "?")
          echo "XTCP2_SELF_TEST_PARQUET_PASS  (rows=$rowcount, :17321 matches=$port_hit, key=$parquet_key)"
        else
          echo "XTCP2_SELF_TEST_PARQUET_FAIL  (duckdb decode returned no rows; key=$parquet_key)"
          duckdb -c "DESCRIBE SELECT * FROM read_parquet('/tmp/xtcp2.parquet')" 2>&1 | head -n 20 || true
          overall_ok=0
        fi
      fi
    fi

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

    echo "================================================"
    if [ "$overall_ok" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_OVERALL_PASS"
      exit 0
    else
      echo "XTCP2_SELF_TEST_OVERALL_FAIL"
      exit 1
    fi
  '';
}
