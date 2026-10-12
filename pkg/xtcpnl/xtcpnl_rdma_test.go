package xtcpnl

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// Hand-built nldev bodies for the decoders' edges; xtcpnl_rdma_corpus_test.go
// holds them to what a 7.1.4 kernel sent.

func rdU8(atype uint16, v uint8) []byte       { return rtattr(atype, []byte{v}) }
func rdU32(atype uint16, v uint32) []byte     { return rtattr(atype, le32(v)) }
func rdU64(atype uint16, v uint64) []byte     { return rtattr(atype, le64(v)) }
func rdStr(atype uint16, s string) []byte     { return rtattr(atype, append([]byte(s), 0)) }
func rdNest(atype uint16, b ...[]byte) []byte { return rtattr(atype, bytes.Join(b, nil)) }
func rdBody(b ...[]byte) []byte               { return bytes.Join(b, nil) }

func TestParseRdmaMsg(t *testing.T) {
	get := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGet)
	portGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdPortGet)
	resGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResGet)
	statGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet)
	sysGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdSysGet)
	monitor := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdMonitor)
	handle := rdBody(rdU32(RdmaNldevAttrDevIndex, 0), rdStr(RdmaNldevAttrDevName, "rxe0"))
	rxe0 := RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true}
	rxe0p1 := RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true, PortIndex: 1, HasPortIndex: true}
	rx0 := RdmaNetdev{NdevIndex: 4, HasNdevIndex: true, NdevName: "rx0", HasNdevName: true}
	ndev := rdBody(rdU32(RdmaNldevAttrNdevIndex, 4), rdStr(RdmaNldevAttrNdevName, "rx0"))
	roce := rdBody(handle, rdU32(RdmaNldevAttrPortIndex, 1),
		rdU8(RdmaNldevAttrPortState, IbPortActive), rdU8(RdmaNldevAttrPortPhysState, IbPortPhysStateLinkUp), ndev)
	rocePort := RdmaPort{RdmaHandle: rxe0p1, RdmaNetdev: rx0,
		State: IbPortActive, HasState: true, PhysState: IbPortPhysStateLinkUp, HasPhysState: true}
	entry := func(name string, curr uint64) []byte {
		return rdNest(RdmaNldevAttrResSummaryEntry, rdStr(RdmaNldevAttrResSummaryEntryName, name),
			rdU64(RdmaNldevAttrResSummaryEntryCurr, curr))
	}
	counter := func(name string, v uint64) []byte {
		return rdNest(RdmaNldevAttrStatHwcounterEntry, rdStr(RdmaNldevAttrStatHwcounterEntryName, name),
			rdU64(RdmaNldevAttrStatHwcounterEntryValue, v))
	}
	tests := []struct {
		description string
		msgType     uint16
		body        []byte
		expected    any
		expectedErr error
	}{
		{description: "positive: dev minimal", msgType: get, body: handle,
			expected: RdmaDev{HasDevIndex: true, DevName: "rxe0", HasDevName: true}},
		{description: "positive: dev, every field", msgType: get, body: rdBody(handle,
			rdU32(RdmaNldevAttrPortIndex, 1), rdU64(RdmaNldevAttrCapFlags, 0x1c001223c76),
			rdStr(RdmaNldevAttrFwVersion, "1.0"), rdU64(RdmaNldevAttrNodeGUID, 0xfffe005200),
			rdU64(RdmaNldevAttrSysImageGUID, 0xfffe005201), rdU8(RdmaNldevAttrDevNodeType, 1),
			rdStr(RdmaNldevAttrDevProtocol, "roce"), rdU8(RdmaNldevAttrDevDIM, 1),
			rdU8(RdmaNldevAttrNameAssignType, 1), rdU8(RdmaNldevAttrDevType, 1),
			rdStr(RdmaNldevAttrParentName, "mlx5_0")),
			expected: RdmaDev{HasDevIndex: true, DevName: "rxe0", HasDevName: true, NumPorts: 1, HasNumPorts: true,
				CapFlags: 0x1c001223c76, HasCapFlags: true, FwVersion: "1.0", HasFwVersion: true,
				NodeGUID: 0xfffe005200, HasNodeGUID: true, SysImageGUID: 0xfffe005201, HasSysImageGUID: true,
				NodeType: 1, HasNodeType: true, DevProtocol: "roce", HasDevProtocol: true, DIM: 1, HasDIM: true,
				NameAssignType: 1, HasNameAssignType: true, DevType: 1, HasDevType: true,
				ParentName: "mlx5_0", HasParentName: true}},
		{description: "positive: port on roce has no LID, LMC or subnet prefix", msgType: portGet, body: roce,
			expected: rocePort},
		{description: "positive: port on IB carries the IB-only attributes", msgType: portGet, body: rdBody(roce,
			rdU64(RdmaNldevAttrCapFlags, 0x2651e848), rdU64(RdmaNldevAttrSubnetPrefix, 0xfe80000000000000),
			rdU32(RdmaNldevAttrLID, 3), rdU32(RdmaNldevAttrSMLID, 1), rdU8(RdmaNldevAttrLMC, 0)),
			expected: RdmaPort{RdmaHandle: rxe0p1, RdmaNetdev: rx0,
				State: IbPortActive, HasState: true, PhysState: IbPortPhysStateLinkUp, HasPhysState: true,
				CapFlags: 0x2651e848, HasCapFlags: true, SubnetPrefix: 0xfe80000000000000, HasSubnetPrefix: true,
				LID: 3, HasLID: true, SMLID: 1, HasSMLID: true, HasLMC: true}},
		{description: "positive: res summary in wire order", msgType: resGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrResSummary, entry("pd", 1), entry("cm_id", 0))),
			expected: RdmaRes{RdmaHandle: rxe0, Summary: []RdmaResEntry{
				{Name: "pd", Curr: 1, HasCurr: true}, {Name: "cm_id", HasCurr: true}}}},
		{description: "positive: stat counters, names and u64 values in order", msgType: statGet,
			body: rdBody(handle, rdU32(RdmaNldevAttrPortIndex, 1), rdNest(RdmaNldevAttrStatHwcounters,
				counter("sent_pkts", 1<<40), counter("rcvd_pkts", 2), counter("link_downed", 0))),
			expected: RdmaStat{RdmaHandle: rxe0p1, HwCounters: []RdmaHwCounter{
				{Name: "sent_pkts", Value: 1 << 40, HasValue: true}, {Name: "rcvd_pkts", Value: 2, HasValue: true},
				{Name: "link_downed", HasValue: true}}}},
		{description: "positive: a STAT_GET_STATUS-style entry carries index and dynamic", msgType: statGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrStatHwcounters, rdNest(RdmaNldevAttrStatHwcounterEntry,
				rdStr(RdmaNldevAttrStatHwcounterEntryName, "x"), rdU32(RdmaNldevAttrStatHwcounterIndex, 7),
				rdU8(RdmaNldevAttrStatHwcounterDynamic, 1)))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{
				{Name: "x", Index: 7, HasIndex: true, Dynamic: 1, HasDynamic: true}}}},
		{description: "positive: sys modes", msgType: sysGet, body: rdBody(rdU8(RdmaNldevSysAttrNetnsMode, 1),
			rdU8(RdmaNldevSysAttrPrivilegedQKeyMode, 0), rdU8(RdmaNldevSysAttrMonitorMode, 1),
			rdU8(RdmaNldevSysAttrCopyOnFork, 1)),
			expected: RdmaSys{NetnsMode: 1, HasNetnsMode: true, HasPrivilegedQKeyMode: true,
				MonitorMode: 1, HasMonitorMode: true, CopyOnFork: 1, HasCopyOnFork: true}},
		{description: "positive: a netdev attach event", msgType: monitor, body: rdBody(handle,
			rdU32(RdmaNldevAttrPortIndex, 1), ndev, rdU8(RdmaNldevAttrEventType, RdmaNetdevAttachEvent)),
			expected: RdmaEvent{RdmaHandle: rxe0p1, RdmaNetdev: rx0, EventType: RdmaNetdevAttachEvent, HasEventType: true}},
		{description: "negative: a short u32", msgType: get, body: rtattr(RdmaNldevAttrDevIndex, []byte{1, 0}),
			expectedErr: ErrRdmaAttrLen},
		{description: "negative: a u64 sent as a u32", msgType: get, body: rdU32(RdmaNldevAttrNodeGUID, 1),
			expectedErr: ErrRdmaAttrLen},
		{description: "negative: a u8 sent as a u32", msgType: sysGet, body: rdU32(RdmaNldevSysAttrNetnsMode, 1),
			expectedErr: ErrRdmaAttrLen},
		{description: "negative: a name without its NUL", msgType: get, body: rtattr(RdmaNldevAttrDevName, []byte("rxe0")),
			expectedErr: ErrRdmaAttrString},
		{description: "negative: an empty name", msgType: get, body: rtattr(RdmaNldevAttrDevName, nil),
			expectedErr: ErrRdmaAttrString},
		{description: "negative: a bad width inside a nest fails the message", msgType: resGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrResSummary, rdNest(RdmaNldevAttrResSummaryEntry,
				rdU32(RdmaNldevAttrResSummaryEntryCurr, 1)))),
			expectedErr: ErrRdmaAttrLen},
		{description: "negative: a nest too short to hold an attribute", msgType: resGet,
			body: rtattr(RdmaNldevAttrResSummary, []byte{1, 2}), expectedErr: ErrRdmaAttrLen},
		{description: "negative: a truncated attribute", msgType: get,
			body: rdBody(handle, rtattrOverrunning(RdmaNldevAttrNodeGUID, 64)), expectedErr: ErrRTAttrSmall},
		{description: "negative: a client other than nldev", msgType: RdmaNlGetType(RdmaNlNldev+1, RdmaNldevCmdGet),
			body: handle, expectedErr: ErrRdmaMsgType},
		{description: "negative: an nldev command with no decoder", msgType: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResQPGetRaw),
			body: handle, expectedErr: ErrRdmaMsgType},
		{description: "negative: an rtnetlink type", msgType: unix.RTM_NEWLINK, body: handle, expectedErr: ErrRdmaMsgType},
		{description: "boundary: an empty body", msgType: get, expected: RdmaDev{}},
		{description: "boundary: an empty sys body has nothing Has", msgType: sysGet, expected: RdmaSys{}},
		{description: "boundary: an empty RES_SUMMARY", msgType: resGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrResSummary)), expected: RdmaRes{RdmaHandle: rxe0}},
		{description: "boundary: a one-byte name is just the NUL", msgType: get, body: rdStr(RdmaNldevAttrDevName, ""),
			expected: RdmaDev{HasDevName: true}},
		// rd_attr_cb stores tb[type] = attr (rdma/utils.c), so the last
		// one wins, below 64 and above it alike.
		{description: "boundary: a repeated attribute below 64 is last-wins", msgType: portGet,
			body: rdBody(roce, rdU32(RdmaNldevAttrNdevIndex, 9)),
			expected: RdmaPort{RdmaHandle: rxe0p1, RdmaNetdev: RdmaNetdev{NdevIndex: 9, HasNdevIndex: true, NdevName: "rx0", HasNdevName: true},
				State: IbPortActive, HasState: true, PhysState: IbPortPhysStateLinkUp, HasPhysState: true}},
		{description: "boundary: a repeated attribute above 64 is last-wins", msgType: monitor,
			body:     rdBody(handle, rdU8(RdmaNldevAttrEventType, RdmaRegisterEvent), rdU8(RdmaNldevAttrEventType, RdmaUnregisterEvent)),
			expected: RdmaEvent{RdmaHandle: rxe0, EventType: RdmaUnregisterEvent, HasEventType: true}},
		{description: "corner: an unknown attribute is recorded, the rest still parsed", msgType: get,
			body:     rdBody(handle, rdU64(119, 5), rdU8(RdmaNldevAttrDevNodeType, 1)),
			expected: RdmaDev{HasDevIndex: true, DevName: "rxe0", HasDevName: true, NodeType: 1, HasNodeType: true, Unknown: []uint16{119}}},
		{description: "corner: a known attribute this decoder does not read is Unknown", msgType: sysGet,
			body:     rdBody(rdU8(RdmaNldevSysAttrNetnsMode, 0), rdU32(RdmaNldevAttrDriverDetails, 0)),
			expected: RdmaSys{HasNetnsMode: true, Unknown: []uint16{RdmaNldevAttrDriverDetails}}},
		// RES_SUMMARY_ENTRY_MAX is 118 in 7.3's header; the 7.0 one the C
		// gate pins does not have it, so it is not declared and lands here.
		{description: "corner: an unknown attribute inside an entry is recorded too", msgType: resGet,
			body: rdBody(handle, rdNest(RdmaNldevAttrResSummary, rdNest(RdmaNldevAttrResSummaryEntry,
				rdStr(RdmaNldevAttrResSummaryEntryName, "qp"), rdU64(RdmaNldevAttrResSummaryEntryCurr, 1), rdU64(118, 64)))),
			expected: RdmaRes{RdmaHandle: rxe0, Summary: []RdmaResEntry{{Name: "qp", Curr: 1, HasCurr: true}}, Unknown: []uint16{118}}},
		{description: "corner: NLA_F_NESTED on the nests", msgType: statGet,
			body: rdBody(handle, rtattr(RdmaNldevAttrStatHwcounters|unix.NLA_F_NESTED,
				rtattr(RdmaNldevAttrStatHwcounterEntry|unix.NLA_F_NESTED, rdBody(rdStr(RdmaNldevAttrStatHwcounterEntryName, "a"),
					rdU64(RdmaNldevAttrStatHwcounterEntryValue, 3))))),
			expected: RdmaStat{RdmaHandle: rxe0, HwCounters: []RdmaHwCounter{{Name: "a", Value: 3, HasValue: true}}}},
		{description: "corner: a GET reply with PORT_STATE is a port (the doit quirk)", msgType: get, body: roce,
			expected: rocePort},
		{description: "corner: PARENT_NAME is MNL_TYPE_STRING in rdma, so no NUL is accepted", msgType: get,
			body:     rtattr(RdmaNldevAttrParentName, []byte("mlx5_0")),
			expected: RdmaDev{ParentName: "mlx5_0", HasParentName: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRdmaMsg(tc.msgType, tc.body)
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("err %v, want %v", err, tc.expectedErr)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
