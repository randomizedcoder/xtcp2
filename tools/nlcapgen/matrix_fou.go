package main

// The fou space (W5-fou): `ip fou`'s read-only show over generic netlink's
// "fou" family. fou's side is ip/ipfou.c (iproute2): do_ipfou resolves the
// family with genl_init_handle (one acknowledged CTRL_CMD_GETFAMILY) BEFORE
// it dispatches add/del/show, so even a bad sub-object is an ack-then-fail,
// never a pre-wire refusal — except `ip fou help`, which usage() refuses
// before the family is resolved, with no netlink at all. `ip fou show`
// sends one FOU_CMD_GET with NLM_F_DUMP and no selector, so the kernel
// dumps every fou in the namespace; -j prints the reply as JSON, holding
// the printer to the same attributes.
//
// The kernel's side is net/ipv4/fou_core.c + fou_nl.c. The fou genl family
// is netnsok=true and per-netns (fou_net_id), so unlike vdpa it answers in
// the row's own fresh namespace: each row's `pre` adds its own fou ports
// with `ip fou add`, and the dump sees only those. No init-namespace mode,
// and no serial execution, is needed — the namespaces are isolated.
//
// fou_fill_info always emits AF, PORT (be16), PEER_PORT (be16), IPPROTO,
// TYPE, IFINDEX and the family's local/peer address pair (zero when
// unset); ip only prints local/peer/peer_port/dev when non-zero. The
// address and port widths are network order on the wire.
//
// This space is read-only: the `ip fou add` setup runs in `pre`, outside
// the capture window; no add/del is captured. The clone (internal/gofou)
// replays these captures and must reproduce `ip fou show`'s stdout byte
// for byte.

var fouModules = []string{"fou"}

// fouAdd spells one `ip fou add`. Ports are fixed so two fresh boots print
// the same bytes; the dump order follows fou_list insertion, which is the
// pre steps' order.
func fouAdd(cmd string) string { return "ip fou add " + cmd }

// fix is a fixture value: its name, the `ip fou` words after the options
// (always "show" for a positive), and the `ip fou add` setup it needs.
// Each fixture populates its namespace's fou list differently, so the same
// `show` command captures a different dump.
func fix(name string, pre ...string) value {
	val := v(name, "show", f("words", "show", "fix", name))
	val.Pre = pre
	return val
}

// fouSpace: `ip fou show`, plain and as JSON, over five namespace fixtures.
func fouSpace() *space {
	sp := &space{
		Name: "fou",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "fix", Values: []value{
				// An empty fou list: the dump returns only
				// NLMSG_DONE. A boundary the parser and clone must
				// both handle.
				fix("empty"),
				// One direct (ipproto) port: ipproto 4 is IPIP.
				// ip prints "port N ipproto 4".
				fix("direct", fouAdd("port 5555 ipproto 4")),
				// One GUE port: ip prints "port N gue". FOU_ATTR_TYPE
				// is GUE, so ipproto is suppressed in the output.
				fix("gue", fouAdd("port 5556 gue")),
				// Several v4 ports: a direct (ipproto 4, IPIP), a GUE
				// and another direct (ipproto 47, GRE). Exercises the
				// multi-message dump (NLM_F_MULTI) and ipproto variety.
				// No local/peer: the kernel connect()s a peer, which
				// needs a route, so peers belong in the 3P unit tests
				// (every reply already carries zero LOCAL/PEER/PEER_PORT
				// attributes, which ip suppresses when zero).
				fix("multi",
					fouAdd("port 5555 ipproto 4"),
					fouAdd("port 5556 gue"),
					fouAdd("port 5557 ipproto 47")),
				// One IPv6 GUE port: exercises FOU_ATTR_AF==AF_INET6,
				// the zero LOCAL_V6/PEER_V6 pair, and ip's " -6" marker
				// in text mode. The address binds to the :: wildcard,
				// so no route is needed.
				fix("v6", "ip -6 fou add port 5558 gue"),
			}},
		},
		Bad: []badValue{
			// `ip fou help`: do_ipfou matches "help" and calls usage()
			// BEFORE genl_init_handle, so it exits 255 (usage's
			// exit(-1)) with no netlink at all — an iproute2 refusal
			// before the wire.
			{Axis: "fix", Value: v("help", "help", nil),
				Expect: expect{RC: intp(255), RefusedBy: refIproute2},
				Why:    "help is refused before the family is resolved"},
			// `ip fou show junk`: genl_init_handle resolves the family
			// (one acknowledged CTRL_CMD_GETFAMILY), then do_show sees a
			// leftover argument and returns -1 ("does not take any
			// arguments"); do_cmd negates it, so rc is 1. ack-then-fail,
			// no fou-family request.
			{Axis: "fix", Value: v("showarg", "show junk", nil),
				Expect: expect{RC: intp(1), RefusedBy: refAckThenFail},
				Why:    "show takes no arguments, refused after the family is resolved"},
			// `ip fou badobj`: the family is resolved, then do_ipfou's
			// final branch prints "Command ... is unknown" and exits
			// 255 (exit(-1)). ack-then-fail.
			{Axis: "fix", Value: v("badobj", "badobj", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "an unknown sub-command, refused after the family is resolved"},
		},
	}
	sp.Build = func(vals []value) row {
		var optArg, fixArg string
		var pre []string
		for i, val := range vals {
			pre = append(pre, val.Pre...)
			switch i {
			case 0:
				optArg = val.Arg
			case 1:
				fixArg = val.Arg
			}
		}
		r := row{
			Cmd:     "ip " + joinArgs(optArg, "fou", fixArg),
			Proto:   "generic",
			Modules: fouModules,
			Expect:  expect{RC: intp(0), RefusedBy: refNone},
			Axes:    mergeFacts(vals),
		}
		for _, p := range pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
		return r
	}
	// The bad values change the fix axis, so a base with opt=plain keeps
	// the bad word at the end of the line.
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
