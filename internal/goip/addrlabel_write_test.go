package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runAddrLabelCtx runs `ip addrlabel` args on c, as runNexthopCtx runs `ip
// nexthop`.
func runAddrLabelCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runAddrLabel(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// TestAddrLabelModifyMatchesIp replays the C1 addrlabel add and del
// captures: goip sends nothing ahead of the write, as neither names a
// device, and writes ip's request, byte for byte with seq and pid zeroed,
// and exits 0 with nothing printed.
//
// go test ./internal/goip/ -run TestAddrLabelModifyMatchesIp
func TestAddrLabelModifyMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		args        string
		expected    uint16 // the write's flags
	}{
		{"positive: add is RTM_NEWADDRLABEL, CREATE|EXCL and acked", "addrlabel_add",
			"add prefix 2001:db8::/32 label 99", 0x605},
		{"positive: del is RTM_DELADDRLABEL, acked", "addrlabel_del", "del prefix 2001:db8::/32 label 99", 0x5},
		{"corner: an unknown word is skipped, as ip skips it", "addrlabel_add",
			"add bogus prefix 2001:db8::/32 label 99", 0x605},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ipReqs := rtnlRequests(t, tc.slug)
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newRuleReplay(t, tc.slug)
			rc, stdout, stderr := runAddrLabelCtx(&runCtx{src: src}, strings.Fields(tc.args))
			if rc != 0 || stdout != "" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if len(src.gets) != 0 || len(ipReqs) != 1 {
				t.Fatalf("goip sent %d gets, ip %d requests; want 0 and 1", len(src.gets), len(ipReqs))
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got := capturedRequest(writes[0]); !bytes.Equal(got, ipReqs[0]) {
				t.Errorf("write:\n got %x\nwant %x", got, ipReqs[0])
			}
			if writes[0].Hdr.Flags != tc.expected {
				t.Errorf("flags %#x, want %#x", writes[0].Hdr.Flags, tc.expected)
			}
		})
	}
}

// TestParseAddrLabelWriteArgs is ipaddrlabel_modify's loop and checks, with
// d0 resolving to 3.
//
// go test ./internal/goip/ -run TestParseAddrLabelWriteArgs
func TestParseAddrLabelWriteArgs(t *testing.T) {
	resolve := func(name string) (int32, error) {
		if name == "d0" {
			return 3, nil
		}
		return 0, nil
	}
	type result struct {
		hdr   string // the ifaddrlblmsg, hex
		label uint32
		addr  string // IFAL_ADDRESS, hex
		err   error
	}
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    result
	}{
		{"positive: an IPv6 prefix and a label", 0, "prefix 2001:db8::/32 label 99",
			result{hdr: "0a0020000000000000000000", label: 99, addr: "20010db8000000000000000000000000"}},
		{"positive: dev is the ifindex", 0, "prefix 2001:db8::/32 dev d0 label 1",
			result{hdr: "0a0020000300000000000000", label: 1, addr: "20010db8000000000000000000000000"}},
		{"positive: the label's base is 0, so hex reads", 0, "label 0x10 prefix ::/0",
			result{hdr: "0a0000000000000000000000", label: 16, addr: "00000000000000000000000000000000"}},
		{"boundary: a prefix with no length is all 128 bits", 0, "prefix 2001:db8::1 label 0",
			result{hdr: "0a0080000000000000000000", label: 0, addr: "20010db8000000000000000000000001"}},
		{"boundary: label 0xfffffffe, one short of ip's unset label", 0, "prefix ::/0 label 0xfffffffe",
			result{hdr: "0a0000000000000000000000", label: 0xfffffffe, addr: "00000000000000000000000000000000"}},
		{"corner: an IPv4 prefix keeps the header AF_INET6, as ip sends it", 0, "prefix 192.0.2.0/24 label 5",
			result{hdr: "0a0018000000000000000000", label: 5, addr: "c0000200"}},
		{"corner: -4 makes the header AF_INET", unix.AF_INET, "prefix 192.0.2.0/24 label 5",
			result{hdr: "020018000000000000000000", label: 5, addr: "c0000200"}},
		{"corner: the last prefix wins", 0, "prefix 2001:db8::/32 prefix 2001:db9::/48 label 1",
			result{hdr: "0a0030000000000000000000", label: 1, addr: "20010db9000000000000000000000000"}},
		{"negative: no prefix", 0, "label 1", result{err: ErrInvalidArgument}},
		{"negative: no label", 0, "prefix 2001:db8::/32", result{err: ErrInvalidArgument}},
		{"negative: no arguments", 0, "", result{err: ErrInvalidArgument}},
		{"negative: label 0xffffffff is ip's unset label", 0, "prefix ::/0 label 0xffffffff",
			result{err: ErrInvalidArgument}},
		{"negative: a label past 32 bits", 0, "prefix ::/0 label 4294967296", result{err: ErrInvalidArgument}},
		{"negative: a label that is no number", 0, "prefix ::/0 label x", result{err: ErrInvalidArgument}},
		{"negative: an unknown device", 0, "prefix ::/0 dev nope label 1", result{err: ErrInvalidArgument}},
		{"negative: a bad prefix", 0, "prefix nope label 1", result{err: ErrInvalidArgument}},
		{"negative: -6 and an IPv4 prefix", unix.AF_INET6, "prefix 192.0.2.0/24 label 1",
			result{err: ErrInvalidArgument}},
		{"negative: a keyword with no value", 0, "prefix ::/0 label", result{err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseAddrLabelWriteArgs(tc.family, resolve, strings.Fields(tc.args))
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			got := result{hdr: hex.EncodeToString(w.hdr()), label: w.label, addr: hex.EncodeToString(w.prefix.data)}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRunAddrLabelVerbs is do_ipaddrlabel's matches() chain over the write
// verbs, with no GOIP_WRITE_OUT so a verb that gets as far as its write
// says so.
//
// go test ./internal/goip/ -run TestRunAddrLabelVerbs
func TestRunAddrLabelVerbs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // a stderr substring
	}{
		{"positive: `a` is add", "a prefix ::/0 label 1", "never applies a write"},
		{"positive: `d` is delete", "d prefix ::/0 label 1", "never applies a write"},
		{"positive: `del` is delete", "del prefix ::/0 label 1", "never applies a write"},
		{"boundary: delete spelled out", "delete prefix ::/0 label 1", "never applies a write"},
		{"negative: `adds` is no verb", "adds prefix ::/0 label 1", "not implemented"},
		{"negative: flush's extra argument is ip's refusal", "flush dev d0", "does not allow extra arguments"},
		{"corner: `f` is flush, not a show selector", "f x", "does not allow extra arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv(writeOutEnvCst, "")
			rc, _, stderr := runAddrLabelCtx(&runCtx{src: newRuleReplay(t, "addrlabel_add")}, strings.Fields(tc.args))
			if rc != ExitUsage || !strings.Contains(stderr, tc.expected) {
				t.Errorf("rc %d, stderr %q; want %d and %q", rc, stderr, ExitUsage, tc.expected)
			}
		})
	}
}

// TestAddrLabelFlushMatchesIp replays addrlabel_flush: goip sends ip's dump
// and writes ip's eleven RTM_DELADDRLABEL copies, the kernel's ten default
// labels and the one the Setup added, byte for byte with seq and pid
// zeroed, flags 0x5, and prints nothing.
//
// go test ./internal/goip/ -run TestAddrLabelFlushMatchesIp
func TestAddrLabelFlushMatchesIp(t *testing.T) {
	ipReqs := rtnlRequests(t, "addrlabel_flush")
	if len(ipReqs) != 12 {
		t.Fatalf("capture holds %d requests, want 12", len(ipReqs))
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newRuleReplay(t, "addrlabel_flush")
	rc, stdout, stderr := runAddrLabelCtx(&runCtx{src: src}, []string{"flush"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	if len(src.dumpReqs) != 1 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
		t.Fatalf("goip sent dumps %x, want %x", src.dumpReqs, ipReqs[0])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 11 {
		t.Fatalf("goip wrote %d messages, want 11", len(writes))
	}
	for i, w := range writes {
		if got := capturedRequest(w); !bytes.Equal(got, ipReqs[1+i]) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, ipReqs[1+i])
		}
		if w.Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
			t.Errorf("write %d flags %#x, want 0x5", i, w.Hdr.Flags)
		}
	}
}

// TestAddrLabelFlush is the flush's family and refusals over
// addrlabel_flush's capture.
//
// go test ./internal/goip/ -run TestAddrLabelFlush$
func TestAddrLabelFlush(t *testing.T) {
	type result struct {
		rc, dumps, writes int
		stderrs           string
	}
	tests := []struct {
		description string
		family      uint8
		noHook      bool
		failDump    bool
		json        bool
		args        string
		expected    result
	}{
		{description: "positive: `f` abbreviates flush", args: "f", expected: result{dumps: 1, writes: 11}},
		{description: "positive: -j changes nothing, as ip's flush prints nothing",
			json: true, args: "flush", expected: result{dumps: 1, writes: 11}},
		{description: "corner: -6 is the default's family", family: unix.AF_INET6,
			args: "flush", expected: result{dumps: 1, writes: 11}},
		{description: "negative: labels to delete with no GOIP_WRITE_OUT, after the dump",
			noHook: true, args: "flush", expected: result{rc: ExitUsage, dumps: 1, stderrs: "never applies a write"}},
		{description: "negative: a failed dump is Flush terminated, exit 255",
			failDump: true, args: "flush", expected: result{rc: ExitMinusOne, dumps: 1, stderrs: "Flush terminated"}},
		{description: "negative: an argument is refused before any request",
			args: "flush all", expected: result{rc: ExitUsage, stderrs: "does not allow extra arguments"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := newRuleReplay(t, "addrlabel_flush")
			src.failRules = tc.failDump
			rc, _, stderr := runAddrLabelCtx(&runCtx{src: src, family: tc.family, json: tc.json}, strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(src.dumpReqs)}
			if writtenFile(out) {
				got.writes = len(restoreWrites(t, out))
			}
			if tc.expected.stderrs != "" && strings.Contains(stderr, tc.expected.stderrs) {
				got.stderrs = tc.expected.stderrs
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}

// TestAddrLabelFlushCopy is flush_addrlabel on one reply, hand-built.
//
// go test ./internal/goip/ -run TestAddrLabelFlushCopy
func TestAddrLabelFlushCopy(t *testing.T) {
	reply := func(attrs ...[]byte) []byte {
		body := append([]byte{unix.AF_INET6, 0, 32, 0, 0, 0, 0, 0, 11, 0, 0, 0}, bytes.Join(attrs, nil)...)
		h := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(body))
		binary.LittleEndian.PutUint32(h[0:4], uint32(len(h)+len(body)))
		binary.LittleEndian.PutUint16(h[4:6], unix.RTM_NEWADDRLABEL)
		binary.LittleEndian.PutUint16(h[6:8], unix.NLM_F_MULTI)
		binary.LittleEndian.PutUint32(h[8:12], 42)
		binary.LittleEndian.PutUint32(h[12:16], 99)
		return append(h, body...)
	}
	addr := fraOf(unix.IFAL_ADDRESS, 0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	label := fraOf(unix.IFAL_LABEL, 99, 0, 0, 0)
	tests := []struct {
		description string
		msg         []byte
		expected    string // "copy", "keep" or "error"
	}{
		{"positive: a label with an address is copied", reply(label, addr), "copy"},
		{"negative: a label with no address is kept", reply(label), "keep"},
		{"boundary: a header alone is kept", reply(), "keep"},
		{"boundary: a body short of ifaddrlblmsg ends the dump", reply()[:xtcpnl.NlMsgHdrSizeCst+11], "error"},
		{"corner: an undecodable attribute is refused", append(reply(addr), 9, 0, 6, 0), "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			del, ok, err := addrLabelFlushCopy(tc.msg)
			got := "keep"
			switch {
			case err != nil:
				got = "error"
			case ok:
				got = "copy"
				want := append([]byte(nil), tc.msg...)
				binary.LittleEndian.PutUint16(want[4:6], unix.RTM_DELADDRLABEL)
				binary.LittleEndian.PutUint16(want[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
				if !bytes.Equal(del, want) {
					t.Errorf("copy:\n got %x\nwant %x", del, want)
				}
			}
			if got != tc.expected {
				t.Errorf("got %s, want %s (err %v)", got, tc.expected, err)
			}
		})
	}
}
