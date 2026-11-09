package xtcpnl

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// KC7-T02: every link kind iproute2 7.2.0 names is captured in the 7.3.0
// link-write corpus or excluded with a reason (docs/netlink/goip-link-kinds.md,
// "KC7-T02 The gate"). A kind is captured when one of its rows was accepted
// (ip.rc 0, with its ip.pcap) and a `-d -j` readback's linkinfo names it.

// linkKind is one link_util id and the 7.2.0 file that names it.
type linkKind struct {
	id  string
	src string
}

// linkKindUniverse is every link_util id in ip/iplink_*.c and ip/link_*.c at
// the iproute2 v7.2.0 pin.
var linkKindUniverse = []linkKind{
	{"amt", "ip/iplink_amt.c"},
	{"bareudp", "ip/iplink_bareudp.c"},
	{"batadv", "ip/iplink_batadv.c"},
	{"bond", "ip/iplink_bond.c"},
	{"bond_slave", "ip/iplink_bond_slave.c"},
	{"bridge", "ip/iplink_bridge.c"},
	{"bridge_slave", "ip/iplink_bridge_slave.c"},
	{"can", "ip/iplink_can.c"},
	{"dsa", "ip/iplink_dsa.c"},
	{"dummy", "ip/iplink_dummy.c"},
	{"erspan", "ip/link_gre.c"},
	{"geneve", "ip/iplink_geneve.c"},
	{"gre", "ip/link_gre.c"},
	{"gretap", "ip/link_gre.c"},
	{"gtp", "ip/iplink_gtp.c"},
	{"hsr", "ip/iplink_hsr.c"},
	{"ifb", "ip/iplink_ifb.c"},
	{"ip6erspan", "ip/link_gre6.c"},
	{"ip6gre", "ip/link_gre6.c"},
	{"ip6gretap", "ip/link_gre6.c"},
	{"ip6tnl", "ip/link_ip6tnl.c"},
	{"ipip", "ip/link_iptnl.c"},
	{"ipoib", "ip/iplink_ipoib.c"},
	{"ipvlan", "ip/iplink_ipvlan.c"},
	{"ipvtap", "ip/iplink_ipvlan.c"},
	{"macvlan", "ip/iplink_macvlan.c"},
	{"macvtap", "ip/iplink_macvlan.c"},
	{"netdevsim", "ip/iplink_netdevsim.c"},
	{"netkit", "ip/iplink_netkit.c"},
	{"nlmon", "ip/iplink_nlmon.c"},
	{"rmnet", "ip/iplink_rmnet.c"},
	{"sit", "ip/link_iptnl.c"},
	{"team", "ip/iplink_team.c"},
	{"team_slave", "ip/iplink_team.c"},
	{"vcan", "ip/iplink_vcan.c"},
	{"veth", "ip/link_veth.c"},
	{"virt_wifi", "ip/iplink_virt_wifi.c"},
	{"vlan", "ip/iplink_vlan.c"},
	{"vrf", "ip/iplink_vrf.c"},
	{"vrf_slave", "ip/iplink_vrf.c"},
	{"vti", "ip/link_vti.c"},
	{"vti6", "ip/link_vti6.c"},
	{"vxcan", "ip/iplink_vxcan.c"},
	{"vxlan", "ip/iplink_vxlan.c"},
	{"wwan", "ip/iplink_wwan.c"},
	{"xfrm", "ip/link_xfrm.c"},
}

// linkKindExclusions are the kinds the VM can't create, with the decision
// log's reasons (docs/netlink/goip-link-kinds-status.md).
var linkKindExclusions = map[string]string{
	"can":       "needs CAN hardware; vcan takes none of iplink_can.c's options",
	"dsa":       "needs a switch's conduit device",
	"ipoib":     "needs an InfiniBand lower device",
	"netdevsim": "created through sysfs, not rtnetlink",
	"rmnet":     "needs a Qualcomm modem lower device",
	"virt_wifi": "needs a wireless-capable lower device the VM lacks",
	"wwan":      "needs a WWAN modem device",
}

// linkKindSource says where a kind's rows are when they aren't
// kinds/<kind>/*: the glob of its rows and the -d -j readback they share.
type linkKindSource struct {
	rows     string
	readback string
}

// linkKindSources are the kinds whose rows predate the kind corpus. The
// write/ rows have no readbacks of their own, so each names the corpus's
// -d -j dump that shows a device of that kind: the converged LACP bond, its
// veth ports, and vrf_slave's enslaved dummy d0.
var linkKindSources = map[string]linkKindSource{
	"bond":       {rows: "write/add_bond_*", readback: "lacp_converged/ip_link_n_json"},
	"bond_slave": {rows: "write/slave_*", readback: "lacp_converged/ip_link_n_json"},
	"dummy":      {rows: "write/add_dummy", readback: "kinds/vrf_slave/set_master/ip_getlink_detail_json"},
	"veth":       {rows: "write/add_veth", readback: "lacp_converged/ip_link_n_json"},
}

// linkKindReadback is the -d -j readback link-write-readback.exp writes in
// each kinds/ row.
const linkKindReadback = "ip_getlink_detail_json"

// linkKindNamed reports whether the -d -j output raw shows a device of kind
// id: its info_kind, or for a _slave kind its info_slave_kind.
func linkKindNamed(raw []byte, id string) bool {
	var links []struct {
		Linkinfo struct {
			Kind      string `json:"info_kind"`
			SlaveKind string `json:"info_slave_kind"`
		} `json:"linkinfo"`
	}
	if json.Unmarshal(raw, &links) != nil {
		return false
	}
	master, slave := strings.CutSuffix(id, "_slave")
	for _, l := range links {
		if slave && l.Linkinfo.SlaveKind == master || !slave && l.Linkinfo.Kind == id {
			return true
		}
	}
	return false
}

// linkKindAccepted reports whether row dir in fsys was accepted and sent:
// ip.rc 0 and an ip.pcap.
func linkKindAccepted(fsys fs.FS, dir string) bool {
	raw, err := fs.ReadFile(fsys, path.Join(dir, "ip.rc"))
	if err != nil || strings.TrimSpace(string(raw)) != "0" {
		return false
	}
	_, err = fs.Stat(fsys, path.Join(dir, "ip.pcap"))
	return err == nil
}

// linkKindCaptured reports whether kind id has an accepted row whose
// readback names it.
func linkKindCaptured(fsys fs.FS, id string) bool {
	src, ok := linkKindSources[id]
	if !ok {
		src = linkKindSource{rows: path.Join("kinds", id, "*")}
	}
	rows, err := fs.Glob(fsys, src.rows)
	if err != nil {
		return false
	}
	for _, dir := range rows {
		if !linkKindAccepted(fsys, dir) {
			continue
		}
		rb := src.readback
		if rb == "" {
			rb = path.Join(dir, linkKindReadback)
		}
		if raw, err := fs.ReadFile(fsys, rb); err == nil && linkKindNamed(raw, id) {
			return true
		}
	}
	return false
}

// linkKindWalk returns the captured kinds of fsys: the universe's kinds and
// every kinds/<kind> directory, so a kind 7.2.0 doesn't name is seen too.
func linkKindWalk(fsys fs.FS) map[string]bool {
	ids := map[string]bool{}
	for _, k := range linkKindUniverse {
		ids[k.id] = true
	}
	if dirs, err := fs.ReadDir(fsys, "kinds"); err == nil {
		for _, d := range dirs {
			ids[d.Name()] = true
		}
	}
	seen := map[string]bool{}
	for id := range ids {
		if linkKindCaptured(fsys, id) {
			seen[id] = true
		}
	}
	return seen
}

// linkKindProblems returns, sorted, every way the corpus and the lists fail
// the gate: an in-scope kind not captured, an exclusion with no reason, a
// stale exclusion, an exclusion or capture of no 7.2.0 kind, a kind citing
// no source, and a kind listed twice.
func linkKindProblems(universe []linkKind, excl map[string]string, seen map[string]bool) []string {
	var out []string
	named := map[string]bool{}
	for _, k := range universe {
		if named[k.id] {
			out = append(out, k.id+": listed twice")
		}
		named[k.id] = true
		if k.src == "" {
			out = append(out, k.id+": cites no iproute2 source")
		}
		reason, excluded := excl[k.id]
		switch {
		case excluded && reason == "":
			out = append(out, k.id+": excluded with no reason")
		case excluded && seen[k.id]:
			out = append(out, k.id+": captured and excluded; the exclusion is stale")
		case !excluded && !seen[k.id]:
			out = append(out, k.id+": no accepted row with a -d readback")
		}
	}
	for id := range excl {
		if !named[id] {
			out = append(out, id+": excluded but not a 7.2.0 kind")
		}
	}
	for id := range seen {
		if !named[id] {
			out = append(out, id+": captured but not a 7.2.0 kind")
		}
	}
	slices.Sort(out)
	return out
}

func TestLinkKindUniverse(t *testing.T) {
	corpus := linkKindWalk(os.DirFS(tdLinkWrite_7_3_0))
	t.Logf("%d of %d kinds captured, %d excluded", len(corpus), len(linkKindUniverse), len(linkKindExclusions))

	// with returns a copy of the corpus's captured kinds plus add, minus del.
	with := func(add, del []string) map[string]bool {
		m := map[string]bool{}
		for id := range corpus {
			if !slices.Contains(del, id) {
				m[id] = true
			}
		}
		for _, id := range add {
			m[id] = true
		}
		return m
	}
	// exclWith returns a copy of the exclusions with id's reason set.
	exclWith := func(id, reason string) map[string]string {
		m := map[string]string{id: reason}
		for k, v := range linkKindExclusions {
			if k != id {
				m[k] = v
			}
		}
		return m
	}

	tests := []struct {
		description string
		universe    []linkKind
		excl        map[string]string
		seen        map[string]bool
		expected    []string
	}{
		{
			description: "positive: the committed corpus passes",
			universe:    linkKindUniverse,
			excl:        linkKindExclusions,
			seen:        corpus,
			expected:    nil,
		},
		{
			description: "negative: an in-scope kind with no row",
			universe:    linkKindUniverse,
			excl:        linkKindExclusions,
			seen:        with(nil, []string{"netkit"}),
			expected:    []string{"netkit: no accepted row with a -d readback"},
		},
		{
			description: "negative: an exclusion with no reason",
			universe:    linkKindUniverse,
			excl:        exclWith("can", ""),
			seen:        corpus,
			expected:    []string{"can: excluded with no reason"},
		},
		{
			description: "corner: an exclusion the corpus contradicts",
			universe:    linkKindUniverse,
			excl:        linkKindExclusions,
			seen:        with([]string{"can"}, nil),
			expected:    []string{"can: captured and excluded; the exclusion is stale"},
		},
		{
			description: "corner: a captured kind 7.2.0 doesn't name",
			universe:    linkKindUniverse,
			excl:        linkKindExclusions,
			seen:        with([]string{"foo"}, nil),
			expected:    []string{"foo: captured but not a 7.2.0 kind"},
		},
		{
			description: "corner: an exclusion of a kind 7.2.0 doesn't name",
			universe:    linkKindUniverse,
			excl:        exclWith("foo", "a reason"),
			seen:        corpus,
			expected:    []string{"foo: excluded but not a 7.2.0 kind"},
		},
		{
			description: "corner: a kind listed twice",
			universe:    append(slices.Clone(linkKindUniverse), linkKind{"vrf", "ip/iplink_vrf.c"}),
			excl:        linkKindExclusions,
			seen:        corpus,
			expected:    []string{"vrf: listed twice"},
		},
		{
			description: "boundary: a kind citing no source",
			universe:    append(slices.DeleteFunc(slices.Clone(linkKindUniverse), func(k linkKind) bool { return k.id == "vrf" }), linkKind{"vrf", ""}),
			excl:        linkKindExclusions,
			seen:        corpus,
			expected:    []string{"vrf: cites no iproute2 source"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := linkKindProblems(tt.universe, tt.excl, tt.seen)
			if !slices.Equal(got, tt.expected) {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestLinkKindCaptured drives the walk's capture rule over hand-built rows.
func TestLinkKindCaptured(t *testing.T) {
	// row returns one kinds/ row with status rc and a -d -j readback.
	row := func(dir, rc, readback string) fstest.MapFS {
		m := fstest.MapFS{
			dir + "/ip.pcap": {Data: []byte{0}},
			dir + "/ip.rc":   {Data: []byte(rc + "\n")},
		}
		if readback != "" {
			m[dir+"/"+linkKindReadback] = &fstest.MapFile{Data: []byte(readback)}
		}
		return m
	}
	// merge returns the union of the rows.
	merge := func(fss ...fstest.MapFS) fstest.MapFS {
		m := fstest.MapFS{}
		for _, f := range fss {
			for k, v := range f {
				m[k] = v
			}
		}
		return m
	}
	const vxcanJSON = `[{"ifname":"vc0","linkinfo":{"info_kind":"vxcan"}}]`

	tests := []struct {
		description string
		fsys        fs.FS
		id          string
		expected    bool
	}{
		{
			description: "positive: an accepted row whose readback names its kind",
			fsys:        row("kinds/vxcan/add", "0", vxcanJSON),
			id:          "vxcan",
			expected:    true,
		},
		{
			description: "negative: an accepted row with no readback",
			fsys:        row("kinds/vxcan/add", "0", ""),
			id:          "vxcan",
			expected:    false,
		},
		{
			description: "negative: an accepted row with no request",
			fsys:        fstest.MapFS{"kinds/vxcan/add/ip.rc": {Data: []byte("0\n")}, "kinds/vxcan/add/" + linkKindReadback: {Data: []byte(vxcanJSON)}},
			id:          "vxcan",
			expected:    false,
		},
		{
			description: "boundary: one accepted row is enough",
			fsys:        merge(row("kinds/vxcan/add", "0", vxcanJSON), row("kinds/vxcan/add_bad", "2", vxcanJSON), row("kinds/vxcan/add_usage", "255", "")),
			id:          "vxcan",
			expected:    true,
		},
		{
			description: "boundary: refused rows alone are not",
			fsys:        merge(row("kinds/vxcan/add_bad", "2", vxcanJSON), row("kinds/vxcan/add_usage", "255", vxcanJSON)),
			id:          "vxcan",
			expected:    false,
		},
		{
			description: "corner: the readback names another kind",
			fsys:        row("kinds/vxcan/add", "0", `[{"linkinfo":{"info_kind":"vrf"}}]`),
			id:          "vxcan",
			expected:    false,
		},
		{
			description: "corner: a slave kind matches its slave kind",
			fsys:        row("kinds/bridge_slave/set_cost", "0", `[{"linkinfo":{"info_kind":"dummy","info_slave_kind":"bridge"}}]`),
			id:          "bridge_slave",
			expected:    true,
		},
		{
			description: "corner: a slave kind's master device is not its capture",
			fsys:        row("kinds/bridge_slave/set_cost", "0", `[{"linkinfo":{"info_kind":"bridge"}}]`),
			id:          "bridge_slave",
			expected:    false,
		},
		{
			description: "corner: a readback that isn't JSON",
			fsys:        row("kinds/vxcan/add", "0", "4: vc0@vc1: <NOARP>"),
			id:          "vxcan",
			expected:    false,
		},
		{
			description: "corner: a write/ kind reads its shared readback",
			fsys: merge(row("write/add_veth", "0", ""), fstest.MapFS{
				"lacp_converged/ip_link_n_json": {Data: []byte(`[{"linkinfo":{"info_kind":"veth","info_slave_kind":"bond"}}]`)},
			}),
			id:       "veth",
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := linkKindCaptured(tt.fsys, tt.id); got != tt.expected {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}
