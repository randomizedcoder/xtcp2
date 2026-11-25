package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// tc/m_mirred.c print_mirred (iproute2 v7.2.0).

// actTail is the line most kinds end with: index (after the kind's own
// prefix), " ref %d", " bind %d", then print_tm under -s. The caller
// prints the newlines around it.
func (r *runner) actTail(g xtcpnl.TcGen, index string, c xtcpnl.TcActCommon) {
	r.actTailFmt(g, c, index, "", " ref ", "", " bind ")
}

// actTailSpaced is simple's and skbmod's: "\t index %u ", "ref %d ",
// "bind %d" (AC26).
func (r *runner) actTailSpaced(g xtcpnl.TcGen, c xtcpnl.TcActCommon) {
	r.actTailFmt(g, c, "\t index ", " ", "ref ", " ", "bind ")
}

func (r *runner) actTailFmt(g xtcpnl.TcGen, c xtcpnl.TcActCommon, index, indexPost, ref, refPost, bind string) {
	o := r.o
	o.uint(printAny, "index", index+u32s(g.Index)+indexPost, g.Index)
	o.int(printAny, "ref", ref+i32s(g.Refcnt)+refPost, g.Refcnt)
	o.int(printAny, "bind", bind+i32s(g.Bindcnt), g.Bindcnt)
	if r.stats > 0 && c.HasTm {
		r.printTm(c.Tm)
	}
}

// wordUnknown is what mirred_n2a and its siblings print for an eaction
// they do not know.
const wordUnknown = "unknown"

// mirredN2A, mirredDirection and mirredAction are m_mirred.c's mirred_n2a,
// mirred_direction and mirred_action.
func mirredN2A(a int32) string {
	switch a {
	case xtcpnl.TcaEgressRedirCst:
		return "Egress Redirect"
	case xtcpnl.TcaIngressRedirCst:
		return "Ingress Redirect"
	case xtcpnl.TcaEgressMirrorCst:
		return "Egress Mirror"
	case xtcpnl.TcaIngressMirrorCst:
		return "Ingress Mirror"
	}
	return wordUnknown
}

func mirredDirection(a int32) string {
	switch a {
	case xtcpnl.TcaEgressRedirCst, xtcpnl.TcaEgressMirrorCst:
		return wordEgress
	case xtcpnl.TcaIngressRedirCst, xtcpnl.TcaIngressMirrorCst:
		return wordIngress
	}
	return wordUnknown
}

func mirredAction(a int32) string {
	switch a {
	case xtcpnl.TcaEgressRedirCst, xtcpnl.TcaIngressRedirCst:
		return "redirect"
	case xtcpnl.TcaEgressMirrorCst, xtcpnl.TcaIngressMirrorCst:
		return "mirror"
	}
	return wordUnknown
}

// printMirred is print_mirred: ll_index_to_name never returns NULL, so a
// redirect to a deleted device prints "if%u" (AC18); BLOCKID is read
// unchecked, here MIN-copied (AC17).
func (r *runner) printMirred(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "mirred ", xtcpnl.TcActKindMirred)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaMirredMaxKnown) // its "!!!Deficit" line
	m, err := xtcpnl.ParseTcMirred(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !m.HasParms {
		r.errf("Missing mirred parameters\n")
		return -1, nil
	}
	if m.ParmsShort(xtcpnl.TcMirredSizeCst) {
		return 0, actShort(xtcpnl.TcActKindMirred, m.ParmsLen)
	}
	p := m.P
	dev, err := r.indexToName(p.Ifindex)
	if err != nil {
		return 0, err
	}
	o.fp("(" + mirredN2A(p.Eaction))
	o.str(printJSON, colorNone, "mirred_action", "", mirredAction(p.Eaction))
	o.str(printJSON, colorNone, "direction", "", mirredDirection(p.Eaction))
	if m.Tb.Has(xtcpnl.TcaMirredBlockid) {
		b := attrU32(m.Tb, xtcpnl.TcaMirredBlockid)
		o.uint(printAny, "to_blockid", " to blockid "+u32s(b)+")", b)
	} else {
		o.str(printAny, colorNone, "to_dev", " to device "+dev+")", dev)
	}
	r.printActionControl(" ", p.Action, "")
	o.nl()
	r.actTail(p.TcGen, "\tindex ", m.TcActCommon)
	o.nl()
	return 0, nil
}
