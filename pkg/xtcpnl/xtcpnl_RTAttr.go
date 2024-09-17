package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Wireshark's netlink disector
// https://github.com/wireshark/wireshark/blob/b24016e0b27bcbc43ba2a269e465eb0f03751b1f/epan/dissectors/packet-netlink-sock_diag.c#L24

// https://github.com/torvalds/linux/blob/bd2463ac7d7ec51d432f23bf0e893fb371a908cd/include/uapi/linux/rtnetlink.h#L195
// /*
//
//	   Generic structure for encapsulation of optional route information.
//	   It is reminiscent of sockaddr, but with sa_family replaced
//	   with attribute type.
//	 */
//
//		struct rtattr {
//			unsigned short	rta_len;
//			unsigned short	rta_type;
//		};
type RTAttr struct {
	Len  uint16 // 2
	Type uint16 // 2 = 4 ( 4 / 4 = 1 )
}

const (
	RTAttrSizeCst = 4
	RTAttrReadCst = RTAttrSizeCst
)

var (
	ErrRTAttrSmall = errors.New("data too small for RTAttr")
)

// DeserializeRTAttr does a binary read of a RTAttr
// It does a basic length check
func DeserializeRTAttr(data []byte, rta *RTAttr) (n int, err error) {

	if len(data) < RTAttrSizeCst {
		return 0, ErrRTAttrSmall
	}

	rta.Len = binary.LittleEndian.Uint16(data[0:2])
	rta.Type = binary.LittleEndian.Uint16(data[2:4])

	return RTAttrReadCst, nil
}

func DeserializeRTAttrReflection(data []byte, rta *RTAttr) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, rta)
	if err != nil {
		return 0, err
	}

	return RTAttrReadCst, err
}
