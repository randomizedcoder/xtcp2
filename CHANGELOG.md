# Changelog

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
- No flag or environment variable was removed or renamed; 51 were added.

### New

- **Link monitor, built in and on by default** ([design](docs/design-linkmonitor-in-xtcp2.md)). go-link-monitor runs inside xtcp2. `/metrics` carries only its problem summary (a handful of series plus one per active problem); `/metrics/<category>[/<iface>]` and `/metrics-all` serve the detail, collected on demand. It learns the expected up-link count on first start, persists it to `/var/lib/xtcp2/link-monitor-baseline.json`, relearns on SIGUSR1, and degrades to 503 on its own paths without stopping collection. `LINK_MONITOR=false` turns it off.
- **Destination IP metadata enrichment** ([docs](docs/ipfeed-asn-enrichment.md)). `-enrichAsn` stamps `enrich_socket_dest_asn` and `enrich_socket_dest_network_owner` from one lookup table built by `ipfeed-collector` from ~19 cloud and SaaS provider feeds. The `*-bootstrap` images carry a checked-in snapshot, so enrichment works from the first poll; `-asnDbPath` and `-asnRefreshInterval` pick up a fresher file from an `oci-ipfeed-collector` sidecar, and the daemon caches the last good table under `/var/lib/xtcp2/ipmeta`. `-enrichLocality` labels each destination self / local_subnet / remote and records the egress interface.
- **Listener authentication** ([design](docs/design-listener-security-hardening.md), [scraping](docs/observability.md#scraping-with-listener-auth)). `-listenerAuthMode` (`raw` or `hmac-utc-minute`) protects every path on the Prometheus listener and every gRPC method. `-listenerAdminAuthMode` adds a second credential for admin RPCs (config setters, link-monitor rebaseline and resync; any method not listed as read-only) and `/debug/pprof/`; the first then becomes read-only. Tokens can come from files (`LISTENER_RAW_TOKEN_FILE`, ...). Failures are delayed by a random 20–200 ms.
- **Unix socket listeners.** `-promListenNetwork unix` and `-grpcListenNetwork unix` serve the metrics and gRPC listeners on a socket, mode 0600 by default (`-promUnixSocketMode`, `-grpcUnixSocketMode`).
- **Admin gRPC API** with the read/admin split above, plus runtime setters such as `SetEnvelopeFlush`.
- **Accurate ECN**: 11 AccECN `tcp_info` columns, which were previously dropped at ingestion.
- **New tools**: `goip`, a read-only Go `ip` with byte-for-byte output parity for the objects it covers, also served under `/ip` with `-linkMonitorIPPaths` (off by default); `ipmeta-bootstrap`; `nix run .#update-ipmeta-bootstrap-lock` to refresh the snapshot.
- **RDMA**: the link monitor's RDMA collection is pure Go (netlink and umad); the cgo path is gone.

### Fixes

- A mid-poll envelope flush at the size cap no longer drops the records that triggered it.
- io_uring netlink reads keep packets in order (one multishot recv per socket).
- The daemon's own pid no longer keeps a network namespace it discovered alive.
- Signals are routed before flags are parsed, and a soft restart re-execs only after the profilers stop.
- `PROM_UNIX_SOCKET_MODE` / `GRPC_UNIX_SOCKET_MODE` are parsed as octal (`0660`) or decimal; values above `0777` are refused with a log line instead of setting stray mode bits.
- `ipfeed-collector`: Salesforce's feed, which lists several prefixes per record, parses again; the Microsoft 365 feed is requested with a valid client request id, which the service now requires.
- Hot-path Prometheus series are looked up once; two per-record series moved behind `-debugPrometheus`.
