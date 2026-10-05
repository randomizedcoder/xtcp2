package render

import (
	"bytes"
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

	// Stats is show_stats, gating print_cacheinfo and the `probes` token
	// together (ip/ipneigh.c:453-460).
	//
	// Unlike the route object, where `-s` gates three members a dump can
	// never populate, this gate is live: NDA_CACHEINFO is on every entry the
	// kernel returns and its counters are the entry's real ages, so `-s
	// neigh show` genuinely prints more than `neigh show`.
	Stats bool
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

	// Flags are the print_null tokens of ndm_flags and NDA_FLAGS_EXT, already
	// merged into print order by NeighFlagTokens, and they are `json:"-"`
	// for the reason AddrView.Flags is: `ip -j` gives each its own key
	// (ip/ipneigh.c:440-451). The value is null, not true — print_null is a
	// different function from print_bool — so MarshalJSON below expands them
	// rather than a `[]string` tag doing it.
	Flags []string `json:"-"`

	// State is `json:"-"` and hand-written by MarshalJSON for one reason
	// only: POSITION. print_neigh emits the flag run at :440-451 and the
	// state at :462-463, in that order, and `ip -j` writes keys in print
	// order — so "state" has to follow the flags. A struct tag cannot express
	// that, because the flags are not struct fields; they are expanded from
	// Flags. Leaving this as `json:"state,omitempty"` put it before every
	// flag, which was correct-looking for as long as no captured neighbor
	// carried a flag at all.
	//
	// The omitempty it used to carry is reproduced by the length test in
	// MarshalJSON, and it is load-bearing rather than tidiness: print_neigh
	// guards the whole call on `if (r->ndm_state)` (:462-463) and
	// print_neigh_state opens the JSON array from inside it (:239-240). A
	// zero state emits no "state" key at all, which is not an empty array —
	// and that is the normal case for a proxy entry, so it is reachable
	// rather than theoretical.
	State []string `json:"-"`

	// The `-s` block: print_cacheinfo's four counters (ip/ipneigh.c:220-234)
	// followed by `probes` (:457-459). Both sit inside one `if (show_stats)`
	// (:453-460), between the flag run and the state.
	//
	// All five are `json:"-"` and hand-written by MarshalJSON for the same
	// POSITION reason as State: they have to land after the flags, which are
	// not struct fields, so no struct tag can express the order.
	//
	// Pointers because presence is not value. Refcnt is suppressed at zero
	// by its own guard, while Used, Confirmed and Updated print
	// unconditionally once NDA_CACHEINFO exists — so a zero Used is a
	// printed `used 0` and an absent one is no token at all. Probes is the
	// same: `probes 0` is a real output.
	//
	// Declared in PRINT order, which is not the order of `struct
	// nda_cacheinfo`. The struct is confirmed, used, updated, refcnt; the
	// print is ref, used, confirmed, updated. Anyone checking this against
	// the header rather than against print_cacheinfo will read it as two
	// transpositions.
	Refcnt    *uint32 `json:"-"`
	Used      *uint32 `json:"-"`
	Confirmed *uint32 `json:"-"`
	Updated   *uint32 `json:"-"`
	Probes    *uint32 `json:"-"`
}

func NeighViewOf(n xtcpnl.NeighInfo, names NameTab, f NeighShowFilter) NeighView {
	v := NeighView{State: neighStates(n.State), Flags: NeighFlagTokens(n.Flags, n.FlagsExt)}
	if !f.IndexSet && n.Ifindex != 0 {
		v.Dev = names.IndexToName(n.Ifindex)
	}
	if a, ok := netip.AddrFromSlice(n.Dst); ok {
		v.Dst = a.String()
	}
	if f.Stats {
		// print_cacheinfo (ip/ipneigh.c:220-234). `ref` is the only one of
		// the four with a guard; the other three print whenever the
		// attribute is present, zero or not. All three are divided by
		// USER_HZ, which truncates.
		if n.HasCacheInfo {
			if n.CacheInfo.Refcnt != 0 {
				r := n.CacheInfo.Refcnt
				v.Refcnt = &r
			}
			used := n.CacheInfo.Used / xtcpnl.RtaUserHzCst
			confirmed := n.CacheInfo.Confirmed / xtcpnl.RtaUserHzCst
			updated := n.CacheInfo.Updated / xtcpnl.RtaUserHzCst
			v.Used, v.Confirmed, v.Updated = &used, &confirmed, &updated
		}
		// `probes` is NOT inside print_cacheinfo and NOT conditional on
		// NDA_CACHEINFO: it is a sibling attribute under the same show_stats
		// guard (:457-459), so an entry with probes and no cacheinfo prints
		// the one without the other.
		if n.HasProbes {
			p := n.Probes
			v.Probes = &p
		}
	}
	if len(n.LLAddr) > 0 {
		// The THIRD consumer of ll_addr_n2a, and the only one that has to go
		// looking for its type. print_neigh passes
		// ll_index_to_type(r->ndm_ifindex) (ip/ipneigh.c:428-430) because
		// struct ndmsg carries no type of its own — so on a tunnel device an
		// NDA_LLADDR of four bytes is a dotted quad, not colon-hex.
		//
		// Note this uses n.Ifindex directly rather than anything derived from
		// f: the NeighShowFilter suppresses the printed `dev` token, but the
		// type lookup still happens on the real index. Reusing the suppressed
		// value here would silently format every entry as ARPHRD_NETROM on
		// `neigh show dev NAME`.
		v.LLAddr = xtcpnl.LLAddrN2A(n.LLAddr, names.IndexToType(n.Ifindex))
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

// neighFlagNames is print_neigh's flag run (ip/ipneigh.c:440-451) in its
// source order, which is also its print order.
//
// # Six ifs, but two words
//
// The run reads like one block of ndm_flags tests and is not. `managed`
// (:444-445) and `extern_valid` (:450-451) test ext_flags — the u32 of
// NDA_FLAGS_EXT — while the other four test ndm_flags, and the `ext` column
// here is which. Keeping them in one table rather than two is what preserves
// the interleaving: `managed` prints THIRD, between two ndm_flags tokens, and
// `extern_valid` prints last. Two tables concatenated would put both at one
// end and quietly reorder any line carrying a bit from each.
//
// The bits themselves give no hint of which word they came from, which is why
// the column is not inferable. NTF_EXT_MANAGED is 1<<0 and so is NTF_USE;
// testing either against the wrong word matches and prints a token `ip` would
// not. See the NtfExt* constants in xtcpnl.
//
// # The bits this deliberately does not name
//
// ndm_flags also carries NTF_USE (0x01), NTF_SELF (0x02), NTF_MASTER (0x04)
// and NTF_STICKY (0x40), and ext_flags carries NTF_EXT_LOCKED (1<<1).
// print_neigh prints none of the five and emits no residue key for them
// either, which is the difference from print_ifa_flags and the reason
// NeighFlagTokens returns one value where IfaFlagTokens returns two: there is
// nothing to report the leftovers as. NTF_EXT_LOCKED is the clearest of them
// — iproute2 prints it only from bridge/fdb.c:121, because it describes a
// bridge FDB entry and not a neighbor.
var neighFlagNames = []struct {
	bit uint32
	// ext selects the word: true reads NDA_FLAGS_EXT, false ndm_flags.
	ext  bool
	name string
}{
	{unix.NTF_ROUTER, false, "router"},
	{unix.NTF_PROXY, false, "proxy"},
	{xtcpnl.NtfExtManaged, true, "managed"},
	{unix.NTF_EXT_LEARNED, false, "extern_learn"},
	{unix.NTF_OFFLOADED, false, "offload"},
	{xtcpnl.NtfExtExtValidated, true, "extern_valid"},
}

// NeighFlagTokens renders ndm_flags and NDA_FLAGS_EXT as print_neigh's named
// tokens, in print order. Two words in, because the six tokens come from two
// (ip/ipneigh.c:440-451); no named bit set in either returns nil, which prints
// nothing and emits no JSON key.
func NeighFlagTokens(flags uint8, flagsExt uint32) []string {
	var out []string
	for _, f := range neighFlagNames {
		word := uint32(flags)
		if f.ext {
			word = flagsExt
		}
		if word&f.bit != 0 {
			out = append(out, f.name)
		}
	}
	return out
}

// MarshalJSON appends one null-valued member per named flag token and then
// the state array, in the order NeighFlagTokens produced them — which is
// print_neigh's order across both ndm_flags and NDA_FLAGS_EXT, not one word's
// and then the other's.
//
// # Why state is spliced here rather than tagged
//
// Because it comes AFTER the flags. print_neigh's six flag ifs are at
// :440-451 and the state is at :462-463, `ip -j` writes keys in print order,
// and a struct tag cannot place a field after members that are not fields.
// Verified against the pinned iproute2 7.1.0 on a live namespace: `ip -j` for
// a neighbor with three flags gives
//
//	{"dst":…,"dev":…,"lladdr":…,"router":null,"extern_learn":null,
//	 "extern_valid":null,"state":["PERMANENT"]}
//
// The two omitempty rules are therefore hand-written below. They are not the
// same rule: a flagless neighbor omits every flag key, a zero-state neighbor
// omits "state", and a proxy entry is both at once.
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
	if len(v.Flags) == 0 && len(v.State) == 0 &&
		v.Refcnt == nil && v.Used == nil && v.Probes == nil {
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
	// The `-s` keys, in print order, after the flags and before the state.
	// Used/Confirmed/Updated are one text token but three JSON keys, because
	// print_cacheinfo makes three print_uint calls whose text formats happen
	// to concatenate (ip/ipneigh.c:231-234).
	for _, kv := range []struct {
		key string
		val *uint32
	}{
		{"refcnt", v.Refcnt},
		{"used", v.Used},
		{"confirmed", v.Confirmed},
		{"updated", v.Updated},
		{"probes", v.Probes},
	} {
		if kv.val == nil {
			continue
		}
		fmt.Fprintf(&b, `,"%s":%d`, kv.key, *kv.val)
	}
	if len(v.State) > 0 {
		state, err := json.Marshal(v.State)
		if err != nil {
			return nil, err
		}
		b.WriteString(`,"state":`)
		b.Write(state)
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
	// The `-s` block, between the flags and the state.
	//
	// print_cacheinfo's formats carry a LEADING space and no trailing one —
	// `" ref %u"`, `" used %u"`, `"/%u"`, `"/%u"` — so the block starts with
	// a space the flag run has already supplied one of, giving the doubled
	// space real `ip` prints after an lladdr. `probes %u ` then has no
	// leading space and a trailing one, which is why its output runs
	// straight on from the last counter as `…/1362276probes 3 `. Both
	// oddities are upstream's; see the golden.
	if v.Refcnt != nil {
		fmt.Fprintf(&b, " ref %d", *v.Refcnt)
	}
	if v.Used != nil {
		fmt.Fprintf(&b, " used %d/%d/%d", *v.Used, *v.Confirmed, *v.Updated)
	}
	if v.Probes != nil {
		fmt.Fprintf(&b, "probes %d ", *v.Probes)
	}
	for _, s := range v.State {
		b.WriteString(s)
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}
