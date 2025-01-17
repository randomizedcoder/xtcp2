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

// INET_DIAG_SHUTDOWN 8

type Shutdown uint8

const (
	ShutdownSizeCst    = 1
	ShutdownMinSizeCst = ShutdownSizeCst

	ShutdownEmumValueCst = 8
)

var (
	ErrShutdownSmall = errors.New("data too small for Shutdown")
)

// DeserializeShutdown does a binary read of a Shutdown
// It does a basic length check
func DeserializeShutdown(data []byte, s *Shutdown) (n int, err error) {

	if len(data) < ShutdownMinSizeCst {
		return 0, ErrShutdownSmall
	}

	*s = Shutdown(data[0])

	return ShutdownSizeCst, nil
}

func DeserializeShutdownReflection(data []byte, s *Shutdown) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, s)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func DeserializeShutdownXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {

	if len(data) < ShutdownMinSizeCst {
		return ErrShutdownSmall
	}

	x.ShutdownState = uint32(data[0])

	return nil
}
