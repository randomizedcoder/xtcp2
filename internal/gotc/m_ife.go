package gotc

import (
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_ife.c print_ife (iproute2 v7.2.0).

// cHashXUpper is C's "%#llX": "0X" and upper-case hex digits, except 0,
// which prints "0" (AC51).
func cHashXUpper(v uint64) string {
	if v == 0 {
		return "0"
	}
	return "0X" + strings.ToUpper(strconv.FormatUint(v, 16))
}

// ifeMetas is print_ife's metadata, in its order: a non-empty value is
// read as a u32 or u16 whatever its length (AC53); an empty one prints
// "allow", so the JSON key is a number or a string.
var ifeMetas = []struct {
	t    uint16
	name string
	n    int
}{
	{xtcpnl.IfeMetaSkbmark, wordMark, 4},
	{xtcpnl.IfeMetaTcindex, "tcindex", 2},
	{xtcpnl.IfeMetaPrio, "prio", 4},
}

// printIfe is print_ife: type prints "%#llX" (AC51); the break after it is
// tested before dst and src could set has_optional (AC52), though the
// kernel always sends TYPE (act_ife.c tcf_ife_dump).
func (r *runner) printIfe(opt []byte, has bool) (int, error) {
	o := r.o
	const kind = xtcpnl.TcActKindIfe
	o.str(printAny, colorNone, "kind", kind+" ", kind)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaIfeMaxKnown)
	f, err := xtcpnl.ParseTcIfe(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !f.HasParms {
		r.errf("Missing ife parameters\n")
		return -1, nil
	}
	if f.ParmsShort(xtcpnl.TcIfeSizeCst) {
		return 0, actShort(kind, f.ParmsLen)
	}
	tb := f.Tb
	if err := attrNeed(kind, "type", tb.Bytes(xtcpnl.TcaIfeType), 2); err != nil {
		return 0, err
	}
	if f.HasMeta {
		f.Meta = r.nested(tb.Bytes(xtcpnl.TcaIfeMetalst), xtcpnl.IfeMetaMaxKnown)
		for _, m := range ifeMetas {
			if b := f.Meta.Bytes(m.t); len(b) > 0 {
				if err := attrNeed(kind, m.name, b, m.n); err != nil {
					return 0, err
				}
			}
		}
	}
	for _, m := range []struct {
		t    uint16
		what string
	}{{xtcpnl.TcaIfeDmac, wordDst}, {xtcpnl.TcaIfeSmac, wordSrc}} {
		if b := tb.Bytes(m.t); b != nil && (len(b) == 0 || len(b) > llAddrMaxCst) {
			return 0, actUnchecked(kind, m.what+" of "+strconv.Itoa(len(b))+" bytes")
		}
	}
	mode := "decode"
	if f.Flags&xtcpnl.IfeEncode != 0 {
		mode = "encode"
	}
	o.str(printAny, colorNone, "mode", mode+" ", mode)
	r.printActionControl("action ", f.P.Action, " ")
	hasOptional := false
	if b := tb.Bytes(xtcpnl.TcaIfeType); b != nil {
		v := uint64(rtaU16(b))
		hasOptional = true
		o.hex0x(printAny, "type", "type "+cHashXUpper(v)+" ", v)
	}
	if hasOptional {
		o.fp(o.sl + "\t")
	}
	if f.HasMeta {
		for _, m := range ifeMetas {
			b := f.Meta.Bytes(m.t)
			switch {
			case b == nil:
			case len(b) == 0:
				o.str(printAny, colorNone, m.name, "allow "+m.name+" ", "allow")
			default:
				v := rtaU32(b)
				if m.n == 2 {
					v = uint32(rtaU16(b))
				}
				o.uint(printAny, m.name, "use "+m.name+" "+u32s(v)+" ", v)
			}
		}
	}
	for _, m := range []struct {
		t   uint16
		key string
	}{{xtcpnl.TcaIfeDmac, wordDst}, {xtcpnl.TcaIfeSmac, wordSrc}} {
		if b := tb.Bytes(m.t); b != nil {
			a := llAddrN2A(b)
			o.str(printAny, colorNone, m.key, m.key+" "+a+" ", a)
		}
	}
	o.nl()
	r.actTail(f.P, "\t index ", f.TcActCommon)
	o.nl()
	return 0, nil
}
