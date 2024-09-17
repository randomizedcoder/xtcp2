package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
)

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L115
// https://github.com/iproute2/iproute2/blob/main/include/uapi/linux/inet_diag.h#L117

// // Base info structure. It contains socket identity (addrs/ports/cookie)
// // and, alas, the information shown by netstat.
// struct inet_diag_msg {
// 	__u8	idiag_family;
// 	__u8	idiag_state;
// 	__u8	idiag_timer;
// 	__u8	idiag_retrans;

// 	struct inet_diag_sockid id;

// 	__u32	idiag_expires;
// 	__u32	idiag_rqueue;
// 	__u32	idiag_wqueue;
// 	__u32	idiag_uid;
// 	__u32	idiag_inode;
// };

// Timer
// https://www.man7.org/linux/man-pages/man7/sock_diag.7.html
// idiag_timer
// 0      no timer is active
// 1      a retransmit timer
// 2      a keep-alive timer
// 3      a TIME_WAIT timer
// 4      a zero window probe timer
// idiag_retrans
// For idiag_timer values 1, 2, and 4, this field contains
// the number of retransmits.  For other idiag_timer values,
// this field is set to 0.

type InetDiagMsg struct {
	Family   uint8          // 1 = 1 [0]
	State    uint8          // 1 = 2 [1]
	Timer    uint8          // 1 = 3 [2]
	Retrans  uint8          // 1 = 4 [3]
	SocketID InetDiagSockID // 44 = 48 [4:48]
	Expires  uint32         // 4 = 56 [52:56]
	Rqueue   uint32         // 4 = 60 [56:60]
	Wqueue   uint32         // 4 = 64 [60:64]
	UID      uint32         // 4 = 68 [64:68]
	Inode    uint32         // 4 = 72 [68:72] ( 72 / 4 = 18 )
}

const (
	InetDiagMsgSizeCst                = 72
	InetDiagMsgBytesBeforeSocketIDCst = 4
	InetDiagMsgReadCst                = InetDiagMsgSizeCst
)

var (
	ErrInetDiagMsgSmall = errors.New("data too small for InetDiagMsg")
)

func DeserializeInetDiagMsgWG(wg *sync.WaitGroup, data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
	defer wg.Done()
	return DeserializeInetDiagMsg(data, idm, s)
}

// DeserializeInetDiagMsg does a binary read of a InetDiagMsg
// It does a basic length check
func DeserializeInetDiagMsg(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {

	if len(data) < InetDiagMsgSizeCst {
		return 0, ErrInetDiagMsgSmall
	}

	//log.Printf("Expires 0:4 hex:%s", hex.EncodeToString(data[0:4]))
	idm.Family = data[0]
	idm.State = data[1]
	idm.Timer = data[2]
	idm.Retrans = data[3]

	//log.Printf("sock 4:%d hex:%s", 4+InetDiagSockIDSizeCst, hex.EncodeToString(data[4:4+InetDiagSockIDSizeCst]))
	_, errD := DeserializeInetDiagSockID(data[4:4+InetDiagSockIDSizeCst], s)
	if errD != nil {
		return 0, errD
	}

	idm.SocketID = *s

	//log.Printf("Expires 52:56 hex:%s", hex.EncodeToString(data[52:56]))
	idm.Expires = binary.LittleEndian.Uint32(data[52:56])

	//log.Printf("Rqueue 52:56 hex:%s", hex.EncodeToString(data[56:60]))
	idm.Rqueue = binary.LittleEndian.Uint32(data[56:60])

	//log.Printf("Wqueue 60:64 hex:%s", hex.EncodeToString(data[60:64]))
	idm.Wqueue = binary.LittleEndian.Uint32(data[60:64])

	//log.Printf("UID 64:68 hex:%s", hex.EncodeToString(data[64:68]))
	idm.UID = binary.LittleEndian.Uint32(data[64:68])

	//log.Printf("Inode 68:72 hex:%s", hex.EncodeToString(data[68:72]))
	idm.Inode = binary.LittleEndian.Uint32(data[68:72])

	return InetDiagMsgReadCst, nil
}

func DeserializeInetDiagMsgViaReflection(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, idm)
	if err != nil {
		return 0, err
	}

	return InetDiagMsgReadCst, err
}

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h#L14
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L14C1-L22C3
// https://github.com/iproute2/iproute2/blob/b176b9f40368735b5bd4e6d49f8ebcbe8b8bef4a/include/uapi/linux/inet_diag.h#L14

// struct inet_diag_sockid {
// 	__be16	idiag_sport;
// 	__be16	idiag_dport;
// 	__be32	idiag_src[4];
// 	__be32	idiag_dst[4];
// 	__u32	idiag_if;
// 	__u32	idiag_cookie[2];
// #define INET_DIAG_NOCOOKIE (~0U)
// };

type InetDiagSockID struct {
	SPort     uint16   // 2 = 2
	DPort     uint16   // 2 = 4
	SrcIP     [16]byte // 16 = 20
	DstIP     [16]byte // 16 = 36
	Interface uint32   // 4 = 40
	Cookie    uint64   // 8 = 48 ( 48 / 4 = 12 )
}

const (
	InetDiagSockIDSizeCst = 48
	InetDiagSockIDReadCst = InetDiagSockIDSizeCst
)

var (
	ErrInetDiagSockIDSmall = errors.New("data too small for InetDiagSockID")
)

// DeserializeInetDiagReqV2 does a binary read of a InetDiagReqV2
// It does a basic length check
// https://github.com/wireshark/wireshark/blob/b24016e0b27bcbc43ba2a269e465eb0f03751b1f/epan/dissectors/packet-netlink-sock_diag.c#L24
func DeserializeInetDiagSockID(data []byte, sockid *InetDiagSockID) (n int, err error) {

	if len(data) < InetDiagSockIDSizeCst {
		return 0, ErrInetDiagSockIDSmall
	}

	sockid.SPort = binary.BigEndian.Uint16(data[0:2])
	sockid.DPort = binary.BigEndian.Uint16(data[2:4])

	// Keep in mind the IPv4 bits are at the start/left
	sockid.SrcIP = *((*[16]byte)(data[4:40]))
	sockid.DstIP = *((*[16]byte)(data[20:36]))

	sockid.Interface = binary.LittleEndian.Uint32(data[36:40])

	// https://github.com/iproute2/iproute2/blob/main/misc/ss.c#L783
	// static unsigned long long cookie_sk_get(const uint32_t *cookie)
	// {
	// 	return (((unsigned long long)cookie[1] << 31) << 1) | cookie[0];
	// }

	sockid.Cookie = binary.LittleEndian.Uint64(data[40:48])

	return InetDiagSockIDReadCst, nil
}

func DeserializeInetDiagSockIDReflection(data []byte, sockid *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, sockid)
	if err != nil {
		return 0, err
	}

	return InetDiagSockIDReadCst, err
}

// XTCP

func DeserializeInetDiagMsgXTCPWG(wg *sync.WaitGroup, data []byte, x *xtcppb.FlatXtcpRecord) (err error) {
	defer wg.Done()
	return DeserializeInetDiagMsgXTCP(data, x)
}

func DeserializeInetDiagMsgXTCP(data []byte, x *xtcppb.FlatXtcpRecord) (err error) {

	if len(data) < InetDiagMsgSizeCst {
		return ErrInetDiagMsgSmall
	}

	//log.Printf("Expires 0:4 hex:%s", hex.EncodeToString(data[0:4]))
	x.InetDiagMsgFamily = uint32(data[0])
	x.InetDiagMsgState = uint32(data[1])
	x.InetDiagMsgTimer = uint32(data[2])
	x.InetDiagMsgRetrans = uint32(data[3])

	//log.Printf("sock 4:%d hex:%s", 4+InetDiagSockIDSizeCst, hex.EncodeToString(data[4:4+InetDiagSockIDSizeCst]))
	errD := DeserializeInetDiagSockIDXTCP(data[4:4+InetDiagSockIDSizeCst], x)
	if errD != nil {
		return errD
	}

	//log.Printf("Expires 52:56 hex:%s", hex.EncodeToString(data[52:56]))
	x.InetDiagMsgExpires = binary.LittleEndian.Uint32(data[52:56])

	//log.Printf("Rqueue 52:56 hex:%s", hex.EncodeToString(data[56:60]))
	x.InetDiagMsgRqueue = binary.LittleEndian.Uint32(data[56:60])

	//log.Printf("Wqueue 60:64 hex:%s", hex.EncodeToString(data[60:64]))
	x.InetDiagMsgWqueue = binary.LittleEndian.Uint32(data[60:64])

	//log.Printf("UID 64:68 hex:%s", hex.EncodeToString(data[64:68]))
	x.InetDiagMsgUid = binary.LittleEndian.Uint32(data[64:68])

	//log.Printf("Inode 68:72 hex:%s", hex.EncodeToString(data[68:72]))
	x.InetDiagMsgInode = binary.LittleEndian.Uint32(data[68:72])

	return nil
}

func DeserializeInetDiagSockIDXTCP(data []byte, x *xtcppb.FlatXtcpRecord) (err error) {

	if len(data) < InetDiagSockIDSizeCst {
		return ErrInetDiagSockIDSmall
	}

	x.InetDiagMsgSocketSourcePort = uint32(binary.BigEndian.Uint16(data[0:2]))
	x.InetDiagMsgSocketDestinationPort = uint32(binary.BigEndian.Uint16(data[2:4]))

	// Keep in mind the IPv4 bits are at the start/left
	x.InetDiagMsgSocketSource = data[4:40]
	x.InetDiagMsgSocketDestination = data[20:36]

	x.InetDiagMsgSocketInterface = binary.LittleEndian.Uint32(data[36:40])

	// https://github.com/iproute2/iproute2/blob/main/misc/ss.c#L783
	// static unsigned long long cookie_sk_get(const uint32_t *cookie)
	// {
	// 	return (((unsigned long long)cookie[1] << 31) << 1) | cookie[0];
	// }

	x.InetDiagMsgSocketCookie = binary.LittleEndian.Uint64(data[40:48])

	return nil
}
