package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// netconfNames is the fixed index cache the netconf render tests resolve
// NETCONFA_IFINDEX against: index 1 is lo, every other index misses and renders
// "if%u" (the LLTab fallback print_netconf's ll_index_to_name relies on).
var netconfNames = fakeNames{1: {name: "lo"}}

// ni builds a NetconfInfo with a family and no attributes; the option funcs set
// each present attribute, so a row lists exactly the tokens it expects to render.
func ni(family uint8, opts ...func(*xtcpnl.NetconfInfo)) xtcpnl.NetconfInfo {
	n := xtcpnl.NetconfInfo{Family: family}
	for _, o := range opts {
		o(&n)
	}
	return n
}

func ifidx(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.Ifindex, n.HasIfindex = v, true }
}
func fwd(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.Forwarding, n.HasForwarding = v, true }
}
func rpf(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.RpFilter, n.HasRpFilter = v, true }
}
func mcf(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.McForwarding, n.HasMcForwarding = v, true }
}
func pn(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.ProxyNeigh, n.HasProxyNeigh = v, true }
}
func irwl(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.IgnoreRoutesWithLinkdown, n.HasIgnoreRoutesWithLinkdown = v, true }
}
func inp(v int32) func(*xtcpnl.NetconfInfo) {
	return func(n *xtcpnl.NetconfInfo) { n.Input, n.HasInput = v, true }
}

// TestNetconfViewText pins print_netconf's text form: a family token, an optional
// device token, then each present setting, every token with a trailing space and
// the line ending in that space before the newline. The format is transcribed
// from the ip_netconf golden; the values drive each render branch.
//
// go test ./internal/goip/render/ -run TestNetconfViewText
func TestNetconfViewText(t *testing.T) {
	tests := []struct {
		description string
		info        xtcpnl.NetconfInfo
		expected    string
	}{
		{
			description: "positive: inet all, forwarding on, rp_filter strict, mc/proxy/input off",
			info:        ni(unix.AF_INET, ifidx(xtcpnl.NetconfIfindexAll), fwd(1), rpf(1), mcf(0), pn(0), inp(0)),
			expected:    "inet all forwarding on rp_filter strict mc_forwarding off proxy_neigh off input off \n",
		},
		{
			description: "positive: inet default (ifindex -2), forwarding off, rp_filter loose",
			info:        ni(unix.AF_INET, ifidx(xtcpnl.NetconfIfindexDefault), fwd(0), rpf(2)),
			expected:    "inet default forwarding off rp_filter loose \n",
		},
		{
			description: "positive: inet mapped device (ifindex 1 -> lo)",
			info:        ni(unix.AF_INET, ifidx(1), fwd(1)),
			expected:    "inet lo forwarding on \n",
		},
		{
			description: "corner: inet uncached device (ifindex 7 -> if7)",
			info:        ni(unix.AF_INET, ifidx(7), fwd(1)),
			expected:    "inet if7 forwarding on \n",
		},
		{
			description: "corner: inet6 with no rp_filter omits that token",
			info:        ni(unix.AF_INET6, ifidx(xtcpnl.NetconfIfindexAll), fwd(1), mcf(0), pn(0), inp(0)),
			expected:    "inet6 all forwarding on mc_forwarding off proxy_neigh off input off \n",
		},
		{
			description: "boundary: rp_filter 0 renders off",
			info:        ni(unix.AF_INET, rpf(0)),
			expected:    "inet rp_filter off \n",
		},
		{
			description: "boundary: rp_filter 1 renders strict",
			info:        ni(unix.AF_INET, rpf(1)),
			expected:    "inet rp_filter strict \n",
		},
		{
			description: "boundary: rp_filter 2 renders loose",
			info:        ni(unix.AF_INET, rpf(2)),
			expected:    "inet rp_filter loose \n",
		},
		{
			description: "corner: rp_filter 3 (past the names) renders as a number",
			info:        ni(unix.AF_INET, rpf(3)),
			expected:    "inet rp_filter 3 \n",
		},
		{
			description: "positive: forwarding off renders off",
			info:        ni(unix.AF_INET, ifidx(1), fwd(0)),
			expected:    "inet lo forwarding off \n",
		},
		{
			description: "corner: no ifindex omits the device token",
			info:        ni(unix.AF_INET, fwd(1)),
			expected:    "inet forwarding on \n",
		},
		{
			description: "corner: unknown family 255 renders the ??? sentinel",
			info:        ni(255, ifidx(xtcpnl.NetconfIfindexAll), fwd(0)),
			expected:    "??? all forwarding off \n",
		},
		{
			description: "corner: AF_MPLS family renders mpls",
			info:        ni(unix.AF_MPLS, ifidx(xtcpnl.NetconfIfindexAll), fwd(0)),
			expected:    "mpls all forwarding off \n",
		},
		{
			description: "boundary: family only, every setting absent, is the minimal line",
			info:        ni(unix.AF_INET),
			expected:    "inet \n",
		},
		{
			description: "positive: ignore_routes_with_linkdown renders between proxy_neigh and input",
			info:        ni(unix.AF_INET, ifidx(1), fwd(0), rpf(2), mcf(0), pn(0), irwl(1), inp(0)),
			expected:    "inet lo forwarding off rp_filter loose mc_forwarding off proxy_neigh off ignore_routes_with_linkdown on input off \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := NetconfViewOf(tc.info, netconfNames).Text()
			if got != tc.expected {
				t.Errorf("Text()\n got %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// TestNetconfViewJSON pins the JSON object: on/off settings become booleans,
// rp_filter a string in range else a number, interface a string, and absent
// attributes emit no key. Transcribed against the ip_netconf_json golden's shape.
//
// go test ./internal/goip/render/ -run TestNetconfViewJSON$
func TestNetconfViewJSON(t *testing.T) {
	tests := []struct {
		description string
		info        xtcpnl.NetconfInfo
		expected    string
	}{
		{
			description: "positive: inet all, forwarding on, rp_filter strict -> booleans and a string",
			info:        ni(unix.AF_INET, ifidx(xtcpnl.NetconfIfindexAll), fwd(1), rpf(1), mcf(0)),
			expected:    `{"family":"inet","interface":"all","forwarding":true,"rp_filter":"strict","mc_forwarding":false}`,
		},
		{
			description: "positive: forwarding off renders a false boolean",
			info:        ni(unix.AF_INET, fwd(0)),
			expected:    `{"family":"inet","forwarding":false}`,
		},
		{
			description: "corner: rp_filter 3 renders a number, not a string",
			info:        ni(unix.AF_INET, rpf(3)),
			expected:    `{"family":"inet","rp_filter":3}`,
		},
		{
			description: "corner: inet6 with no rp_filter omits the key",
			info:        ni(unix.AF_INET6, fwd(1), mcf(0)),
			expected:    `{"family":"inet6","forwarding":true,"mc_forwarding":false}`,
		},
		{
			description: "corner: no ifindex omits the interface key",
			info:        ni(unix.AF_INET, fwd(1)),
			expected:    `{"family":"inet","forwarding":true}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			raw, err := json.Marshal(NetconfViewOf(tc.info, netconfNames))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(raw) != tc.expected {
				t.Errorf("JSON\n got %s\nwant %s", raw, tc.expected)
			}
		})
	}
}

// TestNetconfViewJSONKeyOrder asserts the keys emit in print_netconf order, the
// one property a per-key assertion cannot see.
//
// go test ./internal/goip/render/ -run TestNetconfViewJSONKeyOrder
func TestNetconfViewJSONKeyOrder(t *testing.T) {
	info := ni(unix.AF_INET, ifidx(xtcpnl.NetconfIfindexAll), fwd(0), rpf(2), mcf(0), pn(0), irwl(0), inp(0))
	raw, err := json.Marshal(NetconfViewOf(info, netconfNames))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := []string{
		"family", "interface", "forwarding", "rp_filter",
		"mc_forwarding", "proxy_neigh", "ignore_routes_with_linkdown", "input",
	}
	s := string(raw)
	prev := -1
	for _, k := range want {
		at := strings.Index(s, `"`+k+`":`)
		if at < 0 {
			t.Fatalf("key %q missing from %s", k, s)
		}
		if at < prev {
			t.Errorf("key %q at %d is before the previous key; order wrong in %s", k, at, s)
		}
		prev = at
	}
}
