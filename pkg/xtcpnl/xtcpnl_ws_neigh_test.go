package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip neigh rows' oracles (coverage-status.md, "W3b"). O3 rebuilds the
// NeighInfo the request should parse to from the row's axes alone
// (tools/nlcapgen/matrix_neigh.go), following ip/ipneigh.c
// ipneigh_modify, and checks the request's attribute set and widths. O4
// looks for the entry in the dump read back after it.

// neighWant is the request a row's axes call for. info.Ifindex is left 0:
// it is d0's, as the kernel numbered it.
type neighWant struct {
	typ, flags uint16
	info       NeighInfo
	attrs      []uint16 // every NDA_* the request carries, sorted
	proxy      bool
}

// neighVerbs is each verb's message type and the flags do_ipneigh passes.
var neighVerbs = map[string]struct{ typ, flags uint16 }{
	"add":     {unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	"replace": {unix.RTM_NEWNEIGH, unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	"change":  {unix.RTM_NEWNEIGH, unix.NLM_F_REPLACE},
	"del":     {unix.RTM_DELNEIGH, 0},
}

// axMAC reads an axis "aa:bb:..." link-layer address.
func axMAC(ax map[string]string, k string) ([]byte, error) {
	s, ok := ax[k]
	if !ok {
		return nil, nil
	}
	b, err := hex.DecodeString(strings.ReplaceAll(s, ":", ""))
	if err != nil || len(b) == 0 {
		return nil, fmt.Errorf("axis %s=%q is not a link-layer address", k, s)
	}
	return b, nil
}

// neighWantFromAxes models ipneigh_modify: ndm_state NUD_PERMANENT unless
// named, NTF_PROXY for a proxy entry, NDA_FLAGS_EXT only when an extended
// flag is set, and NDA_PROTOCOL only when one is named.
func neighWantFromAxes(ax map[string]string) (neighWant, error) {
	var w neighWant
	verb, ok := neighVerbs[ax["verb"]]
	if !ok {
		return w, fmt.Errorf("axis verb=%q", ax["verb"])
	}
	w.typ, w.flags = verb.typ, verb.flags
	switch ax["family"] {
	case "4":
		w.info.Family = unix.AF_INET
	case "6":
		w.info.Family = unix.AF_INET6
	default:
		return w, fmt.Errorf("axis family=%q", ax["family"])
	}
	dst, err := axAddr(ax, "dst")
	if err != nil || dst == nil {
		return w, errors.Join(err, errors.New("axis dst missing"))
	}
	lla, err := axMAC(ax, "lladdr")
	if err != nil {
		return w, err
	}
	state, _, err := axU32(ax, "state")
	ntf, _, err2 := axU8(ax, "ntf")
	ext, _, err3 := axU32(ax, "ntf_ext")
	proto, hasProto, err4 := axU8(ax, "protocol")
	if err := errors.Join(err, err2, err3, err4); err != nil {
		return w, err
	}
	if state > 0xff {
		return w, fmt.Errorf("axis state=%d is past u8", state)
	}
	w.proxy = ax["proxy"] == "1"
	if w.proxy {
		ntf |= unix.NTF_PROXY
	}
	w.info.State, w.info.Flags, w.info.FlagsExt = uint16(state), ntf, ext
	w.info.Dst, w.info.LLAddr = dst, lla
	w.attrs = append(w.attrs, unix.NDA_DST)
	if lla != nil {
		w.attrs = append(w.attrs, unix.NDA_LLADDR)
	}
	if ext != 0 {
		w.attrs = append(w.attrs, NdaFlagsExt)
	}
	if hasProto {
		w.info.Protocol, w.info.HasProtocol = proto, true
		w.attrs = append(w.attrs, NdaProtocol)
	}
	slices.Sort(w.attrs)
	return w, nil
}

// neighAttrWidth is the length attribute a must have in w, or -1 if any.
func neighAttrWidth(w neighWant, a uint16) int {
	switch a {
	case unix.NDA_DST:
		return len(w.info.Dst)
	case unix.NDA_LLADDR:
		return len(w.info.LLAddr)
	case NdaFlagsExt:
		return 4
	case NdaProtocol:
		return 1
	}
	return -1
}

// neighRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr; index is d0's.
func neighRequestProblem(w neighWant, flags uint16, body []byte, index int32) error {
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
		if n := neighAttrWidth(w, a); n >= 0 && len(val) != n {
			errs = append(errs, fmt.Errorf("attribute %d is %d bytes, want %d", a, len(val), n))
		}
	})
	if err != nil {
		return err
	}
	slices.Sort(attrs)
	if !slices.Equal(attrs, w.attrs) {
		errs = append(errs, fmt.Errorf("attributes %v, want %v", attrs, w.attrs))
	}
	ni, err := ParseNeigh(body)
	if err != nil {
		return err
	}
	want := w.info
	want.Ifindex = index
	if !reflect.DeepEqual(ni, want) {
		errs = append(errs, fmt.Errorf("neighbor %+v\n  want %+v", ni, want))
	}
	return errors.Join(errs...)
}

// neighDumpMatches reports whether a dumped entry is the one the request
// made. A proxy entry is matched on its address and NTF_PROXY. Otherwise
// the flags and extended flags asked for must be set, and, unless use or
// managed handed the entry to the kernel to resolve, a permanent or noarp
// state and the link-layer address must be as asked. NTF_ROUTER is set
// only on the path that stores a valid state: __neigh_update leaves for an
// invalid state, use or managed before it sets update_isrouter, while
// neigh_update_flags has already set
// NTF_EXT_LEARNED. A protocol of 0 is not stored.
func neighDumpMatches(w neighWant, d NeighInfo) bool {
	if d.Family != w.info.Family || d.Ifindex != w.info.Ifindex || !bytes.Equal(d.Dst, w.info.Dst) {
		return false
	}
	if w.info.Protocol != 0 && (!d.HasProtocol || d.Protocol != w.info.Protocol) {
		return false
	}
	if w.proxy {
		return d.Flags&unix.NTF_PROXY != 0
	}
	handed := w.info.Flags&unix.NTF_USE != 0 || w.info.FlagsExt&NtfExtManaged != 0
	asked := w.info.Flags &^ unix.NTF_USE
	if handed || w.info.State&neighNudValid == 0 {
		asked &^= unix.NTF_ROUTER
	}
	if d.Flags&asked != asked || d.FlagsExt&w.info.FlagsExt != w.info.FlagsExt {
		return false
	}
	if handed {
		return true
	}
	if s := w.info.State; (s == unix.NUD_PERMANENT || s == unix.NUD_NOARP) && d.State != s {
		return false
	}
	return w.info.LLAddr == nil || w.info.State&neighNudValid == 0 || bytes.Equal(d.LLAddr, w.info.LLAddr)
}

// neighNudValid is the kernel's NUD_VALID.
const neighNudValid = unix.NUD_PERMANENT | unix.NUD_NOARP | unix.NUD_REACHABLE | unix.NUD_PROBE | unix.NUD_STALE | unix.NUD_DELAY

// wsNeighDump returns the entries a readback's dump replied with.
func wsNeighDump(t *testing.T, msgs []capturedNlMsg) []NeighInfo {
	t.Helper()
	var out []NeighInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWNEIGH || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		ni, err := ParseNeigh(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ni)
	}
	return out
}

// wsLinkIndexOf is the index of the device named name in the RTM_NEWLINK
// replies ip read (ll_init_map dumps every link), or 0 if none.
func wsLinkIndexOf(msgs []capturedNlMsg, name string) int32 {
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if li, err := ParseNewLink(m.body); err == nil && li.Name == name {
			return li.Index
		}
	}
	return 0
}

// TestWsNeighCorpus runs O3 and O4 over every generated neighbor row, and
// fails if O3 checked nothing.
func TestWsNeighCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "neigh4/") && !strings.HasPrefix(row.ID, "neigh6/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsNeighRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d neighbor requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no neighbor request: are there neighbor rows under testdata/*/ws?")
	}
}

func wsNeighRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	w, err := neighWantFromAxes(row.Axes)
	if err != nil {
		// A value iproute2 refuses to parse has no facts to model; it
		// must send no neighbor request at all.
		sent := len(wsRouteRequests(msgs, unix.RTM_NEWNEIGH)) + len(wsRouteRequests(msgs, unix.RTM_DELNEIGH))
		if row.Expect.RefusedBy == "iproute2" && sent == 0 {
			return 0, 0
		}
		t.Fatalf("O3 model: %v", err)
	}
	reqs := wsRouteRequests(msgs, w.typ)
	rc := wsReadRC(t, dir)
	if len(reqs) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no request of type %d\n  cmd: %s", w.typ, row.Cmd)
		}
		return 0, 0
	}
	if len(reqs) != 1 {
		t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), w.typ, row.Cmd)
	}
	index := wsLinkIndexOf(msgs, "d0")
	if index == 0 {
		t.Fatalf("O3: ip read no RTM_NEWLINK for d0\n  cmd: %s", row.Cmd)
	}
	if err := neighRequestProblem(w, reqs[0].h.Flags, reqs[0].body, index); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	w.info.Ifindex = index
	dump := wsNeighDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	if w.typ == unix.RTM_DELNEIGH {
		if slices.ContainsFunc(dump, func(d NeighInfo) bool { return bytes.Equal(d.Dst, w.info.Dst) }) {
			t.Errorf("O4: after del the dump still holds %v\n  cmd: %s", w.info.Dst, row.Cmd)
		}
		return 1, 1
	}
	if !slices.ContainsFunc(dump, func(d NeighInfo) bool { return neighDumpMatches(w, d) }) {
		t.Errorf("O4: after %s none of the dump's %d entries is it: %+v\n  cmd: %s", row.Axes["verb"], len(dump), dump, row.Cmd)
	}
	return 1, 1
}

func TestNeighWantFromAxes(t *testing.T) {
	base := map[string]string{"verb": "add", "family": "4", "dst": "192.0.2.1", "lladdr": "02:00:00:00:00:01", "state": "128"}
	with := func(kv ...string) map[string]string {
		m := maps.Clone(base)
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] == "" {
				delete(m, kv[i])
			} else {
				m[kv[i]] = kv[i+1]
			}
		}
		return m
	}
	cases := []struct {
		description string
		axes        map[string]string
		expected    func(w neighWant) bool
		wantErr     bool
	}{
		{"positive: an add sends NDA_DST and NDA_LLADDR, asking CREATE|EXCL", base, func(w neighWant) bool {
			return w.typ == unix.RTM_NEWNEIGH && w.flags == unix.NLM_F_CREATE|unix.NLM_F_EXCL &&
				slices.Equal(w.attrs, []uint16{unix.NDA_DST, unix.NDA_LLADDR}) && w.info.State == unix.NUD_PERMANENT
		}, false},
		{"positive: change asks REPLACE alone", with("verb", "change"), func(w neighWant) bool { return w.flags == unix.NLM_F_REPLACE }, false},
		{"positive: a delete asks nothing", with("verb", "del"), func(w neighWant) bool { return w.typ == unix.RTM_DELNEIGH && w.flags == 0 }, false},
		{"corner: no lladdr sends none", with("lladdr", ""), func(w neighWant) bool {
			return w.info.LLAddr == nil && !slices.Contains(w.attrs, unix.NDA_LLADDR)
		}, false},
		{"positive: a proxy entry sets NTF_PROXY", with("proxy", "1"), func(w neighWant) bool {
			return w.proxy && w.info.Flags == unix.NTF_PROXY
		}, false},
		{"positive: router and proxy combine", with("proxy", "1", "ntf", "128"), func(w neighWant) bool {
			return w.info.Flags == unix.NTF_PROXY|unix.NTF_ROUTER
		}, false},
		{"positive: an extended flag sends NDA_FLAGS_EXT", with("ntf_ext", "1", "state", "0"), func(w neighWant) bool {
			return w.info.FlagsExt == NtfExtManaged && w.info.State == 0 && slices.Contains(w.attrs, NdaFlagsExt)
		}, false},
		{"boundary: protocol 0 is still sent", with("protocol", "0"), func(w neighWant) bool {
			return w.info.HasProtocol && slices.Contains(w.attrs, NdaProtocol)
		}, false},
		{"corner: a two-byte lladdr keeps its length", with("lladdr", "02:00"), func(w neighWant) bool {
			return neighAttrWidth(w, unix.NDA_LLADDR) == 2
		}, false},
		{"negative: an unknown verb", with("verb", "flush"), nil, true},
		{"negative: no family", with("family", ""), nil, true},
		{"negative: no destination", with("dst", ""), nil, true},
		{"negative: an lladdr that is not hex", with("lladdr", "zz"), nil, true},
		{"negative: a state past u8", with("state", "256"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := neighWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

// ndBody is an ndmsg built from ni, then attrs.
func ndBody(ni NeighInfo, attrs ...[]byte) []byte {
	h := []byte{ni.Family, 0, 0, 0}
	h = binary.LittleEndian.AppendUint32(h, uint32(ni.Ifindex))
	h = binary.LittleEndian.AppendUint16(h, ni.State)
	h = append(h, ni.Flags, ni.Type)
	return cat(append([][]byte{h}, attrs...)...)
}

func TestNeighRequestProblem(t *testing.T) {
	w, err := neighWantFromAxes(map[string]string{"verb": "add", "family": "4", "dst": "192.0.2.1",
		"lladdr": "02:00:00:00:00:01", "state": "128", "ntf": "128", "ntf_ext": "4", "protocol": "4"})
	if err != nil {
		t.Fatal(err)
	}
	hdr := w.info
	hdr.Ifindex = 2
	dst := rtattr(unix.NDA_DST, []byte{192, 0, 2, 1})
	lla := rtattr(unix.NDA_LLADDR, []byte{2, 0, 0, 0, 0, 1})
	ext := rtattr(NdaFlagsExt, u32le(4))
	proto := rtattr(NdaProtocol, []byte{4})
	nlf := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	with := func(f func(n *NeighInfo)) NeighInfo { n := hdr; f(&n); return n }
	cases := []struct {
		description string
		flags       uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", nlf, ndBody(hdr, proto, dst, ext, lla), ""},
		{"positive: attribute order does not matter", nlf, ndBody(hdr, lla, ext, dst, proto), ""},
		{"negative: NDA_FLAGS_EXT missing", nlf, ndBody(hdr, proto, dst, lla), "attributes"},
		{"negative: NDA_PROTOCOL four bytes wide", nlf, ndBody(hdr, rtattr(NdaProtocol, u32le(4)), dst, ext, lla), "is 4 bytes, want 1"},
		{"negative: another state", nlf, ndBody(with(func(n *NeighInfo) { n.State = unix.NUD_REACHABLE }), proto, dst, ext, lla), "neighbor"},
		{"negative: the router flag missing", nlf, ndBody(with(func(n *NeighInfo) { n.Flags = 0 }), proto, dst, ext, lla), "neighbor"},
		{"negative: another device", nlf, ndBody(with(func(n *NeighInfo) { n.Ifindex = 3 }), proto, dst, ext, lla), "neighbor"},
		{"negative: NLM_F_REPLACE for an add", nlf | unix.NLM_F_REPLACE, ndBody(hdr, proto, dst, ext, lla), "nlmsg flags"},
		{"negative: another link-layer address", nlf, ndBody(hdr, proto, dst, ext, rtattr(unix.NDA_LLADDR, []byte{2, 0, 0, 0, 0, 2})), "neighbor"},
		{"boundary: a bare header is short of what the axes name", nlf, ndBody(hdr), "attributes"},
		{"boundary: a body shorter than ndmsg", nlf, []byte{2, 0, 0}, ErrNdMsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := neighRequestProblem(w, tc.flags, tc.body, 2)
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

func TestNeighDumpMatches(t *testing.T) {
	w := neighWant{info: NeighInfo{Family: unix.AF_INET, Ifindex: 2, State: unix.NUD_PERMANENT, Flags: unix.NTF_ROUTER,
		Dst: []byte{192, 0, 2, 1}, LLAddr: []byte{2, 0, 0, 0, 0, 1}, Protocol: 4, HasProtocol: true}}
	d := w.info
	with := func(base NeighInfo, f func(n *NeighInfo)) NeighInfo { n := base; f(&n); return n }
	wantWith := func(f func(n *neighWant)) neighWant { n := w; f(&n); return n }
	cases := []struct {
		description string
		w           neighWant
		d           NeighInfo
		expected    bool
	}{
		{"positive: the entry as stored", w, d, true},
		{"positive: the kernel adds flags of its own", w, with(d, func(n *NeighInfo) { n.Flags |= unix.NTF_EXT_LEARNED }), true},
		{"negative: the router flag dropped", w, with(d, func(n *NeighInfo) { n.Flags = 0 }), false},
		{"negative: another state for a permanent entry", w, with(d, func(n *NeighInfo) { n.State = unix.NUD_STALE }), false},
		{"negative: another link-layer address", w, with(d, func(n *NeighInfo) { n.LLAddr = []byte{2, 0, 0, 0, 0, 2} }), false},
		{"negative: the protocol not stored", w, with(d, func(n *NeighInfo) { n.HasProtocol, n.Protocol = false, 0 }), false},
		{"negative: another device", w, with(d, func(n *NeighInfo) { n.Ifindex = 3 }), false},
		{"corner: use hands the entry to the kernel, so its state is not compared",
			wantWith(func(n *neighWant) { n.info.Flags |= unix.NTF_USE }), with(d, func(n *NeighInfo) { n.State = unix.NUD_INCOMPLETE }), true},
		{"corner: a reachable entry may have gone stale",
			wantWith(func(n *neighWant) { n.info.State = unix.NUD_REACHABLE }), with(d, func(n *NeighInfo) { n.State = unix.NUD_STALE }), true},
		{"positive: a proxy entry needs only its address and NTF_PROXY",
			wantWith(func(n *neighWant) { n.proxy = true }), NeighInfo{Family: unix.AF_INET, Ifindex: 2, Dst: d.Dst, Flags: unix.NTF_PROXY, Protocol: 4, HasProtocol: true}, true},
		{"negative: a proxy entry without NTF_PROXY",
			wantWith(func(n *neighWant) { n.proxy = true }), NeighInfo{Family: unix.AF_INET, Ifindex: 2, Dst: d.Dst, Protocol: 4, HasProtocol: true}, false},
		{"corner: an invalid state stores no router flag",
			wantWith(func(n *neighWant) { n.info.State = unix.NUD_FAILED }), with(d, func(n *NeighInfo) { n.State, n.Flags = unix.NUD_FAILED, 0 }), true},
		{"corner: use stores no router flag",
			wantWith(func(n *neighWant) { n.info.Flags |= unix.NTF_USE }), with(d, func(n *NeighInfo) { n.Flags = 0 }), true},
		{"boundary: a dumped entry with nothing set", w, NeighInfo{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := neighDumpMatches(tc.w, tc.d); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
