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

// INET_DIAG_SOCKOPT 22

// https://github.com/torvalds/linux/blob/5f583a3162ffd9f7999af76b8ab634ce2dac9f90/include/uapi/linux/inet_diag.h#L189
//
// struct inet_diag_sockopt {
// 	__u8	recverr:1,
// 		is_icsk:1,
// 		freebind:1,
// 		hdrincl:1,
// 		mc_loop:1,
// 		transparent:1,
// 		mc_all:1,
// 		nodefrag:1;
// 	__u8	bind_address_no_port:1,
// 		recverr_rfc4884:1,
// 		defer_connect:1,
// 		unused:5;
// };

type SockOpt uint16

const (
	SockOptSizeCst = 2
	SockOptReadCst = SockOptSizeCst

	SockOptEnumValueCst = 22
)

var (
	ErrSockOptSmall = errors.New("data too small for SockOpt")
)

// DeserializeSockOpt does a binary read of a SockOpt
// It does a basic length check
func DeserializeSockOpt(data []byte, c *SockOpt) (n int, err error) {

	if len(data) < SockOptSizeCst {
		return 0, ErrSockOptSmall
	}

	*c = SockOpt(binary.LittleEndian.Uint16(data[0:2]))

	return SockOptReadCst, nil
}

func DeserializeSockOptReflection(data []byte, c *SockOpt) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func DeserializeSockOptXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < SockOptSizeCst {
		return ErrSockOptSmall
	}

	x.SockOpt = uint32(binary.LittleEndian.Uint16(data[0:2]))

	return nil
}
