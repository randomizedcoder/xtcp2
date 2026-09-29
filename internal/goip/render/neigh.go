package render

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
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
	Dev    string `json:"dev,omitempty"`
	LLAddr string `json:"lladdr,omitempty"`

	// Flags are the print_null tokens of ndm_flags, and they are `json:"-"`
	// for the reason AddrView.Flags is: `ip -j` gives each its own key
	// (ip/ipneigh.c:440-451). The value is null, not true — print_null is a
	// different function from print_bool — so MarshalJSON below expands them
	// rather than a `[]string` tag doing it.
	Flags []string `json:"-"`

	// State is omitempty because print_neigh guards the whole call on
	// `if (r->ndm_state)` (:462-463) and print_neigh_state opens the JSON
	// array from inside it (:239-240). A zero state therefore emits no
	// "state" key at all, which is not the same as an empty array — and it
	// is the normal case for a proxy entry, so this is reachable rather than
	// theoretical.
	State []string `json:"state,omitempty"`
}

func NeighViewOf(n xtcpnl.NeighInfo, names NameTab, f NeighShowFilter) NeighView {
	v := NeighView{State: neighStates(n.State), Flags: NeighFlagTokens(n.Flags)}
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
	if state == 0 {
		// print_neigh does not call print_neigh_state at all here
		// (ip/ipneigh.c:462). Returning []string{"NONE"} — which
		// NudStateString would give — would print a token `ip` never
		// prints and add a JSON key it never emits.
		return nil
	}
	s := xtcpnl.NudStateString(state)
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimPrefix(parts[i], "NUD_")
	}
	return parts
}

// neighFlagNames is the ndm_flags half of print_neigh's flag run
// (ip/ipneigh.c:440-451), in its source order, which is also its print order.
//
// # Why four and not six
//
// The run is six `if`s, but two of them — `managed` (:444-445) and
// `extern_valid` (:450-451) — test ext_flags, the u32 of NDA_FLAGS_EXT, and
// not ndm_flags. xtcpnl does not decode that attribute, so those two are
// unreachable regardless of what this table says; listing them with the wrong
// source would be worse than omitting them. They stay a reported gap.
//
// # The bits this deliberately does not name
//
// ndm_flags also carries NTF_USE (0x01), NTF_SELF (0x02), NTF_MASTER (0x04)
// and NTF_STICKY (0x40). print_neigh prints none of them and emits no residue
// key for them either, which is the difference from print_ifa_flags and the
// reason NeighFlagTokens returns one value where IfaFlagTokens returns two:
// there is nothing to report the leftovers as.
var neighFlagNames = []struct {
	bit  uint8
	name string
}{
	{unix.NTF_ROUTER, "router"},
	{unix.NTF_PROXY, "proxy"},
	{unix.NTF_EXT_LEARNED, "extern_learn"},
	{unix.NTF_OFFLOADED, "offload"},
}

// NeighFlagTokens renders ndm_flags as print_neigh's named tokens, in print
// order. A flags byte with no named bit set returns nil, which prints nothing
// and emits no JSON key.
func NeighFlagTokens(flags uint8) []string {
	var out []string
	for _, f := range neighFlagNames {
		if flags&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	return out
}

// MarshalJSON appends one null-valued member per named ndm_flags bit.
//
// The value is `null` because iproute2 uses print_null, which reaches
// jsonw_null_field (lib/json_writer.c:336-340) and writes the name followed
// by a bare null. `ip -j neigh show proxy` therefore emits `"proxy": null`,
// not `"proxy": true` — the shape AddrView.MarshalJSON produces for
// print_bool. Two similar-looking expansions, two different literals, and the
// only way to get either right is to follow the print function.
//
// Member position and the key-by-key comparison argument are exactly as
// documented on AddrView.MarshalJSON; NeighView is likewise not embedded
// anywhere, so the marshaler composes rather than shadowing a parent's fields.
func (v NeighView) MarshalJSON() ([]byte, error) {
	// neighView sheds this method, so json.Marshal below does not recurse.
	type neighView NeighView

	raw, err := json.Marshal(neighView(v))
	if err != nil {
		return nil, err
	}
	if len(v.Flags) == 0 {
		return raw, nil
	}
	// Dst has no omitempty, so the object always has a member to append to
	// and the comma is always needed. Checked rather than assumed, as in
	// AddrView.MarshalJSON.
	if len(raw) < 2 || raw[len(raw)-1] != '}' || raw[len(raw)-2] == '{' {
		return nil, fmt.Errorf("render: NeighView marshaled to %q, which has no member to append to", raw)
	}

	var b bytes.Buffer
	b.Write(raw[:len(raw)-1])
	for _, f := range v.Flags {
		key, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		b.WriteByte(',')
		b.Write(key)
		b.WriteString(":null")
	}
	b.WriteByte('}')
	return b.Bytes(), nil
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
	// The flag run (:440-451) sits between lladdr and the state, and every
	// token there carries its own trailing space from "%s ". The states do
	// too, via print_neigh_state's PRINT_FLAG (:245), which is why both are
	// written a token at a time rather than joined: a zero state joins to ""
	// and would leave a doubled space before the newline.
	for _, f := range v.Flags {
		b.WriteString(f)
		b.WriteByte(' ')
	}
	for _, s := range v.State {
		b.WriteString(s)
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}
