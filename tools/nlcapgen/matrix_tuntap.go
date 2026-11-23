package main

import "strings"

// The tuntap read space (goip increment 8, 8C): `ip tuntap show`
// (ip/iptuntap.c do_show, print_tuntap, show_processes), plain and -j.
//
// ip dumps the links of kind "tun", then reads each one's
// /sys/class/net/DEV/{tun_flags,owner,group}, and under -d walks /proc for
// the processes holding /dev/net/tun on it. The `tnlsys` readback (tnlsnap)
// records the files and the walk for goip's replay.
//
// The rows pin the leads: show ignores every argument (T13); -d prints the
// processes run together, "name(pid)name(pid)" (T15); and a hard "\n" after
// -o's _SL_ "Attached to processes:" (T16).
//
// Determinism: fixed names. The holder processes' pids differ per run, so
// the -d rows differ between batches in their pids alone (the allow-list in
// V002). /proc is not per namespace, so every -d row is serial and first
// kills the holders earlier rows left (tunHolderKill).

var tuntapModules = []string{"tun"}

// tunFix is tap0 (tap, user 1000, group 100, one_queue), tun1 (tun,
// multi_queue, vnet_hdr) and tap2 (tap, pi), all persistent, beside a
// dummy d0 the kind filter skips.
var tunFix = []string{
	"ip link add d0 address 02:00:00:00:01:01 type dummy && ip link set d0 addrgenmode none",
	"ip tuntap add tap0 mode tap user 1000 group 100 one_queue",
	"ip tuntap add tun1 mode tun multi_queue vnet_hdr",
	"ip tuntap add tap2 mode tap pi",
}

// tunHolderScript is a pre step writing /run/tnlholder.py: attach to
// argv[1] with the TUNSETIFF flags argv[2] (the device's own, so the attach
// changes none of them) and hold it for 120 s. The name is what
// tunHolderKill finds the holders by.
const tunHolderScript = "printf '%s\\n' 'import fcntl,os,struct,sys,time'" +
	" 'f=os.open(\"/dev/net/tun\",os.O_RDWR)'" +
	" 'fcntl.ioctl(f,0x400454ca,struct.pack(\"16sH\",sys.argv[1].encode(),int(sys.argv[2],0)))'" +
	" 'time.sleep(120)' > /run/tnlholder.py"

// tunHolderKill is the first pre step of every -d row: show_processes
// walks all of /proc, not the row's namespace (T22), so an earlier row's
// holder would be listed too. The brackets keep the pattern from matching
// the shell that runs it.
const tunHolderKill = "pkill -f '[t]nlholde[r]'; true"

// tunHolder is a pre step leaving a holder on dev, in the row's namespace.
func tunHolder(dev, flags string) string {
	return "setsid python3 /run/tnlholder.py " + dev + " " + flags + " </dev/null >/dev/null 2>&1 & sleep 1"
}

// tunProcFix is tunFix with one holder on tap0 and two on the multi_queue
// tun1.
var tunProcFix = append(clonePre(tunFix),
	tunHolderScript,
	tunHolder("tap0", "0x3002"),
	tunHolder("tun1", "0x5101"),
	tunHolder("tun1", "0x5101"),
)

// tunXnsFix is tunFix with no holder of its own, and a holder on a tap0 in
// another network namespace, which show_processes matches by name (T22).
var tunXnsFix = append(clonePre(tunFix),
	tunHolderScript,
	"setsid unshare -n sh -c 'ip tuntap add tap0 mode tap && exec python3 /run/tnlholder.py tap0 0x1002'"+
		" </dev/null >/dev/null 2>&1 & sleep 1",
)

// tuntapSpace: ip tuntap show, plain and -j.
func tuntapSpace() *space {
	tool := expect{RC: intp(255), RefusedBy: refIproute2}
	ret := expect{RC: intp(1), RefusedBy: refIproute2} // a returned -1: do_cmd negates it
	sp := &space{
		Name: "tuntap",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				bc(brBoundary, "bare", "tuntap show", nil),
				bc(brPos, "tun", "tuntap show", tunFix),
				bc(brCorner, "tun", "tuntap", tunFix), // no verb: show
				bc(brPos, "tun", "tuntap list", tunFix),
				bc(brPos, "tun", "tuntap lst", tunFix),
				bc(brCorner, "tun", "tuntap sh", tunFix),
				bc(brPos, "tun", "tap show", tunFix), // ip's alias object
				bc(brCorner, "tun", "tap", tunFix),
				bc(brPos, "tun", "-d tuntap show", tunFix), // no holders: an empty list
				bc(brCorner, "tun", "-o tuntap show", tunFix),
				bc(brCorner, "tun", "-s tuntap show", tunFix),
				bc(brCorner, "tun", "-6 tuntap show", tunFix),          // the dump is AF_UNSPEC whatever -6 says
				bc(brCorner, "tun", "tuntap show dev tap0", tunFix),    // arguments ignored (T13)
				bc(brCorner, "tun", "tuntap show bogus words", tunFix), // (T13)
				bc(brPos, "proc", "-d tuntap show", tunProcFix),        // name(pid)name(pid) (T15)
				bc(brCorner, "proc", "-o -d tuntap show", tunProcFix),  // half one-line (T16)
				bc(brCorner, "proc", "-d tuntap", tunProcFix),
				bc(brCorner, "xns", "-d tuntap show", tunXnsFix), // another namespace's tap0 holder (T22)
				bb(brCorner, "tun", "twice", "", false, batchFile("tuntap show", "tuntap show dev tap0"), tunFix),
				bb(brCorner, "tun", "stdin", "", true, batchFile("tap show"), tunFix),
			}},
		},
		Bad: []badValue{
			brBad("bare", "tuntap bogus", nil, tool, "an unknown tuntap command"),
			brBad("bare", "tuntap help", nil, tool, "the usage, exit(-1)"),
			brBad("bare", "tuntap add", nil, ret, "a write with no mode: refused before any ioctl"),
			brBad("bare", "tuntap add mode bogus", nil, tool, "Unknown tunnel mode, before any ioctl"),
			brBad("bare", "tuntap add mode tap user nosuchuser", nil, tool, "invalid user, before any ioctl"),
			brBad("bare", "tuntap del", nil, ret, "a delete with no mode: refused before any ioctl"),
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
			Modules:  tuntapModules,
			Readback: []readback{tnlsysReadback(args)},
			Expect:   expect{RC: intp(0), RefusedBy: refNone},
			Axes:     mergeFacts(vals),
		}
		// A -d row walks /proc, which every namespace shares: it runs alone,
		// after the holders earlier rows left are gone.
		if detailWords(args) {
			r.Serial = true
			pre = append([]string{tunHolderKill}, pre...)
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

// detailWords is whether ip's options in args include -d.
func detailWords(args string) bool {
	for _, w := range strings.Fields(args) {
		if !strings.HasPrefix(w, "-") {
			return false
		}
		if w == "-d" {
			return true
		}
	}
	return false
}
