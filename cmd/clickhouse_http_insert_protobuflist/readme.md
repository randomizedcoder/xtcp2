

clickhouse-client --query "INSERT INTO clickhouse_protolist.clickhouse_protolist SETTINGS format_schema='/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:Envelope_Record' FORMAT ProtobufList" < ./dump.bin.envelope

SYSTEM DROP FORMAT SCHEMA CACHE FOR Protobuf;