// Package goip is the implementation behind cmd/goip: an idiomatic Go `ip`,
// read-only, whose purpose is to make coverage gaps in pkg/xtcpnl fail a
// build rather than go unnoticed.
//
// cmd/goip is a thin main over Run; everything testable lives here.
package goip

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// LLTab is iproute2's interface index cache, `struct ll_cache`
// (lib/ll_map.c:28-35) reduced to the three fields a read-only `show` reads
// back: name, flags and type.
//
// # Why a cache at all, when the dump already has every link
//
// Because `ip` has one, and because what it does on a *miss* is observable in
// the output. Two renderings need an index resolved that the message being
// rendered does not carry: `master br-3a5828b2963a` (IFLA_MASTER is an index)
// and the `@peer` suffix (IFLA_LINK is an index). iproute2 answers both from
// this cache, and when the cache cannot answer it falls back in specific ways
// — `if%u` for a name, and -1 for flags — that goip has to reproduce or it
// prints different text.
//
// # Filling it costs no extra netlink traffic for `link show`
//
// The dump `link show` already runs carries every link, so Fill is handed the
// decoded replies. That is deliberate: `ll_index_to_name` will issue a live
// single-get (`ll_link_get`, lib/ll_map.c:320) for an index it does not know,
// which inside a parity capture window is a side transaction that shows up as
// an L1 transaction-count divergence. Prefilling from the dump is how goip
// avoids emitting one.
//
// The zero value is not usable; use NewLLTab.
type LLTab struct {
	byIndex map[int32]llEntry
	byName  map[string]int32
}

type llEntry struct {
	name  string
	flags uint32
	typ   uint16
}

// NewLLTab returns an empty cache.
func NewLLTab() *LLTab {
	return &LLTab{
		byIndex: make(map[int32]llEntry),
		byName:  make(map[string]int32),
	}
}

// Fill adds every link in a dump to the cache. Later entries for the same
// index replace earlier ones, which is what `ll_remember_index` does — it is
// an update path, not an attribute table, so first-wins does not apply here.
//
// Alternative names are registered too. `ll_name_to_index` resolves an altname
// (via if_nametoindex, which the kernel answers from the same namespace), so
// `ip link show dev enxe04f43e628ef` works; without this, goip would reject a
// name `ip` accepts.
//
// Ranged by index, not by value: LinkInfo is large and this loop reads four
// of its fields, so the value form copies the whole struct — including the
// AltNames slice header and the address byte slices — once per link for nothing.
func (t *LLTab) Fill(links []xtcpnl.LinkInfo) {
	for i := range links {
		li := &links[i]
		t.byIndex[li.Index] = llEntry{name: li.Name, flags: li.Flags, typ: li.Type}
		if li.Name != "" {
			t.byName[li.Name] = li.Index
		}
		for _, alt := range li.AltNames {
			t.byName[alt] = li.Index
		}
	}
}

// IndexToName resolves an index to a name, reproducing ll_index_to_name
// (lib/ll_map.c:308-330) minus its live-lookup step.
//
// Three behaviors, all of them from the source:
//
//   - index 0 is "*" (:313-314). Not an error and not "": index 0 is how the
//     kernel says "no interface", and `ip` prints the asterisk.
//   - a cached index gives its name (:316-318).
//   - a miss gives "if%u" (:327). iproute2 gets there via if_indextoname and
//     then this fallback; goip goes straight to the fallback, because the
//     if_indextoname path would resolve an index the dump did not contain,
//     which for a read-only show means the link appeared after the dump.
//
// The step deliberately skipped is :320's `ll_link_get(NULL, idx)` — a live
// RTM_GETLINK single-get. See the LLTab doc comment for why goip must not
// emit it.
func (t *LLTab) IndexToName(idx int32) string {
	if idx == 0 {
		return "*"
	}
	if e, ok := t.byIndex[idx]; ok && e.name != "" {
		return e.name
	}
	return "if" + strconv.FormatInt(int64(idx), 10)
}

// IndexToFlags returns the cached ifi_flags for an index, or **-1** on a miss,
// exactly as ll_index_to_flags does (lib/ll_map.c:343-352).
//
// The -1 is the whole point of this method having its own signature rather
// than returning (uint32, bool). Its sole caller is print_name_and_link's
// M-DOWN test, which is `m_flag = !(ll_index_to_flags(iflink) & IFF_UP)`. With
// -1 that is `!(0xffffffff & 1)` = `!1` = 0, so **a miss means no M-DOWN**.
// Returning 0 for "not found" — the natural Go reflex — would make it
// `!(0 & 1)` = `!0` = 1 and print M-DOWN on every peer the cache cannot
// resolve. Index 0 really does return 0 here (:347-348), and that one does
// yield M-DOWN.
func (t *LLTab) IndexToFlags(idx int32) int64 {
	if idx == 0 {
		return 0
	}
	if e, ok := t.byIndex[idx]; ok {
		return int64(e.flags)
	}
	return -1
}

// NameToIndex resolves a name — primary or alternative — to an index,
// returning 0 when it is unknown. 0 is iproute2's "no such device" answer from
// ll_name_to_index (lib/ll_map.c:354), and callers test for it rather than for
// an error.
func (t *LLTab) NameToIndex(name string) int32 {
	if name == "" {
		return 0
	}
	return t.byName[name]
}

// IndexToType returns the cached ifi_type, or 0. `ip` reads it through
// ll_index_to_type for address rendering, where the ARPHRD_* of the *link*
// decides how an IFA_ADDRESS is formatted.
func (t *LLTab) IndexToType(idx int32) uint16 {
	return t.byIndex[idx].typ
}
