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

// INET_DIAG_CGROUP_ID 21

// https://github.com/torvalds/linux/blob/521b1e7f4cf0b05a47995b103596978224b380a8/include/linux/inet_diag.h#L77
// #ifdef CONFIG_SOCK_CGROUP_DATA
// 		+ nla_total_size_64bit(sizeof(u64))  /* INET_DIAG_CGROUP_ID */

// 	INET_DIAG_BC_CGROUP_COND,   /* u64 cgroup v2 ID */

// https://github.com/torvalds/linux/blob/521b1e7f4cf0b05a47995b103596978224b380a8/net/ipv4/inet_diag.c#L166
// inet_diag.c comment
/* Fallback to socket priority if class id isn't set.
* Classful qdiscs use it as direct reference to class.
* For cgroup2 classid is always zero.
 */

// type CGroupID struct {
// 	ID uint64
// }

type CGroupID uint64

const (
	CGroupIDSizeCst = 8
	CGroupIDReadCst = CGroupIDSizeCst

	CGroupIDEnumValueCst = 21
)

var (
	ErrCGroupIDSmall = errors.New("data too small for CGroupID")
)

// DeserializeCGroupID does a binary read of a CGroupID
// It does a basic length check
func DeserializeCGroupID(data []byte, c *CGroupID) (n int, err error) {

	if len(data) < CGroupIDSizeCst {
		return 0, ErrCGroupIDSmall
	}

	*c = CGroupID(binary.LittleEndian.Uint64(data[0:8]))

	return CGroupIDSizeCst, nil
}

func DeserializeCGroupIDReflection(data []byte, c *CGroupID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}

	return CGroupIDSizeCst, err
}

func DeserializeCGroupIDXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	// func DeserializeCGroupIDXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < CGroupIDSizeCst {
		return ErrCGroupIDSmall
	}

	x.CGroup = binary.LittleEndian.Uint64(data[0:8])

	return nil
}
