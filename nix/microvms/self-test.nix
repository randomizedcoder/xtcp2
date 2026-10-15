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
#   XTCP2_SELF_TEST_UDS_SECURITY_{PASS,FAIL}   authenticated gRPC/HTTP UDS
#                                              access, config control, and
#                                              both stream modes
#   XTCP2_SELF_TEST_NS_INSPECT_{PASS,FAIL}     ns inspector reads netns state
#   XTCP2_SELF_TEST_NSTEST_{PASS,FAIL}         nsTest binary runs
#   XTCP2_SELF_TEST_NS_LIFECYCLE_{PASS,FAIL}   a netns holding a live process is
#                                              discovered by the Method B /proc
#                                              scan (pre-poll reconcile → nsAdd,
#                                              spawning a per-ns netlinker); when
#                                              the process exits + the ns is
#                                              removed, reconcile → nsDelete
#   XTCP2_SELF_TEST_NS_TRAFFIC_{PASS,FAIL}     TCP socket created inside a fresh
#                                              netns produces records via that
#                                              ns's netlinker (drives the full
#                                              netlinkerSyscall body + real
#                                              Deserialize on real netlink
#                                              bytes — the main reason this
#                                              exists)
#   XTCP2_SELF_TEST_CTL_HOT_{PASS,FAIL}        xtcp2ctl set-poll-frequency is
#                                              reflected by a later `get` and
#                                              bumps the Poller ticker.Reset
#                                              counter (hot change, no restart)
#   XTCP2_SELF_TEST_CTL_TRIGGER_{PASS,FAIL}    xtcp2ctl trigger-poll + poll-burst
#                                              advance the Poller pollRequestCh
#                                              counter with the ticker parked
#   XTCP2_SELF_TEST_CTL_RESTART_{PASS,FAIL}    xtcp2ctl reconfigure = in-place
#                                              soft restart (syscall.Exec): same
#                                              MainPID, and `get` shows the new
#                                              config (tag) live after the
#                                              re-exec (non-coverage)
#   XTCP2_SELF_TEST_NS_ANONYMOUS_{PASS,FAIL}   an anonymous `unshare -n` netns
#                                              (no /run/netns bind mount) held by
#                                              a live process is discovered by
#                                              the Method B /proc scan — the
#                                              audit gap dir-scan/inotify missed
#   XTCP2_SELF_TEST_NODROP_{PASS,FAIL}         no record parsed after its poll's
#                                              flush (envelopePostFlushDrop 0,
#                                              envelopeAppend >= 1); every flavor
#   XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_{PASS,FAIL}    (clickhouse-pipeline only)
#                                              xtcp.xtcp_flat_records > 0 AND
#                                              xtcp.xtcp_flat_records_errors == 0
#   XTCP2_SELF_TEST_CLICKHOUSE_RECONCILE_{PASS,FAIL} (clickhouse-pipeline only)
#                                              Prom envelopeRows counter vs
#                                              ClickHouse row count within 15%
#   XTCP2_SELF_TEST_SCHEMA_VERSION_{PASS,FAIL} (clickhouse-pipeline only)
#                                              per-version routing: every row is
#                                              the current epoch N
#                                              (XtcpFlatRecordSchemaVersion) and
#                                              lands in xtcp_flat_records_vN, no
#                                              other epoch or table, Merge union
#                                              matches, daemon_version populated
#   XTCP2_SELF_TEST_ENRICH_NIC_{PASS,FAIL}     (clickhouse-pipeline only) NIC
#                                              enrichment: uplink1_nic_driver is
#                                              populated in xtcp.xtcp_flat_records
#   XTCP2_SELF_TEST_ENRICH_CONTAINER_{PASS,FAIL} (clickhouse-pipeline only)
#                                              container enrichment: rows exist
#                                              with container_id != '' AND
#                                              netns != 'default' (docker-netns
#                                              discovery + Docker API labels)
#   XTCP2_SELF_TEST_ENRICH_LLDP_{PASS,FAIL}    (clickhouse-pipeline only)
#                                              best-effort LLDP: lldpd socket
#                                              present + daemon healthy + records
#                                              flowing (no peer to assert against)
#   XTCP2_SELF_TEST_ENRICH_BESTEFFORT_NEGATIVE_{PASS,FAIL} (clickhouse-pipeline
#                                              only) enrichers pointed at missing
#                                              sockets are non-fatal: a throwaway
#                                              xtcp2 still starts + still produces
#   XTCP2_SELF_TEST_S3PARQUET_FILES_{PASS,FAIL}       (s3parquet only)
#                                              ≥1 .parquet object lands in
#                                              the MinIO bucket within 90s
#   XTCP2_SELF_TEST_S3PARQUET_ROWS_{PASS,FAIL}        (s3parquet only)
#                                              duckdb decodes the file and
#                                              returns ≥1 row
#   XTCP2_SELF_TEST_VALKEY_CONSUME_{PASS,FAIL}        (valkey only)
#                                              ≥1 record published to a real
#                                              in-VM Valkey server AND consumed
#                                              back by the pre-subscribed
#                                              in-VM subscriber (pub/sub round-trip)
#   XTCP2_SELF_TEST_NATS_CONSUME_{PASS,FAIL}          (nats only)
#                                              same round-trip via a real in-VM
#                                              NATS server + subscriber
#   XTCP2_SELF_TEST_NSQ_CONSUME_{PASS,FAIL}           (nsq only)
#                                              records published to a real in-VM
#                                              nsqd AND finished by an nsq_tail
#                                              consumer (nsqd /stats finish_count)
#   XTCP2_SELF_TEST_RTNL_EVENTS_{PASS,FAIL}    rtnetlink listener (pkg/rtnlmon):
#                                              link/addr events in order, and an
#                                              overrun drained and resynced
#   XTCP2_SELF_TEST_OVERALL_{PASS,FAIL}        overall outcome
#   XTCP2_SELF_TEST_ASN_{PASS,FAIL}           (interface-naming only) ASN
#                                              enrichment end to end: the record
#                                              for the held 8.8.8.8:53 socket
#                                              carries AS15169 / google /
#                                              LOCALITY_REMOTE from an artifact
#                                              the real ipfeed-collector built
#   XTCP2_SELF_TEST_LOCALITY_EVENTS_{PASS,FAIL}  (interface-naming only) an
#                                              address added after start makes
#                                              its socket LOCALITY_SELF via the
#                                              -localityEvents listener
#   XTCP2_SELF_TEST_IPMETA_BOOTSTRAP_{PASS,FAIL}    bootstrap lookup artifact
#                                              enriches records before live data
#   XTCP2_SELF_TEST_IPMETA_RECOVERY_{PASS,FAIL}     delayed in-VM HTTP source
#                                              refreshes the lookup table/cache
#   XTCP2_SELF_TEST_IPMETA_BAD_UPDATE_{PASS,FAIL}   bad/empty update does not
#                                              replace the last good lookup
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
  # When true (clickhouse-http flavor), adds Check 11h: xtcp2 inserts directly
  # into ClickHouse over HTTP (no Kafka), asserting rows arrived AND the
  # destHTTP POST-success counter advanced.
  runClickhouseHttpCheck ? false,
  # When true (clickhouse-pipeline-parquet flavor only), the self-test
  # also queries the in-VM MinIO via ClickHouse's s3() table function
  # and asserts count() > 0 against the parquet objects xtcp2 wrote.
  # Validates the "operator queries parquet from inside ClickHouse"
  # deployment shape side-by-side with the kafka path.
  runClickhouseParquetCheck ? false,
  # When true (clickhouse-pipeline flavor), adds the metadata-enrichment
  # end-to-end checks. They assert the best-effort enrichers (pkg/dockermeta,
  # pkg/nicinfo, pkg/lldp) flow onto the flat records and land in ClickHouse:
  #   ENRICH_NIC                 uplink1_nic_driver populated (VM's virtio NIC)
  #   ENRICH_CONTAINER           rows with container_id!='' AND netns!='default'
  #                              (proves docker-netns discovery + Docker labels)
  #   ENRICH_LLDP                best-effort contract: lldpd socket present +
  #                              daemon healthy + records flowing (a lone VM has
  #                              no LLDP peer, so a neighbor can't be asserted)
  #   ENRICH_BESTEFFORT_NEGATIVE a throwaway xtcp2 with the enrichers pointed at
  #                              NONEXISTENT docker/lldpd sockets still starts and
  #                              still produces records (non-fatal degradation)
  runEnrichCheck ? false,
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
  # When true (set on the valkey flavor), adds a check that records the daemon
  # PUBLISHed to the in-VM Valkey channel were actually consumed back by the
  # pre-subscribed valkey-subscriber (broker-server.nix logs them to
  # /run/xtcp2-valkey-sub.out). pub/sub has no retention, so a real subscriber
  # is the only way to prove end-to-end delivery.
  runValkeyCheck ? false,
  valkeyChannel ? "xtcp2-records",
  # When true (set on the nats flavor), adds the analog of the valkey check: the
  # daemon PUBLISHes to a real in-VM NATS subject and the pre-subscribed
  # nats-subscriber consumes the records back.
  runNatsCheck ? false,
  natsSubject ? "xtcp2-records",
  # When true (set on the nsq flavor): the daemon PUBLISHes to a real in-VM nsqd
  # topic and the nsq_tail consumer finishes the messages; the check reads nsqd's
  # per-channel finish_count from /stats (deterministic — no output parsing).
  runNsqCheck ? false,
  nsqTopic ? "xtcp2-records",
  nsqChannel ? "selftest",
  nsqHttpPort ? 4151,
  # When true (set on the minimal/lifecycle flavor, whose daemon writes jsonl
  # to fileOutputPath), the OUTPUT_CONTENT check validates the daemon's
  # serialized file output. Other flavors use non-file dests, so the file
  # wouldn't exist — hence the gate.
  runFileOutputCheck ? false,
  fileOutputPath ? "/var/log/xtcp2.jsonl",
  # When true, exercise both listener surfaces over private Unix sockets with
  # raw bearer authentication, including negative unauthenticated requests.
  runUdsSecurityCheck ? false,
  udsGrpcSocket ? "/run/xtcp2/grpc.sock",
  udsPromSocket ? "/run/xtcp2/prometheus.sock",
  udsAuthToken ? "",
  # When true (socket-sink flavors), the RAW_SOCKET check validates the records
  # xtcp2 streamed over the raw socket destination (tcp/udp/unix/unixgram) to
  # the in-VM ncat receiver (written to socketSinkFile), and the per-scheme
  # send-side Writes counter (socketSinkMetricFn/Var).
  runRawSocketCheck ? false,
  socketSinkScheme ? "tcp",
  socketSinkFile ? "/tmp/xtcp2-socket-sink.out",
  socketSinkMetricFn ? "destTCP",
  socketSinkMetricVar ? "Writes",
  # When true (clickhouse-pipeline flavor), runs the idiag-extprobe binary against
  # the VM's real kernel to prove inet_diag_req_v2.idiag_ext gates which
  # INET_DIAG_* attributes come back — the integration counterpart to the host
  # IDiagExtFromEnabled unit tests. In particular it proves the daemon's default
  # request (idiag_ext=254) yields NO MEMINFO attribute, confirming the dropped
  # meminfo is not merely un-parsed but never sent by the kernel.
  runNlProbeCheck ? false,
  # When true (interface-naming flavor), Check 5f validates that xtcp2 stamped
  # the correct interface names: a record whose socket was SO_BINDTODEVICE-bound
  # to ifnameBound carries that name in enrich_socket_interface_name, and a record
  # whose destination routes out ifnameEgress carries that name in
  # enrich_socket_dest_egress_ifname. Reads the daemon jsonl at fileOutputPath.
  runInterfaceNamingCheck ? false,
  ifnameBound ? "",
  ifnameEgress ? "",
  # When true (interface-naming flavor), Check 5g validates ASN enrichment end
  # to end: the xtcp2-asn-collector unit builds asnDbPath from a loopback feed,
  # xtcp2 (-enrichAsn) loads it, and the xtcp2-asn-dialer unit holds a TCP
  # connection to asnDialTarget:asnDialPort. A jsonl record for that socket must
  # carry enrich_socket_dest_asn == asnExpectedAsn, network_owner ==
  # asnExpectedOwner and LOCALITY_REMOTE, and the daemon's
  # xtcp_gauges{function="loadAsn",variable="prefixes"} must equal
  # asnExpectedPrefixes (the number of prefixes in the fixture feed). Reads the
  # daemon jsonl at fileOutputPath and /metrics on promPort.
  runAsnCheck ? false,
  asnDbPath ? "",
  asnCachePath ? "",
  asnDialTarget ? "8.8.8.8",
  asnDialPort ? 53,
  asnExpectedAsn ? "15169",
  asnExpectedOwner ? "google",
  asnExpectedPrefixes ? "3",
  # When true (interface-naming flavor, run with -localityEvents and a 10m
  # -localityRefreshInterval), the LOCALITY_EVENTS check adds an address to
  # localityEventsIf after the daemon is up and holds a TCP connection to it:
  # its record must classify LOCALITY_SELF within seconds, which only the
  # rtnetlink listener can explain, not the timed re-dump.
  runLocalityEventsCheck ? false,
  localityEventsIf ? "",
  # When true (ipmeta-bootstrap flavor), validates the cold-start bootstrap
  # lookup, delayed in-VM HTTP source recovery, cache rotation, and rejection of
  # a later empty/bad update while xtcp2 keeps enriching from the last good
  # table.
  runIpmetaBootstrapCheck ? false,
  runNsInspectCheck ? true,
  # Check 20 (every flavor): pkg/rtnlmon's listener against the real kernel,
  # in a namespace of its own; see the check for what it asserts.
  runRtnlEventsCheck ? true,
  # No ipmetaDbPath argument here, deliberately. mkVm.nix has a let binding of
  # that name and passes the artifact path to xtcp2 itself, as -asnDbPath in its
  # own argv, so this runner never needed it: the argument was declared, given a
  # default, and read by nothing. That is what deadnix found. ipmetaCachePath
  # below looks like a twin and is not — it IS read.
  ipmetaCachePath ? "",
  ipmetaBadMarker ? "",
  ipmetaDialTarget ? "203.0.113.42",
  ipmetaDialPort ? 53,
  ipmetaBootstrapExpectedAsn ? "64500",
  ipmetaBootstrapExpectedOwner ? "fixture-net",
  ipmetaLiveExpectedAsn ? "15169",
  ipmetaLiveExpectedOwner ? "google",
  ipmetaExpectedPrefixes ? "1",
  # The record epoch SCHEMA_VERSION asserts. Read from the Go constant the
  # daemon stamps, so the check cannot drift from it; override only to prove
  # the check fails on a wrong epoch.
  schemaVersion ? (import ../lib/xtcp2-schema-version.nix { inherit lib; }).current,
  # Multiplier applied to every retry budget in the script below. See the
  # "Retry-budget scaling" block in `text` for the reasoning; in short these
  # are poll-until-true loops, so a higher ceiling costs nothing when a check
  # passes and only slows down reporting a genuine failure. 4 matches the
  # measured stretch on a ~2x-oversubscribed host with a 2-vCPU guest. The
  # guest env var XTCP2_SELF_TEST_WAIT_SCALE overrides this at runtime.
  # Keep mkLifecycleFullTest's stallSec above (longest single check) x this.
  waitScale ? 4,
}:

let
  sv = toString schemaVersion;
in
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
    docker # only used by Check 11/12 (clickhouse-pipeline); harmless otherwise
    minio-client # mc — only used by Check 13/14 (s3parquet); harmless otherwise
    duckdb # used by Check 14 to decode the Parquet file
    jq # used by Check 19 to edit the config JSON for the reconfigure soft restart
  ];
  text = ''
    set +e   # never exit early — we want all checks to run

    # writeShellApplication PREPENDS runtimeInputs to PATH (inheritPath
    # defaults to true — see pkgs/build-support/trivial-builders/default.nix
    # in nixpkgs); it does NOT clamp PATH to runtimeInputs, as an earlier
    # version of this comment claimed. What is genuinely unreliable is the
    # inherited half: the cmd binaries mkVm.nix installs via
    # environment.systemPackages (xtcp2, xtcp2client, ns, nsTest, …) live
    # only on the NixOS system path, which a systemd unit's default PATH is
    # not guaranteed to carry. Prepend it explicitly so checks 4–7 resolve
    # them whatever the caller's environment.
    export PATH="/run/current-system/sw/bin:$PATH"

    overall_ok=1

    # ─── Retry-budget scaling ──────────────────────────────────────────────
    # Every check below polls for a condition with a fixed iteration count and
    # a 1 s (sometimes 2 s) sleep. Those counts were sized on an idle host. On
    # a loaded one the guest's 2 vCPUs get descheduled hard and a check that
    # normally settles in 3 s can take 30+, so a fixed budget turns "slow" into
    # a spurious FAIL — which, unlike a host-side timeout, looks exactly like a
    # real product defect and cannot be distinguished by the runner.
    #
    # WAIT_SCALE multiplies every budget by one number instead of hand-tuning
    # ~22 separate counts. It defaults to the build-time waitScale parameter
    # and can be overridden per-boot with XTCP2_SELF_TEST_WAIT_SCALE.
    #
    # Scale the ITERATION COUNT, not the sleep: these are poll-until-true
    # loops, so a bigger ceiling does NOT slow down a check that passes — the
    # loop still exits the moment the condition holds. The only thing a larger
    # scale costs is that a check which genuinely fails takes proportionally
    # longer to say so, and the host runner's stall watchdog bounds that.
    # That asymmetry is why the default errs high: a slow PASS misreported as
    # a FAIL is far more expensive than a slow FAIL.
    WAIT_SCALE="''${XTCP2_SELF_TEST_WAIT_SCALE:-${toString waitScale}}"
    case "$WAIT_SCALE" in
      "" | *[!0-9]*) WAIT_SCALE=1 ;;
    esac
    if [ "$WAIT_SCALE" -lt 1 ]; then
      WAIT_SCALE=1
    fi
    #
    # A function rather than a set of precomputed W<n> variables: the checks
    # below are assembled conditionally per flavor, so any fixed set of
    # variables is genuinely unused in some builds and shellcheck rejects it
    # (SC2034) at writeShellApplication build time.
    scaled() { echo "$(($1 * WAIT_SCALE))"; }
    #
    # Wall-clock `timeout`s scale too, through scaled(). A streaming
    # xtcp2client never ends by itself, so it runs under stream_until instead
    # of `timeout`: stopped at its first record, with the scaled value only as
    # a ceiling. A pass costs the time to the first record; only a failing
    # stream waits out the budget. (nix/microvms/self-test-stream-until.nix)
    ${import ./self-test-stream-until.nix}
    #
    # Checks 5d and 5f validate jsonl in one jq pass, not one jq per line,
    # whose cost grew with the run's length. (nix/microvms/self-test-jsonl-tally.nix)
    # Only defined in flavors that run one of them: shellcheck rejects a
    # function that is never invoked (SC2329), so defining it everywhere broke
    # the build of every other flavor.
    ${lib.optionalString (runFileOutputCheck || runRawSocketCheck) (import ./self-test-jsonl-tally.nix)}
    #
    # The netns checks (8, 9, 10, 16) wait for a counter against a
    # wall-clock budget, not a fixed sleep or an iteration count.
    # (nix/microvms/self-test-wait-metric.nix)
    ${import ./self-test-wait-metric.nix}

    uds_security_mode=${if runUdsSecurityCheck then "1" else "0"}

    echo "================================================"
    echo " xtcp2 microvm self-test"
    echo " kernel: $(uname -r)"
    echo " host:   $(uname -n)"
    echo " wait-scale: ''${WAIT_SCALE}x"
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
      metric_curl_args=()
      metric_url="http://127.0.0.1:${toString promPort}/metrics"
      if [ "$uds_security_mode" -eq 1 ]; then
        metric_curl_args=(--unix-socket "${udsPromSocket}" -H "Authorization: Bearer ${udsAuthToken}")
        metric_url="http://xtcp2/metrics"
      fi
      curl --silent --fail --max-time 2 "''${metric_curl_args[@]}" \
           "$metric_url" \
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
    for i in $(seq 1 "$(scaled 30)"); do
      if systemctl is-active --quiet xtcp2; then
        echo "XTCP2_SELF_TEST_SYSTEMD_PASS  (active after ''${i}s)"
        check1=0
        break
      fi
      sleep 1
    done
    if [ "$check1" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_SYSTEMD_FAIL  (not active after $(scaled 30)s)"
      systemctl status xtcp2 --no-pager || true
      overall_ok=0
    fi

    # ─── Check 2: Prometheus /metrics endpoint reachable ──────────────────
    echo "--- check 2: GET http://127.0.0.1:${toString promPort}/metrics ---"
    check2=1
    if [ "$uds_security_mode" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_METRICS_PASS  (covered by authenticated UDS check)"
      check2=0
    else
      for i in $(seq 1 "$(scaled 30)"); do
        if curl --silent --fail --max-time 2 \
             "http://127.0.0.1:${toString promPort}/metrics" \
             | grep -q '^xtcp_'; then
          echo "XTCP2_SELF_TEST_METRICS_PASS  (after ''${i}s)"
          check2=0
          break
        fi
        sleep 1
      done
    fi
    if [ "$check2" -ne 0 ]; then
      echo "XTCP2_SELF_TEST_METRICS_FAIL  (no xtcp2_* metric exposed in $(scaled 30)s)"
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
    for _ in $(seq 1 "$(scaled 20)"); do
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
      echo "XTCP2_SELF_TEST_NETLINK_FAIL  (no inet_diag socket parsed in $(scaled 20)s)"
      overall_ok=0
    fi
    kill "$listener_pid" "$client_pid" 2>/dev/null || true
    wait 2>/dev/null || true

    # ─── Check 4: every cmd binary's -help works ──────────────────────────
    echo "--- check 4: -help smoke on every cmd binary ---"
    # A subset of nix/binaries.nix's binaryNames, deliberately: this runs in a
    # booted guest, so the list is what is expected on PATH via xtcp2-all.
    # ipfeed-collector is the one name in binaryNames that is absent here, and
    # has been since before goip; it is left absent rather than added blind,
    # because adding a name whose -help nobody has run in a guest would turn a
    # gap in coverage into a red self-test.
    binaries=(
      xtcp2
      xtcp2client
      xtcp2ctl
      xtcp2_kafka_client
      clickhouse_protobuflist
      clickhouse_protobuflist_db
      clickhouse_http_insert_protobuflist
      kafka_to_clickhouse
      ns
      nsTest
      register_schema
      goip
      goip-parity
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
      # Derived, not written: the count was hardcoded at 11 and the plan's
      # verification item 8 listed keeping it in sync with two Nix lists as a
      # manual step. ''${#binaries[@]} removes the third place to forget.
      echo "XTCP2_SELF_TEST_BINARIES_HELP_PASS  (''${#binaries[@]} binaries OK)"
    else
      echo "XTCP2_SELF_TEST_BINARIES_HELP_FAIL  (failed:$failed_help)"
      overall_ok=0
    fi

    # ─── Check 5: xtcp2 ↔ xtcp2client gRPC roundtrip ──────────────────────
    echo "--- check 5: xtcp2client connects to xtcp2 gRPC (port ${toString grpcPort}) ---"
    check5=1
    if [ "$uds_security_mode" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_GRPC_ROUNDTRIP_PASS  (covered by authenticated UDS check)"
      check5=0
    elif command -v xtcp2client >/dev/null 2>&1; then
      # Run xtcp2client briefly. Exit code 0 or 124 (timeout) both acceptable
      # for "it connected"; anything else is a wire/handshake failure.
      # xtcp2client takes -target (host) + -port (numeric), not -addr.
      stream_until "$(scaled 3)" /tmp/xtcp2client.log . \
        xtcp2client -target 127.0.0.1 -port "${toString grpcPort}"
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

    # ─── Check 5b: xtcp2client -poll streams live flat records ────────────
    # Regression guard for the PollFlatRecords bidi stream. This stream
    # historically delivered ZERO records to `-poll` clients — the reason
    # CTL_RESTART's Check 19 verifies the soft restart via `get` rather than
    # by streaming. Assert the client prints at least one record AND the
    # daemon's pfr broadcast counter (flatRecordServiceSend/pfrSent, on the
    # main xtcp_counts vec) actually advanced — belt-and-suspenders, like the
    # broker consume checks. The daemon self-polls every -frequency (5s in the
    # lifecycle config) so records flow regardless of client sends; a short
    # -pollFrequency also pokes on-demand polls. Coverage-safe (no re-exec).
    echo "--- check 5b: xtcp2client -poll streams records (port ${toString grpcPort}) ---"
    check5b=1
    if [ "$uds_security_mode" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_POLL_STREAM_PASS  (covered by authenticated UDS check)"
      check5b=0
    elif command -v xtcp2client >/dev/null 2>&1; then
      before_pfr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="pfrSent"')
      stream_until "$(scaled 12)" /tmp/xtcp2poll.log '^\{' \
        xtcp2client -poll -pollFrequency 2s -format json \
        -target 127.0.0.1 -port "${toString grpcPort}"
      rc=$?
      after_pfr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="pfrSent"')
      # Records are single-line JSON objects (printer.record); client debug
      # logs carry a "YYYY/MM/DD" timestamp prefix, so match only '{'-leading
      # lines to count records without counting logs.
      recs=$(grep -c '^{' /tmp/xtcp2poll.log 2>/dev/null || true)
      recs=''${recs:-0}
      if { [ "$rc" -eq 0 ] || [ "$rc" -eq 124 ]; } \
         && [ "$recs" -ge 1 ] 2>/dev/null \
         && [ "$after_pfr" -gt "$before_pfr" ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_POLL_STREAM_PASS  (records=$recs, pfrSent $before_pfr->$after_pfr, rc=$rc)"
        check5b=0
      else
        echo "XTCP2_SELF_TEST_POLL_STREAM_FAIL  (records=$recs, pfrSent $before_pfr->$after_pfr, rc=$rc)"
        echo "--- xtcp2poll.log (head) ---"
        head -n 15 /tmp/xtcp2poll.log 2>/dev/null || true
      fi
    else
      echo "XTCP2_SELF_TEST_POLL_STREAM_FAIL  (xtcp2client not on PATH)"
    fi
    if [ "$check5b" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 5c: health/readiness endpoints + -healthcheck mode ─────────
    # Validates the container-orchestration surface (pkg/health HTTP handlers +
    # the -healthcheck self-probe used by the scratch-image Docker HEALTHCHECK,
    # cmd/xtcp2 runHealthcheck). None of these were exercised before. The
    # daemon is polling by now (Check 1 confirmed active), so /readyz is 200.
    echo "--- check 5c: health endpoints (/healthz, /readyz) + -healthcheck ---"
    check5c=1
    if [ "$uds_security_mode" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_HEALTH_PASS  (covered by authenticated UDS check)"
      check5c=0
    else
      hz=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
        "http://127.0.0.1:${toString promPort}/healthz" 2>/dev/null || echo 000)
      rz=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
        "http://127.0.0.1:${toString promPort}/readyz" 2>/dev/null || echo 000)
    # -healthcheck probes /readyz itself and exits 0 (ready) / 1 (not); it does
    # NOT start the daemon. Uses -promListen to find the port.
    hc_rc=1
    if command -v xtcp2 >/dev/null 2>&1; then
      xtcp2 -healthcheck -promListen "127.0.0.1:${toString promPort}" \
        >/tmp/xtcp2-healthcheck.log 2>&1
      hc_rc=$?
    fi
    if [ "$hz" = "200" ] && [ "$rz" = "200" ] && [ "$hc_rc" -eq 0 ]; then
      echo "XTCP2_SELF_TEST_HEALTH_PASS  (healthz=$hz readyz=$rz healthcheck_rc=$hc_rc)"
      check5c=0
    else
      echo "XTCP2_SELF_TEST_HEALTH_FAIL  (healthz=$hz readyz=$rz healthcheck_rc=$hc_rc)"
      head -n 5 /tmp/xtcp2-healthcheck.log 2>/dev/null || true
    fi
    fi
    if [ "$check5c" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 5d: daemon jsonl file-dest output content ──────────────────
    # (minimal flavor only — its daemon writes jsonl to fileOutputPath). Every
    # existing check asserts counters; this validates the daemon's SERIALIZED
    # output (jsonl marshaller → file destination → record field population).
    ${lib.optionalString runFileOutputCheck ''
      echo "--- check 5d: daemon jsonl output content (${fileOutputPath}) ---"
      check5d=1
      # Wait for the file to appear + accrue at least one record.
      for _ in $(seq 1 "$(scaled 20)"); do
        if [ -s "${fileOutputPath}" ]; then break; fi
        sleep 1
      done
      # Snapshot, then validate only COMPLETE lines: `wc -l` counts newline-
      # terminated lines, so a partial trailing line the daemon is mid-appending
      # is excluded by `head -n $total` — no read/write race.
      cp "${fileOutputPath}" /tmp/xtcp2-out.snap 2>/dev/null || true
      total=$(wc -l < /tmp/xtcp2-out.snap 2>/dev/null || echo 0)
      total=''${total:-0}
      # One jq pass: the file holds every record since boot, so a per-line jq
      # cost more the longer the run (325 s for 788 records at load ~45).
      good=
      bad=
      read -r good bad < <(head -n "$total" /tmp/xtcp2-out.snap 2>/dev/null | jsonl_tally)
      good=''${good:-0}
      bad=''${bad:-0}
      if [ "$good" -ge 1 ] && [ "$bad" -eq 0 ]; then
        echo "XTCP2_SELF_TEST_OUTPUT_CONTENT_PASS  (jsonl records=$good, malformed=$bad)"
        check5d=0
      else
        echo "XTCP2_SELF_TEST_OUTPUT_CONTENT_FAIL  (jsonl records=$good, malformed=$bad, lines=$total)"
        head -n 3 /tmp/xtcp2-out.snap 2>/dev/null || echo "(no file)"
      fi
      if [ "$check5d" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 5e: xtcp2client listen mode streams records ────────────────
    # Parallel to POLL_STREAM (5b) but for FlatRecords server-push (listen)
    # mode — no -poll. The daemon broadcasts each polled record to registered
    # listen clients; assert >=1 record printed AND the frSent counter grew.
    # Check 5 only asserts "some output" (includes the client's own logs).
    echo "--- check 5e: xtcp2client listen mode streams records ---"
    check5e=1
    if [ "$uds_security_mode" -eq 1 ]; then
      echo "XTCP2_SELF_TEST_LISTEN_STREAM_PASS  (covered by authenticated UDS check)"
      check5e=0
    elif command -v xtcp2client >/dev/null 2>&1; then
      before_fr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="frSent"')
      stream_until "$(scaled 12)" /tmp/xtcp2listen.log '^\{' \
        xtcp2client -format json \
        -target 127.0.0.1 -port "${toString grpcPort}"
      rc=$?
      after_fr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="frSent"')
      recs=$(grep -c '^{' /tmp/xtcp2listen.log 2>/dev/null || true)
      recs=''${recs:-0}
      if { [ "$rc" -eq 0 ] || [ "$rc" -eq 124 ]; } \
         && [ "$recs" -ge 1 ] 2>/dev/null \
         && [ "$after_fr" -gt "$before_fr" ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_LISTEN_STREAM_PASS  (records=$recs, frSent $before_fr->$after_fr, rc=$rc)"
        check5e=0
      else
        echo "XTCP2_SELF_TEST_LISTEN_STREAM_FAIL  (records=$recs, frSent $before_fr->$after_fr, rc=$rc)"
        head -n 15 /tmp/xtcp2listen.log 2>/dev/null || true
      fi
    else
      echo "XTCP2_SELF_TEST_LISTEN_STREAM_FAIL  (xtcp2client not on PATH)"
    fi
    if [ "$check5e" -ne 0 ]; then overall_ok=0; fi

    ${lib.optionalString runUdsSecurityCheck ''
      # ─── Check 5u: authenticated gRPC + HTTP UDS integration ───────────
      # Exercise the shipped clients and curl in the guest so socket binding,
      # permissions, auth metadata/header handling, ConfigService control, and
      # both stream RPCs are covered without a TCP fallback.
      echo "--- check 5u: authenticated gRPC/HTTP Unix sockets ---"
      check5u=1
      uds_sockets=0
      grpc_mode=0
      prom_mode=0
      if [ -S "${udsGrpcSocket}" ] && [ -S "${udsPromSocket}" ]; then
        grpc_mode=$(stat -c '%a' "${udsGrpcSocket}" 2>/dev/null || echo 0)
        prom_mode=$(stat -c '%a' "${udsPromSocket}" 2>/dev/null || echo 0)
        if [ "$grpc_mode" = "600" ] && [ "$prom_mode" = "600" ]; then
          uds_sockets=1
        fi
      fi

      prom_unauth=$(curl --silent --output /dev/null --write-out '%{http_code}' \
        --max-time 3 --unix-socket "${udsPromSocket}" \
        "http://xtcp2/metrics" 2>/dev/null || echo 000)
      prom_body=$(mktemp)
      prom_auth_rc=1
      if curl --silent --fail --max-time 3 --unix-socket "${udsPromSocket}" \
        -H "Authorization: Bearer ${udsAuthToken}" \
        "http://xtcp2/metrics" >"$prom_body" 2>/dev/null; then
        prom_auth_rc=0
      fi
      prom_metrics=0
      if grep -q '^xtcp_' "$prom_body" 2>/dev/null; then
        prom_metrics=1
      fi
      rm -f "$prom_body"

      health_code=$(curl --silent --output /dev/null --write-out '%{http_code}' \
        --max-time 3 --unix-socket "${udsPromSocket}" \
        -H "Authorization: Bearer ${udsAuthToken}" \
        "http://xtcp2/healthz" 2>/dev/null || echo 000)
      ready_code=$(curl --silent --output /dev/null --write-out '%{http_code}' \
        --max-time 3 --unix-socket "${udsPromSocket}" \
        -H "Authorization: Bearer ${udsAuthToken}" \
        "http://xtcp2/readyz" 2>/dev/null || echo 000)

      ctl_base=(-network unix -unixSocket "${udsGrpcSocket}")
      ctl_bad=0
      if xtcp2ctl get "''${ctl_base[@]}" >/tmp/uds-ctl-bad.log 2>&1; then
        ctl_bad=1
      fi
      xtcp2ctl get "''${ctl_base[@]}" -auth-token "${udsAuthToken}" \
        >/tmp/uds-ctl-get.log 2>/tmp/uds-ctl-get.err
      ctl_get_rc=$?
      ctl_set_rc=1
      if [ "$ctl_get_rc" -eq 0 ]; then
        # -timeout 1s: the daemon's poll_timeout setting, not scaled.
        xtcp2ctl set-poll-frequency -frequency 7s -timeout 1s \
          "''${ctl_base[@]}" -auth-token "${udsAuthToken}" \
          >/tmp/uds-ctl-set.log 2>&1
        ctl_set_rc=$?
      fi
      ctl_changed=0
      if [ "$ctl_set_rc" -eq 0 ] && xtcp2ctl get "''${ctl_base[@]}" \
        -auth-token "${udsAuthToken}" 2>/dev/null | grep -q '"7s"'; then
        ctl_changed=1
      fi
      xtcp2ctl trigger-poll "''${ctl_base[@]}" -auth-token "${udsAuthToken}" \
        >/tmp/uds-ctl-trigger.log 2>&1
      trigger_rc=$?

      before_pfr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="pfrSent"')
      stream_until "$(scaled 12)" /tmp/uds-poll.log '^\{' \
        xtcp2client -network unix -unixSocket "${udsGrpcSocket}" \
        -auth-token "${udsAuthToken}" -poll -pollFrequency 2s -format json
      poll_rc=$?
      after_pfr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="pfrSent"')
      poll_records=$(grep -c '^{' /tmp/uds-poll.log 2>/dev/null || true)
      poll_records=''${poll_records:-0}

      before_fr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="frSent"')
      stream_until "$(scaled 12)" /tmp/uds-listen.log '^\{' \
        xtcp2client -network unix -unixSocket "${udsGrpcSocket}" \
        -auth-token "${udsAuthToken}" -format json
      listen_rc=$?
      after_fr=$(metric_value "xtcp_counts" 'function="flatRecordServiceSend"' 'variable="frSent"')
      listen_records=$(grep -c '^{' /tmp/uds-listen.log 2>/dev/null || true)
      listen_records=''${listen_records:-0}

      if [ "$uds_sockets" -eq 1 ] && [ "$prom_unauth" = "401" ] \
        && [ "$prom_auth_rc" -eq 0 ] && [ "$prom_metrics" -eq 1 ] \
        && [ "$health_code" = "200" ] && [ "$ready_code" = "200" ] \
        && [ "$ctl_bad" -eq 0 ] && [ "$ctl_get_rc" -eq 0 ] \
        && [ "$ctl_changed" -eq 1 ] && [ "$trigger_rc" -eq 0 ] \
        && { [ "$poll_rc" -eq 0 ] || [ "$poll_rc" -eq 124 ]; } \
        && [ "$poll_records" -ge 1 ] 2>/dev/null \
        && [ "$after_pfr" -gt "$before_pfr" ] 2>/dev/null \
        && { [ "$listen_rc" -eq 0 ] || [ "$listen_rc" -eq 124 ]; } \
        && [ "$listen_records" -ge 1 ] 2>/dev/null \
        && [ "$after_fr" -gt "$before_fr" ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_UDS_SECURITY_PASS  (modes=''${grpc_mode}/''${prom_mode}, metrics=''${prom_unauth}->200, poll_records=$poll_records, listen_records=$listen_records)"
        check5u=0
      else
        echo "XTCP2_SELF_TEST_UDS_SECURITY_FAIL  (sockets=$uds_sockets modes=''${grpc_mode}/''${prom_mode} unauth=$prom_unauth metrics_rc=$prom_auth_rc health=$health_code ready=$ready_code ctl_bad=$ctl_bad get=$ctl_get_rc set=$ctl_set_rc changed=$ctl_changed trigger=$trigger_rc poll=$poll_records listen=$listen_records)"
        echo "--- xtcp2 ExecStart ---"
        systemctl show -p ExecStart xtcp2 2>/dev/null || true
        echo "--- /run/xtcp2 ---"
        ls -la /run/xtcp2 2>/dev/null || true
        echo "--- listening unix sockets ---"
        ss -lx 2>/dev/null || true
        echo "--- xtcp2 journal ---"
        journalctl -u xtcp2 -n 30 --no-pager 2>/dev/null || true
        head -n 8 /tmp/uds-ctl-bad.log /tmp/uds-ctl-get.err /tmp/uds-ctl-set.log /tmp/uds-poll.log /tmp/uds-listen.log 2>/dev/null || true
      fi
      if [ "$check5u" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 5f: interface-naming enrichment content ────────────────────
    # (interface-naming flavor only.) The in-VM generator holds ESTABLISHED
    # sockets open: one SO_BINDTODEVICE-bound to ${ifnameBound} (dialing a peer
    # reachable on that veth), and one unbound dialing a peer whose route egresses
    # ${ifnameEgress}. xtcp2 (-enrichLocality) must stamp those interface names.
    # The two records must be distinguishable, not just present somewhere:
    #   bound  — enrichSocketInterfaceName == ${ifnameBound} AND the kernel's own
    #            idiag_if (inetDiagMsgSocketInterface) is set, proving the name
    #            was resolved from the socket's bound ifindex, not a route;
    #   egress — enrichSocketDestEgressIfname == ${ifnameEgress} on a record with
    #            NO bound-interface name (the unbound client) whose destination
    #            classified as LOCALITY_LOCAL_SUBNET (peer on the connected /24).
    # We poll the jsonl because the locality snapshot is discovered on the
    # reconcile path a few seconds after the veths come up. Runs after 5e so the
    # generators have had the longest possible time to come up.
    # enricherCompiledIn <name> — reads the daemon's
    # xtcp_gauges{function="InitPromethus",variable="compiledInEnrichers",type="<name>"}
    # series, which is 1 when the binary carries that enricher's code and 0
    # when it was built without the `enrich_<name>` tag. Checks 5f and 5g both
    # gate on it: without this, running them against a slim image fails with
    # "no record carried the column", which reads like a daemon bug rather
    # than the wrong artifact. Prints nothing on success.
    #
    # Emitted only when one of those two checks is actually enabled. Defining it
    # unconditionally leaves a function with no caller in every other flavor's
    # script, which shellcheck flags as SC2329.
    ${lib.optionalString (runInterfaceNamingCheck || runAsnCheck) ''
      enricherCompiledIn() {
        local want="$1" line
        line=$(curl -sf "http://127.0.0.1:${toString promPort}/metrics" 2>/dev/null \
          | grep -E "^xtcp_gauges\{[^}]*variable=\"compiledInEnrichers\"[^}]*type=\"$want\"[^}]*\} " \
          | tail -1)
        if [ -z "$line" ]; then
          echo "  enricher '$want': compiledInEnrichers gauge absent (pre-gauge daemon?) — proceeding"
          return 0
        fi
        case "''${line##* }" in
          1|1.0|1.0e+00) return 0 ;;
          *) return 1 ;;
        esac
      }
    ''}

    ${lib.optionalString runInterfaceNamingCheck ''
      echo "--- check 5f: interface-naming (bound=${ifnameBound} egress=${ifnameEgress}) ---"
      check5f=1
      if ! enricherCompiledIn locality; then
        echo "XTCP2_SELF_TEST_IFNAME_FAIL  (the locality enricher is NOT compiled into this binary;"
        echo "  this flavor needs an -enrich / -locality image, i.e. a build with -tags enrich_locality)"
        overall_ok=0
      fi
      boundOk=0
      egressOk=0
      for _ in $(seq 1 "$(scaled 60)"); do
        if [ -s "${fileOutputPath}" ]; then
          cp "${fileOutputPath}" /tmp/xtcp2-ifname.snap 2>/dev/null || true
          if [ "$boundOk" -eq 0 ] && \
            jq -e --arg n "${ifnameBound}" \
              'select(.enrichSocketInterfaceName == $n
                      and .inetDiagMsgSocketInterface != null)' \
              /tmp/xtcp2-ifname.snap >/dev/null 2>&1; then
            boundOk=1
          fi
          if [ "$egressOk" -eq 0 ] && \
            jq -e --arg n "${ifnameEgress}" \
              'select(.enrichSocketDestEgressIfname == $n
                      and (.enrichSocketInterfaceName // "") == ""
                      and .enrichSocketDestLocality == "LOCALITY_LOCAL_SUBNET")' \
              /tmp/xtcp2-ifname.snap >/dev/null 2>&1; then
            egressOk=1
          fi
          if [ "$boundOk" -eq 1 ] && [ "$egressOk" -eq 1 ]; then break; fi
        fi
        sleep 2
      done
      if [ "$boundOk" -eq 1 ] && [ "$egressOk" -eq 1 ]; then
        echo "XTCP2_SELF_TEST_IFNAME_PASS  (bound=${ifnameBound} egress=${ifnameEgress})"
        check5f=0
      else
        echo "XTCP2_SELF_TEST_IFNAME_FAIL  (bound_seen=$boundOk egress_seen=$egressOk)"
        echo "--- sample records carrying interface fields ---"
        jq -c 'select(.enrichSocketInterfaceName != null or .enrichSocketDestEgressIfname != null)
               | {i: .enrichSocketInterfaceName, b: .inetDiagMsgSocketInterface,
                  e: .enrichSocketDestEgressIfname, l: .enrichSocketDestLocality}' \
          /tmp/xtcp2-ifname.snap 2>/dev/null | head -5 || true
      fi
      if [ "$check5f" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 5g: ASN enrichment content ──────────────────────────────────
    # (interface-naming flavor only.) The xtcp2-asn-dialer unit keeps a TCP
    # connection to ${asnDialTarget}:${toString asnDialPort} alive; the
    # xtcp2-asn-collector unit builds ${asnDbPath} (real ipfeed-collector against
    # a loopback goog.json fixture) AFTER xtcp2 started, so the daemon has to
    # pick the artifact up on its -asnRefreshInterval tick. We want the record
    # for that exact socket (destination bytes == the dial target, dport == the
    # dial port) to carry the representative ASN, the network owner, and a
    # REMOTE locality (8.8.8.8 is behind the default gateway). enrich_socket_
    # dest_asn is a uint64, which protojson renders as a JSON *string*.
    ${lib.optionalString runAsnCheck ''
      echo "--- check 5g: asn enrichment (dest=${asnDialTarget}:${toString asnDialPort} want asn=${asnExpectedAsn} owner=${asnExpectedOwner}) ---"
      check5g=1
      if ! enricherCompiledIn asn; then
        echo "XTCP2_SELF_TEST_ASN_FAIL  (the asn enricher is NOT compiled into this binary;"
        echo "  this flavor needs an -enrich / -asn image, i.e. a build with -tags enrich_asn)"
        overall_ok=0
      fi
      # protojson renders idiag_dst as base64. The daemon copies the kernel's
      # raw __be32[4] (16 bytes) for every family, so a v4 address is the 4
      # octets followed by 12 zero bytes; accept the bare 4-byte form as well
      # so a future trim of v4 addresses does not break the check. Each octet →
      # \0NNN octal escape → raw byte via printf %b.
      dest_raw=$(IFS=. read -r o1 o2 o3 o4 <<<"${asnDialTarget}"; \
        printf '\\0%03o\\0%03o\\0%03o\\0%03o' "$o1" "$o2" "$o3" "$o4")
      dest_b64_4=$(printf '%b' "$dest_raw" | base64)
      dest_b64_16=$({ printf '%b' "$dest_raw"; head -c 12 /dev/zero; } | base64)
      echo "expecting inetDiagMsgSocketDestination=$dest_b64_16 (or $dest_b64_4)"
      artifact_seen=0
      asnOk=0
      for _ in $(seq 1 "$(scaled 60)"); do
        if [ "$artifact_seen" -eq 0 ] && [ -s "${asnDbPath}" ]; then
          artifact_seen=1
          echo "asn artifact present: $(ls -la ${asnDbPath})"
        fi
        if [ -s "${fileOutputPath}" ]; then
          cp "${fileOutputPath}" /tmp/xtcp2-asn.snap 2>/dev/null || true
          if jq -e --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
                --argjson p ${toString asnDialPort} \
                --arg a "${asnExpectedAsn}" --arg o "${asnExpectedOwner}" \
              'select((.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
                      and .inetDiagMsgSocketDestinationPort == $p
                      and .enrichSocketDestAsn == $a
                      and .enrichSocketDestNetworkOwner == $o
                      and .enrichSocketDestLocality == "LOCALITY_REMOTE")' \
              /tmp/xtcp2-asn.snap >/dev/null 2>&1; then
            asnOk=1
            break
          fi
        fi
        sleep 2
      done
      # The lookup table's own metrics: the prefixes gauge must equal the number
      # of prefixes in the fixture feed once the artifact is loaded (a record
      # with the right ASN proves a load happened, so the gauge must be there).
      # client_golang sorts label pairs by name, so match labels individually
      # rather than pinning their order.
      cacheOk=0
      for _ in $(seq 1 "$(scaled 20)"); do
        if [ -s "${asnCachePath}" ] && [ -s "$(dirname "${asnCachePath}")/previous.lookup.parquet.zst" ]; then
          cacheOk=1
          break
        fi
        sleep 2
      done
      fetchRetryOk=0
      collector_journal=$(journalctl --no-pager -o cat -u xtcp2-asn-collector.service 2>/dev/null || true)
      if grep -Eq '"msg":"source ok".*"attempts":([2-9]|[1-9][0-9]+)' <<<"$collector_journal"; then
        fetchRetryOk=1
      fi
      echo "--- daemon loadAsn / refreshAsn metrics ---"
      asn_metrics=$(curl -sf "http://127.0.0.1:${toString promPort}/metrics" 2>/dev/null || true)
      grep -E 'loadAsn|refreshAsn|initAsnEnricher' <<<"$asn_metrics" || echo "(no asn metrics yet)"
      asn_prefixes=$(grep -E '^xtcp_gauges\{[^}]*function="loadAsn"[^}]*variable="prefixes"[^}]*\} ' <<<"$asn_metrics" \
        | awk '{print $2}' | head -1)
      prefixesOk=0
      if [ "$asn_prefixes" = "${asnExpectedPrefixes}" ]; then prefixesOk=1; fi
      if [ "$asnOk" -eq 1 ] && [ "$prefixesOk" -eq 1 ] && [ "$cacheOk" -eq 1 ] && [ "$fetchRetryOk" -eq 1 ]; then
        echo "XTCP2_SELF_TEST_ASN_PASS  (dest=${asnDialTarget}:${toString asnDialPort} asn=${asnExpectedAsn} owner=${asnExpectedOwner} prefixes=$asn_prefixes cache=${asnCachePath} fetch_retry=ok)"
        jq -c --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
               'select(.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
               | {state: .inetDiagMsgState, dport: .inetDiagMsgSocketDestinationPort,
                  asn: .enrichSocketDestAsn, owner: .enrichSocketDestNetworkOwner,
                  locality: .enrichSocketDestLocality}' \
          /tmp/xtcp2-asn.snap 2>/dev/null | head -3 || true
        check5g=0
      else
        echo "XTCP2_SELF_TEST_ASN_FAIL  (artifact_seen=$artifact_seen asn_seen=$asnOk prefixes_gauge=''${asn_prefixes:-absent} want=${asnExpectedPrefixes} cache_ok=$cacheOk fetch_retry_ok=$fetchRetryOk cache=${asnCachePath})"
        echo "--- collector retry evidence ---"
        grep -E '"msg":"source ok"|"msg":"fetch failed"|attempts' <<<"$collector_journal" | tail -20 || true
        echo "--- records to the dial target (any enrichment) ---"
        jq -c --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
               'select(.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
               | {state: .inetDiagMsgState, dport: .inetDiagMsgSocketDestinationPort,
                  asn: .enrichSocketDestAsn, owner: .enrichSocketDestNetworkOwner,
                  locality: .enrichSocketDestLocality}' \
          /tmp/xtcp2-asn.snap 2>/dev/null | head -5 || true
        echo "--- live sockets to the dial target (kernel view) ---"
        ss -tn "dst ${asnDialTarget}" 2>&1 | head -10 || true
        echo "--- records to the dial target, count / any record on dport ${toString asnDialPort} ---"
        jq -c --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
               'select(.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)' \
          /tmp/xtcp2-asn.snap 2>/dev/null | wc -l || true
        jq -c --argjson p ${toString asnDialPort} 'select(.inetDiagMsgSocketDestinationPort == $p)
               | {family: .inetDiagMsgFamily, dst: .inetDiagMsgSocketDestination,
                  asn: .enrichSocketDestAsn, locality: .enrichSocketDestLocality}' \
          /tmp/xtcp2-asn.snap 2>/dev/null | head -3 || true
        echo "--- collector / dialer unit state ---"
        systemctl --no-pager status xtcp2-asn-collector.service xtcp2-asn-dialer.service 2>&1 | head -30 || true
        journalctl --no-pager -u xtcp2-asn-dialer.service -n 8 2>&1 || true
      fi
      if [ "$check5g" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check: locality from rtnetlink events ─────────────────────────────
    # (interface-naming flavor only.) The daemon runs -localityEvents with a
    # 10m -localityRefreshInterval, so the timed re-dump cannot run inside this
    # check. 203.0.113.1/24 is added to ${localityEventsIf} only now, after the
    # daemon took its start snapshot, and a TCP connection is held to it: the
    # record must classify LOCALITY_SELF, which needs the listener to have
    # folded the RTM_NEWADDR and published within its 1s debounce.
    ${lib.optionalString runLocalityEventsCheck ''
      echo "--- check: locality events (${localityEventsIf} += 203.0.113.1/24) ---"
      checkLe=1
      le_metrics() { curl -sf "http://127.0.0.1:${toString promPort}/metrics" 2>/dev/null || true; }
      le_start=0
      for _ in $(seq 1 "$(scaled 20)"); do
        le_start=$(le_metrics | grep -E '^xtcp_counts\{[^}]*function="localityListener"[^}]*type="start"[^}]*variable="snapshot"[^}]*\} ' \
          | awk '{print $2}' | head -1)
        [ -n "$le_start" ] && [ "''${le_start%%.*}" -gt 0 ] 2>/dev/null && break
        le_start=0
        sleep 1
      done
      le_ip=203.0.113.1
      le_port=47181
      ip addr add "$le_ip/24" dev ${localityEventsIf}
      # Each nc's stdin is a sleep, which keeps the connection open. $! is only
      # the nc, so the sleeps are killed by name below: a later check's bare
      # `wait` would otherwise block until they end. The odd length is what
      # makes the name unique, and it bounds them if the kill is never reached.
      le_hold=$(( $(scaled 30) + 7 ))
      sleep "$le_hold" | nc -l -p "$le_port" -s "$le_ip" >/dev/null 2>&1 &
      le_srv=$!
      sleep 1
      sleep "$le_hold" | nc "$le_ip" "$le_port" >/dev/null 2>&1 &
      le_cli=$!
      le_b64=$({ printf '\313\000\161\001'; head -c 12 /dev/zero; } | base64)
      le_b64_4=$(printf '\313\000\161\001' | base64)
      leOk=0
      for _ in $(seq 1 "$(scaled 20)"); do
        if [ -s "${fileOutputPath}" ]; then
          cp "${fileOutputPath}" /tmp/xtcp2-le.snap 2>/dev/null || true
          if jq -e --arg d16 "$le_b64" --arg d4 "$le_b64_4" --argjson p "$le_port" \
              'select((.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
                      and .inetDiagMsgSocketDestinationPort == $p
                      and .enrichSocketDestLocality == "LOCALITY_SELF")' \
              /tmp/xtcp2-le.snap >/dev/null 2>&1; then
            leOk=1
            break
          fi
        fi
        sleep 1
      done
      le_live=$(le_metrics | grep -E '^xtcp_gauges\{[^}]*function="localityListener"[^}]*variable="live"[^}]*\} ' \
        | awk '{print $2}' | head -1)
      if [ "$leOk" -eq 1 ] && [ "''${le_start%%.*}" -gt 0 ] 2>/dev/null && [ -n "$le_live" ]; then
        echo "XTCP2_SELF_TEST_LOCALITY_EVENTS_PASS  ($le_ip:$le_port LOCALITY_SELF; start snapshots=$le_start live=$le_live)"
        checkLe=0
      else
        echo "XTCP2_SELF_TEST_LOCALITY_EVENTS_FAIL  (self_seen=$leOk start_snapshots=''${le_start:-absent} live=''${le_live:-absent})"
        echo "--- localityListener metrics ---"
        le_metrics | grep -E 'localityListener|refreshLocality' || echo "(none)"
        echo "--- records to $le_ip:$le_port ---"
        jq -c --arg d16 "$le_b64" --arg d4 "$le_b64_4" --argjson p "$le_port" \
               'select((.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
                       and .inetDiagMsgSocketDestinationPort == $p)
               | {state: .inetDiagMsgState, locality: .enrichSocketDestLocality}' \
          /tmp/xtcp2-le.snap 2>/dev/null | tail -3 || true
        ss -tn "dst $le_ip" 2>&1 | head -5 || true
      fi
      kill "$le_cli" "$le_srv" 2>/dev/null || true
      pkill -x -f "sleep $le_hold" 2>/dev/null || true
      wait "$le_cli" "$le_srv" 2>/dev/null || true
      ip addr del "$le_ip/24" dev ${localityEventsIf} 2>/dev/null || true
      if [ "$checkLe" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 5f: raw socket destination → in-VM ncat sink ───────────────
    # ─── Check 5h: IP metadata bootstrap / recovery / bad update ──────────
    ${lib.optionalString runIpmetaBootstrapCheck ''
      echo "--- check 5h: ipmeta bootstrap/recovery/bad-update (dest=${ipmetaDialTarget}:${toString ipmetaDialPort}) ---"
      check5h_bootstrap=1
      check5h_recovery=1
      check5h_bad=1

      dest_raw=$(IFS=. read -r o1 o2 o3 o4 <<<"${ipmetaDialTarget}"; \
        printf '\\0%03o\\0%03o\\0%03o\\0%03o' "$o1" "$o2" "$o3" "$o4")
      dest_b64_4=$(printf '%b' "$dest_raw" | base64)
      dest_b64_16=$({ printf '%b' "$dest_raw"; head -c 12 /dev/zero; } | base64)

      ipmetaRecordMatches() {
        file="$1"
        asn="$2"
        owner="$3"
        jq -e --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
              --argjson p ${toString ipmetaDialPort} \
              --arg a "$asn" --arg o "$owner" \
          'select((.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
                  and .inetDiagMsgSocketDestinationPort == $p
                  and .enrichSocketDestAsn == $a
                  and .enrichSocketDestNetworkOwner == $o)' \
          "$file" >/dev/null 2>&1
      }

      for _ in $(seq 1 "$(scaled 75)"); do
        if [ -s "${fileOutputPath}" ]; then
          cp "${fileOutputPath}" /tmp/xtcp2-ipmeta-bootstrap.snap 2>/dev/null || true
          if ipmetaRecordMatches /tmp/xtcp2-ipmeta-bootstrap.snap "${ipmetaBootstrapExpectedAsn}" "${ipmetaBootstrapExpectedOwner}"; then
            check5h_bootstrap=0
            break
          fi
        fi
        sleep 2
      done
      if [ "$check5h_bootstrap" -eq 0 ]; then
        echo "XTCP2_SELF_TEST_IPMETA_BOOTSTRAP_PASS  (asn=${ipmetaBootstrapExpectedAsn} owner=${ipmetaBootstrapExpectedOwner})"
      else
        echo "XTCP2_SELF_TEST_IPMETA_BOOTSTRAP_FAIL  (no bootstrap-enriched record seen)"
        overall_ok=0
      fi

      cacheOk=0
      fetchRetryOk=0
      liveOk=0
      for _ in $(seq 1 "$(scaled 90)"); do
        if [ -s "${fileOutputPath}" ]; then
          cp "${fileOutputPath}" /tmp/xtcp2-ipmeta-live.snap 2>/dev/null || true
          if ipmetaRecordMatches /tmp/xtcp2-ipmeta-live.snap "${ipmetaLiveExpectedAsn}" "${ipmetaLiveExpectedOwner}"; then
            liveOk=1
          fi
        fi
        if [ -s "${ipmetaCachePath}" ] && [ -s "$(dirname "${ipmetaCachePath}")/previous.lookup.parquet.zst" ]; then
          cacheOk=1
        fi
        collector_journal=$(journalctl --no-pager -o cat -u xtcp2-ipmeta-collector.service 2>/dev/null || true)
        if grep -Eq '"msg":"source ok".*"attempts":([2-9]|[1-9][0-9]+)' <<<"$collector_journal"; then
          fetchRetryOk=1
        fi
        if [ "$liveOk" -eq 1 ] && [ "$cacheOk" -eq 1 ] && [ "$fetchRetryOk" -eq 1 ]; then
          check5h_recovery=0
          break
        fi
        sleep 2
      done
      asn_metrics=$(curl -sf "http://127.0.0.1:${toString promPort}/metrics" 2>/dev/null || true)
      ipmeta_prefixes=$(grep -E '^xtcp_gauges\{[^}]*function="loadAsn"[^}]*variable="prefixes"[^}]*\} ' <<<"$asn_metrics" \
        | awk '{print $2}' | head -1)
      if [ "$check5h_recovery" -eq 0 ] && [ "$ipmeta_prefixes" = "${ipmetaExpectedPrefixes}" ]; then
        echo "XTCP2_SELF_TEST_IPMETA_RECOVERY_PASS  (asn=${ipmetaLiveExpectedAsn} owner=${ipmetaLiveExpectedOwner} prefixes=$ipmeta_prefixes cache=${ipmetaCachePath} fetch_retry=ok)"
      else
        echo "XTCP2_SELF_TEST_IPMETA_RECOVERY_FAIL  (live_ok=$liveOk cache_ok=$cacheOk fetch_retry_ok=$fetchRetryOk prefixes=''${ipmeta_prefixes:-absent} want=${ipmetaExpectedPrefixes})"
        echo "--- ipmeta collector retry evidence ---"
        grep -E '"msg":"source ok"|"msg":"fetch failed"|attempts' <<<"''${collector_journal:-}" | tail -20 || true
        overall_ok=0
      fi

      for _ in $(seq 1 "$(scaled 60)"); do
        if [ -e "${ipmetaBadMarker}" ]; then
          before_lines=0
          if [ -s "${fileOutputPath}" ]; then before_lines=$(wc -l < "${fileOutputPath}" || echo 0); fi
          sleep 12
          if [ -s "${fileOutputPath}" ]; then
            after_start=$((before_lines + 1))
            tail -n +"$after_start" "${fileOutputPath}" > /tmp/xtcp2-ipmeta-after-bad.snap 2>/dev/null || true
            if [ -s /tmp/xtcp2-ipmeta-after-bad.snap ] &&
               ipmetaRecordMatches /tmp/xtcp2-ipmeta-after-bad.snap "${ipmetaLiveExpectedAsn}" "${ipmetaLiveExpectedOwner}"; then
              xtcp_journal=$(journalctl --no-pager -o cat -u xtcp2.service 2>/dev/null || true)
              if grep -Eq 'ASN reload failed \(keeping current table\)|contains no usable prefixes|open parquet' <<<"$xtcp_journal"; then
                check5h_bad=0
              fi
            fi
          fi
          break
        fi
        sleep 2
      done
      if [ "$check5h_bad" -eq 0 ]; then
        echo "XTCP2_SELF_TEST_IPMETA_BAD_UPDATE_PASS  (bad update rejected; later records kept owner=${ipmetaLiveExpectedOwner})"
      else
        echo "XTCP2_SELF_TEST_IPMETA_BAD_UPDATE_FAIL  (bad_marker=$(test -e "${ipmetaBadMarker}" && echo yes || echo no); no post-bad live enrichment/rejection evidence)"
        echo "--- post-bad records ---"
        jq -c --arg d4 "$dest_b64_4" --arg d16 "$dest_b64_16" \
               'select(.inetDiagMsgSocketDestination == $d16 or .inetDiagMsgSocketDestination == $d4)
               | {dport: .inetDiagMsgSocketDestinationPort, asn: .enrichSocketDestAsn, owner: .enrichSocketDestNetworkOwner}' \
          /tmp/xtcp2-ipmeta-after-bad.snap 2>/dev/null | head -5 || true
        overall_ok=0
      fi
    ''}

    # ─── Check 5i: raw socket destination → in-VM ncat sink ───────────────
    # (socket-sink flavors: tcp/udp/unix/unixgram). xtcp2 streams jsonl records
    # over `-dest <scheme>:...` to an ncat receiver; validate the received
    # records carry a non-empty hostname (send + receipt) AND the per-scheme
    # send counter ran. These dests were Go-component-only before this.
    ${lib.optionalString runRawSocketCheck ''
      echo "--- check 5f: raw ${socketSinkScheme} dest → ncat sink (${socketSinkFile}) ---"
      check5f=1
      # Wait for records to arrive at the sink file.
      for _ in $(seq 1 "$(scaled 20)"); do
        if [ -s "${socketSinkFile}" ]; then break; fi
        sleep 1
      done
      # Per-scheme Writes counter (>=1 = the daemon's send side ran). Not a
      # growth check: the daemon has usually already flushed by the time this
      # runs, so require non-zero, not growing.
      writes=$(metric_value "xtcp_counts" 'function="${socketSinkMetricFn}"' 'variable="${socketSinkMetricVar}"')
      # Snapshot, drop a possibly-partial trailing line (ncat mid-write), then
      # extract JSON objects: `grep '{...}'` strips the unix STREAM dest's
      # varint length prefix and is a no-op for the newline-delimited jsonl of
      # tcp/udp/unixgram. jq-validate each carries a non-empty hostname.
      cp "${socketSinkFile}" /tmp/xtcp2-sink.snap 2>/dev/null || true
      total=$(wc -l < /tmp/xtcp2-sink.snap 2>/dev/null || echo 0)
      total=''${total:-0}
      good=
      bad=
      read -r good bad < <(head -n "$total" /tmp/xtcp2-sink.snap 2>/dev/null \
        | grep -aoE '\{.*\}' | jsonl_tally)
      good=''${good:-0}
      bad=''${bad:-0}
      # good>=1 + counter>=1 is the proof; a few `bad` are tolerated (a varint
      # length byte can occasionally look like '{' on the unix stream framing).
      if [ "$good" -ge 1 ] && [ "''${writes:-0}" -ge 1 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_RAW_SOCKET_PASS  (scheme=${socketSinkScheme}, records=$good, malformed=$bad, ${socketSinkMetricFn}/${socketSinkMetricVar}=$writes)"
        check5f=0
      else
        echo "XTCP2_SELF_TEST_RAW_SOCKET_FAIL  (scheme=${socketSinkScheme}, records=$good, malformed=$bad, ${socketSinkMetricFn}/${socketSinkMetricVar}=$writes)"
        head -c 200 /tmp/xtcp2-sink.snap 2>/dev/null || echo "(no sink file)"
      fi
      if [ "$check5f" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 6: ns inspector reads netns state ─────────────────────────
    ${lib.optionalString runNsInspectCheck ''
      echo "--- check 6: ns inspector ---"
      check6=1
      if command -v ns >/dev/null 2>&1; then
        out=$(timeout "$(scaled 5)s" ns -help 2>&1)
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
    ''}

    # ─── Check 7: nsTest runs ────────────────────────────────────────────
    echo "--- check 7: nsTest ---"
    check7=1
    if command -v nsTest >/dev/null 2>&1; then
      out=$(timeout "$(scaled 5)s" nsTest -help 2>&1)
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

    # ─── Check 8: ns lifecycle — a namespace with a live process ─────────
    # Method B discovers namespaces by scanning /proc/<pid>/ns/net on every
    # pre-poll reconcile — there is no /run/netns fsnotify watcher any more.
    # A namespace is only visible while it holds a LIVE PROCESS, so we keep one
    # alive with `sleep` inside the ns; the daemon should enter it and spawn a
    # per-ns netlinker (netNamespaceInstance "start"). When that process exits
    # and the ns is removed, the next reconcile tears it down (the "delete"
    # counter bumps).
    # ─── Netns checks (8, 9, 10, 16): what "torn down" means ──────────────
    # The counters are process-wide, so "delete went up by one" can be met by
    # any namespace's delete, including a late one left over from an earlier
    # check. That is how a holder that outlived check 16's kill still passed.
    # Instead: netNamespaceInstance "start" ticks once per namespace added
    # (pkg/xtcp/ns_net_namespace.go) and "delete" once per namespace removed
    # (pkg/xtcp/ns_delete.go), so start - delete is the number the daemon
    # tracks. Record it before these checks; after each one kills its holder,
    # every namespace started since must be deleted: delete >= start - ns_live0.
    # A failed scrape reads 0, which can only raise that target, so it fails
    # the check rather than passing it.
    ns_live0=$(( $(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"') \
      - $(metric_value "xtcp_counts" 'function="delete"' 'variable="delete"') ))
    echo "netns baseline: $ns_live0 namespace(s) tracked before check 8"

    # ns_wait_all_deleted: wait for delete >= start - ns_live0. Sets
    # ns_del_want (the target) and ns_del_got (the last delete count seen).
    ns_wait_all_deleted() {
      local start
      start=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      ns_del_want=$((start - ns_live0))
      ns_del_got=$(wait_metric_ge "$(scaled 20)" "$ns_del_want" \
        "xtcp_counts" 'function="delete"' 'variable="delete"')
    }

    # ns_holders <ns inode link>: name every process still in that netns, with
    # its pid, comm, state and wchan, so a failed teardown names its holder.
    ns_holders() {
      local p
      if [ -z "$1" ]; then return 0; fi
      for p in /proc/[0-9]*; do
        if [ "$(readlink "$p/ns/net" 2>/dev/null)" = "$1" ]; then
          echo "  ns holder: pid=''${p#/proc/} comm=$(cat "$p/comm" 2>/dev/null)" \
            "state=$(cut -d' ' -f3 "$p/stat" 2>/dev/null)" \
            "wchan=$(cat "$p/wchan" 2>/dev/null)"
        fi
      done
    }

    echo "--- check 8: ns lifecycle (live process in a fresh netns) ---"
    check8=1
    if command -v ip >/dev/null 2>&1; then
      # The label keys are function/variable/type (see promLabels in
      # pkg/xtcp/prometheus.go). Prometheus prints labels alphabetically
      # (function, type, variable), so the helper takes the function/
      # variable filters as separate args.
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      before_del=$(metric_value "xtcp_counts" 'function="delete"' 'variable="delete"')
      ip netns add xtcp_test_ns_a 2>&1 || true
      # Bring lo up so a subsequent socket inside the ns is meaningful.
      ip netns exec xtcp_test_ns_a ip link set lo up 2>&1 || true
      # Hold the ns open with a live process so the /proc scan sees it.
      ip netns exec xtcp_test_ns_a sleep 60 &
      ns_a_pid=$!
      # Poll for the daemon to reconcile + enter the ns (netlinker start).
      # A retry loop, not a fixed sleep: the reconcile fires once per poll
      # cycle and that cadence is jittered (pollJitterPct), so a fixed 6s
      # wait races the jitter and intermittently samples before the counter
      # ticks — the NS_LIFECYCLE flake. Return as soon as it advances (so the
      # happy path is faster than the old fixed wait) and cap the wait well
      # above a jittered cycle. The cap is wall clock: this used to count
      # iterations of a curl of up to 2 s plus a 1 s sleep. The sleep 60 above
      # outlives this wait.
      after_inst=$(wait_metric_ge "$(scaled 20)" "$((before_inst + 1))" \
        "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      # Drop the process (ns now has no live process) and remove the bind mount.
      kill "$ns_a_pid" 2>/dev/null || true
      wait "$ns_a_pid" 2>/dev/null || true
      ip netns delete xtcp_test_ns_a 2>&1 || true
      # Poll for the daemon to notice the ns is gone (nsDelete) — same
      # jittered-cadence wait as the instantiate side above — and for every
      # other namespace started since the baseline to be gone too.
      ns_wait_all_deleted

      if [ "$after_inst" -gt "$before_inst" ] && [ "$ns_del_got" -ge "$ns_del_want" ]; then
        echo "XTCP2_SELF_TEST_NS_LIFECYCLE_PASS  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
        check8=0
      else
        echo "XTCP2_SELF_TEST_NS_LIFECYCLE_FAIL  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_LIFECYCLE_FAIL  (ip not on PATH)"
    fi
    if [ "$check8" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 9: TCP traffic inside a fresh netns — full netlinker path ─
    # Creates a netns, brings up lo, starts a listening socket. The listener is
    # a LIVE PROCESS, so Method B's pre-poll reconcile discovers the ns via the
    # /proc scan; xtcp2's per-ns netlinker then polls inet_diag and sees the
    # socket, and the Deserialize loop parses the response into TCPInfo /
    # inet_diag attributes. We assert via the Netlinker "packets" counter: it
    # must bump by ≥1 while the ns is live. (The counter is process-wide, so the
    # always-on host netlinker also drives it; this check confirms the netlinker
    # path stays alive end-to-end while the ns exists, not per-ns isolation.)
    echo "--- check 9: TCP socket inside netns drives netlinker traffic ---"
    check9=1
    if command -v ip >/dev/null 2>&1 && command -v nc >/dev/null 2>&1; then
      # Match both Netlinker (syscall) and NetlinkerIoUring (io_uring) packet
      # counters so this check works in both coverage VM flavors.
      before_packets=$(metric_value "xtcp_counts" 'variable="packets"')
      ip netns add xtcp_test_ns_b 2>&1 || true
      ip netns exec xtcp_test_ns_b ip link set lo up 2>&1 || true
      # Listener in the ns — this is the live process Method B keys on. timeout
      # bounds wall-clock so we don't leak a process if the check fails partway;
      # it must outlast the discovery + poll window below, and is scaled so a
      # slow host cannot cut that window short (teardown kills it either way).
      ip netns exec xtcp_test_ns_b timeout "$(scaled 15)s" nc -l 127.0.0.1 17322 >/dev/null 2>&1 &
      ns_listener=$!
      sleep 1
      # Client also in the ns (loopback only — the ns has no real iface).
      ip netns exec xtcp_test_ns_b sh -c '(echo hello; sleep 8) | nc -w 8 127.0.0.1 17322' >/dev/null 2>&1 &
      ns_client=$!

      # xtcp2 polls every ~2s. Method B needs one pre-poll reconcile to discover
      # the ns and a following cycle to poll its socket, so allow a few cycles.
      sleep 8
      after_packets=$(metric_value "xtcp_counts" 'variable="packets"')

      # Tear down the listener + client and the ns itself. Killing the two $!s
      # ends the `ip netns exec` wrappers but not the `(echo; sleep 8) | nc`
      # pipeline inside the client, which kept the ns alive past `ip netns
      # delete`; its late delete then satisfied a later check's "+1". So kill
      # everything left in the ns too, except the daemon: its per-ns goroutine
      # is locked to an OS thread inside the ns, and when that thread is the
      # main one, `ip netns pids` lists xtcp2's own pid.
      daemon_pid=$(systemctl show -p MainPID --value xtcp2 2>/dev/null || true)
      for p in $(ip netns pids xtcp_test_ns_b 2>/dev/null); do
        if [ "$p" != "$daemon_pid" ]; then kill "$p" 2>/dev/null || true; fi
      done
      kill "$ns_listener" "$ns_client" 2>/dev/null || true
      wait 2>/dev/null || true
      ip netns delete xtcp_test_ns_b 2>&1 || true
      # Not part of NS_TRAFFIC (that is about packets): wait for the teardown so
      # check 10 starts with nothing pending. If it times out, check 10's own
      # delete assertion fails.
      ns_wait_all_deleted
      if [ "$ns_del_got" -lt "$ns_del_want" ]; then
        echo "  xtcp_test_ns_b not torn down (del:$ns_del_got want≥$ns_del_want)"
      fi

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

    # ─── Check 10: docker-style netns — /run/docker/netns/ bind mount ──────
    # A container's netns is bind-mounted under /run/docker/netns/<id>. Under
    # Method B the daemon discovers it by the LIVE PROCESS inside it (the /proc
    # scan), and the resolver uses the /run/docker/netns bind mount for a
    # best-effort NAME. We mimic docker: create a netns, bind it under
    # /run/docker/netns/, and hold a live process in it. The daemon should enter
    # it (netNamespaceInstance "start"); dropping the process + mount then tears
    # it down (the "delete" counter bumps).
    echo "--- check 10: docker-style netns (/run/docker/netns/ + live process) ---"
    check10=1
    if command -v ip >/dev/null 2>&1 && [ -d /run/docker/netns ]; then
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      before_del=$(metric_value "xtcp_counts" 'function="delete"' 'variable="delete"')

      ip netns add xtcp_docker_ns 2>&1 || true
      # mount --bind reuses the netns inode under the docker dir, so the resolver
      # can name it just like a docker-managed one.
      mount --bind /run/netns/xtcp_docker_ns /run/docker/netns/xtcp_docker_ns 2>&1 || true
      ip netns exec xtcp_docker_ns ip link set lo up 2>&1 || true
      # Live process holds the ns open for the /proc scan.
      ip netns exec xtcp_docker_ns sleep 60 &
      ns_d_pid=$!
      # Wait for the counter, not a fixed 6 s: see check 8 for why a fixed wait
      # races the jittered reconcile.
      after_inst=$(wait_metric_ge "$(scaled 20)" "$((before_inst + 1))" \
        "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      docker_ino=$(readlink "/proc/$ns_d_pid/ns/net" 2>/dev/null || true)

      kill "$ns_d_pid" 2>/dev/null || true
      wait "$ns_d_pid" 2>/dev/null || true
      umount /run/docker/netns/xtcp_docker_ns 2>&1 || true
      rm -f /run/docker/netns/xtcp_docker_ns 2>&1 || true
      ip netns delete xtcp_docker_ns 2>&1 || true
      ns_wait_all_deleted

      if [ "$after_inst" -gt "$before_inst" ] && [ "$ns_del_got" -ge "$ns_del_want" ]; then
        echo "XTCP2_SELF_TEST_NS_DOCKER_PASS  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
        check10=0
      else
        echo "XTCP2_SELF_TEST_NS_DOCKER_FAIL  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
        ns_holders "$docker_ino"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_DOCKER_FAIL  (ip not on PATH or /run/docker/netns/ missing)"
    fi
    if [ "$check10" -ne 0 ]; then overall_ok=0; fi

    # ─── Check 16: anonymous netns (unshare -n, no bind mount) — audit gap ──
    # THE Method B proof. `unshare --net` creates a network namespace with a
    # live process but NO /run/netns or /run/docker/netns bind mount — exactly
    # the anonymous container/pod netns the old dir-scan/inotify model was blind
    # to. Method B discovers it purely from /proc/<pid>/ns/net, so xtcp2 should
    # enter it and spawn a per-ns netlinker (netNamespaceInstance "start"), then
    # tear it down (delete counter) once the process exits. Numbered 16 (next
    # free id) but belongs with the NS group (8-10).
    echo "--- check 16: anonymous netns (unshare -n, no bind mount) ---"
    check16=1
    if command -v unshare >/dev/null 2>&1; then
      before_inst=$(metric_value "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      before_del=$(metric_value "xtcp_counts" 'function="delete"' 'variable="delete"')
      # Anonymous netns held open by a live process; no bind mount anywhere.
      # unshare execs sleep directly, so the backgrounded pid IS the only
      # process in the ns and the kill below empties it. This used to be
      # `sh -c 'ip link set lo up; exec sleep 60'`, and on a loaded host the ns
      # outlived the kill by 30-40 s. Until the exec, $! was sh, so the most
      # likely holder is an `ip` still blocked on rtnl_lock (check 10's netns
      # teardown takes it). The FAIL branch names any holder. Only the
      # counters are checked, so lo's state does not matter.
      unshare --net sleep 60 &
      anon_pid=$!
      # The pre-poll reconcile finds it within a poll cycle or two; wait for
      # the counter rather than a fixed 6 s (see check 8).
      after_inst=$(wait_metric_ge "$(scaled 20)" "$((before_inst + 1))" \
        "xtcp_counts" 'function="netNamespaceInstance"' 'variable="start"')
      anon_ino=$(readlink "/proc/$anon_pid/ns/net" 2>/dev/null || true)
      # Drop the process → the anonymous ns now has no live process → reconcile
      # tears it down on a following cycle.
      kill "$anon_pid" 2>/dev/null || true
      wait "$anon_pid" 2>/dev/null || true
      ns_wait_all_deleted

      if [ "$after_inst" -gt "$before_inst" ] && [ "$ns_del_got" -ge "$ns_del_want" ]; then
        echo "XTCP2_SELF_TEST_NS_ANONYMOUS_PASS  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
        check16=0
      else
        echo "XTCP2_SELF_TEST_NS_ANONYMOUS_FAIL  (inst:$before_inst→$after_inst del:$before_del→$ns_del_got want≥$ns_del_want)"
        ns_holders "$anon_ino"
      fi
    else
      echo "XTCP2_SELF_TEST_NS_ANONYMOUS_FAIL  (unshare not on PATH)"
    fi
    if [ "$check16" -ne 0 ]; then overall_ok=0; fi

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
      for _ in $(seq 1 "$(scaled 90)"); do
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
        echo "XTCP2_SELF_TEST_S3PARQUET_FILES_FAIL  (no .parquet object after $(scaled 90)s)"
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

      # ─── Check 14b: s3parquet event_date + timestamp_ns value correctness ─
      # Proves the derived event_date column shipped end-to-end AND that the
      # timestamps are vaguely current — catching the seconds-vs-nanoseconds bug
      # (which collapsed event_date to 1970-01-01) and any gross clock/epoch error
      # such as the year being wrong. We take "now" from the host `date` and allow
      # a generous ±12 h window: this is a sanity band, not a precise-time
      # assertion, so normal poll/upload/skew latency never trips it while a
      # seconds-valued (~1.75e9 = 1970) or wrong-year timestamp lands far outside.
      #   - event_date is well-formed YYYY-MM-DD with ≥1 distinct value,
      #   - every event_date falls within [now-12h, now+12h] (by date), and
      #   - every timestamp_ns falls within [now-12h, now+12h] as epoch ns.
      # The window bounds are computed once in the shell (UTC) and the comparisons
      # run inside duckdb returning integer counts, so we never do bash float
      # arithmetic on the timestamp.
      echo "--- check 14b: s3parquet — event_date + timestamp_ns correct ---"
      check14b=1
      if [ -s /tmp/xtcp2-s3p.parquet ]; then
        pq="read_parquet('/tmp/xtcp2-s3p.parquet')"
        now_s=$(date -u +%s)
        win_s=43200  # ±12 h sanity band
        lo_ns=$(( (now_s - win_s) * 1000000000 ))
        hi_ns=$(( (now_s + win_s) * 1000000000 ))
        lo_date=$(date -u -d "@$(( now_s - win_s ))" +%F)
        hi_date=$(date -u -d "@$(( now_s + win_s ))" +%F)
        edMalformed=$(duckdb -noheader -list \
          -c "SELECT count(*) FROM $pq WHERE event_date NOT SIMILAR TO '[0-9]{4}-[0-9]{2}-[0-9]{2}'" 2>/dev/null \
          | tail -n1 | tr -d '[:space:]')
        edDistinct=$(duckdb -noheader -list \
          -c "SELECT count(DISTINCT event_date) FROM $pq" 2>/dev/null \
          | tail -n1 | tr -d '[:space:]')
        edWindow=$(duckdb -noheader -list \
          -c "SELECT count(*) FROM $pq WHERE CAST(event_date AS DATE) < DATE '$lo_date' OR CAST(event_date AS DATE) > DATE '$hi_date'" 2>/dev/null \
          | tail -n1 | tr -d '[:space:]')
        tsWindow=$(duckdb -noheader -list \
          -c "SELECT count(*) FROM $pq WHERE timestamp_ns < $lo_ns OR timestamp_ns > $hi_ns" 2>/dev/null \
          | tail -n1 | tr -d '[:space:]')
        if [ "''${edMalformed:-1}" = "0" ] && [ "''${edDistinct:-0}" -ge 1 ] 2>/dev/null \
          && [ "''${edWindow:-1}" = "0" ] && [ "''${tsWindow:-1}" = "0" ]; then
          echo "XTCP2_SELF_TEST_S3PARQUET_EVENTDATE_PASS  (distinct=$edDistinct, malformed=0, window=[$lo_date..$hi_date]UTC, edOutOfWindow=0, tsOutOfWindow=0)"
          check14b=0
        else
          echo "XTCP2_SELF_TEST_S3PARQUET_EVENTDATE_FAIL  (malformed=$edMalformed, distinct=$edDistinct, window=[$lo_date..$hi_date]UTC, edOutOfWindow=$edWindow, tsOutOfWindow=$tsWindow)"
        fi
      else
        echo "XTCP2_SELF_TEST_S3PARQUET_EVENTDATE_FAIL  (no parquet file to test)"
      fi
      if [ "$check14b" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runValkeyCheck ''
      # ─── Check: valkey — records consumed back via pub/sub ───────────
      # The daemon PUBLISHes each poll's records to the Valkey channel;
      # the pre-subscribed valkey-subscriber (broker-server.nix) logs every
      # delivered message to /run/xtcp2-valkey-sub.out. Counting the "message"
      # reply markers proves records flowed xtcp2 → valkey → subscriber
      # end-to-end (pub/sub has no retention, so this is the real proof, not
      # just that the PUBLISH command returned OK). The daemon's own
      # destValKey publish counter is reported as a cross-check.
      # Genuine end-to-end consume-back: the daemon constructs the dest (which
      # Pings a REAL in-VM valkey-server) and PUBLISHes each poll's records; the
      # pre-subscribed valkey-subscriber (running valkey-cli under a PTY so it
      # flushes per message) logs every delivery to the journal. We require BOTH
      # the daemon's publish counter (function="destValKey",variable="Publish")
      # AND the subscriber's delivered-message count to be ≥1 — proving records
      # flowed xtcp2 → valkey → subscriber, not just that PUBLISH returned OK.
      echo "--- check: valkey — records consumed back via pub/sub ---"
      checkValkey=1
      recv=0
      pub=0
      for _ in $(seq 1 "$(scaled 60)"); do
        pub=$(metric_value "xtcp_counts" 'function="destValKey"' 'variable="Publish"')
        recv=$(journalctl -u valkey-subscriber.service -o cat --no-pager 2>/dev/null \
          | grep -c '"message"')
        recv=''${recv:-0}
        if [ "$pub" -ge 1 ] 2>/dev/null && [ "$recv" -ge 1 ] 2>/dev/null; then
          break
        fi
        sleep 1
      done
      if [ "$pub" -ge 1 ] 2>/dev/null && [ "$recv" -ge 1 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_VALKEY_CONSUME_PASS  (published=$pub, consumed=$recv, channel=${valkeyChannel})"
        checkValkey=0
      else
        echo "XTCP2_SELF_TEST_VALKEY_CONSUME_FAIL  (published=$pub, consumed=$recv, channel=${valkeyChannel})"
        echo "--- valkey-subscriber journal tail (diagnostic) ---"
        journalctl -u valkey-subscriber.service -o cat --no-pager 2>/dev/null | tail -n 20 \
          || echo "(no valkey-subscriber journal)"
      fi
      if [ "$checkValkey" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runNatsCheck ''
      # ─── Check: nats — records consumed back via a subject ───────────
      # Analog of the valkey check. The daemon PUBLISHes each poll's records to
      # a NATS subject on a real in-VM nats-server; the pre-subscribed
      # nats-subscriber (natscli under a PTY) logs one "Received on" header per
      # delivered message to the journal. Require BOTH the daemon's destNATS
      # publish counter AND the subscriber's delivery count ≥1.
      echo "--- check: nats — records consumed back via a subject ---"
      checkNats=1
      nrecv=0
      npub=0
      for _ in $(seq 1 "$(scaled 60)"); do
        npub=$(metric_value "xtcp_counts" 'function="destNATS"' 'variable="Publish"')
        nrecv=$(journalctl -u nats-subscriber.service -o cat --no-pager 2>/dev/null \
          | grep -c 'Received on')
        nrecv=''${nrecv:-0}
        if [ "$npub" -ge 1 ] 2>/dev/null && [ "$nrecv" -ge 1 ] 2>/dev/null; then
          break
        fi
        sleep 1
      done
      if [ "$npub" -ge 1 ] 2>/dev/null && [ "$nrecv" -ge 1 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_NATS_CONSUME_PASS  (published=$npub, consumed=$nrecv, subject=${natsSubject})"
        checkNats=0
      else
        echo "XTCP2_SELF_TEST_NATS_CONSUME_FAIL  (published=$npub, consumed=$nrecv, subject=${natsSubject})"
        echo "--- nats-subscriber journal tail (diagnostic) ---"
        journalctl -u nats-subscriber.service -o cat --no-pager 2>/dev/null | tail -n 20 \
          || echo "(no nats-subscriber journal)"
      fi
      if [ "$checkNats" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runNsqCheck ''
      # ─── Check: nsq — records consumed from the queue ────────────────
      # The daemon PUBLISHes each poll's records to a real in-VM nsqd topic; the
      # nsq_tail consumer reads them on channel "${nsqChannel}" and FINISHes each,
      # so nsqd's per-channel finish_count (from /stats) is a deterministic count
      # of consumed records. Require BOTH the destNSQ publish counter AND that
      # finish_count ≥1.
      echo "--- check: nsq — records consumed from the queue ---"
      checkNsq=1
      qrecv=0
      qpub=0
      for _ in $(seq 1 "$(scaled 60)"); do
        qpub=$(metric_value "xtcp_counts" 'function="destNSQ"' 'variable="Publish"')
        # nsqd's text /stats prints one line per channel:
        #   [selftest ...] depth: 0 ... msgs: 58 ...
        # `msgs` is the message_count the channel received (== consumed once
        # depth returns to 0). Parse it from the "${nsqChannel}" line — robust
        # across nsqd versions (the ?format=json field names have drifted).
        qrecv=$(curl --silent --max-time 2 \
          "http://127.0.0.1:${toString nsqHttpPort}/stats" 2>/dev/null \
          | grep -F '${nsqChannel}' | grep -oE 'msgs: *[0-9]+' | head -n1 \
          | grep -oE '[0-9]+')
        qrecv=''${qrecv:-0}
        if [ "$qpub" -ge 1 ] 2>/dev/null && [ "$qrecv" -ge 1 ] 2>/dev/null; then
          break
        fi
        sleep 1
      done
      if [ "$qpub" -ge 1 ] 2>/dev/null && [ "$qrecv" -ge 1 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_NSQ_CONSUME_PASS  (published=$qpub, consumed=$qrecv, topic=${nsqTopic}, channel=${nsqChannel})"
        checkNsq=0
      else
        echo "XTCP2_SELF_TEST_NSQ_CONSUME_FAIL  (published=$qpub, consumed=$qrecv, topic=${nsqTopic}, channel=${nsqChannel})"
        echo "--- nsqd /stats (diagnostic) ---"
        curl --silent --max-time 2 "http://127.0.0.1:${toString nsqHttpPort}/stats" 2>/dev/null | head -n 25 \
          || echo "(nsqd /stats unreachable)"
      fi
      if [ "$checkNsq" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runClickhouseHttpCheck ''
      # ─── Check 11h: xtcp2 → ClickHouse over HTTP (direct, no Kafka) ───
      # The http destination POSTs each protobufList envelope straight to
      # ClickHouse's HTTP interface (INSERT … FORMAT ProtobufList into the base
      # MergeTree). PASS requires rows in xtcp.xtcp_flat_records AND the daemon's
      # destHTTP POST-success counter advanced — proving ingestion came via HTTP
      # (this flavor configures no kafka dest at all).
      echo "--- check 11h: direct HTTP → ClickHouse ---"
      check11h=1
      chq() {
        docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
          -q "$1" 2>/dev/null | tr -d '\r\n' || echo 0
      }
      # Baseline the row count first: assert rows GROW during the window so a
      # non-empty starting table can't produce a false pass. The definitive
      # signal is still the destHTTP POST-success counter (fresh per boot).
      rows_before=$(chq "SELECT count() FROM xtcp.xtcp_flat_records")
      rows_before=''${rows_before:-0}
      rows=$rows_before
      for _ in $(seq 1 "$(scaled 30)"); do
        rows=$(chq "SELECT count() FROM xtcp.xtcp_flat_records")
        rows=''${rows:-0}
        if [ "$rows" -gt "$rows_before" ] 2>/dev/null; then break; fi
        sleep 2
      done
      posts=$(metric_value "xtcp_counts" 'function="destHTTP"' 'variable="posts"')
      if [ "$rows" -gt "$rows_before" ] 2>/dev/null && [ "''${posts:-0}" -ge 1 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_CLICKHOUSE_HTTP_PASS  (rows $rows_before->$rows, destHTTP_posts=$posts)"
        check11h=0
      else
        errs=$(metric_value "xtcp_counts" 'function="destHTTP"' 'type="error"')
        echo "XTCP2_SELF_TEST_CLICKHOUSE_HTTP_FAIL  (rows $rows_before->$rows, destHTTP_posts=$posts, destHTTP_errors=$errs)"
        journalctl -u xtcp2 --no-pager 2>/dev/null | grep -i 'destHTTP' | tail -n 3 || true
        echo "  -- clickhouse-server errors (docker logs):"
        docker logs clickhouse 2>&1 | grep -iE 'error|exception|DECIMAL|Protobuf|Cannot' | tail -n 8 || true
        echo "  -- direct HTTP probe from the VM host (auth + a trivial insert):"
        curl -sS -m5 "http://127.0.0.1:18123/?query=SELECT+1&password=${clickhousePassword}" 2>&1 | head -2 || true
      fi
      if [ "$check11h" -ne 0 ]; then overall_ok=0; fi
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
      for _ in $(seq 1 "$(scaled 30)"); do
        rows=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
          -q "SELECT count() FROM xtcp.xtcp_flat_records" 2>/dev/null | tr -d '\r\n' || echo 0)
        if [ "''${rows:-0}" -gt 0 ] 2>/dev/null; then
          break
        fi
        sleep 2
      done
      errors=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records_errors" 2>/dev/null | tr -d '\r\n' || echo "?")
      # timestamp_ns correctness guard: the Kafka table ingests the protobuf
      # timestamp and the MV lands it in xtcp_flat_records.timestamp_ns
      # (DateTime64(9,'UTC')). Assert (a) no row is implausibly dated (before
      # 2000 or more than a day in the future) — catches a units/scale mistake
      # in the ingest path — and (b) at least one row carries a sub-second
      # (nanosecond) component, proving resolution survives end-to-end.
      badTs=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records WHERE timestamp_ns < toDateTime64('2000-01-01 00:00:00', 9, 'UTC') OR timestamp_ns > now64(9) + toIntervalDay(1)" 2>/dev/null | tr -d '\r\n' || echo "?")
      subSec=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records WHERE toUnixTimestamp64Nano(timestamp_ns) % 1000000000 <> 0" 2>/dev/null | tr -d '\r\n' || echo 0)
      if [ "''${rows:-0}" -gt 0 ] 2>/dev/null && [ "$errors" = "0" ] \
        && [ "$badTs" = "0" ] && [ "''${subSec:-0}" -gt 0 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_PASS  (rows=$rows, errors=0, badTs=0, subSecondRows=$subSec)"
        check11=0
      else
        echo "XTCP2_SELF_TEST_CLICKHOUSE_RECORDS_FAIL  (rows=$rows, errors=$errors, badTs=$badTs, subSecondRows=$subSec)"
      fi
      if [ "$check11" -ne 0 ]; then overall_ok=0; fi

      # ─── Check 12: Prom records counter vs ClickHouse rows reconcile ─
      # xtcp2 bumps xtcp_counts{function=Poller,variable=envelopeRows} by
      # len(envelope.Row) for every flushed envelope, so promRows is the exact
      # count of rows the MAIN daemon produced. Every one of those rows must
      # reach ClickHouse (via kafka + MV), so the invariant is:
      #   chRows >= 0.4 * promRows   (ClickHouse is not silently dropping the
      #                               main daemon's rows; 40% floor absorbs the
      #                               kafka-consume + MV-flush lag at the
      #                               sampling instant — xtcp produces every 5s,
      #                               the consumer flushes in 5-10s batches).
      # There is deliberately NO tight upper bound: this self-test ALSO spawns
      # auxiliary xtcp2 producers earlier (the POLL_STREAM / LISTEN_STREAM
      # checks) that feed the SAME kafka→ClickHouse pipeline, so chRows
      # legitimately EXCEEDS the long-lived daemon's envelopeRows counter. An
      # upper cap of promRows+100 was structurally unsound here (it can never
      # hold once a second producer shares the pipeline) and is dropped.
      echo "--- check 12: Prom envelopeRows vs ClickHouse rows reconcile ---"
      check12=1
      promRows=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="envelopeRows"')
      chRows=$(docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
        -q "SELECT count() FROM xtcp.xtcp_flat_records" 2>/dev/null | tr -d '\r\n' || echo 0)
      if [ "''${chRows:-0}" -gt 0 ] 2>/dev/null && [ "''${promRows:-0}" -gt 0 ] 2>/dev/null \
         && [ $((chRows * 100)) -ge $((promRows * 40)) ]; then
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
      for _ in $(seq 1 "$(scaled 45)"); do
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

    ${lib.optionalString runEnrichCheck ''
      # ─── Enrichment checks (clickhouse-pipeline only) ────────────────
      # The daemon carries -enrichContainer/-enrichNic/-enrichLldp on this
      # flavor. These assert the best-effort enrichers reach ClickHouse. Run
      # BEFORE the ctl checks (17-19) which park the poll ticker at 1h — that
      # would starve the record flow these queries depend on.
      #
      # chqe: run a ClickHouse query inside the clickhouse container, trimming
      # CRLF; prints 0 on any failure so the numeric comparisons never explode.
      chqe() {
        docker exec clickhouse clickhouse-client --password ${clickhousePassword} \
          -q "$1" 2>/dev/null | tr -d '\r\n' || echo 0
      }

      # ─── ENRICH_NIC: uplink1_nic_driver populated ─────────────────────
      # The most reliable enrichment check — needs no docker/lldp peer. NIC
      # identity is read from sysfs+ethtool for the VM's primary uplink (virtio_net
      # in a microvm), so uplink1_nic_driver should be non-empty on ≥1 row.
      echo "--- enrich: ENRICH_NIC (uplink1_nic_driver populated) ---"
      checkENic=1
      nicRows=0
      for _ in $(seq 1 "$(scaled 30)"); do
        nicRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records WHERE length(uplink1_nic_driver) > 0")
        if [ "''${nicRows:-0}" -gt 0 ] 2>/dev/null; then break; fi
        sleep 2
      done
      if [ "''${nicRows:-0}" -gt 0 ] 2>/dev/null; then
        drv=$(chqe "SELECT any(uplink1_nic_driver) FROM xtcp.xtcp_flat_records WHERE length(uplink1_nic_driver) > 0")
        echo "XTCP2_SELF_TEST_ENRICH_NIC_PASS  (rows_with_driver=$nicRows, driver=$drv)"
        checkENic=0
      else
        # Compact diagnostics folded into the sentinel line itself (separate
        # echoes in this branch were being dropped from the journal). Compute
        # each fact into a var so a failing sub-command can't blank the line:
        #   dnics  = physical NICs sysfs exposes (name:driver, [no-device] if none)
        #   dif    = did ANY row get uplink1_ifname stamped? (uplink detected)
        #   dnone/dfb/dcol = daemon's uplink-selection counters
        dnics=""
        for d in /sys/class/net/*; do
          nn=$(basename "$d")
          [ "$nn" = "lo" ] && continue
          if [ -e "$d/device" ]; then
            dnics="$dnics $nn:$(readlink "$d/device/driver" 2>/dev/null | xargs -r basename || true)"
          else
            dnics="$dnics $nn:[no-device]"
          fi
        done
        dif=$(chqe "SELECT any(uplink1_ifname) FROM xtcp.xtcp_flat_records WHERE length(uplink1_ifname) > 0")
        dnone=$(metric_value "xtcp_counts" 'variable="uplinks"' 'type="none"')
        dfb=$(metric_value "xtcp_counts" 'variable="uplinks"' 'type="physical_fallback"')
        dcol=$(metric_value "xtcp_counts" 'variable="nic"' 'type="collected"')
        ddaemon=$(journalctl -u xtcp2 --no-pager 2>/dev/null | grep -i 'initUplinkEnrichers' | tail -n 1 | sed 's/.*initUplinkEnrichers/initUplinkEnrichers/')
        # Decisive ground-truth probes folded into the sentinel:
        #   dpop   = per-column populated counts on the SAME MergeTree table
        #            (does container populate while uplink doesn't, on one table?)
        #   dsamp  = for a row KNOWN to be enriched (container_id set), what are
        #            its uplink columns? isolates "not stamped" vs "lost in transit"
        #   dtype  = the actual runtime column type of uplink1_ifname
        #   dcherr = ClickHouse protobuf/parse errors in the server log
        dpop=$(chqe "SELECT concat('if=',toString(countIf(length(uplink1_ifname)>0)),' drv=',toString(countIf(length(uplink1_nic_driver)>0)),' spd=',toString(countIf(uplink1_nic_speed_mbps>0)),' cid=',toString(countIf(length(container_id)>0)),' n=',toString(count())) FROM xtcp.xtcp_flat_records")
        dsamp=$(chqe "SELECT concat('if=[',uplink1_ifname,'] drv=[',uplink1_nic_driver,'] spd=[',toString(uplink1_nic_speed_mbps),']') FROM xtcp.xtcp_flat_records WHERE length(container_id) > 0 LIMIT 1")
        dtype=$(chqe "SELECT type FROM system.columns WHERE database='xtcp' AND table='xtcp_flat_records' AND name='uplink1_ifname'")
        dcherr=$(docker logs clickhouse 2>&1 | grep -iE 'protobuf|cannot parse|no_column|uplink' | tail -n 1 | tr -d '\r' | cut -c1-160)
        echo "XTCP2_SELF_TEST_ENRICH_NIC_FAIL  (no uplink1_nic_driver after $(scaled 60)s; stamped_ifname='$dif' cnt_none=$dnone cnt_fallback=$dfb cnt_collected=$dcol pop=[$dpop] enriched_row=[$dsamp] iftype=[$dtype] cherr=[$dcherr] daemon='$ddaemon' sysfs_nics=[$dnics ])"
      fi
      if [ "$checkENic" -ne 0 ]; then overall_ok=0; fi

      # ─── ENRICH_CONTAINER: container_id + non-default netns ───────────
      # The redpanda + clickhouse containers each run in their OWN docker netns
      # holding live processes with TCP sockets. Method B's /proc scan discovers
      # those netns (records carry netns != 'default'), and -enrichContainer maps
      # each netns → its owning container via the Docker Engine API (stamping
      # container_id/name/image). PASS requires BOTH signals in ClickHouse:
      # netns != 'default' (discovery) AND a non-empty container_id (enrichment).
      echo "--- enrich: ENRICH_CONTAINER (container_id + non-default netns) ---"
      checkECont=1
      nsRows=0
      cidRows=0
      for _ in $(seq 1 "$(scaled 45)"); do
        nsRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records WHERE netns != 'default' AND length(netns) > 0")
        cidRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records WHERE length(container_id) > 0")
        if [ "''${nsRows:-0}" -gt 0 ] 2>/dev/null && [ "''${cidRows:-0}" -gt 0 ] 2>/dev/null; then
          break
        fi
        sleep 3
      done
      cnameRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records WHERE length(container_name) > 0")
      if [ "''${nsRows:-0}" -gt 0 ] 2>/dev/null && [ "''${cidRows:-0}" -gt 0 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_ENRICH_CONTAINER_PASS  (netns!=default rows=$nsRows, container_id rows=$cidRows, container_name rows=$cnameRows)"
        checkECont=0
      else
        echo "XTCP2_SELF_TEST_ENRICH_CONTAINER_FAIL  (netns!=default rows=$nsRows, container_id rows=$cidRows, container_name rows=$cnameRows)"
        echo "  -- distinct netns seen (diagnostic):"
        chqe "SELECT groupArray(DISTINCT netns) FROM xtcp.xtcp_flat_records" || true
      fi
      if [ "$checkECont" -ne 0 ]; then overall_ok=0; fi

      # ─── SCHEMA_VERSION: per-version routing + provenance ─────────────
      # The VM daemon is the current binary, so every record carries the
      # current epoch N (XtcpFlatRecordSchemaVersion, read at eval time from
      # pkg/xtcp/schema_version.go) and routes to xtcp.xtcp_flat_records_vN;
      # the earlier epochs' tables stay empty. xtcp.xtcp_flat_records is a
      # Merge view spanning every physical version table. PASS requires:
      #   * epoch-N rows > 0, and no row of any other epoch
      #   * the Merge union spans the physical version tables (proven via the
      #     _table virtual column: every union row comes from _vN, none from
      #     any other table)
      #   * daemon_version populated on every routed row
      #
      # Every metric is derived from a SINGLE scan of the Merge surface. Taking
      # separate counts of _vN and the union races (the daemon keeps producing,
      # so a later query sees more rows) — one snapshot is internally consistent.
      echo "--- schema: SCHEMA_VERSION (epoch ${sv} routing + daemon_version) ---"
      checkSV=1
      svRows=0
      for _ in $(seq 1 "$(scaled 30)"); do
        svRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records_v${sv} WHERE schema_version = ${sv}")
        if [ "''${svRows:-0}" -gt 0 ] 2>/dev/null; then break; fi
        sleep 3
      done
      # tab-separated single-row snapshot: cur other noDver total fromCur fromOther
      svSnap=$(chqe "SELECT countIf(schema_version = ${sv}), countIf(schema_version != ${sv}), countIf(length(daemon_version) = 0), count(), countIf(_table = 'xtcp_flat_records_v${sv}'), countIf(_table != 'xtcp_flat_records_v${sv}') FROM xtcp.xtcp_flat_records")
      read -r svCur svOther svNoDver svTotal svFromCur svFromOther <<< "$svSnap"
      if [ "''${svCur:-0}" -gt 0 ] 2>/dev/null \
        && [ "''${svOther:-1}" -eq 0 ] 2>/dev/null \
        && [ "''${svNoDver:-1}" -eq 0 ] 2>/dev/null \
        && [ "''${svFromOther:-1}" -eq 0 ] 2>/dev/null \
        && [ "''${svFromCur:-0}" -eq "''${svTotal:-1}" ] 2>/dev/null; then
        dver=$(chqe "SELECT any(daemon_version) FROM xtcp.xtcp_flat_records_v${sv} WHERE length(daemon_version) > 0")
        echo "XTCP2_SELF_TEST_SCHEMA_VERSION_PASS  (epoch=${sv}, rows=$svCur, total=$svTotal, from_v${sv}=$svFromCur, daemon_version='$dver')"
        checkSV=0
      else
        echo "XTCP2_SELF_TEST_SCHEMA_VERSION_FAIL  (epoch=${sv}, current=$svCur, other_epoch=$svOther, no_daemon_version=$svNoDver, total=$svTotal, from_v${sv}=$svFromCur, from_other_table=$svFromOther)"
      fi
      if [ "$checkSV" -ne 0 ]; then overall_ok=0; fi

      # ─── ENRICH_LLDP: best-effort contract (no peer available) ────────
      # A lone VM has no LLDP neighbor, so we can't assert a populated
      # uplink*_lldp_* column. Instead assert the best-effort contract that
      # enabling -enrichLldp did NOT break the daemon: the lldpd control socket
      # exists, xtcp2 is still active, and records are still flowing to ClickHouse.
      # If a neighbor DID somehow appear (e.g. a future peer in the test net) we
      # report it, but it is not required. (To assert a real neighbor you need a
      # peer advertising LLDP on a shared link — see the task notes.)
      echo "--- enrich: ENRICH_LLDP (best-effort contract; no peer in a lone VM) ---"
      checkELldp=1
      sockOk=0
      # lldpd's control socket is /run/lldpd.socket (see services.lldpd). -S = is
      # a socket. Give lldpd a moment to create it.
      for _ in $(seq 1 "$(scaled 15)"); do
        if [ -S /run/lldpd.socket ]; then sockOk=1; break; fi
        sleep 1
      done
      lldpNeighbors=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records WHERE length(uplink1_lldp_chassis_name) > 0 OR length(uplink1_lldp_port_id) > 0")
      liveRows=$(chqe "SELECT count() FROM xtcp.xtcp_flat_records")
      if [ "$sockOk" -eq 1 ] && systemctl is-active --quiet xtcp2 && [ "''${liveRows:-0}" -gt 0 ] 2>/dev/null; then
        echo "XTCP2_SELF_TEST_ENRICH_LLDP_PASS  (lldpd socket present, xtcp2 active, rows=$liveRows, lldp_neighbor_rows=$lldpNeighbors [0 expected without a peer])"
        checkELldp=0
      else
        echo "XTCP2_SELF_TEST_ENRICH_LLDP_FAIL  (sockOk=$sockOk, active=$(systemctl is-active xtcp2 || true), rows=$liveRows)"
      fi
      if [ "$checkELldp" -ne 0 ]; then overall_ok=0; fi

      # ─── ENRICH_BESTEFFORT_NEGATIVE: missing sockets are non-fatal ────
      # Launch a THROWAWAY xtcp2 with the enrichers pointed at NONEXISTENT
      # docker + lldpd sockets (and -dest null, alt prom/grpc ports so it never
      # collides with the primary daemon). It must still start (expose /metrics)
      # AND still be alive after the window — proving the best-effort enrichers
      # degrade gracefully instead of aborting the daemon.
      echo "--- enrich: ENRICH_BESTEFFORT_NEGATIVE (missing enricher sockets non-fatal) ---"
      checkENeg=1
      if command -v xtcp2 >/dev/null 2>&1; then
        neg_prom="127.0.0.1:9099"
        # The run window must outlast the startup loop below, or a slow cold
        # start under host CPU contention gets killed by `timeout` and reads as
        # alive=0: the very non-fatal-abort bug this check guards against. The
        # loop is $(scaled 30) iterations of at most 3 s (curl --max-time 2 +
        # sleep 1), so the window is that plus 10 s. The check kills the
        # throwaway itself, so a pass never waits it out. -timeout 1s is
        # xtcp2's netlink poll timeout, a daemon setting, and is not scaled.
        timeout "$(($(scaled 30) * 3 + 10))s" xtcp2 -dest null -frequency 2s -timeout 1s \
          -enrichLldp -lldpdSocket /nonexistent/xtcp2-bogus-lldpd.sock \
          -lldpdVersionHint 1.0.22 \
          -enrichContainer -dockerSocket /nonexistent/xtcp2-bogus-docker.sock \
          -enrichNic \
          -promListen "$neg_prom" -grpcPort 8899 \
          >/tmp/xtcp2-enrich-neg.log 2>&1 &
        neg_pid=$!
        neg_up=0
        for _ in $(seq 1 "$(scaled 30)"); do
          if curl --silent --fail --max-time 2 "http://$neg_prom/metrics" | grep -q '^xtcp_'; then
            neg_up=1
            break
          fi
          # Bail out early if the throwaway died (that would be the bug).
          kill -0 "$neg_pid" 2>/dev/null || break
          sleep 1
        done
        # Readiness (diagnostic): a healthy poll loop despite the missing sockets.
        neg_rz=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 \
          "http://$neg_prom/readyz" 2>/dev/null || echo 000)
        neg_alive=0
        kill -0 "$neg_pid" 2>/dev/null && neg_alive=1
        kill "$neg_pid" 2>/dev/null || true
        wait "$neg_pid" 2>/dev/null || true
        if [ "$neg_up" -eq 1 ] && [ "$neg_alive" -eq 1 ]; then
          echo "XTCP2_SELF_TEST_ENRICH_BESTEFFORT_NEGATIVE_PASS  (throwaway xtcp2 came up + stayed alive with bogus enricher sockets; readyz=$neg_rz)"
          checkENeg=0
        else
          echo "XTCP2_SELF_TEST_ENRICH_BESTEFFORT_NEGATIVE_FAIL  (metrics_up=$neg_up, alive=$neg_alive, readyz=$neg_rz)"
          head -n 15 /tmp/xtcp2-enrich-neg.log 2>/dev/null || true
        fi
      else
        echo "XTCP2_SELF_TEST_ENRICH_BESTEFFORT_NEGATIVE_FAIL  (xtcp2 not on PATH)"
      fi
      if [ "$checkENeg" -ne 0 ]; then overall_ok=0; fi
    ''}

    # ─── Check 14c: no records dropped after the poll-end flush ──────────
    # Every flavor: every parsed record goes through an envelope, whatever
    # the destination, and one parsed after its poll's flush has nowhere to
    # go and is counted as Deserialize/envelopePostFlushDrop
    # (docs/investigations/envelope-post-flush-drop.md). Requires appends
    # > 0 too, so a run that collected nothing doesn't pass vacuously. Runs
    # before checks 17-19 because CTL_RESTART restarts the daemon, which
    # resets the counters.
    echo "--- check 14c: no post-flush drops ---"
    pfDrops=$(metric_value "xtcp_counts" 'function="Deserialize"' 'variable="envelopePostFlushDrop"')
    pfAppends=$(metric_value "xtcp_counts" 'function="Deserialize"' 'variable="envelopeAppend"')
    if [ "''${pfDrops:-1}" = "0" ] && [ "''${pfAppends:-0}" -ge 1 ] 2>/dev/null; then
      echo "XTCP2_SELF_TEST_NODROP_PASS  (drops=0, appends=$pfAppends)"
    else
      echo "XTCP2_SELF_TEST_NODROP_FAIL  (drops=$pfDrops, appends=$pfAppends)"
      overall_ok=0
    fi

    # ─── Checks 17-19: xtcp2ctl runtime operator control ─────────────────
    # Prove the new control CLI actually CHANGES daemon behavior, not just
    # that the RPC returns OK. Run LAST because check 17 parks the poll ticker
    # at 1h (to isolate check 18), which would starve the kafka/s3 checks above
    # if it ran earlier.
    #   17 CTL_HOT     — set-poll-frequency is reflected by a later `get` AND
    #                    bumps the Poller ticker.Reset counter (poller re-read
    #                    config and reset its ticker).
    #   18 CTL_TRIGGER — with the ticker parked at 1h, trigger-poll + poll-burst
    #                    are the ONLY thing that can advance the Poller
    #                    pollRequestCh counter, so a clean delta proves the
    #                    on-demand polls really ran.
    #   19 CTL_RESTART — reconfigure (soft restart via syscall.Exec) with a new
    #                    tag: MainPID is unchanged (in-place re-exec, NOT a
    #                    systemd restart), `get` reflects the new tag, and freshly
    #                    streamed records carry it. Skipped under coverage (a
    #                    re-exec drops pre-exec -cover counters).
    if [ "$uds_security_mode" -eq 1 ]; then
      CTL=(-network unix -unixSocket "${udsGrpcSocket}" -auth-token "${udsAuthToken}")
    else
      CTL=(-target 127.0.0.1 -port ${toString grpcPort})
    fi

    echo "--- check 17: xtcp2ctl set-poll-frequency (hot change, no restart) ---"
    check17=1
    if command -v xtcp2ctl >/dev/null 2>&1; then
      before_reset=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="ticker.Reset"')
      # Park the ticker at 1h with a 1s timeout: the long frequency isolates
      # check 18, and the small timeout lets the burst there use a 2s interval
      # (burst interval must exceed poll_timeout).
      # -timeout 1s is the daemon's poll_timeout setting, not a wall-clock
      # guard, so it is not scaled.
      xtcp2ctl set-poll-frequency -frequency 3600s -timeout 1s "''${CTL[@]}" >/tmp/ctl.log 2>&1
      sp_rc=$?
      got=$(xtcp2ctl get "''${CTL[@]}" 2>/dev/null)
      after_reset=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="ticker.Reset"')
      if [ "$sp_rc" -eq 0 ] && printf '%s' "$got" | grep -q '"3600s"' && [ "$after_reset" -gt "$before_reset" ]; then
        echo "XTCP2_SELF_TEST_CTL_HOT_PASS  (get shows 3600s, ticker.Reset $before_reset→$after_reset)"
        check17=0
      else
        echo "XTCP2_SELF_TEST_CTL_HOT_FAIL  (sp_rc=$sp_rc, ticker.Reset $before_reset→$after_reset)"
        printf '%s\n' "$got" | head -n 3
      fi
    else
      echo "XTCP2_SELF_TEST_CTL_HOT_FAIL  (xtcp2ctl not on PATH)"
    fi
    if [ "$check17" -ne 0 ]; then overall_ok=0; fi

    echo "--- check 18: xtcp2ctl trigger-poll + poll-burst drive on-demand polls ---"
    check18=1
    if command -v xtcp2ctl >/dev/null 2>&1; then
      sleep 3   # let any in-flight poll from the pre-3600s cadence drain
      before_poll=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="pollRequestCh"')
      xtcp2ctl trigger-poll "''${CTL[@]}" >/dev/null 2>&1
      xtcp2ctl poll-burst -count 4 -interval 2s "''${CTL[@]}" >/dev/null 2>&1
      sleep 10  # 4 polls × 2s spacing + slack
      after_poll=$(metric_value "xtcp_counts" 'function="Poller"' 'variable="pollRequestCh"')
      delta=$((after_poll - before_poll))
      # Ticker parked at 1h, so 1 (trigger) + 4 (burst) = 5 on-demand pokes are
      # the only possible source. Require >=4 (tolerate one timing slip).
      if [ "$delta" -ge 4 ]; then
        echo "XTCP2_SELF_TEST_CTL_TRIGGER_PASS  (pollRequestCh +$delta from trigger+burst, ticker parked)"
        check18=0
      else
        echo "XTCP2_SELF_TEST_CTL_TRIGGER_FAIL  (pollRequestCh +$delta, expected >=4)"
      fi
    else
      echo "XTCP2_SELF_TEST_CTL_TRIGGER_FAIL  (xtcp2ctl not on PATH)"
    fi
    if [ "$check18" -ne 0 ]; then overall_ok=0; fi

    ${lib.optionalString (!coverageEnabled) ''
      echo "--- check 19: xtcp2ctl reconfigure = soft restart (same PID, new config live) ---"
      check19=1
      if command -v xtcp2ctl >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        reconf_tag="INC-SELFTEST-9f3a"
        pid_before=$(systemctl show -p MainPID --value xtcp2)
        xtcp2ctl get "''${CTL[@]}" > /tmp/xtcp2-cfg.json 2>/dev/null
        jq --arg t "$reconf_tag" '.tag = $t' /tmp/xtcp2-cfg.json > /tmp/xtcp2-cfg.new.json 2>/tmp/jq.err
        if [ ! -s /tmp/xtcp2-cfg.new.json ]; then
          echo "XTCP2_SELF_TEST_CTL_RESTART_FAIL  (could not build new config: $(head -n1 /tmp/jq.err 2>/dev/null))"
        else
          xtcp2ctl reconfigure -file /tmp/xtcp2-cfg.new.json "''${CTL[@]}" >/tmp/reconf.log 2>&1
          # A soft restart re-execs the daemon in place (syscall.Exec: same
          # PID/container) carrying the new config in XTCP_CONFIG_JSON, then boots
          # from it. We verify it by polling `get` until the freshly-booted daemon
          # serves the new tag — that proves the re-exec completed AND the new
          # config is the LIVE config (get returns the running x.config). Because
          # the daemon stamps x.config.Tag onto every record it builds
          # (pkg/xtcp/deserialize.go), a live config carrying the tag means the
          # exported records carry it too. Same MainPID confirms it was an
          # in-place re-exec, not a full service restart.
          back=0
          for _ in $(seq 1 "$(scaled 30)"); do
            if xtcp2ctl get "''${CTL[@]}" 2>/dev/null | grep -q "$reconf_tag"; then back=1; break; fi
            sleep 1
          done
          pid_after=$(systemctl show -p MainPID --value xtcp2)

          if [ "$back" -eq 1 ] && [ -n "$pid_after" ] && [ "$pid_after" = "$pid_before" ] \
             && systemctl is-active --quiet xtcp2; then
            echo "XTCP2_SELF_TEST_CTL_RESTART_PASS  (soft restart: MainPID stayed $pid_before, new config live with tag=$reconf_tag)"
            check19=0
          else
            echo "XTCP2_SELF_TEST_CTL_RESTART_FAIL  (back=$back, pid $pid_before→$pid_after, active=$(systemctl is-active xtcp2 || true))"
            head -n 5 /tmp/reconf.log 2>/dev/null || true
          fi
        fi
      else
        echo "XTCP2_SELF_TEST_CTL_RESTART_FAIL  (xtcp2ctl or jq not on PATH)"
      fi
      if [ "$check19" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runRtnlEventsCheck ''
      echo "--- check 20: rtnetlink listener: events, then an overrun resync (pkg/rtnlmon) ---"
      # rtnlmon-probe runs pkg/rtnlmon in a namespace of its own, so nothing
      # this check adds can reach the daemon's view of the host. Part a: four
      # changes must arrive as notifications, in order. Part b: with a tiny
      # receive buffer and the probe not reading, 399 address changes overrun
      # it; once it reads again it must report the overrun and resync to the
      # one surviving address. The queue still holds stale adds for the 199
      # deleted ones, so a listener that resyncs without draining first would
      # report them.
      check20=1
      rtnl_a="" rtnl_b=""
      if command -v rtnlmon-probe >/dev/null 2>&1; then
        ip netns delete rtnl_check 2>/dev/null || true
        ip netns add rtnl_check
        rm -f /tmp/rtnl-a.ready /tmp/rtnl-a.log /tmp/rtnl-b.ready /tmp/rtnl-b.log /tmp/rtnl-b.pause

        # Part a: events.
        ip netns exec rtnl_check rtnlmon-probe -groups link,addr -ready /tmp/rtnl-a.ready \
          -timeout "$(scaled 60)s" >/tmp/rtnl-a.log 2>&1 &
        rtnl_pid=$!
        for _ in $(seq 1 "$(scaled 20)"); do [ -e /tmp/rtnl-a.ready ] && break; sleep 0.5; done
        ip -n rtnl_check link add rtnl0 type dummy
        ip -n rtnl_check link set rtnl0 up
        ip -n rtnl_check addr add 192.0.2.1/32 dev rtnl0
        ip -n rtnl_check link del rtnl0
        for _ in $(seq 1 "$(scaled 20)"); do
          grep -q '^RTNLMON_EVENT link del .* ifname=rtnl0 ' /tmp/rtnl-a.log && break
          sleep 0.5
        done
        kill "$rtnl_pid" 2>/dev/null || true
        wait "$rtnl_pid" 2>/dev/null || true
        first() { grep -n -m1 -E "$1" /tmp/rtnl-a.log | cut -d: -f1; }
        l_add=$(first '^RTNLMON_EVENT link add .* ifname=rtnl0 ')
        l_up=$(first '^RTNLMON_EVENT link add .* ifname=rtnl0 up$')
        l_addr=$(first '^RTNLMON_EVENT addr add .* addr=192\.0\.2\.1/32$')
        l_del=$(first '^RTNLMON_EVENT link del .* ifname=rtnl0 ')
        if [ -n "$l_add" ] && [ -n "$l_up" ] && [ -n "$l_addr" ] && [ -n "$l_del" ] \
           && [ "$l_add" -le "$l_up" ] && [ "$l_up" -lt "$l_addr" ] && [ "$l_addr" -lt "$l_del" ]; then
          rtnl_a="ok"
        else
          rtnl_a="events out of order or missing (lines add=$l_add up=$l_up addr=$l_addr del=$l_del)"
        fi

        # Part b: an overrun.
        ip -n rtnl_check link add rtnl1 type dummy
        ip -n rtnl_check link set rtnl1 up
        rtnl_idx=$(ip netns exec rtnl_check cat /sys/class/net/rtnl1/ifindex)
        touch /tmp/rtnl-b.pause
        ip netns exec rtnl_check rtnlmon-probe -groups addr4 -rcvbuf 4096 -ready /tmp/rtnl-b.ready \
          -pause /tmp/rtnl-b.pause -timeout "$(scaled 120)s" >/tmp/rtnl-b.log 2>&1 &
        rtnl_pid=$!
        for _ in $(seq 1 "$(scaled 20)"); do [ -e /tmp/rtnl-b.ready ] && break; sleep 0.5; done
        for i in $(seq 1 200); do echo "addr add 198.51.100.$i/32 dev rtnl1"; done >/tmp/rtnl-b.add
        for i in $(seq 2 200); do echo "addr del 198.51.100.$i/32 dev rtnl1"; done >/tmp/rtnl-b.del
        ip -n rtnl_check -batch /tmp/rtnl-b.add
        ip -n rtnl_check -batch /tmp/rtnl-b.del
        rm -f /tmp/rtnl-b.pause
        for _ in $(seq 1 "$(scaled 30)"); do
          grep -q '^RTNLMON_SNAPSHOT overrun ' /tmp/rtnl-b.log && break
          sleep 0.5
        done
        sleep 1
        kill "$rtnl_pid" 2>/dev/null || true
        wait "$rtnl_pid" 2>/dev/null || true
        # What the last resync reported for rtnl1, and any address event after it.
        after=$(awk '/^RTNLMON_SNAPSHOT overrun /{buf=""; on=1; next} on{buf=buf $0 "\n"} END{printf "%s", buf}' /tmp/rtnl-b.log)
        snap=$(printf '%s' "$after" | grep "^RTNLMON_SNAPSHOT_ADDR overrun ifindex=$rtnl_idx " | sed 's/.* addr=//' | sort | tr '\n' ' ')
        stale=$(printf '%s' "$after" | grep -c "^RTNLMON_EVENT addr add ifindex=$rtnl_idx addr=198\.51\.100\.\([2-9]\|[1-9][0-9]\)" || true)
        if ! grep -q '^RTNLMON_SNAPSHOT overrun ' /tmp/rtnl-b.log; then
          rtnl_b="no overrun reported"
        elif [ "$snap" != "198.51.100.1/32 " ] || [ "$stale" -ne 0 ]; then
          rtnl_b="resync for rtnl1 was '$snap' with $stale stale adds after it"
        else
          rtnl_b="ok"
        fi
        ip netns delete rtnl_check 2>/dev/null || true
      else
        rtnl_a="rtnlmon-probe not on PATH"
      fi
      if [ "$rtnl_a" = "ok" ] && [ "$rtnl_b" = "ok" ]; then
        echo "XTCP2_SELF_TEST_RTNL_EVENTS_PASS  (link/addr events in order; overrun drained and resynced to 1 address)"
        check20=0
      else
        echo "XTCP2_SELF_TEST_RTNL_EVENTS_FAIL  (events: $rtnl_a; overrun: ''${rtnl_b:-not run})"
        sed -n '1,15p' /tmp/rtnl-a.log 2>/dev/null || true
        grep -v '^RTNLMON_EVENT' /tmp/rtnl-b.log 2>/dev/null | sed -n '1,15p' || true
      fi
      if [ "$check20" -ne 0 ]; then overall_ok=0; fi
    ''}

    ${lib.optionalString runNlProbeCheck ''
      # ─── Check: idiag_ext request/response contract (real kernel) ────────
      # idiag-extprobe dumps AF_INET/TCP sockets with several idiag_ext values and
      # asserts the kernel returns exactly the ext-gated attributes requested (and,
      # for the default idiag_ext=254 the daemon now uses, that MEMINFO is absent).
      # It exits 0 + prints IDIAG_EXT_PROBE_OK on success. Runs as root (systemd
      # service user) so the inet_diag dump is unrestricted.
      if command -v idiag-extprobe >/dev/null 2>&1; then
        if idiag-extprobe >/tmp/idiag-extprobe.log 2>&1; then
          echo "XTCP2_SELF_TEST_IDIAG_EXT_PROBE_PASS  ($(grep -c ' OK$' /tmp/idiag-extprobe.log 2>/dev/null || echo '?') cases; default ext=254 returns no MEMINFO)"
        else
          echo "XTCP2_SELF_TEST_IDIAG_EXT_PROBE_FAIL  ($(tail -n1 /tmp/idiag-extprobe.log 2>/dev/null))"
          sed -n '1,20p' /tmp/idiag-extprobe.log 2>/dev/null || true
          overall_ok=0
        fi
      else
        echo "XTCP2_SELF_TEST_IDIAG_EXT_PROBE_FAIL  (idiag-extprobe not on PATH)"
        overall_ok=0
      fi
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
