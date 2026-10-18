package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is `ip -br addr show` and the `-o` address line (A3-T02;
// docs/netlink/addr-write-path.md, "Output modes"). Both are compared byte for
// byte with the `ip_addr_br`, `ip_addr_br_json` and `ip_addr_o` sidecars of
// every addr-show state (internal/goip TestAddrBriefMatchesSidecar and
// TestAddrOnelineMatchesSidecar).

// LinkBriefView is print_linkinfo_brief's link half (ip/ipaddress.c:851-861):
// the name column from print_name_and_link and the operstate. It is the part
// `-br link show` shares, which is why it is a type of its own; the AF_PACKET
// tail (address and flags) is not here, because `-br -0` is refused.
type LinkBriefView struct {
	// Link and LinkIndex are print_name_and_link's two JSON arms, set exactly
	// as LinkViewOf sets them, and emitted before "ifname" as it emits them.
	Link      *LinkTarget
	LinkIndex *int32

	IfName string

	// nameSuffix is the text after '@': the peer name, NONE, or if%u.
	nameSuffix string

	// operState is the name, or "" when IFLA_OPERSTATE was absent, which
	// print_linkinfo_brief tests for presence. operStateIndex is set instead
	// for a value past the table, which print_operstate prints as
	// "state %#llx" (or "operstate_index" in JSON) before it looks at brief.
	operState      string
	operStateIndex *uint8
}

// LinkBriefViewOf builds the brief name column the way LinkViewOf builds the
// stanza's, so the two cannot disagree on `@NONE` or `@if%u`.
func LinkBriefViewOf(li xtcpnl.LinkInfo, names NameTab) LinkBriefView {
	lv := LinkViewOf(li, names)
	v := LinkBriefView{
		Link:       lv.Link,
		LinkIndex:  lv.LinkIndex,
		IfName:     lv.IfName,
		nameSuffix: lv.nameSuffix,
	}
	if li.HasOperState {
		if int(li.OperState) < len(operStates) {
			v.operState = operStates[li.OperState]
		} else {
			st := li.OperState
			v.operStateIndex = &st
		}
	}
	return v
}

// Text is `"%-16s "` on the name (with `@link` when there is one) and
// `"%-14s "` on the operstate. A name longer than the column is not cut, so
// the column widens by exactly the overflow.
func (v LinkBriefView) Text() string {
	name := v.IfName
	if v.nameSuffix != "" {
		name += "@" + v.nameSuffix
	}
	s := fmt.Sprintf("%-16s ", name)
	switch {
	case v.operStateIndex != nil:
		s += fmt.Sprintf("state %#x", *v.operStateIndex)
	case v.operState != "":
		s += fmt.Sprintf("%-14s ", v.operState)
	}
	return s
}

// members writes the link half's JSON members in print_name_and_link and
// print_operstate's order.
func (v LinkBriefView) members(w *jsonObject) {
	switch {
	case v.Link != nil:
		w.member("link", v.Link)
	case v.LinkIndex != nil:
		w.member("link_index", *v.LinkIndex)
	}
	w.member("ifname", v.IfName)
	switch {
	case v.operStateIndex != nil:
		w.member("operstate_index", *v.operStateIndex)
	case v.operState != "":
		w.member("operstate", v.operState)
	}
}

// AddrBriefEntry is one address under -br: print_addrinfo up to its
// `goto brief_exit` (ip/ipaddress.c:1602-1630), which is the local block
// alone — local, the peer when it differs, the prefix length and the metric.
type AddrBriefEntry struct {
	hasLocal  bool
	local     string
	address   string
	prefixLen uint8
	metric    *uint32
}

// AddrBriefEntryOf takes the brief fields from the full view, so the peer and
// metric tests are the ones the plain render uses.
func AddrBriefEntryOf(a AddrView) AddrBriefEntry {
	return AddrBriefEntry{
		hasLocal:  a.hasLocal,
		local:     a.Local,
		address:   a.Address,
		prefixLen: a.PrefixLen,
		metric:    a.Metric,
	}
}

// Text is `local[ peer address]/plen [metric N ]`, or nothing for a message
// with no address.
func (e AddrBriefEntry) Text() string {
	if !e.hasLocal {
		return ""
	}
	var b strings.Builder
	b.WriteString(e.local)
	if e.address != "" {
		fmt.Fprintf(&b, " peer %s", e.address)
	}
	fmt.Fprintf(&b, "/%d ", e.prefixLen)
	if e.metric != nil {
		fmt.Fprintf(&b, "metric %d ", *e.metric)
	}
	return b.String()
}

// MarshalJSON is the same members in the same order. A message with no
// address is `{}`, the empty object print_selected_addrinfo opens for it.
func (e AddrBriefEntry) MarshalJSON() ([]byte, error) {
	w := jsonObject{}
	if e.hasLocal {
		w.member("local", e.local)
		if e.address != "" {
			w.member("address", e.address)
		}
		w.member("prefixlen", e.prefixLen)
		if e.metric != nil {
			w.member("metric", *e.metric)
		}
	}
	return w.bytes()
}

// AddrBriefView is one link's `-br addr show` line: the link brief, then each
// address, then the "\n" print_selected_addrinfo writes under brief
// (ip/ipaddress.c:1753-1755). A link with no address still prints its line.
type AddrBriefView struct {
	LinkBriefView
	AddrInfo []AddrBriefEntry
}

// Text is the whole line, newline included.
func (g AddrBriefView) Text() string {
	var b strings.Builder
	b.WriteString(g.LinkBriefView.Text())
	for i := range g.AddrInfo {
		b.WriteString(g.AddrInfo[i].Text())
	}
	b.WriteString("\n")
	return b.String()
}

// MarshalJSON is the link members, then "addr_info", which `ip` opens even
// when it stays empty.
func (g AddrBriefView) MarshalJSON() ([]byte, error) {
	w := jsonObject{}
	g.members(&w)
	addrs := g.AddrInfo
	if addrs == nil {
		addrs = []AddrBriefEntry{}
	}
	w.member("addr_info", addrs)
	return w.bytes()
}

// OnelineText is the `-o` address line (ip/ipaddress.c:1583-1594): the
// `"%u: %s"` prefix print_addrinfo writes under filter.oneline, then the
// ordinary line with _SL_ = "\\" (ip/ip.c:301), so the lifetime break becomes
// a backslash and only the final newline stays.
func (v AddrView) OnelineText(index int32, dev string) string {
	body := strings.TrimSuffix(v.Text(), "\n")
	return fmt.Sprintf("%d: %s", index, dev) + strings.ReplaceAll(body, "\n", `\`) + "\n"
}
