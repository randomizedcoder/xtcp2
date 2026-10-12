package gordma

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// cmdRes is cmd_res (rdma/res.c).
func cmdRes(r *rd) error {
	return r.execCmd(resShow, []command{
		{"show", resShow},
		{"set", notImplemented("resource set")},
		{"list", resShow},
		{"help", resHelp},
	}, "resource command")
}

var resHelp = usage(
	"Usage: %s resource",
	"          resource show [DEV]",
	"          resource show [qp|cm_id|pd|mr|cq|ctx|srq|frmr_pools]",
	"          resource show qp link [DEV/PORT]",
	"          resource show qp link [DEV/PORT] [FILTER-NAME FILTER-VALUE]",
	"          resource show cm_id link [DEV/PORT]",
	"          resource show cm_id link [DEV/PORT] [FILTER-NAME FILTER-VALUE]",
	"          resource show cq dev [DEV]",
	"          resource show cq dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource show pd dev [DEV]",
	"          resource show pd dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource show mr dev [DEV]",
	"          resource show mr dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource show ctx dev [DEV]",
	"          resource show ctx dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource show srq dev [DEV]",
	"          resource show srq dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource show frmr_pools dev [DEV]",
	"          resource show frmr_pools dev [DEV] [FILTER-NAME FILTER-VALUE]",
	"          resource set frmr_pools dev DEV aging AGING_PERIOD",
	"          resource set frmr_pools dev DEV pinned POOL_KEY PINNED_VALUE",
)

// resShow is res_show. `res show DEV` is the summary of one device.
func resShow(r *rd) error {
	if len(r.args) == 1 && r.lookup(false) != nil {
		return r.execDev(resSummary)
	}
	return r.execCmd(func(r *rd) error { return r.execDev(resSummary) }, []command{
		{"qp", resQP.run},
		{"cm_id", resCMID.run},
		{"cq", resCQ.run},
		{"mr", resMR.run},
		{"pd", resPD.run},
		{"ctx", resCtx.run},
		{"srq", resSRQ.run},
		{"frmr_pools", notImplemented("resource show frmr_pools")},
	}, "parameter")
}

// resSummary is _res_no_args: RES_GET of one device.
func resSummary(r *rd) error {
	return r.talk(xtcpnl.BuildRdmaResGetRequest(r.devIdx, r.portIdx, seq()), func(m Msg) error {
		s, err := xtcpnl.ParseRdmaRes(m.Body)
		if err != nil || !s.HasDevIndex || !s.HasDevName {
			return errCB
		}
		p := r.p
		p.openObject()
		p.printUint(printAny, "ifindex", "%d: ", s.DevIndex)
		p.printString(printAny, "ifname", "%s: ", s.DevName)
		// res_print_summary stops at an incomplete entry; its error is
		// not checked.
		for _, e := range s.Summary {
			if e.Name == "" || !e.HasCurr {
				break
			}
			r.resU64(e.Name, e.Curr, true)
		}
		p.closeObject()
		p.newline()
		return nil
	})
}

// resObject is one RES_FUNC: its command, filters, how it iterates (per
// device, or per port from 0), the id attribute a doit is keyed on, and
// its printer.
type resObject struct {
	cmd     uint16
	raw     bool // -r has a _RAW command for it
	filters []filterDef
	perDev  bool
	idAttr  uint16
	// line prints a reply body; flat is a doit's reply.
	line func(r *rd, body []byte, flat bool) error
}

// run is RES_FUNC's name(): the filters, then each device or port.
func (o *resObject) run(r *rd) error {
	if err := r.buildFilter(o.filters); err != nil {
		return err
	}
	if o.filters != nil {
		if err := r.setArgToDevname(); err != nil {
			return err
		}
	}
	if o.perDev {
		return r.execDev(o.send)
	}
	return r.execLink(o.send, false)
}

// send is RES_FUNC's _name(): an id filter's doit first, with its errors
// suppressed, and the dump when that fails.
func (o *resObject) send(r *rd) error {
	if r.showRaw && o.raw {
		r.errf("gordma: -r (the _RAW resource dumps) is not implemented\n")
		return errFail
	}
	if id, ok := r.doitIndex(); ok && o.idAttr != 0 {
		r.suppressErrors = true
		req, err := xtcpnl.BuildRdmaResDoitRequest(o.cmd, r.devIdx, r.portIdx, o.idAttr, id, seq())
		if err != nil {
			return err
		}
		err = r.talk(req, func(m Msg) error { return o.line(r, m.Body, true) })
		if err == nil {
			return nil
		}
		if !errors.Is(err, errFail) {
			return err
		}
	}
	req, err := xtcpnl.BuildRdmaResDumpRequest(o.cmd, r.devIdx, r.portIdx, seq())
	if err != nil {
		return err
	}
	return r.talk(req, func(m Msg) error { return o.line(r, m.Body, false) })
}

// entries is a parsed reply's objects, as the dump or the doit callback
// takes them: the dump's from the table, which must be there; the doit's
// the top level, as one.
func entries[T any](o xtcpnl.RdmaResObjects[T], err error, flat bool) ([]T, error) {
	if err != nil || !o.HasDevIndex || !o.HasDevName || o.Flat != flat {
		return nil, errCB
	}
	return o.Entries, nil
}

// each prints es with line, stopping at the first that fails.
func each[T any](es []T, line func(T) error) error {
	for _, e := range es {
		if err := line(e); err != nil {
			return err
		}
	}
	return nil
}

var resQP = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResQPGet, raw: true, idAttr: xtcpnl.RdmaNldevAttrResLQPN,
	filters: []filterDef{{"link", false, false}, {"lqpn", true, true}, {"rqpn", true, false},
		{"pid", true, false}, {"sq-psn", true, false}, {"rq-psn", true, false}, {"type", false, false},
		{"path-mig-state", false, false}, {"state", false, false}, {"pdn", true, false}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResQP(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(q xtcpnl.RdmaResQP) error { return r.qpLine(o.DevName, o.DevIndex, q) })
	},
}

var resCMID = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResCMIDGet, idAttr: xtcpnl.RdmaNldevAttrResCMIDN,
	filters: []filterDef{{"link", false, false}, {"lqpn", true, false}, {"qp-type", false, false},
		{"state", false, false}, {"ps", false, false}, {"dev-type", false, false},
		{"transport-type", false, false}, {"pid", true, false}, {"src-addr", false, false},
		{"src-port", true, false}, {"dst-addr", false, false}, {"dst-port", true, false},
		{"cm-idn", true, true}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResCMID(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(c xtcpnl.RdmaResCMID) error { return r.cmidLine(o.DevName, o.DevIndex, c) })
	},
}

var resCQ = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResCQGet, raw: true, perDev: true, idAttr: xtcpnl.RdmaNldevAttrResCQN,
	filters: []filterDef{{"dev", false, false}, {"users", true, false}, {"poll-ctx", false, false},
		{"pid", true, false}, {"cqn", true, true}, {"ctxn", true, false}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResCQ(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(c xtcpnl.RdmaResCQ) error { return r.cqLine(o.DevName, o.DevIndex, c) })
	},
}

var resMR = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResMRGet, raw: true, perDev: true, idAttr: xtcpnl.RdmaNldevAttrResMRN,
	filters: []filterDef{{"dev", false, false}, {"rkey", true, false}, {"lkey", true, false},
		{"mrlen", true, false}, {"pid", true, false}, {"mrn", true, true}, {"pdn", true, false}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResMR(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(m xtcpnl.RdmaResMR) error { return r.mrLine(o.DevName, o.DevIndex, m) })
	},
}

var resPD = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResPDGet, perDev: true, idAttr: xtcpnl.RdmaNldevAttrResPDN,
	filters: []filterDef{{"dev", false, false}, {"users", true, false}, {"pid", true, false},
		{"ctxn", true, false}, {"pdn", true, true}, {"ctxn", true, false}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResPD(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(d xtcpnl.RdmaResPD) error { return r.pdLine(o.DevName, o.DevIndex, d) })
	},
}

var resCtx = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResCtxGet, perDev: true, idAttr: xtcpnl.RdmaNldevAttrResCtxn,
	filters: []filterDef{{"dev", false, false}, {"pid", true, false}, {"ctxn", true, true}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResCtx(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(c xtcpnl.RdmaResCtx) error { return r.ctxLine(o.DevName, o.DevIndex, c) })
	},
}

var resSRQ = &resObject{
	cmd: xtcpnl.RdmaNldevCmdResSRQGet, raw: true, perDev: true, idAttr: xtcpnl.RdmaNldevAttrResSRQN,
	filters: []filterDef{{"dev", false, false}, {"pid", true, false}, {"srqn", true, true},
		{"type", false, false}, {"pdn", true, false}, {"cqn", true, false}, {"lqpn", true, false}},
	line: func(r *rd, body []byte, flat bool) error {
		o, err := xtcpnl.ParseRdmaResSRQ(body)
		es, err := entries(o, err, flat)
		if err != nil {
			return err
		}
		return each(es, func(s xtcpnl.RdmaResSRQ) error { return r.srqLine(o.DevName, o.DevIndex, s) })
	},
}

// resU32 is res_print_u32: the name, then " %u ".
func (r *rd) resU32(name string, v uint32, present bool) {
	if !present {
		return
	}
	if r.p.fpCtx(printAny) {
		r.p.raw(name + " " + strconv.FormatUint(uint64(v), 10) + " ")
		return
	}
	r.p.printUint(printAny, name, "", v)
}

// resU64 is res_print_u64.
func (r *rd) resU64(name string, v uint64, present bool) {
	if !present {
		return
	}
	if r.p.fpCtx(printAny) {
		r.p.raw(name + " " + strconv.FormatUint(v, 10) + " ")
		return
	}
	r.p.printU64(printAny, name, "", v)
}

// printKey is print_key: print_hex's unsigned int keeps the low 32 bits.
func (r *rd) printKey(name string, v uint64, present bool) {
	if !present {
		return
	}
	r.p.fp(name)
	r.p.printHex(printAny, name, " 0x%x ", uint32(v)) //nolint:gosec // print_hex's truncation
}

// printDev is print_dev.
func (r *rd) printDev(idx uint32, name string) {
	r.p.printInt(printAny, "ifindex", "", int32(idx)) //nolint:gosec // print_int's int
	r.p.printString(printAny, "ifname", "dev %s ", name)
}

// printLink is print_link: "link DEV/PORT ", or "DEV/-" with no port.
func (r *rd) printLink(idx uint32, name string, port uint32, hasPort bool) {
	p := r.p
	p.printUint(printJSON, "ifindex", "", idx)
	p.printString(printJSON, "ifname", "", name)
	tmp := name + "/-"
	if hasPort {
		p.printUint(printJSON, "port", "", port)
		tmp = name + "/" + strconv.FormatInt(int64(int32(port)), 10) //nolint:gosec // "%d"
	}
	if len(tmp) > 63 {
		tmp = tmp[:63]
	}
	p.fp("link " + tmp + " ")
}

// comm is a resource's owner name: /proc/<pid>/comm for a user one, which
// leaves none if it cannot be read, else the kernel's name.
func (r *rd) comm(o xtcpnl.RdmaResOwner) (string, bool) {
	if o.HasPID {
		return r.taskName(o.PID)
	}
	if o.HasKernName {
		return o.KernName, true
	}
	return "", false
}

// taskName is get_task_name (lib/fs.c): the first line of comm, in a
// 64-byte buffer.
func (r *rd) taskName(pid uint32) (string, bool) {
	if pid == 0 {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(r.procRoot, strconv.FormatInt(int64(int32(pid)), 10), "comm")) //nolint:gosec // pid_t
	if err != nil {
		return "", false
	}
	s := string(b)
	if len(s) > 63 {
		s = s[:63]
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return s, true
}

// printComm is print_comm: "[name]" for a kernel owner in text, in an
// 18-byte buffer.
func (r *rd) printComm(comm string, ok bool, o xtcpnl.RdmaResOwner) {
	if !ok {
		return
	}
	if !o.HasPID && !r.p.isJSON() {
		comm = "[" + comm + "]"
	}
	if len(comm) > 17 {
		comm = comm[:17]
	}
	r.p.printString(printAny, "comm", "comm %s ", comm)
}

// qpTypeStr is qp_types_to_str.
func qpTypeStr(t uint8) string {
	if t == 0xff {
		return "DRIVER"
	}
	return tableStr([]string{"SMI", "GSI", "RC", "UC", "UD", "RAW_IPV6", "RAW_ETHERTYPE", "UNKNOWN",
		"RAW_PACKET", "XRC_INI", "XRC_TGT"}, uint32(t), "UNKNOWN")
}

func qpStateStr(s uint8) string {
	return tableStr([]string{"RESET", "INIT", "RTR", "RTS", "SQD", "SQE", "ERR"}, uint32(s), "UNKNOWN")
}

func pathMigStr(s uint8) string {
	return tableStr([]string{"MIGRATED", "REARM", "ARMED"}, uint32(s), "UNKNOWN")
}

// qpLine is res_qp_line. A QP of another port than the one being dumped
// is skipped: rdma dumps each port, and the kernel sends every QP each time.
func (r *rd) qpLine(name string, idx uint32, q xtcpnl.RdmaResQP) error {
	if !q.HasLQPN || !q.HasSQPSN || !q.HasType || !q.HasState {
		return errCB
	}
	port := uint32(0)
	if q.HasPortIndex {
		port = q.PortIndex
	}
	if port != r.portIdx ||
		r.isFiltered("lqpn", q.LQPN, q.HasLQPN) ||
		r.isFiltered("pdn", q.PDN, q.HasPDN) ||
		r.isFiltered("rqpn", q.RQPN, q.HasRQPN) ||
		r.isFiltered("rq-psn", q.RQPSN, q.HasRQPSN) ||
		r.isFiltered("sq-psn", q.SQPSN, q.HasSQPSN) ||
		r.isStringFiltered("path-mig-state", pathMigStr(q.PathMigState)) ||
		r.isStringFiltered("type", qpTypeStr(q.Type)) ||
		r.isStringFiltered("state", qpStateStr(q.State)) {
		return nil
	}
	comm, ok := r.comm(q.RdmaResOwner)
	if r.isFiltered("pid", q.PID, q.HasPID) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printLink(idx, name, port, q.HasPortIndex)
	r.resU32("lqpn", q.LQPN, true)
	if q.HasRQPN {
		p.printUintAs(printAny, "rqpn", "rqpn %d ", q.RQPN, int32(q.RQPN)) //nolint:gosec // "%d"
	}
	p.printString(printAny, "type", "type %s ", qpTypeStr(q.Type))
	if q.Type == 0xff && q.HasSubtype {
		p.printString(printAny, "subtype", "subtype %s ", q.Subtype)
	}
	p.printString(printAny, "state", "state %s ", qpStateStr(q.State))
	if q.HasRQPSN {
		p.printUintAs(printAny, "rq-psn", "rq-psn %d ", q.RQPSN, int32(q.RQPSN)) //nolint:gosec // "%d"
	}
	r.resU32("sq-psn", q.SQPSN, true)
	if q.HasPathMigState {
		p.printString(printAny, "path-mig-state", "path-mig-state %s ", pathMigStr(q.PathMigState))
	}
	r.resU32("pdn", q.PDN, q.HasPDN)
	r.resU32("pid", q.PID, q.HasPID)
	r.printComm(comm, ok, q.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

func pollCtxStr(c uint8) string {
	return tableStr([]string{"DIRECT", "SOFTIRQ", "WORKQUEUE", "UNBOUND_WORKQUEUE"}, uint32(c), "UNKNOWN")
}

// cqLine is res_cq_line.
func (r *rd) cqLine(name string, idx uint32, c xtcpnl.RdmaResCQ) error {
	if !c.HasCQE || !c.HasUseCnt {
		return errCB
	}
	if r.isFiltered("users", uint32(c.UseCnt), true) || //nolint:gosec // rd_is_filtered_attr's uint32
		r.isStringFiltered("poll-ctx", pollCtxStr(c.PollCtx)) {
		return nil
	}
	comm, ok := r.comm(c.RdmaResOwner)
	if r.isFiltered("pid", c.PID, c.HasPID) ||
		r.isFiltered("cqn", c.CQN, c.HasCQN) ||
		r.isFiltered("ctxn", c.CTXN, c.HasCTXN) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printDev(idx, name)
	r.resU32("cqn", c.CQN, c.HasCQN)
	r.resU32("cqe", c.CQE, true)
	r.resU64("users", c.UseCnt, true)
	if c.HasPollCtx {
		p.printString(printAny, "poll-ctx", "poll-ctx %s ", pollCtxStr(c.PollCtx))
	}
	if c.HasDIM && c.DIM <= 1 {
		p.printOnOff(printAny, "adaptive-moderation", "adaptive-moderation %s ", c.DIM == 1)
	}
	r.resU32("ctxn", c.CTXN, c.HasCTXN)
	r.resU32("pid", c.PID, c.HasPID)
	r.printComm(comm, ok, c.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

// mrLine is res_mr_line.
func (r *rd) mrLine(name string, idx uint32, m xtcpnl.RdmaResMR) error {
	if !m.HasMRLen {
		return errCB
	}
	if r.isFiltered("mrlen", uint32(m.MRLen), true) { //nolint:gosec // rd_is_filtered_attr's uint32
		return nil
	}
	comm, ok := r.comm(m.RdmaResOwner)
	if r.isFiltered("pid", m.PID, m.HasPID) ||
		r.isFiltered("mrn", m.MRN, m.HasMRN) ||
		r.isFiltered("pdn", m.PDN, m.HasPDN) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printDev(idx, name)
	r.resU32("mrn", m.MRN, m.HasMRN)
	r.printKey("rkey", uint64(m.RKey), m.HasRKey)
	r.printKey("lkey", uint64(m.LKey), m.HasLKey)
	r.printKey("iova", m.IOVA, m.HasIOVA)
	r.resU64("mrlen", m.MRLen, true)
	r.resU32("pdn", m.PDN, m.HasPDN)
	r.resU32("pid", m.PID, m.HasPID)
	r.printComm(comm, ok, m.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

// pdLine is res_pd_line.
func (r *rd) pdLine(name string, idx uint32, d xtcpnl.RdmaResPD) error {
	if !d.HasUseCnt {
		return errCB
	}
	if r.isFiltered("users", uint32(d.UseCnt), true) { //nolint:gosec // rd_is_filtered_attr's uint32
		return nil
	}
	comm, ok := r.comm(d.RdmaResOwner)
	if r.isFiltered("pid", d.PID, d.HasPID) ||
		r.isFiltered("ctxn", d.CTXN, d.HasCTXN) ||
		r.isFiltered("pdn", d.PDN, d.HasPDN) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printDev(idx, name)
	r.resU32("pdn", d.PDN, d.HasPDN)
	r.printKey("local_dma_lkey", uint64(d.LocalDMALKey), d.HasLocalDMALKey)
	r.resU64("users", d.UseCnt, true)
	r.printKey("unsafe_global_rkey", uint64(d.UnsafeGlobalRKey), d.HasUnsafeGlobalRKey)
	r.resU32("ctxn", d.CTXN, d.HasCTXN)
	r.resU32("pid", d.PID, d.HasPID)
	r.printComm(comm, ok, d.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

// ctxLine is res_ctx_line.
func (r *rd) ctxLine(name string, idx uint32, c xtcpnl.RdmaResCtx) error {
	if !c.HasCTXN {
		return errCB
	}
	comm, ok := r.comm(c.RdmaResOwner)
	if r.isFiltered("pid", c.PID, c.HasPID) || r.isFiltered("ctxn", c.CTXN, true) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printDev(idx, name)
	r.resU32("ctxn", c.CTXN, true)
	r.resU32("pid", c.PID, c.HasPID)
	r.printComm(comm, ok, c.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

func srqTypeStr(t uint8) string {
	return tableStr([]string{"BASIC", "XRC", "TM"}, uint32(t), "UNKNOWN")
}

// srqQPMax is res-srq.c's MAX_QP_STR_LEN; srqQPPart is the 16-byte buffer
// each piece is formatted into.
const (
	srqQPMax  = 256
	srqQPPart = 16
)

// srqQPs is get_srq_qps and filter_srq_range_qps: the SRQ's QPs, less
// the filtered ones, as "a,b-c". ok is false when the SRQ is not printed.
//
// xtcpnl decodes a lone LQPN as a range of one, which these print the
// same way. It does not say whether RES_QP was sent at all; rdma skips an
// SRQ without it, and gordma prints it with no lqpn.
func (r *rd) srqQPs(qps []xtcpnl.RdmaQPRange) (string, bool) {
	if len(qps) == 0 {
		return "", !r.keyExists("lqpn")
	}
	var b strings.Builder
	delim := ""
	add := func(format string, a ...any) {
		s := fmt.Sprintf(format, a...)
		if len(s) > srqQPPart-1 {
			s = s[:srqQPPart-1]
		}
		if room := srqQPMax - 1 - b.Len(); len(s) > room {
			s = s[:max(room, 0)]
		}
		b.WriteString(s)
		delim = ","
	}
	for _, q := range qps {
		if q.Min == q.Max {
			if r.isFiltered("lqpn", q.Min, true) {
				continue
			}
			add("%s%d", delim, int32(q.Min)) //nolint:gosec // "%d"
			continue
		}
		var lo, hi uint32
		flush := func() {
			if hi == 0 {
				add("%s%d", delim, int32(lo)) //nolint:gosec // "%d"
			} else {
				add("%s%d-%d", delim, int32(lo), int32(hi)) //nolint:gosec // "%d"
			}
		}
		for n := q.Min; n <= q.Max; n++ {
			switch {
			case r.isFiltered("lqpn", n, true):
				if lo != 0 {
					flush()
					lo, hi = 0, 0
				}
			case lo == 0:
				lo = n
			default:
				hi = n
			}
			if n == ^uint32(0) {
				break
			}
		}
		if lo != 0 {
			flush()
		}
	}
	return b.String(), b.Len() != 0
}

// srqLine is res_srq_line.
func (r *rd) srqLine(name string, idx uint32, s xtcpnl.RdmaResSRQ) error {
	if !s.HasSRQN {
		return errCB
	}
	comm, ok := r.comm(s.RdmaResOwner)
	if r.isFiltered("pid", s.PID, s.HasPID) ||
		r.isFiltered("srqn", s.SRQN, true) ||
		r.isStringFiltered("type", srqTypeStr(s.Type)) ||
		r.isFiltered("pdn", s.PDN, s.HasPDN) ||
		r.isFiltered("cqn", s.CQN, s.HasCQN) {
		return nil
	}
	qps, shown := r.srqQPs(s.QPs)
	if !shown {
		return nil
	}
	p := r.p
	p.openObject()
	r.printDev(idx, name)
	r.resU32("srqn", s.SRQN, true)
	p.printString(printAny, "type", "type %s ", srqTypeStr(s.Type))
	if qps != "" {
		p.openArray(printAny, "lqpn")
		p.fp(" ")
		toks := strings.FieldsFunc(qps, func(c rune) bool { return c == ',' })
		for i, t := range toks {
			p.printString(printAny, "", "%s", t)
			if i+1 < len(toks) {
				p.fp(",")
			}
		}
		p.fp(" ")
		p.closeArray(printJSON, "")
	}
	r.resU32("pdn", s.PDN, s.HasPDN)
	r.resU32("cqn", s.CQN, s.HasCQN)
	r.resU32("pid", s.PID, s.HasPID)
	r.printComm(comm, ok, s.RdmaResOwner)
	p.closeObject()
	p.newline()
	return nil
}

// RDMA_PS_* (rdma_user_cm.h).
const (
	rdmaPSIPoIB = 0x0002
	rdmaPSIB    = 0x013f
	rdmaPSTCP   = 0x0106
	rdmaPSUDP   = 0x0111
)

func cmStateStr(s uint8) string {
	return tableStr([]string{"IDLE", "ADDR_QUERY", "ADDR_RESOLVED", "ROUTE_QUERY", "ROUTE_RESOLVED",
		"CONNECT", "DISCONNECT", "ADDR_BOUND", "LISTEN", "DEVICE_REMOVAL", "DESTROYING"}, uint32(s), "UNKNOWN")
}

func psStr(ps uint32) string {
	switch ps {
	case rdmaPSIPoIB, rdmaPSIB:
		return "IPoIB"
	case rdmaPSTCP:
		return "TCP"
	case rdmaPSUDP:
		return "UDP"
	}
	return "---"
}

// cmidLine is res_cm_id_line. A CM ID with no port, a listener on every
// port, is printed for each port rdma dumps.
func (r *rd) cmidLine(name string, idx uint32, c xtcpnl.RdmaResCMID) error {
	if !c.HasState || !c.HasPS {
		return errCB
	}
	port := uint32(0)
	if c.HasPortIndex {
		port = c.PortIndex
	}
	if port != 0 && port != r.portIdx {
		return nil
	}
	if r.isFiltered("lqpn", c.LQPN, c.HasLQPN) ||
		r.isStringFiltered("qp-type", qpTypeStr(c.Type)) ||
		r.isStringFiltered("ps", psStr(c.PS)) ||
		r.isStringFiltered("state", cmStateStr(c.State)) {
		return nil
	}
	var src, dst string
	if c.HasSrc {
		var ok bool
		if src, ok = ntop(c.Src); !ok {
			return nil
		}
	}
	if r.isStringFiltered("src-addr", src) || r.isFiltered("src-port", uint32(c.Src.Port), c.HasSrc) {
		return nil
	}
	if c.HasDst {
		var ok bool
		if dst, ok = ntop(c.Dst); !ok {
			return nil
		}
	}
	if r.isStringFiltered("dst-addr", dst) || r.isFiltered("dst-port", uint32(c.Dst.Port), c.HasDst) {
		return nil
	}
	comm, ok := r.comm(c.RdmaResOwner)
	if r.isFiltered("pid", c.PID, c.HasPID) || r.isFiltered("cm-idn", c.CMIDN, c.HasCMIDN) {
		return nil
	}
	p := r.p
	p.openObject()
	r.printLink(idx, name, port, c.HasPortIndex)
	r.resU32("cm-idn", c.CMIDN, c.HasCMIDN)
	r.resU32("lqpn", c.LQPN, c.HasLQPN)
	if c.HasType {
		p.printString(printAny, "qp-type", "qp-type %s ", qpTypeStr(c.Type))
	}
	p.printString(printAny, "state", "state %s ", cmStateStr(c.State))
	p.printString(printAny, "ps", "ps %s ", psStr(c.PS))
	r.resU32("pid", c.PID, c.HasPID)
	r.printComm(comm, ok, c.RdmaResOwner)
	if c.HasSrc {
		r.printIPAddr("src-addr", src, c.Src.Port)
	}
	if c.HasDst {
		r.printIPAddr("dst-addr", dst, c.Dst.Port)
	}
	p.closeObject()
	p.newline()
	return nil
}

// printIPAddr is print_ipaddr: "addr:port" in JSON, "key addr:port " as
// text.
func (r *rd) printIPAddr(key, addr string, port uint16) {
	if r.p.jsonCtx(printAny) {
		r.p.printString(printAny, key, "", fmt.Sprintf("%s:%d", addr, port))
		return
	}
	r.p.raw(fmt.Sprintf("%s %s:%d ", key, addr, port))
}
