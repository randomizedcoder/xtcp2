package gobridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bridge/vlan.c: `bridge vlan show|tunnelshow|global show`.

// vlanIDLen is VLAN_ID_LEN, the vlan-id column's width.
const vlanIDLen = 9

// vlanSubject is enum vlan_show_subject.
type vlanSubject int

const (
	subjectVlan vlanSubject = iota
	subjectTunnel
)

// The kernel's flags and attributes vlan.c reads, beside xtcpnl's.
const (
	brVlanInfoPvid     = 1 << 1 // BRIDGE_VLAN_INFO_PVID
	brVlanInfoUntagged = 1 << 2 // BRIDGE_VLAN_INFO_UNTAGGED
	brVlandbMaxCst     = 2      // BRIDGE_VLANDB_MAX

	// The entry and global options print_vlan_opts and
	// print_vlan_global_opts read (if_bridge.h).
	entryMcastRouter   = 6
	entryMcastNGroups  = 7
	entryMcastMaxGroup = 8
	entryNeighSuppress = 9
	entryNeighFwdGrat  = 10

	goptsSnooping    = 3
	goptsIgmpVersion = 4
	goptsMldVersion  = 5
	goptsLastMbrCnt  = 6
	goptsStartupCnt  = 7
	goptsLastMbrIntv = 8
	goptsMbrIntv     = 10
	goptsQuerierIntv = 11
	goptsQueryIntv   = 12
	goptsQueryRspIv  = 13
	goptsStartupIntv = 14
	goptsQuerier     = 15
	goptsRouterPorts = 16
	goptsQuerierSt   = 17
	goptsMsti        = 18

	// IFLA_STATS_* and the bridge xstats nest (if_link.h, if_bridge.h).
	ifStatsMsgSize      = 12
	iflaStatsMax        = 5
	iflaStatsXstats     = 2
	iflaStatsXstatsSlv  = 3
	linkXstatsBridge    = 1
	linkXstatsTypeMax   = 2
	bridgeXstatsVlan    = 1
	bridgeVlanXstatsLen = 40
)

// doVlan is do_vlan.
func (r *runner) doVlan(args []string) error {
	if err := r.initMap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return r.vlanShow(nil, subjectVlan)
	}
	switch w := args[0]; {
	case matches(w, "add"), matches(w, "delete"):
		return r.refuse("vlan " + w)(nil)
	case showWords(w, false):
		return r.vlanShow(args[1:], subjectVlan)
	case matches(w, "tunnelshow"):
		return r.vlanShow(args[1:], subjectTunnel)
	case matches(w, "set"):
		return r.refuse("vlan set")(nil)
	case w == "global":
		return r.vlanGlobal(args[1:])
	case matches(w, helpWord):
		return r.vlanUsage()
	}
	return r.unknownCommand("vlan", args[0])
}

// vlanGlobal is vlan_global: strcmp, unlike do_vlan (B21).
func (r *runner) vlanGlobal(args []string) error {
	if len(args) == 0 {
		return r.vlanGlobalShow(nil)
	}
	switch {
	case showWords(args[0], true):
		return r.vlanGlobalShow(args[1:])
	case args[0] == "set":
		return r.refuse("vlan global set")(nil)
	}
	return r.vlanUsage()
}

// vlanUsage is vlan.c usage.
func (r *runner) vlanUsage() error {
	r.errf("%s", "Usage: bridge vlan { add | del } vid VLAN_ID dev DEV [ tunnel_info id TUNNEL_ID ]\n"+
		"                                                     [ pvid ] [ untagged ]\n"+
		"                                                     [ self ] [ master ]\n"+
		"       bridge vlan { set } vid VLAN_ID dev DEV [ state STP_STATE ]\n"+
		"                                               [ mcast_router MULTICAST_ROUTER ]\n"+
		"                                               [ mcast_max_groups MAX_GROUPS ]\n"+
		"                                               [ neigh_suppress {on | off} ]\n"+
		"                                               [ neigh_forward_grat {on | off} ]\n"+
		"       bridge vlan { show } [ dev DEV ] [ vid VLAN_ID ]\n"+
		"       bridge vlan { tunnelshow } [ dev DEV ] [ vid VLAN_ID ]\n"+
		"       bridge vlan global { set } vid VLAN_ID dev DEV\n"+
		"                      [ mcast_snooping MULTICAST_SNOOPING ]\n"+
		"                      [ mcast_querier MULTICAST_QUERIER ]\n"+
		"                      [ mcast_igmp_version IGMP_VERSION ]\n"+
		"                      [ mcast_mld_version MLD_VERSION ]\n"+
		"                      [ mcast_last_member_count LAST_MEMBER_COUNT ]\n"+
		"                      [ mcast_last_member_interval LAST_MEMBER_INTERVAL ]\n"+
		"                      [ mcast_startup_query_count STARTUP_QUERY_COUNT ]\n"+
		"                      [ mcast_startup_query_interval STARTUP_QUERY_INTERVAL ]\n"+
		"                      [ mcast_membership_interval MEMBERSHIP_INTERVAL ]\n"+
		"                      [ mcast_querier_interval QUERIER_INTERVAL ]\n"+
		"                      [ mcast_query_interval QUERY_INTERVAL ]\n"+
		"                      [ mcast_query_response_interval QUERY_RESPONSE_INTERVAL ]\n"+
		"                      [ msti MSTI ]\n"+
		"       bridge vlan global { show } [ dev DEV ] [ vid VLAN_ID ]\n")
	return exitCode(255)
}

// vlanHeader is the "port vlan-id" header, printed before the dump (B6).
func (r *runner) vlanHeader(tunnel bool) {
	if r.o.isJSON() {
		return
	}
	h := col("port") + "  " + fmt.Sprintf("%-9s", "vlan-id")
	if tunnel {
		h += "  tunnel-id"
	}
	r.o.printf(h + "\n")
}

// dumpOrExit runs a dump and turns a failed one into "Dump terminated" and
// exit(1), as every vlan.c show does.
func (r *runner) dumpOrExit(req []byte, berr error, filter func(Msg) (int, error)) error {
	var perr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		v, e := filter(m)
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
		return exitCode(1)
	}
	return nil
}

// vlanShow is vlan_show.
func (r *runner) vlanShow(args []string, subject vlanSubject) error {
	dev, err := r.showArgs(args, &r.vlanVid)
	if err != nil {
		return err
	}
	if err := r.resolveFilter(dev, &r.vlanIndex); err != nil {
		return err
	}
	o := r.o
	o.newJSONObj()
	switch {
	case r.details && subject == subjectVlan:
		flags := uint32(0)
		if r.stats {
			flags = xtcpnl.BridgeVlandbDumpfStats
		}
		req, berr := xtcpnl.BuildDumpBrVlanRequest(flags, r.nextSeq())
		r.vlanHeader(false)
		if err := r.dumpOrExit(req, berr, func(m Msg) (int, error) { return r.printVlanRtm(m, false) }); err != nil {
			return err
		}
		if r.vlanCurIdx != -1 {
			r.closeVlanPort()
		}
	case !r.stats:
		mask := uint32(xtcpnl.RtextFilterBrvlan)
		if r.compress {
			mask = xtcpnl.RtextFilterBrvlanCompressed
		}
		req, berr := xtcpnl.BuildDumpLinkRequestExt(unix.AF_BRIDGE, mask, r.nextSeq())
		r.vlanHeader(subject == subjectTunnel)
		if err := r.dumpOrExit(req, berr, func(m Msg) (int, error) { return r.printVlan(m, subject) }); err != nil {
			return err
		}
	default:
		// -s, tunnelshow included (B22): the bridge xstats, then the
		// ports'.
		req := xtcpnl.BuildDumpStatsRequest(unix.AF_UNSPEC, 0, 1<<(iflaStatsXstats-1), r.nextSeq())
		if !o.isJSON() {
			o.printf(col("port") + "  vlan-id\n")
		}
		if err := r.dumpOrExit(req, nil, r.printVlanStats); err != nil {
			return err
		}
		req = xtcpnl.BuildDumpStatsRequest(unix.AF_UNSPEC, 0, 1<<(iflaStatsXstatsSlv-1), r.nextSeq())
		if err := r.dumpOrExit(req, nil, r.printVlanStats); err != nil {
			return err
		}
	}
	o.deleteJSONObj()
	return nil
}

// vlanGlobalShow is vlan_global_show.
func (r *runner) vlanGlobalShow(args []string) error {
	dev, err := r.showArgs(args, &r.vlanVid)
	if err != nil {
		return err
	}
	if err := r.resolveFilter(dev, &r.vlanIndex); err != nil {
		return err
	}
	r.o.newJSONObj()
	req, berr := xtcpnl.BuildDumpBrVlanRequest(xtcpnl.BridgeVlandbDumpfGlobal, r.nextSeq())
	r.vlanHeader(false)
	if err := r.dumpOrExit(req, berr, func(m Msg) (int, error) { return r.printVlanRtm(m, true) }); err != nil {
		return err
	}
	if r.vlanCurIdx != -1 {
		r.closeVlanPort()
	}
	r.o.deleteJSONObj()
	return nil
}

// filterVlanCheck is filter_vlan_check: -1 stop (past the filter), 0
// skip, 1 print. filter_vlan is an int compared with the u16 vid.
func (r *runner) filterVlanCheck(vid, flags uint16) int {
	f := int64(r.vlanVid)
	if f != 0 && int64(vid) > f && flags&xtcpnl.BridgeVlanInfoRangeEnd == 0 {
		return -1
	}
	if flags&xtcpnl.BridgeVlanInfoRangeBegin != 0 || int64(vid) < f {
		return 0
	}
	return 1
}

// openVlanPort is open_vlan_port.
func (r *runner) openVlanPort(index uint32, subject vlanSubject) error {
	name, err := r.indexToName(index)
	if err != nil {
		return err
	}
	o := r.o
	o.openObject("")
	o.str(printAny, colorIfname, "ifname", col(name)+"  ", name)
	arr := "vlans"
	if subject == subjectTunnel {
		arr = "tunnels"
	}
	o.openArray(printJSON, arr)
	return nil
}

func (r *runner) closeVlanPort() {
	r.o.closeArray(printJSON, "")
	r.o.closeObject()
}

// printVlanFlags is lib/bridge.c bridge_print_vlan_flags: nothing for 0,
// else the array, which holds only PVID and Egress Untagged (B26).
func (r *runner) printVlanFlags(flags uint16) {
	if flags == 0 {
		return
	}
	o := r.o
	o.openArray(printJSON, "flags")
	if flags&brVlanInfoPvid != 0 {
		o.str(printAny, colorNone, "", " PVID", "PVID")
	}
	if flags&brVlanInfoUntagged != 0 {
		o.str(printAny, colorNone, "", " Egress Untagged", "Egress Untagged")
	}
	o.closeArray(printJSON, "")
}

// printVlan is print_vlan, the BRVLAN dump's callback.
func (r *runner) printVlan(m Msg, subject vlanSubject) (int, error) {
	if m.Type != unix.RTM_NEWLINK {
		r.errf("Not RTM_NEWLINK: %08x %08x %08x\n", len(m.Body)+nlHeaderLen, m.Type, m.Flags)
		return 0, nil
	}
	if len(m.Body) < xtcpnl.IfInfomsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.IfInfomsgSizeCst)
		return -1, nil
	}
	if m.Body[0] != unix.AF_BRIDGE {
		return 0, nil
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.vlanIndex != 0 && r.vlanIndex != index {
		return 0, nil
	}
	// parse_rtattr: no mask, so only an unflagged IFLA_AF_SPEC counts.
	spec, ok := xtcpnl.FindRawAttr(m.Body[xtcpnl.IfInfomsgSizeCst:], xtcpnl.IflaAfSpecCst)
	if !ok {
		return 0, nil
	}
	var err error
	if subject == subjectVlan {
		err = r.printVlanInfo(spec, index)
	} else {
		err = r.printVlanTunnelInfo(spec, index)
	}
	return 0, err
}

// printVlanInfo is print_vlan_info: each IFLA_BRIDGE_VLAN_INFO, a range's
// start remembered from every entry that is not its end.
func (r *runner) printVlanInfo(spec []byte, index uint32) error {
	o := r.o
	var lastStart uint16
	opened := false
	stop := false
	var perr error
	err := xtcpnl.WalkRawAttrs(spec, func(raw uint16, val []byte) {
		if stop || perr != nil || raw != xtcpnl.IflaBridgeVlanInfo {
			return
		}
		if len(val) < xtcpnl.BridgeVlanInfoSizeCst {
			perr = errOverread("IFLA_BRIDGE_VLAN_INFO", "B27")
			return
		}
		flags := binary.LittleEndian.Uint16(val[0:2])
		vid := binary.LittleEndian.Uint16(val[2:4])
		if flags&xtcpnl.BridgeVlanInfoRangeEnd == 0 {
			lastStart = vid
		}
		switch r.filterVlanCheck(vid, flags) {
		case -1:
			stop = true
			return
		case 0:
			return
		}
		if !opened {
			if perr = r.openVlanPort(index, subjectVlan); perr != nil {
				return
			}
			opened = true
		} else {
			o.pad("  ")
		}
		o.openObject("")
		o.rangeOf("vlan", uint32(lastStart), uint32(vid))
		r.printVlanFlags(flags)
		o.closeObject()
		o.nl()
	})
	if perr != nil {
		return perr
	}
	if opened {
		r.closeVlanPort()
	}
	return err
}

// printVlanTunnelInfo is print_vlan_tunnel_info.
func (r *runner) printVlanTunnelInfo(spec []byte, index uint32) error {
	o := r.o
	var lastVid uint16
	var lastID uint32
	opened, stop := false, false
	var perr error
	err := xtcpnl.WalkRawAttrs(spec, func(raw uint16, val []byte) {
		if stop || perr != nil || raw != xtcpnl.IflaBridgeVlanTunnelInfo {
			return
		}
		t, e := xtcpnl.ParseAttrTableFlags(val, 3, 0)
		if e != nil {
			perr = e
			return
		}
		if !t.Has(xtcpnl.IflaBridgeVlanTunnelVid) {
			return
		}
		vid, _ := t.U16(xtcpnl.IflaBridgeVlanTunnelVid)
		id, _ := t.U32(xtcpnl.IflaBridgeVlanTunnelID) // 0 when absent (B15)
		flags, _ := t.U16(xtcpnl.IflaBridgeVlanTunnelFlags)
		if flags&xtcpnl.BridgeVlanInfoRangeEnd == 0 {
			lastVid, lastID = vid, id
		}
		switch r.filterVlanCheck(vid, flags) {
		case -1:
			stop = true
			return
		case 0:
			return
		}
		if !opened {
			if perr = r.openVlanPort(index, subjectTunnel); perr != nil {
				return
			}
			opened = true
		} else {
			o.pad("  ")
		}
		o.openObject("")
		w := o.rangeOf("vlan", uint32(lastVid), uint32(vid))
		if !o.isJSON() {
			o.printf(padLeft(vlanIDLen - w))
		}
		o.rangeOf("tunid", lastID, id)
		o.closeObject()
		o.nl()
	})
	if perr != nil {
		return perr
	}
	if opened {
		r.closeVlanPort()
	}
	return err
}

// printVlanRtm is print_vlan_rtm, the RTM_GETVLAN dump's callback.
func (r *runner) printVlanRtm(m Msg, globalOnly bool) (int, error) {
	if m.Type != unix.RTM_NEWVLAN && m.Type != unix.RTM_DELVLAN && m.Type != unix.RTM_GETVLAN {
		r.errf("Unknown vlan rtm message: %08x %08x %08x\n", len(m.Body)+nlHeaderLen, m.Type, m.Flags)
		return 0, nil
	}
	if len(m.Body) < xtcpnl.BrVlanMsgSizeCst {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-xtcpnl.BrVlanMsgSizeCst)
		return -1, nil
	}
	if m.Body[0] != unix.AF_BRIDGE {
		return 0, nil
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.vlanIndex != 0 && r.vlanIndex != index {
		return 0, nil
	}
	if m.Type == unix.RTM_DELVLAN {
		r.o.boolean(printAny, "deleted", "Deleted ", true)
	}
	if r.vlanCurIdx != -1 && r.vlanCurIdx != int64(index) {
		r.closeVlanPort()
		r.vlanCurIdx = -1
	}
	var perr error
	err := xtcpnl.WalkRawAttrs(m.Body[xtcpnl.BrVlanMsgSizeCst:], func(raw uint16, val []byte) {
		t := raw & xtcpnl.NlaTypeMaskCst
		if perr != nil || t > brVlandbMaxCst || (globalOnly && t != xtcpnl.BridgeVlandbGlobalOptions) {
			return
		}
		switch t {
		case xtcpnl.BridgeVlandbEntry:
			perr = r.printVlanOpts(val, index)
		case xtcpnl.BridgeVlandbGlobalOptions:
			perr = r.printVlanGlobalOpts(val, index)
		}
	})
	if perr != nil {
		return -1, perr
	}
	return 0, err
}

// startVlanEntry opens a port the first time its ifindex is seen, as
// print_vlan_opts and print_vlan_global_opts do.
func (r *runner) startVlanEntry(index uint32) error {
	if r.vlanCurIdx != int64(index) {
		if err := r.openVlanPort(index, subjectVlan); err != nil {
			return err
		}
		r.o.openObject("")
		r.vlanCurIdx = int64(index)
		return nil
	}
	r.o.openObject("")
	r.o.pad("  ")
	return nil
}

// printVlanOpts is print_vlan_opts, an entry of `bridge -d vlan show`.
func (r *runner) printVlanOpts(val []byte, index uint32) error {
	o := r.o
	vtb, err := xtcpnl.ParseAttrTableFlags(val, xtcpnl.BridgeVlandbEntryMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	info := vtb.Bytes(xtcpnl.BridgeVlandbEntryInfo)
	if info == nil {
		return errCrash("BRIDGE_VLANDB_ENTRY_INFO", "B20")
	}
	if len(info) < xtcpnl.BridgeVlanInfoSizeCst {
		return errOverread("BRIDGE_VLANDB_ENTRY_INFO", "B20")
	}
	flags := binary.LittleEndian.Uint16(info[0:2])
	vid := binary.LittleEndian.Uint16(info[2:4])
	vrange := vid
	if vtb.Has(xtcpnl.BridgeVlandbEntryRange) {
		vrange, _ = vtb.U16(xtcpnl.BridgeVlandbEntryRange)
	}
	if f := int64(r.vlanVid); f != 0 && (f < int64(vid) || f > int64(vrange)) {
		return nil
	}
	state, _ := vtb.U8(xtcpnl.BridgeVlandbEntryState)
	var stats [4]uint64 // rx_bytes, rx_packets, tx_bytes, tx_packets
	if s := vtb.Bytes(xtcpnl.BridgeVlandbEntryStats); s != nil {
		stb, err := xtcpnl.ParseAttrTableFlags(s, 5, 0)
		if err != nil {
			return err
		}
		for i, id := range []uint16{xtcpnl.BridgeVlandbStatsRxBytes, xtcpnl.BridgeVlandbStatsRxPkts,
			xtcpnl.BridgeVlandbStatsTxBytes, xtcpnl.BridgeVlandbStatsTxPkts} {
			if v, ok := stb.U64(id); ok {
				stats[i] = v
			}
		}
	}
	if err := r.startVlanEntry(index); err != nil {
		return err
	}
	o.rangeOf("vlan", uint32(vid), uint32(vrange))
	r.printVlanFlags(flags)
	o.nl()
	o.pad("    ")
	r.printSTPState(state)
	if vtb.Has(entryMcastRouter) {
		v, _ := vtb.U8(entryMcastRouter)
		o.uint(printAny, "mcast_router", "mcast_router "+strconv.Itoa(int(v))+" ", uint32(v))
	}
	for _, g := range []struct {
		id  uint16
		key string
	}{{entryMcastNGroups, "mcast_n_groups"}, {entryMcastMaxGroup, "mcast_max_groups"}} {
		if vtb.Has(g.id) {
			v, _ := vtb.U32(g.id)
			o.uint(printAny, g.key, g.key+" "+strconv.FormatUint(uint64(v), 10)+" ", v)
		}
	}
	for _, g := range []struct {
		id  uint16
		key string
	}{{entryNeighSuppress, "neigh_suppress"}, {entryNeighFwdGrat, "neigh_forward_grat"}} {
		if vtb.Has(g.id) {
			v, _ := vtb.U8(g.id)
			o.onOff(printAny, g.key, g.key+" %s ", v != 0)
		}
	}
	o.nl()
	if r.stats {
		r.printVlanStatsOnly(stats[0], stats[1], stats[2], stats[3])
	}
	o.closeObject()
	return nil
}

// printVlanStatsOnly is lib/bridge.c bridge_print_vlan_stats_only.
func (r *runner) printVlanStatsOnly(rxBytes, rxPkts, txBytes, txPkts uint64) {
	o := r.o
	u := func(v uint64) string { return strconv.FormatUint(v, 10) }
	o.pad("    ")
	o.u64(printAny, "rx_bytes", "RX: "+u(rxBytes)+" bytes", rxBytes)
	o.u64(printAny, "rx_packets", " "+u(rxPkts)+" packets\n", rxPkts)
	o.pad("    ")
	o.u64(printAny, "tx_bytes", "TX: "+u(txBytes)+" bytes", txBytes)
	o.u64(printAny, "tx_packets", " "+u(txPkts)+" packets\n", txPkts)
}

// goptsU8 and the like are print_vlan_global_opts' scalar options, in
// order, between the querier state and msti.
var goptsAfterQuerier = []struct {
	id    uint16
	key   string
	width int // 1, 4 or 8 bytes, as rta_getattr_u8/u32/u64 reads it
	guard uint16
}{
	{goptsIgmpVersion, "mcast_igmp_version", 1, goptsIgmpVersion},
	{goptsMldVersion, "mcast_mld_version", 1, goptsMldVersion},
	// Guarded by MLD_VERSION, a copy-paste slip (B18).
	{goptsLastMbrCnt, "mcast_last_member_count", 4, goptsMldVersion},
	{goptsLastMbrIntv, "mcast_last_member_interval", 8, goptsLastMbrIntv},
	{goptsStartupCnt, "mcast_startup_query_count", 4, goptsStartupCnt},
	{goptsStartupIntv, "mcast_startup_query_interval", 8, goptsStartupIntv},
	{goptsMbrIntv, "mcast_membership_interval", 8, goptsMbrIntv},
	{goptsQuerierIntv, "mcast_querier_interval", 8, goptsQuerierIntv},
	{goptsQueryIntv, "mcast_query_interval", 8, goptsQueryIntv},
	{goptsQueryRspIv, "mcast_query_response_interval", 8, goptsQueryRspIv},
}

// printVlanGlobalOpts is print_vlan_global_opts.
func (r *runner) printVlanGlobalOpts(val []byte, index uint32) error {
	o := r.o
	vtb, err := xtcpnl.ParseAttrTableFlags(val, xtcpnl.BridgeVlandbGoptsMaxCst, unix.NLA_F_NESTED)
	if err != nil {
		return err
	}
	if !vtb.Has(xtcpnl.BridgeVlandbGoptsID) {
		return errCrash("BRIDGE_VLANDB_GOPTS_ID", "B19")
	}
	vid, _ := vtb.U16(xtcpnl.BridgeVlandbGoptsID)
	vrange := vid
	if vtb.Has(xtcpnl.BridgeVlandbGoptsRange) {
		vrange, _ = vtb.U16(xtcpnl.BridgeVlandbGoptsRange)
	}
	if f := int64(r.vlanVid); f != 0 && (f < int64(vid) || f > int64(vrange)) {
		return nil
	}
	if err := r.startVlanEntry(index); err != nil {
		return err
	}
	o.rangeOf("vlan", uint32(vid), uint32(vrange))
	o.nl()
	o.pad("    ")
	u8opt := func(id uint16, key string) {
		if vtb.Has(id) {
			v, _ := vtb.U8(id)
			o.uint(printAny, key, key+" "+strconv.Itoa(int(v))+" ", uint32(v))
		}
	}
	u8opt(goptsSnooping, "mcast_snooping")
	u8opt(goptsQuerier, "mcast_querier")
	if vtb.Has(goptsQuerierSt) {
		if err := r.printQuerierState(vtb.Bytes(goptsQuerierSt)); err != nil {
			return err
		}
	}
	for _, g := range goptsAfterQuerier {
		if !vtb.Has(g.guard) {
			continue
		}
		if !vtb.Has(g.id) {
			return errCrash(fmt.Sprintf("global option %d", g.id), "B18")
		}
		switch g.width {
		case 1:
			v, _ := vtb.U8(g.id)
			o.uint(printAny, g.key, g.key+" "+strconv.Itoa(int(v))+" ", uint32(v))
		case 4:
			v, _ := vtb.U32(g.id)
			o.uint(printAny, g.key, g.key+" "+strconv.FormatUint(uint64(v), 10)+" ", v)
		default:
			v, _ := vtb.U64(g.id)
			o.u64(printAny, g.key, g.key+" "+strconv.FormatUint(v, 10)+" ", v)
		}
	}
	if vtb.Has(goptsMsti) {
		v, _ := vtb.U16(goptsMsti)
		o.uint(printAny, "msti", "msti "+strconv.Itoa(int(v))+" ", uint32(v))
	}
	o.nl()
	if rp := vtb.Bytes(goptsRouterPorts); rp != nil {
		if err := r.printRouterPorts(rp); err != nil {
			return err
		}
	}
	o.closeObject()
	return nil
}

// printRouterPorts is print_vlan_router_ports, handed RTA_DATA of the
// option: the MDBA_ROUTER nest inside it, whose payload holds the ports.
func (r *runner) printRouterPorts(opt []byte) error {
	o := r.o
	// RTA_DATA(vtb[...]) read as an rtattr: the first nested attribute.
	inner, ok := firstAttrPayload(opt)
	if !ok {
		return errOverread("BRIDGE_VLANDB_GOPTS_MCAST_ROUTER_PORTS", "B9")
	}
	o.pad("    ")
	name := "router ports: "
	if o.isJSON() {
		name = "router_ports"
	}
	o.openArray(printAny, name)
	first := true
	var perr error
	err := xtcpnl.WalkRawAttrs(inner, func(_ uint16, val []byte) {
		if perr != nil {
			return
		}
		if len(val) < 4 {
			perr = errOverread("router port ifindex", "B9")
			return
		}
		n, e := r.indexToName(binary.LittleEndian.Uint32(val[0:4]))
		if e != nil {
			perr = e
			return
		}
		o.openObject("")
		if r.stats && !first {
			o.nl()
			o.pad("    " + "              ")
		}
		first = false
		o.str(printAny, colorNone, "port", n+" ", n)
		if r.stats {
			perr = r.routerPortStats(val)
		}
		o.closeObject()
	})
	if perr != nil {
		return perr
	}
	o.closeArray(printJSON, "")
	o.nl()
	return err
}

// firstAttrPayload is RTA_DATA of the first attribute in data.
func firstAttrPayload(data []byte) ([]byte, bool) {
	var out []byte
	found := false
	err := xtcpnl.WalkRawAttrs(data, func(_ uint16, val []byte) {
		if !found {
			out, found = val, true
		}
	})
	return out, found && err == nil
}

// printVlanStats is print_vlan_stats, the -s dumps' callback.
func (r *runner) printVlanStats(m Msg) (int, error) {
	if len(m.Body) < ifStatsMsgSize {
		r.errf("BUG: wrong nlmsg len %d\n", len(m.Body)-ifStatsMsgSize)
		return -1, nil
	}
	index := binary.LittleEndian.Uint32(m.Body[4:8])
	if r.vlanIndex != 0 && r.vlanIndex != index {
		return 0, nil
	}
	tb, err := xtcpnl.ParseAttrTableFlags(m.Body[ifStatsMsgSize:], iflaStatsMax, 0)
	if err != nil {
		return -1, err
	}
	for _, id := range []uint16{iflaStatsXstats, iflaStatsXstatsSlv} {
		if a := tb.Bytes(id); a != nil {
			if err := r.printVlanStatsAttr(a, index); err != nil {
				return -1, err
			}
		}
	}
	return 0, nil
}

// printVlanStatsAttr is print_vlan_stats_attr.
func (r *runner) printVlanStatsAttr(attr []byte, index uint32) error {
	brtb, err := xtcpnl.ParseAttrTableFlags(attr, linkXstatsTypeMax, 0)
	if err != nil {
		return err
	}
	list := brtb.Bytes(linkXstatsBridge)
	if list == nil {
		return nil
	}
	found := false
	var perr error
	err = xtcpnl.WalkRawAttrs(list, func(raw uint16, val []byte) {
		if perr != nil || raw != bridgeXstatsVlan {
			return
		}
		if len(val) < bridgeVlanXstatsLen {
			perr = errOverread("BRIDGE_XSTATS_VLAN", "B8")
			return
		}
		var v xtcpnl.BridgeVlanXstats
		if _, e := xtcpnl.DeserializeBridgeVlanXstats(val, &v); e != nil {
			perr = e
			return
		}
		if r.vlanVid != 0 && int64(r.vlanVid) != int64(v.Vid) {
			return
		}
		// Pure port entries are the slave dump's.
		if v.Flags&xtcpnl.BridgeVlanInfoMaster != 0 && v.Flags&xtcpnl.BridgeVlanInfoBrentry == 0 {
			return
		}
		if !found {
			if perr = r.openVlanPort(index, subjectVlan); perr != nil {
				return
			}
			found = true
		} else {
			r.o.pad("  ")
		}
		r.printVlanStatsEntry(v)
	})
	if perr != nil {
		return perr
	}
	if found {
		r.closeVlanPort()
	}
	return err
}

// printVlanStatsEntry is lib/bridge.c bridge_print_vlan_stats: the VLAN
// keyed "vid" where the other shows key it "vlan" (B25).
func (r *runner) printVlanStatsEntry(v xtcpnl.BridgeVlanXstats) {
	o := r.o
	o.openObject("")
	o.uint(printAny, "vid", strconv.Itoa(int(v.Vid)), uint32(v.Vid))
	r.printVlanFlags(v.Flags)
	o.nl()
	r.printVlanStatsOnly(v.RxBytes, v.RxPackets, v.TxBytes, v.TxPackets)
	o.closeObject()
}

// errCrash is bridge reading an attribute the kernel did not send through
// a NULL pointer: it dies of SIGSEGV, stdout lost (exit 139).
func errCrash(what, lead string) error {
	return fmt.Errorf("%w: %s missing; bridge reads it through NULL (%s)", errSegv, what, lead)
}

// errOverread is an attribute shorter than what bridge reads from it:
// bridge reads past it, which no copy can reproduce, so gobridge refuses.
func errOverread(what, lead string) error {
	return fmt.Errorf("gobridge: %s is short; bridge reads past it (%s)", what, lead)
}

// errSegv is bridge dying of SIGSEGV; run exits 139 with stdout lost.
var errSegv = errors.New("gobridge: bridge dies of SIGSEGV here")

// The BRIDGE_QUERIER_* attributes (if_bridge.h).
const (
	querierIPAddr     = 1
	querierIPPort     = 2
	querierIPTimer    = 3
	querierIP6Addr    = 5
	querierIP6Port    = 6
	querierIP6Timer   = 7
	querierMaxCst     = 7
	nsecPerSec        = 1e9
	nsecPerMsec       = 1e6
	nsecPerUsec       = 1e3
	inetAddrLen       = 4
	inet6AddrLen      = 16
	mcastTimerUnknown = ""
)

// printQuerierState is lib/bridge.c bridge_print_mcast_querier_state.
func (r *runner) printQuerierState(nest []byte) error {
	o := r.o
	tb, err := xtcpnl.ParseAttrTableFlags(nest, querierMaxCst, 0)
	if err != nil {
		return err
	}
	for _, fam := range []struct {
		obj, addrKey, portKey, timerKey string
		addr, port, timer               uint16
		alen                            int
		color                           colorAttr
	}{
		{"mcast_querier_state_ipv4", "mcast_querier_ipv4_addr", "mcast_querier_ipv4_port", "mcast_querier_ipv4_other_timer",
			querierIPAddr, querierIPPort, querierIPTimer, inetAddrLen, colorInet},
		{"mcast_querier_state_ipv6", "mcast_querier_ipv6_addr", "mcast_querier_ipv6_port", "mcast_querier_ipv6_other_timer",
			querierIP6Addr, querierIP6Port, querierIP6Timer, inet6AddrLen, colorInet6},
	} {
		o.openObject(fam.obj)
		if a := tb.Bytes(fam.addr); a != nil {
			ip, ok := formatHost(a, fam.alen)
			if !ok {
				return errOverread(fam.addrKey, "C3")
			}
			o.str(printFP, colorNone, "", fam.addrKey+" ", fam.addrKey)
			o.str(printAny, fam.color, fam.addrKey, ip+" ", ip)
		}
		if tb.Has(fam.port) {
			v, _ := tb.U32(fam.port)
			o.uint(printAny, fam.portKey, fam.portKey+" "+strconv.FormatUint(uint64(v), 10)+" ", v)
		}
		if tb.Has(fam.timer) {
			v, _ := tb.U64(fam.timer)
			t := sprintTime64(wrapS64(v))
			o.str(printAny, colorNone, fam.timerKey, fam.timerKey+" "+t+" ", t)
		}
		o.closeObject()
	}
	return nil
}

// wrapS64 is C's (__s64) of a u64: two's complement, so values past
// INT64_MAX are negative.
func wrapS64(v uint64) int64 {
	if s, ok := cint.Int64(v); ok {
		return s
	}
	c, _ := cint.Int64(^v) // ^v < 2^63 here
	return -c - 1
}

// sprintTime64 is lib/utils.c print_time64: nanoseconds as %.3g in s, ms
// or us, or %lld ns.
func sprintTime64(t int64) string {
	ns := float64(t)
	switch {
	case t >= nsecPerSec:
		return fmt.Sprintf("%.3gs", ns/nsecPerSec)
	case t >= nsecPerMsec:
		return fmt.Sprintf("%.3gms", ns/nsecPerMsec)
	case t >= nsecPerUsec:
		return fmt.Sprintf("%.3gus", ns/nsecPerUsec)
	}
	return strconv.FormatInt(t, 10) + "ns"
}

// routerPortStats is br_print_router_port_stats (bridge/mdb.c), under -s:
// parse_rtattr over the MDBA_ROUTER_PORT payload past its ifindex (the
// caller has checked the ifindex is there, B9), then the timer and the
// type. Both are read at their width with no length check (B35). The
// JSON "timer" is the same aligned text as the column (B34), and the type
// is the kernel's port-wide one (K3).
func (r *runner) routerPortStats(val []byte) error {
	t, err := xtcpnl.ParseAttrTableFlags(val[4:], xtcpnl.MdbaRouterPattrMax, 0)
	if err != nil {
		return err
	}
	o := r.o
	if t.Has(xtcpnl.MdbaRouterPattrTimer) {
		v, ok := t.U32(xtcpnl.MdbaRouterPattrTimer)
		if !ok {
			return errOverread("MDBA_ROUTER_PATTR_TIMER", "B35")
		}
		s := formatTimer(v)
		o.str(printAny, colorNone, "timer", " "+s, s)
	}
	if t.Has(xtcpnl.MdbaRouterPattrType) {
		v, ok := t.U8(xtcpnl.MdbaRouterPattrType)
		if !ok {
			return errOverread("MDBA_ROUTER_PATTR_TYPE", "B35")
		}
		s := wordPermanent
		if p := (xtcpnl.MdbRouterPort{Type: v}); p.IsTemp() {
			s = "temp"
		}
		o.str(printAny, colorNone, "type", " "+s, s)
	}
	return nil
}

// formatTimer is mdb.c format_timer(ticks, 1): __jiffies_to_tv's 10 ms
// ticks as "%4lu.%.2lu", seconds and hundredths.
func formatTimer(ticks uint32) string {
	return fmt.Sprintf("%4d.%02d", ticks/100, ticks%100)
}
