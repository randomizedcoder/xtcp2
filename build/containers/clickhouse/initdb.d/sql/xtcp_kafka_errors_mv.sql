--
-- xtcp_kafka_errors.sql
--

-- This sql creates a materialized view that will have the kafka errors

-- SELECT
--   *,
--   _topic AS topic,
--   _key AS key,
--   _offset AS offset,
--   _timestamp_ms AS timestamp_ms,
--   _partition AS partition,
--   _error AS error
-- FROM xtcp.xtcp_flat_records_kafka;

-- AS SELECT *

-- https://clickhouse.com/docs/integrations/kafka/kafka-table-engine#adding-kafka-metadata
-- https://clickhouse.com/docs/engines/table-engines/integrations/kafka#virtual-columns

-- https://clickhouse.com/docs/sql-reference/statements/create/view#materialized-view

CREATE MATERIALIZED VIEW xtcp.kafka_errors (topic String, partition Int64, offset Int64, raw String, error String)
  ENGINE = MergeTree
  ORDER BY (topic, offset)
  AS SELECT
    _topic AS topic,
    _partition AS partition,
    _offset AS offset,
    _raw_message AS raw,
    _error AS error
  FROM xtcp.xtcp_flat_records_kafka WHERE length(_error) > 0;

-- Borrowed from:
-- https://github.com/ClickHouse/ClickHouse/blob/master/tests/integration/test_storage_kafka/test_batch_fast.py#L2679

-- https://github.com/ClickHouse/ClickHouse/blob/master/tests/integration/test_storage_kafka/test_batch_slow.py