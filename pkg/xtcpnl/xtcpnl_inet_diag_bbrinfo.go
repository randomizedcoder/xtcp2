package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

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

// INET_DIAG_BBRINFO 16

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h#L225
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L204
//
//	struct tcp_bbr_info {
//		/* u64 bw: max-filtered BW (app throughput) estimate in Byte per sec: */
//		__u32	bbr_bw_lo;		/* lower 32 bits of bw */
//		__u32	bbr_bw_hi;		/* upper 32 bits of bw */
//		__u32	bbr_min_rtt;		/* min-filtered RTT in uSec */
//		__u32	bbr_pacing_gain;	/* pacing gain shifted left 8 bits */
//		__u32	bbr_cwnd_gain;		/* cwnd gain shifted left 8 bits */
//	};

type BBRInfo struct {
	BwLo       uint32 // 4 = 4
	BwHi       uint32 // 4 = 8
	MinRtt     uint32 // 4 = 12
	PacingGain uint32 // 4 = 16
	CwndGain   uint32 // 4 = 20
}

const (
	BBRInfoSizeCst = 20
	BBRInfoReadCst = BBRInfoSizeCst

	BBRInfoEnumValueCst = 16
)

var (
	ErrBBRInfoSmall = errors.New("data too small for BBRInfo")
)

// DeserializeBBRInfo does a binary read of a BBRInfo
// It does a basic length check
func DeserializeBBRInfo(data []byte, b *BBRInfo) (n int, err error) {

	if len(data) < MemInfoSizeCst {
		return 0, ErrMemInfoSmall
	}

	b.BwLo = binary.LittleEndian.Uint32(data[0:4])
	b.BwHi = binary.LittleEndian.Uint32(data[4:8])
	b.MinRtt = binary.LittleEndian.Uint32(data[8:12])
	b.PacingGain = binary.LittleEndian.Uint32(data[12:16])
	b.CwndGain = binary.LittleEndian.Uint32(data[16:20])

	return BBRInfoReadCst, nil
}

func DeserializeBBRInfoReflection(data []byte, b *BBRInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, b)
	if err != nil {
		return 0, err
	}

	return BBRInfoReadCst, err
}

func DeserializeBBRInfoXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < MemInfoSizeCst {
		return ErrMemInfoSmall
	}

	x.BbrInfoBwLo = binary.LittleEndian.Uint32(data[0:4])
	x.BbrInfoBwHi = binary.LittleEndian.Uint32(data[4:8])
	x.BbrInfoMinRtt = binary.LittleEndian.Uint32(data[8:12])
	x.BbrInfoPacingGain = binary.LittleEndian.Uint32(data[12:16])
	x.BbrInfoCwndGain = binary.LittleEndian.Uint32(data[16:20])

	return nil
}

func ZeroizeBBRInfoXTCP(x *xtcp_flat_record.Envelope_XtcpFlatRecord) {
	x.BbrInfoBwLo = 0
	x.BbrInfoBwHi = 0
	x.BbrInfoMinRtt = 0
	x.BbrInfoPacingGain = 0
	x.BbrInfoCwndGain = 0
}
