package goipparity

import (
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
