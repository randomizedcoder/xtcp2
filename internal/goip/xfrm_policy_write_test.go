package goip

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The C3 corpus's policies, as xfrm-write-corpus.exp spells them.
const (
	xfrmTestP1Sel  = "src 192.0.2.1/32 dst 192.0.2.2/32 dir out"
	xfrmTestP1Tmpl = "tmpl src 192.0.2.1 dst 192.0.2.2 proto esp reqid 1 mode transport"
	xfrmTestP1     = "policy add " + xfrmTestP1Sel + " " + xfrmTestP1Tmpl
	xfrmTestP2     = "policy add src 2001:db8::1/128 dst 2001:db8::2/128 proto tcp dport 443 dir in priority 10 " +
		"action block"
)

// TestXfrmPolicyWritesMatchIp replays the 17 C3 policy write captures,
// and other spellings that send the same bytes. The reads goip sends must
// be ip's, which a replay answers only then, and every read exchange must
// be replayed and no write; the writes goip wrote to GOIP_WRITE_OUT must
// be the capture's, seq aside, in order; stdout must be ip's. A row the
// kernel refused sends the same write, so goip exits 0; one ip refused
// before sending exits as ip did and writes nothing.
//
// go test ./internal/goip/ -run TestXfrmPolicyWritesMatchIp
func TestXfrmPolicyWritesMatchIp(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		exit        int
	}{
		{description: "positive: add, policy 1, a template", row: "policy_add", args: xfrmTestP1},
		{description: "positive: add, policy 2, IPv6 TCP blocked at priority 10", row: "policy_add_v6_block",
			args: xfrmTestP2},
		{description: "negative: add of a policy there, the kernel's EEXIST, the same NEWPOLICY", row: "policy_add_dup",
			args: xfrmTestP1},
		{description: "negative: add with no dir, refused before sending, exit 1", row: "policy_add_no_dir",
			args: "policy add src 192.0.2.1/32 dst 192.0.2.2/32 " + xfrmTestP1Tmpl, exit: ExitUsage},
		{description: "negative: dir sideways, refused before sending, exit 255", row: "policy_add_bad_dir",
			args: "policy add src 192.0.2.1/32 dst 192.0.2.2/32 dir sideways", exit: ExitMinusOne},
		{description: "positive: update, priority 5", row: "policy_update",
			args: "policy update " + xfrmTestP1Sel + " priority 5 " + xfrmTestP1Tmpl},
		{description: "positive: update of a missing policy adds it", row: "policy_update_new",
			args: "policy update " + xfrmTestP1Sel + " " + xfrmTestP1Tmpl},
		{description: "positive: delete, one DELPOLICY", row: "policy_delete", args: "policy delete " + xfrmTestP1Sel},
		{description: "negative: delete of a missing policy, the same DELPOLICY", row: "policy_delete_missing",
			args: "policy delete " + xfrmTestP1Sel},
		{description: "positive: deleteall, two DELPOLICYs in one datagram between the dumps", row: "policy_deleteall",
			args: "policy deleteall"},
		{description: "boundary: deleteall with none, one dump and no write", row: "policy_deleteall_empty",
			args: "policy deleteall"},
		{description: "positive: flush, two policies", row: "policy_flush", args: "policy flush"},
		{description: "boundary: flush with none, the same FLUSHPOLICY", row: "policy_flush_empty",
			args: "policy flush"},
		{description: "positive: set, both hash thresholds", row: "policy_set_hthresh",
			args: "policy set hthresh4 24 16 hthresh6 64 48"},
		{description: "negative: hthresh4 33, refused before sending, exit 255", row: "policy_set_hthresh_bad",
			args: "policy set hthresh4 33 0", exit: ExitMinusOne},
		{description: "positive: setdefault, each direction", row: "policy_setdefault",
			args: "policy setdefault in block fwd block out accept"},
		{description: "negative: setdefault in drop, refused before sending, exit 255", row: "policy_setdefault_bad",
			args: "policy setdefault in drop", exit: ExitMinusOne},
		{description: "corner: a is add, dir and the template before the selector", row: "policy_add",
			args: "policy a dir out " + xfrmTestP1Tmpl + " src 192.0.2.1/32 dst 192.0.2.2/32"},
		{description: "corner: level required is a template's default", row: "policy_add",
			args: xfrmTestP1 + " level required"},
		{description: "corner: u is update, priority after the template", row: "policy_update",
			args: "policy u " + xfrmTestP1Sel + " " + xfrmTestP1Tmpl + " priority 5"},
		{description: "corner: d is delete, dir first", row: "policy_delete",
			args: "policy d dir out src 192.0.2.1/32 dst 192.0.2.2/32"},
		{description: "corner: delall is deleteall", row: "policy_deleteall", args: "policy delall"},
		{description: "corner: f is flush", row: "policy_flush", args: "policy f"},
		{description: "corner: set's values in hex", row: "policy_set_hthresh",
			args: "policy set hthresh4 0x18 0x10 hthresh6 0x40 0x30"},
		{description: "corner: setdefault's directions in another order, allow for accept", row: "policy_setdefault",
			args: "policy setdefault out allow fwd block in block"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdXfrm, tc.row)
			src, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			r, writes := xfrmWriteRun(t, src, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if want := mustReadFile(t, filepath.Join(dir, "ip.out")); r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			if r.stderr != "" {
				t.Errorf("stderr %q, want none", r.stderr)
			}
			var ipWrites [][]byte
			for i, txn := range src.xseg.Txns {
				write := nlparity.IsWrite(txn.Request)
				if write {
					ipWrites = append(ipWrites, canonMsg(txn.Request))
				}
				if src.xused[i] == write {
					t.Errorf("exchange %d (%s): replayed %v, a write %v", i, nlparity.MsgName(txn.Request),
						src.xused[i], write)
				}
			}
			if len(writes) != len(ipWrites) {
				t.Fatalf("goip wrote %d writes, ip sent %d", len(writes), len(ipWrites))
			}
			for i := range writes {
				if got := canonMsg(writes[i]); !bytes.Equal(got, ipWrites[i]) {
					t.Errorf("write %d\n got %x\nwant %x", i, got, ipWrites[i])
				}
			}
		})
	}
}

// xfrmTestTmpls is n copies of policy 1's template.
func xfrmTestTmpls(n int) string {
	return strings.Repeat(" "+xfrmTestP1Tmpl, n)
}

// TestXfrmPolicyWritesArgs checks the policy writes' words, refusals and
// requests without a capture: the exit status, a fragment of the error,
// "" for none, the write's type, and a fragment of the hex of its bytes
// after its nlmsghdr, "-" for no write.
//
// go test ./internal/goip/ -run TestXfrmPolicyWritesArgs
func TestXfrmPolicyWritesArgs(t *testing.T) {
	const add = "policy add dir out "
	ctx255 := strings.Repeat("c", xfrmCtxBufCst-1)
	tests := []struct {
		description string
		args        string
		family      uint8
		exit        int
		expected    string // a fragment of the error
		typ         uint16 // the write's type
		hex         string // a fragment of the write's hex
		absent      string // hex the write must not hold
	}{
		// Positive: each word's field or attribute.
		{description: "positive: no selector is AF_INET's, every limit infinite but the times",
			args: add, typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "0200 0000 00000000 00000000 00000000" + strings.Repeat("ff", 32) + strings.Repeat("00", 32)},
		{description: "positive: -6 with no selector, preferred_family's", args: add, family: unix.AF_INET6,
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0a00 0000 00000000 00000000 00000000" + strings.Repeat("ff", 32)},
		{description: "positive: index, priority and dir in their fields", args: "policy add dir in index 9 priority 3",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "03000000 09000000 00 00 00"},
		{description: "positive: action block and flag localok icmp", args: "policy add dir fwd action block flag " +
			"localok icmp", typ: xtcpnl.XfrmMsgNewPolicy, hex: "00000000 00000000 02 01 03"},
		{description: "positive: flag 0x1, hex", args: add + "flag 0x1", typ: xtcpnl.XfrmMsgNewPolicy, hex: "00000000 00000000 01 00 01"},
		{description: "positive: limit byte-soft, the first limit", args: add + "limit byte-soft 5",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0500000000000000 ffffffffffffffff"},
		{description: "positive: limit time-use-hard, the last", args: add + "limit time-use-hard 7",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0700000000000000" + strings.Repeat("00", 32)},
		{description: "positive: ptype sub, XFRMA_POLICY_TYPE's six bytes", args: add + "ptype sub",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0a001000 010000000000 0000"},
		{description: "positive: a template: its ID, family, reqid, mode and any algorithm",
			args: add + xfrmTestP1Tmpl, typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "44000500 c0000202" + strings.Repeat("00", 12) + "00000000 32000000 0200 0000 c0000201" +
				strings.Repeat("00", 12) + "01000000 00 00 00 00 ffffffff ffffffff ffffffff"},
		{description: "positive: level use, the template optional", args: add + xfrmTestP1Tmpl + " level use",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "01000000 00 00 01 00 ffffffff"},
		{description: "positive: a tunnel template's mode and spi", args: add + "tmpl src 192.0.2.1 dst 192.0.2.2 " +
			"proto esp spi 0x100 mode tunnel", typ: xtcpnl.XfrmMsgNewPolicy, hex: "00000100 32"},
		{description: "positive: an IPv6 template's family", args: add + "tmpl src 2001:db8::1 dst 2001:db8::2 " +
			"proto esp", typ: xtcpnl.XfrmMsgNewPolicy, hex: "0a00 0000 20010db8"},
		{description: "positive: mark, XFRMA_MARK, its mask all ones", args: add + "mark 7",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0c00150007000000ffffffff"},
		{description: "positive: mark 0 with a mask is sent", args: add + "mark 0 mask 0xff",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0c00150000000000ff000000"},
		{description: "positive: ctx, XFRMA_SEC_CTX", args: add + "ctx ab", typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "0f0008000b0008000101030061620000"},
		{description: "positive: if_id, XFRMA_IF_ID", args: add + "if_id 5", typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "08001f0005000000"},
		{description: "positive: offload packet, XFRMA_OFFLOAD_DEV, the index and XFRM_OFFLOAD_PACKET",
			args: add + "offload packet dev if5", typ: xtcpnl.XfrmMsgNewPolicy, hex: "0c001c00 05000000 04000000"},
		{description: "positive: the attributes in ip's order, not the words'", args: add + "offload packet dev if5 " +
			"if_id 5 ctx ab mark 7 " + xfrmTestP1Tmpl + " ptype sub", typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "0a001000010000000000 0000 44000500"},
		{description: "positive: the attributes in ip's order, the template's tail first",
			args: add + "offload packet dev if5 if_id 5 ctx ab mark 7 " + xfrmTestP1Tmpl, typ: xtcpnl.XfrmMsgNewPolicy,
			hex: "ffffffffffffffff 0c00150007000000ffffffff 0f000800 0b0008000101030061620000 08001f0005000000 0c001c00"},
		{description: "positive: update is UPDPOLICY", args: "policy update dir out", typ: xtcpnl.XfrmMsgUpdPolicy,
			hex: "0200"},
		{description: "positive: delete by index, the selector AF_INET's", args: "policy delete index 9 dir in",
			typ: xtcpnl.XfrmMsgDelPolicy, hex: "0200 0000 00000000 00000000 00000000 09000000 00000000"},
		{description: "positive: delete with ptype and a mark", args: "policy delete index 9 dir in ptype sub mark 1",
			typ: xtcpnl.XfrmMsgDelPolicy, hex: "0a0010000100000000000000 0c00150001000000ffffffff"},
		{description: "positive: flush ptype sub", args: "policy flush ptype sub", typ: xtcpnl.XfrmMsgFlushPolicy,
			hex: "0a0010000100000000000000"},
		{description: "positive: set hthresh6 then hthresh4, in that order", args: "policy set hthresh6 64 48 " +
			"hthresh4 24 16", typ: xtcpnl.XfrmMsgNewSPDInfo, hex: "ffffffff 0600040040300000 0600030018100000"},
		{description: "positive: set with no words, the u32 alone", args: "policy set",
			typ: xtcpnl.XfrmMsgNewSPDInfo, hex: "ffffffff"},
		{description: "positive: setdefault in alone, the others unset", args: "policy setdefault in block",
			typ: xtcpnl.XfrmMsgSetDefault, hex: "010000"},
		{description: "positive: setdefault fwd allow", args: "policy setdefault fwd allow",
			typ: xtcpnl.XfrmMsgSetDefault, hex: "000200"},

		// Negative: ip's refusals.
		{description: "negative: no dir", args: "policy add " + xfrmTestP1Tmpl, exit: ExitUsage,
			expected: "DIR is required", hex: "-"},
		{description: "negative: dir twice", args: add + "dir in", exit: ExitMinusOne, expected: `duplicate "dir"`,
			hex: "-"},
		{description: "negative: a second selector is duparg's unknown", args: add + "src 192.0.2.1 " +
			xfrmTestP1Tmpl + " dst 192.0.2.2", exit: ExitMinusOne, expected: `duplicate "unknown"`, hex: "-"},
		{description: "negative: ptype twice", args: add + "ptype main ptype sub", exit: ExitMinusOne,
			expected: `duplicate "ptype"`, hex: "-"},
		{description: "negative: ctx twice", args: add + "ctx a ctx b", exit: ExitMinusOne,
			expected: `duplicate "ctx"`, hex: "-"},
		{description: "negative: action bogus", args: add + "action bogus", exit: ExitMinusOne,
			expected: "ACTION value is invalid", hex: "-"},
		{description: "negative: priority x", args: add + "priority x", exit: ExitMinusOne,
			expected: "PRIORITY value is invalid", hex: "-"},
		{description: "negative: index x", args: add + "index x", exit: ExitMinusOne,
			expected: "INDEX value is invalid", hex: "-"},
		{description: "negative: if_id x", args: add + "if_id x", exit: ExitMinusOne,
			expected: "IF_ID value is invalid", hex: "-"},
		{description: "negative: limit bogus", args: add + "limit bogus 1", exit: ExitMinusOne,
			expected: "LIMIT value is invalid", hex: "-"},
		{description: "negative: a template's level bogus", args: add + xfrmTestP1Tmpl + " level bogus",
			exit: ExitMinusOne, expected: "LEVEL value is invalid", hex: "-"},
		{description: "negative: a template with no protocol", args: add + "tmpl src 192.0.2.1 dst 192.0.2.2",
			exit: ExitMinusOne, expected: `"XFRM-PROTO" is required`, hex: "-"},
		{description: "negative: tmpl last, its value missing", args: add + "tmpl", exit: ExitMinusOne,
			expected: "Command line is not complete", hex: "-"},
		{description: "negative: offload crypto, only packet", args: add + "offload crypto dev if5",
			exit: ExitMinusOne, expected: "Invalid offload mode", hex: "-"},
		{description: "negative: offload bogus, only packet", args: add + "offload bogus dev if5",
			exit: ExitMinusOne, expected: "Invalid offload mode", hex: "-"},
		{description: "negative: offload to a device there is none of", args: add + "offload packet dev nosuch",
			exit: ExitMinusOne, expected: "Invalid device name", hex: "-"},
		{description: "negative: offload packet without dev", args: add + "offload packet if5",
			exit: ExitMinusOne, expected: "Missing dev keyword", hex: "-"},
		{description: "negative: 17 templates, one past the buffer", args: add + xfrmTestTmpls(17),
			exit: ExitUsage, expected: "Too many tmpls: buffer overflow", hex: "-"},
		{description: "negative: delete with no dir", args: "policy delete index 9", exit: ExitUsage,
			expected: "DIR is required", hex: "-"},
		{description: "negative: flush with another word", args: "policy flush bogus", exit: ExitMinusOne,
			expected: `"bogus" is wrong: unknown`, hex: "-"},
		{description: "negative: flush ptype twice", args: "policy flush ptype main ptype sub", exit: ExitMinusOne,
			expected: `duplicate "ptype"`, hex: "-"},
		{description: "negative: set hthresh4 RBITS 33", args: "policy set hthresh4 1 33", exit: ExitMinusOne,
			expected: "hthresh4 RBITS value is invalid", hex: "-"},
		{description: "negative: set hthresh6 LBITS 129", args: "policy set hthresh6 129 0", exit: ExitMinusOne,
			expected: "hthresh6 LBITS value is invalid", hex: "-"},
		{description: "negative: set hthresh4 x, not get_u8's", args: "policy set hthresh4 x 0", exit: ExitMinusOne,
			expected: "hthresh4 LBITS value is invalid", hex: "-"},
		{description: "negative: set hthresh4 twice", args: "policy set hthresh4 1 1 hthresh4 2 2",
			exit: ExitMinusOne, expected: `duplicate "hthresh4"`, hex: "-"},
		{description: "negative: set with another word", args: "policy set bogus", exit: ExitMinusOne,
			expected: `"bogus" is wrong: unknown`, hex: "-"},
		{description: "negative: setdefault in twice", args: "policy setdefault in block in accept",
			exit: ExitMinusOne, expected: `duplicate "in"`, hex: "-"},
		{description: "negative: setdefault fwd drop", args: "policy setdefault fwd drop", exit: ExitMinusOne,
			expected: "fwd policy value is invalid", hex: "-"},
		{description: "negative: setdefault sideways", args: "policy setdefault sideways block", exit: ExitMinusOne,
			expected: "unknown direction", hex: "-"},
		{description: "negative: a ctx past ip's buffer is not implemented", args: add + "ctx " + ctx255 + "c",
			exit: ExitUsage, expected: "not implemented", hex: "-"},

		// Boundary.
		{description: "boundary: 16 templates fill the buffer", args: add + xfrmTestTmpls(16),
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "04040500"},
		{description: "boundary: hthresh4 32 32", args: "policy set hthresh4 32 32", typ: xtcpnl.XfrmMsgNewSPDInfo,
			hex: "0600030020200000"},
		{description: "boundary: hthresh6 128 128", args: "policy set hthresh6 128 128",
			typ: xtcpnl.XfrmMsgNewSPDInfo, hex: "0600040080800000"},
		{description: "boundary: a ctx that fills ip's buffer", args: add + "ctx " + ctx255,
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0c01 0800 0801 0800 0101 0001"},
		{description: "boundary: mark with its mask 0 is not sent", args: add + "mark 7 mask 0",
			typ: xtcpnl.XfrmMsgNewPolicy, hex: "0200", absent: "0c001500"},

		// Corner.
		{description: "corner: a template's family is preferred_family's, then the selector's after it",
			args: add + "tmpl src 2001:db8::1 dst 2001:db8::2 proto esp mode tunnel src 10.0.0.0/8", exit: ExitUsage,
			expected: `inet6 prefix is expected rather than "10.0.0.0/8"`, hex: "-"},
		{description: "corner: a template with no address is the selector's family", args: "policy add " +
			"src 2001:db8::1 dir out tmpl proto esp", typ: xtcpnl.XfrmMsgNewPolicy, hex: "0a00 0000" +
			strings.Repeat("00", 16) + "00000000 00 00 00 00 ffffffff"},
		{description: "corner: a template's ID is parsed with preferred_family cleared, IPv4 after an IPv6 selector",
			args: "policy add src 2001:db8::1 dir out tmpl src 192.0.2.1 dst 192.0.2.2 proto esp",
			typ:  xtcpnl.XfrmMsgNewPolicy, hex: "0200 0000 c0000201"},
		{description: "corner: -s -s is refused for a write", args: "policy flush", exit: ExitUsage,
			expected: "-s -s xfrm policy flush", hex: "-"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stats := 0
			if strings.HasPrefix(tc.description, "corner: -s -s") {
				stats = 2
			}
			r, writes := xfrmWriteRunCtx(t, &xfrmFake{}, tc.family, stats, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.expected != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.expected)) {
				t.Errorf("err %v, want one containing %q", r.err, tc.expected)
			}
			if tc.hex == "-" {
				if writes != nil {
					t.Errorf("%d writes, want none", len(writes))
				}
				return
			}
			if len(writes) != 1 {
				t.Fatalf("%d writes, want 1", len(writes))
			}
			w := writes[0]
			if w.Hdr.Type != tc.typ || w.Hdr.Flags != unix.NLM_F_REQUEST|unix.NLM_F_ACK {
				t.Errorf("write type %d flags %#x, want %d with NLM_F_ACK", w.Hdr.Type, w.Hdr.Flags, tc.typ)
			}
			got := hexOf(w.Body)
			if !strings.Contains(got, strings.ReplaceAll(tc.hex, " ", "")) {
				t.Errorf("write %s, want it to hold %s", got, tc.hex)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Errorf("write %s, want it without %s", got, tc.absent)
			}
		})
	}
}

// TestXfrmPolicyDeleteall runs deleteall over a fake source that answers
// every dump as a kernel nothing was deleted from does, as goip's runs
// are: the number of dumps, of DELPOLICYs written, a fragment of their
// hex, the exit status, and a fragment of the error or stderr.
//
// go test ./internal/goip/ -run TestXfrmPolicyDeleteall
func TestXfrmPolicyDeleteall(t *testing.T) {
	p1 := xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "192.0.2.1", daddr: "192.0.2.2", plenS: 32, plenD: 32,
		dir: 1, index: 9, ptype: -1})
	p2 := xfrmPolMsg(xfrmPol{family: unix.AF_INET6, saddr: "2001:db8::1", daddr: "2001:db8::2", plenS: 128,
		plenD: 128, proto: unix.IPPROTO_TCP, dport: 443, action: 1, priority: 10, index: 16, ptype: -1})
	sock := xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", dir: 3, index: 4,
		ptype: -1})
	sub := xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", dir: 2, index: 18,
		ptype: 1})
	// XFRMA_MARK 5/0xff and XFRMA_IF_ID 9 after it, and the mark alone.
	markIfID := []byte{12, 0, 21, 0, 5, 0, 0, 0, 0xff, 0, 0, 0, 8, 0, 31, 0, 9, 0, 0, 0}
	mark := []byte{12, 0, 21, 0, 5, 0, 0, 0, 0xff, 0, 0, 0}
	// 103 policies: 102 DELPOLICYs of 80 bytes fill 8160 of the 8192, and
	// the last waits for the next round.
	many := make([]xfrmMsg, 0, 103)
	for i := range 103 {
		many = append(many, xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", dir: 1,
			index: uint32(8 + 8*i), ptype: -1}))
	}
	tests := []struct {
		description string
		dump        []xfrmMsg
		dumpErr     error
		args        string
		dumps       int
		writes      int
		hex         string // a fragment of the writes' hex
		exit        int
		text        string // a fragment of the error, or of stderr
		absent      string // what the error must not hold
	}{
		{description: "positive: two policies, one round of two, then a round copying none",
			dump: []xfrmMsg{p2, p1}, args: "policy deleteall", dumps: 2, writes: 2},
		{description: "positive: a DELPOLICY is the selector, index and dir", dump: []xfrmMsg{p1},
			args: "policy deleteall", dumps: 2, writes: 1,
			hex: hexOf(p1.body[:56]) + "09000000" + "01000000"},
		{description: "positive: a filter on dir in copies one", dump: []xfrmMsg{p2, p1},
			args: "policy deleteall dir in", dumps: 2, writes: 1, hex: "10000000" + "00000000"},
		{description: "positive: a mark copied with the four bytes after it, then the if_id",
			dump: []xfrmMsg{xfrmWithAttrs(p1, markIfID...)}, args: "policy deleteall", dumps: 2, writes: 1,
			hex: "1000150005000000ff00000008001f00" + "08001f0009000000"},
		{description: "positive: ptype sub's filter, the type's attribute not copied", dump: []xfrmMsg{p1, sub},
			args: "policy deleteall ptype sub", dumps: 2, writes: 1, hex: "12000000" + "02000000"},
		{description: "boundary: none, one dump and no write", args: "policy deleteall", dumps: 1},
		{description: "boundary: 103 policies, 102 in the first round and one in the second", dump: many,
			args: "policy deleteall", dumps: 3, writes: 103},
		{description: "negative: a dump error is Delete-all terminated, exit 1", dumpErr: unix.EPERM,
			args: "policy deleteall", dumps: 1, exit: ExitUsage, text: "Delete-all terminated"},
		{description: "negative: a short policy ends the dump", dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewPolicy,
			body: make([]byte, 100)}}, args: "policy deleteall", dumps: 1, exit: ExitUsage, text: "wrong nlmsg len -68"},
		{description: "negative: a second selector", args: "policy deleteall 192.0.2.1 192.0.2.2",
			exit: ExitMinusOne, text: "unknown"},
		{description: "corner: a socket policy is skipped", dump: []xfrmMsg{sock}, args: "policy deleteall", dumps: 1},
		{description: "corner: another type is Not a policy on stderr", dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewSA,
			body: make([]byte, 4)}}, args: "policy deleteall", dumps: 1, text: "Not a policy: 00000014 00000010"},
		{description: "corner: a mark ending its message is not implemented",
			dump: []xfrmMsg{xfrmWithAttrs(p1, mark...)}, args: "policy deleteall", dumps: 1, exit: ExitUsage,
			text: "not implemented", absent: "Delete-all terminated"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &xfrmFake{dump: tc.dump, dumpErr: tc.dumpErr}
			r, writes := xfrmWriteRun(t, f, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.text != "" && !strings.Contains(fmt.Sprint(r.err)+r.stderr, tc.text) {
				t.Errorf("err %v, stderr %q, want one holding %q", r.err, r.stderr, tc.text)
			}
			if tc.absent != "" && strings.Contains(fmt.Sprint(r.err), tc.absent) {
				t.Errorf("err %v, want one without %q", r.err, tc.absent)
			}
			if len(f.reqs) != tc.dumps {
				t.Errorf("%d dumps, want %d", len(f.reqs), tc.dumps)
			}
			if len(writes) != tc.writes {
				t.Fatalf("%d writes, want %d", len(writes), tc.writes)
			}
			var all []byte
			for _, w := range writes {
				if w.Hdr.Type != xtcpnl.XfrmMsgDelPolicy || w.Hdr.Flags != unix.NLM_F_REQUEST {
					t.Errorf("write type %d flags %#x, want a DELPOLICY without NLM_F_ACK", w.Hdr.Type, w.Hdr.Flags)
				}
				all = append(all, w.Body...)
			}
			if got := hexOf(all); !strings.Contains(got, tc.hex) {
				t.Errorf("writes %s, want them to hold %s", got, tc.hex)
			}
		})
	}
}
