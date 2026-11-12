package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// `ip stats show` is ipstats_show (ip/ipstats.c, iproute2 7.3.0): a tree of
// stat descriptors, groups over leaves, of which the command enables a set.
// The enabled leaves are packed into one RTM_GETSTATS filter, a dump or a
// get of one device, and each reply prints every enabled leaf in turn, each
// under its own `IFINDEX: NAME: group G [subgroup S [suite T]]` header.

var (
	// ErrStatsInval is a refusal ipstats.c returns -EINVAL for, exit 22:
	// an unknown word, a level out of order and a name no level holds.
	ErrStatsInval = errors.New("stats")
	// ErrStatsUsage is one it ends with exit(-1), 255: NEXT_ARG's missing
	// argument, duparg2, invarg and `stats set`'s missing dev or type.
	ErrStatsUsage = errors.New("stats usage")
	// ErrStatsShow is a body that could not be printed, which ends the
	// command with ipstats_process_ifsm's error: 2, ENOENT or the dump's -2.
	ErrStatsShow = errors.New("stats show")
)

// ipstatsLevels is ipstats_levels.
var ipstatsLevels = [...]string{"group", "subgroup", "suite"}

// ipstatsSel is struct ipstats_sel: the name chosen at each level.
type ipstatsSel [len(ipstatsLevels)]string

// ipstatsDesc is struct ipstats_stat_desc: a group of subs, or a leaf with
// its pack and show. rank is the leaf's place in the order ip prints the
// enabled leaves in, which is ipstats_comp_enabled's sort by descriptor
// address; the captures pin it (pkg/xtcpnl wsStatsOrder), and ip 7.3.0 as
// built here prints the same.
type ipstatsDesc struct {
	name string
	subs []*ipstatsDesc
	rank int
	pack func(f *ipstatsFilters)
	show func(a *ipstatsAttrs, p *render.Printer, extended bool) error
}

func (d *ipstatsDesc) leaf() bool { return d.subs == nil }

// ipstatsFilters is struct ipstats_stat_dump_filters: mask[0] is the
// top-level filter_mask and mask[g] group g's sub-mask.
type ipstatsFilters [iflaStatsMaxCst + 1]uint32

// enable is ipstats_stat_desc_enable_bit; IFLA_STATS_FILTER_BIT(a) is
// 1 << (a - 1).
func (f *ipstatsFilters) enable(group, subgroup uint16) {
	f[0] |= 1 << (group - 1)
	if subgroup != 0 {
		f[group] |= 1 << (subgroup - 1)
	}
}

// The attribute counts ipstats_stat_ifla_max gives each table.
const (
	iflaStatsMaxCst        = unix.IFLA_STATS_AF_SPEC // IFLA_STATS_MAX
	linkXstatsTypeMaxCst   = 2                       // LINK_XSTATS_TYPE_MAX
	offloadXstatsMaxCst    = 3                       // IFLA_OFFLOAD_XSTATS_MAX
	afMaxCst               = unix.AF_MAX - 1
	hwSInfoMaxCst          = 2 // IFLA_OFFLOAD_XSTATS_HW_S_INFO_MAX
	mplsStatsMaxCst        = 1 // MPLS_STATS_MAX
	linkXstatsTypeBridge   = 1
	linkXstatsTypeBond     = 2
	bridgeXstatsVlanCst    = 1
	bridgeXstatsMcastCst   = 2
	bridgeXstatsStpCst     = 4
	offloadXstatsCPUHitCst = 1
	offloadXstatsHwSInfo   = 2
	offloadXstatsL3Stats   = 3
	hwSInfoRequestCst      = 1
	hwSInfoUsedCst         = 2
	mplsStatsLinkCst       = 1
)

// ipstatsIflaMax is ipstats_stat_ifla_max.
var ipstatsIflaMax = ipstatsFilters{
	0:                                   iflaStatsMaxCst,
	unix.IFLA_STATS_LINK_XSTATS:         linkXstatsTypeMaxCst,
	unix.IFLA_STATS_LINK_XSTATS_SLAVE:   linkXstatsTypeMaxCst,
	unix.IFLA_STATS_LINK_OFFLOAD_XSTATS: offloadXstatsMaxCst,
	unix.IFLA_STATS_AF_SPEC:             afMaxCst,
}

// shouldFilter is ipstats_req_should_filter_at.
func (f *ipstatsFilters) shouldFilter(at int) bool {
	return f[at] != 0 && f[at] != 1<<ipstatsIflaMax[at]-1
}

// ipstatsOffloadCst is the offload group's name.
const ipstatsOffloadCst = "offload"

// The tree, as ipstats.c declares it. The subs are in declaration order,
// which `help` lists and the selection walks; rank is print order.
var (
	ipstatsLink = &ipstatsDesc{name: objLinkCst, rank: 1,
		pack: func(f *ipstatsFilters) { f.enable(unix.IFLA_STATS_LINK_64, 0) },
		show: func(a *ipstatsAttrs, p *render.Printer, ext bool) error {
			p.NL()
			return ipstatsShow64(a, p, unix.IFLA_STATS_LINK_64, 0, ext)
		}}
	ipstatsOffload = &ipstatsDesc{name: ipstatsOffloadCst, subs: []*ipstatsDesc{
		{name: "cpu_hit", rank: 4,
			pack: func(f *ipstatsFilters) {
				f.enable(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsCPUHitCst)
			},
			show: func(a *ipstatsAttrs, p *render.Printer, ext bool) error {
				p.NL()
				return ipstatsShow64(a, p, unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsCPUHitCst, ext)
			}},
		{name: "hw_stats_info", rank: 3,
			pack: func(f *ipstatsFilters) {
				f.enable(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsHwSInfo)
			},
			show: ipstatsShowHwSInfo},
		{name: "l3_stats", rank: 2,
			pack: func(f *ipstatsFilters) {
				f.enable(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsL3Stats)
				f.enable(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsHwSInfo)
			},
			show: ipstatsShowL3Stats},
	}}
	ipstatsAfstats = &ipstatsDesc{name: "afstats", subs: []*ipstatsDesc{
		{name: "mpls", rank: 0,
			pack: func(f *ipstatsFilters) { f.enable(unix.IFLA_STATS_AF_SPEC, 0) },
			show: ipstatsShowAfMpls},
	}}
	ipstatsToplev = &ipstatsDesc{name: "top-level", subs: []*ipstatsDesc{
		ipstatsLink,
		ipstatsXstatsGroup("xstats", unix.IFLA_STATS_LINK_XSTATS, 12, 6),
		ipstatsXstatsGroup("xstats_slave", unix.IFLA_STATS_LINK_XSTATS_SLAVE, 9, 5),
		ipstatsOffload,
		ipstatsAfstats,
	}}
)

// ipstatsXstatsGroup is ipstats_stat_desc_xstats_group or its slave
// sibling: the bridge suites stp, mcast and vlan, and bond's 802.3ad, each
// an ipstats_stat_desc_xstats leaf. stpRank is the bridge stp leaf's rank,
// mcast and vlan the two below it.
func ipstatsXstatsGroup(name string, at uint16, stpRank, bondRank int) *ipstatsDesc {
	leaf := func(suite string, rank int, linkType, inner uint16, cb func(*render.Printer, []byte)) *ipstatsDesc {
		return &ipstatsDesc{name: suite, rank: rank,
			pack: func(f *ipstatsFilters) { f.enable(at, 0) },
			show: func(a *ipstatsAttrs, p *render.Printer, _ bool) error {
				ipstatsShowXstats(a, p, at, linkType, inner, cb, suite == "vlan")
				return nil
			}}
	}
	return &ipstatsDesc{name: name, subs: []*ipstatsDesc{
		{name: kindBridgeCst, subs: []*ipstatsDesc{
			leaf("stp", stpRank, linkXstatsTypeBridge, bridgeXstatsStpCst, ipstatsBridgeStp),
			leaf("mcast", stpRank-1, linkXstatsTypeBridge, bridgeXstatsMcastCst, ipstatsBridgeMcast),
			leaf("vlan", stpRank-2, linkXstatsTypeBridge, bridgeXstatsVlanCst, ipstatsBridgeVlan),
		}},
		{name: "bond", subs: []*ipstatsDesc{
			leaf("802.3ad", bondRank, linkXstatsTypeBond, xtcpnl.BondXstats3ad, ipstatsBond3ad),
		}},
	}}
}

// ipstatsEnabled is one struct ipstats_stat_enabled_one.
type ipstatsEnabled struct {
	desc *ipstatsDesc
	sel  ipstatsSel
}

// ipstatsShowArgs is ipstats_show's argument loop and its final push: the
// enabled leaves, sorted and unique, the dev, and whether `help` was asked
// for, which prints the usage and ends the command with 0.
func ipstatsShowArgs(args []string) (enabled []ipstatsEnabled, dev string, help bool, err error) {
	var sel ipstatsSel
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case devKeywordCst:
			if i++; i >= len(args) {
				return nil, "", false, statsUsage(incompleteErr())
			}
			if dev != "" {
				return nil, "", false, statsUsage(duparg2Err(devKeywordCst, args[i]))
			}
			if !validIfname(args[i]) {
				return nil, "", false, statsUsage(invargErr(`"dev" not a valid ifname`, args[i]))
			}
			dev = args[i]
			continue
		case helpVerbCst:
			return nil, "", true, nil
		}
		// Each level is compared in turn against the word under the
		// cursor, which NEXT_ARG moves past a match: `group subgroup x`
		// selects group "subgroup", then reads x as a subgroup.
		found := false
		for level, name := range ipstatsLevels {
			if args[i] != name {
				continue
			}
			if i++; i >= len(args) {
				return nil, "", false, statsUsage(incompleteErr())
			}
			if enabled, err = ipstatsSelect(&sel, args[i], level, enabled); err != nil {
				return nil, "", false, err
			}
			found = true
		}
		if !found {
			return nil, "", false, fmt.Errorf("%w: What is %q?", ErrStatsInval, args[i])
		}
	}
	enabled, err = ipstatsEnable(sel, enabled)
	return enabled, dev, false, err
}

// statsUsage marks a refusal ip ends with exit(-1).
func statsUsage(err error) error { return fmt.Errorf("%w: %w", ErrStatsUsage, err) }

// validIfname is check_ifname: 1 to 15 bytes, none of them '/', ':' or
// white space.
func validIfname(name string) bool {
	if name == "" || len(name) >= unix.IFNAMSIZ {
		return false
	}
	return !strings.ContainsAny(name, "/: \t\n\v\f\r")
}

// ipstatsSelect is ipstats_select: every level above this one must be
// chosen, a selection already made at this level or below is pushed, and
// the levels below are cleared.
func ipstatsSelect(sel *ipstatsSel, name string, level int, enabled []ipstatsEnabled) ([]ipstatsEnabled, error) {
	for i := range level {
		if sel[i] == "" {
			return nil, fmt.Errorf("%w: Error: %s %s requested without selecting a %s first",
				ErrStatsInval, ipstatsLevels[level], name, ipstatsLevels[i])
		}
	}
	for i := level; i < len(sel); i++ {
		if sel[i] != "" {
			var err error
			if enabled, err = ipstatsEnable(*sel, enabled); err != nil {
				return nil, err
			}
			break
		}
	}
	sel[level] = name
	for i := level + 1; i < len(sel); i++ {
		sel[i] = ""
	}
	return enabled, nil
}

// ipstatsEnable is ipstats_enable: the descriptor sel names, every leaf
// under it added, then the set sorted into print order with duplicates
// dropped, the first of each kept.
func ipstatsEnable(sel ipstatsSel, enabled []ipstatsEnabled) ([]ipstatsEnabled, error) {
	desc, err := ipstatsFind(sel)
	if err != nil {
		return nil, err
	}
	enabled = ipstatsEnableRecursively(desc, sel, enabled)
	slices.SortStableFunc(enabled, func(a, b ipstatsEnabled) int { return a.desc.rank - b.desc.rank })
	return slices.CompactFunc(enabled, func(a, b ipstatsEnabled) bool { return a.desc == b.desc }), nil
}

// ipstatsEnableRecursively adds desc's leaves, each with its selection
// completed down to it. No group in the tree is empty, so the -ENOENT of
// one that is cannot arise.
func ipstatsEnableRecursively(desc *ipstatsDesc, sel ipstatsSel, enabled []ipstatsEnabled) []ipstatsEnabled {
	if desc.leaf() {
		return append(enabled, ipstatsEnabled{desc: desc, sel: sel})
	}
	for _, sub := range desc.subs {
		subsel := sel
		for i := range subsel {
			if subsel[i] == "" {
				subsel[i] = sub.name
				break
			}
		}
		enabled = ipstatsEnableRecursively(sub, subsel, enabled)
	}
	return enabled
}

// ipstatsFind is ipstats_enable_find_stat_desc: sel walked down the tree.
func ipstatsFind(sel ipstatsSel) (*ipstatsDesc, error) {
	desc := ipstatsToplev
	for i, name := range sel {
		if name == "" {
			break
		}
		if desc.leaf() {
			return nil, fmt.Errorf("%w: Error: %s %s requested inside leaf %s %s",
				ErrStatsInval, ipstatsLevels[i], name, ipstatsLevels[i-1], desc.name)
		}
		j := slices.IndexFunc(desc.subs, func(d *ipstatsDesc) bool { return d.name == name })
		if j < 0 {
			return nil, fmt.Errorf("%w: Error: no %s named %s found inside %s",
				ErrStatsInval, ipstatsLevels[i], name, desc.name)
		}
		desc = desc.subs[j]
	}
	return desc, nil
}

// ipstatsHelp is do_help's text, which ip prints on stderr.
func ipstatsHelp() string {
	var b strings.Builder
	b.WriteString("Usage: ip stats help\n" +
		"       ip stats show [ dev DEV ] [ group GROUP [ subgroup SUBGROUP [ suite SUITE ] ... ] ... ] ...\n" +
		"       ip stats set dev DEV l3_stats { on | off }\n")
	for i, d := range ipstatsToplev.subs {
		if i == 0 {
			b.WriteString("GROUP := { " + d.name)
		} else {
			b.WriteString(" | " + d.name)
		}
	}
	b.WriteString(" }\n")
	for _, d := range ipstatsToplev.subs {
		if d.leaf() {
			continue
		}
		for j, s := range d.subs {
			if j == 0 {
				b.WriteString(d.name + " SUBGROUP := {")
			} else {
				b.WriteString(" |")
			}
			b.WriteString(" " + s.name)
			for _, t := range s.subs {
				b.WriteString(" [ suite " + t.name + " ]")
			}
		}
		b.WriteString(" }\n")
	}
	return b.String()
}

// ipstatsFilter is ipstats_req_add_filters: each leaf packed, then the
// sub-masks that narrow their group, a full one being no filter at all.
// The AF_SPEC group has no sub-mask any leaf sets.
func ipstatsFilter(enabled []ipstatsEnabled) xtcpnl.StatsFilter {
	var f ipstatsFilters
	for _, e := range enabled {
		e.desc.pack(&f)
	}
	sub := func(at int) uint32 {
		if f.shouldFilter(at) {
			return f[at]
		}
		return 0
	}
	return xtcpnl.StatsFilter{
		Mask:        f[0],
		Xstats:      sub(unix.IFLA_STATS_LINK_XSTATS),
		XstatsSlave: sub(unix.IFLA_STATS_LINK_XSTATS_SLAVE),
		Offload:     sub(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS),
	}
}

// runStatsShow is ipstats_show and ipstats_show_do. level is show_stats
// as the command left it: the `show` verb adds one and -d one more.
func runStatsShow(c *runCtx, args []string, level int) error {
	enabled, dev, help, err := ipstatsShowArgs(args)
	if err != nil {
		return err
	}
	if help {
		_, err := fmt.Fprint(c.errOut, ipstatsHelp())
		return err
	}
	var ifindex int32
	if dev != "" {
		// The kernel's ENODEV is ll_name_to_index's 0 and nodev's -1; an
		// error is a get no answer came back for.
		if ifindex, err = genlNameToIndex(c, dev); err != nil {
			return fmt.Errorf("stats show: cannot find device %q: %w", dev, err)
		}
		if ifindex == 0 {
			return noSuchDevice(nodevErr(dev))
		}
	}
	r, err := xtcpnl.BuildStatsRequest(cint.Bits(ifindex), ipstatsFilter(enabled), c.nextSeq())
	if err != nil {
		return fmt.Errorf("goip: build stats request: %w", err)
	}
	bodies, err := ipstatsFetch(c, r, cint.Bits(ifindex))
	if err != nil {
		return err
	}
	p := render.NewPrinter(c.json)
	p.NewObj()
	for _, body := range bodies {
		if err = ipstatsProcess(c, p, body, enabled, level > 1); err != nil {
			break
		}
		if ifindex == 0 {
			// ipstats_dump_one's newline after each device.
			p.NL()
		}
	}
	if err == nil {
		p.DeleteObj()
	}
	if _, werr := c.out.Write(p.Bytes()); werr != nil {
		return werr
	}
	return err
}

// ipstatsFetch is the dump, or ipstats_show_one's get: rtnl_talk on a live
// source, and the one reply for ifindex in a replayed capture.
func ipstatsFetch(c *runCtx, r []byte, ifindex uint32) ([][]byte, error) {
	if ifindex != 0 {
		if talk, ok := c.src.(TalkSource); ok {
			body, err := talk.Talk(r, uint16(unix.RTM_NEWSTATS))
			if err != nil {
				return nil, err
			}
			return [][]byte{body}, nil
		}
	}
	bodies, err := c.src.Dump(r, uint16(unix.RTM_NEWSTATS))
	if err != nil || ifindex == 0 {
		return bodies, err
	}
	for _, b := range bodies {
		if len(b) >= xtcpnl.IfStatsMsgSizeCst && binary.NativeEndian.Uint32(b[4:8]) == ifindex {
			return [][]byte{b}, nil
		}
	}
	return nil, fmt.Errorf("%w: no RTM_NEWSTATS for ifindex %d", ErrStatsShow, ifindex)
}

// ipstatsAttrs is struct ipstats_stat_show_attrs: the reply's top-level
// table and, built when first asked for, each group's.
type ipstatsAttrs struct {
	tbs [iflaStatsMaxCst + 1][][]byte
	// errOut is stderr, for ipstats_dissect_01's complaint.
	errOut io.Writer
}

// get is ipstats_stat_show_get_attr: a top-level attribute, or one inside
// group's nest.
func (a *ipstatsAttrs) get(group, subgroup uint16) []byte {
	if subgroup == 0 {
		return a.tbs[0][group]
	}
	if a.tbs[0][group] == nil {
		return nil
	}
	if a.tbs[group] == nil {
		a.tbs[group] = rtaTable(a.tbs[0][group], int(ipstatsIflaMax[group]), true)
	}
	return a.tbs[group][subgroup]
}

// rtaTable is parse_rtattr, or parse_rtattr_nested when nested, which
// masks NLA_F_NESTED off: types 0 to max, the first of each kept, the walk
// ending at the first attribute that does not fit. A present attribute
// with no payload is an empty, non-nil slice.
func rtaTable(data []byte, maxType int, nested bool) [][]byte {
	tb := make([][]byte, maxType+1)
	for t, v := range rtaAttrs(data) {
		if nested {
			t &^= unix.NLA_F_NESTED
		}
		if int(t) <= maxType && tb[t] == nil {
			tb[t] = v
		}
	}
	return tb
}

// rtaAttrs is the RTA_OK/RTA_NEXT walk: each attribute's raw type and its
// payload, capped at its length, until one does not fit.
func rtaAttrs(data []byte) iter.Seq2[uint16, []byte] {
	return func(yield func(uint16, []byte) bool) {
		for len(data) >= unix.SizeofRtAttr {
			l := int(binary.NativeEndian.Uint16(data[0:2]))
			if l < unix.SizeofRtAttr || l > len(data) {
				return
			}
			if !yield(binary.NativeEndian.Uint16(data[2:4]), data[unix.SizeofRtAttr:l:l]) {
				return
			}
			adv := (l + unix.RTA_ALIGNTO - 1) &^ (unix.RTA_ALIGNTO - 1)
			if adv >= len(data) {
				return
			}
			data = data[adv:]
		}
	}
}

// payload is IPSTATS_RTA_PAYLOAD: v's bytes, cut or zero-padded to size.
func payload(v []byte, size int) []byte {
	out := make([]byte, size)
	copy(out, v)
	return out
}

// ipstatsProcess is ipstats_process_ifsm: the device named, then each
// enabled leaf under its header.
func ipstatsProcess(c *runCtx, p *render.Printer, body []byte, enabled []ipstatsEnabled, extended bool) error {
	if len(body) < xtcpnl.IfStatsMsgSizeCst {
		return fmt.Errorf("%w: BUG: wrong nlmsg len %d", ErrStatsShow, len(body)-xtcpnl.IfStatsMsgSizeCst)
	}
	ifindex := cint.Int(binary.NativeEndian.Uint32(body[4:8]))
	a := &ipstatsAttrs{errOut: c.errOut}
	a.tbs[0] = rtaTable(body[xtcpnl.IfStatsMsgSizeCst:], iflaStatsMaxCst, false)
	dev := genlIndexToName(c, ifindex)
	for _, e := range enabled {
		p.OpenObject("")
		p.Int(render.PrintAny, "ifindex", "%d:", int64(ifindex))
		p.String(render.PrintAny, "ifname", " %s:", dev)
		for i, name := range e.sel {
			if name == "" {
				break
			}
			p.String(render.PrintJSON, ipstatsLevels[i], "", name)
			p.FPf(" %s ", ipstatsLevels[i])
			p.FPf("%s", name)
		}
		if err := e.desc.show(a, p, extended); err != nil {
			return err
		}
		p.CloseObject()
		p.NL()
	}
	return nil
}

// ipstatsShow64 is ipstats_show_64: the stats64 object, or nothing when the
// attribute is absent.
func ipstatsShow64(a *ipstatsAttrs, p *render.Printer, group, subgroup uint16, extended bool) error {
	at := a.get(group, subgroup)
	if at == nil {
		return nil
	}
	var s xtcpnl.RtnlLinkStats64
	if _, err := xtcpnl.DeserializeRtnlLinkStats64(payload(at, xtcpnl.RtnlLinkStats64SizeCst), &s); err != nil {
		return err
	}
	p.OpenObject("stats64")
	render.IPStats64(p, s, extended)
	p.CloseObject()
	return nil
}

// ipstatsMoo is enum ipstats_maybe_on_off: absent, or on or off.
type ipstatsMoo struct{ set, on bool }

// ipstatsHwSInfo is struct ipstats_hw_s_info_one for l3_stats, and whether
// the L3_STATS member was there at all.
type ipstatsHwSInfo struct {
	present       bool
	request, used ipstatsMoo
}

// ipstatsDissect01 is ipstats_dissect_01: 0 off, 1 on, anything else a
// line on stderr and absent.
func ipstatsDissect01(errOut io.Writer, v []byte, what string) ipstatsMoo {
	b := payload(v, 1)[0]
	if b > 1 {
		fmt.Fprintf(errOut, "Invalid value for %s: expected 0 or 1, got %d.\n", what, b)
		return ipstatsMoo{}
	}
	return ipstatsMoo{set: true, on: b == 1}
}

// ipstatsDissectHwSInfo is ipstats_dissect_hw_s_info for its one member;
// a REQUEST byte that is neither 0 nor 1 fails it with -EINVAL.
func ipstatsDissectHwSInfo(errOut io.Writer, at []byte) (ipstatsHwSInfo, error) {
	tb := rtaTable(at, offloadXstatsMaxCst, true)
	if tb[offloadXstatsL3Stats] == nil {
		return ipstatsHwSInfo{}, nil
	}
	in := rtaTable(tb[offloadXstatsL3Stats], hwSInfoMaxCst, true)
	h := ipstatsHwSInfo{present: true}
	if in[hwSInfoRequestCst] != nil {
		if h.request = ipstatsDissect01(errOut, in[hwSInfoRequestCst], "request"); !h.request.set {
			return ipstatsHwSInfo{}, fmt.Errorf("%w: hw_s_info request", ErrStatsInval)
		}
	}
	if in[hwSInfoUsedCst] != nil {
		h.used = ipstatsDissect01(errOut, in[hwSInfoUsedCst], "used")
	}
	return h, nil
}

// printOne is __ipstats_show_hw_s_info_one.
func (h ipstatsHwSInfo) printOne(p *render.Printer) {
	if !h.present {
		return
	}
	if h.request.set {
		p.OnOff(render.PrintAny, "request", " %s", h.request.on)
	}
	if h.used.set {
		p.OnOff(render.PrintAny, "used", " used %s", h.used.on)
	}
}
