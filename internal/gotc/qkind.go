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

// tcBuiltinKinds is every qdisc_util id the tc binary links (tc/q_*.c at
// v7.2.0): one gotc does not print is refused rather than misprinted. A
// kind outside this list is dlsym's miss, which tc prints with
// print_noqopt.
var tcBuiltinKinds = map[string]bool{
	"cake": true, "cake_mq": true, "cbs": true, "choke": true, "clsact": true, "codel": true,
	"drr": true, "dualpi2": true, "etf": true, "ets": true, "bfifo": true, "pfifo": true,
	"pfifo_head_drop": true, "pfifo_fast": true, "fq": true, "fq_codel": true, "fq_pie": true,
	"gred": true, "hfsc": true, "hhf": true, "htb": true, wordIngress: true, "mqprio": true,
	"multiq": true, "netem": true, "pie": true, "plug": true, "prio": true, "qfq": true,
	"red": true, "sfb": true, "sfq": true, "skbprio": true, "taprio": true, "tbf": true,
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
}

// qdiscKind is get_qdisc_kind: a printed kind's util, print_noqopt's for a
// kind tc does not link (noqueue, mq), and errNotImplemented for one it
// links that gotc does not print.
func qdiscKind(kind string) (*qdiscUtil, error) {
	if q, ok := qdiscUtils[kind]; ok {
		return q, nil
	}
	if tcBuiltinKinds[kind] {
		return nil, errKind(kind)
	}
	return &qdiscUtil{id: kind, printQopt: printNoqopt}, nil
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
