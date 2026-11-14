//go:build goip_write

package goip

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestRouteWriteEncap is iproute_modify's and parse_one_nh's encap: the two
// attributes where the keyword is, in the route or in its hop, and ip's
// buffer bounds as refusals. The expected bytes are built here from the
// uapi numbers, not by the encoder. LE2-T01.
//
// go test ./internal/goip/ -run TestRouteWriteEncap
func TestRouteWriteEncap(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p24    = "198.51.100.0/24"
		gw1    = "192.0.2.1"
	)
	// rta is one rtattr as it goes on the wire, padding included.
	rta := func(typ uint16, payload ...[]byte) []byte {
		b := binary.LittleEndian.AppendUint16(nil, 0)
		b = binary.LittleEndian.AppendUint16(b, typ)
		for _, p := range payload {
			b = append(b, p...)
		}
		binary.LittleEndian.PutUint16(b, uint16(len(b)))
		return append(b, make([]byte, xtcpnl.FourByteAlignPadding(len(b)))...)
	}
	// encap is RTA_ENCAP|NLA_F_NESTED holding sub, then RTA_ENCAP_TYPE.
	encap := func(typ uint16, sub ...[]byte) []xtcpnl.RouteAttr {
		var p []byte
		for _, s := range sub {
			p = append(p, s...)
		}
		return []xtcpnl.RouteAttr{
			xtcpnl.RouteAttrBytes(unix.RTA_ENCAP|unix.NLA_F_NESTED, p),
			xtcpnl.RouteAttrBytes(unix.RTA_ENCAP_TYPE, binary.LittleEndian.AppendUint16(nil, typ)),
		}
	}
	mpls100 := encap(unix.LWTUNNEL_ENCAP_MPLS, rta(1, []byte{0, 0x06, 0x41, 0}))
	ipDst := encap(unix.LWTUNNEL_ENCAP_IP, rta(2, []byte{192, 0, 2, 9}))
	// geneve is `encap ip geneve_opts 0:0:<n zero bytes>`: 40 bytes and n.
	geneve := func(n int) []xtcpnl.RouteAttr {
		g := rta(xtcpnl.LwtunnelIPOptsGeneve|unix.NLA_F_NESTED, rta(1, []byte{0, 0}), rta(2, []byte{0}), rta(3, make([]byte, n)))
		return encap(unix.LWTUNNEL_ENCAP_IP, rta(xtcpnl.LwtunnelIPOpts|unix.NLA_F_NESTED, g))
	}
	geneveArgs := func(n int) string { return " encap ip geneve_opts 0:0:" + strings.Repeat("00", n) }
	gw := func(s string) xtcpnl.RouteAttr { return rtaAddr(unix.RTA_GATEWAY, s) }
	hop := func(attrs ...xtcpnl.RouteAttr) xtcpnl.RouteNexthop {
		return xtcpnl.RouteNexthop{Ifindex: 2, Attrs: attrs}
	}
	cat := func(parts ...[]xtcpnl.RouteAttr) []xtcpnl.RouteAttr {
		var out []xtcpnl.RouteAttr
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	one := func(a ...xtcpnl.RouteAttr) []xtcpnl.RouteAttr { return a }
	dst := one(rtaDst("198.51.100.0"))
	d0 := []string{"d0"}
	// route is `add 198.51.100.0/24 …` with attrs after RTA_DST, a dev
	// route's link scope unless universe, and the hops.
	route := func(universe bool, resolved []string, attrs []xtcpnl.RouteAttr, hops ...xtcpnl.RouteNexthop) routeWriteExpected {
		w := newRouteWrite(create|excl, 24, cat(dst, attrs)...)
		if universe {
			w = scopeUniverse(w)
		}
		w.Nexthops = hops
		return routeWriteExpected{write: w, resolved: resolved}
	}
	invalid := func(msg string, resolved ...string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: encap before via, in argv order", verb: "add", args: p24 + " encap mpls 100 via " + gw1 + " dev d0",
			expected: route(true, d0, cat(mpls100, one(gw(gw1), rtaOif(2))))},
		{description: "positive: encap after via", verb: "add", args: p24 + " via " + gw1 + " encap mpls 100 dev d0",
			expected: route(true, d0, cat(one(gw(gw1)), mpls100, one(rtaOif(2))))},
		{description: "positive: an ip encap on a dev route", verb: "add", args: p24 + " encap ip dst 192.0.2.9 dev d0",
			expected: route(false, d0, cat(ipDst, one(rtaOif(2))))},
		{description: "positive: an ip6 encap on an IPv6 route", verb: "add", family: unix.AF_INET6,
			args: "2001:db8::/64 encap ip6 dst 2001:db8:9::1 via 2001:db8::2",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 64, cat(one(rtaDst("2001:db8::")),
				encap(unix.LWTUNNEL_ENCAP_IP6, rta(2, []byte{0x20, 0x01, 0x0d, 0xb8, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})),
				one(rtaAddr(unix.RTA_GATEWAY, "2001:db8::2")))...),
				func(w *xtcpnl.RouteWrite) { w.Family, w.Scope = unix.AF_INET6, unix.RT_SCOPE_UNIVERSE })}},
		{description: "positive: a hop's encap is in its hop, not the route", verb: "add",
			args:     p24 + " nexthop encap ip dst 192.0.2.9 via " + gw1 + " dev d0 nexthop via 192.0.2.2 dev d0",
			expected: route(true, []string{"d0", "d0"}, nil, hop(cat(ipDst, one(gw(gw1)))...), hop(gw("192.0.2.2")))},
		{description: "positive: a hop's mpls stack and ttl", verb: "add",
			args: p24 + " nexthop via " + gw1 + " encap mpls 100/200 ttl 10 dev d0",
			expected: route(true, d0, nil, hop(cat(one(gw(gw1)),
				encap(unix.LWTUNNEL_ENCAP_MPLS, rta(1, []byte{0, 0x06, 0x40, 0, 0, 0x0c, 0x81, 0}), rta(2, []byte{10})))...))},
		{description: "positive: as in a hop is RTA_NEWDST in the hop, after its gateway", verb: "add",
			args:     p24 + " nexthop via " + gw1 + " as 10.0.0.1 dev d0",
			expected: route(true, d0, nil, hop(gw(gw1), xtcpnl.RouteAttrBytes(unix.RTA_NEWDST, []byte{10, 0, 0, 1})))},
		{description: "negative: a bad type in a hop", verb: "add", args: p24 + " nexthop encap foo 1 dev d0",
			expected: invalid(`"encap type" value is invalid`)},
		{description: "negative: bpf's mode word is obj, not the file", verb: "add", args: p24 + " encap bpf in obj.o dev d0",
			expected: routeWriteExpected{err: ErrBpfLoad, errMsg: `What mode is "obj.o"?`}},
		{description: "boundary: an encap that fills addraw_l's 1024 bytes", verb: "add", args: p24 + geneveArgs(948) + " dev d0",
			expected: route(false, d0, cat(geneve(948), one(rtaOif(2))))},
		{description: "boundary: four bytes more is over addraw_l's bound", verb: "add", args: p24 + geneveArgs(952) + " dev d0",
			expected: invalid("addraw_l ERROR: message exceeded bound of 1024")},
		{description: "boundary: a hop's encap over parse_nexthops' 4096 bytes", verb: "add", args: p24 + " nexthop" + geneveArgs(4048),
			expected: invalid("rta_addattr_l: Error! max allowed bound 4096 exceeded\nError: cannot parse nexthop")},
		{description: "corner: the route's attributes before the encap count to its bound", verb: "add",
			args:     p24 + " table 1000" + geneveArgs(948) + " dev d0",
			expected: invalid("addraw_l ERROR: message exceeded bound of 1024")},
		{description: "corner: the hops before count to a hop's bound", verb: "add",
			args:     p24 + " nexthop" + geneveArgs(2000) + " dev d0 nexthop" + geneveArgs(2000),
			expected: invalid("rta_addattr_l: Error! max allowed bound 4096 exceeded\nError: cannot parse nexthop", "d0")},
		{description: "corner: metric after the encap, as in argv", verb: "add", args: p24 + " encap ip dst 192.0.2.9 metric 10 dev d0",
			expected: route(false, d0, cat(ipDst, one(xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, 10), rtaOif(2))))},
	})
}
