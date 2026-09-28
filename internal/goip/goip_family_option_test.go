package goip

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestRunFamilyOption drives the three options that set preferred_family on a
// read-only path: -4, -6 and -0.
//
// # Why -0 has a test of its own and -4 does not need one
//
// -4 and -6 were reachable from the day the parser was written. -0 was not:
// `addr show`'s AF_PACKET branch has existed in obj_addr.go and in
// Service.AddressSnapshot since they were written, and nothing could reach it,
// because -0 is the only argv `ip` sets preferred_family to AF_PACKET from on
// a show path (ip/ip.c:221-222) and goip did not parse it. A branch no input
// can reach is not covered by the fact that it compiles.
//
// The rows assert the PARSE, not the dump: each passes options and no object,
// so Run parses, fails the "no object" check and prints usage without opening
// a socket. That is the same shape as TestRunStatsOption and for the same
// reason — a row must not be able to fail for a reason belonging to a dump.
//
// go test ./internal/goip/ -run TestRunFamilyOption
func TestRunFamilyOption(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		// wantAccepted is false for a row the parser must reject.
		wantAccepted bool
	}{
		{
			description:  "positive: no family option leaves AF_UNSPEC, which is what makes `addr show` list both families",
			args:         nil,
			wantAccepted: true,
		},
		{
			description:  "positive: -4 is AF_INET",
			args:         []string{"-4"},
			wantAccepted: true,
		},
		{
			description:  "positive: -6 is AF_INET6",
			args:         []string{"-6"},
			wantAccepted: true,
		},
		{
			description:  "positive: -0 is AF_PACKET, the branch that was written before anything could reach it",
			args:         []string{"-0"},
			wantAccepted: true,
		},
		{
			description:  "boundary: the last family option wins, because each assigns rather than or-ing",
			args:         []string{"-4", "-0"},
			wantAccepted: true,
		},
		{
			description:  "boundary: and in the other order, so the row above is not passing on AF_PACKET's value alone",
			args:         []string{"-0", "-4"},
			wantAccepted: true,
		},
		{
			description:  "positive: -0 composes with another option, which is the shape `-0 -j addr show` needs",
			args:         []string{"-0", "-j"},
			wantAccepted: true,
		},
		{
			description: "negative: -00 is not -0, because `ip` compares the three family options with strcmp and not matches()",
			args:        []string{"-00"},
			// A prefix rule here would have made `-00` set AF_PACKET.
			wantAccepted: false,
		},
		{
			description:  "negative: -0x is likewise rejected rather than prefix-matched",
			args:         []string{"-0x"},
			wantAccepted: false,
		},
		{
			description: "corner: -M is AF_MPLS in `ip` and is deliberately NOT implemented here, so it must be rejected rather than silently ignored",
			args:        []string{"-M"},
			// ip/ip.c:223-226 has -M and -B too. goip has no MPLS or bridge
			// object, so accepting them would mean parsing an option that
			// changes nothing — the kind of quiet no-op the parity harness
			// reports as a rendering bug three commands later.
			wantAccepted: false,
		},
	}

	const unknown = "is unknown"

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != ExitUsage {
				t.Fatalf("Run(%q) = %d, want %d (no object, so usage)\nstderr: %s",
					tt.args, code, ExitUsage, stderr.String())
			}
			gotUnknown := strings.Contains(stderr.String(), unknown)
			if gotUnknown == tt.wantAccepted {
				t.Errorf("Run(%q): unknown-option message present = %v, want accepted = %v\nstderr: %s",
					tt.args, gotUnknown, tt.wantAccepted, stderr.String())
			}
		})
	}
}

// TestAddrShowPacketFamily is the other half, and the half that matters: what
// the branch -0 now reaches actually does.
//
// The assertions are relative to the AF_UNSPEC run of the same source rather
// than to a golden, because there is no `ip -0 addr show` sidecar in the
// corpus and a hand-written expectation would only restate obj_addr.go's own
// filter. Stated as a relation, it is checkable:
//
//   - AF_PACKET drops every address line and NOTHING else, so the stanza
//     headers must be byte-identical to the AF_UNSPEC run's;
//   - AF_PACKET does not filter LINKS, which is the one way it differs from
//     -4 and -6 — filterLinksWithAddrs is skipped entirely (obj_addr.go:100),
//     so a link with no addresses at all still appears.
//
// go test ./internal/goip/ -run TestAddrShowPacketFamily
func TestAddrShowPacketFamily(t *testing.T) {
	headers := func(s string) []string {
		var out []string
		for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
			if !strings.HasPrefix(l, " ") {
				out = append(out, l)
			}
		}
		return out
	}

	pkt := runAddrShow(t, newCompositeSource(t), unix.AF_PACKET, false)
	unspec := runAddrShow(t, newCompositeSource(t), unix.AF_UNSPEC, false)
	v4 := runAddrShow(t, newCompositeSource(t), unix.AF_INET, false)

	tests := []struct {
		description string
		check       func(t *testing.T)
	}{
		{
			description: "positive: the output is non-empty, so no row below is vacuous",
			check: func(t *testing.T) {
				if len(headers(pkt)) == 0 {
					t.Fatalf("AF_PACKET rendered no stanzas:\n%s", pkt)
				}
			},
		},
		{
			description: "positive: the stanza headers are byte-identical to the AF_UNSPEC run, so only address lines were dropped",
			check: func(t *testing.T) {
				gotH, wantH := headers(pkt), headers(unspec)
				if len(gotH) != len(wantH) {
					t.Fatalf("AF_PACKET has %d stanzas, AF_UNSPEC has %d", len(gotH), len(wantH))
				}
				for i := range wantH {
					if gotH[i] != wantH[i] {
						t.Errorf("stanza %d differs\n got: %q\nwant: %q", i, gotH[i], wantH[i])
					}
				}
			},
		},
		{
			description: "negative: no line carries an address, which is the whole content of the AF_PACKET branch",
			check: func(t *testing.T) {
				for i, l := range strings.Split(pkt, "\n") {
					for _, bad := range []string{"inet ", "inet6 ", "valid_lft", "preferred_lft"} {
						if strings.Contains(l, bad) {
							t.Errorf("line %d contains %q: %q", i+1, bad, l)
						}
					}
				}
			},
		},
		{
			description: "positive: every line that is not a stanza header is a link-layer line, because that is all that survives",
			check: func(t *testing.T) {
				for _, l := range strings.Split(strings.TrimSuffix(pkt, "\n"), "\n") {
					if !strings.HasPrefix(l, " ") {
						continue
					}
					tr := strings.TrimLeft(l, " ")
					if !strings.HasPrefix(tr, "link/") && !strings.HasPrefix(tr, "altname ") {
						t.Errorf("unexpected indented line under AF_PACKET: %q", l)
					}
				}
			},
		},
		{
			description: "boundary: AF_PACKET keeps at least as many stanzas as AF_INET, because it is the one family that does not filter links",
			check: func(t *testing.T) {
				if len(headers(pkt)) < len(headers(v4)) {
					t.Errorf("AF_PACKET has %d stanzas, fewer than AF_INET's %d — "+
						"filterLinksWithAddrs must not run on the AF_PACKET path",
						len(headers(pkt)), len(headers(v4)))
				}
			},
		},
		{
			description: "corner: AF_PACKET and AF_INET are not the same output, so the row above is not comparing a listing with itself",
			check: func(t *testing.T) {
				if pkt == v4 {
					t.Errorf("AF_PACKET and AF_INET rendered identically, which means "+
						"the family is not reaching the dump:\n%s", pkt)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, tt.check)
	}
}

// TestRunFamilyOptionInUsage keeps the usage text honest about -0, the same
// drift TestRunStatsOptionInUsage guards for -s.
//
// go test ./internal/goip/ -run TestRunFamilyOptionInUsage
func TestRunFamilyOptionInUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-help"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(-help) = %d, want %d", code, ExitOK)
	}
	for _, opt := range []string{"-4", "-6", "-0"} {
		if !strings.Contains(stdout.String(), opt) {
			t.Errorf("usage does not list %s:\n%s", opt, stdout.String())
		}
	}
}
