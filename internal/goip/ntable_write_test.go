package goip

import (
	"bytes"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runNTableCtx runs `ip ntable` args on c, as runNexthopCtx runs `ip
// nexthop`.
func runNTableCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runNTable(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// TestNTableChangeMatchesIp replays the three C1 ntable change captures:
// goip sends ll_init_map's link dump, then writes ip's RTM_SETNEIGHTBL, byte
// for byte with seq and pid zeroed, flags 0x105, and prints nothing. The
// kernel's ENOENT for a missing table and for the thresholds outside the
// initial namespace is ip's, not goip's to know.
//
// go test ./internal/goip/ -run TestNTableChangeMatchesIp
func TestNTableChangeMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string
	}{
		{"positive: a device's queue, NDTPA_IFINDEX out of the dump's cache", "ntable_change",
			"change name arp_cache dev d0 queue 64"},
		{"negative: a table the kernel lacks is the same request", "ntable_change_missing",
			"change name nosuch_cache queue 64"},
		{"negative: thresh1, refused outside the initial namespace, the same request",
			"ntable_change_thresh_netns", "change name arp_cache thresh1 64"},
		{"positive: `c` abbreviates change", "ntable_change", "c name arp_cache dev d0 queue 64"},
		{"positive: chg", "ntable_change", "chg name arp_cache dev d0 queue 64"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ipReqs := rtnlRequests(t, tc.slug)
			if len(ipReqs) != 2 {
				t.Fatalf("capture holds %d requests, want 2", len(ipReqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newRuleReplay(t, tc.slug)
			rc, stdout, stderr := runNTableCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if rc != 0 || stdout != "" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if len(src.dumpReqs) != 1 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) || len(src.gets) != 0 {
				t.Fatalf("goip sent dumps %x and %d gets, want %x alone", src.dumpReqs, len(src.gets), ipReqs[0])
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got := capturedRequest(writes[0]); !bytes.Equal(got, ipReqs[1]) {
				t.Errorf("write:\n got %x\nwant %x", got, ipReqs[1])
			}
			if writes[0].Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_REPLACE|unix.NLM_F_ACK {
				t.Errorf("flags %#x, want 0x105", writes[0].Hdr.Flags)
			}
		})
	}
}

// TestParseNTableWriteArgs is ipntable_modify's loop and checks, with d0
// resolving to 3.
//
// go test ./internal/goip/ -run TestParseNTableWriteArgs
func TestParseNTableWriteArgs(t *testing.T) {
	index := func(name string) int32 {
		if name == "d0" {
			return 3
		}
		return 0
	}
	name := "0e0001006172705f6361636865000000" // NDTA_NAME "arp_cache"
	type result struct {
		attrs string // hex
		err   error
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: a u32 parameter goes in NDTA_PARMS", "name arp_cache queue 64",
			result{attrs: name + "0c000600" + "0800080040000000"}},
		{"positive: dev and a parameter, in argument order", "name arp_cache dev d0 queue 64",
			result{attrs: name + "14000600" + "0800010003000000" + "0800080040000000"}},
		{"positive: thresh1 is top-level, no NDTA_PARMS", "name arp_cache thresh1 64",
			result{attrs: name + "0800020040000000"}},
		{"positive: gc_int is a top-level u64", "name arp_cache gc_int 30000",
			result{attrs: name + "0c000800" + "3075000000000000"}},
		{"positive: a u64 parameter, base 0", "name arp_cache locktime 0x10",
			result{attrs: name + "10000600" + "0c000f00" + "1000000000000000"}},
		{"positive: top-level ahead of NDTA_PARMS whatever the order", "name arp_cache queue 1 thresh3 2",
			result{attrs: name + "0800040002000000" + "0c000600" + "0800080001000000"}},
		{"boundary: a u64 at its maximum", "name arp_cache retrans 18446744073709551615",
			result{attrs: name + "10000600" + "0c000500" + "ffffffffffffffff"}},
		{"corner: dev alone changes nothing", "name arp_cache dev d0", result{err: ErrInvalidArgument}},
		{"negative: no name", "queue 64", result{err: ErrInvalidArgument}},
		{"negative: a name and nothing to change", "name arp_cache", result{err: ErrInvalidArgument}},
		{"negative: two names", "name arp_cache name x queue 1", result{err: ErrInvalidArgument}},
		{"negative: an unknown keyword", "name arp_cache bogus 1", result{err: ErrInvalidArgument}},
		{"negative: an unknown keyword ahead of a valid change", "name arp_cache bogus queue 1",
			result{err: ErrInvalidArgument}},
		{"negative: a u32 past 32 bits", "name arp_cache queue 4294967296", result{err: ErrInvalidArgument}},
		{"negative: a u64 that is no number", "name arp_cache gc_stale x", result{err: ErrInvalidArgument}},
		{"negative: an unknown device", "name arp_cache dev nope queue 1", result{err: ErrInvalidArgument}},
		{"negative: a keyword with no value", "name arp_cache queue", result{err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseNTableWriteArgs(0, index, strings.Fields(tc.args))
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			attrs, err := w.attrs()
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(attrs); got != tc.expected.attrs {
				t.Errorf("attrs\n got %s\nwant %s", got, tc.expected.attrs)
			}
		})
	}
}

// TestNTableChange is the order and refusals around the request: the link
// dump goes first, as ll_init_map runs before the verb is read.
//
// go test ./internal/goip/ -run TestNTableChange$
func TestNTableChange(t *testing.T) {
	type result struct {
		rc, dumps int
		written   bool
	}
	tests := []struct {
		description string
		noHook      bool
		family      uint8
		args        string
		expected    result
	}{
		{description: "positive: -4 is the header's family", family: unix.AF_INET,
			args: "change name arp_cache queue 1", expected: result{dumps: 1, written: true}},
		{description: "negative: no GOIP_WRITE_OUT, after the dump", noHook: true,
			args: "change name arp_cache queue 1", expected: result{rc: ExitUsage, dumps: 1}},
		{description: "negative: a bad argument is refused after the dump",
			args: "change name arp_cache bogus", expected: result{rc: ExitUsage, dumps: 1}},
		{description: "corner: an unknown device is refused out of the cache, no get",
			args: "change name arp_cache dev nope queue 1", expected: result{rc: ExitUsage, dumps: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := newRuleReplay(t, "ntable_change")
			rc, _, stderr := runNTableCtx(&runCtx{src: src, family: tc.family}, strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(src.dumpReqs) + len(src.gets), written: writtenFile(out)}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v; stderr %q", got, tc.expected, stderr)
			}
			if got.written {
				if w := restoreWrites(t, out); w[0].Body[0] != tc.family {
					t.Errorf("family %d, want %d", w[0].Body[0], tc.family)
				}
			}
		})
	}
}

// TestNTableWriteHeader is struct ndtmsg: the family and three pad bytes.
//
// go test ./internal/goip/ -run TestNTableWriteHeader
func TestNTableWriteHeader(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		expected    string
	}{
		{"positive: AF_UNSPEC", unix.AF_UNSPEC, "00000000"},
		{"positive: AF_INET6", unix.AF_INET6, "0a000000"},
		{"boundary: the header is NdtMsgSizeCst long", unix.AF_INET, "02000000"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w := &ntableWrite{family: tc.family}
			got := w.ndtmsg()
			if hex.EncodeToString(got) != tc.expected || len(got) != xtcpnl.NdtMsgSizeCst {
				t.Errorf("got %x, want %s", got, tc.expected)
			}
		})
	}
}
