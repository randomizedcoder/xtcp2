package xtcpnl

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const wsBridgexDir = "testdata/7_3_1/ws/bridgex"

// rport is an MDBA_ROUTER_PORT: ifindex with no header, then attrs.
func rport(ifindex uint32, attrs ...[]byte) []byte {
	return rta(MdbaRouterPort, append(binary.LittleEndian.AppendUint32(nil, ifindex), joinAttrs(attrs...)...)...)
}

func pattr32(t uint16, v uint32) []byte { return rta(t, binary.LittleEndian.AppendUint32(nil, v)...) }

// rportSummary is ports as "ifindex:timer/type/inet/inet6/vid", "-" for an
// absent attribute.
func rportSummary(ps []MdbRouterPort) string {
	var b strings.Builder
	for i := range ps {
		p := &ps[i]
		if i > 0 {
			b.WriteByte(' ')
		}
		f := func(has bool, v uint64) string {
			if !has {
				return "-"
			}
			return fmt.Sprint(v)
		}
		fmt.Fprintf(&b, "%d:%s/%s/%s/%s/%s", p.Ifindex, f(p.HasTimer, uint64(p.Timer)), f(p.HasType, uint64(p.Type)),
			f(p.HasInetTimer, uint64(p.InetTimer)), f(p.HasInet6Timer, uint64(p.Inet6Timer)), f(p.HasVid, uint64(p.Vid)))
	}
	return b.String()
}

// TestParseMdbRouterPorts decodes MDBA_ROUTER payloads, crafted and
// captured (the brrtr fixture's vid 10, 6X-C).
//
// go test ./pkg/xtcpnl/ -run TestParseMdbRouterPorts
func TestParseMdbRouterPorts(t *testing.T) {
	full := rport(4, pattr32(MdbaRouterPattrTimer, 25487), rta(MdbaRouterPattrType, 2),
		pattr32(MdbaRouterPattrInetTimer, 25487), pattr32(MdbaRouterPattrInet6Timer, 25400), rta(MdbaRouterPattrVid, 10, 0))
	tests := []struct {
		description string
		nest        []byte
		expected    string // summary, or the error
	}{
		// Positive.
		{"positive: every attribute", full, "4:25487/2/25487/25400/10"},
		{"positive: two ports in wire order", joinAttrs(
			rport(5, pattr32(MdbaRouterPattrTimer, 0), rta(MdbaRouterPattrType, 1)),
			rport(4, pattr32(MdbaRouterPattrTimer, 100), rta(MdbaRouterPattrType, 3))),
			"5:0/1/-/-/- 4:100/3/-/-/-"},
		{"positive: captured brrtr vid 10: d2, then d1 typed temp-query, its port-wide default (K3)",
			capturedRouterNest(t, "ec-021", 10), "5:~/2/~/~/10 4:0/1/0/0/10"},
		// Negative.
		{"negative: a port shorter than its ifindex", rta(MdbaRouterPort, 1, 2, 3), "error: " + ErrMdbRouterPortSmall.Error()},
		{"negative: a 2-byte timer", rport(4, rta(MdbaRouterPattrTimer, 1, 0)), "error: " + ErrMdbRouterPattrWidth.Error()},
		{"negative: a 4-byte type", rport(4, pattr32(MdbaRouterPattrType, 2)), "error: " + ErrMdbRouterPattrWidth.Error()},
		{"negative: a 4-byte vid", rport(4, pattr32(MdbaRouterPattrVid, 10)), "error: " + ErrMdbRouterPattrWidth.Error()},
		{"negative: a truncated port attribute", rport(4, []byte{8, 0, 1, 0, 0}), "error: " + ErrRTAttrSmall.Error()},
		{"negative: a port longer than the nest", []byte{12, 0, 1, 0, 4, 0, 0, 0}, "error: " + ErrRTAttrSmall.Error()},
		{"negative: a bad port after a good one", joinAttrs(rport(4), rta(MdbaRouterPort, 9)),
			"error: " + ErrMdbRouterPortSmall.Error()},
		// Boundary.
		{"boundary: an empty nest", nil, ""},
		{"boundary: a port with only its ifindex", rport(4), "4:-/-/-/-/-"},
		{"boundary: ifindex and timer at u32 max", rport(0xffffffff, pattr32(MdbaRouterPattrTimer, 0xffffffff)),
			"4294967295:4294967295/-/-/-/-"},
		{"boundary: vid 4094", rport(4, rta(MdbaRouterPattrVid, 0xfe, 0x0f)), "4:-/-/-/-/4094"},
		{"boundary: a tail shorter than a header ends the walk", append(rport(4), 1, 2), "4:-/-/-/-/-"},
		// Corner.
		{"corner: NLA_F_NESTED on the port is masked", func() []byte {
			b := rport(4, rta(MdbaRouterPattrType, 3))
			binary.LittleEndian.PutUint16(b[2:4], MdbaRouterPort|unix.NLA_F_NESTED)
			return b
		}(), "4:-/3/-/-/-"},
		{"corner: an unknown nest type is skipped", joinAttrs(rta(7, 1, 2, 3, 4), rport(4)), "4:-/-/-/-/-"},
		{"corner: an unknown port attribute is skipped", rport(4, pattr32(9, 1), rta(MdbaRouterPattrType, 2)), "4:-/2/-/-/-"},
		{"corner: the first of a repeated type wins", rport(4, rta(MdbaRouterPattrType, 1), rta(MdbaRouterPattrType, 2)),
			"4:-/1/-/-/-"},
		{"corner: type 0 (disabled) is decoded", rport(4, rta(MdbaRouterPattrType, 0)), "4:-/0/-/-/-"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ps, err := ParseMdbRouterPorts(tc.nest)
			got := rportSummary(ps)
			if err != nil {
				got = "error: " + err.Error()
			}
			if strings.Contains(tc.expected, "~") {
				// A running timer: compare all but the timer values.
				got = maskTimers(ps)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// maskTimers is rportSummary with each nonzero timer as "~".
func maskTimers(ps []MdbRouterPort) string {
	m := make([]MdbRouterPort, len(ps))
	copy(m, ps)
	s := rportSummary(m)
	out := make([]string, 0, len(m))
	for i, item := range strings.Fields(s) {
		idx, rest, _ := strings.Cut(item, ":")
		f := strings.Split(rest, "/")
		p := &m[i]
		if p.HasTimer && p.Timer != 0 {
			f[0] = "~"
		}
		if p.HasInetTimer && p.InetTimer != 0 {
			f[2] = "~"
		}
		if p.HasInet6Timer && p.Inet6Timer != 0 {
			f[3] = "~"
		}
		out = append(out, idx+":"+strings.Join(f, "/"))
	}
	return strings.Join(out, " ")
}

// capturedRouterNest is the MDBA_ROUTER payload of vid's global options in
// a bridgex row's RTM_GETVLAN reply.
func capturedRouterNest(t testing.TB, row string, vid uint16) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, filepath.Join(wsBridgexDir, row, "ip.pcap")) {
		if m.h.Type != unix.RTM_NEWVLAN {
			continue
		}
		v, err := ParseBrVlanTables(m.body)
		if err != nil {
			t.Fatalf("%s: %v", row, err)
		}
		for i, tb := range v.Tables {
			if v.Kinds[i] != BridgeVlandbGlobalOptions {
				continue
			}
			if id, _ := tb.U16(BridgeVlandbGoptsID); id != vid {
				continue
			}
			rp := tb.Bytes(BridgeVlandbGoptsRtrPorts)
			nest, ok := FindRawAttr(rp, MdbaRouter)
			if !ok {
				t.Fatalf("%s: vid %d router ports without MDBA_ROUTER", row, vid)
			}
			return nest
		}
	}
	t.Fatalf("%s: no global options for vid %d", row, vid)
	return nil
}

// TestWsBridgexRouterPorts is O4 for the router-port rows: each -j row's
// "router_ports" is the decoded MDBA_ROUTER of its VLAN, ports named from
// the row's own ll_init_map dump, and under -s each port's "type" is
// is_temp_mcast_rtr of the decoded type and its "timer" format_timer's
// aligned text (B34).
//
// go test ./pkg/xtcpnl/ -run TestWsBridgexRouterPorts
func TestWsBridgexRouterPorts(t *testing.T) {
	tests := []struct {
		description string
		row         string
		stats       bool
		expected    int // ports compared
	}{
		{"positive: -j vlan global show", "pw-020", false, 3},
		{"positive: -j -s vlan global show", "pw-021", true, 3},
		{"positive: -j -s filtered to br0 vid 10", "pw-022", true, 2},
		{"corner: -j -s -o (oneline doesn't touch JSON)", "pw-023", true, 3},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(wsBridgexDir, tc.row)
			names := map[uint32]string{}
			want := map[uint16][]MdbRouterPort{}
			for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
				switch m.h.Type {
				case unix.RTM_NEWLINK:
					if len(m.body) < IfInfomsgSizeCst {
						continue
					}
					tb, err := ParseAttrTable(m.body[IfInfomsgSizeCst:], unix.IFLA_IFNAME)
					if err != nil {
						t.Fatal(err)
					}
					n, _ := tb.Str(unix.IFLA_IFNAME)
					names[binary.LittleEndian.Uint32(m.body[4:8])] = n
				case unix.RTM_NEWVLAN:
					v, err := ParseBrVlanTables(m.body)
					if err != nil {
						t.Fatal(err)
					}
					for i, tb := range v.Tables {
						rp := tb.Bytes(BridgeVlandbGoptsRtrPorts)
						if v.Kinds[i] != BridgeVlandbGlobalOptions || rp == nil {
							continue
						}
						nest, _ := FindRawAttr(rp, MdbaRouter)
						ps, err := ParseMdbRouterPorts(nest)
						if err != nil {
							t.Fatal(err)
						}
						id, _ := tb.U16(BridgeVlandbGoptsID)
						want[id] = ps
					}
				}
			}
			var got []struct {
				Vlans []struct {
					Vlan        uint16
					RouterPorts []struct{ Port, Timer, Type string } `json:"router_ports"`
				}
			}
			out, err := os.ReadFile(filepath.Join(dir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, br := range got {
				for _, v := range br.Vlans {
					ps := want[v.Vlan]
					if len(v.RouterPorts) != len(ps) {
						t.Fatalf("vid %d: %d ports printed, %d decoded", v.Vlan, len(v.RouterPorts), len(ps))
					}
					for i := range ps {
						p, j := &ps[i], v.RouterPorts[i]
						typ, timer := "", ""
						if tc.stats {
							typ = "permanent"
							if p.IsTemp() {
								typ = "temp"
							}
							timer = fmt.Sprintf("%4d.%02d", p.Timer/100, p.Timer%100)
						}
						if j.Port != names[p.Ifindex] || j.Type != typ || j.Timer != timer {
							t.Errorf("vid %d port %d: printed %+v, decoded %s %q %q", v.Vlan, i, j, names[p.Ifindex], typ, timer)
						}
						n++
					}
				}
			}
			if n != tc.expected {
				t.Errorf("compared %d ports, want %d", n, tc.expected)
			}
		})
	}
}

// FuzzParseMdbRouterPorts: no input panics, and a decoded nest's ports all
// carry an ifindex.
//
// go test ./pkg/xtcpnl/ -run '^$' -fuzz FuzzParseMdbRouterPorts -fuzztime 30s
func FuzzParseMdbRouterPorts(f *testing.F) {
	for _, row := range []string{"ec-020", "ec-021", "ec-022"} {
		f.Add(capturedRouterNest(f, row, 10))
	}
	f.Add(rport(4, rta(MdbaRouterPattrType, 2)))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		ps, err := ParseMdbRouterPorts(b)
		if err != nil {
			if ps != nil {
				t.Fatal("ports with an error")
			}
			if !errors.Is(err, ErrRTAttrSmall) && !errors.Is(err, ErrMdbRouterPortSmall) && !errors.Is(err, ErrMdbRouterPattrWidth) {
				t.Fatalf("unexpected error %v", err)
			}
		}
	})
}

// BenchmarkParseMdbRouterPorts decodes the captured two-port nest.
//
// go test ./pkg/xtcpnl/ -run '^$' -bench BenchmarkParseMdbRouterPorts -benchmem
func BenchmarkParseMdbRouterPorts(b *testing.B) {
	nest := capturedRouterNest(b, "ec-021", 10)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseMdbRouterPorts(nest); err != nil {
			b.Fatal(err)
		}
	}
}
