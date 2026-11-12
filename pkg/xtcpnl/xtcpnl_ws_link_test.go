package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math/bits"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip link set rows' oracles (coverage-status.md, "W3c"). O3 rebuilds
// every attribute of the request from the row's axes alone
// (tools/nlcapgen/matrix_link.go), following ip/iplink.c iplink_parse, and
// compares each one's bytes and the ifinfomsg's flags and change mask. O4
// finds d0 in the dump of every link read back after it.

// linkSetWant is the request a row's axes call for: an RTM_NEWLINK with no
// NLM_F_CREATE, d0's index, and attrs by type.
type linkSetWant struct {
	flags, change uint32
	attrs         map[uint16][]byte
	name          string // what d0 is called afterwards
}

// linkSetU32s are the u32 axes and the attribute each lands in.
var linkSetU32s = []struct {
	axis string
	attr uint16
}{
	{"mtu", unix.IFLA_MTU}, {"txqlen", unix.IFLA_TXQLEN}, {"group", unix.IFLA_GROUP},
	{"gso_max_size", unix.IFLA_GSO_MAX_SIZE}, {"gso_max_segs", unix.IFLA_GSO_MAX_SEGS},
	{"gro_max_size", unix.IFLA_GRO_MAX_SIZE}, {"gso_ipv4_max_size", unix.IFLA_GSO_IPV4_MAX_SIZE},
	{"gro_ipv4_max_size", unix.IFLA_GRO_IPV4_MAX_SIZE},
}

// linkSetWantFromAxes models iplink_parse for `ip link set dev d0`: up and
// down move IFF_UP in ifi_change and ifi_flags, as each on/off flag moves
// its bit; the alias goes without its NUL and a new name with one.
func linkSetWantFromAxes(ax map[string]string) (linkSetWant, error) {
	w := linkSetWant{attrs: map[uint16][]byte{}, name: "d0"}
	switch ax["up"] {
	case "":
	case "1":
		w.flags, w.change = unix.IFF_UP, unix.IFF_UP
	case "0":
		w.change = unix.IFF_UP
	default:
		return w, fmt.Errorf("axis up=%q", ax["up"])
	}
	if s, ok := ax["flag"]; ok {
		bit, err := strconv.ParseUint(s, 10, 32)
		if err != nil || bits.OnesCount64(bit) != 1 {
			return w, fmt.Errorf("axis flag=%q is not one bit", s)
		}
		w.change |= uint32(bit)
		if ax["flag_on"] == "1" {
			w.flags |= uint32(bit)
		}
	}
	for _, u := range linkSetU32s {
		n, ok, err := axU32(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = u32le(n)
		}
	}
	for _, u := range []struct {
		axis string
		attr uint16
	}{{"linkmode", unix.IFLA_LINKMODE}, {"operstate", unix.IFLA_OPERSTATE}} {
		n, ok, err := axU8(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = []byte{n}
		}
	}
	for _, m := range []struct {
		axis string
		attr uint16
	}{{"address", unix.IFLA_ADDRESS}, {"broadcast", unix.IFLA_BROADCAST}} {
		b, err := axMAC(ax, m.axis)
		if err != nil {
			return w, err
		}
		if b != nil {
			w.attrs[m.attr] = b
		}
	}
	if s, ok := ax["alias"]; ok {
		w.attrs[unix.IFLA_IFALIAS] = []byte(s)
	}
	if s, ok := ax["name"]; ok {
		w.attrs[unix.IFLA_IFNAME] = append([]byte(s), 0)
		w.name = s
	}
	return w, nil
}

// linkSetRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr; index is d0's.
func linkSetRequestProblem(w linkSetWant, nlFlags uint16, body []byte, index int32) error {
	var errs []error
	if got := nlFlags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != 0 {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want none", got))
	}
	var ifi IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &ifi); err != nil {
		return err
	}
	if ifi.Family != unix.AF_UNSPEC || ifi.Type != 0 || ifi.Index != index || ifi.Flags != w.flags || ifi.Change != w.change {
		errs = append(errs, fmt.Errorf("ifinfomsg %+v, want index %d flags %#x change %#x", ifi, index, w.flags, w.change))
	}
	got := map[uint16][]byte{}
	err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(a uint16, val []byte) {
		if _, dup := got[a]; dup {
			errs = append(errs, fmt.Errorf("attribute %d twice", a))
		}
		got[a] = val
	})
	if err != nil {
		return err
	}
	for _, a := range slices.Sorted(maps.Keys(w.attrs)) {
		if g, ok := got[a]; !ok || !bytes.Equal(g, w.attrs[a]) {
			errs = append(errs, fmt.Errorf("attribute %d is %x, want %x", a, g, w.attrs[a]))
		}
	}
	for _, a := range slices.Sorted(maps.Keys(got)) {
		if _, ok := w.attrs[a]; !ok {
			errs = append(errs, fmt.Errorf("attribute %d not asked for", a))
		}
	}
	return errors.Join(errs...)
}

// linkDumpProblem is O4: how the dumped d0 differs from what the request
// set. The kernel keeps every value as given; the operational state is not
// compared, since a down device's stays down whatever was asked.
func linkDumpProblem(w linkSetWant, d LinkInfo) error {
	var errs []error
	if d.Name != w.name {
		errs = append(errs, fmt.Errorf("name %q, want %q", d.Name, w.name))
	}
	if d.Flags&w.change != w.flags {
		errs = append(errs, fmt.Errorf("flags %#x under change %#x, want %#x", d.Flags&w.change, w.change, w.flags))
	}
	u32 := func(a uint16) (uint32, bool) {
		b, ok := w.attrs[a]
		if !ok {
			return 0, false
		}
		return binary.LittleEndian.Uint32(b), true
	}
	for _, c := range []struct {
		attr uint16
		got  uint32
		has  bool
	}{
		{unix.IFLA_MTU, d.MTU, d.HasMTU}, {unix.IFLA_TXQLEN, d.TxQLen, d.HasTxQLen}, {unix.IFLA_GROUP, d.Group, d.HasGroup},
		{unix.IFLA_GSO_MAX_SIZE, d.Detail.GSOMaxSize.Value, d.Detail.GSOMaxSize.Present},
		{unix.IFLA_GSO_MAX_SEGS, d.Detail.GSOMaxSegs.Value, d.Detail.GSOMaxSegs.Present},
		{unix.IFLA_GRO_MAX_SIZE, d.Detail.GROMaxSize.Value, d.Detail.GROMaxSize.Present},
		{unix.IFLA_GSO_IPV4_MAX_SIZE, d.Detail.GSOIPv4MaxSize.Value, d.Detail.GSOIPv4MaxSize.Present},
		{unix.IFLA_GRO_IPV4_MAX_SIZE, d.Detail.GROIPv4MaxSize.Value, d.Detail.GROIPv4MaxSize.Present},
	} {
		if want, ok := u32(c.attr); ok && (!c.has || c.got != want) {
			errs = append(errs, fmt.Errorf("attribute %d is %d (present %v), want %d", c.attr, c.got, c.has, want))
		}
	}
	if b, ok := w.attrs[unix.IFLA_LINKMODE]; ok && (!d.HasLinkMode || d.LinkMode != b[0]) {
		errs = append(errs, fmt.Errorf("link mode %d, want %d", d.LinkMode, b[0]))
	}
	if b, ok := w.attrs[unix.IFLA_ADDRESS]; ok && !bytes.Equal(d.Address, b) {
		errs = append(errs, fmt.Errorf("address %x, want %x", d.Address, b))
	}
	if b, ok := w.attrs[unix.IFLA_BROADCAST]; ok && !bytes.Equal(d.Broadcast, b) {
		errs = append(errs, fmt.Errorf("broadcast %x, want %x", d.Broadcast, b))
	}
	if b, ok := w.attrs[unix.IFLA_IFALIAS]; ok && d.IfAlias != string(b) {
		errs = append(errs, fmt.Errorf("alias %q, want %q", d.IfAlias, b))
	}
	return errors.Join(errs...)
}

// wsLinkByIndex returns the link with index in a readback's dump.
func wsLinkByIndex(t *testing.T, msgs []capturedNlMsg, index int32) (LinkInfo, bool) {
	t.Helper()
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if li.Index == index {
			return li, true
		}
	}
	return LinkInfo{}, false
}

// TestWsLinkSetCorpus runs O3 and O4 over every generated link set row,
// and fails if O3 checked nothing.
func TestWsLinkSetCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "linkset/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsLinkSetRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d link requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no link request: are there link rows under testdata/*/ws?")
	}
}

func wsLinkSetRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	reqs := wsRouteRequests(msgs, unix.RTM_NEWLINK)
	w, err := linkSetWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	rc := wsReadRC(t, dir)
	if len(reqs) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no RTM_NEWLINK\n  cmd: %s", row.Cmd)
		}
		return 0, 0
	}
	if len(reqs) != 1 {
		t.Fatalf("O3: %d RTM_NEWLINK requests, want 1\n  cmd: %s", len(reqs), row.Cmd)
	}
	index := wsLinkIndexOf(msgs, "d0")
	if index == 0 {
		t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of d0\n  cmd: %s", row.Cmd)
	}
	if err := linkSetRequestProblem(w, reqs[0].h.Flags, reqs[0].body, index); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	d, ok := wsLinkByIndex(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")), index)
	if !ok {
		t.Fatalf("O4: the dump holds no link %d\n  cmd: %s", index, row.Cmd)
	}
	if err := linkDumpProblem(w, d); err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	return 1, 1
}

func TestLinkSetWantFromAxes(t *testing.T) {
	cases := []struct {
		description string
		axes        map[string]string
		expected    func(w linkSetWant) bool
		wantErr     bool
	}{
		{"positive: up sets IFF_UP in both", map[string]string{"up": "1"}, func(w linkSetWant) bool {
			return w.flags == unix.IFF_UP && w.change == unix.IFF_UP && len(w.attrs) == 0
		}, false},
		{"positive: down sets it in the change mask alone", map[string]string{"up": "0"}, func(w linkSetWant) bool {
			return w.flags == 0 && w.change == unix.IFF_UP
		}, false},
		{"positive: a flag on joins up", map[string]string{"up": "1", "flag": "256", "flag_on": "1"}, func(w linkSetWant) bool {
			return w.flags == unix.IFF_UP|unix.IFF_PROMISC && w.change == unix.IFF_UP|unix.IFF_PROMISC
		}, false},
		{"corner: a flag off only in the change mask", map[string]string{"flag": "4096", "flag_on": "0"}, func(w linkSetWant) bool {
			return w.flags == 0 && w.change == unix.IFF_MULTICAST
		}, false},
		{"positive: the u32s are little-endian", map[string]string{"mtu": "1400", "gro_max_size": "524280"}, func(w linkSetWant) bool {
			return bytes.Equal(w.attrs[unix.IFLA_MTU], u32le(1400)) && bytes.Equal(w.attrs[unix.IFLA_GRO_MAX_SIZE], u32le(524280))
		}, false},
		{"positive: the alias has no NUL and the new name has one", map[string]string{"alias": "hi", "name": "d9"}, func(w linkSetWant) bool {
			return string(w.attrs[unix.IFLA_IFALIAS]) == "hi" && string(w.attrs[unix.IFLA_IFNAME]) == "d9\x00" && w.name == "d9"
		}, false},
		{"positive: link mode and operstate are bytes", map[string]string{"linkmode": "1", "operstate": "5"}, func(w linkSetWant) bool {
			return bytes.Equal(w.attrs[unix.IFLA_LINKMODE], []byte{1}) && bytes.Equal(w.attrs[unix.IFLA_OPERSTATE], []byte{5})
		}, false},
		{"boundary: no axes asks nothing", map[string]string{}, func(w linkSetWant) bool {
			return w.flags == 0 && w.change == 0 && len(w.attrs) == 0 && w.name == "d0"
		}, false},
		{"negative: a flag of two bits", map[string]string{"flag": "3"}, nil, true},
		{"negative: an up that is not 0 or 1", map[string]string{"up": "2"}, nil, true},
		{"negative: an address that is not hex", map[string]string{"address": "zz"}, nil, true},
		{"negative: an operstate past u8", map[string]string{"operstate": "256"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := linkSetWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

// ifiBody is an ifinfomsg for index with flags and change, then attrs.
func ifiBody(index int32, flags, change uint32, attrs ...[]byte) []byte {
	h := []byte{unix.AF_UNSPEC, 0, 0, 0}
	h = binary.LittleEndian.AppendUint32(h, uint32(index))
	h = binary.LittleEndian.AppendUint32(h, flags)
	h = binary.LittleEndian.AppendUint32(h, change)
	return cat(append([][]byte{h}, attrs...)...)
}

func TestLinkSetRequestProblem(t *testing.T) {
	w, err := linkSetWantFromAxes(map[string]string{"up": "1", "mtu": "1400", "alias": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	mtu := rtattr(unix.IFLA_MTU, u32le(1400))
	alias := rtattr(unix.IFLA_IFALIAS, []byte("hi"))
	nlf := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK)
	cases := []struct {
		description string
		nlFlags     uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, mtu, alias), ""},
		{"positive: attribute order does not matter", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, alias, mtu), ""},
		{"negative: NLM_F_CREATE on a set", nlf | unix.NLM_F_CREATE, ifiBody(2, unix.IFF_UP, unix.IFF_UP, mtu, alias), "nlmsg flags"},
		{"negative: up without the change mask", nlf, ifiBody(2, unix.IFF_UP, 0, mtu, alias), "ifinfomsg"},
		{"negative: another device", nlf, ifiBody(3, unix.IFF_UP, unix.IFF_UP, mtu, alias), "ifinfomsg"},
		{"negative: the alias with a NUL", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, mtu, rtattr(unix.IFLA_IFALIAS, []byte("hi\x00"))), "attribute 20"},
		{"negative: another mtu", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, rtattr(unix.IFLA_MTU, u32le(1500)), alias), "attribute 4"},
		{"negative: an attribute not asked for", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, mtu, alias, rtattr(unix.IFLA_TXQLEN, u32le(0))), "not asked for"},
		{"negative: an attribute twice", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP, mtu, mtu, alias), "twice"},
		{"boundary: a bare header is short of what the axes name", nlf, ifiBody(2, unix.IFF_UP, unix.IFF_UP), "attribute 4"},
		{"boundary: a body shorter than ifinfomsg", nlf, []byte{0, 0, 0}, ErrIfInfomsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := linkSetRequestProblem(w, tc.nlFlags, tc.body, 2)
			if tc.expected == "" {
				if err != nil {
					t.Errorf("err %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Errorf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

func TestLinkDumpProblem(t *testing.T) {
	w, err := linkSetWantFromAxes(map[string]string{"up": "1", "flag": "256", "flag_on": "1", "mtu": "1400", "txqlen": "0",
		"gso_max_segs": "100", "linkmode": "1", "address": "02:00:00:00:00:aa", "alias": "hi", "name": "d9"})
	if err != nil {
		t.Fatal(err)
	}
	d := LinkInfo{Index: 2, Name: "d9", Flags: unix.IFF_UP | unix.IFF_PROMISC | unix.IFF_BROADCAST, MTU: 1400, HasMTU: true,
		TxQLen: 0, HasTxQLen: true, LinkMode: 1, HasLinkMode: true, Address: []byte{2, 0, 0, 0, 0, 0xaa}, IfAlias: "hi"}
	d.Detail.GSOMaxSegs = U32Attr{Value: 100, Present: true}
	with := func(f func(l *LinkInfo)) LinkInfo { l := d; f(&l); return l }
	cases := []struct {
		description string
		d           LinkInfo
		expected    string // in the error; "" for none
	}{
		{"positive: the link as set, with flags of its own", d, ""},
		{"negative: the old name", with(func(l *LinkInfo) { l.Name = "d0" }), "name"},
		{"negative: still down", with(func(l *LinkInfo) { l.Flags &^= unix.IFF_UP }), "flags"},
		{"negative: promisc not set", with(func(l *LinkInfo) { l.Flags &^= unix.IFF_PROMISC }), "flags"},
		{"negative: another mtu", with(func(l *LinkInfo) { l.MTU = 1500 }), "attribute 4"},
		{"boundary: a txqueuelen of 0 not dumped", with(func(l *LinkInfo) { l.HasTxQLen = false }), "attribute 13"},
		{"negative: gso_max_segs not dumped", with(func(l *LinkInfo) { l.Detail.GSOMaxSegs = U32Attr{} }), "attribute 40"},
		{"negative: another link mode", with(func(l *LinkInfo) { l.LinkMode = 0 }), "link mode"},
		{"negative: another address", with(func(l *LinkInfo) { l.Address = []byte{2, 0, 0, 0, 0, 0xbb} }), "address"},
		{"negative: no alias", with(func(l *LinkInfo) { l.IfAlias = "" }), "alias"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := linkDumpProblem(w, tc.d)
			if tc.expected == "" {
				if err != nil {
					t.Errorf("err %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Errorf("err %v, want it to contain %q", err, tc.expected)
			}
		})
	}
}

// The ip link add rows' oracles (coverage-status.md, "W3d"). O3 rebuilds
// the request from the row's axes (tools/nlcapgen/matrix_linkadd.go): the
// top-level attributes by type, and the IFLA_LINKINFO nest byte for byte,
// as iplink_modify writes it, IFLA_INFO_KIND then the kind's
// IFLA_INFO_DATA. O4 finds n0 in the dump read back after it.

// wsLinkByName returns the last reply about the link named name in a
// readback's capture. `ip link show dev n0` first resolves the name with
// ll_link_get, whose RTEXT_FILTER_NAME_ONLY reply (iproute2 7.2.0) holds
// the name alone; the full reply comes after it.
func wsLinkByName(t *testing.T, msgs []capturedNlMsg, name string) (LinkInfo, bool) {
	t.Helper()
	var last LinkInfo
	found := false
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if li.Name == name {
			last, found = li, true
		}
	}
	return last, found
}
