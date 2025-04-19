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

// INET_DIAG_MEMINFO 1
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L174
// /* INET_DIAG_MEM */
//
//	struct inet_diag_meminfo {
//		__u32	idiag_rmem;
//		__u32	idiag_wmem;
//		__u32	idiag_fmem;
//		__u32	idiag_tmem;
//	};
type MemInfo struct {
	Rmem uint32 // 4 = 4
	Wmem uint32 // 4 = 8
	Fmem uint32 // 4 = 12
	Tmem uint32 // 4 = 16
}

const (
	MemInfoSizeCst = 16
	MemInfoReadCst = MemInfoSizeCst

	MemInfoEmumValueCst = 1
)

var (
	ErrMemInfoSmall = errors.New("data too small for MemInfo")
)

// DeserializeMemInfo does a binary read of a MemInfo
// It does a basic length check
func DeserializeMemInfo(data []byte, mi *MemInfo) (n int, err error) {

	if len(data) < MemInfoSizeCst {
		return 0, ErrMemInfoSmall
	}

	mi.Rmem = binary.LittleEndian.Uint32(data[0:4])
	mi.Wmem = binary.LittleEndian.Uint32(data[4:8])
	mi.Fmem = binary.LittleEndian.Uint32(data[8:12])
	mi.Tmem = binary.LittleEndian.Uint32(data[12:16])

	return MemInfoReadCst, nil
}

func DeserializeMemInfoReflection(data []byte, mi *MemInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, mi)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

// INET_DIAG_INFO 2

func DeserializeMemInfoXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	// func DeserializeMemInfoXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < MemInfoSizeCst {
		return ErrMemInfoSmall
	}

	x.MemInfoRmem = binary.LittleEndian.Uint32(data[0:4])
	x.MemInfoWmem = binary.LittleEndian.Uint32(data[4:8])
	x.MemInfoFmem = binary.LittleEndian.Uint32(data[8:12])
	x.MemInfoTmem = binary.LittleEndian.Uint32(data[12:16])

	return nil
}
