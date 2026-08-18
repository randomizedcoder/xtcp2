package xtcp

// XtcpFlatRecordSchemaVersion is the record format epoch stamped into every
// record's schema_version field so downstream consumers can route records to
// per-version ClickHouse tables and migrate/aggregate across them.
//
// 0 is reserved for pre-versioning daemons: such daemons never set the field, so
// proto3 decodes it to the zero value and ClickHouse routes those rows to the
// "legacy" (_v0) table. Bump this constant (and add the matching _vN table + MV
// in build/containers/clickhouse/initdb.d) whenever the record format changes
// meaningfully.
const XtcpFlatRecordSchemaVersion = 1
