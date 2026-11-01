package xtcpnl

import (
	"bytes"
	"encoding/binary"
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

// The ip address rows' oracles (coverage-status.md, "W3a"). O3 rebuilds
// the AddrInfo the request should parse to from the row's axes alone
// (tools/nlcapgen/matrix_addr.go), following ip/ipaddress.c
// ipaddr_modify, and checks the request's attribute set and widths. O4
// looks for the address in the dump read back after it.

// addrWant is the request a row's axes call for. info.Index is left 0:
// the device's index is the one ip looked d0 up as.
type addrWant struct {
	typ, flags uint16
	info       AddrInfo
	hdrFlags   uint8    // the header's ifa_flags: the flags if they fit in 8 bits
	attrs      []uint16 // every IFA_* the request carries, sorted
	noop       bool     // the kernel stores nothing (a zero IPv4 local)
}

// addrVerbs is each verb's message type and the flags ipaddr_modify's
// callers in do_ipaddr pass.
var addrVerbs = map[string]struct{ typ, flags uint16 }{
	"add":     {unix.RTM_NEWADDR, unix.NLM_F_CREATE | unix.NLM_F_EXCL},
	"replace": {unix.RTM_NEWADDR, unix.NLM_F_CREATE | unix.NLM_F_REPLACE},
	"change":  {unix.RTM_NEWADDR, unix.NLM_F_REPLACE},
	"del":     {unix.RTM_DELADDR, 0},
}

// addrComputedBroadcast is ipaddr_modify's "brd +" or "brd -": the base address
// with every bit past the prefix set or cleared, or nil when the prefix is
// longer than 30 bits.
func addrComputedBroadcast(base []byte, bitlen uint8, set bool) []byte {
	if bitlen > 30 || len(base) != 4 {
		return nil
	}
	host := uint32(0xffffffff) >> bitlen
	b := binary.BigEndian.Uint32(base)
	if set {
		b |= host
	} else {
		b &^= host
	}
	return binary.BigEndian.AppendUint32(nil, b)
}

// addrWantAddrs sets the local and peer addresses, the prefix length and
// the broadcast address.
func addrWantAddrs(ax map[string]string, del bool, w *addrWant) error {
	local, err := axAddr(ax, "local")
	if err != nil || local == nil {
		return errors.Join(err, errors.New("axis local missing"))
	}
	peer, err := axAddr(ax, "peer")
	if err != nil {
		return err
	}
	plen, _, err := axU8(ax, "prefixlen")
	peerLen, _, err2 := axU8(ax, "peer_len")
	if err := errors.Join(err, err2); err != nil {
		return err
	}
	w.info.Local, w.info.Address, w.info.Prefixlen = local, local, plen
	w.attrs = append(w.attrs, unix.IFA_LOCAL)
	base, bitlen := local, plen
	switch {
	case peer != nil:
		w.info.Address, w.info.Prefixlen, base, bitlen = peer, peerLen, peer, peerLen
		w.attrs = append(w.attrs, unix.IFA_ADDRESS)
	case del && w.info.Family == unix.AF_INET && ax["bare"] == "1":
		// The wildcard deletion: no IFA_ADDRESS, which ParseNewAddr fills
		// from IFA_LOCAL.
	default:
		w.attrs = append(w.attrs, unix.IFA_ADDRESS)
	}
	switch brd := ax["brd"]; brd {
	case "":
	case "+", "-":
		if !del {
			w.info.Broadcast = addrComputedBroadcast(base, bitlen, brd == "+")
		}
	default:
		if w.info.Broadcast, err = axAddr(ax, "brd"); err != nil {
			return err
		}
	}
	if w.info.Broadcast != nil {
		w.attrs = append(w.attrs, unix.IFA_BROADCAST)
	}
	return nil
}

// addrWantFromAxes models ipaddr_modify.
func addrWantFromAxes(ax map[string]string) (addrWant, error) {
	var w addrWant
	verb, ok := addrVerbs[ax["verb"]]
	if !ok {
		return w, fmt.Errorf("axis verb=%q", ax["verb"])
	}
	w.typ, w.flags = verb.typ, verb.flags
	del := w.typ == unix.RTM_DELADDR
	switch ax["family"] {
	case "4":
		w.info.Family = unix.AF_INET
	case "6":
		w.info.Family = unix.AF_INET6
	default:
		return w, fmt.Errorf("axis family=%q", ax["family"])
	}
	if err := addrWantAddrs(ax, del, &w); err != nil {
		return w, err
	}
	scope, scoped, err := axU8(ax, "scope")
	if err != nil {
		return w, err
	}
	// default_scope: 127/8 is RT_SCOPE_HOST; a delete leaves the scope 0.
	if !scoped && !del && w.info.Family == unix.AF_INET && w.info.Local[0] == 127 {
		scope = unix.RT_SCOPE_HOST
	}
	w.info.Scope = scope
	if l, ok := ax["label"]; ok {
		w.info.Label = l
		w.attrs = append(w.attrs, unix.IFA_LABEL)
	}
	metric, hasMetric, err := axU32(ax, "metric")
	flags, _, err2 := axU32(ax, "flags")
	proto, hasProto, err3 := axU8(ax, "proto")
	valid, hasValid, err4 := axU32(ax, "valid")
	preferred, _, err5 := axU32(ax, "preferred")
	if err := errors.Join(err, err2, err3, err4, err5); err != nil {
		return w, err
	}
	if hasMetric {
		w.info.RtPriority, w.info.HasRtPriority = metric, true
		w.attrs = append(w.attrs, unix.IFA_RT_PRIORITY)
	}
	w.info.Flags = flags
	if flags <= 0xff {
		w.hdrFlags = uint8(flags)
	} else {
		w.attrs = append(w.attrs, unix.IFA_FLAGS)
	}
	if hasProto {
		w.info.Proto, w.info.HasProto = proto, true
		w.attrs = append(w.attrs, IfaProto)
	}
	if hasValid {
		w.info.CacheInfo = IfaCacheinfo{Preferred: preferred, Valid: valid}
		w.info.HasCacheInfo = true
		w.attrs = append(w.attrs, unix.IFA_CACHEINFO)
	}
	w.noop = ax["noop"] == "1"
	slices.Sort(w.attrs)
	return w, nil
}

// addrAttrWidth is the length attribute a must have in w, or -1 if any.
func addrAttrWidth(w addrWant, a uint16) int {
	alen := 4
	if w.info.Family == unix.AF_INET6 {
		alen = 16
	}
	switch a {
	case unix.IFA_LOCAL, unix.IFA_ADDRESS, unix.IFA_BROADCAST:
		return alen
	case unix.IFA_LABEL:
		return len(w.info.Label) + 1
	case unix.IFA_RT_PRIORITY, unix.IFA_FLAGS:
		return 4
	case unix.IFA_CACHEINFO:
		return IfaCacheinfoSizeCst
	case IfaProto:
		return 1
	}
	return -1
}

// addrRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr; index is d0's.
func addrRequestProblem(w addrWant, flags uint16, body []byte, index uint32) error {
	var errs []error
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	if len(body) < IfAddrmsgSizeCst {
		return ErrIfAddrmsgSmall
	}
	if body[2] != w.hdrFlags {
		errs = append(errs, fmt.Errorf("header ifa_flags %#x, want %#x", body[2], w.hdrFlags))
	}
	var attrs []uint16
	err := WalkRTAttrs(body[IfAddrmsgSizeCst:], func(a uint16, val []byte) {
		attrs = append(attrs, a)
		if n := addrAttrWidth(w, a); n >= 0 && len(val) != n {
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
	ai, err := ParseNewAddr(body)
	if err != nil {
		return err
	}
	want := w.info
	want.Index = index
	if !reflect.DeepEqual(ai, want) {
		errs = append(errs, fmt.Errorf("address %+v\n  want %+v", ai, want))
	}
	return errors.Join(errs...)
}

// addrDumpMatches reports whether a dumped address is the one the request
// stored. The kernel reports what it keeps: IPv4 keeps the label (the
// device's name when none is given), the scope and the broadcast address;
// IPv6 keeps none of them, and drops IFA_F_OPTIMISTIC while optimistic DAD
// is off. A zero metric or protocol is not dumped.
func addrDumpMatches(w AddrInfo, d AddrInfo) bool {
	keep := w.Flags &^ unix.IFA_F_OPTIMISTIC
	same := d.Family == w.Family && d.Index == w.Index && bytes.Equal(d.Local, w.Local) &&
		bytes.Equal(d.Address, w.Address) && d.Prefixlen == w.Prefixlen &&
		d.RtPriority == w.RtPriority && d.Proto == w.Proto && d.Flags&keep == keep
	if !same || w.Family != unix.AF_INET {
		return same
	}
	label := w.Label
	if label == "" {
		label = "d0"
	}
	return d.Label == label && d.Scope == w.Scope && bytes.Equal(d.Broadcast, w.Broadcast)
}

// wsAddrDump returns the addresses a readback's dump replied with.
func wsAddrDump(t *testing.T, msgs []capturedNlMsg) []AddrInfo {
	t.Helper()
	var out []AddrInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWADDR || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		ai, err := ParseNewAddr(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ai)
	}
	return out
}

// wsLinkIndex is the index of the device ip looked up: the RTM_NEWLINK the
// kernel answered ll_name_to_index's RTM_GETLINK with, or 0 if none.
func wsLinkIndex(msgs []capturedNlMsg) uint32 {
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		var ifi IfInfomsg
		if _, err := DeserializeIfInfomsg(m.body, &ifi); err == nil && ifi.Index > 0 {
			return uint32(ifi.Index)
		}
	}
	return 0
}

// TestWsAddrCorpus runs O3 and O4 over every generated address row, and
// fails if O3 checked nothing.
func TestWsAddrCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "addr4/") && !strings.HasPrefix(row.ID, "addr6/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsAddrRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d address requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no address request: are there address rows under testdata/*/ws?")
	}
}

func wsAddrRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	w, err := addrWantFromAxes(row.Axes)
	if err != nil {
		// A value iproute2 refuses to parse has no facts to model; it
		// must send no address request at all.
		sent := len(wsRouteRequests(msgs, unix.RTM_NEWADDR)) + len(wsRouteRequests(msgs, unix.RTM_DELADDR))
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
	index := wsLinkIndex(msgs)
	if index == 0 {
		t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of d0\n  cmd: %s", row.Cmd)
	}
	if err := addrRequestProblem(w, reqs[0].h.Flags, reqs[0].body, index); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	want := w.info
	want.Index = index
	dump := wsAddrDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	if w.typ == unix.RTM_DELADDR || w.noop {
		if slices.ContainsFunc(dump, func(d AddrInfo) bool { return bytes.Equal(d.Local, want.Local) }) {
			t.Errorf("O4: after %s the dump still holds %v\n  cmd: %s", row.Axes["verb"], want.Local, row.Cmd)
		}
		return 1, 1
	}
	if !slices.ContainsFunc(dump, func(d AddrInfo) bool { return addrDumpMatches(want, d) }) {
		t.Errorf("O4: after %s none of the dump's %d addresses is it: %+v\n  cmd: %s", row.Axes["verb"], len(dump), dump, row.Cmd)
	}
	return 1, 1
}

func TestAddrComputedBroadcast(t *testing.T) {
	cases := []struct {
		description string
		base        []byte
		bitlen      uint8
		set         bool
		expected    []byte
	}{
		{"positive: brd + on a /24 sets the host byte", []byte{192, 0, 2, 1}, 24, true, []byte{192, 0, 2, 255}},
		{"positive: brd - on a /24 clears it", []byte{192, 0, 2, 1}, 24, false, []byte{192, 0, 2, 0}},
		{"boundary: a /30 still gets one", []byte{192, 0, 2, 1}, 30, true, []byte{192, 0, 2, 3}},
		{"boundary: a /31 gets none", []byte{192, 0, 2, 0}, 31, true, nil},
		{"boundary: a /0 sets every bit", []byte{192, 0, 2, 1}, 0, true, []byte{255, 255, 255, 255}},
		{"corner: an IPv6 base gets none", make([]byte, 16), 64, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := addrComputedBroadcast(tc.base, tc.bitlen, tc.set); !bytes.Equal(got, tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestAddrWantFromAxes(t *testing.T) {
	base := map[string]string{"verb": "add", "family": "4", "local": "192.0.2.1", "prefixlen": "24"}
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
		expected    func(w addrWant) bool
		wantErr     bool
	}{
		{"positive: an add sends IFA_LOCAL and IFA_ADDRESS, asking CREATE|EXCL", base, func(w addrWant) bool {
			return w.typ == unix.RTM_NEWADDR && w.flags == unix.NLM_F_CREATE|unix.NLM_F_EXCL &&
				slices.Equal(w.attrs, []uint16{unix.IFA_ADDRESS, unix.IFA_LOCAL}) && w.info.Prefixlen == 24
		}, false},
		{"positive: change asks REPLACE alone", with("verb", "change"), func(w addrWant) bool {
			return w.flags == unix.NLM_F_REPLACE
		}, false},
		{"positive: a peer is IFA_ADDRESS and gives the prefix length", with("peer", "198.51.100.0", "peer_len", "24"), func(w addrWant) bool {
			return bytes.Equal(w.info.Address, []byte{198, 51, 100, 0}) && w.info.Prefixlen == 24
		}, false},
		{"corner: an IPv4 delete without a prefix length leaves IFA_ADDRESS out", with("verb", "del", "bare", "1", "prefixlen", "32"), func(w addrWant) bool {
			return !slices.Contains(w.attrs, unix.IFA_ADDRESS) && bytes.Equal(w.info.Address, w.info.Local)
		}, false},
		{"corner: an IPv6 address without one keeps it", with("family", "6", "local", "2001:db8::1", "prefixlen", "128", "verb", "del", "bare", "1"), func(w addrWant) bool {
			return slices.Contains(w.attrs, unix.IFA_ADDRESS)
		}, false},
		{"positive: brd + comes from the peer when there is one", with("peer", "198.51.100.0", "peer_len", "24", "brd", "+"), func(w addrWant) bool {
			return bytes.Equal(w.info.Broadcast, []byte{198, 51, 100, 255})
		}, false},
		{"corner: a delete computes no broadcast", with("verb", "del", "brd", "+"), func(w addrWant) bool {
			return w.info.Broadcast == nil && !slices.Contains(w.attrs, unix.IFA_BROADCAST)
		}, false},
		{"corner: 127/8 defaults to host scope", with("local", "127.0.0.2", "prefixlen", "8"), func(w addrWant) bool {
			return w.info.Scope == unix.RT_SCOPE_HOST
		}, false},
		{"corner: a delete of 127/8 leaves the scope 0", with("local", "127.0.0.2", "verb", "del"), func(w addrWant) bool {
			return w.info.Scope == 0
		}, false},
		{"boundary: flags 0xff stay in the header", with("flags", "255"), func(w addrWant) bool {
			return w.hdrFlags == 0xff && !slices.Contains(w.attrs, unix.IFA_FLAGS)
		}, false},
		{"boundary: flags 0x100 go in IFA_FLAGS, the header 0", with("flags", "256"), func(w addrWant) bool {
			return w.hdrFlags == 0 && w.info.Flags == 256 && slices.Contains(w.attrs, unix.IFA_FLAGS)
		}, false},
		{"positive: lifetimes send IFA_CACHEINFO", with("valid", "100", "preferred", "50"), func(w addrWant) bool {
			return w.info.HasCacheInfo && w.info.CacheInfo == IfaCacheinfo{Preferred: 50, Valid: 100}
		}, false},
		{"positive: a label is sent with its NUL", with("label", "d0:1"), func(w addrWant) bool {
			return addrAttrWidth(w, unix.IFA_LABEL) == 5
		}, false},
		{"corner: a zero local is a no-op", with("local", "0.0.0.0", "noop", "1"), func(w addrWant) bool { return w.noop }, false},
		{"negative: an unknown verb", with("verb", "flush"), nil, true},
		{"negative: no family", with("family", ""), nil, true},
		{"negative: no local", with("local", ""), nil, true},
		{"negative: a proto past u8", with("proto", "256"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := addrWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

// ifAddrBody is an ifaddrmsg built from ai and header flags, then attrs.
func ifAddrBody(ai AddrInfo, hdrFlags uint8, attrs ...[]byte) []byte {
	h := []byte{ai.Family, ai.Prefixlen, hdrFlags, ai.Scope}
	h = binary.LittleEndian.AppendUint32(h, ai.Index)
	return cat(append([][]byte{h}, attrs...)...)
}

func TestAddrRequestProblem(t *testing.T) {
	want := func(ax map[string]string) addrWant {
		w, err := addrWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	w := want(map[string]string{"verb": "add", "family": "4", "local": "192.0.2.1", "prefixlen": "24",
		"label": "d0:1", "metric": "100", "flags": "512"})
	hdr := w.info
	hdr.Index = 2
	local := rtattr(unix.IFA_LOCAL, []byte{192, 0, 2, 1})
	addr := rtattr(unix.IFA_ADDRESS, []byte{192, 0, 2, 1})
	label := rtattr(unix.IFA_LABEL, []byte("d0:1\x00"))
	metric := rtattr(unix.IFA_RT_PRIORITY, u32le(100))
	flagsAttr := rtattr(unix.IFA_FLAGS, u32le(512))
	nlf := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	otherIndex := hdr
	otherIndex.Index = 3
	cases := []struct {
		description string
		flags       uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", nlf, ifAddrBody(hdr, 0, local, addr, label, metric, flagsAttr), ""},
		{"positive: attribute order does not matter", nlf, ifAddrBody(hdr, 0, flagsAttr, metric, label, addr, local), ""},
		{"negative: the flags in the header as well", nlf, ifAddrBody(hdr, 0xff, local, addr, label, metric, flagsAttr), "header ifa_flags"},
		{"negative: IFA_FLAGS missing", nlf, ifAddrBody(hdr, 0, local, addr, label, metric), "attributes"},
		{"negative: the label without its NUL", nlf, ifAddrBody(hdr, 0, local, addr, rtattr(unix.IFA_LABEL, []byte("d0:1")), metric, flagsAttr), "is 4 bytes, want 5"},
		{"negative: IFA_RT_PRIORITY two bytes wide", nlf, ifAddrBody(hdr, 0, local, addr, label, rtattr(unix.IFA_RT_PRIORITY, []byte{100, 0}), flagsAttr), "is 2 bytes, want 4"},
		{"negative: another device's index", nlf, ifAddrBody(otherIndex, 0, local, addr, label, metric, flagsAttr), "address"},
		{"negative: NLM_F_REPLACE for an add", nlf | unix.NLM_F_REPLACE, ifAddrBody(hdr, 0, local, addr, label, metric, flagsAttr), "nlmsg flags"},
		{"negative: another local address", nlf, ifAddrBody(hdr, 0, rtattr(unix.IFA_LOCAL, []byte{192, 0, 2, 2}), addr, label, metric, flagsAttr), "address"},
		{"boundary: a bare header is short of what the axes name", nlf, ifAddrBody(hdr, 0), "attributes"},
		{"boundary: a body shorter than ifaddrmsg", nlf, []byte{2, 24, 0}, ErrIfAddrmsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := addrRequestProblem(w, tc.flags, tc.body, 2)
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

func TestAddrDumpMatches(t *testing.T) {
	w := AddrInfo{Family: unix.AF_INET, Prefixlen: 24, Index: 2, Local: []byte{192, 0, 2, 1}, Address: []byte{192, 0, 2, 1},
		Flags: unix.IFA_F_NOPREFIXROUTE, Broadcast: []byte{192, 0, 2, 255}}
	d := w
	d.Label, d.Flags = "d0", unix.IFA_F_NOPREFIXROUTE|unix.IFA_F_PERMANENT
	with := func(base AddrInfo, f func(a *AddrInfo)) AddrInfo { a := base; f(&a); return a }
	v6 := AddrInfo{Family: unix.AF_INET6, Prefixlen: 64, Index: 2, Local: make([]byte, 16), Address: make([]byte, 16),
		Flags: unix.IFA_F_NODAD | unix.IFA_F_OPTIMISTIC, Label: "d0:1", Scope: 253}
	cases := []struct {
		description string
		w, d        AddrInfo
		expected    bool
	}{
		{"positive: the address as stored, labeled with the device's name", w, d, true},
		{"positive: the kernel adds flags of its own", w, with(d, func(a *AddrInfo) { a.Flags |= unix.IFA_F_SECONDARY }), true},
		{"negative: a requested flag dropped", w, with(d, func(a *AddrInfo) { a.Flags = unix.IFA_F_PERMANENT }), false},
		{"negative: another label", with(w, func(a *AddrInfo) { a.Label = "d0:1" }), d, false},
		{"negative: another broadcast", w, with(d, func(a *AddrInfo) { a.Broadcast = []byte{192, 0, 2, 127} }), false},
		{"negative: another prefix length", w, with(d, func(a *AddrInfo) { a.Prefixlen = 25 }), false},
		{"negative: another device", w, with(d, func(a *AddrInfo) { a.Index = 3 }), false},
		{"negative: a metric not stored", with(w, func(a *AddrInfo) { a.RtPriority = 5 }), d, false},
		{"corner: IPv6 keeps no label or scope, and drops optimistic", v6,
			with(v6, func(a *AddrInfo) { a.Label, a.Scope, a.Flags = "", 0, unix.IFA_F_NODAD|unix.IFA_F_PERMANENT }), true},
		{"boundary: a dumped address with nothing set", w, AddrInfo{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := addrDumpMatches(tc.w, tc.d); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
