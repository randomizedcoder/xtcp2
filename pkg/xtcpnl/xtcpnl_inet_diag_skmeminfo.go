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

// INET_DIAG_SKMEMINFO 7

// Described here:
// http://man7.org/linux/man-pages/man7/sock_diag.7.html
// https://github.com/torvalds/linux/blob/a811c1fa0a02c062555b54651065899437bacdbe/net/core/sock.c#L3226
//
//	struct sk_meminfo {
//	    __u32   rmem_alloc; //The amount of data in receive queue
//	    __u32   rcv_buf;    //The receive socket buffer as set by SO_RCVBUF.
//	    __u32   wmem_alloc; //The amount of data in send queue.
//	    __u32   snd_buf;    //The send socket buffer as set by SO_SNDBUF.
//	    __u32   fwd_alloc;  //The amount of memory scheduled for future use (TCP only).
//	    __u32   wmem_queued;//The amount of data queued by TCP, but not yet sent.
//	    __u32   optmem;     //The amount of memory allocated for the sockets service needs
//	    __u32   backlog;    //The amount of packets in the backlog (not yet processed).
//	    __u32   drops;
//	};
type SkMemInfo struct {
	RmemAlloc  uint32 // 4 = 4
	RcvBuf     uint32 // 4 = 8
	WmemAlloc  uint32 // 4 = 12
	SndBuf     uint32 // 4 = 16
	FwdAlloc   uint32 // 4 = 20
	WmemQueued uint32 // 4 = 24
	Optmem     uint32 // 4 = 28
	Backlog    uint32 // 4 = 32
	Drops      uint32 // 4 = 36
}

const (
	SkMemInfoSizeCst    = 36
	SkMemInfoMinSizeCst = SkMemInfoSizeCst

	SkMemInfoEnumValueCst = 7
)

var (
	ErrSkMemInfoSmall = errors.New("data too small for SkMemInfo")
)

// DeserializeSkMemInfo does a binary read of a SkMemInfo
// It does a basic length check
func DeserializeSkMemInfo(data []byte, sm *SkMemInfo) (n int, err error) {

	if len(data) < SkMemInfoMinSizeCst {
		return 0, ErrSkMemInfoSmall
	}

	sm.RmemAlloc = binary.LittleEndian.Uint32(data[0:4])
	sm.RcvBuf = binary.LittleEndian.Uint32(data[4:8])
	sm.WmemAlloc = binary.LittleEndian.Uint32(data[8:12])
	sm.SndBuf = binary.LittleEndian.Uint32(data[12:16])
	sm.FwdAlloc = binary.LittleEndian.Uint32(data[16:20])
	sm.WmemQueued = binary.LittleEndian.Uint32(data[20:24])
	sm.Optmem = binary.LittleEndian.Uint32(data[24:28])
	sm.Backlog = binary.LittleEndian.Uint32(data[28:32])
	sm.Drops = binary.LittleEndian.Uint32(data[32:36])

	return SkMemInfoSizeCst, nil
}

func DeserializeSkMemInfoReflection(data []byte, sm *SkMemInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, sm)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func DeserializeSkMemInfoXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {

	if len(data) < SkMemInfoMinSizeCst {
		return ErrSkMemInfoSmall
	}

	x.SkMemInfoRmemAlloc = binary.LittleEndian.Uint32(data[0:4])
	x.SkMemInfoRcvBuf = binary.LittleEndian.Uint32(data[4:8])
	x.SkMemInfoWmemAlloc = binary.LittleEndian.Uint32(data[8:12])
	x.SkMemInfoSndBuf = binary.LittleEndian.Uint32(data[12:16])
	x.SkMemInfoFwdAlloc = binary.LittleEndian.Uint32(data[16:20])
	x.SkMemInfoWmemQueued = binary.LittleEndian.Uint32(data[20:24])
	x.SkMemInfoOptmem = binary.LittleEndian.Uint32(data[24:28])
	x.SkMemInfoBacklog = binary.LittleEndian.Uint32(data[28:32])
	x.SkMemInfoDrops = binary.LittleEndian.Uint32(data[32:36])

	return nil
}
