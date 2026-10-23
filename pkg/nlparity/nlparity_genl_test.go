package nlparity

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tdGenl is the C2 generic netlink corpus: one directory per row, an ip.pcap of
// the command and a netlink_generic_<obj>.pcap of the show that followed it,
// each opening with ip's CTRL_CMD_GETFAMILY exchange.
const tdGenl = "../xtcpnl/testdata/7_1_4/link-write/genl"

// tdGenlFouAdd is `ip fou add port 5555 ipproto 4`: the controller exchange,
// then the fou ADD (id 41 in that boot) and its ACK.
const tdGenlFouAdd = tdGenl + "/fou_add/ip.pcap"

// genlFouID is the id the fou family had in the boot the C2 corpus was
// captured in.
const genlFouID = 41

// nla builds one netlink attribute, padded to four bytes.
func nla(attrType uint16, val []byte) []byte {
	n := xtcpnl.RTAttrSizeCst + len(val)
	b := make([]byte, n+xtcpnl.FourByteAlignPadding(n))
	binary.LittleEndian.PutUint16(b[0:2], uint16(n))
	binary.LittleEndian.PutUint16(b[2:4], attrType)
	copy(b[xtcpnl.RTAttrSizeCst:], val)
	return b
}

func u16le(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }
func u32le(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// genlBody is a genlmsghdr, version 1, followed by attributes.
func genlBody(cmd uint8, attrs ...[]byte) []byte {
	return concat(append([][]byte{{cmd, 1, 0, 0}}, attrs...)...)
}

// newFamily is the controller's CTRL_CMD_NEWFAMILY reply for a family with one
// multicast group.
func newFamily(id uint16, name string, grp uint32) []byte {
	group := nla(1|unix.NLA_F_NESTED, concat(
		nla(unix.CTRL_ATTR_MCAST_GRP_NAME, []byte("events\x00")),
		nla(unix.CTRL_ATTR_MCAST_GRP_ID, u32le(grp))))
	return genlBody(unix.CTRL_CMD_NEWFAMILY,
		nla(unix.CTRL_ATTR_FAMILY_ID, u16le(id)),
		nla(unix.CTRL_ATTR_FAMILY_NAME, []byte(name+"\x00")),
		nla(unix.CTRL_ATTR_MCAST_GROUPS|unix.NLA_F_NESTED, group))
}

// genlCapture wraps datagrams as NETLINK_GENERIC records of a DLT_NETLINK pcap.
func genlCapture(datagrams ...[]byte) []byte {
	recs := make([][]byte, 0, len(datagrams))
	for _, d := range datagrams {
		recs = append(recs, sllRecord(unix.NETLINK_GENERIC, d))
	}
	return pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, recs)
}

// remapGenl rewrites a capture as if its families had been given other ids:
// each message whose nlmsg_type is a key of ids, each controller reply's
// CTRL_ATTR_FAMILY_ID and each NLMSG_ERROR's echoed type. It reads with xtcpnl's
// pcap reader, not this package's, so the code under test does not build its
// own input.
func remapGenl(t *testing.T, data []byte, ids map[uint16]uint16) ([]byte, int) {
	t.Helper()
	_, records, err := xtcpnl.ParseNetlinkPcap(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	changed := 0
	swap := func(b []byte) {
		if to, ok := ids[binary.LittleEndian.Uint16(b)]; ok {
			binary.LittleEndian.PutUint16(b, to)
			changed++
		}
	}

	recs := make([][]byte, 0, len(records))
	for _, rec := range records {
		fam, body, perr := rec.NetlinkPayload()
		if perr != nil {
			t.Fatalf("record: %v", perr)
		}
		dg := append([]byte(nil), body...)
		for off := 0; fam == unix.NETLINK_GENERIC && off+xtcpnl.NlMsgHdrSizeCst <= len(dg); {
			n := int(binary.LittleEndian.Uint32(dg[off:]))
			if n < xtcpnl.NlMsgHdrSizeCst || off+n > len(dg) {
				break
			}
			msg := dg[off : off+n]
			typ := binary.LittleEndian.Uint16(msg[4:])
			flags := binary.LittleEndian.Uint16(msg[6:])
			swap(msg[4:6])
			payload := msg[xtcpnl.NlMsgHdrSizeCst:]
			switch {
			case typ == unix.NLMSG_ERROR && len(payload) >= nlmsgerrEchoTypeOffCst+2:
				swap(payload[nlmsgerrEchoTypeOffCst:])
			case typ == unix.GENL_ID_CTRL && flags&unix.NLM_F_REQUEST == 0 &&
				len(payload) > genlHdrLen && payload[0] == unix.CTRL_CMD_NEWFAMILY:
				attrs, _ := SubAttrs(payload[genlHdrLen:])
				for _, a := range attrs {
					if a.BareType() == unix.CTRL_ATTR_FAMILY_ID && len(a.Val) == 2 {
						swap(a.Val)
					}
				}
			}
			off += n + xtcpnl.FourByteAlignPadding(n)
		}
		recs = append(recs, sllRecord(fam, dg))
	}
	return pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, recs), changed
}

// segmentGenl parses and segments a NETLINK_GENERIC capture.
func segmentGenl(t *testing.T, data []byte) (Capture, Segmentation) {
	t.Helper()
	c, err := ParseCapture(data, unix.NETLINK_GENERIC)
	if err != nil {
		t.Fatalf("ParseCapture: %v", err)
	}
	return c, SegmentCapture(c)
}

// TestGenlCorpus walks every C2 capture as NETLINK_GENERIC. Each family's rows
// must label every message with that family or the controller, compare clean
// against themselves, and compare clean against a copy whose family was given
// another id, which is what a capture from a different boot looks like.
//
// go test ./pkg/nlparity/ -run TestGenlCorpus
func TestGenlCorpus(t *testing.T) {
	tests := []struct {
		description string
		prefix      string // row directory prefix
		family      string // the name the controller gave it
		rows        int
	}{
		{description: "positive: fou's rows", prefix: "fou_", family: "fou", rows: 21},
		{description: "positive: ila's rows", prefix: "ila_", family: "ila", rows: 15},
		{description: "positive: ioam's rows, an upper-case family name", prefix: "ioam_", family: "IOAM6", rows: 21},
		{description: "positive: l2tp's rows", prefix: "l2tp_", family: "l2tp", rows: 24},
		{description: "positive: macsec's rows", prefix: "macsec_", family: "macsec", rows: 32},
		{description: "positive: mptcp's rows, whose family is mptcp_pm", prefix: "mptcp_", family: "mptcp_pm", rows: 27},
		{description: "positive: sr's rows, whose family is SEG6", prefix: "sr_", family: "SEG6", rows: 11},
		{description: "positive: tcp_metrics' rows", prefix: "tcpm_", family: "tcp_metrics", rows: 14},
	}

	dirs, err := os.ReadDir(tdGenl)
	if err != nil {
		t.Fatalf("read %s: %v", tdGenl, err)
	}

	total := 0
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rows, labeled, remapped := 0, 0, 0
			for _, d := range dirs {
				if !strings.HasPrefix(d.Name(), tc.prefix) {
					continue
				}
				rows++
				pcaps, _ := filepath.Glob(filepath.Join(tdGenl, d.Name(), "*.pcap"))
				for _, p := range pcaps {
					l, r := checkGenlCapture(t, p, tc.family)
					labeled += l
					remapped += r
				}
			}
			total += rows
			if rows != tc.rows {
				t.Errorf("%d rows, want %d", rows, tc.rows)
			}
			// Not vacuous: the family's own messages were seen and moved.
			if labeled == 0 || remapped == 0 {
				t.Errorf("%d messages labeled %q and %d ids remapped, want both non-zero",
					labeled, tc.family, remapped)
			}
		})
	}
	if total != len(dirs) {
		t.Errorf("the rows cover %d of %d directories", total, len(dirs))
	}
}

// checkGenlCapture checks one corpus capture and returns how many messages
// carried family's label and how many ids the remap moved.
func checkGenlCapture(t *testing.T, path, family string) (labeled, remapped int) {
	t.Helper()
	c, err := ParseCaptureFile(path, unix.NETLINK_GENERIC)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	ids := map[uint16]uint16{}
	for id := range c.GenlFamilies {
		if id != unix.GENL_ID_CTRL {
			ids[id] = id + 7
		}
	}
	for _, m := range c.Msgs() {
		if !m.IsGenl() {
			continue
		}
		switch m.Genl {
		case family:
			labeled++
		case genlCtrlName:
		default:
			t.Errorf("%s: type %d labeled %q, want %q or %q",
				path, m.Hdr.Type, m.Genl, family, genlCtrlName)
		}
	}

	seg := SegmentCapture(c)
	if got := Diff(path, seg, seg); len(got) != 0 {
		t.Errorf("%s against itself: %v", path, findingKeys(got))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	moved, n := remapGenl(t, data, ids)
	_, mseg := segmentGenl(t, moved)
	if got := Diff(path, seg, mseg); len(got) != 0 {
		t.Errorf("%s against its remap %v: %v", path, ids, findingKeys(got))
	}
	return labeled, n
}

// TestGenlPair compares fou_add's capture with copies of it edited the way a
// different boot, or a different tool, would edit it.
//
// go test ./pkg/nlparity/ -run TestGenlPair
func TestGenlPair(t *testing.T) {
	data, err := os.ReadFile(tdGenlFouAdd)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	moved, _ := remapGenl(t, data, map[uint16]uint16{genlFouID: 60})
	// A collision: the id fou was given is RTM_NEWADDR's number on
	// NETLINK_ROUTE, so a type-keyed comparator would read it as one.
	onAddr, _ := remapGenl(t, data, map[uint16]uint16{genlFouID: unix.RTM_NEWADDR})

	// edit applies f to the sub side's message at index i of the flattened
	// capture, after parsing, so the label resolution has already run.
	type edit struct {
		msg int
		f   func(m *Msg)
	}
	setCmd := func(cmd byte) func(m *Msg) {
		return func(m *Msg) { m.Body = append([]byte{cmd}, m.Body[1:]...) }
	}

	tests := []struct {
		description string
		sub         []byte
		edits       []edit
		want        []string
	}{
		{
			description: "positive: the capture against itself",
			sub:         data,
			want:        []string{},
		},
		{
			description: "positive: fou given id 60 rather than 41 compares clean",
			sub:         moved,
			want:        []string{},
		},
		{
			description: "corner: fou given RTM_NEWADDR's number compares clean, and as genl",
			sub:         onAddr,
			want:        []string{},
		},
		{
			description: "negative: a different command at the same position is a presence finding",
			sub:         moved,
			edits:       []edit{{msg: 2, f: setCmd(2)}},
			want:        []string{"L2|presence|request:GENL(fou).cmd(1):do"},
		},
		{
			description: "negative: a different family at the same position is a presence finding",
			sub:         moved,
			edits:       []edit{{msg: 2, f: func(m *Msg) { m.Genl = "ila" }}},
			want:        []string{"L2|presence|request:GENL(fou).cmd(1):do"},
		},
		{
			description: "negative: a different flag is a value finding at the genl locus",
			sub:         moved,
			edits:       []edit{{msg: 2, f: func(m *Msg) { m.Hdr.Flags |= unix.NLM_F_ECHO }}},
			want:        []string{"L2|value|request:GENL(fou).cmd(1):header.flags:do"},
		},
		{
			description: "negative: a different genlmsghdr version is a family header finding",
			sub:         moved,
			edits: []edit{{msg: 2, f: func(m *Msg) {
				m.Body = append([]byte{m.Body[0], 9}, m.Body[2:]...)
			}}},
			want: []string{"L2|value|request:GENL(fou).cmd(1):familyhdr:do"},
		},
		{
			description: "negative: a different address family is a value finding on attr(2), not on an rtnetlink name",
			sub:         onAddr,
			edits: []edit{{msg: 2, f: func(m *Msg) {
				b := append([]byte(nil), m.Body...)
				_, attrs, _ := decodeBody(genlHdrLen, b)
				for _, a := range attrs {
					if a.BareType() == 2 { // FOU_ATTR_AF
						a.Val[0] ^= 0xff
					}
				}
				m.Body = b
			}}},
			want: []string{"L2|value|request:GENL(fou).cmd(1):attr(2):do"},
		},
		{
			// As on rtnetlink: an NLMSG_ERROR has no family header and its
			// errno is no attribute, so its body decodes to a remainder L3
			// does not compare. The exit status carries a refusal.
			description: "corner: a different errno in the ACK is no L3 finding",
			sub:         moved,
			edits: []edit{{msg: 3, f: func(m *Msg) {
				b := append([]byte(nil), m.Body...)
				b[0] = 0xff
				m.Body = b
			}}},
			want: []string{},
		},
		{
			description: "negative: a controller reply naming another family is a value finding",
			sub:         moved,
			edits: []edit{{msg: 1, f: func(m *Msg) {
				b := append([]byte(nil), m.Body...)
				_, attrs, _ := decodeBody(genlHdrLen, b)
				for _, a := range attrs {
					if a.BareType() == unix.CTRL_ATTR_FAMILY_NAME {
						a.Val[0] = 'g'
					}
				}
				m.Body = b
			}}},
			want: []string{"L3|value|reply:GENL(nlctrl).CTRL_CMD_NEWFAMILY:attr(2)"},
		},
	}

	_, ref := segmentGenl(t, data)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c, err := ParseCapture(tc.sub, unix.NETLINK_GENERIC)
			if err != nil {
				t.Fatalf("ParseCapture: %v", err)
			}
			msgs := c.Msgs()
			for _, e := range tc.edits {
				e.f(&msgs[e.msg])
			}
			got := findingKeys(Diff("fou add", ref, Segment(msgs)))
			if !sameStrings(got, tc.want) {
				t.Errorf("findings\n  got  %v\n  want %v", got, tc.want)
			}
		})
	}
}

// TestResolveGenl is the label resolution over constructed captures.
//
// go test ./pkg/nlparity/ -run TestResolveGenl
func TestResolveGenl(t *testing.T) {
	const req = unix.NLM_F_REQUEST | unix.NLM_F_ACK
	reply := func(body []byte) []byte { return wellFormedFrom(unix.GENL_ID_CTRL, 0, 1, 77, body) }
	cmd := func(id uint16) []byte { return wellFormed(id, req, genlBody(1)) }

	tests := []struct {
		description string
		input       []byte
		family      uint16
		wantErr     error
		wantLabels  []string // per genl message, in capture order
		wantFams    map[uint16]string
	}{
		{
			description: "positive: the reply names the command's family",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), cmd(30)),
			family:      unix.NETLINK_GENERIC,
			wantLabels:  []string{genlCtrlName, "fou"},
			wantFams:    map[uint16]string{unix.GENL_ID_CTRL: genlCtrlName, 30: "fou"},
		},
		{
			description: "corner: a command ahead of the reply that names it is labeled too",
			input:       genlCapture(cmd(30), reply(newFamily(30, "fou", 5))),
			family:      unix.NETLINK_GENERIC,
			wantLabels:  []string{"fou", genlCtrlName},
		},
		{
			description: "corner: a family no reply names stays unlabeled",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), cmd(31)),
			family:      unix.NETLINK_GENERIC,
			wantLabels:  []string{genlCtrlName, ""},
		},
		{
			description: "boundary: the same reply twice is no conflict",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), reply(newFamily(30, "fou", 6)), cmd(30)),
			family:      unix.NETLINK_GENERIC,
			wantLabels:  []string{genlCtrlName, genlCtrlName, "fou"},
		},
		{
			description: "negative: one id given two names",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), reply(newFamily(30, "ila", 5))),
			family:      unix.NETLINK_GENERIC,
			wantErr:     ErrGenlFamilyConflict,
		},
		{
			description: "negative: one name given two ids",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), reply(newFamily(31, "fou", 5))),
			family:      unix.NETLINK_GENERIC,
			wantErr:     ErrGenlFamilyConflict,
		},
		{
			description: "negative: the controller's own id renamed",
			input:       genlCapture(reply(newFamily(unix.GENL_ID_CTRL, "fou", 5))),
			family:      unix.NETLINK_GENERIC,
			wantErr:     ErrGenlFamilyConflict,
		},
		{
			description: "corner: a reply without a family id names nothing",
			input: genlCapture(reply(genlBody(unix.CTRL_CMD_NEWFAMILY,
				nla(unix.CTRL_ATTR_FAMILY_NAME, []byte("fou\x00")))), cmd(30)),
			family:     unix.NETLINK_GENERIC,
			wantLabels: []string{genlCtrlName, ""},
		},
		{
			description: "corner: a NEWFAMILY sent as a request names nothing",
			input: genlCapture(wellFormed(unix.GENL_ID_CTRL, unix.NLM_F_REQUEST,
				newFamily(30, "fou", 5)), cmd(30)),
			family:     unix.NETLINK_GENERIC,
			wantLabels: []string{genlCtrlName, ""},
		},
		{
			description: "boundary: an NLMSG_ERROR is not a genl message and carries no label",
			input:       genlCapture(reply(newFamily(30, "fou", 5)), wellFormedFrom(unix.NLMSG_ERROR, 0, 1, 77, zeros(20))),
			family:      unix.NETLINK_GENERIC,
			wantLabels:  []string{genlCtrlName},
		},
		{
			description: "negative: a NETLINK_ROUTE parse resolves nothing",
			input: pcapFile(xtcpnl.PcapLinkTypeNetlinkCst, [][]byte{
				sllRecord(unix.NETLINK_ROUTE, reply(newFamily(30, "fou", 5))),
			}),
			family:     unix.NETLINK_ROUTE,
			wantLabels: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c, err := ParseCapture(tc.input, tc.family)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			got := []string{}
			for _, m := range c.Msgs() {
				if m.IsGenl() {
					got = append(got, m.Genl)
				} else if m.Genl != "" {
					t.Errorf("type %d is not genl but labeled %q", m.Hdr.Type, m.Genl)
				}
			}
			if !equalStrings(got, tc.wantLabels) {
				t.Errorf("labels = %q, want %q", got, tc.wantLabels)
			}
			if tc.family != unix.NETLINK_GENERIC && c.GenlFamilies != nil {
				t.Errorf("GenlFamilies = %v on a route capture, want nil", c.GenlFamilies)
			}
			for id, name := range tc.wantFams {
				if c.GenlFamilies[id] != name {
					t.Errorf("GenlFamilies[%d] = %q, want %q", id, c.GenlFamilies[id], name)
				}
			}
		})
	}
}

// TestFamilyHdrLen is the family header length per protocol.
//
// go test ./pkg/nlparity/ -run TestFamilyHdrLen
func TestFamilyHdrLen(t *testing.T) {
	tests := []struct {
		description string
		proto       uint16
		msgType     uint16
		want        int
	}{
		{description: "positive: an rtnetlink link message is an ifinfomsg", proto: unix.NETLINK_ROUTE, msgType: unix.RTM_NEWLINK, want: xtcpnl.IfInfomsgSizeCst},
		{description: "positive: a genl family's message is a genlmsghdr", proto: unix.NETLINK_GENERIC, msgType: genlFouID, want: 4},
		{description: "corner: the controller's id, RTM_NEWLINK's number, is a genlmsghdr on genl", proto: unix.NETLINK_GENERIC, msgType: unix.GENL_ID_CTRL, want: 4},
		{description: "boundary: NLMSG_MIN_TYPE is the first genl type", proto: unix.NETLINK_GENERIC, msgType: unix.NLMSG_MIN_TYPE, want: 4},
		{description: "boundary: the type below it is a control type on genl too", proto: unix.NETLINK_GENERIC, msgType: unix.NLMSG_MIN_TYPE - 1, want: -1},
		{description: "positive: NLMSG_DONE on genl is the control type's", proto: unix.NETLINK_GENERIC, msgType: unix.NLMSG_DONE, want: xtcpnl.FamilyHdrLen(unix.NLMSG_DONE)},
		{description: "positive: NLMSG_ERROR on genl is the control type's", proto: unix.NETLINK_GENERIC, msgType: unix.NLMSG_ERROR, want: xtcpnl.FamilyHdrLen(unix.NLMSG_ERROR)},
		{description: "negative: an unmodeled rtnetlink type", proto: unix.NETLINK_ROUTE, msgType: 0x7fff, want: -1},
		{description: "negative: a protocol neither package models", proto: unix.NETLINK_SOCK_DIAG, msgType: unix.GENL_ID_CTRL, want: -1},
		{description: "positive: XFRM's NEWSA, RTM_NEWLINK's number, is an xfrm_usersa_info", proto: unix.NETLINK_XFRM, msgType: xtcpnl.XfrmMsgNewSA, want: xtcpnl.XfrmUsersaInfoSizeCst},
		{description: "positive: XFRM's GETSA, not a dump, is an xfrm_usersa_id", proto: unix.NETLINK_XFRM, msgType: xtcpnl.XfrmMsgGetSA, want: xtcpnl.XfrmUsersaIDSizeCst},
		{description: "negative: an XFRM type not modeled", proto: unix.NETLINK_XFRM, msgType: xtcpnl.XfrmMsgReport, want: -1},
		{description: "positive: NLMSG_DONE on XFRM is the control type's", proto: unix.NETLINK_XFRM, msgType: unix.NLMSG_DONE, want: xtcpnl.FamilyHdrLen(unix.NLMSG_DONE)},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := FamilyHdrLen(tc.proto, tc.msgType); got != tc.want {
				t.Errorf("FamilyHdrLen(%d, %d) = %d, want %d", tc.proto, tc.msgType, got, tc.want)
			}
		})
	}
}

// TestMsgName is the kind and locus name of a message.
//
// go test ./pkg/nlparity/ -run TestMsgName
func TestMsgName(t *testing.T) {
	genl := func(typ uint16, fam string, body []byte) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ}, Body: body, Proto: unix.NETLINK_GENERIC, Genl: fam}
	}
	tests := []struct {
		description string
		m           Msg
		want        string
	}{
		{description: "positive: a family's command", m: genl(genlFouID, "fou", genlBody(1)), want: "GENL(fou).cmd(1)"},
		{description: "positive: the controller's GETFAMILY", m: genl(unix.GENL_ID_CTRL, genlCtrlName, genlBody(unix.CTRL_CMD_GETFAMILY)), want: "GENL(nlctrl).CTRL_CMD_GETFAMILY"},
		{description: "positive: the controller's NEWFAMILY", m: genl(unix.GENL_ID_CTRL, genlCtrlName, genlBody(unix.CTRL_CMD_NEWFAMILY)), want: "GENL(nlctrl).CTRL_CMD_NEWFAMILY"},
		{description: "corner: another controller command keeps its number", m: genl(unix.GENL_ID_CTRL, genlCtrlName, genlBody(unix.CTRL_CMD_GETPOLICY)), want: "GENL(nlctrl).cmd(10)"},
		{description: "corner: an unresolved family is named by its id", m: genl(genlFouID, "", genlBody(1)), want: "GENL(id=41).cmd(1)"},
		{description: "boundary: a body one byte short of a genlmsghdr", m: genl(genlFouID, "fou", []byte{1, 1, 0}), want: "GENL(fou).short"},
		{description: "boundary: a bare genlmsghdr names its command", m: genl(genlFouID, "fou", []byte{3, 1, 0, 0}), want: "GENL(fou).cmd(3)"},
		{description: "negative: NLMSG_ERROR on genl is the control type", m: genl(unix.NLMSG_ERROR, "", zeros(20)), want: "NLMSG_ERROR"},
		{description: "negative: an rtnetlink message is its type", m: Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWLINK}}, want: "RTM_NEWLINK"},
		{description: "negative: an rtnetlink type with a genl id's number", m: Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID}}, want: "type(41)"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MsgName(tc.m); got != tc.want {
				t.Errorf("MsgName = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestGenlRoleAndAttrs covers the two places a genl family id would otherwise
// be read as an rtnetlink type: the request's role and its attributes' names.
//
// go test ./pkg/nlparity/ -run TestGenlRoleAndAttrs
func TestGenlRoleAndAttrs(t *testing.T) {
	tests := []struct {
		description string
		m           Msg
		attr        uint16
		wantRole    string
		wantAttr    string
	}{
		{
			description: "positive: a genl dump",
			m:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID, Flags: 0x301}, Proto: unix.NETLINK_GENERIC},
			attr:        1, wantRole: "dump", wantAttr: "attr(1)",
		},
		{
			description: "positive: a genl do with an ACK",
			m:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID, Flags: 0x5}, Proto: unix.NETLINK_GENERIC},
			attr:        1 | unix.NLA_F_NESTED, wantRole: "do", wantAttr: "attr(1)+NESTED",
		},
		{
			description: "corner: a genl id on RTM_NEWLINK's number is a do, not a write, and its attr(3) is no IFLA_IFNAME",
			m:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWLINK, Flags: 0x5}, Proto: unix.NETLINK_GENERIC},
			attr:        unix.IFLA_IFNAME, wantRole: "do", wantAttr: "attr(3)",
		},
		{
			description: "boundary: NLM_F_ROOT alone is not a genl dump",
			m:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID, Flags: 0x101}, Proto: unix.NETLINK_GENERIC},
			attr:        1, wantRole: "do", wantAttr: "attr(1)",
		},
		{
			description: "negative: RTM_NEWLINK on NETLINK_ROUTE stays a write with IFLA names",
			m:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWLINK, Flags: 0x5}},
			attr:        unix.IFLA_IFNAME, wantRole: "write", wantAttr: "IFLA_IFNAME",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := requestRole(tc.m); got != tc.wantRole {
				t.Errorf("requestRole = %q, want %q", got, tc.wantRole)
			}
			if got := msgAttrName(tc.m, tc.attr); got != tc.wantAttr {
				t.Errorf("msgAttrName = %q, want %q", got, tc.wantAttr)
			}
		})
	}
}

// TestNormalizeGenl is what a generic netlink exchange reports that the boot
// decided, zeroed, and what it leaves compared.
//
// go test ./pkg/nlparity/ -run TestNormalizeGenl
func TestNormalizeGenl(t *testing.T) {
	errBody := func(echoType uint16) []byte {
		b := zeros(20)
		binary.LittleEndian.PutUint16(b[nlmsgerrEchoTypeOffCst:], echoType)
		return b
	}
	fam := func(id uint16, name string, grp uint32) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.GENL_ID_CTRL}, Body: newFamily(id, name, grp), Proto: unix.NETLINK_GENERIC}
	}

	tests := []struct {
		description string
		a, b        Msg
		wantEqual   bool
	}{
		{
			description: "positive: two ACKs of the same request under different family ids",
			a:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: errBody(41), Proto: unix.NETLINK_GENERIC},
			b:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: errBody(60), Proto: unix.NETLINK_GENERIC},
			wantEqual:   true,
		},
		{
			description: "negative: on NETLINK_ROUTE the echoed type stays compared",
			a:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: errBody(unix.RTM_NEWLINK)},
			b:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: errBody(unix.RTM_NEWADDR)},
			wantEqual:   false,
		},
		{
			description: "positive: one family's controller reply under two ids and group ids",
			a:           fam(41, "fou", 5),
			b:           fam(60, "fou", 9),
			wantEqual:   true,
		},
		{
			description: "negative: two families' controller replies stay different",
			a:           fam(41, "fou", 5),
			b:           fam(41, "ila", 5),
			wantEqual:   false,
		},
		{
			description: "corner: a GETFAMILY carrying a family id is a request's value and stays compared",
			a: Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.GENL_ID_CTRL}, Proto: unix.NETLINK_GENERIC,
				Body: genlBody(unix.CTRL_CMD_GETFAMILY, nla(unix.CTRL_ATTR_FAMILY_ID, u16le(41)))},
			b: Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.GENL_ID_CTRL}, Proto: unix.NETLINK_GENERIC,
				Body: genlBody(unix.CTRL_CMD_GETFAMILY, nla(unix.CTRL_ATTR_FAMILY_ID, u16le(60)))},
			wantEqual: false,
		},
		{
			// IFA_CACHEINFO's number on RTM_NEWADDR's, eight bytes in where
			// an ifaddrmsg would end, and different only where its
			// timestamps would be: a type-keyed normalization would blank
			// them.
			description: "corner: a genl attribute on an rtnetlink volatile's numbers stays compared",
			a: Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWADDR}, Proto: unix.NETLINK_GENERIC, Genl: "fou",
				Body: genlBody(1, nla(7, nil), nla(unix.IFA_CACHEINFO, fill(16, 0x11)))},
			b: Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWADDR}, Proto: unix.NETLINK_GENERIC, Genl: "fou",
				Body: genlBody(1, nla(7, nil), nla(unix.IFA_CACHEINFO, concat(fill(8, 0x11), fill(8, 0x22))))},
			wantEqual: false,
		},
		{
			description: "boundary: a controller reply cut inside its genlmsghdr is left alone",
			a:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.GENL_ID_CTRL}, Body: []byte{1, 2, 0}, Proto: unix.NETLINK_GENERIC},
			b:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.GENL_ID_CTRL}, Body: []byte{1, 3, 0}, Proto: unix.NETLINK_GENERIC},
			wantEqual:   false,
		},
		{
			description: "boundary: an ACK too short for its echoed type is left alone",
			a:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: []byte{0, 0, 0, 0, 0, 0, 0, 0, 41}, Proto: unix.NETLINK_GENERIC},
			b:           Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_ERROR}, Body: []byte{0, 0, 0, 0, 0, 0, 0, 0, 60}, Proto: unix.NETLINK_GENERIC},
			wantEqual:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			before := append([]byte(nil), tc.a.Body...)
			na, nb := NormalizeMsg(tc.a), NormalizeMsg(tc.b)
			if got := string(na.Body) == string(nb.Body); got != tc.wantEqual {
				t.Errorf("equal after normalizing = %v, want %v\n  a %x\n  b %x", got, tc.wantEqual, na.Body, nb.Body)
			}
			if string(tc.a.Body) != string(before) {
				t.Errorf("NormalizeMsg edited its input")
			}
		})
	}
}

// TestGenlReplyKeys is the ordinal keying of genl replies, which is what makes
// one more dump entry a finding.
//
// go test ./pkg/nlparity/ -run TestGenlReplyKeys
func TestGenlReplyKeys(t *testing.T) {
	entry := func(port byte) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID, Flags: unix.NLM_F_MULTI}, Proto: unix.NETLINK_GENERIC,
			Genl: "fou", Body: genlBody(1, nla(1, []byte{port, 0}))}
	}
	done := Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.NLMSG_DONE}, Body: zeros(4), Proto: unix.NETLINK_GENERIC}
	req := Msg{Hdr: xtcpnl.NlMsgHdr{Type: genlFouID, Flags: 0x301}, Proto: unix.NETLINK_GENERIC, Genl: "fou", Body: genlBody(3)}
	txn := func(replies ...Msg) Segmentation {
		return Segmentation{Txns: []Txn{{Request: req, Replies: replies, State: TxnClosedByDone}}}
	}

	tests := []struct {
		description string
		ref, sub    Segmentation
		want        []string
	}{
		{
			description: "positive: the same three entries",
			ref:         txn(entry(1), entry(2), entry(3), done),
			sub:         txn(entry(1), entry(2), entry(3), done),
			want:        []string{},
		},
		{
			description: "negative: one entry fewer is a key-set finding",
			ref:         txn(entry(1), entry(2), entry(3), done),
			sub:         txn(entry(1), entry(2), done),
			want:        []string{"L3|key-set|reply:key-set"},
		},
		{
			description: "negative: a different second entry is a value finding",
			ref:         txn(entry(1), entry(2), done),
			sub:         txn(entry(1), entry(9), done),
			want:        []string{"L3|value|reply:GENL(fou).cmd(1):attr(1)"},
		},
		{
			description: "boundary: an empty dump against an empty dump",
			ref:         txn(done),
			sub:         txn(done),
			want:        []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := findingKeys(Diff("fou show", tc.ref, tc.sub))
			if !sameStrings(got, tc.want) {
				t.Errorf("findings\n  got  %v\n  want %v", got, tc.want)
			}
		})
	}
}

// TestProtocolByName is the protocol names xtcp2-nlcap's --proto takes.
//
// go test ./pkg/nlparity/ -run TestProtocolByName
func TestProtocolByName(t *testing.T) {
	tests := []struct {
		description string
		name        string
		want        uint16
		wantOK      bool
	}{
		{description: "positive: route", name: "route", want: unix.NETLINK_ROUTE, wantOK: true},
		{description: "positive: generic", name: "generic", want: unix.NETLINK_GENERIC, wantOK: true},
		{description: "positive: xfrm", name: "xfrm", want: unix.NETLINK_XFRM, wantOK: true},
		{description: "negative: an unknown name", name: "sock_diag", wantOK: false},
		{description: "boundary: the empty name is no protocol", name: "", wantOK: false},
		{description: "corner: names are case-sensitive, as the helper's are", name: "Generic", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := ProtocolByName(tc.name)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ProtocolByName(%q) = %d, %v; want %d, %v", tc.name, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
