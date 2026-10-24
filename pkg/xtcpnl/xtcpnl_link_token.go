package xtcpnl

import "golang.org/x/sys/unix"

// LinkToken is what `ip token` reads from one RTM_NEWLINK of an AF_INET6
// link dump: the ifinfomsg's family, index and flags, and IFLA_INET6_TOKEN,
// the interface identifier inet6_fill_ifla6_attrs (net/ipv6/addrconf.c) puts
// inside IFLA_PROTINFO.
//
// print_token (ip/iptoken.c:41-87) skips a reply that is not AF_INET6, has
// ifindex 0, or is IFF_LOOPBACK or IFF_NOARP, and ends the dump at one with
// no IFLA_PROTINFO or no token in it, so presence is kept apart from the
// value for both.
//
// Token aliases the message body. Only an AF_INET6 reply's IFLA_PROTINFO is
// read: on an AF_BRIDGE reply the same attribute carries IFLA_BRPORT_*, whose
// type 7 is not a token.
type LinkToken struct {
	Family      uint8
	Index       int32
	Flags       uint32
	HasProtinfo bool
	// Token is IFLA_INET6_TOKEN's payload, 16 bytes from the kernel; nil
	// when absent.
	Token []byte
}

// ParseLinkToken decodes an RTM_NEWLINK body for `ip token`. A body shorter
// than an ifinfomsg, or a malformed top-level attribute, is an error; the
// IFLA_PROTINFO nest is walked tolerantly, keeping what precedes a malformed
// attribute, as parse_rtattr_nested does. A repeated attribute takes the
// first, at either level.
func ParseLinkToken(body []byte) (LinkToken, error) {
	var m IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &m); err != nil {
		return LinkToken{}, err
	}
	lt := LinkToken{Family: m.Family, Index: m.Index, Flags: m.Flags}
	var protinfo []byte
	var seen attrSeen
	err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		if atype == uint16(unix.IFLA_PROTINFO) && seen.first(atype) {
			lt.HasProtinfo, protinfo = true, val
		}
	})
	if err != nil {
		return LinkToken{}, err
	}
	if lt.Family != unix.AF_INET6 || !lt.HasProtinfo {
		return lt, nil
	}
	walkNestTolerant(protinfo, func(atype uint16, val []byte) {
		if atype == uint16(unix.IFLA_INET6_TOKEN) && lt.Token == nil {
			lt.Token = val
		}
	})
	return lt, nil
}
