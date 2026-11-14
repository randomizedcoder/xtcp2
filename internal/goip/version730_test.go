package goip

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// useIP730 runs a test as iproute2 7.3.0: every knob at its default. It
// is for the ws-ip730 corpus, and for rows about the 7.3.0 side of a knob.
// Not for a parallel test: the knobs are package state.
func useIP730(t *testing.T) {
	t.Helper()
	prevLL, prevKw := LLAddrStrict, Keywords730
	LLAddrStrict, Keywords730 = true, true
	t.Cleanup(func() { LLAddrStrict, Keywords730 = prevLL, prevKw })
}

// TestLLAddrA2NStrict is 7.3.0's ll_addr_a2n (5faf18f6, lib/ll_addr.c:64-85):
// each part from a hex digit to a ':' or the end, at most 255, no more parts
// than the buffer, and the whole argument named on a refusal.
//
// go test ./internal/goip/ -run TestLLAddrA2NStrict
func TestLLAddrA2NStrict(t *testing.T) {
	useIP730(t)
	type expected struct {
		addr []byte
		n    int
		bad  string
	}
	tests := []struct {
		description string
		arg         string
		expected    expected
	}{
		{"positive: an address", macvlanMAC1, expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"positive: an IPv4 address is four bytes", "10.0.0.1", expected{addr: []byte{10, 0, 0, 1}, n: 4}},
		{"positive: one-digit parts", "2:0:0:0:0:1", expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"positive: a 0x prefix is strtoul's", "0x2:0:0:0:0:1", expected{addr: []byte{2, 0, 0, 0, 0, 1}, n: 6}},
		{"boundary: one part short is silent", "02:00:00:00:01", expected{addr: []byte{2, 0, 0, 0, 1}, n: 5}},
		{"boundary: ff is the largest part", "ff:ff:ff:ff:ff:ff", expected{addr: []byte{255, 255, 255, 255, 255, 255}, n: 6}},
		{"negative: 1ff is past 255", "1ff", expected{n: -1, bad: "1ff"}},
		{"boundary: 100 is 256, one past ff", "02:100", expected{n: -1, bad: "02:100"}},
		{"negative: trailing garbage after a part, which sscanf skipped", "0aq", expected{n: -1, bad: "0aq"}},
		{"negative: a part that isn't hex names the whole argument", "00:11:zz", expected{n: -1, bad: "00:11:zz"}},
		{"negative: a dash is not a separator", "00-11", expected{n: -1, bad: "00-11"}},
		{"negative: garbage between two hex parts", "0aq1", expected{n: -1, bad: "0aq1"}},
		{"negative: a trailing colon is an empty part", "00:11:", expected{n: -1, bad: "00:11:"}},
		{"negative: an empty argument", "", expected{n: -1, bad: ""}},
		{"negative: a sign is not a hex digit", "+2:0:0:0:0:1", expected{n: -1, bad: "+2:0:0:0:0:1"}},
		{"negative: a leading space is not a hex digit", " 2", expected{n: -1, bad: " 2"}},
		{"negative: an IPv4 address that won't parse is named whole", "10.0.0.256", expected{n: -1, bad: "10.0.0.256"}},
		{"boundary: seven parts are refused, not read past the buffer", macvlanMAC1 + ":07", expected{n: -1, bad: macvlanMAC1 + ":07"}},
		{"corner: a word that starts hex is read up to its first non-hex byte", "bcqueuelen", expected{n: -1, bad: "bcqueuelen"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, n, bad := llAddrA2NPart(tc.arg, macvlanAddrLenCst)
			if !bytes.Equal(b, tc.expected.addr) || n != tc.expected.n || bad != tc.expected.bad {
				t.Errorf("got (% x, %d, %q), want (% x, %d, %q)", b, n, bad, tc.expected.addr, tc.expected.n, tc.expected.bad)
			}
		})
	}
}

// TestLLAddrCutStrict is the word PREV_ARG re-reads: 7.3.0 leaves argv
// whole, where 7.2.0 cut it at the first ':'.
//
// go test ./internal/goip/ -run TestLLAddrCutStrict
func TestLLAddrCutStrict(t *testing.T) {
	tests := []struct {
		description string
		strict      bool
		arg         string
		expected    string
	}{
		{description: "positive: 7.3.0 leaves the word whole", strict: true, arg: "00:11:zz", expected: "00:11:zz"},
		{description: "positive: 7.2.0 cuts at the first colon", strict: false, arg: "00:11:zz", expected: "00"},
		{description: "corner: 7.2.0 leaves an IPv4 address whole", strict: false, arg: "10.0.0.1", expected: "10.0.0.1"},
		{description: "corner: no colon, nothing to cut", strict: false, arg: "bcqueuelen", expected: "bcqueuelen"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := LLAddrStrict
			LLAddrStrict = tc.strict
			defer func() { LLAddrStrict = prev }()
			if got := llAddrCut(tc.arg); got != tc.expected {
				t.Errorf("llAddrCut(%q) = %q, want %q", tc.arg, got, tc.expected)
			}
		})
	}
}

// errKind is which of goip's refusals err is, or nil.
func errKind(err error) error {
	for _, e := range []error{ErrLinkUsage, ErrInvalidArgument, ErrNotImplemented} {
		if errors.Is(err, e) {
			return e
		}
	}
	return err
}

// TestNexthopDstPort is 7.3.0's `dst_port` (ad2b2d95): get_u16 base 0 as a
// be16 NHA_DST_PORT at its place, and before 7.3.0 the chain's last arm.
//
// go test ./internal/goip/ -run TestNexthopDstPort
func TestNexthopDstPort(t *testing.T) {
	type result struct {
		attrs string
		err   error
	}
	fdb := fraOf(xtcpnl.NhaFdb)
	port := func(b0, b1 byte) []byte { return fraOf(xtcpnl.NhaDstPort, b0, b1) }
	cat := func(a ...[]byte) string { return string(bytes.Join(a, nil)) }
	tests := []struct {
		description string
		ip730       bool
		args        string
		expected    result
	}{
		{"positive: 4789 in network order, after fdb", true, "fdb dst_port 4789", result{attrs: cat(fdb, port(0x12, 0xb5))}},
		{"positive: at its place, before fdb", true, "dst_port 4789 fdb", result{attrs: cat(port(0x12, 0xb5), fdb)}},
		{"positive: base 0 reads hex", true, "dst_port 0x12b5", result{attrs: cat(port(0x12, 0xb5))}},
		{"boundary: 0 is sent, for the kernel to judge", true, "dst_port 0", result{attrs: cat(port(0, 0))}},
		{"boundary: 65535", true, "dst_port 65535", result{attrs: cat(port(0xff, 0xff))}},
		{"negative: 65536 is past a u16", true, "dst_port 65536", result{err: ErrInvalidArgument}},
		{"negative: a word", true, "dst_port x", result{err: ErrInvalidArgument}},
		{"negative: a negative number", true, "dst_port -1", result{err: ErrInvalidArgument}},
		{"negative: no value", true, "dst_port", result{err: ErrInvalidArgument}},
		{"corner: strcmp, so a prefix is not the keyword", true, "dst_p 4789", result{err: ErrInvalidArgument}},
		{"negative: 7.2.0 has no dst_port", false, "fdb dst_port 4789", result{err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := Keywords730
			Keywords730 = tc.ip730
			defer func() { Keywords730 = prev }()
			w, err := parseNexthopWriteArgs(0, func(string) (int32, error) { return 0, nil }, strings.Fields(tc.args))
			var got result
			if err != nil {
				got.err = errKind(err)
			} else {
				got.attrs = string(w.attrs.Bytes())
			}
			if got != tc.expected {
				t.Errorf("got %x %v, want %x %v (%v)", got.attrs, got.err, tc.expected.attrs, tc.expected.err, err)
			}
		})
	}
}

// TestBondLacpStrict is 7.3.0's `lacp_strict` (30365d09): matches() after
// ad_actor_system, parse_on_off's two words, an IFLA_BOND_LACP_STRICT u8;
// before 7.3.0 an unknown command.
//
// go test ./internal/goip/ -run TestBondLacpStrict
func TestBondLacpStrict(t *testing.T) {
	type result struct {
		data string
		err  error
	}
	strict := func(v byte) string { return string(fraOf(xtcpnl.IflaBondLacpStrict, v)) }
	tests := []struct {
		description string
		ip730       bool
		args        string
		expected    result
	}{
		{"positive: on", true, "lacp_strict on", result{data: strict(1)}},
		{"positive: off", true, "lacp_strict off", result{data: strict(0)}},
		{"positive: matches() takes a prefix past lacp_rate's", true, "lacp_s on", result{data: strict(1)}},
		{"negative: parse_on_off takes no number", true, "lacp_strict 1", result{err: ErrInvalidArgument}},
		{"negative: nor a prefix of on", true, "lacp_strict o", result{err: ErrInvalidArgument}},
		{"corner: lacp_ is lacp_rate's, tried first, whose invarg refuses on", true, "lacp_ on", result{err: ErrLinkUsage}},
		{"negative: 7.2.0 has no lacp_strict", false, "lacp_strict on", result{err: ErrInvalidArgument}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := Keywords730
			Keywords730 = tc.ip730
			defer func() { Keywords730 = prev }()
			var got result
			settings, err := parseBondArgs(strings.Fields(tc.args), func(string) (uint32, error) { return 0, nil })
			if err == nil {
				var b []byte
				b, err = xtcpnl.EncodeBondInfoData(settings)
				got.data = string(b)
			}
			if err != nil {
				got = result{err: errKind(err)}
			}
			if got != tc.expected {
				t.Errorf("got %x %v, want %x %v (%v)", got.data, got.err, tc.expected.data, tc.expected.err, err)
			}
		})
	}
}

// TestSeg6EncapLookup is 7.3.0's seg6 `lookup` (144c45b6): once,
// rtnl_rttable_a2n and not table 0; before 7.3.0 the encap ends at it.
//
// go test ./internal/goip/ -run TestSeg6EncapLookup
func TestSeg6EncapLookup(t *testing.T) {
	type result struct {
		lookup uint32
		last   int // the index of the encap's last word
		err    error
	}
	tests := []struct {
		description string
		ip730       bool
		args        string
		expected    result
	}{
		{"positive: a number", true, "mode encap segs 2001:db8::1 lookup 100", result{lookup: 100, last: 5}},
		{"positive: main is 254", true, "mode encap segs 2001:db8::1 lookup main", result{lookup: 254, last: 5}},
		{"positive: before mode, in any order", true, "lookup 7 mode encap segs 2001:db8::1", result{lookup: 7, last: 5}},
		{"boundary: the largest table", true, "mode encap segs 2001:db8::1 lookup 4294967295", result{lookup: 4294967295, last: 5}},
		{"negative: table 0 is refused", true, "mode encap segs 2001:db8::1 lookup 0", result{err: ErrInvalidArgument}},
		{"negative: a name rt_tables lacks", true, "mode encap segs 2001:db8::1 lookup nosuch", result{err: ErrInvalidArgument}},
		{"negative: twice is duparg2", true, "mode encap segs 2001:db8::1 lookup 1 lookup 2", result{err: ErrInvalidArgument}},
		{"negative: no value", true, "mode encap segs 2001:db8::1 lookup", result{err: ErrInvalidArgument}},
		{"corner: absent is no table", true, "mode encap segs 2001:db8::1", result{last: 3}},
		{"corner: 7.2.0 ends the encap at lookup", false, "mode encap segs 2001:db8::1 lookup 100", result{last: 3}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := Keywords730
			Keywords730 = tc.ip730
			defer func() { Keywords730 = prev }()
			e, last, err := seg6EncapArgs(strings.Fields(tc.args), 0)
			var got result
			if err != nil {
				got.err = errKind(err)
			} else {
				got = result{lookup: e.Seg6.Lookup, last: last}
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v (%v)", got, tc.expected, err)
			}
		})
	}
}

// TestOvpnData is 7.3.0's ovpn_parse_opt (92f89d0e): `mode` by
// parse_one_of, one IFLA_OVPN_MODE per mode; before 7.3.0 there is no
// parse_opt and any argument is garbage.
//
// go test ./internal/goip/ -run TestOvpnData
func TestOvpnData(t *testing.T) {
	type result struct {
		data string
		warn bool
		err  error
		msg  string // the refusal's text, where a row names it
	}
	mode := func(v byte) string { return string(fraOf(xtcpnl.IflaOvpnMode, v)) }
	tests := []struct {
		description string
		ip730       bool
		args        string
		expected    result
	}{
		{"positive: p2p is 0", true, "mode p2p", result{data: mode(0)}},
		{"positive: mp is 1", true, "mode mp", result{data: mode(1)}},
		{"positive: a prefix matches, with the deprecation warning", true, "mode m", result{data: mode(1), warn: true}},
		{"corner: each mode is its own attribute", true, "mode p2p mode mp", result{data: mode(0) + mode(1)}},
		{"negative: a word not in the table", true, "mode x", result{err: ErrInvalidArgument}},
		{"negative: no value", true, "mode", result{err: ErrLinkUsage}},
		{"negative: an unknown option", true, "peer 1", result{err: ErrInvalidArgument, msg: `ovpn: unknown option "peer"?`}},
		{"negative: help is the usage", true, "help", result{err: ErrInvalidArgument, msg: "Usage: ... ovpn [ mode { p2p | mp } ]"}},
		{"negative: 7.2.0's garbage", false, "mode p2p", result{err: ErrInvalidArgument}},
		{"negative: 7.2.0's help is ip link help", false, "help", result{err: ErrNotImplemented}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prev := Keywords730
			Keywords730 = tc.ip730
			defer func() { Keywords730 = prev }()
			var warn bytes.Buffer
			b, err := ovpnData(&warn, strings.Fields(tc.args))
			got := result{data: string(b), warn: warn.Len() > 0}
			if err != nil {
				got = result{err: errKind(err)}
				if tc.expected.msg != "" && strings.Contains(err.Error(), tc.expected.msg) {
					got.msg = tc.expected.msg
				}
			}
			if got != tc.expected {
				t.Errorf("got %x %v %v, want %x %v %v (%v)", got.data, got.warn, got.err,
					tc.expected.data, tc.expected.warn, tc.expected.err, err)
			}
		})
	}
}
