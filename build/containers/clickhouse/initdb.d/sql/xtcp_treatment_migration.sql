-- Apply to an existing deployment before enabling controller agents.
-- Additive migration: never drop telemetry tables or reset Kafka offsets.
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS agent_id String;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS host_boot_id String;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS collection_id String;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS treatment_state_revision UInt64;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS treatment_assignment_id String;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS treatment_generation UInt64;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS treatment_applied_timestamp_ns Int64;
ALTER TABLE xtcp.xtcp_flat_records_v2 ADD COLUMN IF NOT EXISTS treatment_algorithm_exposed Bool;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS agent_id String;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS host_boot_id String;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS collection_id String;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS treatment_state_revision UInt64;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS treatment_assignment_id String;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS treatment_generation UInt64;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS treatment_applied_timestamp_ns Int64;
ALTER TABLE xtcp.xtcp_flat_records ADD COLUMN IF NOT EXISTS treatment_algorithm_exposed Bool;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS agent_id String;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS host_boot_id String;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS collection_id String;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS treatment_state_revision UInt64;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS treatment_assignment_id String;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS treatment_generation UInt64;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS treatment_applied_timestamp_ns Int64;
ALTER TABLE xtcp.xtcp_flat_records_kafka ADD COLUMN IF NOT EXISTS treatment_algorithm_exposed Bool;
ALTER TABLE xtcp.xtcp_flat_records_v2_mv MODIFY QUERY
SELECT fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns, * EXCEPT (timestamp_ns)
FROM xtcp.xtcp_flat_records_kafka
WHERE length(_error) = 0 AND schema_version = 2;
