package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The link attribute rows' oracles (coverage-status.md, "W4c"). linkattr
// and linkpd are ip link set rows, so O3 reuses linkSetRequestProblem with
// a want that adds the keywords tools/nlcapgen/matrix_linkattr.go sends.
// linkprop's rows are RTM_NEWLINKPROP and RTM_DELLINKPROP, or the two
// RTM_GETLINKs `ip link show dev ALTNAME` sends.

// linkAttrWantFromAxes models iplink_parse for the W4c keywords on top of
// linkSetWantFromAxes's up and down. index gives the index ip looked a
// master up as; an empty master is nomaster's 0.
func linkAttrWantFromAxes(ax map[string]string, index func(string) int32) (linkSetWant, error) {
	w, err := linkSetWantFromAxes(map[string]string{"up": ax["up"]})
	if err != nil {
		return w, err
	}
	if name, ok := ax["master"]; ok {
		var i int32
		if name != "" {
			if i = index(name); i == 0 {
				return w, fmt.Errorf("no lookup of master %q", name)
			}
		}
		w.attrs[unix.IFLA_MASTER] = u32le(uint32(i))
	}
	for _, u := range []struct {
		axis string
		attr uint16
	}{{"carrier", unix.IFLA_CARRIER}, {"protodown", unix.IFLA_PROTO_DOWN}} {
		n, ok, err := axU8(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = []byte{n}
		}
	}
	for _, u := range []struct {
		axis string
		attr uint16
	}{{"new_ifindex", unix.IFLA_NEW_IFINDEX}, {"link_netnsid", unix.IFLA_LINK_NETNSID}} {
		n, ok, err := axU32(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = u32le(n)
		}
	}
	if s, ok := ax["parent_dev_name"]; ok {
		w.attrs[unix.IFLA_PARENT_DEV_NAME] = append([]byte(s), 0)
	}
	if err := linkAttrNests(ax, &w); err != nil {
		return w, err
	}
	if s, ok := ax["netns"]; ok && s != "fd" {
		return w, fmt.Errorf("axis netns=%q", s)
	}
	return w, nil
}

// linkAttrNests adds the nested attributes: addrgenmode's IFLA_AF_SPEC,
// xdp_delete's IFLA_XDP and the protodown reason's mask and value.
// WalkRTAttrs clears NLA_F_NESTED, so the reason's nest, which iproute2
// flags, is keyed by its bare type.
func linkAttrNests(ax map[string]string, w *linkSetWant) error {
	if n, ok, err := axU8(ax, "addrgenmode"); err != nil {
		return err
	} else if ok {
		w.attrs[unix.IFLA_AF_SPEC] = rtattr(unix.AF_INET6, rtattr(unix.IFLA_INET6_ADDR_GEN_MODE, []byte{n}))
	}
	if n, ok, err := axU32(ax, "xdp_flags"); err != nil {
		return err
	} else if ok {
		w.attrs[unix.IFLA_XDP] = slices.Concat(rtattr(unix.IFLA_XDP_FD, u32le(0xFFFFFFFF)), rtattr(unix.IFLA_XDP_FLAGS, u32le(n)))
	}
	mask, okM, err := axU32(ax, "pd_mask")
	if err != nil {
		return err
	}
	value, okV, err := axU32(ax, "pd_value")
	if err != nil {
		return err
	}
	if okM != okV {
		return fmt.Errorf("axes pd_mask and pd_value come together")
	}
	if okM {
		w.attrs[unix.IFLA_PROTO_DOWN_REASON] = slices.Concat(
			rtattr(unix.IFLA_PROTO_DOWN_REASON_MASK, u32le(mask)), rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, u32le(value)))
	}
	return nil
}

// linkReqAttr returns attribute a of an ifinfomsg request body.
func linkReqAttr(body []byte, a uint16) ([]byte, bool) {
	if len(body) < IfInfomsgSizeCst {
		return nil, false
	}
	var got []byte
	var ok bool
	if WalkRTAttrs(body[IfInfomsgSizeCst:], func(t uint16, val []byte) {
		if t == a && !ok {
			got, ok = val, true
		}
	}) != nil {
		return nil, false
	}
	return got, ok
}

// linkNetnsFD fills in the namespace fd netns $$ sent, which the axes
// can't know: any descriptor past stderr, as a u32.
func linkNetnsFD(w *linkSetWant, ax map[string]string, body []byte) error {
	if ax["netns"] != "fd" {
		return nil
	}
	b, ok := linkReqAttr(body, unix.IFLA_NET_NS_FD)
	if !ok || len(b) != 4 || binary.LittleEndian.Uint32(b) < 3 {
		return fmt.Errorf("IFLA_NET_NS_FD is %x, want a u32 descriptor past 2", b)
	}
	w.attrs[unix.IFLA_NET_NS_FD] = b
	return nil
}

// linkProtoDown reads IFLA_PROTO_DOWN and the reason's value from a dumped
// link's body (rtnl_fill_proto_down: the reason only when it is not 0).
func linkProtoDown(body []byte) (down uint8, reason uint32, err error) {
	if len(body) < IfInfomsgSizeCst {
		return 0, 0, fmt.Errorf("a %d-byte link", len(body))
	}
	var nest []byte
	err = WalkRTAttrs(body[IfInfomsgSizeCst:], func(t uint16, val []byte) {
		switch t {
		case unix.IFLA_PROTO_DOWN:
			if len(val) == 1 {
				down = val[0]
			}
		case unix.IFLA_PROTO_DOWN_REASON:
			nest = val
		}
	})
	if err != nil || nest == nil {
		return down, 0, err
	}
	err = WalkRTAttrs(nest, func(t uint16, val []byte) {
		if t == unix.IFLA_PROTO_DOWN_REASON_VALUE && len(val) == 4 {
			reason = binary.LittleEndian.Uint32(val)
		}
	})
	return down, reason, err
}

// linkAttrDumpProblem is O4 for linkattr and linkpd: the dumped device has
// the master, address generation mode and carrier asked for, and protodown
// and its reason as set on a device that had neither.
func linkAttrDumpProblem(w linkSetWant, d LinkInfo, body []byte) error {
	var errs []error
	if b, ok := w.attrs[unix.IFLA_MASTER]; ok && uint32(d.Master) != binary.LittleEndian.Uint32(b) {
		errs = append(errs, fmt.Errorf("master %d, want %d", d.Master, binary.LittleEndian.Uint32(b)))
	}
	if b, ok := w.attrs[unix.IFLA_AF_SPEC]; ok {
		if want := b[len(b)-4]; !d.Detail.HasAddrGenMode || d.Detail.AddrGenMode != want {
			errs = append(errs, fmt.Errorf("addrgenmode %d (present %v), want %d", d.Detail.AddrGenMode, d.Detail.HasAddrGenMode, want))
		}
	}
	if b, ok := w.attrs[unix.IFLA_CARRIER]; ok && (!d.HasCarrier || d.Carrier != b[0]) {
		errs = append(errs, fmt.Errorf("carrier %d (present %v), want %d", d.Carrier, d.HasCarrier, b[0]))
	}
	down, reason, err := linkProtoDown(body)
	if err != nil {
		return err
	}
	var wantDown uint8
	if b, ok := w.attrs[unix.IFLA_PROTO_DOWN]; ok {
		wantDown = b[0]
	}
	if down != wantDown {
		errs = append(errs, fmt.Errorf("protodown %d, want %d", down, wantDown))
	}
	var wantReason uint32
	if b, ok := w.attrs[unix.IFLA_PROTO_DOWN_REASON]; ok {
		wantReason = binary.LittleEndian.Uint32(b[len(b)-4:])
	}
	if reason != wantReason {
		errs = append(errs, fmt.Errorf("protodown reason %#x, want %#x", reason, wantReason))
	}
	return errors.Join(errs...)
}

// wsLinkLastByIndex returns the last reply about the link with index in
// a readback, and its body. `ip link show dev NAME` first looks NAME up
// (ll_name_to_index, answered with the name alone), so the last reply is
// the full one.
func wsLinkLastByIndex(t *testing.T, msgs []capturedNlMsg, index int32) (LinkInfo, []byte) {
	t.Helper()
	var last LinkInfo
	var body []byte
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if li.Index == index {
			last, body = li, m.body
		}
	}
	return last, body
}

// TestWsLinkAttrCorpus runs O3 and O4 over every linkattr and linkpd row,
// and fails if O3 checked nothing.
func TestWsLinkAttrCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		dev := map[string]string{"linkattr": "d0", "linkpd": "m0"}[strings.Split(row.ID, "/")[0]]
		if dev == "" {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsLinkAttrRow(t, dir, row, dev)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d link requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no link request: are there linkattr rows under testdata/*/ws?")
	}
}

func wsLinkAttrRow(t *testing.T, dir string, row wsRow, dev string) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	reqs := wsRouteRequests(msgs, unix.RTM_NEWLINK)
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
	w, err := linkAttrWantFromAxes(row.Axes, func(name string) int32 { return wsLinkIndexOf(msgs, name) })
	if err == nil {
		err = linkNetnsFD(&w, row.Axes, reqs[0].body)
	}
	if err != nil {
		t.Fatalf("O3 model: %v\n  cmd: %s", err, row.Cmd)
	}
	index := wsLinkIndexOf(msgs, dev)
	if index == 0 {
		t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of %s\n  cmd: %s", dev, row.Cmd)
	}
	if err := linkSetRequestProblem(w, reqs[0].h.Flags, reqs[0].body, index); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	d, body := wsLinkLastByIndex(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")), index)
	if body == nil {
		t.Fatalf("O4: the dump holds no link %d\n  cmd: %s", index, row.Cmd)
	}
	if err := linkAttrDumpProblem(w, d, body); err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	return 1, 1
}

// linkPropAlts returns a linkprop row's altnames, in the order given.
func linkPropAlts(ax map[string]string) ([]string, error) {
	names := make([]string, 0, 2)
	for _, k := range []string{"altname", "altname2"} {
		if s, ok := ax[k]; ok {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no altname axis")
	}
	return names, nil
}

// linkPropRequestProblem is O3 for property add and del: iplink_prop sets
// NLM_F_EXCL, NLM_F_CREATE and NLM_F_APPEND on an add, and
// iplink_prop_mod nests one IFLA_ALT_IFNAME per name, NUL-terminated, in
// IFLA_PROP_LIST (with NLA_F_NESTED, which WalkRTAttrs clears).
func linkPropRequestProblem(op string, names []string, nlFlags uint16, body []byte, index int32) error {
	var wantFlags uint16
	if op == "add" {
		wantFlags = unix.NLM_F_EXCL | unix.NLM_F_CREATE | unix.NLM_F_APPEND
	}
	nest := make([]byte, 0, len(names)*(RTAttrSizeCst+unix.IFNAMSIZ))
	for _, n := range names {
		nest = append(nest, rtattr(unix.IFLA_ALT_IFNAME, append([]byte(n), 0))...)
	}
	return linkSetRequestProblem(linkSetWant{attrs: map[uint16][]byte{unix.IFLA_PROP_LIST: nest}},
		nlFlags&^wantFlags, body, index)
}

// linkShowNameAttr is the attribute iplink_get and ll_name_to_index name a
// device by: IFLA_IFNAME when check_ifname passes it, else IFLA_ALT_IFNAME.
func linkShowNameAttr(name string) uint16 {
	if name == "" || len(name) >= unix.IFNAMSIZ || name == "." || name == ".." || strings.ContainsAny(name, "/ \t\n\v\f\r") {
		return unix.IFLA_ALT_IFNAME
	}
	return unix.IFLA_IFNAME
}

// linkShowProblem is O3 for `ip link show dev ALTNAME`: two RTM_GETLINKs
// (ll_name_to_index's, then iplink_get's, whose ifinfomsg carries ip
// link's preferred family, AF_PACKET, and no index), each naming the
// device by the altname.
func linkShowProblem(name string, reqs []capturedNlMsg) error {
	if len(reqs) != 2 {
		return fmt.Errorf("%d RTM_GETLINK requests, want 2", len(reqs))
	}
	var errs []error
	a := linkShowNameAttr(name)
	for i, r := range reqs {
		got, ok := linkReqAttr(r.body, a)
		if !ok || string(got) != name+"\x00" {
			errs = append(errs, fmt.Errorf("request %d: attribute %d is %q, want %q", i, a, got, name+"\x00"))
		}
		if _, ok := linkReqAttr(r.body, unix.IFLA_IFNAME^unix.IFLA_ALT_IFNAME^a); ok {
			errs = append(errs, fmt.Errorf("request %d names the device twice", i))
		}
	}
	var ifi IfInfomsg
	if _, err := DeserializeIfInfomsg(reqs[1].body, &ifi); err != nil {
		return err
	}
	if ifi.Family != unix.AF_PACKET || ifi.Index != 0 {
		errs = append(errs, fmt.Errorf("iplink_get's ifinfomsg %+v, want family AF_PACKET and no index", ifi))
	}
	return errors.Join(errs...)
}

// linkPropDumpProblem is O4 for linkprop: after an add or a show the
// dumped d0 holds every name; after a del, none.
func linkPropDumpProblem(op string, names []string, d LinkInfo) error {
	var errs []error
	for _, n := range names {
		if has := slices.Contains(d.AltNames, n); has != (op != "del") {
			errs = append(errs, fmt.Errorf("altname %q held %v after %s; d0 has %q", n, has, op, d.AltNames))
		}
	}
	return errors.Join(errs...)
}

// TestWsLinkPropCorpus runs O3 and O4 over every linkprop row.
func TestWsLinkPropCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "linkprop/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsLinkPropRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no linkprop request: are there linkprop rows under testdata/*/ws?")
	}
}

func wsLinkPropRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	op := row.Axes["op"]
	typ := map[string]uint16{"add": unix.RTM_NEWLINKPROP, "del": unix.RTM_DELLINKPROP, "show": unix.RTM_GETLINK}[op]
	if typ == 0 {
		t.Fatalf("O3 model: axis op=%q", op)
	}
	reqs := wsRouteRequests(msgs, typ)
	rc := wsReadRC(t, dir)
	if len(reqs) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no request of type %d\n  cmd: %s", typ, row.Cmd)
		}
		return 0, 0
	}
	names, err := linkPropAlts(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	index := wsLinkIndexOf(msgs, "d0")
	if index == 0 {
		t.Fatalf("O3: no RTM_NEWLINK names d0\n  cmd: %s", row.Cmd)
	}
	switch {
	case op == "show":
		err = linkShowProblem(names[0], reqs)
	case len(reqs) != 1:
		err = fmt.Errorf("%d requests, want 1", len(reqs))
	default:
		err = linkPropRequestProblem(op, names, reqs[0].h.Flags, reqs[0].body, index)
	}
	if err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	d, ok := wsLinkByName(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")), "d0")
	if !ok {
		t.Fatalf("O4: the dump holds no d0\n  cmd: %s", row.Cmd)
	}
	if err := linkPropDumpProblem(op, names, d); err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	return 1, 1
}

// wantErrContaining fails unless err is nil for an empty expected, or
// holds expected.
func wantErrContaining(t *testing.T, err error, expected string) {
	t.Helper()
	if expected == "" {
		if err != nil {
			t.Errorf("err %v, want none", err)
		}
		return
	}
	if err == nil || !strings.Contains(err.Error(), expected) {
		t.Errorf("err %v, want it to contain %q", err, expected)
	}
}

func TestLinkAttrWantFromAxes(t *testing.T) {
	index := func(name string) int32 {
		return map[string]int32{"br0": 3, "v0": 4}[name]
	}
	cases := []struct {
		description string
		ax          map[string]string
		attr        uint16 // the attribute to look at; 0 for the count alone
		expected    string // the attribute's bytes in hex, or the error
	}{
		{"positive: a bridge master is its index", map[string]string{"master": "br0"}, unix.IFLA_MASTER, "03000000"},
		{"corner: nomaster is master 0", map[string]string{"master": ""}, unix.IFLA_MASTER, "00000000"},
		{"negative: a master ip never looked up", map[string]string{"master": "nosuch"}, 0, `no lookup of master "nosuch"`},
		{"positive: carrier is a u8", map[string]string{"carrier": "1"}, unix.IFLA_CARRIER, "01"},
		{"positive: protodown off is a u8 0", map[string]string{"protodown": "0"}, unix.IFLA_PROTO_DOWN, "00"},
		{"negative: a carrier past u8", map[string]string{"carrier": "256"}, 0, "past u8"},
		{"positive: netns index is a u32", map[string]string{"netns": "fd", "new_ifindex": "100"}, unix.IFLA_NEW_IFINDEX, "64000000"},
		{"boundary: the largest link-netnsid", map[string]string{"link_netnsid": "4294967295"}, unix.IFLA_LINK_NETNSID, "ffffffff"},
		{"negative: a netns by name", map[string]string{"netns": "nosuch"}, 0, `axis netns="nosuch"`},
		{"positive: parentdev is a NUL-terminated name", map[string]string{"parent_dev_name": "br0"}, unix.IFLA_PARENT_DEV_NAME, "62723000"},
		{"positive: addrgenmode nests under AF_INET6", map[string]string{"addrgenmode": "3"}, unix.IFLA_AF_SPEC,
			"0c000a000500080003000000"},
		{"positive: xdp off clears with fd -1", map[string]string{"xdp_flags": "1"}, unix.IFLA_XDP, "08000100ffffffff0800030001000000"},
		{"boundary: the top protodown reason bit", map[string]string{"pd_mask": "2147483648", "pd_value": "2147483648"},
			unix.IFLA_PROTO_DOWN_REASON, "08000100000000800800020000000080"},
		{"negative: a reason mask with no value", map[string]string{"pd_mask": "1"}, 0, "come together"},
		{"negative: a bad up", map[string]string{"up": "2"}, 0, `axis up="2"`},
		{"corner: no axes want nothing", map[string]string{}, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := linkAttrWantFromAxes(tc.ax, index)
			if tc.attr == 0 {
				wantErrContaining(t, err, tc.expected)
				if tc.expected == "" && len(w.attrs) != 0 {
					t.Errorf("attrs %x, want none", w.attrs)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", w.attrs[tc.attr]); got != tc.expected || len(w.attrs) != 1 {
				t.Errorf("attribute %d is %s (%d attrs), want %s", tc.attr, got, len(w.attrs), tc.expected)
			}
		})
	}
}

func TestLinkNetnsFD(t *testing.T) {
	fd := func(v uint32) []byte { return ifiBody(2, 0, 0, rtattr(unix.IFLA_NET_NS_FD, u32le(v))) }
	cases := []struct {
		description string
		ax          map[string]string
		body        []byte
		expected    string // the error, "" for none
	}{
		{"positive: a descriptor ip opened", map[string]string{"netns": "fd"}, fd(5), ""},
		{"boundary: the first descriptor past stderr", map[string]string{"netns": "fd"}, fd(3), ""},
		{"negative: stderr is not a namespace", map[string]string{"netns": "fd"}, fd(2), "past 2"},
		{"negative: no IFLA_NET_NS_FD", map[string]string{"netns": "fd"}, ifiBody(2, 0, 0), "past 2"},
		{"negative: a u16 descriptor", map[string]string{"netns": "fd"}, ifiBody(2, 0, 0, rtattr(unix.IFLA_NET_NS_FD, []byte{5, 0})), "past 2"},
		{"corner: no netns axis looks at nothing", map[string]string{}, []byte{0}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w := linkSetWant{attrs: map[uint16][]byte{}}
			err := linkNetnsFD(&w, tc.ax, tc.body)
			wantErrContaining(t, err, tc.expected)
			if _, ok := w.attrs[unix.IFLA_NET_NS_FD]; ok != (tc.expected == "" && tc.ax["netns"] == "fd") {
				t.Errorf("IFLA_NET_NS_FD wanted %v", ok)
			}
		})
	}
}

func TestLinkProtoDown(t *testing.T) {
	reason := func(v uint32) []byte {
		return rtattr(unix.IFLA_PROTO_DOWN_REASON|unix.NLA_F_NESTED, rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, u32le(v)))
	}
	pd := func(v byte) []byte { return rtattr(unix.IFLA_PROTO_DOWN, []byte{v}) }
	cases := []struct {
		description string
		body        []byte
		expected    string // "down reason", or the error
	}{
		{"positive: down with a reason", ifiBody(2, 0, 0, pd(1), reason(1)), "1 0x1"},
		{"positive: up with no reason", ifiBody(2, 0, 0, pd(0)), "0 0x0"},
		{"boundary: the top reason bit", ifiBody(2, 0, 0, pd(1), reason(1<<31)), "1 0x80000000"},
		{"corner: neither attribute", ifiBody(2, 0, 0), "0 0x0"},
		{"corner: a u32 protodown is not read", ifiBody(2, 0, 0, rtattr(unix.IFLA_PROTO_DOWN, u32le(1))), "0 0x0"},
		{"negative: a body shorter than ifinfomsg", []byte{0, 0}, "a 2-byte link"},
		{"negative: an attribute overrunning the body", ifiBody(2, 0, 0, rtattrOverrunning(unix.IFLA_PROTO_DOWN, 40)), "too small"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			down, r, err := linkProtoDown(tc.body)
			got := fmt.Sprintf("%d %#x", down, r)
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestLinkAttrDumpProblem(t *testing.T) {
	w, err := linkAttrWantFromAxes(map[string]string{"master": "br0", "addrgenmode": "1", "carrier": "0", "protodown": "1",
		"pd_mask": "1", "pd_value": "1"}, func(string) int32 { return 3 })
	if err != nil {
		t.Fatal(err)
	}
	down := func(pd byte, reason uint32) []byte {
		attrs := [][]byte{rtattr(unix.IFLA_PROTO_DOWN, []byte{pd})}
		if reason != 0 {
			attrs = append(attrs, rtattr(unix.IFLA_PROTO_DOWN_REASON, rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, u32le(reason))))
		}
		return ifiBody(2, 0, 0, attrs...)
	}
	d := LinkInfo{Index: 2, Master: 3, Carrier: 0, HasCarrier: true}
	d.Detail.AddrGenMode, d.Detail.HasAddrGenMode = 1, true
	with := func(f func(l *LinkInfo)) LinkInfo { l := d; f(&l); return l }
	cases := []struct {
		description string
		w           linkSetWant
		d           LinkInfo
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the link as set", w, d, down(1, 1), ""},
		{"negative: no master", w, with(func(l *LinkInfo) { l.Master = 0 }), down(1, 1), "master 0, want 3"},
		{"negative: another addrgenmode", w, with(func(l *LinkInfo) { l.Detail.AddrGenMode = 0 }), down(1, 1), "addrgenmode 0"},
		{"negative: addrgenmode not dumped", w, with(func(l *LinkInfo) { l.Detail.HasAddrGenMode = false }), down(1, 1), "present false"},
		{"boundary: a carrier of 0 not dumped", w, with(func(l *LinkInfo) { l.HasCarrier = false }), down(1, 1), "carrier 0 (present false)"},
		{"negative: carrier on when asked off", w, with(func(l *LinkInfo) { l.Carrier = 1 }), down(1, 1), "carrier 1 (present true), want 0"},
		{"negative: still up", w, d, down(0, 1), "protodown 0, want 1"},
		{"negative: no reason", w, d, down(1, 0), "protodown reason 0x0, want 0x1"},
		{"corner: nothing asked, protodown and its reason dumped as 0", linkSetWant{attrs: map[uint16][]byte{}}, LinkInfo{}, down(0, 0), ""},
		{"corner: nothing asked, but the link went down", linkSetWant{attrs: map[uint16][]byte{}}, LinkInfo{}, down(1, 0), "protodown 1, want 0"},
		{"negative: a short body", w, d, []byte{0}, "a 1-byte link"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, linkAttrDumpProblem(tc.w, tc.d, tc.body), tc.expected)
		})
	}
}

func TestLinkPropAlts(t *testing.T) {
	cases := []struct {
		description string
		ax          map[string]string
		expected    string // the names joined, or the error
	}{
		{"positive: one altname", map[string]string{"altname": "alt0"}, "alt0"},
		{"positive: two, in order", map[string]string{"altname2": "alt2", "altname": "alt1"}, "alt1,alt2"},
		{"corner: an empty altname is still a name", map[string]string{"altname": ""}, ""},
		{"negative: no altname axis", map[string]string{"op": "add"}, "no altname axis"},
		{"boundary: only the second", map[string]string{"altname2": "alt2"}, "alt2"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			names, err := linkPropAlts(tc.ax)
			got := strings.Join(names, ",")
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestLinkPropRequestProblem(t *testing.T) {
	alt := func(names ...string) []byte {
		nest := make([]byte, 0, len(names)*(RTAttrSizeCst+unix.IFNAMSIZ))
		for _, n := range names {
			nest = append(nest, rtattr(unix.IFLA_ALT_IFNAME, append([]byte(n), 0))...)
		}
		return rtattr(unix.IFLA_PROP_LIST|unix.NLA_F_NESTED, nest)
	}
	nlf := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK)
	add := nlf | unix.NLM_F_EXCL | unix.NLM_F_CREATE | unix.NLM_F_APPEND
	cases := []struct {
		description string
		op          string
		names       []string
		nlFlags     uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: an add", "add", []string{"alt0"}, add, ifiBody(2, 0, 0, alt("alt0")), ""},
		{"positive: a del of two", "del", []string{"alt1", "alt2"}, nlf, ifiBody(2, 0, 0, alt("alt1", "alt2")), ""},
		{"negative: a del with the add's flags", "del", []string{"alt0"}, add, ifiBody(2, 0, 0, alt("alt0")), "nlmsg flags"},
		{"corner: an add missing NLM_F_APPEND is still accepted", "add", []string{"alt0"}, add &^ unix.NLM_F_APPEND,
			ifiBody(2, 0, 0, alt("alt0")), ""},
		{"negative: the two names swapped", "del", []string{"alt1", "alt2"}, nlf, ifiBody(2, 0, 0, alt("alt2", "alt1")), "attribute 52"},
		{"negative: a name with no NUL", "add", []string{"alt0"}, add,
			ifiBody(2, 0, 0, rtattr(unix.IFLA_PROP_LIST, rtattr(unix.IFLA_ALT_IFNAME, []byte("alt0")))), "attribute 52"},
		{"negative: another device", "add", []string{"alt0"}, add, ifiBody(3, 0, 0, alt("alt0")), "ifinfomsg"},
		{"boundary: the longest altname", "add", []string{strings.Repeat("b", 127)}, add,
			ifiBody(2, 0, 0, alt(strings.Repeat("b", 127))), ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, linkPropRequestProblem(tc.op, tc.names, tc.nlFlags, tc.body, 2), tc.expected)
		})
	}
}

func TestLinkShowNameAttr(t *testing.T) {
	cases := []struct {
		description string
		name        string
		expected    uint16
	}{
		{"positive: a short name is IFLA_IFNAME", "alt0", unix.IFLA_IFNAME},
		{"boundary: 15 characters still fit IFNAMSIZ", strings.Repeat("a", 15), unix.IFLA_IFNAME},
		{"boundary: 16 characters do not", strings.Repeat("a", 16), unix.IFLA_ALT_IFNAME},
		{"negative: a slash", "a/b", unix.IFLA_ALT_IFNAME},
		{"negative: a space", "a b", unix.IFLA_ALT_IFNAME},
		{"corner: a dot", ".", unix.IFLA_ALT_IFNAME},
		{"corner: two dots", "..", unix.IFLA_ALT_IFNAME},
		{"corner: three dots pass", "...", unix.IFLA_IFNAME},
		{"corner: empty", "", unix.IFLA_ALT_IFNAME},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := linkShowNameAttr(tc.name); got != tc.expected {
				t.Errorf("attribute %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestLinkShowProblem(t *testing.T) {
	req := func(family byte, index int32, attrs ...[]byte) capturedNlMsg {
		b := ifiBody(index, 0, 0, attrs...)
		b[0] = family
		return capturedNlMsg{body: b}
	}
	long := "alternate-name-0"
	ifn := func(n string) []byte { return rtattr(unix.IFLA_IFNAME, append([]byte(n), 0)) }
	altn := func(n string) []byte { return rtattr(unix.IFLA_ALT_IFNAME, append([]byte(n), 0)) }
	cases := []struct {
		description string
		name        string
		reqs        []capturedNlMsg
		expected    string // in the error; "" for none
	}{
		{"positive: a short altname by IFLA_IFNAME", "alt0",
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0")), req(unix.AF_PACKET, 0, ifn("alt0"))}, ""},
		{"positive: a long one by IFLA_ALT_IFNAME", long,
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, altn(long)), req(unix.AF_PACKET, 0, altn(long))}, ""},
		{"negative: one request", "alt0", []capturedNlMsg{req(unix.AF_PACKET, 0, ifn("alt0"))}, "1 RTM_GETLINK requests, want 2"},
		{"negative: the long name as IFLA_IFNAME", long,
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn(long)), req(unix.AF_PACKET, 0, altn(long))}, "request 0: attribute 53"},
		{"negative: named twice", "alt0",
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0"), altn("alt0")), req(unix.AF_PACKET, 0, ifn("alt0"))}, "twice"},
		{"negative: iplink_get by index", "alt0",
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0")), req(unix.AF_PACKET, 2, ifn("alt0"))}, "want family AF_PACKET"},
		{"negative: iplink_get with AF_UNSPEC", "alt0",
			[]capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0")), req(unix.AF_UNSPEC, 0, ifn("alt0"))}, "want family AF_PACKET"},
		{"boundary: no requests", "alt0", nil, "0 RTM_GETLINK requests"},
		{"negative: three requests", "alt0", []capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0")), req(unix.AF_PACKET, 0, ifn("alt0")),
			req(unix.AF_PACKET, 0, ifn("alt0"))}, "3 RTM_GETLINK requests, want 2"},
		{"corner: a short second body", "alt0", []capturedNlMsg{req(unix.AF_UNSPEC, 0, ifn("alt0")), {body: []byte{0}}},
			ErrIfInfomsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, linkShowProblem(tc.name, tc.reqs), tc.expected)
		})
	}
}

func TestLinkPropDumpProblem(t *testing.T) {
	cases := []struct {
		description string
		op          string
		names       []string
		alts        []string
		expected    string // in the error; "" for none
	}{
		{"positive: added, and held", "add", []string{"alt0"}, []string{"d0alt", "alt0"}, ""},
		{"positive: deleted, and gone", "del", []string{"alt1", "alt2"}, []string{"d0alt"}, ""},
		{"positive: shown, and held", "show", []string{"alt1", "alt2"}, []string{"alt1", "alt2"}, ""},
		{"negative: added, but missing", "add", []string{"alt0"}, nil, `altname "alt0" held false after add`},
		{"negative: one of two deleted", "del", []string{"alt1", "alt2"}, []string{"alt2"}, `altname "alt2" held true after del`},
		{"boundary: a prefix is not the name", "add", []string{"alt0"}, []string{"alt"}, "held false"},
		{"corner: no names, nothing to check", "del", nil, []string{"alt0"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, linkPropDumpProblem(tc.op, tc.names, LinkInfo{AltNames: tc.alts}), tc.expected)
		})
	}
}
