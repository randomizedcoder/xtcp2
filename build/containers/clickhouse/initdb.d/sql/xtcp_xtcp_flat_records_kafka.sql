--
-- Recreate xtcp_xtcp_flat_records_kafka.sql
--

-- Kafka Topic --> Kakfa Table Engine --> Materialized View -> MergeTree Table

-- Not using Nested!
-- https://clickhouse.com/docs/en/sql-reference/data-types/nested-data-structures/nested
-- Not using Nullable, because it uses more space and apparently "almost always negatively affects performance"
-- https://clickhouse.com/docs/en/sql-reference/data-types/nullable

-- Column set = epoch-2 record format (schema_version = 2), in proto field-number
-- order. ClickHouse's Protobuf/ProtobufList input maps table column NAME ->
-- schema field -> wire tag, using format_schemas/xtcp_flat_record.proto (a
-- generated copy of proto/xtcp_flat_record/v1/xtcp_flat_record.proto). So:
--   * epoch-1 daemons still in the fleet: a field that only changed NAME keeps
--     its tag, so its bytes decode into the v2-named column here and the v0/v1
--     MVs alias it back onto the old column name. Nothing lost.
--   * epoch-1 fields that changed NUMBER (enrich_socket_dest_egress_ifindex/
--     ifname 301/302 -> 311/312, c_group 2103 -> inet_diag_cgroup_id 2003) are
--     unknown tags to this schema and are dropped for epoch-1 rows. Those three
--     columns read as 0/'' in _v1 until the daemon fleet is on epoch 2.
--   * epoch-0 daemons never sent any of the affected fields.

-- To debug clickhouse
-- make build_clickhouse_and_deploy
-- docker logs xtcp-clickhouse-1 --follow
-- docker exec -ti xtcp-clickhouse-1 tail -f -n 30 /var/log/clickhouse-server/clickhouse-server.log
-- docker exec -ti xtcp-clickhouse-1 tail -f -n 30 /var/log/clickhouse-server/clickhouse-server.err.log
-- docker exec -ti xtcp-clickhouse-1 bash

DROP TABLE IF EXISTS xtcp.xtcp_flat_records_kafka;

CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_kafka
(
    -- ---- metadata: record format provenance (1-2) --------------------------
    -- schema_version is the routing epoch (0 = legacy, 2 = current); daemon_version
    -- is build provenance.
    schema_version                                              UInt32 CODEC(LZ4),
    daemon_version                                              LowCardinality(String),
    -- Raw int64 epoch nanoseconds straight off the protobuf (the daemon
    -- stamps true UnixNano()). The MVs convert this to DateTime64(9) via
    -- fromUnixTimestamp64Nano when landing rows in the _vN tables; ingesting a
    -- numeric value directly into DateTime64 would be read as SECONDS, not
    -- nanoseconds.
    timestamp_ns                                                Int64 CODEC(DoubleDelta, LZ4),

    -- ---- metadata: host identity (20s) -------------------------------------
    -- https://clickhouse.com/docs/en/sql-reference/data-types/lowcardinality
    hostname                                                    LowCardinality(String),
    location                                                    LowCardinality(String),

    -- ---- metadata: network namespace identity (30s) ------------------------
    netns                                                       String CODEC(ZSTD),
    netns_inode                                                 UInt64 CODEC(ZSTD),
    nsid                                                        UInt32 CODEC(LZ4),

    -- ---- metadata: container identity (40s) --------------------------------
    container_id                                                String CODEC(ZSTD),
    container_runtime                                           LowCardinality(String),
    container_name                                              LowCardinality(String),
    container_image                                             LowCardinality(String),

    -- ---- metadata: free-form labels (50s) ----------------------------------
    label                                                       LowCardinality(String),
    tag                                                         LowCardinality(String),

    -- ---- metadata: record bookkeeping (60s) --------------------------------
    record_counter                                              UInt64 CODEC(DoubleDelta, LZ4),
    socket_fd                                                   UInt64 CODEC(LZ4),
    netlinker_id                                                UInt64 CODEC(LZ4),

    -- ---- metadata: host network topology, uplink slot 1 (100s) -------------
    -- Static per boot: NIC via sysfs + ethtool, LLDP neighbor via lldpd. These
    -- repeat on every record for a given host, so LowCardinality dictionary-
    -- compresses them to ~nothing.
    uplink1_ifname                                              LowCardinality(String),
    uplink1_nic_driver                                          LowCardinality(String),
    uplink1_nic_model                                           LowCardinality(String),
    uplink1_nic_pci_vendor                                      UInt32 CODEC(LZ4),
    uplink1_nic_pci_device                                      UInt32 CODEC(LZ4),
    uplink1_nic_bus_info                                        LowCardinality(String),
    uplink1_nic_speed_mbps                                      UInt32 CODEC(LZ4),
    uplink1_nic_fw_version                                      LowCardinality(String),
    uplink1_lldp_chassis_name                                   LowCardinality(String),
    uplink1_lldp_chassis_id                                     LowCardinality(String),
    uplink1_lldp_mgmt_ip                                        LowCardinality(String),
    uplink1_lldp_port_id                                        LowCardinality(String),
    uplink1_lldp_port_descr                                     LowCardinality(String),

    -- ---- metadata: host network topology, uplink slot 2 (200s) -------------
    uplink2_ifname                                              LowCardinality(String),
    uplink2_nic_driver                                          LowCardinality(String),
    uplink2_nic_model                                           LowCardinality(String),
    uplink2_nic_pci_vendor                                      UInt32 CODEC(LZ4),
    uplink2_nic_pci_device                                      UInt32 CODEC(LZ4),
    uplink2_nic_bus_info                                        LowCardinality(String),
    uplink2_nic_speed_mbps                                      UInt32 CODEC(LZ4),
    uplink2_nic_fw_version                                      LowCardinality(String),
    uplink2_lldp_chassis_name                                   LowCardinality(String),
    uplink2_lldp_chassis_id                                     LowCardinality(String),
    uplink2_lldp_mgmt_ip                                        LowCardinality(String),
    uplink2_lldp_port_id                                        LowCardinality(String),
    uplink2_lldp_port_descr                                     LowCardinality(String),

    -- ---- enrichment: daemon-computed fields (300s) --------------------------
    -- NOT read from the kernel inet_diag message; computed during enrichment
    -- (rtnetlink address/route/link discovery, ipfeed ASN feeds). Empty/zero
    -- when the relevant enricher is disabled or had no answer.
    -- 300     socket-side (bound interface, from idiag_if)
    -- 310-322 destination-side (locality/egress, ASN)
    enrich_socket_interface_name                                LowCardinality(String),
    enrich_socket_dest_locality                                 Enum('unspecified'  = 0,
                                                                     'self'         = 1,
                                                                     'local_subnet' = 2,
                                                                     'remote'       = 3
                                                                     ),
    enrich_socket_dest_egress_ifindex                           UInt32 CODEC(LZ4),
    enrich_socket_dest_egress_ifname                            LowCardinality(String),
    enrich_socket_dest_asn                                      UInt64 CODEC(LZ4),
    enrich_socket_dest_next_hop_asn                             UInt64 CODEC(LZ4),
    enrich_socket_dest_network_owner                            LowCardinality(String),

    -- ---- payload: struct inet_diag_msg (1000s) ------------------------------
    inet_diag_msg_family                                        UInt32 CODEC(LZ4),
    inet_diag_msg_state                                         UInt32 CODEC(LZ4),
    inet_diag_msg_timer                                         UInt32 CODEC(LZ4),
    inet_diag_msg_retrans                                       UInt32 CODEC(LZ4),
    inet_diag_msg_socket_source_port                            UInt32 CODEC(LZ4),
    inet_diag_msg_socket_destination_port                       UInt32 CODEC(LZ4),
    inet_diag_msg_socket_source                                 String CODEC(ZSTD),
    inet_diag_msg_socket_destination                            String CODEC(ZSTD),
    inet_diag_msg_socket_interface                              UInt32 CODEC(LZ4),
    inet_diag_msg_socket_cookie                                 UInt64 CODEC(LZ4),
    inet_diag_msg_expires                                       UInt32 CODEC(LZ4),
    inet_diag_msg_rqueue                                        UInt32 CODEC(LZ4),
    inet_diag_msg_wqueue                                        UInt32 CODEC(LZ4),
    inet_diag_msg_uid                                           UInt32 CODEC(LZ4),
    inet_diag_msg_inode                                         UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_MEMINFO (1), struct inet_diag_meminfo (1100s) ---
    -- Deprecated by the kernel in favour of SK_MEMINFO; kept for old kernels.
    mem_info_rmem                                               UInt32 CODEC(LZ4),
    mem_info_wmem                                               UInt32 CODEC(LZ4),
    mem_info_fmem                                               UInt32 CODEC(LZ4),
    mem_info_tmem                                               UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_INFO (2), struct tcp_info (1200s) ----------------
    -- Column names mirror the kernel member spelling (tcpi_rttvar -> tcp_info_rttvar).
    tcp_info_state                                              UInt32 CODEC(LZ4),
    tcp_info_ca_state                                           UInt32 CODEC(LZ4),
    tcp_info_retransmits                                        UInt32 CODEC(LZ4),
    tcp_info_probes                                             UInt32 CODEC(LZ4),
    tcp_info_backoff                                            UInt32 CODEC(LZ4),
    tcp_info_options                                            UInt32 CODEC(LZ4),
    tcp_info_snd_wscale                                         UInt32 CODEC(LZ4),
    tcp_info_rcv_wscale                                         UInt32 CODEC(LZ4),
    tcp_info_delivery_rate_app_limited                          UInt32 CODEC(LZ4),
    tcp_info_fastopen_client_fail                               UInt32 CODEC(LZ4),
    tcp_info_rto                                                UInt32 CODEC(LZ4),
    tcp_info_ato                                                UInt32 CODEC(LZ4),
    tcp_info_snd_mss                                            UInt32 CODEC(LZ4),
    tcp_info_rcv_mss                                            UInt32 CODEC(LZ4),
    tcp_info_unacked                                            UInt32 CODEC(LZ4),
    tcp_info_sacked                                             UInt32 CODEC(LZ4),
    tcp_info_lost                                               UInt32 CODEC(LZ4),
    tcp_info_retrans                                            UInt32 CODEC(LZ4),
    tcp_info_fackets                                            UInt32 CODEC(LZ4),
    tcp_info_last_data_sent                                     UInt32 CODEC(LZ4),
    tcp_info_last_ack_sent                                      UInt32 CODEC(LZ4),
    tcp_info_last_data_recv                                     UInt32 CODEC(LZ4),
    tcp_info_last_ack_recv                                      UInt32 CODEC(LZ4),
    tcp_info_pmtu                                               UInt32 CODEC(LZ4),
    tcp_info_rcv_ssthresh                                       UInt32 CODEC(LZ4),
    tcp_info_rtt                                                UInt32 CODEC(LZ4),
    tcp_info_rttvar                                             UInt32 CODEC(LZ4),
    tcp_info_snd_ssthresh                                       UInt32 CODEC(LZ4),
    tcp_info_snd_cwnd                                           UInt32 CODEC(LZ4),
    tcp_info_advmss                                             UInt32 CODEC(LZ4),
    tcp_info_reordering                                         UInt32 CODEC(LZ4),
    tcp_info_rcv_rtt                                            UInt32 CODEC(LZ4),
    tcp_info_rcv_space                                          UInt32 CODEC(LZ4),
    tcp_info_total_retrans                                      UInt32 CODEC(LZ4),
    tcp_info_pacing_rate                                        UInt64 CODEC(LZ4),
    tcp_info_max_pacing_rate                                    UInt64 CODEC(LZ4),
    tcp_info_bytes_acked                                        UInt64 CODEC(LZ4),
    tcp_info_bytes_received                                     UInt64 CODEC(LZ4),
    tcp_info_segs_out                                           UInt32 CODEC(LZ4),
    tcp_info_segs_in                                            UInt32 CODEC(LZ4),
    tcp_info_notsent_bytes                                      UInt32 CODEC(LZ4),
    tcp_info_min_rtt                                            UInt32 CODEC(LZ4),
    tcp_info_data_segs_in                                       UInt32 CODEC(LZ4),
    tcp_info_data_segs_out                                      UInt32 CODEC(LZ4),
    tcp_info_delivery_rate                                      UInt64 CODEC(LZ4),
    tcp_info_busy_time                                          UInt64 CODEC(LZ4),
    tcp_info_rwnd_limited                                       UInt64 CODEC(LZ4),
    tcp_info_sndbuf_limited                                     UInt64 CODEC(LZ4),
    tcp_info_delivered                                          UInt32 CODEC(LZ4),
    tcp_info_delivered_ce                                       UInt32 CODEC(LZ4),
    tcp_info_bytes_sent                                         UInt64 CODEC(LZ4),
    tcp_info_bytes_retrans                                      UInt64 CODEC(LZ4),
    tcp_info_dsack_dups                                         UInt32 CODEC(LZ4),
    tcp_info_reord_seen                                         UInt32 CODEC(LZ4),
    tcp_info_rcv_ooopack                                        UInt32 CODEC(LZ4),
    tcp_info_snd_wnd                                            UInt32 CODEC(LZ4),
    tcp_info_rcv_wnd                                            UInt32 CODEC(LZ4),
    tcp_info_rehash                                             UInt32 CODEC(LZ4),
    tcp_info_total_rto                                          UInt32 CODEC(LZ4),
    tcp_info_total_rto_recoveries                               UInt32 CODEC(LZ4),
    tcp_info_total_rto_time                                     UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_INFO Accurate ECN trailer (7.0+) -----------------
    -- Kernel 7.0 appended these 11 members to `struct tcp_info`, growing the
    -- wire struct from 248 to 280 bytes (proto tags 1266-1276, pre-reserved).
    -- The trailer is optional on the wire, so A ZERO HERE IS AMBIGUOUS: on any
    -- kernel older than 7.0 it means "this kernel did not report the field",
    -- NOT "no CE marks". Every pre-7.0 capture in the corpus reports zero for
    -- all 11. Disambiguate with the reporting host's kernel version, never with
    -- the value itself.
    tcp_info_received_ce                                        UInt32 CODEC(LZ4),
    tcp_info_delivered_e1_bytes                                 UInt32 CODEC(LZ4),
    tcp_info_delivered_e0_bytes                                 UInt32 CODEC(LZ4),
    tcp_info_delivered_ce_bytes                                 UInt32 CODEC(LZ4),
    tcp_info_received_e1_bytes                                  UInt32 CODEC(LZ4),
    tcp_info_received_e0_bytes                                  UInt32 CODEC(LZ4),
    tcp_info_received_ce_bytes                                  UInt32 CODEC(LZ4),
    tcp_info_ecn_mode                                           UInt32 CODEC(LZ4),
    tcp_info_accecn_opt_seen                                    UInt32 CODEC(LZ4),
    tcp_info_accecn_fail_mode                                   UInt32 CODEC(LZ4),
    tcp_info_options2                                           UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_CONG (4) (1300s) ---------------------------------
    -- inet_diag_cong is the kernel ca_ops->name string; inet_diag_cong_enum is
    -- derived by xtcp from it (proto enum CongestionAlgorithm).
    inet_diag_cong                                              LowCardinality(String),
    inet_diag_cong_enum                                         Enum(''        = 0,
                                                                     'cubic'   = 1,
                                                                     'dctcp'   = 2,
                                                                     'vegas'   = 3,
                                                                     'prague'  = 4,
                                                                     'bbr1'    = 5,
                                                                     'bbr2'    = 6,
                                                                     'bbr3'    = 7
                                                                     ),

    -- ---- payload: INET_DIAG_TOS (5) / INET_DIAG_TCLASS (6) (1400s) ----------
    inet_diag_tos                                               UInt32 CODEC(LZ4),
    inet_diag_tclass                                            UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_SKMEMINFO (7), SK_MEMINFO_* (1500s) -------------
    sk_mem_info_rmem_alloc                                      UInt32 CODEC(LZ4),
    sk_mem_info_rcvbuf                                          UInt32 CODEC(LZ4),
    sk_mem_info_wmem_alloc                                      UInt32 CODEC(LZ4),
    sk_mem_info_sndbuf                                          UInt32 CODEC(LZ4),
    sk_mem_info_fwd_alloc                                       UInt32 CODEC(LZ4),
    sk_mem_info_wmem_queued                                     UInt32 CODEC(LZ4),
    sk_mem_info_optmem                                          UInt32 CODEC(LZ4),
    sk_mem_info_backlog                                         UInt32 CODEC(LZ4),
    sk_mem_info_drops                                           UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_SHUTDOWN (8) (1600s) -----------------------------
    inet_diag_shutdown                                          UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_VEGASINFO (3), struct tcpvegas_info (1700s) ------
    vegas_info_enabled                                          UInt32 CODEC(LZ4),
    vegas_info_rttcnt                                           UInt32 CODEC(LZ4),
    vegas_info_rtt                                              UInt32 CODEC(LZ4),
    vegas_info_minrtt                                           UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_DCTCPINFO (9), struct tcp_dctcp_info (1800s) -----
    dctcp_info_enabled                                          UInt32 CODEC(LZ4),
    dctcp_info_ce_state                                         UInt32 CODEC(LZ4),
    dctcp_info_alpha                                            UInt32 CODEC(LZ4),
    dctcp_info_ab_ecn                                           UInt32 CODEC(LZ4),
    dctcp_info_ab_tot                                           UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_BBRINFO (16), struct tcp_bbr_info (1900s) --------
    bbr_info_bw_lo                                              UInt32 CODEC(LZ4),
    bbr_info_bw_hi                                              UInt32 CODEC(LZ4),
    bbr_info_min_rtt                                            UInt32 CODEC(LZ4),
    bbr_info_pacing_gain                                        UInt32 CODEC(LZ4),
    bbr_info_cwnd_gain                                          UInt32 CODEC(LZ4),

    -- ---- payload: INET_DIAG_CLASS_ID (17) / SOCKOPT (22) / CGROUP_ID (21) (2000s)
    inet_diag_class_id                                          UInt32 CODEC(LZ4),
    inet_diag_sockopt                                           UInt32 CODEC(LZ4),
    inet_diag_cgroup_id                                         UInt64 CODEC(LZ4),
)
ENGINE = Kafka
SETTINGS
  kafka_broker_list = 'redpanda-0:9092',
  kafka_topic_list = 'xtcp',
  kafka_group_name = 'xtcp',
  -- ProtobufList format: kafka_schema MUST point at the ROW type
  -- (XtcpFlatRecord), NOT the Envelope wrapper. ClickHouse's
  -- ProtobufList handles the envelope framing internally; the schema
  -- describes how each row's fields map to table columns. Pointing at
  -- Envelope produces "NO_COLUMNS_SERIALIZED_TO_PROTOBUF_FIELDS" because
  -- Envelope only has the single 'row' field. See:
  -- build/containers/clickhouse/clickhouse_protolist_notes.md
  --
  -- The message name here is the SIMPLE, unqualified type name
  -- (XtcpFlatRecord) — do NOT prepend the proto package
  -- (xtcp_flat_record.v1.). ClickHouse's ProtobufList resolver
  -- (src/Formats/ProtobufSchemas.cpp) looks the name up via
  -- FileDescriptor::FindMessageTypeByName, which expects the name
  -- relative to the file's package; a package-qualified name such as
  -- 'xtcp_flat_record.v1.XtcpFlatRecord' deterministically fails with
  -- "Could not find a message named '...' in the schema file"
  -- (BAD_ARGUMENTS), the consumer detaches, and ingestion stalls.
  -- Regression introduced by 60da4c7 ("schema aligned with proto"),
  -- reverted here.
  kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord',
  kafka_format = 'ProtobufList',
  kafka_max_rows_per_message = 10000,
  kafka_num_consumers = 1,
  kafka_thread_per_consumer = 0,
  kafka_skip_broken_messages = 0,
  kafka_handle_error_mode = 'stream',
  -- ProtobufList already batches: each kafka message is an Envelope
  -- containing ~100-1000 XtcpFlatRecord rows. The kafka_engine's
  -- own Block accumulation (kafka_max_block_size, default 65,505 rows)
  -- is therefore mostly redundant on top — it just holds rows in memory
  -- across many kafka messages before pushing the MV. Combined with
  -- the per-poll batch (kafka_poll_max_batch_size, 16 messages here),
  -- a single MV flush at 65K rows was the source of 131 MiB chunk
  -- allocations that tipped CH's per-server memory cap.
  -- Settings:
  --   kafka_poll_max_batch_size = 16   ~16 kafka messages per poll
  --   kafka_max_block_size      = 1024 ~1 envelope per flush
  --   kafka_flush_interval_ms   = 2000 backstop: flush at most every 2 s
  -- With ~430 envelopeRows/sec from xtcp2 the Block fills in ~2.4 s on
  -- average, so flushes happen at the row-threshold most of the time
  -- and the time-backstop kicks in only when the producer is quiet.
  kafka_max_block_size = 1024,
  kafka_poll_max_batch_size = 16,
  kafka_flush_interval_ms = 2000;

-- SHOW CREATE TABLE xtcp.xtcp_flat_records_kafka;
-- SELECT * FROM system.kafka_consumers FORMAT Vertical;
-- SELECT * FROM system.kafka_consumers WHERE table LIKE '%xtcp_flat_records_kafka%' FORMAT Vertical;

-- https://github.com/ClickHouse/ClickHouse/blob/master/tests/integration/test_storage_kafka/test_batch_fast.py#L226

-- clickhouse-client --query "SELECT * FROM xtcp.xtcp_flat_records_v2 SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/xtcp_flat_record.proto:XtcpFlatRecord' FORMAT ProtobufList" > my_export.bin

-- client can use absolute path, but server cannot!
-- https://github.com/ClickHouse/ClickHouse/issues/4745

-- kafka_format = 'Protobuf',
-- kafka_schema = 'xtcp_flat_record.proto:xtcp_flat_record.v1.XtcpFlatRecord',
-- kafka_schema = 'xtcp_flat_record.proto:XtcpFlatRecord',
-- kafka_schema = 'xtcp_flat_record:xtcp_flat_record',
-- kafka_schema = 'xtcp_flat_record.proto:Envelope',
-- kafka_num_consumers = 1;
-- kafka_thread_per_consumer = 0;
-- kafka_skip_broken_messages = Y,

-- https://clickhouse.com/docs/en/interfaces/formats#protobuflist
-- https://github.com/ClickHouse/ClickHouse/pull/35152
-- https://github.com/ClickHouse/ClickHouse/issues/16436
-- kafka_format = 'ProtobufSingle',
-- https://github.com/ClickHouse/ClickHouse/blob/master/src/Storages/Kafka/KafkaSettings.cpp

-- https://clickhouse.com/docs/engines/table-engines/integrations/kafka#creating-a-table
-- CREATE TABLE [IF NOT EXISTS] [db.]table_name [ON CLUSTER cluster]
-- (
--     name1 [type1] [ALIAS expr1],
--     name2 [type2] [ALIAS expr2],
--     ...
-- ) ENGINE = Kafka()
-- SETTINGS
--     kafka_broker_list = 'host:port',
--     kafka_topic_list = 'topic1,topic2,...',
--     kafka_group_name = 'group_name',
--     kafka_format = 'data_format'[,]
--     [kafka_schema = '',]
--     [kafka_num_consumers = N,]
--     [kafka_max_block_size = 0,]
--     [kafka_skip_broken_messages = N,]
--     [kafka_commit_every_batch = 0,]
--     [kafka_client_id = '',]
--     [kafka_poll_timeout_ms = 0,]
--     [kafka_poll_max_batch_size = 0,]
--     [kafka_flush_interval_ms = 0,]
--     [kafka_thread_per_consumer = 0,]
--     [kafka_handle_error_mode = 'default',]
--     [kafka_commit_on_select = false,]
--     [kafka_max_rows_per_message = 1];

-- SELECT
--     *
-- FROM
--     xtcp.xtcp_flat_records_kafka
-- SETTINGS
--     stream_like_engine_allow_direct_select = 1;

-- DETACH TABLE xtcp.xtcp_flat_records_kafka;

-- https://clickhouse.com/docs/engines/table-engines/integrations/kafka#virtual-columns

-- end
