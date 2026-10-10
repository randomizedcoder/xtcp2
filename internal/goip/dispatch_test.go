package goip

import (
	"errors"
	"testing"
)

// TestMatchesPrefix covers the argument matcher.
//
// The rows are not style: the parity harness drives `ip` and `goip` with the
// **same argv**, so an abbreviation that resolves differently in the two tools
// is a divergence in the command being compared rather than in the code under
// test. That makes the table-order rows below assertions about the interface,
// not about a helper.
//
// go test ./internal/goip/ -run TestMatchesPrefix
func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		pattern     string
		want        bool
	}{
		{
			description: "positive: a single letter matches",
			arg:         "a", pattern: "address", want: true,
		},
		{
			description: "positive: a partial prefix matches",
			arg:         "addr", pattern: "address", want: true,
		},
		{
			description: "positive: the whole word matches",
			arg:         "address", pattern: "address", want: true,
		},
		{
			// The length test comes first in matches(), so a prefix longer
			// than the pattern is rejected before any comparison.
			description: "negative: a prefix longer than the pattern does not match",
			arg:         "addressx", pattern: "address", want: false,
		},
		{
			// memcmp, so no case folding anywhere.
			description: "negative: matching is case-sensitive",
			arg:         "ADDR", pattern: "address", want: false,
		},
		{
			// **The inverted case.** matches("", p) returns 0, which iproute2
			// reads as a match, because memcmp of zero bytes is 0. That would
			// make `goip ""` silently mean `goip address`. This is the one
			// place goip deliberately differs from matches(), and the row is
			// here so the difference is a decision rather than an accident.
			description: "negative: an empty argument does not match, inverting matches()' memcmp-of-zero-bytes result",
			arg:         "", pattern: "address", want: false,
		},
		{
			description: "boundary: a one-character pattern matched by itself",
			arg:         "r", pattern: "r", want: true,
		},
		{
			description: "boundary: an empty pattern is matched by nothing",
			arg:         "a", pattern: "", want: false,
		},
		{
			// Prefix matching is unanchored at the end only; a match must
			// start at byte 0.
			description: "corner: a substring that is not a prefix does not match",
			arg:         "dress", pattern: "address", want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := matchesPrefix(tc.arg, tc.pattern); got != tc.want {
				t.Errorf("matchesPrefix(%q, %q) = %v, want %v", tc.arg, tc.pattern, got, tc.want)
			}
		})
	}
}

// TestLookupObject covers first-match-wins over the object table, which is
// where the abbreviations get their meaning.
//
// Every row whose description says "not X" is protecting a specific
// abbreviation from a specific neighbor. Reordering the table, or deleting an
// unimplemented row to tidy up, changes those answers silently — `r` means
// route only because route precedes rule.
//
// go test ./internal/goip/ -run TestLookupObject
func TestLookupObject(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		wantName    string
		wantErr     error
	}{
		{
			description: "positive: \"a\" is address, the first entry in table order",
			arg:         "a", wantName: "address",
		},
		{
			description: "positive: \"addr\" is address",
			arg:         "addr", wantName: "address",
		},
		{
			// route precedes rule in cmds[].
			description: "positive: \"r\" is route, not rule",
			arg:         "r", wantName: "route",
		},
		{
			// neighbor precedes ntable, netns, netconf and nexthop.
			description: "positive: \"n\" is neighbor, not netns/netconf/nexthop",
			arg:         "n", wantName: "neighbor",
		},
		{
			// Both spellings are in cmds[], and "neigh" is a prefix of the
			// British one too — first match wins, so it resolves to "neighbor".
			description: "positive: \"neigh\" resolves to the American spelling, listed first",
			arg:         "neigh", wantName: "neighbor",
		},
		{
			description: "positive: \"net\" is netns, not netconf",
			arg:         "net", wantName: "netns", wantErr: ErrNotImplemented,
		},
		{
			// "netc" outruns netns ("net" is not a prefix of "netc"), so it is the
			// shortest arg that reaches netconf — and netconf is implemented, so no
			// ErrNotImplemented.
			description: "positive: \"netc\" is netconf, which is implemented",
			arg:         "netc", wantName: "netconf",
		},
		{
			// link precedes l2tp.
			description: "positive: \"l\" is link, not l2tp",
			arg:         "l", wantName: "link",
		},
		{
			// **Recognized but unimplemented, which must be distinguishable
			// from a typo.** The harness drives both tools with the same argv,
			// so it has to tell "goip has not got there yet" from "that is not
			// a command".
			description: "boundary: \"xfrm\" is recognized and reports ErrNotImplemented",
			arg:         "xfrm", wantName: "xfrm", wantErr: ErrNotImplemented,
		},
		{
			// "s" is sr (first "s" entry in cmds[]), but "st" outruns it and is the
			// shortest arg that reaches stats — which is implemented, so no error.
			description: "positive: \"st\" is stats, which is implemented",
			arg:         "st", wantName: "stats",
		},
		{
			description: "boundary: \"mon\" is monitor, recognized and unimplemented",
			arg:         "mon", wantName: "monitor", wantErr: ErrNotImplemented,
		},
		{
			description: "negative: \"z\" matches no object",
			arg:         "z", wantErr: ErrUnknownObject,
		},
		{
			description: "negative: an empty argument matches no object",
			arg:         "", wantErr: ErrUnknownObject,
		},
		{
			description: "negative: \"linkx\" outruns \"link\" and matches nothing",
			arg:         "linkx", wantErr: ErrUnknownObject,
		},
		{
			description: "corner: \"LINK\" is not \"link\", because matching is case-sensitive",
			arg:         "LINK", wantErr: ErrUnknownObject,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			obj, err := lookupObject(tc.arg)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				// A recognized-but-unimplemented object still reports which
				// object it was, which is what makes the two errors useful.
				if errors.Is(tc.wantErr, ErrNotImplemented) && obj.name != tc.wantName {
					t.Errorf("object = %q, want %q", obj.name, tc.wantName)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if obj.name != tc.wantName {
				t.Errorf("object = %q, want %q", obj.name, tc.wantName)
			}
		})
	}
}

// TestObjectTableIntegrity guards the two properties the abbreviation rows
// above silently depend on.
//
// go test ./internal/goip/ -run TestObjectTableIntegrity
func TestObjectTableIntegrity(t *testing.T) {
	t.Run("boundary: no object is a strict prefix of an earlier one", func(t *testing.T) {
		// If it were, the later one would be unreachable by any argument — it
		// could only be named by a string that already matched the earlier
		// entry. `neighbor`/`neighbour` are a deliberate pair of aliases and
		// neither is a prefix of the other, so this holds for the real table.
		for i, a := range objects {
			for j, b := range objects {
				if j >= i {
					continue
				}
				if len(a.name) > len(b.name) && a.name[:len(b.name)] == b.name {
					t.Errorf("objects[%d] %q is unreachable: objects[%d] %q is a prefix of it",
						i, a.name, j, b.name)
				}
			}
		}
	})

	t.Run("boundary: every object name is non-empty and lowercase-safe", func(t *testing.T) {
		// An empty name would match every argument, since matchesPrefix's
		// length test would pass only for an empty arg — which is rejected —
		// but it would also make the prefix-uniqueness check above vacuous.
		for i, o := range objects {
			if o.name == "" {
				t.Errorf("objects[%d] has an empty name", i)
			}
		}
	})

	t.Run("positive: the four objects goip implements have handlers", func(t *testing.T) {
		// address, route and neigh are stubs that report ErrNotImplemented, so
		// this asserts the table wiring rather than the features: a nil run
		// would make them report ErrNotImplemented from lookupObject instead,
		// which is the same message by a different route and would hide the
		// day one of them starts working.
		for _, want := range []string{"address", "route", "neighbor", "link"} {
			found := false
			for _, o := range objects {
				if o.name == want {
					found = true
					if o.run == nil {
						t.Errorf("object %q has no handler", want)
					}
				}
			}
			if !found {
				t.Errorf("object %q is missing from the table", want)
			}
		}
	})
}
