package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// forRdmaKernels runs f as a subtest of t per corpus kernel.
func forRdmaKernels(t *testing.T, f func(t *testing.T, k rdmaKernel)) {
	t.Helper()
	for _, k := range rdmaKernels {
		t.Run(k.dir, func(t *testing.T) { f(t, k) })
	}
}

// corpusRxe0 is the rxe0 the capture builds (scripts/capture-rdma.exp), as
// k reports it: soft-RoCE over veth rx0, whose MAC is pinned at
// 02:00:00:00:52:00, so the node GUID (addrconf_addr_eui48 of that MAC) is
// the same every run and on every kernel.
func corpusRxe0(k rdmaKernel) RdmaDev {
	d := RdmaDev{
		HasDevIndex: true, DevName: "rxe0", HasDevName: true, NumPorts: 1, HasNumPorts: true,
		CapFlags: k.capFlags, HasCapFlags: true,
		NodeGUID: 0xfffe005200, HasNodeGUID: true, SysImageGUID: 0xfffe005200, HasSysImageGUID: true,
		NodeType: 1, HasNodeType: true, DevProtocol: "roce", HasDevProtocol: true, HasDIM: true,
	}
	if k.nameAssignType {
		d.NameAssignType, d.HasNameAssignType = 1, true
	}
	return d
}

// corpusSys is k's SYS_GET reply: shared netns and copy-on-fork on every
// kernel, and the qkey and monitor modes where k sends them.
func corpusSys(k rdmaKernel) RdmaSys {
	s := RdmaSys{NetnsMode: 1, HasNetnsMode: true, CopyOnFork: 1, HasCopyOnFork: true}
	if k.sysModes {
		s.HasPrivilegedQKeyMode, s.MonitorMode, s.HasMonitorMode = true, 1, true
	}
	return s
}

var corpusRxe0Port = RdmaPort{
	RdmaHandle: RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true, PortIndex: 1, HasPortIndex: true},
	RdmaNetdev: RdmaNetdev{NdevIndex: 4, HasNdevIndex: true, NdevName: "rx0", HasNdevName: true},
	State:      IbPortActive, HasState: true, PhysState: IbPortPhysStateLinkUp, HasPhysState: true,
}

// corpusRxeCounterNames is rxe's hardware counter table as k reports it, in
// its order (drivers/infiniband/sw/rxe/rxe_hw_counters.c).
func corpusRxeCounterNames(k rdmaKernel) []string {
	names := []string{"sent_pkts", "rcvd_pkts", "duplicate_request", "out_of_seq_request",
		"rcvd_rnr_err", "send_rnr_err", "rcvd_seq_err", "ack_deferred", "retry_exceeded_err",
		"retry_rnr_exceeded_err", "completer_retry_err", "send_err", "link_downed", "rdma_sends",
		"rdma_recvs"}
	if k.byteCounters {
		names = append(names, "sent_bytes", "rcvd_bytes")
	}
	return names
}

// corpusRxeCounters is that table decoded, every counter zero because
// nothing ever ran over the device.
func corpusRxeCounters(k rdmaKernel) []RdmaHwCounter {
	names := corpusRxeCounterNames(k)
	out := make([]RdmaHwCounter, 0, len(names))
	for _, n := range names {
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
func rdmaCorpusReplies(t *testing.T, k rdmaKernel, slug string) (replies []any, types []uint16, requests int) {
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
	for _, m := range rdmaCorpus(t, k, slug) {
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
	forRdmaKernels(t, func(t *testing.T, k rdmaKernel) {
		rxe0, sys := corpusRxe0(k), corpusSys(k)
		// rxe1 is added and deleted while the monitor listens. Deleting a
		// device sends UNREGISTER and no NETDEV_DETACH. A kernel without
		// MONITOR_MODE gets as far as the SYS_GET: rdma stops there.
		monitor := result{types: []uint16{get, sysGet}, requests: 2, replies: []any{rxe0, sys}}
		if k.sysModes {
			monitor.types = append(monitor.types, mon, mon, mon)
			monitor.replies = append(monitor.replies,
				RdmaEvent{RdmaHandle: rxe1, EventType: RdmaRegisterEvent, HasEventType: true},
				RdmaEvent{RdmaHandle: rxe1p1, RdmaNetdev: RdmaNetdev{NdevIndex: 3, HasNdevIndex: true, NdevName: "rx1", HasNdevName: true},
					EventType: RdmaNetdevAttachEvent, HasEventType: true},
				RdmaEvent{RdmaHandle: rxe1, EventType: RdmaUnregisterEvent, HasEventType: true})
		}
		tests := []struct {
			description string
			slug        string
			expected    result
		}{
			{description: "positive: dev show, the dump then the doit", slug: "dev_show",
				expected: result{replies: []any{rxe0, rxe0}, types: []uint16{get, get}, requests: 2}},
			{description: "positive: link show's port doit is a GET-typed reply", slug: "link_show",
				expected: result{replies: []any{rxe0, corpusRxe0Port}, types: []uint16{get, get}, requests: 2}},
			{description: "positive: link show of one port", slug: "link_show_one",
				expected: result{replies: []any{rxe0, corpusRxe0Port}, types: []uint16{get, get}, requests: 2}},
			{description: "positive: the resource summary in the kernel's order", slug: "res_show",
				expected: result{types: []uint16{get, RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdResGet)}, requests: 2,
					replies: []any{rxe0, RdmaRes{
						RdmaHandle: RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true},
						Summary: []RdmaResEntry{{"pd", 1, true}, {"cq", 1, true}, {"qp", 1, true},
							{"cm_id", 0, true}, {"mr", 0, true}, {"ctx", 0, true}, {"srq", 0, true}}}}}},
			{description: "positive: rxe's hardware counters", slug: "stat_show",
				expected: result{types: []uint16{get, RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet)}, requests: 2,
					replies: []any{rxe0, RdmaStat{RdmaHandle: corpusRxe0Port.RdmaHandle, HwCounters: corpusRxeCounters(k)}}}},
			{description: "positive: system show", slug: "sys_show",
				expected: result{replies: []any{rxe0, sys}, types: []uint16{get, sysGet}, requests: 2}},
			{description: "positive: the monitor events, REGISTER, NETDEV_ATTACH, UNREGISTER, where the kernel has them",
				slug: "monitor_add", expected: monitor},
			{description: "corner: -j changes nothing on the wire", slug: "dev_show_json",
				expected: result{replies: []any{rxe0, rxe0}, types: []uint16{get, get}, requests: 2}},
			{description: "boundary: dev show of a missing device sends only the dump", slug: "dev_missing",
				expected: result{replies: []any{rxe0}, types: []uint16{get}, requests: 1}},
			// rdma checks the port against the dump's PORT_INDEX itself, so
			// port 9 never reaches the kernel and no errno is recorded.
			{description: "negative: a port past the device's last is refused by rdma, not the kernel", slug: "link_missing",
				expected: result{replies: []any{rxe0}, types: []uint16{get}, requests: 1}},
		}
		for _, tc := range tests {
			t.Run(tc.description, func(t *testing.T) {
				var got result
				got.replies, got.types, got.requests = rdmaCorpusReplies(t, k, tc.slug)
				if !reflect.DeepEqual(got, tc.expected) {
					t.Errorf("got  %+v\nwant %+v", got, tc.expected)
				}
			})
		}
	})
}

// TestRdmaCorpusSidecars pins what `rdma` itself made of each row, so the
// decode rows above and the tool's own reading of the same bytes are on
// record together.
func TestRdmaCorpusSidecars(t *testing.T) {
	type sidecars struct{ rc, out, err string }
	forRdmaKernels(t, func(t *testing.T, k rdmaKernel) {
		// The monitor runs until the driver's SIGTERM: 128 + 15. Without
		// MONITOR_MODE rdma refuses it at once (rdma/monitor.c), and with
		// no PRIVILEGED_QKEY_MODE system show leaves the field out.
		monitor := sidecars{rc: "1", err: "RDMA monitoring is not supported by the kernel\n"}
		sys := "netns shared monitor off copy-on-fork on\n"
		if k.sysModes {
			monitor = sidecars{rc: "143",
				out: "[REGISTER]\tdev 1 rxe1\n[NETDEV_ATTACH]\tdev 1 rxe1 port 1 netdev 3 rx1\n[UNREGISTER]\tdev 1 rxe1\n"}
			sys = "netns shared privileged-qkey off monitor on copy-on-fork on\n"
		}
		stat := "link rxe0/1 \n"
		for _, n := range corpusRxeCounterNames(k) {
			stat += "    " + n + " 0 \n"
		}
		tests := []struct {
			description string
			slug        string
			expected    sidecars
		}{
			{description: "positive: dev show", slug: "dev_show", expected: sidecars{rc: "0",
				out: "0: rxe0: node_type ca protocol roce node_guid 0000:00ff:fe00:5200 sys_image_guid 0000:00ff:fe00:5200 \n"}},
			{description: "positive: link show", slug: "link_show", expected: sidecars{rc: "0",
				out: "link rxe0/1 state ACTIVE physical_state LINK_UP netdev rx0 \n"}},
			{description: "positive: res show", slug: "res_show", expected: sidecars{rc: "0",
				out: "0: rxe0: pd 1 cq 1 qp 1 cm_id 0 mr 0 ctx 0 srq 0 \n"}},
			{description: "positive: statistic show, one counter a line", slug: "stat_show", expected: sidecars{rc: "0", out: stat}},
			{description: "positive: system show, with the modes the kernel sends", slug: "sys_show", expected: sidecars{rc: "0", out: sys}},
			{description: "negative: a missing device", slug: "dev_missing", expected: sidecars{rc: "1",
				err: "Wrong device name - nosuch\n"}},
			{description: "negative: a missing port", slug: "link_missing", expected: sidecars{rc: "1",
				err: "Wrong device name\n"}},
			{description: "corner: the monitor ends on SIGTERM, or is refused", slug: "monitor_add", expected: monitor},
		}
		read := func(t *testing.T, name string) string {
			b, err := os.ReadFile(k.rdma() + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		for _, tc := range tests {
			t.Run(tc.description, func(t *testing.T) {
				got := sidecars{rc: strings.TrimSpace(read(t, tc.slug+".rc")),
					out: read(t, tc.slug+".out"), err: read(t, tc.slug+".err")}
				if got != tc.expected {
					t.Errorf("got  %+v\nwant %+v", got, tc.expected)
				}
			})
		}
	})
}

// TestRdmaCorpusHasNoUnknown is the constant block's coverage claim: every
// attribute any corpus kernel sent is one the decoders read.
func TestRdmaCorpusHasNoUnknown(t *testing.T) {
	forRdmaKernels(t, func(t *testing.T, k rdmaKernel) { rdmaCorpusHasNoUnknown(t, k) })
}

func rdmaCorpusHasNoUnknown(t *testing.T, k rdmaKernel) {
	for _, slug := range rdmaCorpusSlugs {
		t.Run("corner: "+slug, func(t *testing.T) {
			replies, _, _ := rdmaCorpusReplies(t, k, slug)
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
// nested ones included, the committed rdma corpora carry between them. Raise it when a
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
	for _, k := range rdmaKernels {
		for _, slug := range rdmaCorpusSlugs {
			for _, m := range rdmaCorpus(t, k, slug) {
				if !m.send && RdmaNlGetClient(m.typ) == RdmaNlNldev {
					walk(m.body)
				}
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
// seeded with every nldev message in every kernel's corpus, each of which
// decodes.
//
// go test ./pkg/xtcpnl/ -run FuzzParseRdmaMsg -fuzz FuzzParseRdmaMsg
func FuzzParseRdmaMsg(f *testing.F) {
	for _, k := range rdmaKernels {
		for _, slug := range rdmaCorpusSlugs {
			for _, m := range rdmaCorpus(f, k, slug) {
				if RdmaNlGetClient(m.typ) == RdmaNlNldev {
					if !m.send {
						if _, err := ParseRdmaMsg(m.typ, m.body); err != nil {
							f.Fatalf("%s/%s: seed type %#x: %v", k.dir, slug, m.typ, err)
						}
					}
					f.Add(m.typ, m.body)
				}
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
// `nix run .#microvm-x86_64-rdma-capture[-<kernel>] -- --out <dir>`, against
// the committed corpus of the kernel its uname names: every row's messages
// equal once masked. It runs only with XTCPNL_RDMA_RECAPTURE=<dir>.
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
	recapture := open(dir)
	k, err := rdmaKernelOfUname(recapture)
	if err != nil {
		t.Fatal(err)
	}
	corpus := open(k.rdma())
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
		msgs := straceNlMsgs(t, dgs)
		out := make([][]byte, 0, len(msgs))
		for _, m := range msgs {
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

// rdmaKernelOfUname is the rdmaKernels entry whose directory names the
// kernel of root's uname sidecar, the way the capture runner names it:
// 7.2.0-rc3 is 7_2_0_rc3.
func rdmaKernelOfUname(root *os.Root) (rdmaKernel, error) {
	b, err := root.ReadFile("uname")
	if err != nil {
		return rdmaKernel{}, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return rdmaKernel{}, fmt.Errorf("not a uname line: %q", b)
	}
	dir := strings.NewReplacer(".", "_", "-", "_").Replace(fields[2])
	for _, k := range rdmaKernels {
		if k.dir == dir {
			return k, nil
		}
	}
	return rdmaKernel{}, fmt.Errorf("kernel %s has no committed corpus; add it to rdmaKernels", fields[2])
}

// rdmaMaskedCorpus is k's row slug as maskRdmaMsg leaves each message.
func rdmaMaskedCorpus(t *testing.T, k rdmaKernel, slug string) [][]byte {
	t.Helper()
	msgs := rdmaCorpus(t, k, slug)
	out := make([][]byte, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, maskRdmaMsg(t, m.raw))
	}
	return out
}

// TestRdmaCorpusAcrossKernels pins where each kernel's corpus differs from
// 7.1.4's, once masked: the message types whose bytes differ at the same
// position, and the rows whose message count differs. A kernel that starts
// sending something new shows up here before anywhere else.
func TestRdmaCorpusAcrossKernels(t *testing.T) {
	base := rdmaKernels[slices.IndexFunc(rdmaKernels, func(k rdmaKernel) bool { return k.dir == "7_1_4" })]
	get := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGet)
	sysGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdSysGet)
	statGet := RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet)
	type delta struct {
		types []uint16 // sorted
		rows  []string // in rdmaCorpusSlugs order
	}
	tests := []struct {
		description string
		dir         string
		expected    delta
	}{
		{description: "positive: 6.12 sends what 7.1.4 does", dir: "6_12_96"},
		{description: "positive: 6.18 sends what 7.1.4 does", dir: "6_18_39"},
		{description: "negative: 6.6's device replies lack NAME_ASSIGN_TYPE and its SYS_GET the two modes, so no monitor",
			dir: "6_6_144", expected: delta{types: []uint16{get, sysGet}, rows: []string{"monitor_add"}}},
		{description: "boundary: 6.1 differs where 6.6 does, its CAP_FLAGS too",
			dir: "6_1_177", expected: delta{types: []uint16{get, sysGet}, rows: []string{"monitor_add"}}},
		{description: "corner: 7.2 adds rxe's two byte counters to STAT_GET and nothing else",
			dir: "7_2_0_rc3", expected: delta{types: []uint16{statGet}}},
		{description: "corner: 7.1.4 against itself", dir: "7_1_4"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			i := slices.IndexFunc(rdmaKernels, func(k rdmaKernel) bool { return k.dir == tc.dir })
			if i < 0 {
				t.Fatalf("%s is not in rdmaKernels", tc.dir)
			}
			var got delta
			for _, slug := range rdmaCorpusSlugs {
				a, b := rdmaMaskedCorpus(t, base, slug), rdmaMaskedCorpus(t, rdmaKernels[i], slug)
				if len(a) != len(b) {
					got.rows = append(got.rows, slug)
				}
				for j := range min(len(a), len(b)) {
					if typ := binary.LittleEndian.Uint16(a[j][4:6]); !bytes.Equal(a[j], b[j]) && !slices.Contains(got.types, typ) {
						got.types = append(got.types, typ)
					}
				}
			}
			slices.Sort(got.types)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
