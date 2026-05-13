# nix/microvms/self-test.nix
#
# Self-test script that runs inside the microvm after xtcp2 starts. Performs
# three independent checks and prints labeled PASS/FAIL sentinels that the host
# launcher (see ./default.nix → fullTest) scrapes from the serial console.
#
# Sentinels:
#   XTCP2_SELF_TEST_SYSTEMD_PASS  or  XTCP2_SELF_TEST_SYSTEMD_FAIL
#   XTCP2_SELF_TEST_METRICS_PASS  or  XTCP2_SELF_TEST_METRICS_FAIL
#   XTCP2_SELF_TEST_NETLINK_PASS  or  XTCP2_SELF_TEST_NETLINK_FAIL
#   XTCP2_SELF_TEST_OVERALL_PASS  or  XTCP2_SELF_TEST_OVERALL_FAIL
#
# Each check is independent: a failure of one does not skip the others, so the
# launcher can attribute failures precisely.
#
{
  pkgs,
  promPort ? 9088,
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
  ];
  text = ''
    set +e   # never exit early — we want all three checks to run

    overall_ok=1

    echo "================================================"
    echo " xtcp2 microvm self-test"
    echo " kernel: $(uname -r)"
    echo " host:   $(hostname)"
    echo "================================================"

    # ─── Check 1: systemd unit active ──────────────────────────────────────
    echo "--- check 1: systemctl is-active xtcp2 ---"
    for i in $(seq 1 30); do
      if systemctl is-active --quiet xtcp2; then
        echo "XTCP2_SELF_TEST_SYSTEMD_PASS  (active after ''${i}s)"
        check1=0
        break
      fi
      sleep 1
    done
    if [ "''${check1:-1}" -ne 0 ]; then
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
           | grep -q '^xtcp2_'; then
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

    # Open a listener and a transient client to produce a stable 4-tuple
    nc -l 127.0.0.1 17321 >/dev/null 2>&1 &
    listener_pid=$!
    sleep 1
    ( echo "hi" | nc -w 2 127.0.0.1 17321 >/dev/null 2>&1 ) &
    client_pid=$!

    # xtcp2 collects on a configurable cadence; wait up to 20s for the record
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

    # Cleanup
    kill "$listener_pid" "$client_pid" 2>/dev/null || true
    wait 2>/dev/null || true

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
