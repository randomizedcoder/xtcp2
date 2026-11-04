package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// nexthopKeywordRow is one ipnh_modify argument line: the nhmsg and
// attributes it builds, or the error and the text it carries. The fake
// resolver knows d0 (3) and veth1 (5); its calls are listed.
type nexthopKeywordRow struct {
	description string
	family      uint8
	args        string
	nhm         []byte
	attrs       []byte
	resolved    []string
	err         error
	errMsg      string
}

func runNexthopKeywordRows(t *testing.T, rows []nexthopKeywordRow) {
	t.Helper()
	for i := range rows {
		tc := &rows[i]
		t.Run(tc.description, func(t *testing.T) {
			var calls []string
			resolve := func(name string) (int32, error) {
				calls = append(calls, name)
				switch name {
				case "d0":
					return 3, nil
				case "veth1":
					return 5, nil
				}
				return 0, nil
			}
			w, err := parseNexthopWriteArgs(tc.family, resolve, strings.Fields(tc.args))
			if strings.Join(calls, " ") != strings.Join(tc.resolved, " ") {
				t.Errorf("resolved %q, want %q", calls, tc.resolved)
			}
			if tc.err != nil {
				if !errors.Is(err, tc.err) || !strings.Contains(err.Error(), tc.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, tc.err, tc.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := w.nhm(); !bytes.Equal(got, tc.nhm) {
				t.Errorf("nhmsg % x, want % x", got, tc.nhm)
			}
			if got := w.attrs.Bytes(); !bytes.Equal(got, tc.attrs) {
				t.Errorf("attrs % x\nwant  % x", got, tc.attrs)
			}
		})
	}
}

// nhmOf is a struct nhmsg: family, scope 0, protocol, a reserved byte and
// flags.
func nhmOf(family, protocol uint8, flags uint32) []byte {
	return (&nexthopWrite{family: family, protocol: protocol, flags: flags}).nhm()
}

// TestNexthopWriteHwStats is ipnh_modify's hw_stats arm
// (ip/ipnexthop.c:1131): parse_on_off into NHA_HW_STATS_ENABLE u32, where the
// loop reads it. GW3-T01 in goip-write-keywords.md.
//
// go test ./internal/goip/ -run TestNexthopWriteHwStats
func TestNexthopWriteHwStats(t *testing.T) {
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	hw := func(v uint32) []byte { return fraOf(xtcpnl.NhaHwStatsEnable, u32(v)...) }
	id1 := fraOf(xtcpnl.NhaID, u32(1)...)
	grp21 := fraOf(xtcpnl.NhaGroup, append(u32(21), 0, 0, 0, 0)...)
	cat := func(a ...[]byte) []byte { return bytes.Join(a, nil) }
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "positive: on", args: "id 1 group 21 hw_stats on", nhm: nhmOf(0, 0, 0), attrs: cat(id1, grp21, hw(1))},
		{description: "positive: off", args: "id 1 group 21 hw_stats off", nhm: nhmOf(0, 0, 0), attrs: cat(id1, grp21, hw(0))},
		{description: "positive: before protocol", args: "group 21 hw_stats off protocol static",
			nhm: nhmOf(0, unix.RTPROT_STATIC, 0), attrs: cat(grp21, hw(0))},
		{description: "positive: the attribute's bytes", args: "hw_stats on", nhm: nhmOf(0, 0, 0),
			attrs: []byte{8, 0, 16, 0, 1, 0, 0, 0}},
		{description: "positive: after a dev", args: "id 10 dev d0 hw_stats on", nhm: nhmOf(unix.AF_INET, 0, 0),
			attrs: cat(fraOf(xtcpnl.NhaID, u32(10)...), fraOf(xtcpnl.NhaOIF, u32(3)...), hw(1)), resolved: []string{"d0"}},
		{description: "negative: a bad value", args: "hw_stats maybe", err: ErrInvalidArgument,
			errMsg: `Error: argument of "hw_stats" must be one of "off", "on", not "maybe"`},
		{description: "negative: no value", args: "id 1 hw_stats", err: ErrInvalidArgument, errMsg: "Command line is not complete"},
		{description: "corner: no prefix match", args: "hw_stat on", err: ErrInvalidArgument,
			errMsg: `Error: argument "hw_stat" is wrong: `},
		{description: "corner: on takes no prefix", args: "hw_stats o", err: ErrInvalidArgument,
			errMsg: `must be one of "off", "on", not "o"`},
		{description: "corner: twice is two attributes", args: "hw_stats on hw_stats off", nhm: nhmOf(0, 0, 0), attrs: cat(hw(1), hw(0))},
	})
}

// TestNexthopWriteDstPort is dst_port on iproute2 7.2.0, which has no such
// keyword: ipnh_modify's last arm, invarg("", *argv), refuses it, after any
// dev before it has been looked up. GW3-T02.
//
// go test ./internal/goip/ -run TestNexthopWriteDstPort
func TestNexthopWriteDstPort(t *testing.T) {
	const refused = `Error: argument "dst_port" is wrong: `
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "negative: dst_port", args: "id 31 fdb dst_port 4790", err: ErrInvalidArgument, errMsg: refused},
		{description: "negative: after a dev", args: "id 10 dev d0 dst_port 4789", resolved: []string{"d0"},
			err: ErrInvalidArgument, errMsg: refused},
		{description: "negative: dst_port is not ErrNotImplemented", args: "id 31 dst_port 4790", err: ErrInvalidArgument,
			errMsg: refused},
		{description: "boundary: port 0", args: "id 31 via 172.16.1.2 fdb dst_port 0", err: ErrInvalidArgument, errMsg: refused},
		{description: "corner: as the last word, refused before its value is read", args: "id 31 dst_port",
			err: ErrInvalidArgument, errMsg: refused},
		{description: "corner: a prefix of it is refused the same way", args: "dst_p 1", err: ErrInvalidArgument,
			errMsg: `Error: argument "dst_p" is wrong: `},
	})
}

// TestNexthopWriteEncapMpls is ipnh_modify's encap arm through
// lwt_parse_encap and parse_encap_mpls: NHA_ENCAP|NLA_F_NESTED holding
// MPLS_IPTUNNEL_DST and an optional MPLS_IPTUNNEL_TTL, then NHA_ENCAP_TYPE
// u16 1, where the loop reads it. GW3-T03.
//
// go test ./internal/goip/ -run TestNexthopWriteEncapMpls
func TestNexthopWriteEncapMpls(t *testing.T) {
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	be := func(v ...uint32) []byte {
		var b []byte
		for _, x := range v {
			b = binary.BigEndian.AppendUint32(b, x)
		}
		return b
	}
	enc := func(dst []byte, ttl ...byte) []byte {
		inner := fraOf(unix.MPLS_IPTUNNEL_DST, dst...)
		if len(ttl) > 0 {
			inner = append(inner, fraOf(unix.MPLS_IPTUNNEL_TTL, ttl...)...)
		}
		return append(fraOf(xtcpnl.NhaEncap|unix.NLA_F_NESTED, inner...), fraOf(xtcpnl.NhaEncapType, 1, 0)...)
	}
	cat := func(a ...[]byte) []byte { return bytes.Join(a, nil) }
	gw4 := fraOf(xtcpnl.NhaGateway, 172, 16, 1, 2)
	v4 := nhmOf(unix.AF_INET, 0, 0)
	addrErr := func(v string) string { return `Error: mpls address is expected rather than "` + v + `".` }
	// stack is n labels of 16, as words and as the entries mpls_pton writes.
	words := func(n int) string { return strings.TrimSuffix(strings.Repeat("16/", n), "/") }
	labels := func(n int) []uint32 {
		l := make([]uint32, n)
		for k := range l {
			l[k] = 16 << 12
		}
		l[n-1] |= 0x100
		return l
	}
	s64, l64, s65 := words(64), labels(64), words(65)
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "positive: one label, as captured", args: "id 51 encap mpls 101 via 172.16.1.2 dev veth1", nhm: v4,
			attrs:    cat(fraOf(xtcpnl.NhaID, u32(51)...), enc(be(0x00065100)), gw4, fraOf(xtcpnl.NhaOIF, u32(5)...)),
			resolved: []string{"veth1"}},
		{description: "positive: the encap's bytes", args: "encap mpls 101 via 172.16.1.2", nhm: v4,
			attrs: cat([]byte{0x0c, 0, 8, 0x80, 8, 0, 1, 0, 0, 6, 0x51, 0, 6, 0, 7, 0, 1, 0, 0, 0}, gw4)},
		{description: "positive: a stack, the bottom bit on the last only", args: "encap mpls 101/102 via 172.16.1.2", nhm: v4,
			attrs: cat(enc(be(0x00065000, 0x00066100)), gw4)},
		{description: "positive: ttl after the labels", args: "encap mpls 101 ttl 64 via 172.16.1.2", nhm: v4,
			attrs: cat(enc(be(0x00065100), 64), gw4)},
		{description: "positive: an IPv6 gateway, as captured", args: "id 52 encap mpls 102 via inet6 2001:db8:91::2 dev veth1",
			nhm: nhmOf(unix.AF_INET6, 0, 0),
			attrs: cat(fraOf(xtcpnl.NhaID, u32(52)...), enc(be(0x00066100)),
				fraOf(xtcpnl.NhaGateway, 0x20, 0x01, 0x0d, 0xb8, 0, 0x91, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2),
				fraOf(xtcpnl.NhaOIF, u32(5)...)),
			resolved: []string{"veth1"}},
		{description: "negative: a label at 2^20", args: "encap mpls 1048576 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: addrErr("1048576")},
		{description: "negative: an unknown type", args: "encap foo 1 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: `Error: argument "foo" is wrong: "encap type" value is invalid`},
		{description: "negative: the label last", args: "id 1 encap mpls 101", err: ErrInvalidArgument,
			errMsg: `Error: argument "encap type" is required`},
		{description: "negative: the type last", args: "id 1 encap mpls", err: ErrInvalidArgument, errMsg: "Command line is not complete"},
		{description: "negative: ttl twice", args: "encap mpls 101 ttl 1 ttl 2 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: `Error: either "ttl" is duplicate, or "2" is garbage`},
		{description: "negative: a bad ttl", args: "encap mpls 101 ttl 256 via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: `Error: argument "256" is wrong: "ttl" value is invalid`},
		{description: "negative: ttl with no value", args: "encap mpls 101 ttl", err: ErrInvalidArgument,
			errMsg: "Command line is not complete"},
		{description: "negative: a label with junk", args: "encap mpls 101x via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: addrErr("101x")},
		{description: "negative: an empty label after a slash", args: "encap mpls 101/ via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: addrErr("101/")},
		{description: "boundary: the largest label", args: "encap mpls 1048575 via 172.16.1.2", nhm: v4,
			attrs: cat(enc(be(0xfffff100)), gw4)},
		{description: "boundary: 64 labels", args: "encap mpls " + s64 + " via 172.16.1.2", nhm: v4,
			attrs: cat(enc(be(l64...)), gw4)},
		{description: "boundary: 65 labels", args: "encap mpls " + s65 + " via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: "Error: too many labels.\n" + addrErr(s65)},
		{description: "boundary: label 0", args: "encap mpls 0 via 172.16.1.2", nhm: v4, attrs: cat(enc(be(0x100)), gw4)},
		{description: "corner: default is no mpls address", args: "encap mpls default via 172.16.1.2", err: ErrInvalidArgument,
			errMsg: addrErr("default")},
		{description: "corner: hex, base 0", args: "encap mpls 0x10 via 172.16.1.2", nhm: v4, attrs: cat(enc(be(0x00010100)), gw4)},
		{description: "corner: bpf, which loads a program object", args: "encap bpf in obj.o via 172.16.1.2",
			err: ErrNotImplemented, errMsg: "encap bpf"},
		{description: "corner: another type last is missarg first", args: "encap ip id", err: ErrInvalidArgument,
			errMsg: `Error: argument "encap type" is required`},
		{description: "corner: ttl in base 0", args: "encap mpls 101 ttl 0x40 via 172.16.1.2", nhm: v4,
			attrs: cat(enc(be(0x00065100), 64), gw4)},
	})
}

// TestMplsPton is mpls_pton1 (lib/mpls_pton.c).
//
// go test ./internal/goip/ -run TestMplsPton
func TestMplsPton(t *testing.T) {
	type result struct {
		data    string
		tooMany bool
		ok      bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{description: "positive: one label", arg: "101", expected: result{"00065100", false, true}},
		{description: "positive: two", arg: "101/102", expected: result{"0006500000066100", false, true}},
		{description: "positive: octal in base 0", arg: "010", expected: result{"00008100", false, true}},
		{description: "negative: no digits", arg: "x", expected: result{"", false, false}},
		{description: "negative: empty", arg: "", expected: result{"", false, false}},
		{description: "negative: a trailing slash", arg: "1/", expected: result{"", false, false}},
		{description: "negative: a negative label is ULONG_MAX", arg: "-1", expected: result{"", false, false}},
		{description: "boundary: 2^20 - 1", arg: "1048575", expected: result{"fffff100", false, true}},
		{description: "boundary: 2^20", arg: "1048576", expected: result{"", false, false}},
		{description: "corner: a space before the digits, as strtoul skips", arg: " 5", expected: result{"00005100", false, true}},
		{description: "corner: a comma is junk", arg: "1,2", expected: result{"", false, false}},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			data, tooMany, ok := mplsPton(tt.arg)
			if got := (result{hex.EncodeToString(data), tooMany, ok}); got != tt.expected {
				t.Errorf("mplsPton(%q) = %+v, want %+v", tt.arg, got, tt.expected)
			}
		})
	}
}
