package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip route get rows' oracles (coverage-status.md, "W4b"). O3 rebuilds
// the lookup from the row's axes (tools/nlcapgen/matrix_routeget.go),
// following ip/iproute.c iproute_get: the rtmsg, then each attribute's
// bytes. O4 holds the kernel's reply to the lookup: the /32 or /128 it
// resolved, or with fibmatch the FIB entry that matched it, out of d0.

// rtaFlowlabel is RTA_FLOWLABEL, which golang.org/x/sys/unix does not
// export: the rtattr_type_t enum runs RTA_NH_ID 30, RTA_FLOWLABEL 31
// (include/uapi/linux/rtnetlink.h).
const rtaFlowlabel = RtaNhID + 1

// routeGetWant is the lookup a row's axes call for.
type routeGetWant struct {
	hdr   RtMsg
	attrs map[uint16][]byte // RTA_OIF, when wanted, is filled in with d0's index
	oif   bool
	fib   bool // fibmatch
	dst   []byte
}

// routeGetWantFromAxes models iproute_get. The ports and the flow label
// are read with get_be16 and get_be32, so they go in network order; the
// mark is sent only when it is not 0; IPv4 always asks
// RTM_F_LOOKUP_TABLE.
func routeGetWantFromAxes(ax map[string]string) (routeGetWant, error) {
	w := routeGetWant{attrs: map[uint16][]byte{}}
	alen := uint8(4)
	switch ax["family"] {
	case "4":
		w.hdr.Family, w.hdr.Flags = unix.AF_INET, unix.RTM_F_LOOKUP_TABLE
	case "6":
		w.hdr.Family, alen = unix.AF_INET6, 16
	default:
		return w, fmt.Errorf("axis family=%q", ax["family"])
	}
	dst, err := axAddr(ax, "dst")
	if err != nil || dst == nil {
		return w, errors.Join(err, errors.New("axis dst missing"))
	}
	w.dst = dst
	w.hdr.DstLen = alen * 8
	w.attrs[unix.RTA_DST] = dst
	if src, err := axAddr(ax, "src"); err != nil || src != nil {
		if err != nil {
			return w, err
		}
		w.hdr.SrcLen = alen * 8
		w.attrs[unix.RTA_SRC] = src
	}
	tos, _, err := axU8(ax, "tos")
	if err != nil {
		return w, err
	}
	w.hdr.Tos = tos
	if ax["fibmatch"] == "1" {
		w.hdr.Flags |= unix.RTM_F_FIB_MATCH
		w.fib = true
	}
	if ax["notify"] == "1" {
		w.hdr.Flags |= unix.RTM_F_NOTIFY
	}
	for _, u := range []struct {
		axis string
		attr uint16
		enc  func(uint32) []byte
	}{
		{"uid", unix.RTA_UID, func(n uint32) []byte { return u32le(n) }},
		{"mark", unix.RTA_MARK, func(n uint32) []byte { return u32le(n) }},
		{"flowlabel", rtaFlowlabel, func(n uint32) []byte { return binary.BigEndian.AppendUint32(nil, n) }},
		{"sport", unix.RTA_SPORT, func(n uint32) []byte { return binary.BigEndian.AppendUint16(nil, uint16(n)) }},
		{"dport", unix.RTA_DPORT, func(n uint32) []byte { return binary.BigEndian.AppendUint16(nil, uint16(n)) }},
	} {
		n, ok, err := axU32(ax, u.axis)
		if err != nil {
			return w, err
		}
		if ok {
			w.attrs[u.attr] = u.enc(n)
		}
	}
	if p, ok, err := axU8(ax, "ipproto"); err != nil || ok {
		if err != nil {
			return w, err
		}
		w.attrs[unix.RTA_IP_PROTO] = []byte{p}
	}
	w.oif = ax["oif"] == "d0"
	return w, nil
}

// routeGetRequestProblem is O3: how a captured lookup differs from the
// model's. d0 is d0's index, for RTA_OIF.
func routeGetRequestProblem(w routeGetWant, nlFlags uint16, body []byte, d0 uint32) error {
	var errs []error
	if got := nlFlags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != 0 {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want none", got))
	}
	var m RtMsg
	if _, err := DeserializeRtMsg(body, &m); err != nil {
		return err
	}
	if m != w.hdr {
		errs = append(errs, fmt.Errorf("rtmsg %+v, want %+v", m, w.hdr))
	}
	want := maps.Clone(w.attrs)
	if w.oif {
		want[unix.RTA_OIF] = u32le(d0)
	}
	got := map[uint16][]byte{}
	err := WalkRTAttrs(body[RtMsgSizeCst:], func(a uint16, val []byte) {
		if _, dup := got[a]; dup {
			errs = append(errs, fmt.Errorf("attribute %d twice", a))
		}
		got[a] = val
	})
	if err != nil {
		return err
	}
	for _, a := range slices.Sorted(maps.Keys(want)) {
		if g, ok := got[a]; !ok || !bytes.Equal(g, want[a]) {
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

// routeGetReplyProblem is O4: how the kernel's answer differs from the
// lookup. Every route in the namespace leaves through d0. Without fibmatch
// the answer is the destination itself, at full length; with it, the FIB
// entry that matched, which is shorter and holds the destination (or is
// the default route, with no RTA_DST).
func routeGetReplyProblem(w routeGetWant, r RouteInfo, d0 uint32) error {
	var errs []error
	full := uint8(len(w.dst) * 8)
	if r.Oif != d0 {
		errs = append(errs, fmt.Errorf("oif %d, want d0's %d", r.Oif, d0))
	}
	if !w.fib {
		if r.DstLen != full || !bytes.Equal(r.Dst, w.dst) {
			errs = append(errs, fmt.Errorf("dst %v/%d, want %v/%d", r.Dst, r.DstLen, w.dst, full))
		}
		return errors.Join(errs...)
	}
	if r.DstLen >= full || !prefixHolds(r.Dst, r.DstLen, w.dst) {
		errs = append(errs, fmt.Errorf("fibmatch entry %v/%d does not hold %v", r.Dst, r.DstLen, w.dst))
	}
	return errors.Join(errs...)
}

// prefixHolds reports whether addr is in prefix/plen; a nil prefix of
// length 0 is the default route, which holds everything.
func prefixHolds(prefix []byte, plen uint8, addr []byte) bool {
	if plen == 0 {
		return true
	}
	if len(prefix) != len(addr) || int(plen) > 8*len(addr) {
		return false
	}
	for i := 0; i < int(plen); i++ {
		bit := byte(0x80 >> (i % 8))
		if prefix[i/8]&bit != addr[i/8]&bit {
			return false
		}
	}
	return true
}

// TestWsRouteGetCorpus runs O3 and O4 over every generated route get row,
// and fails if O3 checked nothing.
func TestWsRouteGetCorpus(t *testing.T) {
	var checked, replies int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "routeget4/") && !strings.HasPrefix(row.ID, "routeget6/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, r := wsRouteGetRow(t, dir, row)
			checked += c
			replies += r
		})
	}
	t.Logf("O3 checked %d lookups, O4 %d replies", checked, replies)
	if checked == 0 {
		t.Error("O3 checked no lookup: are there route get rows under testdata/*/ws?")
	}
}

func wsRouteGetRow(t *testing.T, dir string, row wsRow) (checked, replies int) {
	t.Helper()
	msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
	reqs := wsRouteRequests(msgs, unix.RTM_GETROUTE)
	w, err := routeGetWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	rc := wsReadRC(t, dir)
	if len(reqs) == 0 {
		if rc == 0 {
			t.Errorf("O3: rc 0 but no RTM_GETROUTE\n  cmd: %s", row.Cmd)
		}
		return 0, 0
	}
	if len(reqs) != 1 {
		t.Fatalf("O3: %d RTM_GETROUTE requests, want 1\n  cmd: %s", len(reqs), row.Cmd)
	}
	d0 := uint32(wsLinkIndexOf(msgs, "d0"))
	if w.oif && d0 == 0 {
		t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of d0\n  cmd: %s", row.Cmd)
	}
	if err := routeGetRequestProblem(w, reqs[0].h.Flags, reqs[0].body, d0); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	var answers []RouteInfo
	for _, m := range msgs {
		if m.proto == unix.NETLINK_ROUTE && m.h.Type == unix.RTM_NEWROUTE && m.h.Flags&unix.NLM_F_REQUEST == 0 {
			r, err := ParseNewRoute(m.body)
			if err != nil {
				t.Fatal(err)
			}
			answers = append(answers, r)
		}
	}
	if len(answers) != 1 {
		t.Fatalf("O4: %d answers, want 1\n  cmd: %s", len(answers), row.Cmd)
	}
	if d0 == 0 {
		d0 = answers[0].Oif // no lookup of d0: hold the answer to itself on oif
	}
	if err := routeGetReplyProblem(w, answers[0], d0); err != nil {
		t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
	}
	return 1, 1
}

func TestRouteGetWantFromAxes(t *testing.T) {
	base := map[string]string{"family": "4", "dst": "192.0.2.1"}
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
		expected    func(w routeGetWant) bool
		wantErr     bool
	}{
		{"positive: an IPv4 lookup asks RTM_F_LOOKUP_TABLE for a /32", base, func(w routeGetWant) bool {
			return w.hdr.Flags == unix.RTM_F_LOOKUP_TABLE && w.hdr.DstLen == 32 && len(w.attrs) == 1
		}, false},
		{"positive: an IPv6 one asks no flags for a /128", with("family", "6", "dst", "2001:db8::1"), func(w routeGetWant) bool {
			return w.hdr.Flags == 0 && w.hdr.DstLen == 128
		}, false},
		{"positive: from sets RTA_SRC and the source length", with("src", "192.0.2.10"), func(w routeGetWant) bool {
			return w.hdr.SrcLen == 32 && bytes.Equal(w.attrs[unix.RTA_SRC], []byte{192, 0, 2, 10})
		}, false},
		{"corner: ports are network order", with("sport", "1234", "dport", "80"), func(w routeGetWant) bool {
			return bytes.Equal(w.attrs[unix.RTA_SPORT], []byte{0x04, 0xd2}) && bytes.Equal(w.attrs[unix.RTA_DPORT], []byte{0, 80})
		}, false},
		{"corner: the flow label is network order", with("family", "6", "dst", "2001:db8::1", "flowlabel", "74565"), func(w routeGetWant) bool {
			return bytes.Equal(w.attrs[rtaFlowlabel], []byte{0, 1, 0x23, 0x45})
		}, false},
		{"positive: the mark and uid are host order", with("mark", "16", "uid", "1000"), func(w routeGetWant) bool {
			return bytes.Equal(w.attrs[unix.RTA_MARK], u32le(16)) && bytes.Equal(w.attrs[unix.RTA_UID], u32le(1000))
		}, false},
		{"positive: fibmatch and notify are flags", with("fibmatch", "1", "notify", "1"), func(w routeGetWant) bool {
			return w.fib && w.hdr.Flags == unix.RTM_F_LOOKUP_TABLE|unix.RTM_F_FIB_MATCH|unix.RTM_F_NOTIFY
		}, false},
		{"positive: tos goes in the header", with("tos", "16"), func(w routeGetWant) bool { return w.hdr.Tos == 16 }, false},
		{"positive: ipproto is a byte", with("ipproto", "6"), func(w routeGetWant) bool {
			return bytes.Equal(w.attrs[unix.RTA_IP_PROTO], []byte{6})
		}, false},
		{"negative: no family", with("family", ""), nil, true},
		{"negative: no destination", with("dst", ""), nil, true},
		{"negative: a tos past u8", with("tos", "256"), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := routeGetWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

func TestRouteGetRequestProblem(t *testing.T) {
	w, err := routeGetWantFromAxes(map[string]string{"family": "4", "dst": "192.0.2.1", "dport": "80", "oif": "d0"})
	if err != nil {
		t.Fatal(err)
	}
	dst := rtattr(unix.RTA_DST, []byte{192, 0, 2, 1})
	dport := rtattr(unix.RTA_DPORT, []byte{0, 80})
	oif := rtattr(unix.RTA_OIF, u32le(2))
	nlf := uint16(unix.NLM_F_REQUEST)
	with := func(f func(m *RtMsg)) RtMsg { m := w.hdr; f(&m); return m }
	cases := []struct {
		description string
		nlFlags     uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the lookup the axes call for", nlf, rtmsgBody(w.hdr, dst, dport, oif), ""},
		{"positive: attribute order does not matter", nlf, rtmsgBody(w.hdr, oif, dport, dst), ""},
		{"negative: the port in host order", nlf, rtmsgBody(w.hdr, dst, rtattr(unix.RTA_DPORT, []byte{80, 0}), oif), "attribute 29"},
		{"negative: no RTM_F_LOOKUP_TABLE", nlf, rtmsgBody(with(func(m *RtMsg) { m.Flags = 0 }), dst, dport, oif), "rtmsg"},
		{"negative: a /24 lookup", nlf, rtmsgBody(with(func(m *RtMsg) { m.DstLen = 24 }), dst, dport, oif), "rtmsg"},
		{"negative: another device", nlf, rtmsgBody(w.hdr, dst, dport, rtattr(unix.RTA_OIF, u32le(3))), "attribute 4"},
		{"negative: a mark of 0 sent", nlf, rtmsgBody(w.hdr, dst, dport, oif, rtattr(unix.RTA_MARK, u32le(0))), "not asked for"},
		{"negative: NLM_F_DUMP", nlf | unix.NLM_F_DUMP, rtmsgBody(w.hdr, dst, dport, oif), "nlmsg flags"},
		{"boundary: a body shorter than rtmsg", nlf, []byte{2, 32}, ErrRtMsgSmall.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := routeGetRequestProblem(w, tc.nlFlags, tc.body, 2)
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

func TestRouteGetReplyProblem(t *testing.T) {
	plain, _ := routeGetWantFromAxes(map[string]string{"family": "4", "dst": "198.51.100.7"})
	fib, _ := routeGetWantFromAxes(map[string]string{"family": "4", "dst": "192.0.2.1", "fibmatch": "1"})
	fibDefault, _ := routeGetWantFromAxes(map[string]string{"family": "4", "dst": "198.51.100.7", "fibmatch": "1"})
	cases := []struct {
		description string
		w           routeGetWant
		r           RouteInfo
		expected    string // in the error; "" for none
	}{
		{"positive: the destination at full length, out of d0", plain, RouteInfo{Dst: []byte{198, 51, 100, 7}, DstLen: 32, Oif: 2}, ""},
		{"negative: another device", plain, RouteInfo{Dst: []byte{198, 51, 100, 7}, DstLen: 32, Oif: 3}, "oif"},
		{"negative: the default route without fibmatch", plain, RouteInfo{Oif: 2}, "dst"},
		{"positive: fibmatch answers the connected prefix", fib, RouteInfo{Dst: []byte{192, 0, 2, 0}, DstLen: 24, Oif: 2}, ""},
		{"positive: fibmatch answers the default route, with no RTA_DST", fibDefault, RouteInfo{Oif: 2}, ""},
		{"negative: fibmatch answering a prefix that doesn't hold it", fib, RouteInfo{Dst: []byte{198, 51, 100, 0}, DstLen: 24, Oif: 2}, "does not hold"},
		{"negative: fibmatch answering the host route", fib, RouteInfo{Dst: []byte{192, 0, 2, 1}, DstLen: 32, Oif: 2}, "does not hold"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := routeGetReplyProblem(tc.w, tc.r, 2)
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

func TestPrefixHolds(t *testing.T) {
	cases := []struct {
		description string
		prefix      []byte
		plen        uint8
		addr        []byte
		expected    bool
	}{
		{"positive: inside a /24", []byte{192, 0, 2, 0}, 24, []byte{192, 0, 2, 9}, true},
		{"negative: outside it", []byte{192, 0, 2, 0}, 24, []byte{192, 0, 3, 9}, false},
		{"boundary: a /0 holds everything", nil, 0, []byte{1, 2, 3, 4}, true},
		{"corner: a /25 cuts inside a byte", []byte{192, 0, 2, 128}, 25, []byte{192, 0, 2, 127}, false},
		{"corner: a /20 holds an address whose third byte differs past the cut", []byte{192, 0, 16, 0}, 20, []byte{192, 0, 31, 9}, true},
		{"negative: another family's length", []byte{192, 0, 2, 0}, 24, make([]byte, 16), false},
		{"negative: a length past the address", []byte{192, 0, 2, 0}, 33, []byte{192, 0, 2, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := prefixHolds(tc.prefix, tc.plen, tc.addr); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
