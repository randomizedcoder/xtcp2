package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

//https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L38
// struct inet_diag_req_v2 {
// 	__u8	sdiag_family;
// 	__u8	sdiag_protocol;
// 	__u8	idiag_ext;
// 	__u8	pad;
// 	__u32	idiag_states;
// 	struct inet_diag_sockid id;
// }; 1+1+1+1+4=8

type InetDiagReqV2 struct {
	SDiagFamily   uint8          // 1 = 1
	SDiagProtocol uint8          // 1 = 2
	IDiagExt      uint8          // 1 = 3
	Pad           uint8          // 1 = 4
	IDiagStates   uint32         // 4 = 8
	SocketID      InetDiagSockID // 48 = 56 ( 56 / 4 = 14 )
}

const (
	InetDiagReqV2SizeCst = 56
	InetDiagReqV2ReadCst = 55 // we don't read pad
)

var (
	ErrInetDiagReqV2Small = errors.New("data too small for InetDiagReqV2")
)

// DeserializeInetDiagReqV2 does a binary read of a InetDiagReqV2
// It does a basic length check
func DeserializeInetDiagReqV2(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error) {

	if len(data) < InetDiagReqV2SizeCst {
		return 0, ErrInetDiagReqV2Small
	}

	inetdiagreqv2.SDiagFamily = data[0]
	inetdiagreqv2.SDiagProtocol = data[1]
	inetdiagreqv2.IDiagExt = data[2]

	// Don't bother grabbing pad
	//inetdiagreqv2.Pad = data[3]

	inetdiagreqv2.IDiagStates = binary.BigEndian.Uint32(data[4:8])

	_, errD := DeserializeInetDiagSockID(data[4:4+InetDiagSockIDSizeCst], s)
	if errD != nil {
		return 0, errD
	}

	inetdiagreqv2.SocketID = *s

	return InetDiagReqV2ReadCst, nil
}

func DeserializeInetDiagReqV2Relection(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, inetdiagreqv2)
	if err != nil {
		return 0, err
	}

	return InetDiagReqV2SizeCst, err
}
