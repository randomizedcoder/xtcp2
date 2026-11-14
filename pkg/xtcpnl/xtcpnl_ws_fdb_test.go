package xtcpnl

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The bridge fdb rows' oracles (coverage-status.md, "W5a"). O3 rebuilds
// the request from the row's axes alone (tools/nlcapgen/matrix_fdb.go),
// following bridge/fdb.c fdb_modify, and checks the attributes in the
// order it adds them, with their widths. O4 looks for the entry in the
// device's FDB dump read back after it, or for its absence after a delete.

// fdbWant is the request a row's axes call for. info.Ifindex and info.Via
// are left 0: they are dev's and via's, as the kernel numbered them.
type fdbWant struct {
	typ, flags uint16
	info       NeighInfo
	dev, via   string
	attrs      []uint16 // every NDA_* the request carries, in fdb_modify's order
	extLen     int      // NDA_FDB_EXT_ATTRS's payload length
}

// fdbVerbs is each verb's message type and the flags do_fdb passes.
var fdbVerbs = map[string]struct{ typ, flags uint16 }{
	"add":     {unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	"append":  {unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_APPEND},
	"replace": {unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	"del":     {unix.RTM_DELNEIGH, 0},
}

// axU16 reads a u16 axis.
func axU16(ax map[string]string, k string) (uint16, bool, error) {
	n, ok, err := axU32(ax, k)
	if err != nil || !ok {
		return 0, ok, err
	}
	if n > 0xffff {
		return 0, false, fmt.Errorf("axis %s=%d is past u16", k, n)
	}
	return uint16(n), true, nil
}

// fdbWantFromAxes models fdb_modify: ndm_flags is the target bits with the
// flags', ndm_state is the axes' (fdbStates), and the attributes follow in
// the order fdb_modify adds them.
func fdbWantFromAxes(ax map[string]string) (fdbWant, error) {
	var w fdbWant
	verb, ok := fdbVerbs[ax["verb"]]
	if !ok {
		return w, fmt.Errorf("axis verb=%q", ax["verb"])
	}
	w.typ, w.flags = verb.typ, verb.flags
	w.dev, w.via = ax["dev"], ax["via"]
	if w.dev == "" {
		return w, errors.New("axis dev missing")
	}
	lla, err := axMAC(ax, "lladdr")
	if err != nil || lla == nil {
		return w, errors.Join(err, errors.New("axis lladdr missing"))
	}
	dst, err := axAddr(ax, "dst")
	state, _, err2 := axU16(ax, "state")
	target, _, err3 := axU8(ax, "target")
	ntf, _, err4 := axU8(ax, "ntf")
	if err := errors.Join(err, err2, err3, err4); err != nil {
		return w, err
	}
	in := NeighInfo{Family: unix.AF_BRIDGE, State: state, Flags: target | ntf, LLAddr: lla, Dst: dst}
	errs := make([]error, 0, 6)
	var e error
	in.Vlan, in.HasVlan, e = axU16(ax, "vlan")
	errs = append(errs, e)
	in.NhID, in.HasNhID, e = axU32(ax, "nhid")
	errs = append(errs, e)
	in.Port, in.HasPort, e = axU16(ax, "port")
	errs = append(errs, e)
	in.Vni, in.HasVni, e = axU32(ax, "vni")
	errs = append(errs, e)
	in.SrcVni, in.HasSrcVni, e = axU32(ax, "src_vni")
	errs = append(errs, e)
	in.FdbNotify, in.HasFdbNotify, e = axU8(ax, "notify")
	errs = append(errs, e)
	if err := errors.Join(errs...); err != nil {
		return w, err
	}
	in.HasVia = w.via != ""
	in.FdbDontRefresh = ax["norefresh"] == "1"
	w.info = in
	w.attrs = append(w.attrs, unix.NDA_LLADDR)
	for _, a := range []struct {
		has  bool
		attr uint16
	}{
		{dst != nil, unix.NDA_DST}, {in.HasVlan, unix.NDA_VLAN}, {in.HasNhID, NdaNhID}, {in.HasPort, unix.NDA_PORT},
		{in.HasVni, unix.NDA_VNI}, {in.HasSrcVni, unix.NDA_SRC_VNI}, {in.HasVia, unix.NDA_IFINDEX},
		{in.HasFdbNotify || in.FdbDontRefresh, NdaFdbExtAttrs},
	} {
		if a.has {
			w.attrs = append(w.attrs, a.attr)
		}
	}
	if in.HasFdbNotify {
		w.extLen += RTAttrSizeCst + 4 // a u8, padded
	}
	if in.FdbDontRefresh {
		w.extLen += RTAttrSizeCst
	}
	return w, nil
}

// fdbAttrWidth is the length attribute a must have in w, or -1 if any.
func fdbAttrWidth(w fdbWant, a uint16) int {
	switch a {
	case unix.NDA_LLADDR:
		return len(w.info.LLAddr)
	case unix.NDA_DST:
		return len(w.info.Dst)
	case unix.NDA_VLAN, unix.NDA_PORT:
		return 2
	case NdaNhID, unix.NDA_VNI, unix.NDA_SRC_VNI, unix.NDA_IFINDEX:
		return 4
	case NdaFdbExtAttrs:
		return w.extLen
	}
	return -1
}

// fdbRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr; index and via are the
// devices' indexes.
func fdbRequestProblem(w fdbWant, flags uint16, body []byte, index int32, via uint32) error {
	var errs []error
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	if len(body) < NdMsgSizeCst {
		return ErrNdMsgSmall
	}
	var attrs []uint16
	err := WalkRTAttrs(body[NdMsgSizeCst:], func(a uint16, val []byte) {
		attrs = append(attrs, a)
		if n := fdbAttrWidth(w, a); n >= 0 && len(val) != n {
			errs = append(errs, fmt.Errorf("attribute %d is %d bytes, want %d", a, len(val), n))
		}
	})
	if err != nil {
		return err
	}
	if !slices.Equal(attrs, w.attrs) {
		errs = append(errs, fmt.Errorf("attributes %v, want %v in that order", attrs, w.attrs))
	}
	ni, err := ParseNeigh(body)
	if err != nil {
		return err
	}
	want := w.info
	want.Ifindex = index
	if want.HasVia {
		want.Via = via
	}
	if !reflect.DeepEqual(ni, want) {
		errs = append(errs, fmt.Errorf("fdb entry %+v\n  want %+v", ni, want))
	}
	return errors.Join(errs...)
}

// fdbDumpHas reports whether a dumped entry is the one w made: its device
// and MAC, its VLAN when one was asked, and, on a VXLAN, its remote, port
// and VNI where vxlan_fdb_info would print them (a port or VNI equal to
// the device's own, 4789 and 100, is left out of the dump).
func fdbDumpHas(w fdbWant, d NeighInfo) bool {
	if d.Ifindex != w.info.Ifindex || !bytes.Equal(d.LLAddr, w.info.LLAddr) {
		return false
	}
	if w.info.HasVlan && (!d.HasVlan || d.Vlan != w.info.Vlan) {
		return false
	}
	if w.dev != "vx0" {
		return true
	}
	in := w.info
	switch {
	case in.Dst != nil && !bytes.Equal(d.Dst, in.Dst),
		in.HasNhID && d.NhID != in.NhID,
		in.HasPort && in.Port != 4789 && d.Port != in.Port,
		in.HasVni && in.Vni != 100 && d.Vni != in.Vni,
		in.HasVia && d.Via != in.Via:
		return false
	}
	return true
}

// TestWsFdbCorpus runs O3 and O4 over every generated fdb row, and fails
// if O3 checked nothing.
func TestWsFdbCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "fdbbr/") && !strings.HasPrefix(row.ID, "fdbvx/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsFdbRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d fdb requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no fdb request: are there fdb rows under testdata/*/ws?")
	}
}

// wsFdbSent is every fdb request in msgs.
func wsFdbSent(msgs []capturedNlMsg) []capturedNlMsg {
	return append(wsRouteRequests(msgs, unix.RTM_NEWNEIGH), wsRouteRequests(msgs, unix.RTM_DELNEIGH)...)
}

// fdbAckFailProblem is O2 for bridge: a row whose arguments bridge refuses
// after its link dump must send no fdb request.
func fdbAckFailProblem(refusedBy string, sent int) error {
	if refusedBy == wsRefusedAckFail && sent != 0 {
		return fmt.Errorf("bridge refused its arguments but sent %d fdb requests", sent)
	}
	return nil
}

func wsFdbRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	sent := wsFdbSent(msgs)
	if err := fdbAckFailProblem(row.Expect.RefusedBy, len(sent)); err != nil {
		t.Fatalf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	w, err := fdbWantFromAxes(row.Axes)
	if err != nil {
		// A value bridge refuses to parse has no facts to model.
		if row.Expect.RefusedBy != wsRefusedNone && len(sent) == 0 {
			return 0, 0
		}
		t.Fatalf("O3 model: %v", err)
	}
	rc := wsReadRC(t, dir)
	if len(sent) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no fdb request\n  cmd: %s", row.Cmd)
		}
		return 0, 0
	}
	if len(sent) != 1 || sent[0].h.Type != w.typ {
		t.Fatalf("O3: %d fdb requests, want 1 of type %d\n  cmd: %s", len(sent), w.typ, row.Cmd)
	}
	index := wsLinkIndexOf(msgs, w.dev)
	if index == 0 {
		t.Fatalf("O3: bridge read no RTM_NEWLINK for %s\n  cmd: %s", w.dev, row.Cmd)
	}
	var via uint32
	if w.via != "" {
		v := wsLinkIndexOf(msgs, w.via)
		if v <= 0 {
			t.Fatalf("O3: bridge read no RTM_NEWLINK for %s\n  cmd: %s", w.via, row.Cmd)
		}
		via = uint32(v)
	}
	if err := fdbRequestProblem(w, sent[0].h.Flags, sent[0].body, index, via); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	w.info.Ifindex, w.info.Via = index, via
	dump := wsNeighDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	has := slices.ContainsFunc(dump, func(d NeighInfo) bool { return fdbDumpHas(w, d) })
	if err := fdbRoundTripProblem(w.typ, has); err != nil {
		t.Errorf("O4: %v: %x among %d entries %+v\n  cmd: %s", err, w.info.LLAddr, len(dump), dump, row.Cmd)
	}
	return 1, 1
}

// fdbRoundTripProblem is O4's verdict: after an accepted add the dump holds
// the entry, and after an accepted del it doesn't.
func fdbRoundTripProblem(typ uint16, has bool) error {
	switch {
	case typ == unix.RTM_DELNEIGH && has:
		return errors.New("after del the dump still holds the entry")
	case typ == unix.RTM_NEWNEIGH && !has:
		return errors.New("after an add the dump lacks the entry")
	}
	return nil
}

func TestFdbRoundTripProblem(t *testing.T) {
	tests := []struct {
		description string
		typ         uint16
		has         bool
		expected    bool // a problem
	}{
		{description: "positive: an add the dump holds", typ: unix.RTM_NEWNEIGH, has: true, expected: false},
		{description: "positive: a del the dump lacks", typ: unix.RTM_DELNEIGH, has: false, expected: false},
		{description: "negative: an add the dump lacks", typ: unix.RTM_NEWNEIGH, has: false, expected: true},
		{description: "negative: a del the dump still holds", typ: unix.RTM_DELNEIGH, has: true, expected: true},
		{description: "corner: a type O4 doesn't judge", typ: unix.RTM_GETNEIGH, has: false, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbRoundTripProblem(tc.typ, tc.has) != nil; got != tc.expected {
				t.Errorf("problem %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestFdbWantFromAxes(t *testing.T) {
	mac := []byte{2, 0, 0, 0, 0, 1}
	cases := []struct {
		description string
		axes        map[string]string
		expected    string // the request's attrs and ndmsg, or the error
	}{
		{"positive: a bridge port's master add",
			map[string]string{"verb": "add", "dev": "d0", "lladdr": "02:00:00:00:00:01", "target": "4", "state": "192"},
			"28 1536 [2] flags=4 state=192"},
		{"positive: a VXLAN append in fdb_modify's attribute order",
			map[string]string{"verb": "append", "dev": "vx0", "lladdr": "02:00:00:00:00:01", "target": "2", "state": "66",
				"dst": "192.0.2.1", "port": "4790", "vni": "200", "src_vni": "100", "via": "d1"},
			"28 3072 [2 1 6 7 11 8] flags=2 state=66"},
		{"positive: an nhid, a VLAN and both ext attributes",
			map[string]string{"verb": "del", "dev": "d0", "lladdr": "02:00:00:00:00:01", "target": "6", "ntf": "64",
				"state": "2", "vlan": "10", "nhid": "10", "notify": "3", "norefresh": "1"},
			"29 0 [2 5 13 14] flags=70 state=2"},
		{"negative: no dev", map[string]string{"verb": "add", "lladdr": "02:00:00:00:00:01"}, "axis dev missing"},
		{"negative: an unknown verb", map[string]string{"verb": "show", "dev": "d0"}, `axis verb="show"`},
		{"negative: no MAC", map[string]string{"verb": "add", "dev": "d0"}, "axis lladdr missing"},
		{"boundary: a VLAN past u16", map[string]string{"verb": "add", "dev": "d0", "lladdr": "02:00:00:00:00:01", "vlan": "65536"},
			"axis vlan=65536 is past u16"},
		{"corner: a state past u16", map[string]string{"verb": "add", "dev": "d0", "lladdr": "02:00:00:00:00:01", "state": "65536"},
			"axis state=65536 is past u16"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := fdbWantFromAxes(tc.axes)
			got := ""
			if err != nil {
				got = err.Error()
			} else {
				got = fmt.Sprintf("%d %d %v flags=%d state=%d", w.typ, w.flags, w.attrs, w.info.Flags, w.info.State)
				if !bytes.Equal(w.info.LLAddr, mac) || w.info.Family != unix.AF_BRIDGE {
					t.Errorf("lladdr %x family %d", w.info.LLAddr, w.info.Family)
				}
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// fdbBody builds an RTM_*NEIGH body for dev 5.
func fdbBody(state uint16, flags uint8, attrs ...[]byte) []byte {
	return concat(append([][]byte{ndmsgHdr(unix.AF_BRIDGE, 5, state, flags, 0)}, attrs...)...)
}

func TestFdbRequestProblem(t *testing.T) {
	mac := []byte{2, 0, 0, 0, 0, 1}
	base := map[string]string{"verb": "add", "dev": "vx0", "lladdr": "02:00:00:00:00:01", "target": "2", "state": "192",
		"dst": "192.0.2.1", "port": "4790", "via": "d1", "notify": "1"}
	w, err := fdbWantFromAxes(base)
	if err != nil {
		t.Fatal(err)
	}
	ext := rtattr(NdaFdbExtAttrs|unix.NLA_F_NESTED, rtattr(NfeaActivityNotify, []byte{1}))
	good := [][]byte{rtattr(unix.NDA_LLADDR, mac), rtattr(unix.NDA_DST, []byte{192, 0, 2, 1}),
		rtattr(unix.NDA_PORT, []byte{0x12, 0xb6}), rtattr(unix.NDA_IFINDEX, le32(3)), ext}
	const reqFlags = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	cases := []struct {
		description string
		flags       uint16
		body        []byte
		via         uint32
		expected    string // a fragment of the error, "" for none
	}{
		{"positive: the request the model makes", reqFlags, fdbBody(192, unix.NTF_SELF, good...), 3, ""},
		{"negative: NLM_F_EXCL missing", reqFlags &^ unix.NLM_F_EXCL, fdbBody(192, unix.NTF_SELF, good...), 3, "nlmsg flags"},
		{"negative: the port little-endian", reqFlags, fdbBody(192, unix.NTF_SELF, good[0], good[1],
			rtattr(unix.NDA_PORT, []byte{0xb6, 0x12}), good[3], good[4]), 3, "Port:46610"},
		{"negative: a u32 port", reqFlags, fdbBody(192, unix.NTF_SELF, good[0], good[1],
			rtattr(unix.NDA_PORT, []byte{0, 0, 0x12, 0xb6}), good[3], good[4]), 3, "attribute 6 is 4 bytes, want 2"},
		{"negative: via is another device", reqFlags, fdbBody(192, unix.NTF_SELF, good...), 4, "Via:3"},
		{"negative: NTF_MASTER set", reqFlags, fdbBody(192, unix.NTF_SELF|unix.NTF_MASTER, good...), 3, "Flags:6"},
		{"corner: the same attributes, the via before the port", reqFlags, fdbBody(192, unix.NTF_SELF, good[0], good[1],
			good[3], good[2], good[4]), 3, "in that order"},
		{"boundary: the ext nest without its notify", reqFlags, fdbBody(192, unix.NTF_SELF, good[0], good[1], good[2], good[3],
			rtattr(NdaFdbExtAttrs|unix.NLA_F_NESTED, nil)), 3, "attribute 14 is 0 bytes, want 8"},
		{"negative: a body shorter than an ndmsg", reqFlags, []byte{7}, 3, "too small"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, fdbRequestProblem(w, tc.flags, tc.body, 5, tc.via), tc.expected)
		})
	}
}

func TestFdbDumpHas(t *testing.T) {
	mac := []byte{2, 0, 0, 0, 0, 1}
	vx := fdbWant{dev: "vx0", info: NeighInfo{Ifindex: 5, LLAddr: mac, Dst: []byte{192, 0, 2, 1},
		Port: 4789, HasPort: true, Vni: 200, HasVni: true}}
	br := fdbWant{dev: "d0", info: NeighInfo{Ifindex: 5, LLAddr: mac, Vlan: 10, HasVlan: true}}
	entry := NeighInfo{Ifindex: 5, LLAddr: mac, Dst: []byte{192, 0, 2, 1}, Vni: 200, HasVni: true}
	cases := []struct {
		description string
		w           fdbWant
		d           NeighInfo
		expected    bool
	}{
		{"positive: the VXLAN entry; its default port is left out of the dump", vx, entry, true},
		{"negative: another MAC", vx, NeighInfo{Ifindex: 5, LLAddr: []byte{2, 0, 0, 0, 0, 2}}, false},
		{"negative: another device", vx, NeighInfo{Ifindex: 6, LLAddr: mac, Dst: entry.Dst, Vni: 200, HasVni: true}, false},
		{"negative: another remote", vx, NeighInfo{Ifindex: 5, LLAddr: mac, Dst: []byte{192, 0, 2, 2}, Vni: 200, HasVni: true}, false},
		{"negative: the VNI left out", vx, NeighInfo{Ifindex: 5, LLAddr: mac, Dst: entry.Dst}, false},
		{"positive: the bridge entry in its VLAN", br, NeighInfo{Ifindex: 5, LLAddr: mac, Vlan: 10, HasVlan: true}, true},
		{"negative: the bridge entry with no VLAN", br, NeighInfo{Ifindex: 5, LLAddr: mac}, false},
		{"corner: a bridge entry needs no remote", fdbWant{dev: "d0", info: NeighInfo{Ifindex: 5, LLAddr: mac}},
			NeighInfo{Ifindex: 5, LLAddr: mac, Vlan: 1, HasVlan: true}, true},
		{"boundary: an nhid entry's nhid", fdbWant{dev: "vx0", info: NeighInfo{Ifindex: 5, LLAddr: mac, NhID: 10, HasNhID: true}},
			NeighInfo{Ifindex: 5, LLAddr: mac, NhID: 11, HasNhID: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbDumpHas(tc.w, tc.d); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestFdbAckFailProblem(t *testing.T) {
	tests := []struct {
		description string
		refusedBy   string
		sent        int
		expected    bool // a problem
	}{
		{description: "positive: an ack-then-fail row that sent nothing", refusedBy: wsRefusedAckFail, sent: 0, expected: false},
		{description: "negative: an ack-then-fail row that sent a request", refusedBy: wsRefusedAckFail, sent: 1, expected: true},
		{description: "boundary: a kernel refusal sends its one request", refusedBy: "kernel", sent: 1, expected: false},
		{description: "corner: an accepted row is not this check's", refusedBy: wsRefusedNone, sent: 2, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbAckFailProblem(tc.refusedBy, tc.sent) != nil; got != tc.expected {
				t.Errorf("problem %v, want %v", got, tc.expected)
			}
		})
	}
}
