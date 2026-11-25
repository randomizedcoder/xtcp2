package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// tc/m_skbedit.c print_skbedit (iproute2 v7.2.0).

// skbeditPtypes is if_packet.h's PACKET_HOST..PACKET_OTHERHOST by value.
var skbeditPtypes = [...]string{"host", "broadcast", "multicast", "otherhost"}

// printSkbedit is print_skbedit: QUEUE_MAPPING_MAX and the TXQ_SKBHASH
// flag are ignored, so a hash range prints as one queue_mapping (AC19);
// the scalar attributes are read unchecked, here MIN-copied (AC17).
func (r *runner) printSkbedit(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "skbedit ", xtcpnl.TcActKindSkbedit)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaSkbeditMaxKnown)
	s, err := xtcpnl.ParseTcSkbedit(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !s.HasParms {
		r.errf("Missing skbedit parameters\n")
		return -1, nil
	}
	if s.ParmsShort(xtcpnl.TcGactSizeCst) {
		return 0, actShort(xtcpnl.TcActKindSkbedit, s.ParmsLen)
	}
	tb := s.Tb
	if tb.Has(xtcpnl.TcaSkbeditQueueMapping) {
		v := uint32(rtaU16(tb.Bytes(xtcpnl.TcaSkbeditQueueMapping)))
		o.uint(printAny, "queue_mapping", "queue_mapping "+u32s(v), v)
	}
	if tb.Has(xtcpnl.TcaSkbeditPriority) {
		c := r.classid(attrU32(tb, xtcpnl.TcaSkbeditPriority))
		o.str(printAny, colorNone, "priority", " priority "+c, c)
	}
	if tb.Has(xtcpnl.TcaSkbeditMark) {
		v := attrU32(tb, xtcpnl.TcaSkbeditMark)
		o.uint(printAny, "mark", " mark "+u32s(v), v)
	}
	if tb.Has(xtcpnl.TcaSkbeditMask) {
		v := attrU32(tb, xtcpnl.TcaSkbeditMask)
		o.hex(printAny, "mask", "/"+cHashX(uint64(v)), v)
	}
	if tb.Has(xtcpnl.TcaSkbeditPtype) {
		v := rtaU16(tb.Bytes(xtcpnl.TcaSkbeditPtype))
		if int(v) < len(skbeditPtypes) {
			o.str(printAny, colorNone, "ptype", " ptype "+skbeditPtypes[v], skbeditPtypes[v])
		} else {
			o.uint(printAny, "ptype", " ptype "+u32s(uint32(v)), uint32(v))
		}
	}
	if tb.Has(xtcpnl.TcaSkbeditFlags) && rtaU64(tb.Bytes(xtcpnl.TcaSkbeditFlags))&xtcpnl.SkbeditFInheritDsfield != 0 {
		o.null(printAny, "inheritdsfield", " inheritdsfield")
	}
	r.printActionControl(" ", s.P.Action, "")
	o.nl()
	r.actTail(s.P, "\t index ", s.TcActCommon)
	o.nl()
	return 0, nil
}
