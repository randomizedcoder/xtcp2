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

// INET_DIAG_CLASS_ID 17

type ClassID uint32

const (
	ClassIDSizeCst = 4
	ClassIDReadCst = ClassIDSizeCst

	ClassIDEnumValueCst = 17
)

var (
	ErrClassIDSmall = errors.New("data too small for ClassID")
)

// DeserializeClassID does a binary read of a ClassID
// It does a basic length check
func DeserializeClassID(data []byte, c *ClassID) (n int, err error) {

	if len(data) < ClassIDSizeCst {
		return 0, ErrClassIDSmall
	}

	*c = ClassID(binary.LittleEndian.Uint32(data[0:4]))
	n = len(data)

	return n, nil
}

func DeserializeClassIDReflection(data []byte, c *ClassID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func DeserializeClassIDXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	// func DeserializeClassIDXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < ClassIDSizeCst {
		return ErrClassIDSmall
	}

	x.ClassId = binary.LittleEndian.Uint32(data[0:4])

	return nil
}
