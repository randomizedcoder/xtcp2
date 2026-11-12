package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ipstatsNames is each enabled leaf as its header names it.
func ipstatsNames(enabled []ipstatsEnabled) []string {
	out := make([]string, 0, len(enabled))
	for _, e := range enabled {
		out = append(out, strings.Join(slices.DeleteFunc(slices.Clone(e.sel[:]), func(s string) bool { return s == "" }), " "))
	}
	return out
}

// ipstatsAllCst is the bare show's thirteen leaves in print order, which
// pkg/xtcpnl's wsStatsOrder pins from the captures.
var ipstatsAllCst = []string{
	"afstats mpls", "link", "offload l3_stats", "offload hw_stats_info", "offload cpu_hit",
	"xstats_slave bond 802.3ad", "xstats bond 802.3ad",
	"xstats_slave bridge vlan", "xstats_slave bridge mcast", "xstats_slave bridge stp",
	"xstats bridge vlan", "xstats bridge mcast", "xstats bridge stp",
}

// TestIpstatsShowArgs is ipstats_show's argument loop: what each selection
// enables, in print order, and how each refusal exits.
//
// go test ./internal/goip/ -run TestIpstatsShowArgs
func TestIpstatsShowArgs(t *testing.T) {
	type result struct {
		enabled []string
		dev     string
		help    bool
		exit    int // exitCodeOf the refusal, 0 for none
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: no selection is every leaf", "", result{enabled: ipstatsAllCst}},
		{"positive: a leaf group", "group link", result{enabled: []string{"link"}}},
		{"positive: a group is its leaves, in print order", "group offload",
			result{enabled: []string{"offload l3_stats", "offload hw_stats_info", "offload cpu_hit"}}},
		{"positive: a suite", "group xstats subgroup bridge suite stp",
			result{enabled: []string{"xstats bridge stp"}}},
		{"positive: a subgroup's suites", "group xstats_slave subgroup bridge",
			result{enabled: []string{"xstats_slave bridge vlan", "xstats_slave bridge mcast", "xstats_slave bridge stp"}}},
		{"positive: two groups, sorted, not in argument order", "group link group afstats",
			result{enabled: []string{"afstats mpls", "link"}}},
		{"positive: two subgroups of one group", "group offload subgroup cpu_hit subgroup l3_stats",
			result{enabled: []string{"offload l3_stats", "offload cpu_hit"}}},
		{"boundary: a leaf twice is enabled once", "group link group link", result{enabled: []string{"link"}}},
		{"boundary: a suite then its whole group is the group once", "group xstats subgroup bond suite 802.3ad group xstats",
			result{enabled: []string{"xstats bond 802.3ad", "xstats bridge vlan", "xstats bridge mcast", "xstats bridge stp"}}},
		{"positive: dev with no selection", "dev d0", result{enabled: ipstatsAllCst, dev: "d0"}},
		{"positive: dev after the selection", "group link dev d0", result{enabled: []string{"link"}, dev: "d0"}},
		{"boundary: a 15-byte dev", "dev abcdefghijklmno group link", result{enabled: []string{"link"}, dev: "abcdefghijklmno"}},
		{"corner: dev takes help as its name", "dev help group link", result{enabled: []string{"link"}, dev: "help"}},
		{"corner: help ends the loop, whatever came before", "group nosuch help", result{help: true}},
		{"negative: an unknown word", "nosuch", result{exit: ExitEINVAL}},
		{"negative: an abbreviated keyword, strcmp", "gr link", result{exit: ExitEINVAL}},
		{"negative: a subgroup with no group", "subgroup l3_stats", result{exit: ExitEINVAL}},
		{"negative: a suite with no subgroup", "group xstats suite bond", result{exit: ExitEINVAL}},
		{"negative: a subgroup inside the link leaf", "group link subgroup x", result{exit: ExitEINVAL}},
		{"negative: a group no level holds", "group nosuch", result{exit: ExitEINVAL}},
		{"negative: a subgroup the group lacks", "group offload subgroup nosuch", result{exit: ExitEINVAL}},
		{"corner: a bad group is found when the next one pushes it", "group nosuch group link", result{exit: ExitEINVAL}},
		{"corner: a bad group is found at the end, after dev", "group nosuch dev d0", result{exit: ExitEINVAL}},
		{"corner: `group subgroup x` is group \"subgroup\", then subgroup x", "group subgroup x", result{exit: ExitEINVAL}},
		{"negative: group with no name, NEXT_ARG", "group", result{exit: ExitMinusOne}},
		{"negative: dev with no name", "group link dev", result{exit: ExitMinusOne}},
		{"negative: dev twice, duparg2", "dev d0 dev d1", result{exit: ExitMinusOne}},
		{"negative: a 16-byte dev, invarg", "dev abcdefghijklmnop", result{exit: ExitMinusOne}},
		{"negative: a dev with a colon", "dev d0:1", result{exit: ExitMinusOne}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			enabled, dev, help, err := ipstatsShowArgs(strings.Fields(tc.args))
			var got result
			if err != nil {
				got.exit = exitCodeOf(err)
			} else {
				got = result{enabled: ipstatsNames(enabled), dev: dev, help: help}
			}
			if !slices.Equal(got.enabled, tc.expected.enabled) || got.dev != tc.expected.dev ||
				got.help != tc.expected.help || got.exit != tc.expected.exit {
				t.Errorf("got %+v (%v), want %+v", got, err, tc.expected)
			}
		})
	}
}

// TestIpstatsFilter is ipstats_req_add_filters: the top-level mask from
// every enabled leaf, and an offload sub-mask only when it narrows the
// group.
//
// go test ./internal/goip/ -run TestIpstatsFilter
func TestIpstatsFilter(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    xtcpnl.StatsFilter
	}{
		{"positive: the bare show asks for all five groups", "", xtcpnl.StatsFilter{Mask: 0x1f}},
		{"positive: link", "group link", xtcpnl.StatsFilter{Mask: 0x1}},
		{"positive: xstats, whose suites set no sub-mask", "group xstats subgroup bridge suite stp", xtcpnl.StatsFilter{Mask: 0x2}},
		{"positive: xstats_slave", "group xstats_slave", xtcpnl.StatsFilter{Mask: 0x4}},
		{"positive: afstats", "group afstats", xtcpnl.StatsFilter{Mask: 0x10}},
		{"boundary: all of offload is no sub-mask", "group offload", xtcpnl.StatsFilter{Mask: 0x8}},
		{"positive: cpu_hit", "group offload subgroup cpu_hit", xtcpnl.StatsFilter{Mask: 0x8, Offload: 0x1}},
		{"positive: hw_stats_info", "group offload subgroup hw_stats_info", xtcpnl.StatsFilter{Mask: 0x8, Offload: 0x2}},
		{"positive: l3_stats packs hw_stats_info too", "group offload subgroup l3_stats", xtcpnl.StatsFilter{Mask: 0x8, Offload: 0x6}},
		{"boundary: cpu_hit and l3_stats make the full sub-mask", "group offload subgroup cpu_hit subgroup l3_stats",
			xtcpnl.StatsFilter{Mask: 0x8}},
		{"corner: two groups", "group link group offload subgroup cpu_hit", xtcpnl.StatsFilter{Mask: 0x9, Offload: 0x1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			enabled, _, _, err := ipstatsShowArgs(strings.Fields(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if got := ipstatsFilter(enabled); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestIpstatsHelp is do_help, generated from the tree as ip generates it.
//
// go test ./internal/goip/ -run TestIpstatsHelp
func TestIpstatsHelp(t *testing.T) {
	expected := "Usage: ip stats help\n" +
		"       ip stats show [ dev DEV ] [ group GROUP [ subgroup SUBGROUP [ suite SUITE ] ... ] ... ] ...\n" +
		"       ip stats set dev DEV l3_stats { on | off }\n" +
		"GROUP := { link | xstats | xstats_slave | offload | afstats }\n" +
		"xstats SUBGROUP := { bridge [ suite stp ] [ suite mcast ] [ suite vlan ] | bond [ suite 802.3ad ] }\n" +
		"xstats_slave SUBGROUP := { bridge [ suite stp ] [ suite mcast ] [ suite vlan ] | bond [ suite 802.3ad ] }\n" +
		"offload SUBGROUP := { cpu_hit | hw_stats_info | l3_stats }\n" +
		"afstats SUBGROUP := { mpls }\n"
	tests := []struct {
		description string
		args        []string
		expected    string // stderr; stdout stays empty and the exit 0
	}{
		{"positive: stats help", []string{"help"}, expected},
		{"positive: stats show help", []string{"show", "help"}, expected},
		{"corner: help after a selection", []string{"show", "group", "link", "help"}, expected},
		{"corner: stats set help", []string{"set", "help"}, expected},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errOut bytes.Buffer
			c := &runCtx{out: &out, errOut: &errOut, lltab: NewLLTab()}
			err := runStats(c, tc.args)
			skipIfNotCompiled(t, err)
			if err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 || errOut.String() != tc.expected {
				t.Errorf("stdout %q, stderr %q", out.String(), errOut.String())
			}
		})
	}
}

// TestRunStatsVerbs is do_ipstats' verbs by strcmp.
//
// go test ./internal/goip/ -run TestRunStatsVerbs
func TestRunStatsVerbs(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    int
	}{
		{"negative: an unknown command is -1, exit 1", []string{"list"}, ExitUsage},
		{"negative: show does not abbreviate", []string{"sh"}, ExitUsage},
		{"negative: set does not abbreviate", []string{"se", "dev", "d0"}, ExitUsage},
		{"corner: a refusal under show exits as ipstats_show's", []string{"show", "nosuch"}, ExitEINVAL},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := &runCtx{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, lltab: NewLLTab()}
			err := runStats(c, tc.args)
			if err == nil || exitCodeOf(err) != tc.expected {
				t.Errorf("err %v, exit %d, want %d", err, exitCodeOf(err), tc.expected)
			}
		})
	}
}

// rtattrBytes is one attribute, its length and type, then val padded.
func rtattrBytes(typ uint16, val []byte) []byte {
	b := make([]byte, unix.SizeofRtAttr, unix.SizeofRtAttr+len(val)+3)
	binary.NativeEndian.PutUint16(b[0:2], uint16(unix.SizeofRtAttr+len(val)))
	binary.NativeEndian.PutUint16(b[2:4], typ)
	b = append(b, val...)
	for len(b)%unix.RTA_ALIGNTO != 0 {
		b = append(b, 0)
	}
	return b
}

// TestRtaTable is parse_rtattr and parse_rtattr_nested.
//
// go test ./internal/goip/ -run TestRtaTable
func TestRtaTable(t *testing.T) {
	a1, a1b, a2 := rtattrBytes(1, []byte{1}), rtattrBytes(1, []byte{9}), rtattrBytes(2, []byte{2, 2})
	nested2 := rtattrBytes(2|unix.NLA_F_NESTED, []byte{7})
	tests := []struct {
		description string
		data        []byte
		maxType     int
		nested      bool
		expected    [][]byte
	}{
		{"positive: each type at its index", slices.Concat(a1, a2), 2, false, [][]byte{nil, {1}, {2, 2}}},
		{"positive: the first of a duplicate wins", slices.Concat(a1, a1b), 2, false, [][]byte{nil, {1}, nil}},
		{"boundary: a type above max is dropped", slices.Concat(a1, a2), 1, false, [][]byte{nil, {1}}},
		{"boundary: an empty payload is present", rtattrBytes(1, nil), 1, false, [][]byte{nil, {}}},
		{"positive: nested masks NLA_F_NESTED", nested2, 2, true, [][]byte{nil, nil, {7}}},
		{"negative: unnested keeps it, out of range", nested2, 2, false, [][]byte{nil, nil, nil}},
		{"negative: a length past the data ends the walk", slices.Concat(a1, []byte{99, 0, 2, 0}), 2, false, [][]byte{nil, {1}, nil}},
		{"negative: a length under the header ends the walk", slices.Concat([]byte{2, 0, 1, 0}, a2), 2, false, [][]byte{nil, nil, nil}},
		{"corner: a trailing fragment is ignored", slices.Concat(a2, []byte{1, 2}), 2, false, [][]byte{nil, nil, {2, 2}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := rtaTable(tc.data, tc.maxType, tc.nested)
			if !slices.EqualFunc(got, tc.expected, func(a, b []byte) bool { return (a == nil) == (b == nil) && bytes.Equal(a, b) }) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestValidIfname is check_ifname.
//
// go test ./internal/goip/ -run TestValidIfname
func TestValidIfname(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    bool
	}{
		{"positive: a name", "d0", true},
		{"boundary: 15 bytes", "abcdefghijklmno", true},
		{"negative: 16 bytes", "abcdefghijklmnop", false},
		{"negative: empty", "", false},
		{"negative: a slash", "a/b", false},
		{"negative: a colon", "a:b", false},
		{"negative: a space", "a b", false},
		{"corner: a dot is allowed", "a.b", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := validIfname(tc.name); got != tc.expected {
				t.Errorf("validIfname(%q) = %v, want %v", tc.name, got, tc.expected)
			}
		})
	}
}

// TestIpstatsProcess is ipstats_process_ifsm over hand-built replies: the
// shapes no workbench device sends.
//
// go test ./internal/goip/ -run TestIpstatsProcess
func TestIpstatsProcess(t *testing.T) {
	hdr := func(ifindex uint32) []byte {
		h := make([]byte, xtcpnl.IfStatsMsgSizeCst)
		binary.NativeEndian.PutUint32(h[4:8], ifindex)
		return h
	}
	u8 := func(v byte) []byte { return []byte{v} }
	hwsi := func(request, used []byte) []byte {
		var in []byte
		if request != nil {
			in = append(in, rtattrBytes(hwSInfoRequestCst, request)...)
		}
		if used != nil {
			in = append(in, rtattrBytes(hwSInfoUsedCst, used)...)
		}
		return rtattrBytes(offloadXstatsHwSInfo, rtattrBytes(offloadXstatsL3Stats, in))
	}
	offload := func(members ...[]byte) []byte {
		return rtattrBytes(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, slices.Concat(members...))
	}
	mpls := func(link []byte) []byte {
		var in []byte
		if link != nil {
			in = rtattrBytes(mplsStatsLinkCst, link)
		}
		return rtattrBytes(unix.IFLA_STATS_AF_SPEC, rtattrBytes(unix.AF_MPLS, in))
	}
	big := make([]byte, xtcpnl.MplsLinkStatsSizeCst)
	binary.NativeEndian.PutUint64(big[16:], 1234567890123) // rx_bytes
	tests := []struct {
		description string
		args        string
		body        []byte
		json        bool
		expected    string
		stderr      string
		exit        int
	}{
		{description: "positive: hw_stats_info with request and used",
			args: "group offload subgroup hw_stats_info", body: slices.Concat(hdr(3), offload(hwsi(u8(1), u8(0)))),
			expected: "3: if3: group offload subgroup hw_stats_info\n    l3_stats on used off\n"},
		{description: "boundary: hw_stats_info with used alone",
			args: "group offload subgroup hw_stats_info", body: slices.Concat(hdr(3), offload(hwsi(nil, u8(1)))),
			expected: "3: if3: group offload subgroup hw_stats_info\n    l3_stats used on\n"},
		{description: "negative: a used byte of 2 is a complaint and nothing printed",
			args: "group offload subgroup hw_stats_info", body: slices.Concat(hdr(3), offload(hwsi(u8(0), u8(2)))),
			expected: "3: if3: group offload subgroup hw_stats_info\n    l3_stats off\n",
			stderr:   "Invalid value for used: expected 0 or 1, got 2.\n"},
		{description: "negative: a request byte of 2 fails the body, -EINVAL",
			args: "group offload subgroup hw_stats_info", body: slices.Concat(hdr(3), offload(hwsi(u8(2), nil))),
			expected: "3: if3: group offload subgroup hw_stats_info\n",
			stderr:   "Invalid value for request: expected 0 or 1, got 2.\n", exit: ExitEINVAL},
		{description: "corner: l3_stats with hw_stats_info but no counters is the header's flags alone",
			args: "group offload subgroup l3_stats", body: slices.Concat(hdr(3), offload(hwsi(u8(0), u8(0)))),
			expected: "3: if3: group offload subgroup l3_stats off used off\n"},
		{description: "corner: l3_stats counters with no hw_stats_info print nothing",
			args: "group offload subgroup l3_stats", body: slices.Concat(hdr(3), offload(rtattrBytes(offloadXstatsL3Stats, make([]byte, 72)))),
			expected: "3: if3: group offload subgroup l3_stats\n"},
		{description: "positive: an mpls member widens its columns",
			args: "group afstats", body: slices.Concat(hdr(3), mpls(big)),
			expected: "3: if3: group afstats subgroup mpls\n" +
				"    RX:     bytes packets errors dropped noroute\n" +
				"    1234567890123       0      0       0       0 \n" +
				"    TX:     bytes packets errors dropped\n" +
				"                0       0      0       0 \n"},
		{description: "boundary: a short MPLS_STATS_LINK is zero-padded",
			args: "group afstats", body: slices.Concat(hdr(3), mpls(big[:24])), json: true,
			expected: `[{"ifindex":3,"ifname":"if3","group":"afstats","subgroup":"mpls","mpls_stats":{"rx":{"bytes":1234567890123,` +
				`"packets":0,"errors":0,"dropped":0,"noroute":0},"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0}}}]` + "\n"},
		{description: "negative: an mpls member with no MPLS_STATS_LINK ends the command",
			args: "group afstats", body: slices.Concat(hdr(3), mpls(nil)),
			expected: "3: if3: group afstats subgroup mpls", exit: ExitFailure},
		{description: "corner: no AF_MPLS member is the header alone",
			args: "group afstats", body: slices.Concat(hdr(3), rtattrBytes(unix.IFLA_STATS_AF_SPEC, nil)),
			expected: "3: if3: group afstats subgroup mpls\n"},
		{description: "negative: a body shorter than if_stats_msg",
			args: "group link", body: make([]byte, xtcpnl.IfStatsMsgSizeCst-1), exit: ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errOut bytes.Buffer
			c := &runCtx{out: &out, errOut: &errOut, lltab: NewLLTab(), json: tc.json,
				src: typedStatsSource{unix.RTM_NEWSTATS: {tc.body}}}
			// dev is the point get, which prints no blank line after the
			// device; the replay answers it from its one body.
			c.lltab.Fill([]xtcpnl.LinkInfo{{Index: 3, Name: "if3"}})
			err := runStatsShow(c, append(strings.Fields(tc.args), "dev", "if3"), 1)
			exit := 0
			if err != nil {
				exit = exitCodeOf(err)
			}
			if out.String() != tc.expected || errOut.String() != tc.stderr || exit != tc.exit {
				t.Errorf("stdout %q\nstderr %q\nexit %d (%v)\nwant %q, %q, %d", out.String(), errOut.String(), exit, err,
					tc.expected, tc.stderr, tc.exit)
			}
		})
	}
}

// fetchSource is a dump-only Source answering with fixed bodies, so
// ipstatsFetch takes its replay path.
type fetchSource struct {
	bodies [][]byte
	err    error
}

func (s *fetchSource) Dump([]byte, uint16) ([][]byte, error) { return s.bodies, s.err }

// TestIpstatsFetch checks ipstatsFetch on a source that cannot talk: the
// whole dump for no device, and for a device the one reply whose
// ifindex is its own.
//
// go test ./internal/goip/ -run TestIpstatsFetch
func TestIpstatsFetch(t *testing.T) {
	body := func(ifindex uint32, tail ...byte) []byte {
		b := make([]byte, xtcpnl.IfStatsMsgSizeCst, xtcpnl.IfStatsMsgSizeCst+len(tail))
		binary.NativeEndian.PutUint32(b[4:8], ifindex)
		return append(b, tail...)
	}
	b1, b2 := body(1, 0xaa), body(2, 0xbb)
	short := body(3)[:xtcpnl.IfStatsMsgSizeCst-1]
	dumpErr := errors.New("dump refused")
	tests := []struct {
		description string
		src         *fetchSource
		ifindex     uint32
		expected    [][]byte
		wantErr     error
		wantRC      int
	}{
		{description: "positive: no device is the whole dump", src: &fetchSource{bodies: [][]byte{b1, b2}},
			expected: [][]byte{b1, b2}},
		{description: "positive: a device is its own reply alone", src: &fetchSource{bodies: [][]byte{b1, b2}},
			ifindex: 2, expected: [][]byte{b2}},
		{description: "negative: no reply for the device is ErrStatsShow, exit 1",
			src: &fetchSource{bodies: [][]byte{b1, b2}}, ifindex: 3, wantErr: ErrStatsShow, wantRC: ExitFailure},
		{description: "boundary: a reply shorter than the header is skipped, not read",
			src: &fetchSource{bodies: [][]byte{short}}, ifindex: 3, wantErr: ErrStatsShow, wantRC: ExitFailure},
		{description: "boundary: an empty dump for no device is no replies", src: &fetchSource{}},
		{description: "negative: the dump's error passes through", src: &fetchSource{err: dumpErr}, ifindex: 1,
			wantErr: dumpErr, wantRC: ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ipstatsFetch(&runCtx{src: tc.src}, nil, tc.ifindex)
			if !errors.Is(err, tc.wantErr) || (err == nil) != (tc.wantErr == nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				if rc := exitCodeOf(err); rc != tc.wantRC {
					t.Errorf("exitCodeOf = %d, want %d", rc, tc.wantRC)
				}
				return
			}
			if !slices.EqualFunc(got, tc.expected, bytes.Equal) {
				t.Errorf("got %x, want %x", got, tc.expected)
			}
		})
	}
}
