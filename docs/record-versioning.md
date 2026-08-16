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
change is needed on already-deployed old daemons. The current (enrichment-era)
format is epoch **1**.

Why per-row and not on the `Envelope`? ClickHouse's `ProtobufList` format maps the
**row** type and consumes the envelope framing itself, so envelope fields never
become columns and cannot be routed on. The version must be on `XtcpFlatRecord`.

`schema_version` and the daemon's release version (`./VERSION` → `daemon_version`)
are **independent axes** and must not be coupled: the release version bumps on
every build, whereas `schema_version` bumps only when the record format changes
enough to warrant a new physical table. Tying the epoch to the release version
would spawn a new `_vN` table on every release.

## ClickHouse topology

One Kafka topic (`xtcp`) → one Kafka engine table → **two materialized views that
fan out by `schema_version`** → per-version MergeTree tables. ClickHouse supports
multiple MVs reading one Kafka engine table, so routing stays a ClickHouse-only
concern on the single topic. DDL: `build/containers/clickhouse/initdb.d/sql/`.

```
topic xtcp → xtcp.xtcp_flat_records_kafka
    ├─ xtcp_flat_records_v0_mv : WHERE _error=='' AND schema_version = 0 → xtcp.xtcp_flat_records_v0  (legacy)
    ├─ xtcp_flat_records_v1_mv : WHERE _error=='' AND schema_version = 1 → xtcp.xtcp_flat_records_v1  (current)
    └─ xtcp_flat_records_errors_mv : WHERE _error<>''                    → xtcp.xtcp_flat_records_errors
xtcp.xtcp_flat_records = Merge('xtcp', '^xtcp_flat_records_v[0-9]+$')     -- cross-version query surface
```

- **`xtcp_flat_records`** is now a read-only `Merge` view spanning every
  `_v[0-9]+` table, so existing queries/dashboards that hit `xtcp_flat_records`
  keep working and transparently span all versions. Its `_table` virtual column
  tells you which physical version a row came from.
- **`_v1` is created `AS _v0`**, so the two physical tables share structure/engine/
  `ORDER BY`/TTL and cannot drift.
- The positional MV convention (`SELECT fromUnixTimestamp64Nano(timestamp_ns) AS
  timestamp_ns, * EXCEPT (timestamp_ns)`) is preserved: `schema_version` and
  `daemon_version` are declared right after `timestamp_ns` in the Kafka table and
  every destination table so the positional insert stays aligned.

## Adding a new epoch

1. Change the record format in the proto; `nix run .#regen-protos`.
2. Bump `XtcpFlatRecordSchemaVersion` in `pkg/xtcp/schema_version.go` (and the
   guard in `pkg/xtcp/deserialize_test.go`).
3. In `build/containers/clickhouse/initdb.d/sql/`: add `xtcp_flat_records_v2`
   (`CREATE ... AS xtcp_flat_records_v0` + the new columns) and a
   `xtcp_flat_records_v2_mv` (`WHERE schema_version = 2`). The `Merge` regex picks
   up `_v2` automatically — no edit to the union surface.

Old records keep flowing to their existing `_vN` table; new records land in the
new one. Because ClickHouse maps columns by **name** and absent proto3 scalars
default to zero, a table can also simply tolerate a mixed fleet within one epoch
(new columns read empty on old rows) — the per-version split is for changes big
enough to warrant a physically separate table.

## Deployment

Nothing new is required on the host: `schema_version` is compile-time and
`daemon_version` comes from the existing build `-ldflags`. The versioned DDL ships
inside the ClickHouse image build; a fleet image rebuild (runpod/xtcp2) carries the
new binary + DDL. ansible-host needs no change for this feature.
