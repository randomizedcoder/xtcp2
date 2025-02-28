--
-- clickhouse_protolist
--

DROP DATABASE IF EXISTS clickhouse_protolist;
CREATE DATABASE IF NOT EXISTS clickhouse_protolist;

-- USE clickhouse_protolist;

DROP TABLE IF EXISTS clickhouse_protolist.clickhouse_protolist;
CREATE TABLE clickhouse_protolist.clickhouse_protolist (
  my_uint32 UInt32,
  ) ENGINE = MergeTree() ORDER BY my_uint32;

INSERT INTO clickhouse_protolist.clickhouse_protolist VALUES (1);

-- SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf;

-- end