package xtcpnl

import (
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

	// NdMsgFlagsOffCst is the byte offset of ndm_family's third neighbor:
	// ndm_flags, at 10, after the 4-byte ndm_ifindex at 4 and the 2-byte
	// ndm_state at 8. It is the only member of this struct that a REQUEST
	// ever sets — `ip neigh show proxy` writes NTF_PROXY here and nothing
	// else (ip/ipneigh.c:490) — so the builder needs it by offset while the
	// decoder below reads it by field. Naming it keeps the two from drifting.
	NdMsgFlagsOffCst = 10

	// NdaCacheInfoSizeCst is `struct nda_cacheinfo`: four __u32 counters.
	NdaCacheInfoSizeCst = 16
)

// NdaFlagsExt is NDA_FLAGS_EXT from include/uapi/linux/neighbour.h (Linux
// 5.16+): a __u32 of flags that did not fit in the 8-bit ndm_flags. Declared
// here because golang.org/x/sys/unix does not export it, as RtaNhID is in
// xtcpnl_rtmsg.go.
//
// The value is 15, and the arithmetic is worth writing down because x/sys is
// no help in checking it: its NDA_* run stops at NDA_SRC_VNI = 11, four short.
// The enum continues NDA_PROTOCOL 12, NDA_NH_ID 13, NDA_FDB_EXT_ATTRS 14,
// NDA_FLAGS_EXT 15. TestNdaFlagsExtValue pins that offset against the last
// constant x/sys does define, so a miscount fails a test rather than silently
// decoding NDA_FDB_EXT_ATTRS — a nested attribute whose first four bytes would
// parse as a perfectly plausible flag word.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbour.h
const NdaFlagsExt uint16 = 15

// NTF_EXT_* are the NDA_FLAGS_EXT bits. They share no numbering with the
// ndm_flags NTF_* constants — both sets start at bit 0 — so the two must never
// be tested against the same word. That is the whole reason NeighInfo keeps
// FlagsExt separate from Flags rather than widening one field: NTF_EXT_MANAGED
// and NTF_USE are both 1.
//
// Only two of the three are printed by `ip neigh`. NTF_EXT_LOCKED appears in
// iproute2 exactly once, at bridge/fdb.c:121, and never in ip/ipneigh.c — it
// describes a bridge FDB entry rather than a neighbor — so it is declared for
// completeness of the UAPI and deliberately has no token in the renderer.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/neighbour.h
const (
	NtfExtManaged      uint32 = 1 << 0
	NtfExtLocked       uint32 = 1 << 1
	NtfExtExtValidated uint32 = 1 << 2
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
	m.Flags = data[NdMsgFlagsOffCst]
	m.Type = data[11]

	return NdMsgReadCst, nil
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

// NeighInfo is the subset of an RTM_*NEIGH message xtcp2 keeps. Dst holds the
// raw network-order protocol address (4 bytes for IPv4, 16 for IPv6) and LLAddr
// the link-layer address (6 bytes for Ethernet, but 0 for a link with no
// address, so length must not be assumed).
//
// State is the NUD_* bitmask, not an enum — see NudStateString. A RTM_DELNEIGH
// commonly arrives with State NUD_FAILED rather than the entry's last good
// state, so do not infer reachability from a delete.
type NeighInfo struct {
	Family  uint8
	Ifindex int32
	State   uint16
	Flags   uint8
	Type    uint8
	Dst     []byte // NDA_DST
	LLAddr  []byte // NDA_LLADDR

	// FlagsExt is NDA_FLAGS_EXT, the NTF_EXT_* word. It is a separate field
	// from Flags and not a widening of it because the two bit sets overlap
	// numerically — see the NtfExt* constants.
	//
	// There is no HasFlagsExt companion, and the asymmetry with HasCacheInfo
	// is deliberate rather than an oversight. print_neigh initializes
	// ext_flags to 0 and overwrites it only when the attribute is present
	// (ip/ipneigh.c:356-357), then tests individual bits; absent and
	// present-but-zero take the same branch at every use, so a presence bit
	// would be a field no caller could act on. HasCacheInfo earns its keep
	// because a zero nda_cacheinfo is a real, printable value under `-s`.
	FlagsExt uint32 // NDA_FLAGS_EXT

	HasCacheInfo bool // NDA_CACHEINFO present and well-formed
	CacheInfo    NdaCacheInfo

	// Probes is NDA_PROBES, the count of unanswered solicitations for this
	// entry, and HasProbes records its presence.
	//
	// The presence bit is load-bearing here for the same reason as
	// HasCacheInfo and unlike FlagsExt above: print_neigh emits the token
	// whenever the attribute exists, with no test on its value
	// (ip/ipneigh.c:457-459), so `probes 0` and no token at all are two
	// different outputs for two different wire states.
	Probes    uint32 // NDA_PROBES
	HasProbes bool   // NDA_PROBES present
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
	var seen attrSeen
	err := WalkRTAttrs(body[NdMsgSizeCst:], func(atype uint16, val []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case uint16(unix.NDA_DST):
			ni.Dst = CopyBytes(val)
		case uint16(unix.NDA_LLADDR):
			ni.LLAddr = CopyBytes(val)
		case uint16(unix.NDA_CACHEINFO):
			if _, cerr := DeserializeNdaCacheInfo(val, &ni.CacheInfo); cerr == nil {
				ni.HasCacheInfo = true
			}
		case uint16(unix.NDA_PROBES):
			// Same length guard as NDA_FLAGS_EXT below, and the same
			// divergence from iproute2's unchecked rta_getattr_u32. Note the
			// presence bit is set only for a well-formed attribute, so a
			// short NDA_PROBES renders no token here while `ip` would print
			// one built from over-read bytes.
			if len(val) >= 4 {
				ni.Probes = binary.LittleEndian.Uint32(val[0:4])
				ni.HasProbes = true
			}
		case NdaFlagsExt:
			// The length guard is this package's convention for a u32
			// attribute and it is stricter than iproute2, which reaches
			// straight for rta_getattr_u32 with no check at all. A short
			// NDA_FLAGS_EXT would over-read in `ip` and decode as zero here;
			// the kernel never emits one, so the divergence is confined to a
			// malformed message, where being the defensive one is correct.
			if len(val) >= 4 {
				ni.FlagsExt = binary.LittleEndian.Uint32(val[0:4])
			}
		}
	})
	if err != nil {
		return NeighInfo{}, err
	}
	return ni, nil
}
