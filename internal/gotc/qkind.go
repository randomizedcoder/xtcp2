package gotc

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This is tc.c get_qdisc_kind and the struct qdisc_util hooks of the kinds
// gotc prints. tc's printers' returns are ignored by print_qdisc and
// print_class, so a printer that "returns -1" here just stops.

// qdiscUtil is struct qdisc_util's id and print hooks; a nil hook is a
// NULL one.
type qdiscUtil struct {
	id          string
	printQopt   func(r *runner, q *qdiscUtil, opt []byte, has bool)
	printCopt   func(r *runner, q *qdiscUtil, opt []byte, has bool)
	printXstats func(r *runner, xs []byte)
}

// qdiscUtils are the kinds gotc prints.
var qdiscUtils = map[string]*qdiscUtil{
	xtcpnl.TcKindPrio:          {id: xtcpnl.TcKindPrio, printQopt: printPrioOpt},
	xtcpnl.TcKindPfifoFast:     {id: xtcpnl.TcKindPfifoFast, printQopt: printPrioOpt},
	xtcpnl.TcKindPfifo:         {id: xtcpnl.TcKindPfifo, printQopt: printFifoOpt},
	xtcpnl.TcKindBfifo:         {id: xtcpnl.TcKindBfifo, printQopt: printFifoOpt},
	xtcpnl.TcKindPfifoHeadDrop: {id: xtcpnl.TcKindPfifoHeadDrop, printQopt: printFifoOpt},
	xtcpnl.TcKindFqCodel:       {id: xtcpnl.TcKindFqCodel, printQopt: printFqCodelOpt, printXstats: printFqCodelXstats},
	xtcpnl.TcKindFq:            {id: xtcpnl.TcKindFq, printQopt: printFqOpt, printXstats: printFqXstats},
	xtcpnl.TcKindHtb:           {id: xtcpnl.TcKindHtb, printQopt: printHtbOpt, printCopt: printHtbOpt, printXstats: printHtbXstats},
	xtcpnl.TcKindTbf:           {id: xtcpnl.TcKindTbf, printQopt: printTbfOpt},
	xtcpnl.TcKindSfq:           {id: xtcpnl.TcKindSfq, printQopt: printSfqOpt, printXstats: printSfqXstats},
	xtcpnl.TcKindNetem:         {id: xtcpnl.TcKindNetem, printQopt: printNetemOpt, printXstats: printNetemXstats},
	xtcpnl.TcKindIngress:       {id: xtcpnl.TcKindIngress, printQopt: printIngressOpt},
	xtcpnl.TcKindClsact:        {id: xtcpnl.TcKindClsact, printQopt: func(*runner, *qdiscUtil, []byte, bool) {}},
	xtcpnl.TcKindDrr:           {id: xtcpnl.TcKindDrr, printQopt: printDrrOpt, printCopt: printDrrOpt, printXstats: printDrrXstats},
	xtcpnl.TcKindQfq:           {id: xtcpnl.TcKindQfq, printQopt: printQfqOpt, printCopt: printQfqOpt},
	xtcpnl.TcKindHfsc:          {id: xtcpnl.TcKindHfsc, printQopt: printHfscOpt, printCopt: printHfscClassOpt, printXstats: printHfscXstats},
	xtcpnl.TcKindEts:           {id: xtcpnl.TcKindEts, printQopt: printEtsOpt, printCopt: printEtsCopt},
	xtcpnl.TcKindMqprio:        {id: xtcpnl.TcKindMqprio, printQopt: printMqprioOpt},
	xtcpnl.TcKindTaprio:        {id: xtcpnl.TcKindTaprio, printQopt: printTaprioOpt, printXstats: printTaprioXstats},
	xtcpnl.TcKindMultiq:        {id: xtcpnl.TcKindMultiq, printQopt: printMultiqOpt},
	xtcpnl.TcKindCbs:           {id: xtcpnl.TcKindCbs, printQopt: printCbsOpt},
	xtcpnl.TcKindEtf:           {id: xtcpnl.TcKindEtf, printQopt: printEtfOpt},
	xtcpnl.TcKindRed:           {id: xtcpnl.TcKindRed, printQopt: printRedOpt, printXstats: printRedXstats},
	xtcpnl.TcKindSfb:           {id: xtcpnl.TcKindSfb, printQopt: printSfbOpt, printXstats: printSfbXstats},
	xtcpnl.TcKindCodel:         {id: xtcpnl.TcKindCodel, printQopt: printCodelOpt, printXstats: printCodelXstats},
	xtcpnl.TcKindPie:           {id: xtcpnl.TcKindPie, printQopt: printPieOpt, printXstats: printPieXstats},
	xtcpnl.TcKindFqPie:         {id: xtcpnl.TcKindFqPie, printQopt: printFqPieOpt, printXstats: printFqPieXstats},
	xtcpnl.TcKindHhf:           {id: xtcpnl.TcKindHhf, printQopt: printHhfOpt, printXstats: printHhfXstats},
	xtcpnl.TcKindPlug:          {id: xtcpnl.TcKindPlug, printQopt: printPlugOpt},
	xtcpnl.TcKindSkbprio:       {id: xtcpnl.TcKindSkbprio, printQopt: printSkbprioOpt},
	xtcpnl.TcKindDualpi2:       {id: xtcpnl.TcKindDualpi2, printQopt: printDualpi2Opt, printXstats: printDualpi2Xstats},
	xtcpnl.TcKindCake:          {id: xtcpnl.TcKindCake, printQopt: printCakeOpt, printXstats: printCakeXstats},
	xtcpnl.TcKindCakeMq:        {id: xtcpnl.TcKindCakeMq, printQopt: printCakeOpt, printXstats: printCakeXstats},
	xtcpnl.TcKindGred:          {id: xtcpnl.TcKindGred, printQopt: printGredOpt},
	xtcpnl.TcKindChoke:         {id: xtcpnl.TcKindChoke, printQopt: printChokeOpt, printXstats: printChokeXstats},
}

// refuseKind records a qdisc_util printer's refusal of what tc would read
// unchecked. The print hooks return nothing, as print_qdisc and print_class
// ignore tc's returns, so their callers take the refusal after each hook.
func (r *runner) refuseKind(err error) {
	if r.kindErr == nil {
		r.kindErr = err
	}
}

// takeKindErr is the refusal a hook recorded, cleared.
func (r *runner) takeKindErr() error {
	err := r.kindErr
	r.kindErr = nil
	return err
}

// kindNeed refuses an attribute tc reads n bytes of without testing its
// length; ok is false after a refusal.
func (r *runner) kindNeed(kind, what string, b []byte, n int) bool {
	if err := attrNeed(kind, what, b, n); err != nil {
		r.refuseKind(err)
		return false
	}
	return true
}

// rtaWalk is the RTA_OK/RTA_NEXT loop over data: fn sees each attribute's
// raw type, flags included, and payload, and a malformed attribute ends the
// walk silently.
func rtaWalk(data []byte, fn func(raw uint16, val []byte)) {
	for n := 0; len(data)-n >= xtcpnl.RTAttrSizeCst; {
		l := int(attrLenAt(data, n))
		if l < xtcpnl.RTAttrSizeCst || l > len(data)-n {
			return
		}
		fn(uint16(data[n+2])|uint16(data[n+3])<<8, data[n+xtcpnl.RTAttrSizeCst:n+l])
		n += (l + 3) &^ 3
	}
}

// qdiscKind is get_qdisc_kind: a printed kind's util, which is every
// kind the tc binary links (tc/q_*.c at v7.2.0), and print_noqopt's for
// one it does not (noqueue, mq), dlsym's miss.
func qdiscKind(kind string) *qdiscUtil {
	if q, ok := qdiscUtils[kind]; ok {
		return q
	}
	return &qdiscUtil{id: kind, printQopt: printNoqopt}
}

// printNoqopt is tc.c print_noqopt: "[Unknown qdisc, optlen=N] " for a
// non-empty TCA_OPTIONS, as bare text, so inside -j's JSON too (Q28).
func printNoqopt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if has && len(opt) > 0 {
		r.o.raw("[Unknown qdisc, optlen=" + strconv.Itoa(len(opt)) + "] ")
	}
}

// nested is parse_rtattr_nested(tb, max, opt): NLA_F_NESTED masked, and a
// malformed stream kept up to its bad header, as tc's loop stops there
// (after its "!!!Deficit" line).
func (r *runner) nested(opt []byte, maxType uint16) xtcpnl.AttrTable {
	return r.attrTable(opt, maxType, unix.NLA_F_NESTED)
}

// attrTable is parse_rtattr_flags.
func (r *runner) attrTable(data []byte, maxType, flags uint16) xtcpnl.AttrTable {
	tb, err := xtcpnl.ParseAttrTableFlags(data, maxType, flags)
	if err == nil {
		return tb
	}
	// Keep the attributes before the bad one: re-parse the valid prefix.
	n := validAttrPrefix(data)
	r.errf("!!!Deficit %d, rta_len=%d\n", len(data)-n, attrLenAt(data, n))
	tb, err = xtcpnl.ParseAttrTableFlags(data[:n], maxType, flags)
	if err != nil {
		return xtcpnl.AttrTable{}
	}
	return tb
}

// validAttrPrefix is how many bytes of data RTA_OK accepts before a bad
// header.
func validAttrPrefix(data []byte) int {
	n := 0
	for len(data)-n >= xtcpnl.RTAttrSizeCst {
		l := int(attrLenAt(data, n))
		if l < xtcpnl.RTAttrSizeCst || l > len(data)-n {
			break
		}
		n += (l + 3) &^ 3
	}
	if n > len(data) {
		n = len(data)
	}
	return n
}

// attrLenAt is the rta_len at data[n:], 0 when there is no header.
func attrLenAt(data []byte, n int) uint16 {
	if n < 0 || len(data)-n < 2 {
		return 0
	}
	return uint16(data[n]) | uint16(data[n+1])<<8
}
