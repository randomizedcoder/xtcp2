package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// NdMsg mirrors the kernel's `struct ndmsg` — the family header of an RTM_*NEIGH
// message (the ARP / NDISC neighbor table).
//
//	struct ndmsg {
//		__u8		ndm_family;
//		__u8		ndm_pad1;
//		__u16		ndm_pad2;
//		__s32		ndm_ifindex;
//		__u16		ndm_state;
//		__u8		ndm_flags;
//		__u8		ndm_type;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbor.h
type NdMsg struct {
	Family  uint8  // 1
	Pad1    uint8  // 1
	Pad2    uint16 // 2
	Ifindex int32  // 4 = 8
	State   uint16 // 2 = 10
	Flags   uint8  // 1
	Type    uint8  // 1 = 12 ( 12 / 4 = 3 )
}

const (
	NdMsgSizeCst = 12
	NdMsgReadCst = NdMsgSizeCst

	// NdaCacheInfoSizeCst is `struct nda_cacheinfo`: four __u32 counters.
	NdaCacheInfoSizeCst = 16
)

var (
	ErrNdMsgSmall        = errors.New("data too small for NdMsg")
	ErrNdaCacheInfoSmall = errors.New("data too small for NdaCacheInfo")
)

// DeserializeNdMsg does a binary read of an NdMsg with a basic length check.
func DeserializeNdMsg(data []byte, m *NdMsg) (n int, err error) {
	if len(data) < NdMsgSizeCst {
		return 0, ErrNdMsgSmall
	}

	m.Family = data[0]
	m.Pad1 = data[1]
	m.Pad2 = binary.LittleEndian.Uint16(data[2:4])
	m.Ifindex = int32(binary.LittleEndian.Uint32(data[4:8]))
	m.State = binary.LittleEndian.Uint16(data[8:10])
	m.Flags = data[10]
	m.Type = data[11]

	return NdMsgReadCst, nil
}

func DeserializeNdMsgReflection(data []byte, m *NdMsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return NdMsgReadCst, err
}

// NdaCacheInfo mirrors the kernel's `struct nda_cacheinfo` (NDA_CACHEINFO): the
// neighbor entry's age counters, in units of USER_HZ.
//
//	struct nda_cacheinfo {
//		__u32		ndm_confirmed;
//		__u32		ndm_used;
//		__u32		ndm_updated;
//		__u32		ndm_refcnt;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbor.h
type NdaCacheInfo struct {
	Confirmed uint32 // 4 = 4
	Used      uint32 // 4 = 8
	Updated   uint32 // 4 = 12
	Refcnt    uint32 // 4 = 16 ( 16 / 4 = 4 )
}

// DeserializeNdaCacheInfo does a binary read of an NdaCacheInfo with a basic
// length check.
func DeserializeNdaCacheInfo(data []byte, c *NdaCacheInfo) (n int, err error) {
	if len(data) < NdaCacheInfoSizeCst {
		return 0, ErrNdaCacheInfoSmall
	}

	c.Confirmed = binary.LittleEndian.Uint32(data[0:4])
	c.Used = binary.LittleEndian.Uint32(data[4:8])
	c.Updated = binary.LittleEndian.Uint32(data[8:12])
	c.Refcnt = binary.LittleEndian.Uint32(data[12:16])

	return NdaCacheInfoSizeCst, nil
}

func DeserializeNdaCacheInfoReflection(data []byte, c *NdaCacheInfo) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}

	return NdaCacheInfoSizeCst, err
}

// NeighInfo is the subset of an RTM_*NEIGH message xtcp2 keeps. Dst holds the
// raw network-order protocol address (4 bytes for IPv4, 16 for IPv6) and LLAddr
// the link-layer address (6 bytes for Ethernet, but 0 for a link with no
// address, so length must not be assumed).
//
// State is the NUD_* bitmask, not an enum — see NudStateString. A RTM_DELNEIGH
// commonly arrives with State NUD_FAILED rather than the entry's last good
// state, so do not infer reachability from a delete.
type NeighInfo struct {
	Family       uint8
	Ifindex      int32
	State        uint16
	Flags        uint8
	Type         uint8
	Dst          []byte // NDA_DST
	LLAddr       []byte // NDA_LLADDR
	HasCacheInfo bool   // NDA_CACHEINFO present and well-formed
	CacheInfo    NdaCacheInfo
}

// nudStateNames maps each single NUD_* bit to its kernel name. ndm_state is a
// bitmask, so more than one can be set.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbor.h
var nudStateNames = []struct {
	bit  uint16
	name string
}{
	{unix.NUD_INCOMPLETE, "NUD_INCOMPLETE"},
	{unix.NUD_REACHABLE, "NUD_REACHABLE"},
	{unix.NUD_STALE, "NUD_STALE"},
	{unix.NUD_DELAY, "NUD_DELAY"},
	{unix.NUD_PROBE, "NUD_PROBE"},
	{unix.NUD_FAILED, "NUD_FAILED"},
	{unix.NUD_NOARP, "NUD_NOARP"},
	{unix.NUD_PERMANENT, "NUD_PERMANENT"},
}

// NudStateString renders an ndm_state bitmask as a `|`-joined list of NUD_*
// names, for logs and test failure messages. A zero state is "NUD_NONE"; bits
// outside the known set are reported as a hex remainder so nothing is silently
// dropped.
func NudStateString(state uint16) string {
	if state == 0 {
		return "NUD_NONE"
	}

	var (
		names     []string
		remaining = state
	)
	for _, s := range nudStateNames {
		if state&s.bit != 0 {
			names = append(names, s.name)
			remaining &^= s.bit
		}
	}
	if remaining != 0 {
		names = append(names, "0x"+strconv.FormatUint(uint64(remaining), 16))
	}
	return strings.Join(names, "|")
}

// IsReachable reports whether the neighbor entry can currently be used to send
// without first resolving: NUD_REACHABLE, NUD_PERMANENT and NUD_NOARP all mean
// the link-layer address is valid. NUD_STALE is deliberately excluded — the
// entry is usable but the kernel will revalidate it.
func (ni NeighInfo) IsReachable() bool {
	const usable = unix.NUD_REACHABLE | unix.NUD_PERMANENT | unix.NUD_NOARP
	return ni.State&usable != 0
}

// ParseNeigh decodes an RTM_NEWNEIGH / RTM_DELNEIGH message body (the bytes
// after the nlmsghdr): the ndmsg header followed by NDA_* attributes.
//
// A malformed NDA_CACHEINFO is tolerated (HasCacheInfo stays false) rather than
// failing the whole message: the counters are advisory and an entry is still
// useful without them.
func ParseNeigh(body []byte) (NeighInfo, error) {
	var m NdMsg
	if _, err := DeserializeNdMsg(body, &m); err != nil {
		return NeighInfo{}, err
	}

	ni := NeighInfo{
		Family:  m.Family,
		Ifindex: m.Ifindex,
		State:   m.State,
		Flags:   m.Flags,
		Type:    m.Type,
	}
	err := walkRTAttrs(body[NdMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.NDA_DST):
			ni.Dst = copyBytes(val)
		case uint16(unix.NDA_LLADDR):
			ni.LLAddr = copyBytes(val)
		case uint16(unix.NDA_CACHEINFO):
			if _, cerr := DeserializeNdaCacheInfo(val, &ni.CacheInfo); cerr == nil {
				ni.HasCacheInfo = true
			}
		}
	})
	if err != nil {
		return NeighInfo{}, err
	}
	return ni, nil
}
