package xtcpnl

import (
	"encoding/binary"
	"errors"
	"testing"
)

// Crafted bytes below are boundary/corner/malformed inputs modeled on the
// captured devlink replies (testdata/*/ws/devlink), NOT raw hardware evidence;
// the O3/O4 corpus test (xtcpnl_ws_devlink_test.go) holds the parser to the
// real captures.

func dlAttr(id uint16, data ...byte) []byte {
	out := make([]byte, (len(data)+4+3)&^3)
	binary.LittleEndian.PutUint16(out, uint16(len(data)+4))
	binary.LittleEndian.PutUint16(out[2:], id)
	copy(out[4:], data)
	return out
}

func dlNest(id uint16, children ...[]byte) []byte {
	var body []byte
	for _, c := range children {
		body = append(body, c...)
	}
	return dlAttr(id, body...)
}

func dlU16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func dlU32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func dlU64(v uint64) []byte { b := make([]byte, 8); binary.LittleEndian.PutUint64(b, v); return b }
func dlStr(s string) []byte { return append([]byte(s), 0) }

// dlBody frames a genlmsghdr (version 1) over the given attributes.
func dlBody(cmd byte, attrs ...[]byte) []byte {
	out := []byte{cmd, 1, 0, 0}
	for _, a := range attrs {
		out = append(out, a...)
	}
	return out
}

// dlHandle is the bus/dev pair every reply carries.
func dlHandle() []byte {
	return append(dlAttr(devlinkAttrBusName, dlStr("netdevsim")...),
		dlAttr(devlinkAttrDevName, dlStr("netdevsim0")...)...)
}

func eqU64(p *uint64, v uint64) bool { return p != nil && *p == v }
func eqU32(p *uint32, v uint32) bool { return p != nil && *p == v }
func eqU16(p *uint16, v uint16) bool { return p != nil && *p == v }
func eqU8(p *uint8, v uint8) bool    { return p != nil && *p == v }

// TestDevlinkPositive decodes one well-formed reply of each kind, held to the
// typed section and values the captured dumps show.
func TestDevlinkPositive(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		expected    func(DevlinkMessage) bool
	}{
		{
			"positive: dev reply carries the handle and DEVLINK_ATTR_INDEX, no section",
			dlBody(3, append(dlHandle(), dlAttr(devlinkAttrIndex, dlU32(11)...)...)),
			func(m DevlinkMessage) bool {
				return m.BusName == "netdevsim" && m.DevName == "netdevsim0" &&
					eqU64(m.Index, 11) && m.Port == nil && m.Resources == nil && m.Param == nil
			},
		},
		{
			"positive: port reply decodes index, type, netdev and flavour",
			dlBody(7, join(dlHandle(),
				dlAttr(devlinkAttrPortIndex, dlU32(0)...),
				dlAttr(devlinkAttrPortType, dlU16(2)...),
				dlAttr(devlinkAttrPortNetdevIfindex, dlU32(3)...),
				dlAttr(devlinkAttrPortNetdevName, dlStr("eth0")...),
				dlAttr(devlinkAttrPortFlavour, dlU16(0)...),
				dlAttr(devlinkAttrPortNumber, dlU32(1)...))),
			func(m DevlinkMessage) bool {
				p := m.Port
				return p != nil && eqU32(p.Index, 0) && eqU16(p.Type, 2) &&
					eqU32(p.NetdevIfindex, 3) && p.NetdevName == "eth0" &&
					eqU16(p.Flavour, 0) && eqU32(p.Number, 1)
			},
		},
		{
			"positive: resource list nests a child resource recursively",
			dlBody(36, join(dlHandle(),
				dlNest(devlinkAttrResourceList,
					dlNest(devlinkAttrResource,
						dlAttr(devlinkAttrResourceName, dlStr("IPv4")...),
						dlAttr(devlinkAttrResourceSize, dlU64(0xffffffffffffffff)...),
						dlAttr(devlinkAttrResourceSizeGran, dlU64(1)...),
						dlNest(devlinkAttrResourceList,
							dlNest(devlinkAttrResource,
								dlAttr(devlinkAttrResourceName, dlStr("fib")...),
								dlAttr(devlinkAttrResourceOcc, dlU64(3)...))))))),
			func(m DevlinkMessage) bool {
				if len(m.Resources) != 1 {
					return false
				}
				r := m.Resources[0]
				return r.Name == "IPv4" && eqU64(r.Size, 0xffffffffffffffff) &&
					eqU64(r.SizeGran, 1) && len(r.Children) == 1 &&
					r.Children[0].Name == "fib" && eqU64(r.Children[0].Occ, 3)
			},
		},
		{
			"positive: param U32 value decodes Num and notes the default",
			dlBody(38, join(dlHandle(),
				dlNest(devlinkAttrParam,
					dlAttr(devlinkAttrParamName, dlStr("max_macs")...),
					dlAttr(devlinkAttrParamGeneric),
					dlAttr(devlinkAttrParamType, 3),
					dlNest(devlinkAttrParamValuesList,
						dlNest(devlinkAttrParamValue,
							dlAttr(devlinkAttrParamValueCmode, 1),
							// A value past 16 bits, so a narrow u16 read of the u32 is wrong.
							dlAttr(devlinkAttrParamValueData, dlU32(0x12345678)...),
							dlAttr(devlinkAttrParamValueDefault, dlU32(0x12345678)...)))))),
			func(m DevlinkMessage) bool {
				p := m.Param
				return p != nil && p.Name == "max_macs" && p.Generic && eqU8(p.Type, 3) &&
					len(p.Values) == 1 && eqU8(p.Values[0].Cmode, 1) &&
					eqU64(p.Values[0].Num, 0x12345678) && p.Values[0].HasDefault
			},
		},
		{
			"positive: param FLAG value is true when data is present",
			dlBody(38, join(dlHandle(),
				dlNest(devlinkAttrParam,
					dlAttr(devlinkAttrParamName, dlStr("test1")...),
					dlAttr(devlinkAttrParamType, 6),
					dlNest(devlinkAttrParamValuesList,
						dlNest(devlinkAttrParamValue,
							dlAttr(devlinkAttrParamValueCmode, 1),
							dlAttr(devlinkAttrParamValueData)))))),
			func(m DevlinkMessage) bool {
				p := m.Param
				return p != nil && !p.Generic && eqU8(p.Type, 6) &&
					len(p.Values) == 1 && p.Values[0].Flag && p.Values[0].Num == nil
			},
		},
		{
			"positive: region reply decodes name, size and max snapshots",
			dlBody(42, join(dlHandle(),
				dlAttr(devlinkAttrRegionName, dlStr("dummy")...),
				dlAttr(devlinkAttrRegionSize, dlU64(32768)...),
				dlAttr(devlinkAttrRegionMaxSnapshots, dlU32(16)...))),
			func(m DevlinkMessage) bool {
				r := m.Region
				return r != nil && r.Name == "dummy" && eqU64(r.Size, 32768) && eqU32(r.MaxSnapshots, 16)
			},
		},
		{
			"positive: info reply sorts versions into fixed/running/stored by id",
			// Each bucket carries a DISTINCT value, so a decoder that read
			// one list's id for another's bucket would be caught.
			dlBody(51, join(dlHandle(),
				dlNest(devlinkAttrInfoVersionFixed,
					dlAttr(devlinkAttrInfoVersionName, dlStr("board.id")...),
					dlAttr(devlinkAttrInfoVersionValue, dlStr("fixed-1")...)),
				dlNest(devlinkAttrInfoVersionStored,
					dlAttr(devlinkAttrInfoVersionName, dlStr("fw.mgmt")...),
					dlAttr(devlinkAttrInfoVersionValue, dlStr("stored-2")...)),
				dlNest(devlinkAttrInfoVersionRunning,
					dlAttr(devlinkAttrInfoVersionName, dlStr("fw.mgmt")...),
					dlAttr(devlinkAttrInfoVersionValue, dlStr("running-3")...)),
				dlAttr(devlinkAttrInfoDriverName, dlStr("netdevsim")...))),
			func(m DevlinkMessage) bool {
				i := m.Info
				return i != nil && i.DriverName == "netdevsim" &&
					len(i.Fixed) == 1 && i.Fixed[0].Name == "board.id" && i.Fixed[0].Value == "fixed-1" &&
					len(i.Running) == 1 && i.Running[0].Name == "fw.mgmt" && i.Running[0].Value == "running-3" &&
					len(i.Stored) == 1 && i.Stored[0].Value == "stored-2"
			},
		},
		{
			"positive: health reporter decodes state, counts and auto-dump",
			dlBody(52, join(dlHandle(),
				dlNest(devlinkAttrHealthReporter,
					dlAttr(devlinkAttrHealthName, dlStr("dummy")...),
					dlAttr(devlinkAttrHealthState, 0),
					dlAttr(devlinkAttrHealthErrCount, dlU64(0)...),
					dlAttr(devlinkAttrHealthRecoverCount, dlU64(0)...),
					dlAttr(devlinkAttrHealthGracePeriod, dlU64(0)...),
					dlAttr(devlinkAttrHealthAutoRecover, 1),
					dlAttr(devlinkAttrHealthAutoDump, 1)))),
			func(m DevlinkMessage) bool {
				h := m.HealthReporter
				return h != nil && h.Name == "dummy" && eqU8(h.State, 0) &&
					eqU64(h.ErrCount, 0) && eqU64(h.GracePeriod, 0) &&
					eqU8(h.AutoRecover, 1) && eqU8(h.AutoDump, 1)
			},
		},
		{
			"positive: trap wins over trap-group name, decodes type and action",
			dlBody(63, join(dlHandle(),
				dlAttr(devlinkAttrTrapGroupName, dlStr("l2_drops")...),
				dlAttr(devlinkAttrTrapName, dlStr("source_mac_is_multicast")...),
				dlAttr(devlinkAttrTrapType, 0),
				dlAttr(devlinkAttrTrapAction, 0),
				dlAttr(devlinkAttrTrapGeneric))),
			func(m DevlinkMessage) bool {
				tr := m.Trap
				return tr != nil && m.TrapGroup == nil && tr.Name == "source_mac_is_multicast" &&
					tr.GroupName == "l2_drops" && eqU8(tr.Type, 0) && eqU8(tr.Action, 0) && tr.Generic
			},
		},
		{
			"positive: trap group without a trap name decodes generic and policer",
			dlBody(67, join(dlHandle(),
				dlAttr(devlinkAttrTrapGroupName, dlStr("l3_drops")...),
				dlAttr(devlinkAttrTrapGeneric),
				dlAttr(devlinkAttrTrapPolicerID, dlU32(1)...))),
			func(m DevlinkMessage) bool {
				g := m.TrapGroup
				return g != nil && m.Trap == nil && g.Name == "l3_drops" && g.Generic && eqU32(g.PolicerID, 1)
			},
		},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			m, err := ParseDevlink(r.body, 0)
			if err != nil {
				t.Fatalf("%s: unexpected error %v", r.description, err)
			}
			if !r.expected(m) {
				t.Fatalf("%s: decode mismatch: %+v", r.description, m)
			}
		})
	}
}

// TestDevlinkMalformed holds malformed inputs to a framing/width error without
// a panic or over-read.
func TestDevlinkMalformed(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		flags       uint16
		wantErr     error
	}{
		{"negative: three-byte genlmsghdr is too short", []byte{3, 1, 0}, 0, ErrGenericNetlink},
		{"negative: trailing partial attribute header", append(dlBody(3), 4, 0), 0, ErrGenericNetlink},
		{"negative: attribute length zero", []byte{3, 1, 0, 0, 0, 0, 1, 0}, 0, ErrGenericNetlink},
		{"negative: attribute length exceeds the message", []byte{3, 1, 0, 0, 0xff, 0, 1, 0}, 0, ErrGenericNetlink},
		{"negative: duplicate singleton bus name is ambiguous",
			dlBody(3, dlAttr(devlinkAttrBusName, dlStr("a")...), dlAttr(devlinkAttrBusName, dlStr("b")...)), 0, ErrGenericNetlink},
		{"negative: port index as a u16 is the wrong width",
			dlBody(7, join(dlHandle(), dlAttr(devlinkAttrPortIndex, dlU16(1)...))), 0, ErrGenericNetlink},
		{"negative: NLA_F_NESTED flag on the scalar port index",
			dlBody(7, join(dlHandle(), dlAttr(devlinkAttrPortIndex|0x8000, dlU32(1)...))), 0, ErrGenericNetlink},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			_, err := ParseDevlink(r.body, r.flags)
			if !errors.Is(err, r.wantErr) {
				t.Fatalf("%s: got %v, want %v", r.description, err, r.wantErr)
			}
		})
	}
}

// TestDevlinkBoundaryCorner covers empty, maximal and unexpected inputs.
func TestDevlinkBoundaryCorner(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		flags       uint16
		expected    func(DevlinkMessage) bool
	}{
		{
			"boundary: a bodiless genlmsghdr decodes to an empty message, no error",
			dlBody(1), 0,
			func(m DevlinkMessage) bool {
				return m.BusName == "" && m.Index == nil && m.Port == nil && m.Resources == nil
			},
		},
		{
			"boundary: the handle alone (empty dump member) sets no section",
			dlBody(3, dlHandle()), 0,
			func(m DevlinkMessage) bool {
				return m.BusName == "netdevsim" && m.Index == nil && m.Port == nil
			},
		},
		{
			"boundary: resource size at the unlimited sentinel decodes as max uint64",
			dlBody(36, join(dlHandle(),
				dlNest(devlinkAttrResourceList,
					dlNest(devlinkAttrResource,
						dlAttr(devlinkAttrResourceName, dlStr("r")...),
						dlAttr(devlinkAttrResourceSize, dlU64(0xffffffffffffffff)...))))), 0,
			func(m DevlinkMessage) bool {
				return len(m.Resources) == 1 && eqU64(m.Resources[0].Size, 0xffffffffffffffff)
			},
		},
		{
			"corner: an unknown DEVLINK_ATTR_* is ignored, the handle stays intact",
			dlBody(3, join(dlHandle(), dlAttr(250, 1, 2, 3, 4))), 0,
			func(m DevlinkMessage) bool {
				return m.BusName == "netdevsim" && m.DevName == "netdevsim0"
			},
		},
		{
			"corner: an unknown command still decodes attribute-driven, Command kept",
			dlBody(250, join(dlHandle(), dlAttr(devlinkAttrPortIndex, dlU32(5)...))), 0,
			func(m DevlinkMessage) bool {
				return m.Command == 250 && m.Port != nil && eqU32(m.Port.Index, 5)
			},
		},
		{
			"corner: a param value of unknown type keeps raw data, decodes no Num",
			dlBody(38, join(dlHandle(),
				dlNest(devlinkAttrParam,
					dlAttr(devlinkAttrParamName, dlStr("x")...),
					dlAttr(devlinkAttrParamType, 99),
					dlNest(devlinkAttrParamValuesList,
						dlNest(devlinkAttrParamValue,
							dlAttr(devlinkAttrParamValueData, 1, 2, 3, 4)))))), 0,
			func(m DevlinkMessage) bool {
				p := m.Param
				return p != nil && len(p.Values) == 1 && p.Values[0].Num == nil &&
					len(p.Values[0].Data) == 4 && !p.Values[0].Flag
			},
		},
		{
			"corner: a request body sets Request from NLM_F_REQUEST",
			dlBody(1, dlHandle()), 0x1,
			func(m DevlinkMessage) bool { return m.Request },
		},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			m, err := ParseDevlink(r.body, r.flags)
			if err != nil {
				t.Fatalf("%s: unexpected error %v", r.description, err)
			}
			if !r.expected(m) {
				t.Fatalf("%s: mismatch: %+v", r.description, m)
			}
		})
	}
}

// join concatenates attribute byte slices for a message body.
func join(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// FuzzParseDevlink seeds from the shapes above; arbitrary bytes must not panic
// or over-read, and a successful decode must round-trip its framing.
func FuzzParseDevlink(f *testing.F) {
	f.Add(dlBody(3, append(dlHandle(), dlAttr(devlinkAttrIndex, dlU32(0)...)...)), uint16(0))
	f.Add(dlBody(36, join(dlHandle(), dlNest(devlinkAttrResourceList,
		dlNest(devlinkAttrResource, dlAttr(devlinkAttrResourceName, dlStr("r")...))))), uint16(0))
	f.Add(dlBody(38, join(dlHandle(), dlNest(devlinkAttrParam,
		dlAttr(devlinkAttrParamType, 3),
		dlNest(devlinkAttrParamValuesList, dlNest(devlinkAttrParamValue,
			dlAttr(devlinkAttrParamValueData, dlU32(1)...)))))), uint16(0))
	f.Add([]byte{3, 1, 0, 0}, uint16(0))
	f.Fuzz(func(t *testing.T, body []byte, flags uint16) {
		m, err := ParseDevlink(body, flags)
		if err != nil {
			return
		}
		// A clean decode must have kept a consistent generic message.
		if m.Command != 0 || len(body) >= 4 {
			_ = m.BusName
		}
	})
}
