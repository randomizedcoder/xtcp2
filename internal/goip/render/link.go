package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// LinkView is one link as `ip link show` presents it: everything resolved,
// nothing left to look up.
//
// It exists as a separate type from xtcpnl.LinkInfo for two reasons that both
// pay for themselves. The name resolution — `master br0`, the `@peer` suffix,
// M-DOWN — needs the interface index cache, which the decoder has no business
// knowing about; and the JSON renderer needs field names and omission rules
// that are `ip -j`'s, not the wire's.
//
// The JSON tags are `ip -j link show`'s key names, so the informational diff
// can be `ip -j link show | jq -S` against `goip -json link show | jq -S`.
// Sorting with -S is what makes key *order* irrelevant, which matters because
// iproute2's key order follows its print_* call order and is not worth
// reproducing.
type LinkView struct {
	IfIndex int32    `json:"ifindex"`
	IfName  string   `json:"ifname"`
	Flags   []string `json:"flags"`

	// Link is the resolved name of IFLA_LINK's target, set only on the path
	// where iproute2 resolves it (no IFLA_LINK_NETNSID). LinkIndex is set on
	// the other path, where the peer lives in another namespace and only its
	// index is meaningful. At most one is ever set; see LinkViewOf.
	Link      string `json:"link,omitempty"`
	LinkIndex *int32 `json:"link_index,omitempty"`

	MTU       uint32  `json:"mtu,omitempty"`
	Qdisc     string  `json:"qdisc,omitempty"`
	Master    string  `json:"master,omitempty"`
	OperState string  `json:"operstate,omitempty"`
	LinkMode  string  `json:"linkmode,omitempty"`
	Group     string  `json:"group,omitempty"`
	TxQLen    *uint32 `json:"txqlen,omitempty"`

	// LinkType is `omitempty` because `ip addr show` under -4 or -6 does not
	// print the `link/` line at all, and PRINT_ANY means JSON loses the key
	// with the text. See omitLinkLine.
	LinkType  string `json:"link_type,omitempty"`
	Address   string `json:"address,omitempty"`
	Broadcast string `json:"broadcast,omitempty"`

	// LinkPointToPoint mirrors `ip -j`'s "link_pointtopoint": on a
	// point-to-point link the second address is a peer, not a broadcast, and
	// iproute2 signals that in JSON with a bool rather than by renaming the
	// key (ip/ipaddress.c:1075-1083).
	LinkPointToPoint bool `json:"link_pointtopoint,omitempty"`

	// LinkNetnsID is a pointer because -1 is a real value meaning "the peer is
	// in a namespace I cannot name", which `ip` renders as
	// "link-netnsid unknown" — a different line from printing nothing at all.
	LinkNetnsID *int32 `json:"link_netnsid,omitempty"`

	AltNames []string `json:"altnames,omitempty"`

	// nameSuffix is the "@peer" or "@if2" part of the first line. It is not a
	// JSON field: iproute2 puts the bare ifname in JSON and only concatenates
	// for the text form (print_name_and_link, lib/utils.c:1302-1344).
	nameSuffix string

	// omitLinkLine suppresses the whole `    link/<type> …` continuation
	// line, which `ip addr show` does under -4 and -6.
	//
	// The guard is `if (!filter.family || filter.family == AF_PACKET ||
	// show_details)` (ip/ipaddress.c:1060), and it opens with the print_nl()
	// that starts the second line. So it does not merely hide three fields:
	// it removes the newline, and the two prints that follow it —
	// link-netnsid (:1114) and new-netnsid — sit **outside** the guard and
	// therefore land on the *stanza* line instead. Verified against the
	// pinned `ip` on a veth:
	//
	//	ip addr show:     …group default qlen 1000
	//	                      link/ether 6e:… brd ff:… link-netnsid 0
	//	ip -4 addr show:  …group default qlen 1000 link-netnsid 0
	//
	// which is why this is one flag controlling both, rather than an
	// omitempty on LinkType.
	omitLinkLine bool
}

// LinkViewOf resolves a decoded link against the index cache.
//
// # print_name_and_link, which is where the subtlety lives
//
// lib/utils.c:1302-1344 decides three things at once, and they are coupled:
//
//	if (tb[IFLA_LINK]) {
//		if (iflink) {
//			if (tb[IFLA_LINK_NETNSID])  link = ll_idx_n2a(iflink);      // "if%u"
//			else {
//				link = ll_index_to_name(iflink);
//				m_flag = ll_index_to_flags(iflink);
//				m_flag = !(m_flag & IFF_UP);
//			}
//		} else link = "NONE";
//		if (link) name = name@link;
//	}
//
// So the presence of IFLA_LINK_NETNSID does double duty: it selects the cheap
// `if%u` suffix *and* suppresses the M-DOWN computation entirely. All three
// veths in the committed dump carry it, which is why ip_link_n shows
// `ve-nfb-vpn@if2` and no M-DOWN anywhere — a renderer that always resolved
// the name would emit `@enp1s0` instead, and one that always computed M-DOWN
// could emit it on a link `ip` leaves alone.
//
// # The -1 that must not be normalized
//
// On the resolving path, `ll_index_to_flags` returns -1 for an index that is
// not in the cache. `-1 & IFF_UP` is IFF_UP, which is non-zero, so
// `m_flag = !(...)` is **0** and there is no M-DOWN. A cache miss therefore
// behaves like "the peer is up", not like "the peer is down". Turning the -1
// into a 0 — the obvious Go instinct for "not found" — inverts it and prints
// M-DOWN on every unresolvable peer. That is the one behavior the NameTab
// interface documents at its method.
func LinkViewOf(li xtcpnl.LinkInfo, names NameTab) LinkView {
	v := LinkView{
		IfIndex:   li.Index,
		IfName:    li.Name,
		MTU:       li.MTU,
		Qdisc:     li.Qdisc,
		OperState: operStateName(li.OperState),
		LinkMode:  linkModeName(li.LinkMode),
		LinkType:  li.TypeName(),
		Address:   li.HWAddr(),
		Broadcast: li.BroadcastAddr(),
		AltNames:  li.AltNames,
	}

	// Group and TxQLen are set only when the reply carried the attribute,
	// because iproute2 tests tb[IFLA_GROUP] and tb[IFLA_TXQLEN] for presence
	// (ip/ipaddress.c:1045,1155) and a reply with neither is a real case, not
	// a hypothetical: see xtcpnl.BuildDumpLinkRequestFamily on AF_INET6.
	if li.HasGroup {
		v.Group = groupName(li.Group)
	}
	if li.HasTxQLen {
		txqlen := li.TxQLen
		v.TxQLen = &txqlen
	}

	if li.Broadcast != nil && li.Flags&unix.IFF_POINTOPOINT != 0 {
		v.LinkPointToPoint = true
	}

	mdown := false
	if li.Link != 0 {
		if li.HasLinkNetnsID {
			idx := li.Link
			v.LinkIndex = &idx
			v.nameSuffix = "if" + strconv.FormatInt(int64(li.Link), 10)
		} else {
			v.Link = names.IndexToName(li.Link)
			v.nameSuffix = v.Link
			mdown = names.IndexToFlags(li.Link)&unix.IFF_UP == 0
		}
	}
	v.Flags = FlagTokens(li.Flags, mdown)

	if li.Master != 0 {
		v.Master = names.IndexToName(li.Master)
	}
	if li.HasLinkNetnsID {
		id := li.LinkNetnsID
		v.LinkNetnsID = &id
	}
	return v
}

// LinkViewForAddr is LinkViewOf with the two differences `ip addr show` makes
// to the same stanza, both driven by variables print_linkinfo reads rather
// than by its arguments — which is why they are a separate constructor and
// not a field on LinkInfo.
//
//  1. **No `mode DEFAULT`.** print_linkmode is guarded by `do_link`
//     (ip/ipaddress.c:1043), and `do_link` is set only by `ipaddr_list_link`,
//     the `ip link show` entry point (:2417). The committed sidecars show
//     this directly: ip_link_n:1 has "state UNKNOWN mode DEFAULT group
//     default" and ip_addr_n:1 has "state UNKNOWN group default". Since
//     print_linkmode is the only emitter of the JSON "linkmode" key, the key
//     disappears from `ip -j addr show` too, which the omitempty tag handles.
//  2. **No `link/` line unless the family is AF_UNSPEC or AF_PACKET.** See
//     LinkView.omitLinkLine for what that moves rather than merely hides.
//
// family is the -4/-6/default selection as an AF_*, which for this command
// reaches the renderer unchanged; `link show`'s AF_PACKET override does not
// apply here.
func LinkViewForAddr(li xtcpnl.LinkInfo, names NameTab, family uint8) LinkView {
	v := LinkViewOf(li, names)
	v.LinkMode = ""
	if family != unix.AF_UNSPEC && family != unix.AF_PACKET {
		v.omitLinkLine = true
		v.LinkType = ""
		v.Address = ""
		v.Broadcast = ""
		v.LinkPointToPoint = false
	}
	return v
}

// Text renders the view the way a plain `ip link show` stanza reads, newline
// terminated.
//
// The layout is print_linkinfo's call order (ip/ipaddress.c:1016-1333) with
// every `if (show_details)` branch and every `if (do_link && show_stats)`
// branch removed, since goip implements neither -d nor -s:
//
//	1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
//	    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
//
// Two spacing details are transcribed, not chosen. Each field's format string
// carries its own **trailing** space, so the first line ends in a trailing
// space whenever the last field printed was not qlen — which is exactly what
// the sidecar shows for docker0 ("group default " then end of line). And the
// second line's prefix is `"    link/%s "`, so a link with no IFLA_ADDRESS
// ends in two spaces: `"    link/netlink "` plus nothing, which ip_link_n:33
// confirms.
//
// An altname becomes its own continuation line. iproute2 emits it as
// `"%s    altname "` with _SL_ ("\n") as the leading argument, so the newline
// belongs to the altname rather than to the line before it — which is why the
// single trailing newline is written once at the end.
func (v LinkView) Text() string {
	var b strings.Builder

	name := v.IfName
	if v.nameSuffix != "" {
		name += "@" + v.nameSuffix
	}
	fmt.Fprintf(&b, "%d: %s: ", v.IfIndex, name)
	b.WriteString(joinFlags(v.Flags))

	if v.MTU != 0 {
		fmt.Fprintf(&b, "mtu %d ", v.MTU)
	}
	if v.Qdisc != "" {
		fmt.Fprintf(&b, "qdisc %s ", v.Qdisc)
	}
	if v.Master != "" {
		fmt.Fprintf(&b, "master %s ", v.Master)
	}
	if v.OperState != "" {
		fmt.Fprintf(&b, "state %s ", v.OperState)
	}
	if v.LinkMode != "" {
		fmt.Fprintf(&b, "mode %s ", v.LinkMode)
	}
	if v.Group != "" {
		fmt.Fprintf(&b, "group %s ", v.Group)
	}
	if v.TxQLen != nil && (*v.TxQLen != 0 || RenderQlenZero) {
		fmt.Fprintf(&b, "qlen %d", *v.TxQLen)
	}

	if !v.omitLinkLine {
		fmt.Fprintf(&b, "\n    link/%s ", v.LinkType)
		b.WriteString(v.Address)
		if v.Broadcast != "" {
			if v.LinkPointToPoint {
				b.WriteString(" peer ")
			} else {
				b.WriteString(" brd ")
			}
			b.WriteString(v.Broadcast)
		}
	}
	// Outside the guard above, so with omitLinkLine set this appends to the
	// stanza line rather than opening a second one. ip/ipaddress.c:1114.
	if v.LinkNetnsID != nil {
		if *v.LinkNetnsID >= 0 {
			fmt.Fprintf(&b, " link-netnsid %d", *v.LinkNetnsID)
		} else {
			b.WriteString(" link-netnsid unknown")
		}
	}
	for _, alt := range v.AltNames {
		fmt.Fprintf(&b, "\n    altname %s", alt)
	}
	b.WriteString("\n")

	return b.String()
}
