package xtcpnl

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// C4-T01, the corpus completeness gate: every message type ip 7.1.0 names
// is in a committed DLT 253 pcap, or is excluded with its reason
// (docs/netlink/goip-parity-gaps.md, "The message-type assessment").

// xfrmMsgReport is XFRM_MSG_REPORT (linux/xfrm.h), the one XFRM type the
// corpus has no capture of.
const xfrmMsgReport = 32

// universeType is one message type in the universe: its name, protocol,
// genl family (empty for rtnetlink and XFRM), type or genl command, and
// the first place iproute2 v7.1.0's ip/ or lib/ names it. A genl command
// with request set counts only in ip's request, since replies and events
// reuse command numbers (mptcp_pm's CREATED event is 1, as ADD_ADDR is).
type universeType struct {
	name    string
	proto   uint16
	family  string
	typ     uint16
	request bool
	src     string
}

// netlinkUniverse is every RTM_*, genl command and XFRM_MSG_* name in
// iproute2 v7.1.0's ip/*.c and lib/*.c: 47 rtnetlink, 46 genl and 22 XFRM.
var netlinkUniverse = []universeType{
	{name: "RTM_DELADDR", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELADDR, src: "ip/ipaddress.c:1524"},
	{name: "RTM_DELADDRLABEL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELADDRLABEL, src: "ip/ipaddrlabel.c:51"},
	{name: "RTM_DELANYCAST", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELANYCAST, src: "ip/ipaddress.c:1528"},
	{name: "RTM_DELLINK", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELLINK, src: "ip/ipaddress.c:985"},
	{name: "RTM_DELLINKPROP", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELLINKPROP, src: "ip/iplink.c:1484"},
	{name: "RTM_DELMULTICAST", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELMULTICAST, src: "ip/ipaddress.c:1526"},
	{name: "RTM_DELNEIGH", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELNEIGH, src: "ip/ipmonitor.c:117"},
	{name: "RTM_DELNETCONF", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELNETCONF, src: "ip/ipmonitor.c:143"},
	{name: "RTM_DELNEXTHOP", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELNEXTHOP, src: "ip/ipmonitor.c:91"},
	{name: "RTM_DELNEXTHOPBUCKET", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELNEXTHOPBUCKET, src: "ip/ipmonitor.c:96"},
	{name: "RTM_DELNSID", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELNSID, src: "ip/ipmonitor.c:147"},
	{name: "RTM_DELROUTE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELROUTE, src: "ip/ipmonitor.c:68"},
	{name: "RTM_DELRULE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_DELRULE, src: "ip/ipmonitor.c:134"},
	{name: "RTM_GETADDR", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETADDR, src: "lib/libnetlink.c:322"},
	{name: "RTM_GETADDRLABEL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETADDRLABEL, src: "lib/libnetlink.c:346"},
	{name: "RTM_GETLINK", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETLINK, src: "ip/ipaddress.c:2070"},
	{name: "RTM_GETMDB", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETMDB, src: "lib/libnetlink.c:446"},
	{name: "RTM_GETNEIGH", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNEIGH, src: "ip/ipmonitor.c:118"},
	{name: "RTM_GETNEIGHTBL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNEIGHTBL, src: "lib/libnetlink.c:430"},
	{name: "RTM_GETNETCONF", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNETCONF, src: "ip/ipnetconf.c:166"},
	{name: "RTM_GETNEXTHOP", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNEXTHOP, src: "ip/ipnexthop.c:636"},
	{name: "RTM_GETNEXTHOPBUCKET", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNEXTHOPBUCKET, src: "ip/ipnexthop.c:1354"},
	{name: "RTM_GETNSID", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETNSID, src: "ip/ipnetns.c:73"},
	{name: "RTM_GETROUTE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETROUTE, src: "ip/iproute.c:2051"},
	{name: "RTM_GETRULE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETRULE, src: "lib/libnetlink.c:388"},
	{name: "RTM_GETSTATS", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETSTATS, src: "ip/ipstats.c:837"},
	{name: "RTM_GETTUNNEL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETTUNNEL, src: "lib/libnetlink.c:1575"},
	{name: "RTM_GETVLAN", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETVLAN, src: "lib/libnetlink.c:463"},
	{name: "RTM_NEWADDR", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWADDR, src: "ip/ipaddress.c:1523"},
	{name: "RTM_NEWADDRLABEL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWADDRLABEL, src: "ip/ipaddrlabel.c:51"},
	{name: "RTM_NEWANYCAST", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWANYCAST, src: "ip/ipaddress.c:1527"},
	{name: "RTM_NEWLINK", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWLINK, src: "ip/ipaddress.c:985"},
	{name: "RTM_NEWLINKPROP", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWLINKPROP, src: "ip/iplink.c:1482"},
	{name: "RTM_NEWMULTICAST", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWMULTICAST, src: "ip/ipaddress.c:1525"},
	{name: "RTM_NEWNEIGH", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNEIGH, src: "ip/ipmonitor.c:116"},
	{name: "RTM_NEWNEIGHTBL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNEIGHTBL, src: "ip/ipntable.c:541"},
	{name: "RTM_NEWNETCONF", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNETCONF, src: "ip/ipmonitor.c:142"},
	{name: "RTM_NEWNEXTHOP", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNEXTHOP, src: "ip/ipmonitor.c:90"},
	{name: "RTM_NEWNEXTHOPBUCKET", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNEXTHOPBUCKET, src: "ip/ipmonitor.c:95"},
	{name: "RTM_NEWNSID", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWNSID, src: "ip/ipmonitor.c:148"},
	{name: "RTM_NEWPREFIX", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWPREFIX, src: "ip/ipmonitor.c:129"},
	{name: "RTM_NEWROUTE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWROUTE, src: "ip/ipmonitor.c:67"},
	{name: "RTM_NEWRULE", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWRULE, src: "ip/ipmonitor.c:133"},
	{name: "RTM_NEWSTATS", proto: unix.NETLINK_ROUTE, typ: unix.RTM_NEWSTATS, src: "ip/ipmonitor.c:152"},
	{name: "RTM_SETLINK", proto: unix.NETLINK_ROUTE, typ: unix.RTM_SETLINK, src: "ip/iptoken.c:131"},
	{name: "RTM_SETNEIGHTBL", proto: unix.NETLINK_ROUTE, typ: unix.RTM_SETNEIGHTBL, src: "ip/ipntable.c:690"},
	{name: "RTM_SETSTATS", proto: unix.NETLINK_ROUTE, typ: unix.RTM_SETSTATS, src: "ip/ipstats.c:1247"},
	{name: "CTRL_CMD_GETFAMILY", proto: unix.NETLINK_GENERIC, family: "nlctrl", typ: unix.CTRL_CMD_GETFAMILY, request: true, src: "lib/libgenl.c:52"},
	{name: "CTRL_CMD_GETPOLICY", proto: unix.NETLINK_GENERIC, family: "nlctrl", typ: unix.CTRL_CMD_GETPOLICY, request: true, src: "lib/mnl_utils.c:367"},
	{name: "CTRL_CMD_NEWFAMILY", proto: unix.NETLINK_GENERIC, family: "nlctrl", typ: unix.CTRL_CMD_NEWFAMILY, src: "lib/libgenl.c:34"},
	{name: "FOU_CMD_ADD", proto: unix.NETLINK_GENERIC, family: "fou", typ: fouCmdAdd, request: true, src: "ip/ipfou.c:193"},
	{name: "FOU_CMD_DEL", proto: unix.NETLINK_GENERIC, family: "fou", typ: fouCmdDel, request: true, src: "ip/ipfou.c:205"},
	{name: "FOU_CMD_GET", proto: unix.NETLINK_GENERIC, family: "fou", typ: fouCmdGet, request: true, src: "ip/ipfou.c:305"},
	{name: "ILA_CMD_ADD", proto: unix.NETLINK_GENERIC, family: "ila", typ: ilaCmdAdd, request: true, src: "ip/ipila.c:266"},
	{name: "ILA_CMD_DEL", proto: unix.NETLINK_GENERIC, family: "ila", typ: ilaCmdDel, request: true, src: "ip/ipila.c:278"},
	{name: "ILA_CMD_GET", proto: unix.NETLINK_GENERIC, family: "ila", typ: ilaCmdGet, request: true, src: "ip/ipila.c:142"},
	{name: "IOAM6_CMD_ADD_NAMESPACE", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdAddNamespace, request: true, src: "ip/ipioam6.c:213"},
	{name: "IOAM6_CMD_ADD_SCHEMA", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdAddSchema, request: true, src: "ip/ipioam6.c:229"},
	{name: "IOAM6_CMD_DEL_NAMESPACE", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdDelNamespace, request: true, src: "ip/ipioam6.c:222"},
	{name: "IOAM6_CMD_DEL_SCHEMA", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdDelSchema, request: true, src: "ip/ipioam6.c:234"},
	{name: "IOAM6_CMD_DUMP_NAMESPACES", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdDumpNamespaces, request: true, src: "ip/ipioam6.c:151"},
	{name: "IOAM6_CMD_DUMP_SCHEMAS", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdDumpSchemas, request: true, src: "ip/ipioam6.c:154"},
	{name: "IOAM6_CMD_NS_SET_SCHEMA", proto: unix.NETLINK_GENERIC, family: "IOAM6", typ: ioam6CmdNsSetSchema, request: true, src: "ip/ipioam6.c:237"},
	{name: "L2TP_CMD_SESSION_CREATE", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdSessionCreate, request: true, src: "ip/ipl2tp.c:144"},
	{name: "L2TP_CMD_SESSION_DELETE", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdSessionDelete, request: true, src: "ip/ipl2tp.c:179"},
	{name: "L2TP_CMD_SESSION_GET", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdSessionGet, request: true, src: "ip/ipl2tp.c:450"},
	{name: "L2TP_CMD_TUNNEL_CREATE", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdTunnelCreate, request: true, src: "ip/ipl2tp.c:94"},
	{name: "L2TP_CMD_TUNNEL_DELETE", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdTunnelDelete, request: true, src: "ip/ipl2tp.c:131"},
	{name: "L2TP_CMD_TUNNEL_GET", proto: unix.NETLINK_GENERIC, family: "l2tp", typ: l2tpCmdTunnelGet, request: true, src: "ip/ipl2tp.c:487"},
	{name: "MACSEC_CMD_ADD_RXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdAddRxsa, request: true, src: "ip/ipmacsec.c:393"},
	{name: "MACSEC_CMD_ADD_RXSC", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdAddRxsc, request: true, src: "ip/ipmacsec.c:392"},
	{name: "MACSEC_CMD_ADD_TXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdAddTxsa, request: true, src: "ip/ipmacsec.c:393"},
	{name: "MACSEC_CMD_DEL_RXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdDelRxsa, request: true, src: "ip/ipmacsec.c:401"},
	{name: "MACSEC_CMD_DEL_RXSC", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdDelRxsc, request: true, src: "ip/ipmacsec.c:400"},
	{name: "MACSEC_CMD_DEL_TXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdDelTxsa, request: true, src: "ip/ipmacsec.c:401"},
	{name: "MACSEC_CMD_GET_TXSC", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdGetTxsc, request: true, src: "ip/ipmacsec.c:1083"},
	{name: "MACSEC_CMD_UPD_OFFLOAD", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdUpdOffload, request: true, src: "ip/ipmacsec.c:404"},
	{name: "MACSEC_CMD_UPD_RXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdUpdRxsa, request: true, src: "ip/ipmacsec.c:397"},
	{name: "MACSEC_CMD_UPD_RXSC", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdUpdRxsc, request: true, src: "ip/ipmacsec.c:396"},
	{name: "MACSEC_CMD_UPD_TXSA", proto: unix.NETLINK_GENERIC, family: "macsec", typ: macsecCmdUpdTxsa, request: true, src: "ip/ipmacsec.c:397"},
	{name: "MPTCP_PM_CMD_ADD_ADDR", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdAddAddr, request: true, src: "ip/ipmptcp.c:111"},
	{name: "MPTCP_PM_CMD_DEL_ADDR", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdDelAddr, request: true, src: "ip/ipmptcp.c:112"},
	{name: "MPTCP_PM_CMD_FLUSH_ADDRS", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdFlushAddrs, request: true, src: "ip/ipmptcp.c:354"},
	{name: "MPTCP_PM_CMD_GET_ADDR", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdGetAddr, request: true, src: "ip/ipmptcp.c:310"},
	{name: "MPTCP_PM_CMD_GET_LIMITS", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdGetLimits, request: true, src: "ip/ipmptcp.c:435"},
	{name: "MPTCP_PM_CMD_SET_FLAGS", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdSetFlags, request: true, src: "ip/ipmptcp.c:110"},
	{name: "MPTCP_PM_CMD_SET_LIMITS", proto: unix.NETLINK_GENERIC, family: "mptcp_pm", typ: mptcpCmdSetLimits, request: true, src: "ip/ipmptcp.c:662"},
	{name: "SEG6_CMD_DUMPHMAC", proto: unix.NETLINK_GENERIC, family: "SEG6", typ: uint16(Seg6CmdDumpHmac), request: true, src: "ip/ipseg6.c:119"},
	{name: "SEG6_CMD_GET_TUNSRC", proto: unix.NETLINK_GENERIC, family: "SEG6", typ: uint16(Seg6CmdGetTunsrc), request: true, src: "ip/ipseg6.c:123"},
	{name: "SEG6_CMD_SETHMAC", proto: unix.NETLINK_GENERIC, family: "SEG6", typ: uint16(Seg6CmdSetHmac), request: true, src: "ip/ipseg6.c:150"},
	{name: "SEG6_CMD_SET_TUNSRC", proto: unix.NETLINK_GENERIC, family: "SEG6", typ: uint16(Seg6CmdSetTunsrc), request: true, src: "ip/ipseg6.c:161"},
	{name: "TCP_METRICS_CMD_DEL", proto: unix.NETLINK_GENERIC, family: "tcp_metrics", typ: uint16(TCPMetricsCmdDel), request: true, src: "ip/tcp_metrics.c:233"},
	{name: "TCP_METRICS_CMD_GET", proto: unix.NETLINK_GENERIC, family: "tcp_metrics", typ: uint16(TCPMetricsCmdGet), request: true, src: "ip/tcp_metrics.c:174"},
	{name: "XFRM_MSG_ACQUIRE", proto: unix.NETLINK_XFRM, typ: xfrmMsgAcquire, src: "ip/xfrm_monitor.c:287"},
	{name: "XFRM_MSG_ALLOCSPI", proto: unix.NETLINK_XFRM, typ: xfrmMsgAllocSPI, src: "ip/xfrm_state.c:799"},
	{name: "XFRM_MSG_DELPOLICY", proto: unix.NETLINK_XFRM, typ: xfrmMsgDelPolicy, src: "ip/xfrm_monitor.c:282"},
	{name: "XFRM_MSG_DELSA", proto: unix.NETLINK_XFRM, typ: xfrmMsgDelSA, src: "ip/xfrm_monitor.c:276"},
	{name: "XFRM_MSG_EXPIRE", proto: unix.NETLINK_XFRM, typ: xfrmMsgExpire, src: "ip/xfrm_monitor.c:278"},
	{name: "XFRM_MSG_FLUSHPOLICY", proto: unix.NETLINK_XFRM, typ: xfrmMsgFlushPolicy, src: "ip/xfrm_monitor.c:293"},
	{name: "XFRM_MSG_FLUSHSA", proto: unix.NETLINK_XFRM, typ: xfrmMsgFlushSA, src: "ip/xfrm_monitor.c:290"},
	{name: "XFRM_MSG_GETDEFAULT", proto: unix.NETLINK_XFRM, typ: xfrmMsgGetDefault, src: "ip/xfrm_monitor.c:305"},
	{name: "XFRM_MSG_GETPOLICY", proto: unix.NETLINK_XFRM, typ: xfrmMsgGetPolicy, src: "ip/xfrm_policy.c:584"},
	{name: "XFRM_MSG_GETSA", proto: unix.NETLINK_XFRM, typ: xfrmMsgGetSA, src: "ip/xfrm_state.c:1078"},
	{name: "XFRM_MSG_GETSADINFO", proto: unix.NETLINK_XFRM, typ: xfrmMsgGetSADInfo, src: "ip/xfrm_state.c:1419"},
	{name: "XFRM_MSG_GETSPDINFO", proto: unix.NETLINK_XFRM, typ: xfrmMsgGetSPDInfo, src: "ip/xfrm_policy.c:1119"},
	{name: "XFRM_MSG_MAPPING", proto: unix.NETLINK_XFRM, typ: xfrmMsgMapping, src: "ip/xfrm_monitor.c:302"},
	{name: "XFRM_MSG_NEWAE", proto: unix.NETLINK_XFRM, typ: xfrmMsgNewAE, src: "ip/xfrm_monitor.c:299"},
	{name: "XFRM_MSG_NEWPOLICY", proto: unix.NETLINK_XFRM, typ: xfrmMsgNewPolicy, src: "ip/xfrm_monitor.c:281"},
	{name: "XFRM_MSG_NEWSA", proto: unix.NETLINK_XFRM, typ: xfrmMsgNewSA, src: "ip/xfrm_monitor.c:275"},
	{name: "XFRM_MSG_NEWSPDINFO", proto: unix.NETLINK_XFRM, typ: xfrmMsgNewSPDInfo, src: "ip/xfrm_policy.c:1054"},
	{name: "XFRM_MSG_POLEXPIRE", proto: unix.NETLINK_XFRM, typ: xfrmMsgPolExpire, src: "ip/xfrm_monitor.c:284"},
	{name: "XFRM_MSG_REPORT", proto: unix.NETLINK_XFRM, typ: xfrmMsgReport, src: "ip/xfrm_monitor.c:296"},
	{name: "XFRM_MSG_SETDEFAULT", proto: unix.NETLINK_XFRM, typ: xfrmMsgSetDefault, src: "ip/xfrm_policy.c:1175"},
	{name: "XFRM_MSG_UPDPOLICY", proto: unix.NETLINK_XFRM, typ: xfrmMsgUpdPolicy, src: "ip/xfrm_monitor.c:283"},
	{name: "XFRM_MSG_UPDSA", proto: unix.NETLINK_XFRM, typ: xfrmMsgUpdSA, src: "ip/xfrm_monitor.c:277"},
}

// universeExclusions are the universe types the corpus has no capture of,
// each with its reason. An exclusion is stale once a pcap holds the type.
var universeExclusions = map[string]string{
	"RTM_GETMDB":           "bridge only: lib/libnetlink.c:446's rtnl_mdbdump_req is called only from bridge/mdb.c",
	"RTM_GETVLAN":          "bridge only: lib/libnetlink.c:463's rtnl_brvlandump_req is called only from bridge/vlan.c",
	"RTM_GETTUNNEL":        "bridge only: lib/libnetlink.c:1575's rtnl_tunneldump_req is called only from bridge/vni.c",
	"RTM_DELNEXTHOPBUCKET": "unprovokable: the kernel neither handles nor sends it; ip/ipmonitor.c only names it to print",
	"CTRL_CMD_GETPOLICY":   "devlink only: lib/mnl_utils.c:367's mnlu_gen_cmd_dump_policy is called only from devlink/devlink.c",
	"XFRM_MSG_REPORT":      "unprovokable: km_report's one caller is MIPv6 route optimization (net/ipv6/mip6.c), which no ip command drives",
}

// universeCount is how many messages of a type the corpus holds, and in how
// many pcaps.
type universeCount struct{ msgs, pcaps int }

// universeMatch reports whether m is a message of type u. families maps a
// pcap's genl family ids to their names.
func universeMatch(u universeType, m capturedNlMsg, families map[uint16]string) bool {
	if m.proto != u.proto {
		return false
	}
	if u.proto != unix.NETLINK_GENERIC {
		return m.h.Type == u.typ
	}
	if len(m.body) == 0 || families[m.h.Type] != u.family {
		return false
	}
	if u.request && m.h.Flags&unix.NLM_F_REQUEST == 0 {
		return false
	}
	return uint16(m.body[0]) == u.typ
}

// pcapGenlFamilies maps the genl family ids in a pcap's NEWFAMILY replies to
// their names. The controller's own id is fixed.
func pcapGenlFamilies(tb testing.TB, msgs []capturedNlMsg) map[uint16]string {
	tb.Helper()
	families := map[uint16]string{GenlControllerID: "nlctrl"}
	for _, m := range msgs {
		if m.proto != unix.NETLINK_GENERIC || m.h.Type != GenlControllerID ||
			m.h.Flags&unix.NLM_F_REQUEST != 0 || len(m.body) == 0 || m.body[0] != unix.CTRL_CMD_NEWFAMILY {
			continue
		}
		f, err := ParseGenericNetlinkFamily(m.body)
		if err != nil {
			tb.Fatal(err)
		}
		families[f.ID] = f.Name
	}
	return families
}

// universeWalk counts every universe type in the DLT 253 pcaps under root,
// and returns the counts and the number of pcaps read. A pcapng file or a
// pcap of another link type is skipped; one that doesn't parse fails the
// test.
func universeWalk(t *testing.T, root string) (map[string]universeCount, int) {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pcap") {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]universeCount{}
	npcaps := 0
	for _, path := range paths {
		bs, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ParseNetlinkPcap(bs); errors.Is(err, ErrPcapNotNetlink) || errors.Is(err, ErrPcapBadMagic) {
			continue
		}
		npcaps++
		msgs := pcapNlMsgs(t, path)
		families := pcapGenlFamilies(t, msgs)
		for _, u := range netlinkUniverse {
			n := 0
			for _, m := range msgs {
				if universeMatch(u, m, families) {
					n++
				}
			}
			if n > 0 {
				c := seen[u.name]
				seen[u.name] = universeCount{msgs: c.msgs + n, pcaps: c.pcaps + 1}
			}
		}
	}
	return seen, npcaps
}

// universeProblems returns, sorted, every way the corpus and the lists
// fail the gate: a type in no pcap and not excluded, an exclusion with no
// reason, a stale exclusion, an exclusion of no universe type, a type with
// no source, and a name listed twice.
func universeProblems(universe []universeType, excl map[string]string, seen map[string]universeCount) []string {
	var out []string
	named := map[string]bool{}
	for _, u := range universe {
		if named[u.name] {
			out = append(out, u.name+": listed twice")
		}
		named[u.name] = true
		if u.src == "" {
			out = append(out, u.name+": cites no iproute2 source")
		}
		reason, excluded := excl[u.name]
		n := seen[u.name].pcaps
		switch {
		case excluded && reason == "":
			out = append(out, u.name+": excluded with no reason")
		case excluded && n > 0:
			out = append(out, u.name+": in "+strconv.Itoa(n)+" pcaps and excluded; the exclusion is stale")
		case !excluded && n == 0:
			out = append(out, u.name+": in no pcap and not excluded")
		}
	}
	for name := range excl {
		if !named[name] {
			out = append(out, name+": excluded but not in the universe")
		}
	}
	slices.Sort(out)
	return out
}

func TestNetlinkUniverse(t *testing.T) {
	corpus, npcaps := universeWalk(t, "..")
	t.Logf("%d DLT 253 pcaps under pkg/", npcaps)
	for _, u := range netlinkUniverse {
		c := corpus[u.name]
		t.Logf("%-26s %5d msgs in %4d pcaps", u.name, c.msgs, c.pcaps)
	}

	// without returns a copy of the corpus counts without name.
	without := func(name string) map[string]universeCount {
		m := maps.Clone(corpus)
		delete(m, name)
		return m
	}
	// with returns a copy of the corpus counts with name in one pcap.
	with := func(name string) map[string]universeCount {
		m := maps.Clone(corpus)
		m[name] = universeCount{msgs: 1, pcaps: 1}
		return m
	}
	// exclWith returns a copy of the exclusions with name's reason set.
	exclWith := func(name, reason string) map[string]string {
		m := maps.Clone(universeExclusions)
		m[name] = reason
		return m
	}
	// once is the corpus at its least: every type that isn't excluded in
	// one message in one pcap.
	once := map[string]universeCount{}
	for _, u := range netlinkUniverse {
		if _, ok := universeExclusions[u.name]; !ok {
			once[u.name] = universeCount{msgs: 1, pcaps: 1}
		}
	}
	// uniWith returns a copy of the universe with u appended.
	uniWith := func(u universeType) []universeType {
		return append(slices.Clone(netlinkUniverse), u)
	}

	tests := []struct {
		description string
		universe    []universeType
		excl        map[string]string
		seen        map[string]universeCount
		expected    string
	}{
		{
			description: "positive: the committed corpus has every universe type, or excludes it with a reason",
			universe:    netlinkUniverse, excl: universeExclusions, seen: corpus,
			expected: "",
		},
		{
			description: "negative: MAPPING's pcap gone from the corpus",
			universe:    netlinkUniverse, excl: universeExclusions, seen: without("XFRM_MSG_MAPPING"),
			expected: "XFRM_MSG_MAPPING: in no pcap and not excluded",
		},
		{
			description: "negative: a genl command gone from the corpus",
			universe:    netlinkUniverse, excl: universeExclusions, seen: without("MPTCP_PM_CMD_ADD_ADDR"),
			expected: "MPTCP_PM_CMD_ADD_ADDR: in no pcap and not excluded",
		},
		{
			description: "negative: an exclusion with no reason",
			universe:    netlinkUniverse, excl: exclWith("XFRM_MSG_REPORT", ""), seen: corpus,
			expected: "XFRM_MSG_REPORT: excluded with no reason",
		},
		{
			description: "negative: a universe type that cites no source",
			universe:    uniWith(universeType{name: "RTM_GETMULTICAST", proto: unix.NETLINK_ROUTE, typ: unix.RTM_GETMULTICAST}),
			excl:        universeExclusions, seen: with("RTM_GETMULTICAST"),
			expected: "RTM_GETMULTICAST: cites no iproute2 source",
		},
		{
			description: "corner: a type both captured and excluded, so the exclusion is stale",
			universe:    netlinkUniverse, excl: universeExclusions, seen: with("XFRM_MSG_REPORT"),
			expected: "XFRM_MSG_REPORT: in 1 pcaps and excluded; the exclusion is stale",
		},
		{
			description: "corner: an exclusion of a type outside the universe",
			universe:    netlinkUniverse, excl: exclWith("RTM_GETANYCAST", "not sent by ip"), seen: corpus,
			expected: "RTM_GETANYCAST: excluded but not in the universe",
		},
		{
			description: "corner: a name listed twice",
			universe:    uniWith(netlinkUniverse[0]),
			excl:        universeExclusions, seen: corpus,
			expected: netlinkUniverse[0].name + ": listed twice",
		},
		{
			description: "boundary: one message in one pcap is enough",
			universe:    netlinkUniverse, excl: universeExclusions, seen: once,
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := strings.Join(universeProblems(tt.universe, tt.excl, tt.seen), "\n")
			if got != tt.expected {
				t.Errorf("got\n%s\nexpected\n%s", got, tt.expected)
			}
		})
	}
}

func TestNetlinkUniverseShape(t *testing.T) {
	byProto := map[uint16]int{}
	for _, u := range netlinkUniverse {
		byProto[u.proto]++
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "boundary: 47 rtnetlink names, 46 genl commands and 22 XFRM types",
			got: "route=" + strconv.Itoa(byProto[unix.NETLINK_ROUTE]) +
				" generic=" + strconv.Itoa(byProto[unix.NETLINK_GENERIC]) +
				" xfrm=" + strconv.Itoa(byProto[unix.NETLINK_XFRM]),
			expected: "route=47 generic=46 xfrm=22",
		},
		{
			description: "positive: six exclusions, three of them bridge's",
			got:         strings.Join(slices.Sorted(maps.Keys(universeExclusions)), " "),
			expected:    "CTRL_CMD_GETPOLICY RTM_DELNEXTHOPBUCKET RTM_GETMDB RTM_GETTUNNEL RTM_GETVLAN XFRM_MSG_REPORT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}

func TestNetlinkUniverseMatch(t *testing.T) {
	byName := func(name string) universeType {
		for _, u := range netlinkUniverse {
			if u.name == name {
				return u
			}
		}
		t.Fatalf("%s is not in the universe", name)
		return universeType{}
	}
	const mptcpID = 0x1d
	fams := map[uint16]string{GenlControllerID: "nlctrl", mptcpID: "mptcp_pm"}
	msg := func(proto, typ, flags uint16, body ...byte) capturedNlMsg {
		return capturedNlMsg{proto: proto, h: NlMsgHdr{Type: typ, Flags: flags}, body: body}
	}
	tests := []struct {
		description string
		name        string
		m           capturedNlMsg
		families    map[uint16]string
		expected    bool
	}{
		{
			description: "positive: an rtnetlink NEWLINK",
			name:        "RTM_NEWLINK", m: msg(unix.NETLINK_ROUTE, unix.RTM_NEWLINK, 0), families: fams,
			expected: true,
		},
		{
			description: "corner: type 16 over XFRM is NEWSA, not NEWLINK",
			name:        "RTM_NEWLINK", m: msg(unix.NETLINK_XFRM, unix.RTM_NEWLINK, 0), families: fams,
			expected: false,
		},
		{
			description: "positive: type 16 over XFRM is NEWSA",
			name:        "XFRM_MSG_NEWSA", m: msg(unix.NETLINK_XFRM, xfrmMsgNewSA, 0), families: fams,
			expected: true,
		},
		{
			description: "positive: ip's mptcp_pm ADD_ADDR request",
			name:        "MPTCP_PM_CMD_ADD_ADDR", m: msg(unix.NETLINK_GENERIC, mptcpID, unix.NLM_F_REQUEST|unix.NLM_F_ACK, mptcpCmdAddAddr, 1), families: fams,
			expected: true,
		},
		{
			description: "corner: mptcp_pm's CREATED event, command 1 with no request flag, is not ADD_ADDR",
			name:        "MPTCP_PM_CMD_ADD_ADDR", m: msg(unix.NETLINK_GENERIC, mptcpID, 0, mptcpCmdAddAddr, 1), families: fams,
			expected: false,
		},
		{
			description: "negative: command 1 in a family the pcap never resolved",
			name:        "MPTCP_PM_CMD_ADD_ADDR", m: msg(unix.NETLINK_GENERIC, mptcpID+1, unix.NLM_F_REQUEST, mptcpCmdAddAddr, 1), families: fams,
			expected: false,
		},
		{
			description: "positive: the controller's NEWFAMILY reply",
			name:        "CTRL_CMD_NEWFAMILY", m: msg(unix.NETLINK_GENERIC, GenlControllerID, unix.NLM_F_MULTI, unix.CTRL_CMD_NEWFAMILY, 2), families: fams,
			expected: true,
		},
		{
			description: "boundary: a genl message with no body",
			name:        "CTRL_CMD_NEWFAMILY", m: msg(unix.NETLINK_GENERIC, GenlControllerID, 0), families: fams,
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := universeMatch(byName(tt.name), tt.m, tt.families); got != tt.expected {
				t.Errorf("got %v, expected %v", got, tt.expected)
			}
		})
	}
}
