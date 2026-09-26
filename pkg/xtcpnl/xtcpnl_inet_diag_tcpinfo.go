package xtcpnl

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// wireshark filter
// netlink-sock_diag.inet_sport == 58501

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h#L134
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L133
// INET_DIAG_NONE 0
// INET_DIAG_MEMINFO 1
// INET_DIAG_INFO 2
// INET_DIAG_VEGASINFO 3
// INET_DIAG_CONG 4
// INET_DIAG_TOS 5
// INET_DIAG_TCLASS 6
// INET_DIAG_SKMEMINFO 7
// INET_DIAG_SHUTDOWN 8
// INET_DIAG_DCTCPINFO 9
// INET_DIAG_PROTOCOL 10
// INET_DIAG_SKV6ONLY 11
// INET_DIAG_LOCALS 12
// INET_DIAG_PEERS 13
// INET_DIAG_PAD 14
// INET_DIAG_MARK 15
// INET_DIAG_BBRINFO 16
// INET_DIAG_CLASS_ID 17
// INET_DIAG_MD5SIG 18
// INET_DIAG_ULP_INFO 19
// INET_DIAG_SK_BPF_STORAGES 20
// INET_DIAG_CGROUP_ID 21
// INET_DIAG_SOCKOPT 22
// 23
// __INET_DIAG_MAX 24

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/tcp.h#L222
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/tcp.h#L214
// struct tcp_info {
// 	__u8	_state;
// 	__u8	_ca_state;
// 	__u8	_retransmits;
// 	__u8	_probes;
// 	__u8	_backoff;
// 	__u8	_options;
// 	__u8	_snd_wscale : 4, _rcv_wscale : 4;
// 	__u8	_delivery_rate_app_limited:1, _fastopen_client_fail:2;

// 	__u32	_rto;
// 	__u32	_ato;
// 	__u32	_snd_mss;
// 	__u32	_rcv_mss;

// 	__u32	_unacked;
// 	__u32	_sacked;
// 	__u32	_lost;
// 	__u32	_retrans;
// 	__u32	_fackets;

// 	/* Times. */
// 	__u32	_last_data_sent;
// 	__u32	_last_ack_sent;     /* Not remembered, sorry. */
// 	__u32	_last_data_recv;
// 	__u32	_last_ack_recv;

// 	/* Metrics. */
// 	__u32	_pmtu;
// 	__u32	_rcv_ssthresh;
// 	__u32	_rtt;
// 	__u32	_rttvar;
// 	__u32	_snd_ssthresh;
// 	__u32	_snd_cwnd;
// 	__u32	_advmss;
// 	__u32	_reordering;

// 	__u32	_rcv_rtt;
// 	__u32	_rcv_space;

// 	__u32	_total_retrans;

// 	__u64	_pacing_rate;
// 	__u64	_max_pacing_rate;
// 	__u64	_bytes_acked;    /* RFC4898 tcpEStatsAppHCThruOctetsAcked */
// 	__u64	_bytes_received; /* RFC4898 tcpEStatsAppHCThruOctetsReceived */
// 	__u32	_segs_out;	     /* RFC4898 tcpEStatsPerfSegsOut */
// 	__u32	_segs_in;	     /* RFC4898 tcpEStatsPerfSegsIn */

// 	__u32	_notsent_bytes;
// 	__u32	_min_rtt;
// 	__u32	_data_segs_in;	/* RFC4898 tcpEStatsDataSegsIn */
// 	__u32	_data_segs_out;	/* RFC4898 tcpEStatsDataSegsOut */

// 	__u64   _delivery_rate;

// 	__u64	_busy_time;      /* Time (usec) busy sending data */
// 	__u64	_rwnd_limited;   /* Time (usec) limited by receive window */
// 	__u64	_sndbuf_limited; /* Time (usec) limited by send buffer */

// 	__u32	_delivered;
// 	__u32	_delivered_ce;

// 	__u64	_bytes_sent;     /* RFC4898 tcpEStatsPerfHCDataOctetsOut */
// 	__u64	_bytes_retrans;  /* RFC4898 tcpEStatsPerfOctetsRetrans */
// 	__u32	_dsack_dups;     /* RFC4898 tcpEStatsStackDSACKDups */
// 	__u32	_reord_seen;     /* reordering events seen */

// __u32	tcpi_rcv_ooopack;    /* Out-of-order packets received */

// __u32	tcpi_snd_wnd;	     /* peer's advertised receive window after
// 				  * scaling (bytes)
// 				  */
// __u32	tcpi_rcv_wnd;	     /* local advertised receive window after
// 				  * scaling (bytes)
// 				  */

// __u32   tcpi_rehash;         /* PLB or timeout triggered rehash attempts */

// __u16	tcpi_total_rto;	/* Total number of RTO timeouts, including
// 			 * SYN/SYN-ACK and recurring timeouts.
// 			 */
// __u16	tcpi_total_rto_recoveries;	/* Total number of RTO
// 					 * recoveries, including any
// 					 * unfinished recovery.
// 					 */
// __u32	tcpi_total_rto_time;	/* Total time spent in RTO recoveries
// 				 * in milliseconds, including any
// 				 * unfinished recovery.
// 				 */
// };

type TCPInfo TCPInfo7_0_3

// TCPInfo7_0_3 mirrors the kernel's `struct tcp_info` for Linux 7.0.3 —
// TCPInfo6_10_3 plus the Accurate ECN (AccECN) trailer the kernel appended
// after tcpi_total_rto_time, growing the wire struct from 248 to 280 bytes.
//
// ~/Downloads/linux/include/uapi/linux/tcp.h:337-347
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/tcp.h#L337
//
//	__u32	tcpi_received_ce;        /* # of CE marked segments received */
//	__u32	tcpi_delivered_e1_bytes; /* Accurate ECN byte counters */
//	__u32	tcpi_delivered_e0_bytes;
//	__u32	tcpi_delivered_ce_bytes;
//	__u32	tcpi_received_e1_bytes;
//	__u32	tcpi_received_e0_bytes;
//	__u32	tcpi_received_ce_bytes;
//	__u32	tcpi_ecn_mode:2,
//		tcpi_accecn_opt_seen:2,
//		tcpi_accecn_fail_mode:4,
//		tcpi_options2:24;
//
// The corpus already carried these bytes before the decoder read them: every
// testdata/7_0_3/*_info fixture is a 284-byte INET_DIAG_INFO attribute
// (4-byte nla header + 280-byte payload), and in two of the three,
// tcpi_received_e0_bytes equals tcpi_bytes_received exactly — 11648 and
// 98264 — which pins the trailer offsets independently of the header.
// DeserializeTCPInfo used to stop at 248 and silently discard the rest.
type TCPInfo7_0_3 struct {
	State                  uint8 // bytes:1 [0:1]
	CaState                uint8 // bytes:1 [1:2]
	Retransmits            uint8 // bytes:1 [2:3]
	Probes                 uint8 // bytes:1 [3:4]
	Backoff                uint8 // bytes:1 [4:5]
	Options                uint8 // bytes:1 [5:6]
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32 // bytes:4 [8:12]
	Ato    uint32 // bytes:4 [12:16]
	SndMss uint32 // bytes:4 [16:20]
	RcvMss uint32 // bytes:4 [20:24]

	Unacked uint32 // bytes:4 [24:28]
	Sacked  uint32 // bytes:4 [28:32]
	Lost    uint32 // bytes:4 [32:36]
	Retrans uint32 // bytes:4 [36:40]
	Fackets uint32 // bytes:4 [40:44] // legacy, "has no effect anymore"

	LastDataSent uint32 // bytes:4 [44:48]
	LastAckSent  uint32 // bytes:4 [48:52]
	LastDataRecv uint32 // bytes:4 [52:56]
	LastAckRecv  uint32 // bytes:4 [56:60]

	Pmtu        uint32 // bytes:4 [60:64]
	RcvSsthresh uint32 // bytes:4 [64:68]
	Rtt         uint32 // bytes:4 [68:72]
	Rttvar      uint32 // bytes:4 [72:76]
	SndSsthresh uint32 // bytes:4 [76:80]
	SndCwnd     uint32 // bytes:4 [80:84]
	AdvMss      uint32 // bytes:4 [84:88]
	Reordering  uint32 // bytes:4 [88:92]

	RcvRtt   uint32 // bytes:4 [92:96]
	RcvSpace uint32 // bytes:4 [96:100]

	TotalRetrans uint32 // bytes:4 [100:104]

	PacingRate    uint64 // bytes:8 [104:112]
	MaxPacingRate uint64 // bytes:8 [112:120]
	BytesAcked    uint64 // bytes:8 [120:128] // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // bytes:8 [128:136] // RFC4898 tcpEStatsAppHCThruOctetsReceived

	SegsOut uint32 // bytes:4 [136:140] // RFC4898 tcpEStatsPerfSegsOut
	SegsIn  uint32 // bytes:4 [140:144] // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32 // bytes:4 [144:148]
	MinRtt       uint32 // bytes:4 [148:152]
	DataSegsIn   uint32 // bytes:4 [152:156] // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // bytes:4 [156:160] // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64 // bytes:8 [160:168]

	BusyTime      uint64 // bytes:8 [168:176] // Time (usec) busy sending data
	RwndLimited   uint64 // bytes:8 [176:184] // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32 // bytes:4 [192:196]
	DeliveredCe uint32 // bytes:4 [196:200]

	BytesSent    uint64 // bytes:8 [200:208] // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // bytes:8 [208:216] // RFC4898 tcpEStatsPerfOctetsRetrans

	DsackDups uint32 // bytes:4 [216:220] // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen uint32 // bytes:4 [220:224] // reordering events seen

	RcvOoopack uint32 // bytes:4 [224:228] // Out-of-order packets received

	SndWnd uint32 // bytes:4 [228:232] // peer's advertised receive window after scaling (bytes)

	// 6.5+ below
	RcvWnd uint32 // bytes:4 [232:236] // local advertised receive window after scaling (bytes)
	Rehash uint32 // bytes:4 [236:240] // PLB or timeout triggered rehash attempts

	TotalRTO           uint16 // bytes:2 [240:242] // Total number of RTO timeouts, including SYN/SYN-ACK and recurring timeouts
	TotalRTORecoveries uint16 // bytes:2 [242:244] // Total number of RTO recoveries, including any unfinished recovery
	TotalRTOTime       uint32 // bytes:4 [244:248] // Total time spent in RTO recoveries in milliseconds, including any unfinished recovery

	// 6.10 kernel tcp_info ends here. AccECN trailer below.

	ReceivedCe       uint32 // bytes:4 [248:252] // tcpi_received_ce — CE marked segments received
	DeliveredE1Bytes uint32 // bytes:4 [252:256] // tcpi_delivered_e1_bytes
	DeliveredE0Bytes uint32 // bytes:4 [256:260] // tcpi_delivered_e0_bytes
	DeliveredCeBytes uint32 // bytes:4 [260:264] // tcpi_delivered_ce_bytes
	ReceivedE1Bytes  uint32 // bytes:4 [264:268] // tcpi_received_e1_bytes
	ReceivedE0Bytes  uint32 // bytes:4 [268:272] // tcpi_received_e0_bytes
	ReceivedCeBytes  uint32 // bytes:4 [272:276] // tcpi_received_ce_bytes

	// One kernel __u32 at [276:280], split into its four bitfields the same
	// way SndWscale/RcvWscale split byte [6]. Little-endian bit order:
	// ecn_mode is bits 0-1, accecn_opt_seen bits 2-3, accecn_fail_mode bits
	// 4-7, options2 bits 8-31.
	EcnMode        uint8  // 2 bits from [276:280], bits 0-1   // tcpi_ecn_mode
	AccecnOptSeen  uint8  // 2 bits from [276:280], bits 2-3   // tcpi_accecn_opt_seen
	AccecnFailMode uint8  // 4 bits from [276:280], bits 4-7   // tcpi_accecn_fail_mode
	Options2       uint32 // 24 bits from [276:280], bits 8-31 // tcpi_options2
}

// TCPInfo6_10_3 mirrors the kernel's `struct tcp_info` for Linux 6.10.3
// (tcp_info_for kernel 6.5+).
//
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/tcp.h#L222
type TCPInfo6_10_3 struct {
	State                  uint8 // bytes:1 [0:1]
	CaState                uint8 // bytes:1 [1:2]
	Retransmits            uint8 // bytes:1 [2:3]
	Probes                 uint8 // bytes:1 [3:4]
	Backoff                uint8 // bytes:1 [4:5]
	Options                uint8 // bytes:1 [5:6]
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32 // bytes:4 [8:12]
	Ato    uint32 // bytes:4 [12:16]
	SndMss uint32 // bytes:4 [16:20]
	RcvMss uint32 // bytes:4 [20:24]

	Unacked uint32 // bytes:4 [24:28]
	Sacked  uint32 // bytes:4 [28:32]
	Lost    uint32 // bytes:4 [32:36]
	Retrans uint32 // bytes:4 [36:40]
	Fackets uint32 // bytes:4 [40:44] // sysctl says "This is a legacy option, it has no effect anymore."
	// https://www.kernel.org/doc/html/latest/networking/ip-sysctl.html

	// 	Times
	LastDataSent uint32 // bytes:4 [44:48]
	LastAckSent  uint32 // bytes:4 [48:52]
	LastDataRecv uint32 // bytes:4 [52:56]
	LastAckRecv  uint32 // bytes:4 [56:60]

	// 	Metrics
	Pmtu        uint32 // bytes:4 [60:64]
	RcvSsthresh uint32 // bytes:4 [64:68]
	Rtt         uint32 // bytes:4 [68:72]
	Rttvar      uint32 // bytes:4 [72:76]
	SndSsthresh uint32 // bytes:4 [76:80]
	SndCwnd     uint32 // bytes:4 [80:84]
	AdvMss      uint32 // bytes:4 [84:88]
	Reordering  uint32 // bytes:4 [88:92]

	RcvRtt   uint32 // bytes:4 [92:96]
	RcvSpace uint32 // bytes:4 [96:100]

	TotalRetrans uint32 // bytes:4 [100:104]

	PacingRate    uint64 // bytes:8 [104:112]
	MaxPacingRate uint64 // bytes:8 [112:120]
	BytesAcked    uint64 // bytes:8 [120:128] // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // bytes:8 [128:136] // RFC4898 tcpEStatsAppHCThruOctetsReceived

	SegsOut uint32 // bytes:4 [136:140] // RFC4898 tcpEStatsPerfSegsOut
	SegsIn  uint32 // bytes:4 [140:144] // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32 // bytes:4 [144:148]
	MinRtt       uint32 // bytes:4 [148:152]
	DataSegsIn   uint32 // bytes:4 [152:156] // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // bytes:4 [156:160] // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64 // bytes:8 [160:168]

	BusyTime      uint64 // bytes:8 [168:176] // Time (usec) busy sending data
	RwndLimited   uint64 // bytes:8 [176:184] // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32 // bytes:4 [192:196]
	DeliveredCe uint32 // bytes:4 [196:200]

	BytesSent    uint64 // bytes:8 [200:208] // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // bytes:8 [208:216] // RFC4898 tcpEStatsPerfOctetsRetrans

	DsackDups uint32 // bytes:4 [216:220] // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen uint32 // bytes:4 [220:224] // reordering events seen

	RcvOoopack uint32 // bytes:4 [224:228] // Out-of-order packets received

	SndWnd uint32 // bytes:4 [228:232] // peer's advertised receive window after scaling (bytes)

	// 6.5+ below
	RcvWnd uint32 // bytes:4 [232:236] // local advertised receive window after scaling (bytes)
	Rehash uint32 // bytes:4 [236:240] // PLB or timeout triggered rehash attempts

	TotalRTO           uint16 // bytes:2 [240:242] // Total number of RTO timeouts, including SYN/SYN-ACK and recurring timeouts
	TotalRTORecoveries uint16 // bytes:2 [242:244] // Total number of RTO recoveries, including any unfinished recovery
	TotalRTOTime       uint32 // bytes:4 [244:248] // Total time spent in RTO recoveries in milliseconds, including any unfinished recovery
}

// TCPInfo6_8_12 mirrors the kernel's `struct tcp_info` for Linux 6.8.12
// (tcp_info_for kernel 6.5+).
//
// https://github.com/torvalds/linux/blob/v6.8-rc7/include/uapi/linux/tcp.h#L220
type TCPInfo6_8_12 struct {
	State                  uint8 // bytes:1 [0:1]
	CaState                uint8 // bytes:1 [1:2]
	Retransmits            uint8 // bytes:1 [2:3]
	Probes                 uint8 // bytes:1 [3:4]
	Backoff                uint8 // bytes:1 [4:5]
	Options                uint8 // bytes:1 [5:6]
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32 // bytes:4 [8:12]
	Ato    uint32 // bytes:4 [12:16]
	SndMss uint32 // bytes:4 [16:20]
	RcvMss uint32 // bytes:4 [20:24]

	Unacked uint32 // bytes:4 [24:28]
	Sacked  uint32 // bytes:4 [28:32]
	Lost    uint32 // bytes:4 [32:36]
	Retrans uint32 // bytes:4 [36:40]
	Fackets uint32 // bytes:4 [40:44]

	// 	Times
	LastDataSent uint32 // bytes:4 [44:48]
	LastAckSent  uint32 // bytes:4 [48:52]
	LastDataRecv uint32 // bytes:4 [52:56]
	LastAckRecv  uint32 // bytes:4 [56:60]

	// 	Metrics
	Pmtu        uint32 // bytes:4 [60:64]
	RcvSsthresh uint32 // bytes:4 [64:68]
	Rtt         uint32 // bytes:4 [68:72]
	Rttvar      uint32 // bytes:4 [72:76]
	SndSsthresh uint32 // bytes:4 [76:80]
	SndCwnd     uint32 // bytes:4 [80:84]
	AdvMss      uint32 // bytes:4 [84:88]
	Reordering  uint32 // bytes:4 [88:92]

	RcvRtt   uint32 // bytes:4 [92:96]
	RcvSpace uint32 // bytes:4 [96:100]

	TotalRetrans uint32 // bytes:4 [100:104]

	PacingRate    uint64 // bytes:8 [104:112]
	MaxPacingRate uint64 // bytes:8 [112:120]
	BytesAcked    uint64 // bytes:8 [120:128] // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // bytes:8 [128:136] // RFC4898 tcpEStatsAppHCThruOctetsReceived

	SegsOut uint32 // bytes:4 [136:140] // RFC4898 tcpEStatsPerfSegsOut
	SegsIn  uint32 // bytes:4 [140:144] // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32 // bytes:4 [144:148]
	MinRtt       uint32 // bytes:4 [148:152]
	DataSegsIn   uint32 // bytes:4 [152:156] // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // bytes:4 [156:160] // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64 // bytes:8 [160:168]

	BusyTime      uint64 // bytes:8 [168:176] // Time (usec) busy sending data
	RwndLimited   uint64 // bytes:8 [176:184] // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32 // bytes:4 [192:196]
	DeliveredCe uint32 // bytes:4 [196:200]

	BytesSent    uint64 // bytes:8 [200:208] // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // bytes:8 [208:216] // RFC4898 tcpEStatsPerfOctetsRetrans

	DsackDups uint32 // bytes:4 [216:220] // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen uint32 // bytes:4 [220:224] // reordering events seen

	RcvOoopack uint32 // bytes:4 [224:228] // Out-of-order packets received

	SndWnd uint32 // bytes:4 [228:232] // peer's advertised receive window after scaling (bytes)

	// 6.5+ below
	RcvWnd uint32 // bytes:4 [232:236] // local advertised receive window after scaling (bytes)
	Rehash uint32 // bytes:4 [236:240] // PLB or timeout triggered rehash attempts

	TotalRTO           uint16 // bytes:2 [240:242] // Total number of RTO timeouts, including SYN/SYN-ACK and recurring timeouts
	TotalRTORecoveries uint16 // bytes:2 [242:244] // Total number of RTO recoveries, including any unfinished recovery
	TotalRTOTime       uint32 // bytes:4 [244:248] // Total time spent in RTO recoveries in milliseconds, including any unfinished recovery
}

// TCPInfo6_6_44 mirrors the kernel's `struct tcp_info` for Linux 6.6.44
// (tcp_info_for kernel 6.6+).
//
// https://github.com/torvalds/linux/blob/v6.6-rc7/include/uapi/linux/tcp.h#L214
type TCPInfo6_6_44 struct {
	State                  uint8 // bytes:1 [0:1]
	CaState                uint8 // bytes:1 [1:2]
	Retransmits            uint8 // bytes:1 [2:3]
	Probes                 uint8 // bytes:1 [3:4]
	Backoff                uint8 // bytes:1 [4:5]
	Options                uint8 // bytes:1 [5:6]
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32 // bytes:4 [8:12]
	Ato    uint32 // bytes:4 [12:16]
	SndMss uint32 // bytes:4 [16:20]
	RcvMss uint32 // bytes:4 [20:24]

	Unacked uint32 // bytes:4 [24:28]
	Sacked  uint32 // bytes:4 [28:32]
	Lost    uint32 // bytes:4 [32:36]
	Retrans uint32 // bytes:4 [36:40]
	Fackets uint32 // bytes:4 [40:44]

	// 	Times
	LastDataSent uint32 // bytes:4 [44:48]
	LastAckSent  uint32 // bytes:4 [48:52]
	LastDataRecv uint32 // bytes:4 [52:56]
	LastAckRecv  uint32 // bytes:4 [56:60]

	// 	Metrics
	Pmtu        uint32 // bytes:4 [60:64]
	RcvSsthresh uint32 // bytes:4 [64:68]
	Rtt         uint32 // bytes:4 [68:72]
	Rttvar      uint32 // bytes:4 [72:76]
	SndSsthresh uint32 // bytes:4 [76:80]
	SndCwnd     uint32 // bytes:4 [80:84]
	AdvMss      uint32 // bytes:4 [84:88]
	Reordering  uint32 // bytes:4 [88:92]

	RcvRtt   uint32 // bytes:4 [92:96]
	RcvSpace uint32 // bytes:4 [96:100]

	TotalRetrans uint32 // bytes:4 [100:104]

	PacingRate    uint64 // bytes:8 [104:112]
	MaxPacingRate uint64 // bytes:8 [112:120]
	BytesAcked    uint64 // bytes:8 [120:128] // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // bytes:8 [128:136] // RFC4898 tcpEStatsAppHCThruOctetsReceived

	SegsOut uint32 // bytes:4 [136:140] // RFC4898 tcpEStatsPerfSegsOut
	SegsIn  uint32 // bytes:4 [140:144] // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32 // bytes:4 [144:148]
	MinRtt       uint32 // bytes:4 [148:152]
	DataSegsIn   uint32 // bytes:4 [152:156] // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // bytes:4 [156:160] // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64 // bytes:8 [160:168]

	BusyTime      uint64 // bytes:8 [168:176] // Time (usec) busy sending data
	RwndLimited   uint64 // bytes:8 [176:184] // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32 // bytes:4 [192:196]
	DeliveredCe uint32 // bytes:4 [196:200]

	BytesSent    uint64 // bytes:8 [200:208] // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // bytes:8 [208:216] // RFC4898 tcpEStatsPerfOctetsRetrans

	DsackDups uint32 // bytes:4 [216:220] // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen uint32 // bytes:4 [220:224] // reordering events seen

	RcvOoopack uint32 // bytes:4 [224:228] // Out-of-order packets received

	SndWnd uint32 // bytes:4 [228:232] // peer's advertised receive window after scaling (bytes)

	// 6.5+ below
	RcvWnd uint32 // bytes:4 [232:236] // local advertised receive window after scaling (bytes)
	Rehash uint32 // bytes:4 [236:240] // PLB or timeout triggered rehash attempts
}

// TCPInfo5_4_281 mirrors the kernel's `struct tcp_info` for Linux 5.4.281
// (tcp_info_for kernel 5.4+).
//
// https://github.com/torvalds/linux/blob/v5.4-rc8/include/uapi/linux/tcp.h#L206
type TCPInfo5_4_281 struct {
	State                  uint8
	CaState                uint8
	Retransmits            uint8
	Probes                 uint8
	Backoff                uint8
	Options                uint8
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32
	Ato    uint32
	SndMss uint32
	RcvMss uint32

	Unacked uint32
	Sacked  uint32
	Lost    uint32
	Retrans uint32
	Fackets uint32

	// 	Times
	LastDataSent uint32
	LastAckSent  uint32
	LastDataRecv uint32
	LastAckRecv  uint32

	// 	Metrics
	Pmtu        uint32
	RcvSsthresh uint32
	Rtt         uint32
	Rttvar      uint32
	SndSsthresh uint32
	SndCwnd     uint32
	AdvMss      uint32
	Reordering  uint32

	RcvRtt   uint32
	RcvSpace uint32

	TotalRetrans uint32

	PacingRate    uint64
	MaxPacingRate uint64
	BytesAcked    uint64 // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // RFC4898 tcpEStatsAppHCThruOctetsReceived
	SegsOut       uint32 // RFC4898 tcpEStatsPerfSegsOut
	SegsIn        uint32 // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32
	MinRtt       uint32
	DataSegsIn   uint32 // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64

	BusyTime      uint64 // Time (usec) busy sending data
	RwndLimited   uint64 // Time (usec) limited by receive window
	SndbufLimited uint64 // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32
	DeliveredCe uint32

	BytesSent    uint64 // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // RFC4898 tcpEStatsPerfOctetsRetrans
	DsackDups    uint32 // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen    uint32 // reordering events seen

	// 4.19 kernel tcp_info ends here

	RcvOoopack uint32 // Out-of-order packets received

	SndWnd uint32 // bytes:4 [228:232] // peer's advertised receive window after scaling (bytes)
}

// TCPInfo4_19_219 mirrors the kernel's `struct tcp_info` for Linux 4.19.219.
// Note that the exported bytes are 236, because it includes the RTA header.
//
//   - https://github.com/torvalds/linux/blob/v4.19-rc8/include/uapi/linux/tcp.h#L176
//   - https://git.launchpad.net/~ubuntu-kernel/ubuntu/+source/linux/+git/xenial/tree/include/uapi/linux/tcp.h?h=Ubuntu-hwe-4.15.0-107.108_16.04.1#n168
type TCPInfo4_19_219 struct {
	State                  uint8
	CaState                uint8
	Retransmits            uint8
	Probes                 uint8
	Backoff                uint8
	Options                uint8
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32
	Ato    uint32
	SndMss uint32
	RcvMss uint32

	Unacked uint32
	Sacked  uint32
	Lost    uint32
	Retrans uint32
	Fackets uint32

	// 	Times
	LastDataSent uint32
	LastAckSent  uint32
	LastDataRecv uint32
	LastAckRecv  uint32

	// 	Metrics
	Pmtu        uint32
	RcvSsthresh uint32
	Rtt         uint32
	Rttvar      uint32
	SndSsthresh uint32
	SndCwnd     uint32
	AdvMss      uint32
	Reordering  uint32

	RcvRtt   uint32
	RcvSpace uint32

	TotalRetrans uint32

	PacingRate    uint64
	MaxPacingRate uint64
	BytesAcked    uint64 // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // RFC4898 tcpEStatsAppHCThruOctetsReceived
	SegsOut       uint32 // RFC4898 tcpEStatsPerfSegsOut
	SegsIn        uint32 // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32
	MinRtt       uint32
	DataSegsIn   uint32 // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64

	BusyTime      uint64 // Time (usec) busy sending data
	RwndLimited   uint64 // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below

	Delivered   uint32
	DeliveredCe uint32

	BytesSent    uint64 // RFC4898 tcpEStatsPerfHCDataOctetsOut
	BytesRetrans uint64 // RFC4898 tcpEStatsPerfOctetsRetrans
	DsackDups    uint32 // RFC4898 tcpEStatsStackDSACKDups
	ReordSeen    uint32 // bytes:4 [220:224] // reordering events seen
}

// TCPInfo4_15 mirrors the kernel's `struct tcp_info` for Linux 4.15.
// Note that the exported bytes are 228, because it includes the RTA header.
//
// https://git.launchpad.net/~ubuntu-kernel/ubuntu/+source/linux/+git/xenial/tree/include/uapi/linux/tcp.h?h=Ubuntu-hwe-4.15.0-107.108_16.04.1#n168
type TCPInfo4_15 struct {
	State                  uint8
	CaState                uint8
	Retransmits            uint8
	Probes                 uint8
	Backoff                uint8
	Options                uint8
	SndWscale              uint8 // 4 bits from byte [6], low nibble
	RcvWscale              uint8 // 4 bits from byte [6], high nibble
	DeliveryRateAppLimited uint8 // 1 bit from byte [7], bit 0
	FastopenClientFail     uint8 // 2 bits from byte [7], bits 1-2

	Rto    uint32
	Ato    uint32
	SndMss uint32
	RcvMss uint32

	Unacked uint32
	Sacked  uint32
	Lost    uint32
	Retrans uint32
	Fackets uint32

	// 	Times
	LastDataSent uint32
	LastAckSent  uint32
	LastDataRecv uint32
	LastAckRecv  uint32

	// 	Metrics
	Pmtu        uint32
	RcvSsthresh uint32
	Rtt         uint32
	Rttvar      uint32
	SndSsthresh uint32
	SndCwnd     uint32
	AdvMss      uint32
	Reordering  uint32

	RcvRtt   uint32
	RcvSpace uint32

	TotalRetrans uint32

	PacingRate    uint64
	MaxPacingRate uint64
	BytesAcked    uint64 // RFC4898 tcpEStatsAppHCThruOctetsAcked
	BytesReceived uint64 // RFC4898 tcpEStatsAppHCThruOctetsReceived
	SegsOut       uint32 // RFC4898 tcpEStatsPerfSegsOut
	SegsIn        uint32 // RFC4898 tcpEStatsPerfSegsIn

	NotSentBytes uint32
	MinRtt       uint32
	DataSegsIn   uint32 // RFC4898 tcpEStatsDataSegsIn
	DataSegsOut  uint32 // RFC4898 tcpEStatsDataSegsOut

	DeliveryRate uint64

	BusyTime      uint64 // Time (usec) busy sending data
	RwndLimited   uint64 // Time (usec) limited by receive window
	SndbufLimited uint64 // bytes:8 [184:192] // Time (usec) limited by send buffer

	// 4.15 kernel tcp_info ends here, 5+ below}
}

// [das@t:~/Downloads/xtcp/pkg/xtcpnl/testdata]$ find ./ -name 'attribute_info*' | xargs -n 1 ls -la
// -rw-r--r-- 1 das users 252 Aug  8 19:48 ./6_10_3/attribute_info
// -rw-r--r-- 1 das users 252 Aug  8 19:49 ./6_10_3/attribute_info2
// -rw-r--r-- 1 das users 252 Aug  8 17:47 ./6_8_12/attribute_info
// -rw-r--r-- 1 das users 252 Aug  8 17:47 ./6_8_12/attribute_info2
//
// -rw-r--r-- 1 das users 244 Jul 31 18:02 ./6_6_44/attribute_info
// -rw-r--r-- 1 das users 244 Aug  8 19:25 ./6_6_44/attribute_info2
//
// -rw-r--r-- 1 das users 236 Aug  8 15:43 ./5_4_281/attribute_info
// -rw-r--r-- 1 das users 236 Aug  8 15:44 ./5_4_281/attribute_info2
// -rw-r--r-- 1 das users 236 Aug  8 19:27 ./6_1_103/attribute_info
// -rw-r--r-- 1 das users 236 Aug  8 19:26 ./6_1_103/attribute_info2
//
// -rw-r--r-- 1 das users 228 Aug  8 14:14 ./4_19_319/attribute_info
// -rw-r--r-- 1 das users 228 Aug  8 19:31 ./4_19_319/attribute_info2

// [das@l:~/Downloads/xtcp2/pkg/xtcpnl/testdata]$ ls -la 7_0_3/*_info
// -rw-r--r-- 1 das users 284 Jun 14 21:32 ./7_0_3/netlink_sock_diag_response_7_0_3_sport19000_dport10156_v6_info
// -rw-r--r-- 1 das users 284 Jun 14 21:32 ./7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443_info
// -rw-r--r-- 1 das users 284 Jun 14 21:32 ./7_0_3/netlink_sock_diag_response_7_0_3_sport63282_dport443_rcvrtt_info

// Values for TCPInfo.EcnMode, TCPInfo.AccecnOptSeen and
// TCPInfo.AccecnFailMode.
//
// ~/Downloads/linux/include/uapi/linux/tcp.h:229-245
// https://github.com/torvalds/linux/blob/master/include/uapi/linux/tcp.h#L229
const (
	// /* Values for tcpi_ecn_mode after negotiation */
	TCPIEcnModeDisabledCst = 0x0 // TCPI_ECN_MODE_DISABLED
	TCPIEcnModeRFC3168Cst  = 0x1 // TCPI_ECN_MODE_RFC3168 — classic ECN, not AccECN
	TCPIEcnModeAccECNCst   = 0x2 // TCPI_ECN_MODE_ACCECN
	TCPIEcnModePendingCst  = 0x3 // TCPI_ECN_MODE_PENDING

	// /* Values for accecn_opt_seen */
	TCPAccECNOptNotSeenCst     = 0x0 // TCP_ACCECN_OPT_NOT_SEEN
	TCPAccECNOptEmptySeenCst   = 0x1 // TCP_ACCECN_OPT_EMPTY_SEEN
	TCPAccECNOptCounterSeenCst = 0x2 // TCP_ACCECN_OPT_COUNTER_SEEN
	TCPAccECNOptFailSeenCst    = 0x3 // TCP_ACCECN_OPT_FAIL_SEEN

	// /* Values for accecn_fail_mode */ — a bitmask, not an enum
	TCPAccECNAceFailSendCst = 1 << 0 // TCP_ACCECN_ACE_FAIL_SEND
	TCPAccECNAceFailRecvCst = 1 << 1 // TCP_ACCECN_ACE_FAIL_RECV
	TCPAccECNOptFailSendCst = 1 << 2 // TCP_ACCECN_OPT_FAIL_SEND
	TCPAccECNOptFailRecvCst = 1 << 3 // TCP_ACCECN_OPT_FAIL_RECV
)

const (
	TCPInfo7_0_3_SizeCst    = 280 // 284 - 4
	TCPInfo6_10_3_SizeCst   = 248 // 252 - 4
	TCPInfo6_6_44_SizeCst   = 240 // 244 - 4
	TCPInfo5_4_281_SizeCst  = 232 // 236 - 4
	TCPInfo4_19_219_SizeCst = 224 // 228 - 4
	TCPInfo4_15_SizeCst     = 192
	TCPInfoMinSizeCst       = TCPInfo4_15_SizeCst

	TCPInfoEmumValueCst = 2
)

var (
	ErrTCPInfoSmall = errors.New("data too small for TCPInfo")
)

// DeserializeTCPInfo does a binary read of a TCPInfo, returning the
// number of bytes consumed (kernel-version-specific). The wire layout
// grows monotonically across kernel releases, so the parse is split
// into base + per-kernel-version tail extensions. Each tail reads any
// new fields beyond the previous size cap and returns the matching
// SizeCst when the input ends exactly there.
func DeserializeTCPInfo(data []byte, t *TCPInfo) (n int, err error) {

	if len(data) < TCPInfoMinSizeCst {
		return 0, ErrTCPInfoSmall
	}

	deserializeTCPInfoBase(data, t)

	// 4.15 kernel tcp_info ends here, 5+ below
	if len(data) == TCPInfo4_15_SizeCst {
		return len(data), nil
	}
	deserializeTCPInfoTail4_19(data, t)
	if len(data) == TCPInfo4_19_219_SizeCst {
		return TCPInfo4_19_219_SizeCst, nil
	}
	deserializeTCPInfoTail5_4(data, t)
	if len(data) == TCPInfo5_4_281_SizeCst {
		return TCPInfo5_4_281_SizeCst, nil
	}
	deserializeTCPInfoTail6_6(data, t)
	if len(data) == TCPInfo6_6_44_SizeCst {
		return TCPInfo6_6_44_SizeCst, nil
	}
	deserializeTCPInfoTail6_10(data, t)
	// Anything shorter than 7.0's 280 bytes carries no complete AccECN
	// trailer. Report the 6.10 size rather than a length the message does not
	// have — this is also the branch every pre-7.0 fixture in the corpus
	// takes, so the optional tail must never be a hard length requirement.
	if len(data) < TCPInfo7_0_3_SizeCst {
		return TCPInfo6_10_3_SizeCst, nil
	}
	deserializeTCPInfoTail7_0(data, t)
	return TCPInfo7_0_3_SizeCst, nil
}

// deserializeTCPInfoBase reads fields present in every supported kernel
// (4.15 and later — bytes 0..191).
func deserializeTCPInfoBase(data []byte, t *TCPInfo) {
	if len(data) < TCPInfoMinSizeCst {
		return
	}
	t.State = data[0]
	t.CaState = data[1]
	t.Retransmits = data[2]
	t.Probes = data[3]
	t.Backoff = data[4]
	t.Options = data[5]
	// Bitfield extraction below assumes little-endian bitfield packing, matching
	// the kernel's `__u8 tcpi_snd_wscale:4, tcpi_rcv_wscale:4;` layout on
	// x86/x86_64/ARM. On a big-endian host (PPC-BE, MIPS-BE) snd/rcv_wscale
	// would swap and the delivery_rate/fastopen bits would shift; the rest of
	// the package already uses binary.LittleEndian throughout, so this is
	// consistent rather than a new assumption.
	t.SndWscale = data[6] & 0x0F
	t.RcvWscale = (data[6] >> 4) & 0x0F
	t.DeliveryRateAppLimited = data[7] & 0x01
	t.FastopenClientFail = (data[7] >> 1) & 0x03

	t.Rto = binary.LittleEndian.Uint32(data[8:12])
	t.Ato = binary.LittleEndian.Uint32(data[12:16])
	t.SndMss = binary.LittleEndian.Uint32(data[16:20])
	t.RcvMss = binary.LittleEndian.Uint32(data[20:24])

	t.Unacked = binary.LittleEndian.Uint32(data[24:28])
	t.Sacked = binary.LittleEndian.Uint32(data[28:32])
	t.Lost = binary.LittleEndian.Uint32(data[32:36])
	t.Retrans = binary.LittleEndian.Uint32(data[36:40])
	// t.Fackets = binary.LittleEndian.Uint32(data[40:44]) // sysctl says "This is a legacy option, it has no effect anymore."
	// https://www.kernel.org/doc/html/latest/networking/ip-sysctl.html

	t.LastDataSent = binary.LittleEndian.Uint32(data[44:48])
	t.LastAckSent = binary.LittleEndian.Uint32(data[48:52])
	t.LastDataRecv = binary.LittleEndian.Uint32(data[52:56])
	t.LastAckRecv = binary.LittleEndian.Uint32(data[56:60])

	t.Pmtu = binary.LittleEndian.Uint32(data[60:64])
	t.RcvSsthresh = binary.LittleEndian.Uint32(data[64:68])
	t.Rtt = binary.LittleEndian.Uint32(data[68:72])
	t.Rttvar = binary.LittleEndian.Uint32(data[72:76])
	t.SndSsthresh = binary.LittleEndian.Uint32(data[76:80])
	t.SndCwnd = binary.LittleEndian.Uint32(data[80:84])
	t.AdvMss = binary.LittleEndian.Uint32(data[84:88])
	t.Reordering = binary.LittleEndian.Uint32(data[88:92])

	t.RcvRtt = binary.LittleEndian.Uint32(data[92:96])
	t.RcvSpace = binary.LittleEndian.Uint32(data[96:100])

	t.TotalRetrans = binary.LittleEndian.Uint32(data[100:104])

	t.PacingRate = binary.LittleEndian.Uint64(data[104:112])
	t.MaxPacingRate = binary.LittleEndian.Uint64(data[112:120])
	t.BytesAcked = binary.LittleEndian.Uint64(data[120:128])
	t.BytesReceived = binary.LittleEndian.Uint64(data[128:136])

	t.SegsOut = binary.LittleEndian.Uint32(data[136:140])
	t.SegsIn = binary.LittleEndian.Uint32(data[140:144])

	t.NotSentBytes = binary.LittleEndian.Uint32(data[144:148])
	t.MinRtt = binary.LittleEndian.Uint32(data[148:152])
	t.DataSegsIn = binary.LittleEndian.Uint32(data[152:156])
	t.DataSegsOut = binary.LittleEndian.Uint32(data[156:160])

	t.DeliveryRate = binary.LittleEndian.Uint64(data[160:168])

	t.BusyTime = binary.LittleEndian.Uint64(data[168:176])
	t.RwndLimited = binary.LittleEndian.Uint64(data[176:184])
	t.SndbufLimited = binary.LittleEndian.Uint64(data[184:192])
}

// deserializeTCPInfoTail4_19 reads the bytes added between kernels 4.15
// and 4.19 (bytes 192..223 — Delivered/Ce, BytesSent/Retrans, DsackDups,
// ReordSeen).
func deserializeTCPInfoTail4_19(data []byte, t *TCPInfo) {
	if len(data) < TCPInfo4_19_219_SizeCst {
		return
	}
	t.Delivered = binary.LittleEndian.Uint32(data[192:196])
	t.DeliveredCe = binary.LittleEndian.Uint32(data[196:200])

	t.BytesSent = binary.LittleEndian.Uint64(data[200:208])
	t.BytesRetrans = binary.LittleEndian.Uint64(data[208:216])

	t.DsackDups = binary.LittleEndian.Uint32(data[216:220])
	t.ReordSeen = binary.LittleEndian.Uint32(data[220:224])
}

// deserializeTCPInfoTail5_4 reads the bytes added in kernel 5.4
// (RcvOoopack and SndWnd, bytes 224..231).
func deserializeTCPInfoTail5_4(data []byte, t *TCPInfo) {
	if len(data) < TCPInfo5_4_281_SizeCst {
		return
	}
	t.RcvOoopack = binary.LittleEndian.Uint32(data[224:228])
	t.SndWnd = binary.LittleEndian.Uint32(data[228:232])
}

// deserializeTCPInfoTail6_6 reads the bytes added in kernel 6.5
// (RcvWnd and Rehash, bytes 232..239).
func deserializeTCPInfoTail6_6(data []byte, t *TCPInfo) {
	if len(data) < TCPInfo6_6_44_SizeCst {
		return
	}
	t.RcvWnd = binary.LittleEndian.Uint32(data[232:236])
	t.Rehash = binary.LittleEndian.Uint32(data[236:240])
}

// deserializeTCPInfoTail6_10 reads the RTO totals appended in kernel
// 6.10 (bytes 240..247).
func deserializeTCPInfoTail6_10(data []byte, t *TCPInfo) {
	if len(data) < TCPInfo6_10_3_SizeCst {
		return
	}
	t.TotalRTO = binary.LittleEndian.Uint16(data[240:242])
	t.TotalRTORecoveries = binary.LittleEndian.Uint16(data[242:244])
	t.TotalRTOTime = binary.LittleEndian.Uint32(data[244:248])
}

// deserializeTCPInfoTail7_0 reads the Accurate ECN trailer appended in
// kernel 7.0 (bytes 248..279). See TCPInfo7_0_3 for the kernel source.
func deserializeTCPInfoTail7_0(data []byte, t *TCPInfo) {
	if len(data) < TCPInfo7_0_3_SizeCst {
		return
	}
	t.ReceivedCe = binary.LittleEndian.Uint32(data[248:252])
	t.DeliveredE1Bytes = binary.LittleEndian.Uint32(data[252:256])
	t.DeliveredE0Bytes = binary.LittleEndian.Uint32(data[256:260])
	t.DeliveredCeBytes = binary.LittleEndian.Uint32(data[260:264])
	t.ReceivedE1Bytes = binary.LittleEndian.Uint32(data[264:268])
	t.ReceivedE0Bytes = binary.LittleEndian.Uint32(data[268:272])
	t.ReceivedCeBytes = binary.LittleEndian.Uint32(data[272:276])

	// Same little-endian bitfield assumption as the snd/rcv_wscale split in
	// deserializeTCPInfoBase: ecn_mode:2, accecn_opt_seen:2,
	// accecn_fail_mode:4, options2:24 packed low-bits-first.
	flags2 := binary.LittleEndian.Uint32(data[276:280])
	t.EcnMode = uint8(flags2 & 0x03)
	t.AccecnOptSeen = uint8((flags2 >> 2) & 0x03)
	t.AccecnFailMode = uint8((flags2 >> 4) & 0x0F)
	t.Options2 = (flags2 >> 8) & 0x00FFFFFF
}

// DeserializeTCPInfoXTCP reads a kernel tcp_info payload directly into
// the protobuf XtcpFlatRecord, split per kernel version the same way
// as DeserializeTCPInfo (see comments there).
func DeserializeTCPInfoXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	if len(data) < TCPInfoMinSizeCst {
		return ErrTCPInfoSmall
	}
	deserializeTCPInfoXTCPBase(data, x)
	if len(data) == TCPInfo4_15_SizeCst {
		return nil
	}
	deserializeTCPInfoXTCPTail4_19(data, x)
	if len(data) == TCPInfo4_19_219_SizeCst {
		return nil
	}
	deserializeTCPInfoXTCPTail5_4(data, x)
	if len(data) == TCPInfo5_4_281_SizeCst {
		return nil
	}
	deserializeTCPInfoXTCPTail6_6(data, x)
	if len(data) == TCPInfo6_6_44_SizeCst {
		return nil
	}
	deserializeTCPInfoXTCPTail6_10(data, x)
	// The AccECN trailer is optional — see DeserializeTCPInfo.
	if len(data) < TCPInfo7_0_3_SizeCst {
		return nil
	}
	deserializeTCPInfoXTCPTail7_0(data, x)
	return nil
}

func deserializeTCPInfoXTCPBase(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfoMinSizeCst {
		return
	}
	x.TcpInfoState = uint32(data[0])
	x.TcpInfoCaState = uint32(data[1])
	x.TcpInfoRetransmits = uint32(data[2])
	x.TcpInfoProbes = uint32(data[3])
	x.TcpInfoBackoff = uint32(data[4])
	x.TcpInfoOptions = uint32(data[5])
	x.TcpInfoSndWscale = uint32(data[6] & 0x0F)
	x.TcpInfoRcvWscale = uint32((data[6] >> 4) & 0x0F)
	x.TcpInfoDeliveryRateAppLimited = uint32(data[7] & 0x01)
	x.TcpInfoFastopenClientFail = uint32((data[7] >> 1) & 0x03)

	x.TcpInfoRto = binary.LittleEndian.Uint32(data[8:12])
	x.TcpInfoAto = binary.LittleEndian.Uint32(data[12:16])
	x.TcpInfoSndMss = binary.LittleEndian.Uint32(data[16:20])
	x.TcpInfoRcvMss = binary.LittleEndian.Uint32(data[20:24])

	x.TcpInfoUnacked = binary.LittleEndian.Uint32(data[24:28])
	x.TcpInfoSacked = binary.LittleEndian.Uint32(data[28:32])
	x.TcpInfoLost = binary.LittleEndian.Uint32(data[32:36])
	x.TcpInfoRetrans = binary.LittleEndian.Uint32(data[36:40])
	// x.Fackets = binary.LittleEndian.Uint32(data[40:44]) // sysctl says "This is a legacy option, it has no effect anymore."
	// https://www.kernel.org/doc/html/latest/networking/ip-sysctl.html

	x.TcpInfoLastDataSent = binary.LittleEndian.Uint32(data[44:48])
	x.TcpInfoLastAckSent = binary.LittleEndian.Uint32(data[48:52])
	x.TcpInfoLastDataRecv = binary.LittleEndian.Uint32(data[52:56])
	x.TcpInfoLastAckRecv = binary.LittleEndian.Uint32(data[56:60])

	x.TcpInfoPmtu = binary.LittleEndian.Uint32(data[60:64])
	x.TcpInfoRcvSsthresh = binary.LittleEndian.Uint32(data[64:68])
	x.TcpInfoRtt = binary.LittleEndian.Uint32(data[68:72])
	x.TcpInfoRttvar = binary.LittleEndian.Uint32(data[72:76])
	x.TcpInfoSndSsthresh = binary.LittleEndian.Uint32(data[76:80])
	x.TcpInfoSndCwnd = binary.LittleEndian.Uint32(data[80:84])
	x.TcpInfoAdvmss = binary.LittleEndian.Uint32(data[84:88])
	x.TcpInfoReordering = binary.LittleEndian.Uint32(data[88:92])

	x.TcpInfoRcvRtt = binary.LittleEndian.Uint32(data[92:96])
	x.TcpInfoRcvSpace = binary.LittleEndian.Uint32(data[96:100])

	x.TcpInfoTotalRetrans = binary.LittleEndian.Uint32(data[100:104])

	x.TcpInfoPacingRate = binary.LittleEndian.Uint64(data[104:112])
	x.TcpInfoMaxPacingRate = binary.LittleEndian.Uint64(data[112:120])
	x.TcpInfoBytesAcked = binary.LittleEndian.Uint64(data[120:128])
	x.TcpInfoBytesReceived = binary.LittleEndian.Uint64(data[128:136])

	x.TcpInfoSegsOut = binary.LittleEndian.Uint32(data[136:140])
	x.TcpInfoSegsIn = binary.LittleEndian.Uint32(data[140:144])

	x.TcpInfoNotsentBytes = binary.LittleEndian.Uint32(data[144:148])
	x.TcpInfoMinRtt = binary.LittleEndian.Uint32(data[148:152])
	x.TcpInfoDataSegsIn = binary.LittleEndian.Uint32(data[152:156])
	x.TcpInfoDataSegsOut = binary.LittleEndian.Uint32(data[156:160])

	x.TcpInfoDeliveryRate = binary.LittleEndian.Uint64(data[160:168])

	x.TcpInfoBusyTime = binary.LittleEndian.Uint64(data[168:176])
	x.TcpInfoRwndLimited = binary.LittleEndian.Uint64(data[176:184])
	x.TcpInfoSndbufLimited = binary.LittleEndian.Uint64(data[184:192])
}

func deserializeTCPInfoXTCPTail4_19(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfo4_19_219_SizeCst {
		return
	}
	x.TcpInfoDelivered = binary.LittleEndian.Uint32(data[192:196])
	x.TcpInfoDeliveredCe = binary.LittleEndian.Uint32(data[196:200])

	x.TcpInfoBytesSent = binary.LittleEndian.Uint64(data[200:208])
	x.TcpInfoBytesRetrans = binary.LittleEndian.Uint64(data[208:216])

	x.TcpInfoDsackDups = binary.LittleEndian.Uint32(data[216:220])
	x.TcpInfoReordSeen = binary.LittleEndian.Uint32(data[220:224])
}

func deserializeTCPInfoXTCPTail5_4(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfo5_4_281_SizeCst {
		return
	}
	x.TcpInfoRcvOoopack = binary.LittleEndian.Uint32(data[224:228])
	x.TcpInfoSndWnd = binary.LittleEndian.Uint32(data[228:232])
}

func deserializeTCPInfoXTCPTail6_6(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfo6_6_44_SizeCst {
		return
	}
	x.TcpInfoRcvWnd = binary.LittleEndian.Uint32(data[232:236])
	x.TcpInfoRehash = binary.LittleEndian.Uint32(data[236:240])
}

func deserializeTCPInfoXTCPTail6_10(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfo6_10_3_SizeCst {
		return
	}
	x.TcpInfoTotalRto = uint32(binary.LittleEndian.Uint16(data[240:242]))
	x.TcpInfoTotalRtoRecoveries = uint32(binary.LittleEndian.Uint16(data[242:244]))
	x.TcpInfoTotalRtoTime = binary.LittleEndian.Uint32(data[244:248])
}

// deserializeTCPInfoXTCPTail7_0 reads the Accurate ECN trailer appended in
// kernel 7.0 (bytes 248..279). See TCPInfo7_0_3 for the kernel source.
func deserializeTCPInfoXTCPTail7_0(data []byte, x *xtcp_flat_record.XtcpFlatRecord) {
	if len(data) < TCPInfo7_0_3_SizeCst {
		return
	}
	x.TcpInfoReceivedCe = binary.LittleEndian.Uint32(data[248:252])
	x.TcpInfoDeliveredE1Bytes = binary.LittleEndian.Uint32(data[252:256])
	x.TcpInfoDeliveredE0Bytes = binary.LittleEndian.Uint32(data[256:260])
	x.TcpInfoDeliveredCeBytes = binary.LittleEndian.Uint32(data[260:264])
	x.TcpInfoReceivedE1Bytes = binary.LittleEndian.Uint32(data[264:268])
	x.TcpInfoReceivedE0Bytes = binary.LittleEndian.Uint32(data[268:272])
	x.TcpInfoReceivedCeBytes = binary.LittleEndian.Uint32(data[272:276])

	flags2 := binary.LittleEndian.Uint32(data[276:280])
	x.TcpInfoEcnMode = flags2 & 0x03
	x.TcpInfoAccecnOptSeen = (flags2 >> 2) & 0x03
	x.TcpInfoAccecnFailMode = (flags2 >> 4) & 0x0F
	x.TcpInfoOptions2 = (flags2 >> 8) & 0x00FFFFFF
}
