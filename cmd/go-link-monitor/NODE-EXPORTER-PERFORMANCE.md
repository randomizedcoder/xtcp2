# go-link-monitor and node_exporter network collector performance

Status: measurement, 2026-10-14. On demand is now the default collection
mode; the poll measurements below were taken in poll mode, then the default.
One run on one host per section; repeat with the procedure below before
quoting these numbers as general results. The
benchmark conventions are in
[PERFORMANCE-TESTING-DESIGN.md](PERFORMANCE-TESTING-DESIGN.md), and the paths
measured are described in [METRICS-PATHS-DESIGN.md](METRICS-PATHS-DESIGN.md).

## Summary

- Per scrape, the go-link-monitor summary on `/metrics` costs about 0.15 ms of
  exporter CPU, against 62 ms for node_exporter's network collectors: about
  400× less, with an 871 B response instead of 720 KB.
- `/metrics-all`, which serves about the same 720 KB as node_exporter, costs
  21 ms of CPU per scrape (3× less) and answers in 12.6 ms instead of 88 ms
  (7× faster).
- node_exporter costs nothing between scrapes. go-link-monitor polls in the
  background whether or not anyone scrapes: 3 ms of CPU per second, or about
  0.3% of one core, at `-stats-interval=15s`.
- go-link-monitor's resident memory is higher: 42–61 MB against 28–34 MB.
- At a 15s scrape interval the totals are about 11 s of CPU per hour for
  go-link-monitor (summary only, almost all background polling) against 15 s
  for node_exporter. Scraping `/metrics-all` every 15s as well brings
  go-link-monitor to about 16 s per hour, roughly equal. node_exporter's cost
  is multiplied by every additional scraper; go-link-monitor's barely moves.
- Counting only the work of reading from the kernel and drivers, one
  go-link-monitor poll costs about 12 ms of CPU and 2,144 system calls against
  40 ms and 7,036 for one node_exporter collection: about 3× less of each. See
  [Collection cost](#collection-cost).
- The exception is RDMA counters: both exporters open, read and close one
  sysfs file per counter, and that is about 40% of a go-link-monitor poll.
  Holding the files open and reading them in parallel cuts the read time from
  20 ms to under 3 ms. io_uring does not beat that; see
  [RDMA counter reads and io_uring](#rdma-counter-reads-and-io_uring).
- Most of that cost is the mlx5 driver. Reading `hw_counters` in name order,
  as both exporters do, misses the driver's cache on every file: 8 ms per
  port. One RDMA netlink request, or reading `rx_write_requests` first, takes
  0.3 ms. See [What the mlx5 driver does](#what-the-mlx5-driver-does).
- On demand (`-collection.mode=on-demand`, watch 60s, slow 300s), idle
  go-link-monitor uses 110 ms of CPU and 4,055 system calls per 10 minutes,
  against 270 ms and 14,371 when polling every 15s. A detail scrape then
  collects in about 18 ms. See [On-demand collection](#on-demand-collection).

## Why the costs differ

node_exporter does all collection inside the scrape. For each interface its
ethtool collector issues the driver-information, link and statistics calls,
then for each of about 4,900 statistics sanitizes the name, matches it against
the include pattern, builds two maps and sorts them before emitting. A second
Prometheus replica, a dashboard or an ad-hoc `curl` repeats all of it.

go-link-monitor collects on its own schedule (`-stats-interval`, plus events)
into an immutable snapshot. Metric names are matched once per distinct name for
the process lifetime and counters are reduced once per poll. A scrape only
reads the published snapshot: the summary walks the candidate list of
interfaces with problems, and a category path skips whole sample blocks with
no selected family. Scrape cost is therefore proportional to what the path
emits, and the background cost is independent of how often anyone scrapes.

## Setup

| Item | Value |
|---|---|
| Host | `l`, AMD Ryzen Threadripper PRO 3945WX, 24 threads, Linux 7.2.9 |
| Interfaces | 14 netdevs: `enp1s0` (atlantic), `enp35s0f0np0` and `enp35s0f1np1` (mlx5_core, RoCE `mlx5_0`, `mlx5_1`), and 11 bridge, tap, veth and loopback devices |
| mlx5 NIC | ConnectX-4 Lx (MT4117, PCI `15b3:1015`), firmware 14.27.4000, two ports |
| Toolchain | go1.26.8 for both builds |
| node_exporter | 1.11.1 from `~/Downloads/node_exporter` at `c266744c` |
| go-link-monitor | This branch (`6def4e39` plus the metric-paths changes), built without the `rdma` tag |
| Privileges | Both unprivileged, listening on loopback, client on the same host |

node_exporter ran only the collectors that read the same sources as
go-link-monitor, with all netstat fields to match go-link-monitor's default:

```sh
node_exporter --web.listen-address=127.0.0.1:19100 --collector.disable-defaults \
  --collector.netdev --collector.netclass --collector.ethtool \
  --collector.netstat --collector.netstat.fields='.*' --collector.infiniband
go-link-monitor -metrics-addr=127.0.0.1:19201 -baseline-file=/tmp/baseline.json \
  -collection.mode=poll -stats-interval=15s -max-speed-exceptions=enp1s0=1G -admin.address=
```

Coverage was comparable: 6,102 node_exporter series against 6,136 on
`/metrics-all`.

| Data | node_exporter | go-link-monitor `/metrics-all` |
|---|---|---|
| ethtool driver statistics | 4,922 | 4,846 |
| netstat fields | 397 | 397 |
| InfiniBand counters | 110 | 110 |
| Netdev and netclass | 612 (all 14 netdevs) | 69 traffic counters plus link families (3 eligible interfaces) |
| Collection diagnostics | none | 453 `collector_*` series |

node_exporter exports virtual interfaces that go-link-monitor excludes;
go-link-monitor exports per-source diagnostics that node_exporter does not
have. Neither difference changes the conclusions, as both totals are about
720 KB.

## Results

### Idle

Two minutes with no scrapes, after both had settled:

| Exporter | CPU in 120 s | Per second |
|---|---|---|
| node_exporter | 0 ms | 0 |
| go-link-monitor | 360 ms | 3 ms (0.3% of one core) |

### Per scrape

Sequential scrapes over one keep-alive connection; CPU is the exporter
process's user plus system time divided by the number of scrapes.

| Endpoint | Scrapes | Bytes | Mean | p50 | p99 | CPU per scrape |
|---|---|---|---|---|---|---|
| node_exporter `/metrics` | 300 | 720,146 | 88.2 ms | 83.9 ms | 146.0 ms | 61.7 ms |
| go-link-monitor `/metrics` | 3,000 | 871 | 0.13 ms | 0.12 ms | 0.29 ms | 0.15 ms |
| `/metrics/link` | 300 | 12,825 | 0.37 ms | 0.35 ms | 0.80 ms | 0.57 ms |
| `/metrics/traffic` | 300 | 11,233 | 0.26 ms | 0.24 ms | 0.64 ms | 0.37 ms |
| `/metrics/ethtool` | 300 | 12,023 | 0.32 ms | 0.30 ms | 0.72 ms | 0.47 ms |
| `/metrics/driver` | 300 | 522,290 | 9.29 ms | 9.32 ms | 11.19 ms | 15.87 ms |
| `/metrics/rdma` | 300 | 24,760 | 0.45 ms | 0.41 ms | 1.07 ms | 0.73 ms |
| `/metrics/netstat` | 300 | 91,133 | 0.87 ms | 0.81 ms | 1.78 ms | 1.33 ms |
| `/metrics/collection` | 300 | 50,078 | 0.89 ms | 0.83 ms | 1.95 ms | 1.53 ms |
| `/metrics-all` | 300 | 722,888 | 12.58 ms | 11.58 ms | 24.93 ms | 21.10 ms |

CPU per scrape can exceed the mean latency because promhttp runs collectors
on parallel goroutines and the garbage collector runs concurrently.

### Memory

| Exporter | RSS before scrapes | RSS after all scrapes |
|---|---|---|
| node_exporter | 28.2 MB | 33.7 MB |
| go-link-monitor | 41.9 MB | 60.7 MB |

go-link-monitor holds the published snapshot, counter histories and the
per-collector catalogs. The increase during the run was not profiled; it is
most likely Go heap growth from the large `/metrics-all` and
`/metrics/driver` responses.

### CPU per hour at a 15s scrape interval

Derived from the tables above (240 scrapes per hour):

| Configuration | CPU per hour |
|---|---|
| node_exporter, one scraper | 240 × 61.7 ms ≈ 14.8 s |
| node_exporter, two Prometheus replicas | ≈ 29.6 s |
| go-link-monitor, `/metrics` only | 10.8 s background + 0.04 s ≈ 10.8 s |
| go-link-monitor, `/metrics` plus `/metrics/link` and `/metrics/rdma` every 1m and `/metrics/netstat` every 5m | ≈ 10.9 s |
| go-link-monitor, `/metrics` and `/metrics-all` every 15s | 10.8 s + 240 × 21.1 ms ≈ 15.9 s |
| go-link-monitor, `/metrics` with two replicas | ≈ 10.9 s |

The background cost scales with `-stats-interval` and with the number of
interfaces and driver statistics, not with scrapes. It also covers sources
node_exporter does not poll in this configuration: carrier counters, link
settings, channels, rings, RDMA state and freshness tracking.

## Collection cost

The per-scrape numbers above include encoding the response. This section
compares only collection: the system calls into the kernel and the drivers,
and the processing of what they return.

### Method

- **node_exporter.** A 30 s CPU profile from its `/debug/pprof/profile`
  endpoint while scraping `/metrics` back to back (354 scrapes). Collection is
  everything under `collector.execute`; the HTTP handler's gathering and
  encoding are counted separately.
- **go-link-monitor.** Every source polls on `-stats-interval`, so it ran with
  `-stats-interval=1s` and no scrapes, and process CPU from `/proc/<pid>/stat`
  over 120 s was divided by 120 polls. `perf record -g` over 30 s gave the
  breakdown.
- **System calls.** A second instance of each ran under `strace -f -ttt`; the
  calls inside a window of 20 node_exporter scrapes, or 30 go-link-monitor
  polls, were counted per cycle.

### Per collection cycle

| | node_exporter, per scrape | go-link-monitor, per poll |
|---|---|---|
| Collection CPU | 39.7 ms | ≈ 12 ms (5 user, 7 system) |
| of which in system calls | 11.4 ms | ≈ 7 ms |
| Encoding and HTTP, not collection | 13.2 ms | 0 (scrapes read the snapshot) |
| System calls | 7,036 | 2,144 |
| `ioctl(SIOCETHTOOL)` | 61 (14 netdevs) | 10 (3 eligible ports) |
| sysfs files under `/sys/class/net` | 453 | 2 |
| sysfs files under `/sys/class/infiniband` | 131 | 144 |
| Link and traffic state | sysfs attribute files and `/proc/net/dev` | rtnetlink, about 14 `recvmsg` |
| netstat | 3 `/proc/net` files | the same 3 files |

go-link-monitor's poll also includes work node_exporter does not do: counter
reduction and histories, freshness, problem evidence and snapshot
publication.

### Where the time goes

node_exporter, per scrape:

| Collector | CPU | of which system calls | Main cost |
|---|---|---|---|
| ethtool | 27.1 ms | 1.3 ms | `regexp.ReplaceAllString` sanitizing about 4,900 statistic names on every scrape: 18.3 ms |
| netclass | 7.9 ms | 6.5 ms | Opening, reading and closing one sysfs file per attribute per interface |
| infiniband | 3.6 ms | 3.2 ms | One sysfs file per counter |
| netstat | 0.7 ms | 0.1 ms | |
| netdev | 0.4 ms | 0.3 ms | |

go-link-monitor, per poll (share of process CPU from `perf`, and each
collector's own `collector_duration_seconds` wall time summed over its
sources):

| Collector | CPU share | Wall time | Main cost |
|---|---|---|---|
| RDMA counters | ≈ 44% | 29.4 ms | One sysfs file per counter, 9 system calls each, plus the identity guard below |
| ethtool driver and PHY statistics | ≈ 11% | 7.1 ms and 3.7 ms | The `ioctl` itself; names are matched once per process lifetime |
| settings, identity, carrier, standard, channels, rings | small | 4.9, 1.3, 0.8, 0.8, 0.4 and 0.3 ms | Netlink and `ioctl` |
| netstat | small | 0.4 ms | |
| Scheduler, reducer and publication | ≈ 10% | | |

Each RDMA counter costs `openat`, two `fcntl` and two `epoll_ctl` calls (Go
registers sysfs files with its poller), two `read` calls and a `close`. Each
RDMA port read is also bracketed by an identity guard: `os.Stat` of the port
directory and `filepath.EvalSymlinks` of the device link before and after,
which adds about 250 `newfstatat` and `readlinkat` calls per poll. About 1,500
of the 2,144 calls per poll are RDMA counter reads.

Most of the wall time, and most of the system time, is the driver: on mlx5
each counter read is a firmware command, about 190 µs each when read one after
another. Both exporters pay it; node_exporter's infiniband collector reported
70 ms of wall time per scrape.

### Background cost at the default interval

At `-stats-interval=15s` a 120 s idle window measured 350 ms of CPU, about
44 ms per 15 s rather than one 12 ms poll. In the profile, polling was 40%.
Two costs that do not depend on the interval made up most of the rest:

- RDMA inventory rescans, including reading every GID, every 30 s: 17%. This
  build has no `rdma` tag, so the RDMA event source cannot open. Each retry,
  capped at 30 s, invalidated every RDMA port and forced a full resync, which
  also re-collected every collector on those ports. This was a defect, not a
  fallback. The `rdma_state` poll and `-resync` already cover state and
  topology without events. It is fixed in
  [On-demand collection](#on-demand-collection).
- `freezeDescriptors` and `describeSchemas` at publication: 18%.

## RDMA counter reads and io_uring

A standalone program read the same 106 counter files under
`/sys/class/infiniband/*/ports/*/{counters,hw_counters}` (two mlx5 ports) 200
times with each method. It reads files only; go-link-monitor's identity guard
and sample handling are not included. CPU is from `getrusage`, which includes
io_uring worker threads.

| Method | Wall per poll | User | System | System calls per poll |
|---|---|---|---|---|
| `os.Open`, `io.ReadAll`, `Close` (current) | 21.7 ms | 0.5 ms | 3.8 ms | ≈ 950 |
| `unix.Open`, `unix.Read`, `unix.Close` | 21.4 ms | 0.4 ms | 3.6 ms | ≈ 320 |
| Held descriptors, `pread` at offset 0 | 20.2 ms | 0.4 ms | 2.6 ms | 106 |
| Held descriptors, `pread`, 8 goroutines | 2.7 ms | 0.15 ms | 0.8 ms | 106 |
| io_uring, held descriptors, one batch of `READ` | 20.1 ms | 0.05 ms | 2.2 ms | 1–2 |
| io_uring, registered descriptors | 20.1 ms | 0.05 ms | 2.2 ms | 1–2 |
| io_uring, registered descriptors, `IOSQE_ASYNC` | 5.6 ms | 0.07 ms | 2.9 ms | 1–2 |

Reading a held sysfs descriptor with `pread` at offset 0 returns a fresh value
each time; this was checked on a counter that changed between reads.

What this shows:

- **The kernel work dominates.** Removing system calls saves little CPU:
  io_uring with one submission for all 106 reads uses 2.2 ms of system time,
  against 2.6 ms for 106 `pread` calls. Going from 9 calls per file to 1 by
  holding descriptors is most of the available saving.
- **io_uring does not parallelize these reads by itself.** sysfs files do not
  support non-blocking reads, so each read runs inline, one after another, and
  the batch takes as long as sequential `pread`. Forcing the io_uring worker
  pool with `IOSQE_ASYNC` overlaps the firmware commands (5.6 ms), but a few
  goroutines doing `pread` overlap them better (2.7 ms) with less CPU.
- **io_uring brings costs.** The `giouring` dependency uses `go:linkname` to
  `syscall.mmap`, so binaries need `-ldflags=-checklinkname=0`.
  `RegisterFiles`, `RegisterFilesTags`, `RegisterFilesUpdate` and
  `RegisterFilesUpdateTag` pass a Go `[]int` (8 bytes per descriptor) where
  the kernel reads `__s32`, so fixed-file index `i` resolves to the wrong
  descriptor and odd indexes read descriptor 0. The table above registers
  through the raw system call. xtcp2's own io_uring code does not register
  files. io_uring is also often disabled (`kernel.io_uring_disabled`, container
  seccomp profiles), so a fallback path would be needed anyway.
- **The other sources are not candidates.** There is no io_uring operation for
  `ioctl(SIOCETHTOOL)`; `IORING_OP_URING_CMD` on sockets supports only a few
  socket commands. Netlink is already about 20 calls per poll. io_uring pays
  off in xtcp2's netlinker, where large inet_diag dumps keep many receives in
  flight, not for a few hundred small reads.

The driver analysis below changes the plan: most of the time is avoidable
firmware work, not system calls. The suggested change is in
[Suggested changes](#suggested-changes).

## What the mlx5 driver does

Source: Linux 7.3-rc6 (`af32da41b032`); the host runs 7.2.9. Paths are
relative to `drivers/infiniband/`.

### How a counter read reaches the card

The hardware counters below are not memory-mapped registers that the driver
reads, and the card does not stream them into host memory. Each refresh is a
request to the NIC firmware through the mlx5 command interface
(`drivers/net/ethernet/mellanox/mlx5/core/cmd.c`):

1. `mlx5_cmd_exec` queues the command on the device's single-threaded command
   workqueue. `cmd_work_handler` takes one of the command queue slots
   (`max_reg_cmds`, 2^`log_sz` − 1 as reported by the device; usually 31).
2. It writes the command descriptor into that slot in host memory. Input and
   output that do not fit inline go in chained 512-byte mailbox blocks
   allocated from a DMA pool.
3. After a write barrier it does one MMIO write of the slot's bit to the
   `cmd_dbell` doorbell in the initialization segment of BAR 0.
4. The firmware reads the descriptor and input mailboxes by DMA, runs the
   command, gathering the counters from the port, queue or congestion
   hardware, and writes the output back by DMA.
5. It posts a command completion event on an event queue and raises an
   interrupt. `mlx5_cmd_comp_handler` wakes the reading thread, which has
   been sleeping in `wait_for_completion`.

So DMA moves the data both ways, but the cost is a round trip to the
firmware's own processors, almost all of it spent waiting. On this card a PMA
file, one command, takes about 97 µs; an uncached `hw_counters` file, three
commands, about 260 µs. The kernel CPU cost per read is small, which is why the
system time in [Where the time goes](#where-the-time-goes) is far below the
wall time. Several commands can be outstanding at once, which is why parallel
reads of the uncached `counters` files help.

Not every statistic goes to the firmware. The per-ring ethtool statistics
(`rx<n>_*`, `tx<n>_*`, `ch<n>_*`) are software counters in host memory. The
rtnetlink link statistics (`ndo_get_stats64`, `mlx5e_get_stats`) return
values the driver already holds; when the device does not push counter-change
events (`mlx5e_monitor_counter_supported`) the read schedules a background
refresh for next time and does not wait for it. That is why go-link-monitor's
link and traffic state over rtnetlink is cheap.

### `ports/<n>/counters`: one firmware command per file

Each file is a `port_table_attribute` whose `show` is `show_pma_counter`
(`core/sysfs.c`). It calls `get_perf_mad`, which allocates a MAD, asks the
driver's `process_mad` for a whole PerfMgmt attribute (PortCounters or
PortCountersExtended), copies out one field and frees the rest. Nothing is
cached.

On mlx5, `process_pma_cmd` (`hw/mlx5/mad.c`) turns each MAD into one firmware
command on a RoCE port:

| Files | Firmware command | Returns |
|---|---|---|
| 12 error counters (`symbol_error`, `link_downed`, `port_rcv_errors`, …) and `port_xmit_wait` | `ACCESS_REG(PPCNT)`, InfiniBand port counters group | The whole group |
| 8 data and packet counters (`port_xmit_data`, `port_rcv_packets`, `unicast_*`, `multicast_*`) | `QUERY_VPORT_COUNTER` | The whole vport counter block |

So 21 files cost 21 firmware commands that return two distinct results. The
8 `QUERY_VPORT_COUNTER` values are the same data mlx5e exposes as the
`{rx,tx}_vport_rdma_{unicast,multicast}_{packets,bytes}` ethtool statistics,
which go-link-monitor already reads every poll (`port_xmit_data` is the byte
sum divided by 4).

### `ports/<n>/hw_counters`: a cache that name order defeats

`show_hw_stats` (`core/sysfs.c`) takes the port's `stats->lock`, and
`update_hw_stats` calls the driver only if the last refresh is older than
`lifespan` (default `RDMA_HW_STATS_DEFAULT_LIFESPAN`, 10 ms; writable per
port, 0–10,000 ms). The refresh counts as fresh only if the driver reports
that it filled every counter: `if (ret == stats->num_counters)`.

`mlx5_ib_get_hw_stats` (`hw/mlx5/counters.c`) passes a read of index `i` with
`0 < i < num_hw_counters` to `do_get_hw_stats`. That queries the Q counters
(`QUERY_Q_COUNTER`), plus the extended Ethernet `PPCNT` group and congestion
statistics when the device has those capabilities, and returns the hardware
counter count only. mlx5 always registers optional "op" counters, so
`num_counters` is larger and the timestamp is never set. Only a read of index
0 takes the full path, which also refreshes the op counters, and stamps the
cache. sysfs creates the files in index order, and index 0 on mlx5 is
`rx_write_requests`, which sorts last by name. The partial path was added in
`a020094090e5` ("RDMA/mlx5: Add optional counter support in get_hw_stats
callback", 2021-10-08).

Reads are also serialized by `stats->lock`, so reading in parallel does not
help.

### Measured on the host

`pread` at offset 0 on held descriptors for `mlx5_0` port 1, 50 rounds, 20 ms
apart so the 10 ms lifespan expires between rounds:

| Read | Files | Wall | Per file |
|---|---|---|---|
| `hw_counters` in name order (both exporters today) | 31 | 8.05 ms | 260 µs |
| `hw_counters`, `rx_write_requests` (index 0) first, then name order | 31 | 0.33 ms | 11 µs |
| `hw_counters`, only index 0 | 1 | 0.28 ms | |
| `hw_counters` in name order, 8 goroutines | 31 | 7.57 ms | |
| RDMA netlink `RDMA_NLDEV_CMD_STAT_GET` for the port (`rdma -b`, 200 requests) | all 31 | 0.31 ms | |
| `counters` in name order | 21 | 2.04 ms | 97 µs |
| `counters`, 8 goroutines | 21 | 0.54 ms | |

Each uncached `hw_counters` file costs about as much as a full refresh, so a
name-order walk does about 30 refreshes per port where 1 is enough. On this
host that is about 15.5 ms of the 29 ms `rdma_counters` wall time per poll,
and node_exporter's infiniband collector pays the same, since it also reads
the directory in name order.

`RDMA_NLDEV_CMD_STAT_GET` (`core/nldev.c`, `stat_get_doit_default_counter`)
calls `get_hw_stats` with index 0, so it always takes the full path, and
returns every counter of the port in one message. It needs no privileges;
`nldev_cb_table` does not mark it `RDMA_NL_ADMIN_PERM`. It reads the same
`stats->value` plus `rdma_counter_get_hwstat_value` as sysfs, and it does not
depend on which counter a driver puts at index 0. go-link-monitor already has
an RDMA netlink client in `internal/linuxio/rdma_linux.go`.

### mlx5e ethtool statistics

Every `ETHTOOL_GSTATS` on an mlx5 netdev runs `mlx5e_stats_update`
(`drivers/net/ethernet/mellanox/mlx5/core/en_stats.c`), which refreshes every
statistics group. The Q counter, vNIC environment, vport, IEEE 802.3, RFC 2863,
RFC 2819, physical layer, extended Ethernet, PCIe, per-port buffer congestion
and per-priority groups each issue firmware commands; the per-priority group
alone issues 8 `PPCNT` reads. That is the same for node_exporter, and
there is no narrower ethtool request. The cost is one `ioctl` per netdev per
poll, which is already the minimum.

### Other ConnectX generations

`mlx5_core` and `mlx5_ib` drive ConnectX-4 through ConnectX-10 and BlueField 1
to 4 (the PCI table in `mlx5/core/main.c`), with the same code paths. What
changes between generations is which capability bits the firmware reports,
not the logic above. This host has a ConnectX-4 Lx; ConnectX-5, 6 and 7 were
not measured. From the source:

- **The `hw_counters` cache miss applies to every generation.** The basic and
  packet op counters are registered without a capability check, so
  `num_counters` is always larger than the hardware count, and
  `rx_write_requests` is index 0 on every card (first in `basic_q_cnts`).
- **Each refresh costs at least as much on newer cards.** A refresh issues
  `QUERY_Q_COUNTER`, then `PPCNT` when the device reports
  `rx_icrc_encapsulated_counter`, then `QUERY_CONG_STATISTICS` when it reports
  `cc_query_allowed`. This ConnectX-4 Lx already has both, so it pays all
  three. With bonding (LAG) the congestion query runs once per bonded port.
- **Newer cards have more files to read.** `out_of_seq_cnt`,
  `retransmission_q_counters` and `enhanced_error_q_counters` each add Q
  counters, so a name-order walk does more refreshes per port.
- **PMA `counters` are one firmware command per file on any link layer.** On
  RoCE and on InfiniBand ports with `vport_counters` the driver answers with
  `PPCNT` or `QUERY_VPORT_COUNTER`; otherwise the MAD goes to the firmware with
  `MAD_IFC`. ConnectX-7 InfiniBand ports therefore pay the same, though the
  vport counters are not duplicated in ethtool there.
- **mlx5e ethtool statistics** refresh capability-gated groups that newer
  firmware tends to report (for example PCIe performance and per-lane error
  counters), so each `ETHTOOL_GSTATS` costs
  more, but it is still one `ioctl` per netdev.

Absolute times depend on the generation and firmware version, so the
measurements here will not transfer directly. The ratios do: a name-order
`hw_counters` walk does one refresh per file instead of one per port, and the
PMA files cost one command each instead of one per attribute. The suggested
netlink read, or reading index 0 first, works on all of them.

### Kernel improvements worth proposing upstream

- **mlx5 `hw_counters` caching.** In `mlx5_ib_get_hw_stats`, take the full
  path for a read of any hardware counter index, or have it report a full
  refresh when no op counter is enabled. Disabled op counters cost nothing
  to refresh (`do_get_op_stat` skips a counter with no flow counter). This
  restores the 10 ms cache for every reader of sysfs.
- **PMA counter caching.** Cache the `get_perf_mad` reply per port and
  attribute for a short lifespan in `core/sysfs.c`, as `hw_counters` does, so
  reading the 21 files costs 2 firmware commands instead of 21.

## Suggested changes

For go-link-monitor, in order of benefit on this host:

1. **Read `hw_counters` with `RDMA_NLDEV_CMD_STAT_GET`**, one request per port,
   instead of one sysfs file per counter. Fall back to sysfs when netlink is
   unavailable, and then read the index-0 counter first. About 15.5 ms of wall
   time and about 60 fewer firmware refreshes per poll here.
2. **Hold the remaining sysfs descriptors** (the `counters` files) for the life
   of the RDMA port's inventory generation and read them with `pread` at
   offset 0 into a reused buffer. Reopen on resync, inventory change, or a read
   error such as `ENODEV`. That is 9 system calls per file down to 1.
3. **Read the `counters` files on a small bounded pool**, so a slow driver
   cannot delay other sources: 2.0 ms to 0.5 ms per port here.
4. **Replace the per-poll identity guard** with the held descriptors. A
   descriptor stays bound to the kernfs node it opened, so a device that is
   removed and replaced under the same name returns an error instead of
   another device's values. Check identity only when reopening.
5. **Optionally skip the 8 PMA data and packet files on mlx5 RoCE** and derive
   them from the `*_vport_rdma_*` ethtool statistics already read. This
   couples to one driver's naming, so it is the lowest priority.
6. **Separately**, find why descriptors are rebuilt at publication when no
   schema changed, and whether the inventory fallback needs to reread GIDs
   every 30 s.

Expected effect on this host: RDMA counter wall time per poll from about 29 ms
to about 2 ms, roughly 1,500 fewer system calls and about 60 fewer firmware
refreshes per poll, and 3–5 ms less CPU. Each step should come with a
benchmark and a guard on system calls per poll. node_exporter could gain the
same from item 1 or the index-0 ordering.

### Measured after items 1–4

Items 1 to 4 are implemented on branch `perf/linkmonitor-rdma-counters`.
Measured on this host, both mlx5 ports, with rounds 11 ms apart so the
driver's 10 ms cache expires between them as it does between polls
(`BenchmarkRDMAHostCounters`, `strace -c` over the same rounds):

| Measure, one round over both ports | Before | After |
|---|---|---|
| RDMA counter collection | 23.4 ms | 1.6–2.0 ms |
| `hw_counters` alone, sysfs | 17.2 ms (name order) | 1.65 ms (index 0 first) |
| `hw_counters` alone, one `STAT_GET` per port | – | 0.73 ms |
| File and netlink system calls | 1,034 | 54 (46 `pread64`, 2 `sendmsg`, 4 `recvmsg`) |
| Same, on the sysfs fallback | – | 108 |

- The netlink path and the sysfs fallback cost about the same per round,
  because the round is now bound by the 21 PMA files, which no cache covers.
  `STAT_GET` runs while the pool reads them.
- The `/metrics` RDMA series are the same before and after: 110 series with
  the same names, labels, kinds and sources.
- On tmpfs (`BenchmarkRDMACounterRead`), a poll with every cataloged counter
  takes about 60 µs and 121–138 allocations with held descriptors, against
  430 µs and 471 allocations with open, read and close per file. Pool sizes 1,
  2, 4 and 8 cost about the same there, so the default stays 4, which bounds
  the firmware commands in flight at 16 across 4 workers.
- Device index 0 is a real nldev index (`mlx5_0` here); only ports discovered
  through sysfs skip netlink.

### After the counters: inventory, state and parsing

With the counters cheap, a whole-process `strace -f -tt` and an allocation
profile showed three more RDMA costs. They are fixed on the same branch:

| Cost | Before | After | Benchmark |
|---|---|---|---|
| GID scan, 512 slots on this host | 7.9 ms, 4,072 allocations | 5.9 ms, 2,024 allocations | `BenchmarkRDMAHostGIDs` |
| Same, 256 slots on tmpfs | 1.6–2.6 ms, 182 KB | 0.92 ms, 18 KB | `BenchmarkRDMAGIDScan` |
| Port state, both ports, each poll | 600 µs, 428 allocations | 345 µs, 104 allocations | `BenchmarkRDMAHostState` |
| `STAT_GET` reply, 32 counters | 27.5 µs, 119 KB, 78 allocations | 5.4 µs, 12 KB, 47 allocations | `BenchmarkParseRdmaStat` |
| Resync, whole process, one second | 11,700 system calls | 5,600 | `strace -f -tt` |
| Steady polling, whole process | about 535 system calls/s | about 455 | `strace -f -tt` |

- **Attribute reads:** every inventory, state and association file now costs
  `open`, one `pread64` and `close`. `os.Open` registered each file with the
  poller (two `fcntl`, two `epoll_ctl`) and `io.ReadAll` read again for EOF.
  `TestRDMAGIDScanCostTable` holds the scan at one read per file through
  `/proc/self/io`.
- **Port state:** a `GET_PORT` reply carries the device name, index and port.
  Kernel device indexes are allocated cyclically, so a matching reply is the
  same registration; the sysfs identity check now runs only when the reply
  does not match, fails, or lacks state.
- **Parsing:** each `STAT_GET` counter entry decoded into a new 3 KiB
  attribute table; one table is now reused across the entries.
- **Left alone:** ethtool statistics build a new `[]model.Sample` each poll,
  31% of allocated bytes. The reducer consumes the slice on another
  goroutine, so reusing it means passing it back with the next job, a change
  to `model.Job`, which is compared with `==`. CPU per poll and the GC rate
  (16–17 collections in 47 s) did not move measurably with these fixes.

## On-demand collection

### Method

Two pairs of instances of the same build (no `rdma` tag, `CGO_ENABLED=0`) ran
side by side on the reference host for 11 minutes:
- **poll:** `-collection.mode=poll -stats-interval=15s`;
- **on-demand:** `-collection.mode=on-demand -watch.interval=60s -watch.slow-interval=300s`.

Each pair was scraped on `/metrics` every 60s. One instance of each pair ran
under `strace -f -qq -ttt`. After a 60 s warm-up, the window was 600 s:
- CPU as `utime + stime` from `/proc/<pid>/stat`;
- GC cycles from `GODEBUG=gctrace=1`;
- system calls counted from the trace's timestamps.

After the window, `/metrics-all` was scraped twice on the on-demand instance.

### The 30 s RDMA retry

The first run showed almost no difference: 1,390 ms against 1,330 ms of CPU,
and about 103,000 system calls each. Every RDMA collector, `channels` and
`rings` were re-collected every 30 s with no scrape and no kernel link event.
The cause was the event source retry described in
[Background cost at the default interval](#background-cost-at-the-default-interval).
On demand, the invalidation also left `collector` and `check_unknown`
problems on both mlx5 ports between collections.

`rdmaevents.ErrUnavailable` means the build has no verbs binding, which
cannot change while the process runs. The source is now never reopened after
it. Diagnostics still report `rdma_events` unsupported, and a new RDMA
identity is reported the same way (`TestRDMAEventUnavailable`).

### Results

| 600 s idle window | Poll, before | Poll | On demand |
|---|---|---|---|
| CPU | 1,390 ms | 270 ms | 110 ms |
| GC cycles | 45 | 6 | 5 |
| System calls | 102,605 | 14,371 | 4,055 |
| `pread64` (held RDMA counter files) | 15,960 | 2,080 | 240 |
| `ioctl` (ethtool) | 1,560 | 840 | 210 |
| Problems on `/metrics` | 1 | 1 | 1 (the same `enp1s0` errors) |

| Scrape, on demand | Time |
|---|---|
| `/metrics` | 0.4 ms |
| `/metrics-all`, collecting everything | 18.4 ms |
| `/metrics-all` again inside `-on-demand.ttl` | 14.8 ms, served from the cache |
| `/metrics-all`, poll mode | 13.4 ms |

- The retry fix alone cut poll mode's idle CPU about 5×.
- On demand cuts the remainder by 2.5× for CPU and 3.5× for system calls.
  What is left is the watch tier, the slow tier, rtnetlink and Go runtime
  timers.
- The first detail scrape costs one collection, about 5 ms more than a
  cached scrape. Concurrent scrapes join that collection.
- These are one run on one host, with a no-`rdma` build. A build with RDMA
  events adds verbs event handling but not the retry.

## Caveats

- One run, one host, client and exporters on the same machine. CPU is read
  from `/proc/<pid>/stat` in 10 ms ticks, which is adequate over 300 scrapes
  and 120 s but not for individual requests.
- Both ran unprivileged. A root or capability-granted run may read more
  statistics in either exporter.
- go-link-monitor was built without the `rdma` tag, so its RDMA event source
  was unavailable and `collection_healthy` stayed 0. This does not change the
  scrape or polling cost measured here.
- The node_exporter checkout is a pre-release branch of 1.11.1, not a tagged
  release.
- Under `strace`, Go runtime calls such as `nanosleep`, `futex` and
  `epoll_pwait` are inflated by tracing overhead. The counts of file, `ioctl`
  and netlink calls are exact.
- node_exporter's netclass collector covers 14 netdevs to go-link-monitor's 3
  eligible ports. That inflates its netclass numbers but not the ethtool name
  sanitizing, which is the largest cost.
- Firmware command counts are inferred from the source and timings; the mlx5
  per-command counters in debugfs need root and were not read.
- The RDMA counters on this host were idle. The read cost is the driver's
  firmware command, which does not depend on the value.
- The summary reported `interface_problem{interface="enp1s0",reason="errors"}`,
  the known large `rx_errors` driver counter on that port; `-errors.exclude`
  silences it. One problem series does not measurably change summary cost.

## Reproducing

Build both exporters, start them as in [Setup](#setup), wait until
`/metrics-all` reports driver statistics (one stats interval), then run:

```bash
#!/usr/bin/env bash
# measure.sh NODE_PID GLM_PID
set -euo pipefail
node=$1 glm=$2
hz=$(getconf CLK_TCK)
ticks() { awk '{print $14+$15}' "/proc/$1/stat"; }
rss() { awk '/VmRSS/{print $2}' "/proc/$1/status"; }
scrape() { # pid url count
  local pid=$1 url=$2 n=$3 t0 t1 args=()
  for ((i = 0; i < n; i++)); do args+=("$url" -o /dev/null); done
  t0=$(ticks "$pid")
  curl -s -w '%{time_total} %{size_download}\n' "${args[@]}" > times.txt
  t1=$(ticks "$pid")
  sort -n times.txt | awk -v n="$n" -v d=$((t1 - t0)) -v hz="$hz" -v u="$url" '
    {t[NR]=$1; s+=$1; b=$2}
    END {printf "%-40s n=%-5d bytes=%-8d mean=%7.2fms p50=%7.2fms p99=%7.2fms cpu/scrape=%7.2fms\n",
      u, n, b, s/n*1000, t[int(n*0.5)]*1000, t[int(n*0.99)]*1000, d/hz/n*1000}'
}
n0=$(ticks "$node") g0=$(ticks "$glm"); sleep 120; n1=$(ticks "$node") g1=$(ticks "$glm")
echo "idle: node=$(((n1 - n0) * 1000 / hz))ms glm=$(((g1 - g0) * 1000 / hz))ms"
echo "rss before: node=$(rss "$node")kB glm=$(rss "$glm")kB"
scrape "$node" http://127.0.0.1:19100/metrics 300
scrape "$glm" http://127.0.0.1:19201/metrics 3000
for c in link traffic ethtool driver rdma netstat collection; do
  scrape "$glm" "http://127.0.0.1:19201/metrics/$c" 300
done
scrape "$glm" http://127.0.0.1:19201/metrics-all 300
echo "rss after: node=$(rss "$node")kB glm=$(rss "$glm")kB"
```

Collection cost, with the exporters on ports 19100 and 19201, go-link-monitor
started with `-collection.mode=poll -stats-interval=1s`, and `glm` set to its
PID:

```bash
curl -s -o node.pprof 'http://127.0.0.1:19100/debug/pprof/profile?seconds=30' &
end=$((SECONDS + 32))
while ((SECONDS < end)); do curl -s -o /dev/null http://127.0.0.1:19100/metrics; done
wait
go tool pprof -top -cum node.pprof
perf record -F 2999 -g -p "$glm" -o glm.perf -- sleep 30
perf report -i glm.perf --children --sort sym --stdio -g none
```

For system call counts, start a second instance of each under
`strace -f -qq -ttt -o trace.txt`, note the time before and after a set of
scrapes or polls, and count the calls between the two times.

For a fleet comparison, also vary `-stats-interval`, the number of concurrent
scrapers, and the number of interfaces, and run each configuration at least
five times. The in-process equivalents for go-link-monitor are
`BenchmarkSummaryHTTP`, `BenchmarkCategoryCollect` and
`BenchmarkPrometheusFilteredHTTP`.
