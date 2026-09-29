package render

import (
	"encoding/hex"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NeighShowFilter carries the one thing about `ip neigh show`'s arguments that
// changes what print_neigh emits, as opposed to which neighbors reach it.
//
// It is the neighbor counterpart of RouteShowFilter and has the same polarity:
// naming the device REMOVES the `dev` token from every line, because it is
// already in the command line (ip/ipneigh.c:415).
//
// What it does NOT share with route is the transaction consequence. There, the
// suppressed token was the only caller of ll_index_to_name for RTA_OIF, so
// hiding it deleted netlink traffic. Here ll_init_map has already dumped every
// link before the neighbor dump is even sent (:597), so the cache is full
// either way and the suppression is purely textual. Same guard shape, same
// source, entirely different cost — see req.NeighShowDump.
type NeighShowFilter struct {
	// IndexSet is `filter.index != 0`, i.e. a `dev NAME` selector was given
	// and resolved. True suppresses the `dev` token, in text and in JSON.
	//
	// A bool rather than the index, because print_neigh's guard never
	// compares the two: `if (!filter.index && r->ndm_ifindex)` tests only
	// that a filter exists. The per-neighbor index test is a separate,
	// earlier statement (:331).
	IndexSet bool
}

// NeighView is the iproute2-compatible typed presentation of one neighbor.
type NeighView struct {
	Dst string `json:"dst"`

	// Dev is omitempty for two reasons that coincide. print_neigh's guard
	// (:415) has two arms — it skips the token under a device filter, and it
	// also skips it for a neighbor whose ndm_ifindex is 0 — and iproute2's
	// print_color_string emits the JSON key only when it prints the text, so
	// in both cases the key is absent rather than empty. NeighViewOf leaves
	// this "" for either.
	Dev    string   `json:"dev,omitempty"`
	LLAddr string   `json:"lladdr,omitempty"`
	State  []string `json:"state"`
}

func NeighViewOf(n xtcpnl.NeighInfo, names NameTab, f NeighShowFilter) NeighView {
	v := NeighView{State: neighStates(n.State)}
	if !f.IndexSet && n.Ifindex != 0 {
		v.Dev = names.IndexToName(n.Ifindex)
	}
	if a, ok := netip.AddrFromSlice(n.Dst); ok {
		v.Dst = a.String()
	}
	if len(n.LLAddr) > 0 {
		s := hex.EncodeToString(n.LLAddr)
		var b strings.Builder
		for i := 0; i < len(s); i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			b.WriteString(s[i : i+2])
		}
		v.LLAddr = b.String()
	}
	return v
}

func neighStates(state uint16) []string {
	s := xtcpnl.NudStateString(state)
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimPrefix(parts[i], "NUD_")
	}
	return parts
}

func (v NeighView) Text() string {
	var b strings.Builder
	b.WriteString(v.Dst)
	b.WriteByte(' ')
	// print_neigh writes the literal `dev ` and the name as two calls
	// (ip/ipneigh.c:416-421), both inside the one guard, so an empty Dev drops
	// the keyword with it rather than leaving a bare `dev`.
	if v.Dev != "" {
		b.WriteString("dev ")
		b.WriteString(v.Dev)
		b.WriteByte(' ')
	}
	if v.LLAddr != "" {
		b.WriteString("lladdr ")
		b.WriteString(v.LLAddr)
		b.WriteByte(' ')
	}
	b.WriteString(strings.Join(v.State, " "))
	b.WriteString(" \n")
	return b.String()
}
