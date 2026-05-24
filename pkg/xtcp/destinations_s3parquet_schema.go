//go:build dest_s3parquet

package xtcp

// ParquetRow mirrors xtcp_flat_record.v1.XtcpFlatRecord one-to-one. Each
// proto field becomes one Parquet column, named via the `parquet:` tag
// using the proto field's snake_case name (NOT the Go field's PascalCase)
// so SQL on the Parquet files matches SQL on the ClickHouse table.
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
// to the proto, that test fails until you mirror it here.
type ParquetRow struct {
	TimestampNs float64 `parquet:"timestamp_ns,snappy"`

	Hostname string `parquet:"hostname,zstd"`

	Netns string `parquet:"netns,zstd"`
	Nsid  uint32 `parquet:"nsid,snappy"`

	Label string `parquet:"label,zstd"`
	Tag   string `parquet:"tag,zstd"`

	RecordCounter uint64 `parquet:"record_counter,snappy"`
	SocketFd      uint64 `parquet:"socket_fd,snappy"`
	NetlinkerId   uint64 `parquet:"netlinker_id,snappy"`

	InetDiagMsgFamily               uint32 `parquet:"inet_diag_msg_family,snappy"`
	InetDiagMsgState                uint32 `parquet:"inet_diag_msg_state,snappy"`
	InetDiagMsgTimer                uint32 `parquet:"inet_diag_msg_timer,snappy"`
	InetDiagMsgRetrans              uint32 `parquet:"inet_diag_msg_retrans,snappy"`
	InetDiagMsgSocketSourcePort     uint32 `parquet:"inet_diag_msg_socket_source_port,snappy"`
	InetDiagMsgSocketDestinationPort uint32 `parquet:"inet_diag_msg_socket_destination_port,snappy"`
	InetDiagMsgSocketSource         []byte `parquet:"inet_diag_msg_socket_source,zstd"`
	InetDiagMsgSocketDestination    []byte `parquet:"inet_diag_msg_socket_destination,zstd"`
	InetDiagMsgSocketInterface      uint32 `parquet:"inet_diag_msg_socket_interface,snappy"`
	InetDiagMsgSocketCookie         uint64 `parquet:"inet_diag_msg_socket_cookie,snappy"`
	InetDiagMsgSocketDestAsn        uint64 `parquet:"inet_diag_msg_socket_dest_asn,snappy"`
	InetDiagMsgSocketNextHopAsn     uint64 `parquet:"inet_diag_msg_socket_next_hop_asn,snappy"`
	InetDiagMsgExpires              uint32 `parquet:"inet_diag_msg_expires,snappy"`
	InetDiagMsgRqueue               uint32 `parquet:"inet_diag_msg_rqueue,snappy"`
	InetDiagMsgWqueue               uint32 `parquet:"inet_diag_msg_wqueue,snappy"`
	InetDiagMsgUid                  uint32 `parquet:"inet_diag_msg_uid,snappy"`
	InetDiagMsgInode                uint32 `parquet:"inet_diag_msg_inode,snappy"`

	MemInfoRmem uint32 `parquet:"mem_info_rmem,snappy"`
	MemInfoWmem uint32 `parquet:"mem_info_wmem,snappy"`
	MemInfoFmem uint32 `parquet:"mem_info_fmem,snappy"`
	MemInfoTmem uint32 `parquet:"mem_info_tmem,snappy"`

	TcpInfoState                    uint32 `parquet:"tcp_info_state,snappy"`
	TcpInfoCaState                  uint32 `parquet:"tcp_info_ca_state,snappy"`
	TcpInfoRetransmits              uint32 `parquet:"tcp_info_retransmits,snappy"`
	TcpInfoProbes                   uint32 `parquet:"tcp_info_probes,snappy"`
	TcpInfoBackoff                  uint32 `parquet:"tcp_info_backoff,snappy"`
	TcpInfoOptions                  uint32 `parquet:"tcp_info_options,snappy"`
	TcpInfoSendScale                uint32 `parquet:"tcp_info_send_scale,snappy"`
	TcpInfoRcvScale                 uint32 `parquet:"tcp_info_rcv_scale,snappy"`
	TcpInfoDeliveryRateAppLimited   uint32 `parquet:"tcp_info_delivery_rate_app_limited,snappy"`
	TcpInfoFastOpenClientFailed     uint32 `parquet:"tcp_info_fast_open_client_failed,snappy"`
	TcpInfoRto                      uint32 `parquet:"tcp_info_rto,snappy"`
	TcpInfoAto                      uint32 `parquet:"tcp_info_ato,snappy"`
	TcpInfoSndMss                   uint32 `parquet:"tcp_info_snd_mss,snappy"`
	TcpInfoRcvMss                   uint32 `parquet:"tcp_info_rcv_mss,snappy"`
	TcpInfoUnacked                  uint32 `parquet:"tcp_info_unacked,snappy"`
	TcpInfoSacked                   uint32 `parquet:"tcp_info_sacked,snappy"`
	TcpInfoLost                     uint32 `parquet:"tcp_info_lost,snappy"`
	TcpInfoRetrans                  uint32 `parquet:"tcp_info_retrans,snappy"`
	TcpInfoFackets                  uint32 `parquet:"tcp_info_fackets,snappy"`
	TcpInfoLastDataSent             uint32 `parquet:"tcp_info_last_data_sent,snappy"`
	TcpInfoLastAckSent              uint32 `parquet:"tcp_info_last_ack_sent,snappy"`
	TcpInfoLastDataRecv             uint32 `parquet:"tcp_info_last_data_recv,snappy"`
	TcpInfoLastAckRecv              uint32 `parquet:"tcp_info_last_ack_recv,snappy"`
	TcpInfoPmtu                     uint32 `parquet:"tcp_info_pmtu,snappy"`
	TcpInfoRcvSsthresh              uint32 `parquet:"tcp_info_rcv_ssthresh,snappy"`
	TcpInfoRtt                      uint32 `parquet:"tcp_info_rtt,snappy"`
	TcpInfoRttVar                   uint32 `parquet:"tcp_info_rtt_var,snappy"`
	TcpInfoSndSsthresh              uint32 `parquet:"tcp_info_snd_ssthresh,snappy"`
	TcpInfoSndCwnd                  uint32 `parquet:"tcp_info_snd_cwnd,snappy"`
	TcpInfoAdvMss                   uint32 `parquet:"tcp_info_adv_mss,snappy"`
	TcpInfoReordering               uint32 `parquet:"tcp_info_reordering,snappy"`
	TcpInfoRcvRtt                   uint32 `parquet:"tcp_info_rcv_rtt,snappy"`
	TcpInfoRcvSpace                 uint32 `parquet:"tcp_info_rcv_space,snappy"`
	TcpInfoTotalRetrans             uint32 `parquet:"tcp_info_total_retrans,snappy"`
	TcpInfoPacingRate               uint64 `parquet:"tcp_info_pacing_rate,snappy"`
	TcpInfoMaxPacingRate            uint64 `parquet:"tcp_info_max_pacing_rate,snappy"`
	TcpInfoBytesAcked               uint64 `parquet:"tcp_info_bytes_acked,snappy"`
	TcpInfoBytesReceived            uint64 `parquet:"tcp_info_bytes_received,snappy"`
	TcpInfoSegsOut                  uint32 `parquet:"tcp_info_segs_out,snappy"`
	TcpInfoSegsIn                   uint32 `parquet:"tcp_info_segs_in,snappy"`
	TcpInfoNotSentBytes             uint32 `parquet:"tcp_info_not_sent_bytes,snappy"`
	TcpInfoMinRtt                   uint32 `parquet:"tcp_info_min_rtt,snappy"`
	TcpInfoDataSegsIn               uint32 `parquet:"tcp_info_data_segs_in,snappy"`
	TcpInfoDataSegsOut              uint32 `parquet:"tcp_info_data_segs_out,snappy"`
	TcpInfoDeliveryRate             uint64 `parquet:"tcp_info_delivery_rate,snappy"`
	TcpInfoBusyTime                 uint64 `parquet:"tcp_info_busy_time,snappy"`
	TcpInfoRwndLimited              uint64 `parquet:"tcp_info_rwnd_limited,snappy"`
	TcpInfoSndbufLimited            uint64 `parquet:"tcp_info_sndbuf_limited,snappy"`
	TcpInfoDelivered                uint32 `parquet:"tcp_info_delivered,snappy"`
	TcpInfoDeliveredCe              uint32 `parquet:"tcp_info_delivered_ce,snappy"`
	TcpInfoBytesSent                uint64 `parquet:"tcp_info_bytes_sent,snappy"`
	TcpInfoBytesRetrans             uint64 `parquet:"tcp_info_bytes_retrans,snappy"`
	TcpInfoDsackDups                uint32 `parquet:"tcp_info_dsack_dups,snappy"`
	TcpInfoReordSeen                uint32 `parquet:"tcp_info_reord_seen,snappy"`
	TcpInfoRcvOoopack               uint32 `parquet:"tcp_info_rcv_ooopack,snappy"`
	TcpInfoSndWnd                   uint32 `parquet:"tcp_info_snd_wnd,snappy"`
	TcpInfoRcvWnd                   uint32 `parquet:"tcp_info_rcv_wnd,snappy"`
	TcpInfoRehash                   uint32 `parquet:"tcp_info_rehash,snappy"`
	TcpInfoTotalRto                 uint32 `parquet:"tcp_info_total_rto,snappy"`
	TcpInfoTotalRtoRecoveries       uint32 `parquet:"tcp_info_total_rto_recoveries,snappy"`
	TcpInfoTotalRtoTime             uint32 `parquet:"tcp_info_total_rto_time,snappy"`

	CongestionAlgorithmString string `parquet:"congestion_algorithm_string,zstd"`
	CongestionAlgorithmEnum   int32  `parquet:"congestion_algorithm_enum,snappy"`

	TypeOfService uint32 `parquet:"type_of_service,snappy"`
	TrafficClass  uint32 `parquet:"traffic_class,snappy"`

	SkMemInfoRmemAlloc  uint32 `parquet:"sk_mem_info_rmem_alloc,snappy"`
	SkMemInfoRcvBuf     uint32 `parquet:"sk_mem_info_rcv_buf,snappy"`
	SkMemInfoWmemAlloc  uint32 `parquet:"sk_mem_info_wmem_alloc,snappy"`
	SkMemInfoSndBuf     uint32 `parquet:"sk_mem_info_snd_buf,snappy"`
	SkMemInfoFwdAlloc   uint32 `parquet:"sk_mem_info_fwd_alloc,snappy"`
	SkMemInfoWmemQueued uint32 `parquet:"sk_mem_info_wmem_queued,snappy"`
	SkMemInfoOptmem     uint32 `parquet:"sk_mem_info_optmem,snappy"`
	SkMemInfoBacklog    uint32 `parquet:"sk_mem_info_backlog,snappy"`
	SkMemInfoDrops      uint32 `parquet:"sk_mem_info_drops,snappy"`

	ShutdownState uint32 `parquet:"shutdown_state,snappy"`

	VegasInfoEnabled uint32 `parquet:"vegas_info_enabled,snappy"`
	VegasInfoRttCnt  uint32 `parquet:"vegas_info_rtt_cnt,snappy"`
	VegasInfoRtt     uint32 `parquet:"vegas_info_rtt,snappy"`
	VegasInfoMinRtt  uint32 `parquet:"vegas_info_min_rtt,snappy"`

	DctcpInfoEnabled uint32 `parquet:"dctcp_info_enabled,snappy"`
	DctcpInfoCeState uint32 `parquet:"dctcp_info_ce_state,snappy"`
	DctcpInfoAlpha   uint32 `parquet:"dctcp_info_alpha,snappy"`
	DctcpInfoAbEcn   uint32 `parquet:"dctcp_info_ab_ecn,snappy"`
	DctcpInfoAbTot   uint32 `parquet:"dctcp_info_ab_tot,snappy"`

	BbrInfoBwLo       uint32 `parquet:"bbr_info_bw_lo,snappy"`
	BbrInfoBwHi       uint32 `parquet:"bbr_info_bw_hi,snappy"`
	BbrInfoMinRtt     uint32 `parquet:"bbr_info_min_rtt,snappy"`
	BbrInfoPacingGain uint32 `parquet:"bbr_info_pacing_gain,snappy"`
	BbrInfoCwndGain   uint32 `parquet:"bbr_info_cwnd_gain,snappy"`

	ClassId uint32 `parquet:"class_id,snappy"`
	SockOpt uint32 `parquet:"sock_opt,snappy"`
	CGroup  uint64 `parquet:"c_group,snappy"`
}

// The rowFromProto conversion function lives in
// destinations_s3parquet.go (where the xtcp_flat_record import already
// lives). The schema file is kept import-free so it reads as a clean
// columnar listing of the proto's surface.
