package gotc

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/f_bpf.c bpf_print_opt (iproute2 v7.2.0), cBPF only.

// printBpfOpt is f_bpf.c bpf_print_opt: the u32 handle prints "%#llx" (a
// JSON string) and the classid is keyed "flowid" (AC66); unlike the bpf
// action, the ops have no space after them.
func printBpfOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	const kind = "bpf filter"
	if opt == nil {
		return nil
	}
	tb := r.nested(opt, xtcpnl.TcaBpfMaxKnown)
	f, err := xtcpnl.ParseTcFopt(xtcpnl.TcFKindBpf, opt[:validAttrPrefix(opt)])
	if err != nil {
		return err
	}
	for _, c := range []struct {
		t    uint16
		what string
	}{{xtcpnl.TcaBpfClassid, wordClassid}, {xtcpnl.TcaBpfFlags, "flags"}, {xtcpnl.TcaBpfFlagsGen, "gen flags"}} {
		if err := attrNeed(kind, c.what, tb.Bytes(c.t), 4); err != nil {
			return err
		}
	}
	if err := bpfProgCheck(kind, f.Bpf, tb, xtcpnl.TcaBpfID); err != nil {
		return err
	}
	if handle != 0 {
		o.hex0x(printAny, "handle", "handle "+cHashX(uint64(handle))+" ", uint64(handle))
	}
	if b := tb.Bytes(xtcpnl.TcaBpfClassid); b != nil {
		c := r.classid(rtaU32(b))
		o.str(printAny, colorNone, "flowid", "flowid "+c+" ", c)
	}
	if f.Bpf.HasName {
		o.str(printAny, colorNone, "bpf_name", f.Bpf.Name+" ", f.Bpf.Name)
	}
	if b := tb.Bytes(xtcpnl.TcaBpfFlags); b != nil && rtaU32(b)&xtcpnl.TcaBpfFlagActDirect != 0 {
		o.boolean(printAny, "direct-action", "direct-action ", true)
	}
	if b := tb.Bytes(xtcpnl.TcaBpfFlagsGen); b != nil {
		r.clsFlags(rtaU32(b), "", " ")
	}
	if f.Bpf.HasOps {
		r.bpfPrintOps(f.Bpf.Ops)
	}
	if t := tb.Bytes(xtcpnl.TcaBpfTag); t != nil {
		r.bpfTag(t)
	}
	if b := tb.Bytes(xtcpnl.TcaBpfPolice); b != nil {
		if err := r.printFilterPolice(b, true); err != nil {
			return err
		}
	}
	if b := tb.Bytes(xtcpnl.TcaBpfAct); b != nil {
		return r.printActions(b)
	}
	return nil
}
