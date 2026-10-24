package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip xfrm policy` is do_xfrm_policy (ip/xfrm_policy.c): with no verb a
// list, else the verb by prefix, in its order: add, update, delete,
// deleteall (or delall), list (show, lst), get, flush, count, set,
// setdefault, getdefault and help.
//
// goip runs the reads, each on its own NETLINK_XFRM socket as ip's do:
// list, a GETPOLICY dump each answer of which is printed, filtered on goip's
// side as ip's is; get, one GETPOLICY; count, one GETSPDINFO; and
// getdefault, one GETDEFAULT. The writes are G7-T04's.

const (
	xfrmPtypeCst    = "ptype"
	xfrmPriorityCst = "priority"
	xfrmActionCst   = "action"
	xfrmCtxCst      = "ctx"
	xfrmIfIDCst     = "if_id"
	xfrmCountCst    = "count"
	xfrmMainCst     = "main"
	xfrmIcmpCst     = "icmp"
	// xfrmPolicyMaxCst is XFRM_POLICY_MAX: a direction at or past it is a
	// socket policy's.
	xfrmPolicyMaxCst = 3
	// xfrmDefaultMinCst is NLMSG_SPACE of struct xfrm_userpolicy_default,
	// the least nlmsg_len xfrm_policy_default_print takes.
	xfrmDefaultMinCst = xtcpnl.NlMsgHdrSizeCst + 4
	// xfrmPolicyUsageCst is ip/xfrm_policy.c's usage().
	xfrmPolicyUsageCst = "Usage: ip xfrm policy { add | update } SELECTOR dir DIR [ ctx CTX ]\n" +
		"\t[ mark MARK [ mask MASK ] ] [ index INDEX ] [ ptype PTYPE ]\n" +
		"\t[ action ACTION ] [ priority PRIORITY ] [ flag FLAG-LIST ]\n" +
		"\t[ if_id IF_ID ] [ LIMIT-LIST ] [ TMPL-LIST ]\n" +
		"\t[ offload packet dev DEV] } ]\n" +
		"Usage: ip xfrm policy { delete | get } { SELECTOR | index INDEX } dir DIR\n" +
		"\t[ ctx CTX ] [ mark MARK [ mask MASK ] ] [ ptype PTYPE ]\n" +
		"\t[ if_id IF_ID ]\n" +
		"Usage: ip xfrm policy { deleteall | list } [ nosock ] [ SELECTOR ] [ dir DIR ]\n" +
		"\t[ index INDEX ] [ ptype PTYPE ] [ action ACTION ] [ priority PRIORITY ]\n" +
		"\t[ flag FLAG-LIST ]\n" +
		"Usage: ip xfrm policy flush [ ptype PTYPE ]\n" +
		"Usage: ip xfrm policy count\n" +
		"Usage: ip xfrm policy set [ hthresh4 LBITS RBITS ] [ hthresh6 LBITS RBITS ]\n" +
		"Usage: ip xfrm policy setdefault DIR ACTION [ DIR ACTION ] [ DIR ACTION ]\n" +
		"Usage: ip xfrm policy getdefault\n" +
		"SELECTOR := [ src ADDR[/PLEN] ] [ dst ADDR[/PLEN] ] [ dev DEV ] [ UPSPEC ]\n" +
		"UPSPEC := proto { { tcp | udp | sctp | dccp } [ sport PORT ] [ dport PORT ] |\n" +
		"                  { icmp | ipv6-icmp | mobility-header } [ type NUMBER ] [ code NUMBER ] |\n" +
		"                  gre [ key { DOTTED-QUAD | NUMBER } ] | PROTO }\n" +
		"DIR := in | out | fwd\n" +
		"PTYPE := main | sub\n" +
		"ACTION := allow | block\n" +
		"FLAG-LIST := [ FLAG-LIST ] FLAG\n" +
		"FLAG := localok | icmp\n" +
		"LIMIT-LIST := [ LIMIT-LIST ] limit LIMIT\n" +
		"LIMIT := { time-soft | time-hard | time-use-soft | time-use-hard } SECONDS |\n" +
		"         { byte-soft | byte-hard } SIZE | { packet-soft | packet-hard } COUNT\n" +
		"TMPL-LIST := [ TMPL-LIST ] tmpl TMPL\n" +
		"TMPL := ID [ mode MODE ] [ reqid REQID ] [ level LEVEL ]\n" +
		"ID := [ src ADDR ] [ dst ADDR ] [ proto XFRM-PROTO ] [ spi SPI ]\n" +
		"XFRM-PROTO := esp | ah | comp | route2 | hao\n" +
		"MODE := transport | tunnel | beet | ro | in_trigger\n" +
		"LEVEL := required | use"
)

// xfrmPolicyWrite is a policy write, G7-T04's.
func xfrmPolicyWrite(verb string) func(*runCtx, []string) error {
	return func(*runCtx, []string) error {
		return fmt.Errorf("xfrm policy %s, G7-T04: %w", verb, ErrNotImplemented)
	}
}

// xfrmPolicyVerbs are do_xfrm_policy's verbs, each matched by prefix in
// its order: `d` is delete, `s` show and `g` get.
var xfrmPolicyVerbs = []struct {
	verbs []string
	run   func(*runCtx, []string) error
}{
	{[]string{xfrmAddCst}, xfrmPolicyWrite(xfrmAddCst)},
	{[]string{xfrmUpdateCst}, xfrmPolicyWrite(xfrmUpdateCst)},
	{[]string{fouDeleteCst}, xfrmPolicyWrite(fouDeleteCst)},
	{[]string{xfrmDeleteallCst, "delall"}, xfrmPolicyWrite(xfrmDeleteallCst)},
	{[]string{listVerbCst, showVerbCst, lstVerbCst}, xfrmPolicyList},
	{[]string{nexthopGetVerbCst}, xfrmPolicyGet},
	{[]string{xfrmFlushCst}, xfrmPolicyWrite(xfrmFlushCst)},
	{[]string{xfrmCountCst}, func(c *runCtx, _ []string) error { return xfrmPolicyCount(c) }},
	{[]string{iftokSetVerbCst}, xfrmPolicyWrite(iftokSetVerbCst)},
	{[]string{"setdefault"}, xfrmPolicyWrite("setdefault")},
	{[]string{"getdefault"}, func(c *runCtx, _ []string) error { return xfrmPolicyGetDefault(c) }},
	{[]string{xfrmHelpCst}, func(*runCtx, []string) error {
		return fmt.Errorf("%w: %s", ErrXfrmUsage, xfrmPolicyUsageCst)
	}},
}

// xfrmPolicy is do_xfrm_policy. Of its verbs only count renders
// show_stats > 1, its buckets and thresholds, so goip refuses `-s -s` for
// the others (flush's "Flush policy" line is G7-T04's).
func xfrmPolicy(c *runCtx, args []string) error {
	if len(args) == 0 {
		args = []string{listVerbCst}
	}
	verb, rest := args[0], args[1:]
	for _, v := range xfrmPolicyVerbs {
		if matchesAny(verb, v.verbs) {
			if c.showStats > 1 && v.verbs[0] != xfrmCountCst {
				return fmt.Errorf("-s -s xfrm policy %s: %w", v.verbs[0], ErrNotImplemented)
			}
			return v.run(c, rest)
		}
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip xfrm policy help\"", ErrXfrmUsage, verb)
}

// xfrmPolicyFilter is struct xfrm_filter's policy half: the use flag, the
// selector the arguments describe, whose family and masks it reads, and
// each other field with whether it was given.
type xfrmPolicyFilter struct {
	use                   bool
	sel                   xfrmSel
	dir, action, flags    uint8
	ptype                 uint8
	index, priority       uint32
	dirSet, actionSet     bool
	flagsSet, ptypeSet    bool
	indexSet, prioritySet bool
	socket                bool
}

// match is xfrm_policy_filter_match.
func (f *xfrmPolicyFilter) match(x *xtcpnl.XfrmUserpolicyInfo, ptype uint8) bool {
	s := &f.sel
	switch {
	case !f.use:
		return true
	case s.family != unix.AF_UNSPEC && s.family != x.Sel.Family,
		f.dirSet && x.Dir != f.dir,
		f.socket && x.Dir >= xfrmPolicyMaxCst,
		f.ptypeSet && ptype != f.ptype,
		s.srcMask != 0 && !xfrmAddrMatch(&x.Sel.Saddr, &s.saddr, s.srcMask),
		s.dstMask != 0 && !xfrmAddrMatch(&x.Sel.Daddr, &s.daddr, s.dstMask),
		s.devSet && x.Sel.Ifindex != s.ifindex,
		s.protoSet && x.Sel.Proto != s.proto,
		s.sportSet && x.Sel.Sport != s.sport,
		s.dportSet && x.Sel.Dport != s.dport,
		f.indexSet && x.Index != f.index,
		f.actionSet && x.Action != f.action,
		f.prioritySet && x.Priority != f.priority,
		f.flagsSet && x.Flags&f.flags == 0:
		return false
	}
	return true
}

// xfrmPolicyList is xfrm_policy_list_or_deleteall's list: its filter's
// words, then the GETPOLICY dump, with no header, each answer printed
// through the filter.
func xfrmPolicyList(c *runCtx, args []string) error {
	f, err := xfrmPolicyListArgs(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetPolicy, unix.NLM_F_DUMP, c.nextSeq(), nil, nil)
	if err != nil {
		return err
	}
	msgs, err := xs.XfrmDump(req)
	for i := 0; err == nil && i < len(msgs); i++ {
		err = xfrmPolicyPrint(c, &f, msgs[i])
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrXfrmDumpTerminated, err)
	}
	return nil
}

// xfrmPolicyListArgs is the list's loop: dir, index, ptype, action,
// priority and flag by strcmp, each with its value, nosock, else one
// selector. Any word, or a family, turns the filter on; the family is
// preferred_family's until a selector gives its own.
func xfrmPolicyListArgs(c *runCtx, args []string) (xfrmPolicyFilter, error) {
	f := xfrmPolicyFilter{use: len(args) > 0 || c.family != unix.AF_UNSPEC}
	f.sel.family = uint16(c.family)
	selSeen := false
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		var err error
		switch a.cur() {
		case xfrmDirCst:
			f.dirSet = true
			err = xfrmPolicyNextDir(a, &f.dir)
		case nhIndexKeywordCst:
			f.indexSet = true
			err = xfrmPolicyNextU32(a, &f.index, "INDEX")
		case xfrmPtypeCst:
			f.ptypeSet = true
			err = xfrmPolicyNextPtype(a, &f.ptype)
		case xfrmActionCst:
			f.actionSet = true
			err = xfrmPolicyNextAction(a, &f.action)
		case xfrmPriorityCst:
			f.prioritySet = true
			err = xfrmPolicyNextU32(a, &f.priority, "PRIORITY")
		case xfrmFlagCst:
			f.flagsSet = true
			if err = a.next(); err == nil {
				err = xfrmPolicyFlagParse(a, &f.flags)
			}
		case "nosock":
			f.socket = true
		default:
			if selSeen {
				return f, xfrmUsage(invargErr("unknown", a.cur()))
			}
			selSeen = true
			if err = xfrmSelectorParse(c, a, &f.sel); err == nil && c.family == unix.AF_UNSPEC {
				c.family = uint8(f.sel.family)
			}
		}
		if err != nil {
			return f, err
		}
	}
	return f, nil
}

// xfrmPolicyNextDir is dir's value, xfrm_policy_dir_parse's, by strcmp.
func xfrmPolicyNextDir(a *xfrmArgs, dir *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	for i, d := range []string{"in", "out", "fwd"} {
		if a.cur() == d {
			*dir = uint8(i)
			return nil
		}
	}
	return xfrmUsage(invargErr("DIR value is invalid", a.cur()))
}

// xfrmPolicyNextPtype is ptype's value, xfrm_policy_ptype_parse's: main or
// sub, by strcmp.
func xfrmPolicyNextPtype(a *xfrmArgs, ptype *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	switch a.cur() {
	case xfrmMainCst:
		*ptype = 0
	case "sub":
		*ptype = 1
	default:
		return xfrmUsage(invargErr("PTYPE value is invalid", a.cur()))
	}
	return nil
}

// xfrmPolicyNextAction is the list's action value: allow or block, by
// strcmp; ip's message ends in a newline of its own.
func xfrmPolicyNextAction(a *xfrmArgs, action *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	switch a.cur() {
	case "allow":
		*action = 0
	case "block":
		*action = 1
	default:
		return xfrmUsage(invargErr("ACTION value is invalid\n", a.cur()))
	}
	return nil
}

// xfrmPolicyNextU32 is a get_u32 base 0 value, refused as what's.
func xfrmPolicyNextU32(a *xfrmArgs, v *uint32, what string) error {
	if err := a.next(); err != nil {
		return err
	}
	n, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr(what+" value is invalid", a.cur()))
	}
	*v = n
	return nil
}

// xfrmPolicyFlagParse is xfrm_policy_flag_parse: a 0x value, get_u8 base
// 16, which sets the flags, or a run of localok and icmp, each ORed in,
// until another word, left as PREV_ARG leaves it.
func xfrmPolicyFlagParse(a *xfrmArgs, flags *uint8) error {
	if v := a.cur(); len(v) > 2 && v[:2] == "0x" {
		n, ok := getU8(v, 16)
		if !ok {
			return xfrmUsage(invargErr("FLAG value is invalid", v))
		}
		*flags = n
		return nil
	}
	for {
		switch a.cur() {
		case "localok":
			*flags |= 1
		case xfrmIcmpCst:
			*flags |= 2
		default:
			a.i--
			return nil
		}
		if !a.nextOK() {
			return nil
		}
		a.i++
	}
}

// xfrmPolicyPrint is xfrm_policy_print for a NEWPOLICY or UPDPOLICY, the
// types a GETPOLICY answers with: another type is "Not a policy" on stderr
// and skipped, and the monitor's DELPOLICY and POLEXPIRE are G7-T05's; a
// body short of its struct, or a policy type short of its, refuses the
// message; a policy the filter does not match is skipped; else "Updated "
// for an UPDPOLICY and xfrm_policy_info_print.
func xfrmPolicyPrint(c *runCtx, f *xfrmPolicyFilter, m xfrmMsg) error {
	switch m.typ {
	case xtcpnl.XfrmMsgNewPolicy, xtcpnl.XfrmMsgUpdPolicy:
	case xtcpnl.XfrmMsgDelPolicy, xtcpnl.XfrmMsgPolExpire:
		return fmt.Errorf("xfrm policy event %d, G7-T05: %w", m.typ, ErrNotImplemented)
	default:
		_, err := fmt.Fprintf(c.errOut, "Not a policy: %08x %08x %08x\n", xtcpnl.NlMsgHdrSizeCst+len(m.body),
			m.typ, m.flags)
		return err
	}
	if len(m.body) < xtcpnl.XfrmUserpolicyInfoSizeCst {
		return fmt.Errorf("BUG: wrong nlmsg len %d", len(m.body)-xtcpnl.XfrmUserpolicyInfoSizeCst)
	}
	p, err := xtcpnl.ParseXfrmPolicy(xtcpnl.NlMsgHdr{Type: m.typ, Flags: m.flags}, m.body)
	if err != nil {
		return err
	}
	var ptype uint8
	if p.Attrs.PolicyType != nil {
		ptype = *p.Attrs.PolicyType
	}
	if !f.match(p.Info, ptype) {
		return nil
	}
	o := render.XfrmOpt{Stats: c.stats(), Family: c.family,
		IndexName: func(idx int32) string { return genlIndexToName(c, idx) }}
	if err := xfrmProtoOpt(&o, p.Info.Sel.Proto); err != nil {
		return err
	}
	out := render.XfrmPolicyInfo(p.Info, &p.Attrs, &o)
	if m.typ == xtcpnl.XfrmMsgUpdPolicy {
		out = "Updated " + out
	}
	_, err = io.WriteString(c.out, out)
	return err
}

// xfrmPolicyIDRequest is xfrm_policy_get_or_delete's loop and request:
// dir, ctx, index and ptype, once each, mark and if_id, else one selector;
// then DIR, then a selector or an index, not both, are required. The
// request is struct xfrm_userpolicy_id, AF_INET when nothing gave a
// family, then XFRMA_POLICY_TYPE when ptype was given, the mark when both
// its halves are set, the context and the interface id. get sends it as a
// GETPOLICY.
func xfrmPolicyIDRequest(c *runCtx, args []string) (hdr, attrs []byte, err error) {
	var q xfrmPolicyID
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		if err := q.word(c, a); err != nil {
			return nil, nil, err
		}
	}
	return q.request()
}

// xfrmPolicyID is what xfrm_policy_get_or_delete's loop reads: the words
// seen, by the word ip's duplicate checks keep, and their values.
type xfrmPolicyID struct {
	dirp, selp, indexp, ptypep, sctxp string
	sel                               xfrmSel
	index                             uint32
	dir, ptype                        uint8
	ctx                               []byte
	mark                              [2]uint32
	ifID                              uint32
	ifIDSet                           bool
}

// word reads one of the loop's words and its value.
func (q *xfrmPolicyID) word(c *runCtx, a *xfrmArgs) error {
	switch w := a.cur(); w {
	case xfrmDirCst:
		if q.dirp != "" {
			return xfrmUsage(dupargErr(w, w))
		}
		q.dirp = w
		return xfrmPolicyNextDir(a, &q.dir)
	case xfrmCtxCst:
		if q.sctxp != "" {
			return xfrmUsage(dupargErr(w, w))
		}
		q.sctxp = w
		if err := a.next(); err != nil {
			return err
		}
		if len(a.cur())+1 > xfrmCtxBufCst {
			return fmt.Errorf("a ctx of %d bytes, which overruns ip's %d-byte buffer: %w", len(a.cur())+1,
				xfrmCtxBufCst, ErrNotImplemented)
		}
		q.ctx = xfrmSecCtxBytes(a.cur())
	case xfrmMarkCst:
		return xfrmParseMark(a, &q.mark)
	case nhIndexKeywordCst:
		if q.indexp != "" {
			return xfrmUsage(dupargErr(w, w))
		}
		q.indexp = w
		return xfrmPolicyNextU32(a, &q.index, "INDEX")
	case xfrmPtypeCst:
		if q.ptypep != "" {
			return xfrmUsage(dupargErr(w, w))
		}
		q.ptypep = w
		return xfrmPolicyNextPtype(a, &q.ptype)
	case xfrmIfIDCst:
		q.ifIDSet = true
		return xfrmPolicyNextU32(a, &q.ifID, "IF_ID")
	default:
		if q.selp != "" {
			return xfrmUsage(invargErr("unknown", w))
		}
		q.selp = w
		if err := xfrmSelectorParse(c, a, &q.sel); err != nil {
			return err
		}
		if c.family == unix.AF_UNSPEC {
			c.family = uint8(q.sel.family)
		}
	}
	return nil
}

// request is the checks after the loop and the request they leave.
func (q *xfrmPolicyID) request() (hdr, attrs []byte, err error) {
	if q.dirp == "" {
		return nil, nil, fmt.Errorf("%w: Not enough information: DIR is required", ErrXfrmRefused)
	}
	if q.selp == "" && q.indexp == "" {
		return nil, nil, fmt.Errorf("%w: Not enough information: either SELECTOR or INDEX is required",
			ErrXfrmRefused)
	}
	if q.selp != "" && q.indexp != "" {
		return nil, nil, xfrmUsage(duparg2Err("SELECTOR", "INDEX"))
	}
	if q.sel.family == unix.AF_UNSPEC {
		q.sel.family = unix.AF_INET
	}
	hdr = q.sel.bytes()
	hdr = binary.LittleEndian.AppendUint32(hdr, q.index)
	hdr = append(hdr, q.dir, 0, 0, 0)
	var raw [512]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	if q.ptypep != "" {
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrPolicyType, []byte{q.ptype, 0, 0, 0}))
	}
	if q.mark[0]&q.mark[1] != 0 {
		v := binary.LittleEndian.AppendUint32(nil, q.mark[0])
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrMark, binary.LittleEndian.AppendUint32(v, q.mark[1])))
	}
	if q.ctx != nil {
		errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrSecCtx, q.ctx))
	}
	if q.ifIDSet {
		errs = append(errs, b.PutU32(xtcpnl.XfrmAttrIfID, q.ifID))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
	}
	return hdr, b.Bytes(), nil
}

// xfrmPolicyGet is xfrm_policy_get: the GETPOLICY, its answer printed
// unfiltered; a message the printer refuses is "An error :-)", exit 1.
func xfrmPolicyGet(c *runCtx, args []string) error {
	hdr, attrs, err := xfrmPolicyIDRequest(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetPolicy, 0, c.nextSeq(), hdr, attrs)
	if err != nil {
		return err
	}
	typ, body, err := xs.XfrmTalk(req)
	if err != nil {
		return err
	}
	if err := xfrmPolicyPrint(c, &xfrmPolicyFilter{}, xfrmMsg{typ: typ, body: body}); err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return err
		}
		return fmt.Errorf("%w: An error :-): %w", ErrXfrmRefused, err)
	}
	return nil
}

// xfrmPolicyCount is xfrm_spd_getinfo: one GETSPDINFO asking for every
// field, then print_spdinfo. Any word after count is unread.
func xfrmPolicyCount(c *runCtx) error {
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetSPDInfo, 0, c.nextSeq(),
		binary.LittleEndian.AppendUint32(nil, xfrmMaskAllCst), nil)
	if err != nil {
		return err
	}
	typ, body, err := xs.XfrmTalk(req)
	if err != nil {
		return err
	}
	info, err := xtcpnl.ParseXfrmInfo(xtcpnl.NlMsgHdr{Type: typ}, body)
	if err != nil {
		return fmt.Errorf("%w: SPDinfo: %w", ErrXfrmRefused, err)
	}
	_, err = io.WriteString(c.out, render.XfrmSPDInfo(&info, c.showStats))
	return err
}

// xfrmPolicyGetDefault is xfrm_spd_getdefault: one GETDEFAULT, its struct
// zero, then xfrm_policy_default_print; an answer short of its struct is a
// BUG line on stderr, exit 0. Any word after getdefault is unread.
func xfrmPolicyGetDefault(c *runCtx) error {
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetDefault, 0, c.nextSeq(),
		make([]byte, xtcpnl.XfrmUserpolicyDefaultSizeCst), nil)
	if err != nil {
		return err
	}
	_, body, err := xs.XfrmTalk(req)
	if err != nil {
		return err
	}
	if n := xtcpnl.NlMsgHdrSizeCst + len(body); n < xfrmDefaultMinCst {
		_, err := fmt.Fprintf(c.errOut, "BUG: short nlmsg len %d (expect %d) for XFRM_MSG_GETDEFAULT\n", n,
			xfrmDefaultMinCst)
		return err
	}
	d := xtcpnl.XfrmUserpolicyDefault{In: body[0], Fwd: body[1], Out: body[2]}
	_, err = io.WriteString(c.out, render.XfrmPolicyDefault(&d))
	return err
}
