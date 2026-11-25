package gotc

import (
	"encoding/binary"
	"math/bits"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_nat.c print_nat (iproute2 v7.2.0).

// printNat is print_nat: its kind is keyed "type" and prints " nat ", so
// the order line has two spaces (AC22, AC23); the prefix length is
// 33 - ffs(mask), wrong for a mask that is not contiguous (AC23).
func (r *runner) printNat(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "type", " nat ", xtcpnl.TcActKindNat)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaNatMaxKnown)
	n, err := xtcpnl.ParseTcNat(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !n.HasParms {
		r.errf("Missing nat parameters\n")
		return -1, nil
	}
	if n.ParmsShort(xtcpnl.TcNatSizeCst) {
		return 0, actShort(xtcpnl.TcActKindNat, n.ParmsLen)
	}
	p := n.P
	plen := 0
	if m := binary.BigEndian.Uint32(p.Mask[:]); m != 0 {
		plen = 33 - (bits.TrailingZeros32(m) + 1)
	}
	dir := wordIngress
	if p.Flags&xtcpnl.TcaNatFlagEgress != 0 {
		dir = wordEgress
	}
	o.str(printAny, colorNone, "direction", dir, dir)
	old := inetNtop4(p.OldAddr[:]) + "/" + strconv.Itoa(plen)
	o.str(printAny, colorNone, "old_addr", " "+old, old)
	nw := inetNtop4(p.NewAddr[:])
	o.str(printAny, colorNone, "new_addr", " "+nw, nw)
	r.printActionControl(" ", p.Action, "")
	o.nl()
	r.actTail(p.TcGen, "\t index ", n.TcActCommon)
	o.nl()
	return 0, nil
}
