package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestBridgeSpace pins the bridge rows (gobridge 6C): every row runs in a
// fresh namespace, captures rtnetlink with bridge and vxlan loaded, and
// carries the expected outcome for its axes. Rows are keyed by command and
// fixture.
//
// go test ./tools/nlcapgen/ -run TestBridgeSpace
func TestBridgeSpace(t *testing.T) {
	rows, err := matrixRows(families["bridge"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byCmd := map[string]row{}
	for _, r := range rows {
		if r.InitNS || r.Serial {
			t.Errorf("%s: initns %t serial %t, want neither", r.ID, r.InitNS, r.Serial)
		}
		if r.Proto != "route" || strings.Join(r.Modules, ",") != "bridge,vxlan" {
			t.Errorf("%s: proto %q modules %v, want route and [bridge vxlan]", r.ID, r.Proto, r.Modules)
		}
		k := r.Cmd + "|" + r.Axes["fix"]
		if _, dup := byCmd[k]; dup {
			t.Errorf("%s: %q in fixture %q twice", r.ID, r.Cmd, r.Axes["fix"])
		}
		byCmd[k] = r
	}

	tests := []struct {
		description string
		cmd         string
		fix         string
		expected    string // kind rc refused-by errno extack | pre count
	}{
		// Positive.
		{"positive: br link show, its ports", "bridge link show", "br", "positive 0 none   | 4"},
		{"positive: br -j vlan show", "bridge -j vlan show", "br", "positive 0 none   | 4"},
		{"positive: -com is compression", "bridge -com vlan show", "br", "positive 0 none   | 4"},
		{"positive: -s -d vlan show", "bridge -s -d vlan show", "br", "positive 0 none   | 4"},
		{"positive: global options after mcast_vlan_snooping", "bridge vlan global show dev br0 vid 10", "brglob", "positive 0 none   | 6"},
		{"positive: mst show with MST on", "bridge mst show", "brmst", "positive 0 none   | 5"},
		{"positive: tunnelshow on the vxlan port", "bridge -j vlan tunnelshow dev vx0", "vxtun", "positive 0 none   | 8"},
		{"positive: vni show with stats", "bridge -j -s vni show", "vnif", "positive 0 none   | 6"},
		// Negative.
		{"negative: an unknown option, no request", "bridge -z link show", "bare", "negative 255 iproute2   | 0"},
		{"negative: an unknown object, no request", "bridge badobj", "bare", "negative 255 iproute2   | 0"},
		{"negative: help exits 255", "bridge help", "bare", "negative 255 iproute2   | 0"},
		{"negative: unknown mst sub-command after the link dump", "bridge mst badcmd", "bare", "negative 255 ack-then-fail   | 0"},
		{"negative: nodev on vni's filter", "bridge vni show dev nosuch", "bare", "negative 255 kernel ENODEV  | 0"},
		{"negative: dev given twice", "bridge link show dev d1 dev d2", "br", "negative 255 ack-then-fail   | 4"},
		{"negative: B10, mst dump aborted with MST off", "bridge mst show", "br", "negative 255 ack-then-fail   | 4"},
		{"negative: B10 when one port has no VLAN", "bridge mst show dev d1", "brmstnov", "negative 255 ack-then-fail   | 3"},
		{"negative: vnifilter dump of a dummy", "bridge vni show dev d1", "br",
			"negative 1 kernel EINVAL The device is not a vxlan device | 4"},
		{"negative: B1, a device named master", "bridge link show dev master", "brmaster", "negative 255 ack-then-fail   | 2"},
		// Boundary.
		{"boundary: empty namespace, mst prints its header", "bridge mst show", "bare", "boundary 0 none   | 0"},
		{"boundary: empty -s vni show", "bridge -j -s vni show", "bare", "boundary 0 none   | 0"},
		{"boundary: vids 2-4094 compressed", "bridge -com vlan show dev d3", "brwide", "boundary 0 none   | 6"},
		// Corner.
		{"corner: vlan sh is show (matches)", "bridge vlan sh", "bare", "corner 0 none   | 0"},
		{"corner: vni sh is refused (strcmp)", "bridge vni sh", "bare", "negative 255 ack-then-fail   | 0"},
		{"corner: vlan global sh is refused (strcmp, B21)", "bridge vlan global sh", "bare", "negative 255 ack-then-fail   | 0"},
		{"corner: -c is color", "bridge -c vlan show", "br", "corner 0 none   | 4"},
		{"corner: vid abc is no filter (B5)", "bridge vlan show vid abc", "br", "corner 0 none   | 4"},
		{"corner: vid 0 escapes duparg (B5)", "bridge vlan show vid 0 vid 5", "br", "corner 0 none   | 4"},
		{"corner: -s tunnelshow is the stats path (B22)", "bridge -s vlan tunnelshow", "br", "corner 0 none   | 4"},
		{"corner: if999 accepted, ENODEV ignored (B24)", "bridge vlan show dev if999", "br", "negative 0 kernel-ignored ENODEV  | 4"},
		{"corner: master as a device then as the keyword", "bridge link show dev master master br0", "brmaster", "corner 0 none   | 2"},
		{"corner: master filter first, then dev master", "bridge link show master br0 dev master", "brmaster", "negative 255 ack-then-fail   | 2"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd+"|"+tc.fix]
			if !ok {
				t.Fatalf("no row runs %q in fixture %q", tc.cmd, tc.fix)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Expect.Extack, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	// Every positive command runs plain and -j.
	for k, r := range byCmd {
		if strings.HasPrefix(r.Desc, "negative:") || strings.Contains(r.Cmd, " -j ") {
			continue
		}
		cmd, fix, _ := strings.Cut(k, "|")
		if _, ok := byCmd[strings.Replace(cmd, "bridge ", "bridge -j ", 1)+"|"+fix]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}

	// Fixture order: MST is enabled when br0 is made, before any port; the
	// vnifilter device exists before its VNIs; every device is up only after
	// addrgenmode none.
	order := []struct {
		description string
		row         string
		first, then string
	}{
		{"positive: mst_enabled precedes the ports", "bridge mst show|brmst", "mst_enabled 1", "ip link add d1"},
		{"positive: mst_enabled precedes mst set", "bridge mst show|brmst", "mst_enabled 1", "bridge mst set"},
		{"positive: vnifilter precedes vni add", "bridge vni show|vnif", "vnifilter", "bridge vni add"},
		{"positive: mcast_vlan_snooping precedes global set", "bridge vlan global show|brglob", "mcast_vlan_snooping 1", "vlan global set"},
		{"positive: vlan_tunnel precedes tunnel_info", "bridge vlan tunnelshow|vxtun", "vlan_tunnel on", "tunnel_info"},
		{"positive: the VLAN precedes its tunnel_info", "bridge vlan tunnelshow|vxtun", "dev vx0 vid 100 &&", "tunnel_info"},
	}
	for _, tc := range order {
		t.Run(tc.description, func(t *testing.T) {
			pre := byCmd[tc.row].Pre
			var all string
			for _, p := range pre {
				all += p.Cmd + "\n"
			}
			i, j := strings.Index(all, tc.first), strings.Index(all, tc.then)
			if i < 0 || j < 0 || i >= j {
				t.Errorf("%q at %d, %q at %d in %q", tc.first, i, tc.then, j, all)
			}
		})
	}
	for _, r := range rows {
		for _, p := range r.Pre {
			for _, part := range strings.Split(p.Cmd, " && ") {
				if !strings.HasPrefix(part, "ip link set ") || !strings.HasSuffix(part, " up") || strings.HasPrefix(part, "ip link set br0") {
					continue
				}
				dev := strings.TrimPrefix(strings.TrimSuffix(strings.TrimPrefix(part, "ip link set "), " up"), "dev ")
				if !strings.Contains(p.Cmd, " "+dev+" addrgenmode none") {
					t.Errorf("%s: %s comes up without addrgenmode none", r.ID, dev)
				}
			}
		}
	}

	if len(rows) != 113 {
		t.Errorf("%d rows, want 113 (44 commands x 2 opts + 25 negatives)", len(rows))
	}
	// Two generations of the space are the same rows file.
	again, err := matrixRows(families["bridge"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	var a, b bytes.Buffer
	if err := writeRows(&a, nil, rows); err != nil {
		t.Fatal(err)
	}
	if err := writeRows(&b, nil, again); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two generations of the bridge space differ")
	}
}
