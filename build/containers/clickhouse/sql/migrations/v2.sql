--
-- Migration: record format epoch 1 -> epoch 2 (XtcpFlatRecordSchemaVersion = 2)
--
-- For EXISTING deployments that already run the epoch-0/1 layout from
-- build/containers/clickhouse/initdb.d/sql/. Fresh deployments do not need this:
-- initdb.d recreates everything from scratch.
--
-- Apply with (data in _v0/_v1 is untouched; only the Kafka table, the MVs and the
-- Merge view are recreated, plus a new empty _v2 table):
--
--   clickhouse-client --multiquery < build/containers/clickhouse/sql/migrations/v2.sql
--
-- What changed in epoch 2 (proto/xtcp_flat_record/v1/xtcp_flat_record.proto):
--   * 18 payload columns renamed to the kernel struct member spelling, e.g.
--     tcp_info_rtt_var -> tcp_info_rttvar, type_of_service -> inet_diag_tos,
--     congestion_algorithm_string -> inet_diag_cong, c_group -> inet_diag_cgroup_id.
--   * enrich_socket_next_hop_asn -> enrich_socket_dest_next_hop_asn.
--   * Three fields changed NUMBER: enrich_socket_dest_egress_ifindex 301 -> 311,
--     enrich_socket_dest_egress_ifname 302 -> 312, c_group 2103 ->
--     inet_diag_cgroup_id 2003.
--   * Locality enum label 'connected_subnet' -> 'local_subnet' (value 2 unchanged).
--   Full table: docs/record-versioning.md.
--
-- Mixed-fleet behaviour while epoch-1 daemons are still producing (the Kafka
-- table decodes by column NAME -> proto tag against the epoch-2 schema):
--   * renamed-only fields keep their tag, decode fine, and the _v1 MV aliases
--     them back onto the old _v1 column names -> no data loss;
--   * the three RENUMBERED fields are unknown tags for epoch-1 rows and are
--     DROPPED: _v1.enrich_socket_dest_egress_ifindex/ifname and _v1.c_group read
--     as 0/'' for rows produced after this migration until the daemon fleet is on
--     epoch 2. Roll the daemons soon after applying this.
--
-- Cross-epoch reads: the Merge view xtcp.xtcp_flat_records is re-declared AS
-- _v2, so epoch-2 column names resolve everywhere; for epoch-0/1 rows those
-- columns read as defaults (their data is under the old names in _v0/_v1).
-- If you prefer one coherent name set across all epochs instead, run
--   ALTER TABLE xtcp.xtcp_flat_records_v1 RENAME COLUMN tcp_info_rtt_var TO tcp_info_rttvar, ...
-- for each pair in the rename table AND switch _v0_mv/_v1_mv to the
-- `* EXCEPT (timestamp_ns)` form. This file deliberately does not do that, to
-- leave the epoch-0/1 tables exactly as they were.

-- Also copy the regenerated schema file into the server's format_schemas dir
-- before running this (the Kafka table below references it):
--   build/containers/clickhouse/format_schemas/xtcp_flat_record.proto
--   -> /var/lib/clickhouse/format_schemas/xtcp_flat_record.proto

-- 1. Stop ingestion while the schema swaps.
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_mv;
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_v0_mv;
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_v1_mv;
DROP VIEW IF EXISTS xtcp.xtcp_flat_records_v2_mv;
DROP TABLE IF EXISTS xtcp.xtcp_flat_records_kafka;

-- 2. Bring the existing epoch-0/1 tables up to the column set the new
--    _v0_mv/_v1_mv alias lists insert into. Deployments created from main before
--    the enrichment block landed (xtcp2 <= 1.3.x) lack these seven columns;
--    ADD COLUMN IF NOT EXISTS is a no-op where they already exist. The two
--    never-populated epoch-1 columns inet_diag_msg_socket_dest_asn /
--    inet_diag_msg_socket_next_hop_asn (tags 1011/1012, now reserved) are left in
--    place; nothing writes them any more.
ALTER TABLE xtcp.xtcp_flat_records_v0
    ADD COLUMN IF NOT EXISTS enrich_socket_interface_name       LowCardinality(String) AFTER uplink2_lldp_port_descr,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_egress_ifindex  UInt32 CODEC(LZ4)      AFTER enrich_socket_interface_name,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_egress_ifname   LowCardinality(String) AFTER enrich_socket_dest_egress_ifindex,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_locality        Enum('unspecified' = 0, 'self' = 1, 'local_subnet' = 2, 'remote' = 3) AFTER enrich_socket_dest_egress_ifname,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_asn             UInt64 CODEC(LZ4)      AFTER enrich_socket_dest_locality,
    ADD COLUMN IF NOT EXISTS enrich_socket_next_hop_asn         UInt64 CODEC(LZ4)      AFTER enrich_socket_dest_asn,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_network_owner   LowCardinality(String) AFTER enrich_socket_next_hop_asn;
ALTER TABLE xtcp.xtcp_flat_records_v1
    ADD COLUMN IF NOT EXISTS enrich_socket_interface_name       LowCardinality(String) AFTER uplink2_lldp_port_descr,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_egress_ifindex  UInt32 CODEC(LZ4)      AFTER enrich_socket_interface_name,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_egress_ifname   LowCardinality(String) AFTER enrich_socket_dest_egress_ifindex,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_locality        Enum('unspecified' = 0, 'self' = 1, 'local_subnet' = 2, 'remote' = 3) AFTER enrich_socket_dest_egress_ifname,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_asn             UInt64 CODEC(LZ4)      AFTER enrich_socket_dest_locality,
    ADD COLUMN IF NOT EXISTS enrich_socket_next_hop_asn         UInt64 CODEC(LZ4)      AFTER enrich_socket_dest_asn,
    ADD COLUMN IF NOT EXISTS enrich_socket_dest_network_owner   LowCardinality(String) AFTER enrich_socket_next_hop_asn;

--    Unify the locality label where the column pre-existed as
--    'connected_subnet' (metadata-only; the stored UInt8 values are unchanged).
ALTER TABLE xtcp.xtcp_flat_records_v0 MODIFY COLUMN enrich_socket_dest_locality
    Enum('unspecified' = 0, 'self' = 1, 'local_subnet' = 2, 'remote' = 3);
ALTER TABLE xtcp.xtcp_flat_records_v1 MODIFY COLUMN enrich_socket_dest_locality
    Enum('unspecified' = 0, 'self' = 1, 'local_subnet' = 2, 'remote' = 3);

-- 3. New epoch-2 table.
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

-- 4. Kafka table with the epoch-2 column set.
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

-- 5. Re-create the per-epoch MVs (v0/v1 alias new -> old names; v2 positional).
-- Legacy bucket: pre-versioning daemons never set schema_version, so it decodes to
-- proto3 zero. Those rows land in _v0. Epoch 0 never sent enrichment fields, so
-- the aliased enrichment columns are always default here.
CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_v0_mv TO xtcp.xtcp_flat_records_v0
  AS SELECT
    schema_version,
    daemon_version,
    fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns,
    hostname,
    location,
    netns,
    netns_inode,
    nsid,
    container_id,
    container_runtime,
    container_name,
    container_image,
    label,
    tag,
    record_counter,
    socket_fd,
    netlinker_id,
    uplink1_ifname,
    uplink1_nic_driver,
    uplink1_nic_model,
    uplink1_nic_pci_vendor,
    uplink1_nic_pci_device,
    uplink1_nic_bus_info,
    uplink1_nic_speed_mbps,
    uplink1_nic_fw_version,
    uplink1_lldp_chassis_name,
    uplink1_lldp_chassis_id,
    uplink1_lldp_mgmt_ip,
    uplink1_lldp_port_id,
    uplink1_lldp_port_descr,
    uplink2_ifname,
    uplink2_nic_driver,
    uplink2_nic_model,
    uplink2_nic_pci_vendor,
    uplink2_nic_pci_device,
    uplink2_nic_bus_info,
    uplink2_nic_speed_mbps,
    uplink2_nic_fw_version,
    uplink2_lldp_chassis_name,
    uplink2_lldp_chassis_id,
    uplink2_lldp_mgmt_ip,
    uplink2_lldp_port_id,
    uplink2_lldp_port_descr,
    enrich_socket_interface_name,
    toUInt8(enrich_socket_dest_locality) AS enrich_socket_dest_locality,
    enrich_socket_dest_egress_ifindex,
    enrich_socket_dest_egress_ifname,
    enrich_socket_dest_asn,
    enrich_socket_dest_next_hop_asn          AS enrich_socket_next_hop_asn,
    enrich_socket_dest_network_owner,
    inet_diag_msg_family,
    inet_diag_msg_state,
    inet_diag_msg_timer,
    inet_diag_msg_retrans,
    inet_diag_msg_socket_source_port,
    inet_diag_msg_socket_destination_port,
    inet_diag_msg_socket_source,
    inet_diag_msg_socket_destination,
    inet_diag_msg_socket_interface,
    inet_diag_msg_socket_cookie,
    inet_diag_msg_expires,
    inet_diag_msg_rqueue,
    inet_diag_msg_wqueue,
    inet_diag_msg_uid,
    inet_diag_msg_inode,
    mem_info_rmem,
    mem_info_wmem,
    mem_info_fmem,
    mem_info_tmem,
    tcp_info_state,
    tcp_info_ca_state,
    tcp_info_retransmits,
    tcp_info_probes,
    tcp_info_backoff,
    tcp_info_options,
    tcp_info_snd_wscale                      AS tcp_info_send_scale,
    tcp_info_rcv_wscale                      AS tcp_info_rcv_scale,
    tcp_info_delivery_rate_app_limited,
    tcp_info_fastopen_client_fail            AS tcp_info_fast_open_client_failed,
    tcp_info_rto,
    tcp_info_ato,
    tcp_info_snd_mss,
    tcp_info_rcv_mss,
    tcp_info_unacked,
    tcp_info_sacked,
    tcp_info_lost,
    tcp_info_retrans,
    tcp_info_fackets,
    tcp_info_last_data_sent,
    tcp_info_last_ack_sent,
    tcp_info_last_data_recv,
    tcp_info_last_ack_recv,
    tcp_info_pmtu,
    tcp_info_rcv_ssthresh,
    tcp_info_rtt,
    tcp_info_rttvar                          AS tcp_info_rtt_var,
    tcp_info_snd_ssthresh,
    tcp_info_snd_cwnd,
    tcp_info_advmss                          AS tcp_info_adv_mss,
    tcp_info_reordering,
    tcp_info_rcv_rtt,
    tcp_info_rcv_space,
    tcp_info_total_retrans,
    tcp_info_pacing_rate,
    tcp_info_max_pacing_rate,
    tcp_info_bytes_acked,
    tcp_info_bytes_received,
    tcp_info_segs_out,
    tcp_info_segs_in,
    tcp_info_notsent_bytes                   AS tcp_info_not_sent_bytes,
    tcp_info_min_rtt,
    tcp_info_data_segs_in,
    tcp_info_data_segs_out,
    tcp_info_delivery_rate,
    tcp_info_busy_time,
    tcp_info_rwnd_limited,
    tcp_info_sndbuf_limited,
    tcp_info_delivered,
    tcp_info_delivered_ce,
    tcp_info_bytes_sent,
    tcp_info_bytes_retrans,
    tcp_info_dsack_dups,
    tcp_info_reord_seen,
    tcp_info_rcv_ooopack,
    tcp_info_snd_wnd,
    tcp_info_rcv_wnd,
    tcp_info_rehash,
    tcp_info_total_rto,
    tcp_info_total_rto_recoveries,
    tcp_info_total_rto_time,
    inet_diag_cong                           AS congestion_algorithm_string,
    toUInt8(inet_diag_cong_enum) AS congestion_algorithm_enum,
    inet_diag_tos                            AS type_of_service,
    inet_diag_tclass                         AS traffic_class,
    sk_mem_info_rmem_alloc,
    sk_mem_info_rcvbuf                       AS sk_mem_info_rcv_buf,
    sk_mem_info_wmem_alloc,
    sk_mem_info_sndbuf                       AS sk_mem_info_snd_buf,
    sk_mem_info_fwd_alloc,
    sk_mem_info_wmem_queued,
    sk_mem_info_optmem,
    sk_mem_info_backlog,
    sk_mem_info_drops,
    inet_diag_shutdown                       AS shutdown_state,
    vegas_info_enabled,
    vegas_info_rttcnt                        AS vegas_info_rtt_cnt,
    vegas_info_rtt,
    vegas_info_minrtt                        AS vegas_info_min_rtt,
    dctcp_info_enabled,
    dctcp_info_ce_state,
    dctcp_info_alpha,
    dctcp_info_ab_ecn,
    dctcp_info_ab_tot,
    bbr_info_bw_lo,
    bbr_info_bw_hi,
    bbr_info_min_rtt,
    bbr_info_pacing_gain,
    bbr_info_cwnd_gain,
    inet_diag_class_id                       AS class_id,
    inet_diag_sockopt                        AS sock_opt,
    inet_diag_cgroup_id                      AS c_group
  FROM xtcp.xtcp_flat_records_kafka
  WHERE length(_error) == 0 AND schema_version = 0;

-- Epoch 1 (XtcpFlatRecordSchemaVersion = 1). Same alias list as _v0_mv. Note the
-- three renumbered fields (egress_ifindex/ifname, cgroup id) cannot be recovered
-- for epoch-1 rows: the Kafka schema only knows their epoch-2 tags.
CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_v1_mv TO xtcp.xtcp_flat_records_v1
  AS SELECT
    schema_version,
    daemon_version,
    fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns,
    hostname,
    location,
    netns,
    netns_inode,
    nsid,
    container_id,
    container_runtime,
    container_name,
    container_image,
    label,
    tag,
    record_counter,
    socket_fd,
    netlinker_id,
    uplink1_ifname,
    uplink1_nic_driver,
    uplink1_nic_model,
    uplink1_nic_pci_vendor,
    uplink1_nic_pci_device,
    uplink1_nic_bus_info,
    uplink1_nic_speed_mbps,
    uplink1_nic_fw_version,
    uplink1_lldp_chassis_name,
    uplink1_lldp_chassis_id,
    uplink1_lldp_mgmt_ip,
    uplink1_lldp_port_id,
    uplink1_lldp_port_descr,
    uplink2_ifname,
    uplink2_nic_driver,
    uplink2_nic_model,
    uplink2_nic_pci_vendor,
    uplink2_nic_pci_device,
    uplink2_nic_bus_info,
    uplink2_nic_speed_mbps,
    uplink2_nic_fw_version,
    uplink2_lldp_chassis_name,
    uplink2_lldp_chassis_id,
    uplink2_lldp_mgmt_ip,
    uplink2_lldp_port_id,
    uplink2_lldp_port_descr,
    enrich_socket_interface_name,
    toUInt8(enrich_socket_dest_locality) AS enrich_socket_dest_locality,
    enrich_socket_dest_egress_ifindex,
    enrich_socket_dest_egress_ifname,
    enrich_socket_dest_asn,
    enrich_socket_dest_next_hop_asn          AS enrich_socket_next_hop_asn,
    enrich_socket_dest_network_owner,
    inet_diag_msg_family,
    inet_diag_msg_state,
    inet_diag_msg_timer,
    inet_diag_msg_retrans,
    inet_diag_msg_socket_source_port,
    inet_diag_msg_socket_destination_port,
    inet_diag_msg_socket_source,
    inet_diag_msg_socket_destination,
    inet_diag_msg_socket_interface,
    inet_diag_msg_socket_cookie,
    inet_diag_msg_expires,
    inet_diag_msg_rqueue,
    inet_diag_msg_wqueue,
    inet_diag_msg_uid,
    inet_diag_msg_inode,
    mem_info_rmem,
    mem_info_wmem,
    mem_info_fmem,
    mem_info_tmem,
    tcp_info_state,
    tcp_info_ca_state,
    tcp_info_retransmits,
    tcp_info_probes,
    tcp_info_backoff,
    tcp_info_options,
    tcp_info_snd_wscale                      AS tcp_info_send_scale,
    tcp_info_rcv_wscale                      AS tcp_info_rcv_scale,
    tcp_info_delivery_rate_app_limited,
    tcp_info_fastopen_client_fail            AS tcp_info_fast_open_client_failed,
    tcp_info_rto,
    tcp_info_ato,
    tcp_info_snd_mss,
    tcp_info_rcv_mss,
    tcp_info_unacked,
    tcp_info_sacked,
    tcp_info_lost,
    tcp_info_retrans,
    tcp_info_fackets,
    tcp_info_last_data_sent,
    tcp_info_last_ack_sent,
    tcp_info_last_data_recv,
    tcp_info_last_ack_recv,
    tcp_info_pmtu,
    tcp_info_rcv_ssthresh,
    tcp_info_rtt,
    tcp_info_rttvar                          AS tcp_info_rtt_var,
    tcp_info_snd_ssthresh,
    tcp_info_snd_cwnd,
    tcp_info_advmss                          AS tcp_info_adv_mss,
    tcp_info_reordering,
    tcp_info_rcv_rtt,
    tcp_info_rcv_space,
    tcp_info_total_retrans,
    tcp_info_pacing_rate,
    tcp_info_max_pacing_rate,
    tcp_info_bytes_acked,
    tcp_info_bytes_received,
    tcp_info_segs_out,
    tcp_info_segs_in,
    tcp_info_notsent_bytes                   AS tcp_info_not_sent_bytes,
    tcp_info_min_rtt,
    tcp_info_data_segs_in,
    tcp_info_data_segs_out,
    tcp_info_delivery_rate,
    tcp_info_busy_time,
    tcp_info_rwnd_limited,
    tcp_info_sndbuf_limited,
    tcp_info_delivered,
    tcp_info_delivered_ce,
    tcp_info_bytes_sent,
    tcp_info_bytes_retrans,
    tcp_info_dsack_dups,
    tcp_info_reord_seen,
    tcp_info_rcv_ooopack,
    tcp_info_snd_wnd,
    tcp_info_rcv_wnd,
    tcp_info_rehash,
    tcp_info_total_rto,
    tcp_info_total_rto_recoveries,
    tcp_info_total_rto_time,
    inet_diag_cong                           AS congestion_algorithm_string,
    toUInt8(inet_diag_cong_enum) AS congestion_algorithm_enum,
    inet_diag_tos                            AS type_of_service,
    inet_diag_tclass                         AS traffic_class,
    sk_mem_info_rmem_alloc,
    sk_mem_info_rcvbuf                       AS sk_mem_info_rcv_buf,
    sk_mem_info_wmem_alloc,
    sk_mem_info_sndbuf                       AS sk_mem_info_snd_buf,
    sk_mem_info_fwd_alloc,
    sk_mem_info_wmem_queued,
    sk_mem_info_optmem,
    sk_mem_info_backlog,
    sk_mem_info_drops,
    inet_diag_shutdown                       AS shutdown_state,
    vegas_info_enabled,
    vegas_info_rttcnt                        AS vegas_info_rtt_cnt,
    vegas_info_rtt,
    vegas_info_minrtt                        AS vegas_info_min_rtt,
    dctcp_info_enabled,
    dctcp_info_ce_state,
    dctcp_info_alpha,
    dctcp_info_ab_ecn,
    dctcp_info_ab_tot,
    bbr_info_bw_lo,
    bbr_info_bw_hi,
    bbr_info_min_rtt,
    bbr_info_pacing_gain,
    bbr_info_cwnd_gain,
    inet_diag_class_id                       AS class_id,
    inet_diag_sockopt                        AS sock_opt,
    inet_diag_cgroup_id                      AS c_group
  FROM xtcp.xtcp_flat_records_kafka
  WHERE length(_error) == 0 AND schema_version = 1;

-- Current format (XtcpFlatRecordSchemaVersion = 2). Column names match 1:1.
CREATE MATERIALIZED VIEW xtcp.xtcp_flat_records_v2_mv TO xtcp.xtcp_flat_records_v2
  AS SELECT
    fromUnixTimestamp64Nano(timestamp_ns) AS timestamp_ns,
    * EXCEPT (timestamp_ns)
  FROM xtcp.xtcp_flat_records_kafka
  WHERE length(_error) == 0 AND schema_version = 2;

-- 6. Re-declare the Merge view AS the newest epoch.
DROP TABLE IF EXISTS xtcp.xtcp_flat_records;
CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records
  AS xtcp.xtcp_flat_records_v2
  ENGINE = Merge('xtcp', '^xtcp_flat_records_v[0-9]+$');

-- 7. Verify.
-- SELECT schema_version, count() FROM xtcp.xtcp_flat_records GROUP BY 1 ORDER BY 1;
-- SELECT * FROM system.kafka_consumers WHERE table = 'xtcp_flat_records_kafka' FORMAT Vertical;

-- end
