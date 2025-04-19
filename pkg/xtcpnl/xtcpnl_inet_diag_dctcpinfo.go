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
// INET_DIAG_DCTCPInfo 16
// INET_DIAG_CLASS_ID 17
// INET_DIAG_MD5SIG 18
// INET_DIAG_ULP_INFO 19
// INET_DIAG_SK_BPF_STORAGES 20
// INET_DIAG_CGROUP_ID 21
// INET_DIAG_SOCKOPT 22
// 23
// __INET_DIAG_MAX 24

// INET_DIAG_DCTCPINFO 9

// /* INET_DIAG_DCTCPINFO */

// https://github.com/torvalds/linux/blob/5f583a3162ffd9f7999af76b8ab634ce2dac9f90/include/uapi/linux/inet_diag.h#L215

// struct tcp_dctcp_info {
// 	__u16	dctcp_enabled;
// 	__u16	dctcp_ce_state;
// 	__u32	dctcp_alpha;
// 	__u32	dctcp_ab_ecn;
// 	__u32	dctcp_ab_tot;
// };

type DCTCPInfo struct {
	Enabled uint16 // 2 = 2
	CEState uint16 // 2 = 4
	Alpha   uint32 // 4 = 8
	ABECN   uint32 // 8 = 12
	ABTOT   uint32 // 12 = 16
}

const (
	DCTCPInfoSizeCst = 16
	DCTCPInfoReadCst = DCTCPInfoSizeCst

	DCTCPInfoEnumValueCst = 9
)

var (
	ErrDCTCPInfoSmall = errors.New("data too small for DCTCPInfo")
)

// DeserializeDCTCPInfo does a binary read of a DCTCPInfo
// It does a basic length check
func DeserializeDCTCPInfo(data []byte, d *DCTCPInfo) (n int, err error) {

	if len(data) < DCTCPInfoSizeCst {
		return 0, ErrDCTCPInfoSmall
	}

	d.Enabled = binary.LittleEndian.Uint16(data[0:2])
	d.CEState = binary.LittleEndian.Uint16(data[2:4])
	d.Alpha = binary.LittleEndian.Uint32(data[4:8])
	d.ABECN = binary.LittleEndian.Uint32(data[8:12])
	d.ABTOT = binary.LittleEndian.Uint32(data[12:16])

	return DCTCPInfoReadCst, nil
}

func DeserializeDCTCPInfoReflection(data []byte, d *DCTCPInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, d)
	if err != nil {
		return 0, err
	}

	return DCTCPInfoReadCst, err
}

func DeserializeDCTCPInfoXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	// func DeserializeDCTCPInfoXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < DCTCPInfoSizeCst {
		return ErrDCTCPInfoSmall
	}

	x.DctcpInfoEnabled = uint32(binary.LittleEndian.Uint16(data[0:2]))
	x.DctcpInfoCeState = uint32(binary.LittleEndian.Uint16(data[2:4]))
	x.DctcpInfoAlpha = binary.LittleEndian.Uint32(data[4:8])
	x.DctcpInfoAbEcn = binary.LittleEndian.Uint32(data[8:12])
	x.DctcpInfoAbTot = binary.LittleEndian.Uint32(data[12:16])

	return nil
}

func ZeroizeDCTCPInfoXTCP(x *xtcp_flat_record.XtcpFlatRecord) {
	// func ZeroizeDCTCPInfoXTCP(x *xtcp_flat_record.Envelope_XtcpFlatRecord) {
	x.DctcpInfoEnabled = 0
	x.DctcpInfoCeState = 0
	x.DctcpInfoAlpha = 0
	x.DctcpInfoAbEcn = 0
	x.DctcpInfoAbTot = 0
}
