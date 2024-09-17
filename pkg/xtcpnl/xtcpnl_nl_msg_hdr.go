package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Wireshark's netlink disector
// https://github.com/wireshark/wireshark/blob/b24016e0b27bcbc43ba2a269e465eb0f03751b1f/epan/dissectors/packet-netlink-sock_diag.c#L24

// https://pkg.go.dev/golang.org/x/sys/unix#NlMsghdr
// type NlMsghdr struct {
// 	Len   uint32
// 	Type  uint16
// 	Flags uint16
// 	Seq   uint32
// 	Pid   uint32
// }

type NlMsgHdr struct {
	Len   uint32 // 4
	Type  uint16 // 2
	Flags uint16 // 2
	Seq   uint32 // 4
	Pid   uint32 // 4 = 16 ( 16 / 4 = 4 )
}

const (
	NlMsgHdrSizeCst = 16
	NlMsgHdrReadCst = NlMsgHdrSizeCst

	NlMsgHdrTypeInetDiagCst = 20
	NlMsgHdrTypeDoneCst     = 3
)

var (
	ErrNlMsgHdrSmall = errors.New("data too small for NlMsgHdr")
)

// DeserializeNlMsgHdrLengthAndType does a binary read of Length and Type
// It does a basic length check
func DeserializeNlMsgHdr(data []byte, nlmsghr *NlMsgHdr) (n int, err error) {

	if len(data) < NlMsgHdrSizeCst {
		return 0, ErrNlMsgHdrSmall
	}

	nlmsghr.Len = binary.LittleEndian.Uint32(data[0:4])
	nlmsghr.Type = binary.LittleEndian.Uint16(data[4:6])
	nlmsghr.Flags = binary.LittleEndian.Uint16(data[6:8])
	nlmsghr.Seq = binary.LittleEndian.Uint32(data[8:12])
	nlmsghr.Pid = binary.LittleEndian.Uint32(data[12:16])

	return NlMsgHdrReadCst, nil
}

func DeserializeNlMsgHdrRelection(data []byte, nlmsghr *NlMsgHdr) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, nlmsghr)
	if err != nil {
		return 0, err
	}

	return NlMsgHdrReadCst, err
}
