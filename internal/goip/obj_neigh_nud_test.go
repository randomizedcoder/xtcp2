package goip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestNudStateA2n pins nud_state_a2n (ip/ipneigh.c:61-88): which words
// abbreviate, which are exact, and the numeric arm's single-bit test.
//
// go test ./internal/goip/ -run TestNudStateA2n
func TestNudStateA2n(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    uint32
		expectedOK  bool
	}{
		{description: "positive: permanent", arg: "permanent", expected: unix.NUD_PERMANENT, expectedOK: true},
		{description: "positive: reachable", arg: "reachable", expected: unix.NUD_REACHABLE, expectedOK: true},
		{description: "positive: noarp", arg: "noarp", expected: unix.NUD_NOARP, expectedOK: true},
		{description: "positive: none is NUD_NONE, 0", arg: "none", expected: unix.NUD_NONE, expectedOK: true},
		{description: "positive: stale", arg: "stale", expected: unix.NUD_STALE, expectedOK: true},
		{description: "positive: incomplete", arg: "incomplete", expected: unix.NUD_INCOMPLETE, expectedOK: true},
		{description: "positive: delay", arg: "delay", expected: unix.NUD_DELAY, expectedOK: true},
		{description: "positive: probe", arg: "probe", expected: unix.NUD_PROBE, expectedOK: true},
		{description: "positive: failed", arg: "failed", expected: unix.NUD_FAILED, expectedOK: true},
		{
			// matches() at :63, tested before the exact `probe` at :77.
			description: "corner: `p` abbreviates permanent, not probe",
			arg:         "p", expected: unix.NUD_PERMANENT, expectedOK: true,
		},
		{description: "positive: `re` abbreviates reachable", arg: "re", expected: unix.NUD_REACHABLE, expectedOK: true},
		{description: "positive: `f` abbreviates failed", arg: "f", expected: unix.NUD_FAILED, expectedOK: true},
		{description: "negative: `stal` does not abbreviate stale (strcmp)", arg: "stal"},
		{description: "negative: `noar` does not abbreviate noarp (strcmp)", arg: "noar"},
		{description: "negative: `del` does not abbreviate delay (strcmp)", arg: "del"},
		{description: "negative: a word longer than permanent", arg: "permanentx"},
		{
			// `all` is do_show_or_flush's, not nud_state_a2n's (:561-564).
			description: "negative: `all` is not a nud_state_a2n word",
			arg:         "all",
		},
		{description: "negative: the empty string", arg: ""},
		{description: "positive: a single NUD bit in decimal", arg: "4", expected: unix.NUD_STALE, expectedOK: true},
		{description: "positive: a single NUD bit in hex", arg: "0x40", expected: unix.NUD_NOARP, expectedOK: true},
		{description: "positive: a single NUD bit in octal (base 0)", arg: "020", expected: unix.NUD_PROBE, expectedOK: true},
		{description: "boundary: 0 is accepted, as NUD_NONE", arg: "0", expected: 0, expectedOK: true},
		{description: "boundary: 0x80 (PERMANENT), the highest single bit below 0x100", arg: "0x80", expected: unix.NUD_PERMANENT, expectedOK: true},
		{description: "boundary: 0x100 is refused (>= 0x100)", arg: "0x100"},
		{description: "negative: two bits are refused", arg: "3"},
		{description: "negative: 0xff is refused", arg: "0xff"},
		{description: "negative: -1 wraps above UINT_MAX and is refused", arg: "-1"},
		{description: "negative: 08 is not octal", arg: "08"},
		{
			// strtoul skips leading whitespace and takes a sign.
			description: "corner: a leading space and a plus sign are strtoul's",
			arg:         " +2", expected: unix.NUD_REACHABLE, expectedOK: true,
		},
		{description: "negative: trailing junk", arg: "2x"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := nudStateA2n(tc.arg)
			if ok != tc.expectedOK || got != tc.expected {
				t.Errorf("nudStateA2n(%q) = %#x, %v; expected %#x, %v", tc.arg, got, ok, tc.expected, tc.expectedOK)
			}
		})
	}
}

// TestParseNeighShowNud pins filter.state as do_show_or_flush leaves it
// (ip/ipneigh.c:523, :553-570), and nud's composition with dev and proxy.
//
// go test ./internal/goip/ -run TestParseNeighShowNud
func TestParseNeighShowNud(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    neighSelectors
		expectedErr string
	}{
		{
			description: "positive: no nud keeps the default mask",
			args:        nil,
			expected:    neighSelectors{State: neighShowStateMaskCst},
		},
		{
			description: "positive: the first nud replaces the default",
			args:        []string{"nud", "stale"},
			expected:    neighSelectors{State: unix.NUD_STALE, StateGiven: true},
		},
		{
			description: "positive: a second nud ors into the first, not into the default",
			args:        []string{"nud", "stale", "nud", "failed"},
			expected:    neighSelectors{State: unix.NUD_STALE | unix.NUD_FAILED, StateGiven: true},
		},
		{
			description: "positive: nud noarp selects the rows the default hides",
			args:        []string{"nud", "noarp"},
			expected:    neighSelectors{State: unix.NUD_NOARP, StateGiven: true},
		},
		{
			description: "corner: nud none is the 0x100 sentinel",
			args:        []string{"nud", "none"},
			expected:    neighSelectors{State: nudNoneSentinelCst, StateGiven: true},
		},
		{
			description: "corner: nud 0 is the sentinel too",
			args:        []string{"nud", "0"},
			expected:    neighSelectors{State: nudNoneSentinelCst, StateGiven: true},
		},
		{
			description: "boundary: nud all is every bit, the sentinel included",
			args:        []string{"nud", "all"},
			expected:    neighSelectors{State: ^uint32(0), StateGiven: true},
		},
		{
			description: "corner: nud composes with dev and proxy in any order",
			args:        []string{"proxy", "nud", "reachable", "dev", "goip0"},
			expected: neighSelectors{
				Dev: "goip0", DevSet: true, NdmFlags: unix.NTF_PROXY,
				State: unix.NUD_REACHABLE, StateGiven: true,
			},
		},
		{
			description: "boundary: a trailing nud is missing its value",
			args:        []string{"nud"},
			expectedErr: `argument "nud" is missing its value`,
		},
		{
			description: "negative: a bad state is invarg's message",
			args:        []string{"nud", "bogus"},
			expectedErr: `argument "bogus" is wrong: nud state is bad`,
		},
		{
			description: "negative: a bad state after a good one still fails",
			args:        []string{"nud", "stale", "nud", "3"},
			expectedErr: `argument "3" is wrong: nud state is bad`,
		},
		{
			description: "negative: `nu` does not abbreviate nud (strcmp)",
			args:        []string{"nu", "stale"},
			expectedErr: "not implemented",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseNeighShowArgs(tc.args)
			if tc.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expectedErr) {
					t.Fatalf("err = %v, expected to contain %q", err, tc.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("selectors = %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

// TestNeighStateFiltered is print_neigh's four-clause skip (ip/ipneigh.c:
// 335-338) on hand-built replies, including the state-0 non-proxy entry no
// committed capture carries.
//
// go test ./internal/goip/ -run TestNeighStateFiltered
func TestNeighStateFiltered(t *testing.T) {
	tests := []struct {
		description string
		n           xtcpnl.NeighInfo
		state       uint32
		expected    bool
	}{
		{
			description: "positive: a state in the mask is kept",
			n:           xtcpnl.NeighInfo{State: unix.NUD_STALE}, state: neighShowStateMaskCst,
		},
		{
			description: "negative: NOARP is dropped by the default mask",
			n:           xtcpnl.NeighInfo{State: unix.NUD_NOARP}, state: neighShowStateMaskCst,
			expected: true,
		},
		{
			description: "positive: NOARP is kept by nud noarp",
			n:           xtcpnl.NeighInfo{State: unix.NUD_NOARP}, state: unix.NUD_NOARP,
		},
		{
			description: "corner: a proxy entry escapes any mask",
			n:           xtcpnl.NeighInfo{Flags: unix.NTF_PROXY}, state: unix.NUD_STALE,
		},
		{
			description: "corner: an extern_learn entry escapes any mask",
			n:           xtcpnl.NeighInfo{State: unix.NUD_PERMANENT, Flags: unix.NTF_EXT_LEARNED}, state: unix.NUD_STALE,
		},
		{
			description: "negative: a state-0 entry is dropped by the default mask",
			n:           xtcpnl.NeighInfo{}, state: neighShowStateMaskCst,
			expected: true,
		},
		{
			description: "positive: a state-0 entry is kept by nud none (the fourth clause)",
			n:           xtcpnl.NeighInfo{}, state: nudNoneSentinelCst,
		},
		{
			description: "boundary: a state-0 entry is kept by nud all",
			n:           xtcpnl.NeighInfo{}, state: ^uint32(0),
		},
		{
			description: "negative: the sentinel keeps no entry that has a state",
			n:           xtcpnl.NeighInfo{State: unix.NUD_STALE}, state: nudNoneSentinelCst,
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := neighStateFiltered(tc.n, tc.state); got != tc.expected {
				t.Errorf("neighStateFiltered(%+v, %#x) = %v, expected %v", tc.n, tc.state, got, tc.expected)
			}
		})
	}
}

// TestNeighShowNud replays the bare and proxy captures through Run with nud
// selectors. The bare capture holds PERMANENT, STALE, INCOMPLETE and NOARP
// entries (two of the PERMANENT ones extern_learn), so each row is a real
// subset; nud sends nothing new, so replaying a bare capture is the whole
// request, not an approximation of it.
//
// go test ./internal/goip/ -run TestNeighShowNud
func TestNeighShowNud(t *testing.T) {
	const (
		p50   = "192.0.2.50 dev goip0"
		s51   = "192.0.2.51 dev goip0"
		i52   = "192.0.2.52 dev goip0"
		p53   = "192.0.2.53 dev goip0"
		x54   = "192.0.2.54 dev goip0" // extern_learn
		p55   = "192.0.2.55 dev goip0"
		x56   = "192.0.2.56 dev goip0" // extern_learn
		v6p50 = "2001:db8::50 dev goip0"
		v6p53 = "2001:db8::53 dev goip0"
		n2    = "ff02::2 dev goip0"
		n16   = "ff02::16 dev goip0"
		n2v   = "ff02::2 dev goipv"
	)
	tests := []struct {
		description string
		args        []string
		pcap        string
		portid      string
		expected    []string
		expectedErr string
		expectedRC  int
	}{
		{
			description: "positive: nud stale is the stale entry and the extern_learn escapes",
			args:        []string{"neigh", "show", "nud", "stale"},
			expected:    []string{s51, x54, x56},
		},
		{
			description: "positive: nud noarp shows the multicast rows the default hides",
			args:        []string{"neigh", "show", "nud", "noarp"},
			expected:    []string{x54, x56, n2, n16, n2v},
		},
		{
			description: "positive: nud p (permanent) abbreviates",
			args:        []string{"n", "s", "nud", "p"},
			expected:    []string{p50, p53, x54, p55, x56, v6p50, v6p53},
		},
		{
			description: "positive: two nud keywords or together",
			args:        []string{"neigh", "show", "nud", "stale", "nud", "incomplete"},
			expected:    []string{s51, i52, x54, x56},
		},
		{
			description: "boundary: nud all is every entry, NOARP included",
			args:        []string{"neigh", "show", "nud", "all"},
			expected:    []string{p50, s51, i52, p53, x54, p55, x56, v6p50, v6p53, n2, n16, n2v},
		},
		{
			description: "corner: nud none matches no stateful entry; the escapes remain",
			args:        []string{"neigh", "show", "nud", "none"},
			expected:    []string{x54, x56},
		},
		{
			description: "corner: nud with dev goip0 drops the other device's NOARP row",
			args:        []string{"neigh", "show", "nud", "noarp", "dev", "goip0"},
			expected:    []string{"192.0.2.54 lladdr", "192.0.2.56 lladdr", "ff02::2 lladdr", "ff02::16 lladdr"},
		},
		{
			description: "corner: proxy entries are state 0 and escape nud stale",
			args:        []string{"neigh", "show", "proxy", "nud", "stale"},
			pcap:        neighProxyPcap,
			expected:    []string{"192.0.2.60 dev goip0", "2001:db8::60 dev goip0"},
		},
		{
			description: "negative: a bad state exits ExitUsage with invarg's message",
			args:        []string{"neigh", "show", "nud", "sleepy"},
			expectedRC:  ExitUsage,
			expectedErr: "nud state is bad",
		},
		{
			description: "boundary: a trailing nud exits ExitUsage",
			args:        []string{"neigh", "show", "nud"},
			expectedRC:  ExitUsage,
			expectedErr: "missing its value",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap, portid := neighDumpPcap, neighDumpPortid
			if tc.pcap != "" {
				pcap, portid = tc.pcap, ""
			}
			t.Setenv("GOIP_REPLAY", pcap)
			t.Setenv("GOIP_REPLAY_PORTID", portid)
			var stdout, stderr bytes.Buffer
			rc := Run(tc.args, &stdout, &stderr)
			if rc != tc.expectedRC {
				t.Fatalf("Run(%q) = %d, expected %d; stderr: %s", tc.args, rc, tc.expectedRC, stderr.String())
			}
			if tc.expectedErr != "" {
				if !strings.Contains(stderr.String(), tc.expectedErr) {
					t.Errorf("stderr = %q, expected to contain %q", stderr.String(), tc.expectedErr)
				}
				return
			}
			lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
			if len(lines) != len(tc.expected) {
				t.Fatalf("%d lines, expected %d:\n%s", len(lines), len(tc.expected), stdout.String())
			}
			for i, want := range tc.expected {
				if !strings.HasPrefix(lines[i], want+" ") {
					t.Errorf("line %d = %q, expected prefix %q", i, lines[i], want)
				}
			}
		})
	}
}
