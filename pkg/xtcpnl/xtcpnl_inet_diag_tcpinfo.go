package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
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

type TCPInfo TCPInfo6_10_3

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

const (
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
	return TCPInfo6_10_3_SizeCst, nil
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

func DeserializeTCPInfoReflection(data []byte, mi *TCPInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, mi)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

func DeserializeTCPInfoTCPInfoTCPInfo6_10_3Reflection(data []byte, t *TCPInfo6_10_3) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

func DeserializeTCPInfoTCPInfo6_6_44Reflection(data []byte, t *TCPInfo6_6_44) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

func DeserializeTCPInfo5_4_281Reflection(data []byte, t *TCPInfo5_4_281) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

func DeserializeTCPInfo4_19_219Reflection(data []byte, t *TCPInfo4_19_219) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
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
	x.TcpInfoSendScale = uint32(data[6] & 0x0F)
	x.TcpInfoRcvScale = uint32((data[6] >> 4) & 0x0F)
	x.TcpInfoDeliveryRateAppLimited = uint32(data[7] & 0x01)
	x.TcpInfoFastOpenClientFailed = uint32((data[7] >> 1) & 0x03)

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
	x.TcpInfoRttVar = binary.LittleEndian.Uint32(data[72:76])
	x.TcpInfoSndSsthresh = binary.LittleEndian.Uint32(data[76:80])
	x.TcpInfoSndCwnd = binary.LittleEndian.Uint32(data[80:84])
	x.TcpInfoAdvMss = binary.LittleEndian.Uint32(data[84:88])
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

	x.TcpInfoNotSentBytes = binary.LittleEndian.Uint32(data[144:148])
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
