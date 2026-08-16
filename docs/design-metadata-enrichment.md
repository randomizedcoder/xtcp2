# Metadata enrichment: container / netns / LLDP topology / NIC

xtcp2 stamps every TCP-socket record with metadata columns. Historically several
were dead in production:

| Column | Old symptom | Root cause |
|---|---|---|
| `container_id` / `container_runtime` | 100% empty | only host-netns sockets were ever sampled (see discovery bug) |
| `netns` | 100% the literal `"default"` | namespace discovery never saw non-host namespaces |
| `nsid` | 100% zero | never populated in Go |

This feature fixes discovery and adds four **optional, best-effort** enrichers.
Each one is independently toggleable, and *any* failure (socket unreachable,
sysfs unreadable, parse error) is logged, counted in Prometheus, and skipped —
it can never make the daemon fatal. LLDP + NIC are captured **once at startup**
(static per boot); container/netns/nsid are captured **per namespace** as
namespaces appear.

## 1. Discovery fix (the linchpin)

Namespace discovery is a `/proc` scan ("Method B", `pkg/nsdiscover`): it can only
see a namespace that has a live process visible in `/proc`. In production the
deploy container does **not** set `pid_mode: host`, so `/proc` shows only xtcp2's
own process → discovery found only xtcp2's own (host) namespace → every record
was emitted through the hard-coded `"default"` socket, and only host-netns
sockets (mostly with no container cgroup) were sampled.

The fix (`pkg/xtcp/ns_discover.go` → `discoverNamespaces`) makes discovery a
**union** of two passes:

1. the existing `/proc` scan (live-pid entry via `/proc/<pid>/ns/net`);
2. a **bind-mount scan** of the already-mounted `/run/netns` + `/run/docker/netns`
   directories (`pkg/nsdiscover` `Resolver.EachBindMount`), entering each
   namespace by opening its bind-mount file and `setns`-ing the fd directly
   (`pkg/xtcp/ns_net_namespace.go`).

Pass 2 needs **no host PID**, so container/pod namespaces become visible on the
current production deployment (which already mounts both dirs read-only). The
`/proc`-scan entry wins when both passes find the same inode. `pid_mode: host`
remains a valid *complementary* knob but is no longer required.

`netns_inode` (the nsfs inode) is the canonical, stable namespace key. `nsid`
(NETNSA_NSID) is a small kernel-relative id, usually unassigned for
Docker/containerd namespaces; it is opt-in (`-populateNsid`) and queried
best-effort via `RTM_GETNSID` (`pkg/nsdiscover.Nsid`).

## 2. The four enrichers

| Enricher | Source | Package | Cadence |
|---|---|---|---|
| Container/netns labels | Docker Engine API over `/run/docker.sock` | `pkg/dockermeta` | per new namespace + event stream / slow refresh |
| LLDP neighbor | lldpd control socket `/run/lldpd.socket` (pure-Go wire parser) | `pkg/lldp` | once at startup |
| NIC info | sysfs + `ETHTOOL_GDRVINFO` ioctl + embedded pci.ids | `pkg/nicinfo` | once at startup |
| `nsid` | `RTM_GETNSID` rtnetlink | `pkg/nsdiscover` | per namespace (opt-in) |

Wiring lives in `pkg/xtcp/enrich.go` (`initEnrichers`, `applyEnrichment`,
`buildUplinkStamp`, `closeEnrichers`, `refreshNsids`). Container labels are
resolved by joining the socket's **netns inode** against the Docker index; the
cgroup-id resolver (`pkg/cgroupid`) remains a fallback for host-net container
sockets. The static uplink NIC/LLDP columns are precomputed once and copied onto
every record on the hot path (they dictionary-compress to ~nothing downstream).

Topology is modeled as **two fixed uplink slots** (`uplink1_*`, `uplink2_*`) since
hosts are dual-homed. Uplinks default to the interfaces on the default IPv4/IPv6
routes (`nicinfo.DefaultUplinks`), overridable with `-uplinkInterfaces`.

## 3. Config: flags / env / config protobuf

Precedence: **defaults < flags < env < `XTCP_CONFIG_JSON`** (`cmd/xtcp2/xtcp2.go`).

| Flag | Env | Config field | Default |
|---|---|---|---|
| `-enrichContainer` | `ENRICH_CONTAINER` | `enrich_container_enable` | false |
| `-dockerSocket` | `DOCKER_SOCKET` | `docker_socket_path` | `/run/docker.sock` |
| `-enrichLldp` | `ENRICH_LLDP` | `enrich_lldp_enable` | false |
| `-lldpdSocket` | `LLDPD_SOCKET` | `lldpd_socket_path` | `/run/lldpd.socket` |
| `-lldpdVersionHint` | `LLDPD_VERSION_HINT` | `lldpd_version_hint` | "" (auto) |
| `-enrichNic` | `ENRICH_NIC` | `enrich_nic_enable` | false |
| `-uplinkCount` | `UPLINK_COUNT` | `uplink_count` | 2 |
| `-uplinkInterfaces` | `UPLINK_INTERFACES` (CSV) | `uplink_interfaces` | auto (default routes) |
| `-populateNsid` | `POPULATE_NSID` | `populate_nsid` | false |

## 4. ClickHouse

New/reordered columns land in `build/containers/clickhouse/initdb.d/sql/`
(`xtcp_xtcp_flat_records.sql` + `_kafka.sql`, kept column-order-identical because
the MV maps positionally via `SELECT * EXCEPT (timestamp_ns)`; ClickHouse's
Protobuf format maps by field **name**). New columns: `container_name`,
`container_image`, `nsid`, and the `uplink1_*` / `uplink2_*` blocks. The
per-host-constant string columns use `LowCardinality(String)`.

## 5. Deployment (ansible-host — separate repo)

Runtime config lives in `ansible-host/ansible/roles/monitoring/xtcp2/`. The
current deploy **already** mounts `/run/netns:ro` and `/run/docker/netns:ro`, so
the discovery fix (§1) restores `netns`/`container_id` with **no deploy change**
once the new image ships.

To enable the *new* enrichers, add (recommended as **opt-in** ansible vars,
default off, mirroring `xtcp2_resolve_container_id`):

- **Mounts** (`tasks/main.yml`, the `volumes:` block):
  - `/run/docker.sock:/run/docker.sock:ro` — container labels.
  - `/run/lldpd.socket:/run/lldpd.socket:rw` — LLDP (control socket is
    bidirectional). Only on hosts that actually run lldpd; gate the mount on a
    var so hosts without lldpd don't get an empty bind-mount dir.
  - `/sys/class/net:/sys/class/net:ro` — NIC info (only if not already visible;
    the rootfs is `read_only: true`, which is why pci.ids is embedded, not read
    from the host).
- **Env** (`tasks/main.yml`, the `env:` block): `ENRICH_CONTAINER`, `ENRICH_LLDP`,
  `ENRICH_NIC` (+ socket-path overrides if diverging from the defaults above).
- **Defaults** (`defaults/main.yml`): `xtcp2_enrich_container`, `xtcp2_enrich_lldp`,
  `xtcp2_enrich_nic` (all `false`), plus the socket-path vars.

`pid_mode: host` is an optional complementary discovery aid — not required.

### lldpd version caveat

The pure-Go LLDP parser (`pkg/lldp`) implements the lldpd control-protocol struct
layouts for **1.0.13** and **1.0.18** (only `struct lldpd_hardware` changed size
between them). For a newer lldpd on the target hosts, set `-lldpdVersionHint`
appropriately, or add a matching `Layout` preset in `pkg/lldp/lldp.go` (the only
version-dependent number is the `lldpd_hardware` fixed-block size). A layout
mismatch is non-fatal: LLDP columns are simply left empty.
