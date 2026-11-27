//go:build gotc_write

// gotc_write builds this for gotc's qdisc and class writes. Like goip's
// write builders it is compiled only behind its tag, so the default build
// of this package cannot lay out a tc write at all.

package xtcpnl

import (
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// This file encodes the request `tc qdisc` and `tc class` add, change,
// replace, link and delete send: tc_qdisc_modify (tc/tc_qdisc.c:43) and
// tc_class_modify (tc/tc_class.c:52) at iproute2 v7.2.0. The tcmsg comes
// first, then the attributes in the order those functions write them,
// whatever the argv order was:
//
//	qdisc: TCA_KIND, TCA_RATE, TCA_INGRESS_BLOCK, TCA_EGRESS_BLOCK,
//	       the kind's parse_qopt attributes, TCA_STAB {BASE, DATA}
//	class: TCA_KIND, TCA_RATE, the kind's parse_copt attributes
//
// The kind's own attributes are its parser's business (gotc's q_*.go), so
// they come in through Opts, which writes into the same builder.
//
// Nothing here sends. BuildTcWriteRequest goes through BuildWriteRequest,
// and gotc compares the bytes with a replay or writes them to a file
// (gotc-status.md, increment 17: dry-run, as goip's decision 5).

const (
	// TcQdiscWriteBufCst is TCA_BUF_MAX (tc/tc_common.h:3), the
	// buf[] after the tcmsg in tc_qdisc_modify's request.
	TcQdiscWriteBufCst = 64 * 1024
	// TcClassWriteBufCst is tc_class_modify's char buf[4096]: a class
	// write has a sixteenth of a qdisc write's room (lead W6).
	TcClassWriteBufCst = 4096
	// tcReqHdrCst is sizeof(struct nlmsghdr) + sizeof(struct tcmsg),
	// what addattr_l's maxlen counts before the attributes.
	tcReqHdrCst = NlMsgHdrSizeCst + TcMsgSizeCst
)

var (
	// ErrTcWriteType is a TcWrite whose MsgType is not one of
	// RTM_NEWQDISC, RTM_DELQDISC, RTM_NEWTCLASS or RTM_DELTCLASS.
	ErrTcWriteType = errors.New("xtcpnl: a tc write is RTM_NEW/DELQDISC or RTM_NEW/DELTCLASS")
	// ErrTcWriteStab is a TcWrite whose StabData length is not the
	// sizespec's tsize: tc sends tsize __u16s.
	ErrTcWriteStab = errors.New("xtcpnl: TCA_STAB_DATA is not tsize entries")
)

// TcWrite is one qdisc or class write as tc_qdisc_modify or
// tc_class_modify assembles it.
type TcWrite struct {
	// MsgType is RTM_NEWQDISC or RTM_DELQDISC (qdisc), RTM_NEWTCLASS or
	// RTM_DELTCLASS (class).
	MsgType uint16
	// Flags are the NLM_F_* bits besides NLM_F_REQUEST, as do_qdisc and
	// do_class pass them: EXCL|CREATE for add, 0 for change,
	// CREATE|REPLACE for replace, REPLACE for link, 0 for delete. tc's
	// rtnl_talk adds NLM_F_ACK.
	Flags uint16

	// The struct tcmsg; tcm_family is always AF_UNSPEC.
	Ifindex int32
	Handle  uint32
	Parent  uint32
	Info    uint32

	// Kind is TCA_KIND, sent as strlen+1 when non-empty.
	Kind string
	// Est is TCA_RATE, sent when EwmaLog is non-zero (`if
	// (est.ewma_log)`).
	Est TcEstimator
	// IngressBlock and EgressBlock are sent when non-zero (qdisc only).
	IngressBlock uint32
	EgressBlock  uint32

	// Opts writes the kind's attributes, TCA_OPTIONS and anything beside
	// it, in parse_qopt or parse_copt order. Nil writes none.
	Opts func(ab *AttrBuilder) error

	// HasStab sends TCA_STAB (check_size_table_opts was true): BASE is
	// Stab, and DATA is StabData when it is non-nil.
	HasStab  bool
	Stab     TcSizespec
	StabData []uint16
}

// IsTcQdiscWrite reports whether msgType is a qdisc write.
func IsTcQdiscWrite(msgType uint16) bool {
	return msgType == uint16(unix.RTM_NEWQDISC) || msgType == uint16(unix.RTM_DELQDISC)
}

func isTcClassWrite(msgType uint16) bool {
	return msgType == uint16(unix.RTM_NEWTCLASS) || msgType == uint16(unix.RTM_DELTCLASS)
}

// TcWriteRoom is the attribute room a write of msgType has: the request
// buffer after the tcmsg (TCA_BUF_MAX for a qdisc, 4096 for a class).
func TcWriteRoom(msgType uint16) int {
	if IsTcQdiscWrite(msgType) {
		return TcQdiscWriteBufCst
	}
	return TcClassWriteBufCst
}

// TcMaxlenRoom is the room left under a parser's own addattr maxlen, which
// counts the nlmsghdr and tcmsg: q_prio's addattr_nest_compat(n, 1024, …)
// fails once 16+20+attributes would pass 1024, whatever the buffer is.
func TcMaxlenRoom(maxlen, written int) int {
	return maxlen - tcReqHdrCst - written
}

// EncodeTcMsg lays out a struct tcmsg: family, two pad bytes and a pad
// byte, ifindex, handle, parent, info.
// It returns an array so a request costs no allocation for it.
func EncodeTcMsg(w *TcWrite) [TcMsgSizeCst]byte {
	var arr [TcMsgSizeCst]byte
	b := append(arr[:0], unix.AF_UNSPEC, 0, 0, 0) // tcm_family, tcm__pad1, tcm__pad2
	b = binary.LittleEndian.AppendUint32(b, cint.Bits(w.Ifindex))
	b = binary.LittleEndian.AppendUint32(b, w.Handle)
	b = binary.LittleEndian.AppendUint32(b, w.Parent)
	b = binary.LittleEndian.AppendUint32(b, w.Info)
	return [TcMsgSizeCst]byte(b)
}

// BuildTcWriteRequest lays out w in tc_qdisc_modify's or tc_class_modify's
// order. An attribute that does not fit the buffer is an error, where tc
// prints "addattr_l ERROR" and sends what it has (W6): gotc refuses a
// request tc would truncate rather than send it.
func BuildTcWriteRequest(w *TcWrite, seq uint32) ([]byte, error) {
	qdisc := IsTcQdiscWrite(w.MsgType)
	if !qdisc && !isTcClassWrite(w.MsgType) {
		return nil, ErrTcWriteType
	}
	// One allocation holds the builder and its buffer: Opts is a call the
	// compiler cannot see into, so the builder it is handed escapes.
	r := new(tcWriteBuf)
	r.ab = NewAttrBuilder(r.raw[:TcWriteRoom(w.MsgType)])
	if err := w.putFrame(&r.ab, qdisc); err != nil {
		return nil, err
	}
	hdr := EncodeTcMsg(w)
	return BuildWriteRequest(w.MsgType, w.Flags, seq, hdr[:], r.ab.Bytes())
}

// tcWriteBuf is a request's attribute builder and the buffer it writes,
// sized for the larger (qdisc) room.
type tcWriteBuf struct {
	ab  AttrBuilder
	raw [TcQdiscWriteBufCst]byte
}

func (w *TcWrite) putFrame(ab *AttrBuilder, qdisc bool) error {
	if w.Kind != "" {
		if err := ab.PutString(TcaKind, w.Kind); err != nil {
			return err
		}
	}
	if w.Est.EwmaLog != 0 {
		var e [TcEstimatorSizeCst]byte
		if err := ab.PutBytes(TcaRate, w.Est.Append(e[:0])); err != nil {
			return err
		}
	}
	if qdisc {
		for _, blk := range [...]struct {
			t uint16
			v uint32
		}{{TcaIngressBlock, w.IngressBlock}, {TcaEgressBlock, w.EgressBlock}} {
			if blk.v == 0 {
				continue
			}
			if err := ab.PutU32(blk.t, blk.v); err != nil {
				return err
			}
		}
	}
	if w.Opts != nil {
		if err := w.Opts(ab); err != nil {
			return err
		}
	}
	if qdisc && w.HasStab {
		return w.putStab(ab)
	}
	return nil
}

// putStab is TCA_STAB: a nest of TCA_STAB_BASE (the sizespec) and, when
// tc_calc_size_table made one, TCA_STAB_DATA (tsize __u16s).
func (w *TcWrite) putStab(ab *AttrBuilder) error {
	if w.StabData != nil && len(w.StabData) != int(w.Stab.Tsize) {
		return ErrTcWriteStab
	}
	m, err := ab.BeginNest(TcaStab)
	if err != nil {
		return err
	}
	var base [TcSizespecSizeCst]byte
	if err := ab.PutBytes(TcaStabBase, w.Stab.Append(base[:0])); err != nil {
		return err
	}
	if w.StabData != nil {
		p, err := ab.reserve(TcaStabData, 2*len(w.StabData))
		if err != nil {
			return err
		}
		for i, v := range w.StabData {
			binary.LittleEndian.PutUint16(p[2*i:], v)
		}
	}
	return ab.EndNest(m)
}

// NestCompat is an open addattr_nest_compat: the outer attribute holding a
// struct, and the empty nest after it.
type NestCompat struct {
	outer, inner NestMark
}

// BeginNestCompat is addattr_nest_compat (lib/libnetlink.c:1424): an
// attribute carrying data, then a bare nest header of the same type, both
// to be closed by EndNestCompat. q_prio sends TCA_OPTIONS this way, with
// tc_prio_qopt as the data.
func (a *AttrBuilder) BeginNestCompat(atype uint16, data []byte) (NestCompat, error) {
	start := a.n
	if err := a.PutBytes(atype, data); err != nil {
		return NestCompat{}, err
	}
	in, err := a.BeginNest(atype)
	if err != nil {
		a.n = start
		return NestCompat{}, err
	}
	return NestCompat{outer: NestMark{off: start}, inner: in}, nil
}

// EndNestCompat is addattr_nest_compat_end (lib/libnetlink.c:1434): the
// outer attribute's rta_len becomes everything to the cursor, and the
// inner nest's rta_len everything after its own header, 4 when empty.
func (a *AttrBuilder) EndNestCompat(c NestCompat) error {
	if err := a.EndNest(c.outer); err != nil {
		return err
	}
	return a.EndNest(c.inner)
}
