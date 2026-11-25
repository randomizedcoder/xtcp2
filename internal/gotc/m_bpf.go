package gotc

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_bpf.c bpf_print_opt and lib/bpf_legacy.c bpf_print_ops (iproute2
// v7.2.0). cBPF only: an eBPF program's info comes from bpf(2)
// (bpf_dump_prog_info), which gotc refuses.

// errBpfProgInfo is the refusal for an ID: tc asks the kernel for the
// program's info with bpf(BPF_OBJ_GET_INFO_BY_FD).
func errBpfProgInfo() error {
	return fmt.Errorf("%w: eBPF program info via bpf(2)", errNotImplemented)
}

// bpfProgCheck refuses what bpf_print_ops and the NAME print would read
// past: OPS_LEN instructions past OPS (AC63), a NAME with no NUL (AC64),
// and an eBPF program's ID.
func bpfProgCheck(kind string, p xtcpnl.TcBpfProg, tb xtcpnl.AttrTable, idT uint16) error {
	if p.HasOps && p.Ops.Short {
		return actUnchecked(kind, fmt.Sprintf("bytecode of %d instructions past its ops", p.Ops.Len))
	}
	if p.HasName && p.NameNoNUL {
		return actUnchecked(kind, "name without its NUL")
	}
	if tb.Has(idT) {
		return errBpfProgInfo()
	}
	return nil
}

// bpfPrintOps is bpf_print_ops: nothing for a length of 0.
func (r *runner) bpfPrintOps(ops xtcpnl.TcBpfOps) {
	o := r.o
	if ops.Len == 0 {
		return
	}
	n := uint32(ops.Len)
	o.openObject("bytecode")
	o.uint(printAny, "length", "bytecode '"+u32s(n)+",", n)
	o.openArray(printJSON, "insns")
	for i, in := range ops.Insns {
		o.openObject("")
		o.uint(printAny, "code", u32s(uint32(in.Code))+" ", uint32(in.Code))
		o.hhu(printAny, "jt", u32s(uint32(in.Jt))+" ", in.Jt)
		o.hhu(printAny, "jf", u32s(uint32(in.Jf))+" ", in.Jf)
		end := ","
		if i == len(ops.Insns)-1 {
			end = "'"
		}
		o.uint(printAny, "k", u32s(in.K)+end, in.K)
		o.closeObject()
	}
	o.closeArray(printJSON, "")
	o.closeObject()
}

// bpfTag is print_hexstring("tag", "tag %s ", ...).
func (r *runner) bpfTag(b []byte) {
	s := hexstringN2A(b)
	r.o.str(printAny, colorNone, "tag", "tag "+s+" ", s)
}

// printActBpf is m_bpf.c bpf_print_opt: the trailing "\n " puts a space
// before the next line's tab (AC65).
func (r *runner) printActBpf(opt []byte, has bool) (int, error) {
	o := r.o
	const kind = xtcpnl.TcActKindBpf
	o.str(printAny, colorNone, "kind", kind+" ", kind)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaActBpfMaxKnown)
	b, err := xtcpnl.ParseTcActBpf(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !b.HasParms {
		r.errf("Missing bpf parameters\n")
		return -1, nil
	}
	if b.ParmsShort(xtcpnl.TcActBpfSizeCst) {
		return 0, actShort(kind, b.ParmsLen)
	}
	if err := bpfProgCheck(kind, b.TcBpfProg, b.Tb, xtcpnl.TcaActBpfID); err != nil {
		return 0, err
	}
	if b.HasName {
		o.str(printAny, colorNone, "bpf_name", b.Name+" ", b.Name)
	}
	if b.HasOps {
		r.bpfPrintOps(b.Ops)
		o.fp(" ")
	}
	if t := b.Tb.Bytes(xtcpnl.TcaActBpfTag); t != nil {
		r.bpfTag(t)
	}
	r.printActionControl("default-action ", b.P.Action, o.sl)
	r.actTail(b.P, "\t index ", b.TcActCommon)
	o.fp("\n ")
	return 0, nil
}
