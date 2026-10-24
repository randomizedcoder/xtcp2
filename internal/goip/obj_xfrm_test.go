package goip

import (
	"bytes"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tdXfrm is the C3 XFRM corpus.
const tdXfrm = "../../pkg/xtcpnl/testdata/7_1_4/link-write/xfrm"

// xfrmRun is one `goip [-4|-6] [-s] xfrm ARGS`.
type xfrmRun struct {
	stdout, stderr string
	err            error
}

// xfrmRunArgs runs `goip xfrm ARGS` over src, with one -s when stats.
func xfrmRunArgs(src Source, family uint8, stats bool, args string) xfrmRun {
	n := 0
	if stats {
		n = 1
	}
	return xfrmRunCount(src, family, n, args)
}

// xfrmRunCount runs `goip xfrm ARGS` over src, with stats -s's.
func xfrmRunCount(src Source, family uint8, stats int, args string) xfrmRun {
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.family, c.showStats = src, family, stats
	err := runXfrm(c, strings.Fields(args))
	return xfrmRun{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// TestXfrmStateMatchesIp replays the nine C3 state read captures, and other
// spellings that send the same bytes. A replay answers only a request whose
// bytes are ip's, seq aside, so every exchange replayed means goip sent
// ip's requests; stdout must be the capture's output and the exit status
// ip's.
//
// go test ./internal/goip/ -run TestXfrmStateMatchesIp
func TestXfrmStateMatchesIp(t *testing.T) {
	const get = "state get src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100"
	tests := []struct {
		description string
		row         string
		args        string
		stats       bool
		exit        int
	}{
		{description: "boundary: list with none", row: "state_list_empty", args: "state list"},
		{description: "positive: list, two SAs, the newest first", row: "state_list", args: "state list"},
		{description: "positive: list dst, the address filter and the client's", row: "state_list_dst",
			args: "state list dst 192.0.2.2"},
		{description: "positive: list nokeys, the keys hidden", row: "state_list_nokeys", args: "state list nokeys"},
		{description: "positive: get, one SA by its id", row: "state_get", args: get},
		{description: "negative: get of a missing SA, the kernel's ESRCH, exit 2", row: "state_get_missing",
			args: get, exit: ExitFailure},
		{description: "positive: count", row: "state_count", args: "state count"},
		{description: "boundary: count with none", row: "state_count_empty", args: "state count"},
		{description: "positive: -s count, the hash table's buckets", row: "state_count_stats", args: "state count",
			stats: true},
		{description: "corner: no verb is list", row: "state_list", args: "state"},
		{description: "corner: sa is state, l is list", row: "state_list", args: "sa l"},
		{description: "corner: show", row: "state_list", args: "state show"},
		{description: "corner: lst, and s is show", row: "state_list", args: "s lst"},
		{description: "corner: an address's /32 is its default bit length", row: "state_list_dst",
			args: "state list dst 192.0.2.2/32"},
		{description: "corner: g is get, the id's words in another order, the SPI decimal", row: "state_get",
			args: "state g spi 256 proto esp dst 192.0.2.2 src 192.0.2.1"},
		{description: "corner: c is count, a word after it unread", row: "state_count", args: "state c bogus"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src, err := OpenReplay(filepath.Join(tdXfrm, tc.row, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			r := xfrmRunArgs(src, unix.AF_UNSPEC, tc.stats, tc.args)
			if code := ioamExit(r.err); code != tc.exit {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if want := mustReadFile(t, filepath.Join(tdXfrm, tc.row, "ip.out")); r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
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

// xfrmFake is an XFRM source that records each request and answers a
// dump with its messages or its error, and a talk with its answer.
type xfrmFake struct {
	dump     []xfrmMsg
	dumpErr  error
	talkTyp  uint16
	talkBody []byte
	talkErr  error
	reqs     [][]byte
}

func (f *xfrmFake) Dump([]byte, uint16) ([][]byte, error) { return nil, nil }

func (f *xfrmFake) Talk([]byte, uint16) ([]byte, error) { return nil, unix.ENODEV }

func (f *xfrmFake) XfrmDump(req []byte) ([]xfrmMsg, error) {
	f.reqs = append(f.reqs, req)
	return f.dump, f.dumpErr
}

func (f *xfrmFake) XfrmTalk(req []byte) (uint16, []byte, error) {
	f.reqs = append(f.reqs, req)
	return f.talkTyp, f.talkBody, f.talkErr
}

// xfrmCaptureSAs are state_list's two NEWSAs, the IPv6 tunnel SA (reqid 2)
// and the IPv4 transport one (reqid 1).
func xfrmCaptureSAs(t *testing.T) []xfrmMsg {
	t.Helper()
	src, err := OpenReplay(filepath.Join(tdXfrm, "state_list", "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	var out []xfrmMsg
	for _, m := range src.xseg.Txns[0].Replies {
		if m.Hdr.Type == xtcpnl.XfrmMsgNewSA {
			out = append(out, xfrmMsg{typ: m.Hdr.Type, flags: m.Hdr.Flags, body: m.Body})
		}
	}
	if len(out) != 2 {
		t.Fatalf("%d SAs in state_list's capture, want 2", len(out))
	}
	return out
}

// TestXfrmStateArgs runs the state reads' parsing, requests, filters and
// refusals over a fake source answering with state_list's two SAs: the exit
// status, the requests' bytes after their nlmsghdr, "-" for none, which of
// the two SAs printed, by reqid, and a fragment of the error or stderr.
//
// go test ./internal/goip/ -run TestXfrmStateArgs
func TestXfrmStateArgs(t *testing.T) {
	const (
		// filter0 is XFRMA_ADDRESS_FILTER with nothing in it.
		filter0 = "28001a00" + "00000000000000000000000000000000" + "00000000000000000000000000000000" + "00000000"
		esp     = "05001900" + "32000000"
		// sa1Get is get's xfrm_usersa_id for 192.0.2.2, SPI 0x100, AF_INET,
		// ESP, and XFRMA_SRCADDR 192.0.2.1.
		sa1Get = "c0000202000000000000000000000000" + "00000100" + "0200" + "32" + "00" + "14000d00" +
			"c0000201000000000000000000000000"
	)
	sas := xfrmCaptureSAs(t)
	short := xfrmMsg{typ: xtcpnl.XfrmMsgNewSA, body: make([]byte, 100)}
	other := xfrmMsg{typ: xtcpnl.XfrmMsgNewPolicy, flags: unix.NLM_F_MULTI, body: make([]byte, 8)}
	withProto := append([]byte(nil), sas[1].body...)
	withProto[44] = 200
	withTCP := append([]byte(nil), sas[1].body...)
	withTCP[44] = unix.IPPROTO_TCP
	tests := []struct {
		description string
		args        string
		family      uint8
		dump        []xfrmMsg
		talkTyp     uint16
		talkBody    []byte
		talkErr     error
		exit        int
		reqs        string // the requests' bodies, space-separated; "-" for none
		printed     string // the reqids printed, in order
		text        string // in the error, or in stderr
	}{
		{description: "positive: list, both", args: "state list", dump: sas, reqs: filter0, printed: "2 1"},
		{description: "positive: list proto esp, XFRMA_PROTO first", args: "state list proto esp", dump: sas,
			reqs: esp + filter0, printed: "2 1"},
		{description: "positive: list src with a prefix length, the filter's saddr and splen", dump: sas,
			args:    "state list src 192.0.2.0/24",
			reqs:    "28001a00" + "c0000200000000000000000000000000" + "00000000000000000000000000000000" + "02001800",
			printed: "1"},
		{description: "positive: -4 list, the family in the filter, the IPv6 SA dropped", args: "state list",
			family: unix.AF_INET, dump: sas, reqs: filter0[:len(filter0)-8] + "02000000", printed: "1"},
		{description: "positive: mode tunnel, the client's alone", args: "state list mode tunnel", dump: sas,
			reqs: filter0, printed: "2"},
		{description: "positive: mode by prefix", args: "state list mode tr", dump: sas, reqs: filter0, printed: "1"},
		{description: "positive: reqid 1", args: "state list reqid 1", dump: sas, reqs: filter0, printed: "1"},
		{description: "positive: spi 0x101", args: "state list spi 0x101", dump: sas, reqs: filter0, printed: "2"},
		{description: "negative: flag noecn, neither", args: "state list flag noecn", dump: sas, reqs: filter0},
		{description: "corner: flag 0x0 matches no SA, flags & 0 being 0", args: "state list flag 0x0", dump: sas,
			reqs: filter0},
		{description: "corner: flag then a word not a flag, read as the id, the flag mask still matching none",
			args: "state list flag src 192.0.2.1", dump: sas, reqs: "28001a00" + "c0000201000000000000000000000000" +
				"00000000000000000000000000000000" + "02002000"},
		{description: "negative: a dump answer of another type is not a state", args: "state list",
			dump: []xfrmMsg{other, sas[1]}, reqs: filter0, printed: "1",
			text: "Not a state: 00000018 00000013 00000002"},
		{description: "negative: an SA short of its struct ends the dump, exit 1", args: "state list",
			dump: []xfrmMsg{sas[0], short}, reqs: filter0, printed: "2", exit: ExitUsage, text: "wrong nlmsg len"},
		{description: "corner: a selector with a protocol goip cannot name is not rendered", args: "state list",
			dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewSA, body: withProto}}, reqs: filter0, exit: ExitUsage,
			text: "getprotobynumber"},
		{description: "positive: a selector with a protocol goip names", args: "state list",
			dump: []xfrmMsg{{typ: xtcpnl.XfrmMsgNewSA, body: withTCP}}, reqs: filter0, printed: "1"},
		{description: "negative: an unknown word, read twice as the id", args: "state list bogus", exit: ExitMinusOne,
			reqs: "-", text: `"bogus" is wrong: unknown`},
		{description: "negative: a second id", args: "state list src 192.0.2.1 nokeys dst 192.0.2.2",
			exit: ExitMinusOne, reqs: "-", text: `"dst" is wrong: unknown`},
		{description: "negative: mode with no value", args: "state list mode", exit: ExitMinusOne, reqs: "-",
			text: "not complete"},
		{description: "negative: a mode it does not know", args: "state list mode x", exit: ExitMinusOne, reqs: "-",
			text: "MODE value is invalid"},
		{description: "negative: a reqid not a number", args: "state list reqid x", exit: ExitMinusOne, reqs: "-",
			text: "REQID value is invalid"},
		{description: "negative: a 0x flag not a byte", args: "state list flag 0x100", exit: ExitMinusOne, reqs: "-",
			text: "FLAG value is invalid"},
		{description: "negative: src any has no family", args: "state list src any", exit: ExitMinusOne, reqs: "-",
			text: "unrecognized address family"},
		{description: "negative: src not an address is get_prefix's, exit 1", args: "state list src bogus",
			exit: ExitUsage, reqs: "-", text: "prefix is expected"},
		{description: "negative: two families", args: "state list src 192.0.2.1 dst 2001:db8::2", exit: ExitMinusOne,
			reqs: "-", text: "the same address family is required"},
		{description: "negative: an SPI on route2, exit 1", args: "state list proto route2 spi 1", exit: ExitUsage,
			reqs: "-", text: `"spi" is invalid with XFRM-PROTO value "route2"`},
		{description: "boundary: an IPComp SPI of 0x10000, exit 1", args: "state list proto comp spi 0x10000",
			exit: ExitUsage, reqs: "-", text: "SPI value is too large"},
		{description: "boundary: an IPComp SPI of 0xffff", args: "state list proto comp spi 0xffff", dump: sas,
			reqs: "05001900" + "6c000000" + filter0},
		{description: "negative: a protocol it does not know", args: "state list proto tcp", exit: ExitMinusOne,
			reqs: "-", text: "XFRM-PROTO value is invalid"},
		{description: "negative: a refused dump, exit 1", args: "state list", talkErr: nil, reqs: filter0,
			dump: nil, exit: ExitUsage, text: "dump terminated"},
		{description: "positive: get, its id and source", args: "state get src 192.0.2.1 dst 192.0.2.2 proto esp " +
			"spi 0x100", talkTyp: xtcpnl.XfrmMsgNewSA, talkBody: sas[1].body, reqs: sa1Get, printed: "1"},
		{description: "positive: get with a mark and mask, XFRMA_MARK after the source", talkTyp: xtcpnl.XfrmMsgNewSA,
			args: "state get mark 5 mask 0xf src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100", talkBody: sas[1].body,
			reqs: sa1Get + "0c001500" + "05000000" + "0f000000", printed: "1"},
		{description: "corner: a mark last takes an all-ones mask", talkTyp: xtcpnl.XfrmMsgNewSA,
			args: "state get src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100 mark 5", talkBody: sas[1].body,
			reqs: sa1Get + "0c001500" + "05000000" + "ffffffff", printed: "1"},
		{description: "corner: a mark of 0 is not sent", talkTyp: xtcpnl.XfrmMsgNewSA,
			args: "state get mark 0 src 192.0.2.1 dst 192.0.2.2 proto esp spi 0x100", talkBody: sas[1].body,
			reqs: sa1Get, printed: "1"},
		{description: "corner: get with nothing still asks, AF_INET, no source", talkTyp: xtcpnl.XfrmMsgNewSA,
			args: "state get", talkBody: sas[1].body,
			reqs: "00000000000000000000000000000000" + "00000000" + "0200" + "00" + "00", printed: "1"},
		{description: "negative: get with no protocol is missarg", args: "state get dst 192.0.2.2",
			exit: ExitMinusOne, reqs: "-", text: `"XFRM-PROTO" is required`},
		{description: "negative: a mark not a number", args: "state get mark x", exit: ExitMinusOne, reqs: "-",
			text: "MARK value is invalid"},
		{description: "negative: get refused by the kernel, exit 2", args: "state get proto esp",
			talkErr: unix.ESRCH, exit: ExitFailure, reqs: "00000000000000000000000000000000" + "00000000" + "0200" +
				"32" + "00" + "14000d00" + "00000000000000000000000000000000"},
		{description: "negative: an answer not a state", args: "state get proto esp",
			talkTyp: xtcpnl.XfrmMsgNewPolicy, talkBody: make([]byte, 8),
			reqs: "00000000000000000000000000000000" + "00000000" + "0200" + "32" + "00" + "14000d00" +
				"00000000000000000000000000000000", text: "Not a state"},
		{description: "negative: an answer short of its struct, An error :-), exit 1", args: "state get proto esp",
			talkTyp: xtcpnl.XfrmMsgNewSA, talkBody: make([]byte, 8), exit: ExitUsage,
			reqs: "00000000000000000000000000000000" + "00000000" + "0200" + "32" + "00" + "14000d00" +
				"00000000000000000000000000000000", text: "An error :-)"},
		{description: "positive: count asks for every field", args: "state count", talkTyp: xtcpnl.XfrmMsgNewSADInfo,
			talkBody: []byte{0xff, 0xff, 0xff, 0xff, 8, 0, 1, 0, 3, 0, 0, 0}, reqs: "ffffffff"},
		{description: "negative: count's answer with no count, printed, exit 0", args: "state count",
			talkTyp: xtcpnl.XfrmMsgNewSADInfo, talkBody: []byte{0xff, 0xff, 0xff, 0xff}, reqs: "ffffffff"},
		{description: "negative: count refused, exit 2", args: "state count", talkErr: unix.EPERM, exit: ExitFailure,
			reqs: "ffffffff"},
		{description: "negative: state help", args: "state help", exit: ExitMinusOne, reqs: "-",
			text: "Usage: ip xfrm state"},
		{description: "negative: a state verb it does not know", args: "state bogus", exit: ExitMinusOne, reqs: "-",
			text: `Command "bogus" is unknown`},
		{description: "negative: d is delete, a write, which needs GOIP_WRITE_OUT", args: "state d", exit: ExitUsage,
			reqs: "-", text: "GOIP_WRITE_OUT"},
		{description: "negative: flush, a write, which needs GOIP_WRITE_OUT", args: "state flush", exit: ExitUsage,
			reqs: "-", text: "GOIP_WRITE_OUT"},
		{description: "negative: policy add reaches the policy writes, DIR required", args: "policy add",
			exit: ExitUsage, reqs: "-", text: "DIR is required"},
		{description: "negative: monitor, G7-T05's", args: "mon", exit: ExitUsage, reqs: "-", text: "not implemented"},
		{description: "negative: no object is usage", args: "", exit: ExitMinusOne, reqs: "-",
			text: "Usage: ip xfrm XFRM-OBJECT"},
		{description: "negative: help is usage", args: "help", exit: ExitMinusOne, reqs: "-", text: "XFRM-OBJECT"},
		{description: "negative: an object it does not know is usage", args: "bogus", exit: ExitMinusOne, reqs: "-",
			text: "XFRM-OBJECT"},
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
				reqs = append(reqs, hex.EncodeToString(q[xtcpnl.NlMsgHdrSizeCst:]))
			}
			got := strings.Join(reqs, " ")
			if got == "" {
				got = "-"
			}
			if got != tc.reqs {
				t.Errorf("requests\n got %s\nwant %s", got, tc.reqs)
			}
			if p := xfrmPrinted(r.stdout); p != tc.printed {
				t.Errorf("printed reqids %q, want %q\n%s", p, tc.printed, r.stdout)
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

// xfrmPrinted is the reqids of the SAs a list printed, in order.
func xfrmPrinted(out string) string {
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "reqid "); i >= 0 {
			ids = append(ids, strings.Fields(line[i:])[1])
		}
	}
	return strings.Join(ids, " ")
}
