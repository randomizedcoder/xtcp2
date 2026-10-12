package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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

// rdmaCorpusSlugs is every row of the committed corpus, in capture order
// (scripts/capture-rdma.exp). Every corpus-wide test reads this one list,
// and TestRdmaCorpusSlugs holds it to the directory.
var rdmaCorpusSlugs = []string{
	"dev_show", "dev_show_one", "dev_show_json", "dev_missing", "link_show", "link_show_one",
	"link_missing", "res_show", "stat_show", "sys_show", "monitor_add",
	// Kernel-owned objects only, before any holder runs.
	"res_qp", "res_cq", "res_pd", "res_mr_empty", "res_qp_json", "res_qp_lqpn", "res_qp_raw",
	// With the RC, SRQ and rping holders running.
	"res_show_busy", "res_qp_user", "res_cq_user", "res_mr", "res_pd_user", "res_ctx", "res_srq",
	"res_cm_id", "res_cq_cqn",
}

// rdmaCorpusErrnos is the one errno each row that has one recorded, in a
// NLMSG_ERROR or NLMSG_DONE. Every other row's errnos are all 0.
//
//   - res_qp_lqpn: the doit for `lqpn 1` is refused, because the GSI QP's
//     restrack id is not its QP number; rdma falls back to the dump.
//   - res_qp_raw: rxe has no raw QP fill, so the RAW dump ends -EINVAL.
var rdmaCorpusErrnos = map[string]int32{
	"res_qp_lqpn": -int32(unix.ENOENT),
	"res_qp_raw":  -int32(unix.EINVAL),
}

// rdmaCorpusReplies decodes every nldev message the kernel sent in a row, in
// order, and checks every non-zero errno, in an NLMSG_ERROR or an
// NLMSG_DONE, is the row's rdmaCorpusErrnos entry and that a row with one
// recorded it.
func rdmaCorpusReplies(t *testing.T, slug string) (replies []any, types []uint16, requests int) {
	t.Helper()
	want, seen := rdmaCorpusErrnos[slug], false
	check := func(errno int32, what string) {
		switch errno {
		case 0:
		case want:
			seen = true
		default:
			t.Errorf("%s: errno %d in %s, want %d", slug, errno, what, want)
		}
	}
	for _, m := range rdmaCorpus(t, slug) {
		switch {
		case m.send:
			requests++
		case m.typ == unix.NLMSG_DONE:
			if len(m.body) >= 4 {
				check(int32(binary.LittleEndian.Uint32(m.body)), "NLMSG_DONE") //nolint:gosec // the wire int
			}
		case m.typ == unix.NLMSG_ERROR:
			e, err := ParseNlMsgErr(m.flags, m.body)
			if err != nil {
				t.Fatal(err)
			}
			check(e.Errno, fmt.Sprintf("the NLMSG_ERROR for request type %#x", e.RequestType))
		default:
			v, err := ParseRdmaMsg(m.typ, m.body)
			if err != nil {
				t.Fatalf("%s: type %#x: %v", slug, m.typ, err)
			}
			replies = append(replies, v)
			types = append(types, m.typ)
		}
	}
	if want != 0 && !seen {
		t.Errorf("%s: errno %d was not recorded", slug, want)
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
	for _, slug := range rdmaCorpusSlugs {
		t.Run("corner: "+slug, func(t *testing.T) {
			replies, _, _ := rdmaCorpusReplies(t, slug)
			if len(replies) == 0 {
				t.Fatal("no replies")
			}
			for i, r := range replies {
				v := reflect.ValueOf(r)
				if u := v.FieldByName("Unknown"); u.Len() != 0 {
					t.Errorf("reply %d: Unknown %v", i, u.Interface())
				}
				if es := v.FieldByName("Entries"); es.IsValid() {
					for j := range es.Len() {
						if u := es.Index(j).FieldByName("Unknown"); u.Len() != 0 {
							t.Errorf("reply %d entry %d: Unknown %v", i, j, u.Interface())
						}
					}
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
const rdmaCaptureCoverageCst = 68

func TestRdmaAttrCoverageFloor(t *testing.T) {
	btoi := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
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
	for _, slug := range rdmaCorpusSlugs {
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
		{description: "negative: the floor is not stale; raise it when the corpus grows", got: len(seen),
			expected: rdmaCaptureCoverageCst},
		{description: "boundary: the floor is not vacuous", got: min(rdmaCaptureCoverageCst, 1), expected: 1},
		{description: "boundary: attributes inside RES_*_ENTRY nests count", got: btoi(seen[RdmaNldevAttrResLQPN]), expected: 1},
		{description: "corner: the sockaddr attributes count", got: btoi(seen[RdmaNldevAttrResSrcAddr]), expected: 1},
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

// rdmaDecodeErrs are the errors ParseRdmaMsg may return; any other is a
// bug.
var rdmaDecodeErrs = []error{ErrRdmaAttrLen, ErrRdmaAttrString, ErrRdmaMsgType, ErrRTAttrSmall}

func isRdmaDecodeErr(err error) bool {
	for _, e := range rdmaDecodeErrs {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// FuzzParseRdmaMsg checks ParseRdmaMsg never panics, returns no error it
// does not declare, and decodes the same bytes the same way twice. It is
// seeded with every nldev message in the corpus, each of which decodes.
//
// go test ./pkg/xtcpnl/ -run FuzzParseRdmaMsg -fuzz FuzzParseRdmaMsg
func FuzzParseRdmaMsg(f *testing.F) {
	for _, slug := range rdmaCorpusSlugs {
		for _, m := range rdmaCorpus(f, slug) {
			if RdmaNlGetClient(m.typ) == RdmaNlNldev {
				if !m.send {
					if _, err := ParseRdmaMsg(m.typ, m.body); err != nil {
						f.Fatalf("%s: seed type %#x: %v", slug, m.typ, err)
					}
				}
				f.Add(m.typ, m.body)
			}
		}
	}
	f.Fuzz(func(t *testing.T, msgType uint16, body []byte) {
		a, err := ParseRdmaMsg(msgType, body)
		if err != nil {
			if !isRdmaDecodeErr(err) {
				t.Fatalf("undeclared error %v", err)
			}
			return
		}
		if b, _ := ParseRdmaMsg(msgType, body); !reflect.DeepEqual(a, b) {
			t.Fatalf("two decodes differ\n%+v\n%+v", a, b)
		}
	})
}

// TestParseRdmaMsgEmptyBodies is the fuzz target's boundary: every decoded
// type takes an empty body, and so does nothing else.
func TestParseRdmaMsgEmptyBodies(t *testing.T) {
	tests := []struct {
		description string
		cmd         uint16
		expected    error
	}{
		{description: "positive: GET", cmd: RdmaNldevCmdGet},
		{description: "positive: PORT_GET", cmd: RdmaNldevCmdPortGet},
		{description: "positive: SYS_GET", cmd: RdmaNldevCmdSysGet},
		{description: "positive: RES_GET", cmd: RdmaNldevCmdResGet},
		{description: "positive: STAT_GET", cmd: RdmaNldevCmdStatGet},
		{description: "positive: MONITOR", cmd: RdmaNldevCmdMonitor},
		{description: "boundary: RES_QP_GET, the first object GET", cmd: RdmaNldevCmdResQPGet},
		{description: "boundary: RES_SRQ_GET, the last", cmd: RdmaNldevCmdResSRQGet},
		{description: "negative: RES_QP_GET_RAW", cmd: RdmaNldevCmdResQPGetRaw, expected: ErrRdmaMsgType},
		{description: "corner: UNSPEC", cmd: RdmaNldevCmdUnspec, expected: ErrRdmaMsgType},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, err := ParseRdmaMsg(RdmaNlGetType(RdmaNlNldev, tc.cmd), nil)
			if !errors.Is(err, tc.expected) {
				t.Fatalf("err %v, want %v", err, tc.expected)
			}
			if err == nil && !reflect.ValueOf(v).IsValid() {
				t.Error("nil value with a nil error")
			}
		})
	}
}

// maskRdmaVolatile returns value, or for an attribute whose value differs
// run to run a zeroed copy of it: the owner's pid, the keys and PSNs the
// kernel and rdma-core pick, and the object numbers. A sockaddr keeps its
// family and address and loses its port. Masking keys on the type alone,
// never on the value.
func maskRdmaVolatile(attrType uint16, value []byte) []byte {
	switch attrType {
	case RdmaNldevAttrResPID, RdmaNldevAttrResLQPN, RdmaNldevAttrResRQPN, RdmaNldevAttrResRqPSN,
		RdmaNldevAttrResSqPSN, RdmaNldevAttrResRKey, RdmaNldevAttrResLKey, RdmaNldevAttrResLocalDMALKey,
		RdmaNldevAttrResUnsafeGlobalRKey, RdmaNldevAttrResPDN, RdmaNldevAttrResCQN, RdmaNldevAttrResMRN,
		RdmaNldevAttrResCtxn, RdmaNldevAttrResSRQN, RdmaNldevAttrResCMIDN, RdmaNldevAttrMinRange,
		RdmaNldevAttrMaxRange:
		return make([]byte, len(value))
	case RdmaNldevAttrResSrcAddr, RdmaNldevAttrResDstAddr:
		v := bytes.Clone(value)
		if len(v) >= rdmaSinAddrOffCst {
			clear(v[rdmaSinPortOffCst:rdmaSinAddrOffCst])
		}
		return v
	}
	return value
}

// maskRdmaMsg is a netlink message with its seq and pid zeroed and every
// attribute, nested ones included, passed through maskRdmaVolatile.
func maskRdmaMsg(t testing.TB, raw []byte) []byte {
	t.Helper()
	m := bytes.Clone(raw)
	clear(m[8:NlMsgHdrSizeCst])
	// An NLMSG_ERROR echoes the request's header after its errno.
	if binary.LittleEndian.Uint16(m[4:6]) == unix.NLMSG_ERROR && len(m) >= NlMsgHdrSizeCst+4+NlMsgHdrSizeCst {
		clear(m[NlMsgHdrSizeCst+4+8 : NlMsgHdrSizeCst+4+NlMsgHdrSizeCst])
		return m
	}
	if RdmaNlGetClient(binary.LittleEndian.Uint16(m[4:6])) != RdmaNlNldev {
		return m
	}
	var walk func(b []byte)
	walk = func(b []byte) {
		if err := WalkRTAttrs(b, func(atype uint16, val []byte) {
			if rdmaKindOf(atype) == rdmaKindNested {
				walk(val)
				return
			}
			copy(val, maskRdmaVolatile(atype, val))
		}); err != nil {
			t.Fatal(err)
		}
	}
	walk(m[NlMsgHdrSizeCst:])
	return m
}

func TestMaskRdmaVolatile(t *testing.T) {
	listen := make([]byte, rdmaSockaddrLenCst)
	binary.LittleEndian.PutUint16(listen[0:2], rdmaAfInetCst)
	binary.BigEndian.PutUint16(listen[2:4], 7174)
	copy(listen[rdmaSinAddrOffCst:], []byte{10, 99, 0, 1})
	masked := bytes.Clone(listen)
	clear(masked[2:4])
	tests := []struct {
		description string
		attrType    uint16
		value       []byte
		expected    []byte
	}{
		{description: "positive: PID is masked", attrType: RdmaNldevAttrResPID, value: le32(1234), expected: le32(0)},
		{description: "positive: LQPN is masked", attrType: RdmaNldevAttrResLQPN, value: le32(17), expected: le32(0)},
		{description: "negative: RES_TYPE is kept", attrType: RdmaNldevAttrResType, value: []byte{2}, expected: []byte{2}},
		{description: "negative: KERN_NAME is kept", attrType: RdmaNldevAttrResKernName,
			value: []byte("ib_core\x00"), expected: []byte("ib_core\x00")},
		{description: "boundary: a sockaddr loses its port and keeps its address", attrType: RdmaNldevAttrResSrcAddr,
			value: listen, expected: masked},
		{description: "corner: the GSI QP's LQPN 1 is masked too", attrType: RdmaNldevAttrResLQPN,
			value: le32(1), expected: le32(0)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			in := bytes.Clone(tc.value)
			if got := maskRdmaVolatile(tc.attrType, in); !bytes.Equal(got, tc.expected) {
				t.Errorf("got %x, want %x", got, tc.expected)
			}
			if !bytes.Equal(in, tc.value) {
				t.Errorf("the input was modified: %x", in)
			}
		})
	}
}

// TestRdmaRecaptureMatches compares a fresh capture, unpacked with
// `nix run .#microvm-x86_64-rdma-capture -- --out <dir>`, against the
// committed corpus: every row's messages equal once masked. It runs only
// with XTCPNL_RDMA_RECAPTURE=<dir>.
func TestRdmaRecaptureMatches(t *testing.T) {
	dir := os.Getenv("XTCPNL_RDMA_RECAPTURE")
	if dir == "" {
		t.Skip("XTCPNL_RDMA_RECAPTURE is not set")
	}
	// Both trees are opened as roots, so a slug cannot reach outside them.
	open := func(path string) *os.Root {
		r, err := os.OpenRoot(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		})
		return r
	}
	corpus, recapture := open(tdRdma_7_1_4), open(dir)
	read := func(t *testing.T, root *os.Root, name string) [][]byte {
		f, err := root.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}()
		dgs, err := parseStrace(f)
		if err != nil {
			t.Fatal(err)
		}
		var out [][]byte
		for _, m := range straceNlMsgs(t, dgs) {
			out = append(out, maskRdmaMsg(t, m.raw))
		}
		return out
	}
	for _, slug := range rdmaCorpusSlugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			want, got := read(t, corpus, slug+".strace"), read(t, recapture, slug+".strace")
			if len(got) != len(want) {
				t.Fatalf("%d messages, the corpus has %d", len(got), len(want))
			}
			for i := range want {
				if !bytes.Equal(got[i], want[i]) {
					t.Errorf("message %d differs\nrecapture %x\ncorpus    %x", i, got[i], want[i])
				}
			}
		})
	}
}
