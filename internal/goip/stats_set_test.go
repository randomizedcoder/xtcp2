package goip

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// runStatsCtx runs `ip stats` args on c, as runNTableCtx runs `ip ntable`.
func runStatsCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runStats(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// TestStatsSetMatchesIp replays stats_set_l3: goip sends ip's by-name link
// get, then writes ip's RTM_SETSTATS, byte for byte with seq and pid
// zeroed, flags 0x5, and prints nothing.
//
// go test ./internal/goip/ -run TestStatsSetMatchesIp
func TestStatsSetMatchesIp(t *testing.T) {
	ipReqs := rtnlRequests(t, "stats_set_l3")
	if len(ipReqs) != 2 {
		t.Fatalf("capture holds %d requests, want 2", len(ipReqs))
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newRuleReplay(t, "stats_set_l3")
	rc, stdout, stderr := runStatsCtx(&runCtx{src: src}, strings.Fields("set dev d0 l3_stats on"))
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	if len(src.gets) != 1 || !bytes.Equal(src.gets[0], ipReqs[0]) || len(src.dumpReqs) != 0 {
		t.Fatalf("goip sent gets %x and %d dumps, want %x alone", src.gets, len(src.dumpReqs), ipReqs[0])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 1 {
		t.Fatalf("goip wrote %d messages, want 1", len(writes))
	}
	if got := capturedRequest(writes[0]); !bytes.Equal(got, ipReqs[1]) {
		t.Errorf("write:\n got %x\nwant %x", got, ipReqs[1])
	}
	if writes[0].Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
		t.Errorf("flags %#x, want 0x5", writes[0].Hdr.Flags)
	}
}

// TestParseStatsSetArgs is ipstats_set's loop and the checks after it.
//
// go test ./internal/goip/ -run TestParseStatsSetArgs
func TestParseStatsSetArgs(t *testing.T) {
	type result struct {
		dev    string
		enable bool
		err    error
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: on", "dev d0 l3_stats on", result{dev: "d0", enable: true}},
		{"positive: off", "dev d0 l3_stats off", result{dev: "d0"}},
		{"positive: the suite ahead of dev", "l3_stats on dev d0", result{dev: "d0", enable: true}},
		{"boundary: a 15-byte name, IFNAMSIZ less one", "dev abcdefghijklmno l3_stats on",
			result{dev: "abcdefghijklmno", enable: true}},
		{"negative: a 16-byte name", "dev abcdefghijklmnop l3_stats on", result{err: ErrInvalidArgument}},
		{"negative: no dev", "l3_stats on", result{err: ErrInvalidArgument}},
		{"negative: no suite", "dev d0", result{err: ErrInvalidArgument}},
		{"negative: no arguments", "", result{err: ErrInvalidArgument}},
		{"negative: a value neither on nor off", "dev d0 l3_stats 1", result{err: ErrInvalidArgument}},
		{"negative: the suite twice", "dev d0 l3_stats on l3_stats off", result{err: ErrInvalidArgument}},
		{"negative: dev twice", "dev d0 dev d1 l3_stats on", result{err: ErrInvalidArgument}},
		{"negative: a name with a slash", "dev a/b l3_stats on", result{err: ErrInvalidArgument}},
		{"negative: an unknown word", "dev d0 bogus", result{err: ErrInvalidArgument}},
		{"negative: an unknown word ahead of the suite", "dev d0 bogus l3_stats on", result{err: ErrInvalidArgument}},
		{"negative: a keyword with no value", "dev d0 l3_stats", result{err: ErrInvalidArgument}},
		{"corner: help is not implemented", "dev d0 help", result{err: ErrNotImplemented}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a, err := parseStatsSetArgs(strings.Fields(tc.args))
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			got := result{dev: a.dev, enable: a.enable}
			if got != tc.expected || a.suite != unix.IFLA_STATS_SET_OFFLOAD_XSTATS_L3_STATS {
				t.Errorf("got %+v suite %d, want %+v", got, a.suite, tc.expected)
			}
		})
	}
}

// TestStatsSet is the order and refusals around the request.
//
// go test ./internal/goip/ -run TestStatsSet$
func TestStatsSet(t *testing.T) {
	type result struct {
		rc, gets int
		written  bool
		value    byte
	}
	tests := []struct {
		description string
		noHook      bool
		args        string
		expected    result
	}{
		{description: "positive: off writes 0", args: "set dev d0 l3_stats off",
			expected: result{gets: 1, written: true}},
		{description: "negative: no GOIP_WRITE_OUT, after the get", noHook: true,
			args: "set dev d0 l3_stats on", expected: result{rc: ExitUsage, gets: 1}},
		{description: "negative: a bad argument is refused before the get",
			args: "set dev d0 l3_stats maybe", expected: result{rc: ExitUsage}},
		{description: "corner: `se` is no verb, strcmp", args: "se dev d0 l3_stats on",
			expected: result{rc: ExitUsage}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := newRuleReplay(t, "stats_set_l3")
			rc, _, stderr := runStatsCtx(&runCtx{src: src}, strings.Fields(tc.args))
			got := result{rc: rc, gets: len(src.gets), written: writtenFile(out)}
			if got.written {
				b := restoreWrites(t, out)[0].Body
				got.value = b[len(b)-4]
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}

// TestStatsSetDevice is the device lookup against a Talk source, which,
// unlike a replay, can answer the by-name get with ENODEV: a known device is
// written, an unknown one is ip's "Cannot find device" after its one get.
//
// go test ./internal/goip/ -run TestStatsSetDevice
func TestStatsSetDevice(t *testing.T) {
	type result struct {
		rc      int
		talks   string
		written bool
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{description: "positive: a known device is written", args: "set dev d0 l3_stats on",
			expected: result{talks: "d0", written: true}},
		{description: "negative: ENODEV is no such device, nothing written", args: "set dev nope l3_stats on",
			expected: result{rc: ExitUsage, talks: "nope"}},
		{description: "corner: a refused get falls back to the ifN spelling", args: "set dev if7 l3_stats on",
			expected: result{talks: "if7", written: true}},
		{description: "boundary: if0 is no device", args: "set dev if0 l3_stats on",
			expected: result{rc: ExitUsage, talks: "if0"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := &talkLinks{replies: map[string][]byte{"d0": newLinkBody(3, "d0")}}
			rc, _, stderr := runStatsCtx(&runCtx{src: src},
				strings.Fields(tc.args))
			got := result{rc: rc, talks: strings.Join(src.talks, " "), written: writtenFile(out)}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}
