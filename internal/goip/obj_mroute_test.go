package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runMrouteCtx runs `ip mroute` args on a fresh context over src.
func runMrouteCtx(src Source, family uint8, stats, details int, json bool, args string) (string, string, error) {
	var out, errOut bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &errOut, family: family,
		showStats: stats, showDetails: details, json: json}
	err := runMroute(c, strings.Fields(args))
	return out.String(), errOut.String(), err
}

// TestMrouteMatchesIp replays the three mroute captures: goip sends
// ll_init_map's link dump, then ip's RTM_GETROUTE dump, byte for byte with
// seq and pid zeroed, and prints the sidecar.
//
// go test ./internal/goip/ -run TestMrouteMatchesIp
func TestMrouteMatchesIp(t *testing.T) {
	const (
		getlink  = unix.RTM_GETLINK
		getroute = unix.RTM_GETROUTE
	)
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		family      uint8
		args        string
		expected    []uint16 // request types, in order
	}{
		{"positive: the smcrouted (S,G) entry, v0 in, v1 out", "netlink_route_getroute_mroute.pcap",
			"ip_mroute", unix.AF_UNSPEC, "show", []uint16{getlink, getroute}},
		{"boundary: -6 dumps IP6MR, no table, and prints nothing", "netlink_route_getroute_mroute6.pcap",
			"ip_mroute6", unix.AF_INET6, "show", []uint16{getlink, getroute}},
		{"boundary: before smcrouted, nothing to print", "netlink_route_getroute_mroute_empty.pcap",
			"ip_mroute_empty", unix.AF_UNSPEC, "show", []uint16{getlink, getroute}},
		{"positive: no verb is the listing", "netlink_route_getroute_mroute.pcap",
			"ip_mroute", unix.AF_UNSPEC, "", []uint16{getlink, getroute}},
		{"positive: -4 is the default's request", "netlink_route_getroute_mroute.pcap",
			"ip_mroute", unix.AF_INET, "lst", []uint16{getlink, getroute}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := linkReadsDir + tc.pcap
			src := newStatsReplay(t, pcap)
			stdout, _, err := runMrouteCtx(src, tc.family, 0, 0, false, tc.args)
			if err != nil {
				t.Fatalf("runMroute: %v", err)
			}
			want, err := os.ReadFile(linkReadsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if stdout != string(want) {
				t.Errorf("stdout differs from %s\n got %q\nwant %q", tc.sidecar, stdout, want)
			}
			capt, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			ip := capt.Requests()
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], capturedRequest(ip[i])) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], capturedRequest(ip[i]))
				}
			}
		})
	}
}

// TestMrouteShowFilters runs the filters, -s, -d and -j over the populated
// capture's one entry. The replay answers every dump with the recorded
// replies, so a filter's effect is print_mroute's alone; `table all`'s
// request, which carries no RTA_TABLE, is TestParseMrouteArgs' to pin.
//
// go test ./internal/goip/ -run TestMrouteShowFilters
func TestMrouteShowFilters(t *testing.T) {
	const line = "(192.0.2.10,239.1.1.1)           Iif: v0         Oifs: v1  State: resolved"
	type result struct {
		stdout string
		err    error
	}
	tests := []struct {
		description string
		family      uint8
		stats       int
		details     int
		json        bool
		args        string
		expected    result
	}{
		{description: "positive: iif v0, the entry's", args: "show iif v0", expected: result{stdout: line + "\n"}},
		{description: "negative: iif v1 is not the entry's", args: "show iif v1"},
		{description: "negative: iif of no device", args: "show iif nope", expected: result{err: ErrNoSuchDevice}},
		{description: "positive: from the source", args: "show from 192.0.2.10", expected: result{stdout: line + "\n"}},
		{description: "positive: `f` is from, a /24 holding the source", args: "show f 192.0.2.0/24",
			expected: result{stdout: line + "\n"}},
		{description: "negative: from another source", args: "show from 192.0.2.11"},
		{description: "positive: to the group", args: "show to 239.1.1.1", expected: result{stdout: line + "\n"}},
		{description: "positive: a bare group prefix is to", args: "show 239.0.0.0/8", expected: result{stdout: line + "\n"}},
		{description: "negative: another group", args: "show 239.1.1.2"},
		{description: "boundary: from 0/0 keeps everything", args: "show from 0/0", expected: result{stdout: line + "\n"}},
		{description: "corner: a v6 group keeps nothing from a v4 entry", args: "show to ff0e::1"},
		{description: "positive: table all prints the table", args: "show table all",
			expected: result{stdout: line + " Table: default\n"}},
		{description: "positive: table default, the filter, prints none", args: "show table default",
			expected: result{stdout: line + "\n"}},
		{description: "negative: table main keeps nothing", args: "show table main"},
		{description: "positive: -s adds the counters and the age, 50 ticks", stats: 1, args: "show",
			expected: result{stdout: line + "\n  0 packets, 0 bytes, Age 0.50\n"}},
		{description: "corner: -d prints no table under the default filter", details: 1, args: "show",
			expected: result{stdout: line + "\n"}},
		{description: "positive: -j", json: true, args: "show", expected: result{stdout: `[{"src":"192.0.2.10",` +
			`"dst":"239.1.1.1","iif":"v0","multipath":[{"oif":"v1"}],"state":"resolved"}]` + "\n"}},
		{description: "positive: -j -s, the age a float", json: true, stats: 1, args: "show table all",
			expected: result{stdout: `[{"src":"192.0.2.10","dst":"239.1.1.1","iif":"v0","multipath":[{"oif":"v1"}],` +
				`"state":"resolved","packets":0,"bytes":0,"expires":0.5,"table":"default"}]` + "\n"}},
		{description: "boundary: -j with nothing kept is an empty array", json: true, args: "show iif v1",
			expected: result{stdout: "[]\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, linkReadsDir+"netlink_route_getroute_mroute.pcap")
			stdout, _, err := runMrouteCtx(src, tc.family, tc.stats, tc.details, tc.json, tc.args)
			got := result{stdout: stdout}
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				got.err = tc.expected.err
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestParseMrouteArgs is mroute_list's family selection and argument loop.
//
// go test ./internal/goip/ -run TestParseMrouteArgs
func TestParseMrouteArgs(t *testing.T) {
	type result struct {
		af      uint8
		table   uint32
		iif     string
		src     string
		dst     string
		skipped bool
		err     error
	}
	pfx := func(p inetPrefix) string {
		if p.bitlen <= 0 && p.family == unix.AF_UNSPEC {
			return ""
		}
		a, _ := netip.AddrFromSlice(p.data[:afBitLen(p.family)/8])
		return a.String() + "/" + strconv.Itoa(p.bitlen)
	}
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    result
	}{
		{description: "positive: the v4 default, IPMR and table default", args: "",
			expected: result{af: 128, table: 253}},
		{description: "positive: -6 is IP6MR with no table", family: unix.AF_INET6, args: "",
			expected: result{af: 129}},
		{description: "positive: table by number", args: "table 10", expected: result{af: 128, table: 10}},
		{description: "positive: `t` is table, main by name", args: "t main", expected: result{af: 128, table: 254}},
		{description: "positive: table all is no filter", args: "table all", expected: result{af: 128}},
		{description: "boundary: table 4294967295", args: "table 4294967295",
			expected: result{af: 128, table: 4294967295}},
		{description: "negative: table 4294967296", args: "table 4294967296", expected: result{err: ErrInvalidArgument}},
		{description: "negative: table bogus", args: "table bogus", expected: result{err: ErrInvalidArgument}},
		{description: "corner: table help", args: "table help", expected: result{err: ErrNotImplemented}},
		{description: "positive: iif, last one wins", args: "iif v0 iif v1", expected: result{af: 128, table: 253, iif: "v1"}},
		{description: "negative: `i` is no keyword but a bad destination", args: "i v0",
			expected: result{err: ErrInvalidArgument}},
		{description: "positive: from and to", args: "from 192.0.2.10 to 239.1.1.0/24",
			expected: result{af: 128, table: 253, src: "192.0.2.10/32", dst: "239.1.1.0/24"}},
		{description: "positive: a bare destination", args: "239.1.1.1",
			expected: result{af: 128, table: 253, dst: "239.1.1.1/32"}},
		{description: "positive: -6 takes a v6 group", family: unix.AF_INET6, args: "ff0e::1",
			expected: result{af: 129, dst: "ff0e::1/128"}},
		{description: "negative: -6 refuses a v4 group", family: unix.AF_INET6, args: "239.1.1.1",
			expected: result{err: ErrInvalidArgument}},
		{description: "negative: from a word", args: "from v0", expected: result{err: ErrInvalidArgument}},
		{description: "negative: from with no value", args: "from", expected: result{err: ErrInvalidArgument}},
		{description: "negative: to with no value", args: "to", expected: result{err: ErrInvalidArgument}},
		{description: "negative: iif with no value", args: "iif", expected: result{err: ErrInvalidArgument}},
		{description: "corner: help", args: "help", expected: result{err: ErrNotImplemented}},
		{description: "corner: to help", args: "to help", expected: result{err: ErrNotImplemented}},
		{description: "corner: -0 has no multicast routing, nothing read", family: unix.AF_PACKET, args: "bogus",
			expected: result{skipped: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f, ok, err := parseMrouteArgs(tc.family, strings.Fields(tc.args))
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				return
			}
			got := result{af: f.af, table: f.table, iif: f.iif, src: pfx(f.src), dst: pfx(f.dst), skipped: !ok}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// mrouteFake answers ll_init_map's link dump with one link, and the route
// dump with routes, or fails either.
type mrouteFake struct {
	routes    [][]byte
	failLinks bool
	failDump  bool
	sent      []uint16
}

func (m *mrouteFake) Dump(request []byte, _ uint16) ([][]byte, error) {
	typ := requestTypeOf(request)
	m.sent = append(m.sent, typ)
	switch {
	case typ == unix.RTM_GETLINK && m.failLinks, typ == unix.RTM_GETROUTE && m.failDump:
		return nil, unix.EINVAL
	case typ == unix.RTM_GETLINK:
		return [][]byte{newLinkBody(3, "d0")}, nil
	}
	return m.routes, nil
}

// mrouteBody is an rtmsg of family, type and flags, then RTA_TABLE, RTA_SRC
// when src is not nil, RTA_DST and, when iif is set, RTA_IIF.
func mrouteBody(family, typ uint8, flags uint32, table uint32, src, dst []byte, iif uint32) []byte {
	b := make([]byte, xtcpnl.RtMsgSizeCst)
	b[0], b[1], b[2], b[4], b[7] = family, 8*uint8(len(dst)), 8*uint8(len(src)), unix.RT_TABLE_DEFAULT, typ
	binary.LittleEndian.PutUint32(b[8:12], flags)
	attr := func(t uint16, v []byte) {
		a := make([]byte, 4, 4+len(v)+3)
		binary.LittleEndian.PutUint16(a[0:2], uint16(4+len(v)))
		binary.LittleEndian.PutUint16(a[2:4], t)
		a = append(a, v...)
		for len(a)%4 != 0 {
			a = append(a, 0)
		}
		b = append(b, a...)
	}
	u32 := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	attr(unix.RTA_TABLE, u32(table))
	if src != nil {
		attr(unix.RTA_SRC, src)
	}
	attr(unix.RTA_DST, dst)
	if iif != 0 {
		attr(unix.RTA_IIF, u32(iif))
	}
	return b
}

// TestMrouteShowConstructed is what no capture holds: an unresolved entry,
// offload, a v6 entry, a reply that is not multicast, and failed dumps.
//
// go test ./internal/goip/ -run TestMrouteShowConstructed
func TestMrouteShowConstructed(t *testing.T) {
	v4 := func(a, b, c, d byte) []byte { return []byte{a, b, c, d} }
	g6 := []byte{0xff, 0x0e, 15: 1}
	s6 := []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}
	type result struct {
		stdout string
		stderr string
		sent   int
		err    error
	}
	tests := []struct {
		description string
		family      uint8
		src         *mrouteFake
		args        string
		expected    result
	}{
		{description: "positive: unresolved, no iif, no oifs", src: &mrouteFake{routes: [][]byte{
			mrouteBody(128, unix.RTN_MULTICAST, unix.RTNH_F_UNRESOLVED, 253, v4(192, 0, 2, 9), v4(239, 1, 1, 9), 0)}},
			expected: result{stdout: "(192.0.2.9,239.1.1.9)            Iif: unresolved  State: unresolved\n", sent: 2}},
		{description: "positive: offload, iif cached", src: &mrouteFake{routes: [][]byte{
			mrouteBody(128, unix.RTN_MULTICAST, 8, 253, v4(192, 0, 2, 9), v4(239, 1, 1, 9), 3)}},
			expected: result{stdout: "(192.0.2.9,239.1.1.9)            Iif: d0          State: resolved offload\n", sent: 2}},
		{description: "corner: no RTA_SRC is unknown", src: &mrouteFake{routes: [][]byte{
			mrouteBody(128, unix.RTN_MULTICAST, 0, 253, nil, v4(239, 1, 1, 9), 3)}},
			expected: result{stdout: "(unknown,239.1.1.9)              Iif: d0          State: resolved\n", sent: 2}},
		{description: "corner: an uncached iif is ifN", src: &mrouteFake{routes: [][]byte{
			mrouteBody(128, unix.RTN_MULTICAST, 0, 253, v4(192, 0, 2, 9), v4(239, 1, 1, 9), 77)}},
			expected: result{stdout: "(192.0.2.9,239.1.1.9)            Iif: if77        State: resolved\n", sent: 2}},
		{description: "positive: a v6 entry, in table main under -6, prints no table", family: unix.AF_INET6,
			src:      &mrouteFake{routes: [][]byte{mrouteBody(129, unix.RTN_MULTICAST, 0, 254, s6, g6, 3)}},
			expected: result{stdout: "(2001:db8::1,ff0e::1)            Iif: d0          State: resolved\n", sent: 2}},
		{description: "positive: a v6 entry in table 10 prints the table", family: unix.AF_INET6,
			src:      &mrouteFake{routes: [][]byte{mrouteBody(129, unix.RTN_MULTICAST, 0, 10, s6, g6, 3)}},
			expected: result{stdout: "(2001:db8::1,ff0e::1)            Iif: d0          State: resolved Table: 10\n", sent: 2}},
		{description: "negative: an IP6MR reply to an IPMR dump is dropped", src: &mrouteFake{routes: [][]byte{
			mrouteBody(129, unix.RTN_MULTICAST, 0, 253, s6, g6, 3)}}, expected: result{sent: 2}},
		{description: "negative: a unicast reply ends the dump after the lines before it", src: &mrouteFake{routes: [][]byte{
			mrouteBody(128, unix.RTN_MULTICAST, 0, 253, v4(192, 0, 2, 9), v4(239, 1, 1, 9), 3),
			mrouteBody(128, unix.RTN_UNICAST, 0, 253, v4(192, 0, 2, 9), v4(10, 0, 0, 1), 3),
			mrouteBody(128, unix.RTN_MULTICAST, 0, 253, v4(192, 0, 2, 8), v4(239, 1, 1, 8), 3)}},
			expected: result{stdout: "(192.0.2.9,239.1.1.9)            Iif: d0          State: resolved\n",
				stderr: mrouteNonMcastCst + "\n", sent: 2, err: ErrDumpTerminated}},
		{description: "negative: a failed link dump is Dump terminated, no route dump",
			src: &mrouteFake{failLinks: true}, expected: result{sent: 1, err: ErrDumpTerminated}},
		{description: "negative: a failed route dump is Dump terminated",
			src: &mrouteFake{failDump: true}, expected: result{sent: 2, err: ErrDumpTerminated}},
		{description: "negative: iif of no device sends no route dump", src: &mrouteFake{}, args: "show iif nope",
			expected: result{sent: 1, err: ErrNoSuchDevice}},
		{description: "negative: a bad argument sends nothing", src: &mrouteFake{}, args: "show from v0",
			expected: result{err: ErrInvalidArgument}},
		{description: "corner: -0 sends nothing and prints nothing", family: unix.AF_PACKET, src: &mrouteFake{},
			args: "show"},
		{description: "negative: an unknown verb", src: &mrouteFake{}, args: "add",
			expected: result{err: ErrInvalidArgument}},
		{description: "corner: `h` is help", src: &mrouteFake{}, args: "h", expected: result{err: ErrNotImplemented}},
		{description: "positive: `s` is show", src: &mrouteFake{}, args: "s", expected: result{sent: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			stdout, stderr, err := runMrouteCtx(tc.src, tc.family, 0, 0, false, tc.args)
			got := result{stdout: stdout, stderr: stderr, sent: len(tc.src.sent)}
			if tc.expected.err != nil || err != nil {
				if !errors.Is(err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", err, tc.expected.err)
				}
				got.err = tc.expected.err
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
