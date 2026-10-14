package gordma

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// cmdDev is cmd_dev (rdma/dev.c).
func cmdDev(r *rd) error {
	return r.execCmd(devShow, []command{
		{"add", notImplemented("dev add")},
		{"delete", notImplemented("dev delete")},
		{"show", devShow},
		{"list", devShow},
		{"set", notImplemented("dev set")},
		{"help", usage(
			"Usage: %s dev show [DEV]",
			"       %s dev add DEVNAME type TYPE parent PARENT_DEVNAME",
			"       %s dev delete DEVNAME",
			"       %s dev set [DEV] name DEVNAME",
			"       %s dev set [DEV] netns NSNAME",
			"       %s dev set [DEV] adaptive-moderation [on|off]")},
	}, "dev command")
}

func devShow(r *rd) error {
	return r.execDev(func(r *rd) error {
		return r.execCmd(devNoArgs, nil, "parameter")
	})
}

// devNoArgs is dev_no_args: one device's GET doit.
func devNoArgs(r *rd) error {
	return r.talk(xtcpnl.BuildRdmaDevGetRequest(r.devIdx, seq()), func(m Msg) error {
		d, err := xtcpnl.ParseRdmaDev(m.Body)
		if err != nil {
			return errCB
		}
		return r.devLine(d)
	})
}

// devLine is dev_parse_cb.
func (r *rd) devLine(d xtcpnl.RdmaDev) error {
	if !d.HasDevIndex || !d.HasDevName {
		return errCB
	}
	p := r.p
	p.openObject()
	p.printUint(printAny, "ifindex", "%d: ", d.DevIndex)
	p.printString(printAny, "ifname", "%s: ", d.DevName)
	if d.HasNodeType {
		p.printString(printAny, "node_type", "node_type %s ", nodeTypeStr(d.NodeType))
	}
	if d.HasDevProtocol {
		p.printString(printAny, "protocol", "protocol %s ", d.DevProtocol)
	}
	if d.HasFwVersion {
		p.printString(printAny, "fw", "fw %s ", d.FwVersion)
	}
	if d.HasNodeGUID {
		p.printString(printAny, "node_guid", "node_guid %s ", guidStr(d.NodeGUID))
	}
	if d.HasSysImageGUID {
		p.printString(printAny, "sys_image_guid", "sys_image_guid %s ", guidStr(d.SysImageGUID))
	}
	if d.HasDevType {
		t := "unknown"
		if d.DevType == rdmaDeviceTypeSMI {
			t = "smi"
		}
		p.printString(printAny, "type", "type %s ", t)
	}
	if d.HasParentName {
		p.printString(printAny, "parent", "parent %s ", d.ParentName)
	}
	if r.showDetails {
		if d.HasDIM && d.DIM <= 1 {
			p.printOnOff(printAny, "adaptive-moderation", "adaptive-moderation %s ", d.DIM == 1)
		}
		if d.HasCapFlags {
			r.printCaps(d.CapFlags, devCapNames)
		}
	}
	p.closeObject()
	p.newline()
	return nil
}

// rdmaDeviceTypeSMI is RDMA_DEVICE_TYPE_SMI.
const rdmaDeviceTypeSMI = 1

// guidStr is dev_print_node_guid's "%04x:%04x:%04x:%04x", high word first.
func guidStr(g uint64) string {
	return fmt.Sprintf("%04x:%04x:%04x:%04x", g>>48&0xffff, g>>32&0xffff, g>>16&0xffff, g&0xffff)
}

// nodeTypeStr is node_type_to_str.
func nodeTypeStr(t uint8) string {
	return tableStr([]string{"unknown", "ca", "switch", "router", "rnic", "usnic", "usnic_udp", "unspecified"}, uint32(t), "unknown")
}

func tableStr(names []string, i uint32, fallback string) string {
	if i < uint32(len(names)) && names[i] != "" {
		return names[i]
	}
	return fallback
}

// capNames is a RDMA_*_FLAGS_LOW/HIGH bitmap: the low word's names by bit,
// then the high word's.
type capNames struct{ low, high []string }

// capName is dev_caps_to_str and caps_to_str. A bit missing from the low
// table does not fall through to the high one, as its index minus the
// table's length wraps.
func (c capNames) name(idx uint32) string {
	if idx < uint32(len(c.low)) {
		if c.low[idx] != "" {
			return c.low[idx]
		}
		return "UNKNOWN"
	}
	return tableStr(c.high, idx-uint32(len(c.low)), "UNKNOWN")
}

// devCapNames is dev.c's RDMA_DEV_FLAGS_LOW and RDMA_DEV_FLAGS_HIGH.
var devCapNames = capNames{
	low: []string{0: "RESIZE_MAX_WR", 1: "BAD_PKEY_CNTR", 2: "BAD_QKEY_CNTR", 3: "RAW_MULTI",
		4: "AUTO_PATH_MIG", 5: "CHANGE_PHY_PORT", 6: "UD_AV_PORT_ENFORCE_PORT_ENFORCE",
		7: "CURR_QP_STATE_MOD", 8: "SHUTDOWN_PORT", 9: "INIT_TYPE", 10: "PORT_ACTIVE_EVENT",
		11: "SYS_IMAGE_GUID", 12: "RC_RNR_NAK_GEN", 13: "SRQ_RESIZE", 14: "N_NOTIFY_CQ",
		15: "LOCAL_DMA_LKEY", 17: "MEM_WINDOW", 18: "UD_IP_CSUM", 19: "UD_TSO", 20: "XRC",
		21: "MEM_MGT_EXTENSIONS", 22: "BLOCK_MULTICAST_LOOPBACK", 23: "MEM_WINDOW_TYPE_2A",
		24: "MEM_WINDOW_TYPE_2B", 25: "RC_IP_CSUM", 26: "RAW_IP_CSUM", 27: "CROSS_CHANNEL",
		29: "MANAGED_FLOW_STEERING", 30: "SIGNATURE_HANDOVER", 31: "ON_DEMAND_PAGING"},
	high: []string{"SG_GAPS_REG", "VIRTUAL_FUNCTION", "RAW_SCATTER_FCS", "RDMA_NETDEV_OPA_VNIC",
		"PCI_WRITE_END_PADDING"},
}

// portCapNames is link.c's RDMA_PORT_FLAGS_LOW and RDMA_PORT_FLAGS_HIGH.
var portCapNames = capNames{
	low: []string{"RESERVED", "SM", "NOTICE", "TRAP", "OPT_IPD", "AUTO_MIGR", "SL_MAP",
		"MKEY_NVRAM", "PKEY_NVRAM", "LED_INFO", "SM_DISABLED", "SYS_IMAGE_GUID",
		"PKEY_SW_EXT_PORT_TRAP", "CABLE_INFO", "EXTENDED_SPEEDS", "CAP_MASK2", "CM",
		"SNMP_TUNNEL", "REINIT", "DEVICE_MGMT", "VENDOR_CLASS", "DR_NOTICE", "CAP_MASK_NOTICE",
		"BOOT_MGMT", "LINK_LATENCY", "CLIENT_REG", "OTHER_LOCAL_CHANGES", "LINK_SPPED_WIDTH",
		"VENDOR_SPECIFIC_MADS", "MULT_PKER_TRAP", "MULT_FDB", "HIERARCHY_INFO"},
	high: []string{"SET_NODE_DESC", "EXT_INFO", "VIRT", "SWITCH_POR_STATE_TABLE", "LINK_WIDTH_2X",
		"LINK_SPEED_HDR"},
}

// printCaps is dev_print_caps and link_print_caps.
func (r *rd) printCaps(caps uint64, names capNames) {
	p := r.p
	if p.fpCtx(printFP) {
		p.raw(p.sl + "    caps: <")
	}
	if p.jsonCtx(printJSON) {
		p.openArray(printJSON, "caps")
	}
	for idx := uint32(0); caps != 0; idx++ {
		if caps&1 != 0 {
			format := "%s"
			if caps>>1 != 0 {
				format = "%s, "
			}
			p.printString(printAny, "", format, names.name(idx))
		}
		caps >>= 1
	}
	p.closeArray(printAny, ">")
}

// cmdLink is cmd_link (rdma/link.c).
func cmdLink(r *rd) error {
	return r.execCmd(linkShow, []command{
		{"add", notImplemented("link add")},
		{"delete", notImplemented("link delete")},
		{"show", linkShow},
		{"list", linkShow},
		{"help", usage(
			"Usage: %s link show [DEV/PORT_INDEX]",
			"Usage: %s link add NAME type TYPE netdev NETDEV",
			"Usage: %s link delete NAME")},
	}, "link command")
}

func linkShow(r *rd) error {
	return r.execLink(func(r *rd) error {
		if r.portIdx == 0 {
			return nil
		}
		return r.execCmd(linkNoArgs, nil, "parameter")
	}, true)
}

// linkNoArgs is link_no_args: one port's PORT_GET doit.
func linkNoArgs(r *rd) error {
	return r.talk(xtcpnl.BuildRdmaPortGetRequest(r.devIdx, r.portIdx, seq()), func(m Msg) error {
		pt, err := xtcpnl.ParseRdmaPort(m.Body)
		if err != nil {
			return errCB
		}
		return r.linkLine(pt)
	})
}

// linkLine is link_parse_cb.
func (r *rd) linkLine(pt xtcpnl.RdmaPort) error {
	if !pt.HasDevIndex || !pt.HasDevName {
		return errCB
	}
	if !pt.HasPortIndex {
		r.errf("This tool doesn't support switches yet\n")
		return errCB
	}
	p := r.p
	p.openObject()
	p.printUint(printJSON, "ifindex", "", pt.DevIndex)
	p.printString(printAny, "ifname", "link %s/", pt.DevName)
	p.printUint(printAny, "port", "%d ", pt.PortIndex)
	if pt.HasSubnetPrefix {
		p.printString(printAny, "subnet_prefix", "subnet_prefix %s ", guidStr(pt.SubnetPrefix))
	}
	if pt.HasLID {
		p.printUint(printAny, "lid", "lid %d ", pt.LID)
	}
	if pt.HasSMLID {
		p.printUint(printAny, "sm_lid", "sm_lid %d ", pt.SMLID)
	}
	if pt.HasLMC {
		p.printUint(printAny, "lmc", "lmc %d ", uint32(pt.LMC))
	}
	if pt.HasState {
		p.printString(printAny, "state", "state %s ", linkStateStr(pt.State))
	}
	if pt.HasPhysState {
		p.printString(printAny, "physical_state", "physical_state %s ", physStateStr(pt.PhysState))
	}
	if pt.HasNdevName && pt.HasNdevIndex {
		p.printString(printAny, "netdev", "netdev %s ", pt.NdevName)
		format := ""
		if r.showDetails {
			format = "netdev_index %d "
		}
		p.printUint(printAny, "netdev_index", format, pt.NdevIndex)
	}
	if r.showDetails && pt.HasCapFlags {
		r.printCaps(pt.CapFlags, portCapNames)
	}
	p.closeObject()
	p.newline()
	return nil
}

// linkStateStr is link_state_to_str.
func linkStateStr(s uint8) string {
	return tableStr([]string{"NOP", "DOWN", "INIT", "ARMED", "ACTIVE", "ACTIVE_DEFER"}, uint32(s), "UNKNOWN")
}

// physStateStr is phys_state_to_str.
func physStateStr(s uint8) string {
	return tableStr([]string{"NOP", "SLEEP", "POLLING", "DISABLED", "ARMED", "LINK_UP",
		"LINK_ERROR_RECOVER", "PHY_TEST", "UNKNOWN", "OPA_OFFLINE", "UNKNOWN", "OPA_TEST"}, uint32(s), "UNKNOWN")
}

// cmdSys is cmd_sys (rdma/sys.c).
func cmdSys(r *rd) error {
	return r.execCmd(sysShow, []command{
		{"show", sysShow},
		{"set", notImplemented("system set")},
		{"help", usage(
			"Usage: %s system show [ netns ]",
			"       %s system set netns { shared | exclusive }")},
	}, "system command")
}

func sysShow(r *rd) error {
	return r.execCmd(sysShowNoArgs, []command{
		{"netns", sysShowNoArgs},
		{"privileged-qkey", sysShowNoArgs},
		{"monitor", sysShowNoArgs},
	}, "parameter")
}

// sysShowNoArgs is sys_show_no_args. It never opens a JSON object, so -j
// prints the text line.
func sysShowNoArgs(r *rd) error {
	return r.talk(xtcpnl.BuildRdmaSysGetRequest(seq()), func(m Msg) error {
		s, err := xtcpnl.ParseRdmaSys(m.Body)
		if err != nil {
			return errCB
		}
		r.sysLine(s)
		return nil
	})
}

// sysLine is sys_show_parse_cb: netns and privileged-qkey when sent, and
// monitor and copy-on-fork always, off when not sent.
func (r *rd) sysLine(s xtcpnl.RdmaSys) {
	p := r.p
	if s.HasNetnsMode {
		p.printString(printAny, "netns", "netns %s ", tableStr([]string{"exclusive", "shared"}, uint32(s.NetnsMode), "unknown"))
	}
	if s.HasPrivilegedQKeyMode {
		p.printOnOff(printAny, "privileged-qkey", "privileged-qkey %s ", s.PrivilegedQKeyMode != 0)
	}
	p.printOnOff(printAny, "monitor", "monitor %s ", s.MonitorMode != 0)
	p.printOnOff(printAny, "copy-on-fork", "copy-on-fork %s", s.CopyOnFork != 0)
	p.printNl()
}

// cmdStat is cmd_stat (rdma/stat.c). Only `statistic show [link ...]` is
// a read xtcpnl builds.
func cmdStat(r *rd) error {
	return r.execCmd(statShow, []command{
		{"show", statShow},
		{"list", statShow},
		{"help", statHelp},
		{"qp", notImplemented("statistic qp")},
		{"mr", notImplemented("statistic mr")},
		{"mode", notImplemented("statistic mode")},
		{"set", notImplemented("statistic set")},
		{"unset", notImplemented("statistic unset")},
	}, "statistic command")
}

func statShow(r *rd) error {
	return r.execCmd(statShowLink, []command{
		{"link", statShowLink},
		{"mr", notImplemented("statistic show mr")},
		{"help", statHelp},
	}, "parameter")
}

func statShowLink(r *rd) error {
	return r.execLink(statShowOneLink, false)
}

// statShowOneLink is stat_show_one_link: a port's STAT_GET.
func statShowOneLink(r *rd) error {
	if r.portIdx == 0 {
		return nil
	}
	return r.talk(xtcpnl.BuildRdmaStatGetRequest(r.devIdx, r.portIdx, seq()), func(m Msg) error {
		s, err := xtcpnl.ParseRdmaStat(m.Body)
		if err != nil {
			return errCB
		}
		return r.statLine(s)
	})
}

// statLine is stat_show_parse_cb with res_get_hwcounters. xtcpnl does not
// say whether STAT_HWCOUNTERS was sent, so a port without it prints its
// link line where rdma would fail the reply.
func (r *rd) statLine(s xtcpnl.RdmaStat) error {
	if !s.HasDevIndex || !s.HasDevName || !s.HasPortIndex {
		return errCB
	}
	p := r.p
	p.openObject()
	p.printString(printAny, "ifname", "link %s/", s.DevName)
	p.printUint(printAny, "port", "%d ", s.PortIndex)
	var err error
	for _, c := range s.HwCounters {
		if c.Name == "" || !c.HasValue {
			err = errCB
			break
		}
		p.newlineIndent()
		r.resU64(c.Name, c.Value, true)
	}
	p.closeObject()
	p.newline()
	return err
}

var statHelp = usage(
	"Usage: %s [ OPTIONS ] statistic { COMMAND | help }",
	"       %s statistic OBJECT show",
	"       %s statistic OBJECT show link [ DEV/PORT_INDEX ] [ FILTER-NAME FILTER-VALUE ]",
	"       %s statistic OBJECT mode",
	"       %s statistic OBJECT set COUNTER_SCOPE [DEV/PORT_INDEX] auto {CRITERIA | off}",
	"       %s statistic OBJECT bind COUNTER_SCOPE [DEV/PORT_INDEX] [OBJECT-ID] [COUNTER-ID]",
	"       %s statistic OBJECT unbind COUNTER_SCOPE [DEV/PORT_INDEX] [COUNTER-ID]",
	"       %s statistic show",
	"       %s statistic show link [ DEV/PORT_INDEX ]",
	"       %s statistic mode [ supported ]",
	"       %s statistic mode [ supported ] link [ DEV/PORT_INDEX ]",
	"       %s statistic set link [ DEV/PORT_INDEX ] optional-counters [ OPTIONAL-COUNTERS ]",
	"       %s statistic unset link [ DEV/PORT_INDEX ] optional-counters",
	"where  OBJECT: = { qp }",
	"       CRITERIA : = { type }",
	"       COUNTER_SCOPE: = { link | dev }",
	"       FILTER_NAME: = { cntn | lqpn | pid }",
	"Examples:",
	"       %s statistic qp show",
	"       %s statistic qp show link mlx5_2/1",
	"       %s statistic qp mode",
	"       %s statistic qp mode link mlx5_0",
	"       %s statistic qp set link mlx5_2/1 auto type on",
	"       %s statistic qp set link mlx5_2/1 auto off",
	"       %s statistic qp bind link mlx5_2/1 lqpn 178",
	"       %s statistic qp bind link mlx5_2/1 lqpn 178 cntn 4",
	"       %s statistic qp unbind link mlx5_2/1 cntn 4",
	"       %s statistic qp unbind link mlx5_2/1 cntn 4 lqpn 178",
	"       %s statistic show",
	"       %s statistic show link mlx5_2/1",
	"       %s statistic mode",
	"       %s statistic mode link mlx5_2/1",
	"       %s statistic mode supported",
	"       %s statistic mode supported link mlx5_2/1",
	"       %s statistic set link mlx5_2/1 optional-counters cc_rx_ce_pkts,cc_rx_cnp_pkts",
	"       %s statistic unset link mlx5_2/1 optional-counters",
)

// cmdMon is cmd_mon (rdma/monitor.c).
func cmdMon(r *rd) error {
	return r.execCmd(monShow, []command{
		{"help", usage("Usage: rdma monitor [ -j ]")},
	}, "mon command")
}

// monShow is mon_show: SYS_GET's MONITOR_MODE says whether the kernel
// sends events, then the events are printed until the stream ends.
func monShow(r *rd) error {
	supported := false
	err := r.talk(xtcpnl.BuildRdmaSysGetRequest(seq()), func(m Msg) error {
		s, err := xtcpnl.ParseRdmaSys(m.Body)
		if err == nil && s.HasMonitorMode {
			supported = s.MonitorMode != 0
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, ErrReplay) {
			r.errf("Failed to check if RDMA monitoring is supported\n")
		}
		return err
	}
	if !supported {
		r.errf("RDMA monitoring is not supported by the kernel\n")
		return errFail
	}
	r.p.newJSONObj()
	var cbErr error
	ended := false
	errno, err := r.src.Listen(xtcpnl.RdmaNlGroupNotify, func(m Msg) bool {
		ended, cbErr = r.cbOne(m, 0, 0, r.monLine)
		return !ended && cbErr == nil
	})
	if err != nil {
		r.errf("Failed to open NETLINK_RDMA socket. Error: %s\n", strerror(errnoOf(err)))
		return errFail
	}
	if cbErr == nil && !ended && errno != 0 {
		cbErr = recvError{errno}
	}
	if cbErr != nil {
		r.errf("Failed to listen to rdma socket\n")
		r.p.deleteJSONObj()
		return errFail
	}
	// The stream ended, which a kernel's does not: rdma is stopped by a
	// signal, before delete_json_obj, so the JSON array stays open.
	return nil
}

// monLine is mon_show_cb.
func (r *rd) monLine(m Msg) error {
	e, err := xtcpnl.ParseRdmaEvent(m.Body)
	if err != nil || !e.HasEventType {
		return errCB
	}
	p := r.p
	p.openObject()
	names := []string{xtcpnl.RdmaRegisterEvent: "[REGISTER]", xtcpnl.RdmaUnregisterEvent: "[UNREGISTER]",
		xtcpnl.RdmaNetdevAttachEvent: "[NETDEV_ATTACH]", xtcpnl.RdmaNetdevDetachEvent: "[NETDEV_DETACH]",
		xtcpnl.RdmaRenameEvent: "[RENAME]", xtcpnl.RdmaNetdevRenameEvent: "[NETDEV_RENAME]"}
	p.printString(printAny, "event_type", "%s\t", tableStr(names, uint32(e.EventType), fmt.Sprintf("[UNKNOWN 0x%02x]", e.EventType)))
	if e.HasDevIndex {
		p.printUint(printAny, "rdma_index", "dev %d", e.DevIndex)
	}
	if e.HasDevName {
		p.printString(printAny, "rdma_dev", " %s", e.DevName)
	}
	if e.HasPortIndex {
		p.printUint(printAny, "port", " port %d", e.PortIndex)
	}
	if e.HasNdevIndex {
		p.printUint(printAny, "netdev_idx", " netdev %d", e.NdevIndex)
	}
	if e.HasNdevName {
		p.printString(printAny, "netdev_name", " %s", e.NdevName)
	}
	p.closeObject()
	p.newline()
	return nil
}
