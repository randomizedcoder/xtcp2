package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestXfrmPolicyMatchesIp replays the twelve C3 policy read captures, and
// other spellings that send the same bytes. A replay answers only a request
// whose bytes are ip's, seq aside, so every exchange replayed means goip
// sent ip's requests; stdout must be the capture's output, stderr hold
// ip's refusal, and the exit status be ip's.
//
// go test ./internal/goip/ -run TestXfrmPolicyMatchesIp
func TestXfrmPolicyMatchesIp(t *testing.T) {
	const get = "policy get src 192.0.2.1/32 dst 192.0.2.2/32 dir out"
	tests := []struct {
		description string
		row         string
		args        string
		stats       int
		exit        int
		stderr      string // in the error, for a refusal
	}{
		{description: "boundary: list with none", row: "policy_list_empty", args: "policy list"},
		{description: "positive: list, two policies, the IPv6 TCP block first", row: "policy_list",
			args: "policy list"},
		{description: "positive: list dir in, the filter goip's, one left", row: "policy_list_dir_in",
			args: "policy list dir in"},
		{description: "positive: get, one policy by its selector and dir", row: "policy_get", args: get},
		{description: "positive: get by index and dir", row: "policy_get_index", args: "policy get index 1 dir out"},
		{description: "negative: get of a missing policy, the kernel's ENOENT, exit 2", row: "policy_get_missing",
			args: get, exit: ExitFailure},
		{description: "negative: a selector and an index, duparg2, exit 255", row: "policy_get_sel_and_index",
			args: get + " index 1", exit: ExitMinusOne,
			stderr: `Error: either "SELECTOR" is duplicate, or "INDEX" is garbage`},
		{description: "negative: dir with neither a selector nor an index, exit 1", row: "policy_get_no_sel",
			args: "policy get dir out", exit: ExitUsage,
			stderr: "Not enough information: either SELECTOR or INDEX is required"},
		{description: "positive: count", row: "policy_count", args: "policy count"},
		{description: "boundary: count with none", row: "policy_count_empty", args: "policy count"},
		{description: "positive: -s -s count, the socket counts, buckets and thresholds", row: "policy_count_stats",
			args: "policy count", stats: 2},
		{description: "positive: getdefault", row: "policy_getdefault", args: "policy getdefault"},
		{description: "corner: no verb is list", row: "policy_list", args: "policy"},
		{description: "corner: p is policy, s is show", row: "policy_list", args: "p s"},
		{description: "corner: lst", row: "policy_list", args: "policy lst"},
		{description: "corner: the filter's words reordered, its selector by prefix", row: "policy_list_dir_in",
			args: "policy list src 2001:db8::/64 dir in"},
		{description: "corner: g is get, its words reordered", row: "policy_get",
			args: "policy g dir out dst 192.0.2.2 src 192.0.2.1"},
		{description: "corner: the index in hex", row: "policy_get_index", args: "policy get dir out index 0x1"},
		{description: "corner: c is count, a word after it unread", row: "policy_count", args: "policy c bogus"},
		{description: "corner: getd is getdefault, a word after it unread", row: "policy_getdefault",
			args: "policy getd x"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src, err := OpenReplay(filepath.Join(tdXfrm, tc.row, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			r := xfrmRunCount(src, unix.AF_UNSPEC, tc.stats, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if want := mustReadFile(t, filepath.Join(tdXfrm, tc.row, "ip.out")); r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			if tc.stderr != "" && (r.err == nil || !strings.Contains(r.err.Error(), tc.stderr)) {
				t.Errorf("err %v, want one holding %q", r.err, tc.stderr)
			}
			if tc.exit == 0 && r.stderr != "" {
				t.Errorf("stderr %q, want none, as ip's", r.stderr)
			}
			for i := range src.xseg.Txns {
				if !src.xused[i] {
					t.Errorf("exchange %d not replayed", i)
				}
			}
		})
	}
}

// TestXfrmWideStats drives Run, its option loop and its `-s -s` refusal
// included, over the C3 policy captures: xfrm is the object that refuses
// show_stats > 1 itself, and policy count the one verb that renders it.
//
// go test ./internal/goip/ -run TestXfrmWideStats
func TestXfrmWideStats(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		expected    int
		stderr      string // in stderr; stdout is ip's when the exit is 0
	}{
		{description: "positive: -s -s xfrm policy count, past Run's refusal", row: "policy_count_stats",
			args: "-s -s xfrm policy count", expected: ExitOK},
		{description: "positive: -s -s with c, count by prefix", row: "policy_count_stats",
			args: "-s -s xfrm policy c", expected: ExitOK},
		{description: "boundary: -s -s -s is show_stats > 1 too, ip's same render", row: "policy_count_stats",
			args: "-s -s -s xfrm policy count", expected: ExitOK},
		{description: "negative: -s -s xfrm policy list, a render goip does not have", row: "policy_list",
			args: "-s -s xfrm policy list", expected: ExitUsage, stderr: "-s -s xfrm policy list"},
		{description: "negative: -s -s xfrm policy get", row: "policy_get",
			args: "-s -s xfrm policy get src 192.0.2.1/32 dst 192.0.2.2/32 dir out", expected: ExitUsage,
			stderr: "-s -s xfrm policy get"},
		{description: "negative: -s -s xfrm state count, whose buckets goip does not render", row: "policy_count",
			args: "-s -s xfrm state count", expected: ExitUsage, stderr: "-s -s xfrm state"},
		{description: "negative: -s -s link show, still Run's refusal", row: "policy_count",
			args: "-s -s link show", expected: ExitUsage, stderr: "show_stats = 2"},
		{description: "corner: -s -s xfrm policy with no verb is list, refused", row: "policy_list",
			args: "-s -s xfrm policy", expected: ExitUsage, stderr: "-s -s xfrm policy list"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", filepath.Join(tdXfrm, tc.row, "ip.pcap"))
			var stdout, stderr bytes.Buffer
			if code := Run(strings.Fields(tc.args), &stdout, &stderr); code != tc.expected {
				t.Fatalf("Run(%q) = %d, want %d; stderr: %s", tc.args, code, tc.expected, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr %q, want one holding %q", stderr.String(), tc.stderr)
			}
			if tc.expected != ExitOK {
				return
			}
			if want := mustReadFile(t, filepath.Join(tdXfrm, tc.row, "ip.out")); stdout.String() != want {
				t.Errorf("stdout\n got %q\nwant %q", stdout.String(), want)
			}
		})
	}
}

// TestWideStatsObject checks which argv's object refuses show_stats > 1
// itself.
//
// go test ./internal/goip/ -run TestWideStatsObject
func TestWideStatsObject(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    bool
	}{
		{"positive: xfrm", []string{"xfrm", "policy", "count"}, true},
		{"positive: xf, xfrm by prefix", []string{"xf"}, true},
		{"negative: link", []string{"link", "show"}, false},
		{"negative: an unknown object", []string{"bogus"}, false},
		{"boundary: no object", nil, false},
		{"corner: xfrmx, longer than xfrm, no object", []string{"xfrmx"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := wideStatsObject(tc.args); got != tc.expected {
				t.Errorf("wideStatsObject(%q) = %v, want %v", tc.args, got, tc.expected)
			}
		})
	}
}

// xfrmPol is a policy for xfrmPolBody: its selector's family, addresses,
// lengths, protocol and ports, and its fields.
type xfrmPol struct {
	family             uint16
	saddr, daddr       string
	plenS, plenD       uint8
	proto              uint8
	sport, dport       uint16
	dir, action, flags uint8
	priority, index    uint32
	ifindex            int32
	ptype              int // -1 for no XFRMA_POLICY_TYPE
}

// xfrmPolBody is p's struct xfrm_userpolicy_info and, when it has one, its
// XFRMA_POLICY_TYPE.
func xfrmPolBody(p xfrmPol) []byte {
	b := make([]byte, xtcpnl.XfrmUserpolicyInfoSizeCst)
	copy(b[0:], netip.MustParseAddr(p.daddr).AsSlice())
	copy(b[16:], netip.MustParseAddr(p.saddr).AsSlice())
	binary.BigEndian.PutUint16(b[32:], p.dport)
	binary.BigEndian.PutUint16(b[36:], p.sport)
	if p.dport != 0 {
		binary.BigEndian.PutUint16(b[34:], 0xffff)
	}
	if p.sport != 0 {
		binary.BigEndian.PutUint16(b[38:], 0xffff)
	}
	binary.LittleEndian.PutUint16(b[40:], p.family)
	b[42], b[43], b[44] = p.plenD, p.plenS, p.proto
	binary.LittleEndian.PutUint32(b[48:], uint32(p.ifindex))
	binary.LittleEndian.PutUint32(b[152:], p.priority)
	binary.LittleEndian.PutUint32(b[156:], p.index)
	b[160], b[161], b[162] = p.dir, p.action, p.flags
	if p.ptype >= 0 {
		b = append(b, 10, 0, byte(xtcpnl.XfrmAttrPolicyType), 0, byte(p.ptype), 0, 0, 0, 0, 0, 0, 0)
	}
	return b
}

// xfrmPolMsg is p as a dump's NEWPOLICY.
func xfrmPolMsg(p xfrmPol) xfrmMsg {
	return xfrmMsg{typ: xtcpnl.XfrmMsgNewPolicy, flags: unix.NLM_F_MULTI, body: xfrmPolBody(p)}
}

// xfrmPolicyPrinted is the priorities of the policies printed, in order.
func xfrmPolicyPrinted(out string) string {
	var ps []string
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "priority "); i >= 0 {
			ps = append(ps, strings.Fields(line[i:])[1])
		}
	}
	return strings.Join(ps, " ")
}

// TestXfrmPolicyArgs checks the policy verbs over a fake source: the
// requests' types, flags and bodies, each list filter against four
// policies, the get request's header and attributes in ip's order, the
// refusals and their exit status, and answers the printer skips or
// refuses.
//
// go test ./internal/goip/ -run TestXfrmPolicyArgs
func TestXfrmPolicyArgs(t *testing.T) {
	pols := []xfrmMsg{
		xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "192.0.2.1", daddr: "192.0.2.2", plenS: 32, plenD: 32,
			dir: 1, index: 9, ptype: -1}),
		xfrmPolMsg(xfrmPol{family: unix.AF_INET6, saddr: "2001:db8::1", daddr: "2001:db8::2", plenS: 128,
			plenD: 128, proto: unix.IPPROTO_TCP, dport: 443, action: 1, flags: 1, priority: 10, index: 16,
			ptype: 0}),
		xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "10.0.0.0", daddr: "10.1.0.0", plenS: 8, plenD: 16,
			proto: unix.IPPROTO_UDP, sport: 53, dir: 2, priority: 20, index: 18, ptype: 1}),
		xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", dir: 3, priority: 30,
			index: 4, ptype: -1}),
	}
	any4 := func(p xfrmPol) xfrmMsg {
		p.family, p.saddr, p.daddr, p.ptype = unix.AF_INET, "0.0.0.0", "0.0.0.0", -1
		return xfrmPolMsg(p)
	}
	gre := []xfrmMsg{any4(xfrmPol{proto: unix.IPPROTO_GRE, sport: 9, dport: 2, priority: 60}),
		any4(xfrmPol{proto: unix.IPPROTO_GRE, sport: 9, dport: 3, priority: 61})}
	icmp := []xfrmMsg{any4(xfrmPol{proto: unix.IPPROTO_ICMP, sport: 8, priority: 70}),
		any4(xfrmPol{proto: unix.IPPROTO_ICMP, priority: 71})}
	dev := []xfrmMsg{pols[0], any4(xfrmPol{ifindex: 5, priority: 80})}
	noFamily := xfrmPolBody(xfrmPol{saddr: "192.0.2.1", daddr: "192.0.2.2", plenS: 32, plenD: 32, dir: 1,
		ptype: -1})
	const all, dump = "0 10 20 30", "21:769:"
	unknown := xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", proto: 200,
		priority: 40, ptype: -1})
	upd := xfrmPolMsg(xfrmPol{family: unix.AF_INET, saddr: "0.0.0.0", daddr: "0.0.0.0", priority: 50, ptype: -1})
	upd.typ = xtcpnl.XfrmMsgUpdPolicy
	// P1's selector and dir out, as get sends them.
	p1Get := "c0000202" + strings.Repeat("00", 12) + "c0000201" + strings.Repeat("00", 12) + "00000000" +
		"00000000" + "0200" + "20" + "20" + "00" + "000000" + "00000000" + "00000000" + "00000000" + "01000000"
	idxGet := strings.Repeat("00", 40) + "0200" + strings.Repeat("00", 14) + "09000000" + "01000000"
	tests := []struct {
		description string
		args        string
		family      uint8
		dump        []xfrmMsg
		talkTyp     uint16
		talkBody    []byte
		talkErr     error
		exit        int
		reqs        string // each request's type:flags:body, space-separated; "-" for none
		printed     string // the priorities printed, in order
		text        string // in the error, or in stderr
		stdout      string // in stdout
	}{
		{description: "positive: list, all four, a dump with no header", args: "policy list", dump: pols,
			reqs: dump, printed: all},
		{description: "positive: dir in", args: "policy list dir in", dump: pols, reqs: dump, printed: "10"},
		{description: "positive: dir fwd", args: "policy list dir fwd", dump: pols, reqs: dump, printed: "20"},
		{description: "positive: index 18", args: "policy list index 18", dump: pols, reqs: dump, printed: "20"},
		{description: "positive: ptype sub, the attribute's", args: "policy list ptype sub", dump: pols, reqs: dump,
			printed: "20"},
		{description: "positive: ptype main, an attribute's or none", args: "policy list ptype main", dump: pols,
			reqs: dump, printed: "0 10 30"},
		{description: "positive: action block", args: "policy list action block", dump: pols, reqs: dump,
			printed: "10"},
		{description: "positive: action allow", args: "policy list action allow", dump: pols, reqs: dump,
			printed: "0 20 30"},
		{description: "positive: priority 10", args: "policy list priority 10", dump: pols, reqs: dump,
			printed: "10"},
		{description: "positive: flag localok", args: "policy list flag localok", dump: pols, reqs: dump,
			printed: "10"},
		{description: "negative: flag icmp, none", args: "policy list flag icmp", dump: pols, reqs: dump},
		{description: "positive: flag 0x1", args: "policy list flag 0x1", dump: pols, reqs: dump, printed: "10"},
		{description: "corner: flag then a word not a flag, the flag mask matching none", dump: pols, reqs: dump,
			args: "policy list flag nosock"},
		{description: "positive: nosock drops the socket policy", args: "policy list nosock", dump: pols, reqs: dump,
			printed: "0 10 20"},
		{description: "positive: src 192.0.2.0/24, its prefix and family", args: "policy list src 192.0.2.0/24",
			dump: pols, reqs: dump, printed: "0"},
		{description: "positive: dst 10.1.0.0/16", args: "policy list dst 10.1.0.0/16", dump: pols, reqs: dump,
			printed: "20"},
		{description: "positive: flag localok, then dir in, read on", args: "policy list flag localok dir in",
			dump: pols, reqs: dump, printed: "10"},
		{description: "corner: a GRE key filters on its dport half alone, as ip's mask does",
			args: "policy list proto gre key 2", dump: gre, reqs: dump, printed: "60"},
		{description: "positive: an ICMP type, its sport", args: "policy list proto icmp type 8", dump: icmp,
			reqs: dump, printed: "70"},
		{description: "positive: dev none drops a policy on a device", args: "policy list dev none", dump: dev,
			reqs: dump, printed: "0"},
		{description: "positive: -4, the family alone", args: "policy list", family: unix.AF_INET, dump: pols,
			reqs: dump, printed: "0 20 30"},
		{description: "positive: proto tcp", args: "policy list proto tcp", dump: pols, reqs: dump, printed: "10"},
		{description: "positive: proto udp sport 53", args: "policy list proto udp sport 53", dump: pols,
			reqs: dump, printed: "20"},
		{description: "boundary: sport 0 still filters, its mask the word's", args: "policy list sport 0",
			dump: pols, reqs: dump, printed: "0 10 30"},
		{description: "corner: dport alone, no protocol, IPPROTO_IP's", args: "policy list dport 443", dump: pols,
			reqs: dump, printed: "10"},
		{description: "corner: dev none filters on ifindex 0", args: "policy list dev none", dump: pols, reqs: dump,
			printed: all},
		{description: "positive: UPDPOLICY prints Updated", args: "policy list", dump: []xfrmMsg{upd}, reqs: dump,
			printed: "50", stdout: "Updated src 0.0.0.0/0"},
		{description: "negative: an answer of another type is not a policy", args: "policy list",
			dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewSA, flags: unix.NLM_F_MULTI, body: make([]byte, 8)}, pols[0]},
			reqs: dump, printed: "0", text: "Not a policy: 00000018 00000010 00000002"},
		{description: "negative: a policy short of its struct ends the dump, exit 1", args: "policy list",
			dump: []xfrmMsg{pols[0], {typ: xtcpnl.XfrmMsgNewPolicy, body: make([]byte, 167)}}, reqs: dump,
			printed: "0", exit: ExitUsage, text: "wrong nlmsg len -1"},
		{description: "corner: a DELPOLICY is the monitor's, not implemented", args: "policy list",
			dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgDelPolicy, body: make([]byte, 64)}}, reqs: dump, exit: ExitUsage,
			text: "not implemented"},
		{description: "corner: a selector protocol goip cannot name is not implemented", args: "policy list",
			dump: []xfrmMsg{unknown}, reqs: dump, exit: ExitUsage, text: "getprotobynumber"},
		{description: "negative: a refused dump, exit 1", args: "policy list", reqs: dump, exit: ExitUsage,
			text: "dump terminated"},
		{description: "negative: dir sideways", args: "policy list dir sideways", exit: ExitMinusOne, reqs: "-",
			text: "DIR value is invalid"},
		{description: "negative: dir with no value", args: "policy list dir", exit: ExitMinusOne, reqs: "-",
			text: "not complete"},
		{description: "negative: ptype x", args: "policy list ptype x", exit: ExitMinusOne, reqs: "-",
			text: "PTYPE value is invalid"},
		{description: "negative: action drop", args: "policy list action drop", exit: ExitMinusOne, reqs: "-",
			text: "ACTION value is invalid"},
		{description: "negative: priority x", args: "policy list priority x", exit: ExitMinusOne, reqs: "-",
			text: "PRIORITY value is invalid"},
		{description: "negative: index x", args: "policy list index x", exit: ExitMinusOne, reqs: "-",
			text: "INDEX value is invalid"},
		{description: "boundary: flag 0x100 is not a byte", args: "policy list flag 0x100", exit: ExitMinusOne,
			reqs: "-", text: "FLAG value is invalid"},
		{description: "negative: a second selector", args: "policy list src 192.0.2.1 nosock dst 192.0.2.2",
			exit: ExitMinusOne, reqs: "-", text: `"dst" is wrong: unknown`},
		{description: "positive: get by selector, struct xfrm_userpolicy_id", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get src 192.0.2.1/32 dst 192.0.2.2/32 dir out", talkBody: pols[0].body,
			reqs: "21:1:" + p1Get, printed: "0"},
		{description: "positive: get by index, AF_INET with no selector", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get index 9 dir out", talkBody: pols[0].body, reqs: "21:1:" + idxGet, printed: "0"},
		{description: "positive: get's attributes in ip's order, not the words'", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get if_id 7 ctx c mark 5 mask 0xf ptype sub index 9 dir out", talkBody: pols[0].body,
			reqs: "21:1:" + idxGet + "0a001000" + "010000000000" + "0000" + "0c001500" + "05000000" + "0f000000" +
				"0e000800" + "0a000800" + "01010200" + "6300" + "0000" + "08001f00" + "07000000",
			printed: "0"},
		{description: "corner: a selector's family is preferred_family's for the answer, whose selector has none",
			talkTyp: xtcpnl.XfrmMsgNewPolicy, args: "policy get src 192.0.2.1 dir out", talkBody: noFamily,
			reqs: "21:1:" + strings.Repeat("00", 16) + "c0000201" + strings.Repeat("00", 12) + "0000000000000000" +
				"0200" + "00" + "20" + "00" + "000000" + "0000000000000000" + "00000000" + "01000000",
			printed: "0", stdout: "src 192.0.2.1/32 dst 192.0.2.2/32"},
		{description: "corner: a mark with mask 0 is not sent", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get mark 5 mask 0 index 9 dir out", talkBody: pols[0].body, reqs: "21:1:" + idxGet,
			printed: "0"},
		{description: "corner: a mark of 0 is not sent", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get mark 0 index 9 dir out", talkBody: pols[0].body, reqs: "21:1:" + idxGet,
			printed: "0"},
		{description: "negative: no dir, exit 1", args: "policy get src 192.0.2.1", exit: ExitUsage, reqs: "-",
			text: "DIR is required"},
		{description: "negative: dir checked before the selector", args: "policy get", exit: ExitUsage, reqs: "-",
			text: "DIR is required"},
		{description: "negative: dir twice", args: "policy get dir out dir in", exit: ExitMinusOne, reqs: "-",
			text: `duplicate "dir"`},
		{description: "negative: index twice", args: "policy get index 1 index 2", exit: ExitMinusOne, reqs: "-",
			text: `duplicate "index"`},
		{description: "negative: ptype twice", args: "policy get ptype main ptype sub", exit: ExitMinusOne,
			reqs: "-", text: `duplicate "ptype"`},
		{description: "negative: ctx twice", args: "policy get ctx a ctx b", exit: ExitMinusOne, reqs: "-",
			text: `duplicate "ctx"`},
		{description: "negative: if_id x", args: "policy get if_id x", exit: ExitMinusOne, reqs: "-",
			text: "IF_ID value is invalid"},
		{description: "negative: a second selector", args: "policy get src 192.0.2.1 dir out dst 192.0.2.2",
			exit: ExitMinusOne, reqs: "-", text: `"dst" is wrong: unknown`},
		{description: "boundary: a ctx past ip's buffer is not implemented", exit: ExitUsage, reqs: "-",
			args: "policy get ctx " + strings.Repeat("c", 256), text: "not implemented"},
		{description: "boundary: a ctx filling ip's buffer", talkTyp: xtcpnl.XfrmMsgNewPolicy,
			args: "policy get index 9 dir out ctx " + strings.Repeat("c", 255), talkBody: pols[0].body,
			reqs:    "21:1:" + idxGet + "0c010800" + "08010800" + "01010001" + strings.Repeat("63", 255) + "00",
			printed: "0"},
		{description: "negative: get refused by the kernel, exit 2", args: "policy get index 9 dir out",
			talkErr: unix.ENOENT, exit: ExitFailure, reqs: "21:1:" + idxGet},
		{description: "negative: an answer short of its struct, An error :-), exit 1",
			args: "policy get index 9 dir out", talkTyp: xtcpnl.XfrmMsgNewPolicy, talkBody: make([]byte, 8),
			exit: ExitUsage, reqs: "21:1:" + idxGet, text: "An error :-)"},
		{description: "negative: an answer not a policy, on stderr, exit 0", args: "policy get index 9 dir out",
			talkTyp: xtcpnl.XfrmMsgNewSA, talkBody: make([]byte, 8), reqs: "21:1:" + idxGet,
			text: "Not a policy"},
		{description: "positive: count asks for every field", args: "policy count", talkTyp: xtcpnl.XfrmMsgNewSPDInfo,
			talkBody: []byte{0xff, 0xff, 0xff, 0xff}, reqs: "37:1:ffffffff", stdout: "\t SPD"},
		{description: "negative: count's answer short of its flags, exit 1", args: "policy count",
			talkTyp: xtcpnl.XfrmMsgNewSPDInfo, talkBody: []byte{1}, reqs: "37:1:ffffffff", exit: ExitUsage,
			text: "SPDinfo"},
		{description: "negative: count refused, exit 2", args: "policy count", talkErr: unix.EPERM, exit: ExitFailure,
			reqs: "37:1:ffffffff"},
		{description: "positive: getdefault, its struct zero", args: "policy getdefault",
			talkTyp: xtcpnl.XfrmMsgGetDefault, talkBody: []byte{1, 2, 0, 0}, reqs: "40:1:000000",
			stdout: "Default policies:\n in:  block\n fwd: accept\n out: unspec\n"},
		{description: "boundary: an answer of nlmsg_len 19, its struct unpadded, a BUG line, exit 0",
			args: "policy getdefault", talkTyp: xtcpnl.XfrmMsgGetDefault, talkBody: []byte{1, 2, 0},
			reqs: "40:1:000000", text: "BUG: short nlmsg len 19 (expect 20) for XFRM_MSG_GETDEFAULT"},
		{description: "negative: getdefault refused, exit 2", args: "policy getdefault", talkErr: unix.EPERM,
			exit: ExitFailure, reqs: "40:1:000000"},
		{description: "negative: help is usage", args: "policy help", exit: ExitMinusOne, reqs: "-",
			text: "Usage: ip xfrm policy"},
		{description: "negative: a verb it does not know", args: "policy bogus", exit: ExitMinusOne, reqs: "-",
			text: `Command "bogus" is unknown, try "ip xfrm policy help"`},
		{description: "corner: d is delete, not deleteall: DIR is required, no dump", args: "policy d",
			exit: ExitUsage, reqs: "-", text: "DIR is required"},
		{description: "corner: delall is deleteall, one dump, none to delete", args: "policy delall", reqs: dump},
		{description: "corner: f is flush, whose only word is ptype", args: "policy f bogus", exit: ExitMinusOne,
			reqs: "-", text: `"bogus" is wrong: unknown`},
		{description: "corner: se is set, after show", args: "policy se hthresh4 33", exit: ExitMinusOne, reqs: "-",
			text: "hthresh4 LBITS value is invalid"},
		{description: "corner: setd is setdefault", args: "policy setd bogus", exit: ExitMinusOne, reqs: "-",
			text: "unknown direction"},
		{description: "corner: u is update, DIR required", args: "policy u", exit: ExitUsage, reqs: "-",
			text: "DIR is required"},
		{description: "corner: a is add, DIR required", args: "policy a", exit: ExitUsage, reqs: "-",
			text: "DIR is required"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &xfrmFake{dump: tc.dump, talkTyp: tc.talkTyp, talkBody: tc.talkBody, talkErr: tc.talkErr}
			if tc.dump == nil && tc.text == "dump terminated" {
				f.dumpErr = unix.EPERM
			}
			r := xfrmRunArgs(f, tc.family, false, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			reqs := make([]string, 0, len(f.reqs))
			for _, q := range f.reqs {
				reqs = append(reqs, fmt.Sprintf("%d:%d:%s", binary.LittleEndian.Uint16(q[4:]),
					binary.LittleEndian.Uint16(q[6:]), hex.EncodeToString(q[xtcpnl.NlMsgHdrSizeCst:])))
			}
			got := strings.Join(reqs, " ")
			if got == "" {
				got = "-"
			}
			if got != tc.reqs {
				t.Errorf("requests\n got %s\nwant %s", got, tc.reqs)
			}
			if p := xfrmPolicyPrinted(r.stdout); p != tc.printed {
				t.Errorf("printed priorities %q, want %q\n%s", p, tc.printed, r.stdout)
			}
			if !strings.Contains(r.stdout, tc.stdout) {
				t.Errorf("stdout %q, want it to hold %q", r.stdout, tc.stdout)
			}
			msg := r.stderr
			if r.err != nil {
				msg += r.err.Error()
			}
			if !strings.Contains(msg, tc.text) {
				t.Errorf("%q does not hold %q", msg, tc.text)
			}
		})
	}
}
