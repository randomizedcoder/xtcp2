package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// IfAddrmsg mirrors the kernel's `struct ifaddrmsg` — the family header of an
// RTM_*ADDR message.
//
//	struct ifaddrmsg {
//		__u8	ifa_family;
//		__u8	ifa_prefixlen;	/* The prefix length		*/
//		__u8	ifa_flags;	/* Flags			*/
//		__u8	ifa_scope;	/* Address scope		*/
//		__u32	ifa_index;	/* Link index			*/
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
type IfAddrmsg struct {
	Family    uint8  // 1
	Prefixlen uint8  // 1
	Flags     uint8  // 1
	Scope     uint8  // 1
	Index     uint32 // 4 = 8 ( 8 / 4 = 2 )
}

const (
	IfAddrmsgSizeCst = 8
	IfAddrmsgReadCst = IfAddrmsgSizeCst
)

var (
	ErrIfAddrmsgSmall = errors.New("data too small for IfAddrmsg")
)

// DeserializeIfAddrmsg does a binary read of an IfAddrmsg with a basic length
// check.
func DeserializeIfAddrmsg(data []byte, m *IfAddrmsg) (n int, err error) {
	if len(data) < IfAddrmsgSizeCst {
		return 0, ErrIfAddrmsgSmall
	}

	m.Family = data[0]
	m.Prefixlen = data[1]
	m.Flags = data[2]
	m.Scope = data[3]
	m.Index = binary.LittleEndian.Uint32(data[4:8])

	return IfAddrmsgReadCst, nil
}

// IfaCacheinfo mirrors the kernel's `struct ifa_cacheinfo`, the payload of
// IFA_CACHEINFO — an address's validity lifetimes and the timestamps behind
// `ip addr show`'s "valid_lft 47871sec preferred_lft 47871sec" line.
//
//	struct ifa_cacheinfo {
//		__u32	ifa_prefered;
//		__u32	ifa_valid;
//		__u32	cstamp; /* created timestamp, hundredths of seconds */
//		__u32	tstamp; /* updated timestamp, hundredths of seconds */
//	};
//
// The Go field below is Preferred with two r's. The kernel member is spelled
// with one, as transcribed above, and has been since the struct was added; both
// names refer to the same four bytes.
//
// 0xffffffff is INFINITY_LIFE_TIME, which `ip` renders as "forever" rather than
// as a number. cstamp and tstamp are hundredths of a second since boot, so they
// are the fields a parity comparison must normalize — two runs of the same
// command are guaranteed to disagree on them.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
type IfaCacheinfo struct {
	Preferred uint32 // 4
	Valid     uint32 // 4
	Cstamp    uint32 // 4
	Tstamp    uint32 // 4 = 16 ( 16 / 4 = 4 )
}

const (
	IfaCacheinfoSizeCst = 16
	IfaCacheinfoReadCst = IfaCacheinfoSizeCst

	// IfaLifetimeInfinityCst is the kernel's INFINITY_LIFE_TIME, the value
	// both lifetimes carry for a permanent address. `ip` prints "forever".
	//
	// linux/include/net/addrconf.h INFINITY_LIFE_TIME
	IfaLifetimeInfinityCst uint32 = 0xffffffff
)

var (
	ErrIfaCacheinfoSmall = errors.New("data too small for IfaCacheinfo")
)

// DeserializeIfaCacheinfo does a binary read of an IfaCacheinfo with a basic
// length check.
func DeserializeIfaCacheinfo(data []byte, m *IfaCacheinfo) (n int, err error) {
	if len(data) < IfaCacheinfoSizeCst {
		return 0, ErrIfaCacheinfoSmall
	}

	m.Preferred = binary.LittleEndian.Uint32(data[0:4])
	m.Valid = binary.LittleEndian.Uint32(data[4:8])
	m.Cstamp = binary.LittleEndian.Uint32(data[8:12])
	m.Tstamp = binary.LittleEndian.Uint32(data[12:16])

	return IfaCacheinfoReadCst, nil
}

// IfaProto is IFA_PROTO, the attribute carrying an address's origin, and
// IFAPROT_* are its values — what `ip` renders as "proto kernel_ll" and
// friends. golang.org/x/sys/unix v0.47.0 exports neither (verified: its IFA_*
// set stops at IFA_TARGET_NETNSID), so both are declared here from the kernel
// UAPI, as RtaNhID is in xtcpnl_rtmsg.go.
//
// Seven of the fifteen addresses in the committed IPv6 dump carry it, and
// ip_addr_n:27 is the line it renders.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
const (
	IfaProto uint16 = 11

	IfaProtoUnspec   uint8 = 0
	IfaProtoKernelLo uint8 = 1 // loopback
	IfaProtoKernelRA uint8 = 2 // set by kernel from a router announcement
	IfaProtoKernelLL uint8 = 3 // link-local, set by kernel
)

// AddrInfo is the subset of an RTM_NEWADDR message xtcp2 keeps. The prefix
// length comes from the ifaddrmsg header (not an attribute). Address/Local hold
// the raw network-order address bytes (4 for IPv4, 16 for IPv6); the caller
// builds a netip.Addr from them. For IPv4 the kernel sends both IFA_LOCAL (the
// local address) and IFA_ADDRESS (the peer, on point-to-point links); when they
// differ, Local is authoritative for "this host's address".
//
// # Flags is 32-bit for a reason
//
// ifa_flags in the header is 8 bits, and the flag space outgrew it: IFA_F_*
// runs to IFA_F_STABLE_PRIVACY (0x800), so MANAGETEMPADDR (0x100),
// NOPREFIXROUTE (0x200), MCAUTOJOIN (0x400) and STABLE_PRIVACY cannot be
// expressed in the header field at all. IFA_FLAGS carries the full u32, and
// when present it **replaces** the header value rather than being OR-ed with
// it — iproute2's get_ifa_flags is `ifa_flags_attr ? rta_getattr_u32(...) :
// ifa->ifa_flags` (ip/ipaddress.c:1371-1376). Not cosmetic: the committed
// ip_addr_n:16 shows a real address rendering as "dynamic mngtmpaddr
// noprefixroute", and two of those three flags are invisible to the header
// field.
//
// CacheInfo/HasCacheInfo, Broadcast and Proto complete what an `ip addr show`
// line needs. Proto 0 is IFAPROT_UNSPEC, which is also what an absent
// IFA_PROTO leaves behind; `ip` prints the token only for a present, non-zero
// value, so the conflation changes nothing.
type AddrInfo struct {
	Family    uint8
	Prefixlen uint8
	Scope     uint8
	Index     uint32
	Address   []byte // IFA_ADDRESS
	Local     []byte // IFA_LOCAL
	Label     string // IFA_LABEL

	Flags        uint32       // header ifa_flags, REPLACED by IFA_FLAGS when present
	Broadcast    []byte       // IFA_BROADCAST; nil if absent
	Proto        uint8        // IFA_PROTO (IFAPROT_*); IfaProtoUnspec if absent
	CacheInfo    IfaCacheinfo // IFA_CACHEINFO; zero unless HasCacheInfo
	HasCacheInfo bool         // IFA_CACHEINFO present and long enough to decode
}

// IsPermanent reports whether the address never expires — both lifetimes at
// INFINITY_LIFE_TIME, which is the "valid_lft forever preferred_lft forever"
// line, or no IFA_CACHEINFO at all.
//
// An address with no cacheinfo is permanent by omission: the kernel sends the
// attribute precisely when there is a lifetime to report.
func (ai AddrInfo) IsPermanent() bool {
	if !ai.HasCacheInfo {
		return true
	}
	return ai.CacheInfo.Valid == IfaLifetimeInfinityCst &&
		ai.CacheInfo.Preferred == IfaLifetimeInfinityCst
}

// IsDeprecated reports an address past its preferred lifetime but still valid —
// `ip`'s "deprecated" token. The kernel also sets IFA_F_DEPRECATED, and this
// helper reads the lifetime instead so the two can be cross-checked:
// ip_addr_n:14 carries both on one address ("temporary deprecated dynamic",
// preferred_lft 0sec).
func (ai AddrInfo) IsDeprecated() bool {
	return ai.HasCacheInfo && ai.CacheInfo.Preferred == 0 &&
		ai.CacheInfo.Valid != 0
}

// ParseNewAddr decodes an RTM_NEWADDR message body (the bytes after the
// nlmsghdr): the ifaddrmsg header followed by IFA_* attributes.
//
// Two iproute2 behaviors are reproduced here rather than left to callers,
// because both change what a correct renderer prints:
//
//   - IFA_FLAGS replaces the 8-bit header field, it does not extend it.
//   - IFA_LOCAL and IFA_ADDRESS alias each other in **both** directions when
//     one is missing (ip/ipaddress.c:1531-1534). IPv6 replies carry only
//     IFA_ADDRESS — all 15 in the committed v6 dump do — so without the alias
//     every IPv6 address would decode with an empty Local.
//
// Duplicated attributes take the first occurrence; see
// xtcpnl_rtattr_firstwins.go.
func ParseNewAddr(body []byte) (AddrInfo, error) {
	var m IfAddrmsg
	if _, err := DeserializeIfAddrmsg(body, &m); err != nil {
		return AddrInfo{}, err
	}

	ai := AddrInfo{
		Family:    m.Family,
		Prefixlen: m.Prefixlen,
		Scope:     m.Scope,
		Index:     m.Index,
		Flags:     uint32(m.Flags),
	}
	var seen attrSeen
	err := WalkRTAttrs(body[IfAddrmsgSizeCst:], func(atype uint16, val []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case uint16(unix.IFA_ADDRESS):
			ai.Address = CopyBytes(val)
		case uint16(unix.IFA_LOCAL):
			ai.Local = CopyBytes(val)
		case uint16(unix.IFA_LABEL):
			ai.Label = string(bytes.TrimRight(val, "\x00"))
		case uint16(unix.IFA_BROADCAST):
			ai.Broadcast = CopyBytes(val)
		case uint16(unix.IFA_FLAGS):
			if len(val) >= 4 {
				ai.Flags = binary.LittleEndian.Uint32(val[0:4])
			}
		case IfaProto:
			if len(val) >= 1 {
				ai.Proto = val[0]
			}
		case uint16(unix.IFA_CACHEINFO):
			var ci IfaCacheinfo
			if _, cerr := DeserializeIfaCacheinfo(val, &ci); cerr == nil {
				ai.CacheInfo = ci
				ai.HasCacheInfo = true
			}
		}
	})
	if err != nil {
		return AddrInfo{}, err
	}

	// ip/ipaddress.c:1531-1534 — the alias runs both ways.
	if ai.Local == nil {
		ai.Local = ai.Address
	}
	if ai.Address == nil {
		ai.Address = ai.Local
	}
	return ai, nil
}
