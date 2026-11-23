package xtcpnl

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// bridge's request model (gobridge 6P): which netlink requests a bridge(8)
// command sends, read from bridge/bridge.c main and do_cmd, the objects'
// do_* dispatch, and their *_show parsers. Every object first dumps the
// links (ll_init_map). A dev or master filter is resolved by
// ll_name_to_index: the name from that dump's cache, else an RTM_GETLINK
// by name (ll_link_get), else "ifN" as index N (ll_idx_a2n); with none,
// bridge prints nodev and stops.

// llInitMapMaskCst is ll_init_map's IFLA_EXT_MASK: RTEXT_FILTER_VF |
// SKIP_STATS | NAME_ONLY (lib/ll_map.c), also on ll_link_get's request.
const llInitMapMaskCst = RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS | RtextFilterNameOnly

// bridgeObj is the show a command reaches.
type bridgeObj int

const (
	brObjNone bridgeObj = iota
	brObjLink
	brObjVlan
	brObjVlanTunnel
	brObjVlanGlobal
	brObjVni
	brObjMst
)

// bridgeCmd is a parsed bridge command line.
type bridgeCmd struct {
	json, stats, details, compress bool
	// noReq: refused before any request (an unknown option, -help, an
	// unknown object, `help`, no object). refused: refused by bridge after
	// ll_init_map, so the link dump is its only request.
	noReq, refused bool
	obj            bridgeObj
	// dev and master are the filter names ll_name_to_index resolves, dev
	// first (master is link show's alone).
	dev, master string
}

// names are the filters to resolve, in bridge's order.
func (c bridgeCmd) names() []string {
	var out []string
	for _, n := range []string{c.dev, c.master} {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// bridgeMatches is iproute2's matches(prefix, s) == 0.
func bridgeMatches(prefix, s string) bool { return prefix != "" && strings.HasPrefix(s, prefix) }

// bridgeParseCmd models bridge's parsing for the corpus's command lines.
func bridgeParseCmd(cmd string) (bridgeCmd, error) {
	var c bridgeCmd
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "bridge" {
		return c, fmt.Errorf("not a bridge command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		opt := f[0]
		if opt == "--" {
			f = f[1:]
			break
		}
		if strings.HasPrefix(opt, "--") {
			opt = opt[1:]
		}
		color, _, _ := strings.Cut(opt, "=")
		switch {
		case bridgeMatches(opt, "-help"), bridgeMatches(opt, "-Version"):
			c.noReq = true
			return c, nil
		case bridgeMatches(opt, "-stats"), bridgeMatches(opt, "-statistics"):
			c.stats = true
		case bridgeMatches(opt, "-details"):
			c.details = true
		case bridgeMatches(opt, "-oneline"), bridgeMatches(opt, "-timestamp"):
		case bridgeMatches(color, "-color"):
		case bridgeMatches(opt, "-compressvlans"):
			c.compress = true
		case bridgeMatches(opt, "-force"):
		case bridgeMatches(opt, "-json"):
			c.json = true
		case bridgeMatches(opt, "-pretty"):
		default:
			c.noReq = true // Option "-z" is unknown
			return c, nil
		}
		f = f[1:]
	}
	if len(f) == 0 {
		c.noReq = true // usage after rtnl_open
		return c, nil
	}
	obj := ""
	for _, o := range []string{"link", "fdb", "mdb", "mst", "vlan", "vni", "monitor", "help"} {
		if bridgeMatches(f[0], o) {
			obj = o
			break
		}
	}
	args := f[1:]
	show := func(w string, strict bool) bool {
		for _, s := range []string{"show", "lst", "list"} {
			if (strict && w == s) || (!strict && bridgeMatches(w, s)) {
				return true
			}
		}
		return false
	}
	switch obj {
	case "link":
		if len(args) > 0 && !show(args[0], false) {
			c.refused = true
			return c, nil
		}
		c.obj = brObjLink
		return c, c.parseShowArgs(tail(args), true, false)
	case "vlan":
		switch {
		case len(args) == 0 || show(args[0], false):
			c.obj = brObjVlan
			return c, c.parseShowArgs(tail(args), false, true)
		case bridgeMatches(args[0], "tunnelshow"):
			c.obj = brObjVlanTunnel
			return c, c.parseShowArgs(tail(args), false, true)
		case args[0] == "global" && (len(args) == 1 || show(args[1], true)):
			c.obj = brObjVlanGlobal
			return c, c.parseShowArgs(tail(tail(args)), false, true)
		}
		c.refused = true
		return c, nil
	case "vni":
		if len(args) > 0 && !show(args[0], true) {
			c.refused = true
			return c, nil
		}
		c.obj = brObjVni
		return c, c.parseShowArgs(tail(args), false, false)
	case "mst":
		if len(args) > 0 && !show(args[0], false) {
			c.refused = true
			return c, nil
		}
		c.obj = brObjMst
		return c, c.parseShowArgs(tail(args), false, false)
	}
	c.noReq = true // an unknown object, or help: no request
	return c, nil
}

func tail(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s[1:]
}

// parseShowArgs is a *_show loop: dev (and link's master, not else-if:
// B1), vid where takeVid; other words ignored (B2). A keyword with no
// value, or a duplicate, refuses after ll_init_map.
func (c *bridgeCmd) parseShowArgs(args []string, master, takeVid bool) error {
	var dev, mst string
	vid := 0
	for i := 0; i < len(args); i++ {
		next := func() bool {
			if i+1 >= len(args) {
				c.refused = true // Command line is not complete
				return false
			}
			i++
			return true
		}
		if args[i] == "dev" {
			if !next() {
				return nil
			}
			if dev != "" {
				c.refused = true // duparg
				return nil
			}
			dev = args[i]
		} else if takeVid && args[i] == "vid" {
			if !next() {
				return nil
			}
			if vid != 0 {
				c.refused = true // duparg; vid 0 escapes it (B5)
				return nil
			}
			vid, _ = strconv.Atoi(args[i])
			continue
		}
		if master && args[i] == "master" {
			if !next() {
				return nil
			}
			if mst != "" {
				c.refused = true
				return nil
			}
			mst = args[i]
		}
	}
	c.dev, c.master = dev, mst
	return nil
}

var ifNameIndexRE = regexp.MustCompile(`^if([0-9]+)$`)

// bridgeCounts is how much the bridge rows checked.
type bridgeCounts struct{ requests, lookups, replies, json int }

// TestBridgeParseCmd pins the request model on each command shape.
func TestBridgeParseCmd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		cmd         string
		expected    string // obj flags names, or noreq / refused
	}{
		{"positive: link show", "bridge link show", "1 ---- []"},
		{"positive: -j -s -d -com flags", "bridge -j -s -d -com vlan show", "2 jsdc []"},
		{"positive: vlan with no command is show", "bridge vlan", "2 ---- []"},
		{"positive: tunnelshow with a dev", "bridge vlan tunnelshow dev vx0", "3 ---- [vx0]"},
		{"positive: vlan global show dev vid", "bridge vlan global show dev br0 vid 10", "4 ---- [br0]"},
		{"positive: vni show dev", "bridge -s vni show dev vx1", "5 -s-- [vx1]"},
		{"positive: mst show dev", "bridge mst show dev d1", "6 ---- [d1]"},
		{"positive: link show dev and master", "bridge link show dev d1 master br0", "1 ---- [d1 br0]"},
		{"negative: an unknown option", "bridge -z link show", "noreq"},
		{"negative: an unknown object", "bridge badobj", "noreq"},
		{"negative: help", "bridge help", "noreq"},
		{"negative: an unknown link command", "bridge link badcmd", "refused"},
		{"negative: dev with no value", "bridge link show dev", "refused"},
		{"negative: dev twice", "bridge link show dev d1 dev d2", "refused"},
		{"negative: vid twice", "bridge vlan show vid 5 vid 6", "refused"},
		{"boundary: no object", "bridge -j", "noreq"},
		{"corner: -c is color, -com is compression", "bridge -c vlan show", "2 ---- []"},
		{"corner: vlan sh matches show", "bridge vlan sh", "2 ---- []"},
		{"corner: vni sh is refused (strcmp)", "bridge vni sh", "refused"},
		{"corner: vlan global sh is refused (strcmp, B21)", "bridge vlan global sh", "refused"},
		{"corner: vid 0 escapes duparg (B5)", "bridge vlan show vid 0 vid 5", "2 ---- []"},
		{"corner: unknown words ignored (B2)", "bridge link show bogus", "1 ---- []"},
		{"corner: dev master re-read as the keyword (B1)", "bridge link show dev master", "refused"},
		{"corner: dev master master br0: master filter is the third word", "bridge link show dev master master br0", "1 ---- [master master]"},
		{"corner: m is mdb, not mst", "bridge m", "noreq"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			c, err := bridgeParseCmd(tc.cmd)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			switch {
			case c.noReq:
				got = "noreq"
			case c.refused:
				got = "refused"
			default:
				flag := func(b bool, ch string) string {
					if b {
						return ch
					}
					return "-"
				}
				got = fmt.Sprintf("%d %s%s%s%s %v", c.obj, flag(c.json, "j"), flag(c.stats, "s"), flag(c.details, "d"),
					flag(c.compress, "c"), c.names())
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestWsBridgeCorpus holds the bridge ws rows to the request model and the
// decoders (gobridge 6P): O3, every request bridge sent is rebuilt byte for
// byte (seq aside), in order, the name lookups from the link dump's cache
// and the kernel's answers; every object reply decodes; O4, each -j output
// that bridge prints from replies lists what they hold.
//
// go test ./pkg/xtcpnl/ -run TestWsBridgeCorpus
func TestWsBridgeCorpus(t *testing.T) {
	var c bridgeCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "bridge/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsBridgeRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 rebuilt %d requests (%d name lookups), %d replies decoded, O4 %d JSON outputs",
		c.requests, c.lookups, c.replies, c.json)
	// Pinned on the 7.3.1 corpus (V098).
	if want := (bridgeCounts{requests: 214, lookups: 5, replies: 213, json: 41}); c != want {
		t.Errorf("checked %+v, want %+v", c, want)
	}
}

func wsBridgeRow(t *testing.T, dir string, row wsRow, c *bridgeCounts) {
	cmd, err := bridgeParseCmd(row.Cmd)
	if err != nil {
		t.Fatal(err)
	}
	msgs := pcapNlMsgs(t, dir+"/ip.pcap")
	var reqIdx []int
	for i, m := range msgs {
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			reqIdx = append(reqIdx, i)
		}
	}
	if cmd.noReq {
		if len(msgs) != 0 {
			t.Fatalf("%d messages, want none", len(msgs))
		}
		return
	}
	// The answers to request k: the messages up to the next request.
	answers := func(k int) []capturedNlMsg {
		end := len(msgs)
		if k+1 < len(reqIdx) {
			end = reqIdx[k+1]
		}
		return msgs[reqIdx[k]+1 : end]
	}
	type build func(seq uint32) ([]byte, error)
	want := []build{func(seq uint32) ([]byte, error) {
		return BuildDumpLinkRequestExt(unix.AF_UNSPEC, llInitMapMaskCst, seq)
	}}
	cache := map[string]int32{}
	if len(reqIdx) > 0 {
		for _, m := range answers(0) {
			if m.h.Type != unix.RTM_NEWLINK {
				continue
			}
			l, err := ParseNewLink(m.body)
			if err != nil {
				t.Fatalf("ll_init_map reply: %v", err)
			}
			cache[l.Name] = l.Index
		}
	}
	// The filters' ifindexes, 0 for none.
	var index, masterIndex int32
	ok := !cmd.refused
	for i, n := range cmd.names() {
		if !ok {
			break
		}
		idx, hit := cache[n]
		if !hit {
			name := n
			want = append(want, func(seq uint32) ([]byte, error) {
				return BuildGetLinkByNameRequest(unix.AF_UNSPEC, name, llInitMapMaskCst, seq)
			})
			c.lookups++
			k := len(want) - 1
			if k >= len(reqIdx) {
				t.Fatalf("%d requests, want a lookup of %s", len(reqIdx), n)
			}
			for _, m := range answers(k) {
				if m.h.Type == unix.RTM_NEWLINK {
					l, err := ParseNewLink(m.body)
					if err != nil {
						t.Fatal(err)
					}
					idx, hit = l.Index, true
				}
			}
			if !hit {
				if sm := ifNameIndexRE.FindStringSubmatch(n); sm != nil {
					v, _ := strconv.Atoi(sm[1])
					u, _ := cint.U32(v)
					idx, hit = cint.Int(u), true
				}
			}
		}
		if !hit {
			ok = false // nodev
			break
		}
		if i == 0 && cmd.dev != "" {
			index = idx
		} else {
			masterIndex = idx
		}
	}
	objStart := len(want) // the object's own requests start here
	if ok {
		dump := func(mask uint32) build {
			return func(seq uint32) ([]byte, error) { return BuildDumpLinkRequestExt(unix.AF_BRIDGE, mask, seq) }
		}
		vlanMask := uint32(RtextFilterBrvlan)
		if cmd.compress {
			vlanMask = RtextFilterBrvlanCompressed
		}
		stats := func(bit uint32) build {
			return func(seq uint32) ([]byte, error) { return BuildDumpStatsRequest(unix.AF_UNSPEC, 0, bit, seq), nil }
		}
		brvlan := func(flags uint32) build {
			return func(seq uint32) ([]byte, error) { return BuildDumpBrVlanRequest(flags, seq) }
		}
		switch cmd.obj {
		case brObjNone:
			// Every command that reaches here names an object.
		case brObjLink:
			want = append(want, dump(0))
		case brObjVlan, brObjVlanTunnel:
			switch {
			case cmd.details && cmd.obj == brObjVlan:
				f := uint32(0)
				if cmd.stats {
					f = BridgeVlandbDumpfStats
				}
				want = append(want, brvlan(f))
			case !cmd.stats:
				want = append(want, dump(vlanMask))
			default:
				// The slave stats dump follows the first only when it
				// succeeded.
				want = append(want, stats(1<<(unix.IFLA_STATS_LINK_XSTATS-1)), stats(1<<(unix.IFLA_STATS_LINK_XSTATS_SLAVE-1)))
			}
		case brObjVlanGlobal:
			want = append(want, brvlan(BridgeVlandbDumpfGlobal))
		case brObjVni:
			fl := uint8(0)
			if cmd.stats {
				fl = TunnelMsgFlagStats
			}
			ix := cint.Bits(index)
			want = append(want, func(seq uint32) ([]byte, error) { return BuildDumpTunnelRequest(ix, fl, seq), nil })
		case brObjMst:
			want = append(want, dump(RtextFilterMst))
		}
	}
	if len(reqIdx) != len(want) {
		t.Fatalf("%d requests, want %d", len(reqIdx), len(want))
	}
	for i, b := range want {
		got := msgs[reqIdx[i]]
		built, err := b(got.h.Seq)
		if err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
		if err := tipcSameRequest(got, built); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		c.requests++
	}

	// Every object reply decodes.
	last := len(reqIdx) - 1
	var links []*BridgeLink
	var tunnels []TunnelMsg
	var vlans []BrVlanTables
	var stats []*IfStatsInfo
	if ok && last >= objStart {
		for k := objStart; k <= last; k++ {
			for _, m := range answers(k) {
				var err error
				switch m.h.Type {
				case unix.RTM_NEWLINK:
					var l BridgeLink
					if l, err = ParseBridgeLink(m.body); err == nil && l.Family == unix.AF_BRIDGE {
						links = append(links, &l)
					}
				case unix.RTM_NEWTUNNEL:
					var tm TunnelMsg
					if tm, err = ParseTunnelMsg(m.body); err == nil {
						tunnels = append(tunnels, tm)
					}
				case unix.RTM_NEWVLAN:
					var v BrVlanTables
					if v, err = ParseBrVlanTables(m.body); err == nil {
						vlans = append(vlans, v)
					}
				case unix.RTM_NEWSTATS:
					var st IfStatsInfo
					if st, err = ParseNewStats(m.body); err == nil {
						stats = append(stats, &st)
					}
				case unix.NLMSG_DONE, unix.NLMSG_ERROR:
					continue
				default:
					t.Fatalf("reply type %d", m.h.Type)
				}
				if err != nil {
					t.Fatalf("reply type %d: %v", m.h.Type, err)
				}
				c.replies++
			}
		}
	}

	if !cmd.json || row.Expect.RC == nil || *row.Expect.RC != 0 || !ok {
		return
	}
	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	var checked bool
	switch {
	case cmd.obj == brObjLink:
		err, checked = bridgeLinkJSON(out, links, index, masterIndex, cache), true
	case cmd.obj == brObjVni:
		err, checked = bridgeVniJSON(out, tunnels, index), true
	case cmd.obj == brObjVlanTunnel && !cmd.stats:
		err, checked = bridgeTunnelJSON(out, links, index), true
	case cmd.obj == brObjMst:
		err, checked = bridgeMstJSON(out, links, index), true
	case cmd.obj == brObjVlan && !cmd.stats && !cmd.details:
		err, checked = bridgeVlanJSON(out, links, index), true
	case cmd.obj == brObjVlanGlobal:
		err, checked = bridgeGlobalJSON(out, vlans, index), true
	case cmd.stats && !cmd.details && (cmd.obj == brObjVlan || cmd.obj == brObjVlanTunnel):
		err, checked = bridgeStatsJSON(out, stats), true
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if checked {
		c.json++
	}
}

// bridgeLinkName is a decoded link's IFLA_IFNAME.
func bridgeLinkName(l *BridgeLink) string { n, _ := l.Attrs.Str(unix.IFLA_IFNAME); return n }

// bridgeLinkJSON: `bridge -j link show` prints one object per link the
// filters pass (link.c print_linkinfo): the dev filter by index, the
// master filter by IFLA_MASTER, passing a link without one (B7).
func bridgeLinkJSON(out []byte, links []*BridgeLink, devIndex, masterIndex int32, cache map[string]int32) error {
	var got []struct {
		Ifindex int32
		Ifname  string
		Master  string
		Mtu     uint32
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	byIndex := map[int32]string{}
	for n, i := range cache {
		byIndex[i] = n
	}
	var want []*BridgeLink
	for _, l := range links {
		m, has := l.Attrs.U32(unix.IFLA_MASTER)
		if (devIndex != 0 && l.Index != devIndex) || (masterIndex != 0 && has && cint.Int(m) != masterIndex) {
			continue
		}
		want = append(want, l)
	}
	if len(got) != len(want) {
		return fmt.Errorf("%d objects for %d links", len(got), len(want))
	}
	for i, l := range want {
		m, _ := l.Attrs.U32(unix.IFLA_MASTER)
		mtu, _ := l.Attrs.U32(unix.IFLA_MTU)
		master := byIndex[cint.Int(m)]
		if got[i].Ifindex != l.Index || got[i].Ifname != bridgeLinkName(l) || got[i].Master != master || got[i].Mtu != mtu {
			return fmt.Errorf("object %d is %+v, want %d %s master %s mtu %d", i, got[i], l.Index, bridgeLinkName(l), master, mtu)
		}
	}
	return nil
}

// bridgeVniJSON: `bridge -j vni show` prints one object per device with
// entries, its vnis as {vni[, vniEnd]} (vni.c print_vni).
func bridgeVniJSON(out []byte, tunnels []TunnelMsg, index int32) error {
	var got []struct {
		Ifname string
		Vnis   []struct {
			Vni, VniEnd uint32
		}
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	var want []TunnelMsg
	for _, tm := range tunnels {
		if (index == 0 || int64(tm.Ifindex) == int64(index)) && len(tm.Entries) > 0 {
			want = append(want, tm)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%d objects for %d devices", len(got), len(want))
	}
	for i, tm := range want {
		if len(got[i].Vnis) != len(tm.Entries) {
			return fmt.Errorf("device %d: %d vnis for %d entries", i, len(got[i].Vnis), len(tm.Entries))
		}
		for j, e := range tm.Entries {
			s, _ := e.Attrs.U32(VxlanVnifilterEntryStart)
			end, _ := e.Attrs.U32(VxlanVnifilterEntryEnd)
			if got[i].Vnis[j].Vni != s || got[i].Vnis[j].VniEnd != end {
				return fmt.Errorf("device %d vni %d is %+v, want %d-%d", i, j, got[i].Vnis[j], s, end)
			}
		}
	}
	return nil
}

// bridgeVlanRange is one printed vlan or range from a link's VLAN list as
// print_vlan_info walks it: RANGE_BEGIN opens, RANGE_END closes.
type bridgeVlanRange struct{ vlan, vlanEnd uint16 }

func bridgeRanges(vs []BridgeVlanInfo) []bridgeVlanRange {
	var out []bridgeVlanRange
	var start uint16
	for _, v := range vs {
		switch {
		case v.Flags&BridgeVlanInfoRangeBegin != 0:
			start = v.Vid
		case v.Flags&BridgeVlanInfoRangeEnd != 0:
			out = append(out, bridgeVlanRange{start, v.Vid})
		default:
			out = append(out, bridgeVlanRange{v.Vid, 0})
		}
	}
	return out
}

// bridgeVlanJSON: `bridge -j vlan show` prints one object per link with
// VLANs (vlan.c print_vlan_info), each vlan or range once, unless a vid
// filter is set (the corpus's -j vid rows are checked by count alone).
func bridgeVlanJSON(out []byte, links []*BridgeLink, index int32) error {
	var got []struct {
		Ifname string
		Vlans  []struct{ Vlan, VlanEnd uint16 }
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	var want []*BridgeLink
	for _, l := range links {
		if len(l.Vlans) > 0 && (index == 0 || l.Index == index) {
			want = append(want, l)
		}
	}
	if len(got) > len(want) {
		return fmt.Errorf("%d objects for %d links with VLANs", len(got), len(want))
	}
	// A vid filter prints fewer; check those that are printed in full.
	for _, g := range got {
		for _, l := range want {
			if bridgeLinkName(l) != g.Ifname {
				continue
			}
			rs := bridgeRanges(l.Vlans)
			if len(g.Vlans) == len(rs) {
				for j, r := range rs {
					if g.Vlans[j].Vlan != r.vlan || g.Vlans[j].VlanEnd != r.vlanEnd {
						return fmt.Errorf("%s vlan %d is %+v, want %+v", g.Ifname, j, g.Vlans[j], r)
					}
				}
			}
		}
	}
	return nil
}

// bridgeTunnelJSON: `bridge -j vlan tunnelshow` prints one object per link
// with tunnels, each {vlan[, vlanEnd], tunid[, tunidEnd]}.
func bridgeTunnelJSON(out []byte, links []*BridgeLink, index int32) error {
	var got []struct {
		Ifname  string
		Tunnels []struct{ Vlan, VlanEnd, Tunid, TunidEnd uint32 }
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	n := 0
	for _, l := range links {
		if len(l.Tunnels) > 0 && (index == 0 || l.Index == index) {
			n++
		}
	}
	if len(got) != n {
		return fmt.Errorf("%d objects for %d links with tunnels", len(got), n)
	}
	return nil
}

// bridgeMstJSON: `bridge -j mst show` prints one object per link with MST
// entries, each {msti, state}.
func bridgeMstJSON(out []byte, links []*BridgeLink, index int32) error {
	var got []struct {
		Ifname string
		Mst    []struct{ Msti uint16 }
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	var want []*BridgeLink
	for _, l := range links {
		if l.HasMst && (index == 0 || l.Index == index) {
			want = append(want, l)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%d objects for %d links with MST", len(got), len(want))
	}
	for i, l := range want {
		if len(got[i].Mst) != len(l.Mst) {
			return fmt.Errorf("%s: %d entries for %d", bridgeLinkName(l), len(got[i].Mst), len(l.Mst))
		}
		for j, e := range l.Mst {
			m, _ := e.Attrs.U16(IflaBridgeMstEntryMsti)
			if got[i].Mst[j].Msti != m {
				return fmt.Errorf("%s entry %d msti %d, want %d", bridgeLinkName(l), j, got[i].Mst[j].Msti, m)
			}
		}
	}
	return nil
}

// bridgeGlobalJSON: `bridge -j vlan global show` prints one object per
// bridge with global options; checked by object count, unfiltered.
func bridgeGlobalJSON(out []byte, vlans []BrVlanTables, index int32) error {
	var got []struct {
		Ifname string
		Vlans  []struct{ Vlan uint16 }
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	n := 0
	for _, v := range vlans {
		if len(v.Tables) > 0 && (index == 0 || int64(v.Ifindex) == int64(index)) {
			n++
		}
	}
	if len(got) != n {
		return fmt.Errorf("%d objects for %d bridges", len(got), n)
	}
	return nil
}

// bridgeStatsJSON: `bridge -j -s vlan show` prints one object per link
// whose bridge xstats (the master's, then each port's from the slave dump)
// list VLANs it prints (print_vlan_stats_attr skips pure port entries),
// each keyed "vid", not "vlan" (B25).
func bridgeStatsJSON(out []byte, stats []*IfStatsInfo) error {
	var got []struct {
		Ifname string
		Vlans  []map[string]any
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	var want [][]BridgeVlanXstats
	for _, st := range stats {
		for _, all := range [][]BridgeVlanXstats{st.BridgeVlans, st.Slave.BridgeVlans} {
			// Pure port entries (MASTER without BRENTRY) are skipped:
			// the slave dump prints them.
			var vs []BridgeVlanXstats
			for _, v := range all {
				if v.Flags&BridgeVlanInfoMaster == 0 || v.Flags&BridgeVlanInfoBrentry != 0 {
					vs = append(vs, v)
				}
			}
			if len(vs) > 0 {
				want = append(want, vs)
			}
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("%d objects for %d links with vlan stats", len(got), len(want))
	}
	for i, vs := range want {
		if len(got[i].Vlans) != len(vs) {
			return fmt.Errorf("%s: %d vlans for %d", got[i].Ifname, len(got[i].Vlans), len(vs))
		}
		for j, v := range vs {
			vid, ok := got[i].Vlans[j]["vid"].(float64)
			if _, hasVlan := got[i].Vlans[j]["vlan"]; !ok || hasVlan || uint16(vid) != v.Vid {
				return fmt.Errorf("%s vlan %d is %v, want vid %d and no \"vlan\" key", got[i].Ifname, j, got[i].Vlans[j], v.Vid)
			}
		}
	}
	return nil
}
