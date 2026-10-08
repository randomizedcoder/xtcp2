package goipparity

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// Two real stanzas, trimmed to what the facets read. Taken from the shape
// `ip link show` prints rather than invented, because a synthetic format that
// the regexes happen to match would prove only that they match themselves.
const (
	refLinkShow = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
2: goip0: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/ether 02:00:00:00:00:01 brd ff:ff:ff:ff:ff:ff
`
	refAddrShow = `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
2: goip0: <BROADCAST,NOARP,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN group default qlen 1000
    inet 192.0.2.1/24 scope global goip0
       valid_lft forever preferred_lft forever
    inet6 2001:db8::1/64 scope global
       valid_lft 86400sec preferred_lft 14400sec
`
)

// go test ./internal/goipparity/ -run TestCompareStdout
func TestCompareStdout(t *testing.T) {
	tests := []struct {
		description string
		ref, sub    string
		// wantLoci are the loci expected to be reported, in any order. An
		// exact set, not a subset: a facet firing that should not have is as
		// much a defect as one staying quiet.
		wantLoci []string
		// wantClass is asserted on every reported finding, which is how the
		// suppressibility split is pinned rather than assumed.
		wantClass map[string]nlparity.DivergenceClass
	}{
		{
			description: "positive: identical output produces no findings at all",
			ref:         refLinkShow,
			sub:         refLinkShow,
			wantLoci:    nil,
		},
		{
			description: "positive: a changed mtu is reported as a suppressible keyword value and nothing else",
			ref:         refLinkShow,
			sub:         strings.Replace(refLinkShow, "mtu 1500", "mtu 9000", 1),
			wantLoci:    []string{"stdout:keyword:mtu"},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:keyword:mtu": nlparity.DivergenceValue,
			},
		},
		{
			description: "positive: an absent qlen is reported at the same locus the committed allowlist entries use",
			ref:         refAddrShow,
			sub:         strings.ReplaceAll(refAddrShow, " qlen 1000", ""),
			wantLoci:    []string{"stdout:keyword:qlen"},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:keyword:qlen": nlparity.DivergenceValue,
			},
		},
		{
			// The whole reason this file exists. A goip that sends perfect
			// requests and prints nothing is green on every netlink tier.
			description: "negative: an empty subject fires every structural facet, none of them suppressible",
			ref:         refAddrShow,
			sub:         "",
			wantLoci: []string{
				"stdout:lines", "stdout:ifnames", "stdout:ifindexes", "stdout:cidrs",
				"stdout:keyword:mtu", "stdout:keyword:qdisc", "stdout:keyword:state",
				"stdout:keyword:group", "stdout:keyword:qlen", "stdout:keyword:scope",
				"stdout:keyword:valid_lft", "stdout:keyword:preferred_lft",
			},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:lines":        nlparity.DivergencePresence,
				"stdout:ifnames":      nlparity.DivergencePresence,
				"stdout:ifindexes":    nlparity.DivergencePresence,
				"stdout:cidrs":        nlparity.DivergencePresence,
				"stdout:keyword:qlen": nlparity.DivergenceValue,
			},
		},
		{
			description: "negative: a dropped link fires ifnames, ifindexes and lines, and all three are unsuppressible",
			ref:         refLinkShow,
			sub: `1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
`,
			wantLoci: []string{
				"stdout:lines", "stdout:ifnames", "stdout:ifindexes", "stdout:macs",
				"stdout:keyword:mtu", "stdout:keyword:qdisc", "stdout:keyword:state",
				"stdout:keyword:mode", "stdout:keyword:group", "stdout:keyword:qlen",
				"stdout:keyword:brd",
			},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:ifnames":   nlparity.DivergencePresence,
				"stdout:ifindexes": nlparity.DivergencePresence,
				"stdout:macs":      nlparity.DivergencePresence,
			},
		},
		{
			description: "negative: a wrong CIDR is a presence finding on both sides, not a value one, so it can never be allowlisted",
			ref:         refAddrShow,
			sub:         strings.Replace(refAddrShow, "192.0.2.1/24", "192.0.2.9/24", 1),
			wantLoci:    []string{"stdout:cidrs"},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:cidrs": nlparity.DivergencePresence,
			},
		},
		{
			description: "boundary: both sides empty is clean, because zero equals zero",
			ref:         "",
			sub:         "",
			wantLoci:    nil,
		},
		{
			description: "boundary: a trailing newline is a terminator, not an extra line",
			ref:         "1: lo: <LOOPBACK> mtu 65536\n",
			sub:         "1: lo: <LOOPBACK> mtu 65536",
			wantLoci:    nil,
		},
		{
			description: "boundary: a duplicated stanza is caught, because the facets are multisets and not sets",
			ref:         refLinkShow,
			sub:         refLinkShow + refLinkShow,
			wantLoci: []string{
				"stdout:lines", "stdout:ifnames", "stdout:ifindexes", "stdout:macs",
				"stdout:keyword:mtu", "stdout:keyword:qdisc", "stdout:keyword:state",
				"stdout:keyword:mode", "stdout:keyword:group", "stdout:keyword:qlen",
				"stdout:keyword:brd",
			},
		},
		{
			description: "boundary: a peer suffix is stripped, so a veth's name matches the name iproute2 puts in JSON",
			ref:         "58: ve-nfb-vpn@if2: <BROADCAST,UP> mtu 1500\n",
			sub:         "58: ve-nfb-vpn: <BROADCAST,UP> mtu 1500\n",
			wantLoci:    nil,
		},
		{
			description: "corner: a MAC is matched case-insensitively, since a renderer's case is not a divergence",
			ref:         "    link/ether 02:00:00:00:00:01 brd ff:ff:ff:ff:ff:ff\n",
			sub:         "    link/ether 02:00:00:00:00:01 brd FF:FF:FF:FF:FF:FF\n",
			// brd is also a compared keyword, and there the value is taken
			// verbatim — so the MAC facet is clean and the keyword facet is
			// not. That asymmetry is deliberate and this row records it: the
			// facet answers "is this address present anywhere", the keyword
			// answers "is this field rendered the same way".
			wantLoci: []string{"stdout:keyword:brd"},
		},
		{
			description: "corner: an IPv6 address is not mistaken for a MAC, which would make macs fire on an addr change",
			ref:         "    inet6 2001:db8::1/64 scope global\n",
			sub:         "    inet6 2001:db8::2/64 scope global\n",
			wantLoci:    []string{"stdout:cidrs"},
		},
		{
			description: "corner: a flag list changing does not fire any facet, because flags are positional and have no locus",
			ref:         "1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536\n",
			sub:         "1: lo: <UP,LOOPBACK,LOWER_UP> mtu 65536\n",
			// Not a gap being excused: a flag-order locus would move every
			// time iproute2 added a flag, and the netlink side already
			// asserts IFLA_* presence and value exactly. What this row pins
			// is that the stdout comparator stays quiet rather than emitting
			// something unstable.
			wantLoci: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := CompareStdout("link show", tt.ref, tt.sub)

			gotLoci := map[string]nlparity.DivergenceClass{}
			for _, d := range got {
				if _, dup := gotLoci[d.Locus()]; dup {
					t.Errorf("locus %q reported twice; one finding per facet is the "+
						"invariant that keeps the locus set finite", d.Locus())
				}
				gotLoci[d.Locus()] = d.Class
			}

			want := map[string]bool{}
			for _, l := range tt.wantLoci {
				want[l] = true
			}
			for l := range gotLoci {
				if !want[l] {
					t.Errorf("unexpected finding at %q", l)
				}
			}
			for l := range want {
				if _, ok := gotLoci[l]; !ok {
					t.Errorf("missing finding at %q", l)
				}
			}
			for l, wantClass := range tt.wantClass {
				if gotClass, ok := gotLoci[l]; ok && gotClass != wantClass {
					t.Errorf("finding at %q has class %s, want %s",
						l, gotClass, wantClass)
				}
			}
		})
	}
}

// TestStdoutLociAreEnumerable is the stdout half of the derived-locus rule.
//
// A locus is a persisted allowlist key. An entry whose locus nothing derives
// matches nothing forever while presenting as a decision somebody made — and
// this test found exactly that on the two committed `stdout:` entries, whose
// loci were `stdout:link:qlen:IFLA_TXQLEN=0` and
// `stdout:addr6:qlen:IFLA_TXQLEN-absent`. Both described the cause rather
// than naming the quantity, so neither could ever have matched. They are
// `stdout:keyword:qlen` now, distinguished by command.
//
// Unlike the netlink side, this can be checked with no capture at all,
// because the stdout locus set is closed: the facets plus one per compared
// keyword.
//
// go test ./internal/goipparity/ -run TestStdoutLociAreEnumerable
func TestStdoutLociAreEnumerable(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	derivable := map[string]bool{}
	for _, l := range StdoutLoci() {
		derivable[l] = true
	}

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: every committed stdout locus is one StdoutLoci can emit",
			check: func(t *testing.T) {
				for _, e := range al.Entries {
					if !strings.HasPrefix(e.Locus, "stdout:") {
						continue
					}
					if !derivable[e.Locus] {
						t.Errorf("the allowlist entry for %q at %q is at a locus no "+
							"comparator emits, so it can never match; StdoutLoci() is %v",
							e.Command, e.Locus, StdoutLoci())
					}
				}
			},
		},
		{
			description: "positive: at least one committed entry is a stdout entry, so this test is not vacuous",
			check: func(t *testing.T) {
				n := 0
				for _, e := range al.Entries {
					if strings.HasPrefix(e.Locus, "stdout:") {
						n++
					}
				}
				if n == 0 {
					t.Error("no committed stdout entries; this guard is checking nothing")
				}
			},
		},
		{
			description: "positive: every locus StdoutLoci reports is actually reachable by CompareStdout",
			check: func(t *testing.T) {
				// The other direction. A locus in the set that nothing can
				// produce would let a reviewer repoint an entry onto a dead
				// spelling and still pass the row above.
				reached := map[string]bool{}
				for _, f := range Facets() {
					reached[f.Locus()] = true
				}
				for _, kw := range Keywords() {
					// One side carries the keyword, the other does not, which
					// is the minimum that makes the facet differ.
					got := CompareStdout("probe", "x: y: "+kw+" 1\n", "x: y:\n")
					for _, d := range got {
						reached[d.Locus()] = true
					}
				}
				for _, l := range StdoutLoci() {
					if !reached[l] {
						t.Errorf("StdoutLoci reports %q, which CompareStdout never emits", l)
					}
				}
			},
		},
		{
			description: "negative: a locus that merely looks derived is rejected",
			check: func(t *testing.T) {
				for _, bad := range []string{
					"stdout:keyword:qlen:IFLA_TXQLEN=0", // the old spelling, with a suffix
					"stdout:link:qlen",                  // the old spelling's command half
					"stdout:keyword:txqlen",             // not a compared keyword
					"stdout:qlen",                       // missing the facet family
				} {
					if derivable[bad] {
						t.Errorf("%q is treated as derivable", bad)
					}
				}
			},
		},
		{
			description: "boundary: the locus set has no duplicates, so an entry cannot be ambiguous about which facet it means",
			check: func(t *testing.T) {
				seen := map[string]bool{}
				for _, l := range StdoutLoci() {
					if seen[l] {
						t.Errorf("locus %q appears twice in StdoutLoci", l)
					}
					seen[l] = true
				}
			},
		},
		{
			description: "corner: no facet name collides with a keyword name, which would make two facets share a locus",
			check: func(t *testing.T) {
				// `stdout:lines` and a keyword called `lines` would both be
				// spelled `stdout:lines` only if FacetKeyword dropped its
				// prefix — it does not, and this row is what keeps it so.
				for _, f := range Facets() {
					for _, kw := range Keywords() {
						if f.Locus() == FacetKeyword(kw).Locus() {
							t.Errorf("facet %q and keyword %q share locus %q",
								f, kw, f.Locus())
						}
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// TestSubtractStdout pins the control subtraction, which is what makes
// valid_lft and preferred_lft comparable without a hand-written volatile list.
//
// go test ./internal/goipparity/ -run TestSubtractStdout
func TestSubtractStdout(t *testing.T) {
	d := func(f StdoutFacet, class nlparity.DivergenceClass, ref, sub string) StdoutDivergence {
		return StdoutDivergence{Command: "link show", Facet: f, Class: class, Ref: ref, Sub: sub}
	}
	valA := d(FacetKeyword("valid_lft"), nlparity.DivergenceValue, "100sec", "99sec")
	valB := d(FacetKeyword("valid_lft"), nlparity.DivergenceValue, "5sec", "4sec")
	mtu := d(FacetKeyword("mtu"), nlparity.DivergenceValue, "1500", "9000")
	names := d(FacetIfNames, nlparity.DivergencePresence, "goip0", "")

	tests := []struct {
		description string
		test        []StdoutDivergence
		control     []StdoutDivergence
		wantLoci    []string
	}{
		{
			description: "positive: a finding whose key is in the control is removed, values differing or not",
			test:        []StdoutDivergence{valA},
			control:     []StdoutDivergence{valB},
			wantLoci:    nil,
		},
		{
			description: "positive: a finding whose key is not in the control survives",
			test:        []StdoutDivergence{mtu},
			control:     []StdoutDivergence{valB},
			wantLoci:    []string{"stdout:keyword:mtu"},
		},
		{
			description: "negative: an empty control removes nothing",
			test:        []StdoutDivergence{valA, mtu},
			control:     nil,
			wantLoci:    []string{"stdout:keyword:valid_lft", "stdout:keyword:mtu"},
		},
		{
			description: "negative: a presence finding is subtracted too, because the control is about the capture and not about suppressibility",
			// Control subtraction and allowlisting are different mechanisms
			// and only the second is class-restricted. A presence difference
			// between the two REFERENCE captures means the topology moved
			// mid-run, which is not goip's doing either — and it shows up as
			// ControlSize, which is the sentinel for distrusting the run.
			test:     []StdoutDivergence{names},
			control:  []StdoutDivergence{names},
			wantLoci: nil,
		},
		{
			description: "boundary: the subtraction is per-occurrence, so two findings against one control entry leave one",
			test:        []StdoutDivergence{valA, valA},
			control:     []StdoutDivergence{valB},
			wantLoci:    []string{"stdout:keyword:valid_lft"},
		},
		{
			description: "boundary: an empty test with a non-empty control is still empty",
			test:        nil,
			control:     []StdoutDivergence{valB},
			wantLoci:    nil,
		},
		{
			description: "corner: a control entry of a different class does not subtract, since the class is in the key",
			test:        []StdoutDivergence{d(FacetIfNames, nlparity.DivergencePresence, "a", "")},
			control:     []StdoutDivergence{d(FacetIfNames, nlparity.DivergenceValue, "a", "")},
			wantLoci:    []string{"stdout:ifnames"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := SubtractStdout(tt.test, tt.control)
			if len(got) != len(tt.wantLoci) {
				t.Fatalf("got %d findings %v, want %d %v",
					len(got), got, len(tt.wantLoci), tt.wantLoci)
			}
			for i := range got {
				if got[i].Locus() != tt.wantLoci[i] {
					t.Errorf("finding %d is at %q, want %q", i, got[i].Locus(), tt.wantLoci[i])
				}
			}
		})
	}
}

// TestCompareStdoutWithControl is the assembled stdout comparison: diff,
// control subtraction, allowlisting, in that order.
//
// go test ./internal/goipparity/ -run TestCompareStdoutWithControl
func TestCompareStdoutWithControl(t *testing.T) {
	al, err := nlparity.EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}
	noQlen := strings.ReplaceAll(refAddrShow, " qlen 1000", "")

	tests := []struct {
		description         string
		command             string
		ipA, goip, ipB      string
		al                  *nlparity.Allowlist
		wantFindings        []string
		wantAllowSuppressed []string
		wantControlSize     int
		wantFailed          bool
	}{
		{
			description: "positive: three identical renderings are clean",
			command:     "link show", ipA: refLinkShow, goip: refLinkShow, ipB: refLinkShow,
			al:         al,
			wantFailed: false,
		},
		{
			description: "positive: the committed -6 addr show entry suppresses the qlen absence it exists for",
			command:     "-6 addr show", ipA: refAddrShow, goip: noQlen, ipB: refAddrShow,
			al:                  al,
			wantAllowSuppressed: []string{"stdout:keyword:qlen"},
			wantFailed:          false,
		},
		{
			description: "negative: the same qlen absence on an unlisted command is NOT suppressed",
			// The allowlist is keyed on command and locus together, and this
			// is the half of that which makes the two committed entries
			// narrow rather than global.
			command: "addr show", ipA: refAddrShow, goip: noQlen, ipB: refAddrShow,
			al:           al,
			wantFindings: []string{"stdout:keyword:qlen"},
			wantFailed:   true,
		},
		{
			description: "negative: a nil allowlist suppresses nothing, so the same case fails",
			command:     "-6 addr show", ipA: refAddrShow, goip: noQlen, ipB: refAddrShow,
			al:           nil,
			wantFindings: []string{"stdout:keyword:qlen"},
			wantFailed:   true,
		},
		{
			description: "negative: an empty goip rendering fails on the structural facets even with the allowlist loaded",
			command:     "link show", ipA: refLinkShow, goip: "", ipB: refLinkShow,
			al:         al,
			wantFailed: true,
		},
		{
			description: "boundary: a difference between the two reference captures is subtracted and counted",
			// ipA and ipB disagree on mtu, so the mtu key is in the control
			// and the identical difference in the test is dropped.
			command: "link show",
			ipA:     refLinkShow,
			goip:    strings.Replace(refLinkShow, "mtu 1500", "mtu 9000", 1),
			ipB:     strings.Replace(refLinkShow, "mtu 1500", "mtu 4000", 1),
			al:      al, wantControlSize: 1, wantFailed: false,
		},
		{
			description: "corner: control subtraction runs before allowlisting, so a suppressed finding is attributed to the control",
			command:     "-6 addr show",
			ipA:         refAddrShow, goip: noQlen, ipB: noQlen,
			al: al,
			// The control already flags qlen, so the finding never reaches the
			// allowlist and AllowSuppressed stays empty. Which stage claimed
			// it is the difference between "the reference tool is noisy here"
			// and "we decided to accept this", and a report that conflated
			// them would hide a topology that moved mid-run.
			wantControlSize: 1,
			wantFailed:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			r := CompareStdoutWithControl(tt.command, tt.ipA, tt.goip, tt.ipB, tt.al)

			if !r.Compared {
				t.Error("Compared is false, but both sides were given")
			}
			if r.Failed() != tt.wantFailed {
				t.Errorf("Failed() = %v, want %v; findings = %v",
					r.Failed(), tt.wantFailed, r.Findings)
			}
			if tt.wantControlSize != 0 && r.ControlSize != tt.wantControlSize {
				t.Errorf("ControlSize = %d, want %d", r.ControlSize, tt.wantControlSize)
			}
			assertLoci(t, "Findings", r.Findings, tt.wantFindings)
			assertLoci(t, "AllowSuppressed", r.AllowSuppressed, tt.wantAllowSuppressed)
		})
	}
}

// assertLoci checks that a finding list holds exactly the named loci, when the
// row named any. A row that names none asserts nothing about that list, so a
// row can pin one bucket without having to enumerate the others.
func assertLoci(t *testing.T, what string, got []StdoutDivergence, want []string) {
	t.Helper()
	if len(want) == 0 {
		return
	}
	set := map[string]bool{}
	for _, d := range got {
		set[d.Locus()] = true
	}
	for _, l := range want {
		if !set[l] {
			t.Errorf("%s lacks %q; got %v", what, l, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s has %d entries %v, want %d %v", what, len(got), got, len(want), want)
	}
}

// go test ./internal/goipparity/ -run TestStdoutDivergenceString
func TestStdoutDivergenceString(t *testing.T) {
	tests := []struct {
		description string
		d           StdoutDivergence
		want        string
	}{
		{
			description: "positive: both sides present renders both values",
			d: StdoutDivergence{Facet: FacetKeyword("mtu"),
				Class: nlparity.DivergenceValue, Ref: "1500", Sub: "9000"},
			want: "stdout value stdout:keyword:mtu: ip=1500 goip=9000",
		},
		{
			description: "negative: goip absent says so, rather than rendering an empty string",
			d: StdoutDivergence{Facet: FacetIfNames,
				Class: nlparity.DivergencePresence, Ref: "goip0", Sub: ""},
			want: "stdout presence stdout:ifnames: ip=goip0 goip=<absent>",
		},
		{
			description: "negative: ip absent says so, which is the goip-invented-it direction",
			d: StdoutDivergence{Facet: FacetIfNames,
				Class: nlparity.DivergencePresence, Ref: "", Sub: "ghost0"},
			want: "stdout presence stdout:ifnames: ip=<absent> goip=ghost0",
		},
		{
			description: "boundary: both sides empty renders the locus alone, with no trailing colon",
			d:           StdoutDivergence{Facet: FacetLines, Class: nlparity.DivergencePresence},
			want:        "stdout presence stdout:lines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.d.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The two nexthop lines of the committed ip_route_main, byte for byte
// including the trailing space `print_rta_multipath` leaves on each. Spelled
// out rather than sliced out of the file so that a row which swaps or drops
// one is unambiguous about what it swapped or dropped.
const (
	routeNH1 = "\tnexthop via 192.0.2.10 dev goip0 weight 1 \n"
	routeNH2 = "\tnexthop via 192.0.2.11 dev goip0 weight 3 \n"
)

// routeGolden reads one committed `ip route show` sidecar from the same dump
// directory the netlink fixtures live in.
//
// Real `ip` output rather than a transcription: these facets exist to read
// what iproute2 actually prints, trailing spaces and tab-indented nexthop
// continuation lines included, and a hand-copied constant with that
// whitespace normalized away would be testing the copy.
func routeGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(tdGuest + "/" + name)
	if err != nil {
		t.Fatalf("reading sidecar %s: %v", name, err)
	}
	if len(b) == 0 {
		t.Fatalf("sidecar %s is empty, so any row using it is vacuous", name)
	}
	return string(b)
}

// TestStdoutRouteFacets pins the extraction half of the route work: what
// FacetDevNames, FacetFlags and the route keywords actually pull out of the
// committed goldens.
//
// Extraction is tested separately from comparison because the two fail
// differently. A facet that extracts nothing makes CompareStdout silent, and
// a silent comparator reads as a pass — so "the regex matched what I think it
// matched" has to be asserted directly rather than inferred from a clean
// diff.
//
// go test ./internal/goipparity/ -run TestStdoutRouteFacets
func TestStdoutRouteFacets(t *testing.T) {
	tests := []struct {
		description string
		// golden names a committed sidecar. When it is empty, in is used.
		golden string
		in     string
		facet  StdoutFacet
		// want is the whole multiset for that facet, counts included. An
		// exact map, not a subset: an element appearing twice where it should
		// appear once is the duplicate-line bug the multisets exist for.
		want map[string]int
		// countOnly is for the one facet whose VALUES are random per boot:
		// the v6 tunnels' permaddr comes from eth_random_addr, so the set
		// cannot be written down and only its size is stable. Set it instead
		// of want; zero means "use want".
		countOnly int
	}{
		{
			description: "positive: every `dev goip0` in ip_route_main is extracted, the six route lines and the two nexthop continuations",
			golden:      "ip_route_main",
			facet:       FacetDevNames,
			want:        map[string]int{"goip0": 8},
		},
		{
			description: "positive: ip_route_table_all reaches lo as well as goip0, which is the second interface the lazy name resolution has to fetch",
			golden:      "ip_route_table_all",
			facet:       FacetDevNames,
			want:        map[string]int{"goip0": 20, "lo": 4},
		},
		{
			description: "positive: the via facet separates the two ECMP gateways and records `inet6` for the RFC-5549 route",
			golden:      "ip_route_main",
			facet:       FacetKeyword("via"),
			// `via inet6 2001:db8::2` contributes `inet6`, not the address:
			// print_rta_via prints the family word first. The address is
			// still compared, by the netlink side, which sees RTA_VIA whole.
			want: map[string]int{"192.0.2.10": 4, "192.0.2.11": 1, "inet6": 1},
		},
		{
			description: "positive: the ECMP weights are extracted apart, so rendering both legs with weight 1 is a finding",
			golden:      "ip_route_main",
			facet:       FacetKeyword("weight"),
			want:        map[string]int{"1": 1, "3": 1},
		},
		{
			description: "positive: the nexthop facet counts the two continuation lines of the ECMP route",
			golden:      "ip_route_main",
			facet:       FacetNextHops,
			want:        map[string]int{"nexthop": 2},
		},
		{
			description: "boundary: a listing with no multipath route has an empty nexthops facet, not a missing one",
			golden:      "mesh/ip_route_main",
			facet:       FacetNextHops,
			want:        map[string]int{},
		},
		{
			description: "positive: RTA_METRICS reaches stdout as mtu",
			golden:      "ip_route_main",
			facet:       FacetKeyword("mtu"),
			want:        map[string]int{"1400": 1},
		},
		{
			description: "positive: RTA_METRICS reaches stdout as advmss",
			golden:      "ip_route_main",
			facet:       FacetKeyword("advmss"),
			want:        map[string]int{"1300": 1},
		},
		{
			description: "positive: RTA_PREFSRC reaches stdout as src",
			golden:      "ip_route_main",
			facet:       FacetKeyword("src"),
			want:        map[string]int{"192.0.2.1": 1},
		},
		{
			description: "positive: the v6 metrics are extracted, the kernel's 256 and the topology's 1024",
			golden:      "ip_route6",
			facet:       FacetKeyword("metric"),
			want:        map[string]int{"256": 2, "1024": 3},
		},
		{
			description: "positive: RTA_PREF ends every v6 line and is compared, which is why pref is a keyword at all",
			golden:      "ip_route6",
			facet:       FacetKeyword("pref"),
			want:        map[string]int{"medium": 5},
		},
		{
			description: "positive: `table local` is extracted from every table-all line that carries it",
			golden:      "ip_route_table_all",
			facet:       FacetKeyword("table"),
			want:        map[string]int{"local": 10},
		},
		{
			description: "positive: linkdown at end of line is extracted, which no keyword facet could do",
			golden:      "mesh/ip_route_main",
			facet:       FacetFlags,
			want:        map[string]int{"linkdown": 2},
		},
		{
			description: "positive: linkdown mid-line, before `pref medium`, is the same element as linkdown at end of line",
			golden:      "mesh/ip_route6",
			facet:       FacetFlags,
			want:        map[string]int{"linkdown": 1},
		},
		{
			description: "negative: link stanzas contribute no device names, so the facet cannot fire spuriously on `link show`",
			in:          refLinkShow,
			facet:       FacetDevNames,
			want:        map[string]int{},
		},
		{
			description: "negative: a route listing with no flags set has an empty flags facet rather than a missing one",
			golden:      "ip_route_main",
			facet:       FacetFlags,
			want:        map[string]int{},
		},
		{
			// The live one. `neigh show proxy` is gated, and until `proxy`
			// joined flagTokens the token that distinguishes this command's
			// output from `neigh show`'s was compared only as part of a line
			// count. Both golden lines carry it, so the count is two.
			description: "positive: both proxy entries in ip_neigh_proxy are extracted, on a command that is already gated",
			golden:      "ip_neigh_proxy",
			facet:       FacetFlags,
			want:        map[string]int{"proxy": 2},
		},
		{
			// The A/B that makes the row above mean something: the two
			// commands walk different kernel tables, and the plain listing
			// has no proxy entry in it at all. If `proxy` were matching some
			// substring rather than the flag token, it would fire here too.
			//
			// The other three tokens ARE here now, and asserting them by
			// name is what keeps this a negative about `proxy` rather than
			// a negative about flags in general. It read `map[string]int{}`
			// while the topology had no flagged neighbor — an emptiness that
			// was a fact about the fixture and not about pneigh, and that
			// would have gone on passing if `proxy` were dropped from
			// flagTokens entirely.
			//
			// The counts come from nltopo::build_clean: `router` on .53, .56
			// and 2001:db8::53; `extern_learn` on .54 and .56;
			// `extern_valid` on .55, .56 and 2001:db8::53.
			description: "negative: the plain ip_neigh listing carries the other flag tokens but no proxy, since pneigh is a different table",
			golden:      "ip_neigh",
			facet:       FacetFlags,
			want: map[string]int{
				"router": 3, "extern_learn": 2, "extern_valid": 3,
			},
		},
		{
			description: "positive: the neigh flag run is extracted whole, including the two tokens that come from NDA_FLAGS_EXT",
			in:          "192.0.2.72 dev goip0 lladdr 02:00:00:00:00:03 router proxy managed extern_learn offload extern_valid STALE \n",
			facet:       FacetFlags,
			want: map[string]int{
				"router": 1, "proxy": 1, "managed": 1,
				"extern_learn": 1, "offload": 1, "extern_valid": 1,
			},
		},
		{
			// `offload` is in flagTokens once and serves both vocabularies —
			// print_rt_flags and print_neigh both spell it that way. This row
			// checks that the single entry really does cover both, rather
			// than the route spelling shadowing a neighbor one that was never
			// added.
			description: "corner: offload is one element serving both the route and the neighbor vocabulary",
			in:          "192.0.2.80 dev goip0 offload STALE \n",
			facet:       FacetFlags,
			want:        map[string]int{"offload": 1},
		},
		{
			// The word-boundary claim, tested rather than asserted in a
			// comment. `proxy_arp` and `rt_offload` each contain a shorter
			// token, and `_` being a word character is the only thing
			// stopping them matching it. rt_offload IS itself a token, so it
			// must appear exactly once and not also as `offload`.
			description: "corner: proxy_arp does not yield proxy, and rt_offload yields itself rather than offload",
			in:          "192.0.2.81 dev goip0 proxy_arp rt_offload \n",
			facet:       FacetFlags,
			want:        map[string]int{"rt_offload": 1},
		},
		{
			description: "negative: a link listing contributes no neighbor flag tokens, so the new entries cannot fire on link show",
			in:          refLinkShow,
			facet:       FacetFlags,
			want:        map[string]int{},
		},
		{
			description: "boundary: empty output yields an empty facet, which is what makes the diff against a non-empty side fire",
			in:          "",
			facet:       FacetDevNames,
			want:        map[string]int{},
		},
		{
			description: "corner: a device literally named `dev` is extracted once, because the scan is non-overlapping",
			in:          "192.0.2.0/24 dev dev scope link \n",
			facet:       FacetDevNames,
			want:        map[string]int{"dev": 1},
		},
		{
			description: "corner: a device name ending in `dev` is not split, because \\bdev needs a word boundary",
			in:          "192.0.2.0/24 dev netdev0 scope link \n",
			facet:       FacetDevNames,
			want:        map[string]int{"netdev0": 1},
		},
		{
			description: "corner: rt_offload is one token and does not also register as offload, because _ is a word character",
			in:          "192.0.2.0/24 dev goip0 rt_offload \n",
			facet:       FacetFlags,
			want:        map[string]int{"rt_offload": 1},
		},
		{
			description: "corner: preferred_lft does not register as pref, because the keyword pattern requires whitespace after the name",
			in:          refAddrShow,
			facet:       FacetKeyword("pref"),
			want:        map[string]int{},
		},
		{
			// The three ll_addr_n2a keywords, against the goldens that
			// actually contain them. A keyword added to the list but not
			// matched by the pattern is precisely the silent failure this
			// test exists for: CompareStdout would stay quiet and read as a
			// pass.
			//
			// tunnel/ip_link is the only golden with `peer`, and it has five
			// — one per configured tunnel. The five fallback devices take the
			// `brd` arm instead, which is why this is 5 and not 10.
			description: "positive: peer is extracted from tunnel/ip_link, once per configured tunnel",
			golden:      "tunnel/ip_link",
			facet:       FacetKeyword("peer"),
			want: map[string]int{
				"198.51.100.1": 1, "198.51.100.2": 1, "198.51.100.3": 1,
				"2001:db8:100::1": 1, "2001:db8:100::2": 1,
			},
		},
		{
			// permaddr prints only where it DIFFERS from the address, so four
			// of the fourteen links carry it: the v6 tunnels, whose perm_addr
			// comes from eth_random_addr. The values change every boot, so
			// this row asserts the count alone.
			description: "positive: permaddr is extracted from tunnel/ip_link on exactly the four v6 tunnels",
			golden:      "tunnel/ip_link",
			facet:       FacetKeyword("permaddr"),
			countOnly:   4,
		},
		{
			// Eight of the nine entries: 192.0.2.52 is the `nud incomplete`
			// one, added with no lladdr precisely so an absent NDA_LLADDR
			// has a positive. That it is the only one missing here is the
			// assertion — a keyword facet that invented an empty value for
			// it would show a ninth key.
			description: "positive: lladdr is extracted from ip_neigh, on the eight entries that have one",
			golden:      "ip_neigh",
			facet:       FacetKeyword("lladdr"),
			want: map[string]int{
				"02:00:00:00:00:01": 1, "02:00:00:00:00:02": 1, "02:00:00:00:00:03": 1,
				"02:00:00:00:00:04": 1, "02:00:00:00:00:05": 1, "02:00:00:00:00:06": 1,
				"02:00:00:00:00:07": 1, "02:00:00:00:00:08": 1,
			},
		},
		{
			// The type-driven values, which are why lladdr is in the list at
			// all. A renderer that ignored the device's ifi_type would put
			// "02:00:00:00" and "20:01:0d:b8:…" here instead, and the facet
			// now reports the difference rather than missing it.
			description: "positive: tunnel/ip_neigh's lladdrs are the ll_addr_n2a forms, not colon-hex",
			golden:      "tunnel/ip_neigh",
			facet:       FacetKeyword("lladdr"),
			want:        map[string]int{"2.0.0.0": 1, "2001:db8::63": 1},
		},
		{
			description: "negative: the clean link golden has no peer, because nothing in it is IFF_POINTOPOINT",
			golden:      "ip_link",
			facet:       FacetKeyword("peer"),
			want:        map[string]int{},
		},
		{
			description: "negative: the clean link golden has no permaddr, because no address there differs from its perm_addr",
			golden:      "ip_link",
			facet:       FacetKeyword("permaddr"),
			want:        map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			in := tt.in
			if tt.golden != "" {
				in = routeGolden(t, tt.golden)
			}
			got, ok := stdoutFacets(in)[tt.facet]
			if !ok {
				t.Fatalf("facet %q is not extracted at all; it must be in Facets() or keywords", tt.facet)
			}
			if tt.countOnly != 0 {
				total := 0
				for _, n := range got {
					total += n
				}
				if total != tt.countOnly {
					t.Errorf("facet %q has %d values, want %d; whole set %v",
						tt.facet, total, tt.countOnly, map[string]int(got))
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("facet %q = %v, want %v", tt.facet, map[string]int(got), tt.want)
			}
			for k, n := range tt.want {
				if got[k] != n {
					t.Errorf("facet %q element %q counted %d, want %d; whole set %v",
						tt.facet, k, got[k], n, map[string]int(got))
				}
			}
		})
	}
}

// TestStdoutStatsHeaderFacet pins the extraction half of FacetStatsHeaders
// against the committed `ip -s link show` sidecars and against the shapes no
// capture on this topology produces.
//
// The committed rows are the ones that matter, because they are the only
// evidence that reStatsHeader matches what the pinned `ip` actually prints
// rather than what this file assumes it prints. The constructed rows cover
// the conditional seventh column and the two ways the pattern could overmatch.
//
// go test ./internal/goipparity/ -run TestStdoutStatsHeaderFacet
func TestStdoutStatsHeaderFacet(t *testing.T) {
	const (
		rx = "RX:bytes,packets,errors,dropped,missed,mcast"
		tx = "TX:bytes,packets,errors,dropped,carrier,collsns"
	)

	tests := []struct {
		description string
		golden      string
		in          string
		want        map[string]int
	}{
		{
			description: "positive: the clean topology's three links each contribute one RX and one TX heading",
			golden:      "ip_link_stats",
			want:        map[string]int{rx: 3, tx: 3},
		},
		{
			description: "positive: the mesh topology's five links scale the same way, which is what makes this per-line rather than per-command",
			golden:      "mesh/ip_link_stats",
			want:        map[string]int{rx: 5, tx: 5},
		},
		{
			description: "positive: RX and TX are distinct elements, so a renderer that headed both lines RX is a finding",
			in: "    RX: bytes packets errors dropped missed mcast\n" +
				"    TX: bytes packets errors dropped carrier collsns\n",
			want: map[string]int{rx: 1, tx: 1},
		},
		{
			description: "boundary: the column widths are discarded, so the same headings at different widths are the same element",
			in: "    RX:  bytes packets errors dropped  missed   mcast           \n" +
				"    RX:      bytes    packets errors dropped missed mcast\n",
			want: map[string]int{rx: 2},
		},
		{
			description: "positive: the conditional seventh column appears as a seventh element when rx_compressed is non-zero",
			in:          "    RX: bytes packets errors dropped missed mcast compressed\n",
			want:        map[string]int{rx + ",compressed": 1},
		},
		{
			description: "negative: `ip link show` has no stats block at all, so the facet is empty rather than missing",
			in:          refLinkShow,
			want:        map[string]int{},
		},
		{
			description: "negative: a value line is not a header line, because every field of it is a digit and the pattern is anchored on the direction word",
			in:          "             0       0      0       0       0       0 \n",
			want:        map[string]int{},
		},
		{
			description: "boundary: `ip -s -s`'s second-level `RX errors:` line is not matched, because the colon is not on the direction word",
			in:          "    RX errors: length crc frame fifo missed\n",
			want:        map[string]int{},
		},
		{
			description: "corner: a column-zero `RX:` is not a stats header — the block is indented four spaces and the pattern requires leading whitespace",
			in:          "RX: bytes packets errors dropped missed mcast\n",
			want:        map[string]int{},
		},
		{
			description: "corner: a heading line with a trailing-only remainder is not matched, because \\S must follow the colon",
			in:          "    RX:    \n",
			want:        map[string]int{},
		},
		{
			description: "boundary: empty output yields an empty facet, which is what makes the diff against a side that has stats fire",
			in:          "",
			want:        map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			in := tt.in
			if tt.golden != "" {
				in = routeGolden(t, tt.golden)
			}
			got, ok := stdoutFacets(in)[FacetStatsHeaders]
			if !ok {
				t.Fatalf("FacetStatsHeaders is not extracted at all; it must be in Facets()")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("FacetStatsHeaders = %v, want %v", map[string]int(got), tt.want)
			}
			for k, n := range tt.want {
				if got[k] != n {
					t.Errorf("element %q counted %d, want %d; whole set %v",
						k, got[k], n, map[string]int(got))
				}
			}
		})
	}
}

// TestCompareStdoutStats is the comparison half: which divergences a wrong
// `-s` rendering produces, and — just as important — which correct-but-noisy
// ones it does not.
//
// The width rows are the reason this facet exists in the form it does. A
// verbatim comparison of the header lines would fire on every run in which a
// counter crossed a power of ten, which on a live interface is most of them.
//
// go test ./internal/goipparity/ -run TestCompareStdoutStats
func TestCompareStdoutStats(t *testing.T) {
	ref := routeGolden(t, "ip_link_stats")

	// widened is the same output with every stats column one space wider,
	// which is what size_columns does when a counter gains a digit. It must
	// produce no statsheaders divergence.
	widened := strings.ReplaceAll(ref, "    RX:", "    RX: ")
	widened = strings.ReplaceAll(widened, "    TX:", "    TX: ")

	tests := []struct {
		description string
		got         string
		wantLoci    []string
	}{
		{
			description: "positive: identical output diverges nowhere",
			got:         ref,
			wantLoci:    nil,
		},
		{
			description: "positive: widening every column changes no heading, which is the whole reason the facet splits on whitespace",
			got:         widened,
			wantLoci:    nil,
		},
		{
			description: "negative: dropping the stats block entirely fires statsheaders as well as the line count",
			got:         stripStatsBlock(ref),
			wantLoci: []string{
				FacetLines.Locus(), FacetStatsHeaders.Locus(),
			},
		},
		{
			description: "negative: `missed` rendered as `over` — the one column whose text and JSON names disagree upstream — fires statsheaders and nothing else",
			got:         strings.ReplaceAll(ref, "  missed ", "    over "),
			wantLoci:    []string{FacetStatsHeaders.Locus()},
		},
		{
			description: "negative: heading the TX line RX is a finding even though the multiset of words is unchanged, because the direction is part of the element",
			got:         strings.ReplaceAll(ref, "    TX:", "    RX:"),
			wantLoci:    []string{FacetStatsHeaders.Locus()},
		},
		{
			description: "boundary: a stats block missing from one link of three is a count of 2 against 3, not a matching set",
			got:         dropFirstStatsBlock(ref),
			wantLoci: []string{
				FacetLines.Locus(), FacetStatsHeaders.Locus(),
			},
		},
		{
			description: "corner: changing only the counter VALUES fires nothing, which is deliberate — those are live and D_control would subtract them anyway",
			got:         strings.ReplaceAll(ref, "65796", "99999"),
			wantLoci:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			ds := CompareStdout("-s link show", ref, tt.got)
			var got []string
			for _, d := range ds {
				got = append(got, d.Locus())
			}
			sort.Strings(got)
			want := append([]string(nil), tt.wantLoci...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("loci = %v, want %v\n%v", got, want, ds)
			}
		})
	}
}

// stripStatsBlock removes every stats line, header and value both, leaving
// the output `ip link show` would have produced.
func stripStatsBlock(s string) string { return dropStatsLines(s, -1) }

// dropFirstStatsBlock removes only the first link's two-line stats block, so
// the two sides differ by one block rather than by all of them.
func dropFirstStatsBlock(s string) string { return dropStatsLines(s, 2) }

// dropStatsLines removes up to limit stats lines, or all of them when limit
// is negative.
//
// It splits on "\n" and rejoins rather than using SplitAfter, because
// reStatsHeader is not compiled with the `m` flag: in Go `$` matches only at
// the end of the text, so a line that still carries its own newline never
// matches. stdoutFacets splits the same way, which is the point — a helper
// that recognized stats lines by a different rule than the code under test
// would be testing itself.
func dropStatsLines(s string, limit int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	dropped := 0
	for _, line := range lines {
		if (limit < 0 || dropped < limit) &&
			(reStatsHeader.MatchString(line) || isStatsValueLine(line)) {
			dropped++
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// isStatsValueLine reports whether a line is one of print_stats64's value
// lines: leading whitespace and then nothing but digits and spaces.
func isStatsValueLine(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 || line == strings.TrimLeft(line, " ") {
		return false
	}
	for _, tok := range f {
		if strings.TrimLeft(tok, "0123456789") != "" {
			return false
		}
	}
	return true
}

// TestCompareStdoutRoute is the comparison half: which loci fire when a route
// rendering goes wrong, and with which class.
//
// Before FacetDevNames and FacetFlags existed, every row here that is not the
// empty-output one reported nothing at all — reStanza is anchored on a `N:
// name` header that route output does not have, so ifnames and ifindexes are
// permanently empty for this object and the comparison came down to a line
// count. That is the hole these rows close.
//
// go test ./internal/goipparity/ -run TestCompareStdoutRoute
func TestCompareStdoutRoute(t *testing.T) {
	main := routeGolden(t, "ip_route_main")
	mesh := routeGolden(t, "mesh/ip_route_main")

	// Constructed once and checked, because a strings.Replace that matched
	// nothing would silently turn the row it feeds into "identical == clean".
	noDev := strings.ReplaceAll(main, " dev goip0", "")
	noLinkdown := strings.ReplaceAll(mesh, " linkdown", "")
	swapped := strings.Replace(main, routeNH1+routeNH2, routeNH2+routeNH1, 1)
	oneLeg := strings.Replace(main, routeNH2, "", 1)
	for _, c := range []struct {
		name     string
		mutated  string
		original string
	}{
		{"noDev", noDev, main},
		{"noLinkdown", noLinkdown, mesh},
		{"swapped", swapped, main},
		{"oneLeg", oneLeg, main},
	} {
		if c.mutated == c.original {
			t.Fatalf("%s did not change the golden, so its row would assert nothing", c.name)
		}
	}

	tests := []struct {
		description string
		ref, sub    string
		wantLoci    []string
		wantClass   map[string]nlparity.DivergenceClass
	}{
		{
			description: "positive: identical route listings produce no findings",
			ref:         main,
			sub:         main,
			wantLoci:    nil,
		},
		{
			description: "negative: a rendering that drops every `dev` token is caught, and the finding is unsuppressible",
			// The Risk 1 failure mode, and the reason FacetDevNames exists:
			// the line count, the prefixes, the gateways and the metrics all
			// still match, so before this facet the listing compared clean.
			ref:      main,
			sub:      noDev,
			wantLoci: []string{"stdout:devnames"},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:devnames": nlparity.DivergencePresence,
			},
		},
		{
			description: "negative: a dropped linkdown is caught, which is the only thing distinguishing the mesh topology's output",
			ref:         mesh,
			sub:         noLinkdown,
			wantLoci:    []string{"stdout:flags"},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:flags": nlparity.DivergencePresence,
			},
		},
		{
			description: "negative: an empty rendering fires every facet the golden populates, and the structural ones are unsuppressible",
			ref:         main,
			sub:         "",
			// No ifnames or ifindexes: route output has no stanza header, so
			// those two facets are empty on BOTH sides and correctly stay
			// quiet. No macs, and no flags, because this topology has neither.
			wantLoci: []string{
				"stdout:lines", "stdout:devnames", "stdout:cidrs", "stdout:nexthops",
				"stdout:keyword:proto", "stdout:keyword:scope", "stdout:keyword:src",
				"stdout:keyword:via", "stdout:keyword:mtu", "stdout:keyword:advmss",
				"stdout:keyword:weight",
			},
			wantClass: map[string]nlparity.DivergenceClass{
				"stdout:lines":    nlparity.DivergencePresence,
				"stdout:devnames": nlparity.DivergencePresence,
				"stdout:cidrs":    nlparity.DivergencePresence,
			},
		},
		{
			description: "boundary: dropping one leg of the ECMP pair is caught on five facets at once",
			ref:         main,
			sub:         oneLeg,
			wantLoci: []string{
				"stdout:lines", "stdout:devnames", "stdout:nexthops",
				"stdout:keyword:via", "stdout:keyword:weight",
			},
		},
		{
			description: "boundary: reordering the two nexthop lines is NOT reported, because every facet is a multiset",
			// Recorded rather than fixed. An ordering facet here would be
			// byte-for-byte stdout parity by the back door, which is out of
			// scope for exactly the reasons the file header gives — and
			// nexthop order is already compared exactly on the netlink side,
			// where RTA_MULTIPATH is one attribute whose bytes are diffed
			// whole. If goip ever emitted the legs in the wrong order, the
			// request/reply tiers would say so; stdout deliberately would not.
			ref:      main,
			sub:      swapped,
			wantLoci: nil,
		},
		{
			description: "corner: renaming the device fires devnames alone, since nothing else on the line moved",
			ref:         main,
			sub:         strings.ReplaceAll(main, "dev goip0", "dev eth0"),
			wantLoci:    []string{"stdout:devnames"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := CompareStdout("route show", tt.ref, tt.sub)

			gotLoci := map[string]nlparity.DivergenceClass{}
			for _, d := range got {
				gotLoci[d.Locus()] = d.Class
			}
			want := map[string]bool{}
			for _, l := range tt.wantLoci {
				want[l] = true
			}
			for l := range gotLoci {
				if !want[l] {
					t.Errorf("unexpected finding at %q", l)
				}
			}
			for l := range want {
				if _, ok := gotLoci[l]; !ok {
					t.Errorf("missing finding at %q", l)
				}
			}
			for l, wantClass := range tt.wantClass {
				if gotClass, ok := gotLoci[l]; ok && gotClass != wantClass {
					t.Errorf("finding at %q has class %s, want %s", l, gotClass, wantClass)
				}
			}
		})
	}
}
