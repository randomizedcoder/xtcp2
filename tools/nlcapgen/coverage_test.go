package main

import (
	"encoding/binary"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func TestUapiEnum(t *testing.T) {
	cases := []struct {
		description string
		src         string
		prefix      string
		expected    string // name=value pairs for the names asked, or the error's text
		names       []string
	}{
		{"positive: entries count from 0", "enum {\n\tX_UNSPEC,\n\tX_A,\n\tX_B,\n\t__X_MAX\n};", "X_", "X_UNSPEC=0 X_A=1 X_B=2", []string{"X_UNSPEC", "X_A", "X_B"}},
		{"positive: comments and #defines are skipped", "enum {\n\tX_UNSPEC,\n\tX_A, /* a */\n#define X_A X_A\n\tX_B, // b\n};", "X_", "X_A=1 X_B=2", []string{"X_A", "X_B"}},
		{"corner: a number resets the count", "enum {\n\tX_UNSPEC,\n\tX_A = 10,\n\tX_B,\n};", "X_", "X_A=10 X_B=11", []string{"X_A", "X_B"}},
		{"corner: an alias takes an earlier entry's number", "enum {\n\tX_UNSPEC,\n\tX_PAD = X_UNSPEC,\n\tX_A,\n};", "X_", "X_PAD=0 X_A=1", []string{"X_PAD", "X_A"}},
		{"boundary: the MAX sentinel is left out", "enum {\n\tX_UNSPEC,\n\t__X_MAX\n};", "X_", "__X_MAX=", []string{"__X_MAX"}},
		{"corner: a longer prefix's enum is not taken", "enum {\n\tX_SUB_UNSPEC,\n\tX_SUB_A,\n};\nenum {\n\tX_UNSPEC,\n\tX_A,\n};", "X_", "X_A=1 X_SUB_A=", []string{"X_A", "X_SUB_A"}},
		{"negative: no such enum", "enum {\n\tY_UNSPEC,\n};", "X_", "no enum starting X_UNSPEC", nil},
		{"negative: a value that is neither", "enum {\n\tX_UNSPEC,\n\tX_A = Y_B,\n};", "X_", "neither a number", nil},
		{"positive: the YNL MAX, a sentinel less one", "enum {\n\tX_UNSPEC,\n\tX_A,\n\n\t__X_CNT,\n\tX_MAX = (__X_CNT - 1)\n};", "X_",
			"X_A=1 X_MAX=1 __X_CNT=", []string{"X_A", "X_MAX", "__X_CNT"}},
		{"corner: an earlier entry plus a number, unparenthesized", "enum {\n\tX_UNSPEC,\n\tX_A = X_UNSPEC + 5,\n\tX_B,\n};", "X_",
			"X_A=5 X_B=6", []string{"X_A", "X_B"}},
		{"negative: a later entry less one", "enum {\n\tX_UNSPEC,\n\tX_A = (X_B - 1),\n\tX_B,\n};", "X_", "neither a number", nil},
		{"negative: a product", "enum {\n\tX_UNSPEC,\n\tX_A = X_UNSPEC * 2,\n};", "X_", "neither a number", nil},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := uapiEnum(tc.src, tc.prefix)
			if err != nil {
				if tc.names != nil || !strings.Contains(err.Error(), tc.expected) {
					t.Fatalf("err %v, want %q", err, tc.expected)
				}
				return
			}
			parts := make([]string, 0, len(tc.names))
			for _, n := range tc.names {
				v, ok := got[n]
				s := n + "="
				if ok {
					s += strconv.Itoa(v)
				}
				parts = append(parts, s)
			}
			if s := strings.Join(parts, " "); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

func TestPolicyNames(t *testing.T) {
	src := "static const struct nla_policy p[X_MAX + 1] = {\n" +
		"\t[X_UNSPEC] = { .strict_start_type = X_B + 1 },\n" +
		"\t[X_A] = { .type = NLA_U32 }, /* [X_C] = in a comment */\n" +
		"\t[X_B] = NLA_POLICY_MAX(NLA_U8, 3),\n" +
		"};\nstatic const struct nla_policy q[] = {\n\t[X_D] = { .type = NLA_U8 },\n};\n"
	cases := []struct {
		description string
		name        string
		expected    string
		ok          bool
	}{
		{"positive: every designator but UNSPEC, not those in comments", "p", "X_A X_B", true},
		{"positive: an unsized table", "q", "X_D", true},
		{"negative: a table the source doesn't define", "r", "", false},
		{"corner: a name that only prefixes a table's", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			names, ok, err := policyNames(src, tc.name)
			if err != nil || ok != tc.ok || strings.Join(names, " ") != tc.expected {
				t.Errorf("got %v %v %v, want %q %v", names, ok, err, tc.expected, tc.ok)
			}
		})
	}
	if _, _, err := policyNames("struct nla_policy p[] = {\n\t[X_A] = { .type = NLA_U8 },\n", "p"); err == nil {
		t.Error("an unclosed table: want an error")
	}
}

func TestFindPolicy(t *testing.T) {
	pol := "static const struct nla_policy p[] = {\n\t[X_A] = { .type = NLA_U8 },\n};\n"
	cases := []struct {
		description string
		fsys        fstest.MapFS
		expected    string
	}{
		{"positive: the one file that defines it", fstest.MapFS{"net/a.c": {Data: []byte(pol)}, "net/b.c": {Data: []byte("int x;")}}, "X_A"},
		{"negative: defined twice", fstest.MapFS{"net/a.c": {Data: []byte(pol)}, "net/b.c": {Data: []byte(pol)}}, "defined in 2 files"},
		{"negative: defined nowhere", fstest.MapFS{"net/b.c": {Data: []byte("int x;")}}, "defined in 0 files"},
		{"corner: a header is not read", fstest.MapFS{"net/a.h": {Data: []byte(pol)}, "net/b.c": {Data: []byte("int x;")}}, "defined in 0 files"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			names, err := findPolicy(tc.fsys, "net", "p")
			got := strings.Join(names, " ")
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// covMsg is a request of type typ: a family header of hdrLen bytes whose
// first byte is family, then one attribute of each type in attrs.
func covMsg(typ uint16, flags uint16, family byte, hdrLen int, attrs ...uint16) nlparity.Msg {
	body := make([]byte, hdrLen)
	body[0] = family
	for _, a := range attrs {
		body = binary.LittleEndian.AppendUint16(body, 8)
		body = binary.LittleEndian.AppendUint16(body, a)
		body = append(body, 0, 0, 0, 0)
	}
	return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags}, Body: body}
}

func TestCovTally(t *testing.T) {
	req := uint16(unix.NLM_F_REQUEST)
	cases := []struct {
		description string
		msg         nlparity.Msg
		expected    string // family:requests:attrs
	}{
		{"positive: an IPv4 route add counts its attributes", covMsg(unix.RTM_NEWROUTE, req, unix.AF_INET, 12, unix.RTA_DST, unix.RTA_OIF), "route4:1:1,4"},
		{"positive: an IPv6 one counts for route6", covMsg(unix.RTM_DELROUTE, req, unix.AF_INET6, 12, unix.RTA_GATEWAY), "route6:1:5"},
		{"corner: a nested flag is masked off", covMsg(unix.RTM_NEWROUTE, req, unix.AF_INET, 12, unix.RTA_METRICS|unix.NLA_F_NESTED), "route4:1:8"},
		{"positive: a neighbor of any family", covMsg(unix.RTM_NEWNEIGH, req, unix.AF_INET6, 12, unix.NDA_DST), "neigh:1:1"},
		{"negative: a bridge fdb add is judged by no policy", covMsg(unix.RTM_NEWNEIGH, req, unix.AF_BRIDGE, 12, unix.NDA_VLAN), ":0:/1"},
		{"negative: nor is a bridge fdb delete", covMsg(unix.RTM_DELNEIGH, req, unix.AF_BRIDGE, 12, unix.NDA_LLADDR), ":0:/1"},
		{"corner: a bridge fdb get is judged by the neighbor policy", covMsg(unix.RTM_GETNEIGH, req, unix.AF_BRIDGE, 12, unix.NDA_VLAN), "neigh:1:5"},
		{"negative: a reply is not a request", covMsg(unix.RTM_NEWROUTE, 0, unix.AF_INET, 12, unix.RTA_DST), ":0:"},
		{"positive: a route get is judged by the same policy", covMsg(unix.RTM_GETROUTE, req, unix.AF_INET, 12, unix.RTA_IIF), "route4:1:3"},
		{"negative: a rule dump is no family's", covMsg(unix.RTM_GETRULE, req, unix.AF_INET, 12, unix.FRA_TABLE), ":0:"},
		{"corner: an AF_UNSPEC address dump is neither family's", covMsg(unix.RTM_GETADDR, req, unix.AF_UNSPEC, 8), ":0:"},
		{"negative: a nexthop delete is judged by another policy", covMsg(unix.RTM_DELNEXTHOP, req, 0, 8, unix.NHA_ID), ":0:"},
		{"boundary: a body shorter than its header", covMsg(unix.RTM_NEWLINK, req, 0, 12), ":0:"},
		{"positive: an altname add is judged by the link policy", covMsg(unix.RTM_NEWLINKPROP, req, 0, 16, unix.IFLA_PROP_LIST|unix.NLA_F_NESTED), "link:1:52"},
		{"negative: a link property get is no family's", covMsg(unix.RTM_GETLINKPROP, req, 0, 16, unix.IFLA_IFNAME), ":0:"},
		{"corner: an odd-length attribute is padded to four bytes before the next", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWROUTE, Flags: req},
			Body: append(append([]byte{unix.AF_INET, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 5, 0, unix.RTA_PRIORITY, 0, 9, 0, 0, 0), 8, 0, unix.RTA_OIF, 0, 2, 0, 0, 0)}, "route4:1:4,6"},
		{"corner: an attribute overrunning the body ends the walk", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWADDR, Flags: req},
			Body: append(append(make([]byte, 0, 16), unix.AF_INET, 0, 0, 0, 0, 0, 0, 0), 40, 0, 2, 0)}, "addr4:1:"},
		{"positive: an mdb add counts its attributes, and its entry nest's for mdbe", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWMDB, Flags: req},
			Body: []byte{unix.AF_BRIDGE, 0, 0, 0, 4, 0, 0, 0, 4, 0, 1, 0, 12, 0, 2, 0x80, 4, 0, 1, 0, 4, 0, 5, 0}}, "mdb:1:1,2;mdbe:1:1,5"},
		{"negative: an mdb delete without the nest is no mdbe request", covMsg(unix.RTM_DELMDB, req, unix.AF_BRIDGE, 8, 1), "mdb:1:1"},
		{"corner: an mdb write of another family is no family's", covMsg(unix.RTM_NEWMDB, req, unix.AF_INET, 8, 1), ":0:"},
		{"negative: an mdb get is judged by another policy", covMsg(unix.RTM_GETMDB, req, unix.AF_BRIDGE, 8, 1), ":0:"},
		{"boundary: a nest that overruns the body is not found", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWMDB, Flags: req},
			Body: []byte{unix.AF_BRIDGE, 0, 0, 0, 4, 0, 0, 0, 12, 0, 2, 0, 4, 0, 1, 0}}, "mdb:1:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			tl := newCovTally()
			tl.add(tc.msg)
			fams := slices.Sorted(maps.Keys(tl.requests))
			if len(fams) == 0 {
				fams = []string{""}
			}
			parts := make([]string, 0, len(fams))
			for _, fam := range fams {
				var attrs []string
				for a := 0; a < 64; a++ {
					if tl.sent[fam][a] > 0 {
						attrs = append(attrs, strconv.Itoa(a))
					}
				}
				parts = append(parts, fam+":"+strconv.Itoa(tl.requests[fam])+":"+strings.Join(attrs, ","))
			}
			got := strings.Join(parts, ";")
			if tl.unjudged > 0 {
				got += "/" + strconv.Itoa(tl.unjudged)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// covDiagMsg is an inet SOCK_DIAG_BY_FAMILY request with bytecode bc.
func covDiagMsg(flags uint16, family uint8, bc ...byte) nlparity.Msg {
	body := make([]byte, covInetDiagReqSize)
	body[0] = family
	if bc != nil {
		body = binary.LittleEndian.AppendUint16(body, uint16(4+len(bc)))
		body = binary.LittleEndian.AppendUint16(body, 1)
		body = append(body, bc...)
	}
	return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: covSockDiagByFamily, Flags: flags}, Body: body, Proto: unix.NETLINK_SOCK_DIAG}
}

func TestCovAddDiag(t *testing.T) {
	req := uint16(unix.NLM_F_REQUEST)
	sportGe := []byte{2, 8, 12, 0, 0, 0, 22, 0}
	auto := []byte{6, 4, 8, 0}
	cases := []struct {
		description string
		msg         nlparity.Msg
		expected    string // requests:op codes
	}{
		{"positive: a dump with no bytecode counts the request", covDiagMsg(req, unix.AF_INET), "1:"},
		{"positive: each op once, walked by yes", covDiagMsg(req, unix.AF_INET6, append(append([]byte{}, sportGe...), auto...)...), "1:2,6"},
		{"negative: a reply is not a request", covDiagMsg(0, unix.AF_INET, auto...), "0:"},
		{"negative: a unix diag request is not inet", covDiagMsg(req, unix.AF_UNIX, auto...), "0:"},
		{"boundary: a body shorter than inet_diag_req_v2", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: covSockDiagByFamily, Flags: req}, Body: []byte{unix.AF_INET}}, "0:"},
		{"corner: a yes under 4 ends the walk", covDiagMsg(req, unix.AF_INET, 6, 4, 8, 0, 7, 0, 8, 0), "1:6"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			tl := newCovTally()
			tl.addDiag(tc.msg)
			ops := make([]string, 0, len(tl.diagOps))
			for _, code := range slices.Sorted(maps.Keys(tl.diagOps)) {
				ops = append(ops, strconv.Itoa(code))
			}
			if got := strconv.Itoa(tl.diag) + ":" + strings.Join(ops, ","); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestTallyCorpusSockDiag reads real ws rows: an ss row's sock_diag
// requests reach addDiag, a route row's do not.
func TestTallyCorpusSockDiag(t *testing.T) {
	ws := "../../pkg/xtcpnl/testdata/7_3_0/ws/"
	cases := []struct {
		description string
		row         string
		expected    string // diag requests:op codes
	}{
		{"positive: ss -u -4 sport = :22 is one request, one S_COND", "ss/ec-002", "1:7"},
		{"positive: ss -t -4 not sport = :22 adds the JMP", "ss/pw-054", "1:1,7"},
		{"negative: a route row sends no sock_diag", "route4/bd-001", "0:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			b, err := os.ReadFile(ws + tc.row + "/ip.pcap")
			if err != nil {
				t.Fatal(err)
			}
			tl, files, err := tallyCorpus(fstest.MapFS{"r/ip.pcap": {Data: b}})
			if err != nil || files != 1 {
				t.Fatalf("files %d, err %v", files, err)
			}
			ops := make([]string, 0, len(tl.diagOps))
			for _, code := range slices.Sorted(maps.Keys(tl.diagOps)) {
				ops = append(ops, strconv.Itoa(code))
			}
			if got := strconv.Itoa(tl.diag) + ":" + strings.Join(ops, ","); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// covAttr is one netlink attribute, padded to 4 bytes.
func covAttr(typ uint16, val []byte) []byte {
	b := binary.LittleEndian.AppendUint16(nil, uint16(4+len(val)))
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = append(b, val...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// covGenlMsg is a generic netlink message of family id typ and command cmd.
func covGenlMsg(typ, flags uint16, cmd uint8, attrs ...[]byte) nlparity.Msg {
	body := []byte{cmd, 1, 0, 0}
	for _, a := range attrs {
		body = append(body, a...)
	}
	return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags}, Body: body, Proto: unix.NETLINK_GENERIC}
}

// covNewFamily is a CTRL_CMD_NEWFAMILY reply giving name the id id.
func covNewFamily(id uint16, name string) nlparity.Msg {
	return covGenlMsg(covGenlCtrl, 0, covCtrlNewFamily,
		covAttr(covCtrlFamilyName, append([]byte(name), 0)),
		covAttr(covCtrlFamilyID, binary.LittleEndian.AppendUint16(nil, id)))
}

func TestCovGenlIDs(t *testing.T) {
	req := uint16(unix.NLM_F_REQUEST)
	cases := []struct {
		description string
		msgs        []nlparity.Msg
		expected    map[uint16]string
	}{
		{"positive: a NEWFAMILY reply names its id", []nlparity.Msg{covNewFamily(0x15, "ethtool")}, map[uint16]string{0x15: "ethtool"}},
		{"positive: two replies, two names", []nlparity.Msg{covNewFamily(0x15, "ethtool"), covNewFamily(0x16, "devlink")},
			map[uint16]string{0x15: "ethtool", 0x16: "devlink"}},
		{"negative: a GETFAMILY request names nothing", []nlparity.Msg{covGenlMsg(covGenlCtrl, req, 3,
			covAttr(covCtrlFamilyName, []byte("ethtool\x00")), covAttr(covCtrlFamilyID, []byte{0x15, 0}))}, map[uint16]string{}},
		{"negative: a request carrying NEWFAMILY's command", []nlparity.Msg{func() nlparity.Msg {
			m := covNewFamily(0x15, "ethtool")
			m.Hdr.Flags = req
			return m
		}()}, map[uint16]string{}},
		{"negative: a reply of another family is not CTRL's", []nlparity.Msg{func() nlparity.Msg {
			m := covNewFamily(0x15, "ethtool")
			m.Hdr.Type = 0x15
			return m
		}()}, map[uint16]string{}},
		{"negative: a CTRL reply of another command", []nlparity.Msg{covGenlMsg(covGenlCtrl, 0, 2,
			covAttr(covCtrlFamilyName, []byte("ethtool\x00")), covAttr(covCtrlFamilyID, []byte{0x15, 0}))}, map[uint16]string{}},
		{"boundary: a reply without a name", []nlparity.Msg{covGenlMsg(covGenlCtrl, 0, covCtrlNewFamily,
			covAttr(covCtrlFamilyID, []byte{0x15, 0}))}, map[uint16]string{}},
		{"boundary: a body shorter than genlmsghdr", []nlparity.Msg{{Hdr: xtcpnl.NlMsgHdr{Type: covGenlCtrl}, Body: []byte{1, 1},
			Proto: unix.NETLINK_GENERIC}}, map[uint16]string{}},
		{"corner: an id of the wrong width is ignored", []nlparity.Msg{covGenlMsg(covGenlCtrl, 0, covCtrlNewFamily,
			covAttr(covCtrlFamilyName, []byte("ethtool\x00")), covAttr(covCtrlFamilyID, []byte{0x15, 0, 0, 0}))}, map[uint16]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := covGenlIDs(tc.msgs); !maps.Equal(got, tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestCovAddGenl(t *testing.T) {
	req := uint16(unix.NLM_F_REQUEST)
	ids := map[uint16]string{0x15: "ethtool", 0x16: "devlink"}
	rx := covAttr(6, []byte{1, 0, 0, 0})
	tx := covAttr(9, []byte{1, 0, 0, 0})
	cases := []struct {
		description string
		msg         nlparity.Msg
		expected    string // key requests:attr types
	}{
		{"positive: RINGS_SET counts under ethtool-rings", covGenlMsg(0x15, req, 16, rx, tx), "ethtool-rings 1:6,9"},
		{"positive: COALESCE_SET counts under ethtool-coalesce", covGenlMsg(0x15, req, 20, rx), "ethtool-coalesce 1:6"},
		{"negative: a reply is not a request", covGenlMsg(0x15, 0, 16, rx), ""},
		{"negative: a family id the capture never named", covGenlMsg(0x17, req, 16, rx), ""},
		{"negative: another genl family's command 16", covGenlMsg(0x16, req, 16, rx), ""},
		{"negative: a get no policy judges", covGenlMsg(0x15, req, 15, rx), ""},
		{"negative: rtnetlink with an ethtool-looking type", func() nlparity.Msg {
			m := covGenlMsg(0x15, req, 16, rx)
			m.Proto = unix.NETLINK_ROUTE
			return m
		}(), ""},
		{"boundary: a body shorter than genlmsghdr", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: 0x15, Flags: req}, Body: []byte{16, 1, 0},
			Proto: unix.NETLINK_GENERIC}, ""},
		{"boundary: a set with no attributes still counts", covGenlMsg(0x15, req, 18), "ethtool-channels 1:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			tl := newCovTally()
			tl.addGenl(tc.msg, ids)
			keys := slices.Sorted(maps.Keys(tl.requests))
			got := make([]string, 0, len(keys))
			for _, key := range keys {
				types := make([]string, 0, len(tl.sent[key]))
				for _, typ := range slices.Sorted(maps.Keys(tl.sent[key])) {
					types = append(types, strconv.Itoa(typ))
				}
				got = append(got, key+" "+strconv.Itoa(tl.requests[key])+":"+strings.Join(types, ","))
			}
			if s := strings.Join(got, ";"); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

// TestTallyCorpusGenl reads real ws rows: an ethtool -G row's RINGS_SET
// reaches addGenl through the capture's CTRL reply, a route row's
// requests do not.
func TestTallyCorpusGenl(t *testing.T) {
	ws := "../../pkg/xtcpnl/testdata/7_3_0/ws/"
	cases := []struct {
		description string
		row         string
		expected    string // ethtool-rings requests:attr types
	}{
		{"positive: ethtool -G with six arguments and the header", "ethring/ec-002", "1:1,6,7,8,9,11,17"},
		{"negative: a route row sends no generic netlink", "route4/bd-001", "0:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			b, err := os.ReadFile(ws + tc.row + "/ip.pcap")
			if err != nil {
				t.Fatal(err)
			}
			tl, files, err := tallyCorpus(fstest.MapFS{"r/ip.pcap": {Data: b}})
			if err != nil || files != 1 {
				t.Fatalf("files %d, err %v", files, err)
			}
			types := make([]string, 0, len(tl.sent["ethtool-rings"]))
			for _, typ := range slices.Sorted(maps.Keys(tl.sent["ethtool-rings"])) {
				types = append(types, strconv.Itoa(typ))
			}
			if got := strconv.Itoa(tl.requests["ethtool-rings"]) + ":" + strings.Join(types, ","); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestWriteCoverage(t *testing.T) {
	pol := covPolicy{fam: covFamily{Key: "route4", Policy: "rtm_ipv4_policy", Dir: "net/ipv4"},
		names: map[int]string{1: "RTA_DST", 4: "RTA_OIF", 5: "RTA_GATEWAY"},
		enum:  map[int]string{1: "RTA_DST", 4: "RTA_OIF", 5: "RTA_GATEWAY", 18: "RTA_VIA"}}
	tl := newCovTally()
	tl.add(covMsg(unix.RTM_NEWROUTE, unix.NLM_F_REQUEST, unix.AF_INET, 12, unix.RTA_DST, 18, 99))
	tl.add(covMsg(unix.RTM_NEWNEIGH, unix.NLM_F_REQUEST, unix.AF_BRIDGE, 12, unix.NDA_VLAN))
	tl.addDiag(covDiagMsg(unix.NLM_F_REQUEST, unix.AF_INET, 6, 4, 8, 0))
	var b strings.Builder
	if err := writeCoverage(&b, []string{"h"}, []covPolicy{pol}, tl); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	cases := []struct {
		description string
		expected    string
	}{
		{"positive: the share sent", "# route4: 1/3 (1 requests), from rtm_ipv4_policy in net/ipv4\n"},
		{"positive: the never-sent list, in number order", "route4 never sent: RTA_OIF RTA_GATEWAY\n"},
		{"corner: an attribute outside the policy by name, or by number when unnamed", "route4 sent outside the policy: RTA_VIA 99\n"},
		{"positive: the header", "# h\n"},
		{"corner: a bridge fdb write is counted apart", "# bridge fdb writes, which no policy judges: 1 requests\n"},
		{"positive: the inet_diag ops' share", "# inet_diag dumps, which inet_diag_bc_audit judges: 1/14 bytecode ops (1 requests)\n"},
		{"positive: the ops never sent, in code order", "inet_diag bytecode ops never sent: NOP JMP S_GE S_LE D_GE D_LE S_COND"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if !strings.Contains(out, tc.expected) {
				t.Errorf("report:\n%s\nwant it to contain %q", out, tc.expected)
			}
		})
	}
	tl = newCovTally()
	b.Reset()
	if err := writeCoverage(&b, nil, []covPolicy{pol}, tl); err != nil || !strings.Contains(b.String(), "route4 sent outside the policy: none") {
		t.Errorf("boundary: no requests: %v\n%s", err, b.String())
	}
}

func TestLoadPolicies(t *testing.T) {
	fsys := fstest.MapFS{}
	fams := covAllFamilies()
	for i := range fams {
		fam := &fams[i]
		hdr := "enum {\n\t" + fam.Prefix + "UNSPEC,\n\t" + fam.Prefix + "A,\n\t" + fam.Prefix + "B,\n};\n"
		fsys[uapiDir+"/"+strings.ToLower(strings.TrimSuffix(fam.Prefix, "_"))+".h"] = &fstest.MapFile{Data: []byte(hdr)}
		pol := "static const struct nla_policy " + fam.Policy + "[] = {\n\t[" + fam.Prefix + "B] = { .type = NLA_U8 },\n};\n"
		fsys[fam.Dir+"/"+fam.Key+".c"] = &fstest.MapFile{Data: []byte(pol)}
	}
	pols, err := loadPolicies(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(pols) != len(fams) || pols[0].names[2] != covFamilies[0].Prefix+"B" || len(pols[0].names) != 1 {
		t.Errorf("got %+v", pols[0])
	}
	bad := fstest.MapFS{}
	for k, v := range fsys {
		bad[k] = v
	}
	bad[covFamilies[0].Dir+"/"+covFamilies[0].Key+".c"] = &fstest.MapFile{Data: []byte("static const struct nla_policy " +
		covFamilies[0].Policy + "[] = {\n\t[NOT_NUMBERED] = { .type = NLA_U8 },\n};\n")}
	if _, err := loadPolicies(bad); err == nil || !strings.Contains(err.Error(), "does not number") {
		t.Errorf("negative: a policy naming an attribute the enum lacks: %v", err)
	}
}

func TestFindEnum(t *testing.T) {
	enum := func(prefix string) []byte { return []byte("enum {\n\t" + prefix + "UNSPEC,\n\t" + prefix + "A,\n};\n") }
	cases := []struct {
		description string
		fsys        fstest.MapFS
		expected    string // the header found, or the error's text
	}{
		{"positive: the one header that starts it", fstest.MapFS{"inc/a.h": {Data: enum("X_")}, "inc/b.h": {Data: enum("Y_")}}, "inc/a.h"},
		{"negative: started twice", fstest.MapFS{"inc/a.h": {Data: enum("X_")}, "inc/b.h": {Data: enum("X_")}}, "started in 2 headers"},
		{"negative: started nowhere", fstest.MapFS{"inc/b.h": {Data: enum("Y_")}}, "started in 0 headers"},
		{"corner: a mention that starts no enum is not a definition", fstest.MapFS{"inc/a.h": {Data: enum("X_")},
			"inc/b.h": {Data: []byte("#define FOO X_UNSPEC\n")}}, "inc/a.h"},
		{"corner: a C file is not read", fstest.MapFS{"inc/a.c": {Data: enum("X_")}}, "started in 0 headers"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			_, hdr, err := findEnum(tc.fsys, "inc", "X_")
			got := hdr
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
