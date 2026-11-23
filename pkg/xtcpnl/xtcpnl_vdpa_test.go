package xtcpnl

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// Crafted bytes below are boundary/corner/malformed inputs modeled on the
// captured vdpa replies (testdata/*/ws/vdpa), NOT raw device evidence; the
// O3/O4 corpus test (xtcpnl_ws_vdpa_test.go) holds the parser to the real
// captures. They reuse the devlink tests' attribute builders (dlAttr, dlBody,
// join, ...).

// vdNetFeatures is the captured vdpasim_net's negotiated feature word.
const vdNetFeatures = 12893487144

func vdDevName() []byte { return dlAttr(vdpaAttrDevName, dlStr("vdpa0")...) }

func vdStat(name string, v uint64) []byte {
	return join(dlAttr(vdpaAttrDevVendorAttrName, dlStr(name)...), dlAttr(vdpaAttrDevVendorAttrValue, dlU64(v)...))
}

func vdStatNames(st []VdpaVendorStat) string {
	parts := make([]string, 0, len(st))
	for _, s := range st {
		if s.Name == nil {
			parts = append(parts, "<nil>")
			continue
		}
		parts = append(parts, *s.Name)
	}
	return strings.Join(parts, ",")
}

// TestVdpaPositive decodes one well-formed reply of each kind, held to the
// typed section and the values the captured replies show.
func TestVdpaPositive(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		expected    func(VdpaMessage) bool
	}{
		{
			"positive: mgmtdev reply decodes classes, max vqs and supported features",
			dlBody(1, join(
				dlAttr(vdpaAttrMgmtdevDevName, dlStr("vdpasim_net")...),
				dlAttr(vdpaAttrMgmtdevSupportedCls, dlU64(2)...),
				dlAttr(vdpaAttrDevMgmtdevMaxVQs, dlU32(3)...),
				dlAttr(vdpaAttrDevSupportedFeatures, dlU64(13027704872)...))),
			func(m VdpaMessage) bool {
				md := m.Mgmtdev
				return md != nil && m.MgmtdevDevName == "vdpasim_net" && m.MgmtdevBusName == "" &&
					eqU64(md.SupportedClasses, 2) && eqU32(md.MaxVQs, 3) &&
					eqU64(md.SupportedFeatures, 13027704872) && m.Dev == nil && m.Config == nil
			},
		},
		{
			"positive: a mgmtdev on a bus carries the bus name too",
			dlBody(1, join(
				dlAttr(vdpaAttrMgmtdevBusName, dlStr("pci")...),
				dlAttr(vdpaAttrMgmtdevDevName, dlStr("0000:00:04.0")...),
				dlAttr(vdpaAttrMgmtdevSupportedCls, dlU64(2)...))),
			func(m VdpaMessage) bool {
				return m.Mgmtdev != nil && m.MgmtdevBusName == "pci" && m.MgmtdevDevName == "0000:00:04.0"
			},
		},
		{
			"positive: dev reply decodes ids and queue limits, not the mgmtdev section",
			dlBody(3, join(
				dlAttr(vdpaAttrMgmtdevDevName, dlStr("vdpasim_net")...),
				vdDevName(),
				dlAttr(vdpaAttrDevID, dlU32(1)...),
				dlAttr(vdpaAttrDevVendorID, dlU32(0)...),
				dlAttr(vdpaAttrDevMaxVQs, dlU32(3)...),
				dlAttr(vdpaAttrDevMaxVQSize, dlU16(256)...),
				dlAttr(vdpaAttrDevMinVQSize, dlU16(1)...))),
			func(m VdpaMessage) bool {
				d := m.Dev
				return d != nil && m.Mgmtdev == nil && m.DevName == "vdpa0" && eqU32(m.DeviceID, 1) &&
					eqU32(d.VendorID, 0) && eqU32(d.MaxVQs, 3) && eqU16(d.MaxVQSize, 256) && eqU16(d.MinVQSize, 1)
			},
		},
		{
			"positive: net config reply decodes MAC, u16 status, MTU and both feature words",
			dlBody(6, join(
				vdDevName(),
				dlAttr(vdpaAttrDevID, dlU32(1)...),
				dlAttr(vdpaAttrDevNegotiatedFeat, dlU64(vdNetFeatures)...),
				dlAttr(vdpaAttrDevFeatures, dlU64(13027704872)...),
				dlAttr(vdpaAttrDevNetCfgMTU, dlU16(1500)...),
				dlAttr(vdpaAttrDevNetCfgMACAddr, 0, 0x11, 0x22, 0x33, 0x44, 0x55),
				dlAttr(vdpaAttrDevNetStatus, dlU16(1)...))),
			func(m VdpaMessage) bool {
				c := m.Config
				return c != nil && hex.EncodeToString(c.MAC) == "001122334455" && eqU16(c.Status, 1) &&
					eqU16(c.MTU, 1500) && c.MaxVQP == nil && eqU64(c.NegotiatedFeatures, vdNetFeatures) &&
					eqU64(c.Features, 13027704872)
			},
		},
		{
			"positive: a config with MQ negotiated carries max_vqp",
			dlBody(6, join(vdDevName(), dlAttr(vdpaAttrDevNetCfgMaxVQP, dlU16(4)...))),
			func(m VdpaMessage) bool { return m.Config != nil && eqU16(m.Config.MaxVQP, 4) },
		},
		{
			"positive: vstats reply keeps the rx counters in the kernel's order",
			dlBody(7, join(
				vdDevName(),
				dlAttr(vdpaAttrDevID, dlU32(1)...),
				dlAttr(vdpaAttrDevNegotiatedFeat, dlU64(vdNetFeatures)...),
				dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				vdStat("rx packets", 7), vdStat("rx bytes", 900), vdStat("rx errors", 0),
				vdStat("rx overruns", 0), vdStat("rx drops", 1))),
			func(m VdpaMessage) bool {
				v := m.Vstats
				return v != nil && m.Config == nil && eqU32(m.QueueIndex, 0) && v.MaxVQP == nil &&
					eqU64(v.NegotiatedFeatures, vdNetFeatures) &&
					vdStatNames(v.Stats) == "rx packets,rx bytes,rx errors,rx overruns,rx drops" &&
					v.Stats[0].Value == 7 && v.Stats[1].Value == 900 && v.Stats[4].Value == 1
			},
		},
		{
			"positive: a vstats reply with MQ negotiated carries max_vqp before the index",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevNetCfgMaxVQP, dlU16(2)...),
				dlAttr(vdpaAttrDevQueueIndex, dlU32(4)...), vdStat("cvq requests", 3))),
			func(m VdpaMessage) bool {
				v := m.Vstats
				return v != nil && eqU16(v.MaxVQP, 2) && eqU32(m.QueueIndex, 4) && len(v.Stats) == 1 && v.Stats[0].Value == 3
			},
		},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			m, err := ParseVdpa(r.body, 0)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !r.expected(m) {
				t.Fatalf("mismatch: %+v", m)
			}
		})
	}
}

// TestVdpaMalformed holds malformed inputs to a framing/width error without a
// panic or over-read.
func TestVdpaMalformed(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		expected    error
	}{
		{"negative: three-byte genlmsghdr is too short", []byte{3, 1, 0}, ErrGenericNetlink},
		{"negative: trailing partial attribute header", append(dlBody(3), 4, 0), ErrGenericNetlink},
		{"negative: attribute length exceeds the message", []byte{3, 1, 0, 0, 0xff, 0, 4, 0}, ErrGenericNetlink},
		{"negative: duplicate device name is ambiguous",
			dlBody(3, join(vdDevName(), dlAttr(vdpaAttrDevName, dlStr("vdpa1")...))), ErrGenericNetlink},
		{"negative: device name without its NUL",
			dlBody(3, dlAttr(vdpaAttrDevName, 'v', 'd')), ErrGenericNetlink},
		{"negative: a five-byte MAC",
			dlBody(6, join(vdDevName(), dlAttr(vdpaAttrDevNetCfgMACAddr, 1, 2, 3, 4, 5))), ErrGenericNetlink},
		{"negative: status as the uapi comment's u8, not the u16 the kernel puts",
			dlBody(6, join(vdDevName(), dlAttr(vdpaAttrDevNetStatus, 1))), ErrGenericNetlink},
		{"negative: queue index as the u16 vdpa reads, not the kernel's u32",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU16(0)...))), ErrGenericNetlink},
		{"negative: a four-byte vendor stat value",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				dlAttr(vdpaAttrDevVendorAttrName, dlStr("rx packets")...),
				dlAttr(vdpaAttrDevVendorAttrValue, dlU32(1)...))), ErrGenericNetlink},
		{"negative: a vendor stat name without its NUL",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				dlAttr(vdpaAttrDevVendorAttrName, 'r', 'x'))), ErrGenericNetlink},
		{"negative: NLA_F_NESTED on the scalar device id",
			dlBody(3, join(vdDevName(), dlAttr(vdpaAttrDevID|0x8000, dlU32(1)...))), ErrGenericNetlink},
		{"negative: NLA_F_NESTED on a vendor stat value",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				dlAttr(vdpaAttrDevVendorAttrValue|0x8000, dlU64(1)...))), ErrGenericNetlink},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			_, err := ParseVdpa(r.body, 0)
			if !errors.Is(err, r.expected) {
				t.Fatalf("got %v, want %v", err, r.expected)
			}
		})
	}
}

// TestVdpaBoundaryCorner covers empty, maximal and unexpected inputs.
func TestVdpaBoundaryCorner(t *testing.T) {
	rows := []struct {
		description string
		body        []byte
		flags       uint16
		expected    func(VdpaMessage) bool
	}{
		{
			"boundary: a bodiless genlmsghdr decodes to an empty message, no section",
			dlBody(1), 0,
			func(m VdpaMessage) bool {
				return m.DevName == "" && m.Mgmtdev == nil && m.Dev == nil && m.Config == nil && m.Vstats == nil
			},
		},
		{
			"boundary: a vstats reply with no counters is an empty, non-nil section",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(2)...))), 0,
			func(m VdpaMessage) bool { return m.Vstats != nil && len(m.Vstats.Stats) == 0 },
		},
		{
			"boundary: the largest queue index decodes as u32 max",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0xffffffff)...))), 0,
			func(m VdpaMessage) bool { return eqU32(m.QueueIndex, 0xffffffff) && m.Vstats != nil },
		},
		{
			"boundary: a counter at u64 max",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...), vdStat("rx bytes", 0xffffffffffffffff))), 0,
			func(m VdpaMessage) bool {
				return m.Vstats != nil && len(m.Vstats.Stats) == 1 && m.Vstats.Stats[0].Value == 0xffffffffffffffff
			},
		},
		{
			"corner: a value no name preceded has a nil name",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				dlAttr(vdpaAttrDevVendorAttrValue, dlU64(5)...))), 0,
			func(m VdpaMessage) bool {
				return m.Vstats != nil && vdStatNames(m.Vstats.Stats) == "<nil>" && m.Vstats.Stats[0].Value == 5
			},
		},
		{
			"corner: two names before one value: the later name wins, a trailing name is dropped",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...),
				dlAttr(vdpaAttrDevVendorAttrName, dlStr("a")...),
				vdStat("b", 1),
				dlAttr(vdpaAttrDevVendorAttrName, dlStr("c")...))), 0,
			func(m VdpaMessage) bool { return m.Vstats != nil && vdStatNames(m.Vstats.Stats) == "b" },
		},
		{
			"corner: a blk config attribute is left raw; the net config still decodes",
			dlBody(6, join(vdDevName(), dlAttr(21, dlU64(2048)...), dlAttr(vdpaAttrDevNetCfgMTU, dlU16(9000)...))), 0,
			func(m VdpaMessage) bool { return m.Config != nil && eqU16(m.Config.MTU, 9000) && m.Config.MAC == nil },
		},
		{
			"corner: an unknown VDPA_ATTR_* is ignored, the handle stays intact",
			dlBody(3, join(vdDevName(), dlAttr(250, 1, 2, 3, 4), dlAttr(vdpaAttrDevMaxVQs, dlU32(3)...))), 0,
			func(m VdpaMessage) bool { return m.DevName == "vdpa0" && m.Dev != nil && eqU32(m.Dev.MaxVQs, 3) },
		},
		{
			"corner: an unknown command still classifies by attributes, Command kept",
			dlBody(250, join(vdDevName(), dlAttr(vdpaAttrDevMaxVQs, dlU32(3)...))), 0,
			func(m VdpaMessage) bool { return m.Command == 250 && m.Dev != nil },
		},
		{
			"corner: a vstats request names its device and queue but carries no section",
			dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(1)...))), 0x1,
			func(m VdpaMessage) bool {
				return m.Request && m.DevName == "vdpa0" && eqU32(m.QueueIndex, 1) && m.Vstats == nil && m.Config == nil
			},
		},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			m, err := ParseVdpa(r.body, r.flags)
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if !r.expected(m) {
				t.Fatalf("mismatch: %+v", m)
			}
		})
	}
}

func vdU32(v uint32) *uint32 { return &v }

// TestBuildGetVdpaRequest holds the builder to the requests vdpa put on the
// wire (the hex rows are copied from testdata/7_3_1/ws/vdpa, family 40, with
// their captured seq) and to its refusals.
func TestBuildGetVdpaRequest(t *testing.T) {
	rows := []struct {
		description string
		family      uint16
		kind        VdpaKind
		dev         string
		qidx        *uint32
		seq         uint32
		expected    string // hex, or "error"
	}{
		{"positive: mgmtdev show is a dump with ACK (ec-001)", 40, VdpaMgmtdevKind, "", nil, 0x6af86e6e,
			"14000000280005036e6ef86a0000000002010000"},
		{"positive: dev show vdpa0 is a targeted GET (ec-003)", 40, VdpaDevKind, "vdpa0", nil, 0x6af86e6f,
			"20000000280005006f6ef86a00000000050100000a0004007664706130000000"},
		{"positive: vstats qidx 0 carries the u32 index (ec-005)", 40, VdpaVstatsKind, "vdpa0", vdU32(0), 0x6af86e70,
			"2800000028000500706ef86a00000000070100000a00040076647061300000000800110000000000"},
		{"corner: vstats without qidx is sent as vdpa sends it (ng-006)", 40, VdpaVstatsKind, "vdpa0", nil, 0x6af86e76,
			"2000000028000500766ef86a00000000070100000a0004007664706130000000"},
		{"positive: config show with no device is a dump", 40, VdpaConfigKind, "", nil, 1,
			"14000000280005030100000000000000" + "06010000"},
		{"boundary: a one-byte name pads its attribute to four bytes", 40, VdpaDevKind, "v", nil, 1,
			"1c000000280005000100000000000000050100000600040076000000"},
		{"boundary: the largest qidx", 40, VdpaVstatsKind, "v", vdU32(0xffffffff), 1,
			"24000000280005000100000000000000070100000600040076000000" + "08001100ffffffff"},
		{"negative: the controller's id is not vdpa", GenlControllerID, VdpaDevKind, "", nil, 1, "error"},
		{"negative: vstats needs a device", 40, VdpaVstatsKind, "", vdU32(0), 1, "error"},
		{"negative: qidx on a dev show", 40, VdpaDevKind, "vdpa0", vdU32(0), 1, "error"},
		{"negative: a targeted mgmtdev show is not built", 40, VdpaMgmtdevKind, "vdpasim_net", nil, 1, "error"},
		{"negative: an unknown kind", 40, VdpaKind(99), "", nil, 1, "error"},
		{"negative: a name with a NUL in it", 40, VdpaDevKind, "vd\x00pa", nil, 1, "error"},
		{"boundary: a 256-byte name is past the bound", 40, VdpaDevKind, strings.Repeat("v", 256), nil, 1, "error"},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			got, err := BuildGetVdpaRequest(r.family, r.kind, r.dev, r.qidx, r.seq)
			if r.expected == "error" {
				if err == nil {
					t.Fatalf("built % x, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h := hex.EncodeToString(got); h != r.expected {
				t.Fatalf("got  %s\nwant %s", h, r.expected)
			}
		})
	}
}

// TestBuildGetVdpaRequestRoundTrip: what the builder sends, ParseVdpa reads
// back as the same device and queue, as a request.
func TestBuildGetVdpaRequestRoundTrip(t *testing.T) {
	rows := []struct {
		description string
		kind        VdpaKind
		dev         string
		qidx        *uint32
		expected    string // dev/qidx
	}{
		{"positive: vstats", VdpaVstatsKind, "vdpa0", vdU32(2), "vdpa0/2"},
		{"positive: dev show", VdpaDevKind, "vdpa0", nil, "vdpa0/-"},
		{"boundary: the longest name", VdpaConfigKind, strings.Repeat("n", vdpaNameMax), nil, strings.Repeat("n", vdpaNameMax) + "/-"},
		{"boundary: a dump names nothing", VdpaMgmtdevKind, "", nil, "/-"},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			b, err := BuildGetVdpaRequest(40, r.kind, r.dev, r.qidx, 1)
			if err != nil {
				t.Fatal(err)
			}
			m, err := ParseVdpa(b[NlMsgHdrSizeCst:], 0x1)
			if err != nil {
				t.Fatal(err)
			}
			q := "-"
			if m.QueueIndex != nil {
				q = strconv.FormatUint(uint64(*m.QueueIndex), 10)
			}
			if got := m.DevName + "/" + q; got != r.expected || !m.Request {
				t.Fatalf("got %q request=%t, want %q", got, m.Request, r.expected)
			}
		})
	}
}

// FuzzParseVdpa seeds from the shapes above; arbitrary bytes must not panic
// or over-read, and a clean decode must hold the section invariants.
func FuzzParseVdpa(f *testing.F) {
	f.Add(dlBody(1, join(dlAttr(vdpaAttrMgmtdevDevName, dlStr("vdpasim_net")...), dlAttr(vdpaAttrMgmtdevSupportedCls, dlU64(2)...))), uint16(0))
	f.Add(dlBody(3, join(vdDevName(), dlAttr(vdpaAttrDevMaxVQs, dlU32(3)...))), uint16(0))
	f.Add(dlBody(6, join(vdDevName(), dlAttr(vdpaAttrDevNetCfgMACAddr, 0, 1, 2, 3, 4, 5), dlAttr(vdpaAttrDevNetStatus, dlU16(1)...))), uint16(0))
	f.Add(dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...), vdStat("rx packets", 1))), uint16(0))
	f.Add(dlBody(7, join(vdDevName(), dlAttr(vdpaAttrDevQueueIndex, dlU32(0)...))), uint16(1))
	f.Fuzz(func(t *testing.T, body []byte, flags uint16) {
		m, err := ParseVdpa(body, flags)
		if err != nil {
			return
		}
		n := 0
		for _, set := range []bool{m.Mgmtdev != nil, m.Dev != nil, m.Config != nil, m.Vstats != nil} {
			if set {
				n++
			}
		}
		if n > 1 || (m.Request && n != 0) {
			t.Fatalf("%d sections, request=%t", n, m.Request)
		}
		if m.Vstats != nil && m.QueueIndex == nil {
			t.Fatal("a vstats section without a queue index")
		}
		if m.Config != nil && m.Config.MAC != nil && len(m.Config.MAC) != 6 {
			t.Fatalf("a %d-byte MAC", len(m.Config.MAC))
		}
	})
}
