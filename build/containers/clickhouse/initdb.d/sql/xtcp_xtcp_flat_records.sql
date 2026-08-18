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

-- Per-version routing. Rows are fanned out by schema_version (see the versioned
-- MVs in xtcp_xtcp_flat_records_mv.sql) into:
--   xtcp.xtcp_flat_records_v0  — legacy / pre-versioning rows (schema_version = 0)
--   xtcp.xtcp_flat_records_v1  — current format (schema_version = 1)
-- xtcp.xtcp_flat_records is a Merge view over ^xtcp_flat_records_v[0-9]+$ so
-- existing queries/dashboards that hit xtcp_flat_records transparently span every
-- version. Adding a future epoch = add a _vN table (CREATE ... AS _v0) + a _vN MV;
-- the Merge regex picks it up with no edit here. _v1 is created "AS _v0" so the two
-- physical tables can never drift.

DROP TABLE IF EXISTS xtcp.xtcp_flat_records;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_v0;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_v1;

CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v0
(
    -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
    timestamp_ns                                                DateTime64(9,'UTC') CODEC(DoubleDelta, LZ4),
    -- sec                                                         DateTime64(3,'UTC') CODEC(DoubleDelta, LZ4),
    -- nsec                                                        Int64,

    -- ---- metadata: record format provenance (1-2) --------------------------
    -- schema_version is the routing epoch (0 = legacy); daemon_version is build
    -- provenance. Placed right after timestamp_ns to match the Kafka table +
    -- positional MV expansion.
    schema_version                                              UInt32 CODEC(LZ4),
    daemon_version                                              LowCardinality(String),

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

    inet_diag_msg_family                                        UInt32 CODEC(LZ4),
    inet_diag_msg_state                                         UInt32 CODEC(LZ4),
    -- inet_diag_msg_family                                        LowCardinality(UInt32),
    -- inet_diag_msg_state                                         LowCardinality(UInt32),
    inet_diag_msg_timer                                         UInt32 CODEC(LZ4),
    inet_diag_msg_retrans                                       UInt32 CODEC(LZ4),

    inet_diag_msg_socket_source_port                            UInt32 CODEC(LZ4),
    inet_diag_msg_socket_destination_port                       UInt32 CODEC(LZ4),
    inet_diag_msg_socket_source                                 String CODEC(ZSTD),
    inet_diag_msg_socket_destination                            String CODEC(ZSTD),
    inet_diag_msg_socket_interface                              UInt32 CODEC(LZ4),
    inet_diag_msg_socket_cookie                                 UInt64 CODEC(LZ4),
    inet_diag_msg_socket_dest_asn                               UInt64 CODEC(LZ4),
    inet_diag_msg_socket_next_hop_asn                           UInt64 CODEC(LZ4),

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
    tcp_info_rcv_wnd                                            UInt32 CODEC(LZ4),
    tcp_info_rehash                                             UInt32 CODEC(LZ4),
    tcp_info_total_rto                                          UInt32 CODEC(LZ4),
    tcp_info_total_rto_recoveries                               UInt32 CODEC(LZ4),
    tcp_info_total_rto_time                                     UInt32 CODEC(LZ4),

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

-- Current-format table: identical structure/engine/ORDER BY/TTL to _v0 (so the two
-- physical tables can never drift), differing only in which rows the MVs route here.
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v1 AS xtcp.xtcp_flat_records_v0;

-- Cross-version query surface. Read-only Merge over every ^xtcp_flat_records_v[0-9]+$
-- table (excludes _kafka, _errors, and the _mv views). Backward-compatible: queries
-- against xtcp_flat_records keep working and now span all versions.
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records
  AS xtcp.xtcp_flat_records_v0
  ENGINE = Merge('xtcp', '^xtcp_flat_records_v[0-9]+$');

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