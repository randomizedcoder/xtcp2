package gotc

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/tc_class.c tc_class_list, print_class and the -g graph
// (graph_node_add, graph_indent, graph_cls_show).

// classFilter is tc_class.c's statics. filter_qdisc and filter_classid are
// reset each list; filter_ifindex never is (Q30). The graph lists are the
// static hlist heads root_cls_list and cls_list.
type classFilter struct {
	ifindex uint32
	qdisc   uint32
	classid uint32

	roots, others *graphNode
	// stale is set once a graph has been shown: tc frees its nodes but
	// keeps root_cls_list pointing at the first, so the next -g line in
	// the process walks freed memory and aborts (Q34).
	stale bool
	// err is an out-of-slice kind met while drawing the graph.
	err error
}

// graphNode is struct graph_node with its hlist links.
type graphNode struct {
	next, prev *graphNode // hlist.next; prev stands in for pprev
	head       **graphNode
	id, parent uint32
	parentNode *graphNode
	rightNode  *graphNode
	data       []byte
	nodesCount int
	freed      bool // a freed node's hlist.next is the allocator's pointer, never NULL
}

// hasNext is `node->hlist.next != NULL`: a freed node's first word is
// glibc's (safe-linked) free-list pointer, which is never NULL.
func (n *graphNode) hasNext() bool { return n.next != nil || n.freed }

func listAddHead(head **graphNode, n *graphNode) {
	n.next, n.prev, n.head = *head, nil, head
	if *head != nil {
		(*head).prev = n
	}
	*head = n
}

func listDel(n *graphNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else if n.head != nil {
		*n.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	}
	n.next, n.prev, n.head = nil, nil, nil
}

// classList is tc_class_list.
func (r *runner) classList(args []string) error {
	f := &r.cf
	f.qdisc, f.classid = 0, 0
	dev := ""
	var parent uint32
	for ; len(args) > 0; args = args[1:] {
		a := args[0]
		var err error
		switch {
		case a == wordDev, a == wordQdisc, a == "classid":
			args, err = r.classArg(f, &dev, a, args)
		case a == wordRoot:
			if parent != 0 {
				r.errf("Error: \"root\" is duplicate parent ID\n")
				return retCode(-1)
			}
			parent = tcHRoot
		case a == "parent":
			if parent != 0 {
				return r.duparg("parent", a)
			}
			if len(args) < 2 {
				return r.incomplete()
			}
			args = args[1:]
			h, ok := tcClassid(args[0])
			if !ok {
				return r.invarg("invalid parent ID", args[0])
			}
			parent = h
		case matches(a, helpWord):
			r.classUsage()
		default:
			r.errf("What is \"%s\"? Try \"tc class help\".\n", a)
			return retCode(-1)
		}
		if err != nil {
			return err
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
	}
	req, berr := xtcpnl.BuildTcDumpRequest(uint16(unix.RTM_GETTCLASS), ifindex, parent, 0, false, r.nextSeq())
	if err := r.dumpShow(req, berr, r.printClass); err != nil {
		return err
	}
	if r.graph {
		return r.graphShow()
	}
	return nil
}

// classArg is the dev, qdisc and classid arms: NEXT_ARG, duparg, parse.
func (r *runner) classArg(f *classFilter, dev *string, a string, args []string) ([]string, error) {
	if len(args) < 2 {
		return args, r.incomplete()
	}
	args = args[1:]
	v := args[0]
	switch a {
	case wordDev:
		if *dev != "" {
			return args, r.duparg(wordDev, v)
		}
		*dev = truncName(v)
	case wordQdisc:
		if f.qdisc != 0 {
			return args, r.duparg(wordQdisc, v)
		}
		h, ok := qdiscHandle(v)
		if !ok {
			return args, r.invarg("invalid qdisc ID", v)
		}
		f.qdisc = h
	default:
		if f.classid != 0 {
			return args, r.duparg("classid", v)
		}
		h, ok := tcClassid(v)
		if !ok {
			return args, r.invarg("invalid class ID", v)
		}
		f.classid = h
	}
	return args, nil
}

// printClass is print_class. Its kind key is "class" where a qdisc's is
// "kind" (Q21), its stats sit under "stats" where a qdisc's are at the top
// (Q19), and under -g it only collects (Q22: the qdisc and classid filters
// do not apply to the graph).
func (r *runner) printClass(m Msg) (int, error) {
	if m.Type != unix.RTM_NEWTCLASS && m.Type != unix.RTM_DELTCLASS {
		r.errf("Not a class\n")
		return 0, nil
	}
	if len(m.Body) < xtcpnl.TcMsgSizeCst {
		r.errf("Wrong len %d\n", len(m.Body)-xtcpnl.TcMsgSizeCst)
		return -1, nil
	}
	ti, err := xtcpnl.ParseTcMsg(m.Body)
	if err != nil {
		return 0, fmt.Errorf("gotc: a class message: %w", err)
	}
	t := ti.Hdr
	f := &r.cf
	if r.graph {
		n := &graphNode{id: t.Handle, parent: t.Parent, data: m.Body[xtcpnl.TcMsgSizeCst:]}
		if t.Parent == tcHRoot {
			listAddHead(&f.roots, n)
		} else {
			listAddHead(&f.others, n)
		}
		return 0, nil
	}
	if (f.qdisc != 0 && tcHMaj(t.Handle^f.qdisc) != 0) || (f.classid != 0 && t.Handle != f.classid) {
		return 0, nil
	}
	if !hasAttr(ti.Attrs, xtcpnl.TcaKind) {
		r.errf("print_class: NULL kind\n")
		return -1, nil
	}
	q, err := qdiscKind(ti.Kind)
	if err != nil {
		return 0, err
	}
	return 0, r.printClassBody(m, &ti, q)
}

func (r *runner) printClassBody(m Msg, ti *xtcpnl.TcInfo, q *qdiscUtil) error {
	o, t, f := r.o, ti.Hdr, &r.cf
	o.openObject("")
	if m.Type == unix.RTM_DELTCLASS {
		o.null(printAny, "deleted", "deleted ")
	}
	abuf := ""
	if t.Handle != 0 {
		abuf = r.classidUnder(t.Handle)
	}
	o.str(printAny, colorNone, "class", "class "+ti.Kind+" ", ti.Kind)
	o.str(printAny, colorNone, "handle", abuf+" ", abuf)
	if f.ifindex == 0 {
		if err := r.devname(printAny, t.Ifindex); err != nil {
			return err
		}
	}
	if t.Parent == tcHRoot {
		o.boolean(printAny, wordRoot, "root ", true)
	} else {
		p := r.classidUnder(t.Parent)
		o.str(printAny, colorNone, "parent", "parent "+p+" ", p)
	}
	if t.Info != 0 {
		o.hex0x(printAny, "leaf", "leaf "+u32hex(t.Info>>16)+": ", uint64(t.Info>>16))
	}
	if hasAttr(ti.Attrs, xtcpnl.TcaOptions) {
		if q.printCopt != nil {
			q.printCopt(r, q, ti.Options, true)
		} else {
			r.errf("[cannot parse class parameters]")
		}
	}
	o.nl()
	if r.stats > 0 {
		o.openObject("stats")
		var xs []byte
		var hasX bool
		if ti.HasStats || ti.HasStats2 {
			var err error
			if xs, hasX, err = r.tcStats(ti, " "); err != nil {
				return err
			}
			o.fp("\n")
		}
		if !hasX && ti.HasXstats {
			xs, hasX = ti.Xstats, true
		}
		if hasX && q.printXstats != nil {
			q.printXstats(r, xs)
			o.fp("\n")
		}
		o.closeObject()
	}
	o.closeObject()
	return nil
}

// classidUnder is print_class's classid: the minor alone under a qdisc
// filter.
func (r *runner) classidUnder(h uint32) string {
	if r.cf.qdisc != 0 {
		return r.classid(tcHMin(h))
	}
	return r.classid(h)
}

// graphShow is graph_cls_show from root_cls_list, the text written
// straight to stdout, inside -j's array too (Q22).
func (r *runner) graphShow() error {
	f := &r.cf
	if f.stale && f.roots != nil {
		return r.graphAbort()
	}
	if f.roots == nil {
		return nil
	}
	var buf strings.Builder
	f.err = nil
	r.graphLevel(&buf, &f.roots)
	f.stale = true
	return f.err
}

// graphAbort is Q34: the second graph of a process walks the first's freed
// nodes, glibc aborts with a double free, and stdout's buffer is lost.
func (r *runner) graphAbort() error {
	r.o.w.Reset(r.o.under)
	r.errf("free(): double free detected in tcache 2\n")
	return errAbort
}

// errAbort is abort()'s SIGABRT as a shell reports it: 128 + 6.
const errAbort = exitCode(134)

// graphLevel is one graph_cls_show call over list.
func (r *runner) graphLevel(buf *strings.Builder, list **graphNode) {
	f := &r.cf
	for cls := *list; cls != nil; {
		nextCls := cls.next
		var children *graphNode
		for c := f.others; c != nil; {
			nc := c.next
			if cls.id == c.parent {
				listDel(c)
				listAddHead(&children, c)
				cls.nodesCount++
				c.parentNode = cls
			}
			c = nc
		}
		r.graphNodeShow(buf, cls)
		r.graphLevel(buf, &children)
		if !cls.hasNext() {
			graphIndent(buf, cls, false, 0)
			buf.WriteString("\n")
		}
		r.o.raw(buf.String())
		buf.Reset()
		cls.freed = true
		cls = nextCls
	}
}

// graphNodeShow is graph_cls_show's per-class body up to its recursion.
func (r *runner) graphNodeShow(buf *strings.Builder, cls *graphNode) {
	o := r.o
	graphIndent(buf, cls, false, 0)
	id := r.classid(cls.id)
	buf.WriteString("+---(" + id + ")")
	tb := r.attrTable(cls.data, xtcpnl.TcaMaxKnown, unix.NLA_F_NESTED)
	kind, ok := tb.Str(xtcpnl.TcaKind)
	if !ok {
		buf.WriteString(" [unknown qdisc kind] ")
	} else {
		buf.WriteString(" " + kind + " ")
		o.raw(buf.String())
		buf.Reset()
		q, err := qdiscKind(kind)
		if err != nil {
			r.cf.err, q = err, nil
		}
		if q != nil && q.printCopt != nil {
			q.printCopt(r, q, tb.Bytes(xtcpnl.TcaOptions), tb.Has(xtcpnl.TcaOptions))
		}
		if q != nil && r.stats > 0 {
			r.graphStats(buf, cls, q, id, tb)
		}
	}
	o.raw(buf.String() + "\n")
	buf.Reset()
}

func (r *runner) graphStats(buf *strings.Builder, cls *graphNode, q *qdiscUtil, id string, tb xtcpnl.AttrTable) {
	graphIndent(buf, cls, true, len(q.id)-2+len(id))
	if tb.Has(xtcpnl.TcaStats) || tb.Has(xtcpnl.TcaStats2) {
		r.o.raw("\n")
		ti := xtcpnl.TcInfo{
			Stats: tb.Bytes(xtcpnl.TcaStats), HasStats: tb.Has(xtcpnl.TcaStats),
			Stats2: tb.Bytes(xtcpnl.TcaStats2), HasStats2: tb.Has(xtcpnl.TcaStats2),
		}
		if _, _, err := r.tcStats(&ti, buf.String()); err != nil {
			r.errf("%v\n", err)
		}
		buf.Reset()
	}
	if cls.hasNext() || cls.nodesCount > 0 {
		buf.WriteString("\n")
		graphIndent(buf, cls, true, 0)
	}
}

// graphIndent is graph_indent, right_node links and all: they persist, so
// a later walk can run past the node it started from.
func graphIndent(buf *strings.Builder, node *graphNode, newline bool, spaces int) {
	for node != nil && node.parentNode != nil {
		node.parentNode.rightNode = node
		node = node.parentNode
	}
	for node != nil && node.rightNode != nil {
		if node.hasNext() {
			buf.WriteString("|    ")
		} else {
			buf.WriteString("     ")
		}
		node = node.rightNode
	}
	if newline && node != nil {
		switch {
		case node.hasNext() && node.nodesCount > 0:
			buf.WriteString("|    |")
		case node.hasNext():
			buf.WriteString("|     ")
		case node.nodesCount > 0:
			buf.WriteString("     |")
		default:
			buf.WriteString("      ")
		}
	}
	if spaces > 0 {
		buf.WriteString(strings.Repeat(" ", spaces))
	}
}
