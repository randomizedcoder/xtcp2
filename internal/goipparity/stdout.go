package goipparity

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// Structural stdout comparison — the plan's Risk 1.
//
// # Why stdout is compared at all, when the decision said "informational"
//
// Request parity is reply-independent. A goip that sends byte-identical
// requests and then discards every reply is a perfect green on every netlink
// tier there is, because nothing in pkg/nlparity looks at what either tool
// printed. That is the largest hole in the harness and it costs about a
// hundred lines to close, so the plan recommends overriding the decision and
// this is that override.
//
// # Why it is coarse
//
// Byte-for-byte stdout parity is explicitly out of scope, and for good
// reason: `ip`'s column widths are data-dependent, its flag ordering is a
// hand-written table, and its lifetime counters change between two runs a
// second apart. A gate on any of that would fail for reasons unrelated to
// goip. So the comparison is over a handful of structural facets that are
// stable across runs and still cannot be satisfied by an empty output.
//
// # Why the loci are a closed, enumerable set
//
// A locus is a persisted allowlist key, and an entry whose locus nothing
// derives matches nothing forever while presenting as a decision somebody
// made. pkg/nlparity learned that the hard way with the RTM_GETLINK request
// role. Here every locus this file can emit is one of Facets() or
// FacetKeyword(kw) for kw in Keywords() — a finite list, which is what lets
// TestStdoutLociAreEnumerable check the committed allowlist against it
// statically, before any capture exists.
//
// # Why suppressibility is split the way it is
//
// Set membership and line count are DivergencePresence, which
// DivergenceClass.Suppressible refuses. That is the whole point: "goip
// rendered three of the four links" and "goip rendered nothing" must not be
// allowlistable, or the hole this file exists to close reopens as an entry
// with a plausible-sounding reason. Keyword *values* are DivergenceValue and
// therefore suppressible, which is what the two committed `stdout:` entries
// need — iproute2 7.1.0 suppresses `qlen 0` where the tip prints it, and that
// is a version skew in the reference tool, not a goip defect.
//
// # Where this narrows the plan's suppression rule, and why
//
// The plan says suppression applies to value divergences only, "never to
// presence/absence, [...] or a goip that omits an attribute entirely would be
// masked by that attribute's value being noisy". A keyword that `ip` prints
// and goip does not is an absence, and it is nonetheless DivergenceValue
// here. That is deliberate, and the argument is that the rule was written
// about netlink attributes, where an absence means goip failed to decode
// something the kernel sent.
//
// A rendered keyword is not that. `ip -6 addr show` prints `qlen 1000` for lo
// from a SIOCGIFTXQLEN ioctl, because the link dump inet6_dump_ifinfo answers
// carries no IFLA_TXQLEN at all — so there is nothing in the netlink reply for
// goip to have missed, and "absent" is a rendering decision rather than a
// decode failure. Treating it as unsuppressible would make `-6 addr show`
// permanently ungateable over a difference that is not about goip.
//
// What keeps the plan's protection intact is that the facets which would
// catch a genuinely broken renderer — ifnames, ifindexes, cidrs, macs and the
// line count — are all DivergencePresence and cannot be allowlisted at all.
// A goip that rendered nothing, or three links of four, fails on those no
// matter what any entry says about a keyword.

// StdoutFacet is one structural property of a command's rendered output.
type StdoutFacet string

// The set facets. Each is compared as a multiset, so a duplicated line is a
// finding rather than a coincidence.
const (
	// FacetLines is the line count. Coarse, and the first thing to move when
	// a renderer breaks.
	FacetLines StdoutFacet = "lines"
	// FacetIfNames is the set of interface names, taken from the stanza
	// headers. The peer suffix is stripped: `58: ve-nfb-vpn@if2:` contributes
	// `ve-nfb-vpn`, because iproute2 puts the bare name in JSON and only
	// concatenates the suffix for the text form, so keeping the suffix would
	// make this facet disagree with itself across -json.
	FacetIfNames StdoutFacet = "ifnames"
	// FacetIfIndexes is the set of `N:` stanza prefixes.
	FacetIfIndexes StdoutFacet = "ifindexes"
	// FacetCIDRs is the set of address/prefixlen tokens, v4 and v6.
	FacetCIDRs StdoutFacet = "cidrs"
	// FacetMACs is the set of six-octet hardware addresses.
	FacetMACs StdoutFacet = "macs"
)

// Facets returns the set facets in report order.
func Facets() []StdoutFacet {
	return []StdoutFacet{FacetLines, FacetIfNames, FacetIfIndexes, FacetCIDRs, FacetMACs}
}

// FacetKeyword is the facet for one `<keyword> <value>` pair family.
func FacetKeyword(kw string) StdoutFacet { return StdoutFacet("keyword:" + kw) }

// Locus renders the allowlist key's second half.
func (f StdoutFacet) Locus() string { return "stdout:" + string(f) }

// keywords are the iproute2 output keywords whose values are compared.
//
// A closed list rather than "every second token", because `ip`'s output is
// only mostly key/value: `<BROADCAST,MULTICAST,UP>` and `link/ether` are
// positional, and a positional reading of them produces loci that move
// whenever a flag is added. Naming the keywords keeps the locus set finite.
//
// valid_lft and preferred_lft are deliberately included even though they
// change between any two runs. They are not special-cased out, because
// SubtractStdout removes them for free: a value that moved between the two
// reference captures is in the control and is subtracted from the test. A
// hand-written exclusion would also hide a goip that printed the lifetime of
// the wrong address.
var keywords = []string{
	"mtu", "qdisc", "state", "mode", "group", "qlen", "master",
	"scope", "brd", "link-netnsid", "proto", "valid_lft", "preferred_lft",
}

// Keywords returns the compared keywords, sorted, so a report's line order is
// stable enough to diff.
func Keywords() []string {
	out := make([]string, len(keywords))
	copy(out, keywords)
	sort.Strings(out)
	return out
}

// StdoutLoci is every locus this file can emit. The allowlist is checked
// against it; see the header.
func StdoutLoci() []string {
	out := make([]string, 0, len(Facets())+len(keywords))
	for _, f := range Facets() {
		out = append(out, f.Locus())
	}
	for _, kw := range Keywords() {
		out = append(out, FacetKeyword(kw).Locus())
	}
	sort.Strings(out)
	return out
}

// StdoutDivergence is one structural stdout finding.
//
// Kept out of nlparity.Divergence rather than folded into it: pkg/nlparity is
// netlink-only by construction, and its Level enum has no honest value for a
// finding about rendered text. Class is borrowed, though, so that the same
// Suppressible rule governs both halves of the harness — there is exactly one
// place that decides what may be allowlisted.
type StdoutDivergence struct {
	// Command is the argv both tools were driven with, and the allowlist key's
	// first half.
	Command string
	// Facet is what differed.
	Facet StdoutFacet
	// Class decides suppressibility. Presence for structure, Value for a
	// keyword's payload.
	Class nlparity.DivergenceClass
	// Ref and Sub are the differing elements on each side, rendered. Empty
	// means "nothing on this side", which is the finding worth reading.
	Ref, Sub string
}

// Locus is the allowlist key's second half.
func (d StdoutDivergence) Locus() string { return d.Facet.Locus() }

// Key is the finding's identity for allowlist lookup and control
// subtraction. It excludes Ref and Sub necessarily: a control diff's values
// differ from a test diff's by definition, so a key including them would
// never match and the subtraction would be a no-op that looked like one
// working.
func (d StdoutDivergence) Key() string {
	return fmt.Sprintf("%s|%s", d.Class, d.Locus())
}

// String renders a report line.
func (d StdoutDivergence) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "stdout %s %s", d.Class, d.Locus())
	switch {
	case d.Ref != "" && d.Sub != "":
		fmt.Fprintf(&b, ": ip=%s goip=%s", d.Ref, d.Sub)
	case d.Ref != "":
		fmt.Fprintf(&b, ": ip=%s goip=<absent>", d.Ref)
	case d.Sub != "":
		fmt.Fprintf(&b, ": ip=<absent> goip=%s", d.Sub)
	}
	return b.String()
}

// The extraction patterns. Anchored where `ip`'s format lets them be, because
// an unanchored ifname pattern matches half the line.
var (
	// A stanza header opens at column zero with `N: name` or `N: name@peer`.
	// Continuation lines are indented, which is how they are told apart.
	reStanza = regexp.MustCompile(`^(\d+):\s+([^:@\s]+)`)
	// A v4 CIDR. The octet bound is not validated: this is a set comparison,
	// and `999.0.0.1/8` appearing on exactly one side is still the finding.
	reCIDR4 = regexp.MustCompile(`\b(\d{1,3}(?:\.\d{1,3}){3}/\d{1,2})\b`)
	// A v6 CIDR, requiring the double colon or two single ones so a MAC
	// cannot match it.
	reCIDR6 = regexp.MustCompile(`\b([0-9a-fA-F:]*:[0-9a-fA-F:]*:[0-9a-fA-F]*/\d{1,3})\b`)
	// Exactly six two-digit octets, which a compressed IPv6 address cannot
	// be: `ip` prints v6 with up to four digits per group.
	reMAC = regexp.MustCompile(`\b([0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5})\b`)
)

// reKeyword matches `<keyword> <value>` for the compared keywords, built once
// from the table so the two cannot drift.
var reKeyword = regexp.MustCompile(
	`\b(` + strings.Join(keywords, "|") + `)\s+(\S+)`)

// multiset counts occurrences, because a duplicated element is a finding.
type multiset map[string]int

// add records one occurrence.
func (m multiset) add(s string) { m[s]++ }

// diff renders the symmetric difference of two multisets as two sorted,
// comma-joined strings, or two empty strings when they are equal.
//
// Sorted so the report is diffable, and rendered with counts only where a
// count is above one, so the common case reads as a plain list.
func (m multiset) diff(other multiset) (refOnly, subOnly string) {
	var refs, subs []string
	seen := make(map[string]bool, len(m)+len(other))
	for k := range m {
		seen[k] = true
	}
	for k := range other {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a, b := m[k], other[k]
		if a == b {
			continue
		}
		if a > b {
			refs = append(refs, render(k, a-b))
		} else {
			subs = append(subs, render(k, b-a))
		}
	}
	return strings.Join(refs, ","), strings.Join(subs, ",")
}

// render writes an element, with its surplus count when that is above one.
func render(k string, n int) string {
	if n == 1 {
		return k
	}
	return fmt.Sprintf("%s x%d", k, n)
}

// stdoutFacets extracts every facet of one side's output in a single pass.
func stdoutFacets(s string) (sets map[StdoutFacet]multiset) {
	sets = map[StdoutFacet]multiset{}
	for _, f := range Facets() {
		sets[f] = multiset{}
	}
	for _, kw := range keywords {
		sets[FacetKeyword(kw)] = multiset{}
	}

	for _, line := range splitLines(s) {
		// The line count is a multiset over the *line numbers*, not over the
		// lines: comparing the lines themselves would be byte-for-byte
		// parity by the back door, which is out of scope. A count is all
		// this facet claims to be.
		sets[FacetLines].add("line")

		if m := reStanza.FindStringSubmatch(line); m != nil {
			sets[FacetIfIndexes].add(m[1])
			sets[FacetIfNames].add(m[2])
		}
		for _, m := range reCIDR4.FindAllString(line, -1) {
			sets[FacetCIDRs].add(m)
		}
		for _, m := range reCIDR6.FindAllString(line, -1) {
			sets[FacetCIDRs].add(strings.ToLower(m))
		}
		for _, m := range reMAC.FindAllString(line, -1) {
			sets[FacetMACs].add(strings.ToLower(m))
		}
		for _, m := range reKeyword.FindAllStringSubmatch(line, -1) {
			sets[FacetKeyword(m[1])].add(m[2])
		}
	}
	return sets
}

// splitLines splits on newlines, treating a trailing newline as a terminator
// rather than as an extra empty line. The empty string is zero lines, not
// one — which matters, because "goip printed nothing" is the finding this
// facet exists to catch and an off-by-one there would report 1 against 1.
func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// CompareStdout compares two renderings of one command structurally.
//
// One finding per facet, naming the symmetric difference — not one per
// differing element. A goip that rendered no links at all should produce a
// handful of readable findings rather than one per interface, and the locus
// set has to stay finite for the allowlist to be checkable.
func CompareStdout(command, ref, sub string) []StdoutDivergence {
	refSets, subSets := stdoutFacets(ref), stdoutFacets(sub)

	var out []StdoutDivergence
	emit := func(f StdoutFacet, class nlparity.DivergenceClass) {
		r, s := refSets[f].diff(subSets[f])
		if r == "" && s == "" {
			return
		}
		out = append(out, StdoutDivergence{
			Command: command, Facet: f, Class: class, Ref: r, Sub: s,
		})
	}

	// Structure is unsuppressible; see the header.
	for _, f := range Facets() {
		emit(f, nlparity.DivergencePresence)
	}
	// Keyword values are, which is what the committed qlen entries need.
	for _, kw := range Keywords() {
		emit(FacetKeyword(kw), nlparity.DivergenceValue)
	}
	return out
}

// SubtractStdout removes from test every finding whose Key also appears in
// control, once per occurrence.
//
// The same D_control trick as the netlink side, for the same reason: a
// difference that shows up between the two *reference* captures is by
// construction not attributable to goip. It is what makes valid_lft and
// preferred_lft comparable at all without a hand-written volatile list.
func SubtractStdout(test, control []StdoutDivergence) []StdoutDivergence {
	if len(control) == 0 {
		return test
	}
	budget := make(map[string]int, len(control))
	for _, d := range control {
		budget[d.Key()]++
	}
	out := make([]StdoutDivergence, 0, len(test))
	for _, d := range test {
		if budget[d.Key()] > 0 {
			budget[d.Key()]--
			continue
		}
		out = append(out, d)
	}
	return out
}

// StdoutReport is the stdout half of one command's result.
type StdoutReport struct {
	// Command is the argv both tools were driven with.
	Command string
	// Findings survived control subtraction and allowlisting.
	Findings []StdoutDivergence
	// ControlSuppressed moved between the two reference captures.
	ControlSuppressed []StdoutDivergence
	// AllowSuppressed were matched by an allowlist entry. Value class only,
	// because Suppresses refuses the rest.
	AllowSuppressed []StdoutDivergence
	// ControlSize is |D_control| for stdout, the noise sentinel.
	ControlSize int
	// Compared is false when a side's output was not recorded, in which case
	// nothing above was measured and the caller must not read a clean report
	// as a passing one.
	Compared bool
}

// Failed reports whether the stdout comparison should fail a build.
func (r StdoutReport) Failed() bool { return len(r.Findings) != 0 }

// CompareStdoutWithControl is the whole stdout comparison: diff, control
// subtraction, allowlisting. al may be nil, which suppresses nothing.
func CompareStdoutWithControl(
	command, ipA, goip, ipB string, al *nlparity.Allowlist,
) StdoutReport {
	control := CompareStdout(command, ipA, ipB)
	all := CompareStdout(command, ipA, goip)

	r := StdoutReport{Command: command, ControlSize: len(control), Compared: true}

	kept := SubtractStdout(all, control)
	if len(kept) != len(all) {
		keptKeys := make(map[string]int, len(kept))
		for _, d := range kept {
			keptKeys[d.Key()]++
		}
		for _, d := range all {
			if keptKeys[d.Key()] > 0 {
				keptKeys[d.Key()]--
				continue
			}
			r.ControlSuppressed = append(r.ControlSuppressed, d)
		}
	}

	for _, d := range kept {
		if al != nil && al.Suppresses(command, d.Locus(), d.Class) {
			r.AllowSuppressed = append(r.AllowSuppressed, d)
			continue
		}
		r.Findings = append(r.Findings, d)
	}
	return r
}
