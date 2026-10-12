package xtcpnl

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The rxe0 the capture builds (scripts/capture-rdma.exp): soft-RoCE over
// veth rx0, whose MAC is pinned at 02:00:00:00:52:00, so the node GUID
// (addrconf_addr_eui48 of that MAC) is the same every run.
var corpusRxe0 = RdmaDev{
	HasDevIndex: true, DevName: "rxe0", HasDevName: true, NumPorts: 1, HasNumPorts: true,
	CapFlags: 0x1c001223c76, HasCapFlags: true,
	NodeGUID: 0xfffe005200, HasNodeGUID: true, SysImageGUID: 0xfffe005200, HasSysImageGUID: true,
	NodeType: 1, HasNodeType: true, DevProtocol: "roce", HasDevProtocol: true, HasDIM: true,
	NameAssignType: 1, HasNameAssignType: true,
}

var corpusSys = RdmaSys{NetnsMode: 1, HasNetnsMode: true, HasPrivilegedQKeyMode: true,
	MonitorMode: 1, HasMonitorMode: true, CopyOnFork: 1, HasCopyOnFork: true}

var corpusRxe0Port = RdmaPort{
	RdmaHandle: RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true, PortIndex: 1, HasPortIndex: true},
	RdmaNetdev: RdmaNetdev{NdevIndex: 4, HasNdevIndex: true, NdevName: "rx0", HasNdevName: true},
	State:      IbPortActive, HasState: true, PhysState: IbPortPhysStateLinkUp, HasPhysState: true,
}

// corpusRxeCounters is rxe's hardware counter table as 7.1.4 reports it, in
// its order (drivers/infiniband/sw/rxe/rxe_hw_counters.c), every one zero
// because nothing ever ran over the device.
func corpusRxeCounters() []RdmaHwCounter {
	var out []RdmaHwCounter
	for _, n := range []string{"sent_pkts", "rcvd_pkts", "duplicate_request", "out_of_seq_request",
		"rcvd_rnr_err", "send_rnr_err", "rcvd_seq_err", "ack_deferred", "retry_exceeded_err",
		"retry_rnr_exceeded_err", "completer_retry_err", "send_err", "link_downed", "rdma_sends",
		"rdma_recvs"} {
		out = append(out, RdmaHwCounter{Name: n, HasValue: true})
	}
	return out
}

// rdmaCorpusReplies decodes every nldev message the kernel sent in a row, in
// order, and checks every NLMSG_ERROR is an ACK of a request in the row.
func rdmaCorpusReplies(t *testing.T, slug string) (replies []any, types []uint16, requests int) {
	t.Helper()
	for _, m := range rdmaCorpus(t, slug) {
		switch {
		case m.send:
			requests++
		case m.typ == unix.NLMSG_DONE:
		case m.typ == unix.NLMSG_ERROR:
			e, err := ParseNlMsgErr(m.flags, m.body)
			if err != nil {
				t.Fatal(err)
			}
			if e.Errno != 0 {
				t.Errorf("%s: errno %d for request type %#x", slug, -e.Errno, e.RequestType)
			}
		default:
			v, err := ParseRdmaMsg(m.typ, m.body)
			if err != nil {
				t.Fatalf("%s: type %#x: %v", slug, m.typ, err)
			}
			replies = append(replies, v)
			types = append(types, m.typ)
		}
	}
	return replies, types, requests
}

func TestParseRdmaCorpus(t *testing.T) {
	get := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGet)
	sysGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdSysGet)
	mon := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdMonitor)
	rxe1 := RdmaHandle{DevIndex: 1, HasDevIndex: true, DevName: "rxe1", HasDevName: true}
	rxe1p1 := rxe1
	rxe1p1.PortIndex, rxe1p1.HasPortIndex = 1, true
	type result struct {
		replies  []any
		types    []uint16
		requests int
	}
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{description: "positive: dev show, the dump then the doit", slug: "dev_show",
			expected: result{replies: []any{corpusRxe0, corpusRxe0}, types: []uint16{get, get}, requests: 2}},
		{description: "positive: link show's port doit is a GET-typed reply", slug: "link_show",
			expected: result{replies: []any{corpusRxe0, corpusRxe0Port}, types: []uint16{get, get}, requests: 2}},
		{description: "positive: link show of one port", slug: "link_show_one",
			expected: result{replies: []any{corpusRxe0, corpusRxe0Port}, types: []uint16{get, get}, requests: 2}},
		{description: "positive: the resource summary in the kernel's order", slug: "res_show",
			expected: result{types: []uint16{get, RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResGet)}, requests: 2,
				replies: []any{corpusRxe0, RdmaRes{
					RdmaHandle: RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true},
					Summary: []RdmaResEntry{{"pd", 1, true}, {"cq", 1, true}, {"qp", 1, true},
						{"cm_id", 0, true}, {"mr", 0, true}, {"ctx", 0, true}, {"srq", 0, true}}}}}},
		{description: "positive: rxe's 15 hardware counters", slug: "stat_show",
			expected: result{types: []uint16{get, RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet)}, requests: 2,
				replies: []any{corpusRxe0, RdmaStat{RdmaHandle: corpusRxe0Port.RdmaHandle, HwCounters: corpusRxeCounters()}}}},
		{description: "positive: system show", slug: "sys_show",
			expected: result{replies: []any{corpusRxe0, corpusSys}, types: []uint16{get, sysGet}, requests: 2}},
		// rxe1 is added and deleted while the monitor listens. Deleting a
		// device sends UNREGISTER and no NETDEV_DETACH.
		{description: "positive: the monitor events, REGISTER, NETDEV_ATTACH, UNREGISTER", slug: "monitor_add",
			expected: result{types: []uint16{get, sysGet, mon, mon, mon}, requests: 2, replies: []any{corpusRxe0, corpusSys,
				RdmaEvent{RdmaHandle: rxe1, EventType: RdmaRegisterEvent, HasEventType: true},
				RdmaEvent{RdmaHandle: rxe1p1, RdmaNetdev: RdmaNetdev{NdevIndex: 3, HasNdevIndex: true, NdevName: "rx1", HasNdevName: true},
					EventType: RdmaNetdevAttachEvent, HasEventType: true},
				RdmaEvent{RdmaHandle: rxe1, EventType: RdmaUnregisterEvent, HasEventType: true}}}},
		{description: "corner: -j changes nothing on the wire", slug: "dev_show_json",
			expected: result{replies: []any{corpusRxe0, corpusRxe0}, types: []uint16{get, get}, requests: 2}},
		{description: "boundary: dev show of a missing device sends only the dump", slug: "dev_missing",
			expected: result{replies: []any{corpusRxe0}, types: []uint16{get}, requests: 1}},
		// rdma checks the port against the dump's PORT_INDEX itself, so
		// port 9 never reaches the kernel and no errno is recorded.
		{description: "negative: a port past the device's last is refused by rdma, not the kernel", slug: "link_missing",
			expected: result{replies: []any{corpusRxe0}, types: []uint16{get}, requests: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			got.replies, got.types, got.requests = rdmaCorpusReplies(t, tc.slug)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRdmaCorpusSidecars pins what `rdma` itself made of each row, so the
// decode rows above and the tool's own reading of the same bytes are on
// record together.
func TestRdmaCorpusSidecars(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		expected    struct{ rc, out, err string }
	}{
		{description: "positive: dev show", slug: "dev_show", expected: struct{ rc, out, err string }{rc: "0",
			out: "0: rxe0: node_type ca protocol roce node_guid 0000:00ff:fe00:5200 sys_image_guid 0000:00ff:fe00:5200 \n"}},
		{description: "positive: link show", slug: "link_show", expected: struct{ rc, out, err string }{rc: "0",
			out: "link rxe0/1 state ACTIVE physical_state LINK_UP netdev rx0 \n"}},
		{description: "positive: res show", slug: "res_show", expected: struct{ rc, out, err string }{rc: "0",
			out: "0: rxe0: pd 1 cq 1 qp 1 cm_id 0 mr 0 ctx 0 srq 0 \n"}},
		{description: "negative: a missing device", slug: "dev_missing", expected: struct{ rc, out, err string }{rc: "1",
			err: "Wrong device name - nosuch\n"}},
		{description: "negative: a missing port", slug: "link_missing", expected: struct{ rc, out, err string }{rc: "1",
			err: "Wrong device name\n"}},
		// The monitor runs until the driver's SIGTERM: 128 + 15.
		{description: "corner: the monitor ends on SIGTERM", slug: "monitor_add", expected: struct{ rc, out, err string }{rc: "143",
			out: "[REGISTER]\tdev 1 rxe1\n[NETDEV_ATTACH]\tdev 1 rxe1 port 1 netdev 3 rx1\n[UNREGISTER]\tdev 1 rxe1\n"}},
	}
	read := func(t *testing.T, name string) string {
		b, err := os.ReadFile(tdRdma_7_1_4 + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rc := strings.TrimSpace(read(t, tc.slug+".rc"))
			out, errOut := read(t, tc.slug+".out"), read(t, tc.slug+".err")
			if rc != tc.expected.rc || out != tc.expected.out || errOut != tc.expected.err {
				t.Errorf("got rc %q out %q err %q\nwant %+v", rc, out, errOut, tc.expected)
			}
		})
	}
}

// TestRdmaCorpusHasNoUnknown is the constant block's coverage claim: every
// attribute 7.1.4 sent in the corpus is one the decoders read.
func TestRdmaCorpusHasNoUnknown(t *testing.T) {
	for _, slug := range []string{"dev_show", "dev_show_one", "dev_show_json", "dev_missing", "link_show",
		"link_show_one", "link_missing", "res_show", "stat_show", "sys_show", "monitor_add"} {
		t.Run("corner: "+slug, func(t *testing.T) {
			replies, _, _ := rdmaCorpusReplies(t, slug)
			if len(replies) == 0 {
				t.Fatal("no replies")
			}
			for i, r := range replies {
				if u := reflect.ValueOf(r).FieldByName("Unknown"); u.Len() != 0 {
					t.Errorf("reply %d: Unknown %v", i, u.Interface())
				}
			}
		})
	}
}

// rdmaCaptureCoverageCst is the number of distinct nldev attribute types,
// nested ones included, the committed rdma corpus carries. Raise it when a
// richer capture lands; never lower it without recording why the corpus
// shrank. It is the rdma counterpart of TestAttrCoverageFloors, kept here
// because that test reads pcaps and this corpus is strace.
const rdmaCaptureCoverageCst = 27

func TestRdmaAttrCoverageFloor(t *testing.T) {
	seen := map[uint16]bool{}
	var walk func(b []byte)
	walk = func(b []byte) {
		if err := WalkRTAttrs(b, func(atype uint16, val []byte) {
			seen[atype] = true
			if rdmaKindOf(atype) == rdmaKindNested {
				walk(val)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, slug := range []string{"dev_show", "link_show", "res_show", "stat_show", "sys_show", "monitor_add"} {
		for _, m := range rdmaCorpus(t, slug) {
			if !m.send && RdmaNlGetClient(m.typ) == RdmaNlNldev {
				walk(m.body)
			}
		}
	}
	tests := []struct {
		description string
		got         int
		expected    int
	}{
		{description: "positive: the corpus carries at least the floor", got: min(len(seen), rdmaCaptureCoverageCst),
			expected: rdmaCaptureCoverageCst},
		{description: "boundary: the floor is not vacuous", got: min(rdmaCaptureCoverageCst, 1), expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %d, want %d (the corpus has %d distinct attributes)", tc.got, tc.expected, len(seen))
			}
		})
	}
	t.Logf("real kernel bytes carry %d distinct nldev attributes (floor %d)", len(seen), rdmaCaptureCoverageCst)
}

// FuzzParseRdmaMsg checks ParseRdmaMsg never panics, seeded with every
// nldev message in the corpus.
//
// go test ./pkg/xtcpnl/ -run FuzzParseRdmaMsg -fuzz FuzzParseRdmaMsg
func FuzzParseRdmaMsg(f *testing.F) {
	for _, slug := range []string{"dev_show", "link_show", "res_show", "stat_show", "sys_show", "monitor_add"} {
		for _, m := range rdmaCorpus(f, slug) {
			if RdmaNlGetClient(m.typ) == RdmaNlNldev {
				f.Add(m.typ, m.body)
			}
		}
	}
	f.Fuzz(func(_ *testing.T, msgType uint16, body []byte) {
		_, _ = ParseRdmaMsg(msgType, body)
	})
}
