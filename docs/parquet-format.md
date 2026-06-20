# Parquet format (for data teams)

This document is written for a **data / analytics team** that needs to consume xtcp2's TCP telemetry into an enterprise data platform (lakehouse, warehouse, query engine). It assumes you're fluent in Parquet, object storage, and SQL, but only have a *basic* understanding of TCP — so it explains the columns that matter most and where to focus your first implementation.

The short version: when xtcp2 runs with the S3/Parquet destination it writes **Hive-style partitioned, column-compressed Apache Parquet files** to an S3-compatible bucket. One Parquet **row = one socket observed at one poll**. The schema is flat (no nested or repeated fields), one column per field, so it loads cleanly into any Parquet reader.

## Table of contents

- [Where the files land](#where-the-files-land)
- [File size, cadence, and compression](#file-size-cadence-and-compression)
- [Reading the data](#reading-the-data)
- [Loading into Snowflake](#loading-into-snowflake)
- [The grain: one row per socket per poll](#the-grain-one-row-per-socket-per-poll)
- [Start here: the columns that matter](#start-here-the-columns-that-matter)
- [Decoding cheat sheet](#decoding-cheat-sheet)
- [Full schema and column types](#full-schema-and-column-types)
- [Types, nulls, and gotchas](#types-nulls-and-gotchas)
- [Where the schema is defined](#where-the-schema-is-defined)
- [See also](#see-also)

## Where the files land

Object keys are **Hive-partitioned** by host, date, and hour (all UTC):

```
<prefix>/host=<hostname>/date=<YYYY-MM-DD>/hour=<HH>/<unix_ts>_<rand>.parquet
```

Example:

```
xtcp/host=web-01/date=2026-06-19/hour=14/1750345200_9f3a1c20.parquet
```

- `host=` — the emitting machine (`hostname`); sanitized for object-store safety, empty → `unknown`.
- `date=` / `hour=` — **UTC** wall clock at write time, ready for partition pruning (`WHERE date = '...' AND hour = '...'`).
- The file name is `<unix-seconds>_<random-hex>.parquet` — unique, append-only; xtcp2 never rewrites a file.

These partition keys are part of the **path, not the file** (standard Hive convention). Most engines (Spark, Trino/Athena, DuckDB, BigQuery external tables) expose `host`, `date`, `hour` as virtual columns automatically when you point them at `<prefix>/`.

## File size, cadence, and compression

- **Size:** xtcp2 finalizes and uploads a file when its in-memory builder reaches a soft cap of **~63 MiB uncompressed** (configurable via `-s3ParquetFlushBytes`). On the wire the `.parquet` is several times smaller after compression. A partial file is also flushed on shutdown, so the last file of a run may be small.
- **Cadence:** depends on traffic volume — a busy host fills 63 MiB quickly; a quiet host may take a while, so don't assume one file per hour. Use the `date`/`hour` partitions, not file counts.
- **Compression:** per-column. String and address columns use **ZSTD** (high ratio); numeric columns use **SNAPPY** (fast, widely supported). Every mainstream Parquet reader handles both — you don't need to configure anything.

## Reading the data

Point any Parquet engine at the prefix. A few starting points:

```sql
-- DuckDB (great for exploration); hive_partitioning surfaces host/date/hour as columns
SELECT host, date, hour, count(*) AS rows
FROM read_parquet('s3://bucket/xtcp/**/*.parquet', hive_partitioning = true)
WHERE date = '2026-06-19'
GROUP BY 1,2,3 ORDER BY 1,2,3;
```

```python
# pandas / pyarrow
import pyarrow.dataset as ds
dataset = ds.dataset("s3://bucket/xtcp/", format="parquet", partitioning="hive")
df = dataset.to_table(columns=["timestamp_ns","hostname","tcp_info_rtt"]).to_pandas()
```

```sql
-- Trino / Athena: create an external table over the prefix with
-- partitions (host string, date string, hour string); project columns you need.
```

**Always select only the columns you need** — there are 122, and columnar pruning is where Parquet earns its keep. Likewise filter on `date`/`hour` for partition pruning.

## Loading into Snowflake

If your platform team already manages an S3 storage integration, ingesting is a few statements. The column names match the Parquet schema, so name-based matching does the mapping for you.

```sql
-- One-time: a Parquet file format and an external stage over the prefix.
-- STORAGE_INTEGRATION is the bucket grant your platform team provisions.
CREATE OR REPLACE FILE FORMAT xtcp_parquet TYPE = PARQUET;

CREATE OR REPLACE STAGE xtcp_stage
  URL = 's3://bucket/xtcp/'
  STORAGE_INTEGRATION = my_s3_int
  FILE_FORMAT = xtcp_parquet;

-- Auto-create a table whose columns mirror the Parquet schema.
CREATE OR REPLACE TABLE xtcp_flat_records
  USING TEMPLATE (
    SELECT ARRAY_AGG(OBJECT_CONSTRUCT(*))
    FROM TABLE(INFER_SCHEMA(LOCATION => '@xtcp_stage', FILE_FORMAT => 'xtcp_parquet'))
  );

-- Load. MATCH_BY_COLUMN_NAME maps Parquet columns → table columns by name.
COPY INTO xtcp_flat_records
  FROM @xtcp_stage
  FILE_FORMAT = (TYPE = PARQUET)
  MATCH_BY_COLUMN_NAME = CASE_INSENSITIVE;
```

For **continuous, query-in-place** ingestion instead of a one-shot load, define an external table with `AUTO_REFRESH = TRUE` (backed by Snowpipe + an S3 event notification) over the same stage.

Two Snowflake-specific notes:

- **Partitions live in the path, not the file.** Snowflake won't surface `host`/`date`/`hour` automatically the way Spark/Trino do — derive them from `metadata$filename` (e.g. `REGEXP_SUBSTR(metadata$filename, 'date=([^/]+)', 1, 1, 'e', 1)`) as virtual columns on an external table, or as extra columns during `COPY` via a transform. They make great clustering/pruning keys.
- **The two IP-address columns load as `BINARY`.** Decode them with `inet_diag_msg_family` as in the [cheat sheet](#decoding-cheat-sheet) — or, if you'd rather not decode in SQL, point the daemon at the humanized CSV/JSON output instead (see [output formats](output-and-destinations.md)).

## The grain: one row per socket per poll

xtcp2 polls every network namespace on a fixed interval (default 10s) and emits one row per TCP socket per poll. So:

- A long-lived connection appears in **many** rows over time — one per poll while it exists.
- Most counters (bytes, segments, retransmits) are **cumulative over the socket's lifetime**, so you typically `MAX()` them per socket or take deltas between consecutive polls.
- Identify a single socket across polls with `inet_diag_msg_socket_cookie` (a stable kernel-assigned id) together with `hostname`/`netns`.

## Start here: the columns that matter

If you're scoping an initial implementation, these are the high-value columns. Everything else can come later.

### Identity & time
| Column | Type | Meaning |
|---|---|---|
| `timestamp_ns` | double | When the sample was taken — **Unix epoch nanoseconds, UTC**. Divide by 1e9 for seconds. |
| `hostname` | string | Emitting machine (also the `host=` partition). |
| `netns` | string | Network namespace path — distinguishes host vs container/pod sockets. |
| `inet_diag_msg_socket_cookie` | uint64 | Stable per-socket id; use to track one connection across polls. |

### The connection (4-tuple)
| Column | Type | Meaning |
|---|---|---|
| `inet_diag_msg_family` | uint32 | Address family: **2 = IPv4, 10 = IPv6**. Tells you how to read the address bytes. |
| `inet_diag_msg_socket_source` | bytes | Local IP, **raw bytes** (4 for v4, 16 for v6). See [decoding](#decoding-cheat-sheet). |
| `inet_diag_msg_socket_source_port` | uint32 | Local port (host byte order; use as-is). |
| `inet_diag_msg_socket_destination` | bytes | Remote IP, raw bytes. |
| `inet_diag_msg_socket_destination_port` | uint32 | Remote port. |
| `inet_diag_msg_state` | uint32 | TCP state (see the [state map](#decoding-cheat-sheet)); `1`=ESTABLISHED, `10`=LISTEN. |

### Health & performance (the metrics most teams want)
| Column | Type | Unit / meaning |
|---|---|---|
| `tcp_info_rtt` | uint32 | Smoothed round-trip time, **microseconds**. The headline latency metric. |
| `tcp_info_min_rtt` | uint32 | Minimum RTT seen, microseconds — a cleaner latency baseline. |
| `tcp_info_rtt_var` | uint32 | RTT variance, microseconds (jitter). |
| `tcp_info_snd_cwnd` | uint32 | Congestion window, **in packets/segments** (not bytes). |
| `tcp_info_total_retrans` | uint32 | Cumulative retransmitted segments — the simplest "is this connection healthy?" signal. |
| `tcp_info_bytes_sent` / `tcp_info_bytes_acked` | uint64 | Cumulative bytes sent / acknowledged. |
| `tcp_info_bytes_received` | uint64 | Cumulative bytes received. |
| `tcp_info_delivery_rate` | uint64 | Recent delivery rate, **bytes/second** — effective throughput. |
| `tcp_info_pacing_rate` | uint64 | Sender pacing rate, bytes/second. |
| `congestion_algorithm_string` | string | Congestion-control algorithm name (e.g. `cubic`, `bbr`) — easiest to read. |

A solid first dashboard: per host/destination, `MAX(tcp_info_rtt)` and `MAX(tcp_info_min_rtt)`, the delta of `tcp_info_total_retrans`, and throughput from `tcp_info_delivery_rate` — filtered to `inet_diag_msg_state = 1` (ESTABLISHED).

## Decoding cheat sheet

A few columns are stored as machine values for fidelity/size and need decoding for humans:

- **IP addresses** (`inet_diag_msg_socket_source` / `_destination`) are raw bytes. Read them with `inet_diag_msg_family`: 4 bytes → dotted-quad IPv4, 16 bytes → IPv6. In DuckDB you can reconstruct IPv4 as `concat_ws('.', get_byte(col,0), get_byte(col,1), get_byte(col,2), get_byte(col,3))`. If you'd rather not decode in SQL at all, the daemon can emit **already humanized** CSV/JSON instead — see [output formats](output-and-destinations.md) — but the Parquet path keeps raw bytes so nothing is lost.
- **TCP state** (`inet_diag_msg_state`, and `tcp_info_state`) is a kernel integer. Map:

  | value | name | value | name |
  |---|---|---|---|
  | 1 | ESTABLISHED | 7 | CLOSE |
  | 2 | SYN_SENT | 8 | CLOSE_WAIT |
  | 3 | SYN_RECV | 9 | LAST_ACK |
  | 4 | FIN_WAIT1 | 10 | LISTEN |
  | 5 | FIN_WAIT2 | 11 | CLOSING |
  | 6 | TIME_WAIT | 12 | NEW_SYN_RECV |

- **Congestion algorithm**: prefer `congestion_algorithm_string` (the kernel name). The `congestion_algorithm_enum` integer is `0`=UNSPECIFIED, `1`=CUBIC, `2`=DCTCP, `3`=VEGAS, `4`=PRAGUE, `5`=BBR1, `6`=BBR2, `7`=BBR3.
- **timestamp_ns** is a double; `to_timestamp(timestamp_ns / 1e9)` (or your engine's equivalent) gives a UTC timestamp.

## Full schema and column types

The complete column list (122 columns) groups as follows; column names are the proto's snake_case names, identical to the ClickHouse table columns:

- **Metadata** — `timestamp_ns` (double), `hostname`, `netns`, `nsid`, `label`, `tag`, `record_counter`, `socket_fd`, `netlinker_id`.
- **`inet_diag_msg_*`** — the socket id/4-tuple, state, queues, uid/inode, ASN annotations.
- **`mem_info_*` / `sk_mem_info_*`** — socket memory accounting.
- **`tcp_info_*`** — the bulk of the data: RTT, cwnd, ssthresh, MSS, windows, segment and byte counters, pacing/delivery rates, RTO stats, busy/limited times.
- **`congestion_algorithm_*`** — enum (`int32`) + string name.
- **Per-algorithm blocks** — `vegas_info_*`, `dctcp_info_*`, `bbr_info_*` (only meaningful when that algorithm is in use).
- **QoS / misc** — `type_of_service`, `traffic_class`, `shutdown_state`, `class_id`, `sock_opt`, `c_group`.

Column types are: `double` (timestamp only), `string` (hostname/netns/label/tag/congestion string), `bytes` (the two IP-address columns), `int32` (congestion enum), and `uint32`/`uint64` for everything else. The authoritative, field-by-field list with types and compression is the [`ParquetRow` struct](../pkg/xtcp/destinations_s3parquet_schema.go); field meanings are in the [protobuf schema](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto) and [protobuf-formats.md](protobuf-formats.md).

## Types, nulls, and gotchas

- **No NULLs.** The records come from proto3, which has no null — an absent/zero value is the numeric `0` (or empty string/bytes). Treat `0` as "unset or genuinely zero"; don't expect SQL `NULL`.
- **Counters are cumulative**, per socket lifetime — delta between consecutive polls (matched by `inet_diag_msg_socket_cookie`) for per-interval rates, or `MAX()` for totals.
- **Units differ**: RTTs are microseconds; rates are bytes/second; `snd_cwnd` is packets; byte counters are bytes. The per-column units are in the tables above.
- **Per-algorithm columns are sparse-in-meaning**: `bbr_info_*` is only populated when the socket uses BBR, etc. Filter on `congestion_algorithm_string` before trusting them.
- **Schema evolution**: new fields are *added* (never renamed/reordered in place), so plan for forward-compatible reads (select by name, tolerate new columns).

## Where the schema is defined

The Parquet columns are generated from the [`ParquetRow`](../pkg/xtcp/destinations_s3parquet_schema.go) struct, whose `parquet:` tags set the column names and per-column compression. A drift test (`TestS3ParquetSchema_matchesProto`) asserts that set matches the [`XtcpFlatRecord` proto](../proto/xtcp_flat_record/v1/xtcp_flat_record.proto) field-for-field, so the Parquet schema, the protobuf, and the ClickHouse table never diverge. To change the schema you edit the proto, regenerate (`nix run .#regen-protos`), and mirror the field in `ParquetRow`. The S3/Parquet destination itself is documented in [output formats & destinations](output-and-destinations.md#s3-and-parquet); it ships only in builds that include the `dest_s3parquet` tag (see [build flavors](build-flavors.md)).

## See also

- [Socket analysis](socket-analysis.md) — finding RTT bands and other socket groupings by clustering, once the data is loaded.
- [Protobuf formats](protobuf-formats.md) — the canonical schema and field semantics.
- [Output formats & destinations](output-and-destinations.md) — the S3/Parquet destination and the alternative humanized CSV/JSON formats.
- [Build flavors](build-flavors.md) — enabling the `s3parquet` destination.
