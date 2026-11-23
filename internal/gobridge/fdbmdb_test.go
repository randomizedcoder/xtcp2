package gobridge

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// neigh is an AF_BRIDGE RTM_NEWNEIGH on ifindex with the NUD state, the
// NTF flags and attrs.
func neigh(ifindex uint32, state uint16, flags uint8, attrs ...[]byte) Msg {
	rest := cat(attrs...)
	b := make([]byte, xtcpnl.NdMsgSizeCst, xtcpnl.NdMsgSizeCst+len(rest))
	b[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint16(b[8:10], state)
	b[10] = flags
	return Msg{Type: unix.RTM_NEWNEIGH, Flags: unix.NLM_F_MULTI, Body: append(b, rest...)}
}

func lladdr(last byte) []byte { return at(unix.NDA_LLADDR, 2, 0, 0, 0, 0, last) }

// cacheinfo is NDA_CACHEINFO: confirmed, used, updated, refcnt.
func cacheinfo(used, updated uint32) []byte {
	return at(unix.NDA_CACHEINFO, cat(u32(0), u32(used), u32(updated), u32(0))...)
}

// mdbMsg is a br_port_msg of the type on ifindex, then attrs.
func mdbMsg(typ uint16, ifindex uint32, attrs ...[]byte) Msg {
	rest := cat(attrs...)
	b := make([]byte, xtcpnl.BrPortMsgSizeCst, xtcpnl.BrPortMsgSizeCst+len(rest))
	if typ == unix.RTM_NEWMDB {
		b[0] = unix.AF_BRIDGE
	}
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	return Msg{Type: typ, Flags: unix.NLM_F_MULTI, Body: append(b, rest...)}
}

// mdbEntry is MDBA_MDB > MDBA_MDB_ENTRY > MDBA_MDB_ENTRY_INFO: a
// br_mdb_entry of the port, state, flags, vid, group and proto, then the
// entry attributes.
func mdbEntry(port uint32, state, flags uint8, vid uint16, grp []byte, proto uint16, eattrs ...[]byte) []byte {
	rest := cat(eattrs...)
	e := make([]byte, xtcpnl.MdbEntrySizeCst, xtcpnl.MdbEntrySizeCst+len(rest))
	binary.LittleEndian.PutUint32(e[0:4], port)
	e[4], e[5] = state, flags
	binary.LittleEndian.PutUint16(e[6:8], vid)
	copy(e[8:24], grp)
	binary.BigEndian.PutUint16(e[24:26], proto)
	info := at(xtcpnl.MdbaMdbEntryInfo, append(e, rest...)...)
	return at(xtcpnl.MdbaMdb, at(xtcpnl.MdbaMdbEntry, info...)...)
}

// srcEnt is one MDBA_MDB_SRCLIST_ENTRY: a v4 address and its timer.
func srcEnt(a [4]byte, timer uint32) []byte {
	return at(xtcpnl.MdbSrcListEntry|unix.NLA_F_NESTED,
		cat(at(xtcpnl.MdbSrcAttrAddress, a[:]...), at(xtcpnl.MdbSrcAttrTimer, u32(timer)...))...)
}

// rport is an MDBA_ROUTER nest with one port and its attributes.
func rport(ifindex uint32, pattrs ...[]byte) []byte {
	return at(xtcpnl.MdbaRouter, at(xtcpnl.MdbaRouterPort, append(u32(ifindex), cat(pattrs...)...)...)...)
}

// fdbGetErr is the kernel's NLMSG_ERROR for a get: the errno, the capped
// request header and the extack message.
func fdbGetErr(errno unix.Errno, msg string) Msg {
	body := append(u32(negErrno(errno)), make([]byte, nlHeaderLen)...)
	body = append(body, at(unix.NLMSGERR_ATTR_MSG, append([]byte(msg), 0)...)...)
	return Msg{Type: unix.NLMSG_ERROR, Flags: unix.NLM_F_ACK_TLVS | unix.NLM_F_CAPPED, Body: body}
}

var grp4 = []byte{239, 1, 1, 1}

// TestGobridgeFdbMdbUnit drives fdb and mdb, show and get, through
// scripted replies: the shapes the corpus does not reach.
func TestGobridgeFdbMdbUnit(t *testing.T) {
	t.Parallel()
	type want struct {
		stdout string
		stderr string
		rc     int
		reqs   int
	}
	// B: d1's static vlan 10 entry; C: d2's reachable one.
	entB := neigh(4, unix.NUD_NOARP, 0, lladdr(0x0b), at(unix.NDA_VLAN, u16(10)...), at(unix.NDA_MASTER, u32(3)...))
	entC := neigh(5, unix.NUD_REACHABLE, unix.NTF_MASTER, lladdr(0x0c), at(unix.NDA_MASTER, u32(3)...))
	entA := neigh(4, unix.NUD_PERMANENT, 0, lladdr(0x0a), at(unix.NDA_MASTER, u32(3)...))
	mdbB := mdbMsg(unix.RTM_GETMDB, 3, mdbEntry(4, mdbPermanent, 0, 10, grp4, ethPIP))
	tests := []struct {
		description string
		args        string
		replies     []Reply
		expected    want
	}{
		{
			description: "positive: fdb show, a bridge entry",
			args:        "fdb show",
			replies:     []Reply{cache(), rep(entB, done())},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 vlan 10 master br0 static\n", reqs: 2},
		},
		{
			description: "positive: -j fdb show, two entries, flags always there",
			args:        "-j fdb show",
			replies:     []Reply{cache(), rep(entB, entC, done())},
			expected: want{stdout: `[{"mac":"02:00:00:00:00:0b","ifname":"d1","vlan":10,"flags":[],"master":"br0","state":"static"},` +
				`{"mac":"02:00:00:00:00:0c","ifname":"d2","flags":["master"],"master":"br0","state":""}]` + "\n", reqs: 2},
		},
		{
			description: "positive: -s fdb, used/updated in user hz",
			args:        "-s fdb",
			replies:     []Reply{cache(), rep(neigh(4, unix.NUD_NOARP, 0, lladdr(0x0b), cacheinfo(250, 500)), done())},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 used 2/5 static\n", reqs: 2},
		},
		{
			description: "positive: -d fdb, activity_notify and inactive",
			args:        "-d fdb",
			replies: []Reply{cache(), rep(neigh(4, unix.NUD_NOARP, 0, lladdr(0x0b),
				at(xtcpnl.NdaFdbExtAttrs|unix.NLA_F_NESTED, at(xtcpnl.NfeaActivityNotify, 3)...)), done())},
			expected: want{stdout: "02:00:00:00:00:0b dev d1 activity_notify inactive static\n", reqs: 2},
		},
		{
			description: "positive: a vxlan entry's dst, port, vni, src_vni, via and nhid",
			args:        "fdb show",
			replies: []Reply{cache(), rep(neigh(6, unix.NUD_NOARP, unix.NTF_SELF, lladdr(0), at(unix.NDA_DST, 10, 0, 0, 1),
				at(unix.NDA_PORT, 0x12, 0xb6), at(unix.NDA_VNI, u32(200)...), at(unix.NDA_SRC_VNI, u32(300)...),
				at(unix.NDA_IFINDEX, u32(4)...), at(xtcpnl.NdaNhID, u32(1)...)), done())},
			expected: want{stdout: "02:00:00:00:00:00 dev master dst 10.0.0.1 port 4790 vni 200 src_vni 300 via d1 nhid 1 self static\n", reqs: 2},
		},
		{
			description: "positive: fdb get, the answer printed alone",
			args:        "fdb get 02:00:00:00:00:0b br br0 vlan 10",
			replies:     []Reply{cache(), rep(entB)},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 vlan 10 master br0 static\n", reqs: 2},
		},
		{
			description: "positive: -d mdb, filter mode, source list and proto",
			args:        "-d mdb",
			replies: []Reply{cache(), rep(mdbMsg(unix.RTM_GETMDB, 3, mdbEntry(4, 0, 0, 0, []byte{239, 2, 2, 2}, ethPIP,
				at(xtcpnl.MdbEattrGroupMode, mcastInclude),
				at(xtcpnl.MdbEattrSrcList|unix.NLA_F_NESTED, cat(srcEnt([4]byte{10, 0, 0, 1}, 260), srcEnt([4]byte{10, 0, 0, 2}, 0))...),
				at(xtcpnl.MdbEattrRtProt, 4))), done()), rep(done())},
			expected: want{stdout: "dev br0 port d1 grp 239.2.2.2 temp filter_mode include source_list 10.0.0.1/2.60,10.0.0.2/0.00 proto static\n", reqs: 3},
		},
		{
			description: "positive: -d mdb, a bridge's router ports",
			args:        "-d mdb",
			replies:     []Reply{cache(), rep(done()), rep(mdbMsg(unix.RTM_GETMDB, 3, rport(4)), done())},
			expected:    want{stdout: "router ports on br0: d1 \n", reqs: 3},
		},
		{
			description: "positive: -j mdb get, a flat array with no router key",
			args:        "-j mdb get dev br0 grp 239.1.1.1 vid 10",
			replies:     []Reply{cache(), rep(mdbMsg(unix.RTM_NEWMDB, 3, mdbEntry(4, mdbPermanent, 0, 10, grp4, ethPIP)))},
			expected: want{stdout: `[{"index":3,"dev":"br0","port":"d1","grp":"239.1.1.1","state":"permanent","flags":[],"vid":10}]` + "\n",
				reqs: 2},
		},
		{
			description: "positive: mdb show, a vxlan's router port as a line",
			args:        "mdb",
			replies:     []Reply{cache(), rep(done()), rep(mdbMsg(unix.RTM_NEWMDB, 6, rport(4)), done())},
			expected:    want{stdout: "router port dev d1 master master\n", reqs: 3},
		},
		{
			description: "negative: an unknown fdb command",
			args:        "fdb bogus",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Command \"bogus\" is unknown, try \"bridge fdb help\".\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: mdb g is not get (strcmp, M2)",
			args:        "mdb g dev br0 grp 239.1.1.1",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Command \"g\" is unknown, try \"bridge mdb help\".\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: fdb show dev nosuch sends no dump",
			args:        "fdb show dev nosuch",
			replies:     []Reply{cache(), rep(nlErr(unix.ENODEV))},
			expected:    want{stderr: "Cannot find device \"nosuch\"\n", rc: 255, reqs: 2},
		},
		{
			description: "negative: fdb show br nosuch",
			args:        "fdb show br nosuch",
			replies:     []Reply{cache(), rep(nlErr(unix.ENODEV))},
			expected:    want{stderr: "Cannot find bridge device \"nosuch\"\n", rc: 255, reqs: 2},
		},
		{
			description: "negative: an fdb dump error exits 1 with the array open (F12)",
			args:        "-j fdb show",
			replies:     []Reply{cache(), rep(nlErr(unix.EPERM))},
			expected:    want{stdout: "[", stderr: "RTNETLINK answers: Operation not permitted\nDump terminated\n", rc: 1, reqs: 2},
		},
		{
			description: "negative: an mdb dump error in text returns -1",
			args:        "mdb",
			replies:     []Reply{cache(), rep(nlErr(unix.EPERM))},
			expected:    want{stderr: "RTNETLINK answers: Operation not permitted\nDump terminated\n", rc: 255, reqs: 2},
		},
		{
			description: "negative: an mdb dump error under -j is the assert abort (M4)",
			args:        "-j mdb",
			replies:     []Reply{cache(), rep(nlErr(unix.EPERM))},
			expected: want{stderr: "RTNETLINK answers: Operation not permitted\nDump terminated\n" +
				"bridge: json_writer.c:122: jsonw_destroy: Assertion `self->depth == 0' failed.\n", rc: 134, reqs: 2},
		},
		{
			description: "negative: fdb get of a missing entry is rc 254 with the extack",
			args:        "fdb get 02:00:00:00:00:99 br br0",
			replies:     []Reply{cache(), rep(fdbGetErr(unix.ENOENT, "Fdb entry not found"))},
			expected:    want{stderr: "Error: Fdb entry not found.\n", rc: 254, reqs: 2},
		},
		{
			description: "negative: fdb get of a bad mac",
			args:        "fdb get zz br br0",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Invalid mac address zz\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: fdb get, two addresses (duparg2)",
			args:        "fdb get 02:00:00:00:00:01 02:00:00:00:00:02 br br0",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Error: either \"to\" is duplicate, or \"02:00:00:00:00:02\" is garbage.\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: fdb get vni past 24 bits, a double newline (F13)",
			args:        "fdb get 02:00:00:00:00:01 br br0 vni 16777216",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Error: argument \"16777216\" is wrong: invalid VNI\n\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: fdb show state bogus",
			args:        "fdb show state bogus",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Error: argument \"bogus\" is wrong: invalid state\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: mdb get without a group",
			args:        "mdb get dev br0",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Device and group address are required arguments.\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: a partial dotted quad is 4 bytes, not a MAC: no lladdr message",
			args:        "mdb get dev br0 grp 1.2.3",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Invalid address \"1.2.3\"\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: bogus is sscanf %x of b, one byte: no lladdr message",
			args:        "mdb get dev br0 grp bogus",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Invalid address \"bogus\"\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: a non-hex part is named alone",
			args:        "mdb get dev br0 grp zz:01",
			replies:     []Reply{cache()},
			expected:    want{stderr: "\"zz\" is invalid lladdr.\nInvalid address \"zz:01\"\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: a part past 255",
			args:        "mdb get dev br0 grp 01:300:03:04:05:06",
			replies:     []Reply{cache()},
			expected:    want{stderr: "\"300\" is invalid lladdr.\nInvalid address \"01:300:03:04:05:06\"\n", rc: 255, reqs: 1},
		},
		{
			description: "negative: a dotted form with a colon fails get_addr_1",
			args:        "mdb get dev br0 grp 1.2:3",
			replies:     []Reply{cache()},
			expected:    want{stderr: "\"1.2:3\" is invalid lladdr.\nInvalid address \"1.2:3\"\n", rc: 255, reqs: 1},
		},
		{
			description: "boundary: seven parts are one too many, silently",
			args:        "mdb get dev br0 grp 01:02:03:04:05:06:07",
			replies:     []Reply{cache()},
			expected:    want{stderr: "Invalid address \"01:02:03:04:05:06:07\"\n", rc: 255, reqs: 1},
		},
		{
			description: "corner: an empty last part is named as \"\"",
			args:        "mdb get dev br0 grp 01:02:03:04:05:",
			replies:     []Reply{cache()},
			expected:    want{stderr: "\"\" is invalid lladdr.\nInvalid address \"01:02:03:04:05:\"\n", rc: 255, reqs: 1},
		},
		{
			description: "corner: a negative part is an int below 0",
			args:        "mdb get dev br0 grp -1:02:03:04:05:06",
			replies:     []Reply{cache()},
			expected:    want{stderr: "\"-1\" is invalid lladdr.\nInvalid address \"-1:02:03:04:05:06\"\n", rc: 255, reqs: 1},
		},
		{
			description: "boundary: an empty fdb dump prints nothing",
			args:        "fdb show",
			replies:     []Reply{cache(), rep(done())},
			expected:    want{reqs: 2},
		},
		{
			description: "boundary: -j fdb, an empty dump is []",
			args:        "-j fdb show",
			replies:     []Reply{cache(), rep(done())},
			expected:    want{stdout: "[]\n", reqs: 2},
		},
		{
			description: "boundary: -j mdb, empty dumps keep both keys",
			args:        "-j mdb",
			replies:     []Reply{cache(), rep(done()), rep(done())},
			expected:    want{stdout: `[{"mdb":[],"router":{}}]` + "\n", reqs: 3},
		},
		{
			description: "boundary: state 0 is no filter (F5)",
			args:        "fdb show state 0",
			replies:     []Reply{cache(), rep(entB, done())},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 vlan 10 master br0 static\n", reqs: 2},
		},
		{
			description: "boundary: LINK_NETNSID -1, signed text (F8)",
			args:        "fdb show",
			replies: []Reply{cache(), rep(neigh(6, unix.NUD_NOARP, 0, lladdr(0), at(unix.NDA_IFINDEX, u32(4)...),
				at(unix.NDA_LINK_NETNSID, u32(0xffffffff)...)), done())},
			expected: want{stdout: "02:00:00:00:00:00 dev master via ifindex 4 link-netnsid -1 static\n", reqs: 2},
		},
		{
			description: "boundary: LINK_NETNSID -1, unsigned JSON and viaIfIndex (F6, F8)",
			args:        "-j fdb show",
			replies: []Reply{cache(), rep(neigh(6, unix.NUD_NOARP, 0, lladdr(0), at(unix.NDA_IFINDEX, u32(4)...),
				at(unix.NDA_LINK_NETNSID, u32(0xffffffff)...)), done())},
			expected: want{stdout: `[{"mac":"02:00:00:00:00:00","ifname":"master","viaIfIndex":4,"linkNetNsId":4294967295,"flags":[],"state":"static"}]` + "\n", reqs: 2},
		},
		{
			description: "corner: a reachable entry's state is empty, a trailing space (F7)",
			args:        "fdb show",
			replies:     []Reply{cache(), rep(entC, done())},
			expected:    want{stdout: "02:00:00:00:00:0c dev d2 master master br0 \n", reqs: 2},
		},
		{
			description: "corner: an unknown state is hex (F7)",
			args:        "fdb show",
			replies:     []Reply{cache(), rep(neigh(4, unix.NUD_FAILED, 0, lladdr(0x0b)), done())},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 state=0x20\n", reqs: 2},
		},
		{
			description: "corner: dynamic hides permanent but keeps a static (F4)",
			args:        "fdb show dynamic",
			replies:     []Reply{cache(), rep(entA, entB, done())},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 vlan 10 master br0 static\n", reqs: 2},
		},
		{
			description: "corner: dev filters and hides the dev column",
			args:        "fdb show dev d2",
			replies:     []Reply{cache(), rep(entB, entC, done())},
			expected:    want{stdout: "02:00:00:00:00:0c master master br0 \n", reqs: 2},
		},
		{
			description: "corner: an L2 group's src is (null) in text (M5)",
			args:        "mdb",
			replies: []Reply{cache(), rep(mdbMsg(unix.RTM_GETMDB, 3, mdbEntry(4, 0, 0, 0, []byte{1, 0, 0x5e, 1, 1, 1}, 0,
				at(xtcpnl.MdbEattrSource, 10, 0, 0, 1))), done()), rep(done())},
			expected: want{stdout: "dev br0 port d1 grp 01:00:5e:01:01:01 src (null) temp\n", reqs: 3},
		},
		{
			description: "corner: an L2 group's src is a bare key in JSON (M5)",
			args:        "-j mdb",
			replies: []Reply{cache(), rep(mdbMsg(unix.RTM_GETMDB, 3, mdbEntry(4, 0, 0, 0, []byte{1, 0, 0x5e, 1, 1, 1}, 0,
				at(xtcpnl.MdbEattrSource, 10, 0, 0, 1))), done()), rep(done())},
			expected: want{stdout: `[{"mdb":[{"index":3,"dev":"br0","port":"d1","grp":"01:00:5e:01:01:01","src":"state":"temp","flags":[]}],"router":{}}]` + "\n", reqs: 3},
		},
		{
			description: "corner: a router port of type 0 prints permanent (M8)",
			args:        "-d -s mdb",
			replies: []Reply{cache(), rep(done()), rep(mdbMsg(unix.RTM_GETMDB, 3,
				rport(4, at(xtcpnl.MdbaRouterPattrType, 0))), done())},
			expected: want{stdout: "router ports on br0: d1 permanent\n", reqs: 3},
		},
		{
			description: "corner: mdb show ignores unknown words and help (M1)",
			args:        "mdb show bogus help",
			replies:     []Reply{cache(), rep(mdbB, done()), rep(done())},
			expected:    want{stdout: "dev br0 port d1 grp 239.1.1.1 permanent vid 10\n", reqs: 3},
		},
		{
			description: "corner: mdb show vid filters in userspace",
			args:        "mdb show vid 20",
			replies:     []Reply{cache(), rep(mdbB, done()), rep(done())},
			expected:    want{reqs: 3},
		},
		{
			description: "corner: fdb g is get (prefix); get's dev sets no filter, so the dev column stays",
			args:        "fdb g 02:00:00:00:00:0b dev d1",
			replies:     []Reply{cache(), rep(entB)},
			expected:    want{stdout: "02:00:00:00:00:0b dev d1 vlan 10 master br0 static\n", reqs: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src}, strings.Fields(tc.args), &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc, reqs: len(src.reqs)}
			if got != tc.expected {
				t.Errorf("bridge %s\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}
