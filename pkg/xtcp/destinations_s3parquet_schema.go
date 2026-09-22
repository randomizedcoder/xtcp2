//go:build dest_s3parquet

package xtcp

// ParquetRow mirrors xtcp_flat_record.v1.XtcpFlatRecord one-to-one, in proto
// DECLARATION ORDER. Each proto field becomes one Parquet column, named via
// the `parquet:` tag using the proto field's snake_case name (NOT the Go
// field's PascalCase) so SQL on the Parquet files matches SQL on the
// ClickHouse table. Go field names follow the generated proto Go names so
// rowFromProto reads as a straight copy.
//
// Compression strategy mirrors the ClickHouse codec choices in
// build/containers/clickhouse/initdb.d/sql/xtcp_xtcp_flat_records.sql:
//   - ZSTD for strings + bytes (high-entropy, low-cardinality-friendly via
//     parquet-go's column-level dictionary encoding on top of ZSTD)
//   - SNAPPY for numeric columns (fast, decent ratio, broad reader support)
//
// Drift defense: TestS3ParquetSchema_matchesProto asserts that the set of
// `parquet:` tag names here exactly matches the field-name set in
// xtcp_flat_record.XtcpFlatRecord's proto descriptor. If you add a field
// to the proto, that test fails until you mirror it here. The sole exception
// is the derived event_date column (allowlisted in that test).
//
// Schema evolution: a proto field RENAME renames the Parquet column and ships
// as a schema_version bump (see pkg/xtcp/schema_version.go); readers spanning
// epochs branch on schema_version. See docs/parquet-format.md.
type ParquetRow struct {
	// ---- metadata: record format provenance (1-2)
	SchemaVersion uint32 `parquet:"schema_version,snappy"`
	DaemonVersion string `parquet:"daemon_version,zstd"`

	// ---- metadata: time (10)
	TimestampNs int64 `parquet:"timestamp_ns,snappy"`

	// event_date: derived, not a proto field. Per-row UTC date of timestamp_ns,
	// named event_date (not date) to avoid the hive path-segment collision.
	EventDate string `parquet:"event_date,zstd"`

	// ---- metadata: host identity (20s)
	Hostname string `parquet:"hostname,zstd"`
	Location string `parquet:"location,zstd"`

	// ---- metadata: network namespace identity (30s)
	Netns      string `parquet:"netns,zstd"`
	NetnsInode uint64 `parquet:"netns_inode,snappy"`
	Nsid       uint32 `parquet:"nsid,snappy"`

	// ---- metadata: container identity (40s)
	ContainerId      string `parquet:"container_id,zstd"`
	ContainerRuntime string `parquet:"container_runtime,zstd"`
	ContainerName    string `parquet:"container_name,zstd"`
	ContainerImage   string `parquet:"container_image,zstd"`

	// ---- metadata: free-form labels (50s)
	Label string `parquet:"label,zstd"`
	Tag   string `parquet:"tag,zstd"`

	// ---- metadata: record bookkeeping (60s)
	RecordCounter uint64 `parquet:"record_counter,snappy"`
	SocketFd      uint64 `parquet:"socket_fd,snappy"`
	NetlinkerId   uint64 `parquet:"netlinker_id,snappy"`

	// ---- metadata: host network topology, uplink slot 1 (100s)
	Uplink1Ifname          string `parquet:"uplink1_ifname,zstd"`
	Uplink1NicDriver       string `parquet:"uplink1_nic_driver,zstd"`
	Uplink1NicModel        string `parquet:"uplink1_nic_model,zstd"`
	Uplink1NicPciVendor    uint32 `parquet:"uplink1_nic_pci_vendor,snappy"`
	Uplink1NicPciDevice    uint32 `parquet:"uplink1_nic_pci_device,snappy"`
	Uplink1NicBusInfo      string `parquet:"uplink1_nic_bus_info,zstd"`
	Uplink1NicSpeedMbps    uint32 `parquet:"uplink1_nic_speed_mbps,snappy"`
	Uplink1NicFwVersion    string `parquet:"uplink1_nic_fw_version,zstd"`
	Uplink1LldpChassisName string `parquet:"uplink1_lldp_chassis_name,zstd"`
	Uplink1LldpChassisId   string `parquet:"uplink1_lldp_chassis_id,zstd"`
	Uplink1LldpMgmtIp      string `parquet:"uplink1_lldp_mgmt_ip,zstd"`
	Uplink1LldpPortId      string `parquet:"uplink1_lldp_port_id,zstd"`
	Uplink1LldpPortDescr   string `parquet:"uplink1_lldp_port_descr,zstd"`

	// ---- metadata: host network topology, uplink slot 2 (200s)
	Uplink2Ifname          string `parquet:"uplink2_ifname,zstd"`
	Uplink2NicDriver       string `parquet:"uplink2_nic_driver,zstd"`
	Uplink2NicModel        string `parquet:"uplink2_nic_model,zstd"`
	Uplink2NicPciVendor    uint32 `parquet:"uplink2_nic_pci_vendor,snappy"`
	Uplink2NicPciDevice    uint32 `parquet:"uplink2_nic_pci_device,snappy"`
	Uplink2NicBusInfo      string `parquet:"uplink2_nic_bus_info,zstd"`
	Uplink2NicSpeedMbps    uint32 `parquet:"uplink2_nic_speed_mbps,snappy"`
	Uplink2NicFwVersion    string `parquet:"uplink2_nic_fw_version,zstd"`
	Uplink2LldpChassisName string `parquet:"uplink2_lldp_chassis_name,zstd"`
	Uplink2LldpChassisId   string `parquet:"uplink2_lldp_chassis_id,zstd"`
	Uplink2LldpMgmtIp      string `parquet:"uplink2_lldp_mgmt_ip,zstd"`
	Uplink2LldpPortId      string `parquet:"uplink2_lldp_port_id,zstd"`
	Uplink2LldpPortDescr   string `parquet:"uplink2_lldp_port_descr,zstd"`

	// ---- enrichment: daemon-computed (300-399)
	EnrichSocketInterfaceName     string `parquet:"enrich_socket_interface_name,zstd"`
	EnrichSocketDestLocality      int32  `parquet:"enrich_socket_dest_locality,snappy"`
	EnrichSocketDestEgressIfindex uint32 `parquet:"enrich_socket_dest_egress_ifindex,snappy"`
	EnrichSocketDestEgressIfname  string `parquet:"enrich_socket_dest_egress_ifname,zstd"`
	EnrichSocketDestAsn           uint64 `parquet:"enrich_socket_dest_asn,snappy"`
	EnrichSocketDestNextHopAsn    uint64 `parquet:"enrich_socket_dest_next_hop_asn,snappy"`
	EnrichSocketDestNetworkOwner  string `parquet:"enrich_socket_dest_network_owner,zstd"`

	// ---- payload: struct inet_diag_msg (1000s)
	InetDiagMsgFamily                uint32 `parquet:"inet_diag_msg_family,snappy"`
	InetDiagMsgState                 uint32 `parquet:"inet_diag_msg_state,snappy"`
	InetDiagMsgTimer                 uint32 `parquet:"inet_diag_msg_timer,snappy"`
	InetDiagMsgRetrans               uint32 `parquet:"inet_diag_msg_retrans,snappy"`
	InetDiagMsgSocketSourcePort      uint32 `parquet:"inet_diag_msg_socket_source_port,snappy"`
	InetDiagMsgSocketDestinationPort uint32 `parquet:"inet_diag_msg_socket_destination_port,snappy"`
	InetDiagMsgSocketSource          []byte `parquet:"inet_diag_msg_socket_source,zstd"`
	InetDiagMsgSocketDestination     []byte `parquet:"inet_diag_msg_socket_destination,zstd"`
	InetDiagMsgSocketInterface       uint32 `parquet:"inet_diag_msg_socket_interface,snappy"`
	InetDiagMsgSocketCookie          uint64 `parquet:"inet_diag_msg_socket_cookie,snappy"`
	InetDiagMsgExpires               uint32 `parquet:"inet_diag_msg_expires,snappy"`
	InetDiagMsgRqueue                uint32 `parquet:"inet_diag_msg_rqueue,snappy"`
	InetDiagMsgWqueue                uint32 `parquet:"inet_diag_msg_wqueue,snappy"`
	InetDiagMsgUid                   uint32 `parquet:"inet_diag_msg_uid,snappy"`
	InetDiagMsgInode                 uint32 `parquet:"inet_diag_msg_inode,snappy"`

	// ---- payload: struct inet_diag_meminfo (1100s, deprecated)
	MemInfoRmem uint32 `parquet:"mem_info_rmem,snappy"`
	MemInfoWmem uint32 `parquet:"mem_info_wmem,snappy"`
	MemInfoFmem uint32 `parquet:"mem_info_fmem,snappy"`
	MemInfoTmem uint32 `parquet:"mem_info_tmem,snappy"`

	// ---- payload: struct tcp_info (1200s)
	TcpInfoState                  uint32 `parquet:"tcp_info_state,snappy"`
	TcpInfoCaState                uint32 `parquet:"tcp_info_ca_state,snappy"`
	TcpInfoRetransmits            uint32 `parquet:"tcp_info_retransmits,snappy"`
	TcpInfoProbes                 uint32 `parquet:"tcp_info_probes,snappy"`
	TcpInfoBackoff                uint32 `parquet:"tcp_info_backoff,snappy"`
	TcpInfoOptions                uint32 `parquet:"tcp_info_options,snappy"`
	TcpInfoSndWscale              uint32 `parquet:"tcp_info_snd_wscale,snappy"`
	TcpInfoRcvWscale              uint32 `parquet:"tcp_info_rcv_wscale,snappy"`
	TcpInfoDeliveryRateAppLimited uint32 `parquet:"tcp_info_delivery_rate_app_limited,snappy"`
	TcpInfoFastopenClientFail     uint32 `parquet:"tcp_info_fastopen_client_fail,snappy"`
	TcpInfoRto                    uint32 `parquet:"tcp_info_rto,snappy"`
	TcpInfoAto                    uint32 `parquet:"tcp_info_ato,snappy"`
	TcpInfoSndMss                 uint32 `parquet:"tcp_info_snd_mss,snappy"`
	TcpInfoRcvMss                 uint32 `parquet:"tcp_info_rcv_mss,snappy"`
	TcpInfoUnacked                uint32 `parquet:"tcp_info_unacked,snappy"`
	TcpInfoSacked                 uint32 `parquet:"tcp_info_sacked,snappy"`
	TcpInfoLost                   uint32 `parquet:"tcp_info_lost,snappy"`
	TcpInfoRetrans                uint32 `parquet:"tcp_info_retrans,snappy"`
	TcpInfoFackets                uint32 `parquet:"tcp_info_fackets,snappy"`
	TcpInfoLastDataSent           uint32 `parquet:"tcp_info_last_data_sent,snappy"`
	TcpInfoLastAckSent            uint32 `parquet:"tcp_info_last_ack_sent,snappy"`
	TcpInfoLastDataRecv           uint32 `parquet:"tcp_info_last_data_recv,snappy"`
	TcpInfoLastAckRecv            uint32 `parquet:"tcp_info_last_ack_recv,snappy"`
	TcpInfoPmtu                   uint32 `parquet:"tcp_info_pmtu,snappy"`
	TcpInfoRcvSsthresh            uint32 `parquet:"tcp_info_rcv_ssthresh,snappy"`
	TcpInfoRtt                    uint32 `parquet:"tcp_info_rtt,snappy"`
	TcpInfoRttvar                 uint32 `parquet:"tcp_info_rttvar,snappy"`
	TcpInfoSndSsthresh            uint32 `parquet:"tcp_info_snd_ssthresh,snappy"`
	TcpInfoSndCwnd                uint32 `parquet:"tcp_info_snd_cwnd,snappy"`
	TcpInfoAdvmss                 uint32 `parquet:"tcp_info_advmss,snappy"`
	TcpInfoReordering             uint32 `parquet:"tcp_info_reordering,snappy"`
	TcpInfoRcvRtt                 uint32 `parquet:"tcp_info_rcv_rtt,snappy"`
	TcpInfoRcvSpace               uint32 `parquet:"tcp_info_rcv_space,snappy"`
	TcpInfoTotalRetrans           uint32 `parquet:"tcp_info_total_retrans,snappy"`
	TcpInfoPacingRate             uint64 `parquet:"tcp_info_pacing_rate,snappy"`
	TcpInfoMaxPacingRate          uint64 `parquet:"tcp_info_max_pacing_rate,snappy"`
	TcpInfoBytesAcked             uint64 `parquet:"tcp_info_bytes_acked,snappy"`
	TcpInfoBytesReceived          uint64 `parquet:"tcp_info_bytes_received,snappy"`
	TcpInfoSegsOut                uint32 `parquet:"tcp_info_segs_out,snappy"`
	TcpInfoSegsIn                 uint32 `parquet:"tcp_info_segs_in,snappy"`
	TcpInfoNotsentBytes           uint32 `parquet:"tcp_info_notsent_bytes,snappy"`
	TcpInfoMinRtt                 uint32 `parquet:"tcp_info_min_rtt,snappy"`
	TcpInfoDataSegsIn             uint32 `parquet:"tcp_info_data_segs_in,snappy"`
	TcpInfoDataSegsOut            uint32 `parquet:"tcp_info_data_segs_out,snappy"`
	TcpInfoDeliveryRate           uint64 `parquet:"tcp_info_delivery_rate,snappy"`
	TcpInfoBusyTime               uint64 `parquet:"tcp_info_busy_time,snappy"`
	TcpInfoRwndLimited            uint64 `parquet:"tcp_info_rwnd_limited,snappy"`
	TcpInfoSndbufLimited          uint64 `parquet:"tcp_info_sndbuf_limited,snappy"`
	TcpInfoDelivered              uint32 `parquet:"tcp_info_delivered,snappy"`
	TcpInfoDeliveredCe            uint32 `parquet:"tcp_info_delivered_ce,snappy"`
	TcpInfoBytesSent              uint64 `parquet:"tcp_info_bytes_sent,snappy"`
	TcpInfoBytesRetrans           uint64 `parquet:"tcp_info_bytes_retrans,snappy"`
	TcpInfoDsackDups              uint32 `parquet:"tcp_info_dsack_dups,snappy"`
	TcpInfoReordSeen              uint32 `parquet:"tcp_info_reord_seen,snappy"`
	TcpInfoRcvOoopack             uint32 `parquet:"tcp_info_rcv_ooopack,snappy"`
	TcpInfoSndWnd                 uint32 `parquet:"tcp_info_snd_wnd,snappy"`
	TcpInfoRcvWnd                 uint32 `parquet:"tcp_info_rcv_wnd,snappy"`
	TcpInfoRehash                 uint32 `parquet:"tcp_info_rehash,snappy"`
	TcpInfoTotalRto               uint32 `parquet:"tcp_info_total_rto,snappy"`
	TcpInfoTotalRtoRecoveries     uint32 `parquet:"tcp_info_total_rto_recoveries,snappy"`
	TcpInfoTotalRtoTime           uint32 `parquet:"tcp_info_total_rto_time,snappy"`

	// ---- payload: INET_DIAG_CONG (1300s)
	InetDiagCong     string `parquet:"inet_diag_cong,zstd"`
	InetDiagCongEnum int32  `parquet:"inet_diag_cong_enum,snappy"`

	// ---- payload: INET_DIAG_TOS / INET_DIAG_TCLASS (1400s)
	InetDiagTos    uint32 `parquet:"inet_diag_tos,snappy"`
	InetDiagTclass uint32 `parquet:"inet_diag_tclass,snappy"`

	// ---- payload: SK_MEMINFO_* (1500s)
	SkMemInfoRmemAlloc  uint32 `parquet:"sk_mem_info_rmem_alloc,snappy"`
	SkMemInfoRcvbuf     uint32 `parquet:"sk_mem_info_rcvbuf,snappy"`
	SkMemInfoWmemAlloc  uint32 `parquet:"sk_mem_info_wmem_alloc,snappy"`
	SkMemInfoSndbuf     uint32 `parquet:"sk_mem_info_sndbuf,snappy"`
	SkMemInfoFwdAlloc   uint32 `parquet:"sk_mem_info_fwd_alloc,snappy"`
	SkMemInfoWmemQueued uint32 `parquet:"sk_mem_info_wmem_queued,snappy"`
	SkMemInfoOptmem     uint32 `parquet:"sk_mem_info_optmem,snappy"`
	SkMemInfoBacklog    uint32 `parquet:"sk_mem_info_backlog,snappy"`
	SkMemInfoDrops      uint32 `parquet:"sk_mem_info_drops,snappy"`

	// ---- payload: INET_DIAG_SHUTDOWN (1600)
	InetDiagShutdown uint32 `parquet:"inet_diag_shutdown,snappy"`

	// ---- payload: struct tcpvegas_info (1700s)
	VegasInfoEnabled uint32 `parquet:"vegas_info_enabled,snappy"`
	VegasInfoRttcnt  uint32 `parquet:"vegas_info_rttcnt,snappy"`
	VegasInfoRtt     uint32 `parquet:"vegas_info_rtt,snappy"`
	VegasInfoMinrtt  uint32 `parquet:"vegas_info_minrtt,snappy"`

	// ---- payload: struct tcp_dctcp_info (1800s)
	DctcpInfoEnabled uint32 `parquet:"dctcp_info_enabled,snappy"`
	DctcpInfoCeState uint32 `parquet:"dctcp_info_ce_state,snappy"`
	DctcpInfoAlpha   uint32 `parquet:"dctcp_info_alpha,snappy"`
	DctcpInfoAbEcn   uint32 `parquet:"dctcp_info_ab_ecn,snappy"`
	DctcpInfoAbTot   uint32 `parquet:"dctcp_info_ab_tot,snappy"`

	// ---- payload: struct tcp_bbr_info (1900s)
	BbrInfoBwLo       uint32 `parquet:"bbr_info_bw_lo,snappy"`
	BbrInfoBwHi       uint32 `parquet:"bbr_info_bw_hi,snappy"`
	BbrInfoMinRtt     uint32 `parquet:"bbr_info_min_rtt,snappy"`
	BbrInfoPacingGain uint32 `parquet:"bbr_info_pacing_gain,snappy"`
	BbrInfoCwndGain   uint32 `parquet:"bbr_info_cwnd_gain,snappy"`

	// ---- payload: INET_DIAG_CLASS_ID / SOCKOPT / CGROUP_ID (2000s)
	InetDiagClassId  uint32 `parquet:"inet_diag_class_id,snappy"`
	InetDiagSockopt  uint32 `parquet:"inet_diag_sockopt,snappy"`
	InetDiagCgroupId uint64 `parquet:"inet_diag_cgroup_id,snappy"`
}

// The rowFromProto conversion function lives in
// destinations_s3parquet.go (where the xtcp_flat_record import already
// lives). The schema file is kept import-free so it reads as a clean
// columnar listing of the proto's surface.
