package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
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

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h#L206
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L183C1-L190C3
/* INET_DIAG_VEGASINFO */
//
// struct tcpvegas_info {
// 	__u32	tcpv_enabled;
// 	__u32	tcpv_rttcnt;
// 	__u32	tcpv_rtt;
// 	__u32	tcpv_minrtt;
// };

type VegasInfo struct {
	Enabled uint32 // 4 = 4
	RttCnt  uint32 // 4 = 8
	Rtt     uint32 // 4 = 12
	MinRtt  uint32 // 4 = 16
}

const (
	VegasInfoSizeCst = 16
	VegasInfoReadCst = VegasInfoSizeCst

	VegasInfoEnumValueCst = 3
)

var (
	ErrVegasInfoSmall = errors.New("data too small for VegasInfo")
)

// DeserializeVegasInfo does a binary read of a VegasInfo
// It does a basic length check
func DeserializeVegasInfo(data []byte, vi *VegasInfo) (n int, err error) {

	if len(data) < VegasInfoSizeCst {
		return 0, ErrVegasInfoSmall
	}

	vi.Enabled = binary.LittleEndian.Uint32(data[0:4])
	vi.RttCnt = binary.LittleEndian.Uint32(data[4:8])
	vi.Rtt = binary.LittleEndian.Uint32(data[8:12])
	vi.MinRtt = binary.LittleEndian.Uint32(data[12:16])

	return VegasInfoReadCst, nil
}

func DeserializeVegasInfoReflection(data []byte, vi *VegasInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, vi)
	if err != nil {
		return 0, err
	}

	return VegasInfoReadCst, err
}

func DeserializeVegasInfoXTCP(data []byte, x *xtcppb.FlatXtcpRecord) (err error) {

	if len(data) < VegasInfoSizeCst {
		return ErrVegasInfoSmall
	}

	x.VegasInfoEnabled = binary.LittleEndian.Uint32(data[0:4])
	x.VegasInfoRttCnt = binary.LittleEndian.Uint32(data[4:8])
	x.VegasInfoRtt = binary.LittleEndian.Uint32(data[8:12])
	x.VegasInfoMinRtt = binary.LittleEndian.Uint32(data[12:16])

	return nil
}

func ZeroizeVegasInfoXTCP(x *xtcppb.FlatXtcpRecord) {
	x.VegasInfoEnabled = 0
	x.VegasInfoRttCnt = 0
	x.VegasInfoRtt = 0
	x.VegasInfoMinRtt = 0
}
