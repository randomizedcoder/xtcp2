-- INSERT INTO clickhouse_protolist.clickhouse_protolist VALUES (0);
-- INSERT INTO clickhouse_protolist.clickhouse_protolist VALUES (1);
-- INSERT INTO clickhouse_protolist.clickhouse_protolist VALUES (4294967295); -- 2^32-1

-- SELECT * FROM clickhouse_protolist;

-- 5b59b18e84e0 :) SELECT * FROM clickhouse_protolist;

-- SELECT *
-- FROM clickhouse_protolist

-- Query id: 041259cf-20fe-4ef7-8572-e19f85bec383

--    ┌─myUint32─┐
-- 1. │        1 │
--    └──────────┘
--    ┌───myUint32─┐
-- 2. │ 4294967295 │ -- 4.29 billion
--    └────────────┘
--    ┌─myUint32─┐
-- 3. │        0 │
--    └──────────┘

-- 3 rows in set. Elapsed: 0.001 sec.


If something funny is going on, dump the protobuf cache!!!
```
SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf;
```

```
SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32 ASC
FORMAT ProtobufList
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';
```

```
SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32
INTO OUTFILE 'clickhouse_protolist.proto.insert1.bin'
FORMAT ProtobufList
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';
```

```
SELECT *
FROM clickhouse_protolist.clickhouse_protolist
WHERE my_uint32 = 1
ORDER BY my_uint32
INTO OUTFILE 'clickhouse_protolist.proto.insert1.proto.bin'
FORMAT Protobuf
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';
```

```
SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32
INTO OUTFILE 'clickhouse_protolist.proto.insert4294967295.proto.bin'
FORMAT Protobuf
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';
```


SELECT
  *
FROM
  clickhouse_protolist.clickhouse_protolist
ORDER BY
  my_uint32
INTO OUTFILE
  'clickhouse_protolist.proto.insert1.bin'
FORMAT
  ProtobufList SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Envelope';

-- ProtobufList SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/xtcp_flat_record.proto:xtcp_flat_record.v1.XtcpFlatRecord';
-- ProtobufList SETTINGS format_schema = 'clickhouse_protolist.proto:clickhouse_protolist.v1.Record';


-- TRUNCATE TABLE clickhouse_protolist.clickhouse_protolist

-- Query id: edc432fe-8369-47c3-b6ed-fbc55917c366

-- Connecting to localhost:9000 as user default.
-- Connected to ClickHouse server version 24.8.12.

-- Ok.

-- 0 rows in set. Elapsed: 0.019 sec.


```
root@6a483cf7feb5:/# rm clickhouse_protolist.proto.bin
root@6a483cf7feb5:/# clickhouse-client
ClickHouse client version 24.8.12.28 (official build).
Connecting to localhost:9000 as user default.
Connected to ClickHouse server version 24.8.12.

Warnings:
 * Delay accounting is not enabled, OSIOWaitMicroseconds will not be gathered. You can enable it using `echo 1 > /proc/sys/kernel/task_delayacct` or by using sysctl.

6a483cf7feb5 :) SELECT
  *
FROM
  clickhouse_protolist.clickhouse_protolist
ORDER BY
  my_uint32
-- INTO OUTFILE
--   'clickhouse_protolist.proto.bin'
FORMAT
  ProtobufList SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.Record';

SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32 ASC
FORMAT ProtobufList
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.Record'

Query id: 1a8eca07-f541-44cb-b1a6-78619dd4d7b6

Ok.
Error on processing query: Code: 36. DB::Exception: Code: 36. DB::Exception: Could not find a message named 'clickhouse_protolist.Record' in the schema file 'clickhouse_protolist.proto'. (BAD_ARGUMENTS) (version 24.8.12.28 (official build)). (BAD_ARGUMENTS) (version 24.8.12.28 (official build))

6a483cf7feb5 :) SELECT
  *
FROM
  clickhouse_protolist.clickhouse_protolist
ORDER BY
  my_uint32
-- INTO OUTFILE
--   'clickhouse_protolist.proto.bin'
FORMAT
  ProtobufList SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';

SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32 ASC
FORMAT ProtobufList
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record'

Query id: f4526c74-32e9-4bb7-b7c6-588f24b62df7




����
3 rows in set. Elapsed: 0.002 sec.

6a483cf7feb5 :) SELECT
  *
FROM
  clickhouse_protolist.clickhouse_protolist
ORDER BY
  my_uint32
INTO OUTFILE
  'clickhouse_protolist.proto.bin'
FORMAT
  ProtobufList SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record';

SELECT *
FROM clickhouse_protolist.clickhouse_protolist
ORDER BY my_uint32 ASC
INTO OUTFILE 'clickhouse_protolist.proto.bin'
FORMAT ProtobufList
SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record'

Query id: fbe6e3cd-2e1c-4d9a-a894-7c5f80fe9c2b


3 rows in set. Elapsed: 0.002 sec.
```

```
53590ba0990e :) SHOW CREATE TABLE clickhouse_protolist.clickhouse_protolist;


SHOW CREATE TABLE clickhouse_protolist.clickhouse_protolist

Query id: d7e8f740-a301-4af9-ac3a-708267cc8aec

Connecting to localhost:9000 as user default.
Connected to ClickHouse server version 24.12.4.

   ┌─statement───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐
1. │ CREATE TABLE clickhouse_protolist.clickhouse_protolist
(
    `my_uint32` UInt32
)
ENGINE = MergeTree
ORDER BY my_uint32
SETTINGS index_granularity = 8192 │
   └─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘

1 row in set. Elapsed: 0.001 sec.
```


https://vincent.bernat.ch/en/blog/2023-dynamic-protobuf-golang

https://vincent.bernat.ch/en/blog