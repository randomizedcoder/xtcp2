package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
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
// INET_DIAG_DCTCPINFO 9 // - Don't have an example of this type
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
// INET_DIAG_PRAGUEINFO 23
// __INET_DIAG_MAX 24

// https://github.com/L4STeam/linux/blob/56eae305cddf172b87c54d8a61db8d1e9e2204f0/include/uapi/linux/inet_diag.h#L237

// INET_DIAG_PRAGUEINFO 23

// struct tcp_prague_info {
// 	__u64	prague_alpha;
// 	__u64	prague_frac_cwnd;
// 	__u64   prague_rate_bytes;
// 	__u32	prague_max_burst;
// 	__u32	prague_round;
// 	__u32	prague_rtt_target;
// 	bool	prague_enabled;
// };

type PragueInfo struct {
	Alpha     uint64 // 8 = 8
	FracCwnd  uint64 // 8 = 16
	RateBytes uint64 // 8 = 24
	MaxBurst  uint32 // 4 = 28
	Round     uint32 // 4 = 32
	RttTarget uint32 // 4 = 36
}

const (
	PragueInfoSizeCst = 36
	PragueInfoReadCst = PragueInfoSizeCst

	PragueInfoEnumValueCst = 23
)

var (
	ErrPragueInfoSmall = errors.New("data too small for PragueInfo")
)

// DeserializePragueInfo does a binary read of a PragueInfo
// It does a basic length check
func DeserializePragueInfo(data []byte, p *PragueInfo) (n int, err error) {

	if len(data) < PragueInfoSizeCst {
		return 0, ErrPragueInfoSmall
	}

	p.Alpha = binary.LittleEndian.Uint64(data[0:8])
	p.FracCwnd = binary.LittleEndian.Uint64(data[8:16])
	p.RateBytes = binary.LittleEndian.Uint64(data[16:24])
	p.MaxBurst = binary.LittleEndian.Uint32(data[24:28])
	p.Round = binary.LittleEndian.Uint32(data[28:32])
	p.RttTarget = binary.LittleEndian.Uint32(data[32:36])

	return PragueInfoReadCst, nil
}

func DeserializePragueInfoReflection(data []byte, p *PragueInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, p)
	if err != nil {
		return 0, err
	}

	return PragueInfoReadCst, err
}

// func DeserializePragueInfoXTCP(data []byte, x *xtcppb.FlatXtcpRecord) (err error) {

// 	if len(data) < PragueInfoSizeCst {
// 		return ErrPragueInfoSmall
// 	}

// 	x.Alpha = binary.LittleEndian.Uint64(data[0:8])
// 	x.FracCwnd = binary.LittleEndian.Uint64(data[8:16])
// 	x.RateBytes = binary.LittleEndian.Uint64(data[16:24])
// 	x.MaxBurst = binary.LittleEndian.Uint32(data[24:28])
// 	x.Round = binary.LittleEndian.Uint32(data[28:32])
// 	x.RttTarget = binary.LittleEndian.Uint32(data[32:36])

// 	return nil
// }

// func ZeroizePragueInfoXTCP(x *xtcppb.FlatXtcpRecord) {
// 	x.Alpha = 0
// 	x.FracCwnd = 0
// 	x.RateBytes = 0
// 	x.MaxBurst = 0
// 	x.Round = 0
// 	x.RttTarget = 0
// }
