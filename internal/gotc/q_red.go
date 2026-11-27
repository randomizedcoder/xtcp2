package gotc

import (
	"encoding/binary"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_red.c red_print_opt and red_print_xstats, tc/q_choke.c
// choke_print_opt and choke_print_xstats, tc/tc_red.c tc_red_print_flags
// and tc/tc_qevent.c's qevents_read and qevents_print.

// redFlags are tc_red_print_flags' bits, in its order.
var redFlags = [...]struct {
	bit  uint32
	name string
}{{1, wordEcn}, {2, "harddrop"}, {4, "adaptive"}, {8, "nodrop"}}

// printRedFlags is tc_red_print_flags: JSON always has the four bools, text
// only the true ones (Q87).
func printRedFlags(o *out, flags uint32) {
	for _, f := range redFlags {
		on := flags&f.bit != 0
		text := ""
		if on {
			text = f.name + " "
		}
		o.boolean(printAny, f.name, text, on)
	}
}

// redParms is red_print_opt's and choke_print_opt's head: PARMS present
// and at least a struct tc_red_qopt, else the printer returns -1 having
// printed nothing; MAX_P read when a u32.
func redParms(tb xtcpnl.AttrTable, parms, maxPType uint16) (q xtcpnl.TcRedQopt, maxP uint32, ok bool) {
	q, err := xtcpnl.DecodeTcRedQopt(tb.Bytes(parms))
	if err != nil {
		return q, 0, false
	}
	if v, has := tb.U32(maxPType); has {
		maxP = v
	}
	return q, maxP, true
}

// printRedDetails is the show_details block both printers share. Its
// Scell_log has no trailing space, so a qevent after it runs on (Q66).
func (r *runner) printRedDetails(q xtcpnl.TcRedQopt, maxP uint32) {
	if r.details == 0 {
		return
	}
	o := r.o
	o.uint(printAny, "ewma", "ewma "+u32s(uint32(q.Wlog))+" ", uint32(q.Wlog))
	if maxP != 0 {
		p := float64(maxP) / math.Pow(2, 32)
		o.float(printAny, "probability", "probability "+cG(p)+" ", p)
	} else {
		o.uint(printAny, "Plog", "Plog "+u32s(uint32(q.Plog))+" ", uint32(q.Plog))
	}
	o.uint(printAny, "Scell_log", "Scell_log "+u32s(uint32(q.ScellLog)), uint32(q.ScellLog))
}

// printRedOpt is red_print_opt.
func printRedOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaRedMaxKnown)
	q, maxP, ok := redParms(tb, xtcpnl.TcaRedParms, xtcpnl.TcaRedMaxP)
	if !ok {
		return
	}
	if fb := tb.Bytes(xtcpnl.TcaRedFlags); len(fb) >= xtcpnl.NlaBitfield32SizeCst {
		q.Flags = fb[0] // qopt->flags, an unsigned char, = flags_bf->value
	}
	o := r.o
	o.size(printAny, "limit", "limit ", " ", uint64(q.Limit))
	o.size(printAny, "min", "min ", " ", uint64(q.QthMin))
	o.size(printAny, "max", "max ", " ", uint64(q.QthMax))
	printRedFlags(o, uint32(q.Flags))
	r.printRedDetails(q, maxP)
	r.printRedQevents(tb)
}

// redQevents are red's plain qevents, in qevents[] order.
var redQevents = [...]struct {
	id string
	t  uint16
}{{"early_drop", xtcpnl.TcaRedEarlyDropBlock}, {"mark", xtcpnl.TcaRedMarkBlock}}

// readRedQevents is qevents_init then qevents_read: read_block_idx reads
// each present block's u32 unchecked (a short one is refused), and a zero
// one is "Illegal qevent block index" and the caller's -1 (bad).
func (r *runner) readRedQevents(tb xtcpnl.AttrTable) (blk [len(redQevents)]uint32, bad bool, err error) {
	for i, q := range redQevents {
		if !tb.Has(q.t) {
			continue
		}
		b := tb.Bytes(q.t)
		if err := attrNeed(xtcpnl.TcKindRed, "qevent "+q.id+" block", b, 4); err != nil {
			return blk, true, err
		}
		blk[i] = binary.LittleEndian.Uint32(b)
		if blk[i] == 0 {
			r.errf("Illegal qevent block index\n")
			return blk, true, nil
		}
	}
	return blk, false, nil
}

// printRedQevents is qevents_read then qevents_print.
func (r *runner) printRedQevents(tb xtcpnl.AttrTable) {
	blk, bad, err := r.readRedQevents(tb)
	if err != nil {
		r.refuseKind(err)
	}
	if bad {
		return
	}
	o := r.o
	first := true
	for i, q := range redQevents {
		if blk[i] == 0 {
			continue
		}
		if first {
			o.openArray(printJSON, "qevents")
			first = false
		}
		o.openObject("")
		o.str(printAny, colorNone, "kind", "qevent "+q.id, q.id)
		o.uint(printAny, "block", " block "+u32s(blk[i]), blk[i])
		o.fp(" ")
		o.closeObject()
	}
	if !first {
		o.closeArray(printAny, "")
	}
}

// redHasBlock is red_has_block: qevents_have_block compares every
// qevent's index, an unset one's 0 included. rc is the callback's -1 on
// an illegal block.
func (r *runner) redHasBlock(opt []byte, has bool, block uint32) (found bool, rc int, err error) {
	if !has {
		return false, 0, nil
	}
	blk, bad, err := r.readRedQevents(r.nested(opt, xtcpnl.TcaRedMaxKnown))
	if err != nil || bad {
		return false, -1, err
	}
	for _, b := range blk {
		if b == block {
			found = true
		}
	}
	return found, 0, nil
}

// printRedXstats is red_print_xstats: a short payload is its -1.
func printRedXstats(r *runner, xs []byte) {
	x, err := xtcpnl.ParseTcXstats(xtcpnl.TcKindRed, xs)
	if err != nil {
		return
	}
	st := x.Aqm.Red
	o := r.o
	o.uint(printAny, "marked", "  marked "+u32s(st.Marked)+" ", st.Marked)
	o.uint(printAny, "early", "early "+u32s(st.Early)+" ", st.Early)
	o.uint(printAny, "pdrop", "pdrop "+u32s(st.Pdrop)+" ", st.Pdrop)
	o.uint(printAny, "other", "other "+u32s(st.Other)+" ", st.Other)
}

// printChokeOpt is choke_print_opt: red's, with packet counts for sizes,
// no FLAGS attribute and no qevents.
func printChokeOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaChokeMaxKnown)
	q, maxP, ok := redParms(tb, xtcpnl.TcaChokeParms, xtcpnl.TcaChokeMaxP)
	if !ok {
		return
	}
	o := r.o
	o.uint(printAny, "limit", "limit "+u32s(q.Limit)+"p ", q.Limit)
	o.uint(printAny, "min", "min "+u32s(q.QthMin)+"p ", q.QthMin)
	o.uint(printAny, "max", "max "+u32s(q.QthMax)+"p ", q.QthMax)
	printRedFlags(o, uint32(q.Flags))
	r.printRedDetails(q, maxP)
}

// printChokeXstats is choke_print_xstats: a short payload is its -1.
func printChokeXstats(r *runner, xs []byte) {
	x, err := xtcpnl.ParseTcXstats(xtcpnl.TcKindChoke, xs)
	if err != nil {
		return
	}
	st := x.Aqm.Red
	o := r.o
	o.uint(printAny, "marked", "  marked "+u32s(st.Marked), st.Marked)
	o.uint(printAny, "early", " early "+u32s(st.Early), st.Early)
	o.uint(printAny, "pdrop", " pdrop "+u32s(st.Pdrop), st.Pdrop)
	o.uint(printAny, "other", " other "+u32s(st.Other), st.Other)
	o.uint(printAny, "matched", " matched "+u32s(st.Matched), st.Matched)
}
