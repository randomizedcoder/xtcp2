package render

import (
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// seg6ActionNames is seg6_action_names (ip/iproute_lwtunnel.c), indexed by
// SEG6_LOCAL_ACTION_*; SEG6_LOCAL_ACTION_UNSPEC has no entry.
var seg6ActionNames = [...]string{
	"", "End", "End.X", "End.T", "End.DX2", "End.DX6", "End.DX4", "End.DT6", "End.DT4",
	"End.B6", "End.B6.Encaps", "End.BM", "End.S", "End.AS", "End.AM", "End.BPF", "End.DT46",
}

// seg6ActionName is format_action_type: "<invalid>" past the table, which
// covers a u32 the int parameter makes negative, and "<unknown>" for the
// entry it has no name for.
func seg6ActionName(a uint32) string {
	if a >= uint32(len(seg6ActionNames)) {
		return "<invalid>"
	}
	if seg6ActionNames[a] == "" {
		return "<unknown>"
	}
	return seg6ActionNames[a]
}

// seg6FlavorNames is seg6_flavor_names, indexed by SEG6_LOCAL_FLV_OP_*.
var seg6FlavorNames = [...]string{"", "psp", "usp", "usd", "next-csid"}

// printEncapSeg6Local is print_encap_seg6local: nothing without the
// action, then each attribute in ip's order. The counters print only under
// -s, which is stats.
func printEncapSeg6Local(p *Printer, s *xtcpnl.Seg6LocalInfo, tab NameTab, stats bool) {
	if !s.HasAction {
		return
	}
	p.String(PrintAny, "action", "action %s ", seg6ActionName(s.Action))
	if s.HasSRH {
		p.OpenObject("srh")
		printSRH(p, s.SRH)
		p.CloseObject()
	}
	if s.HasTable {
		p.String(PrintAny, "table", "table %s ", routeTableName(s.Table))
	}
	if s.HasVrfTable {
		p.String(PrintAny, "vrftable", "vrftable %s ", routeTableName(s.VrfTable))
	}
	if s.HasNH4 {
		p.String(PrintAny, "nh4", "nh4 %s ", addrString(s.NH4, unix.AF_INET))
	}
	if s.HasNH6 {
		p.String(PrintAny, "nh6", "nh6 %s ", addrString(s.NH6, unix.AF_INET6))
	}
	if s.HasIIF {
		p.String(PrintAny, "iif", "iif %s ", tab.IndexToName(int32(s.IIF)))
	}
	if s.HasOIF {
		p.String(PrintAny, "oif", "oif %s ", tab.IndexToName(int32(s.OIF)))
	}
	if s.HasBPF {
		printEncapBPFProg(p, "endpoint", s.BPFName, s.HasBPFName)
	}
	if s.HasCounters && stats {
		printSeg6LocalCounters(p, s.Counters)
	}
	if s.HasFlavors {
		printSeg6LocalFlavors(p, s.Flavors)
	}
}

// printEncapBPFProg is print_encap_bpf_prog: under -j the name, or
// "<unknown>"; in text the label, then the name when there is one.
func printEncapBPFProg(p *Printer, label, name string, hasName bool) {
	if p.JSON() {
		if !hasName {
			name = "<unknown>"
		}
		p.String(PrintJSON, label, "", name)
		return
	}
	p.FPf("%s ", label)
	if hasName {
		p.FPf("%s ", name)
	}
}

// printSeg6LocalCounters is print_seg6_local_counters: a "stats64" object,
// or each name and print_num's number and space.
func printSeg6LocalCounters(p *Printer, c xtcpnl.Seg6LocalCountersInfo) {
	if p.JSON() {
		p.OpenObject("stats64")
		p.U64(PrintJSON, "packets", "", c.Packets)
		p.U64(PrintJSON, "bytes", "", c.Bytes)
		p.U64(PrintJSON, "errors", "", c.Errors)
		p.CloseObject()
		return
	}
	p.FPf("packets %d bytes %d errors %d ", c.Packets, c.Bytes, c.Errors)
}

// printSeg6LocalFlavors is print_seg6_local_flavors: the flavor names, a
// comma between them in text and an array in JSON, skipping a bit with no
// name; then lblen and nflen, each a number in JSON and print_num's in text.
func printSeg6LocalFlavors(p *Printer, f xtcpnl.Seg6LocalFlavorsInfo) {
	if f.HasOps {
		var names []string
		for i, n := range seg6FlavorNames {
			if n != "" && f.Ops&(1<<i) != 0 {
				names = append(names, n)
			}
		}
		p.OpenArray("flavors")
		p.FPf("flavors ")
		for _, n := range names {
			p.String(PrintJSON, "", "", n)
		}
		p.FPf("%s ", strings.Join(names, ","))
		p.CloseArray()
	}
	if f.HasLcblockBits {
		p.U64(PrintJSON, "lblen", "", uint64(f.LcblockBits))
		p.FPf("lblen %d ", f.LcblockBits)
	}
	if f.HasLcnodeFnBits {
		p.U64(PrintJSON, "nflen", "", uint64(f.LcnodeFnBits))
		p.FPf("nflen %d ", f.LcnodeFnBits)
	}
}
