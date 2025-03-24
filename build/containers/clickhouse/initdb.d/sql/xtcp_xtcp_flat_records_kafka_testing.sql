DROP TABLE IF EXISTS xtcp.xtcp_flat_records_kafka;

CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_kafka
(
    -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
    timestamp_ns                                                DateTime64(9,'UTC') CODEC(DoubleDelta, LZ4),
)
ENGINE = Kafka
SETTINGS
  kafka_broker_list = 'redpanda-0:9092',
  kafka_topic_list = 'xtcp',
  kafka_schema = 'xtcp_flat_record_repeated.proto:XtcpFlatRecord',
  kafka_max_rows_per_message = 10000,
  kafka_format = 'ProtobufList',
  kafka_num_consumers = 1,
  kafka_thread_per_consumer = 0,
  kafka_group_name = 'xtcp',
  kafka_skip_broken_messages = 1,
  kafka_handle_error_mode = 'stream';