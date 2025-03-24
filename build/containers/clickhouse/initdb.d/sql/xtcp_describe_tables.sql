--
-- Recreate xtcp_describe_tables.sql
--

-- DETACH TABLE xtcp.xtcp_flat_records_kafka;
-- ATTACH TABLE xtcp.xtcp_flat_records_kafka;

-- SELECT * FROM xtcp.xtcp_flat_records_kafka SETTINGS stream_like_engine_allow_direct_select = 1;
-- DESCRIBE TABLE xtcp.xtcp_flat_records_kafka SETTINGS stream_like_engine_allow_direct_select = 1;

-- https://clickhouse.com/docs/en/sql-reference/statements/describe-table

DESCRIBE TABLE xtcp.xtcp_flat_records_kafka
  TRUNCATE INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.xtcp_flat_records_kafka';
DESCRIBE TABLE xtcp.xtcp_flat_records
  TRUNCATE INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.xtcp_flat_records';

DESCRIBE TABLE xtcp.xtcp_flat_records_kafka
  TRUNCATE INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.xtcp_flat_records_kafka.csv' FORMAT CSV;
DESCRIBE TABLE xtcp.xtcp_flat_records
  TRUNCATE INTO OUTFILE '/docker-entrypoint-initdb.d/xtcp.xtcp_flat_records.csv' FORMAT CSV;

-- end