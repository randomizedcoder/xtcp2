package goipparity

import (
	"bytes"
	"encoding/json"
	"strings"
)

// JSON stdout comparison — the `-j` half of the structural facets.
//
// # Why this file exists
//
// stdout.go's patterns are text-only, and until this file was written a `-j`
// row compared almost nothing while reporting PASS. Measured against the
// committed pkg/xtcpnl/testdata/7_1_4/dumps/ip_addr_json: reStanza never
// matches, so ifnames and ifindexes are empty; reKeyword's `\s+` never
// matches `"mtu": 65536`, so all 23 keyword facets are empty; reCIDR4 never
// matches, because JSON puts `local` and `prefixlen` in separate keys; reDev
// and reStatsHeader never match at all; and FacetLines is 1, because
// json.NewEncoder emits one line. Only FacetMACs survived, by coincidence —
// `"address": "00:00:00:00:00:00",` happens to match reMAC. A goip emitting
// `[]` would have passed.
//
// # The invariant this file is built to preserve
//
// It adds NO locus. Every element it produces lands in one of Facets() or
// FacetKeyword(kw) for kw in Keywords(), exactly as the text extractor's do,
// so StdoutLoci() is unchanged and the committed allowlist and
// TestStdoutLociAreEnumerable need no amendment. That is what rules out the
// stronger design — a structural JSON diff keyed on elided-index paths like
// `[].addr_info[].prefixlen` — which would compare more and would make the
// locus set unbounded, which stdout.go's header rests on not being.
//
// # Why it mirrors the text extractor's SHAPE and not just its facet names
//
// The text extractor runs every pattern over every line. This one runs the
// value-shaped rules — reMAC, reCIDR4, reCIDR6 — over every string value
// anywhere in the document, and reserves the key-driven tables for what the
// text form gets from POSITION rather than from content. That is why route's
// `"dst": "192.0.2.0/24"` needs no entry in any table below (the blanket CIDR
// scan finds it, as reCIDR4 does in text) while addr's `local` does (the text
// form prints a joined token that JSON never contains).
//
// The calibration is the test, not the intent: TestStdoutJSONFacetsMatchText
// runs both extractors over the same topology's text and JSON sidecars and
// requires them to agree, and requires every disagreement to be one of the
// declared ones below. A mapping invented rather than measured fails there.

// jsonEntries decodes one side's output as an iproute2 JSON listing.
//
// # Why the top level must be an array, and not merely valid JSON
//
// Every `ip -j` listing this harness compares is a JSON ARRAY of entries, and
// `ip` with no results emits `[]`. Requiring that SHAPE is what makes
// detection safe in both directions: no `ip` text output begins with `[`, so a
// text side is never misread as JSON, and a truncated or bare-object side
// fails to decode and falls through to the text extractor rather than being
// read as an empty listing — which would have been indistinguishable from a
// clean empty result.
//
// Two checks enforce the shape and only one of them is load-bearing, which was
// measured rather than assumed. Mutating each separately showed the `[` PREFIX
// does all of the work: anything that passes it decodes to an array, so
// relaxing the `[]any` assertion alone changes no outcome, while relaxing the
// prefix sends every text side down the JSON path and reddens thirty-odd rows.
// The assertion stays because jsonFacets takes a []any where decodeOrdered
// returns any — it is type correctness, not a second gate, and calling it a
// gate would overstate what it defends.
//
// The decode is ordered, because FacetStatsHeaders compares the heading order
// and a map[string]any does not have one; see decodeOrdered.
func jsonEntries(s string) ([]any, bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "[") {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader([]byte(t)))
	// Numbers as their source literals, so `"mtu": 65536` contributes the
	// element `65536` and compares against the text form's `mtu 65536`. The
	// default float64 would render large counters in exponent form and would
	// compare against nothing.
	d.UseNumber()
	v, err := decodeOrdered(d)
	if err != nil {
		return nil, false
	}
	// Trailing content after the value means this was not a single listing.
	if _, err := d.Token(); err == nil {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	return arr, true
}

// jsonMember is one key and its value, in the order the document wrote them.
type jsonMember struct {
	Key string
	Val any
}

// jsonObject is a decoded JSON object that remembers its key order.
//
// encoding/json's own map[string]any discards it, and one facet needs it:
// FacetStatsHeaders compares `RX:bytes,packets,errors,…` as a single joined
// element, so a renderer that emitted the right headings in the wrong order
// has to be a finding rather than a match. Nothing else here depends on
// order, every other facet being a multiset.
type jsonObject []jsonMember

// lookup returns the first value for a key, as iproute2's writers emit each
// key once.
func (o jsonObject) lookup(key string) (any, bool) {
	for _, m := range o {
		if m.Key == key {
			return m.Val, true
		}
	}
	return nil, false
}

// decodeOrdered reads one JSON value, decoding objects into jsonObject so key
// order survives. Scalars are returned as encoding/json's own token types:
// string, json.Number, bool or nil.
func decodeOrdered(d *json.Decoder) (any, error) {
	tok, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if delim == '{' {
		obj := jsonObject{}
		for d.More() {
			// In a well-formed object the key is always a string token, and
			// the decoder guarantees it: d.More() inside an object means a
			// member follows.
			key, err := d.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, errJSONKeyNotString
			}
			val, err := decodeOrdered(d)
			if err != nil {
				return nil, err
			}
			obj = append(obj, jsonMember{Key: name, Val: val})
		}
		if _, err := d.Token(); err != nil { // the closing brace
			return nil, err
		}
		return obj, nil
	}
	arr := []any{}
	for d.More() {
		val, err := decodeOrdered(d)
		if err != nil {
			return nil, err
		}
		arr = append(arr, val)
	}
	if _, err := d.Token(); err != nil { // the closing bracket
		return nil, err
	}
	return arr, nil
}

// errJSONKeyNotString cannot be produced by encoding/json's decoder, which
// rejects a non-string object key before Token() returns it. It exists so the
// type assertion above has an error to return rather than a panic.
var errJSONKeyNotString = jsonError("goipparity: JSON object key is not a string")

// jsonError is a sentinel error type, so this file needs no errors import for
// one constant.
type jsonError string

func (e jsonError) Error() string { return string(e) }

// The iproute2 JSON key names this file reads. Named rather than inlined
// because each is also a row in a test table, and a literal in two places is
// a literal that can disagree with itself.
const (
	jsonStatsKeyCst    = "stats64"
	jsonNextHopsKeyCst = "nexthops"
	jsonRxKeyCst       = "rx"
	jsonTxKeyCst       = "tx"
	// nextHopTokenCst is the element FacetNextHops carries. It is the text
	// form's word, SINGULAR, where the JSON key is plural: reNextHop matches
	// `nexthop` and adds the match itself, so a JSON side that added its own
	// key name would produce `nexthops` and disagree with every text row
	// while looking like a working facet.
	nextHopTokenCst = "nexthop"
)

// jsonFacetKeys maps a JSON key to the SET facet its value belongs in.
//
// These are the facets the text form fills from position rather than from
// content, which is why they need a key table at all: `1: lo:` is an ifindex
// and an ifname because of where it sits on the line, and JSON states the
// same two facts as named keys.
//
// `priority` is the entry a reader will question. A rule's priority is not an
// interface index and nothing pretends it is — but the text extractor files it
// under FacetIfIndexes, because reStanza is anchored on `^(\d+):` and
// print_rule opens every line with `100:\t`. The facet is where the text
// extractor puts the number, so JSON parity requires the same filing. Moving
// it would make `-j rule show` and `rule show` disagree about a number they
// both printed.
//
// Absent on purpose:
//
//	address, broadcast, permaddr, lladdr   MAC-shaped values reach FacetMACs
//	                                       through the blanket string scan, as
//	                                       reMAC reaches them in text
//	dst (route)                            already a joined CIDR; the blanket
//	                                       scan finds it
//	label                                  the text form prints it as a bare
//	                                       trailing token on the inet line,
//	                                       which the text extractor also
//	                                       ignores
//	family, link, link_type, host          no text token at all, or a token
//	                                       (`link/ether`) that is positional
//	                                       and deliberately uncompared
//	action, iif, oif, fwmark, fwmask,      rule selectors; none is a compared
//	goto, tun_id, suppress_*, flow_*,      keyword in the text form either, so
//	uid_*, sport_*, dport_*                an entry would create an element on
//	                                       one side of no row
var jsonFacetKeys = map[string]StdoutFacet{
	"ifname":   FacetIfNames,
	"ifindex":  FacetIfIndexes,
	"priority": FacetIfIndexes,
	"dev":      FacetDevNames,
}

// jsonKeywordKeys maps a JSON key to the keywords table's spelling.
//
// Ten of the 23 keywords are spelled differently in JSON, and the spellings
// are iproute2's own choice rather than a transliteration — which is exactly
// why they are worth comparing live: a version bump can move one, and a
// committed sidecar cannot notice.
//
// The identity entries are listed too, rather than being left implicit. A
// `jsonKeywordKeys[k]` lookup that falls back to `k` would silently promote
// any future JSON key whose name happened to match a keyword, and the dead
// locus rule this package follows wants the set of compared keys written
// down. TestStdoutJSONKeywordsAreReachable checks this table against
// keywords() in both directions.
var jsonKeywordKeys = map[string]string{
	// Identical spellings. Both sides are the SAME constant from stdout.go, so
	// the identity is a property of the code rather than of two literals that
	// agree today; see that file's keyword const block.
	kwMtuCst:      kwMtuCst,
	kwQdiscCst:    kwQdiscCst,
	kwGroupCst:    kwGroupCst,
	kwMasterCst:   kwMasterCst,
	kwScopeCst:    kwScopeCst,
	kwTableCst:    kwTableCst,
	kwViaCst:      kwViaCst,
	kwMetricCst:   kwMetricCst,
	kwWeightCst:   kwWeightCst,
	kwAdvmssCst:   kwAdvmssCst,
	kwPrefCst:     kwPrefCst,
	kwPermaddrCst: kwPermaddrCst,
	kwLladdrCst:   kwLladdrCst,
	kwPeerCst:     kwPeerCst,

	// Renames. Each is print_linkinfo's or print_addrinfo's JSON name against
	// its text name. Only the JSON key is a literal here, the keyword being
	// the named constant, so a rename cannot silently point at a keyword that
	// does not exist.
	"operstate":           kwStateCst,        // `state UNKNOWN`
	"linkmode":            kwModeCst,         // `mode DEFAULT`
	"txqlen":              kwQlenCst,         // `qlen 1000`
	"broadcast":           kwBrdCst,          // `brd ff:ff:ff:ff:ff:ff`
	"protocol":            kwProtoCst,        // `proto kernel_lo`
	"valid_life_time":     kwValidLftCst,     // `valid_lft forever`
	"preferred_life_time": kwPreferredLftCst, // `preferred_lft forever`
	"prefsrc":             kwSrcCst,          // print_route's `src`
	"gateway":             kwViaCst,          // print_route's `via`
	"link_netnsid":        kwLinkNetnsidCst,  // `link-netnsid 0`

	// `state` is BOTH a JSON key and a keyword, and not for the same object.
	// print_neigh emits `"state": ["STALE"]` where the text form prints the
	// bare word STALE with no keyword in front of it, so this entry populates
	// the keyword facet on the JSON side of a neigh row and the text side is
	// empty. Declared, and asserted in both directions by a test row, rather
	// than discovered as a one-sided finding on a live run.
	kwStateCst: kwStateCst,
}

// jsonCIDRPairs are the (address, prefix length) key pairs iproute2 splits in
// JSON and joins in the text form.
//
// This is the one mapping a pattern could not have done, and the one whose
// failure is silent: unjoined, FacetCIDRs is empty on both sides of a `-j addr
// show` row and empty matches empty. The text form prints
// `inet 127.0.0.1/8`; the JSON form prints `"local": "127.0.0.1"` and
// `"prefixlen": 8` as separate members of one object.
//
// Route's `dst` is deliberately not here: print_route writes it already
// joined, so the blanket string scan finds it and a pair entry would need a
// `dstlen` that route never emits. Rule's `dst` IS here, because print_rule
// splits it — the same key name, two shapes, in two objects.
// `address`/`prefixlen` is print_addrlabel's split (ip/ipaddrlabel.c:44-97): the
// text form glues `prefix ADDR/LEN` while the JSON form writes the two as separate
// members of one entry. The key name `address` is also a link's MAC and a tunnel's
// endpoint, but never in an object that ALSO carries `prefixlen` — those sit on the
// top-level link object while prefixlen sits in nested addr_info — so the pair joins
// only the addrlabel entry it is here for, which the calibration row proves.
var jsonCIDRPairs = []struct{ addr, length string }{
	{addr: "local", length: "prefixlen"},   // print_addrinfo
	{addr: "src", length: "srclen"},        // print_rule's `from`
	{addr: "dst", length: "dstlen"},        // print_rule's `to`
	{addr: "address", length: "prefixlen"}, // print_addrlabel
}

// jsonStatsDirections maps the stats64 sub-object names to the direction
// prefix FacetStatsHeaders uses, which is the text form's heading spelling.
var jsonStatsDirections = map[string]string{
	jsonRxKeyCst: "RX",
	jsonTxKeyCst: "TX",
}

// jsonFacets fills every facet from a decoded `ip -j` listing.
func jsonFacets(entries []any, sets map[StdoutFacet]multiset) {
	for _, e := range entries {
		// FacetLines counts TOP-LEVEL ENTRIES here, not lines.
		//
		// Bare `-j` is one line: iproute2 pretty-prints only under `-p`, and
		// goip's json.NewEncoder never does. So counting lines would compare
		// 1 against 1 — a facet asserting nothing while presenting as the one
		// that catches an empty render, which is the whole reason this file
		// exists. The entry count is the honest analog of the text form's
		// stanza count.
		//
		// Entries are not lines, and for `addr show` they are not even the
		// same number: the text form prints a stanza per link PLUS a line per
		// address, where JSON nests the addresses in addr_info. That is
		// harmless because both sides of any one row are the same format, and
		// it is pinned by a test row so the difference is a decision.
		sets[FacetLines].add("line")
		jsonWalk(e, sets)
	}
}

// jsonWalk records one value's facets and descends into it.
func jsonWalk(v any, sets map[StdoutFacet]multiset) {
	switch t := v.(type) {
	case jsonObject:
		jsonObjectFacets(t, sets)
	case []any:
		for _, e := range t {
			jsonWalk(e, sets)
		}
	case string:
		jsonStringFacets(t, sets)
	}
}

// jsonStringFacets runs the value-shaped patterns over one string, which is
// what keeps the two formats in agreement without a table: these are exactly
// the patterns the text extractor runs over every line.
func jsonStringFacets(s string, sets map[StdoutFacet]multiset) {
	for _, m := range reCIDR4.FindAllString(s, -1) {
		sets[FacetCIDRs].add(m)
	}
	for _, m := range reCIDR6.FindAllString(s, -1) {
		sets[FacetCIDRs].add(strings.ToLower(m))
	}
	for _, m := range reMAC.FindAllString(s, -1) {
		sets[FacetMACs].add(strings.ToLower(m))
	}
	// Route flag tokens arrive as the ELEMENTS of a JSON array —
	// `"flags": [ "linkdown" ]` — not as keys, so the null-valued key rule in
	// jsonMemberFacets does not see them and only a value scan does. The
	// whole point of the mesh/ topology is that every route in it is
	// linkdown, so this is a live comparison and not a precaution.
	for _, m := range reFlag.FindAllString(s, -1) {
		sets[FacetFlags].add(m)
	}
}

// jsonObjectFacets records one object's keys and descends into its values.
func jsonObjectFacets(obj jsonObject, sets map[StdoutFacet]multiset) {
	jsonPairedCIDRs(obj, sets)
	for _, m := range obj {
		// stats64 is TERMINAL: jsonStatsHeaders takes the heading names and
		// the walk does not descend, so no counter value can reach a facet
		// through a key that happens to collide with a keyword.
		if m.Key == jsonStatsKeyCst {
			jsonStatsHeaders(m.Val, sets)
			continue
		}
		if m.Key == jsonNextHopsKeyCst {
			// FacetNextHops counts the text form's bare `nexthop` word, one
			// per continuation line of a multipath route. In JSON that is the
			// array's length.
			if hops, ok := m.Val.([]any); ok {
				for range hops {
					sets[FacetNextHops].add(nextHopTokenCst)
				}
			}
		}
		// `ip nexthop show`'s group is an array of {id[,weight]} objects, where
		// the text form prints the one slash-joined token `group 1/2` or the
		// weighted `group 1,2/2,3` (print_nh_group, ip/ipnexthop.c:255). Join it
		// to that token and do NOT descend: the inner id and weight members are
		// not separate text tokens, and weight IS a keyword, so facing them would
		// fill keyword:weight on the JSON side of a row whose text side is empty.
		// Link's `group default` is a scalar and falls through to the generic
		// path below.
		if m.Key == kwGroupCst {
			if arr, ok := m.Val.([]any); ok {
				sets[FacetKeyword(kwGroupCst)].add(jsonNexthopGroup(arr))
				continue
			}
		}
		jsonMemberFacets(m, sets)
		jsonWalk(m.Val, sets)
	}
}

// jsonMemberFacets files one member under its set facet, its keyword, or as a
// flag token.
func jsonMemberFacets(m jsonMember, sets map[StdoutFacet]multiset) {
	// A PRESENT key whose name is a flag token is the JSON spelling of the
	// text form's bare positional word: print_neigh's `router`,
	// `extern_learn` and `extern_valid`, and `neigh show proxy`'s `proxy`.
	//
	// # The value is null, and that is iproute2's encoding rather than a gap
	//
	// print_null(PRINT_ANY, "router", "%s ", "router") — ip/ipneigh.c:441,
	// with extern_learn at :447, extern_valid at :451 and proxy at :443 —
	// writes the word to the text stream and a JSON NULL to the JSON one.
	// (The run also holds managed at :445 and offload at :449, which are in
	// flagTokens too and which no committed topology produces.) So the flag's
	// presence is the key's existence and its value carries nothing. A rule
	// written against `true` would have matched none of them, and would have
	// matched nothing at all on a live run while the facet reported clean.
	//
	// A true-valued boolean is accepted as well, because print_bool exists
	// alongside print_null and iproute2 uses it for presence too —
	// `link_pointtopoint` at ip/ipaddress.c:1080-1081 is the nearest example.
	// Neither spelling is privileged.
	//
	// # The flagTokens restriction is what keeps the formats in agreement
	//
	// addr emits mngtmpaddr, nodad and noprefixroute this way, and rule emits
	// not, masquerade, nop and l3mdev. None is in flagTokens, and none is
	// matched by reFlag in text either, so all seven contribute nothing on
	// both sides. Dropping the restriction would populate this facet on the
	// JSON side of rows whose text side is empty, and every one of those
	// would present as a goip defect.
	if jsonIsPresenceOnly(m.Val) {
		if jsonIsFlagToken(m.Key) {
			sets[FacetFlags].add(m.Key)
		}
		return
	}
	if f, ok := jsonFacetKeys[m.Key]; ok {
		for _, v := range jsonScalars(m.Val) {
			sets[f].add(v)
		}
	}
	if kw, ok := jsonKeywordKeys[m.Key]; ok {
		for _, v := range jsonScalars(m.Val) {
			sets[FacetKeyword(kw)].add(v)
		}
	}
}

// jsonIsPresenceOnly reports whether a value carries no datum of its own, so
// the member's meaning is that its key exists. iproute2 writes that two ways,
// print_null's JSON null and print_bool's true; a false boolean is a datum
// and is not one of them.
func jsonIsPresenceOnly(v any) bool {
	if v == nil {
		return true
	}
	b, ok := v.(bool)
	return ok && b
}

// jsonIsFlagToken reports whether a key names one of the bare positional flag
// words FacetFlags compares. A linear scan over sixteen strings, against
// stdout.go's flagTokens so the two cannot drift.
func jsonIsFlagToken(key string) bool {
	for _, t := range flagTokens {
		if t == key {
			return true
		}
	}
	return false
}

// jsonScalars renders the value(s) one member contributes to a facet.
//
// An array contributes each element, because a multiset is what the facet is:
// print_neigh's `"state": ["STALE"]` is one element and a two-state entry
// would be two.
//
// An object contributes every scalar member it has, in document order, and
// print_route's `via` is the only mapped key this reaches. iproute2 writes
// `{"family":"inet6","host":"2001:db8::2"}` where the text form prints
// `via inet6 2001:db8::2` — and reKeyword consumes only the token after the
// keyword, so the TEXT side of that facet holds `inet6` and never the
// address. Recording both halves here means the JSON side is a superset
// rather than a different set: it compares the family the text side compares
// AND the gateway the text side structurally cannot. That direction is the
// one worth having, and it is declared by a calibration row rather than
// discovered as a one-sided finding.
func jsonScalars(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case json.Number:
		return []string{t.String()}
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, jsonScalars(e)...)
		}
		return out
	case jsonObject:
		var out []string
		for _, m := range t {
			out = append(out, jsonScalars(m.Val)...)
		}
		return out
	}
	return nil
}

// jsonNexthopGroup renders an `ip nexthop show` group array as the one
// slash-joined token the text form prints: each member its id, plus `,weight`
// only where the weight exceeds one, members joined by `/` (print_nh_group,
// ip/ipnexthop.c:255-277). iproute2 omits the weight key when it is one, so a
// present weight is reproduced and an absent one left off, which is the JSON
// spelling of the text form's own `weight > 1` guard.
func jsonNexthopGroup(arr []any) string {
	parts := make([]string, 0, len(arr))
	for _, e := range arr {
		obj, ok := e.(jsonObject)
		if !ok {
			continue
		}
		id, ok := obj.lookup("id")
		if !ok {
			continue
		}
		ids := jsonScalars(id)
		if len(ids) != 1 {
			continue
		}
		part := ids[0]
		if w, ok := obj.lookup(kwWeightCst); ok {
			if ws := jsonScalars(w); len(ws) == 1 {
				part += "," + ws[0]
			}
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// jsonPairedCIDRs joins the split address/prefix-length key pairs.
func jsonPairedCIDRs(obj jsonObject, sets map[StdoutFacet]multiset) {
	for _, p := range jsonCIDRPairs {
		addr, ok := obj.lookup(p.addr)
		if !ok {
			continue
		}
		length, ok := obj.lookup(p.length)
		if !ok {
			continue
		}
		as, ls := jsonScalars(addr), jsonScalars(length)
		if len(as) != 1 || len(ls) != 1 {
			continue
		}
		sets[FacetCIDRs].add(strings.ToLower(as[0] + "/" + ls[0]))
	}
}

// jsonStatsHeaders renders a stats64 object as FacetStatsHeaders elements.
//
// # The key names, never the values
//
// The text form prints a heading line and a value line, and
// FacetStatsHeaders compares the headings alone because every counter under
// them is live and every column width moves with it. JSON has no heading line
// at all: the headings ARE the keys of the rx and tx objects, and the
// counters are their values. So the port takes the keys and discards the
// values, and getting that backwards would make every `-j -s` row
// permanently noisy — which has a precedent in this table to be read as
// expected rather than as broken. It is not.
//
// # Why the heading NAMES are not translated to the text spellings
//
// They cannot be, and the reason is a real asymmetry in iproute2 rather than
// a naming choice. The text form's fifth RX column is `missed`, from
// s->rx_missed_errors (ip/ipaddress.c:749, :757). The JSON form's fifth rx
// key is `over_errors`, from s->rx_over_errors (:649). Those are DIFFERENT
// kernel counters: text reaches rx_over_errors only under `-s -s` (:779) and
// JSON reaches rx_missed_errors only under show_stats > 1 (:670-671). A
// rename table would therefore have to claim two distinct struct members are
// one heading.
//
// TX is a pure rename — `carrier`/`carrier_errors` is s->tx_carrier_errors
// either way (:799 against :689-690) and `collsns`/`collisions` is
// s->collisions either way (:800 against :691) — but translating two of four
// and inventing the other two is worse than translating none. So the element
// carries iproute2's JSON names, both sides of a `-j` row agree, and the
// cross-format difference is declared and asserted rather than papered over.
//
// Order is preserved, which is why this file decodes into jsonObject: the
// element is a single joined string, so the same headings in a different
// order must be a finding.
func jsonStatsHeaders(v any, sets map[StdoutFacet]multiset) {
	obj, ok := v.(jsonObject)
	if !ok {
		return
	}
	for _, m := range obj {
		dir, ok := jsonStatsDirections[m.Key]
		if !ok {
			continue
		}
		sub, ok := m.Val.(jsonObject)
		if !ok {
			continue
		}
		names := make([]string, 0, len(sub))
		for _, c := range sub {
			names = append(names, c.Key)
		}
		sets[FacetStatsHeaders].add(dir + ":" + strings.Join(names, ","))
	}
}
