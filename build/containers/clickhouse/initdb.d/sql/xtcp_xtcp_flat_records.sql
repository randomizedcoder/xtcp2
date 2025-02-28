--
-- Recreate xtcp_xtcp_flat_records.sql
--

-- Kafka Topic --> Kakfa Table Engine --> Materialized View -> MergeTree Table

-- https://clickhouse.com/docs/en/interfaces/formats#protobuf
-- https://clickhouse.com/docs/en/interfaces/formats#protobufsingle
-- https://clickhouse.com/docs/en/interfaces/formats#protobuflist

-- https://protobuf.dev/programming-guides/encoding/#structure

-- https://clickhouse.com/blog/optimize-clickhouse-codecs-compression-schema

-- https://altinity.com/blog/2019-7-new-encodings-to-improve-clickhouse
-- https://altinity.com/blog/clickhouse-for-time-series

DROP TABLE IF EXISTS xtcp.xtcp_flat_records;

CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records
(
    -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
    timestamp_ns                                                DateTime64(9,'UTC') CODEC(DoubleDelta, LZ4),
    -- sec                                                         DateTime64(3,'UTC') CODEC(DoubleDelta, LZ4),
    -- nsec                                                        Int64,

    -- https://clickhouse.com/docs/en/sql-reference/data-types/lowcardinality
    hostname                                                    LowCardinality(String),

    netns                                                       String CODEC(ZSTD),
    nsid                                                        UInt32 CODEC(LZ4),

    label                                                       LowCardinality(String),
    tag                                                         LowCardinality(String),

    record_counter                                              UInt64 CODEC(DoubleDelta, LZ4),
    socket_fd                                                   UInt64 CODEC(LZ4),
    netlinker_id                                                UInt64 CODEC(LZ4),

    inet_diag_msg_family                                        UInt32 CODEC(LZ4),
    inet_diag_msg_state                                         UInt32 CODEC(LZ4),
    -- inet_diag_msg_family                                        LowCardinality(UInt32),
    -- inet_diag_msg_state                                         LowCardinality(UInt32),
    inet_diag_msg_timer                                         UInt32 CODEC(LZ4),
    inet_diag_msg_retrans                                       UInt32 CODEC(LZ4),

    inet_diag_msg_socket_source_port                            UInt32 CODEC(LZ4),
    inet_diag_msg_socket_destination_port                       UInt32 CODEC(LZ4),
    inet_diag_msg_socket_source                                 String CODEC(ZSTD),
    inet_diag_msg_socket_source_ipv4                            Nullable(IPv4),
    inet_diag_msg_socket_source_ipv6                            Nullable(IPv6),
    inet_diag_msg_socket_destination                            String CODEC(ZSTD),
    inet_diag_msg_socket_destination_ipv4                       Nullable(IPv4),
    inet_diag_msg_socket_destination_ipv6                       Nullable(IPv6),
    inet_diag_msg_socket_interface                              UInt32 CODEC(LZ4),
    inet_diag_msg_socket_cookie                                 UInt64 CODEC(LZ4),
    inet_diag_msg_socket_dest_asn                               UInt64 CODEC(LZ4),
    inet_diag_msg_socket_next_hop_asn                           UInt64 CODEC(LZ4),
    inet_diag_msg_socket_source_asn                             UInt64 CODEC(LZ4),

    inet_diag_msg_expires                                       UInt32 CODEC(LZ4),
    inet_diag_msg_rqueue                                        UInt32 CODEC(LZ4),
    inet_diag_msg_wqueue                                        UInt32 CODEC(LZ4),
    inet_diag_msg_uid                                           UInt32 CODEC(LZ4),
    inet_diag_msg_inode                                         UInt32 CODEC(LZ4),

    mem_info_rmem                                               UInt32 CODEC(LZ4),
    mem_info_wmem                                               UInt32 CODEC(LZ4),
    mem_info_fmem                                               UInt32 CODEC(LZ4),
    mem_info_tmem                                               UInt32 CODEC(LZ4),

    tcp_info_state                                              UInt32 CODEC(LZ4),
    tcp_info_ca_state                                           UInt32 CODEC(LZ4),
    -- tcp_info_state                                              LowCardinality(UInt32),
    -- tcp_info_ca_state                                           LowCardinality(UInt32),
    tcp_info_retransmits                                        UInt32 CODEC(LZ4),
    tcp_info_probes                                             UInt32 CODEC(LZ4),
    tcp_info_backoff                                            UInt32 CODEC(LZ4),
    tcp_info_options                                            UInt32 CODEC(LZ4),
    tcp_info_send_scale                                         UInt32 CODEC(LZ4),
    tcp_info_rcv_scale                                          UInt32 CODEC(LZ4),
    tcp_info_delivery_rate_app_limited                          UInt32 CODEC(LZ4),
    tcp_info_fast_open_client_failed                            UInt32 CODEC(LZ4),
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
    -- tcp_info_pmtu                                               LowCardinality(UInt32),
    tcp_info_rcv_ssthresh                                       UInt32 CODEC(LZ4),
    tcp_info_rtt                                                UInt32 CODEC(LZ4),
    tcp_info_rtt_var                                            UInt32 CODEC(LZ4),
    tcp_info_snd_ssthresh                                       UInt32 CODEC(LZ4),
    tcp_info_snd_cwnd                                           UInt32 CODEC(LZ4),
    tcp_info_adv_mss                                            UInt32 CODEC(LZ4),
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
    tcp_info_not_sent_bytes                                     UInt32 CODEC(LZ4),
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

    congestion_algorithm_string                                 LowCardinality(String),
    -- congestion_algorithm_enum                                   LowCardinality(String),
    congestion_algorithm_enum                                   Enum(''        = 0,
                                                                     'cubic'   = 1,
                                                                     'dctcp'   = 2,
                                                                     'vegas'   = 3,
                                                                     'prague'  = 4,
                                                                     'bbr1'    = 5,
                                                                     'bbr2'    = 6,
                                                                     'bbr3'    = 7
                                                                     ),

    -- enum CongestionAlgorithm {
    --   CONGESTION_ALGORITHM_UNSPECIFIED = 0;
    --   CONGESTION_ALGORITHM_CUBIC       = 1;
    --   CONGESTION_ALGORITHM_DCTCP       = 2;
    --   CONGESTION_ALGORITHM_VEGAS       = 3;
    --   CONGESTION_ALGORITHM_PRAGUE      = 4;
    --   CONGESTION_ALGORITHM_BBR1        = 5;
    --   CONGESTION_ALGORITHM_BBR2        = 6;
    --   CONGESTION_ALGORITHM_BBR3        = 7;
    -- };

    type_of_service                                             UInt32 CODEC(LZ4),
    traffic_class                                               UInt32 CODEC(LZ4),
    -- type_of_service                                             LowCardinality(UInt32),
    -- traffic_class                                               LowCardinality(UInt32),

    sk_mem_info_rmem_alloc                                      UInt32 CODEC(LZ4),
    sk_mem_info_rcv_buf                                         UInt32 CODEC(LZ4),
    sk_mem_info_wmem_alloc                                      UInt32 CODEC(LZ4),
    sk_mem_info_snd_buf                                         UInt32 CODEC(LZ4),
    sk_mem_info_fwd_alloc                                       UInt32 CODEC(LZ4),
    sk_mem_info_wmem_queued                                     UInt32 CODEC(LZ4),
    sk_mem_info_optmem                                          UInt32 CODEC(LZ4),
    sk_mem_info_backlog                                         UInt32 CODEC(LZ4),
    sk_mem_info_drops                                           UInt32 CODEC(LZ4),

    shutdown_state                                              UInt32 CODEC(LZ4),
    -- shutdown_state                                              LowCardinality(UInt32),

    vegas_info_enabled                                          UInt32 CODEC(LZ4),
    -- vegas_info_enabled                                          LowCardinality(UInt32),
    vegas_info_rtt_cnt                                          UInt32 CODEC(LZ4),
    vegas_info_rtt                                              UInt32 CODEC(LZ4),
    vegas_info_min_rtt                                          UInt32 CODEC(LZ4),

    dctcp_info_enabled                                          UInt32 CODEC(LZ4),
    -- dctcp_info_enabled                                          LowCardinality(UInt32),
    dctcp_info_ce_state                                         UInt32 CODEC(LZ4),
    dctcp_info_alpha                                            UInt32 CODEC(LZ4),
    dctcp_info_ab_ecn                                           UInt32 CODEC(LZ4),
    dctcp_info_ab_tot                                           UInt32 CODEC(LZ4),

    bbr_info_bw_lo                                              UInt32 CODEC(LZ4),
    bbr_info_bw_hi                                              UInt32 CODEC(LZ4),
    bbr_info_min_rtt                                            UInt32 CODEC(LZ4),
    bbr_info_pacing_gain                                        UInt32 CODEC(LZ4),
    bbr_info_cwnd_gain                                          UInt32 CODEC(LZ4),

    class_id                                                    UInt32 CODEC(LZ4), -- LowCardinality?
    sock_opt                                                    UInt32 CODEC(LZ4), -- LowCardinality?
    c_group                                                     UInt64 CODEC(LZ4),

)
  ENGINE = MergeTree
  -- ENGINE = ReplicatedMergeTree
  -- Note that for xtcp repo, the docker is MergeTree, while k8s is ReplicatedMergeTree
  -- PARTITION BY toYYYYMMDD(sec)
  ORDER BY (timestamp_ns, hostname, record_counter, netlinker_id, socket_fd)
  -- ORDER BY (sec, nsec, hostname, record_counter, netlinker_id, socket_fd)
  TTL toDateTime(timestamp_ns) + INTERVAL 1 MONTH DELETE;
  --TTL toDateTime(sec) + INTERVAL 2 MONTH DELETE;

-- https://clickhouse.com/docs/integrations/kafka/kafka-table-engine#adding-kafka-metadata
-- https://clickhouse.com/docs/engines/table-engines/integrations/kafka#virtual-columns
-- ALTER TABLE xtcp.xtcp_flat_records
--   ADD COLUMN topic String,
--   ADD COLUMN key String,
--   ADD COLUMN offset UInt64,
--   ADD COLUMN timestamp_ms Nullable(DateTime64(3)),
--   ADD COLUMN partition UInt64,
--   ADD COLUMN error String;

-- end