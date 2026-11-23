package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const wsBridgeDir = "testdata/7_3_1/ws/bridge"

// bridgeObjReplies are a ws bridge row's replies of type typ to its last
// request, the object dump (the first answers ll_init_map).
func bridgeObjReplies(t testing.TB, row string, typ uint16) [][]byte {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(wsBridgeDir, row, "ip.pcap"))
	last := -1
	for i, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			last = i
		}
	}
	var out [][]byte
	for _, m := range msgs[last+1:] {
		if m.h.Type == typ {
			out = append(out, m.body)
		}
	}
	return out
}

// bridgeLinkNamed is the decoded AF_BRIDGE link of that name in a row's
// object dump.
func bridgeLinkNamed(t testing.TB, row, name string) BridgeLink {
	t.Helper()
	for _, b := range bridgeObjReplies(t, row, unix.RTM_NEWLINK) {
		l, err := ParseBridgeLink(b)
		if err != nil {
			t.Fatalf("%s: %v", row, err)
		}
		if n, _ := l.Attrs.Str(unix.IFLA_IFNAME); n == name {
			return l
		}
	}
	t.Fatalf("%s: no link %s", row, name)
	return BridgeLink{}
}

// ifinfo is an AF_BRIDGE ifinfomsg for index, then attrs.
func ifinfo(index uint32, attrs ...[]byte) []byte {
	body := joinAttrs(attrs...)
	b := make([]byte, IfInfomsgSizeCst, IfInfomsgSizeCst+len(body))
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], index)
	return append(b, body...)
}

func vinfo(flags, vid uint16) []byte {
	b := make([]byte, 4, 8)
	binary.LittleEndian.PutUint16(b[0:2], flags)
	binary.LittleEndian.PutUint16(b[2:4], vid)
	return rta(IflaBridgeVlanInfo, b...)
}

// vlanSummary is a link's VLAN list as "flags:vid" items.
func vlanSummary(l BridgeLink) string {
	s := make([]string, 0, len(l.Vlans))
	for _, v := range l.Vlans {
		s = append(s, fmt.Sprintf("%#x:%d", v.Flags, v.Vid))
	}
	return strings.Join(s, " ")
}

// TestParseBridgeLink pins the AF_BRIDGE RTM_NEWLINK decoder on captured
// replies (gobridge 6C corpus) and crafted ones.
func TestParseBridgeLink(t *testing.T) {
	t.Parallel()
	u8 := func(a AttrTable, typ uint16) string {
		v, ok := a.U8(typ)
		return fmt.Sprintf("%d/%t", v, ok)
	}
	tests := []struct {
		description string
		get         func(t *testing.T) (BridgeLink, error)
		check       func(BridgeLink) string
		expected    string // check's result, or "error: <substring>"
	}{
		{
			description: "positive: port with protinfo (br d2: cost 7, priority 5, learning off, hairpin on)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-013", "d2"), nil },
			check: func(l BridgeLink) string {
				c, _ := l.Port.U32(unix.IFLA_BRPORT_COST)
				p, _ := l.Port.U16(unix.IFLA_BRPORT_PRIORITY)
				return fmt.Sprintf("nested=%t cost=%d prio=%d learning=%s hairpin=%s state=%s", l.ProtInfoNested, c, p,
					u8(l.Port, unix.IFLA_BRPORT_LEARNING), u8(l.Port, unix.IFLA_BRPORT_MODE), u8(l.Port, unix.IFLA_BRPORT_STATE))
			},
			expected: "nested=true cost=7 prio=5 learning=0/true hairpin=1/true state=3/true",
		},
		{
			description: "positive: vlan list with pvid/untagged (br d1)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-014", "d1"), nil },
			check:       vlanSummary,
			expected:    "0x4:1 0x6:10 0x0:20 0x0:21 0x0:22 0x0:23 0x0:24 0x0:25",
		},
		{
			description: "positive: compressed range, one RANGE_BEGIN/RANGE_END pair (br -com d1)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-015", "d1"), nil },
			check:       vlanSummary,
			expected:    "0x4:1 0x6:10 0x8:20 0x10:25",
		},
		{
			description: "positive: tunnel info (vxtun vx0: 100→1000, 200-202→2000-2002)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-035", "vx0"), nil },
			check: func(l BridgeLink) string {
				s := make([]string, 0, len(l.Tunnels))
				for _, tn := range l.Tunnels {
					id, _ := tn.Attrs.U32(IflaBridgeVlanTunnelID)
					vid, _ := tn.Attrs.U16(IflaBridgeVlanTunnelVid)
					fl, _ := tn.Attrs.U16(IflaBridgeVlanTunnelFlags)
					s = append(s, fmt.Sprintf("%d:%d:%#x", vid, id, fl))
				}
				return strings.Join(s, " ")
			},
			expected: "100:1000:0x0 200:2000:0x8 202:2002:0x10",
		},
		{
			description: "positive: mst entries (brmst d1: msti 0 forwarding, msti 3 learning)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-032", "d1"), nil },
			check: func(l BridgeLink) string {
				s := make([]string, 0, len(l.Mst))
				for _, e := range l.Mst {
					m, _ := e.Attrs.U16(IflaBridgeMstEntryMsti)
					st, _ := e.Attrs.U8(IflaBridgeMstEntryState)
					s = append(s, fmt.Sprintf("%d:%d", m, st))
				}
				return fmt.Sprintf("%t %s", l.HasMst, strings.Join(s, " "))
			},
			expected: "true 0:3 3:2",
		},
		{
			description: "positive: K1, the bridge's own entry names itself as master",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ec-014", "br0"), nil },
			check: func(l BridgeLink) string {
				m, ok := l.Attrs.U32(unix.IFLA_MASTER)
				return fmt.Sprintf("self=%t present=%t protinfo=%t", int64(m) == int64(l.Index), ok, l.Attrs.Has(IflaProtinfoCst))
			},
			expected: "self=true present=true protinfo=false",
		},
		{
			description: "negative: body shorter than ifinfomsg",
			get:         func(*testing.T) (BridgeLink, error) { return ParseBridgeLink([]byte{7, 0, 0}) },
			expected:    "error: too small",
		},
		{
			description: "negative: truncated attribute",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(append(ifinfo(4), 0x20, 0, 3, 0, 'd'))
			},
			expected: "error: RTAttr",
		},
		{
			description: "negative: a 2-byte IFLA_BRIDGE_VLAN_INFO",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst, rta(IflaBridgeVlanInfo, 1, 0)...)))
			},
			expected: "error: bridge_vlan_info",
		},
		{
			description: "negative: a truncated nest inside AF_SPEC",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst, rta(IflaBridgeVlanTunnelInfo, 0x10, 0, 1, 0)...)))
			},
			expected: "error: RTAttr",
		},
		{
			description: "boundary: vid 1 and vid 4094 decoded exactly",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst, joinAttrs(vinfo(0, 1), vinfo(0, 4094))...)))
			},
			check:    vlanSummary,
			expected: "0x0:1 0x0:4094",
		},
		{
			description: "boundary: an empty AF_SPEC nest, zero vlans",
			get:         func(*testing.T) (BridgeLink, error) { return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst))) },
			check: func(l BridgeLink) string {
				return fmt.Sprintf("%t %d %t", l.Attrs.Has(IflaAfSpecCst), len(l.Vlans), l.HasMst)
			},
			expected: "true 0 false",
		},
		{
			description: "boundary: B10's shape, no AF_SPEC at all (MST off)",
			get:         func(t *testing.T) (BridgeLink, error) { return bridgeLinkNamed(t, "ng-018", "d1"), nil },
			check: func(l BridgeLink) string {
				return fmt.Sprintf("%t %t", l.Attrs.Has(IflaAfSpecCst), l.HasMst)
			},
			expected: "false false",
		},
		{
			description: "corner: PROTINFO without NLA_F_NESTED is a bare u8 state",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(ifinfo(4, rta(IflaProtinfoCst, 3)))
			},
			check: func(l BridgeLink) string {
				return fmt.Sprintf("nested=%t state=%s port=%d", l.ProtInfoNested, u8(l.Attrs, IflaProtinfoCst), l.Port.Len())
			},
			expected: "nested=false state=3/true port=0",
		},
		{
			description: "corner: an unknown IFLA_BRIDGE_* type is ignored, siblings intact",
			get: func(*testing.T) (BridgeLink, error) {
				return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst, joinAttrs(vinfo(0, 5), rta(IflaBridgeMst+3, 1, 2, 3, 4), vinfo(0, 6))...)))
			},
			check:    vlanSummary,
			expected: "0x0:5 0x0:6",
		},
		{
			description: "corner: IFLA_BRIDGE_MST twice, the first wins",
			get: func(*testing.T) (BridgeLink, error) {
				e := func(msti uint16) []byte {
					return rta(IflaBridgeMstEntry|nlaFNestedCst, joinAttrs(rta(IflaBridgeMstEntryMsti, le16(msti)...), rta(IflaBridgeMstEntryState, 3))...)
				}
				return ParseBridgeLink(ifinfo(4, rta(IflaAfSpecCst, joinAttrs(rta(IflaBridgeMst, e(1)...), rta(IflaBridgeMst, e(9)...))...)))
			},
			check: func(l BridgeLink) string {
				m, _ := l.Mst[0].Attrs.U16(IflaBridgeMstEntryMsti)
				return fmt.Sprintf("%d %d", len(l.Mst), m)
			},
			expected: "1 1",
		},
		{
			description: "corner: a negative ifindex is a C int",
			get:         func(*testing.T) (BridgeLink, error) { return ParseBridgeLink(ifinfo(0xffffffff)) },
			check:       func(l BridgeLink) string { return fmt.Sprint(l.Index) },
			expected:    "-1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			l, err := tc.get(t)
			if want, isErr := strings.CutPrefix(tc.expected, "error: "); isErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v, want one containing %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.check(l); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// tmsg is a tunnel_msg then attrs.
func tmsg(ifindex uint32, attrs ...[]byte) []byte {
	body := joinAttrs(attrs...)
	b := make([]byte, TunnelMsgSizeCst, TunnelMsgSizeCst+len(body))
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	return append(b, body...)
}

// vniSummary is a tunnel message's entries as start[-end][@group].
func vniSummary(m TunnelMsg) string {
	s := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		st, _ := e.Attrs.U32(VxlanVnifilterEntryStart)
		item := fmt.Sprint(st)
		if end, ok := e.Attrs.U32(VxlanVnifilterEntryEnd); ok {
			item += fmt.Sprintf("-%d", end)
		}
		if g := e.Attrs.Bytes(VxlanVnifilterEntryGroup); g != nil {
			item += "@" + net.IP(g).String()
		}
		if g := e.Attrs.Bytes(VxlanVnifilterEntryGroup6); g != nil {
			item += "@" + net.IP(g).String()
		}
		if e.Stats.Len() > 0 {
			rx, _ := e.Stats.U64(VnifilterEntryStatsRxBytes)
			item += fmt.Sprintf("+stats(%d)", rx)
		}
		s = append(s, item)
	}
	return strings.Join(s, " ")
}

// TestParseTunnelMsg pins the RTM_NEWTUNNEL decoder.
func TestParseTunnelMsg(t *testing.T) {
	t.Parallel()
	captured := func(row string) func() (TunnelMsg, error) {
		return func() (TunnelMsg, error) {
			rs := bridgeObjReplies(t, row, unix.RTM_NEWTUNNEL)
			if len(rs) != 1 {
				return TunnelMsg{}, fmt.Errorf("%s: %d tunnel replies", row, len(rs))
			}
			return ParseTunnelMsg(rs[0])
		}
	}
	tests := []struct {
		description string
		get         func() (TunnelMsg, error)
		expected    string // vniSummary, or "error: <substring>"
	}{
		{"positive: vnifilter entries (vnif)", captured("ec-040"),
			"100-102 200@239.1.1.1 300@2001:db8::1 16777214-16777215"},
		{"positive: stats nests under -s (vnif)", captured("ec-042"),
			"100+stats(0) 101+stats(0) 102+stats(0) 200@239.1.1.1+stats(0) 300@2001:db8::1+stats(0) 16777214+stats(0) 16777215+stats(0)"},
		{"negative: body shorter than tunnel_msg", func() (TunnelMsg, error) { return ParseTunnelMsg([]byte{7, 0, 0, 0}) }, "error: tunnel_msg"},
		{"negative: truncated attribute", func() (TunnelMsg, error) { return ParseTunnelMsg(append(tmsg(4), 0x10, 0, 1, 0)) }, "error: RTAttr"},
		{"negative: truncated attribute inside an entry", func() (TunnelMsg, error) {
			return ParseTunnelMsg(tmsg(4, rta(VxlanVnifilterEntry, 0x10, 0, 1, 0)))
		}, "error: RTAttr"},
		{"boundary: no entries", func() (TunnelMsg, error) { return ParseTunnelMsg(tmsg(4)) }, ""},
		{"boundary: vni 16777215, the largest", func() (TunnelMsg, error) {
			return ParseTunnelMsg(tmsg(4, rta(VxlanVnifilterEntry, rta(VxlanVnifilterEntryStart, le32(16777215)...)...)))
		}, "16777215"},
		{"corner: a non-ENTRY attribute is skipped", func() (TunnelMsg, error) {
			return ParseTunnelMsg(tmsg(4, rta(7, 1, 2, 3, 4), rta(VxlanVnifilterEntry, rta(VxlanVnifilterEntryStart, le32(5)...)...)))
		}, "5"},
		{"corner: a v6 group", func() (TunnelMsg, error) {
			g := net.ParseIP("ff05::1").To16()
			return ParseTunnelMsg(tmsg(4, rta(VxlanVnifilterEntry|nlaFNestedCst,
				joinAttrs(rta(VxlanVnifilterEntryStart, le32(9)...), rta(VxlanVnifilterEntryGroup6, g...))...)))
		}, "9@ff05::1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			m, err := tc.get()
			if want, isErr := strings.CutPrefix(tc.expected, "error: "); isErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v, want one containing %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := vniSummary(m); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestParseBrVlanTables pins the table view of RTM_NEWVLAN.
func TestParseBrVlanTables(t *testing.T) {
	t.Parallel()
	first := func(row string, ifindexName string) func() (BrVlanTables, error) {
		return func() (BrVlanTables, error) {
			rs := bridgeObjReplies(t, row, unix.RTM_NEWVLAN)
			if len(rs) == 0 {
				return BrVlanTables{}, errors.New("no RTM_NEWVLAN")
			}
			return ParseBrVlanTables(rs[0])
		}
	}
	summary := func(v BrVlanTables) string {
		var s []string
		for i, t := range v.Tables {
			switch v.Kinds[i] {
			case BridgeVlandbEntry:
				info := t.Bytes(BridgeVlandbEntryInfo)
				st, _ := t.U8(BridgeVlandbEntryState)
				item := fmt.Sprintf("e%d/s%d", binary.LittleEndian.Uint16(info[2:4]), st)
				if v.Stats[i].Len() > 0 {
					item += "+stats"
				}
				s = append(s, item)
			case BridgeVlandbGlobalOptions:
				id, _ := t.U16(BridgeVlandbGoptsID)
				sn, _ := t.U8(3) // MCAST_SNOOPING
				s = append(s, fmt.Sprintf("g%d/snoop%d", id, sn))
			}
		}
		return strings.Join(s, " ")
	}
	tests := []struct {
		description string
		get         func() (BrVlanTables, error)
		expected    string
	}{
		{"positive: -d vlan show, br0's entries with state", first("ec-019", "br0"), "e1/s3 e10/s3"},
		{"positive: -s -d vlan show adds stats nests", first("ec-021", "br0"), "e1/s3+stats e10/s3+stats"},
		{"positive: global options (brglob: vid 10 snooping off; 20 and 30 from the ports)", first("ec-029", "br0"), "g1/snoop1 g10/snoop0 g20/snoop1 g30/snoop1"},
		{"negative: body shorter than br_vlan_msg", func() (BrVlanTables, error) { return ParseBrVlanTables([]byte{7}) }, "error: br_vlan_msg"},
		{"negative: truncated entry", func() (BrVlanTables, error) {
			return ParseBrVlanTables(append(make([]byte, 8), rta(BridgeVlandbEntry, 0x10, 0, 1, 0)...))
		}, "error: RTAttr"},
		{"boundary: no nests", func() (BrVlanTables, error) { return ParseBrVlanTables(make([]byte, 8)) }, ""},
		{"corner: an unknown top-level type is skipped", func() (BrVlanTables, error) {
			return ParseBrVlanTables(append(make([]byte, 8), joinAttrs(rta(9, 1, 2, 3, 4),
				rta(BridgeVlandbGlobalOptions, rta(BridgeVlandbGoptsID, le16(7)...)...))...))
		}, "g7/snoop0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			v, err := tc.get()
			if want, isErr := strings.CutPrefix(tc.expected, "error: "); isErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v, want one containing %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := summary(v); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// bridgeFuzzSeeds are the corpus's reply bodies of type typ.
func bridgeFuzzSeeds(f *testing.F, typ uint16, rows ...string) {
	f.Helper()
	for _, r := range rows {
		for _, b := range bridgeObjReplies(f, r, typ) {
			f.Add(b)
		}
	}
	f.Add([]byte{})
}

func FuzzParseBridgeLink(f *testing.F) {
	bridgeFuzzSeeds(f, unix.RTM_NEWLINK, "ec-013", "ec-015", "ec-032", "ec-035")
	f.Fuzz(func(t *testing.T, b []byte) {
		l, err := ParseBridgeLink(b)
		if err != nil {
			return
		}
		if len(l.Vlans)*8 > len(b) || len(l.Mst)*4 > len(b) {
			t.Fatalf("%d vlans, %d mst from %d bytes", len(l.Vlans), len(l.Mst), len(b))
		}
	})
}

func FuzzParseTunnelMsg(f *testing.F) {
	bridgeFuzzSeeds(f, unix.RTM_NEWTUNNEL, "ec-040", "ec-042")
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ParseTunnelMsg(b)
		if err != nil {
			return
		}
		if len(m.Entries)*4 > len(b) {
			t.Fatalf("%d entries from %d bytes", len(m.Entries), len(b))
		}
	})
}

func FuzzParseBrVlanTables(f *testing.F) {
	bridgeFuzzSeeds(f, unix.RTM_NEWVLAN, "ec-019", "ec-021", "ec-029")
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := ParseBrVlanTables(b)
		if err != nil {
			return
		}
		if len(v.Kinds) != len(v.Tables) || len(v.Tables) != len(v.Stats) {
			t.Fatalf("lists differ: %d %d %d", len(v.Kinds), len(v.Tables), len(v.Stats))
		}
	})
}

// benchBodies decodes every reply of type typ in a corpus row per op.
func benchBodies(b *testing.B, row string, typ uint16, parse func([]byte) error) {
	bodies := bridgeObjReplies(b, row, typ)
	if len(bodies) == 0 {
		b.Fatalf("%s: no replies of type %d", row, typ)
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, body := range bodies {
			if err := parse(body); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkParseBridgeLink decodes `bridge vlan show`'s links
// (br: br0, d1, d2 with VLAN lists).
func BenchmarkParseBridgeLink(b *testing.B) {
	benchBodies(b, "ec-014", unix.RTM_NEWLINK, func(x []byte) error { _, err := ParseBridgeLink(x); return err })
}

// BenchmarkParseTunnelMsg decodes `bridge -s vni show`'s reply (seven VNIs
// with stats).
func BenchmarkParseTunnelMsg(b *testing.B) {
	benchBodies(b, "ec-042", unix.RTM_NEWTUNNEL, func(x []byte) error { _, err := ParseTunnelMsg(x); return err })
}

// BenchmarkParseBrVlanTables decodes `bridge -s -d vlan show`'s replies.
func BenchmarkParseBrVlanTables(b *testing.B) {
	benchBodies(b, "ec-021", unix.RTM_NEWVLAN, func(x []byte) error { _, err := ParseBrVlanTables(x); return err })
}
