package main

// The lwt spaces: goip-lwt-encap.md's forms, a lightweight-tunnel encap
// (mpls, ip and ip6, with the tunnel options) on a route, in a hop and on a
// nexthop object. Built as kwlive is: a verb and a form per space, ip-only
// setup, and kwNhModules loaded before the batch so the mpls rows don't
// autoload mpls_router mid-batch.

// The prefixes the forms share: the routes' destinations, and an ip encap's
// id and dst, which the option forms extend.
const (
	lwtP4   = "198.51.100.0/24"
	lwtP6   = "2001:db8:2::/64"
	lwtIP4  = lwtP4 + " encap ip id 1 dst 203.0.113.5"
	lwtIP6  = lwtP6 + " encap ip6 id 1 dst 2001:db8:9::1"
	lwtGw4  = " via 192.0.2.1 dev d0"
	lwtGw6  = " via 2001:db8:1::1 dev d0"
	lwtG1   = "0x0102:0x80:00112233"
	lwtDst4 = "203.0.113.5"
	lwtDst6 = "2001:db8:9::1"
)

// lwtIPFacts are an ip or ip6 encap's tun_id and dst, with more pairs
// after. tun_id is LWTUNNEL_IP_ID; a nexthop object's own id is "id".
func lwtIPFacts(typ, dst string, kv ...string) map[string]string {
	return f(append([]string{"encap", typ, "tun_id", "1", "dst", dst}, kv...)...)
}

// lwtGeneve1 is lwtG1's option: class 0x0102, type 0x80, data 00112233.
var lwtGeneve1 = []string{"opts", "geneve", "geneve1", "258:128:00112233"}

func lwtRoute4Space() *space {
	sp := &space{
		Name: "lwtroute4",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("mpls", lwtP4+" encap mpls 100"+lwtGw4, f("encap", "mpls", "labels", "100")),
			v("mpls-stack-ttl", lwtP4+" encap mpls 100/200 ttl 10"+lwtGw4, f("encap", "mpls", "labels", "100/200", "ttl", "10")),
			bv("mpls-ttl-0", lwtP4+" encap mpls 100 ttl 0"+lwtGw4, f("encap", "mpls", "labels", "100", "ttl", "0")),
			v("ip", lwtIP4+" dev d0", lwtIPFacts("ip", lwtDst4)),
			v("ip-every", lwtIP4+" src 192.0.2.10 tos 0x10 ttl 64 key csum seq dev d0",
				lwtIPFacts("ip", lwtDst4, "src", "192.0.2.10", "tos", "16", "ttl", "64", "flags", "13")),
			v("ip-geneve", lwtIP4+" geneve_opts "+lwtG1+" dev d0", lwtIPFacts("ip", lwtDst4, lwtGeneve1...)),
			cv("ip-geneve-base0", lwtIP4+" geneve_opts 0102:80:00112233 dev d0",
				lwtIPFacts("ip", lwtDst4, "opts", "geneve", "geneve1", "66:80:00112233")),
			v("ip-geneve-two", lwtIP4+" geneve_opts "+lwtG1+",0x0102:0x81:44556677 dev d0",
				lwtIPFacts("ip", lwtDst4, append(lwtGeneve1, "geneve2", "258:129:44556677")...)),
			v("ip-vxlan", lwtIP4+" vxlan_opts 0x800 dev d0", lwtIPFacts("ip", lwtDst4, "opts", "vxlan", "gbp", "2048")),
			v("ip-erspan", lwtIP4+" erspan_opts 1:0x1234:: dev d0",
				lwtIPFacts("ip", lwtDst4, "opts", "erspan", "ver", "1", "index", "4660")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("mpls-implicit-null", lwtP4+" encap mpls 3"+lwtGw4, f("encap", "mpls", "labels", "3")),
				Expect: *expKernelEINVAL, Why: "label 3, implicit null (nla_get_labels: Implicit NULL Label (3) can not be used in encapsulation)"},
			{Axis: "form", Value: v("geneve-data-3", lwtIP4+" geneve_opts 0x0102:0x80:001122 dev d0",
				lwtIPFacts("ip", lwtDst4, "opts", "geneve", "geneve1", "258:128:001122")),
				Expect: *expKernelEINVAL, Why: "geneve data of 3 bytes (ip_tun_parse_opts_geneve: data_len % 4)"},
			{Axis: "form", Value: v("two-opts", lwtIP4+" geneve_opts "+lwtG1+" vxlan_opts 1 dev d0", f("encap", "ip")),
				Expect: expIproute2Invarg, Why: `two option kinds (duparg2("opts"))`},
			{Axis: "form", Value: v("type-unknown", lwtP4+" encap foo 1 dev d0", f("encap", "foo")),
				Expect: expIproute2Invarg, Why: `an unknown type (invarg("encap type"))`},
			{Axis: "form", Value: v("type-last", lwtP4+" dev d0 encap ip key", f("encap", "ip")),
				Expect: expIproute2Invarg, Why: `the type's word last (missarg("encap type"))`},
		},
	}
	sp.Build = func(vals []value) row { return buildLwt("ip -4 route ", "ip -4 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func lwtRoute6Space() *space {
	sp := &space{
		Name: "lwtroute6",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("ip6", lwtIP6+" dev d0", lwtIPFacts("ip6", lwtDst6)),
			v("ip6-every", lwtIP6+" src 2001:db8:1::10 tc 0x10 hoplimit 64 key csum seq dev d0",
				lwtIPFacts("ip6", lwtDst6, "src", "2001:db8:1::10", "tos", "16", "ttl", "64", "flags", "13")),
			v("ip6-geneve", lwtIP6+" geneve_opts "+lwtG1+" dev d0", lwtIPFacts("ip6", lwtDst6, lwtGeneve1...)),
			v("mpls", lwtP6+" encap mpls 100"+lwtGw6, f("encap", "mpls", "labels", "100")),
			cv("ip-on-v6", lwtP6+" encap ip id 1 dst 203.0.113.5 dev d0", lwtIPFacts("ip", lwtDst4)),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwt("ip -6 route ", "ip -6 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func lwtHopSpace() *space {
	sp := &space{
		Name: "lwthop",
		Axes: []axis{{Name: "verb", Values: []value{v("add", "", f("verb", "add"))}}, {Name: "form", Values: []value{
			v("hop-mpls", "-4 route add "+lwtP4+" nexthop encap mpls 100"+lwtGw4+" nexthop via 192.0.2.2 dev d0",
				f("hop1_encap", "mpls", "hop1_labels", "100")),
			v("hops-ip", "-4 route add "+lwtP4+" nexthop encap ip id 1 dst 203.0.113.5"+lwtGw4+
				" nexthop encap ip id 2 dst 203.0.113.6 via 192.0.2.2 dev d0",
				f("hop1_encap", "ip", "hop1_tun_id", "1", "hop1_dst", lwtDst4, "hop2_encap", "ip", "hop2_tun_id", "2", "hop2_dst", "203.0.113.6")),
			v("hop-ip6", "-6 route add "+lwtP6+" nexthop encap ip6 dst "+lwtDst6+lwtGw6+" nexthop via 2001:db8:1::2 dev d0",
				f("hop1_encap", "ip6", "hop1_dst", lwtDst6)),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("hop-type-unknown", "-4 route add "+lwtP4+" nexthop encap foo 1"+lwtGw4, f("hop1_encap", "foo")),
				Expect: expIproute2Invarg, Why: `a hop's unknown type (invarg("encap type"))`},
		},
	}
	sp.Build = func(vals []value) row { return buildLwt("ip ", "ip route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func lwtNhSpace() *space {
	sp := &space{
		Name: "lwtnh",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("ip", "id 61 encap ip id 1 dst 203.0.113.5"+lwtGw4, lwtIPFacts("ip", lwtDst4, "id", "61")),
			v("ip6", "id 62 encap ip6 dst "+lwtDst6+lwtGw6, f("encap", "ip6", "dst", lwtDst6, "id", "62")),
			v("ip-geneve", "id 63 encap ip id 1 dst 203.0.113.5 geneve_opts "+lwtG1+lwtGw4,
				lwtIPFacts("ip", lwtDst4, append([]string{"id", "63"}, lwtGeneve1...)...)),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwt("ip nexthop ", "ip nexthop show", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ENOENT", nhHasID) }
	return sp
}

// buildLwt is buildKw over routeSetup, with the MPLS tunnel's modules.
func buildLwt(head, show string, vals []value) row {
	r := buildKw(head, routeSetup, show, vals)
	r.Modules = kwNhModules
	return r
}
