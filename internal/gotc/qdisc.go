package gotc

import (
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/tc_qdisc.c tc_qdisc_list and print_qdisc, with the argument errors
// of lib/utils.c they reach.

// ifNameSize is IFNAMSIZ: `dev` is strncpy'd into a buffer of it, so a
// longer name keeps its first 15 bytes.
const ifNameSize = 16

// qdiscFilter is tc_qdisc.c's statics filter_ifindex, filter_parent and
// filter_handle. tc never resets them, so a batch line inherits the last
// one's (Q29).
type qdiscFilter struct {
	ifindex uint32
	parent  uint32
	handle  uint32
}

// incomplete is incomplete_command: exit(-1).
func (r *runner) incomplete() error {
	r.errf("Command line is not complete. Try option \"help\"\n")
	return exitCode(-1)
}

// invarg is lib/utils.c invarg: exit(-1).
func (r *runner) invarg(msg, arg string) error {
	r.errf("Error: argument \"%s\" is wrong: %s\n", arg, msg)
	return exitCode(-1)
}

// duparg is lib/utils.c duparg: exit(-1).
func (r *runner) duparg(key, arg string) error {
	r.errf("Error: duplicate \"%s\": \"%s\" is the second value.\n", key, arg)
	return exitCode(-1)
}

// strtoul16 is glibc strtoul(s, &p, 16): leading space, a sign, an "0x"
// with a hex digit after it, then digits; the rest is what p points at.
// ok is false when no digit was read (p == s). Overflow saturates.
func strtoul16(s string) (v uint64, rest string, ok bool) {
	t := skipSpace(s)
	neg := false
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg = t[0] == '-'
		t = t[1:]
	}
	if len(t) > 2 && t[0] == '0' && (t[1]|0x20) == 'x' && hexVal(t[2]) >= 0 {
		t = t[2:]
	}
	i, over := 0, false
	for ; i < len(t) && hexVal(t[i]) >= 0; i++ {
		d8, _ := cint.U8(hexVal(t[i]))
		d := uint64(d8)
		if v > (math.MaxUint64-d)/16 {
			over = true
		}
		v = v*16 + d
	}
	if i == 0 {
		return 0, s, false
	}
	switch {
	case over:
		v = math.MaxUint64
	case neg:
		v = -v
	}
	return v, t[i:], true
}

// qdiscHandle is get_qdisc_handle.
func qdiscHandle(s string) (uint32, bool) {
	if s == wordNone {
		return 0, true
	}
	maj, rest, ok := strtoul16(s)
	if !ok || maj >= 1<<16 || (rest != "" && rest[0] != ':') {
		return 0, false
	}
	return uint32(maj) << 16, true
}

// tcClassid is get_tc_classid.
func tcClassid(s string) (uint32, bool) {
	switch s {
	case wordRoot:
		return tcHRoot, true
	case wordNone:
		return 0, true
	}
	maj, rest, ok := strtoul16(s)
	if !ok {
		maj, rest = 0, s
		if rest == "" || rest[0] != ':' {
			return 0, false
		}
	}
	if rest == "" {
		return uint32(maj & math.MaxUint32), true // *h = maj, truncated
	}
	if rest[0] != ':' {
		return 0, false
	}
	if maj >= 1<<16 {
		return 0, false
	}
	minor, tail, _ := strtoul16(rest[1:])
	if tail != "" || minor >= 1<<16 {
		return 0, false
	}
	return uint32(maj)<<16 | uint32(minor), true
}

// qdiscList is tc_qdisc_list.
func (r *runner) qdiscList(args []string) error {
	dev := ""
	invisible := false
	f := &r.qf
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		switch {
		case a == wordDev:
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			dev = truncName(args[0])
		case a == wordRoot, a == wordIngress, a == "clsact":
			if err := r.qdiscOneOf(f, a); err != nil {
				return err
			}
			f.parent = tcHRoot
			if a != wordRoot {
				f.parent = tcHIngress
			}
		case matches(a, wordParent), matches(a, "handle"):
			if err := r.qdiscOneOf(f, a); err != nil {
				return err
			}
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			if matches(a, wordParent) {
				h, ok := tcClassid(args[0])
				if !ok {
					return r.invarg("invalid parent ID", args[0])
				}
				f.parent = h
			} else {
				h, ok := qdiscHandle(args[0])
				if !ok {
					return r.invarg("invalid handle ID", args[0])
				}
				f.handle = h
			}
		case matches(a, helpWord):
			r.qdiscUsage() // and on to the dump (Q25)
		case a == "invisible":
			invisible = true
		default:
			r.errf("What is \"%s\"? Try \"tc qdisc help\".\n", a)
			return retCode(-1)
		}
	}
	var ifindex uint32
	if dev != "" {
		i, err := r.nameToIndex(dev)
		if err != nil {
			return err
		}
		if i == 0 {
			return r.nodev(dev)
		}
		ifindex, f.ifindex = i, i
	} else if err := r.initMap(); err != nil {
		return err
	}
	req, berr := xtcpnl.BuildTcDumpRequest(uint16(unix.RTM_GETQDISC), ifindex, f.parent, f.handle, invisible, r.nextSeq())
	return r.dumpShow(req, berr, r.printQdisc)
}

// qdiscOneOf is the shared test of root, ingress, clsact, parent and
// handle: one of them per line, and the statics remember the last line's.
func (r *runner) qdiscOneOf(f *qdiscFilter, a string) error {
	switch {
	case f.parent != 0:
		return r.invarg("parent is already specified", a)
	case f.handle != 0:
		return r.invarg("handle is already specified", a)
	}
	return nil
}

// truncName is strncpy(d, arg, IFNAMSIZ-1).
func truncName(s string) string {
	if len(s) > ifNameSize-1 {
		return s[:ifNameSize-1]
	}
	return s
}

// dumpShow is the tail both lists share: new_json_obj, rtnl_dump_filter,
// "Dump terminated" and return 1 on failure, delete_json_obj.
func (r *runner) dumpShow(req []byte, berr error, filter func(Msg) (int, error)) error {
	r.o.newJSONObj()
	var ferr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		n, err := filter(m)
		if err != nil {
			ferr = err
			return -1
		}
		return n
	})
	if err == nil {
		err = ferr
	}
	if err != nil {
		return err
	}
	if rc < 0 {
		r.errf("Dump terminated\n")
		r.o.deleteJSONObj()
		return retCode(1)
	}
	r.o.deleteJSONObj()
	return nil
}

// printQdisc is print_qdisc.
func (r *runner) printQdisc(m Msg) (int, error) {
	if m.Type != unix.RTM_NEWQDISC && m.Type != unix.RTM_DELQDISC {
		r.errf("Not a qdisc\n")
		return 0, nil
	}
	if len(m.Body) < xtcpnl.TcMsgSizeCst {
		r.errf("Wrong len %d\n", len(m.Body)-xtcpnl.TcMsgSizeCst)
		return -1, nil
	}
	ti, err := xtcpnl.ParseTcMsg(m.Body)
	if err != nil {
		return 0, fmt.Errorf("gotc: a qdisc message: %w", err)
	}
	t := ti.Hdr
	f := r.qf
	if (f.ifindex != 0 && f.ifindex != t.Ifindex) || (f.handle != 0 && f.handle != t.Handle) ||
		(f.parent != 0 && f.parent != t.Parent) {
		return 0, nil
	}
	if !hasAttr(ti.Attrs, xtcpnl.TcaKind) {
		r.errf("print_qdisc: NULL kind\n")
		return -1, nil
	}
	q, err := qdiscKind(qdiscKindName(ti.Kind))
	if err != nil {
		return 0, err
	}
	return 0, r.printQdiscBody(m, &ti, q)
}

// qdiscKindName is print_qdisc's lookup name: pfifo_fast is printed by
// prio's printer.
func qdiscKindName(kind string) string {
	if kind == xtcpnl.TcKindPfifoFast {
		return xtcpnl.TcKindPrio
	}
	return kind
}

func hasAttr(attrs []uint16, t uint16) bool {
	for _, a := range attrs {
		if a == t {
			return true
		}
	}
	return false
}

// printQdiscEvent is print_qdisc's event words. replaced and added both
// need NLM_F_CREATE, which qdisc_notify never sends: its flags are 0 or
// NLM_F_REPLACE (MO6, K19), so only a crafted event reaches them.
func printQdiscEvent(o *out, m Msg) {
	if m.Type == unix.RTM_DELQDISC {
		o.boolean(printAny, "deleted", "deleted ", true)
	}
	if m.Type == unix.RTM_NEWQDISC && m.Flags&unix.NLM_F_CREATE != 0 && m.Flags&unix.NLM_F_REPLACE != 0 {
		o.boolean(printAny, "replaced", "replaced ", true)
	}
	if m.Type == unix.RTM_NEWQDISC && m.Flags&unix.NLM_F_CREATE != 0 && m.Flags&unix.NLM_F_EXCL != 0 {
		o.boolean(printAny, "added", "added ", true)
	}
}

func (r *runner) printQdiscBody(m Msg, ti *xtcpnl.TcInfo, q *qdiscUtil) error {
	o, t := r.o, ti.Hdr
	o.openObject("")
	printQdiscEvent(o, m)
	o.str(printAny, colorNone, "kind", "qdisc "+ti.Kind, ti.Kind)
	h := u32hex(t.Handle>>16) + ":"
	o.str(printAny, colorNone, "handle", " "+h, h)
	if r.raw > 0 {
		o.fp(fmt.Sprintf("[%08x]", t.Handle))
	}
	o.fp(" ")
	if r.qf.ifindex == 0 {
		if err := r.devname(printAny, t.Ifindex); err != nil {
			return err
		}
	}
	if t.Parent == tcHRoot {
		o.boolean(printAny, wordRoot, "root ", true)
	} else if t.Parent != 0 {
		p := r.classid(t.Parent)
		o.str(printAny, colorNone, wordParent, "parent "+p+" ", p)
	}
	if t.Info != 1 {
		o.uint(printAny, "refcnt", "refcnt "+u32s(t.Info)+" ", t.Info)
	}
	if ti.HasHwOffload && ti.HwOffload != 0 {
		o.boolean(printAny, "offloaded", "offloaded ", true)
	}
	if ti.HasIngressBlock && ti.IngressBlock != 0 {
		o.uint(printAny, "ingress_block", "ingress_block "+u32s(ti.IngressBlock)+" ", ti.IngressBlock)
	}
	if ti.HasEgressBlock && ti.EgressBlock != 0 {
		o.uint(printAny, "egress_block", "egress_block "+u32s(ti.EgressBlock)+" ", ti.EgressBlock)
	}
	o.openObject("options")
	if hasAttr(ti.Attrs, xtcpnl.TcaOptions) {
		q.printQopt(r, q, ti.Options, true)
		if err := r.takeKindErr(); err != nil {
			return err
		}
	}
	o.closeObject()
	o.nl()
	if r.details > 0 && ti.HasStab {
		if err := r.printSizeTable(ti.Stab); err != nil {
			return err
		}
		o.nl()
	}
	if r.stats > 0 {
		var xs []byte
		var hasX bool
		if ti.HasStats || ti.HasStats2 || ti.HasXstats {
			var err error
			if xs, hasX, err = r.tcStats(ti, " "); err != nil {
				return err
			}
			o.nl()
		}
		if hasX && q.printXstats != nil {
			q.printXstats(r, xs)
			if err := r.takeKindErr(); err != nil {
				return err
			}
			o.nl()
		}
	}
	r.extMsg(ti)
	o.closeObject()
	return nil
}

// u32hex is "%x".
func u32hex(v uint32) string { return fmt.Sprintf("%x", v) }

// extMsg is print_ext_msg.
func (r *runner) extMsg(ti *xtcpnl.TcInfo) {
	if !hasAttr(ti.Attrs, xtcpnl.TcaExtWarnMsg) {
		return
	}
	r.o.str(printAny, colorNone, "warn", ti.ExtWarnMsg, ti.ExtWarnMsg)
	r.o.nl()
}

// printSizeTable is tc_stab.c print_size_table: TCA_STAB_BASE MIN-copied
// into a zeroed struct tc_sizespec, then each nonzero field. The table
// itself (TCA_STAB_DATA) is not printed.
func (r *runner) printSizeTable(stab []byte) error {
	tb := r.nested(stab, xtcpnl.TcaStabMaxKnown)
	if !tb.Has(xtcpnl.TcaStabBase) {
		return nil
	}
	var buf [xtcpnl.TcSizespecSizeCst]byte
	copy(buf[:], tb.Bytes(xtcpnl.TcaStabBase))
	s, err := xtcpnl.DecodeTcSizespec(buf[:])
	if err != nil {
		return err
	}
	o := r.o
	o.openObject("stab")
	o.fp(" ")
	if s.Linklayer != 0 {
		ll := wordUnknown // sprint_linklayer of an unsigned int
		if s.Linklayer <= 2 {
			ll = sprintLinklayer(uint8(s.Linklayer))
		}
		o.str(printAny, colorNone, "linklayer", "linklayer "+ll+" ", ll)
	}
	if s.Overhead != 0 {
		o.int(printAny, "overhead", "overhead "+i32s(s.Overhead)+" ", s.Overhead)
	}
	for _, f := range []struct {
		key string
		v   uint32
	}{{"mpu", s.Mpu}, {"mtu", s.Mtu}, {"tsize", s.Tsize}} {
		if f.v != 0 {
			o.uint(printAny, f.key, f.key+" "+u32s(f.v)+" ", f.v)
		}
	}
	o.closeObject()
	return nil
}
