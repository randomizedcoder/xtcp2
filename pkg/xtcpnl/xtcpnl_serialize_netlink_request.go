package xtcpnl

import (
	"encoding/binary"
)

const (
	//sdebugLevel int = 11

	InetDiagRequestSizeCst = 72
	SocketDiagByFamilyCst  = 20

	TCPAllStatesCst = 4282318848
)

func SerializeNetlinkDagRequest(nlh NlMsgHdr, req InetDiagReqV2, b *[]byte) {

	// len
	binary.LittleEndian.PutUint32(
		(*b)[0:4],
		nlh.Len,
	) // constant hack for the length

	// type
	binary.LittleEndian.PutUint16(
		(*b)[4:6],
		nlh.Type,
		//uint16(SocketDiagByFamilyCst),
	) // #define SOCK_DIAG_BY_FAMILY 20  in uapi/linux/sock_diag.h

	// flags
	// https://pkg.go.dev/syscall#NLM_F_REQUEST
	// binary.LittleEndian.PutUint16(packetBytes[6:8], uint16(syscall.NLM_F_DUMP|syscall.NLM_F_REQUEST))
	binary.LittleEndian.PutUint16(
		(*b)[6:8],
		nlh.Flags,
		//uint16(syscall.NLM_F_DUMP|syscall.NLM_F_REQUEST|syscall.NLM_F_REPLACE|syscall.NLM_F_EXCL),
	)

	// seq
	binary.LittleEndian.PutUint32(
		(*b)[8:12],
		nlh.Seq,
	)

	// pid - leave this blank
	//binary.LittleEndian.PutUint32(packetBytes[12:16], uint32(r.NlMsgPid)) // not using pid

	// // There is no PutUint8
	// // family
	// *(*uint8)(unsafe.Pointer(&b[16:17][0])) = uint8(req.SDiagFamily) // #define AF_INET      2
	(*b)[16] = byte(req.SDiagFamily)
	// // protocol
	// //*(*uint8)(unsafe.Pointer(&b[17:18][0])) = uint8(unix.IPPROTO_TCP) // IPPROTO_TCP = 6
	// *(*uint8)(unsafe.Pointer(&b[17:18][0])) = uint8(req.SDiagProtocol) // IPPROTO_TCP = 6
	(*b)[17] = byte(req.SDiagProtocol)

	// // ext
	// *(*uint8)(unsafe.Pointer(&b[18:19][0])) = uint8(req.IDiagExt)
	(*b)[18] = byte(req.IDiagExt)
	// // pad
	// *(*uint8)(unsafe.Pointer(&b[19:20][0])) = uint8(0) // pad

	// states
	// https://github.com/torvalds/linux/blob/5ad7ff8738b8bd238ca899df08badb1f61bcc39e/include/net/tcp_states.h#L4
	// https://github.com/torvalds/linux/blob/2f4c53349961c8ca480193e47da4d44fdb8335a8/include/net/tcp_states.h
	binary.BigEndian.PutUint32(
		(*b)[20:24],
		req.IDiagStates,
	)

	// inet_diag_sockid
	// Leave this blank, go has already zero-ed for us

}
