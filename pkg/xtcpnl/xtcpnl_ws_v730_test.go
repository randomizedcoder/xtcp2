package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The v730 rows' oracles (tools/nlcapgen/matrix_v730.go, NC3-T03): what
// iproute2 7.3.0 sends that 7.2.0 can't, captured at both versions. O3
// checks the request each 7.3.0 keyword makes, and that 7.2.0, which
// refuses each, sends none.

// Attributes golang.org/x/sys/unix doesn't name, from include/uapi/linux
// (if_link.h, nexthop.h, seg6_iptunnel.h).
const (
	wsIflaBondLacpStrict uint16 = 34
	wsIflaOvpnMode       uint16 = 1
	wsNhaFdb             uint16 = 11
	wsSeg6IptunnelSRH    uint16 = 1
	wsSeg6IptunnelTable  uint16 = 3
)

// v730Attrs walks a request's attributes after its family header.
func v730Attrs(body []byte, hdr int) ([]uint16, map[uint16][]byte, error) {
	if len(body) < hdr {
		return nil, nil, fmt.Errorf("body of %d bytes", len(body))
	}
	var types []uint16
	vals := map[uint16][]byte{}
	err := WalkRTAttrs(body[hdr:], func(t uint16, v []byte) {
		types = append(types, t)
		vals[t] = v
	})
	return types, vals, err
}

// v730U8Problem checks an optional u8 against an axis ("" for absent).
func v730U8Problem(what string, vals map[uint16][]byte, a uint16, want string) error {
	v, ok := vals[a]
	if want == "" {
		if ok {
			return fmt.Errorf("%s %x, want none", what, v)
		}
		return nil
	}
	if len(v) != 1 || strconv.Itoa(int(v[0])) != want {
		return fmt.Errorf("%s %x, want the u8 %s", what, v, want)
	}
	return nil
}

// v730LinkProblem checks a bond or ovpn RTM_NEWLINK: the kind, and the
// one info-data attribute the keyword sends.
func v730LinkProblem(ax map[string]string, body []byte) error {
	_, vals, err := v730Attrs(body, IfInfomsgSizeCst)
	if err != nil {
		return err
	}
	_, info, err := v730Attrs(vals[unix.IFLA_LINKINFO], 0)
	if err != nil {
		return err
	}
	if k := strings.TrimRight(string(info[unix.IFLA_INFO_KIND]), "\x00"); k != ax["kind"] {
		return fmt.Errorf("kind %q, want %q", k, ax["kind"])
	}
	_, data, err := v730Attrs(info[unix.IFLA_INFO_DATA], 0)
	if err != nil {
		return err
	}
	if ax["kind"] == "ovpn" {
		return v730U8Problem("IFLA_OVPN_MODE", data, wsIflaOvpnMode, ax["ovpn_mode"])
	}
	return v730U8Problem("IFLA_BOND_LACP_STRICT", data, wsIflaBondLacpStrict, ax["lacp_strict"])
}

// v730NexthopProblem checks NHA_DST_PORT: big-endian, two bytes, right
// after NHA_FDB when there is one.
func v730NexthopProblem(ax map[string]string, body []byte) error {
	types, vals, err := v730Attrs(body, NhMsgSizeCst)
	if err != nil {
		return err
	}
	port, ok := vals[wsNhaDstPort]
	if !ok || len(port) != 2 {
		return fmt.Errorf("NHA_DST_PORT %x, want two bytes", port)
	}
	if got := strconv.Itoa(int(binary.BigEndian.Uint16(port))); got != ax["dst_port"] {
		return fmt.Errorf("NHA_DST_PORT %s (big-endian), want %s", got, ax["dst_port"])
	}
	if ax["fdb"] == "1" {
		i := slices.Index(types, wsNhaFdb)
		if i < 0 || i+1 >= len(types) || types[i+1] != wsNhaDstPort {
			return fmt.Errorf("attributes %v: NHA_DST_PORT not right after NHA_FDB", types)
		}
	}
	return nil
}

// v730Seg6Problem checks SEG6_IPTUNNEL_TABLE: a u32 after the SRH in the
// route's RTA_ENCAP.
func v730Seg6Problem(ax map[string]string, body []byte) error {
	_, vals, err := v730Attrs(body, RtMsgSizeCst)
	if err != nil {
		return err
	}
	types, encap, err := v730Attrs(vals[unix.RTA_ENCAP], 0)
	if err != nil {
		return err
	}
	if !slices.Equal(types, []uint16{wsSeg6IptunnelSRH, wsSeg6IptunnelTable}) {
		return fmt.Errorf("encap attributes %v, want the SRH then the table", types)
	}
	t := encap[wsSeg6IptunnelTable]
	if len(t) != 4 || strconv.FormatUint(uint64(binary.LittleEndian.Uint32(t)), 10) != ax["table"] {
		return fmt.Errorf("SEG6_IPTUNNEL_TABLE %x, want the u32 %s", t, ax["table"])
	}
	return nil
}

// v730MaddrWant is the families maddress show dumps, in order.
func v730MaddrWant(which string) []uint8 {
	switch which {
	case "inet":
		return []uint8{unix.AF_INET}
	case "inet6":
		return []uint8{unix.AF_INET6}
	}
	return []uint8{unix.AF_INET, unix.AF_INET6}
}

// v730Space returns the row's space and the request type its keyword
// sends.
func v730Space(id string) (string, uint16) {
	space, _, _ := strings.Cut(id, "/")
	switch space {
	case "v730bond", "v730ovpn", "v730lladdr":
		return space, unix.RTM_NEWLINK
	case "v730nh":
		return space, unix.RTM_NEWNEXTHOP
	case "v730seg6":
		return space, unix.RTM_NEWROUTE
	}
	return space, unix.RTM_GETMULTICAST
}

func TestWsV730Corpus(t *testing.T) {
	var checked, refusals int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		space, typ := v730Space(row.ID)
		if !strings.HasPrefix(space, "v730") || space == "v730lladdr" {
			continue
		}
		at730 := strings.Contains(row.Env.Iproute2, "iproute2-7.3.0")
		t.Run(filepath.ToSlash(dir), func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), typ)
			if space == "v730maddr" {
				var fams []uint8
				for _, r := range reqs {
					fams = append(fams, r.body[0])
				}
				want := v730MaddrWant(row.Axes["maddr"])
				if !at730 || row.Expect.RefusedBy == wsRefusedIgnored {
					want = nil // procfs; or the lookup failed first
				}
				if !slices.Equal(fams, want) {
					t.Errorf("O3: RTM_GETMULTICAST families %v, want %v\n  cmd: %s", fams, want, row.Cmd)
				}
				checked++
				return
			}
			if row.Expect.RefusedBy == wsRefusedIproute2 || row.Expect.RefusedBy == wsRefusedAckFail {
				if len(reqs) != 0 {
					t.Errorf("O3: refused by %s, yet sent %d", row.Expect.RefusedBy, len(reqs))
				}
				refusals++
				return
			}
			if len(reqs) != 1 {
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), typ, row.Cmd)
			}
			var err error
			switch space {
			case "v730bond", "v730ovpn":
				err = v730LinkProblem(row.Axes, reqs[0].body)
			case "v730nh":
				if row.Axes["dst_port"] != "" {
					err = v730NexthopProblem(row.Axes, reqs[0].body)
				}
			case "v730seg6":
				err = v730Seg6Problem(row.Axes, reqs[0].body)
			}
			if err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
			checked++
		})
	}
	t.Logf("O3 checked %d v730 rows, %d refusals sending nothing", checked, refusals)
	if checked == 0 || refusals == 0 {
		t.Error("O3 checked nothing: are there v730 rows under testdata/*/ws{,-ip730}?")
	}
}

func TestV730Problems(t *testing.T) {
	be16 := func(n uint16) []byte { return binary.BigEndian.AppendUint16(nil, n) }
	le16 := func(n uint16) []byte { return binary.LittleEndian.AppendUint16(nil, n) }
	u32 := func(n uint32) []byte { return binary.LittleEndian.AppendUint32(nil, n) }
	nh := func(attrs ...[]byte) []byte { return cat(append([][]byte{make([]byte, NhMsgSizeCst)}, attrs...)...) }
	gw := rtattr(unix.NHA_GATEWAY, []byte{192, 0, 2, 2})
	fdb := rtattr(wsNhaFdb, nil)
	link := func(kind string, data ...[]byte) []byte {
		info := cat(rtattr(unix.IFLA_INFO_KIND, []byte(kind)), rtattr(unix.IFLA_INFO_DATA, cat(data...)))
		return ifiBody(0, 0, 0, rtattr(unix.IFLA_IFNAME, []byte("b0\x00")), rtattr(unix.IFLA_LINKINFO, info))
	}
	route := func(encap ...[]byte) []byte {
		return cat(make([]byte, RtMsgSizeCst), rtattr(unix.RTA_ENCAP, cat(encap...)), rtattr(unix.RTA_ENCAP_TYPE, le16(5)))
	}
	srh := rtattr(wsSeg6IptunnelSRH, make([]byte, 24))
	cases := []struct {
		description string
		check       func() error
		expected    string // "" or a substring of the error
	}{
		{"positive: dst_port 4789 at 7.3.0", func() error {
			return v730NexthopProblem(map[string]string{"fdb": "1", "dst_port": "4789"}, nh(gw, fdb, rtattr(wsNhaDstPort, be16(4789))))
		}, ""},
		{"negative: dst_port little-endian", func() error {
			return v730NexthopProblem(map[string]string{"fdb": "1", "dst_port": "4789"}, nh(gw, fdb, rtattr(wsNhaDstPort, le16(4789))))
		}, "(big-endian), want 4789"},
		{"negative: dst_port before fdb", func() error {
			return v730NexthopProblem(map[string]string{"fdb": "1", "dst_port": "4789"}, nh(gw, rtattr(wsNhaDstPort, be16(4789)), fdb))
		}, "not right after NHA_FDB"},
		{"boundary: dst_port as a u32", func() error {
			return v730NexthopProblem(map[string]string{"dst_port": "4789"}, nh(gw, rtattr(wsNhaDstPort, u32(4789))))
		}, "want two bytes"},
		{"positive: lacp_strict on", func() error {
			return v730LinkProblem(map[string]string{"kind": "bond", "lacp_strict": "1"}, link("bond", rtattr(wsIflaBondLacpStrict, []byte{1})))
		}, ""},
		{"negative: lacp_strict as a u32", func() error {
			return v730LinkProblem(map[string]string{"kind": "bond", "lacp_strict": "1"}, link("bond", rtattr(wsIflaBondLacpStrict, u32(1))))
		}, "want the u8 1"},
		{"corner: a plain bond sends no lacp_strict", func() error {
			return v730LinkProblem(map[string]string{"kind": "bond"}, link("bond", rtattr(wsIflaBondLacpStrict, []byte{0})))
		}, "want none"},
		{"positive: ovpn mode mp", func() error {
			return v730LinkProblem(map[string]string{"kind": "ovpn", "ovpn_mode": "1"}, link("ovpn", rtattr(wsIflaOvpnMode, []byte{1})))
		}, ""},
		{"negative: the wrong kind", func() error {
			return v730LinkProblem(map[string]string{"kind": "ovpn", "ovpn_mode": "1"}, link("bond", rtattr(wsIflaOvpnMode, []byte{1})))
		}, `kind "bond"`},
		{"positive: seg6 lookup 100", func() error {
			return v730Seg6Problem(map[string]string{"table": "100"}, route(srh, rtattr(wsSeg6IptunnelTable, u32(100))))
		}, ""},
		{"negative: the table before the SRH", func() error {
			return v730Seg6Problem(map[string]string{"table": "100"}, route(rtattr(wsSeg6IptunnelTable, u32(100)), srh))
		}, "the SRH then the table"},
		{"negative: the wrong table", func() error {
			return v730Seg6Problem(map[string]string{"table": "254"}, route(srh, rtattr(wsSeg6IptunnelTable, u32(100))))
		}, "want the u32 254"},
		{"corner: a short body", func() error { return v730NexthopProblem(nil, []byte{0}) }, "body of 1"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, tc.check(), tc.expected)
		})
	}
	fams := []struct {
		description string
		which       string
		expected    []uint8
	}{
		{"positive: maddress show dumps inet then inet6", "all", []uint8{unix.AF_INET, unix.AF_INET6}},
		{"positive: -4 dumps inet", "inet", []uint8{unix.AF_INET}},
		{"positive: -6 dumps inet6", "inet6", []uint8{unix.AF_INET6}},
	}
	for _, tc := range fams {
		t.Run(tc.description, func(t *testing.T) {
			if got := v730MaddrWant(tc.which); !slices.Equal(got, tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
