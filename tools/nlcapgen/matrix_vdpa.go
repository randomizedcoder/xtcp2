package main

// The vdpa space (W5-vdpa): vdpa's read-only show/dump commands over
// generic netlink's "vdpa" family. vdpa's side is vdpa/vdpa.c (iproute2):
// main() opens the genl socket (resolving the family with one acknowledged
// CTRL_CMD_GETFAMILY) BEFORE it dispatches the object/command, so even a
// bad object is an ack-then-fail, never a pre-wire iproute2 refusal. Each
// show then sends one VDPA_CMD_*_GET; mgmtdev/dev/config dumps add
// NLM_F_DUMP (no selector, so the kernel dumps every instance), while
// `dev vstats show` is a targeted GET that needs a device handle. -j
// prints the reply as JSON, holding the printer to the same attributes.
//
// The kernel's side is drivers/vdpa/ plus the simulator. Every row loads
// vdpa_sim_net (drivers/vdpa/vdpa_sim/vdpa_sim_net.c), which registers one
// software management device "vdpasim_net" at module_init with nothing to
// simulate; the pre step adds one vdpa device off it. Every row also loads
// virtio_vdpa, which binds each new vdpa device to virtio_net and so
// completes feature negotiation: without a bound driver the kernel answers
// DEV_VSTATS_GET with EAGAIN ("feature negotiation not complete") and
// `dev config show` has no negotiated_features to print.
//
// The bound driver makes a netdev, eth0 in init_net, that the guest's
// networkd would manage, bringing it up and sending DHCP/RS through the
// simulator's loopback: the queue counters then depend on timing. The pre
// step drops a runtime .network file marking the device's fixed MAC
// Unmanaged, so the link stays down and every counter stays 0.
//
// This space is read-only: no dev add/del/set in the capture window (the
// add/del are pre-step setup, outside it). The clone (internal/govdpa)
// replays these captures and must reproduce vdpa's stdout byte for byte.

// vdpa rows run in init_net (row.InitNS). The kernel registers the vdpa
// genl family with netnsok false (drivers/vdpa/vdpa.c, vdpa_nl_family), so
// from the per-row namespace every other space uses, `vdpa` cannot reach
// the family at all ("Failed to connect to vdpa Netlink"). The capture
// still holds only the row's transaction: the helper filters it to
// NETLINK_GENERIC, and the quiet guest sends no generic netlink of its own.
//
// vdpaSetup provisions the row's vdpa device off the
// global vdpasim_net mgmtdev. The name "vdpa0", the MAC and the MTU are
// all FIXED so `dev show`/`dev config show` print the same bytes on every
// boot: vdpa_sim_net would otherwise leave the MAC at its driver default,
// which is not a value this capture should depend on. A fixed name is safe
// only because vdpa rows run serially (row.Serial) — the vdpasim_net
// mgmtdev is a kernel-wide object, so two rows must never add/del at once.
// The device is deleted first in case a prior row's teardown lingered; its
// failure on a fresh namespace is swallowed so the combined step's rc is
// the add's.
//
// Unlike devlink's index counter, vdpa prints no creation-order field
// (VDPA_ATTR_DEV_ID is the virtio class id, VIRTIO_ID_NET, not a counter),
// so serial execution alone makes two fresh boots byte-identical.
const vdpaSetup = `mkdir -p /run/systemd/network &&` +
	` printf '[Match]\nMACAddress=00:11:22:33:44:55\n\n[Link]\nUnmanaged=yes\n'` +
	` >/run/systemd/network/00-nlcap-vdpa.network && networkctl reload;` +
	` vdpa dev del vdpa0 2>/dev/null;` +
	` vdpa dev add name vdpa0 mgmtdev vdpasim_net mac 00:11:22:33:44:55 mtu 1500`

var vdpaModules = []string{"vdpa_sim_net", "virtio_vdpa"}

// vdShow is a show value: its name, the vdpa words after the options, and
// the VDPA_CMD_* the request carries (uapi/linux/vdpa.h). Unlike devlink,
// vdpa's command enum is short and append-only, so the capture kernel's
// numbers match upstream (MGMTDEV_GET 2, DEV_GET 5, DEV_CONFIG_GET 6,
// DEV_VSTATS_GET 7); even so, the corpus test (xtcpnl_ws_vdpa_test.go)
// ties each row to its operation by the section ParseVdpa decodes the
// reply into (keyed off `words`), not by this number, matching how the
// devlink space already works. cmd stays as the documented reference.
func vdShow(name, words, cmd string) value {
	return v(name, words, f("cmd", cmd, "words", words))
}

// vdpaSpace: vdpa's read-only dumps/gets on one vdpasim_net device, each
// plain and as JSON.
func vdpaSpace() *space {
	sp := &space{
		Name: "vdpa",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// VDPA_CMD_MGMTDEV_GET 2, DEV_GET 5,
				// DEV_CONFIG_GET 6, DEV_VSTATS_GET 7.
				vdShow("mgmtdev", "mgmtdev show", "2"),
				vdShow("dev", "dev show", "5"),
				// A device handle makes DEV_GET targeted, not a dump.
				vdShow("devname", "dev show vdpa0", "5"),
				vdShow("config", "dev config show", "6"),
				// vstats is a targeted GET: it needs the device
				// handle and a queue index. With one queue pair and
				// CTRL_VQ negotiated, qidx 0 is rx, 1 tx and 2 the
				// control queue (vdpa_sim_net's vdpasim_net_get_stats
				// returns a different counter set for each).
				vdShow("vstats-rx", "dev vstats show vdpa0 qidx 0", "7"),
				vdShow("vstats-tx", "dev vstats show vdpa0 qidx 1", "7"),
				vdShow("vstats-cvq", "dev vstats show vdpa0 qidx 2", "7"),
			}},
		},
		Bad: []badValue{
			// An object vdpa does not know: vdpa_init resolves the
			// family first (one acknowledged CTRL_CMD_GETFAMILY),
			// then vdpa_cmd rejects the object and it exits 1 with no
			// vdpa-family request — ack-then-fail, not an iproute2
			// refusal before the wire (vdpa always opens the socket).
			{Axis: "cmd", Value: v("nosuch", "nosuch show", nil),
				Expect: expect{RC: intp(1), RefusedBy: refAckThenFail},
				Why:    "an object vdpa's command table lacks"},
			// A device handle that matches nothing: the kernel answers
			// the targeted (non-dump) DEV_GET with ENODEV.
			{Axis: "cmd", Value: v("nodev", "dev show vdpanosuch", f("cmd", "5")),
				Expect: expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				Why:    "a device handle no instance has"},
			// One past the last queue: vdpasim_net_get_stats has no
			// counters for it (its switch's default) and answers EINVAL
			// with no extack.
			{Axis: "cmd", Value: v("qidx3", "dev vstats show vdpa0 qidx 3", f("cmd", "7")),
				Expect: expect{RC: intp(1), Errno: "EINVAL", RefusedBy: refKernel},
				Why:    "the first queue index past the device's queues"},
			// The largest index vdpa_dev_vendor_stats_fill lets through to
			// the driver: it refuses only index > VIRTIO_NET_CTRL_MQ_VQ_PAIRS_MAX
			// (0x8000), so 32768 reaches vdpasim_net_get_stats and is EINVAL.
			{Axis: "cmd", Value: v("qidxlast", "dev vstats show vdpa0 qidx 32768", f("cmd", "7")),
				Expect: expect{RC: intp(1), Errno: "EINVAL", RefusedBy: refKernel},
				Why:    "the last queue index the core's range check passes"},
			// One past it: the core refuses with ERANGE and the extack
			// "queue index exceeds max value", before the driver.
			{Axis: "cmd", Value: v("qidxover", "dev vstats show vdpa0 qidx 32769", f("cmd", "7")),
				Expect: expect{RC: intp(1), Errno: "ERANGE", Extack: "queue index exceeds max value", RefusedBy: refKernel},
				Why:    "the first queue index past the core's range check"},
			// vdpa's usage calls qidx optional, but it sends the GET
			// without VDPA_ATTR_DEV_QUEUE_INDEX and the kernel refuses
			// it with EINVAL.
			{Axis: "cmd", Value: v("noqidx", "dev vstats show vdpa0", f("cmd", "7")),
				Expect: expect{RC: intp(1), Errno: "EINVAL", RefusedBy: refKernel},
				Why:    "a vstats request with no queue index"},
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
			Cmd:     "vdpa " + joinArgs(args...),
			Proto:   "generic",
			Pre:     []preStep{{Cmd: vdpaSetup}},
			Modules: vdpaModules,
			Serial:  true,
			InitNS:  true,
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
