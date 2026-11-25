package gotc

import (
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/m_action.c tc_print_action (tc_dump_action with bind set),
// tc_print_one_action and print_hw_stats; tc/m_gact.c print_gact;
// tc/tc_util.c print_action_control and print_tm.

// The tc action verdicts (pkt_cls.h TC_ACT_*).
const (
	tcActUnspec     = -1
	tcActOK         = 0
	tcActReclassify = 1
	tcActShot       = 2
	tcActPipe       = 3
	tcActStolen     = 4
	tcActTrap       = 8

	tcActExtValMask = 1<<28 - 1 // TC_ACT_EXT_VAL_MASK
	tcActJump       = 1 << 28   // TC_ACT_JUMP
	tcActGotoChain  = 2 << 28   // TC_ACT_GOTO_CHAIN

	// userHZ is __get_user_hz: sysconf(_SC_CLK_TCK), USER_HZ, 100 on
	// Linux whatever the kernel's HZ. tcf_t is in its ticks.
	userHZ = 100
)

// printActions is tc_print_action(f, arg, 0): a filter's actions, bound
// (bind 1), each by order, 0..TCA_ACT_MAX_PRIO.
func (r *runner) printActions(nest []byte) error {
	return r.dumpAction(nest, 0, true)
}

// actTabMaxCst bounds tc_dump_action's VLA: tot_acts is the reply's
// TCA_ROOT_COUNT, a u16 tc trusts; past the attribute table's bound gotc
// refuses.
const actTabMaxCst = 1024

// dumpAction is tc_dump_action: tot of 0 is TCA_ACT_MAX_PRIO, and the
// table holds orders 0..tot. The flush-reply path (tab_flush) follows
// only RTM_DELACTION, which gotc does not send.
func (r *runner) dumpAction(nest []byte, tot uint16, bind bool) error {
	o := r.o
	if tot == 0 {
		tot = xtcpnl.TcaActMaxPrioCst
	}
	if tot > actTabMaxCst {
		return fmt.Errorf("%w: an action table of %d orders", errNotImplemented, tot)
	}
	tb := r.nested(nest, tot)
	o.openArray(printJSON, "actions")
	for i := range tot + 1 {
		if !tb.Has(i) {
			continue
		}
		o.openObject("")
		o.nl()
		o.uint(printAny, "order", "\taction order "+strconv.Itoa(int(i))+": ", uint32(i))
		rc, err := r.printOneAction(tb.Bytes(i), bind)
		if err != nil {
			return err
		}
		if rc < 0 {
			r.errf("Error printing action\n")
		}
		o.closeObject()
	}
	o.closeArray(printJSON, "")
	return nil
}

// actPrinter is an action_util's print_aopt: its options (nil when
// TCA_ACT_OPTIONS is absent, has false) and tc's return.
type actPrinter func(r *runner, opt []byte, has bool) (int, error)

// actionUtils is the action_util list tc 7.2.0 links, by id, for the kinds
// gotc prints. The other linked kinds (deferredActKinds) are refused by
// name; tc prints a kind it does not link as gact (L22), which gotc
// refuses too.
var actionUtils = map[string]actPrinter{
	xtcpnl.TcActKindGact:     (*runner).printGact,
	xtcpnl.TcActKindPolice:   (*runner).printPolice,
	xtcpnl.TcActKindMirred:   (*runner).printMirred,
	xtcpnl.TcActKindSkbedit:  (*runner).printSkbedit,
	xtcpnl.TcActKindVlan:     (*runner).printVlan,
	xtcpnl.TcActKindCsum:     (*runner).printCsum,
	xtcpnl.TcActKindNat:      (*runner).printNat,
	xtcpnl.TcActKindSimple:   (*runner).printSimple,
	xtcpnl.TcActKindConnmark: (*runner).printConnmark,
	xtcpnl.TcActKindCtinfo:   (*runner).printCtinfo,
	xtcpnl.TcActKindSample:   (*runner).printSample,
	xtcpnl.TcActKindSkbmod:   (*runner).printSkbmod,
	xtcpnl.TcActKindMpls:     (*runner).printMpls,
}

// deferredActKinds are the action_util ids tc 7.2.0 links that gotc does
// not print yet (increment 13): a kind moves from here to actionUtils.
var deferredActKinds = []string{"bpf", "ct", "gate", "ife", "pedit", "tunnel_key"}

// linkedActKinds is every action_util id tc 7.2.0 links (TCMODULES), the
// kinds get_action_kind finds by name: get and list refuse any other. It
// is actionUtils' kinds plus deferredActKinds.
var linkedActKinds = func() map[string]bool {
	m := make(map[string]bool, len(actionUtils)+len(deferredActKinds))
	for k := range actionUtils {
		m[k] = true
	}
	for _, k := range deferredActKinds {
		m[k] = true
	}
	return m
}()

// printOneAction is tc_print_one_action, for both callers: bind is a
// filter's action (tc actions passes it unbound), and only unbound
// actions print skip_hw, skip_sw and in_hw (printActFlags).
func (r *runner) printOneAction(nest []byte, bind bool) (int, error) {
	o := r.o
	tb := r.nested(nest, xtcpnl.TcaActMaxKnown)
	if !tb.Has(xtcpnl.TcaActKind) {
		r.errf("NULL Action!\n")
		return -1, nil
	}
	kind, _ := tb.Str(xtcpnl.TcaActKind)
	pr, ok := actionUtils[kind]
	if !ok {
		return 0, fmt.Errorf("%w: action kind %q", errNotImplemented, kind)
	}
	if rc, err := pr(r, tb.Bytes(xtcpnl.TcaActOptions), tb.Has(xtcpnl.TcaActOptions)); err != nil || rc < 0 {
		return rc, err
	}
	if r.brief > 0 && tb.Has(xtcpnl.TcaActIndex) {
		v := attrU32(tb, xtcpnl.TcaActIndex)
		o.uint(printAny, "index", "\t index "+u32s(v), v)
		o.nl()
	}
	if r.stats > 0 && tb.Has(xtcpnl.TcaActStats) {
		o.fp("\tAction statistics:")
		o.nl()
		o.openObject("stats")
		s, err := xtcpnl.ParseTcStats2(tb.Bytes(xtcpnl.TcaActStats))
		if err != nil {
			return 0, err
		}
		r.tcStats2(&s, "\t")
		o.closeObject()
		o.nl()
	}
	if tb.Has(xtcpnl.TcaActCookie) {
		c := fmt.Sprintf("%x", tb.Bytes(xtcpnl.TcaActCookie))
		o.str(printAny, colorNone, "cookie", "\tcookie "+c, c)
		o.nl()
	}
	if tb.Has(xtcpnl.TcaActFlags) || tb.Has(xtcpnl.TcaActInHwCount) {
		r.printActFlags(tb, bind)
	}
	if tb.Has(xtcpnl.TcaActHwStats) {
		r.printHwStats(tb.Bytes(xtcpnl.TcaActHwStats), false)
	}
	if tb.Has(xtcpnl.TcaActUsedHwStats) {
		r.printHwStats(tb.Bytes(xtcpnl.TcaActUsedHwStats), true)
	}
	return 0, nil
}

// printActFlags is tc_print_one_action's flags block: no_percpu on its
// selector bit alone (F20); unbound, skip_hw, skip_sw, and in_hw (with its
// count) or not_in_hw unless skip_hw is set. A newline only if any
// printed.
func (r *runner) printActFlags(tb xtcpnl.AttrTable, bind bool) {
	o := r.o
	skipHw, newline := false, false
	if tb.Has(xtcpnl.TcaActFlags) {
		fl := xtcpnl.DecodeNlaBitfield32(tb.Bytes(xtcpnl.TcaActFlags))
		for _, f := range []struct {
			bit      uint32
			key      string
			unbound  bool
			isSkipHw bool
		}{
			{xtcpnl.TcaActFlagsNoPercpuStats, "no_percpu", false, false},
			{xtcpnl.TcaActFlagsSkipHw, "skip_hw", true, true},
			{xtcpnl.TcaActFlagsSkipSw, "skip_sw", true, false},
		} {
			if (f.unbound && bind) || fl.Selector&f.bit == 0 {
				continue
			}
			newline = true
			o.boolean(printAny, f.key, "\t"+f.key, fl.Value&f.bit != 0)
			if f.isSkipHw {
				skipHw = fl.Value&f.bit != 0
			}
		}
	}
	if tb.Has(xtcpnl.TcaActInHwCount) && !bind && !skipHw {
		newline = true
		if n := attrU32(tb, xtcpnl.TcaActInHwCount); n != 0 {
			o.boolean(printAny, "in_hw", "\tin_hw", true)
			o.uint(printAny, "in_hw_count", " in_hw_count "+u32s(n), n)
		} else {
			o.boolean(printAny, "not_in_hw", "\tnot_in_hw", true)
		}
	}
	if newline {
		o.nl()
	}
}

// printHwStats is print_hw_stats; tc casts the bitfield unchecked (L28)
// and keeps its low byte.
func (r *runner) printHwStats(b []byte, used bool) {
	o := r.o
	bf := xtcpnl.DecodeNlaBitfield32(b)
	hw := bf.Value & bf.Selector & 0xFF
	o.fp("\t")
	name := "hw_stats"
	if used {
		name = "used_hw_stats"
	}
	o.openArray(printAny, name)
	for _, it := range []struct {
		name string
		typ  uint32
	}{{"immediate", xtcpnl.TcaActHwStatsImmediate}, {"delayed", xtcpnl.TcaActHwStatsDelayed}, {"disabled", 0}} {
		if (hw == 0 && it.typ == 0) || hw&it.typ != 0 {
			o.str(printAny, colorNone, "", " "+it.name, it.name)
		}
	}
	o.closeArray(printJSON, "")
	o.nl()
}

// printGact is print_gact: without TCA_GACT_PROB a zeroed tc_gact_p
// prints all the same (F19); PARMS, PROB and TM are MIN-copied (L28).
func (r *runner) printGact(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "gact ", "gact")
	if !has {
		return 0, nil
	}
	g, err := xtcpnl.ParseTcGact(opt)
	if err != nil {
		r.nested(opt, xtcpnl.TcaGactMaxKnown) // its "!!!Deficit" line
	}
	if !g.HasParms {
		r.errf("Missing gact parameters\n")
		return -1, nil
	}
	r.printActionControl("action ", g.Parms.Action, "")
	o.openObject("prob")
	o.nl()
	pt := probN2A(g.Prob.Ptype)
	o.str(printAny, colorNone, "random_type", "\t random type "+pt, pt)
	r.printActionControl(" ", g.Prob.Paction, " ")
	pv := int32(g.Prob.Pval)
	o.int(printAny, "val", "val "+i32s(pv), pv)
	o.closeObject()
	o.nl()
	o.uint(printAny, "index", "\t index "+u32s(g.Parms.Index), g.Parms.Index)
	o.int(printAny, "ref", " ref "+i32s(g.Parms.Refcnt), g.Parms.Refcnt)
	o.int(printAny, "bind", " bind "+i32s(g.Parms.Bindcnt), g.Parms.Bindcnt)
	if r.stats > 0 && g.HasTm {
		r.printTm(g.Tm)
	}
	o.nl()
	return 0, nil
}

// probN2A is m_gact.c prob_n2a.
func probN2A(p uint16) string {
	switch p {
	case 1:
		return "netrand"
	case 2:
		return "determ"
	}
	return "none"
}

// actionN2A is tc_util.c action_n2a.
func actionN2A(a int32) string {
	switch a &^ tcActExtValMask {
	case tcActGotoChain:
		return "goto"
	case tcActJump:
		return "jump"
	}
	switch a {
	case tcActUnspec:
		return "continue"
	case tcActOK:
		return "pass"
	case tcActShot:
		return "drop"
	case tcActReclassify:
		return "reclassify"
	case tcActPipe:
		return "pipe"
	case tcActStolen:
		return "stolen"
	case tcActTrap:
		return "trap"
	}
	return i32s(a)
}

// printActionControl is print_action_control.
func (r *runner) printActionControl(prefix string, a int32, suffix string) {
	o := r.o
	o.fp(prefix)
	o.openObject("control_action")
	t := actionN2A(a)
	o.str(printAny, colorNone, "type", t, t)
	v := cint.Bits(a) & tcActExtValMask
	if a&^tcActExtValMask == tcActGotoChain {
		o.uint(printAny, "chain", " chain "+u32s(v), v)
	}
	if a&^tcActExtValMask == tcActJump {
		o.uint(printAny, "jump", " "+u32s(v), v)
	}
	o.closeObject()
	o.fp(suffix)
}

// printTm is print_tm: each non-zero time over USER_HZ, through
// print_uint, which narrows the __u64 (F24).
func (r *runner) printTm(tm xtcpnl.TcfT) {
	for _, f := range []struct {
		key, word string
		v         uint64
	}{
		{"installed", " installed ", tm.Install},
		{"last_used", " used ", tm.Lastuse},
		{"first_used", " firstused ", tm.Firstuse},
		{"expires", " expires ", tm.Expires},
	} {
		if f.v != 0 {
			s := cint.Low32(f.v / userHZ)
			r.o.uint(printAny, f.key, f.word+u32s(s)+" sec", s)
		}
	}
}

// rtRealmTab is lib/rt_names.c rtnl_rtrealm_tab: realm 0 is "unknown"
// until the file is read.
type rtRealmTab struct {
	names  [256]string
	inited bool
}

// rtrealmN2A is rtnl_rtrealm_n2a: a negative id, one past 255, or -N is
// the number; a missing name reads the database once (CONF_ETC_DIR's
// rt_realms, else CONF_USR_DIR's).
func (r *runner) rtrealmN2A(id int32) string {
	if id < 0 || id >= 256 || r.numeric > 0 {
		return i32s(id)
	}
	t := &r.realms
	if !t.inited && t.names[0] == "" {
		t.names[0] = "unknown"
	}
	if t.names[id] == "" && !t.inited {
		t.inited = true
		if r.realmTabInit(confEtcDirCst + "/rt_realms") {
			r.realmTabInit(r.sys.usrDir + "/rt_realms")
		}
	}
	return realmName(t, id)
}

func realmName(t *rtRealmTab, id int32) string {
	if t.names[id] != "" {
		return t.names[id]
	}
	return i32s(id)
}

// realmTabInit is rtnl_tab_initialize(file, tab, 256): fread_id_name's
// "0x%x %s" or "%d %s" per line; a corrupt line ends it with its report.
// An id of 256 is written one past tc's table; here it is dropped. noent
// is fopen's ENOENT, on which tc tries CONF_USR_DIR.
func (r *runner) realmTabInit(path string) (noent bool) {
	content, err := r.sys.readFile(path)
	if err != nil {
		return true
	}
	for _, line := range fgets(content) {
		p := stripLeadBlank(cstr(line))
		if p == "" || p[0] == '#' || p[0] == '\n' {
			continue
		}
		id, name, ok := freadIDName(p)
		if !ok {
			r.errf("Database %s is corrupted at %s\n", path, p)
			return false
		}
		if id >= 0 && id < 256 {
			r.realms.names[id] = name
		}
	}
	return false
}

func stripLeadBlank(s string) string {
	for s != "" && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// freadIDName is one line of fread_id_name: "0x%x %s", else "%d %s".
func freadIDName(p string) (int32, string, bool) {
	if len(p) > 2 && p[0] == '0' && p[1] == 'x' {
		if v, rest, ok := scanHex(p[2:], 0); ok {
			if n, _, ok := scanStr(rest); ok {
				return cint.Int(v), n, true
			}
		}
	}
	if v, rest, ok := scanDec(p); ok {
		if n, _, ok := scanStr(rest); ok {
			return v, n, true
		}
	}
	return 0, "", false
}

// talkAnswer is rtnl_talk with an answer: the reply's first message, or,
// for an NLMSG_ERROR, its report and false (a non-zero error), or the
// ack itself as the answer.
func (r *runner) talkAnswer(req []byte, berr error) (Msg, bool, error) {
	rep, err := r.talk(req, berr)
	if err != nil {
		return Msg{}, false, err
	}
	if len(rep.Msgs) == 0 {
		return Msg{}, false, fmt.Errorf("%w: a request's reply has no message", ErrReplay)
	}
	m := rep.Msgs[0]
	if m.Type != unix.NLMSG_ERROR {
		return m, true, nil
	}
	if len(m.Body) < 4+nlHeaderLen {
		r.errf("ERROR truncated\n")
		return Msg{}, false, nil
	}
	e := cint.Int(binary.LittleEndian.Uint32(m.Body[0:4]))
	if e == 0 {
		r.dumpExtAck(m, false)
		return m, true, nil
	}
	if !r.dumpExtAck(m, true) {
		r.errf("RTNETLINK answers: %s\n", strerror(unix.Errno(-e)))
	}
	return Msg{}, false, nil
}

// nlmsgerrAttrMissTypeCst is NLMSGERR_ATTR_MISS_TYPE, which x/sys/unix
// lacks.
const nlmsgerrAttrMissTypeCst = 5

// dumpExtAck is nl_dump_ext_ack with no errfn: only with NLM_F_ACK_TLVS,
// the TLVs past struct nlmsgerr (and past the echoed request unless
// NLM_F_CAPPED). It reports whether an error message was printed.
func (r *runner) dumpExtAck(m Msg, isErr bool) bool {
	if m.Flags&unix.NLM_F_ACK_TLVS == 0 {
		return false
	}
	hlen := 4 + nlHeaderLen
	if m.Flags&unix.NLM_F_CAPPED == 0 {
		inner := int(binary.LittleEndian.Uint32(m.Body[4:8]))
		hlen += max(inner-nlHeaderLen, 0)
	}
	if hlen > len(m.Body) {
		return false
	}
	t, err := xtcpnl.ParseAttrTable(m.Body[hlen:], nlmsgerrAttrMaxCst)
	if err != nil {
		return false
	}
	if v, ok := t.U32(nlmsgerrAttrMissTypeCst); ok {
		r.errf("Missing required attribute type %d\n", v)
	}
	msg, _ := t.Str(unix.NLMSGERR_ATTR_MSG)
	if msg == "" {
		return false
	}
	r.extack(isErr, msg)
	return isErr
}
