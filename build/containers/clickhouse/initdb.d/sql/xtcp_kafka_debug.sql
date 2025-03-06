--
-- Recreate xtcp_kafka_debug.sql
--

SELECT * FROM system.kafka_consumers FORMAT Vertical;
SELECT now();

-- SELECT * FROM system.stack_trace LIMIT 10;

-- 155ecab95bba :) SELECT * FROM system.kafka_consumers FORMAT Vertical;

-- SELECT *
-- FROM system.kafka_consumers
-- FORMAT Vertical

-- Query id: 1bd62a16-06a6-4192-8b88-c096868e7fcb

-- Row 1:
-- ──────
-- database:                   xtcp
-- table:                      xtcp_flat_records_kafka
-- consumer_id:
-- assignments.topic:          []
-- assignments.partition_id:   []
-- assignments.current_offset: []
-- exceptions.time:            []
-- exceptions.text:            []
-- last_poll_time:             1970-01-01 00:00:00
-- num_messages_read:          0
-- last_commit_time:           1970-01-01 00:00:00
-- num_commits:                0
-- last_rebalance_time:        1970-01-01 00:00:00
-- num_rebalance_revocations:  0
-- num_rebalance_assignments:  0
-- is_currently_used:          0
-- last_used:                  0
-- rdkafka_stat:

-- 1 row in set. Elapsed: 0.002 sec.

-- https://github.com/ClickHouse/ClickHouse/blob/master/tests/integration/test_storage_kafka/test_batch_fast.py#L3151

-- create or replace function stable_timestamp as
--   (d)->multiIf(d==toDateTime('1970-01-01 00:00:00'), 'never', abs(dateDiff('second', d, now())) < 30, 'now', toString(d));

-- -- check last_used stores microseconds correctly
-- create or replace function check_last_used as
--   (v) -> if(abs(toStartOfSecond(last_used) - last_used) * 1e6 > 0, 'microseconds', toString(v));

-- SELECT database, table, length(consumer_id), assignments.topic, assignments.partition_id,
--   assignments.current_offset,
--   if(length(exceptions.time)>0, exceptions.time[1]::String, 'never') as last_exception_time_,
--   if(length(exceptions.text)>0, exceptions.text[1], 'no exception') as last_exception_,
--   stable_timestamp(last_poll_time) as last_poll_time_, num_messages_read, stable_timestamp(last_commit_time) as last_commit_time_,
--   num_commits, stable_timestamp(last_rebalance_time) as last_rebalance_time_,
--   num_rebalance_revocations, num_rebalance_assignments, is_currently_used
--   FROM system.kafka_consumers WHERE database='xtcp' and table='xtcp_flat_records_kafka' format Vertical;