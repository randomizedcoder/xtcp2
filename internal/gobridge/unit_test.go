package gobridge

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeSource answers requests from a script, in order, and records them.
type fakeSource struct {
	replies []Reply
	reqs    [][]byte
}

var errScript = errors.New("fake: no reply scripted")

func (f *fakeSource) Talk(req []byte) (Reply, error) {
	f.reqs = append(f.reqs, req)
	if len(f.replies) == 0 {
		return Reply{}, fmt.Errorf("%w: request %d", errScript, len(f.reqs))
	}
	r := f.replies[0]
	f.replies = f.replies[1:]
	return r, nil
}

// at is one attribute, padded to four bytes.
func at(typ uint16, val ...byte) []byte {
	l := 4 + len(val)
	b := make([]byte, 4, l+3)
	ul, ok := cint.U16(l)
	if !ok {
		panic("attribute too long")
	}
	binary.LittleEndian.PutUint16(b[0:2], ul)
	binary.LittleEndian.PutUint16(b[2:4], typ)
	b = append(b, val...)
	return append(b, make([]byte, (4-l%4)%4)...)
}

func cat(bs ...[]byte) []byte {
	n := 0
	for _, b := range bs {
		n += len(b)
	}
	out := make([]byte, 0, n)
	for _, b := range bs {
		out = append(out, b...)
	}
	return out
}

func u16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }

// negErrno is -errno as the kernel's int, in its unsigned bits.
func negErrno(e unix.Errno) uint32 {
	v, _ := cint.U32(int(e))
	return -v
}
func name(s string) []byte {
	return at(unix.IFLA_IFNAME, append([]byte(s), 0)...)
}

// link is an RTM_NEWLINK of the family, index and flags, then attrs.
func link(family uint8, index, flags uint32, attrs ...[]byte) Msg {
	rest := cat(attrs...)
	b := make([]byte, 16, 16+len(rest))
	b[0] = family
	binary.LittleEndian.PutUint32(b[4:8], index)
	binary.LittleEndian.PutUint32(b[8:12], flags)
	return Msg{Type: unix.RTM_NEWLINK, Flags: unix.NLM_F_MULTI, Body: append(b, rest...)}
}

func done() Msg { return Msg{Type: unix.NLMSG_DONE, Flags: unix.NLM_F_MULTI, Body: u32(0)} }

func doneErr(errno unix.Errno, msg string) Msg {
	body := u32(negErrno(errno))
	if msg != "" {
		body = append(body, at(unix.NLMSGERR_ATTR_MSG, append([]byte(msg), 0)...)...)
	}
	return Msg{Type: unix.NLMSG_DONE, Flags: unix.NLM_F_MULTI | unix.NLM_F_ACK_TLVS, Body: body}
}

func nlErr(errno unix.Errno) Msg {
	return Msg{Type: unix.NLMSG_ERROR, Body: append(u32(negErrno(errno)), make([]byte, 16)...)}
}

func rep(ms ...Msg) Reply { return Reply{Msgs: ms} }

const up = unix.IFF_UP | unix.IFF_BROADCAST | unix.IFF_RUNNING | unix.IFF_LOWER_UP

// cache is ll_init_map's reply: br0 (3), d1 (4), d2 (5), master (6).
func cache() Reply {
	return rep(link(unix.AF_UNSPEC, 3, up, name("br0")), link(unix.AF_UNSPEC, 4, up, name("d1")),
		link(unix.AF_UNSPEC, 5, up, name("d2")), link(unix.AF_UNSPEC, 6, up, name("master")), done())
}

// port is an AF_BRIDGE link under br0 with protinfo state, priority, cost.
func port(index uint32, n string, extra ...[]byte) Msg {
	prot := at(xtcpnl.IflaProtinfoCst|unix.NLA_F_NESTED, cat(at(unix.IFLA_BRPORT_STATE, 3),
		at(unix.IFLA_BRPORT_PRIORITY, u16(32)...), at(unix.IFLA_BRPORT_COST, u32(100)...))...)
	return link(unix.AF_BRIDGE, index, up, append([][]byte{name(n), at(unix.IFLA_MTU, u32(1500)...),
		at(unix.IFLA_MASTER, u32(3)...), prot}, extra...)...)
}

func vinfo(flags, vid uint16) []byte {
	return at(xtcpnl.IflaBridgeVlanInfo, cat(u16(flags), u16(vid))...)
}

func spec(as ...[]byte) []byte { return at(xtcpnl.IflaAfSpecCst, cat(as...)...) }

// mstEntry is one IFLA_BRIDGE_MST_ENTRY; a zero msti or state leaves it out
// when omit says so.
func mstEntry(msti uint16, state uint8, omitState bool) []byte {
	in := at(xtcpnl.IflaBridgeMstEntryMsti, u16(msti)...)
	if !omitState {
		in = cat(in, at(xtcpnl.IflaBridgeMstEntryState, state))
	}
	return at(xtcpnl.IflaBridgeMstEntry|unix.NLA_F_NESTED, in...)
}

// vlanEntry is an RTM_NEWVLAN for index holding nests.
func vlanMsg(index uint32, nests ...[]byte) Msg {
	rest := cat(nests...)
	b := make([]byte, 8, 8+len(rest))
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], index)
	return Msg{Type: unix.RTM_NEWVLAN, Flags: unix.NLM_F_MULTI, Body: append(b, rest...)}
}

// TestGobridgeUnit runs gobridge on scripted replies.
func TestGobridgeUnit(t *testing.T) {
	t.Parallel()
	hdr := col("port") + "  " + "vlan-id  " + "\n"
	type want struct {
		stdout, stderr string
		rc, reqs       int
	}
	tests := []struct {
		description string
		args        string
		replies     []Reply
		expected    want
	}{
		{
			description: "positive: -com vlan show prints a range 20-25",
			args:        "-com vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(6, 10), vinfo(8, 20), vinfo(16, 25))), done())},
			expected: want{stdout: hdr + col("d1") + "  10 PVID Egress Untagged\n" + strings.Repeat(" ", 18) + "20-25\n",
				reqs: 2},
		},
		{
			description: "positive: -j link show, two ports",
			args:        "-j link show",
			replies:     []Reply{cache(), rep(port(4, "d1"), port(5, "d2"), done())},
			expected: want{stdout: `[{"ifindex":4,"ifname":"d1","flags":["BROADCAST","UP","LOWER_UP"],"mtu":1500,"master":"br0","state":"forwarding","priority":32,"cost":100},` +
				`{"ifindex":5,"ifname":"d2","flags":["BROADCAST","UP","LOWER_UP"],"mtu":1500,"master":"br0","state":"forwarding","priority":32,"cost":100}]` + "\n", reqs: 2},
		},
		{
			description: "negative: an unknown object, no request",
			args:        "badobj",
			expected:    want{stderr: "Object \"badobj\" is unknown, try \"bridge help\".\n", rc: 255},
		},
		{
			description: "negative: nodev after the kernel's ENODEV, no dump",
			args:        "vlan show dev nosuch",
			replies:     []Reply{cache(), rep(nlErr(unix.ENODEV))},
			expected:    want{stderr: "Cannot find device \"nosuch\"\n", rc: 255, reqs: 2},
		},
		{
			description: "negative: a write is refused after ll_init_map",
			args:        "vlan add dev d1 vid 5",
			replies:     []Reply{cache()},
			expected:    want{stderr: "gobridge: not implemented: bridge vlan add\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: an NLMSG_ERROR mid-dump keeps the first line, exit 1",
			args:        "link show",
			replies:     []Reply{cache(), rep(port(4, "d1"), nlErr(unix.EBUSY))},
			expected: want{stdout: "4: d1: <BROADCAST,UP,LOWER_UP> mtu 1500 master br0 state forwarding priority 32 cost 100 \n",
				stderr: "RTNETLINK answers: Device or resource busy\nDump terminated\n", rc: 1, reqs: 2},
		},
		{
			description: "negative: B4, mst's failed dump exits 255 and closes the JSON",
			args:        "-j mst show",
			replies:     []Reply{cache(), rep(doneErr(unix.EINVAL, "nope"))},
			expected:    want{stdout: "[]\n", stderr: "Error: nope.\nDump terminated\n", rc: 255, reqs: 2},
		},
		{
			description: "negative: a script that runs out is a replay error",
			args:        "link show",
			replies:     []Reply{cache()},
			expected:    want{stderr: "fake: no reply scripted: request 2\n", rc: 1, reqs: 2},
		},
		{
			description: "boundary: empty dumps print vni's and mst's headers",
			args:        "vni show",
			replies:     []Reply{cache(), rep(done())},
			expected:    want{stdout: col("dev") + "  " + col17("vni") + "  group/remote\n", reqs: 2},
		},
		{
			description: "boundary: empty -j dump is []",
			args:        "-j vlan show",
			replies:     []Reply{cache(), rep(done())},
			expected:    want{stdout: "[]\n", reqs: 2},
		},
		{
			description: "boundary: vid 4094",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(0, 4094))), done())},
			expected:    want{stdout: hdr + col("d1") + "  4094\n", reqs: 2},
		},
		{
			description: "corner: vni sh is refused (strcmp)",
			args:        "vni sh",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Command \"sh\" is unknown, try \"bridge vni help\".\n", rc: 255, reqs: 1},
		},
		{
			description: "corner: B1, link show dev master",
			args:        "link show dev master",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Command line is not complete. Try option \"help\"\n", rc: 255, reqs: 1},
		},
		{
			description: "corner: vid abc is no filter (atoi)",
			args:        "vlan show vid abc",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(0, 7))), done())},
			expected:    want{stdout: hdr + col("d1") + "  7\n", reqs: 2},
		},
		{
			description: "corner: B10, a port without AF_SPEC ends the mst dump",
			args:        "mst show dev d2",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(at(xtcpnl.IflaBridgeMst|unix.NLA_F_NESTED, mstEntry(0, 3, false)...))), port(5, "d2"), done())},
			expected:    want{stdout: col("port") + "  msti     \n", stderr: "Dump terminated\n", rc: 255, reqs: 2},
		},
		{
			description: "corner: B11, a broken MST entry's message has no newline",
			args:        "mst show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(at(xtcpnl.IflaBridgeMst|unix.NLA_F_NESTED, mstEntry(3, 0, true)...))), done())},
			expected:    want{stdout: col("port") + "  msti     \n" + col("d1") + "  ", stderr: "BUG: broken MST entry", reqs: 2},
		},
		{
			description: "corner: B13, an unknown STP state is a number in JSON",
			args:        "-j link show",
			replies: []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"),
				at(xtcpnl.IflaProtinfoCst|unix.NLA_F_NESTED, at(unix.IFLA_BRPORT_STATE, 5)...)), done())},
			expected: want{stdout: `[{"ifindex":4,"ifname":"d1","flags":[],"state":5}]` + "\n", reqs: 2},
		},
		{
			description: "corner: B12, an mtu above INT_MAX is negative in JSON, unsigned in text",
			args:        "-j link show",
			replies:     []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"), at(unix.IFLA_MTU, u32(0x80000000)...)), done())},
			expected:    want{stdout: `[{"ifindex":4,"ifname":"d1","flags":[],"mtu":-2147483648}]` + "\n", reqs: 2},
		},
		{
			description: "corner: B7, the master filter passes a link with no IFLA_MASTER",
			args:        "link show master br0",
			replies:     []Reply{cache(), rep(link(unix.AF_BRIDGE, 5, 0, name("d2")), done())},
			expected:    want{stdout: "5: d2: <> \n", reqs: 2},
		},
		{
			description: "corner: B15, tunnel info without an ID prints tunid 0",
			args:        "vlan tunnelshow",
			replies: []Reply{cache(), rep(port(4, "d1", spec(at(xtcpnl.IflaBridgeVlanTunnelInfo,
				at(xtcpnl.IflaBridgeVlanTunnelVid, u16(9)...)...))), done())},
			expected: want{stdout: col("port") + "  vlan-id    tunnel-id\n" + col("d1") + "  9" + strings.Repeat(" ", 10) + "0\n", reqs: 2},
		},
		{
			description: "corner: B16, a non-NEWLINK reply is reported and skipped",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(Msg{Type: unix.RTM_NEWADDR, Flags: 2, Body: make([]byte, 8)}, done())},
			expected:    want{stdout: hdr, stderr: "Not RTM_NEWLINK: 00000018 00000014 00000002\n", reqs: 2},
		},
		{
			description: "corner: B18, LAST_MEMBER_CNT missing beside MLD_VERSION: bridge segfaults",
			args:        "vlan global show",
			replies: []Reply{cache(), rep(vlanMsg(3, at(xtcpnl.BridgeVlandbGlobalOptions,
				cat(at(xtcpnl.BridgeVlandbGoptsID, u16(1)...), at(goptsMldVersion, 1))...)), done())},
			expected: want{rc: 139, reqs: 2},
		},
		{
			description: "corner: B19, a global entry without an ID: bridge segfaults",
			args:        "vlan global show",
			replies:     []Reply{cache(), rep(vlanMsg(3, at(xtcpnl.BridgeVlandbGlobalOptions, at(goptsSnooping, 1)...)), done())},
			expected:    want{rc: 139, reqs: 2},
		},
		{
			description: "corner: B20, a -d entry without INFO: bridge segfaults",
			args:        "-d vlan show",
			replies:     []Reply{cache(), rep(vlanMsg(4, at(xtcpnl.BridgeVlandbEntry, at(xtcpnl.BridgeVlandbEntryState, 3)...)), done())},
			expected:    want{rc: 139, reqs: 2},
		},
		{
			description: "corner: -c colors the port name, dark palette by default",
			args:        "-c vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(0, 7))), done())},
			expected:    want{stdout: hdr + "\x1b[1;36m" + col("d1") + "  \x1b[0m7\n", reqs: 2},
		},
		{
			description: "corner: -j with -c has no color",
			args:        "-j -c vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(0, 7))), done())},
			expected:    want{stdout: `[{"ifname":"d1","vlans":[{"vlan":7}]}]` + "\n", reqs: 2},
		},
		{
			description: "corner: B22, -s tunnelshow takes the stats dumps",
			args:        "-s vlan tunnelshow",
			replies:     []Reply{cache(), rep(done()), rep(done())},
			expected:    want{stdout: col("port") + "  vlan-id\n", reqs: 3},
		},
		{
			description: "positive: hwmode VEPA from AF_SPEC",
			args:        "link show",
			replies:     []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"), spec(at(xtcpnl.IflaBridgeMode, u16(1)...))), done())},
			expected:    want{stdout: "4: d1: <> hwmode VEPA \n", reqs: 2},
		},
		{
			description: "corner: B13, an unknown hwmode is hex text and a hex string in JSON",
			args:        "-j link show",
			replies:     []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"), spec(at(xtcpnl.IflaBridgeMode, u16(7)...))), done())},
			expected:    want{stdout: `[{"ifindex":4,"ifname":"d1","flags":[],"hwmode":"0x7"}]` + "\n", reqs: 2},
		},
		{
			description: "positive: IFLA_LINK names the peer; a down peer is M-DOWN",
			args:        "link show",
			replies: []Reply{rep(link(unix.AF_UNSPEC, 4, up, name("d1")), link(unix.AF_UNSPEC, 5, 0, name("d2")), done()),
				rep(link(unix.AF_BRIDGE, 4, unix.IFF_UP, name("d1"), at(unix.IFLA_LINK, u32(5)...)), done())},
			expected: want{stdout: "4: d1@d2: <NO-CARRIER,UP,M-DOWN> \n", reqs: 2},
		},
		{
			description: "boundary: IFLA_LINK 0 is NONE in text, null in JSON",
			args:        "-j link show",
			replies:     []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"), at(unix.IFLA_LINK, u32(0)...)), done())},
			expected:    want{stdout: `[{"ifindex":4,"link":null,"ifname":"d1","flags":[]}]` + "\n", reqs: 2},
		},
		{
			description: "corner: an uncached master is looked up by index, then if%u",
			args:        "link show",
			replies: []Reply{cache(), rep(link(unix.AF_BRIDGE, 4, 0, name("d1"), at(unix.IFLA_MASTER, u32(77)...)), done()),
				rep(nlErr(unix.ENODEV))},
			expected: want{stdout: "4: d1: <> master if77 \n", reqs: 3},
		},
		{
			description: "positive: -o puts protinfo's detail line on the same line",
			args:        "-o -d link show",
			replies:     []Reply{cache(), rep(port(4, "d1", at(xtcpnl.IflaProtinfoCst|unix.NLA_F_NESTED, at(unix.IFLA_BRPORT_MODE, 1)...)), done())},
			expected:    want{stdout: "4: d1: <BROADCAST,UP,LOWER_UP> mtu 1500 master br0 state forwarding priority 32 cost 100 \\    \n", reqs: 2},
		},
		{
			description: "positive: -j -p pretty-prints",
			args:        "-j -p vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(vinfo(0, 7))), done())},
			expected:    want{stdout: "[ {\n        \"ifname\": \"d1\",\n        \"vlans\": [ {\n                \"vlan\": 7\n            } ]\n    } ]\n", reqs: 2},
		},
		{
			description: "positive: global querier state and router ports",
			args:        "vlan global show",
			replies: []Reply{cache(), rep(vlanMsg(3, at(xtcpnl.BridgeVlandbGlobalOptions, cat(
				at(xtcpnl.BridgeVlandbGoptsID, u16(1)...),
				at(goptsQuerierSt|unix.NLA_F_NESTED, cat(at(querierIPAddr, 10, 0, 0, 1), at(querierIPPort, u32(4)...),
					at(querierIPTimer, u64(2_000_000)...))...),
				at(goptsRouterPorts|unix.NLA_F_NESTED, at(1, at(1, u32(4)...)...)...))...)), done())},
			expected: want{stdout: hdr + col("br0") + "  1\n" + strings.Repeat(" ", 20) +
				"mcast_querier_ipv4_addr 10.0.0.1 mcast_querier_ipv4_port 4 mcast_querier_ipv4_other_timer 2ms \n" +
				strings.Repeat(" ", 20) + "router ports: d1 \n", reqs: 2},
		},
		{
			description: "negative: link help prints link's usage",
			args:        "link help",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Usage: bridge link set", rc: 255, reqs: 1},
		},
		{
			description: "negative: B27, a 2-byte VLAN_INFO is refused, not read past",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(port(4, "d1", spec(at(xtcpnl.IflaBridgeVlanInfo, 1, 0))), done())},
			expected:    want{stdout: hdr, stderr: "gobridge: IFLA_BRIDGE_VLAN_INFO is short; bridge reads past it (B27)\n", rc: 1, reqs: 2},
		},
		{
			description: "boundary: a DONE with ENOENT ends the dump silently",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(doneErr(unix.ENOENT, ""))},
			expected:    want{stdout: hdr, stderr: "Dump terminated\n", rc: 1, reqs: 2},
		},
		{
			description: "boundary: a DONE with EMSGSIZE says the buffer is too small",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(doneErr(unix.EMSGSIZE, ""))},
			expected:    want{stdout: hdr, stderr: "Error: Buffer too small for object.\nDump terminated\n", rc: 1, reqs: 2},
		},
		{
			description: "boundary: a DONE with another errno is perror'd",
			args:        "vlan show",
			replies:     []Reply{cache(), rep(doneErr(unix.EPERM, ""))},
			expected:    want{stdout: hdr, stderr: "RTNETLINK answers: Operation not permitted\nDump terminated\n", rc: 1, reqs: 2},
		},
		{
			description: "corner: vni's failed dump keeps its typo (B3) and exits 1",
			args:        "vni show dev d1",
			replies:     []Reply{cache(), rep(doneErr(unix.EINVAL, "The device is not a vxlan device"))},
			expected: want{stdout: col("dev") + "  " + col17("vni") + "  group/remote\n",
				stderr: "Error: The device is not a vxlan device.\nDump ternminated\n", rc: 1, reqs: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src}, strings.Fields(tc.args), &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if strings.HasPrefix(tc.expected.stderr, "Usage:") && strings.HasPrefix(got.stderr, tc.expected.stderr) {
				got.stderr = tc.expected.stderr // a usage row checks the first line
			}
			if got != tc.expected {
				t.Errorf("bridge %s\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestHelpers pins the small C emulations.
func TestHelpers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: atoi digits", fmt.Sprint(atoi("22")), "22"},
		{"negative: atoi of letters is 0", fmt.Sprint(atoi("abc")), "0"},
		{"boundary: atoi wraps past INT_MAX as C's int does", fmt.Sprint(atoi("4294967297")), "1"},
		{"corner: atoi takes space, a sign and stops at junk", fmt.Sprint(atoi("  -7x")), "-7"},
		{"positive: idxA2N if999", fmt.Sprint(idxA2N("if999")), "999"},
		{"negative: idxA2N of a name", fmt.Sprint(idxA2N("nosuch")), "0"},
		{"corner: idxA2N ignores a tail, as sscanf does", fmt.Sprint(idxA2N("if12ab")), "12"},
		{"positive: padLeft 9-5", fmt.Sprintf("%q", padLeft(4)), `"      "`},
		{"corner: padLeft of a negative width is its magnitude", fmt.Sprintf("%q", padLeft(-2)), `"    "`},
		{"positive: formatHost v4", must(formatHost([]byte{239, 1, 1, 1}, 4)), "239.1.1.1"},
		{"corner: formatHost writes an IPv4-compatible v6 as glibc does", must(formatHost(append(make([]byte, 12), 1, 2, 3, 4), 16)), "::1.2.3.4"},
		{"corner: formatHost v4-mapped", must(formatHost(append(append(make([]byte, 10), 0xff, 0xff), 1, 2, 3, 4), 16)), "::ffff:1.2.3.4"},
		{"boundary: cHashX of 0 is 0", cHashX(0), "0"},
		{"positive: cHashX", cHashX(7), "0x7"},
		{"positive: sprintTime64 seconds", sprintTime64(1_500_000_000), "1.5s"},
		{"boundary: sprintTime64 ns", sprintTime64(999), "999ns"},
		{"corner: wrapS64 past INT64_MAX is negative", fmt.Sprint(wrapS64(^uint64(0))), "-1"},
		{"positive: dark background by default", fmt.Sprint(darkBackground("")), "true"},
		{"negative: COLORFGBG 0;15 is light", fmt.Sprint(darkBackground("0;15")), "false"},
		{"corner: COLORFGBG 15;default;0 is dark", fmt.Sprint(darkBackground("15;default;0")), "true"},
		{"negative: an invalid ifname", fmt.Sprint(validIfName("a b")), "false"},
		{"boundary: a 15-byte ifname is valid, 16 is not", fmt.Sprint(validIfName(strings.Repeat("a", 15)), validIfName(strings.Repeat("a", 16))), "true false"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.expected {
				t.Errorf("got %q, want %q", tc.got, tc.expected)
			}
		})
	}
}

func must(s string, ok bool) string {
	if !ok {
		return "!ok"
	}
	return s
}
