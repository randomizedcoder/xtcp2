--
-- xtcp_xtcp_flat_records_errors_mv.sql
--
-- Captures Kafka-engine parse failures. With
-- kafka_handle_error_mode = 'stream' on xtcp.xtcp_flat_records_kafka,
-- any message that fails protobuf parsing keeps a populated `_error`
-- virtual column. The main MV (xtcp_flat_records_mv) filters those out
-- so they don't pollute analytical queries; this MV captures them so
-- operators can inspect what went wrong without sifting through
-- destination noise.
--
-- TTL keeps the table self-cleaning at 1 day — these rows are for
-- diagnostics, not retention.

DROP VIEW IF EXISTS xtcp.xtcp_flat_records_errors_mv;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_errors;

CREATE TABLE xtcp.xtcp_flat_records_errors
(
    observed_at  DateTime DEFAULT now(),
    topic        LowCardinality(String),
    partition    UInt64,
    offset       UInt64,
    err          String CODEC(ZSTD),
    raw_message  String CODEC(ZSTD)
)
ENGINE = MergeTree
ORDER BY observed_at
TTL observed_at + INTERVAL 1 DAY DELETE;

CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_errors_mv
TO xtcp.xtcp_flat_records_errors
AS SELECT
    now()         AS observed_at,
    _topic        AS topic,
    _partition    AS partition,
    _offset       AS offset,
    _error        AS err,
    _raw_message  AS raw_message
FROM xtcp.xtcp_flat_records_kafka
WHERE length(_error) > 0;

-- end
