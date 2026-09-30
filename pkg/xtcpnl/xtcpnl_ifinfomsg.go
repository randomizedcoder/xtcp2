package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"

	"golang.org/x/sys/unix"
)

// IfInfomsg mirrors the kernel's `struct ifinfomsg` — the family header of an
// RTM_*LINK message.
//
//	struct ifinfomsg {
//		unsigned char	ifi_family;
//		unsigned char	__ifi_pad;
//		unsigned short	ifi_type;
//		int		ifi_index;
//		unsigned	ifi_flags;
//		unsigned	ifi_change;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h
type IfInfomsg struct {
	Family uint8  // 1
	Pad    uint8  // 1
	Type   uint16 // 2
	Index  int32  // 4
	Flags  uint32 // 4
	Change uint32 // 4 = 16 ( 16 / 4 = 4 )
}

const (
	IfInfomsgSizeCst = 16
	IfInfomsgReadCst = IfInfomsgSizeCst
)

var (
	ErrIfInfomsgSmall = errors.New("data too small for IfInfomsg")
)

// DeserializeIfInfomsg does a binary read of an IfInfomsg with a basic length
// check.
func DeserializeIfInfomsg(data []byte, m *IfInfomsg) (n int, err error) {
	if len(data) < IfInfomsgSizeCst {
		return 0, ErrIfInfomsgSmall
	}

	m.Family = data[0]
	m.Pad = data[1]
	m.Type = binary.LittleEndian.Uint16(data[2:4])
	m.Index = int32(binary.LittleEndian.Uint32(data[4:8]))
	m.Flags = binary.LittleEndian.Uint32(data[8:12])
	m.Change = binary.LittleEndian.Uint32(data[12:16])

	return IfInfomsgReadCst, nil
}

// IF_OPER_* are the RFC 2863 operational states carried in IFLA_OPERSTATE.
// golang.org/x/sys/unix does not export them (unlike IFLA_OPERSTATE itself), so
// they are declared here from the kernel UAPI, as RtaNhID is in
// xtcpnl_rtmsg.go.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if.h
const (
	IfOperUnknown        uint8 = 0
	IfOperNotPresent     uint8 = 1
	IfOperDown           uint8 = 2
	IfOperLowerLayerDown uint8 = 3
	IfOperTesting        uint8 = 4
	IfOperDormant        uint8 = 5
	IfOperUp             uint8 = 6
)

// LinkInfo is the subset of an RTM_*LINK message xtcp2 keeps: the interface
// index, flags, and name (IFLA_IFNAME), used to label addresses/routes per
// link, plus the state fields a link up/down event turns on.
//
// Change (ifi_change) is a mask of which IFF_* bits this particular message
// reports as having changed. A full dump reply carries 0; a notification
// triggered by `ip link set dev X down` carries IFF_UP. It is therefore the way
// to tell "this link happens to be down" from "this link just went down".
//
// OperState and Carrier come from optional attributes: both read 0 when the
// attribute is absent, which for OperState coincides with the real value
// IfOperUnknown. Prefer the Flags-derived helpers (IsUp, IsAdminDown,
// IsCarrierDown) for decisions, since ifi_flags is always present.
//
// The second group of fields is what `ip link show` needs to render a line at
// all, and each is present in the committed 7.1.8 link dump:
//
//	Address/Broadcast  the MAC and its broadcast — "link/ether aa:.. brd ff:.."
//	Qdisc              "qdisc noqueue"
//	TxQLen             "qlen 1000"; `ip` omits the word entirely when 0
//	LinkMode           "mode DEFAULT" (0) or "mode DORMANT" (1)
//	Group              "group default" (0)
//	Link               the peer index behind the "@if2" suffix on a veth
//	Master             the enslaving bridge — "master br-3a5828b2963a"
//	Kind               IFLA_LINKINFO -> IFLA_INFO_KIND: "veth", "bridge", "nlmon"
//
// Index 0 is not a valid interface index, so Master == 0 means the attribute
// was absent. Link does NOT work that way — see HasLink. IFLA_LINK_NETNSID
// needs the explicit HasLinkNetnsID because -1 is a value the kernel really
// sends, meaning "the peer is in a netns I cannot name" — iproute2 prints
// "link-netnsid unknown" for it, which is a different line from printing
// nothing.
type LinkInfo struct {
	Index        int32
	Flags        uint32
	Name         string
	Change       uint32 // ifi_change — which IFF_* bits this message reports changing
	Type         uint16 // ifi_type — ARPHRD_* (ARPHRD_ETHER, ARPHRD_LOOPBACK, …)
	OperState    uint8  // IFLA_OPERSTATE (IF_OPER_*); IfOperUnknown if absent
	HasOperState bool   // IFLA_OPERSTATE present (zero is IF_OPER_UNKNOWN)
	Carrier      uint8  // IFLA_CARRIER (0/1); 0 if absent
	HasCarrier   bool   // IFLA_CARRIER present (zero is legitimate)
	MTU          uint32 // IFLA_MTU; 0 if absent
	HasMTU       bool   // IFLA_MTU present

	Address   []byte // IFLA_ADDRESS — the hardware address; nil if absent
	Broadcast []byte // IFLA_BROADCAST; nil if absent

	// PermAddress is IFLA_PERM_ADDRESS, the address the device was born with
	// — `ip`'s " permaddr …" token. nil if absent.
	//
	// It is rendered CONDITIONALLY, unlike the two above: `ip` prints it only
	// when it is absent-from or different-to IFLA_ADDRESS
	// (ip/ipaddress.c:1094-1111), so on an ordinary NIC whose MAC has never
	// been overridden it decodes to a value and prints nothing. See
	// LinkInfo.PermAddrDiffers, which is that test and not a nil check.
	PermAddress []byte
	Qdisc       string // IFLA_QDISC
	Kind        string // IFLA_LINKINFO -> IFLA_INFO_KIND
	Link        int32  // IFLA_LINK — peer/lower interface index

	// HasLink separates "IFLA_LINK absent" from "IFLA_LINK carrying 0", which
	// are two different renders and not one.
	//
	// Index 0 is not a valid interface index, so it is tempting to read Link
	// == 0 as absence — and print_name_and_link does not
	// (lib/utils.c:1309-1337). It tests the attribute first and the value
	// second: absent prints no suffix at all, while present-and-zero prints
	// the literal "@NONE" and, in JSON, "link": null. A tunnel device sits on
	// no underlying interface and sends exactly the second form, so every link
	// in the tunnel capture set is "@NONE".
	HasLink bool

	Master         int32 // IFLA_MASTER — enslaving interface index; 0 if absent
	LinkNetnsID    int32 // IFLA_LINK_NETNSID; only meaningful with HasLinkNetnsID
	HasLinkNetnsID bool  // IFLA_LINK_NETNSID present (the value may be -1)
	LinkMode       uint8 // IFLA_LINKMODE — IF_LINK_MODE_DEFAULT / _DORMANT
	HasLinkMode    bool  // IFLA_LINKMODE present (zero is DEFAULT)

	// TxQLen/Group carry presence flags because zero is a legal value for
	// both and "absent" renders differently from "zero".
	//
	// Three of the eleven links in the committed dump carry IFLA_TXQLEN with
	// value 0 (docker0, br-3a5828b2963a, veth179a698) while a whole class of
	// reply carries no IFLA_TXQLEN at all: an RTM_GETLINK dump whose
	// ifi_family is AF_INET6 is answered by the kernel's inet6_dump_ifinfo
	// (net/ipv6/addrconf.c) rather than rtnl_dump_ifinfo, and that function
	// emits only IFLA_IFNAME, IFLA_ADDRESS, IFLA_MTU, IFLA_LINK,
	// IFLA_OPERSTATE and IFLA_PROTINFO. Measured, not inferred: the 11 link
	// replies to `ip -6 addr show` in netlink_route_getaddr.pcap carry six
	// attribute types against the AF_INET run's forty-six.
	//
	// iproute2 distinguishes the two cases by testing tb[IFLA_TXQLEN] and
	// tb[IFLA_GROUP] for presence (ip/ipaddress.c:1045,1155), so a decoder
	// that collapses absent to zero cannot render either command correctly.
	TxQLen    uint32 // IFLA_TXQLEN; only meaningful with HasTxQLen
	HasTxQLen bool   // IFLA_TXQLEN present (the value may be 0)
	Group     uint32 // IFLA_GROUP; only meaningful with HasGroup
	HasGroup  bool   // IFLA_GROUP present (the value may be 0)

	// AltNames are the IFLA_ALT_IFNAME entries inside IFLA_PROP_LIST, in wire
	// order — `ip`'s "altname" continuation lines. nil when the link has none,
	// which is the common case: 4 of the 11 links in the committed dump carry
	// one. Unlike every other field here this is a slice, because a link may
	// hold several alternative names and iproute2 prints one line per name
	// rather than picking one.
	AltNames []string

	// Stats is IFLA_STATS64, or IFLA_STATS widened, or nil when the reply
	// carried neither — which is every reply to a request that set
	// RTEXT_FILTER_SKIP_STATS, i.e. everything but `ip -s`. See
	// xtcpnl_link_stats.go for the selection and length rules, which are
	// iproute2's, not this package's.
	//
	// A pointer, and the only one in this struct, for two reasons that point
	// the same way. RtnlLinkStats64 is 200 bytes against the rest of LinkInfo
	// put together, and LinkInfo is copied per link on every dump — a dump of
	// a few hundred interfaces would pay that on every reply to carry a field
	// that is nil in all of them. And nil already means "absent", so this is
	// the one field that needs no Has* companion.
	Stats *RtnlLinkStats64

	// StatsIs64 records which attribute Stats came from: true for
	// IFLA_STATS64, false for a widened IFLA_STATS. Meaningless when Stats is
	// nil.
	//
	// It exists because the choice is observable in `ip -j` output and
	// nowhere else. __print_link_stats passes get_rtnl_link_stats_rta's
	// return value through as the JSON object name — `(ret == sizeof(*s)) ?
	// "stats64" : "stats"` (ip/ipaddress.c:836) — so the same counters appear
	// under a different key depending on which attribute the kernel sent.
	// The text form is identical either way, which is exactly why this is
	// easy to lose.
	StatsIs64 bool
}

// HWAddr returns the hardware address as `ip` prints it, or "" when
// IFLA_ADDRESS is absent.
//
// It is NOT always colon-hex: on the five tunnel ARPHRD types the address is
// an IP endpoint and `ip` prints it as one. See LLAddrN2A, which this defers
// to, and which is why the link's ifi_type is part of the answer.
//
// The length is not assumed to be 6 either. InfiniBand carries 20 bytes and a
// tunnel carries 4, and `ip` prints all of them. Truncating to 6 would
// silently corrupt those.
//
// An absent attribute is not the same as a zero address: lo really does have
// 00:00:00:00:00:00, while nlmon0 in the committed dump has no IFLA_ADDRESS at
// all and `ip` prints "link/netlink " with nothing after it. A tunnel fallback
// device such as tunl0 is a third case again — four bytes of zero, which is a
// present address that prints "0.0.0.0".
func (li LinkInfo) HWAddr() string {
	return LLAddrN2A(li.Address, li.Type)
}

// BroadcastAddr is HWAddr for IFLA_BROADCAST, the "brd ff:ff:ff:ff:ff:ff" half
// of the same line — or the "peer 198.51.100.1" half, since `ip` swaps the
// keyword on IFF_POINTOPOINT (ip/ipaddress.c:1077-1084) without changing how
// the value itself is formatted. Same ifi_type, same function, so a tunnel's
// broadcast renders as an IP exactly as its address does.
func (li LinkInfo) BroadcastAddr() string {
	return LLAddrN2A(li.Broadcast, li.Type)
}

// PermAddr is HWAddr for IFLA_PERM_ADDRESS — the third and last attribute `ip`
// puts through ll_addr_n2a with this link's ifi_type (ip/ipaddress.c:1106-1109),
// so a tunnel's permaddr prints as an IP exactly as its address does.
//
// This does NOT decide whether to print it. See PermAddrDiffers.
func (li LinkInfo) PermAddr() string {
	return LLAddrN2A(li.PermAddress, li.Type)
}

// PermAddrDiffers reports whether `ip` would print the " permaddr …" token,
// which is not the same question as whether IFLA_PERM_ADDRESS arrived.
//
// ip/ipaddress.c:1097-1100 prints it only when IFLA_ADDRESS is absent, or is a
// different length, or differs byte for byte. An ordinary NIC reports a
// permanent address equal to its current one and gets no token at all, so a
// renderer keyed on presence alone would add a line to nearly every link in a
// normal dump.
//
// The five tunnel families are where the two answers part company: ip6_tunnel
// and ip6_gre fill perm_addr with eth_random_addr (ip6_tunnel.c:1913,
// ip6_gre.c:1443) while IFLA_ADDRESS holds the configured local endpoint, so
// they always differ and the token is always printed — with a value that is
// fresh every boot.
func (li LinkInfo) PermAddrDiffers() bool {
	if len(li.PermAddress) == 0 {
		return false
	}
	return !bytes.Equal(li.PermAddress, li.Address)
}

// LLAddrN2A mirrors iproute2's ll_addr_n2a (lib/ll_addr.c:26-44). `ip` passes
// BOTH halves of the `link/` line through it — IFLA_ADDRESS and
// IFLA_BROADCAST, each with ifi->ifi_type (ip/ipaddress.c:1067-1092) — so a
// divergence here is a divergence in two places at once.
//
// # Three consumers, and the third is not on a link at all
//
// IFLA_ADDRESS, IFLA_BROADCAST and IFLA_PERM_ADDRESS take ifi_type straight off
// the message being printed. print_neigh is the odd one: NDA_LLADDR belongs to
// a NEIGHBOR, whose ndmsg carries no type, so `ip` reaches for the type of the
// neighbor's DEVICE via ll_index_to_type(r->ndm_ifindex)
// (ip/ipneigh.c:428-430). That is why this is exported — internal/goip/render
// needs it for a path that has no LinkInfo in hand.
//
// # The parse side is not symmetric, which is worth knowing before capturing
//
// ll_addr_a2n (lib/ll_addr.c:47-63) decides on a literal '.' in the STRING and
// never looks at the device type. So `ip neigh add … lladdr 192.0.2.99 dev
// gre1` stores four bytes no matter what gre1 is, while rendering those four
// bytes back needs gre1 to be ARPHRD_IPGRE. Input is type-blind, output is
// type-driven.
//
// It is not a hex formatter. Two special cases come first:
//
//	len == 4  && type ∈ {ARPHRD_TUNNEL, ARPHRD_SIT, ARPHRD_IPGRE}
//	          → inet_ntop(AF_INET)   (:32-35), e.g. "192.0.2.3"
//	len == 16 && type ∈ {ARPHRD_TUNNEL6, ARPHRD_IP6GRE}
//	          → inet_ntop(AF_INET6)  (:37-38), e.g. "2001:db8::1"
//
// Each test is a length AND a type, never one or the other. A 4-byte address
// on an ARPHRD_ETHER link stays hex, and a 6-byte address on an ARPHRD_SIT
// link stays hex too — the type alone does not license the conversion. Only
// when both tests fail does the generic loop at :40-43 run.
//
// netip rather than net.IP is deliberate, and it is not a style choice.
// net.IP.String() renders a v4-mapped 16-byte address as the dotted quad
// "1.2.3.4"; inet_ntop(AF_INET6) renders "::ffff:1.2.3.4", and so does netip.
// A v4-mapped local endpoint on an ip6tnl is unusual but perfectly legal, and
// the entire point of this function is to agree with the C.
func LLAddrN2A(b []byte, ifiType uint16) string {
	switch {
	case len(b) == 4 &&
		(ifiType == unix.ARPHRD_TUNNEL ||
			ifiType == unix.ARPHRD_SIT ||
			ifiType == unix.ARPHRD_IPGRE):
		return netip.AddrFrom4([4]byte(b)).String()

	case len(b) == 16 &&
		(ifiType == unix.ARPHRD_TUNNEL6 || ifiType == unix.ARPHRD_IP6GRE):
		return netip.AddrFrom16([16]byte(b)).String()
	}
	return hwAddrString(b)
}

// TypeName is the ARPHRD_* name `ip` prints after "link/".
func (li LinkInfo) TypeName() string {
	return ARPHRDName(li.Type)
}

// hwAddrString is the FALL-THROUGH half of ll_addr_n2a (lib/ll_addr.c:40-43)
// on its own: "%02x" per byte, ":" between, any length.
//
// Callers rendering a link-layer address want LLAddrN2A, which applies the
// type-dependent special cases first and then lands here. This half is
// separate because it is also the whole answer for every ARPHRD type that has
// no special case, which is all but five of them.
func hwAddrString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3-1)
	for i, c := range b {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hexDigits[c>>4], hexDigits[c&0x0f])
	}
	return string(out)
}

// IsUp reports whether the link is both administratively up and operationally
// running — IFF_UP and IFF_RUNNING together, which is what "usable" means.
func (li LinkInfo) IsUp() bool {
	const up = unix.IFF_UP | unix.IFF_RUNNING
	return li.Flags&up == up
}

// IsAdminDown reports an administrative down: IFF_UP is clear, i.e. someone ran
// `ip link set dev X down`.
func (li LinkInfo) IsAdminDown() bool {
	return li.Flags&unix.IFF_UP == 0
}

// IsCarrierDown reports a carrier loss: the link is administratively up but not
// running, i.e. the cable is out or the veth peer went away.
//
// This is the distinction that makes link events worth having — both cases show
// up as "not usable", but only one of them is an operator action.
func (li LinkInfo) IsCarrierDown() bool {
	return li.Flags&unix.IFF_UP != 0 && li.Flags&unix.IFF_RUNNING == 0
}

// ParseNewLink decodes an RTM_NEWLINK or RTM_DELLINK message body (the bytes
// after the nlmsghdr): the ifinfomsg header followed by IFLA_* attributes.
//
// The set decoded is what it takes to render an `ip link show` line: IFNAME,
// OPERSTATE, CARRIER, MTU, ADDRESS, BROADCAST, QDISC, TXQLEN, LINKMODE, GROUP,
// LINK, MASTER, LINK_NETNSID, and IFLA_INFO_KIND from inside IFLA_LINKINFO.
// Everything else in the message — 30-odd attributes per link in the committed
// dump, most of them the `-d` detail `ip` only prints on request — is skipped.
//
// IFLA_STATS and IFLA_STATS64 ARE decoded, into Stats, and the claim that
// stood here before them is worth recording because it was wrong in a
// load-bearing way. It read: "deliberately not here — absent from every reply
// in the committed fixtures because the request sets RTEXT_FILTER_SKIP_STATS,
// and they return only under `ip -s`."
//
// The first half is false and the second is misleading. `ip -4 addr show` and
// `ip neigh show` take their link dump through a path that sends no
// IFLA_EXT_MASK at all — rtnl_linkdump_req_filter_fn forwards filter_fn only
// for AF_UNSPEC and AF_PACKET (lib/libnetlink.c:595) — so no
// RTEXT_FILTER_SKIP_STATS reaches the kernel and it appends both attributes to
// every reply. Both are in the committed corpus at full length, 96 and 200
// bytes, on every link of netlink_route_getaddr_v4.pcap and
// netlink_route_getneigh.pcap, and kernel notifications carry them too. `ip
// -s` is the way to get stats onto a command that DOES set the mask, such as
// `link show`; it was never what made them reachable.
//
// The decode rules are iproute2's and live in xtcpnl_link_stats.go.
//
// Duplicated attributes take the first occurrence; see
// xtcpnl_rtattr_firstwins.go for why that is iproute2's rule and not the
// kernel's.
//
// The name is retained for the dump path; RTM_DELLINK carries the same layout,
// so ParseRtnetlinkEvent reuses this for both senses.
func ParseNewLink(body []byte) (LinkInfo, error) {
	var m IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &m); err != nil {
		return LinkInfo{}, err
	}

	li := LinkInfo{
		Index:  m.Index,
		Flags:  m.Flags,
		Change: m.Change,
		Type:   m.Type,
	}
	var seen attrSeen
	var raw linkStatsRaw
	err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		if !seen.first(atype) {
			return
		}
		setLinkAttr(&li, &raw, atype, val)
	})
	if err != nil {
		return LinkInfo{}, err
	}

	// Absent is not zero, and absent is not an error either. DecodeLinkStats
	// returns ErrLinkStatsNone when neither attribute was present, which is
	// the ordinary case for every command except `ip -s` — so Stats is left
	// nil rather than the error dropping a perfectly good link.
	if s, serr := DecodeLinkStats(raw.stats64, raw.stats); serr == nil {
		li.Stats = &s
		// The same test DecodeLinkStats made, and the reason it is repeated
		// rather than returned: the selection rule is presence of
		// IFLA_STATS64, full stop, so a caller that has the raw attributes
		// already knows the answer and a third return value would only be a
		// second place for it to be wrong.
		li.StatsIs64 = raw.stats64 != nil
	}
	return li, nil
}

// linkStatsRaw carries the two stats attributes out of the attribute walk
// undecoded, because neither arm can decide on its own which one wins: the
// kernel emits IFLA_STATS before IFLA_STATS64, so choosing at the case would
// mean choosing before the winner has been seen.
type linkStatsRaw struct {
	stats   []byte
	stats64 []byte
}

// setLinkAttr applies one RTA to a LinkInfo under construction.
//
// It is a function rather than the closure it used to be for one reason
// worth stating, because it is the kind of split that otherwise looks like
// taste: ParseNewLink's cyclomatic complexity is this switch's, and adding
// the two stats cases took it past the gocyclo ceiling of 30. Moving the
// switch out is the split that keeps the ceiling meaningful — the alternative
// was raising it, which would have been suppressing the finding rather than
// fixing it. The duplicate-attribute check stays at the call site, because it
// is a property of the walk and not of any one attribute.
func setLinkAttr(li *LinkInfo, raw *linkStatsRaw, atype uint16, val []byte) {
	switch atype {
	case uint16(unix.IFLA_IFNAME):
		li.Name = string(bytes.TrimRight(val, "\x00"))
	case uint16(unix.IFLA_OPERSTATE):
		if len(val) >= 1 {
			li.OperState = val[0]
			li.HasOperState = true
		}
	case uint16(unix.IFLA_CARRIER):
		if len(val) >= 1 {
			li.Carrier = val[0]
			li.HasCarrier = true
		}
	case uint16(unix.IFLA_MTU):
		if len(val) >= 4 {
			li.MTU = binary.LittleEndian.Uint32(val[0:4])
			li.HasMTU = true
		}
	case uint16(unix.IFLA_ADDRESS):
		li.Address = CopyBytes(val)
	case uint16(unix.IFLA_BROADCAST):
		li.Broadcast = CopyBytes(val)
	case uint16(unix.IFLA_PERM_ADDRESS):
		li.PermAddress = CopyBytes(val)
	case uint16(unix.IFLA_QDISC):
		li.Qdisc = string(bytes.TrimRight(val, "\x00"))
	case uint16(unix.IFLA_TXQLEN):
		if len(val) >= 4 {
			li.TxQLen = binary.LittleEndian.Uint32(val[0:4])
			li.HasTxQLen = true
		}
	case uint16(unix.IFLA_LINKMODE):
		if len(val) >= 1 {
			li.LinkMode = val[0]
			li.HasLinkMode = true
		}
	case uint16(unix.IFLA_GROUP):
		if len(val) >= 4 {
			li.Group = binary.LittleEndian.Uint32(val[0:4])
			li.HasGroup = true
		}
	case uint16(unix.IFLA_LINK):
		if len(val) >= 4 {
			li.Link = int32(binary.LittleEndian.Uint32(val[0:4]))
			li.HasLink = true
		}
	case uint16(unix.IFLA_MASTER):
		if len(val) >= 4 {
			li.Master = int32(binary.LittleEndian.Uint32(val[0:4]))
		}
	case uint16(unix.IFLA_LINK_NETNSID):
		if len(val) >= 4 {
			li.LinkNetnsID = int32(binary.LittleEndian.Uint32(val[0:4]))
			li.HasLinkNetnsID = true
		}
	case uint16(unix.IFLA_LINKINFO):
		li.Kind = linkInfoKind(val)
	case uint16(unix.IFLA_PROP_LIST):
		li.AltNames = linkAltNames(val)

	// The two stats attributes are kept as raw slices and resolved after
	// the walk rather than decoded in place. IFLA_STATS64 wins over
	// IFLA_STATS whenever both are present, and that is not a property
	// either arm can evaluate on its own: the kernel emits IFLA_STATS
	// first, so deciding there would mean deciding before the winner has
	// been seen.
	case uint16(unix.IFLA_STATS):
		raw.stats = val
	case uint16(unix.IFLA_STATS64):
		raw.stats64 = val
	}
}

// linkInfoKind descends IFLA_LINKINFO and returns IFLA_INFO_KIND — "veth",
// "bridge", "nlmon" — or "" when the nest carries no kind.
//
// This is the first production caller of WalkRTAttrsNested, closing
// TODO-SOON.md §12. The descent is one level and stops there: IFLA_INFO_DATA is
// the per-kind blob `ip` hands to one of forty print_opt implementations, and
// decoding it is explicitly out of scope.
//
// A walk error inside the nest is swallowed rather than failing the whole
// message. That is the same tolerance WalkRTAttrs already applies to a short
// trailing attribute at the top level, and the alternative — dropping an
// otherwise good link because a nest it did not need was malformed — is worse
// for a renderer.
func linkInfoKind(val []byte) string {
	var kind string
	walkNestTolerant(val, func(atype uint16, inner []byte) {
		if atype == uint16(unix.IFLA_INFO_KIND) && kind == "" {
			kind = string(bytes.TrimRight(inner, "\x00"))
		}
	})
	return kind
}

// walkNestTolerant walks a nested attribute stream and keeps whatever fn
// collected before a malformed attribute, instead of reporting the error.
//
// It exists so that the tolerance is stated once, in a signature, rather than
// as a bare `_ =` at each call site: a discarded error reads identically
// whether it was considered or overlooked, and this package has both kinds.
// ParseNewRoute is the contrast - it parks a nested error and returns it,
// because a truncated RTA_MULTIPATH changes what the route MEANS. The nests
// below are decorative by comparison: losing an altname or a device kind costs
// a renderer one line, and dropping the whole link to report it costs more.
func walkNestTolerant(val []byte, fn func(atype uint16, val []byte)) {
	if err := WalkRTAttrsNested(val, fn); err != nil {
		return
	}
}

// linkAltNames descends IFLA_PROP_LIST and returns every IFLA_ALT_IFNAME in
// it, in wire order — the alternative interface names `ip` prints as one
// "altname <name>" continuation line each.
//
// # Why first-wins does NOT apply inside this nest
//
// Everywhere else in this package a repeated attribute type takes the first
// occurrence, because that is what iproute2's parse_rtattr does
// (lib/libnetlink.c:1554). IFLA_PROP_LIST is the exception, and the exception
// is in iproute2 too: ipaddress.c:1318-1330 walks the nest with a bare
// RTA_NEXT loop and prints *every* IFLA_ALT_IFNAME it finds rather than
// building a tb[] table. A link may legitimately carry several alternative
// names, so collapsing them to the first would drop output rather than
// deduplicate it.
//
// # Why this is not an `ip -d` detail
//
// It looks like one, because the committed sidecar was captured with `ip -d`
// and the altname lines sit among the detail attributes. They are not: the
// IFLA_PROP_LIST block sits *outside* print_linkinfo's `if (show_details)`
// guard, so a plain `ip link show` prints altnames too. Four of the eleven
// links in the committed dump carry one — enp1s0, both enp35s0f* ports and
// one veth — so a renderer that skips them is four lines short on this
// fixture.
//
// Non-nested short reads are tolerated the same way linkInfoKind tolerates
// them, and an entry of any type other than IFLA_ALT_IFNAME is ignored: the
// kernel is free to add siblings to this nest.
func linkAltNames(val []byte) []string {
	var names []string
	walkNestTolerant(val, func(atype uint16, inner []byte) {
		if atype != uint16(unix.IFLA_ALT_IFNAME) {
			return
		}
		if n := string(bytes.TrimRight(inner, "\x00")); n != "" {
			names = append(names, n)
		}
	})
	return names
}
