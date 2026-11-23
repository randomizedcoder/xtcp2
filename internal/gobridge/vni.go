package gobridge

import (
	"encoding/binary"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/vni.c: `bridge vni show`.

// vxlanIDLen is VXLAN_ID_LEN, the vni column's width.
const vxlanIDLen = 17

// doVni is do_vni: strcmp throughout, so `vni sh` is refused.
func (r *runner) doVni(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return r.vniShow(nil)
	}
	switch w := args[0]; {
	case w == "add", w == "delete", w == "del":
		return r.refuse("vni " + w)(nil)
	case showWords(w, true):
		return r.vniShow(args[1:])
	case w == helpWord:
		return r.vniUsage()
	}
	return r.unknownCommand("vni", args[0])
}

// vniUsage is vni.c usage.
func (r *runner) vniUsage() error {
	r.errf("%s", "Usage: bridge vni { add | del } vni VNI\n"+
		"\t\t[ { group | remote } IP_ADDRESS ]\n"+
		"\t\tdev DEV\n"+
		"       bridge vni { show } [ dev DEV ]\n"+
		"\n"+
		"Where:\tVNI\t:= 0-16777215\n")
	return exitCode(255)
}

// vniShow is vni_show: the header before the dump (B6), and its failure
// message's typo (B3).
func (r *runner) vniShow(args []string) error {
	dev, err := r.showArgs(args, nil)
	if err != nil {
		return err
	}
	if err := r.resolveFilter(dev, &r.vniIndex); err != nil {
		return err
	}
	o := r.o
	o.newJSONObj()
	flags := uint8(0)
	if r.stats {
		flags = xtcpnl.TunnelMsgFlagStats
	}
	req := xtcpnl.BuildDumpTunnelRequest(r.vniIndex, flags, r.nextSeq())
	if !o.isJSON() {
		o.printf(col("dev") + "  " + col17("vni") + "  group/remote\n")
	}
	var perr error
	rc, err := r.dump(req, nil, func(m Msg) int {
		v, e := r.printVnifilterRtm(m)
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
		r.errf("Dump ternminated\n")
		return exitCode(1)
	}
	o.deleteJSONObj()
	return nil
}

// col17 is "%-17s".
func col17(s string) string {
	for len(s) < vxlanIDLen {
		s += " "
	}
	return s
}

// printVnifilterRtm is print_vnifilter_rtm.
func (r *runner) printVnifilterRtm(m Msg) (int, error) {
	if m.Type != unix.RTM_NEWTUNNEL && m.Type != unix.RTM_DELTUNNEL && m.Type != unix.RTM_GETTUNNEL {
		r.errf("Unknown vni tunnel rtm msg: %08x %08x %08x\n", len(m.Body)+nlHeaderLen, m.Type, m.Flags)
		return 0, nil
	}
	if len(m.Body) < xtcpnl.TunnelMsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.TunnelMsgSizeCst)
		return -1, nil
	}
	if m.Body[0] != unix.AF_BRIDGE {
		return 0, nil
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.vniIndex != 0 && r.vniIndex != index {
		return 0, nil
	}
	o := r.o
	if m.Type == unix.RTM_DELTUNNEL {
		o.boolean(printAny, "deleted", "Deleted ", true)
	}
	opened := false
	var perr error
	err := xtcpnl.WalkRawAttrs(m.Body[xtcpnl.TunnelMsgSizeCst:], func(raw uint16, val []byte) {
		if perr != nil || raw&xtcpnl.NlaTypeMaskCst != xtcpnl.VxlanVnifilterEntry {
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
			o.openArray(printJSON, "vnis")
			opened = true
		} else {
			o.pad("  ")
		}
		perr = r.printVni(val)
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

// printVni is print_vni.
func (r *runner) printVni(val []byte) error {
	o := r.o
	t, err := xtcpnl.ParseAttrTableFlags(val, 5, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	start, _ := t.U32(xtcpnl.VxlanVnifilterEntryStart)
	end := start
	if t.Has(xtcpnl.VxlanVnifilterEntryEnd) {
		end, _ = t.U32(xtcpnl.VxlanVnifilterEntryEnd)
	}
	o.openObject("")
	w := o.rangeOf("vni", start, end)
	if !o.isJSON() {
		o.printf(padLeft(vxlanIDLen - w))
	}
	switch {
	case t.Has(xtcpnl.VxlanVnifilterEntryGroup):
		a := t.Bytes(xtcpnl.VxlanVnifilterEntryGroup)
		if len(a) < inetAddrLen {
			return errOverread("VXLAN_VNIFILTER_ENTRY_GROUP", "C3")
		}
		if binary.BigEndian.Uint32(a) != 0 {
			s, _ := formatHost(a, inetAddrLen)
			key := "remote"
			if a[0]&0xf0 == 0xe0 { // IN_MULTICAST
				key = "group"
			}
			o.str(printAny, colorNone, key, s, s)
		}
	case t.Has(xtcpnl.VxlanVnifilterEntryGroup6):
		a := t.Bytes(xtcpnl.VxlanVnifilterEntryGroup6)
		if len(a) < inet6AddrLen {
			return errOverread("VXLAN_VNIFILTER_ENTRY_GROUP6", "B23")
		}
		if !allZero(a[:inet6AddrLen]) {
			s, _ := formatHost(a, inet6AddrLen)
			key := "remote"
			if a[0] == 0xff { // IN6_IS_ADDR_MULTICAST
				key = "group"
			}
			o.str(printAny, colorNone, key, s, s)
		}
	}
	if s := t.Bytes(xtcpnl.VxlanVnifilterEntryStats); s != nil {
		if err := r.printVniStats(s); err != nil {
			return err
		}
	}
	o.closeObject()
	o.nl()
	return nil
}

// printVniStats is print_vnifilter_entry_stats.
func (r *runner) printVniStats(nest []byte) error {
	o := r.o
	o.openObject("stats")
	stb, err := xtcpnl.ParseAttrTableFlags(nest, 9, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	o.nl()
	for _, dir := range []struct {
		label string
		ids   [4]uint16
		keys  [4]string
	}{
		{"RX: ", [4]uint16{xtcpnl.VnifilterEntryStatsRxBytes, xtcpnl.VnifilterEntryStatsRxPkts, xtcpnl.VnifilterEntryStatsRxDrops, xtcpnl.VnifilterEntryStatsRxErrors},
			[4]string{"rx_bytes", "rx_pkts", "rx_drops", "rx_errors"}},
		{"TX: ", [4]uint16{xtcpnl.VnifilterEntryStatsTxBytes, xtcpnl.VnifilterEntryStatsTxPkts, xtcpnl.VnifilterEntryStatsTxDrops, xtcpnl.VnifilterEntryStatsTxErrors},
			[4]string{"tx_bytes", "tx_pkts", "tx_drops", "tx_errors"}},
	} {
		if dir.label == "TX: " {
			o.nl()
		}
		o.pad("    " + dir.label)
		for i, id := range dir.ids {
			if !stb.Has(id) {
				continue
			}
			v, _ := stb.U64(id)
			word := [4]string{"bytes", "pkts", "drops", "errors"}[i]
			o.u64(printAny, dir.keys[i], word+" "+strconv.FormatUint(v, 10)+" ", v)
		}
	}
	o.closeObject()
	return nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// formatHost is format_host for AF_INET (4 bytes) or AF_INET6 (16), as
// glibc's inet_ntop writes them: netip's text, except an IPv6 address
// whose first six words alone are zero, which glibc writes "::a.b.c.d".
func formatHost(b []byte, alen int) (string, bool) {
	if len(b) < alen {
		return "", false
	}
	if alen == inetAddrLen {
		return netip.AddrFrom4([4]byte(b[:4])).String(), true
	}
	a := [16]byte(b[:16])
	if allZero(a[:12]) && (a[12] != 0 || a[13] != 0) {
		return "::" + netip.AddrFrom4([4]byte(a[12:16])).String(), true
	}
	return netip.AddrFrom16(a).String(), true
}
