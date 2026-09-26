package xtcp

// XtcpFlatRecordSchemaVersion is the record format epoch stamped into every
// record's schema_version field so downstream consumers can route records to
// per-version ClickHouse tables and migrate/aggregate across them.
//
// 0 is reserved for pre-versioning daemons: such daemons never set the field, so
// proto3 decodes it to the zero value and ClickHouse routes those rows to the
// "legacy" (_v0) table. Bump this constant (and add the matching _vN table + MV
// in build/containers/clickhouse/initdb.d) whenever the record format changes
// meaningfully — i.e. any field RENAME or RENUMBER. Adding a field in a free
// slot is not a bump (name-mapped consumers just see a new column).
//
// History:
//
//	0  pre-versioning (no schema_version on the wire)
//	1  2026-08/09: metadata blocks 1-299, enrichment 300s, payload 1000+
//	2  2026-09: payload names aligned to kernel struct spelling (tcp_info_rttvar,
//	   inet_diag_tos, inet_diag_cgroup_id, ...), enrichment 300s regrouped by
//	   subject (egress 301/302 -> 311/312, next_hop_asn renamed dest_next_hop_asn),
//	   c_group 2103 -> inet_diag_cgroup_id 2003. See
//	   build/containers/clickhouse/sql/migrations/v2.sql.
const XtcpFlatRecordSchemaVersion = 2
