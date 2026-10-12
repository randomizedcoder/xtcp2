package xtcpnl

import (
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// zeroRdmaVolatile is v, a decoded object or reply, with the values that
// differ run to run zeroed: the owner's pid and the keys. Their Has bits
// stay. The object numbers do not differ, because the capture starts each
// holder only once the one before it has allocated everything.
func zeroRdmaVolatile(v any) any {
	rv := reflect.New(reflect.TypeOf(v)).Elem()
	rv.Set(reflect.ValueOf(v))
	for _, n := range []string{"PID", "RKey", "LKey", "LocalDMALKey"} {
		if f := rv.FieldByName(n); f.IsValid() {
			f.SetZero()
		}
	}
	return rv.Interface()
}

// rdmaResCorpusRow is what a RES row's messages come to: each request as
// type/flags/length, the entry count and Flat of each object reply, one
// entry, and the last reply.
type rdmaResCorpusRow struct {
	requests []string
	counts   []int
	flat     []bool
	entryAt  int
	entry    any // replies' entry entryAt of the last object reply, zeroRdmaVolatile'd; nil skips
	last     any // the last reply, zeroRdmaVolatile'd; nil skips
}

func rdmaResCorpusSummary(t *testing.T, slug string, entryAt int, wantEntry, wantLast bool) rdmaResCorpusRow {
	t.Helper()
	var r rdmaResCorpusRow
	for _, m := range rdmaCorpus(t, slug) {
		if m.send {
			r.requests = append(r.requests, fmt.Sprintf("%#x/%#x/%d", m.typ, m.flags, len(m.raw)))
		}
	}
	replies, _, _ := rdmaCorpusReplies(t, slug)
	var lastObjects reflect.Value
	for _, v := range replies {
		rv := reflect.ValueOf(v)
		if es := rv.FieldByName("Entries"); es.IsValid() {
			r.counts = append(r.counts, es.Len())
			r.flat = append(r.flat, rv.FieldByName("Flat").Bool())
			lastObjects = es
		}
	}
	r.entryAt = entryAt
	if wantEntry {
		if !lastObjects.IsValid() || entryAt >= lastObjects.Len() {
			t.Fatalf("no entry %d", entryAt)
		}
		r.entry = zeroRdmaVolatile(lastObjects.Index(entryAt).Interface())
	}
	if wantLast {
		r.last = zeroRdmaVolatile(replies[len(replies)-1])
	}
	return r
}

func TestParseRdmaResCorpus(t *testing.T) {
	const (
		devDump  = "0x1401/0x305/16"
		dump     = "/0x305/32" // DRIVER_DETAILS, DEV_INDEX
		dumpPort = "/0x305/40" // and PORT_INDEX 1
	)
	ibCore := RdmaResOwner{KernName: "ib_core", HasKernName: true}
	user := RdmaResOwner{HasPID: true}
	gsi := RdmaResQP{PortIndex: 1, HasPortIndex: true, LQPN: 1, HasLQPN: true, HasSQPSN: true,
		Type: 1, HasType: true, State: 3, HasState: true, RdmaResOwner: ibCore}
	rxe0 := RdmaHandle{HasDevIndex: true, DevName: "rxe0", HasDevName: true}
	tests := []struct {
		description string
		slug        string
		expected    rdmaResCorpusRow
	}{
		{description: "positive: kernel QPs, one dump per port, the GSI QP in each", slug: "res_qp",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140a" + dump, "0x140a" + dumpPort},
				counts: []int{1, 1}, flat: []bool{false, false}, entry: gsi}},
		// IB_POLL_WORKQUEUE is 2: rdma prints it as poll-ctx WORKQUEUE.
		{description: "positive: the kernel CQ, the MAD layer's", slug: "res_cq",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140c" + dump}, counts: []int{1}, flat: []bool{false},
				entry: RdmaResCQ{CQE: 1023, HasCQE: true, UseCnt: 2, HasUseCnt: true, PollCtx: 2, HasPollCtx: true,
					HasDIM: true, HasCQN: true, RdmaResOwner: ibCore}}},
		{description: "positive: the kernel PD, with its local DMA lkey", slug: "res_pd",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140e" + dump}, counts: []int{1}, flat: []bool{false},
				entry: RdmaResPD{HasLocalDMALKey: true, UseCnt: 1, HasUseCnt: true, HasPDN: true, RdmaResOwner: ibCore}}},
		// The dump is in restrack id order, and the GSI QP's id is
		// 1|port<<24, so it comes last, after the user QPs 17 to 33.
		{description: "positive: a user RC QP, with its remote fields", slug: "res_qp_user", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x140a" + dump, "0x140a" + dumpPort}, counts: []int{18, 18}, flat: []bool{false, false},
			entry: RdmaResQP{PortIndex: 1, HasPortIndex: true, LQPN: 17, HasLQPN: true, PDN: 1, HasPDN: true,
				HasRQPN: true, HasRQPSN: true, HasSQPSN: true, HasPathMigState: true, Type: 2, HasType: true,
				State: 1, HasState: true, RdmaResOwner: user}}},
		{description: "positive: a user CQ, with its context", slug: "res_cq_user", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x140c" + dump}, counts: []int{3}, flat: []bool{false}, entryAt: 2,
			entry: RdmaResCQ{CQE: 1023, HasCQE: true, UseCnt: 32, HasUseCnt: true, HasDIM: true, CQN: 2, HasCQN: true,
				CTXN: 1, HasCTXN: true, RdmaResOwner: user}}},
		{description: "positive: an MR, with both keys", slug: "res_mr", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x140d" + dump}, counts: []int{2}, flat: []bool{false}, entryAt: 1,
			entry: RdmaResMR{HasRKey: true, HasLKey: true, MRLen: 4096, HasMRLen: true, MRN: 1, HasMRN: true,
				PDN: 2, HasPDN: true, RdmaResOwner: user}}},
		{description: "positive: rping's PD, which librdmacm opens and nothing uses", slug: "res_pd_user",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140e" + dump}, counts: []int{4}, flat: []bool{false},
				entryAt: 3, entry: RdmaResPD{HasLocalDMALKey: true, HasUseCnt: true, PDN: 3, HasPDN: true,
					CTXN: 2, HasCTXN: true, RdmaResOwner: user}}},
		{description: "positive: the three verbs contexts", slug: "res_ctx", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x1416" + dump}, counts: []int{3}, flat: []bool{false}, entryAt: 2,
			entry: RdmaResCtx{CTXN: 2, HasCTXN: true, RdmaResOwner: user}}},
		{description: "positive: the SRQ, its 16 QPs one range", slug: "res_srq", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x1417" + dump}, counts: []int{1}, flat: []bool{false},
			entry: RdmaResSRQ{HasSRQN: true, HasType: true, PDN: 2, HasPDN: true, QPs: []RdmaQPRange{{18, 33}},
				RdmaResOwner: user}}},
		// RDMA_CM_LISTEN is 8 (enum rdma_cm_state, cma_priv.h); RDMA_PS_TCP
		// is 0x106. A listener's DST_ADDR is sent, AF_INET and all zero.
		{description: "positive: rping's listening CM ID", slug: "res_cm_id", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x140b" + dump, "0x140b" + dumpPort}, counts: []int{1, 1}, flat: []bool{false, false},
			entry: RdmaResCMID{PortIndex: 1, HasPortIndex: true, PS: 0x106, HasPS: true, State: 8, HasState: true,
				Src:    RdmaSockaddr{Family: rdmaAfInetCst, Port: 7174, Addr: netip.MustParseAddr("10.99.0.1")},
				HasSrc: true, Dst: RdmaSockaddr{Family: rdmaAfInetCst, Addr: netip.MustParseAddr("0.0.0.0")}, HasDst: true,
				HasCMIDN: true, RdmaResOwner: user}}},
		{description: "positive: the busy summary", slug: "res_show_busy", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x1409/0x5/32"},
			last: RdmaRes{RdmaHandle: rxe0, Summary: []RdmaResEntry{{"pd", 4, true}, {"cq", 3, true}, {"qp", 18, true},
				{"cm_id", 1, true}, {"mr", 2, true}, {"ctx", 3, true}, {"srq", 1, true}}}}},
		{description: "positive: the CQ doit answers flat, CQN as asked", slug: "res_cq_cqn", expected: rdmaResCorpusRow{
			requests: []string{devDump, "0x140c/0x5/40"}, counts: []int{1}, flat: []bool{true},
			entry: RdmaResCQ{CQE: 511, HasCQE: true, UseCnt: 2, HasUseCnt: true, HasDIM: true, CQN: 1, HasCQN: true,
				HasCTXN: true, RdmaResOwner: user}}},
		// rdmaCorpusErrnos pins the -ENOENT. With a port named, rdma falls
		// back to that port's dump alone.
		{description: "negative: the lqpn doit is refused, then the port's dump", slug: "res_qp_lqpn",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140a/0x5/48", "0x140a" + dumpPort},
				counts: []int{1}, flat: []bool{false}, entry: gsi}},
		{description: "negative: the RAW dump ends -EINVAL with no reply", slug: "res_qp_raw",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x1413" + dump}}},
		{description: "boundary: an MR dump with no MRs is DONE alone", slug: "res_mr_empty",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140d" + dump}}},
		{description: "corner: -j leaves the wire alone", slug: "res_qp_json",
			expected: rdmaResCorpusRow{requests: []string{devDump, "0x140a" + dump, "0x140a" + dumpPort},
				counts: []int{1, 1}, flat: []bool{false, false}, entry: gsi}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := rdmaResCorpusSummary(t, tc.slug, tc.expected.entryAt, tc.expected.entry != nil, tc.expected.last != nil)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRdmaResCorpusSidecars pins what `rdma` printed for each RES row, with
// pids written as N and keys as K, the two things that change run to run.
func TestRdmaResCorpusSidecars(t *testing.T) {
	pid, key := regexp.MustCompile(`pid [0-9]+ `), regexp.MustCompile(`key 0x[0-9a-f]+ `)
	gsiLine := "link rxe0/1 lqpn 1 type GSI state RTS sq-psn 0 comm [ib_core] \n"
	tests := []struct {
		description string
		slug        string
		expected    struct{ rc, out, err string }
	}{
		{description: "positive: the GSI QP", slug: "res_qp",
			expected: struct{ rc, out, err string }{rc: "0", out: gsiLine}},
		{description: "positive: rping's CM ID", slug: "res_cm_id", expected: struct{ rc, out, err string }{rc: "0",
			out: "link rxe0/1 cm-idn 0 state LISTEN ps TCP pid N comm rping src-addr 10.99.0.1:7174 dst-addr 0.0.0.0:0 \n"}},
		{description: "positive: the MRs", slug: "res_mr", expected: struct{ rc, out, err string }{rc: "0",
			out: "dev rxe0 mrn 0 rkey K lkey K mrlen 4096 pdn 1 pid N comm ibv_rc_pingpong \n" +
				"dev rxe0 mrn 1 rkey K lkey K mrlen 4096 pdn 2 pid N comm ibv_srq_pingpon \n"}},
		{description: "positive: the SRQ prints its QPs as a range", slug: "res_srq", expected: struct{ rc, out, err string }{
			rc: "0", out: "dev rxe0 srqn 0 type BASIC lqpn 18-33 pdn 2 pid N comm ibv_srq_pingpon \n"}},
		{description: "negative: the RAW dump's -EINVAL", slug: "res_qp_raw",
			expected: struct{ rc, out, err string }{rc: "1", err: "error: Invalid argument\n"}},
		{description: "boundary: no MRs prints nothing", slug: "res_mr_empty",
			expected: struct{ rc, out, err string }{rc: "0"}},
		{description: "boundary: the JSON array, newline before the bracket", slug: "res_qp_json",
			expected: struct{ rc, out, err string }{rc: "0", out: `[{"ifindex":0,"ifname":"rxe0","port":1,"lqpn":1,` +
				`"type":"GSI","state":"RTS","sq-psn":0,"comm":"ib_core"}` + "\n]\n"}},
		{description: "corner: the refused lqpn doit prints what the dump found", slug: "res_qp_lqpn",
			expected: struct{ rc, out, err string }{rc: "0", out: gsiLine}},
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
			out := key.ReplaceAllString(pid.ReplaceAllString(read(t, tc.slug+".out"), "pid N "), "key K ")
			errOut := read(t, tc.slug+".err")
			if rc != tc.expected.rc || out != tc.expected.out || errOut != tc.expected.err {
				t.Errorf("got rc %q out %q err %q\nwant %+v", rc, out, errOut, tc.expected)
			}
		})
	}
}

func TestRdmaCorpusSlugs(t *testing.T) {
	ents, err := os.ReadDir(tdRdma_7_1_4)
	if err != nil {
		t.Fatal(err)
	}
	inDir := map[string]bool{}
	for _, e := range ents {
		if s, ok := strings.CutSuffix(e.Name(), ".strace"); ok {
			inDir[s] = true
		}
	}
	listed := map[string]bool{}
	missing := 0
	for _, s := range rdmaCorpusSlugs {
		listed[s] = true
		for _, ext := range []string{".strace", ".out", ".err", ".rc"} {
			if _, err := os.Stat(tdRdma_7_1_4 + "/" + s + ext); err != nil {
				missing++
				t.Log(err)
			}
		}
	}
	src, err := os.ReadFile("xtcpnl_rdma_corpus_test.go")
	if err != nil {
		t.Fatal(err)
	}
	// A slug list of its own in a corpus-wide test starts with the first row.
	ownLists := strings.Count(string(src), `"dev_show", "`)
	tests := []struct {
		description string
		got         any
		expected    any
	}{
		{description: "positive: every .strace in the directory is listed", got: inDir, expected: listed},
		{description: "negative: no listed slug lacks a file", got: missing, expected: 0},
		{description: "boundary: 11 rows and 16 RES rows", got: len(rdmaCorpusSlugs), expected: 27},
		{description: "corner: the fuzz, Unknown and coverage tests keep no list of their own", got: ownLists, expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.expected) {
				t.Errorf("got %v, want %v", tc.got, tc.expected)
			}
		})
	}
}
