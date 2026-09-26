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
--   xtcp.xtcp_flat_records_v1  — epoch 1 (schema_version = 1); same columns as _v0
--   xtcp.xtcp_flat_records_v2  — current format (schema_version = 2): payload
--                                columns renamed to kernel struct spelling,
--                                enrichment block regrouped. See
--                                docs/record-versioning.md for the rename table.
-- xtcp.xtcp_flat_records is a Merge view over ^xtcp_flat_records_v[0-9]+$ so
-- existing queries/dashboards that hit xtcp_flat_records transparently span every
-- version. The Merge view is declared AS _v2 (the current, superset column set);
-- columns that do not exist in an older _vN table read as defaults for that
-- table's rows, so branch on schema_version when a renamed column matters.
--
-- Adding a future epoch: bump XtcpFlatRecordSchemaVersion, add a _vN table with
-- its own full DDL (AS _v(N-1) only if nothing was renamed), add a _vN MV, and
-- re-declare the Merge view AS the newest table. The Merge regex needs no edit.
--
-- The v0/v1 DDL below is frozen: it must keep the epoch-0/1 column names because
-- xtcp_xtcp_flat_records_mv.sql aliases the v2 Kafka columns onto them by NAME.

DROP TABLE IF EXISTS xtcp.xtcp_flat_records;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_v0;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_v1;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_v2;

CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v0
(
    -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
    timestamp_ns                                                DateTime64(9,'UTC') CODEC(DoubleDelta, LZ4),
    -- sec                                                         DateTime64(3,'UTC') CODEC(DoubleDelta, LZ4),
    -- nsec                                                        Int64,

    -- ---- metadata: record format provenance (1-2) --------------------------
    -- schema_version is the routing epoch (0 = legacy); daemon_version is build
    -- provenance.
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

    -- ---- enrichment: daemon-computed fields (300s) --------------------------
    -- NOT read from the kernel inet_diag message; computed during enrichment
    -- (rtnetlink address/route/link discovery, ipfeed ASN feeds). Empty/zero
    -- when the relevant enricher is disabled or had no answer.
    enrich_socket_interface_name                                LowCardinality(String),
    enrich_socket_dest_egress_ifindex                           UInt32 CODEC(LZ4),
    enrich_socket_dest_egress_ifname                            LowCardinality(String),
    enrich_socket_dest_locality                                 Enum('unspecified'  = 0,
                                                                     'self'         = 1,
                                                                     'local_subnet' = 2,
                                                                     'remote'       = 3
                                                                     ),
    enrich_socket_dest_asn                                      UInt64 CODEC(LZ4),
    enrich_socket_next_hop_asn                                  UInt64 CODEC(LZ4),
    enrich_socket_dest_network_owner                           LowCardinality(String),

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

    -- ---- payload: INET_DIAG_INFO Accurate ECN trailer (7.0+) -----------------
    -- Kernel 7.0 appended these 11 members to `struct tcp_info`, growing the
    -- wire struct from 248 to 280 bytes (proto tags 1266-1276, pre-reserved).
    -- The trailer is optional on the wire, so A ZERO HERE IS AMBIGUOUS: on any
    -- kernel older than 7.0 it means "this kernel did not report the field",
    -- NOT "no CE marks". Every pre-7.0 capture in the corpus reports zero for
    -- all 11. Disambiguate with the reporting host's kernel version, never with
    -- the value itself.
    --
    -- This epoch predates AccECN and will never populate these columns. They are
    -- carried anyway so that the Merge('xtcp', '^xtcp_flat_records_v[0-9]+$')
    -- table at the bottom of this file sees one consistent column set.
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

-- Epoch-1 table: identical structure/engine/ORDER BY/TTL to _v0 (the epoch-1
-- format only added fields in free slots, no renames), differing only in which
-- rows the MVs route here.
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v1 AS xtcp.xtcp_flat_records_v0;

-- Epoch-2 table (current). Full DDL, NOT "AS _v0": epoch 2 renamed 18 payload
-- columns to the kernel struct member spelling (tcp_info_rtt_var ->
-- tcp_info_rttvar, c_group -> inet_diag_cgroup_id, ...), renamed
-- enrich_socket_next_hop_asn -> enrich_socket_dest_next_hop_asn, and reordered
-- the enrichment block. Column order matches proto field-number order in
-- proto/xtcp_flat_record/v1/xtcp_flat_record.proto so the _v2 MV can use the
-- positional `* EXCEPT (timestamp_ns)` form. Keep the two in sync.
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v2
(
    -- ---- metadata: record format provenance (1-2) --------------------------
    -- schema_version is the routing epoch (0 = legacy, 2 = current); daemon_version
    -- is build provenance.
    schema_version                                              UInt32 CODEC(LZ4),
    daemon_version                                              LowCardinality(String),
    -- https://clickhouse.com/docs/en/sql-reference/data-types/datetime64
    timestamp_ns                                                DateTime64(9,'UTC') CODEC(DoubleDelta, LZ4),

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
  ENGINE = MergeTree
  -- ENGINE = ReplicatedMergeTree
  -- Note that for xtcp repo, the docker is MergeTree, while k8s is ReplicatedMergeTree
  ORDER BY (timestamp_ns, hostname, record_counter, netlinker_id, socket_fd)
  TTL toDateTime(timestamp_ns) + INTERVAL 1 MONTH DELETE;

-- Cross-version query surface. Read-only Merge over every ^xtcp_flat_records_v[0-9]+$
-- table (excludes _kafka, _errors, and the _mv views). Declared AS the newest
-- epoch so every current column name resolves; older tables contribute defaults
-- for columns they lack (their data lives under the pre-rename names, queryable
-- directly on xtcp_flat_records_v0 / _v1).
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records
  AS xtcp.xtcp_flat_records_v2
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
