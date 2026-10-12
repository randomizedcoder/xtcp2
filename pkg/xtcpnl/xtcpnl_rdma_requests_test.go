package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestBuildRdmaRequest(t *testing.T) {
	type result struct {
		len   int
		typ   uint16
		flags uint16
		seq   uint32
		attrs []byte
		err   error
	}
	u32 := func(atype uint16, v uint32) []byte { return rdmaIndexAttrs(uint32(atype), v) }
	cat := func(b ...[]byte) []byte { return bytes.Join(b, nil) }
	tests := []struct {
		description string
		cmd         uint16
		flags       uint16
		attrs       []byte
		expected    result
	}{
		{description: "positive: the dev dump", cmd: RdmaNldevCmdGet, flags: rdmaDumpFlagsCst,
			expected: result{len: 16, typ: 0x1401, flags: 0x305, seq: 7}},
		{description: "positive: the port doit", cmd: RdmaNldevCmdPortGet, flags: rdmaDoitFlagsCst,
			attrs:    cat(u32(RdmaNldevAttrDevIndex, 1), u32(RdmaNldevAttrPortIndex, 1)),
			expected: result{len: 32, typ: 0x1405, flags: 0x5, seq: 7, attrs: cat(u32(RdmaNldevAttrDevIndex, 1), u32(RdmaNldevAttrPortIndex, 1))}},
		{description: "positive: sys get", cmd: RdmaNldevCmdSysGet, flags: rdmaDoitFlagsCst,
			expected: result{len: 16, typ: 0x1406, flags: 0x5, seq: 7}},
		{description: "positive: stat get", cmd: RdmaNldevCmdStatGet, flags: rdmaDoitFlagsCst,
			attrs:    cat(u32(RdmaNldevAttrDevIndex, 1), u32(RdmaNldevAttrPortIndex, 1)),
			expected: result{len: 32, typ: 0x1411, flags: 0x5, seq: 7, attrs: cat(u32(RdmaNldevAttrDevIndex, 1), u32(RdmaNldevAttrPortIndex, 1))}},
		{description: "positive: a per-object resource dump is a read", cmd: RdmaNldevCmdResQPGet, flags: rdmaDumpFlagsCst,
			expected: result{len: 16, typ: 0x140a, flags: 0x305, seq: 7}},
		{description: "positive: NLM_F_REQUEST is added when the caller leaves it out", cmd: RdmaNldevCmdSysGet,
			expected: result{len: 16, typ: 0x1406, flags: 0x1, seq: 7}},
		{description: "negative: an admin cmd", cmd: RdmaNldevCmdNewlink, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "negative: SYS_SET is a write", cmd: RdmaNldevCmdSysSet, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "negative: a RAW dump", cmd: RdmaNldevCmdResQPGetRaw, flags: rdmaDumpFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "negative: GET_CHARDEV is not a read of state", cmd: RdmaNldevCmdGetChardev, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "negative: a flag beyond REQUEST|ACK|DUMP (NLM_F_CREATE)", cmd: RdmaNldevCmdGet, flags: rdmaDoitFlagsCst | 0x400,
			expected: result{err: ErrRdmaCmd}},
		{description: "boundary: out of range, NUM_OPS", cmd: RdmaNldevNumOps, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "boundary: op 0, UNSPEC", cmd: RdmaNldevCmdUnspec, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "boundary: the last command, MONITOR, is not a request", cmd: RdmaNldevCmdMonitor, flags: rdmaDoitFlagsCst,
			expected: result{err: ErrRdmaCmd}},
		{description: "boundary: dev index 0 is encoded as is; it is a real index (rxe0's)", cmd: RdmaNldevCmdGet, flags: rdmaDoitFlagsCst,
			attrs:    u32(RdmaNldevAttrDevIndex, 0),
			expected: result{len: 24, typ: 0x1401, flags: 0x5, seq: 7, attrs: u32(RdmaNldevAttrDevIndex, 0)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := BuildRdmaRequest(tc.cmd, tc.flags, 7, tc.attrs)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err %v, want %v", err, tc.expected.err)
			}
			if err != nil {
				if b != nil {
					t.Errorf("an error came with %d bytes", len(b))
				}
				return
			}
			got := result{
				len:   len(b),
				typ:   binary.LittleEndian.Uint16(b[4:6]),
				flags: binary.LittleEndian.Uint16(b[6:8]),
				seq:   binary.LittleEndian.Uint32(b[8:12]),
				attrs: b[NlMsgHdrSizeCst:],
			}
			if int(binary.LittleEndian.Uint32(b[0:4])) != len(b) {
				t.Errorf("nlmsg_len %d, %d bytes", binary.LittleEndian.Uint32(b[0:4]), len(b))
			}
			if got.len != tc.expected.len || got.typ != tc.expected.typ || got.flags != tc.expected.flags ||
				got.seq != tc.expected.seq || !bytes.Equal(got.attrs, tc.expected.attrs) {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// rdmaMust is a builder's request for a command it admits.
func rdmaMust(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// TestRdmaRequestsMatchRdma holds each wrapper to the request `rdma` sent in
// the captured corpus, byte for byte once seq is zeroed (rdma's seq is
// time(NULL), rd_prepare_msg, rdma/utils.c). rxe0 is device 0.
func TestRdmaRequestsMatchRdma(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		nth         int // which request of the row
		expected    []byte
	}{
		{description: "positive: every row opens with the dev dump", slug: "sys_show", nth: 0,
			expected: BuildRdmaDevDumpRequest(0)},
		{description: "positive: dev show's doit", slug: "dev_show", nth: 1,
			expected: BuildRdmaDevGetRequest(0, 0)},
		{description: "positive: link show's port doit", slug: "link_show_one", nth: 1,
			expected: BuildRdmaPortGetRequest(0, 1, 0)},
		{description: "positive: res show's RES_GET, DRIVER_DETAILS first", slug: "res_show", nth: 1,
			expected: BuildRdmaResGetRequest(0, 0, 0)},
		{description: "positive: statistic show's STAT_GET", slug: "stat_show", nth: 1,
			expected: BuildRdmaStatGetRequest(0, 1, 0)},
		{description: "positive: system show", slug: "sys_show", nth: 1,
			expected: BuildRdmaSysGetRequest(0)},
		{description: "positive: monitor checks the system modes first", slug: "monitor_add", nth: 1,
			expected: BuildRdmaSysGetRequest(0)},
		{description: "corner: -j leaves the wire alone", slug: "dev_show_json", nth: 1,
			expected: BuildRdmaDevGetRequest(0, 0)},
		{description: "corner: link show with no argument sends the same doit", slug: "link_show", nth: 1,
			expected: BuildRdmaPortGetRequest(0, 1, 0)},
		{description: "positive: res show qp dumps port 0 first", slug: "res_qp", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 0, 0))},
		{description: "positive: then port 1", slug: "res_qp", nth: 2,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 1, 0))},
		{description: "positive: the cm_id dumps, per port too", slug: "res_cm_id", nth: 2,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResCMIDGet, 0, 1, 0))},
		{description: "positive: the cq dump, once per device", slug: "res_cq", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResCQGet, 0, 0, 0))},
		{description: "positive: the mr dump", slug: "res_mr", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResMRGet, 0, 0, 0))},
		{description: "positive: the pd dump", slug: "res_pd", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResPDGet, 0, 0, 0))},
		{description: "positive: the ctx dump", slug: "res_ctx", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResCtxGet, 0, 0, 0))},
		{description: "positive: the srq dump", slug: "res_srq", nth: 1,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResSRQGet, 0, 0, 0))},
		{description: "positive: the cqn doit, no PORT_INDEX", slug: "res_cq_cqn", nth: 1,
			expected: rdmaMust(BuildRdmaResDoitRequest(RdmaNldevCmdResCQGet, 0, 0, RdmaNldevAttrResCQN, 1, 0))},
		{description: "corner: the lqpn doit carries the port", slug: "res_qp_lqpn", nth: 1,
			expected: rdmaMust(BuildRdmaResDoitRequest(RdmaNldevCmdResQPGet, 0, 1, RdmaNldevAttrResLQPN, 1, 0))},
		{description: "corner: the fallback dumps the named port only", slug: "res_qp_lqpn", nth: 2,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 1, 0))},
		{description: "corner: -j leaves the res dump alone", slug: "res_qp_json", nth: 2,
			expected: rdmaMust(BuildRdmaResDumpRequest(RdmaNldevCmdResQPGet, 0, 1, 0))},
		{description: "corner: a busy res show sends the same RES_GET", slug: "res_show_busy", nth: 1,
			expected: BuildRdmaResGetRequest(0, 0, 0)},
	}
	// The one rdma sent the same requests to every kernel.
	forRdmaKernels(t, func(t *testing.T, k rdmaKernel) {
		for _, tc := range tests {
			t.Run(tc.description, func(t *testing.T) {
				var reqs [][]byte
				for _, m := range rdmaCorpus(t, k, tc.slug) {
					if m.send {
						reqs = append(reqs, bytes.Clone(m.raw))
					}
				}
				if tc.nth >= len(reqs) {
					t.Fatalf("%d requests, want request %d", len(reqs), tc.nth)
				}
				got := reqs[tc.nth]
				binary.LittleEndian.PutUint32(got[8:12], 0)
				if !bytes.Equal(got, tc.expected) {
					t.Errorf("rdma sent\n%x\nthe builder makes\n%x", got, tc.expected)
				}
			})
		}
	})
}
