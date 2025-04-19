--
-- Recreate xtcp_xtcp_flat_records_mv.sql
--

-- Kafka Topic --> Kakfa Table Engine --> Materialized View -> MergeTree Table

-- https://clickhouse.com/docs/en/integrations/kafka/kafka-table-engine#6-create-the-materialized-view
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_mv;

-- CREATE MATERIALIZED VIEW IF NOT EXISTS xtcp.xtcp_flat_records_mv TO xtcp.xtcp_flat_records

CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_mv TO xtcp.xtcp_flat_records
  AS SELECT
    *
  FROM xtcp.xtcp_flat_records_kafka;
-- FROM xtcp.xtcp_flat_records_kafka WHERE length(_error) == 0;

-- https://github.com/ClickHouse/ClickHouse/blob/master/tests/integration/test_storage_kafka/test_batch_fast.py#L2678

-- 756526eb1051 :) SHOW CREATE TABLE xtcp.xtcp_flat_records_mv;

-- SHOW CREATE TABLE xtcp.xtcp_flat_records_mv

-- Query id: 7f84109e-97e5-42c4-a12f-73248761ee90

--    ┌─statement───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
-- 1. │ CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_mv TO xtcp.xtcp_flat_records                                                           ↴│
--    │↳(                                                                                                                                      ↴│
--    │↳    `timestamp_ns` DateTime64(9, 'UTC'),                                                                                               ↴│
--    │↳    `hostname` LowCardinality(String),                                                                                                 ↴│
--    │↳    `netns` String,                                                                                                                    ↴│
--    │↳    `nsid` UInt32,                                                                                                                     ↴│
--    │↳    `label` LowCardinality(String),
-- ...

-- end