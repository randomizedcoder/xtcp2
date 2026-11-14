//go:build goip_write

package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// linkAddInfoData is the kind's IFLA_INFO_DATA, or nil when ip sends
// none (no options, or a kind it has no parser for).
func linkAddInfoData(ax map[string]string) ([]byte, error) {
	switch ax["kind"] {
	case "veth":
		peer := make([]byte, IfInfomsgSizeCst)
		// The peer's iplink_parse adds IFLA_MTU as it reads it and
		// IFLA_IFNAME after its loop.
		if s, ok := ax["peer_mtu"]; ok {
			n, err := strconv.ParseUint(s, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("axis peer_mtu=%q: %w", s, err)
			}
			peer = append(peer, rtattr(unix.IFLA_MTU, u32le(uint32(n)))...)
		}
		peer = append(peer, rtattr(unix.IFLA_IFNAME, append([]byte(ax["peer"]), 0))...)
		return rtattr(VethInfoPeer, peer), nil
	case "vlan":
		var out []byte
		if s, ok := ax["vlan_proto"]; ok {
			n, err := strconv.ParseUint(s, 10, 16)
			if err != nil {
				return nil, fmt.Errorf("axis vlan_proto=%q: %w", s, err)
			}
			out = append(out, rtattr(unix.IFLA_VLAN_PROTOCOL, binary.BigEndian.AppendUint16(nil, uint16(n)))...)
		}
		n, err := strconv.ParseUint(ax["vlan_id"], 10, 16)
		if err != nil {
			return nil, fmt.Errorf("axis vlan_id=%q: %w", ax["vlan_id"], err)
		}
		return append(out, rtattr(unix.IFLA_VLAN_ID, binary.LittleEndian.AppendUint16(nil, uint16(n)))...), nil
	case "macvlan":
		n, ok, err := axU32(ax, "macvlan_mode")
		if err != nil || !ok {
			return nil, errors.Join(err, errors.New("axis macvlan_mode missing"))
		}
		return rtattr(unix.IFLA_MACVLAN_MODE, u32le(n)), nil
	case "bridge":
		if n, ok, err := axU32(ax, "br_stp"); err != nil || ok {
			return rtattr(unix.IFLA_BR_STP_STATE, u32le(n)), err
		}
	}
	return nil, nil
}

// linkAddWantFromAxes models iplink_modify for `ip link add n0 ... type K`.
func linkAddWantFromAxes(ax map[string]string) (linkAddWant, error) {
	w := linkAddWant{attrs: map[uint16][]byte{}, kind: ax["kind"]}
	if w.kind == "" || ax["name"] == "" {
		return w, errors.New("axes kind and name are required")
	}
	if ax["up"] == "1" {
		w.flags = unix.IFF_UP
	}
	w.attrs[unix.IFLA_IFNAME] = append([]byte(ax["name"]), 0)
	for _, u := range []struct {
		axis string
		attr uint16
	}{{"mtu", unix.IFLA_MTU}, {"txqlen", unix.IFLA_TXQLEN}, {"numtxqueues", unix.IFLA_NUM_TX_QUEUES}, {"numrxqueues", unix.IFLA_NUM_RX_QUEUES}} {
		n, ok, err := axU32(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = u32le(n)
		}
	}
	mac, err := axMAC(ax, "address")
	if err != nil {
		return w, err
	}
	if mac != nil {
		w.attrs[unix.IFLA_ADDRESS] = mac
	}
	w.link = ax["link"] == "d0"
	data, err := linkAddInfoData(ax)
	if err != nil {
		return w, err
	}
	info := rtattr(unix.IFLA_INFO_KIND, append([]byte(w.kind), 0))
	if data != nil {
		info = append(info, rtattr(unix.IFLA_INFO_DATA, data)...)
	}
	w.attrs[unix.IFLA_LINKINFO] = info
	return w, nil
}

// TestWsLinkAddCorpus runs O3 and O4 over every generated link add row,
// and fails if O3 checked nothing.
func TestWsLinkAddCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "linkadd/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsLinkAddRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d link add requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no link add request: are there link add rows under testdata/*/ws?")
	}
}

func wsLinkAddRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	reqs := wsRouteRequests(msgs, unix.RTM_NEWLINK)
	w, err := linkAddWantFromAxes(row.Axes)
	if err != nil {
		// A value iproute2 refuses to parse has no facts to model; it
		// must send no RTM_NEWLINK at all.
		if row.Expect.RefusedBy != "none" && row.Expect.RefusedBy != "kernel" && len(reqs) == 0 {
			return 0, 0
		}
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
	var d0 int32
	if w.link {
		if d0 = wsLinkIndexOf(msgs, "d0"); d0 == 0 {
			t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of d0\n  cmd: %s", row.Cmd)
		}
	}
	if err := linkAddRequestProblem(w, reqs[0].h.Flags, reqs[0].body, d0); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	d, ok := wsLinkByName(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")), row.Axes["name"])
	if !ok {
		t.Fatalf("O4: the dump holds no %s\n  cmd: %s", row.Axes["name"], row.Cmd)
	}
	if err := linkAddDumpProblem(w, row.Axes, d, d0); err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	return 1, 1
}

func TestLinkAddInfoData(t *testing.T) {
	cases := []struct {
		description string
		axes        map[string]string
		expected    []byte
		wantErr     bool
	}{
		{"positive: a veth peer is a zeroed ifinfomsg and its name", map[string]string{"kind": "veth", "peer": "p0"},
			rtattr(VethInfoPeer, cat(make([]byte, IfInfomsgSizeCst), rtattr(unix.IFLA_IFNAME, []byte("p0\x00")))), false},
		{"corner: a peer's mtu comes before its name, as iplink_parse adds them", map[string]string{"kind": "veth", "peer": "p0", "peer_mtu": "1400"},
			rtattr(VethInfoPeer, cat(make([]byte, IfInfomsgSizeCst), rtattr(unix.IFLA_MTU, u32le(1400)), rtattr(unix.IFLA_IFNAME, []byte("p0\x00")))), false},
		{"positive: a vlan id is host order", map[string]string{"kind": "vlan", "vlan_id": "10"},
			rtattr(unix.IFLA_VLAN_ID, []byte{10, 0}), false},
		{"corner: a vlan protocol is network order, before the id", map[string]string{"kind": "vlan", "vlan_proto": "34984", "vlan_id": "20"},
			cat(rtattr(unix.IFLA_VLAN_PROTOCOL, []byte{0x88, 0xa8}), rtattr(unix.IFLA_VLAN_ID, []byte{20, 0})), false},
		{"positive: a macvlan mode is a u32", map[string]string{"kind": "macvlan", "macvlan_mode": "4"},
			rtattr(unix.IFLA_MACVLAN_MODE, u32le(4)), false},
		{"positive: a bridge option", map[string]string{"kind": "bridge", "br_stp": "1"}, rtattr(unix.IFLA_BR_STP_STATE, u32le(1)), false},
		{"boundary: a bridge with no options sends no data", map[string]string{"kind": "bridge"}, nil, false},
		{"boundary: a dummy sends none", map[string]string{"kind": "dummy"}, nil, false},
		{"negative: a vlan id past u16", map[string]string{"kind": "vlan", "vlan_id": "65536"}, nil, true},
		{"negative: a macvlan with no mode", map[string]string{"kind": "macvlan"}, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := linkAddInfoData(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("got %x, want %x", got, tc.expected)
			}
		})
	}
}

func TestLinkAddRequestProblem(t *testing.T) {
	ax := map[string]string{"kind": "vlan", "name": "n0", "link": "d0", "vlan_id": "10", "mtu": "1400", "up": "1"}
	w, err := linkAddWantFromAxes(ax)
	if err != nil {
		t.Fatal(err)
	}
	name := rtattr(unix.IFLA_IFNAME, []byte("n0\x00"))
	mtu := rtattr(unix.IFLA_MTU, u32le(1400))
	link := rtattr(unix.IFLA_LINK, u32le(3))
	info := rtattr(unix.IFLA_LINKINFO, cat(rtattr(unix.IFLA_INFO_KIND, []byte("vlan\x00")),
		rtattr(unix.IFLA_INFO_DATA, rtattr(unix.IFLA_VLAN_ID, []byte{10, 0}))))
	nlf := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	cases := []struct {
		description string
		nlFlags     uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, link, name, info), ""},
		{"positive: attribute order does not matter", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, info, name, link, mtu), ""},
		{"negative: no NLM_F_EXCL", nlf &^ unix.NLM_F_EXCL, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, link, name, info), "nlmsg flags"},
		{"negative: an index on a create", nlf, ifiBody(5, unix.IFF_UP, unix.IFF_UP, mtu, link, name, info), "ifinfomsg"},
		{"negative: IFLA_LINK names another device", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, rtattr(unix.IFLA_LINK, u32le(4)), name, info), "attribute 5"},
		{"negative: the vlan id in network order", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, link, name,
			rtattr(unix.IFLA_LINKINFO, cat(rtattr(unix.IFLA_INFO_KIND, []byte("vlan\x00")),
				rtattr(unix.IFLA_INFO_DATA, rtattr(unix.IFLA_VLAN_ID, []byte{0, 10}))))), "attribute 18"},
		{"negative: the kind without its NUL", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, link, name,
			rtattr(unix.IFLA_LINKINFO, cat(rtattr(unix.IFLA_INFO_KIND, []byte("vlan")),
				rtattr(unix.IFLA_INFO_DATA, rtattr(unix.IFLA_VLAN_ID, []byte{10, 0}))))), "attribute 18"},
		{"negative: no IFLA_LINK", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, name, info), "attribute 5"},
		{"negative: an attribute not asked for", nlf, ifiBody(0, unix.IFF_UP, unix.IFF_UP, mtu, link, name, info, rtattr(unix.IFLA_TXQLEN, u32le(0))), "not asked for"},
		{"boundary: a body shorter than ifinfomsg", nlf, []byte{0, 0}, ErrIfInfomsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := linkAddRequestProblem(w, tc.nlFlags, tc.body, 3)
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

func TestLinkAddDumpProblem(t *testing.T) {
	vlanAx := map[string]string{"kind": "vlan", "name": "n0", "link": "d0", "vlan_id": "10", "address": "02:00:00:00:01:01", "numtxqueues": "4"}
	ptAx := map[string]string{"kind": "macvlan", "name": "n0", "link": "d0", "macvlan_mode": "8", "address": "02:00:00:00:01:01"}
	want := func(ax map[string]string) linkAddWant {
		w, err := linkAddWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	mac := []byte{2, 0, 0, 0, 1, 1}
	vlan := LinkInfo{Name: "n0", Kind: "vlan", Link: 3, HasLink: true, Address: mac,
		Vlan: &VlanInfo{ID: 10, HasID: true, Protocol: unix.ETH_P_8021Q, HasProtocol: true}}
	vlan.Detail.NumTxQueues = U32Attr{Value: 4, Present: true}
	pt := LinkInfo{Name: "n0", Kind: "macvlan", Link: 3, HasLink: true, Address: []byte{0x46, 0xcb, 0x54, 0xaa, 0x37, 0xde},
		Macvlan: &MacvlanInfo{Mode: MacvlanModePassthru, HasMode: true}}
	with := func(base LinkInfo, f func(l *LinkInfo)) LinkInfo { l := base; f(&l); return l }
	cases := []struct {
		description string
		ax          map[string]string
		d           LinkInfo
		expected    string // in the error; "" for none
	}{
		{"positive: a vlan as made, 802.1Q by default", vlanAx, vlan, ""},
		{"negative: another kind", vlanAx, with(vlan, func(l *LinkInfo) { l.Kind = "macvlan" }), "kind"},
		{"negative: another lower device", vlanAx, with(vlan, func(l *LinkInfo) { l.Link = 4 }), "link"},
		{"negative: another vlan id", vlanAx, with(vlan, func(l *LinkInfo) { l.Vlan = &VlanInfo{ID: 11, Protocol: unix.ETH_P_8021Q} }), "vlan"},
		{"negative: 802.1ad where 802.1Q was the default", vlanAx, with(vlan, func(l *LinkInfo) { l.Vlan = &VlanInfo{ID: 10, Protocol: unix.ETH_P_8021AD} }), "vlan"},
		{"negative: another address", vlanAx, with(vlan, func(l *LinkInfo) { l.Address = []byte{2, 0, 0, 0, 1, 2} }), "address"},
		{"negative: up when not asked", vlanAx, with(vlan, func(l *LinkInfo) { l.Flags = unix.IFF_UP }), "flags"},
		{"negative: another tx queue count", vlanAx, with(vlan, func(l *LinkInfo) { l.Detail.NumTxQueues = U32Attr{Value: 1, Present: true} }), "attribute 31"},
		{"corner: a passthru macvlan keeps its lower device's address", ptAx, pt, ""},
		{"negative: a passthru macvlan in another mode", ptAx, with(pt, func(l *LinkInfo) { l.Macvlan = &MacvlanInfo{Mode: 4} }), "macvlan"},
		{"boundary: a link with nothing decoded", vlanAx, LinkInfo{Name: "n0"}, "kind"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := linkAddDumpProblem(want(tc.ax), tc.ax, tc.d, 3)
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

// linkAddWant is the request a row's axes call for: an RTM_NEWLINK with
// NLM_F_CREATE|NLM_F_EXCL and index 0. attrs holds IFLA_LINK as d0's
// index, filled in once ip's lookup says what it is.
type linkAddWant struct {
	flags uint32
	attrs map[uint16][]byte
	link  bool // IFLA_LINK names d0
	kind  string
}

// linkAddRequestProblem is O3: how a captured request differs from the
// model's. d0 is d0's index, for IFLA_LINK.
func linkAddRequestProblem(w linkAddWant, nlFlags uint16, body []byte, d0 int32) error {
	var errs []error
	if got := nlFlags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != unix.NLM_F_CREATE|unix.NLM_F_EXCL {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want CREATE|EXCL", got))
	}
	var ifi IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &ifi); err != nil {
		return err
	}
	if ifi.Family != unix.AF_UNSPEC || ifi.Index != 0 || ifi.Flags != w.flags || ifi.Change != w.flags {
		errs = append(errs, fmt.Errorf("ifinfomsg %+v, want index 0, flags and change %#x", ifi, w.flags))
	}
	want := maps.Clone(w.attrs)
	if w.link {
		want[unix.IFLA_LINK] = u32le(uint32(d0))
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
	for _, a := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[a&^unix.NLA_F_NESTED]; !ok || !bytes.Equal(g, want[a]) {
			errs = append(errs, fmt.Errorf("attribute %d is %x, want %x", a, g, want[a]))
		}
	}
	for _, a := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[a]; !ok {
			errs = append(errs, fmt.Errorf("attribute %d not asked for", a))
		}
	}
	return errors.Join(errs...)
}

// linkAddDumpProblem is O4: how the dumped n0 differs from what the
// request made. A kind's options are checked where the decoder has them:
// a vlan's id and protocol (802.1Q when none is named), a macvlan's mode,
// a bridge's STP state. A passthru macvlan takes its lower device's
// address whatever was asked (eth_hw_addr_inherit in macvlan_common_newlink,
// drivers/net/macvlan.c:1552), so its address is not compared.
func linkAddDumpProblem(w linkAddWant, ax map[string]string, d LinkInfo, d0 int32) error {
	var errs []error
	if d.Kind != w.kind {
		errs = append(errs, fmt.Errorf("kind %q, want %q", d.Kind, w.kind))
	}
	if d.Flags&unix.IFF_UP != w.flags&unix.IFF_UP {
		errs = append(errs, fmt.Errorf("flags %#x, want IFF_UP %v", d.Flags, w.flags&unix.IFF_UP != 0))
	}
	if w.link && (!d.HasLink || d.Link != d0) {
		errs = append(errs, fmt.Errorf("link %d (present %v), want d0's %d", d.Link, d.HasLink, d0))
	}
	for _, c := range []struct {
		attr uint16
		got  uint32
		has  bool
	}{
		{unix.IFLA_MTU, d.MTU, d.HasMTU}, {unix.IFLA_TXQLEN, d.TxQLen, d.HasTxQLen},
		{unix.IFLA_NUM_TX_QUEUES, d.Detail.NumTxQueues.Value, d.Detail.NumTxQueues.Present},
		{unix.IFLA_NUM_RX_QUEUES, d.Detail.NumRxQueues.Value, d.Detail.NumRxQueues.Present},
	} {
		if b, ok := w.attrs[c.attr]; ok && (!c.has || c.got != binary.LittleEndian.Uint32(b)) {
			errs = append(errs, fmt.Errorf("attribute %d is %d (present %v), want %d", c.attr, c.got, c.has, binary.LittleEndian.Uint32(b)))
		}
	}
	passthru := w.kind == "macvlan" && ax["macvlan_mode"] == strconv.FormatUint(uint64(MacvlanModePassthru), 10)
	if b, ok := w.attrs[unix.IFLA_ADDRESS]; ok && !passthru && !bytes.Equal(d.Address, b) {
		errs = append(errs, fmt.Errorf("address %x, want %x", d.Address, b))
	}
	switch w.kind {
	case "vlan":
		proto := uint64(unix.ETH_P_8021Q)
		if s, ok := ax["vlan_proto"]; ok {
			proto, _ = strconv.ParseUint(s, 10, 16)
		}
		id, _ := strconv.ParseUint(ax["vlan_id"], 10, 16)
		if d.Vlan == nil || uint64(d.Vlan.ID) != id || uint64(d.Vlan.Protocol) != proto {
			errs = append(errs, fmt.Errorf("vlan %+v, want id %d protocol %#x", d.Vlan, id, proto))
		}
	case "macvlan":
		mode, _, _ := axU32(ax, "macvlan_mode")
		if d.Macvlan == nil || d.Macvlan.Mode != mode {
			errs = append(errs, fmt.Errorf("macvlan %+v, want mode %d", d.Macvlan, mode))
		}
	case "bridge":
		if stp, ok, _ := axU32(ax, "br_stp"); ok && d.Bridge.Value(unix.IFLA_BR_STP_STATE).U != uint64(stp) {
			errs = append(errs, fmt.Errorf("bridge stp_state %d, want %d", d.Bridge.Value(unix.IFLA_BR_STP_STATE).U, stp))
		}
	}
	return errors.Join(errs...)
}
