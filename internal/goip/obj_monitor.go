package goip

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip monitor` is do_ipmonitor (ip/ipmonitor.c): the objects named, or all,
// become multicast groups, and every message the socket is sent goes through
// accept_msg, which hands it to the object's print function, the one its show
// uses. goip does the same with its renderers. With `file FILE` the messages
// come from a file of them, rtnl_from_file's, and no socket is opened.
//
// The monitor prints text under -j as well: do_ipmonitor never opens a JSON
// writer, so every print_* call takes its text arm.

// ErrMonitorAbort is do_ipmonitor's exit(-1): an argument it refuses,
// `help`, and a file it cannot open. Status 255.
var ErrMonitorAbort = errors.New("monitor")

// ErrMonitorFile is rtnl_from_file's -1, a file whose walk fails, which
// do_ipmonitor returns and do_cmd negates. Status 1.
var ErrMonitorFile = errors.New("monitor file")

// ErrMonitorSetup is do_ipmonitor's exit(1): the socket would not open, a
// group it needs would not join, or NETLINK_LISTEN_ALL_NSID was refused.
var ErrMonitorSetup = errors.New("monitor setup")

// The IPMON_L* bits, do_ipmonitor's object mask.
const (
	ipmonLink uint32 = 1 << iota
	ipmonAddr
	ipmonRoute
	ipmonMroute
	ipmonPrefix
	ipmonNeigh
	ipmonNetconf
	ipmonStats
	ipmonRule
	ipmonNsid
	ipmonNexthop
	ipmonMaddr
	ipmonAcaddr
)

const (
	monitorUsageCst = "Usage: ip monitor [ all | OBJECTS ] [ FILE ] [ label ] [ all-nsid ]\n" +
		"                  [ dev DEVICE ]\n" +
		"OBJECTS :=  address | link | mroute | maddress | acaddress | neigh |\n" +
		"            netconf | nexthop | nsid | prefix | route | rule | stats\n" +
		"FILE := file FILENAME\n"
	monitorFileCst    = "file"
	monitorLabelCst   = "label"
	monitorAllCst     = "all"
	monitorAllNsidCst = "all-nsid"
	monitorHelpCst    = "help"
	// monitorBufCst is rtnl_from_file's buffer, the longest message it reads.
	monitorBufCst = 16384
	// nlmsgTstampCst is NLMSG_TSTAMP (include/libnetlink.h), the record
	// rtmon writes ahead of each batch in its file.
	nlmsgTstampCst = 15
	// The rtnetlink multicast families of an mroute, RTNL_FAMILY_IPMR and
	// RTNL_FAMILY_IP6MR.
	rtnlFamilyIPMRCst  = 128
	rtnlFamilyIP6MRCst = 129
	// ndOptPrefixInformationCst is ND_OPT_PREFIX_INFORMATION, the one ND
	// option type a prefixmsg carries.
	ndOptPrefixInformationCst = 3
)

// monitorObjects is do_ipmonitor's object words in its order. matches()
// takes a prefix, strcmp the whole word.
var monitorObjects = []struct {
	word   string
	prefix bool
	bit    uint32
}{
	{objLinkCst, true, ipmonLink},
	{"address", true, ipmonAddr},
	{"maddress", true, ipmonMaddr},
	{"acaddress", false, ipmonAcaddr},
	{objRouteCst, true, ipmonRoute},
	{"mroute", true, ipmonMroute},
	{"prefix", true, ipmonPrefix},
	{"neigh", true, ipmonNeigh},
	{"netconf", true, ipmonNetconf},
	{"rule", true, ipmonRule},
	{"nsid", true, ipmonNsid},
	{objNexthopCst, true, ipmonNexthop},
	{"stats", false, ipmonStats},
}

// monitorArgs is do_ipmonitor's argument loop's result.
type monitorArgs struct {
	file    string
	hasFile bool
	// label is prefix_banner: `label` or `all`.
	label bool
	// lmask is the objects named; zero is all of them.
	lmask   uint32
	allNsid bool
	// ifindex is `dev`'s, 0 for none.
	ifindex int32
}

// parseMonitorArgs is do_ipmonitor's loop, in its order: `file` and
// `label`, then the objects, then `all`, `all-nsid`, `help` and `dev`. All
// but `acaddress`, `stats`, `all` and `dev` are matches(), so the order
// decides an abbreviation: `l` is label, not link, and `al`, past
// `address`, is all-nsid. dev's name is resolved as it is read.
func parseMonitorArgs(args []string, index func(string) int32) (monitorArgs, error) {
	var a monitorArgs
	for i := 0; i < len(args); i++ {
		w := args[i]
		bit, isObject := monitorObject(w)
		switch {
		case matchesPrefix(w, monitorFileCst):
			if i+1 >= len(args) {
				return a, fmt.Errorf("%w: %w", ErrMonitorAbort, incompleteErr())
			}
			i++
			a.file, a.hasFile = args[i], true
		case matchesPrefix(w, monitorLabelCst):
			a.label = true
		case isObject:
			a.lmask |= bit
		case w == monitorAllCst:
			a.label = true
		case matchesPrefix(w, monitorAllNsidCst):
			a.allNsid = true
		case matchesPrefix(w, monitorHelpCst):
			return a, fmt.Errorf("%w: %s", ErrMonitorAbort, strings.TrimSuffix(monitorUsageCst, "\n"))
		case w == devKeywordCst:
			if i+1 >= len(args) {
				return a, fmt.Errorf("%w: %w", ErrMonitorAbort, incompleteErr())
			}
			i++
			if a.ifindex = index(args[i]); a.ifindex == 0 {
				return a, fmt.Errorf("%w: %w", ErrMonitorAbort, invargErr("Device does not exist\n", args[i]))
			}
		default:
			return a, fmt.Errorf("%w: Argument %q is unknown, try \"ip monitor help\"", ErrMonitorAbort, w)
		}
	}
	return a, nil
}

// monitorObject is the object word's bit, the first in do_ipmonitor's
// order that w matches.
func monitorObject(w string) (uint32, bool) {
	for _, o := range monitorObjects {
		if (o.prefix && matchesPrefix(w, o.word)) || (!o.prefix && w == o.word) {
			return o.bit, true
		}
	}
	return 0, false
}

// monitorAdd is one rtnl_add_nl_group, a group past 31 that the bind's mask
// cannot hold. A refusal other than EINVAL, the kernel's word for a group it
// does not have, ends the monitor when needed is set, with errMsg.
type monitorAdd struct {
	group  uint32
	needed bool
	errMsg string
}

// monitorSub is what do_ipmonitor subscribes: the bind's groups, the
// groups added after it, and NETLINK_LISTEN_ALL_NSID.
type monitorSub struct {
	groups  uint32
	adds    []monitorAdd
	allNsid bool
	// errOut is where rtnl_listen's ENOBUFS report goes.
	errOut io.Writer
}

// nlMgrp is nl_mgrp: a group's bit in the bind mask.
func nlMgrp(group uint32) uint32 {
	return 1 << (group - 1)
}

// monitorGroups is do_ipmonitor's mask to groups, family being -4, -6 or
// -0's preferred_family. No object named is every object. stats is needed
// only when named; the rest of the added groups always are.
func monitorGroups(lmask uint32, family uint8, allNsid bool) monitorSub {
	nmask := lmask
	if lmask == 0 {
		lmask = ^uint32(0)
	}
	fam := func(f uint8) bool { return family == unix.AF_UNSPEC || family == f }
	var s monitorSub
	s.allNsid = allNsid
	bind := func(on bool, group uint32) {
		if on {
			s.groups |= nlMgrp(group)
		}
	}
	add := func(on bool, group uint32, needed bool, what string) {
		if on {
			s.adds = append(s.adds, monitorAdd{group: group, needed: needed,
				errMsg: "Failed to add " + what + " group to list"})
		}
	}
	bind(lmask&ipmonLink != 0, unix.RTNLGRP_LINK)
	bind(lmask&ipmonAddr != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_IFADDR)
	bind(lmask&ipmonAddr != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_IFADDR)
	bind(lmask&ipmonRoute != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_ROUTE)
	bind(lmask&ipmonRoute != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_ROUTE)
	bind(lmask&ipmonRoute != 0 && fam(unix.AF_MPLS), unix.RTNLGRP_MPLS_ROUTE)
	bind(lmask&ipmonMroute != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_MROUTE)
	bind(lmask&ipmonMroute != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_MROUTE)
	bind(lmask&ipmonPrefix != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_PREFIX)
	bind(lmask&ipmonNeigh != 0, unix.RTNLGRP_NEIGH)
	bind(lmask&ipmonNetconf != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_NETCONF)
	bind(lmask&ipmonNetconf != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_NETCONF)
	bind(lmask&ipmonNetconf != 0 && fam(unix.AF_MPLS), unix.RTNLGRP_MPLS_NETCONF)
	bind(lmask&ipmonRule != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_RULE)
	bind(lmask&ipmonRule != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_RULE)
	bind(lmask&ipmonNsid != 0, unix.RTNLGRP_NSID)
	add(lmask&ipmonNexthop != 0, unix.RTNLGRP_NEXTHOP, true, objNexthopCst)
	add(lmask&ipmonStats != 0, unix.RTNLGRP_STATS, nmask&ipmonStats != 0, "stats")
	add(lmask&ipmonMaddr != 0 && fam(unix.AF_INET), unix.RTNLGRP_IPV4_MCADDR, true, "ipv4 mcaddr")
	add(lmask&ipmonMaddr != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_MCADDR, true, "ipv6 mcaddr")
	add(lmask&ipmonAcaddr != 0 && fam(unix.AF_INET6), unix.RTNLGRP_IPV6_ACADDR, true, "ipv6 acaddr")
	return s
}

// runMonitor is do_ipmonitor. The live run subscribes, then fills the link
// cache and the netns names as ll_init_map and netns_map_init do, the
// first on the subscribed socket, as `ip`'s is, then prints until it is
// killed. A `dev` is resolved before, by a dump of its own. A pcap replay
// subscribes too, and plays its capture's notifications.
func runMonitor(c *runCtx, args []string) error {
	svc := service.New(c.src, c.nextSeq)
	linked := false
	linkMap := func() error {
		if linked {
			return nil
		}
		linked = true
		return monitorLinkMap(c, svc)
	}
	var mapErr error
	a, err := parseMonitorArgs(args, func(name string) int32 {
		if mapErr = linkMap(); mapErr != nil {
			return 0
		}
		return c.lltab.NameToIndex(name)
	})
	if mapErr != nil {
		return mapErr
	}
	if err != nil {
		return err
	}
	m := newMonitor(c, &a)
	m.netnsName = func(nsid int32) (string, bool) { return monitorNetnsName(svc, nsid) }
	if a.hasFile {
		// rtnl_from_file runs before ll_init_map, so `ip` names each index by
		// a get as it prints; goip dumps the links first.
		if err := linkMap(); err != nil {
			return err
		}
		return m.fromFile(a.file)
	}
	ms, ok := c.src.(MonitorSource)
	if !ok {
		return fmt.Errorf("monitor: %w", ErrNotImplemented)
	}
	sub := monitorGroups(a.lmask, c.family, a.allNsid)
	sub.errOut = c.errOut
	conn, err := ms.Subscribe(sub)
	if err != nil {
		return err
	}
	err = func() error {
		if err := monitorLinkMap(c, service.New(conn, c.nextSeq)); err != nil {
			return err
		}
		m.netns = netnsMapInit(svc)
		return conn.Listen(m.accept)
	}()
	if cerr := conn.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

// monitorNetnsName is netns_get_name, a variable so a replay's test can name
// the nsids its capture's `ip` found under NETNS_RUN_DIR.
var monitorNetnsName = netnsGetName

// monitorLinkMap is ll_init_map: the AF_UNSPEC link dump `ip neigh show`
// sends too, every link into the cache.
func monitorLinkMap(c *runCtx, svc *service.Service) error {
	links, err := svc.NeighborLinks()
	if err != nil {
		return err
	}
	ls := make([]xtcpnl.LinkInfo, len(links))
	for i := range links {
		ls[i] = xtcpnl.LinkInfo(links[i])
	}
	c.lltab.Fill(ls)
	return nil
}

// monitor is do_ipmonitor's state across messages: print_headers' flags,
// the filters the *_reset_filter calls leave, the link cache and the netns
// names.
type monitor struct {
	c       *runCtx
	label   bool
	allNsid bool
	ifindex int32
	// netns is netns_map_init's cache, netnsName netns_get_name's walk.
	netns     netnsMap
	netnsName func(int32) (string, bool)
	now       func() time.Time
}

func newMonitor(c *runCtx, a *monitorArgs) *monitor {
	return &monitor{
		c: c, label: a.label, allNsid: a.allNsid, ifindex: a.ifindex,
		netns:     netnsMap{},
		netnsName: func(int32) (string, bool) { return "", false },
		now:       nowFuncFromEnv(),
	}
}

// monitorMsg is one message accept_msg is handed, with the netns id
// rtnl_listen read from NETLINK_LISTEN_ALL_NSID's control message, -1 for
// none, as for a message read from a file.
type monitorMsg struct {
	hdr  xtcpnl.NlMsgHdr
	body []byte
	nsid int32
}

// errMonitorStop is a message accept_msg fails on, which ends rtnl_listen:
// a route shorter than its rtmsg.
var errMonitorStop = errors.New("monitor stopped")

// accept is accept_msg: the message's print function by type, its lines
// written to stdout and its complaints to stderr.
func (m *monitor) accept(msg monitorMsg) error {
	out, errOut, err := m.render(&msg)
	if errOut != "" {
		if _, werr := fmt.Fprint(m.c.errOut, errOut); werr != nil {
			return werr
		}
	}
	if out != "" {
		if _, werr := fmt.Fprint(m.c.out, out); werr != nil {
			return werr
		}
	}
	return err
}

// render is accept_msg's switch.
func (m *monitor) render(msg *monitorMsg) (out, errOut string, err error) {
	h, body := &msg.hdr, msg.body
	switch h.Type {
	case unix.RTM_NEWROUTE, unix.RTM_DELROUTE:
		return m.route(msg)
	case unix.RTM_NEWNEXTHOP, unix.RTM_DELNEXTHOP:
		return m.nexthop(msg)
	case unix.RTM_NEWNEXTHOPBUCKET, unix.RTM_DELNEXTHOPBUCKET:
		nb, perr := xtcpnl.ParseNexthopBucket(body)
		if perr != nil {
			return "", refusal(h, perr), nil
		}
		v := render.NexthopBucketViewOf(nb)
		return m.headers("[NEXTHOPBUCKET]", msg.nsid) + v.Text(), "", nil
	case unix.RTM_NEWLINK, unix.RTM_DELLINK:
		return m.link(msg)
	case unix.RTM_NEWADDR, unix.RTM_DELADDR,
		unix.RTM_NEWMULTICAST, unix.RTM_DELMULTICAST,
		unix.RTM_NEWANYCAST, unix.RTM_DELANYCAST:
		return m.addr(msg)
	case unix.RTM_NEWADDRLABEL, unix.RTM_DELADDRLABEL:
		return m.addrlabel(msg)
	case unix.RTM_NEWNEIGH, unix.RTM_DELNEIGH, unix.RTM_GETNEIGH:
		return m.neigh(msg)
	case unix.RTM_NEWPREFIX:
		return m.prefix(msg)
	case unix.RTM_NEWRULE, unix.RTM_DELRULE:
		return m.rule(msg)
	case nlmsgTstampCst:
		return tstampText(body), "", nil
	case unix.RTM_NEWNETCONF, unix.RTM_DELNETCONF:
		return m.netconf(msg)
	case unix.RTM_NEWNSID, unix.RTM_DELNSID:
		return m.nsid(msg)
	case unix.RTM_NEWSTATS:
		info, perr := xtcpnl.ParseNewStats(body)
		if perr != nil {
			return "", refusal(h, perr), nil
		}
		o, e := render.IfStatsHwSInfoText(&info, m.c.lltab)
		return m.headers("[STATS]", msg.nsid) + o, e, nil
	case unix.NLMSG_ERROR, unix.NLMSG_NOOP, unix.NLMSG_DONE:
		return "", "", nil
	}
	return "", fmt.Sprintf("Unknown message: type=0x%08x(%d) flags=0x%08x(%d) len=0x%08x(%d)\n",
		h.Type, h.Type, h.Flags, h.Flags, h.Len, h.Len), nil
}

// refusal is the stderr line for a message goip cannot decode or render,
// which `ip` would print; the monitor goes on.
func refusal(h *xtcpnl.NlMsgHdr, err error) string {
	return fmt.Sprintf("goip: monitor: message type %d: %v\n", h.Type, err)
}

// deleted is print_bool's "Deleted " for a DEL type.
func deleted(on bool) string {
	if on {
		return "Deleted "
	}
	return ""
}

// headers is print_headers: the timestamp under -t, the sender's netns
// under all-nsid, and the object's label under `label` or `all`.
func (m *monitor) headers(label string, nsid int32) string {
	var b strings.Builder
	if m.c.timestamp > 0 {
		b.WriteString(timestampText(m.now(), m.c.timestampShort > 0))
	}
	if m.allNsid {
		if nsid < 0 {
			b.WriteString("[nsid current]")
		} else {
			fmt.Fprintf(&b, "[nsid %d]", nsid)
		}
	}
	if m.label {
		b.WriteString(label)
	}
	return b.String()
}

// asctimeLayoutCst is asctime's "%.3s %.3s%3d %.2d:%.2d:%.2d %d".
const asctimeLayoutCst = "Mon Jan _2 15:04:05 2006"

// timestampText is print_timestamp: asctime of the local time and the
// microseconds on a line of their own, or under -ts the ISO form in
// brackets ahead of the label.
func timestampText(t time.Time, short bool) string {
	t = t.Local()
	usec := t.Nanosecond() / 1000
	if short {
		return fmt.Sprintf("[%s.%06d] ", t.Format("2006-01-02T15:04:05"), usec)
	}
	return fmt.Sprintf("Timestamp: %s %d usec\n", t.Format(asctimeLayoutCst), usec)
}

// tstampText is print_nlmsg_timestamp, an NLMSG_TSTAMP record's seconds and
// microseconds, two u32s. `ip` reads them whatever the length; a short body
// reads as zeros here.
func tstampText(body []byte) string {
	var w [8]byte
	copy(w[:], body)
	secs := int64(xtcpnl.NativeEndian().Uint32(w[0:4]))
	usecs := xtcpnl.NativeEndian().Uint32(w[4:8])
	return fmt.Sprintf("Timestamp: %s %d us\n", time.Unix(secs, 0).Local().Format(asctimeLayoutCst), usecs)
}

// route is accept_msg's route arm: a cache entry is skipped, an mroute
// family goes to print_mroute, and the rest to print_route under
// iproute_reset_filter's filter: every table, the family -4 or -6 picked,
// and dev's output interface.
func (m *monitor) route(msg *monitorMsg) (out, errOut string, err error) {
	h, body := &msg.hdr, msg.body
	if len(body) < xtcpnl.RtMsgSizeCst {
		return "", fmt.Sprintf("BUG: wrong nlmsg len %d\n", len(body)-xtcpnl.RtMsgSizeCst), errMonitorStop
	}
	if xtcpnl.NativeEndian().Uint32(body[8:12])&unix.RTM_F_CLONED != 0 {
		return "", "", nil
	}
	ri, perr := xtcpnl.ParseNewRoute(body)
	if perr != nil {
		return "", refusal(h, perr), nil
	}
	del := h.Type == unix.RTM_DELROUTE
	if ri.Family == rtnlFamilyIPMRCst || ri.Family == rtnlFamilyIP6MRCst {
		return m.mroute(&ri, del, msg.nsid)
	}
	if m.c.family != unix.AF_UNSPEC && ri.Family != m.c.family {
		return "", "", nil
	}
	if m.ifindex > 0 {
		if !routeMatchesOif((*model.Route)(&ri), uint32(m.ifindex)) {
			return "", "", nil
		}
	}
	f := render.RouteShowFilter{OifMask: m.ifindex > 0, Details: m.c.detailed(), Stats: m.c.stats()}
	v := render.RouteViewOf(ri, m.c.lltab, f)
	return m.headers("[ROUTE]", msg.nsid) + deleted(del) + v.Text(), "", nil
}

// mroute is print_mroute under ipmroute_reset_filter's: every table, and
// dev's input interface. A route that is not RTN_MULTICAST is refused.
func (m *monitor) mroute(ri *xtcpnl.RouteInfo, del bool, nsid int32) (out, errOut string, err error) {
	if ri.Type != unix.RTN_MULTICAST {
		return "", mrouteNonMcastCst + "\n", nil
	}
	if m.ifindex != 0 && ri.Iif != uint32(m.ifindex) {
		return "", "", nil
	}
	o := render.MrouteOptions{Stats: m.c.stats(), Details: m.c.detailed()}
	v := render.MrouteViewOf(ri, del, m.c.lltab, o)
	return m.headers("[MROUTE]", nsid) + v.Text(), "", nil
}

// nexthop is print_cache_nexthop: no filter applies, "Deleted " leads the
// entry, and the line ends in a newline.
func (m *monitor) nexthop(msg *monitorMsg) (out, errOut string, err error) {
	nh, perr := xtcpnl.ParseNewNexthop(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	line, rerr := render.NexthopText(nh, m.c.detailed(), m.c.lltab)
	if rerr != nil {
		return "", refusal(&msg.hdr, rerr), nil
	}
	return m.headers("[NEXTHOP]", msg.nsid) + deleted(msg.hdr.Type == unix.RTM_DELNEXTHOP) + line + "\n", "", nil
}

// link is ll_remember_index, then print_linkinfo under dev's index.
func (m *monitor) link(msg *monitorMsg) (out, errOut string, err error) {
	li, perr := xtcpnl.ParseNewLink(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	del := msg.hdr.Type == unix.RTM_DELLINK
	m.c.lltab.Forget(li.Index)
	if !del {
		m.c.lltab.Fill([]xtcpnl.LinkInfo{li})
	}
	if m.ifindex != 0 && li.Index != m.ifindex {
		return "", "", nil
	}
	// do_link is 0 in the monitor, as under `ip addr`, so no mode, no
	// statistics under -s and no addrgenmode under -d; filter.showqueue is
	// clear, so no qlen.
	v := render.LinkViewForAddr(li, m.c.lltab, unix.AF_UNSPEC, m.c.detailed()).WithoutQlen()
	if m.c.detailed() {
		v = v.WithDetail(li, false, m.c.lltab)
	}
	return m.headers("[LINK]", msg.nsid) + deleted(del) + v.Text(), "", nil
}

// addr is print_addrinfo under ipaddr_reset_filter(1, dev): the index and
// name lead the line, as for `ip -o addr`, but its lines are not joined.
func (m *monitor) addr(msg *monitorMsg) (out, errOut string, err error) {
	ai, perr := xtcpnl.ParseNewAddr(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if m.ifindex != 0 && int32(ai.Index) != m.ifindex {
		return "", "", nil
	}
	t := msg.hdr.Type
	del := t == unix.RTM_DELADDR || t == unix.RTM_DELMULTICAST || t == unix.RTM_DELANYCAST
	v := render.AddrViewOf(ai)
	lead := strconv.FormatUint(uint64(ai.Index), 10) + ": " + m.c.lltab.IndexToName(int32(ai.Index))
	return m.headers("[ADDR]", msg.nsid) + deleted(del) + lead + v.Text(), "", nil
}

// addrlabel is print_addrlabel, which no filter reaches.
func (m *monitor) addrlabel(msg *monitorMsg) (out, errOut string, err error) {
	al, perr := xtcpnl.ParseNewAddrLabel(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	v, verr := render.AddrLabelViewOf(al)
	if verr != nil {
		return "", refusal(&msg.hdr, verr), nil
	}
	return m.headers("[ADDRLABEL]", msg.nsid) + deleted(msg.hdr.Type == unix.RTM_DELADDRLABEL) + v.Text(), "", nil
}

// neigh is accept_msg's family test, then print_neigh under
// ipneigh_reset_filter's: every state, and dev's index. An RTM_GETNEIGH, the
// kernel asking an application to resolve, is a "miss".
func (m *monitor) neigh(msg *monitorMsg) (out, errOut string, err error) {
	n, perr := xtcpnl.ParseNeigh(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if m.c.family != unix.AF_UNSPEC && n.Family != m.c.family {
		return "", "", nil
	}
	if m.ifindex != 0 && n.Ifindex != m.ifindex {
		return "", "", nil
	}
	lead := deleted(msg.hdr.Type == unix.RTM_DELNEIGH)
	if msg.hdr.Type == unix.RTM_GETNEIGH {
		lead = "miss "
	}
	v := render.NeighViewOf(n, m.c.lltab, render.NeighShowFilter{IndexSet: m.ifindex != 0, Stats: m.c.stats()})
	return m.headers("[NEIGH]", msg.nsid) + lead + v.Text(), "", nil
}

// prefix is print_prefix: IPv6 only, under -4 nothing, and a prefix of
// another family or ND type is refused on stderr.
func (m *monitor) prefix(msg *monitorMsg) (out, errOut string, err error) {
	pi, perr := xtcpnl.ParsePrefix(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if m.c.family != unix.AF_UNSPEC && m.c.family != unix.AF_INET6 {
		return "", "", nil
	}
	if pi.Family != unix.AF_INET6 {
		return "", fmt.Sprintf("incorrect protocol family: %d\n", pi.Family), nil
	}
	if pi.Type != ndOptPrefixInformationCst {
		return "", fmt.Sprintf("wrong ND type %d\n", pi.Type), nil
	}
	return m.headers("[PREFIX]", msg.nsid) + render.PrefixText(&pi, m.c.lltab), "", nil
}

// rule is print_rule, whose filter is the family -4 or -6 picked.
func (m *monitor) rule(msg *monitorMsg) (out, errOut string, err error) {
	ri, perr := xtcpnl.ParseRule(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if m.c.family != unix.AF_UNSPEC && ri.Family != m.c.family {
		return "", "", nil
	}
	if rerr := checkRuleRenderable([]xtcpnl.RuleInfo{ri}); rerr != nil {
		return "", refusal(&msg.hdr, rerr), nil
	}
	if !m.c.detailed() && (ri.Protocol == 0 || ri.Protocol == unix.RTPROT_KERNEL) {
		ri.HasProtocol = false
	}
	v := render.RuleViewOf(ri)
	return m.headers("[RULE]", msg.nsid) + deleted(msg.hdr.Type == unix.RTM_DELRULE) + v.Text(), "", nil
}

// netconf is print_netconf under ipnetconf_reset_filter's dev index.
func (m *monitor) netconf(msg *monitorMsg) (out, errOut string, err error) {
	ni, perr := xtcpnl.ParseNewNetconf(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if m.ifindex != 0 && ni.Ifindex != m.ifindex {
		return "", "", nil
	}
	v := render.NetconfViewOf(ni, m.c.lltab)
	return m.headers("[NETCONF]", msg.nsid) + deleted(msg.hdr.Type == unix.RTM_DELNETCONF) + v.Text(), "", nil
}

// nsid is print_nsid as the monitor runs it: the name is the cache's for
// the current nsid, if the message has one, else the nsid, and is taken out
// of the cache once printed; failing that, for an RTM_NEWNSID alone, the
// directory is walked for the nsid and a name found is cached.
func (m *monitor) nsid(msg *monitorMsg) (out, errOut string, err error) {
	ni, perr := xtcpnl.ParseNsid(msg.body)
	if perr != nil {
		return "", refusal(&msg.hdr, perr), nil
	}
	if !ni.HasNSID {
		return "", "BUG: NETNSA_NSID is missing print_nsid\n", nil
	}
	key := ni.NSID
	if ni.HasCurrentNSID {
		key = ni.CurrentNSID
	}
	del := msg.hdr.Type == unix.RTM_DELNSID
	name, ok := m.netns[key]
	if ok {
		delete(m.netns, key)
	} else if !del {
		if name, ok = m.netnsName(ni.NSID); ok {
			m.netns[ni.NSID] = name
		}
	}
	v := render.NsidViewOf(&ni, del, name)
	return m.headers("[NSID]", msg.nsid) + v.Text(), "", nil
}

// fromFile is rtnl_from_file: each header, then its body to the 4-byte
// boundary, until the file ends between messages. A header cut short, a
// length under the header's or over the buffer, a body cut short, or a
// message accept_msg fails ends it with -1, so `ip` exits 1; a file it
// cannot open is exit(-1), 255.
func (m *monitor) fromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		var errno unix.Errno
		if errors.As(err, &errno) {
			return fmt.Errorf("%w: Cannot fopen: %s", ErrMonitorAbort, errno.Error())
		}
		return fmt.Errorf("%w: Cannot fopen: %w", ErrMonitorAbort, err)
	}
	off := 0
	for off < len(data) {
		if len(data)-off < xtcpnl.NlMsgHdrSizeCst {
			return fmt.Errorf("%w: rtnl-from_file: truncated message", ErrMonitorFile)
		}
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(data[off:], &h); err != nil {
			return fmt.Errorf("%w: %w", ErrMonitorFile, err)
		}
		if h.Len < xtcpnl.NlMsgHdrSizeCst || h.Len > monitorBufCst {
			return fmt.Errorf("%w: !!!malformed message: len=%d @%d", ErrMonitorFile,
				int32(h.Len), off+xtcpnl.NlMsgHdrSizeCst)
		}
		l := int(h.Len) - xtcpnl.NlMsgHdrSizeCst
		end := off + xtcpnl.NlMsgHdrSizeCst + nlmsgAlign(l)
		if end > len(data) {
			return fmt.Errorf("%w: rtnl-from_file: truncated message", ErrMonitorFile)
		}
		body := data[off+xtcpnl.NlMsgHdrSizeCst : off+xtcpnl.NlMsgHdrSizeCst+l]
		if err := m.accept(monitorMsg{hdr: h, body: body, nsid: -1}); err != nil {
			return fmt.Errorf("%w: %w", ErrMonitorFile, err)
		}
		off = end
	}
	return nil
}
