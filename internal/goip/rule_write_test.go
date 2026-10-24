package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runRuleCtx runs `ip rule` args on c, as runRouteCtx runs `ip route`.
func runRuleCtx(c *runCtx, args []string) (int, string, string) {
	var out, errOut bytes.Buffer
	if c.out == nil {
		c.out = &out
	}
	c.errOut = &errOut
	if c.lltab == nil {
		c.lltab = NewLLTab()
	}
	if err := runRule(c, args); err != nil {
		errOut.WriteString("goip: " + err.Error() + "\n")
		return exitCodeOf(err), out.String(), errOut.String()
	}
	return ExitOK, out.String(), errOut.String()
}

// ruleReplay is a restoreReplay that also answers DumpMsgs, the whole replies
// save and flush read, recording the request as Dump does. failRules fails
// the rule dump.
type ruleReplay struct {
	*restoreReplay
	failRules bool
}

func newRuleReplay(t *testing.T, slug string) *ruleReplay {
	t.Helper()
	return &ruleReplay{restoreReplay: &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+slug+"/ip.pcap")}}
}

func (r *ruleReplay) DumpMsgs(request []byte, msgType uint16) ([][]byte, error) {
	r.dumpReqs = append(r.dumpReqs, zeroSeqPidCopy(request))
	r.dumpSeqs = append(r.dumpSeqs, binary.LittleEndian.Uint32(request[8:12]))
	if r.failRules {
		return nil, errFailSource
	}
	return r.inner.DumpMsgs(request, msgType)
}

// TestRuleModifyMatchesIp replays each C1 rule write capture: goip sends
// nothing, as ip resolves nothing first, and writes ip's one request, byte
// for byte with seq and pid zeroed, and exits 0 with nothing printed. The
// fourteen selector adds are one capture, request i being the i-th add. The
// kernel's EEXIST and ENOENT are ip's, not goip's to know.
//
// go test ./internal/goip/ -run TestRuleModifyMatchesIp
func TestRuleModifyMatchesIp(t *testing.T) {
	const sel = "rule_add_selectors"
	tests := []struct {
		description string
		slug        string
		index       int // the request in the capture
		family      uint8
		args        string
		expected    uint16 // the write's flags
	}{
		{"positive: from, a source prefix", sel, 0, 0, "add pref 101 from 192.0.2.0/24 table 100", 0x605},
		{"positive: to, a destination prefix", sel, 1, 0, "add pref 102 to 198.51.100.0/24 table 100", 0x605},
		{"positive: tos in the header", sel, 2, 0, "add pref 103 tos 0x10 table 100", 0x605},
		{"positive: dscp, its full mask not sent", sel, 3, 0, "add pref 104 dscp 10 table 100", 0x605},
		{"positive: fwmark with a mask", sel, 4, 0, "add pref 105 fwmark 0x7/0xff table 100", 0x605},
		{"positive: iif by name, no lookup", sel, 5, 0, "add pref 106 iif d9 table 100", 0x605},
		{"positive: oif by name, no lookup", sel, 6, 0, "add pref 107 oif d9 table 100", 0x605},
		{"positive: uidrange", sel, 7, 0, "add pref 108 uidrange 1000-2000 table 100", 0x605},
		{"positive: ipproto by name, a port range and a single port", sel, 8, 0,
			"add pref 109 ipproto tcp sport 1000-2000 dport 80 table 100", 0x605},
		{"positive: not and goto, the goto's table left unset", sel, 9, 0,
			"add pref 110 not from 203.0.113.0/24 goto 112", 0x605},
		{"positive: table main and suppress_prefixlength", sel, 10, 0,
			"add pref 111 table main suppress_prefixlength 0", 0x605},
		{"positive: protocol by name", sel, 11, 0, "add pref 112 protocol static table 100", 0x605},
		{"positive: l3mdev sets no table", sel, 12, 0, "add pref 113 l3mdev", 0x605},
		{"positive: -6 flowlabel, its default mask sent", sel, 13, unix.AF_INET6,
			"add pref 114 flowlabel 0x12345 table 100", 0x605},
		{"positive: an add the kernel refuses is the same request", "rule_add_existing", 0, 0,
			"add pref 100 from 192.0.2.0/24 table 100", 0x605},
		{"positive: del is RTM_DELRULE, acked, no table", "rule_del", 0, 0, "del pref 100", 0x5},
		{"positive: a del the kernel refuses is the same request", "rule_del_missing", 0, 0, "del pref 4242", 0x5},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := routeRtnlDir + tc.slug + "/ip.pcap"
			ipReqs := captureRequests(t, pcap)
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			rc, stdout, stderr := runRuleCtx(&runCtx{src: src, family: tc.family}, strings.Fields(tc.args))
			if rc != 0 || stdout != "" || stderr != "" {
				t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
			}
			if src.dumps != 0 || len(src.gets) != 0 {
				t.Fatalf("goip sent %d dumps, %d gets, want none", src.dumps, len(src.gets))
			}
			writes := restoreWrites(t, out)
			if len(writes) != 1 {
				t.Fatalf("goip wrote %d messages, want 1", len(writes))
			}
			if got := wireBytesOf(writes[0]); !bytes.Equal(got, ipReqs[tc.index]) {
				t.Errorf("write:\n got %x\nwant %x", got, ipReqs[tc.index])
			}
			if f := writes[0].Hdr.Flags; f != tc.expected {
				t.Errorf("flags %#x, want %#x", f, tc.expected)
			}
		})
	}
}

// fraOf is one FIB rule attribute.
func fraOf(typ uint16, val ...byte) []byte {
	b := make([]byte, 4, 4+len(val)+3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(4+len(val)))
	binary.LittleEndian.PutUint16(b[2:4], typ)
	b = append(b, val...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// TestParseRuleWriteArgs is iprule_modify's loop and the checks after it, on
// the header and attributes it builds. The refusals' messages and exit codes
// were checked against `ip rule` from iproute2-7.1.0 in an unprivileged
// namespace, where they are refused before the kernel is asked.
//
// go test ./internal/goip/ -run TestParseRuleWriteArgs
func TestParseRuleWriteArgs(t *testing.T) {
	type result struct {
		frh   string // family dst src tos table action flags
		attrs string
		err   error
	}
	frh := func(family, dst, src, tos, table, action uint8, flags uint32) string {
		w := ruleWrite{family: family, dstLen: dst, srcLen: src, tos: tos, table: table, action: action, flags: flags}
		return string(w.frh())
	}
	cat := func(a ...[]byte) string { return string(bytes.Join(a, nil)) }
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	be32 := func(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
	addFrh := frh(unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0)
	tests := []struct {
		description string
		family      uint8
		msgType     uint16
		args        string
		expected    result
	}{
		{"positive: a bare add looks up main", 0, unix.RTM_NEWRULE, "add pref 7",
			result{addFrh, cat(fraOf(unix.FRA_PRIORITY, u32(7)...)), nil}},
		{"positive: `order` and `priority` are preference", 0, unix.RTM_NEWRULE, "add priority 7 order 8",
			result{addFrh, cat(fraOf(unix.FRA_PRIORITY, u32(7)...), fraOf(unix.FRA_PRIORITY, u32(8)...)), nil}},
		{"positive: a del sends no table and no action", 0, unix.RTM_DELRULE, "del pref 7",
			result{frh(unix.AF_INET, 0, 0, 0, 0, 0, 0), cat(fraOf(unix.FRA_PRIORITY, u32(7)...)), nil}},
		{"positive: a table past 255 goes in FRA_TABLE, the header's unspec", 0, unix.RTM_NEWRULE, "add lookup 1000",
			result{frh(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, 0), cat(fraOf(unix.FRA_TABLE, u32(1000)...)), nil}},
		{"positive: `t` abbreviates tun_id, sent big-endian", 0, unix.RTM_NEWRULE, "add t 0x102",
			result{addFrh, cat(fraOf(unix.FRA_TUN_ID, 0, 0, 0, 0, 0, 0, 1, 2)), nil}},
		{"positive: dsfield takes a name from rt_dsfield", 0, unix.RTM_NEWRULE, "add dsfield AF11",
			result{frh(unix.AF_INET, 0, 0, 0x28, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0), "", nil}},
		{"positive: dscp takes a DS field name, shifted to its DSCP", 0, unix.RTM_NEWRULE, "add dscp EF",
			result{addFrh, cat(fraOf(xtcpnl.FraDscp, 46)), nil}},
		{"positive: a dscp mask short of 0x3f is sent", 0, unix.RTM_NEWRULE, "add dscp 10/0x38",
			result{addFrh, cat(fraOf(xtcpnl.FraDscp, 10), fraOf(xtcpnl.FraDscpMask, 0x38)), nil}},
		{"positive: a port mask short of 0xffff is sent", 0, unix.RTM_NEWRULE, "add sport 80/0xff00",
			result{addFrh, cat(fraOf(unix.FRA_SPORT_RANGE, 80, 0, 80, 0), fraOf(xtcpnl.FraSportMask, 0, 0xff)), nil}},
		{"positive: a flowlabel mask is sent as given, big-endian", unix.AF_INET6, unix.RTM_NEWRULE,
			"add flowlabel 1/0xf",
			result{frh(unix.AF_INET6, 0, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				cat(fraOf(xtcpnl.FraFlowlabel, be32(1)...), fraOf(xtcpnl.FraFlowlabelMask, be32(0xf)...)), nil}},
		{"positive: fwmark with no mask sends no FRA_FWMASK", 0, unix.RTM_NEWRULE, "add fwmark 7",
			result{addFrh, cat(fraOf(unix.FRA_FWMARK, u32(7)...)), nil}},
		{"negative: a fwmask past 32 bits", 0, unix.RTM_NEWRULE, "add fwmark 7/0x100000000",
			result{err: ErrInvalidArgument}},
		{"positive: type unreachable is the action, and sets the table", 0, unix.RTM_NEWRULE, "add type unreachable",
			result{frh(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_UNREACHABLE, 0), "", nil}},
		{"positive: nop sets the table", 0, unix.RTM_NEWRULE, "add nop",
			result{frh(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_NOP, 0), "", nil}},
		{"positive: suppress_ifgroup takes `default`", 0, unix.RTM_NEWRULE, "add sup_group default",
			result{addFrh, cat(fraOf(unix.FRA_SUPPRESS_IFGROUP, u32(0)...)), nil}},
		{"boundary: -6 keeps the header's family for an IPv6 prefix", unix.AF_INET6, unix.RTM_DELRULE,
			"del from 2001:db8::/32",
			result{frh(unix.AF_INET6, 0, 32, 0, 0, 0, 0), cat(fraOf(unix.FRA_SRC, 0x20, 0x01, 0x0d, 0xb8,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)), nil}},
		{"boundary: a port range of 0-65535", 0, unix.RTM_NEWRULE, "add dport 0-65535",
			result{addFrh, cat(fraOf(unix.FRA_DPORT_RANGE, 0, 0, 0xff, 0xff)), nil}},
		{"boundary: uidrange wraps -1 to 4294967295, as sscanf %u does", 0, unix.RTM_NEWRULE, "add uidrange 0--1",
			result{addFrh, cat(fraOf(unix.FRA_UID_RANGE, append(u32(0), u32(0xffffffff)...)...)), nil}},
		{"negative: add with no arguments", 0, unix.RTM_NEWRULE, "", result{err: ErrInvalidArgument}},
		{"negative: del with no arguments", 0, unix.RTM_DELRULE, "", result{err: ErrInvalidArgument}},
		{"negative: a preference past 32 bits", 0, unix.RTM_NEWRULE, "add pref 4294967296", result{err: ErrInvalidArgument}},
		{"negative: a tos past 255", 0, unix.RTM_NEWRULE, "add tos 0x100", result{err: ErrInvalidArgument}},
		{"negative: a negative suppress_prefixlength", 0, unix.RTM_NEWRULE, "add suppress_prefixlength -1",
			result{err: ErrInvalidArgument}},
		{"negative: a uidrange with one number", 0, unix.RTM_NEWRULE, "add uidrange 1000", result{err: ErrInvalidArgument}},
		{"negative: a port past 16 bits", 0, unix.RTM_NEWRULE, "add sport 65536", result{err: ErrInvalidArgument}},
		{"negative: an unknown rule type", 0, unix.RTM_NEWRULE, "add bogus", result{err: ErrInvalidArgument}},
		{"negative: a keyword with no value", 0, unix.RTM_NEWRULE, "add pref", result{err: ErrInvalidArgument}},
		{"negative: l3mdev with a table", 0, unix.RTM_NEWRULE, "add l3mdev table 100", result{err: ErrInvalidArgument}},
		{"negative: realms reads rt_realms, which goip does not", 0, unix.RTM_NEWRULE, "add realms 1",
			result{err: ErrNotImplemented}},
		{"negative: nat is not built", 0, unix.RTM_NEWRULE, "add nat 192.0.2.1", result{err: ErrNotImplemented}},
		{"negative: an ipproto name outside goip's table", 0, unix.RTM_NEWRULE, "add ipproto ospf",
			result{err: ErrNotImplemented}},
		{"corner: l3mdev with table 0 is no table, as tid stays 0", 0, unix.RTM_NEWRULE, "add l3mdev table 0",
			result{frh(unix.AF_INET, 0, 0, 0, 0, unix.FR_ACT_TO_TBL, 0), cat(fraOf(unix.FRA_L3MDEV, 1)), nil}},
		{"corner: `tos` is not abbreviated, so `to` is the destination", 0, unix.RTM_NEWRULE, "add to 10.0.0.0/8",
			result{frh(unix.AF_INET, 8, 0, 0, unix.RT_TABLE_MAIN, unix.FR_ACT_TO_TBL, 0),
				cat(fraOf(unix.FRA_DST, 10, 0, 0, 0)), nil}},
		{"corner: a range takes no mask, the slash is in its end", 0, unix.RTM_NEWRULE, "add sport 1-2/3",
			result{err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := parseRuleWriteArgs(tc.family, tc.msgType, strings.Fields(tc.args)[min(1, len(strings.Fields(tc.args))):])
			var got result
			if err != nil {
				for _, e := range []error{ErrInvalidArgument, ErrNotImplemented} {
					if errors.Is(err, e) {
						got.err = e
					}
				}
			} else {
				got = result{frh: string(w.frh()), attrs: string(w.attrs.Bytes())}
			}
			if got != tc.expected {
				t.Errorf("got  %x %x %v\nwant %x %x %v (%v)", got.frh, got.attrs, got.err,
					tc.expected.frh, tc.expected.attrs, tc.expected.err, err)
			}
		})
	}
}

// TestRuleFlushMatchesIp replays rule_flush's capture: goip sends ip's one
// rule dump, byte for byte with seq and pid zeroed, and writes ip's four
// RTM_DELRULE copies, NLM_F_REQUEST|NLM_F_ACK at the seq of the reply each
// copies, which replayed is ip's dump's, keeping the local rule, which carries
// no FRA_PRIORITY, and exits 0 with nothing printed.
//
// go test ./internal/goip/ -run TestRuleFlushMatchesIp
func TestRuleFlushMatchesIp(t *testing.T) {
	ipReqs := rtnlRequests(t, "rule_flush")
	if len(ipReqs) != 5 {
		t.Fatalf("capture holds %d requests, want 5", len(ipReqs))
	}
	capt, err := nlparity.ParseRouteCaptureFile(routeRtnlDir + "rule_flush/ip.pcap")
	if err != nil {
		t.Fatal(err)
	}
	ipSeq := capt.Requests()[0].Hdr.Seq
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newRuleReplay(t, "rule_flush")
	rc, stdout, stderr := runRuleCtx(&runCtx{src: src}, []string{"flush"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	if len(src.dumpReqs) != 1 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
		t.Fatalf("goip sent dumps %x, want %x", src.dumpReqs, ipReqs[0])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 4 {
		t.Fatalf("goip wrote %d messages, want 4", len(writes))
	}
	for i, w := range writes {
		if got := capturedRequest(w); !bytes.Equal(got, ipReqs[1+i]) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, ipReqs[1+i])
		}
		if w.Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK || w.Hdr.Seq != ipSeq {
			t.Errorf("write %d flags %#x seq %d, want 0x5 and %d", i, w.Hdr.Flags, w.Hdr.Seq, ipSeq)
		}
	}
}

// TestRuleFlush is the family filter and the refusals over rule_flush's
// capture, whose five rules are AF_INET.
//
// go test ./internal/goip/ -run TestRuleFlush$
func TestRuleFlush(t *testing.T) {
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
		{description: "positive: a bare flush deletes the four rules with a preference",
			args: "flush", expected: result{dumps: 1, writes: 4}},
		{description: "positive: `f` abbreviates flush", args: "f", expected: result{dumps: 1, writes: 4}},
		{description: "corner: -4 checks the family and keeps all four",
			family: unix.AF_INET, args: "flush", expected: result{dumps: 1, writes: 4}},
		{description: "corner: -6 against AF_INET replies copies none, and needs no hook",
			family: unix.AF_INET6, noHook: true, args: "flush", expected: result{dumps: 1}},
		{description: "negative: rules to delete with no GOIP_WRITE_OUT, after the dump",
			noHook: true, args: "flush", expected: result{rc: ExitUsage, dumps: 1}},
		{description: "negative: a failed dump is Dump terminated, exit 255",
			failDump: true, args: "flush", expected: result{rc: ExitMinusOne, dumps: 1, stderrs: "Dump terminated"}},
		{description: "negative: a selector is refused before any request",
			args: "flush pref 100", expected: result{rc: ExitUsage, stderrs: "not implemented"}},
		{description: "negative: -j is refused before any request",
			json: true, args: "flush", expected: result{rc: ExitUsage, stderrs: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := newRuleReplay(t, "rule_flush")
			src.failRules = tc.failDump
			rc, _, stderr := runRuleCtx(&runCtx{src: src, family: tc.family, json: tc.json}, strings.Fields(tc.args))
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

// TestRuleFlushCopy is flush_rule on one reply, hand-built.
//
// go test ./internal/goip/ -run TestRuleFlushCopy
func TestRuleFlushCopy(t *testing.T) {
	reply := func(family uint8, attrs ...[]byte) []byte {
		body := append([]byte{family, 0, 0, 0, 100, 0, 0, unix.FR_ACT_TO_TBL, 0, 0, 0, 0}, bytes.Join(attrs, nil)...)
		h := make([]byte, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst+len(body))
		binary.LittleEndian.PutUint32(h[0:4], uint32(len(h)+len(body)))
		binary.LittleEndian.PutUint16(h[4:6], unix.RTM_NEWRULE)
		binary.LittleEndian.PutUint16(h[6:8], unix.NLM_F_MULTI)
		binary.LittleEndian.PutUint32(h[8:12], 42)
		binary.LittleEndian.PutUint32(h[12:16], 99)
		return append(h, body...)
	}
	prio := fraOf(unix.FRA_PRIORITY, 1, 0, 0, 0)
	tests := []struct {
		description string
		family      uint8
		msg         []byte
		expected    string // "copy", "keep" or "error"
	}{
		{"positive: a rule with a preference is copied", 0, reply(unix.AF_INET, prio), "copy"},
		{"positive: -6 copies an AF_INET6 rule", unix.AF_INET6, reply(unix.AF_INET6, prio), "copy"},
		{"negative: a rule with no preference is kept", 0, reply(unix.AF_INET), "keep"},
		{"negative: -6 keeps an AF_INET rule", unix.AF_INET6, reply(unix.AF_INET, prio), "keep"},
		{"boundary: a body short of fib_rule_hdr ends the dump", 0, reply(unix.AF_INET)[:xtcpnl.NlMsgHdrSizeCst+11], "error"},
		{"corner: an undecodable attribute is refused", 0, append(reply(unix.AF_INET), 9, 0, 6, 0), "error"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			del, ok, err := ruleFlushCopy(tc.family, tc.msg)
			got := "keep"
			switch {
			case err != nil:
				got = "error"
			case ok:
				got = "copy"
				want := append([]byte(nil), tc.msg...)
				binary.LittleEndian.PutUint16(want[4:6], unix.RTM_DELRULE)
				binary.LittleEndian.PutUint16(want[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
				if !bytes.Equal(del, want) {
					t.Errorf("copy % x, want % x", del, want)
				}
			}
			if got != tc.expected {
				t.Errorf("got %s, want %s (%v)", got, tc.expected, err)
			}
		})
	}
}

// TestRuleSave replays rule_save's capture: goip's file is ip.save byte for
// byte and its request ip's, and the refusals keep ip's order.
//
// go test ./internal/goip/ -run TestRuleSave
func TestRuleSave(t *testing.T) {
	ipReqs := rtnlRequests(t, "rule_save")
	want := readRtnlFile(t, "rule_save", "ip.save")
	type result struct {
		rc    int
		dumps int
		file  string
	}
	tests := []struct {
		description string
		family      uint8
		failDump    bool
		json        bool
		args        string
		expected    result
	}{
		{description: "positive: the magic, then every reply whole", args: "save",
			expected: result{dumps: 1, file: string(want)}},
		{description: "positive: `sa` abbreviates save", args: "sa", expected: result{dumps: 1, file: string(want)}},
		{description: "negative: an argument is refused before the magic", args: "save pref 100",
			expected: result{rc: ExitUsage}},
		{description: "negative: a failed dump leaves the magic, exit 255", failDump: true, args: "save",
			expected: result{rc: ExitMinusOne, dumps: 1, file: string(want[:4])}},
		{description: "negative: -j is refused before the magic", json: true, args: "save", expected: result{rc: ExitUsage}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRuleReplay(t, "rule_save")
			src.failRules = tc.failDump
			var file bytes.Buffer
			rc, _, stderr := runRuleCtx(&runCtx{src: src, out: &file, family: tc.family, json: tc.json},
				strings.Fields(tc.args))
			got := result{rc: rc, dumps: len(src.dumpReqs), file: file.String()}
			if got != tc.expected {
				t.Errorf("rc %d dumps %d file %x\nwant %+v; stderr %q", got.rc, got.dumps, got.file, tc.expected, stderr)
			}
			if got.dumps > 0 && !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
				t.Errorf("dump %x, want %x", src.dumpReqs[0], ipReqs[0])
			}
		})
	}
}

// TestRuleRestoreMatchesIp replays rule_restore's capture: goip sends ip's
// one link dump and writes ip's five RTM_NEWRULE, each flagged
// REQUEST|ACK|EXCL|CREATE at the next seq, and exits 0 with nothing printed.
//
// go test ./internal/goip/ -run TestRuleRestoreMatchesIp
func TestRuleRestoreMatchesIp(t *testing.T) {
	pcap := routeRtnlDir + "rule_restore/ip.pcap"
	ipReqs := captureRequests(t, pcap)
	if len(ipReqs) != 6 {
		t.Fatalf("capture holds %d requests, want 6", len(ipReqs))
	}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	in := bytes.NewReader(readRtnlFile(t, "rule_restore", "ip.save"))
	rc, stdout, stderr := runRuleCtx(&runCtx{src: src, in: in}, []string{"restore"})
	if rc != 0 || stdout != "" || stderr != "" {
		t.Fatalf("rc %d, stdout %q, stderr %q", rc, stdout, stderr)
	}
	if len(src.dumpReqs) != 1 || len(src.gets) != 0 || !bytes.Equal(src.dumpReqs[0], ipReqs[0]) {
		t.Fatalf("goip sent %d dumps, %d gets; link dump %x, want %x", len(src.dumpReqs), len(src.gets), src.dumpReqs, ipReqs[0])
	}
	writes := restoreWrites(t, out)
	if len(writes) != 5 {
		t.Fatalf("goip wrote %d messages, want 5", len(writes))
	}
	seq := src.dumpSeqs[0]
	for i, w := range writes {
		seq++
		if got := wireBytesOf(w); !bytes.Equal(got, ipReqs[1+i]) {
			t.Errorf("write %d:\n got %x\nwant %x", i, got, ipReqs[1+i])
		}
		if w.Hdr.Flags != 0x407 || w.Hdr.Seq != seq {
			t.Errorf("write %d flags %#x seq %d, want 0x407 and %d", i, w.Hdr.Flags, w.Hdr.Seq, seq)
		}
	}
}

// TestRuleRestore is the refusals, over rule_save's file cut and patched.
//
// go test ./internal/goip/ -run TestRuleRestore$
func TestRuleRestore(t *testing.T) {
	file := readRtnlFile(t, "rule_restore", "ip.save")
	first := 4 + int(binary.LittleEndian.Uint32(file[4:8]))
	addrRec := append([]byte(nil), file[:first]...)
	binary.LittleEndian.PutUint16(addrRec[8:10], unix.RTM_NEWADDR)
	type result struct {
		rc, dumps, writes int
	}
	tests := []struct {
		description string
		in          []byte
		noHook      bool
		expected    result
	}{
		{"positive: the whole file", file, false, result{dumps: 1, writes: 5}},
		{"boundary: the magic alone sends nothing and needs no hook", file[:4], true, result{}},
		{"negative: records with no GOIP_WRITE_OUT send nothing", file, true, result{rc: ExitUsage}},
		{"negative: a bad magic, exit 255", append([]byte{0, 0, 0, 0}, file[4:]...), false, result{rc: ExitMinusOne}},
		{"negative: a truncated record: the one before it goes, exit 255",
			file[:first+10], false, result{rc: ExitMinusOne, dumps: 1, writes: 1}},
		{"negative: a record that is not a rule is refused before anything is sent",
			append(addrRec, file[first:]...), false, result{rc: ExitUsage}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.noHook {
				t.Setenv(writeOutEnvCst, "")
			} else {
				t.Setenv(writeOutEnvCst, out)
			}
			src := &restoreReplay{linkReplay: newLinkReplay(t, routeRtnlDir+"rule_restore/ip.pcap")}
			rc, _, stderr := runRuleCtx(&runCtx{src: src, in: bytes.NewReader(tc.in)}, []string{"restore"})
			got := result{rc: rc, dumps: len(src.dumpReqs)}
			if writtenFile(out) {
				got.writes = len(restoreWrites(t, out))
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v; stderr %q", got, tc.expected, stderr)
			}
		})
	}
}

// TestRunRuleVerbs is do_iprule's verb order under matches().
//
// go test ./internal/goip/ -run TestRunRuleVerbs
func TestRunRuleVerbs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    error
	}{
		{"positive: `a` is add", "a", ErrInvalidArgument},
		{"positive: `d` is delete", "d", ErrInvalidArgument},
		{"positive: `del` is delete", "del", ErrInvalidArgument},
		{"positive: `sa` is save, which refuses an argument", "sa x", ErrInvalidArgument},
		{"negative: `delet3` is no verb", "delet3", ErrNotImplemented},
		{"negative: help is not implemented", "help", ErrNotImplemented},
		{"corner: `s` is show, which refuses a selector", "s x", ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := runRule(&runCtx{}, strings.Fields(tc.args))
			if !errors.Is(err, tc.expected) {
				t.Errorf("err %v, want %v", err, tc.expected)
			}
		})
	}
}
