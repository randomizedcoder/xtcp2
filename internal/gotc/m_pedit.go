package gotc

import (
	"fmt"
	"math/bits"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_pedit.c print_pedit and print_pedit_location (iproute2 v7.2.0).

// peditHtypeStr is m_pedit.c pedit_htype_str.
var peditHtypeStr = [...]string{"", "eth", wordIPv4, wordIPv6, wordTCP, wordUDP}

// printPedit is print_pedit: the kind prints " %s ", so the line reads
// `action order 0:  pedit` (AC44); "keys %d\n" ends in a hard newline even
// under -o (AC39), as does each key's "\n\t key #%d  at " with its double
// space (AC42); text says val where JSON says "set" (AC43). tc walks
// sel->nkeys keys and KEY_EX children unchecked (AC40, AC45); gotc
// refuses those. With nkeys 0 tc fprintfs straight to stdout, inside -j's
// JSON (AC41), which gotc refuses; text prints it.
func (r *runner) printPedit(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", " pedit ", xtcpnl.TcActKindPedit)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaPeditMaxKnown)
	p, err := xtcpnl.ParseTcPedit(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !p.HasParms {
		r.errf("Missing pedit parameters\n")
		return -1, nil
	}
	if p.ParmsShort(xtcpnl.TcPeditSelSizeCst) {
		return 0, actShort(xtcpnl.TcActKindPedit, p.ParmsLen)
	}
	if p.Ex && (!p.HasKeysEx || p.KeysExRet != 0) {
		r.errf("Netlink error\n")
		return -1, nil
	}
	if p.KeysShort {
		return 0, actUnchecked(xtcpnl.TcActKindPedit, fmt.Sprintf("nkeys %d past the parameters", p.Nkeys))
	}
	for i, k := range p.KeysEx {
		if !k.HtypeOK || !k.CmdOK {
			return 0, actUnchecked(xtcpnl.TcActKindPedit, fmt.Sprintf("key %d without a full HTYPE and CMD", i))
		}
	}
	if p.Nkeys == 0 && o.isJSON() {
		return 0, fmt.Errorf("%w: pedit of no keys (tc writes its note into the JSON)", errNotImplemented)
	}
	r.printActionControl("action ", p.P.Action, " ")
	n := uint32(p.Nkeys)
	o.uint(printAny, "nkeys", "keys "+u32s(n)+"\n", n)
	r.actTail(p.P, " \t index ", p.TcActCommon)
	o.openArray(printJSON, "keys")
	for i, k := range p.Keys {
		htype, cmd := uint16(xtcpnl.PeditHtypeNetwork), uint16(xtcpnl.PeditCmdSet)
		if p.Ex {
			htype, cmd = p.KeysEx[i].Htype, p.KeysEx[i].Cmd
		}
		o.openObject("")
		o.fp("\n\t key #" + strconv.Itoa(i) + "  at ")
		r.peditLocation(htype, cint.Int(k.Off))
		word, jword := "val", "set"
		if cmd != 0 {
			word, jword = "add", "add"
		}
		o.fp(": " + word)
		o.str(printJSON, colorNone, "cmd", "", jword)
		val, mask := bits.ReverseBytes32(k.Val), bits.ReverseBytes32(k.Mask)
		o.hex(printAny, "val", fmt.Sprintf(" %08x", val), val)
		o.hex(printAny, "mask", fmt.Sprintf(" mask %08x", mask), mask)
		o.closeObject()
	}
	if p.Nkeys == 0 {
		o.raw(fmt.Sprintf("\npedit %x keys %d is not LEGIT", p.P.Index, p.Nkeys))
	}
	o.closeArray(printJSON, " ")
	o.nl()
	return 0, nil
}

// peditLocation is print_pedit_location: an htype past the table prints
// "unknown(%d)" (AC43).
func (r *runner) peditLocation(htype uint16, off int32) {
	o := r.o
	if htype == xtcpnl.PeditHtypeNetwork {
		o.str(printJSON, colorNone, "htype", "", "network")
		o.int(printAny, "offset", i32s(off), off)
		return
	}
	s := "unknown(" + strconv.Itoa(int(htype)) + ")"
	if int(htype) < len(peditHtypeStr) {
		s = peditHtypeStr[htype]
	}
	o.str(printAny, colorNone, "htype", s, s)
	o.int(printAny, "offset", fmt.Sprintf("%+d", off), off)
}
