package main

import (
	"slices"
	"strings"
)

// The ss space (W5c): `ss` over the inet socket tables, which it reads with
// SOCK_DIAG_BY_FAMILY on NETLINK_SOCK_DIAG. iproute2's side is misc/ss.c:
// inet_show_netlink sends one dump per address family (AF_INET, then
// AF_INET6, unless -4 or -6 picks one) and sockdiag_send builds each
// request: struct inet_diag_req_v2 with the protocol, the state mask and
// the extension bits, then the filter compiled to bytecode by
// ssfilter_bytecompile (INET_DIAG_REQ_BYTECODE), then the protocol again in
// INET_DIAG_REQ_PROTOCOL when it is over 255 (MPTCP). The kernel's side is
// net/ipv4/inet_diag.c: inet_diag_bc_audit refuses bad bytecode and
// inet_diag_bc_run applies it to each socket.
//
// Every row's namespace holds the same sockets (ssHolder), so the corpus
// test (pkg/xtcpnl/xtcpnl_ws_ss_test.go) knows which sockets each reply
// should list. A value's facts say what the request must carry: protocol,
// families, states (the mask the kernel reads), ext, and pred, the filter
// as the user meant it, in a prefix notation the test evaluates on its own
// (and(a,b), or(a,b), not(a), and atoms such as sport>=1024,
// src=127.0.0.0/8:22, auto, mark=16/255, dev=lo, cgroup=PATH). bc=none marks a
// filter ss applies itself and sends no bytecode for; bug names an
// iproute2 bug the row shows, which the test asserts is still there.

// ssHolder opens the sockets every ss row lists, then forks a child that
// holds them for 10 s with its stdio on /dev/null, so the pre step returns
// at once. With the namespace's port range one port wide, both autobound
// connects take port 45000. pkg/xtcpnl/xtcpnl_ws_ss_test.go wsSsKnown is
// this list; change both together.
const ssHolder = `import os, socket as s, time
L = []
def tcp(fam, addr, port, v6only=1, mark=0):
    k = s.socket(fam, s.SOCK_STREAM)
    k.setsockopt(s.SOL_SOCKET, s.SO_REUSEADDR, 1)
    if fam == s.AF_INET6:
        k.setsockopt(s.IPPROTO_IPV6, s.IPV6_V6ONLY, v6only)
    if mark:
        k.setsockopt(s.SOL_SOCKET, s.SO_MARK, mark)
    k.bind((addr, port)); k.listen(8); L.append(k); return k
def conn(port):
    c = s.socket(s.AF_INET, s.SOCK_STREAM); c.connect(("127.0.0.1", port)); L.append(c)
def udp(fam, addr, port):
    k = s.socket(fam, s.SOCK_DGRAM)
    if fam == s.AF_INET6:
        k.setsockopt(s.IPPROTO_IPV6, s.IPV6_V6ONLY, 1)
    k.bind((addr, port)); L.append(k)
tcp(s.AF_INET, "127.0.0.1", 22)
tcp(s.AF_INET6, "::1", 80)
a = tcp(s.AF_INET, "127.0.0.1", 8080); conn(8080); L.append(a.accept()[0])
b = tcp(s.AF_INET6, "::", 443, 0); conn(443); L.append(b.accept()[0])
tcp(s.AF_INET, "127.0.0.1", 9000, mark=16)
udp(s.AF_INET, "127.0.0.1", 53)
udp(s.AF_INET6, "::1", 5353)
u = s.socket(s.AF_INET, s.SOCK_DGRAM); u.connect(("127.0.0.1", 53)); L.append(u)
L.append(s.socket(s.AF_INET, s.SOCK_RAW, s.IPPROTO_ICMP))
if os.fork() == 0:
    n = os.open(os.devnull, os.O_RDWR)
    for fd in (0, 1, 2):
        os.dup2(n, fd)
    time.sleep(10)
    os._exit(0)
`

const ssSetup = "ip link set lo up && sysctl -qw net.ipv4.ip_local_port_range='45000 45000' && python3 -c '" + ssHolder + "'"

// ssModules are the sock_diag handlers ss's dumps need; inet_diag
// request_module()s them too, but loading them up front keeps the first
// row's capture free of the module load.
var ssModules = []string{"inet_diag", "tcp_diag", "udp_diag", "raw_diag", "mptcp_diag", "sctp_diag"}

// The state masks (ss.c's SS_* bits: SS_ESTABLISHED 1 ... SS_CLOSING 11,
// SS_NEW_SYN_RECV 12, SS_BOUND_INACTIVE 13). SS_CONN is ss's default:
// every state but LISTEN, CLOSE, TIME_WAIT and SYN_RECV.
const (
	ssAll    = "16383" // SS_ALL, (1 << SS_MAX) - 1
	ssConn   = "15159" // SS_CONN
	ssListen = "1152"  // -l: 1<<SS_LISTEN | 1<<SS_CLOSE
)

// ssExpParse is a filter or state ss refuses before sending anything.
// rc is set per value once the workbench has shown it.
func ssExpParse(rc int) expect {
	return expect{RC: intp(rc), RefusedBy: "iproute2"}
}

func ssFilter(name, arg, pred string, kv ...string) value {
	return v(name, arg, f(append([]string{"pred", pred}, kv...)...))
}

func ssFilterC(name, arg, pred string, kv ...string) value {
	val := ssFilter(name, arg, pred, kv...)
	val.Corner = true
	return val
}

func ssFilterB(name, arg, pred string, kv ...string) value {
	val := ssFilter(name, arg, pred, kv...)
	val.Boundary = true
	return val
}

func ssSpace() *space {
	sp := &space{
		Name: "ss",
		Axes: []axis{
			// proto: the table; the request's protocol is the kernel's
			// IPPROTO_*, and MPTCP's 262 also rides in REQ_PROTOCOL.
			{Name: "proto", Values: []value{
				v("tcp", "-t", f("proto", "6")),
				v("udp", "-u", f("proto", "17")),
				v("raw", "-w", f("proto", "255")),
				cv("mptcp", "-M", f("proto", "262")),
				cv("sctp", "-S", f("proto", "132")),
			}},
			{Name: "family", Values: []value{
				v("both", "", f("families", "2,10")),
				v("4", "-4", f("families", "2")),
				v("6", "-6", f("families", "10")),
			}},
			// ext: idiag_ext's bits, 1 << (INET_DIAG_* - 1).
			{Name: "ext", Values: []value{
				v("none", "", f("ext", "0")),
				v("mem", "-m", f("ext", "65")),  // MEMINFO, SKMEMINFO
				v("info", "-i", f("ext", "14")), // INFO, VEGASINFO, CONG
				cv("tos", "--tos", f("ext", "48")),
				cv("meminfo", "-mi", f("ext", "79")),
			}},
			{Name: "state", Values: []value{
				v("default", "", f("states", ssConn)),
				v("all", "-a", f("states", ssAll)),
				v("listen", "-l", f("states", ssListen)),
				v("established", "state established", f("states", "2")),
				cv("excl", "exclude time-wait", f("states", "16319")),
				cv("connected", "state connected", f("states", "15231")),
				cv("two", "state listening state unconnected", f("states", ssListen)),
				bv("bound", "-B", f("states", "8192")),
			}},
			{Name: "filter", Values: []value{
				ssFilter("none", "", "", "bc", "none"),
				ssFilter("sport", "sport = :22", "sport=22"),
				ssFilter("dportne", "dport ne :45000", "dport!=45000"),
				ssFilterC("dportnesym", "dport != :45000", "dport!=45000"),
				ssFilter("sportge", "sport ge :1024", "sport>=1024"),
				ssFilter("sportle", "sport le :1024", "sport<=1024"),
				ssFilter("dportgt", "dport gt :8080", "dport>8080"),
				ssFilter("dportlt", "dport lt :1024", "dport<1024"),
				ssFilterC("sportgtsym", `sport \> :8080`, "sport>8080"),
				ssFilterB("dportzero", "dport = :0", "dport=0"),
				ssFilterB("sportmax", "sport = :65535", "sport=65535"),
				ssFilterB("sportgemax", "sport ge :65535", "sport>=65535"),
				ssFilter("src4", "src 127.0.0.1", "src=127.0.0.1/32"),
				ssFilter("dst4net", "dst 127.0.0.0/8", "dst=127.0.0.0/8"),
				ssFilterB("src4any", "src 0.0.0.0/0", "src=0.0.0.0/0"),
				ssFilter("src6", "src [::1]", "src=::1/128"),
				// parse_hostcond takes a bracketed address's prefix inside the
				// brackets; after them it is read as the port (see v6slash).
				ssFilter("dst6net", "dst [2001:db8::/32]", "dst=2001:db8::/32"),
				ssFilterB("src6any", "src [::/0]", "src=::/0"),
				ssFilter("srcport4", "src 127.0.0.1:22", "src=127.0.0.1/32:22"),
				ssFilterC("srcport6", "src [::1]:80", "src=::1/128:80"),
				ssFilter("or", "sport = :22 or sport = :80", "or(sport=22,sport=80)"),
				ssFilter("and", "sport = :8080 and dport = :45000", "and(sport=8080,dport=45000)"),
				ssFilter("not", "not sport = :22", "not(sport=22)"),
				ssFilterC("nested", `\( sport = :22 or sport = :443 \) and not dst 127.0.0.1`,
					"and(or(sport=22,sport=443),not(dst=127.0.0.1/32))"),
				ssFilterC("implicitand", "sport = :8080 dport = :45000", "and(sport=8080,dport=45000)"),
				ssFilterC("orsym", `sport = :22 \|\| dport = :443`, "or(sport=22,dport=443)"),
				ssFilter("auto", "autobound", "auto"),
				ssFilterC("notauto", "not autobound", "not(auto)"),
				ssFilter("fwmark", "fwmark = 0x10", "mark=16/4294967295"),
				ssFilterC("fwmarkmask", "fwmark = 0x10/0xf0", "mark=16/240"),
				// get_cgroup2_id tries the path as given first (lib/fs.c:145),
				// so "/" names the root filesystem, not the cgroup2 root.
				// A cgroup filter makes ss probe for kernel support first
				// (ssfilter_check.c cgroup_filter_check): one more request.
				// The probe's result is cached in a copy of the table entry
				// (ssfilter_is_supported), so each cgroup term probes again.
				ssFilterC("cgroup", "cgroup /sys/fs/cgroup", "cgroup=/sys/fs/cgroup", "probes", "1"),
				ssFilterC("cgroup2", "cgroup /sys/fs/cgroup or cgroup /sys/fs/cgroup",
					"or(cgroup=/sys/fs/cgroup,cgroup=/sys/fs/cgroup)", "probes", "2"),
				ssFilter("dev", "dev lo", "dev=lo", "bc", "none"),
				ssFilterC("devand", "dev lo and sport = :22", "and(dev=lo,sport=22)", "bc", "none"),
				// ssfilter_bytecompile builds every address of a name from
				// the first one (ss.c:1945), so ::1 is never tested.
				ssFilterC("localhost", "src localhost", "src=localhost", "bug", "ss-scond-first-addr"),
				ssFilterC("port65536", "sport = :65536", "sport=65536"),
				// A port comparison's operand is a u16 (ss.c:2062), so 65536
				// wraps to 0 and the filter passes every socket.
				ssFilterC("ge65536", "sport ge :65536", "sport>=65536", "bug", "ss-port-u16-wrap"),
				// get_long takes -1, which a hostcond reads as any port.
				ssFilterC("portneg", "sport = :-1", "sport=-1", "bug", "ss-port-minus-one-any"),
			}},
		},
		Bad: []badValue{
			{Axis: "filter", Value: v("noport", "sport = :nosuchport", nil), Expect: ssExpParse(1),
				Why: "a port name getservbyname lacks (does not look like a port)"},
			{Axis: "filter", Value: v("badprefix", "src 127.0.0.1/33", nil), Expect: ssExpParse(1),
				Why: "a prefix longer than 32 (an inet prefix is expected)"},
			{Axis: "filter", Value: v("v6slash", "dst [2001:db8::]/32", nil), Expect: ssExpParse(1),
				Why: "a bracketed IPv6 address with its prefix after the bracket, which parse_hostcond reads as the port"},
			{Axis: "filter", Value: v("dangling", "sport = :22 or", nil), Expect: ssExpParse(255),
				Why: "a filter that ends on an operator (bison bellows)"},
			{Axis: "filter", Value: v("nodev", "dev nosuch0", nil), Expect: ssExpParse(1),
				Why: "a device that does not exist (Cannot parse device)"},
			{Axis: "filter", Value: v("badmark", "fwmark = bogus", nil), Expect: ssExpParse(1),
				Why: "a mark that is no number"},
			{Axis: "filter", Value: v("nocgroup", "cgroup /nosuch", nil), Expect: ssExpParse(1),
				Why: "a cgroup path that does not exist"},
			{Axis: "state", Value: v("nosuch", "state nosuch", nil), Expect: ssExpParse(255),
				Why: "a state name scan_state lacks (wrong state name)"},
			{Axis: "state", Value: v("newsynrecv", "state new-syn-recv", nil), Expect: ssExpParse(255),
				Why: "the kernel-only NEW_SYN_RECV, which scan_state refuses"},
		},
	}
	sp.Excl = ssFamilyExcl()
	sp.Build = buildSs
	return sp
}

// ssFamilyExcl: -4 and -6 set the family get_prefix_1 parses addresses in
// (parse_hostcond), so an address of the other family is refused.
func ssFamilyExcl() []exclusion {
	var out []exclusion
	for fam, filters := range map[string][]string{
		"4": {"src6", "dst6net", "src6any", "srcport6"},
		"6": {"src4", "dst4net", "src4any", "srcport4", "nested"},
	} {
		for _, flt := range filters {
			exp := ssExpParse(1)
			out = append(out, exclusion{A: "family=" + fam, B: "filter=" + flt, Expect: &exp,
				Why: "an address of the other family than -" + fam + " (an inet prefix is expected)"})
		}
	}
	slices.SortFunc(out, func(a, b exclusion) int { return strings.Compare(a.A+a.B, b.A+b.B) })
	return out
}

// buildSs makes the row: ss -n, the options, then the state words and the
// filter, which ss reads from its remaining arguments.
//
// Two values need more than their spelling. ip netns exec mounts a fresh
// sysfs, without the cgroup2 mount get_cgroup2_id looks in, so a cgroup
// filter mounts it first, in the exec's private mount namespace. And -4 or
// -6 resolves a name in that family only, so src localhost is one address
// there and the multi-address bug cannot show.
func buildSs(vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals {
		args = append(args, val.Arg)
	}
	cmd := "ss -n " + joinArgs(args...)
	if strings.Contains(cmd, " cgroup ") {
		cmd = "mount -t cgroup2 cgroup2 /sys/fs/cgroup && " + cmd
	}
	axes := mergeFacts(vals)
	// With no state option, the mask is the tables' defaults ORed
	// together (filter_db_set): SS_CONN for tcp, mptcp and sctp, only
	// SS_ESTABLISHED for udp and raw (ss.c:276, 280). -4 and -6 OR in
	// their family's SS_CONN too (filter_af_set, ss.c:436).
	if axes["states"] == ssConn && axes["families"] == "2,10" && (axes["proto"] == "17" || axes["proto"] == "255") {
		axes["states"] = "2"
	}
	if axes["pred"] == "src=localhost" && axes["families"] != "2,10" {
		delete(axes, "bug")
		axes["pred"] = map[string]string{"2": "src=127.0.0.1/32", "10": "src=::1/128"}[axes["families"]]
	}
	return row{
		Cmd:     cmd,
		Proto:   "sock_diag",
		Pre:     []preStep{{Cmd: ssSetup}},
		Modules: ssModules,
		Expect:  expect{RC: intp(0), RefusedBy: "none"},
		Axes:    axes,
	}
}
