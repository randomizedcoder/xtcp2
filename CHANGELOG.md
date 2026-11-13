# Changelog

## Unreleased

### Breaking

- **Listeners: no TCP port by default, auth on by default.** xtcp2 now serves health, metrics and the link monitor's detail paths on one Unix socket, `/run/xtcp2/metrics.sock` (mode `0600`), with a raw bearer token from `LISTENER_RAW_TOKEN_FILE`, and exits 64 without one. It no longer listens on `:9088` or `:8889`: gRPC, pprof and `/ip` are served only by a listener that names them. `-listener` (repeatable) / `LISTENERS` describes each listener, its credentials (files only) and the classes it serves: `health`, `metrics`, `metrics-detail`, `ip`, `debug`, `grpc-read`, `grpc-admin` ([listeners](docs/security.md#listeners)). TCP must be typed (`network=tcp`) and needs a credential unless it is loopback or serves health alone; `debug` and `grpc-admin` need an admin credential; pprof never serves `cmdline`. A `grpc-read` listener answers admin RPCs `Unimplemented`. Bad listener config, a missing token or token file exits 64; `ConfigService.Set` refuses the same.
- **The per-listener flags are deprecated.** `-promListen*`, `-grpcPort`, `-grpcListen*`, `-grpc*UnixSocket*`, `-linkMonitorIPListen` and their variables still work, expanded to listeners with a `DEPRECATED` line naming the `-listener` equivalent; the Prometheus listener they describe no longer serves pprof, and as TCP it now needs a token. Giving them with `-listener` exits 64. An empty `-listenerAuthMode` / `LISTENER_AUTH_MODE=` now means unset (raw), not disabled.
- **Clients dial the gRPC socket.** `xtcp2ctl` and `xtcp2client` default to `unix:/run/xtcp2/grpc.sock`; pass `-network tcp` for a TCP listener. The OCI images expose no ports, and `-healthcheck` probes the listener serving `health` from the same settings as the daemon. The NixOS module gains `listeners` and `listenerTokenFile` (null generates a token once, at `/var/lib/xtcp2/listener-token`); `linkMonitor.ipListen` becomes a loopback `ip` listener. New gauge `xtcp2_listener_active{name,network,class}`.
- **S3 uses TLS unless told otherwise.** A bare `host:port` S3 endpoint now uses TLS (it was plaintext), and `http://` is refused at startup (exit 64) unless `-s3AllowInsecure` / `S3_ALLOW_INSECURE=true`, which logs a warning and sets `xtcp2_s3_insecure 1`. TLS 1.2 is the minimum; `-s3CAFile` / `S3_CA_FILE` adds a private CA. `ConfigService.Set` refuses a plaintext endpoint without the opt-in.
- **Credentials on the command line.** The production (slim) flavors refuse `-s3AccessKey`, `-s3SecretKey` and the four `-listener*` secret flags with exit 64; development builds warn, now for the S3 flags too and even when the env var is also set. Use the `_FILE` variables.
- **`_FILE` secrets are checked.** A missing or unreadable `*_FILE` path, a non-regular file, or one readable by group or others stops startup with exit 64. It used to be ignored silently (listener tokens) or logged (S3 keys), leaving the credential empty. Mount secrets mode `0400`.
- **The debug writers are build-time opt-ins.** The netlink capture writer (`-writeFiles` / `-capturePath`) is compiled in only with `-tags capture`, which no flavor or image sets; use `nix build .#xtcp2-capture`. The `file:` destination needs `-tags dest_file` and is in the full (development) flavor only, so `xtcp2-<dest>` slim flavors and their images no longer have it. Asking for either on a binary without it exits 64 at startup with the tag to rebuild with; `ConfigService.Set` refuses it with `FailedPrecondition`. A capture build reports `+capture` in its version line, and when armed logs a warning and sets `xtcp2_debug_capture_enabled 1`.

### New

- **Per-namespace caps.** `-maxDumpBytes` (default 256 MiB) and `-maxRecordsPerNetns` (default 262144) bound one namespace's dump per poll: packets past the byte cap are drained undecoded, and records past the record cap are dropped before decode. `-maxNetns` (default 4096) bounds the namespaces tracked. At the cap, tracked namespaces keep their slots, and new ones wait for a freed slot, first come, first served. Alert on `xtcp_netns_dropped_total`. Drops are counted in `xtcp_dump_dropped_total{reason}` and `xtcp_netns_dropped_total`, with one log line per truncated dump or short reconcile. `0` turns a cap off; the matching env vars are `MAX_DUMP_BYTES`, `MAX_RECORDS_PER_NETNS` and `MAX_NETNS`. The caps are startup-only: `ConfigService.Set` keeps the running values. No healthy host should reach the defaults; a host that does now loses that namespace's excess rows, where it used to collect them all ([polling and batching](docs/polling-and-batching.md#per-namespace-caps)).
- **Socket-count gate, reconcile limits, per-owner quota.** Before each poll xtcp2 reads each namespace's sockstat (an fd opened in the namespace; `pread`, or io_uring batches with `-ioUring`) and, when the total is over `-maxRecordsPerPoll` (default 1,048,576), degrades only the namespaces over their fair share: first TIME_WAIT/SYN_RECV are not asked for, then one of K port shards is dumped per poll. `-maxNetnsPerOwner` (default 64) caps namespaces tracked per owner (the container runtime's cgroup), `-maxNetnsAddsPerReconcile` (256) the adds per reconcile, and `-reconcileTimeout` (1s) the namespace scan; a timed-out scan deletes nothing. New metrics `xtcp_poll_sockets`, `xtcp_poll_degraded_total{level}` and `xtcp_reconcile_incomplete_total{reason}`; `xtcp_netns_dropped_total` gains `reason`. `0` turns each off; all are startup-only. Each tracked namespace now holds two fds ([polling and batching](docs/polling-and-batching.md#socket-count-gate), [network namespaces](docs/network-namespaces.md#configuration)). The analysis behind an upstream all-namespace dump is in [docs/kernel](docs/kernel/inet-diag-all-netns.md).
- **Vulnerability check.** `checks.govulncheck` runs govulncheck against a pinned snapshot of the Go vulnerability database, with no tags and with every flavor tag, and fails on a reachable finding not in its accepted list. One is accepted: GO-2026-6859 in `net/http`, fixed in go1.27.4, until the toolchain moves. `nix run .#govulncheck-fresh` checks against the live database and runs `go mod verify`; `nix build .#sbom-<binary>` writes a CycloneDX SBOM for any binary package.

## 0.2.1 (2026-11)

Fixes for what the first fleet rollout of 0.2.0 found: the link monitor baseline, the uplink columns, and container attribution. No schema, flag-default or `XtcpConfig` change; 0.2.0 clients and consumers work unchanged.

### Upgrade checklist

1. **Mount the netns dirs `rslave`** in a container deployment (`-v /run/netns:/run/netns:ro,rslave`, the same for `/run/docker/netns`). Without it, pods started after xtcp2 are not collected; 0.2.1 now warns at startup and sets a gauge (below).

### New

- **Link monitor learn deadline.** `-linkMonitorLearnDeadline` (`LINK_MONITOR_LEARN_DEADLINE`, `link_monitor.learn_deadline`; go-link-monitor `-learn-deadline`), default 5m, 0 waits forever. If no baseline is learned by then, the visible up count becomes a provisional baseline, so `up_links_delta` still reports when the count never becomes ready (a stuck RDMA port, a hardware fault). It is kept in memory only, never written to the baseline file, and flagged by the sparse `go_link_monitor_baseline_provisional 1`; a normally learned baseline replaces it and is saved.
- **Netns mount propagation check.** In a container, `/run/netns` and `/run/docker/netns` must be mounted `rslave` (`-v /run/netns:/run/netns:ro,rslave`). With Docker's default `rprivate`, namespaces created after xtcp2 starts are invisible: their sockets aren't collected and their container name/image stays empty. xtcp2 now logs a warning at startup and sets `xtcp_gauges{function="checkNetnsPropagation",variable="private",type="<dir>"}` to 1 for each such dir.
- **`goip vrf show NAME`**, `ip`'s `ipvrf_get_table` single lookup: prints `NAME TABLE`, `Invalid VRF` (exit 1) for a device that isn't a VRF, and table 254 for `default`. Byte-for-byte with iproute2 7.2.0; the parity matrix is 510 rows, 509 gated.

### Fixes

- **Link monitor baseline never written on most prod hosts.** Two causes, found on 78 of 105 hosts: the stock Ubuntu `ib_core` answers the RDMA netlink event subscription with `EOPNOTSUPP`, which was retried every 30 s and kept the count unready; and the bonding module's `/sys/class/net/bonding_masters`, a regular file, failed the RDMA netdev walk. A kernel without RDMA events now falls back to polled RDMA state (reported once, as `rdma_events` support `unsupported`), and non-netdev entries are skipped.
- **Uplink columns: `lo` as UPLINK2, empty NIC and LLDP columns.** Automatic uplink detection took the kernel's `unreachable ::/0 dev lo`, which every host has, as a default route, and used a bond itself as the uplink, which has no NIC attributes and no LLDP neighbour. Only up, non-reject default routes off `lo` count now, and a bond, vlan or bridge holding the route is replaced by the physical NICs beneath it. `-uplinkInterfaces` is unchanged.

## 0.2.0 (2026-10)

The first tagged release. These notes compare against `32a4fed4` (2026-08-17), the last revision most deployments built from; that tree also said `0.1.0` in `VERSION`.

### Upgrade checklist

1. **Update every consumer of the records before the daemons.** Schema epoch 2 renames columns (below); a table that maps by column name reads the renamed ones as NULL until it knows the new names.
2. **Upgrade `xtcp2ctl` and `xtcp2client` together with the daemon.** `XtcpConfig` was renumbered, and an old client talking to a new daemon (or the reverse) gets garbled config from `Get`/`Set` with no error.
3. **Give the daemon a writable `/var/lib/xtcp2`.** The link monitor is on by default and keeps its baseline there. If it can't create the directory or its lock file (a read-only root filesystem without that mount, say), the link monitor stops: `xtcp_gauges{function="linkMonitor",variable="up"}` reads 0 and its paths answer 503. Collection carries on either way. `LINK_MONITOR=false` turns it off.
4. **Check the listener TTL.** The Prometheus and gRPC TCP listeners now reply with TTL / hop limit 3, so a scraper more than three hops away gets no answer. Set `IPV4_TTL` / `IPV6_HOP_LIMIT` (0 = kernel default) for a distant scraper.
5. **Re-check `-netlinkers`** if you relied on the old default of 4.

### Breaking changes

- **Record schema epoch 2** (`schema_version = 2`). About twenty payload fields now use the kernel's struct member spelling, for example `tcp_info_rtt_var` → `tcp_info_rttvar`, `congestion_algorithm_string` → `inet_diag_cong`, `c_group` → `inet_diag_cgroup_id`. Three tags moved: `enrich_socket_dest_egress_ifindex` 301 → 311, `enrich_socket_dest_egress_ifname` 302 → 312, `inet_diag_cgroup_id` 2103 → 2003. The ClickHouse `Locality` enum label 2 is now `local_subnet` (was `connected_subnet`); the value is unchanged. Full table: [docs/record-versioning.md](docs/record-versioning.md#epoch-1--2-rename-table).
- **`XtcpConfig` renumbered** (part of epoch 2). Clients and daemon must match; see the checklist.
- **Changed defaults:**

  | Flag | Env | Was | Now |
  |---|---|---|---|
  | `-netlinkers` | `NETLINKERS` | 4 | 2 |
  | `-ipv4Ttl` | `IPV4_TTL` | 0 (kernel default) | 3 |
  | `-ipv6HopLimit` | `IPV6_HOP_LIMIT` | 0 (kernel default) | 3 |
  | `-linkMonitor` | `LINK_MONITOR` | (new) | true |

- **Changed meanings:**
  - `-netlinkers` now counts decoders per socket; one receiver reads each socket.
  - `-ioUring` covers netlink reads only; destination writes always use syscalls.
  - `-ioUringRecvBatch` is the number of receive buffers per socket (rounded up to a power of two), not SQEs per netlinker.
  - `-healthcheck` follows `PROM_LISTEN_NETWORK` (TCP or a Unix socket) and sends the configured listener credential.
- No flag or environment variable was removed or renamed; 52 were added.

### New

- **Link monitor, built in and on by default** ([design](docs/design-linkmonitor-in-xtcp2.md)). go-link-monitor runs inside xtcp2. `/metrics` carries only its problem summary (a handful of series plus one per active problem); `/metrics/<category>[/<iface>]` and `/metrics-all` serve the detail, collected on demand. It learns the expected up-link count on first start, persists it to `/var/lib/xtcp2/link-monitor-baseline.json`, relearns on SIGUSR1, and degrades to 503 on its own paths without stopping collection. `LINK_MONITOR=false` turns it off.
- **Destination IP metadata enrichment** ([docs](docs/ipfeed-asn-enrichment.md)). `-enrichAsn` stamps `enrich_socket_dest_asn` and `enrich_socket_dest_network_owner` from one lookup table built by `ipfeed-collector` from ~19 cloud and SaaS provider feeds. The `*-bootstrap` images carry a checked-in snapshot, so enrichment works from the first poll; `-asnDbPath` and `-asnRefreshInterval` pick up a fresher file from an `oci-ipfeed-collector` sidecar, and the daemon caches the last good table under `/var/lib/xtcp2/ipmeta`. `-enrichLocality` labels each destination self / local_subnet / remote and records the egress interface.
- **Listener authentication** ([design](docs/design-listener-security-hardening.md), [scraping](docs/observability.md#scraping-with-listener-auth)). `-listenerAuthMode` (`raw` or `hmac-utc-minute`) protects every path on the Prometheus listener and every gRPC method. `-listenerAdminAuthMode` adds a second credential for admin RPCs (config setters, link-monitor rebaseline and resync; any method not listed as read-only) and `/debug/pprof/`; the first then becomes read-only. Tokens can come from files (`LISTENER_RAW_TOKEN_FILE`, ...). Failures are delayed by a random 20–200 ms.
- **Unix socket listeners.** `-promListenNetwork unix` and `-grpcListenNetwork unix` serve the metrics and gRPC listeners on a socket, mode 0600 by default (`-promUnixSocketMode`, `-grpcUnixSocketMode`).
- **Admin gRPC API** with the read/admin split above, plus runtime setters such as `SetEnvelopeFlush`.
- **Accurate ECN**: 11 AccECN `tcp_info` columns, which were previously dropped at ingestion.
- **New tools**: `goip`, a read-only Go `ip` with byte-for-byte output parity for the objects it covers, also served under `/ip` with `-linkMonitorIPPaths` (off by default), or on a loopback listener of its own, without listener auth, with `-linkMonitorIPListen 127.0.0.1:9089`; `ipmeta-bootstrap`; `nix run .#update-ipmeta-bootstrap-lock` to refresh the snapshot.
- **RDMA**: the link monitor's RDMA collection is pure Go (netlink and umad); the cgo path is gone.

### Fixes

- A mid-poll envelope flush at the size cap no longer drops the records that triggered it.
- io_uring netlink reads keep packets in order (one multishot recv per socket).
- The daemon's own pid no longer keeps a network namespace it discovered alive.
- Signals are routed before flags are parsed, and a soft restart re-execs only after the profilers stop.
- `PROM_UNIX_SOCKET_MODE` / `GRPC_UNIX_SOCKET_MODE` are parsed as octal (`0660`) or decimal; values above `0777` are refused with a log line instead of setting stray mode bits.
- `ipfeed-collector`: Salesforce's feed, which lists several prefixes per record, parses again; the Microsoft 365 feed is requested with a valid client request id, which the service now requires.
- Hot-path Prometheus series are looked up once; two per-record series moved behind `-debugPrometheus`.
