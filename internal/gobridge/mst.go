package gobridge

import (
	"encoding/binary"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/mst.c: `bridge mst show`.

// mstIDLen is MST_ID_LEN, the msti column's width.
const mstIDLen = 9

// doMst is do_mst.
func (r *runner) doMst(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return r.mstShow(nil)
	}
	switch w := args[0]; {
	case matches(w, "set"):
		return r.refuse("mst set")(nil)
	case showWords(w, false):
		return r.mstShow(args[1:])
	case matches(w, helpWord):
		return r.mstUsage()
	}
	return r.unknownCommand("mst", args[0])
}

// mstUsage is mst.c usage.
func (r *runner) mstUsage() error {
	r.errf("%s", "Usage: bridge mst set dev DEV msti MSTI state STATE\n"+
		"       bridge mst {show} [ dev DEV ]\n")
	return exitCode(255)
}

// mstShow is mst_show: a failed dump returns -1 (exit 255) where the other
// objects exit 1 (B4), after closing the JSON. Being a return, it is the
// one failed dump a -force batch goes past.
func (r *runner) mstShow(args []string) error {
	dev, err := r.showArgs(args, nil)
	if err != nil {
		return err
	}
	if err := r.resolveFilter(dev, &r.mstIndex); err != nil {
		return err
	}
	req, berr := xtcpnl.BuildDumpLinkRequestExt(unix.AF_BRIDGE, xtcpnl.RtextFilterMst, r.nextSeq())
	o := r.o
	o.newJSONObj()
	if !o.isJSON() {
		o.printf(col("port") + "  " + fmt.Sprintf("%-*s", mstIDLen, "msti") + "\n")
	}
	var perr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		v, e := r.printMsts(m)
		if e != nil && perr == nil {
			perr = e
			return -1
		}
		return v
	})
	switch {
	case err != nil:
		return err
	case perr != nil:
		return perr
	case rc < 0:
		r.errf("Dump terminated\n")
		o.deleteJSONObj()
		return retCode(-1)
	}
	o.deleteJSONObj()
	return nil
}

// printMsts is print_msts. A reply without an unflagged IFLA_AF_SPEC ends
// the whole dump, before the dev filter is applied (B10).
func (r *runner) printMsts(m Msg) (int, error) {
	if len(m.Body) < xtcpnl.IfInfomsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.IfInfomsgSizeCst)
		return -1, nil
	}
	spec, ok := xtcpnl.FindRawAttr(m.Body[xtcpnl.IfInfomsgSizeCst:], xtcpnl.IflaAfSpecCst)
	if !ok {
		return -1, nil
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.mstIndex != 0 && r.mstIndex != index {
		return 0, nil
	}
	mst, ok := xtcpnl.FindRawAttr(spec, unix.NLA_F_NESTED|xtcpnl.IflaBridgeMst)
	if !ok {
		return 0, nil
	}
	o := r.o
	opened := false
	var perr error
	err := xtcpnl.WalkRawAttrs(mst, func(raw uint16, val []byte) {
		if perr != nil || raw&xtcpnl.NlaTypeMaskCst != xtcpnl.IflaBridgeMstEntry {
			return
		}
		if !opened {
			name, e := r.indexToName(index)
			if e != nil {
				perr = e
				return
			}
			o.openObject("")
			o.str(printAny, colorIfname, "ifname", col(name)+"  ", name)
			o.openArray(printJSON, "mst")
			opened = true
		} else {
			o.pad("  ")
		}
		perr = r.printMstEntry(val)
	})
	if perr != nil {
		return -1, perr
	}
	if opened {
		o.closeArray(printJSON, "")
		o.closeObject()
	}
	return 0, err
}

// printMstEntry is print_mst_entry; its complaint has no newline (B11).
func (r *runner) printMstEntry(val []byte) error {
	t, err := xtcpnl.ParseAttrTableFlags(val, 2, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if !t.Has(xtcpnl.IflaBridgeMstEntryMsti) || !t.Has(xtcpnl.IflaBridgeMstEntryState) {
		r.errf("BUG: broken MST entry")
		return nil
	}
	msti, _ := t.U16(xtcpnl.IflaBridgeMstEntryMsti)
	state, _ := t.U8(xtcpnl.IflaBridgeMstEntryState)
	o := r.o
	o.openObject("")
	o.uint(printAny, "msti", fmt.Sprint(msti), uint32(msti))
	o.nl()
	o.pad("    ")
	r.printSTPState(state)
	o.nl()
	o.closeObject()
	return nil
}
