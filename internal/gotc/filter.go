package gotc

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/tc_filter.c: do_filter, do_chain, tc_filter_list, tc_filter_get and
// print_filter, with each slice kind's parse_fopt as far as `get ... KIND
// handle H` reaches it (iproute2 v7.2.0).

// filterNameSize is FILTER_NAMESZ: the kind word is strncpy'd into it.
const filterNameSize = 16

// The clsact parents (TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_*)).
const (
	tcHClsactIngress uint32 = 0xFFFFFFF2
	tcHClsactEgress  uint32 = 0xFFFFFFF3
)

// filterFilter is tc_filter.c's statics filter_parent, filter_ifindex,
// filter_prio, filter_protocol, filter_chain_index(_set) and
// filter_block_index. tc never resets them, so a batch line inherits the
// last one's, and a second `chain` on a later line is a duparg (Q29f).
// protocol is the htons'd value ll_proto_a2n gives.
type filterFilter struct {
	parent     uint32
	ifindex    uint32
	prio       uint32
	protocol   uint16
	chain      uint32
	chainSet   bool
	blockIndex uint32
}

// doFilter is do_filter.
func (r *runner) doFilter(args []string) error {
	if len(args) == 0 {
		return r.filterList(unix.RTM_GETTFILTER, nil)
	}
	switch w := args[0]; {
	case writeVerb(w, "add", "change", "replace", "delete"):
		return fmt.Errorf("%w: tc filter %s", errNotImplemented, w)
	case matches(w, "get"):
		return r.filterGet(unix.RTM_GETTFILTER, args[1:])
	case listWords(w):
		return r.filterList(unix.RTM_GETTFILTER, args[1:])
	case matches(w, helpWord):
		r.filterUsage()
		return nil
	}
	r.errf("Command \"%s\" is unknown, try \"tc filter help\".\n", args[0])
	return retCode(-1)
}

// doChain is do_chain.
func (r *runner) doChain(args []string) error {
	if len(args) == 0 {
		return r.filterList(unix.RTM_GETCHAIN, nil)
	}
	switch w := args[0]; {
	case writeVerb(w, "add", "delete"):
		return fmt.Errorf("%w: tc chain %s", errNotImplemented, w)
	case matches(w, "get"):
		return r.filterGet(unix.RTM_GETCHAIN, args[1:])
	case listWords(w):
		return r.filterList(unix.RTM_GETCHAIN, args[1:])
	case matches(w, helpWord):
		r.errf("%s", "Usage: tc chain [ add | del | get | show ] [ dev STRING ]\n"+
			"       tc chain [ add | del | get | show ] [ block BLOCK_INDEX ] ]\n")
		return nil
	}
	r.errf("Command \"%s\" is unknown, try \"tc chain help\".\n", args[0])
	return retCode(-1)
}

// filterUsage is tc_filter.c usage.
func (r *runner) filterUsage() {
	r.errf("%s", "Usage: tc filter [ add | del | change | replace | show ] [ dev STRING ]\n"+
		"       tc filter [ add | del | change | replace | show ] [ block BLOCK_INDEX ]\n"+
		"       tc filter get dev STRING parent CLASSID protocol PROTO handle FILTERID pref PRIO FILTER_TYPE\n"+
		"       tc filter get block BLOCK_INDEX protocol PROTO handle FILTERID pref PRIO FILTER_TYPE\n"+
		"       [ pref PRIO ] protocol PROTO [ chain CHAIN_INDEX ]\n"+
		"       [ estimator INTERVAL TIME_CONSTANT ]\n"+
		"       [ root | ingress | egress | parent CLASSID ]\n"+
		"       [ handle FILTERID ] [ [ FILTER_TYPE ] [ help | OPTIONS ] ]\n"+
		"\n"+
		"       tc filter show [ dev STRING ] [ root | ingress | egress | parent CLASSID ]\n"+
		"       tc filter show [ block BLOCK_INDEX ]\n"+
		"Where:\n"+
		"FILTER_TYPE := { u32 | bpf | fw | route | etc. }\n"+
		"FILTERID := ... format depends on classifier, see there\n"+
		"OPTIONS := ... try tc filter add <desired FILTER_KIND> help\n")
}

// filterArgs is what tc_filter_list's and tc_filter_get's loops share:
// the request's tcmsg fields and the words seen.
type filterArgs struct {
	dev        string
	blockIndex uint32
	parent     uint32
	handle     string
	hasHandle  bool
	prio       uint32
	protocol   uint16
	protoSet   bool
	chain      uint32
	chainSet   bool
}

// parentWord is root, ingress or egress: the parent and the word's
// duplicate message.
func parentWord(a string) (uint32, bool) {
	switch a {
	case wordRoot:
		return tcHRoot, true
	case wordIngress:
		return tcHClsactIngress, true
	case "egress":
		return tcHClsactEgress, true
	}
	return 0, false
}

// filterWord is one word both loops share: dev, block, root, ingress,
// egress, parent, handle, pref, protocol and chain, as tc_filter_list (get
// false) or tc_filter_get (get true) parses it. handled is false for a
// word that is neither's; rest is what follows the word and its value.
func (r *runner) filterWord(a *filterArgs, args []string, get bool) (rest []string, handled bool, err error) {
	w, f := args[0], &r.ff
	if w == wordRoot || w == wordIngress || w == "egress" {
		if a.parent != 0 {
			r.errf("Error: \"%s\" is duplicate parent ID\n", w)
			return nil, true, retCode(-1)
		}
		a.parent, _ = parentWord(w)
		if !get {
			f.parent = a.parent
		}
		return args[1:], true, nil
	}
	valued := w == wordDev || w == wordParent || w == "handle" || matches(w, "block") ||
		matches(w, "preference") || matches(w, "priority") || matches(w, "protocol") || matches(w, "chain")
	if !valued {
		return nil, false, nil
	}
	if len(args) < 2 {
		return nil, true, r.incomplete()
	}
	v := args[1]
	rest = args[2:]
	switch {
	case w == wordDev:
		err = r.devArg(a, v, get)
	case matches(w, "block"):
		msg := "Error: \"block\" cannot be used in the same time as \"dev\"\n"
		if get {
			msg = "Error: \"dev\" and \"block\" are mutually exclusive\n"
		}
		err = r.blockArg(a, v, msg)
	case w == wordParent:
		err = r.parentArg(a, v, get)
	case w == "handle":
		if a.hasHandle {
			return nil, true, r.duparg("handle", v)
		}
		a.handle, a.hasHandle = v, true // a list parses it and never uses it (Q29h)
	case matches(w, "preference") || matches(w, "priority"):
		err = r.prioArg(a, v, get)
	case matches(w, "protocol"):
		err = r.protoArg(a, v, get)
	default:
		err = r.chainArg(a, v, get)
	}
	return rest, true, err
}

func (r *runner) devArg(a *filterArgs, v string, get bool) error {
	if a.dev != "" {
		return r.duparg(wordDev, v)
	}
	if a.blockIndex != 0 {
		if get {
			r.errf("Error: \"dev\" and \"block\" are mutually exclusive\n")
		} else {
			r.errf("Error: \"dev\" cannot be used in the same time as \"block\"\n")
		}
		return retCode(-1)
	}
	a.dev = truncName(v)
	return nil
}

func (r *runner) parentArg(a *filterArgs, v string, get bool) error {
	if a.parent != 0 {
		return r.duparg(wordParent, v)
	}
	h, ok := tcClassid(v)
	switch {
	case !ok && get:
		return r.invarg("Invalid parent ID", v)
	case !ok:
		return r.invarg("invalid parent ID", v)
	}
	a.parent = h
	if !get {
		r.ff.parent = h
	}
	return nil
}

// prioArg is pref: a list never checks it against 0xFFFF (L40), a get
// does.
func (r *runner) prioArg(a *filterArgs, v string, get bool) error {
	if a.prio != 0 {
		return r.duparg("priority", v)
	}
	p, ok := getU32(v, 0)
	switch {
	case get && (!ok || p > math.MaxUint16):
		return r.invarg("invalid priority value", v)
	case !ok:
		return r.invarg("invalid preference", v)
	}
	a.prio = p
	if !get {
		r.ff.prio = p
	}
	return nil
}

// protoArg is protocol: a list's duparg tests the value, a get's whether
// one was given.
func (r *runner) protoArg(a *filterArgs, v string, get bool) error {
	if (get && a.protoSet) || (!get && a.protocol != 0) {
		return r.duparg("protocol", v)
	}
	p, ok := llProtoA2N(v)
	if !ok {
		return r.invarg("invalid protocol", v)
	}
	a.protocol, a.protoSet = p, true
	if !get {
		r.ff.protocol = p
	}
	return nil
}

// chainArg is chain: a list's index is the static, so a second batch line
// naming one is a duparg (Q29f); a get's is its own.
func (r *runner) chainArg(a *filterArgs, v string, get bool) error {
	f := &r.ff
	if (get && a.chainSet) || (!get && f.chainSet) {
		return r.duparg("chain", v)
	}
	c, ok := getU32(v, 0)
	if !ok {
		return r.invarg("invalid chain index value", v)
	}
	if get {
		a.chain, a.chainSet = c, true
	} else {
		f.chain, f.chainSet = c, true
	}
	return nil
}

// filterList is tc_filter_list: `filter show` (RTM_GETTFILTER) and
// `chain show` (RTM_GETCHAIN).
func (r *runner) filterList(cmd uint16, args []string) error {
	var a filterArgs
	f := &r.ff
	for len(args) > 0 {
		rest, handled, err := r.filterWord(&a, args, false)
		switch {
		case err != nil:
			return err
		case handled:
			args = rest
			continue
		case matches(args[0], helpWord):
			r.filterUsage() // and on to the dump (F7)
		default:
			r.errf(" What is \"%s\"? Try \"tc filter help\"\n", args[0]) // a leading space (L41)
			return retCode(-1)
		}
		args = args[1:]
	}
	q := xtcpnl.TcFilterReq{Parent: a.parent, Prio: a.prio, Protocol: bits.ReverseBytes16(a.protocol),
		Chain: f.chain, ChainSet: f.chainSet}
	if a.dev != "" {
		i, err := r.devIndex(a.dev)
		if err != nil {
			return err
		}
		q.Ifindex, f.ifindex = i, i
	} else {
		if err := r.initMap(); err != nil {
			return err
		}
		if a.blockIndex != 0 {
			found, err := r.blockExists(a.blockIndex)
			if err != nil {
				return err
			}
			if !found {
				r.errf("Cannot find block \"%d\"\n", a.blockIndex)
				return retCode(1)
			}
			q.Ifindex, q.Parent = xtcpnl.TcmIfindexMagicBlock, a.blockIndex
			f.blockIndex = a.blockIndex
		}
	}
	req, berr := xtcpnl.BuildTcFilterDump(cmd, q, r.brief > 0, r.nextSeq())
	return r.dumpShow(req, berr, r.printFilter)
}

// devIndex is ll_name_to_index, and -nodev(d) when it finds nothing.
func (r *runner) devIndex(dev string) (uint32, error) {
	i, err := r.nameToIndex(dev)
	if err != nil {
		return 0, err
	}
	if i == 0 {
		return 0, r.nodev(dev)
	}
	return i, nil
}

// blockArg is the `block` word of both loops: a duparg, the exclusion
// with dev (each loop has its message), and get_u32 with a non-zero check.
func (r *runner) blockArg(a *filterArgs, v, devMsg string) error {
	if a.blockIndex != 0 {
		return r.duparg("block", v)
	}
	if a.dev != "" {
		r.errf("%s", devMsg)
		return retCode(-1)
	}
	b, ok := getU32(v, 0)
	if !ok || b == 0 {
		return r.invarg("invalid block index value", v)
	}
	a.blockIndex = b
	return nil
}

// blockExists is tc_qdisc_block_exists: a qdisc dump looking for the block
// on any qdisc's ingress or egress.
func (r *runner) blockExists(block uint32) (bool, error) {
	req, berr := xtcpnl.BuildTcDumpRequest(unix.RTM_GETQDISC, 0, 0, 0, false, r.nextSeq())
	found := false
	var kerr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		if m.Type != unix.RTM_NEWQDISC {
			return 0
		}
		if len(m.Body) < xtcpnl.TcMsgSizeCst {
			return -1
		}
		tb := r.attrTable(m.Body[xtcpnl.TcMsgSizeCst:], xtcpnl.TcaExtWarnMsg, unix.NLA_F_NESTED)
		if !tb.Has(xtcpnl.TcaKind) {
			return -1
		}
		for _, t := range []uint16{xtcpnl.TcaIngressBlock, xtcpnl.TcaEgressBlock} {
			if v, ok := tb.U32(t); ok && v == block {
				found = true
			}
		}
		if kind, _ := tb.Str(xtcpnl.TcaKind); kind == "red" {
			kerr = errKind("red") // red's has_block reads its options
			return -1
		}
		return 0
	})
	if err == nil {
		err = kerr
	}
	if err != nil {
		return false, err
	}
	if rc < 0 {
		r.errf("Dump terminated\n: Success\n") // perror("Dump terminated\n")
		return false, nil
	}
	return found, nil
}

// filterGetArgs is tc_filter_get's loop: the shared words, help (usage,
// and return 0: done), and the kind, which ends it. rest is what follows
// the kind.
func (r *runner) filterGetArgs(a *filterArgs, args []string) (kind string, haveKind bool, rest []string, done bool, err error) {
	for len(args) > 0 {
		next, handled, err := r.filterWord(a, args, true)
		switch {
		case err != nil:
			return "", false, nil, false, err
		case handled:
			args = next
			continue
		case matches(args[0], helpWord):
			r.filterUsage()
			return "", false, nil, true, nil
		case args[0] == "":
			return "", false, nil, false, r.invarg("invalid filter name", args[0])
		}
		return cutName(args[0], filterNameSize-1), true, args[1:], false, nil
	}
	return "", false, nil, false, nil
}

// filterGet is tc_filter_get: `filter get` (RTM_GETTFILTER) and `chain
// get` (RTM_GETCHAIN). It returns 1 when the kind's parse_fopt fails, 2
// when the kernel does, and -1 for the rest (F15).
func (r *runner) filterGet(cmd uint16, args []string) error {
	var a filterArgs
	kind, haveKind, args, done, err := r.filterGetArgs(&a, args)
	if err != nil || done {
		return err
	}
	q := xtcpnl.TcFilterReq{Parent: a.parent, Chain: a.chain, ChainSet: a.chainSet}
	isFilter := cmd == unix.RTM_GETTFILTER
	if err := r.filterGetChecks(&a, &q, isFilter, haveKind); err != nil {
		return err
	}
	var opts []byte
	if isFilter {
		if opts, err = r.parseFopt(kind, a, args, &q); err != nil {
			return err
		}
		if !a.hasHandle {
			r.errf("Must specify filter \"handle\"\n")
			return retCode(-1)
		}
	}
	if len(args) > 0 {
		if matches(args[0], helpWord) {
			r.filterUsage()
		}
		r.errf("Garbage instead of arguments \"%s ...\". Try \"tc filter help\".\n", args[0])
		return retCode(-1)
	}
	req, berr := xtcpnl.BuildTcFilterGet(cmd, q, kind, opts, r.nextSeq())
	ans, ok, err := r.talkAnswer(req, berr)
	if err != nil {
		return err
	}
	if !ok {
		r.errf("We have an error talking to the kernel\n")
		return retCode(2)
	}
	r.o.newJSONObj()
	if _, err := r.printFilter(ans); err != nil {
		return err
	}
	r.o.deleteJSONObj()
	return nil
}

// filterGetChecks is tc_filter_get's tests after its loop, in its order,
// with the dev lookup (which sets filter_ifindex) or the block.
func (r *runner) filterGetChecks(a *filterArgs, q *xtcpnl.TcFilterReq, isFilter, haveKind bool) error {
	if isFilter {
		if !a.protoSet {
			r.errf("Must specify filter protocol\n")
			return retCode(-1)
		}
		if a.prio == 0 {
			r.errf("Must specify filter priority\n")
			return retCode(-1)
		}
		q.Prio, q.Protocol = a.prio, bits.ReverseBytes16(a.protocol)
	}
	if q.Parent == tcHUnspec {
		r.errf("Must specify filter parent\n")
		return retCode(-1)
	}
	if isFilter && !haveKind {
		r.errf("Must specify filter type\n")
		return retCode(-1)
	}
	switch {
	case a.dev != "":
		i, err := r.devIndex(a.dev)
		if err != nil {
			return err
		}
		q.Ifindex, r.ff.ifindex = i, i
	case a.blockIndex != 0:
		q.Ifindex, q.Parent = xtcpnl.TcmIfindexMagicBlock, a.blockIndex
		r.ff.blockIndex = a.blockIndex
	default:
		r.errf("Must specify netdevice \"dev\" or block index \"block\"\n")
		return retCode(-1)
	}
	return nil
}

// cutName is strncpy(buf, s, n): the first n bytes, up to a NUL.
func cutName(s string, n int) string {
	s = cstr(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// parseFopt is the kind's parse_fopt(q, fhandle, argc, argv, n) for a get:
// the handle into tcm_handle, and the attributes it appends (cgroup's
// empty TCA_OPTIONS, flow's nest with TCA_FLOW_MODE). Its -1 is
// tc_filter_get's return 1. Options after the kind would be the kind's
// whole selector grammar, a write's; gotc refuses them.
func (r *runner) parseFopt(kind string, a filterArgs, rest []string, q *xtcpnl.TcFilterReq) ([]byte, error) {
	fail := func(format string, v ...any) ([]byte, error) {
		r.errf(format, v...)
		return nil, retCode(1)
	}
	if _, ok := filterUtils[kind]; !ok && !tcBuiltinFilters[kind] {
		// parse_nofopt
		if len(rest) > 0 {
			return fail("Unknown filter \"%s\", hence option \"%s\" is unparsable\n", kind, rest[0])
		}
		if a.hasHandle {
			h, ok := getU32(a.handle, 16)
			if !ok {
				return fail("Unparsable filter ID \"%s\"\n", a.handle)
			}
			q.Handle = h
		}
		return nil, nil
	}
	if _, ok := filterUtils[kind]; !ok {
		return nil, fmt.Errorf("%w: filter kind %q", errNotImplemented, kind)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("%w: %s options in a get (%q)", errNotImplemented, kind, rest[0])
	}
	switch kind {
	case xtcpnl.TcFKindU32:
		if a.hasHandle {
			h, ok := getU32Handle(a.handle)
			if !ok {
				return fail("Illegal filter ID\n")
			}
			q.Handle = h
		}
	case xtcpnl.TcFKindFw:
		if a.hasHandle {
			h, _, _ := strings.Cut(a.handle, "/")
			v, ok := getU32(h, 0)
			if !ok {
				return fail("Illegal \"handle\"\n")
			}
			q.Handle = v
			if _, m, slash := strings.Cut(a.handle, "/"); slash {
				if _, ok := getU32(m, 0); !ok {
					return fail("Illegal \"handle\" mask\n")
				}
			}
		}
	case xtcpnl.TcFKindRoute, xtcpnl.TcFKindFlow:
		if a.hasHandle {
			v, ok := getU32(a.handle, 0)
			if !ok {
				return fail("Illegal \"handle\"\n")
			}
			q.Handle = v
		}
		if kind == xtcpnl.TcFKindFlow {
			// addattr_nest, then addattr32(TCA_FLOW_MODE, FLOW_MODE_MAP).
			mode := binary.LittleEndian.AppendUint16(nil, xtcpnl.RTAttrSizeCst+4)
			mode = binary.LittleEndian.AppendUint16(mode, xtcpnl.TcaFlowMode)
			mode = binary.LittleEndian.AppendUint32(mode, xtcpnl.FlowModeMap)
			nest := binary.LittleEndian.AppendUint16(nil, xtcpnl.RTAttrSizeCst+8)
			nest = binary.LittleEndian.AppendUint16(nest, xtcpnl.TcaOptions)
			return append(nest, mode...), nil
		}
	default: // basic, cgroup, matchall: strtol(handle, NULL, 0)
		var h int64
		if a.hasHandle {
			h = strtol(a.handle)
			if h == math.MinInt64 || h == math.MaxInt64 {
				return fail("Illegal handle \"%s\", must be numeric.\n", a.handle)
			}
		}
		q.Handle = cint.LongBits(h) // F5: "1junk" is 1, 4294967297 is 1
		if kind == xtcpnl.TcFKindCgroup {
			// addattr_l(n, MAX_MSG, TCA_OPTIONS, NULL, 0)
			nest := binary.LittleEndian.AppendUint16(nil, xtcpnl.RTAttrSizeCst)
			return binary.LittleEndian.AppendUint16(nest, xtcpnl.TcaOptions), nil
		}
	}
	return nil, nil
}

// printFilter is print_filter.
func (r *runner) printFilter(m Msg) (int, error) {
	isFilter := m.Type == unix.RTM_NEWTFILTER || m.Type == unix.RTM_GETTFILTER || m.Type == unix.RTM_DELTFILTER
	isChain := m.Type == unix.RTM_NEWCHAIN || m.Type == unix.RTM_GETCHAIN || m.Type == unix.RTM_DELCHAIN
	if !isFilter && !isChain {
		r.errf("Not a filter(cmd %d)\n", m.Type)
		return 0, nil
	}
	if len(m.Body) < xtcpnl.TcMsgSizeCst {
		r.errf("Wrong len %d\n", len(m.Body)-xtcpnl.TcMsgSizeCst)
		return -1, nil
	}
	var t xtcpnl.Tcmsg
	if _, err := xtcpnl.DeserializeTcmsg(m.Body, &t); err != nil {
		return 0, err
	}
	tb := r.attrTable(m.Body[xtcpnl.TcMsgSizeCst:], xtcpnl.TcaExtWarnMsg, unix.NLA_F_NESTED)
	if !tb.Has(xtcpnl.TcaKind) && isFilter {
		r.errf("print_filter: NULL kind\n")
		return -1, nil
	}
	o := r.o
	o.openObject("")
	r.printFilterVerb(m)
	if isFilter {
		o.fp("filter ")
	} else {
		o.fp("chain ")
	}
	if err := r.printFilterWhere(t); err != nil {
		return 0, err
	}
	if t.Info != 0 && isFilter {
		r.printFilterInfo(t.Info)
	}
	if err := r.printFilterBody(m, t, tb); err != nil {
		return 0, err
	}
	o.closeObject()
	return 0, nil
}

// printFilterVerb is print_filter's deleted, replaced or added.
func (r *runner) printFilterVerb(m Msg) {
	o := r.o
	isNew := m.Type == unix.RTM_NEWTFILTER || m.Type == unix.RTM_NEWCHAIN
	switch {
	case m.Type == unix.RTM_DELTFILTER || m.Type == unix.RTM_DELCHAIN:
		o.boolean(printAny, "deleted", "deleted ", true)
	case isNew && m.Flags&unix.NLM_F_CREATE != 0 && m.Flags&unix.NLM_F_EXCL == 0:
		o.boolean(printAny, "replaced", "replaced ", true)
	case isNew && m.Flags&unix.NLM_F_CREATE != 0:
		o.boolean(printAny, "added", "added ", true)
	}
}

// printFilterWhere is the block, or the dev and parent, each unless the
// list's statics filter it out.
func (r *runner) printFilterWhere(t xtcpnl.Tcmsg) error {
	f := r.ff
	if t.Ifindex == xtcpnl.TcmIfindexMagicBlock {
		if f.blockIndex == 0 || f.blockIndex != t.Parent {
			r.o.uint(printAny, "block", "block "+u32s(t.Parent)+" ", t.Parent)
		}
		return nil
	}
	if f.ifindex == 0 || f.ifindex != t.Ifindex {
		if err := r.devname(printAny, t.Ifindex); err != nil {
			return err
		}
	}
	if f.parent == 0 || f.parent != t.Parent {
		r.printFilterParent(t.Parent)
	}
	return nil
}

// printFilterInfo is tcm_info's protocol and pref, setting f_proto.
func (r *runner) printFilterInfo(info uint32) {
	o, f := r.o, r.ff
	r.fProto = uint16(info & math.MaxUint16) // TC_H_MIN: the htons'd protocol
	prio := info >> 16
	if (f.protocol == 0 || f.protocol != r.fProto) && r.fProto != 0 {
		p := r.llProtoN2A(r.fProto)
		o.str(printAny, colorNone, "protocol", "protocol "+p+" ", p)
	}
	if (f.prio == 0 || f.prio != prio) && prio != 0 {
		o.uint(printAny, "pref", "pref "+u32s(prio)+" ", prio)
	}
}

// printFilterBody is the kind, the chain, the options, -s's stats and the
// extack warning.
func (r *runner) printFilterBody(m Msg, t xtcpnl.Tcmsg, tb xtcpnl.AttrTable) error {
	o, f := r.o, r.ff
	kind, hasKind := tb.Str(xtcpnl.TcaKind)
	if hasKind {
		o.str(printAny, colorNone, "kind", kind+" ", kind)
	}
	if tb.Has(xtcpnl.TcaChain) {
		c := attrU32(tb, xtcpnl.TcaChain)
		if !f.chainSet || f.chain != c {
			o.uint(printAny, "chain", "chain "+u32s(c)+" ", c)
		}
	}
	if hasKind && tb.Has(xtcpnl.TcaOptions) {
		o.openObject("options")
		if err := r.printFopt(kind, tb.Bytes(xtcpnl.TcaOptions), t.Handle); err != nil {
			return err
		}
		o.closeObject()
	}
	o.nl()
	if r.stats > 0 && (tb.Has(xtcpnl.TcaStats) || tb.Has(xtcpnl.TcaStats2)) {
		ti, err := xtcpnl.ParseTcMsg(m.Body)
		if err != nil {
			return err
		}
		if _, _, err := r.tcStats(&ti, " "); err != nil {
			return err
		}
		o.nl()
	}
	if msg, ok := tb.Str(xtcpnl.TcaExtWarnMsg); ok {
		o.str(printAny, colorNone, "warn", msg, msg)
		o.nl()
	}
	return nil
}

// printFilterParent is print_filter's parent: root, ingress, egress or
// "parent CLASSID".
func (r *runner) printFilterParent(p uint32) {
	o := r.o
	switch p {
	case tcHRoot:
		o.boolean(printAny, wordRoot, "root ", true)
	case tcHClsactIngress:
		o.boolean(printAny, wordIngress, "ingress ", true)
	case tcHClsactEgress:
		o.boolean(printAny, "egress", "egress ", true)
	default:
		c := r.classid(p)
		o.str(printAny, colorNone, wordParent, "parent "+c+" ", c)
	}
}

// attrU32 is rta_getattr_u32 on an attribute that may be short: tc reads
// four bytes whatever its length; here the bytes past it read as zero.
func attrU32(tb xtcpnl.AttrTable, t uint16) uint32 {
	return binary.LittleEndian.Uint32(xtcpnl.TcMinCopy(tb.Bytes(t), 4))
}

// llProtoN2A is ll_proto_n2a of an htons'd id: its name, or "[%d]" of
// the host-order id, which -N always gives.
func (r *runner) llProtoN2A(id uint16) string {
	host := bits.ReverseBytes16(id)
	if r.numeric == 0 {
		for _, p := range xtcpnl.LLProtoNames {
			if p.ID == host {
				return p.Name
			}
		}
	}
	return "[" + strconv.Itoa(int(host)) + "]"
}

// llProtoA2N is ll_proto_a2n: a name (strcasecmp) or get_be16's base-0
// number, htons'd.
func llProtoA2N(s string) (uint16, bool) {
	for _, p := range xtcpnl.LLProtoNames {
		if strings.EqualFold(p.Name, s) {
			return bits.ReverseBytes16(p.ID), true
		}
	}
	v, ok := getU32(s, 0)
	if !ok || v > math.MaxUint16 {
		return 0, false
	}
	u, _ := cint.U16(int(v))
	return bits.ReverseBytes16(u), true
}

// getU32 is lib/utils.c get_u32: strtoul with nothing left over and no
// value past 32 bits; an empty string fails.
func getU32(s string, base uint64) (uint32, bool) {
	if s == "" {
		return 0, false
	}
	v, rest, ok := strtoul(s, base)
	if !ok || rest != "" || v > math.MaxUint32 {
		return 0, false
	}
	u, _ := cint.U32From64(v)
	return u, true
}

// getU32Handle is f_u32.c get_u32_handle: "0x..." without a colon, else
// up to three hex fields after any one separator each, trailing garbage
// after the node id ignored (F6).
func getU32Handle(s string) (uint32, bool) {
	if !strings.Contains(s, ":") {
		if strings.HasPrefix(s, "0x") {
			return getU32(s, 16)
		}
		return 0, false
	}
	htid, rest, ok := strtoul(s, 16)
	if !ok && s[0] != ':' {
		return 0, false
	}
	if htid >= 0x1000 {
		return 0, false
	}
	var hash, node uint64
	if rest != "" {
		str := rest[1:]
		if hash, rest, ok = strtoul(str, 16); !ok && str != "" && str[0] != ':' {
			return 0, false
		}
		if hash >= 0x100 {
			return 0, false
		}
		if rest != "" {
			str = rest[1:]
			if node, _, ok = strtoul(str, 16); !ok && str != "" {
				return 0, false
			}
			if node >= 0x1000 {
				return 0, false
			}
		}
	}
	h, _ := cint.U32From64(htid<<20 | hash<<12 | node)
	return h, true
}

// strtoul is C strtoul on a 64-bit long: leading space, a sign, base 0's
// and 16's 0x prefix and base 0's 0, then digits. It returns the value
// (negated modulo 2^64 for '-', ULONG_MAX on overflow), the unparsed
// rest, and whether any digit was read; with none, rest is the input.
func strtoul(s string, base uint64) (uint64, string, bool) {
	v, rest, ok, _ := strtoulOver(s, base)
	return v, rest, ok
}

// strtoulOver is strtoul that also reports overflow (ERANGE).
func strtoulOver(s string, base uint64) (v uint64, rest string, ok, over bool) {
	orig := s
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	switch {
	case (base == 0 || base == 16) && len(s) > 2 && s[0] == '0' && (s[1]|0x20) == 'x' && hexVal(s[2]) >= 0:
		s, base = s[2:], 16
	case base == 0 && s != "" && s[0] == '0':
		base = 8
	case base == 0:
		base = 10
	}
	n := 0
	for ; n < len(s); n++ {
		d := hexVal(s[n])
		if d < 0 || uint64(d) >= base {
			break
		}
		if !over {
			if v > (math.MaxUint64-uint64(d))/base {
				over = true
			} else {
				v = v*base + uint64(d)
			}
		}
	}
	if n == 0 {
		return 0, orig, false, false
	}
	switch {
	case over:
		v = math.MaxUint64
	case neg:
		v = -v
	}
	return v, s[n:], true, over
}

// strtol is strtol(s, NULL, 0) on a 64-bit long: LONG_MAX or LONG_MIN on
// overflow, 0 when no digit is read.
func strtol(s string) int64 {
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg, t = t[0] == '-', t[1:]
	}
	if t == "" || t[0] == '+' || t[0] == '-' || cIsSpace(t[0]) {
		return 0 // strtoul would skip what strtol, past its sign, does not
	}
	mag, _, ok, over := strtoulOver(t, 0)
	switch {
	case !ok:
		return 0
	case neg && (over || mag > 1<<63):
		return math.MinInt64
	case neg:
		return -cint.LongLong(mag)
	case over || mag > math.MaxInt64:
		return math.MaxInt64
	}
	return cint.LongLong(mag)
}
