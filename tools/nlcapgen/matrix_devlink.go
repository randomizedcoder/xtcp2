package main

// The devlink space (W5-devlink): devlink's read-only show/dump commands
// over generic netlink's "devlink" family. devlink's side is
// devlink/devlink.c (iproute2): each show resolves the family with
// CTRL_CMD_GETFAMILY, then sends one DEVLINK_CMD_*_GET with NLM_F_DUMP and
// no device selector, so the kernel dumps every instance in the namespace;
// -j prints the reply as JSON, which holds the printer to the same
// attributes.
//
// The kernel's side is net/devlink/ plus the driver. Every row's namespace
// holds one netdevsim device (devlinkSetup), whose dev.c registers seven
// resources, a region, params, two health reporters, and a trap set with
// its groups (drivers/net/netdevsim/dev.c). netdevsim registers no shared
// buffer, so `devlink sb show` dumps nothing: an empty dump, which is a
// boundary the parser and the clone must both handle.
//
// This space is read-only: no write, reload, monitor or eswitch. The
// clone (internal/godevlink) replays these captures and must reproduce
// devlink's stdout byte for byte.

// devlinkSetup creates the row's netdevsim device in its namespace with a
// FIXED bus id 0, so the handle devlink prints ("netdevsim/netdevsim0") is
// the same on every boot. A slot-derived id would make the handle vary with
// which slot ran the row, which devlink echoes in its output; a fixed id is
// safe only because devlink rows run serially (row.Serial), so two rows
// never write the netdevsim bus at once. The id is deleted first in case a
// prior device lingers. "0 1 1" is one port with one queue, all a read-only
// dump needs.
//
// `devlink dev show` also prints an `index`, a counter netdevsim bumps on
// every device creation. Serial execution makes it deterministic too: the
// module loads fresh at the batch's start, the rows create devices in a
// fixed order, so a given row always sees the same count. Two fresh boots
// capture byte-identical output.
const devlinkSetup = `echo 0 > /sys/bus/netdevsim/del_device 2>/dev/null;` +
	` echo "0 1 1" > /sys/bus/netdevsim/new_device && ip link show eth0 >/dev/null`

var devlinkModules = []string{"netdevsim"}

// dlShow is a show value: its name, the devlink words after the options,
// and the DEVLINK_CMD_* the dump sends (uapi/linux/devlink.h). cmd is the
// UPSTREAM command number: the capture kernel renumbers the enum (commands
// inserted mid-enum shift later ones, so `resource show` is 36 on the
// capture kernel, 40 upstream), so the corpus test (xtcpnl_ws_devlink_test.go)
// does NOT match the request against this number — it ties each row to its
// operation by the section ParseDevlink decodes the reply into (keyed off
// `words`), which the stable, append-only DEVLINK_ATTR_* tree builds. cmd
// stays here as the documented upstream reference.
func dlShow(name, words, cmd string) value {
	return v(name, words, f("cmd", cmd, "words", words))
}

// devlinkSpace: devlink's read-only dumps on a netdevsim device, each
// plain and as JSON.
func devlinkSpace() *space {
	sp := &space{
		Name: "devlink",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// DEVLINK_CMD_GET 1, PORT_GET 5, SB_GET 11,
				// RESOURCE_DUMP 40, PARAM_GET 44, REGION_GET 48,
				// INFO_GET 57, HEALTH_REPORTER_GET 58, TRAP_GET 67,
				// TRAP_GROUP_GET 71.
				dlShow("dev", "dev show", "1"),
				dlShow("port", "port show", "5"),
				dlShow("sb", "sb show", "11"),
				dlShow("resource", "resource show", "40"),
				dlShow("param", "dev param show", "44"),
				dlShow("region", "region show", "48"),
				dlShow("info", "dev info", "57"),
				dlShow("health", "health show", "58"),
				dlShow("trap", "trap show", "67"),
				dlShow("trapgroup", "trap group show", "71"),
			}},
		},
		Bad: []badValue{
			// An object devlink does not know: devlink resolves the
			// family first (one acknowledged CTRL_CMD_GETFAMILY), then its
			// command table rejects the object and it exits 1 without a
			// devlink-family request — ack-then-fail, not an iproute2
			// refusal before the wire.
			{Axis: "cmd", Value: v("nosuch", "nosuch show", nil),
				Expect: expect{RC: intp(1), RefusedBy: refAckThenFail},
				Why:    "an object devlink's command table lacks"},
			// A device handle that matches nothing: the kernel answers
			// the targeted (non-dump) GET with ENODEV.
			{Axis: "cmd", Value: v("nodev", "dev show netdevsim/netdevsim999", f("cmd", "1")),
				Expect: expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				Why:    "a device handle no instance has"},
		},
	}
	sp.Build = func(vals []value) row {
		args := make([]string, 0, len(vals))
		for _, val := range vals {
			if val.Arg != "" {
				args = append(args, val.Arg)
			}
		}
		axes := mergeFacts(vals)
		return row{
			Cmd:     "devlink " + joinArgs(args...),
			Proto:   "generic",
			Pre:     []preStep{{Cmd: devlinkSetup}},
			Modules: devlinkModules,
			Serial:  true,
			Expect:  expect{RC: intp(0), RefusedBy: refNone},
			Axes:    axes,
		}
	}
	// The bad values change the cmd axis, so a base with opt=plain keeps
	// the bad word at the end of the line.
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
