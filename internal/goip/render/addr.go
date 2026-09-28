package render

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// scopeNames is rtnl_rtscope_n2a's table, and unlike every other name table in
// this package it has no static initializer in iproute2 at all: rtnl_rtscope_tab
// starts empty and is filled entirely from a config file
// (rtnl_rtscope_initialize -> rtnl_tab_initialize("/etc/iproute2/rt_scopes"),
// lib/rt_names.c). The five entries below are the contents of the rt_scopes
// file iproute2 ships, transcribed:
//
//	0	global
//	255	nowhere
//	254	host
//	253	link
//	200	site
//
// So baking them in is the same trade-off already taken for group names, and
// it carries the same caveat: a host that has edited rt_scopes will diverge.
// An id with no entry renders as its decimal value, which is also what `ip -N`
// does for every id.
var scopeNames = map[uint8]string{
	0:   "global",
	200: "site",
	253: "link",
	254: "host",
	255: "nowhere",
}

// scopeName is rtnl_rtscope_n2a (lib/rt_names.c).
func scopeName(scope uint8) string {
	if n, ok := scopeNames[scope]; ok {
		return n
	}
	return strconv.FormatUint(uint64(scope), 10)
}

// addrProtoNames is rtnl_addrprot_tab's static initializer
// (lib/rt_names.c:300-305), the IFAPROT_* names. This one *does* have a static
// table, which is why it is a slice here and scopeNames is not.
var addrProtoNames = [...]string{"unspec", "kernel_lo", "kernel_ra", "kernel_ll"}

// addrProtoName is rtnl_addrprot_n2a. The fallback is `%#x`, not decimal —
// different from every other n2a in this file, and transcribed rather than
// harmonized.
func addrProtoName(proto uint8) string {
	if int(proto) < len(addrProtoNames) {
		return addrProtoNames[proto]
	}
	return fmt.Sprintf("%#x", proto)
}

// familyName is lib/utils.c's family_name, returning "" where it returns
// "???" — print_addrinfo tests the first character for '?' and takes a
// different branch, with a different format string and a different JSON key,
// rather than printing the sentinel. Returning the empty string makes the
// caller's branch unavoidable instead of letting "???" leak into output.
func familyName(family uint8) string {
	switch family {
	case unix.AF_INET:
		return "inet"
	case unix.AF_INET6:
		return "inet6"
	case unix.AF_PACKET:
		return "link"
	case unix.AF_BRIDGE:
		return "bridge"
	case unix.AF_MPLS:
		return "mpls"
	}
	return ""
}

// ifaFlagNames is ifa_flag_data (ip/ipaddress.c:1379-1412) in table order,
// which print_ifa_flags walks in order and which therefore decides the token
// order on every address line.
//
// # Two entries share IFA_F_SECONDARY, and only the first one can ever print
//
// The table's first two rows are {"secondary", IFA_F_SECONDARY} and
// {"temporary", IFA_F_SECONDARY}. print_ifa_flags clears the bit at the end of
// every iteration (`flags &= ~flag_data->mask`), so by the time the loop
// reaches "temporary" the bit is gone and that row is unreachable as a print.
// The rendering it looks like it provides comes from a special case inside the
// *first* row instead:
//
//	if (flag_data->mask == IFA_F_SECONDARY && ifa->ifa_family == AF_INET6)
//		print "temporary" else print flag_data->name
//
// The duplicate row exists for the parsing direction. Collapsing the two into
// one entry here would be the faithful thing to do; keeping both, with the
// second marked unreachable, is what makes the next reader check.
//
// # IFA_F_PERMANENT prints when it is CLEAR, under a different name
//
// Its row emits "dynamic" on absence rather than anything on presence, which
// is the one inversion in the table and the reason this cannot be a plain
// bit-to-name map. Its position (index 8) still decides where "dynamic" lands
// among the other tokens: ip_addr_n:14 is "temporary deprecated dynamic",
// which is indices 0, 6, 8 in order.
var ifaFlagNames = []struct {
	bit  uint32
	name string
	// v6Secondary marks the row whose name changes to "temporary" for an
	// AF_INET6 address.
	v6Secondary bool
	// permanentInverted marks the row that prints when the bit is clear.
	permanentInverted bool
	// unreachable marks a row the bit-clearing makes dead; see above.
	unreachable bool
}{
	{bit: unix.IFA_F_SECONDARY, name: "secondary", v6Secondary: true},
	{bit: unix.IFA_F_SECONDARY, name: "temporary", unreachable: true},
	{bit: unix.IFA_F_NODAD, name: "nodad"},
	{bit: unix.IFA_F_OPTIMISTIC, name: "optimistic"},
	{bit: unix.IFA_F_DADFAILED, name: "dadfailed"},
	{bit: unix.IFA_F_HOMEADDRESS, name: "home"},
	{bit: unix.IFA_F_DEPRECATED, name: "deprecated"},
	{bit: unix.IFA_F_TENTATIVE, name: "tentative"},
	{bit: unix.IFA_F_PERMANENT, name: "permanent", permanentInverted: true},
	{bit: unix.IFA_F_MANAGETEMPADDR, name: "mngtmpaddr"},
	{bit: unix.IFA_F_NOPREFIXROUTE, name: "noprefixroute"},
	{bit: unix.IFA_F_MCAUTOJOIN, name: "autojoin"},
	{bit: unix.IFA_F_STABLE_PRIVACY, name: "stable-privacy"},
}

// IfaFlagTokens renders ifa_flags the way print_ifa_flags does, returning the
// tokens so the JSON renderer can reuse the same decisions.
//
// Unrecognized bits become a single `flags %02x` token, the same shape
// FlagTokens uses for unrecognized IFF_* bits but with a keyword and a width.
func IfaFlagTokens(flags uint32, family uint8) []string {
	var out []string
	rest := flags
	for _, f := range ifaFlagNames {
		if f.unreachable {
			continue
		}
		switch {
		case f.permanentInverted:
			if rest&f.bit == 0 {
				out = append(out, "dynamic")
			}
		case rest&f.bit != 0:
			if f.v6Secondary && family == unix.AF_INET6 {
				out = append(out, "temporary")
			} else {
				out = append(out, f.name)
			}
		}
		rest &= ^f.bit
	}
	if rest != 0 {
		out = append(out, fmt.Sprintf("flags %02x", rest))
	}
	return out
}

// AddrView is one address as `ip addr show` presents it.
//
// The JSON tags are `ip -j addr show`'s key names, taken from the
// print_*(PRINT_ANY, "<key>", …) call sites in print_addrinfo rather than from
// sample output, so a key that `ip` emits only in JSON is still right.
type AddrView struct {
	// Family and FamilyIndex are mutually exclusive, and which one is set
	// changes the JSON key as well as the text: print_addrinfo emits
	// "family" for a family it can name and "family_index" for one it
	// cannot (ip/ipaddress.c:1596-1601).
	Family      string `json:"family,omitempty"`
	FamilyIndex *uint8 `json:"family_index,omitempty"`

	Local     string `json:"local"`
	PrefixLen uint8  `json:"prefixlen"`

	// Address is `ip`'s "address" key, which is the PEER on a point-to-point
	// address and is emitted only when it differs from Local. The naming is
	// iproute2's and it is the reverse of the wire's: IFA_LOCAL is the local
	// address and IFA_ADDRESS is the peer.
	Address   string `json:"address,omitempty"`
	Broadcast string `json:"broadcast,omitempty"`

	Scope string `json:"scope"`
	// Flags are the print_ifa_flags tokens. `ip -j` emits each as its own
	// boolean key rather than an array; goip emits the array, which is the
	// one deliberate JSON key-shape divergence in this package, taken because
	// a caller cannot enumerate booleans it does not know the names of.
	Flags []string `json:"flags,omitempty"`

	Proto string `json:"protocol,omitempty"`
	Label string `json:"label,omitempty"`

	// ValidLifetime and PreferredLifetime are pointers because absent
	// IFA_CACHEINFO prints no lifetime line at all, which is different from a
	// lifetime of zero. 0xFFFFFFFF is "forever".
	ValidLifetime     *uint32 `json:"valid_life_time,omitempty"`
	PreferredLifetime *uint32 `json:"preferred_life_time,omitempty"`

	// deprecated selects `%dsec` over `%usec` for the preferred lifetime.
	// print_addrinfo branches on IFA_F_DEPRECATED for that one field, so a
	// kernel reporting a negative preferred lifetime on a deprecated address
	// would print the negative rather than a huge unsigned. Keeping the
	// branch means keeping the bit.
	deprecated bool
}

// AddrViewOf resolves a decoded address for rendering.
//
// The IFA_LOCAL/IFA_ADDRESS aliasing is already done by xtcpnl.ParseNewAddr in
// both directions, so what is left here is the comparison print_addrinfo makes
// between them: the peer is printed only when the two differ, over the
// family's address length rather than the slice length
// (ip/ipaddress.c:1607-1617).
func AddrViewOf(ai xtcpnl.AddrInfo) AddrView {
	v := AddrView{
		Family:    familyName(ai.Family),
		Local:     addrString(ai.Local, ai.Family),
		PrefixLen: ai.Prefixlen,
		Broadcast: addrString(ai.Broadcast, ai.Family),
		Scope:     scopeName(ai.Scope),
		Flags:     IfaFlagTokens(ai.Flags, ai.Family),
		Label:     ai.Label,
	}
	if v.Family == "" {
		fam := ai.Family
		v.FamilyIndex = &fam
	}
	if !equalAddrBytes(ai.Address, ai.Local, ai.Family) {
		v.Address = addrString(ai.Address, ai.Family)
	}
	if ai.Proto != 0 {
		v.Proto = addrProtoName(ai.Proto)
	}
	if ai.HasCacheInfo {
		valid := ai.CacheInfo.Valid
		preferred := ai.CacheInfo.Preferred
		v.ValidLifetime = &valid
		v.PreferredLifetime = &preferred
		v.deprecated = ai.Flags&unix.IFA_F_DEPRECATED != 0
	}
	return v
}

// equalAddrBytes is print_addrinfo's memcmp: four bytes for AF_INET and
// sixteen for everything else, which is the literal
// `ifa->ifa_family == AF_INET ? 4 : 16`. Comparing the full slices instead
// would be equivalent here and wrong on a family whose address is longer than
// the field iproute2 looks at.
func equalAddrBytes(a, b []byte, family uint8) bool {
	n := 16
	if family == unix.AF_INET {
		n = 4
	}
	if len(a) < n || len(b) < n {
		return len(a) == len(b) && string(a) == string(b)
	}
	return string(a[:n]) == string(b[:n])
}

// addrString is format_host_rta with name resolution off, which is what `ip`
// does for an address dump unless -r is given.
//
// netip is used rather than a hand-rolled formatter because its output is
// RFC 5952 and so is glibc's inet_ntop: both compress the longest run of two
// or more zero groups, leave a single zero group uncompressed, strip leading
// zeros within a group, and lowercase the hex. The committed sidecar exercises
// all four (`fd10:10:4::2`, `fe80::609:73ff:fecf:d8d0`).
//
// The agreement stops at IPv4-mapped addresses, and netip.Addr.Unmap is
// deliberately NOT called here. glibc renders a 16-byte
// 0:…:ffff:c000:0201 as "::ffff:192.0.2.1" (measured), keeping the prefix,
// whereas Unmap would turn it into the bare "192.0.2.1". Sixteen bytes came
// off the wire on an AF_INET6 address, so sixteen bytes' worth of text is the
// faithful rendering; the kernel does not put a mapped address on an
// interface, which is why the divergence had to be reasoned about rather than
// observed.
//
// A length that matches neither family renders as bare lowercase hex rather
// than being dropped, so a malformed attribute is visible instead of silently
// becoming an empty field.
func addrString(b []byte, family uint8) string {
	if len(b) == 0 {
		return ""
	}
	switch {
	case family == unix.AF_INET && len(b) == 4,
		family == unix.AF_INET6 && len(b) == 16:
		if a, ok := netip.AddrFromSlice(b); ok {
			return a.String()
		}
	}
	var sb strings.Builder
	for _, c := range b {
		fmt.Fprintf(&sb, "%02x", c)
	}
	return sb.String()
}

// Text renders one address line, plus its lifetime continuation line when the
// reply carried IFA_CACHEINFO. Newline terminated.
//
// print_addrinfo's field order (ip/ipaddress.c:1578-1707), which is not the
// order the attributes arrive in:
//
//	inet 172.16.50.219/24 brd 172.16.50.255 scope global dynamic noprefixroute enp1s0
//	   valid_lft 47871sec preferred_lft 47871sec
//
// Three spacing details, all transcribed:
//
//   - the prefix is `"    %s "` on the family name, and `/%d ` on the prefix
//     length, so every field carries its own trailing space;
//   - the label is `"%s"` with no trailing space, so a v4 line ends flush and
//     a v6 line — which has no IFA_LABEL — ends in a trailing space. ip_addr_n
//     shows both, and the difference is not cosmetic for a byte comparison;
//   - the lifetime line is indented seven spaces, not four.
func (v AddrView) Text() string {
	var b strings.Builder

	switch {
	case v.Family != "":
		fmt.Fprintf(&b, "    %s ", v.Family)
	case v.FamilyIndex != nil:
		// The unnameable-family fallback is a whole different format string.
		fmt.Fprintf(&b, "    family %d ", *v.FamilyIndex)
	}

	b.WriteString(v.Local)
	if v.Address != "" {
		fmt.Fprintf(&b, " peer %s", v.Address)
	}
	fmt.Fprintf(&b, "/%d ", v.PrefixLen)

	if v.Broadcast != "" {
		fmt.Fprintf(&b, "brd %s ", v.Broadcast)
	}
	fmt.Fprintf(&b, "scope %s ", v.Scope)
	for _, f := range v.Flags {
		fmt.Fprintf(&b, "%s ", f)
	}
	if v.Proto != "" {
		fmt.Fprintf(&b, "proto %s ", v.Proto)
	}
	b.WriteString(v.Label)

	if v.ValidLifetime != nil && v.PreferredLifetime != nil {
		b.WriteString("\n       valid_lft ")
		b.WriteString(lifetime(*v.ValidLifetime, false))
		b.WriteString(" preferred_lft ")
		b.WriteString(lifetime(*v.PreferredLifetime, v.deprecated))
	}
	b.WriteString("\n")
	return b.String()
}

// lifetime is the valid_lft/preferred_lft value: "forever" for
// INFINITY_LIFE_TIME, else the seconds with a "sec" suffix. signed selects
// `%dsec` over `%usec`, which print_addrinfo does for the preferred lifetime
// of a deprecated address only.
func lifetime(v uint32, signed bool) string {
	if v == xtcpnl.IfaLifetimeInfinityCst {
		return "forever"
	}
	if signed {
		return strconv.FormatInt(int64(int32(v)), 10) + "sec"
	}
	return strconv.FormatUint(uint64(v), 10) + "sec"
}

// AddrGroupView is what `ip addr show` prints for one link: the link stanza,
// then that link's addresses.
//
// The nesting mirrors iproute2's own loop, which walks the *link* list and
// calls print_selected_addrinfo per link (ip/ipaddress.c:2320-2334) rather
// than walking addresses. That is why an address whose ifindex matches no link
// in the dump is not printed at all, and why the JSON shape is a link object
// with an addr_info array inside it.
type AddrGroupView struct {
	LinkView
	AddrInfo []AddrView `json:"addr_info"`
}

// Text renders the link stanza and its address lines.
//
// Ranged by index: AddrView is 176 bytes and Text reads it, so the value form
// copies every address in the group to call a method on it.
func (g AddrGroupView) Text() string {
	var b strings.Builder
	b.WriteString(g.LinkView.Text())
	for i := range g.AddrInfo {
		b.WriteString(g.AddrInfo[i].Text())
	}
	return b.String()
}
