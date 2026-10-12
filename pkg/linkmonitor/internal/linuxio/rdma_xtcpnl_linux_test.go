package linuxio

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

var rdmaResGet = xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdResGet)

// The captured fixtures are what Linux 7.1.4 sent rdma 7.1.0 about rxe0 over
// the veth rx0, in pkg/xtcpnl/testdata/7_1_4/rdma/ (`nix run
// .#microvm-x86_64-rdma-capture`). The bodies follow the 16-byte nlmsghdr.
var (
	// capturedRDMADevice is link_show.strace's device dump reply: the handle,
	// CAP_FLAGS, both GUIDs, NODE_TYPE, DEV_DIM, NAME_ASSIGN_TYPE and
	// DEV_PROTOCOL "roce".
	capturedRDMADevice = rdmaHex("080001000000000009000200727865300000000008000300010000000c000400763c2201c00100000c000600005200feff0000000c000700005200feff00000005000e00010000000500540000000000050065000100000009004300726f636500000000")
	// capturedRDMAPort is link_show_one.strace's port doit reply, typed GET:
	// rxe0/1 ACTIVE, LINK_UP, over rx0 (ifindex 4).
	capturedRDMAPort = rdmaHex("0800010000000000090002007278653000000000080003000100000005000c000400000005000d000500000008003200040000000800330072783000")
	// capturedRDMASys is sys_show.strace's SYS_GET reply: NETNS_MODE 1,
	// PRIVILEGED_QKEY_MODE 0, MONITOR_MODE 1 and COPY_ON_FORK 1.
	capturedRDMASys = rdmaHex("05004200010000000500600000000000050067000100000005005d0001000000")
	// captured61RDMADevice is 6.1.177's link_show.strace device dump reply
	// (pkg/xtcpnl/testdata/6_1_177/rdma, `nix run
	// .#microvm-x86_64-rdma-capture-6_1`): no NAME_ASSIGN_TYPE, and a
	// CAP_FLAGS without the bits from 38 up.
	captured61RDMADevice = rdmaHex("080001000000000009000200727865300000000008000300010000000c000400763c2201000000000c000600005200feff0000000c000700005200feff00000005000e0001000000050054000000000009004300726f636500000000")
	// captured66RDMASys is 6.6.144's sys_show.strace SYS_GET reply:
	// NETNS_MODE and COPY_ON_FORK, and neither PRIVILEGED_QKEY_MODE nor
	// MONITOR_MODE, which that kernel does not send.
	captured66RDMASys = rdmaHex("050042000100000005005d0001000000")
	// capturedRDMAEvents is monitor_add.strace's three MONITOR datagrams, as
	// one buffer: rxe1's REGISTER, its NETDEV_ATTACH to rx1 and UNREGISTER.
	capturedRDMAEvents = rdmaHex("2c0000001c140000000000000000000008000100010000000900020072786531000000000500660000000000" +
		"440000001c140000000000000000000008000100010000000900020072786531000000000800030001000000080032000300000008003300727831000500660002000000" +
		"2c0000001c140000000000000000000008000100010000000900020072786531000000000500660001000000")
)

func rdmaHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// TestRDMARequestBytes pins every request linkmonitor sends on NETLINK_RDMA.
// The bytes are rdma's with NLM_F_ACK cleared; see rdmaRequest.
func TestRDMARequestBytes(t *testing.T) {
	const seq = 7
	for _, tc := range []struct {
		description string
		command     uint16
		index, port uint32
		dump        bool
		expected    string
		expectedErr error
	}{
		{description: "positive: device dump, REQUEST|DUMP and no attributes",
			command: rdmaDeviceGet, dump: true, expected: "10000000011401030700000000000000"},
		{description: "positive: single port, link_show_one's doit without NLM_F_ACK",
			command: rdmaPortGet, port: 1,
			expected: "20000000051401000700000000000000" + "0800010000000000" + "0800030001000000"},
		{description: "positive: SYS_GET, sys_show's doit without NLM_F_ACK",
			command: rdmaSysGet, expected: "10000000061401000700000000000000"},
		{description: "negative: a write command is refused by xtcpnl",
			command: xtcpnl.RdmaNlGetType(xtcpnl.RdmaNlNldev, xtcpnl.RdmaNldevCmdDellink), expectedErr: xtcpnl.ErrRdmaCmd},
		{description: "boundary: port dump of device index zero still sends DEV_INDEX",
			command: rdmaPortGet, dump: true, expected: "18000000051401030700000000000000" + "0800010000000000"},
		{description: "corner: port dump of device 3, the DEV_INDEX attribute bytes",
			command: rdmaPortGet, index: 3, dump: true, expected: "18000000051401030700000000000000" + "0800010003000000"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got, err := rdmaRequest(tc.command, tc.index, tc.port, seq, tc.dump)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if want := rdmaHex(tc.expected); !bytes.Equal(got, want) {
				t.Fatalf("request\n got %x\nwant %x", got, want)
			}
		})
	}
}

// TestRDMALegacyNumbers pins that moving onto xtcpnl's names changed no
// number linkmonitor used to spell out.
func TestRDMALegacyNumbers(t *testing.T) {
	var portSeen [xtcpnl.RdmaNldevAttrNdevName + 1]bool
	var sysSeen [rdmaMonitorMode + 1]bool
	for _, tc := range []struct {
		description string
		got         []uint64
		expected    []uint64
	}{
		{description: "positive: the request and event types",
			got:      []uint64{uint64(rdmaDeviceGet), uint64(rdmaPortGet), uint64(rdmaSysGet), uint64(rdmaMonitor)},
			expected: []uint64{0x1401, 0x1405, 0x1406, 0x141c}},
		{description: "positive: the attributes linkmonitor reads",
			got: []uint64{uint64(xtcpnl.RdmaNldevAttrDevIndex), uint64(xtcpnl.RdmaNldevAttrDevName),
				uint64(xtcpnl.RdmaNldevAttrPortIndex), uint64(xtcpnl.RdmaNldevAttrPortState),
				uint64(xtcpnl.RdmaNldevAttrPortPhysState), uint64(xtcpnl.RdmaNldevAttrNdevIndex),
				uint64(xtcpnl.RdmaNldevAttrNdevName), uint64(rdmaEventType), uint64(rdmaMonitorMode)},
			expected: []uint64{1, 2, 3, 12, 13, 50, 51, 102, 103}},
		{description: "negative: the notify group is 4, which is not the attribute 4 (CAP_FLAGS)",
			got: []uint64{uint64(xtcpnl.RdmaNlGroupNotify), uint64(xtcpnl.RdmaNldevAttrCapFlags)}, expected: []uint64{4, 4}},
		{description: "boundary: the seen arrays are sized as before",
			got: []uint64{uint64(len(portSeen)), uint64(len(sysSeen))}, expected: []uint64{52, 104}},
		{description: "corner: the port states the policy reads, and their ranges",
			got: []uint64{uint64(xtcpnl.IbPortActive), uint64(xtcpnl.IbPortPhysStateLinkUp),
				uint64(xtcpnl.IbPortDown), uint64(xtcpnl.IbPortActiveDefer),
				uint64(xtcpnl.IbPortPhysStateSleep), uint64(xtcpnl.IbPortPhysStatePhyTest)},
			expected: []uint64{4, 5, 1, 5, 1, 7}},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if len(tc.got) != len(tc.expected) {
				t.Fatalf("%d values, want %d", len(tc.got), len(tc.expected))
			}
			for i := range tc.got {
				if tc.got[i] != tc.expected[i] {
					t.Errorf("value %d is %d, want %d", i, tc.got[i], tc.expected[i])
				}
			}
		})
	}
}

// TestRDMACapturedFixtures checks the fixtures are the messages they claim to
// be, so a decoder row built on one is not passing on the wrong bytes.
func TestRDMACapturedFixtures(t *testing.T) {
	for _, tc := range []struct {
		description string
		msgType     uint16
		body        []byte
		expected    func(any) bool
	}{
		{description: "positive: the device fixture is rxe0 with one port", msgType: rdmaDeviceGet, body: capturedRDMADevice,
			expected: func(v any) bool { d, ok := v.(xtcpnl.RdmaDev); return ok && d.DevName == "rxe0" && d.NumPorts == 1 }},
		{description: "positive: the port fixture is rxe0/1 over rx0", msgType: rdmaDeviceGet, body: capturedRDMAPort,
			expected: func(v any) bool { p, ok := v.(xtcpnl.RdmaPort); return ok && p.PortIndex == 1 && p.NdevName == "rx0" }},
		{description: "boundary: the sys fixture has all four sys attributes", msgType: rdmaSysGet, body: capturedRDMASys,
			expected: func(v any) bool {
				s, ok := v.(xtcpnl.RdmaSys)
				return ok && s.HasNetnsMode && s.HasPrivilegedQKeyMode && s.HasMonitorMode && s.HasCopyOnFork
			}},
		{description: "corner: no fixture body has an attribute xtcpnl does not know", msgType: rdmaDeviceGet, body: capturedRDMADevice,
			expected: func(v any) bool { d, ok := v.(xtcpnl.RdmaDev); return ok && len(d.Unknown) == 0 }},
		{description: "boundary: the 6.1 device fixture is rxe0 without NAME_ASSIGN_TYPE", msgType: rdmaDeviceGet, body: captured61RDMADevice,
			expected: func(v any) bool {
				d, ok := v.(xtcpnl.RdmaDev)
				return ok && d.DevName == "rxe0" && !d.HasNameAssignType && d.CapFlags == 0x1223c76 && len(d.Unknown) == 0
			}},
		{description: "boundary: the 6.6 sys fixture has no MONITOR_MODE", msgType: rdmaSysGet, body: captured66RDMASys,
			expected: func(v any) bool {
				s, ok := v.(xtcpnl.RdmaSys)
				return ok && s.HasNetnsMode && s.HasCopyOnFork && !s.HasPrivilegedQKeyMode && !s.HasMonitorMode
			}},
	} {
		t.Run(tc.description, func(t *testing.T) {
			v, err := xtcpnl.ParseRdmaMsg(tc.msgType, tc.body)
			if err != nil || !tc.expected(v) {
				t.Fatalf("%+v, %v", v, err)
			}
		})
	}
	t.Run("negative: the event fixture is three MONITOR datagrams and nothing else", func(t *testing.T) {
		n := 0
		err := xtcpnl.WalkNetlinkEnvelopes(capturedRDMAEvents, func(e xtcpnl.NetlinkEnvelope) error {
			if e.Header.Type != rdmaMonitor {
				return ErrReply
			}
			n++
			return nil
		})
		if err != nil || n != 3 {
			t.Fatalf("%d events, %v", n, err)
		}
	})
}
