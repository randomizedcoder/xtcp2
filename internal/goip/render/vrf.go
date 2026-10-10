package render

import (
	"fmt"
	"strings"
)

// This file is `ip vrf show`'s output, ipvrf_show + ipvrf_print
// (ip/ipvrf.c:516-626). A VRF row is just a device name and its routing table id
// (IFLA_INFO_DATA -> IFLA_VRF_TABLE), already decoded into xtcpnl.LinkInfo by the
// link parser; VrfView carries only those two fields.

// VrfView is one row of `ip vrf show`: a VRF device and its table id. The json
// tags are ipvrf_print's keys in order: print_string "name", print_uint "table".
type VrfView struct {
	Name  string `json:"name"`
	Table uint32 `json:"table"`
}

// vrfSepCst is the 23-dash rule under the header (ip/ipvrf.c:610-611). The
// header and empty-form lines are built from the same widths as the prints so
// the whitespace cannot drift from the source.
const vrfSepCst = "-----------------------\n"

// vrfEmptyCst is printed in place of any rows when no VRF matched
// (ip/ipvrf.c:616-618).
const vrfEmptyCst = "No VRF has been configured\n"

// vrfHeader is "%-16s" of "Name" then "  %5s\n" of "Table" (ip/ipvrf.c:608-609),
// computed rather than hand-spaced so it matches the two prints exactly.
var vrfHeader = fmt.Sprintf("%-16s  %5s\n", "Name", "Table")

// RenderVrfText renders `ip vrf show`: the header and separator always, then one
// "%-16s %5u\n" row per VRF in order (ipvrf_print), or the empty-form line when
// no VRF matched (ipvrf_show prints the header regardless).
func RenderVrfText(views []VrfView) string {
	var b strings.Builder
	b.WriteString(vrfHeader)
	b.WriteString(vrfSepCst)
	if len(views) == 0 {
		b.WriteString(vrfEmptyCst)
		return b.String()
	}
	for _, v := range views {
		fmt.Fprintf(&b, "%-16s %5d\n", v.Name, v.Table)
	}
	return b.String()
}
