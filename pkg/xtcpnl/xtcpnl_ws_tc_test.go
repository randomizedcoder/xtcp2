package xtcpnl

// The workbench corpus's tc oracles (docs/netlink/coverage-status.md,
// "Workbench and generated rows"). Like the generic ones in
// xtcpnl_ws_corpus_test.go they hold every row, not hand-picked ones:
//
//   - O6: every tc message in every capture (command and readbacks) parses,
//     and carries no attribute type past the last one the parser knows.
//   - O3: a qdisc, class or filter request carries what its command line
//     says. tcWantFromCmd reads the command line the way iproute2's
//     argument loops do (tc/tc_qdisc.c tc_qdisc_modify, tc/tc_class.c
//     tc_class_modify, tc/tc_filter.c tc_filter_modify), so a request that
//     disagrees is an encoding bug in tc, or a misread in our parser.
//   - O4: a qdisc the kernel accepted shows up in the row's verify dump,
//     on the same device, with the same kind, parent and handle.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The tc handle values iproute2 writes for its keywords (pkt_sched.h).
const (
	tcHRoot       = 0xFFFFFFFF // TC_H_ROOT
	tcHIngress    = 0xFFFFFFF1 // TC_H_INGRESS, which is also TC_H_CLSACT
	tcHFilterIn   = 0xFFFFFFF2 // TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_INGRESS)
	tcHFilterOut  = 0xFFFFFFF3 // TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_EGRESS)
	tcHIngressMaj = 0xFFFF0000 // TC_H_MAKE(TC_H_INGRESS, 0), the handle of ingress and clsact
	tcKindMax     = 15         // FILTER_NAMESZ less its NUL: tc truncates a longer kind
	tcEthPAll     = 0x0003     // ETH_P_ALL, a new filter's protocol when none is given
)

var (
	wsTcmsgTypes = map[uint16]bool{
		unix.RTM_NEWQDISC: true, unix.RTM_DELQDISC: true, unix.RTM_GETQDISC: true,
		unix.RTM_NEWTCLASS: true, unix.RTM_DELTCLASS: true, unix.RTM_GETTCLASS: true,
		unix.RTM_NEWTFILTER: true, unix.RTM_DELTFILTER: true, unix.RTM_GETTFILTER: true,
		unix.RTM_NEWCHAIN: true, unix.RTM_DELCHAIN: true, unix.RTM_GETCHAIN: true,
	}
	wsTcActionTypes = map[uint16]bool{
		unix.RTM_NEWACTION: true, unix.RTM_DELACTION: true, unix.RTM_GETACTION: true,
	}
)

// tcUnknown names the attribute types in attrs past last.
func tcUnknown(what string, attrs []uint16, last uint16) error {
	var errs []error
	for _, a := range attrs {
		if a > last { // WalkRTAttrs has cleared the flag bits
			errs = append(errs, fmt.Errorf("%s attribute %d is past the last one the parser knows (%d)", what, a, last))
		}
	}
	return errors.Join(errs...)
}

// wsTcMsgProblem is O6 for one message: nil for a message that is not tc's.
func wsTcMsgProblem(typ uint16, body []byte) error {
	switch {
	case wsTcmsgTypes[typ]:
		ti, err := ParseTcMsg(body)
		if err != nil {
			return err
		}
		return tcUnknown("TCA", ti.Attrs, TcaMaxKnown)
	case wsTcActionTypes[typ]:
		ai, err := ParseTcAction(body)
		if err != nil {
			return err
		}
		errs := []error{tcUnknown("TCA_ROOT", ai.Attrs, TcaRootMaxKnown)}
		for _, a := range ai.Actions {
			errs = append(errs, tcUnknown(fmt.Sprintf("TCA_ACT (order %d)", a.Order), a.Attrs, TcaActMaxKnown))
		}
		return errors.Join(errs...)
	}
	return nil
}

// tcMatches is iproute2's matches(): tok is a non-empty prefix of word.
func tcMatches(tok, word string) bool {
	return tok != "" && strings.HasPrefix(word, tok)
}

// tcStrtoul16 is strtoul(s, &p, 16): the value of the longest hex prefix
// (after an optional 0x) and the rest; ok is false when no digit was read.
func tcStrtoul16(s string) (v uint64, rest string, ok bool) {
	t := s
	if len(t) > 2 && t[0] == '0' && (t[1] == 'x' || t[1] == 'X') && strings.ContainsAny(t[2:3], "0123456789abcdefABCDEF") {
		t = t[2:]
	}
	i := 0
	for i < len(t) && strings.IndexByte("0123456789abcdefABCDEF", t[i]) >= 0 {
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	v, err := strconv.ParseUint(t[:i], 16, 64)
	if err != nil {
		v = 1 << 63 // strtoul saturates; any value this big is refused
	}
	return v, t[i:], true
}

// tcQdiscHandle is get_qdisc_handle (tc/tc_util.c): "none", or a hex
// major below 1<<16 with an optional trailing ':' and anything after it.
func tcQdiscHandle(s string) (uint32, error) {
	if s == "none" {
		return 0, nil
	}
	maj, rest, ok := tcStrtoul16(s)
	if !ok || maj >= 1<<16 || (rest != "" && rest[0] != ':') {
		return 0, fmt.Errorf("invalid qdisc ID %q", s)
	}
	return uint32(maj) << 16, nil
}

// tcClassid is get_tc_classid (tc/tc_util.c): "root", "none", "maj:min"
// in hex with either half omitted, or a bare number taken unshifted.
func tcClassid(s string) (uint32, error) {
	switch s {
	case "root":
		return tcHRoot, nil
	case "none":
		return 0, nil
	}
	bad := fmt.Errorf("invalid class ID %q", s)
	maj, rest, ok := tcStrtoul16(s)
	if !ok && !strings.HasPrefix(s, ":") {
		return 0, bad
	}
	if !strings.HasPrefix(rest, ":") {
		if rest != "" || maj > 0xFFFFFFFF {
			return 0, bad
		}
		return uint32(maj), nil
	}
	if maj >= 1<<16 {
		return 0, bad
	}
	minor, tail, mok := tcStrtoul16(rest[1:])
	if tail != "" || minor >= 1<<16 || (!mok && rest[1:] != "") {
		return 0, bad
	}
	return uint32(maj)<<16 | uint32(minor), nil
}

// tcU32Arg is get_u32(..., 0): a whole C-style number (decimal, 0x hex or
// 0 octal) that fits in 32 bits.
func tcU32Arg(s string) (uint32, error) {
	base, t := 10, s
	switch {
	case len(t) > 2 && (t[:2] == "0x" || t[:2] == "0X"):
		base, t = 16, t[2:]
	case len(t) > 1 && t[0] == '0':
		base, t = 8, t[1:]
	}
	v, err := strconv.ParseUint(t, base, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", s)
	}
	return uint32(v), nil
}

// tcEthProtos is the part of lib/ll_proto.c's table a tc command line is
// likely to name; ll_proto_a2n compares names without case.
var tcEthProtos = map[string]uint16{
	"loop": 0x0060, "ip": 0x0800, "arp": 0x0806, "rarp": 0x8035, "ipv6": 0x86DD,
	"802_3": 0x0001, "all": 0x0003, "802_2": 0x0004, "802.1q": 0x8100, "802.1ad": 0x88A8,
	"mpls_uc": 0x8847, "mpls_mc": 0x8848, "teb": 0x6558, "cfm": 0x8902, "ipx": 0x8137,
}

// tcProto is ll_proto_a2n: a name from the table, or a 16-bit number.
// known is false for a name outside tcEthProtos, which O3 then leaves be.
func tcProto(s string) (proto uint32, known bool, err error) {
	if p, ok := tcEthProtos[strings.ToLower(s)]; ok {
		return uint32(p), true, nil
	}
	v, err := tcU32Arg(s)
	if err != nil {
		if strings.ContainsAny(s[:1], "0123456789") {
			return 0, false, fmt.Errorf("invalid protocol %q", s)
		}
		return 0, false, nil
	}
	if v > 0xFFFF {
		return 0, false, fmt.Errorf("invalid protocol %q", s)
	}
	return v, true, nil
}

// tcWant is what tc puts in a qdisc, class or filter request, read off its
// command line.
type tcWant struct {
	typ    uint16 // RTM_NEWQDISC, RTM_DELTFILTER, ...
	kind   string
	handle uint32 // qdisc and class only: a filter's handle is its kind's to parse
	parent uint32
	filter bool
	prio   uint32
	proto  uint32
	// protoKnown is false when the protocol is a name tcEthProtos lacks.
	protoKnown bool
	chain      *uint32
	block      uint32 // a filter on a shared block: ifindex TCM_IFINDEX_MAGIC_BLOCK, parent the block
	inBlock    uint32 // a qdisc's TCA_INGRESS_BLOCK
	outBlock   uint32 // a qdisc's TCA_EGRESS_BLOCK
}

// tcGlobalFlags are tc's options that take no argument, spelled the way
// tdc and our rows spell them. A command with any other option is outside
// O3's scope: -b, -n, -cf and their prefixes take one, which the loops
// below would misread as an object.
var tcGlobalFlags = map[string]bool{
	"-s": true, "-stats": true, "-statistics": true, "-d": true, "-details": true, "-r": true, "-raw": true,
	"-p": true, "-pretty": true, "-j": true, "-json": true, "-o": true, "-oneline": true, "-N": true,
	"-iec": true, "-echo": true,
}

// tcVerbs maps each object's verbs to the request type and whether tc asks
// to create (NLM_F_CREATE), from do_qdisc, do_class and do_filter.
var tcVerbs = map[string][]struct {
	verb   string
	typ    uint16
	create bool
}{
	"qdisc": {
		{"add", unix.RTM_NEWQDISC, true}, {"change", unix.RTM_NEWQDISC, false},
		{"replace", unix.RTM_NEWQDISC, true}, {"link", unix.RTM_NEWQDISC, false},
		{"delete", unix.RTM_DELQDISC, false},
	},
	"class": {
		{"add", unix.RTM_NEWTCLASS, true}, {"change", unix.RTM_NEWTCLASS, false},
		{"replace", unix.RTM_NEWTCLASS, true}, {"delete", unix.RTM_DELTCLASS, false},
	},
	"filter": {
		{"add", unix.RTM_NEWTFILTER, true}, {"change", unix.RTM_NEWTFILTER, false},
		{"replace", unix.RTM_NEWTFILTER, true}, {"delete", unix.RTM_DELTFILTER, false},
	},
}

// tcWantFromCmd reads a row's command. ok is false when the command is not
// one tc qdisc, class or filter modification O3 can read (a pipeline, a
// batch, a show, an action, an option with an argument); err is set when it
// is, but tc would refuse it before sending anything.
func tcWantFromCmd(cmd string) (w tcWant, ok bool, err error) {
	if strings.ContainsAny(cmd, "'\"`|;&<>$()\\") {
		return w, false, nil
	}
	f := strings.Fields(cmd)
	if len(f) < 3 || f[0] != "tc" {
		return w, false, nil
	}
	i := 1
	for i < len(f) && strings.HasPrefix(f[i], "-") {
		if !tcGlobalFlags[f[i]] {
			return w, false, nil
		}
		i++
	}
	if i+1 >= len(f) {
		return w, false, nil
	}
	obj := ""
	for _, o := range []string{"qdisc", "class", "filter"} {
		if tcMatches(f[i], o) {
			obj = o
			break
		}
	}
	if obj == "" {
		return w, false, nil
	}
	create := false
	for _, v := range tcVerbs[obj] {
		if tcMatches(f[i+1], v.verb) {
			w.typ, create = v.typ, v.create
			break
		}
	}
	if w.typ == 0 {
		return w, false, nil
	}
	args := f[i+2:]
	switch obj {
	case "qdisc":
		err = w.readQdisc(args)
	case "class":
		err = w.readClass(args)
	default:
		w.filter = true
		if w.typ == unix.RTM_NEWTFILTER && create {
			w.proto, w.protoKnown = tcEthPAll, true
		} else {
			w.protoKnown = true
		}
		err = w.readFilter(args)
	}
	if errors.Is(err, errTcOutOfScope) {
		return w, false, nil
	}
	if len(w.kind) > tcKindMax {
		w.kind = w.kind[:tcKindMax]
	}
	return w, true, err
}

var errTcOutOfScope = errors.New("outside O3's model")

// tcNext returns args[i+1], or an error naming the keyword left without it.
func tcNext(args []string, i int) (string, error) {
	if i+1 >= len(args) {
		return "", fmt.Errorf("%q needs an argument", args[i])
	}
	return args[i+1], nil
}

// readQdisc is tc_qdisc_modify's loop.
func (w *tcWant) readQdisc(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var err error
		switch {
		case a == "dev":
			_, err = tcNext(args, i)
			i++
		case a == "handle":
			var v string
			if v, err = tcNext(args, i); err == nil {
				w.handle, err = tcQdiscHandle(v)
			}
			i++
		case a == "root":
			w.parent = tcHRoot
		case a == "clsact" || a == "ingress":
			w.parent, w.kind, w.handle = tcHIngress, a, tcHIngressMaj
			return nil
		case a == "parent":
			var v string
			if v, err = tcNext(args, i); err == nil {
				w.parent, err = tcClassid(v)
			}
			i++
		case tcMatches(a, "estimator"):
			i += 2
		case tcMatches(a, "stab"), tcMatches(a, "help"):
			return errTcOutOfScope
		case tcMatches(a, "ingress_block"), tcMatches(a, "egress_block"):
			var v string
			var n uint32
			if v, err = tcNext(args, i); err == nil {
				n, err = tcU32Arg(v)
			}
			if tcMatches(a, "ingress_block") {
				w.inBlock = n
			} else {
				w.outBlock = n
			}
			i++
		default:
			w.kind = a
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readClass is tc_class_modify's loop.
func (w *tcWant) readClass(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var err error
		switch {
		case a == "dev":
			_, err = tcNext(args, i)
			i++
		case a == "classid":
			var v string
			if v, err = tcNext(args, i); err == nil {
				w.handle, err = tcClassid(v)
			}
			i++
		case a == "handle":
			return errors.New(`tc refuses "handle" on a class`)
		case a == "root":
			w.parent = tcHRoot
		case a == "parent":
			var v string
			if v, err = tcNext(args, i); err == nil {
				w.parent, err = tcClassid(v)
			}
			i++
		case tcMatches(a, "estimator"):
			i += 2
		case tcMatches(a, "help"):
			return errTcOutOfScope
		default:
			w.kind = a
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// readFilter is tc_filter_modify's loop.
func (w *tcWant) readFilter(args []string) error {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var err error
		var v string
		switch {
		case a == "dev", a == "handle":
			_, err = tcNext(args, i)
			i++
		case tcMatches(a, "block"):
			if v, err = tcNext(args, i); err == nil {
				w.block, err = tcU32Arg(v)
			}
			i++
		case a == "root":
			w.parent = tcHRoot
		case a == "ingress":
			w.parent = tcHFilterIn
		case a == "egress":
			w.parent = tcHFilterOut
		case a == "parent":
			if v, err = tcNext(args, i); err == nil {
				w.parent, err = tcClassid(v)
			}
			i++
		case tcMatches(a, "preference"), tcMatches(a, "priority"):
			var n uint32
			if v, err = tcNext(args, i); err == nil {
				if n, err = tcU32Arg(v); err == nil && n > 0xFFFF {
					err = fmt.Errorf("invalid priority value %q", v)
				}
			}
			w.prio = n
			i++
		case tcMatches(a, "protocol"):
			if v, err = tcNext(args, i); err == nil {
				w.proto, w.protoKnown, err = tcProto(v)
			}
			i++
		case tcMatches(a, "chain"):
			var n uint32
			if v, err = tcNext(args, i); err == nil {
				n, err = tcU32Arg(v)
			}
			w.chain = &n
			i++
		case tcMatches(a, "estimator"):
			i += 2
		case tcMatches(a, "help"):
			return errTcOutOfScope
		default:
			w.kind = a
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// tcRequestProblem is O3: the request tc sent against what its command line
// asked for.
func tcRequestProblem(w tcWant, ti TcInfo) error {
	var errs []error
	if ti.Kind != w.kind {
		errs = append(errs, fmt.Errorf("TCA_KIND %q, want %q", ti.Kind, w.kind))
	}
	parent := w.parent
	if w.block != 0 {
		parent = w.block
		if ti.Hdr.Ifindex != TcmIfindexMagicBlock {
			errs = append(errs, fmt.Errorf("ifindex %#x, want TCM_IFINDEX_MAGIC_BLOCK for block %d", ti.Hdr.Ifindex, w.block))
		}
	}
	if ti.Hdr.Parent != parent {
		errs = append(errs, fmt.Errorf("tcm_parent %#x, want %#x", ti.Hdr.Parent, parent))
	}
	if !w.filter && ti.Hdr.Handle != w.handle {
		errs = append(errs, fmt.Errorf("tcm_handle %#x, want %#x", ti.Hdr.Handle, w.handle))
	}
	if !w.filter {
		if w.inBlock != 0 && (!ti.HasIngressBlock || ti.IngressBlock != w.inBlock) {
			errs = append(errs, fmt.Errorf("TCA_INGRESS_BLOCK %d (present %v), want %d", ti.IngressBlock, ti.HasIngressBlock, w.inBlock))
		}
		if w.outBlock != 0 && (!ti.HasEgressBlock || ti.EgressBlock != w.outBlock) {
			errs = append(errs, fmt.Errorf("TCA_EGRESS_BLOCK %d (present %v), want %d", ti.EgressBlock, ti.HasEgressBlock, w.outBlock))
		}
		return errors.Join(errs...)
	}
	if uint32(ti.FilterPrio) != w.prio {
		errs = append(errs, fmt.Errorf("prio %d, want %d", ti.FilterPrio, w.prio))
	}
	if w.protoKnown && uint32(ti.FilterProtocol) != w.proto {
		errs = append(errs, fmt.Errorf("protocol %#04x, want %#04x", ti.FilterProtocol, w.proto))
	}
	switch {
	case w.chain == nil && ti.HasChain:
		errs = append(errs, fmt.Errorf("TCA_CHAIN %d, but the command names no chain", ti.Chain))
	case w.chain != nil && (!ti.HasChain || ti.Chain != *w.chain):
		errs = append(errs, fmt.Errorf("TCA_CHAIN %d (present %v), want %d", ti.Chain, ti.HasChain, *w.chain))
	}
	return errors.Join(errs...)
}

// tcDumpHas is O4: whether a dump holds the qdisc a request made. A
// request's zero parent or handle leaves it to the kernel (it picks a
// handle; a change found by handle alone keeps its parent), so a zero is
// not compared.
func tcDumpHas(req TcInfo, dump []TcInfo) bool {
	for i := range dump {
		d := &dump[i]
		if d.Kind == req.Kind && d.Hdr.Ifindex == req.Hdr.Ifindex &&
			(req.Hdr.Parent == 0 || d.Hdr.Parent == req.Hdr.Parent) &&
			(req.Hdr.Handle == 0 || d.Hdr.Handle == req.Hdr.Handle) {
			return true
		}
	}
	return false
}

// wsTcRequests returns the parsed requests of type typ in msgs.
func wsTcRequests(t *testing.T, msgs []capturedNlMsg, typ uint16, request bool) []TcInfo {
	t.Helper()
	var out []TcInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		ti, err := ParseTcMsg(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ti)
	}
	return out
}

// TestWsTcCorpus runs O6 over every workbench row, and O3 and O4 over the
// rows whose command is one tc modification. It fails when no row was in
// O3's scope, so a corpus or model change that quietly empties it shows.
func TestWsTcCorpus(t *testing.T) {
	dirs := wsRows(t)
	if len(dirs) == 0 {
		t.Fatal("no workbench rows under testdata/*/ws")
	}
	var checked, roundTrips int
	for _, dir := range dirs {
		row := wsLoadRow(t, dir)
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it; it has no captures to read
			}
			ip := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
			files := map[string][]capturedNlMsg{"ip.pcap": ip}
			for _, rb := range row.Readback {
				if rb.Proto == "" || rb.Proto == "route" {
					name := "netlink_route_" + rb.Name + ".pcap"
					files[name] = pcapNlMsgs(t, filepath.Join(dir, name))
				}
			}
			for name, msgs := range files {
				for i, m := range msgs {
					if m.proto != unix.NETLINK_ROUTE {
						continue
					}
					if err := wsTcMsgProblem(m.h.Type, m.body); err != nil {
						t.Errorf("O6: %s message %d (type %d): %v", name, i, m.h.Type, err)
					}
				}
			}
			w, ok, err := tcWantFromCmd(row.Cmd)
			if !ok {
				return
			}
			reqs := wsTcRequests(t, ip, w.typ, true)
			if len(reqs) == 0 {
				if wsReadRC(t, dir) == 0 {
					t.Errorf("O3: rc 0 but no request of type %d\n  cmd: %s", w.typ, row.Cmd)
				}
				return
			}
			if err != nil {
				t.Fatalf("O3: tc sent a request, but the model says it refuses: %v\n  cmd: %s", err, row.Cmd)
			}
			if len(reqs) != 1 {
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), w.typ, row.Cmd)
			}
			checked++
			if err := tcRequestProblem(w, reqs[0]); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
			if w.typ != unix.RTM_NEWQDISC || wsReadRC(t, dir) != 0 {
				return
			}
			dump := wsTcRequests(t, files["netlink_route_verify.pcap"], unix.RTM_NEWQDISC, false)
			if len(dump) == 0 {
				return
			}
			roundTrips++
			if !tcDumpHas(reqs[0], dump) {
				t.Errorf("O4: the verify dump's %d qdiscs lack kind %q parent %#x handle %#x on ifindex %d\n  cmd: %s",
					len(dump), reqs[0].Kind, reqs[0].Hdr.Parent, reqs[0].Hdr.Handle, reqs[0].Hdr.Ifindex, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no request: is the model reading any command?")
	}
}

// wsDirBytes is the size of every file under root.
func wsDirBytes(t *testing.T, root string) int64 {
	t.Helper()
	var n int64
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		n += fi.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// wsBudgetBytes is Part 4's size budget: what one kernel version's ws/
// may hold, in file bytes (what git stores), not disk blocks.
const wsBudgetBytes = 20 << 20

// TestWsSizeBudget holds each kernel version's workbench corpus to the
// budget, so a rows file that grows past it is noticed when it is captured.
func TestWsSizeBudget(t *testing.T) {
	roots, err := filepath.Glob(filepath.Join("testdata", "*", "ws"))
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 {
		t.Fatal("no testdata/*/ws")
	}
	for _, root := range roots {
		if n := wsDirBytes(t, root); n > wsBudgetBytes {
			t.Errorf("%s holds %d bytes, over the %d-byte budget: pack the largest family (coverage-status.md, Workbench)", root, n, wsBudgetBytes)
		} else {
			t.Logf("%s: %d of %d bytes", root, n, wsBudgetBytes)
		}
	}
}

func TestTcQdiscHandle(t *testing.T) {
	cases := []struct {
		description string
		in          string
		expected    uint32
		wantErr     bool
	}{
		{"positive: a major with its colon", "1:", 0x10000, false},
		{"positive: hex without 0x", "ffff:", 0xFFFF0000, false},
		{"positive: a bare major", "10", 0x100000, false},
		{"corner: a minor after the colon is ignored, as tc ignores it", "1:5", 0x10000, false},
		{"corner: 0x is taken as strtoul takes it", "0x10:", 0x100000, false},
		{"positive: none", "none", 0, false},
		{"boundary: the largest major", "ffff", 0xFFFF0000, false},
		{"boundary: one past the largest major", "10000:", 0, true},
		{"negative: not a number", "x:", 0, true},
		{"negative: junk after the major", "1g", 0, true},
		{"negative: empty", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tcQdiscHandle(tc.in)
			if (err != nil) != tc.wantErr || got != tc.expected {
				t.Errorf("tcQdiscHandle(%q) = %#x, %v; want %#x, error %v", tc.in, got, err, tc.expected, tc.wantErr)
			}
		})
	}
}

func TestTcClassid(t *testing.T) {
	cases := []struct {
		description string
		in          string
		expected    uint32
		wantErr     bool
	}{
		{"positive: major and minor", "1:10", 0x10010, false},
		{"positive: root", "root", tcHRoot, false},
		{"positive: none", "none", 0, false},
		{"positive: the minor omitted", "1:", 0x10000, false},
		{"positive: the major omitted", ":5", 0x5, false},
		{"corner: a bare number is taken unshifted", "10", 0x10, false},
		{"positive: the ingress parent", "ffff:fff1", tcHIngress, false},
		{"boundary: the largest minor", "1:ffff", 0x1FFFF, false},
		{"boundary: one past the largest minor", "1:10000", 0, true},
		{"boundary: one past the largest major", "10000:1", 0, true},
		{"negative: junk after the minor", "1:2x", 0, true},
		{"negative: not a number", "zz", 0, true},
		{"negative: empty", "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tcClassid(tc.in)
			if (err != nil) != tc.wantErr || got != tc.expected {
				t.Errorf("tcClassid(%q) = %#x, %v; want %#x, error %v", tc.in, got, err, tc.expected, tc.wantErr)
			}
		})
	}
}

func TestTcProto(t *testing.T) {
	cases := []struct {
		description string
		in          string
		expected    uint32
		known       bool
		wantErr     bool
	}{
		{"positive: ip", "ip", 0x0800, true, false},
		{"positive: names compare without case", "802.1Q", 0x8100, true, false},
		{"positive: all", "all", tcEthPAll, true, false},
		{"positive: a hex number", "0x88cc", 0x88CC, true, false},
		{"boundary: the largest protocol", "0xffff", 0xFFFF, true, false},
		{"boundary: one past the largest protocol", "0x10000", 0, false, true},
		{"corner: a name outside the table is left unchecked", "ieeepup", 0, false, false},
		{"negative: a malformed number", "0x8g", 0, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, known, err := tcProto(tc.in)
			if (err != nil) != tc.wantErr || got != tc.expected || known != tc.known {
				t.Errorf("tcProto(%q) = %#x, %v, %v; want %#x, %v, error %v", tc.in, got, known, err, tc.expected, tc.known, tc.wantErr)
			}
		})
	}
}

func TestTcWantFromCmd(t *testing.T) {
	seven := uint32(7)
	cases := []struct {
		description string
		cmd         string
		expected    tcWant
		ok          bool
		wantErr     bool
	}{
		{"positive: a root qdisc with a handle", "tc qdisc add dev d0 handle 1: root fq",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "fq", handle: 0x10000, parent: tcHRoot}, true, false},
		{"positive: ingress sets parent, kind and handle, and ends the loop", "tc qdisc add dev d0 ingress",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "ingress", handle: tcHIngressMaj, parent: tcHIngress}, true, false},
		{"positive: clsact overrides an earlier handle", "tc qdisc replace dev d0 handle 5: clsact",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "clsact", handle: tcHIngressMaj, parent: tcHIngress}, true, false},
		{"positive: a delete with no kind", "tc qdisc del dev d0 root",
			tcWant{typ: unix.RTM_DELQDISC, parent: tcHRoot}, true, false},
		{"positive: shared blocks on a qdisc", "tc qdisc add dev d0 ingress_block 21 egress_block 22 clsact",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "clsact", handle: tcHIngressMaj, parent: tcHIngress, inBlock: 21, outBlock: 22}, true, false},
		{"positive: an estimator's two arguments are skipped", "tc qdisc add dev d0 root est 1sec 8sec sfq",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "sfq", parent: tcHRoot}, true, false},
		{"positive: a class", "tc class add dev d0 parent 1: classid 1:10 htb rate 1mbit",
			tcWant{typ: unix.RTM_NEWTCLASS, kind: "htb", handle: 0x10010, parent: 0x10000}, true, false},
		{"positive: a new filter defaults to protocol all", "tc filter add dev d0 parent ffff: prio 3 u32 match ip src 1.1.1.1",
			tcWant{typ: unix.RTM_NEWTFILTER, kind: "u32", parent: 0xFFFF0000, filter: true, prio: 3, proto: tcEthPAll, protoKnown: true}, true, false},
		{"positive: change leaves protocol 0", "tc filter change dev d0 ingress pref 1 handle 1 flower",
			tcWant{typ: unix.RTM_NEWTFILTER, kind: "flower", parent: tcHFilterIn, filter: true, prio: 1, protoKnown: true}, true, false},
		{"positive: protocol, chain and egress", "tc filter add dev d0 egress protocol ipv6 chain 7 matchall",
			tcWant{typ: unix.RTM_NEWTFILTER, kind: "matchall", parent: tcHFilterOut, filter: true, proto: 0x86DD, protoKnown: true, chain: &seven}, true, false},
		{"positive: a filter on a shared block", "tc filter add block 22 protocol ip pref 25 flower",
			tcWant{typ: unix.RTM_NEWTFILTER, kind: "flower", filter: true, prio: 25, proto: 0x0800, protoKnown: true, block: 22}, true, false},
		{"positive: global flags before the object", "tc -s -j qdisc add dev d0 root prio",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "prio", parent: tcHRoot}, true, false},
		{"corner: an abbreviated object and verb", "tc q a dev d0 root fq",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "fq", parent: tcHRoot}, true, false},
		{"corner: a kind longer than FILTER_NAMESZ is cut as tc cuts it", "tc qdisc add dev d0 root abcdefghijklmnopq",
			tcWant{typ: unix.RTM_NEWQDISC, kind: "abcdefghijklmno", parent: tcHRoot}, true, false},
		{"boundary: the largest prio", "tc filter del dev d0 prio 65535",
			tcWant{typ: unix.RTM_DELTFILTER, filter: true, prio: 65535, protoKnown: true}, true, false},
		{"boundary: one past the largest prio", "tc filter del dev d0 prio 65536",
			tcWant{typ: unix.RTM_DELTFILTER, filter: true, prio: 65536, protoKnown: true}, true, true},
		{"negative: a qdisc handle tc refuses", "tc qdisc add dev d0 handle 10000: root fq",
			tcWant{typ: unix.RTM_NEWQDISC}, true, true},
		{"negative: handle on a class", "tc class add dev d0 handle 1:1 htb",
			tcWant{typ: unix.RTM_NEWTCLASS}, true, true},
		{"negative: a keyword left without its argument", "tc qdisc add dev d0 parent",
			tcWant{typ: unix.RTM_NEWQDISC}, true, true},
		{"corner: a show is out of scope", "tc qdisc show dev d0", tcWant{}, false, false},
		{"corner: actions are out of scope", "tc actions add action pass index 1", tcWant{}, false, false},
		{"corner: chain is out of scope", "tc chain add dev d0 ingress chain 1", tcWant{}, false, false},
		{"corner: a batch is out of scope", "tc -b /tmp/x", tcWant{}, false, false},
		{"corner: an option with an argument is out of scope", "tc -n ns qdisc add dev d0 root fq", tcWant{}, false, false},
		{"corner: a pipeline is out of scope", "tc qdisc add dev d0 root fq | cat", tcWant{}, false, false},
		{"corner: a quoted argument is out of scope", "tc filter add dev d0 basic match 'u32(u32 1 0xff at 0)'", tcWant{}, false, false},
		{"corner: stab is out of scope", "tc qdisc add dev d0 root stab overhead 4 fq", tcWant{}, false, false},
		{"corner: not tc", "ip link add d0 type dummy", tcWant{}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, ok, err := tcWantFromCmd(tc.cmd)
			if ok != tc.ok || (err != nil) != tc.wantErr {
				t.Fatalf("ok %v err %v; want ok %v, error %v", ok, err, tc.ok, tc.wantErr)
			}
			if !ok || tc.wantErr {
				return
			}
			if !tcWantEqual(got, tc.expected) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// tcWantEqual compares two tcWants, chain by value.
func tcWantEqual(a, b tcWant) bool {
	ac, bc := a, b
	ac.chain, bc.chain = nil, nil
	return ac == bc && (a.chain == nil) == (b.chain == nil) && (a.chain == nil || *a.chain == *b.chain)
}

// TestTcRequestProblem holds O3 to hand-built requests, so a corpus row
// passing means it looked: each wrong field must be named.
func TestTcRequestProblem(t *testing.T) {
	seven := uint32(7)
	qdisc := tcWant{typ: unix.RTM_NEWQDISC, kind: "fq", handle: 0x10000, parent: tcHRoot}
	qdiscMsg := TcInfo{Hdr: Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: tcHRoot}, Kind: "fq"}
	filter := tcWant{typ: unix.RTM_NEWTFILTER, kind: "u32", filter: true, parent: 0xFFFF0000, prio: 3, proto: 0x0800, protoKnown: true, chain: &seven}
	filterMsg := TcInfo{Hdr: Tcmsg{Ifindex: 3, Parent: 0xFFFF0000, Handle: 0x800}, FilterPrio: 3, FilterProtocol: 0x0800, Kind: "u32", Chain: 7, HasChain: true}
	with := func(ti TcInfo, f func(*TcInfo)) TcInfo {
		f(&ti)
		return ti
	}
	cases := []struct {
		description string
		want        tcWant
		msg         TcInfo
		expected    string // a substring of the problem, "" for none
	}{
		{"positive: a qdisc request that agrees", qdisc, qdiscMsg, ""},
		{"positive: a filter request that agrees, its handle not compared", filter, filterMsg, ""},
		{"negative: the wrong kind", qdisc, with(qdiscMsg, func(ti *TcInfo) { ti.Kind = "fq_codel" }), "TCA_KIND"},
		{"negative: the wrong parent", qdisc, with(qdiscMsg, func(ti *TcInfo) { ti.Hdr.Parent = tcHIngress }), "tcm_parent"},
		{"negative: the wrong qdisc handle", qdisc, with(qdiscMsg, func(ti *TcInfo) { ti.Hdr.Handle = 0x20000 }), "tcm_handle"},
		{"negative: prio in the wrong half of tcm_info", filter, with(filterMsg, func(ti *TcInfo) { ti.FilterPrio = 0 }), "prio 0, want 3"},
		{"negative: protocol not in network order", filter, with(filterMsg, func(ti *TcInfo) { ti.FilterProtocol = 0x0008 }), "protocol"},
		{"negative: a chain the command named is missing", filter, with(filterMsg, func(ti *TcInfo) { ti.HasChain = false }), "TCA_CHAIN"},
		{"negative: a chain the command did not name", func() tcWant { w := filter; w.chain = nil; return w }(), filterMsg, "names no chain"},
		{"negative: a block filter without the magic ifindex",
			func() tcWant { w := filter; w.block = 22; w.chain = nil; return w }(),
			with(filterMsg, func(ti *TcInfo) { ti.Hdr.Parent = 22; ti.HasChain = false }), "TCM_IFINDEX_MAGIC_BLOCK"},
		{"positive: a block filter with the magic ifindex and the block as parent",
			func() tcWant { w := filter; w.block = 22; w.chain = nil; return w }(),
			with(filterMsg, func(ti *TcInfo) { ti.Hdr.Parent, ti.Hdr.Ifindex, ti.HasChain = 22, TcmIfindexMagicBlock, false }), ""},
		{"negative: an ingress block the command named is missing",
			func() tcWant { w := qdisc; w.inBlock = 21; return w }(), qdiscMsg, "TCA_INGRESS_BLOCK"},
		{"corner: a protocol name outside the table is not compared",
			func() tcWant { w := filter; w.protoKnown = false; return w }(), with(filterMsg, func(ti *TcInfo) { ti.FilterProtocol = 0x1234 }), ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := tcRequestProblem(tc.want, tc.msg)
			if tc.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("problem %v, want it to name %q", err, tc.expected)
			}
		})
	}
}

// TestWsTcMsgProblem holds O6 to hand-built messages.
func TestWsTcMsgProblem(t *testing.T) {
	hdr := tcmsgHdr(1, 0, tcHRoot, 0, 0)
	act := cat([]byte{0, 0, 0, 0}, rtattr(TcaRootTab, rtattr(1, rtattr(TcaActKind, []byte("gact\x00")))))
	cases := []struct {
		description string
		typ         uint16
		body        []byte
		expected    string // a substring of the problem, "" for none
	}{
		{"positive: a qdisc with known attributes", unix.RTM_NEWQDISC, cat(hdr, rtattr(TcaKind, []byte("fq\x00"))), ""},
		{"positive: an action with known attributes", unix.RTM_NEWACTION, act, ""},
		{"positive: a chain message is a tcmsg", unix.RTM_NEWCHAIN, cat(hdr, rtattr(TcaChain, u32le(1))), ""},
		{"corner: a nested flag on a known type is not unknown", unix.RTM_NEWQDISC, cat(hdr, rtattr(TcaOptions|unix.NLA_F_NESTED, nil)), ""},
		{"corner: a message that is not tc's is let be", unix.RTM_NEWLINK, []byte{1}, ""},
		{"boundary: the last known TCA attribute", unix.RTM_NEWTFILTER, cat(hdr, rtattr(TcaMaxKnown, nil)), ""},
		{"boundary: one past the last known TCA attribute", unix.RTM_NEWTFILTER, cat(hdr, rtattr(TcaMaxKnown+1, nil)), "TCA attribute"},
		{"negative: one past the last TCA_ROOT attribute", unix.RTM_GETACTION,
			cat([]byte{0, 0, 0, 0}, rtattr(TcaRootMaxKnown+1, nil)), "TCA_ROOT attribute"},
		{"negative: one past the last TCA_ACT attribute", unix.RTM_DELACTION,
			cat([]byte{0, 0, 0, 0}, rtattr(TcaRootTab, rtattr(2, rtattr(TcaActMaxKnown+1, nil)))), "TCA_ACT (order 2)"},
		{"negative: a short tcmsg", unix.RTM_GETQDISC, hdr[:TcMsgSizeCst-1], "too small"},
		{"negative: a short tcamsg", unix.RTM_GETACTION, []byte{0}, "too small"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := wsTcMsgProblem(tc.typ, tc.body)
			if tc.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("problem %v, want it to name %q", err, tc.expected)
			}
		})
	}
}

// TestTcDumpHas holds O4 to hand-built dumps.
func TestTcDumpHas(t *testing.T) {
	req := TcInfo{Hdr: Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: tcHRoot}, Kind: "fq"}
	auto := TcInfo{Hdr: Tcmsg{Ifindex: 3, Parent: tcHRoot}, Kind: "fq"}
	got := TcInfo{Hdr: Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: tcHRoot}, Kind: "fq"}
	other := TcInfo{Hdr: Tcmsg{Ifindex: 2, Handle: 0, Parent: tcHRoot}, Kind: "noqueue"}
	cases := []struct {
		description string
		req         TcInfo
		dump        []TcInfo
		expected    bool
	}{
		{"positive: the qdisc among others", req, []TcInfo{other, got}, true},
		{"positive: a request without a handle matches the kernel's choice", auto,
			[]TcInfo{{Hdr: Tcmsg{Ifindex: 3, Handle: 0x80010000, Parent: tcHRoot}, Kind: "fq"}}, true},
		{"negative: another device", req, []TcInfo{{Hdr: Tcmsg{Ifindex: 4, Handle: 0x10000, Parent: tcHRoot}, Kind: "fq"}}, false},
		{"negative: another kind", req, []TcInfo{{Hdr: Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: tcHRoot}, Kind: "fq_codel"}}, false},
		{"negative: another handle", req, []TcInfo{{Hdr: Tcmsg{Ifindex: 3, Handle: 0x20000, Parent: tcHRoot}, Kind: "fq"}}, false},
		{"positive: a change found by handle alone keeps its parent",
			TcInfo{Hdr: Tcmsg{Ifindex: 3, Handle: 0x20000}, Kind: "plug"},
			[]TcInfo{{Hdr: Tcmsg{Ifindex: 3, Handle: 0x20000, Parent: 0x10001}, Kind: "plug"}}, true},
		{"negative: another parent", req, []TcInfo{{Hdr: Tcmsg{Ifindex: 3, Handle: 0x10000, Parent: 0x10001}, Kind: "fq"}}, false},
		{"boundary: an empty dump", req, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if g := tcDumpHas(tc.req, tc.dump); g != tc.expected {
				t.Errorf("tcDumpHas = %v, want %v", g, tc.expected)
			}
		})
	}
}

// wsTcSeedsMax caps the corpus messages each fuzz target is seeded with:
// the seeds also run in every plain go test, and a few hundred distinct
// bodies cover the corpus's shapes.
const wsTcSeedsMax = 400

// wsTcSeeds returns distinct tc message bodies from the workbench corpus
// (every row's command and route readback captures) whose type is in
// types, at most wsTcSeedsMax, in corpus order.
func wsTcSeeds(tb testing.TB, types map[uint16]bool) [][]byte {
	tb.Helper()
	seen := map[string]bool{}
	var out [][]byte
	for _, dir := range wsRows(tb) {
		pcaps, err := filepath.Glob(filepath.Join(dir, "*.pcap"))
		if err != nil {
			tb.Fatal(err)
		}
		for _, p := range pcaps {
			if base := filepath.Base(p); base != "ip.pcap" && !strings.HasPrefix(base, "netlink_route_") {
				continue
			}
			for _, m := range pcapNlMsgs(tb, p) {
				if m.proto != unix.NETLINK_ROUTE || !types[m.h.Type] || seen[string(m.body)] {
					continue
				}
				seen[string(m.body)] = true
				out = append(out, m.body)
				if len(out) == wsTcSeedsMax {
					return out
				}
			}
		}
	}
	return out
}

// FuzzParseTcMsg is O7 for tcmsg: seeded from the corpus, it must never
// panic, and a parse that succeeds owns its byte fields, so a later write
// to the message does not reach them.
func FuzzParseTcMsg(f *testing.F) {
	for _, b := range wsTcSeeds(f, wsTcmsgTypes) {
		f.Add(b)
	}
	f.Add(tcmsgHdr(1, 0x10000, tcHRoot, 0, 0))
	f.Fuzz(func(t *testing.T, in []byte) {
		body := slices.Clone(in) // the engine's input must not be written
		ti, err := ParseTcMsg(body)
		if err != nil {
			return
		}
		opts := append([]byte(nil), ti.Options...)
		for i := range body {
			body[i] ^= 0xFF
		}
		if !slices.Equal(opts, ti.Options) {
			t.Fatal("TCA_OPTIONS aliases the message")
		}
		if strings.IndexByte(ti.Kind, 0) >= 0 {
			t.Fatalf("kind %q holds a NUL", ti.Kind)
		}
	})
}

// FuzzParseTcAction is O7 for tcamsg, held to the same rules.
func FuzzParseTcAction(f *testing.F) {
	for _, b := range wsTcSeeds(f, wsTcActionTypes) {
		f.Add(b)
	}
	f.Add(cat([]byte{0, 0, 0, 0}, rtattr(TcaRootTab, rtattr(1, rtattr(TcaActKind, []byte("gact\x00"))))))
	f.Fuzz(func(t *testing.T, in []byte) {
		body := slices.Clone(in) // the engine's input must not be written
		ai, err := ParseTcAction(body)
		if err != nil {
			return
		}
		var before [][]byte
		for _, a := range ai.Actions {
			before = append(before, append([]byte(nil), a.Options...), append([]byte(nil), a.Cookie...))
		}
		for i := range body {
			body[i] ^= 0xFF
		}
		for i, a := range ai.Actions {
			if !slices.Equal(before[2*i], a.Options) || !slices.Equal(before[2*i+1], a.Cookie) {
				t.Fatalf("action %d's options or cookie alias the message", i)
			}
			if strings.IndexByte(a.Kind, 0) >= 0 {
				t.Fatalf("action %d's kind %q holds a NUL", i, a.Kind)
			}
		}
	})
}
