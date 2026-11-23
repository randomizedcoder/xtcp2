package main

import "strings"

// The tunnel read space (goip increment 8, 8C): `ip tunnel show` and
// `ip -6 tunnel show` (ip/iptunnel.c, ip/ip6tunnel.c, ip/tunnel.c), each
// plain and -j, over fixed fixtures of every tunnel kind the two list.
//
// ip dumps the links, then asks each tunnel of the family's ARPHRDs for its
// parameters with SIOCGETTUNNEL (and SIOCGETPRL, SIOCGET6RD). nlmon sees
// only the dump, so every row carries the `tnlsys` readback: tnlsnap runs
// the same `ip` under ptrace and records each ioctl's input and output
// (internal/tnlsys), which goip's replay reads through GOIP_TNLSYS.
//
// The rows pin the plan's leads (docs/netlink/goip-tunnel.md; T1-T21 in
// upstream/iproute2-tunnel-tuntap.md once 8B writes it): -s text inside
// -j (T1); a mode or a gre/ipip/sit/vti name skipping the list for the
// fallback device, whose answer depends on the parameters passed (T5);
// keys refused without a mode (T6); the printers' text/JSON differences
// (T7); the 16-entry PRL buffer (T8); the v6 named path's missing newline
// and -s sending it to the list (T9); `mode any` switching to v6, and
// staying switched for the next batch line (T10, T21); "invalid TTL\n"
// (T11); the v6 proto match (T12); a v4 show naming an ip6gre device, which
// the kernel answers with 76 bytes into 52 (T19); and v6 encaplimit's
// unchecked get_u8 (T20).
//
// Determinism: fixed names, addresses and MACs; the tunnels stay down, so
// -s counters stay 0. Writes appear only as refusals ip makes before any
// ioctl.

// tunnelModules are every kind the fixtures make, so each row's namespace
// has all fallback devices: tunl0, gre0 (and gretap0, erspan0), sit0,
// ip_vti0, ip6tnl0, ip6gre0 and ip6_vti0.
var tunnelModules = []string{"ipip", "ip_gre", "sit", "ip_vti", "ip6_tunnel", "ip6_gre", "ip6_vti"}

// tnlD0 is the underlay: dummy d0 with a v4 and a v6 address.
const tnlD0 = "ip link add d0 address 02:00:00:00:01:01 type dummy && ip link set d0 addrgenmode none" +
	" && ip link set d0 up && ip addr add 192.0.2.1/24 dev d0 && ip addr add 2001:db8::1/64 dev d0 nodad"

// tnl4Fix is one of each v4 kind: ipip1 (ttl, tos, dev), ipip2 (remote,
// local any, nopmtudisc, ttl and tos inherit; not remote any too, which
// would be tunl0's parameters, EEXIST), gre1 (key, seq, csum), gre2
// (ikey/okey differ), gre3 (a multicast remote), vti1, and sit1. Those are
// made over netlink, and the kernel then reports their iph.protocol as 0,
// "any/ip" (K2). isa1 and sit2 are made with `ip tunnel add`, as their own
// mode, so isa1's routers print: print_tunnel reads the PRL only for
// protocol IPPROTO_IPV6 with SIT_ISATAP. isan is an isatap made over
// netlink, whose routers do not print. No 6rd: the workbench kernel has no
// CONFIG_IPV6_SIT_6RD, so SIOCADD6RD is EINVAL and SIOCGET6RD's EINVAL is
// the one error ip skips silently (K1); 6rd printing is a unit row.
var tnl4Fix = []string{
	tnlD0,
	"ip link add ipip1 type ipip remote 198.51.100.1 local 192.0.2.1 ttl 64 tos 0x10 dev d0",
	"ip link add ipip2 type ipip remote 198.51.100.9 nopmtudisc tos inherit",
	"ip link add gre1 type gre remote 198.51.100.2 local 192.0.2.1 key 5 seq csum ttl 32",
	"ip link add gre2 type gre remote 198.51.100.3 local 192.0.2.1 ikey 1 okey 2 iseq ocsum",
	"ip link add gre3 type gre remote 239.1.1.1 local 192.0.2.1 ttl 1",
	"ip link add sit1 type sit remote 198.51.100.4 local 192.0.2.1 ttl 64",
	"ip tunnel add sit2 mode sit remote 198.51.100.8 local 192.0.2.1 ttl 16",
	"ip tunnel add isa1 mode isatap local 192.0.2.1",
	"ip tunnel prl dev isa1 prl-default 192.0.2.10 && ip tunnel prl dev isa1 prl-nodefault 192.0.2.11",
	"ip link add isan type sit local 192.0.2.2 isatap && ip tunnel prl dev isan prl-default 192.0.2.12",
	"ip link add vti1 type vti remote 198.51.100.5 local 192.0.2.1 ikey 7 okey 8",
}

// tnlPrl16Fix is isa16 with sixteen routers: ip reads them into a
// 16-entry buffer whose first entry is the header, so 15 fit (T8).
var tnlPrl16Fix = []string{
	tnlD0,
	"ip tunnel add isa16 mode isatap local 192.0.2.1",
	"i=10; while [ $i -lt 26 ]; do ip tunnel prl dev isa16 prl-nodefault 192.0.2.$i || exit 1; i=$((i+1)); done",
}

// tnl6Fix is one of each v6 kind: ip6t1 (ip6ip6, encaplimit none,
// hoplimit 1, tclass inherit, a flowlabel, allow-localremote, dev),
// ip6t2 (ipip6, tclass 0x20, flowlabel inherit, dscp inherit), ip6g1
// (ip6gre with key, seq, csum), vti61, and gre6x, an ip6gre whose name
// starts with gre, which a v4 show asks by name (T19).
var tnl6Fix = []string{
	tnlD0,
	"ip link add ip6t1 type ip6tnl mode ip6ip6 remote 2001:db8::2 local 2001:db8::1 encaplimit none" +
		" hoplimit 1 tclass inherit flowlabel 0x12345 allow-localremote dev d0",
	"ip link add ip6t2 type ip6tnl mode ipip6 remote 2001:db8::3 local 2001:db8::1 encaplimit 2" +
		" hoplimit 64 tclass 0x20 flowlabel inherit dscp inherit",
	"ip link add ip6g1 type ip6gre remote 2001:db8::4 local 2001:db8::1 key 9 seq csum",
	"ip link add vti61 type vti6 remote 2001:db8::5 local 2001:db8::1 ikey 3 okey 4",
	"ip link add gre6x type ip6gre remote 2001:db8::6 local 2001:db8::1",
}

// tnlExtFix is the kinds the lists skip or barely know: an external
// (collect_md) ipip, and the Ethernet gretap, erspan and ip6gretap,
// whose ARPHRD_ETHER neither list takes.
var tnlExtFix = []string{
	tnlD0,
	"ip link add ipipx type ipip external",
	"ip link add gtap1 type gretap remote 198.51.100.6 local 192.0.2.1 key 11",
	"ip link add ersp1 type erspan remote 198.51.100.7 local 192.0.2.1 seq key 12 erspan_ver 1 erspan 5",
	"ip link add g6tap1 type ip6gretap remote 2001:db8::7 local 2001:db8::1",
}

// tunnelSpace: ip tunnel show and ip -6 tunnel show, each plain and -j.
func tunnelSpace() *space {
	// exit(-1) is 255; a returned -1 is 1, since do_cmd returns
	// -(func()): an ioctl failure, nodev, refused keys, a refused add.
	tool := expect{RC: intp(255), RefusedBy: refIproute2}
	ret := expect{RC: intp(1), RefusedBy: refIproute2}
	sp := &space{
		Name: "tunnel",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// No tunnels but the fallback devices.
				bc(brBoundary, "bare", "tunnel show", nil),
				bc(brBoundary, "bare", "-6 tunnel show", nil),
				// v4: the list.
				bc(brPos, "tnl4", "tunnel show", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel", tnl4Fix), // no verb: show
				bc(brPos, "tnl4", "tunnel list", tnl4Fix),
				bc(brPos, "tnl4", "tunnel lst", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel sh", tnl4Fix), // matches(): a prefix
				bc(brCorner, "tnl4", "tunl show", tnl4Fix), // ip's alias object
				bc(brPos, "tnl4", "-4 tunnel show", tnl4Fix),
				bc(brPos, "tnl4", "-s tunnel show", tnl4Fix), // printf stats, inside -j too (T1)
				bc(brCorner, "tnl4", "-d tunnel show", tnl4Fix),
				bc(brCorner, "tnl4", "-o tunnel show", tnl4Fix), // _SL_ in the gre flags
				bc(brCorner, "tnl4", "-p tunnel show", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show remote 198.51.100.2", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show local 192.0.2.1", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show dev d0", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel show isa1", tnl4Fix), // no prefix: the list, by name
				bc(brCorner, "tnl4", "tunnel show isan", tnl4Fix), // a netlink isatap: no routers (K2)
				bc(brPos, "tnl4", "tunnel show sit2", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel show nosuch", tnl4Fix),     // the list finds nothing, rc 0
				bc(brCorner, "tnl4", "tunnel show name ipip2", tnl4Fix), // name: the ipip prefix asks ipip2
				// v4: a name prefix or a mode asks one device (T5).
				bc(brPos, "tnl4", "tunnel show gre1", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show ipip1", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show sit1", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show vti1", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show mode gre", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show mode ipip", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show mode sit", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show mode isatap", tnl4Fix),
				bc(brPos, "tnl4", "tunnel show mode vti", tnl4Fix),
				bc(brCorner, "tnl4", "-s tunnel show gre1", tnl4Fix), // the named path prints no stats
				// The fallback device answers for the tunnel the parameters describe.
				bc(brCorner, "tnl4", "tunnel show mode gre remote 198.51.100.2 local 192.0.2.1 key 5", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel show mode gre remote 198.51.100.2", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel show mode gre remote 239.1.1.1 local 192.0.2.1", tnl4Fix),
				bc(brCorner, "tnl4", "tunnel show mode vti remote 198.51.100.5 local 192.0.2.1 ikey 7 okey 8", tnl4Fix),
				// mode any is a v6 mode (T10).
				bc(brCorner, "tnl4", "tunnel show mode any", tnl4Fix),
				bc(brCorner, "tnl4", "-6 tunnel show", tnl4Fix),
				bc(brPos, "prl16", "tunnel show isa16", tnlPrl16Fix), // 15 of 16 routers (T8)
				// v6.
				bc(brPos, "tnl6", "-6 tunnel show", tnl6Fix),
				bc(brCorner, "tnl6", "-6 tunnel", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel list", tnl6Fix),
				bc(brPos, "tnl6", "-s -6 tunnel show", tnl6Fix),
				bc(brCorner, "tnl6", "-o -6 tunnel show", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show ip6t1", tnl6Fix),       // named: no final newline (T9)
				bc(brCorner, "tnl6", "-s -6 tunnel show ip6t1", tnl6Fix), // -s: the list (T9)
				bc(brPos, "tnl6", "-6 tunnel show name ip6g1", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show ip6tnl0", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show mode ip6gre", tnl6Fix), // proto 0 passes (T12)
				bc(brPos, "tnl6", "-6 tunnel show mode ip6ip6", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show mode ipip6", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show mode vti6", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show mode any", tnl6Fix),
				bc(brCorner, "tnl6", "tunnel show mode ip6ip6", tnl6Fix), // the pre-scan picks v6
				bc(brPos, "tnl6", "-6 tunnel show remote 2001:db8::2", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show local 2001:db8::1", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show dev d0", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show encaplimit none", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show hoplimit 1", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show tclass inherit", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show flowlabel 0x12345", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show key 9", tnl6Fix),
				bc(brPos, "tnl6", "-6 tunnel show allow-localremote", tnl6Fix),
				bc(brCorner, "tnl6", "-6 tunnel show encaplimit 300", tnl6Fix), // get_u8 fails unchecked (T20)
				bc(brPos, "tnl6", "tunnel show", tnl6Fix),
				bc(brCorner, "tnl6", "tunnel show gre6x", tnl6Fix), // 76 bytes into 52 (T19)
				// External and Ethernet kinds.
				bc(brCorner, "ext", "tunnel show", tnlExtFix),
				bc(brCorner, "ext", "-6 tunnel show", tnlExtFix),
				bc(brCorner, "ext", "tunnel show ipipx", tnlExtFix),
				// Batch: ip -batch, and what carries between lines.
				bb(brCorner, "tnl4", "twice", "", false, batchFile("tunnel show", "tunnel show"), tnl4Fix),
				bb(brCorner, "tnl4", "mode-then-list", "", false,
					batchFile("tunnel show mode gre", "tunnel show"), tnl4Fix),
				bb(brCorner, "tnl4", "any-sticks", "", false, // preferred_family stays AF_INET6 (T21)
					batchFile("tunnel show mode any", "tunnel show"), tnl4Fix),
				// A batch line takes no options: the mode pre-scan reaches the v6
				// named path, whose missing newline runs into the next line (T9).
				bb(brCorner, "tnl6", "v6-named-then-list", "", true,
					batchFile("tunnel show mode ip6ip6 ip6t1", "tunnel show"), tnl6Fix),
			}},
		},
		Bad: []badValue{
			brBad("tnl4", "tunnel show gre9", tnl4Fix, ret, "a gre name asks gre9 by ioctl: ENODEV, rc 1 (T5)"),
			brBad("tnl4", "tunnel show key 5", tnl4Fix, ret, "keys without a gre or vti mode are refused (T6)"),
			brBad("bare", "tunnel show mode bogus", nil, tool, "Unknown tunnel mode, exit(-1)"),
			brBad("bare", "tunnel show mode gre mode ipip", nil, tool, "more than one tunnel mode, exit(-1)"),
			brBad("bare", "tunnel show mode", nil, tool, "mode with no value: the command is incomplete"),
			brBad("bare", "tunnel show ttl 300", nil, tool, "invarg(\"invalid TTL\\n\"): two newlines (T11)"),
			brBad("bare", "tunnel show ttl bogus", nil, tool, "invalid TTL"),
			brBad("bare", "tunnel show tos bogus", nil, tool, "bad TOS value"),
			brBad("bare", "tunnel show key bogus", nil, tool, "tnl_parse_key: not an unsigned integer"),
			brBad("bare", "tunnel show remote bogus", nil, ret, "get_addr32: not an address, exit(1)"),
			brBad("bare", "tunnel show name a name b", nil, tool, "duparg2 name"),
			brBad("bare", "tunnel show help", nil, tool, "a prefix of help in the words prints the usage"),
			brBad("bare", "tunnel show dev nosuch", nil, expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				"ll_name_to_index asks the kernel, then nodev"),
			brBad("bare", "tunnel bogus", nil, tool, "an unknown tunnel command"),
			brBad("bare", "tunnel help", nil, tool, "the usage, exit(-1)"),
			brBad("bare", "-0 tunnel show", nil, tool, "AF_PACKET: Unsupported protocol family"),
			brBad("bare", "tunnel add", nil, ret, "a write with no mode: cannot determine tunnel mode, before any ioctl"),
			brBad("bare", "tunnel add mode gre ttl 64 nopmtudisc", nil, ret, "ttl and nopmtudisc refused before any ioctl"),
			brBad("bare", "tunnel prl", nil, tool, "prl with no device: Must specify device"),
			brBad("bare", "tunnel 6rd", nil, tool, "6rd with no device: Must specify device"),
			brBad("tnl6", "-6 tunnel show nosuch", tnl6Fix, ret, "the v6 named path: ENODEV, rc 1"),
			brBad("bare", "-6 tunnel show mode gre", nil, tool, "gre is not a v6 mode: Unknown tunnel mode"),
			brBad("bare", "-6 tunnel show ttl 300", nil, tool, "invarg(\"invalid TTL\"): one newline (T11)"),
			brBad("bare", "-6 tunnel show flowlabel 0x100000", nil, tool, "a 21-bit flowlabel: invalid Flowlabel"),
			brBad("bare", "-6 tunnel show dscp 5", nil, tool, "dscp takes only inherit"),
			brBad("bare", "-6 tunnel bogus", nil, tool, "an unknown v6 tunnel command"),
			brBad("bare", "-6 tunnel help", nil, tool, "the v6 usage, exit(-1)"),
		},
	}
	sp.Build = func(vals []value) row {
		var optArg, cmdArg string
		var pre []string
		for i, val := range vals {
			pre = append(pre, val.Pre...)
			switch i {
			case 0:
				optArg = val.Arg
			case 1:
				cmdArg = val.Arg
			}
		}
		args := joinArgs(optArg, cmdArg)
		r := row{
			Cmd:      strings.TrimSpace("ip " + args),
			Proto:    "route",
			Modules:  tunnelModules,
			Readback: []readback{tnlsysReadback(args)},
			Expect:   expect{RC: intp(0), RefusedBy: refNone},
			Axes:     mergeFacts(vals),
		}
		for _, p := range pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
		return r
	}
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}

// tnlsysReadback is the sidecar readback: tnlsnap runs `ip ARGS` again
// under ptrace, from the row's directory (so b.txt and `< b.txt` work),
// and records what it asks outside netlink.
func tnlsysReadback(args string) readback {
	return readback{Name: "tnlsys", Cmd: "tnlsnap -- " + args}
}
