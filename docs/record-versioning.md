# Record versioning & per-version ClickHouse routing

`XtcpFlatRecord` evolves over time (fields get added, and occasionally the format
changes in ways that matter). Because xtcp2 rolls out across the fleet
incrementally, at any moment the Kafka stream is a **mix of record formats** — old
daemons and new daemons producing side by side. To keep that tractable, every
record is self-describing and ClickHouse routes each row to a per-version table.

## The two provenance fields

Both live in `proto/xtcp_flat_record/v1/xtcp_flat_record.proto` at the lowest
(single-byte-tag) field numbers, and are stamped in `pkg/xtcp/deserialize.go`:

| Field | # | Meaning |
|---|---|---|
| `schema_version` | 1 | Format **epoch**. Stamped unconditionally from the daemon constant `XtcpFlatRecordSchemaVersion` (`pkg/xtcp/schema_version.go`). Used for routing. |
| `daemon_version` | 2 | Build provenance (commit/date/version, from `-ldflags`), plumbed via `XtcpConfig.daemon_version`. Informational only. The `version` component is the hand-bumped semver in the repo-root [`./VERSION`](../VERSION) file (read by `nix/binaries.nix`); `commit`/`date` are filled by the build (the fleet build overrides all three). |

`schema_version = 0` is reserved for **pre-versioning daemons**: they never set the
field, so proto3 decodes it to zero. That makes `0` a free "legacy" bucket — no
change is needed on already-deployed old daemons.

Why per-row and not on the `Envelope`? ClickHouse's `ProtobufList` format maps the
**row** type and consumes the envelope framing itself, so envelope fields never
become columns and cannot be routed on. The version must be on `XtcpFlatRecord`.

`schema_version` and the daemon's release version (`./VERSION` → `daemon_version`)
are **independent axes** and must not be coupled: the release version bumps on
every build, whereas `schema_version` bumps only when the record format changes
enough to warrant a new physical table. Tying the epoch to the release version
would spawn a new `_vN` table on every release.

## Epoch history

| Epoch | When | What changed |
|---|---|---|
| 0 | pre-2026-08 | No `schema_version` on the wire. |
| 1 | 2026-08/09 | Metadata blocks 1–299, enrichment block 300s, payload 1000+. Fields only added. |
| 2 | 2026-09 | Payload field names aligned to the **kernel struct member spelling**, the 300s enrichment block regrouped by subject, `XtcpConfig` renumbered. See the rename table below and the layout policy in [protobuf-formats.md](protobuf-formats.md#field-layout-policy). |

**When to bump.** Any field **rename or renumber** bumps the epoch. Adding a field
in a free slot does not: ClickHouse and Parquet map by name, so new columns simply
read as default on older rows.

### Epoch 1 → 2 rename table

Wire tag unchanged unless noted. Because the ClickHouse Kafka table maps column
name → proto field → tag, a same-tag rename still decodes epoch-1 bytes correctly;
only the three renumbered fields are lost for epoch-1 rows during a mixed rollout.

| Epoch 2 name | Epoch 0/1 name | Tag | Kernel source |
|---|---|---|---|
| `enrich_socket_dest_egress_ifindex` | same | **301 → 311** | daemon-derived |
| `enrich_socket_dest_egress_ifname` | same | **302 → 312** | daemon-derived |
| `enrich_socket_dest_next_hop_asn` | `enrich_socket_next_hop_asn` | 321 | daemon-derived |
| `tcp_info_snd_wscale` | `tcp_info_send_scale` | 1207 | `tcp_info.tcpi_snd_wscale` |
| `tcp_info_rcv_wscale` | `tcp_info_rcv_scale` | 1208 | `tcp_info.tcpi_rcv_wscale` |
| `tcp_info_fastopen_client_fail` | `tcp_info_fast_open_client_failed` | 1210 | `tcp_info.tcpi_fastopen_client_fail` |
| `tcp_info_rttvar` | `tcp_info_rtt_var` | 1231 | `tcp_info.tcpi_rttvar` |
| `tcp_info_advmss` | `tcp_info_adv_mss` | 1234 | `tcp_info.tcpi_advmss` |
| `tcp_info_notsent_bytes` | `tcp_info_not_sent_bytes` | 1245 | `tcp_info.tcpi_notsent_bytes` |
| `inet_diag_cong` | `congestion_algorithm_string` | 1300 | `INET_DIAG_CONG` |
| `inet_diag_cong_enum` | `congestion_algorithm_enum` | 1301 | derived from 1300 |
| `inet_diag_tos` | `type_of_service` | 1401 | `INET_DIAG_TOS` |
| `inet_diag_tclass` | `traffic_class` | 1402 | `INET_DIAG_TCLASS` |
| `sk_mem_info_rcvbuf` | `sk_mem_info_rcv_buf` | 1502 | `SK_MEMINFO_RCVBUF` |
| `sk_mem_info_sndbuf` | `sk_mem_info_snd_buf` | 1504 | `SK_MEMINFO_SNDBUF` |
| `inet_diag_shutdown` | `shutdown_state` | 1600 | `INET_DIAG_SHUTDOWN` |
| `vegas_info_rttcnt` | `vegas_info_rtt_cnt` | 1702 | `tcpvegas_info.tcpv_rttcnt` |
| `vegas_info_minrtt` | `vegas_info_min_rtt` | 1704 | `tcpvegas_info.tcpv_minrtt` |
| `inet_diag_class_id` | `class_id` | 2001 | `INET_DIAG_CLASS_ID` |
| `inet_diag_sockopt` | `sock_opt` | 2002 | `INET_DIAG_SOCKOPT` |
| `inet_diag_cgroup_id` | `c_group` | **2103 → 2003** | `INET_DIAG_CGROUP_ID` |

The ClickHouse `Locality` enum label for value 2 also changed from
`connected_subnet` to `local_subnet` (matching the proto `LOCALITY_LOCAL_SUBNET`
and the `localnet` package). Values are unchanged.

## ClickHouse topology

One Kafka topic (`xtcp`) → one Kafka engine table → **one materialized view per
epoch, fanning out by `schema_version`** → per-version MergeTree tables. ClickHouse
supports multiple MVs reading one Kafka engine table, so routing stays a
ClickHouse-only concern on the single topic. DDL:
`build/containers/clickhouse/initdb.d/sql/`.

```
topic xtcp → xtcp.xtcp_flat_records_kafka          (epoch-2 column names, proto order)
    ├─ xtcp_flat_records_v0_mv : WHERE _error=='' AND schema_version = 0 → xtcp.xtcp_flat_records_v0  (legacy)
    ├─ xtcp_flat_records_v1_mv : WHERE _error=='' AND schema_version = 1 → xtcp.xtcp_flat_records_v1  (epoch 1)
    ├─ xtcp_flat_records_v2_mv : WHERE _error=='' AND schema_version = 2 → xtcp.xtcp_flat_records_v2  (current)
    └─ xtcp_flat_records_errors_mv : WHERE _error<>''                    → xtcp.xtcp_flat_records_errors
xtcp.xtcp_flat_records = Merge('xtcp', '^xtcp_flat_records_v[0-9]+$')     -- cross-version query surface, AS _v2
```

- **`xtcp_flat_records`** is a read-only `Merge` view spanning every `_v[0-9]+`
  table, so existing queries/dashboards that hit `xtcp_flat_records` keep working
  and transparently span all versions. Its `_table` virtual column tells you which
  physical version a row came from. It is declared `AS _v2` (the newest, superset
  column set); a column an older table lacks reads as default for that table's
  rows, so **branch on `schema_version`** when a renamed column matters (epoch-0/1
  rows keep their data under the old names in `_v0` / `_v1`).
- **`_v1` is created `AS _v0`** (epoch 1 only added fields). **`_v2` has its own
  full DDL** because epoch 2 renamed columns.
- **MV → table mapping is by column NAME**, not position (a `TO` MV is an
  `INSERT ... SELECT`). `_v2_mv` therefore uses the short
  `fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns, * EXCEPT (timestamp_ns)`
  form, while `_v0_mv` / `_v1_mv` carry an **explicit select list** aliasing every
  renamed column back to the old name (`tcp_info_rttvar AS tcp_info_rtt_var`,
  `inet_diag_cgroup_id AS c_group`, …). The two Enum columns pass through as
  `toUInt8(...)` so the insert does not depend on the target's enum labels.

### Mixed fleet during the epoch-1 → 2 rollout

The Kafka table decodes against the epoch-2 schema
(`format_schemas/xtcp_flat_record.proto`). For rows produced by epoch-1 daemons:

- renamed-only fields keep their tag → decode into the epoch-2-named column → the
  `_v1` MV aliases them back → **no data loss**;
- the three **renumbered** fields (`enrich_socket_dest_egress_ifindex/ifname`,
  `c_group`) are unknown tags → **dropped**; `_v1` reads them as `0`/`''` for rows
  produced after the ClickHouse migration until the daemon fleet is on epoch 2.
  Roll daemons promptly after migrating ClickHouse.

## Adding a new epoch

1. Change the record format in the proto; `nix run .#regen-protos`.
   Every payload field (tag ≥ 1000) must carry its kernel-source trailing comment
   (`go run ./tools/proto-field-audit` enforces this).
2. Bump `XtcpFlatRecordSchemaVersion` in `pkg/xtcp/schema_version.go` (append to its
   history comment) and the guard in `pkg/xtcp/deserialize_test.go`.
3. In `build/containers/clickhouse/initdb.d/sql/`:
   - `xtcp_xtcp_flat_records_kafka.sql`: Kafka table = the new column set, in proto
     order.
   - `xtcp_xtcp_flat_records.sql`: add `xtcp_flat_records_vN`. Use
     `CREATE ... AS _v(N-1)` only if nothing was renamed; otherwise write the full
     DDL. Re-declare the `Merge` view `AS _vN`.
   - `xtcp_xtcp_flat_records_mv.sql`: add `_vN_mv` (`WHERE schema_version = N`,
     `* EXCEPT (timestamp_ns)` form). If columns were renamed, rewrite every older
     `_vM_mv` with an explicit alias list mapping new → old names.
4. Add `build/containers/clickhouse/sql/migrations/vN.sql` for existing deployments
   (drop/recreate the Kafka table + MVs, create `_vN`, re-declare the Merge view);
   `v2.sql` is the template.
5. Mirror the field set in `ParquetRow` (`TestS3ParquetSchema_matchesProto` fails
   otherwise) and update this document's history/rename tables.

Old records keep flowing to their existing `_vN` table; new records land in the
new one. Because ClickHouse maps columns by **name** and absent proto3 scalars
default to zero, a table can also simply tolerate a mixed fleet within one epoch
(new columns read empty on old rows) — the per-version split is for changes big
enough to warrant a physically separate table.

## Deployment

Nothing new is required on the host: `schema_version` is compile-time and
`daemon_version` comes from the existing build `-ldflags`. The versioned DDL ships
inside the ClickHouse image build; a fleet image rebuild (runpod/xtcp2) carries the
new binary + DDL. Existing ClickHouse deployments apply
`build/containers/clickhouse/sql/migrations/v2.sql` (after copying the regenerated
`format_schemas/xtcp_flat_record.proto` into the server's `format_schemas/`
directory). ansible-host needs no change for this feature.
