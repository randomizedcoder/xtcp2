package goip

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip xfrm policy`'s writes (ip/xfrm_policy.c):
//   - add and update, xfrm_policy_modify: one NEWPOLICY or UPDPOLICY,
//     struct xfrm_userpolicy_info and the attributes its words give, with
//     NLM_F_ACK;
//   - delete, xfrm_policy_get_or_delete: get's request as a DELPOLICY with
//     NLM_F_ACK;
//   - deleteall, xfrm_policy_list_or_deleteall: GETPOLICY dumps, each
//     answer the filter passes copied into a DELPOLICY, the copies sent in
//     one datagram with no ACK asked for, until a dump finds none;
//   - flush, xfrm_policy_flush: one FLUSHPOLICY, with no header;
//   - set, xfrm_spd_setinfo: one NEWSPDINFO and the hash thresholds;
//   - setdefault, xfrm_spd_setdefault: one SETDEFAULT, struct
//     xfrm_userpolicy_default.
//
// As for the state writes, goip sends the dumps and writes every write to
// GOIP_WRITE_OUT, as ip sends it, and sends none.

const (
	// xfrmTmplsBufCst is XFRM_TMPLS_BUF_SIZE, the templates' buffer: 16.
	xfrmTmplsBufCst = 1024
	// xfrmPolicyIDMsgCst is NLMSG_LENGTH(sizeof(struct
	// xfrm_userpolicy_id)), what deleteall needs free to copy a policy.
	xfrmPolicyIDMsgCst = xtcpnl.NlMsgHdrSizeCst + xtcpnl.XfrmUserpolicyIDSizeCst
	// xfrmTmplAlgosAllCst is a template's aalgos, ealgos and calgos: any.
	xfrmTmplAlgosAllCst = 0xffffffff
	xfrmOffloadCst      = "offload"
	xfrmLevelCst        = "level"
	xfrmLimitCst        = "limit"
	xfrmTmplCst         = "tmpl"
)

// xfrmPolicyInfo is struct xfrm_userpolicy_info as xfrm_policy_modify
// fills it.
type xfrmPolicyInfo struct {
	sel                xfrmSel
	lft                [8]uint64
	priority, index    uint32
	dir, action, flags uint8
}

// bytes is the policy's 168 bytes; its current lifetime is zero.
func (p *xfrmPolicyInfo) bytes() []byte {
	b := make([]byte, xtcpnl.XfrmUserpolicyInfoSizeCst)
	copy(b, p.sel.bytes())
	for i, v := range p.lft {
		binary.LittleEndian.PutUint64(b[56+8*i:], v)
	}
	binary.LittleEndian.PutUint32(b[152:], p.priority)
	binary.LittleEndian.PutUint32(b[156:], p.index)
	b[160], b[161], b[162] = p.dir, p.action, p.flags
	return b
}

// xfrmTmpl is struct xfrm_user_tmpl as xfrm_tmpl_parse fills it.
type xfrmTmpl struct {
	id                  xfrmID
	family              uint16
	reqid               uint32
	mode, optional      uint8
	aalgos, ealgos, cal uint32
}

// bytes is the template's 64 bytes.
func (t *xfrmTmpl) bytes() []byte {
	b := make([]byte, xtcpnl.XfrmUserTmplSizeCst)
	copy(b, t.id.daddr[:])
	binary.BigEndian.PutUint32(b[16:], t.id.spi)
	b[20] = t.id.proto
	binary.LittleEndian.PutUint16(b[24:], t.family)
	copy(b[28:], t.id.saddr[:])
	binary.LittleEndian.PutUint32(b[44:], t.reqid)
	b[48], b[50] = t.mode, t.optional
	binary.LittleEndian.PutUint32(b[52:], t.aalgos)
	binary.LittleEndian.PutUint32(b[56:], t.ealgos)
	binary.LittleEndian.PutUint32(b[60:], t.cal)
	return b
}

// xfrmPolicyMod is xfrm_policy_modify's state: the policy, and what
// its loop reads for the attributes after it.
type xfrmPolicyMod struct {
	c                         *runCtx
	info                      xfrmPolicyInfo
	dirp, selp, ptypep, sctxp bool
	ptype                     uint8
	tmpls, ctx                []byte
	mark                      [2]uint32
	ifID                      uint32
	ifIDSet, offload          bool
	ifindex                   int32
}

// xfrmPolicyModifyWords are the loop's words by strcmp; any other is the
// selector, once.
var xfrmPolicyModifyWords = map[string]func(m *xfrmPolicyMod, a *xfrmArgs) error{
	xfrmDirCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		if m.dirp {
			return xfrmUsage(dupargErr(xfrmDirCst, a.cur()))
		}
		m.dirp = true
		return xfrmPolicyNextDir(a, &m.info.dir)
	},
	xfrmCtxCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		if m.sctxp {
			return xfrmUsage(dupargErr(xfrmCtxCst, a.cur()))
		}
		m.sctxp = true
		if err := a.next(); err != nil {
			return err
		}
		if len(a.cur())+1 > xfrmCtxBufCst {
			return fmt.Errorf("a ctx of %d bytes, which overruns ip's %d-byte buffer: %w", len(a.cur())+1,
				xfrmCtxBufCst, ErrNotImplemented)
		}
		m.ctx = xfrmSecCtxBytes(a.cur())
		return nil
	},
	xfrmMarkCst:       func(m *xfrmPolicyMod, a *xfrmArgs) error { return xfrmParseMark(a, &m.mark) },
	nhIndexKeywordCst: func(m *xfrmPolicyMod, a *xfrmArgs) error { return xfrmPolicyNextU32(a, &m.info.index, "INDEX") },
	xfrmPtypeCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		if m.ptypep {
			return xfrmUsage(dupargErr(xfrmPtypeCst, a.cur()))
		}
		m.ptypep = true
		return xfrmPolicyNextPtype(a, &m.ptype)
	},
	xfrmActionCst: func(m *xfrmPolicyMod, a *xfrmArgs) error { return xfrmPolicyNextAction(a, &m.info.action) },
	xfrmPriorityCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		return xfrmPolicyNextU32(a, &m.info.priority, "PRIORITY")
	},
	xfrmFlagCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		if err := a.next(); err != nil {
			return err
		}
		return xfrmPolicyFlagParse(a, &m.info.flags)
	},
	xfrmLimitCst: func(m *xfrmPolicyMod, a *xfrmArgs) error { return xfrmLifetimeParse(a, &m.info.lft) },
	xfrmTmplCst:  (*xfrmPolicyMod).tmpl,
	xfrmIfIDCst: func(m *xfrmPolicyMod, a *xfrmArgs) error {
		m.ifIDSet = true
		return xfrmPolicyNextU32(a, &m.ifID, "IF_ID")
	},
	xfrmOffloadCst: (*xfrmPolicyMod).offloadDev,
}

// xfrmPolicyModify is xfrm_policy_modify: the loop, DIR required, then the
// NEWPOLICY or UPDPOLICY with its attributes in ip's order, the policy
// type, the templates, the mark when its mask is set, the context, the
// interface id and the offload, with NLM_F_ACK to GOIP_WRITE_OUT. Each
// attribute is addattr_l's, whose bound none reaches: 16 templates, a
// 256-byte context and the rest come to under 1600 bytes.
func xfrmPolicyModify(c *runCtx, msgType uint16, args []string) error {
	m := &xfrmPolicyMod{c: c}
	m.info.sel.family = uint16(c.family)
	for i := range 4 {
		m.info.lft[i] = ^uint64(0)
	}
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		var err error
		if w, ok := xfrmPolicyModifyWords[a.cur()]; ok {
			err = w(m, a)
		} else {
			err = m.selector(a)
		}
		if err != nil {
			return err
		}
	}
	if !m.dirp {
		return fmt.Errorf("%w: Not enough information: DIR is required", ErrXfrmRefused)
	}
	attrs, err := m.attrs()
	if err != nil {
		return err
	}
	if m.info.sel.family == unix.AF_UNSPEC {
		m.info.sel.family = unix.AF_INET
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(msgType, unix.NLM_F_ACK, c.nextSeq(), m.info.bytes(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// selector is the loop's last branch: the selector, once, a second one
// duparg's "unknown"; its family becomes preferred_family's when that has
// none.
func (m *xfrmPolicyMod) selector(a *xfrmArgs) error {
	if m.selp {
		return xfrmUsage(dupargErr("unknown", a.cur()))
	}
	m.selp = true
	if err := xfrmSelectorParse(m.c, a, &m.info.sel); err != nil {
		return err
	}
	if m.c.family == unix.AF_UNSPEC {
		m.c.family = uint8(m.info.sel.family)
	}
	return nil
}

// attrs are the attributes after the loop, in ip's order.
func (m *xfrmPolicyMod) attrs() ([]byte, error) {
	var raw [xfrmRtaBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	if m.ptypep {
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrPolicyType, xfrmPolicyTypeBytes(m.ptype)))
	}
	if len(m.tmpls) > 0 {
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrTmpl, m.tmpls))
	}
	if m.mark[1] != 0 {
		v := binary.LittleEndian.AppendUint32(nil, m.mark[0])
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrMark, binary.LittleEndian.AppendUint32(v, m.mark[1])))
	}
	if m.ctx != nil {
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrSecCtx, m.ctx))
	}
	if m.ifIDSet {
		errs = append(errs, b.PutU32(xtcpnl.XfrmAttrIfID, m.ifID))
	}
	if m.offload {
		xuo := binary.LittleEndian.AppendUint32(nil, uint32(m.ifindex))
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrOffloadDev, append(xuo, xfrmOffloadPktCst, 0, 0, 0)))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// tmpl is tmpl: past 16 templates, "Too many tmpls", exit 1; else a
// template in preferred_family, any algorithm, and xfrm_tmpl_parse's
// words.
func (m *xfrmPolicyMod) tmpl(a *xfrmArgs) error {
	if len(m.tmpls)+xtcpnl.XfrmUserTmplSizeCst > xfrmTmplsBufCst {
		return fmt.Errorf("%w: Too many tmpls: buffer overflow", ErrXfrmRefused)
	}
	t := xfrmTmpl{family: uint16(m.c.family), aalgos: xfrmTmplAlgosAllCst, ealgos: xfrmTmplAlgosAllCst,
		cal: xfrmTmplAlgosAllCst}
	if err := a.next(); err != nil {
		return err
	}
	if err := xfrmTmplParse(m.c, a, &t); err != nil {
		return err
	}
	m.tmpls = append(m.tmpls, t.bytes()...)
	return nil
}

// xfrmTmplParse is xfrm_tmpl_parse: mode, reqid and level by strcmp, each
// with its value, else the ID, once, parsed with preferred_family cleared
// and left as the template's family; a second other word is left as
// PREV_ARG leaves it. ip's missarg of TMPL is unreachable: the ID's own
// check refuses an ID with no protocol first.
func xfrmTmplParse(c *runCtx, a *xfrmArgs, t *xfrmTmpl) error {
	idp := false
	for {
		var err error
		switch a.cur() {
		case xfrmModeCst:
			err = xfrmNextMode(a, &t.mode)
		case xfrmReqidCst:
			err = xfrmNextReqid(a, &t.reqid)
		case xfrmLevelCst:
			err = xfrmNextLevel(a, &t.optional)
		default:
			if idp {
				a.i--
				return nil
			}
			idp = true
			c.family = unix.AF_UNSPEC
			var f xfrmFilter
			err = xfrmIDParse(c, a, &t.id, &t.family, &f, false)
			c.family = uint8(t.family)
		}
		if err != nil {
			return err
		}
		if !a.nextOK() {
			return nil
		}
		a.i++
	}
}

// xfrmNextLevel is level's value: required or use, by strcmp; ip's
// message ends in a newline of its own.
func xfrmNextLevel(a *xfrmArgs, optional *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	switch a.cur() {
	case "required":
		*optional = 0
	case "use":
		*optional = 1
	default:
		return xfrmUsage(invargErr("LEVEL value is invalid\n", a.cur()))
	}
	return nil
}

// offloadDev is offload: packet, the only mode, then dev and its index.
func (m *xfrmPolicyMod) offloadDev(a *xfrmArgs) error {
	if err := a.next(); err != nil {
		return err
	}
	if a.cur() != "packet" {
		return xfrmUsage(invargErr("Invalid offload mode", a.cur()))
	}
	if err := a.next(); err != nil {
		return err
	}
	if a.cur() != fouDevCst {
		return xfrmUsage(invargErr("Missing dev keyword", a.cur()))
	}
	if err := a.next(); err != nil {
		return err
	}
	idx, err := genlNameToIndex(m.c, a.cur())
	if err != nil {
		return err
	}
	if idx == 0 {
		return xfrmUsage(invargErr("Invalid device name", a.cur()))
	}
	m.ifindex, m.offload = idx, true
	return nil
}

// xfrmPolicyDelete is xfrm_policy_delete: get's request as a DELPOLICY,
// with NLM_F_ACK, to GOIP_WRITE_OUT.
func xfrmPolicyDelete(c *runCtx, args []string) error {
	hdr, attrs, err := xfrmPolicyIDRequest(c, args)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgDelPolicy, unix.NLM_F_ACK, c.nextSeq(), hdr, attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmPolicyFlush is xfrm_policy_flush: ptype, once, else "unknown", then
// one FLUSHPOLICY, with no header, XFRMA_POLICY_TYPE when ptype was given,
// and NLM_F_ACK, to GOIP_WRITE_OUT.
func xfrmPolicyFlush(c *runCtx, args []string) error {
	var ptype uint8
	ptypep := false
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		if a.cur() != xfrmPtypeCst {
			return xfrmUsage(invargErr("unknown", a.cur()))
		}
		if ptypep {
			return xfrmUsage(dupargErr(xfrmPtypeCst, a.cur()))
		}
		ptypep = true
		if err := xfrmPolicyNextPtype(a, &ptype); err != nil {
			return err
		}
	}
	var attrs []byte
	if ptypep {
		attrs = xfrmAttr(xtcpnl.XfrmAttrPolicyType, xfrmPolicyTypeBytes(ptype))
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgFlushPolicy, unix.NLM_F_ACK, c.nextSeq(), nil, attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmAttr is one attribute, its rta_len unpadded and its data padded to
// four bytes.
func xfrmAttr(typ uint16, data []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(4+len(data)))
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = append(b, data...)
	return append(b, make([]byte, nlmsgAlign(len(b))-len(b))...)
}

// xfrmHthresh is one of set's words: its attribute and the largest prefix
// length its family allows.
type xfrmHthresh struct {
	attr uint16
	max  uint8
}

// xfrmHthreshes are xfrm_spd_setinfo's words.
var xfrmHthreshes = map[string]xfrmHthresh{
	"hthresh4": {xtcpnl.XfrmSPDAttrIPv4HThresh, 32},
	"hthresh6": {xtcpnl.XfrmSPDAttrIPv6HThresh, 128},
}

// xfrmPolicySet is xfrm_spd_setinfo: hthresh4 and hthresh6, once each,
// each with its local and remote prefix lengths, get_u8 base 0 and no
// longer than the family's, its attribute in argument order; another word
// is "unknown". Then one NEWSPDINFO, its u32 all ones, with NLM_F_ACK, to
// GOIP_WRITE_OUT.
func xfrmPolicySet(c *runCtx, args []string) error {
	seen := map[string]bool{}
	var attrs []byte
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		w := a.cur()
		h, ok := xfrmHthreshes[w]
		if !ok {
			return xfrmUsage(invargErr("unknown", w))
		}
		if seen[w] {
			return xfrmUsage(dupargErr(w, w))
		}
		seen[w] = true
		var thr [2]uint8
		for i, what := range []string{"LBITS", "RBITS"} {
			if err := a.next(); err != nil {
				return err
			}
			v, ok := getU8(a.cur(), 0)
			if !ok || v > h.max {
				return xfrmUsage(invargErr(w+" "+what+" value is invalid", a.cur()))
			}
			thr[i] = v
		}
		attrs = append(attrs, xfrmAttr(h.attr, thr[:])...)
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgNewSPDInfo, unix.NLM_F_ACK, c.nextSeq(),
		binary.LittleEndian.AppendUint32(nil, xfrmMaskAllCst), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmDefaultDirs are xfrm_spd_setdefault's words, each its byte in
// struct xfrm_userpolicy_default.
var xfrmDefaultDirs = map[string]int{"in": 0, "fwd": 1, xfrmDirOutCst: 2}

// xfrmPolicySetDefault is xfrm_spd_setdefault: in, fwd and out, each with
// block, or accept or allow, xfrm_str_to_policy's; one already set is
// duparg's, another word "unknown direction". Then one SETDEFAULT, an
// unset direction 0, with NLM_F_ACK, to GOIP_WRITE_OUT.
func xfrmPolicySetDefault(c *runCtx, args []string) error {
	up := make([]byte, xtcpnl.XfrmUserpolicyDefaultSizeCst)
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		w := a.cur()
		i, ok := xfrmDefaultDirs[w]
		if !ok {
			return xfrmUsage(invargErr("unknown direction", w))
		}
		if up[i] != 0 {
			return xfrmUsage(dupargErr(w, w))
		}
		if err := a.next(); err != nil {
			return err
		}
		switch a.cur() {
		case "block":
			up[i] = 1
		case "accept", "allow":
			up[i] = 2
		default:
			return xfrmUsage(invargErr(w+" policy value is invalid", a.cur()))
		}
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgSetDefault, unix.NLM_F_ACK, c.nextSeq(), up, nil)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmPolicyDeleteall is deleteall's state across its rounds: the filter,
// each DELPOLICY copied, without its sequence number, to skip a policy
// copied in an earlier round, and the copies' bytes.
type xfrmPolicyDeleteall struct {
	c      *runCtx
	f      *xfrmPolicyFilter
	copied map[string]bool
	out    []byte
}

// xfrmPolicyDeleteallRun is xfrm_policy_list_or_deleteall's deleteall: the
// list's words, then rounds of a GETPOLICY dump, each answer
// xfrm_policy_keep copies a DELPOLICY of, until a round copies none. ip
// sends each round's copies in one datagram; goip writes every round's to
// GOIP_WRITE_OUT, when there are any.
func xfrmPolicyDeleteallRun(c *runCtx, args []string) error {
	f, err := xfrmPolicyListArgs(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	d := &xfrmPolicyDeleteall{c: c, f: &f, copied: map[string]bool{}}
	for {
		req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetPolicy, unix.NLM_F_DUMP, c.nextSeq(), nil, nil)
		if err != nil {
			return err
		}
		msgs, err := xs.XfrmDump(req)
		n := 0
		if err == nil {
			n, err = d.round(msgs)
		}
		if errors.Is(err, ErrNotImplemented) {
			return err
		}
		if err != nil {
			return fmt.Errorf("%w: Delete-all terminated: %w", ErrXfrmDumpTerminated, err)
		}
		if n == 0 {
			break
		}
	}
	if len(d.out) == 0 {
		return nil
	}
	return writeAddrOut(d.out)
}

// round is one dump's answers through xfrm_policy_keep: the copies' count.
func (d *xfrmPolicyDeleteall) round(msgs []xfrmMsg) (int, error) {
	var batch []byte
	n := 0
	for _, m := range msgs {
		del, err := d.keep(m, len(batch))
		if err != nil {
			return 0, err
		}
		if del == nil {
			continue
		}
		batch = append(batch, del...)
		n++
	}
	d.out = append(d.out, batch...)
	return n, nil
}

// keep is xfrm_policy_keep: another type than NEWPOLICY is "Not a policy"
// on stderr; a body short of its struct, or a policy type short of its,
// ends the dump; a policy the filter does not match, a socket policy, or
// one past the room left in the buffer, is skipped, as is one an earlier
// round copied; else a DELPOLICY of its selector, index and direction,
// its mark as xfrm_state_keep copies an SA's, and its interface id,
// without NLM_F_ACK.
func (d *xfrmPolicyDeleteall) keep(m xfrmMsg, offset int) ([]byte, error) {
	if m.typ != xtcpnl.XfrmMsgNewPolicy {
		_, err := fmt.Fprintf(d.c.errOut, "Not a policy: %08x %08x %08x\n", xtcpnl.NlMsgHdrSizeCst+len(m.body), m.typ,
			m.flags)
		return nil, err
	}
	if len(m.body) < xtcpnl.XfrmUserpolicyInfoSizeCst {
		return nil, fmt.Errorf("BUG: wrong nlmsg len %d", len(m.body)-xtcpnl.XfrmUserpolicyInfoSizeCst)
	}
	p, err := xtcpnl.ParseXfrmPolicy(xtcpnl.NlMsgHdr{Type: m.typ, Flags: m.flags}, m.body)
	if err != nil {
		return nil, err
	}
	var ptype uint8
	if p.Attrs.PolicyType != nil {
		ptype = *p.Attrs.PolicyType
	}
	if !d.f.match(p.Info, ptype) || p.Info.Dir >= xfrmPolicyMaxCst || offset+xfrmPolicyIDMsgCst > xfrmDeleteallBufCst {
		return nil, nil
	}
	hdr := make([]byte, xtcpnl.XfrmUserpolicyIDSizeCst)
	copy(hdr, m.body[:56])
	copy(hdr[56:60], m.body[156:160])
	hdr[60] = m.body[160]
	mark, err := xfrmDeleteallMark(m.body, xtcpnl.XfrmUserpolicyInfoSizeCst)
	if err != nil {
		return nil, err
	}
	var attrs []byte
	if mark != nil {
		attrs = xfrmAttr(xtcpnl.XfrmAttrMark, mark)
	}
	if p.Attrs.IfID != nil {
		attrs = append(attrs, xfrmAttr(xtcpnl.XfrmAttrIfID, binary.LittleEndian.AppendUint32(nil, *p.Attrs.IfID))...)
	}
	del, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgDelPolicy, 0, 0, hdr, attrs)
	if err != nil {
		return nil, err
	}
	key := string(del[xtcpnl.NlMsgHdrSizeCst:])
	if d.copied[key] {
		return nil, nil
	}
	d.copied[key] = true
	if offset+len(del) > xfrmDeleteallBufCst {
		return nil, fmt.Errorf("a deleteall round of more than %d bytes, which overruns ip's buffer: %w",
			xfrmDeleteallBufCst, ErrNotImplemented)
	}
	binary.LittleEndian.PutUint32(del[8:], d.c.nextSeq())
	return del, nil
}
