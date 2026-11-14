package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip route rows' oracles (coverage-status.md, "Grammar matrix (W2)").
// A generated row carries axes: what its values mean, written by
// tools/nlcapgen/matrix_route.go independently of the command's spelling.
// O3 rebuilds the request ip should send from the axes alone, following
// ip/iproute.c iproute_modify, and compares it attribute by attribute,
// width included. O4 looks for the route in the dump read back after it.

// routeTypes maps the axes' type names to RTN_*.
var routeTypes = map[string]uint8{
	"unicast": unix.RTN_UNICAST, "local": unix.RTN_LOCAL, "broadcast": unix.RTN_BROADCAST,
	"anycast": unix.RTN_ANYCAST, "multicast": unix.RTN_MULTICAST, "blackhole": unix.RTN_BLACKHOLE,
	"unreachable": unix.RTN_UNREACHABLE, "prohibit": unix.RTN_PROHIBIT, "throw": unix.RTN_THROW,
}

// routeVerbFlags is each verb's nlmsg flags beyond NLM_F_REQUEST|NLM_F_ACK
// (ip/iproute.c do_iproute).
var routeVerbFlags = map[string]uint16{
	"add":     unix.NLM_F_CREATE | unix.NLM_F_EXCL,
	"replace": unix.NLM_F_CREATE | unix.NLM_F_REPLACE,
	"append":  unix.NLM_F_CREATE | unix.NLM_F_APPEND,
	"prepend": unix.NLM_F_CREATE,
	"del":     0,
}

// routeMP is one multipath entry the axes name: its gateway and weight.
type routeMP struct {
	gw     []byte
	weight int
}

// routeWant is the request a row's axes call for.
type routeWant struct {
	typ, flags uint16
	hdr        RtMsg
	attrs      []uint16 // every RTA_* the request carries, sorted
	dst        []byte
	table      uint32 // RTA_TABLE when attrs holds it
	priority   uint32
	gw         []byte
	via        *RtVia
	mp         []routeMP
	nhid       uint32
	prefsrc    []byte
	pref       uint8
	expires    uint32
	mtu        uint32
	mtuLock    bool
	advmss     uint32
}

func axU32(ax map[string]string, k string) (uint32, bool, error) {
	s, ok := ax[k]
	if !ok {
		return 0, false, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false, fmt.Errorf("axis %s=%q: %w", k, s, err)
	}
	return uint32(n), true, nil
}

func axU8(ax map[string]string, k string) (uint8, bool, error) {
	n, ok, err := axU32(ax, k)
	if err != nil || !ok {
		return 0, ok, err
	}
	if n > 255 {
		return 0, false, fmt.Errorf("axis %s=%d is past u8", k, n)
	}
	return uint8(n), true, nil
}

func axAddr(ax map[string]string, k string) ([]byte, error) {
	s, ok := ax[k]
	if !ok {
		return nil, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return nil, fmt.Errorf("axis %s: %w", k, err)
	}
	return a.AsSlice(), nil
}

func axMP(ax map[string]string) ([]routeMP, error) {
	s, ok := ax["mp"]
	if !ok {
		return nil, nil
	}
	var out []routeMP
	for e := range strings.SplitSeq(s, ";") {
		gw, w, ok := strings.Cut(e, "/")
		a, err := netip.ParseAddr(gw)
		if !ok || err != nil {
			return nil, fmt.Errorf("axis mp entry %q", e)
		}
		n, err := strconv.Atoi(w)
		if err != nil || n < 1 || n > 256 {
			return nil, fmt.Errorf("axis mp weight %q", w)
		}
		out = append(out, routeMP{gw: a.AsSlice(), weight: n})
	}
	return out, nil
}

// routeDefaultScope is iproute_modify's scope when the command names none.
func routeDefaultScope(fam uint8, verb string, rtype uint8, typed, gw bool) uint8 {
	initial := uint8(unix.RT_SCOPE_UNIVERSE)
	if verb == "del" {
		initial = unix.RT_SCOPE_NOWHERE
	}
	switch {
	case fam == unix.AF_INET6:
		return unix.RT_SCOPE_UNIVERSE
	case rtype == unix.RTN_LOCAL:
		return unix.RT_SCOPE_HOST
	case rtype == unix.RTN_BROADCAST || rtype == unix.RTN_MULTICAST || rtype == unix.RTN_ANYCAST:
		return unix.RT_SCOPE_LINK
	case !typed || rtype == unix.RTN_UNICAST:
		if verb == "del" {
			return unix.RT_SCOPE_NOWHERE
		}
		if !gw {
			return unix.RT_SCOPE_LINK
		}
	}
	return initial
}

// routeWantHdr fills the rtmsg and the message type and flags.
func routeWantHdr(ax map[string]string, w *routeWant) error {
	verb := ax["verb"]
	flags, ok := routeVerbFlags[verb]
	if !ok {
		return fmt.Errorf("axis verb=%q", verb)
	}
	w.typ, w.flags = unix.RTM_NEWROUTE, flags
	if verb == "del" {
		w.typ = unix.RTM_DELROUTE
	}
	switch ax["family"] {
	case "4":
		w.hdr.Family = unix.AF_INET
	case "6":
		w.hdr.Family = unix.AF_INET6
	default:
		return fmt.Errorf("axis family=%q", ax["family"])
	}
	dstLen, _, err := axU8(ax, "dst_len")
	if err != nil {
		return err
	}
	w.hdr.DstLen = dstLen
	t, typed := ax["type"]
	if typed {
		if w.hdr.Type, ok = routeTypes[t]; !ok {
			return fmt.Errorf("axis type=%q", t)
		}
	} else if verb != "del" {
		w.hdr.Type = unix.RTN_UNICAST
	}
	w.hdr.Protocol = unix.RTPROT_BOOT
	if verb == "del" {
		w.hdr.Protocol = 0
	}
	if p, ok, err := axU8(ax, "proto"); err != nil {
		return err
	} else if ok {
		w.hdr.Protocol = p
	}
	gw := ax["gw"] != "" || ax["via"] != "" || ax["mp"] != "" || ax["nhid"] != ""
	w.hdr.Scope = routeDefaultScope(w.hdr.Family, verb, w.hdr.Type, typed, gw)
	if s, ok, err := axU8(ax, "scope"); err != nil {
		return err
	} else if ok {
		w.hdr.Scope = s
	}
	if ax["onlink"] == "1" {
		w.hdr.Flags = unix.RTNH_F_ONLINK
	}
	return nil
}

// routeWantTable sets rtm_table and RTA_TABLE: a table under 256 fits the
// header; a larger one goes in RTA_TABLE with the header RT_TABLE_UNSPEC.
// Without one, local, broadcast and anycast routes go to the local table.
func routeWantTable(ax map[string]string, w *routeWant) error {
	t, ok, err := axU32(ax, "table")
	switch {
	case err != nil:
		return err
	case !ok:
		w.hdr.Table = unix.RT_TABLE_MAIN
		switch w.hdr.Type {
		case unix.RTN_LOCAL, unix.RTN_BROADCAST, unix.RTN_ANYCAST:
			w.hdr.Table = unix.RT_TABLE_LOCAL
		}
	case t < 256:
		w.hdr.Table = uint8(t)
	default:
		w.hdr.Table = unix.RT_TABLE_UNSPEC
		w.table = t
		w.attrs = append(w.attrs, unix.RTA_TABLE)
	}
	return nil
}

// routeWantAttrs fills every attribute the axes call for. The device is
// looked up by name, so RTA_OIF's value is not modeled, only its presence.
// With the family given (-4, -6), get_addr_1 sizes "default" to it, so
// default sends an all-zero RTA_DST just as 0.0.0.0/0 does.
func routeWantAttrs(ax map[string]string, w *routeWant) error {
	var err error
	switch {
	case ax["dst"] != "default":
		if w.dst, err = axAddr(ax, "dst"); err != nil {
			return err
		}
	case ax["family"] == "6":
		w.dst = make([]byte, 16)
	default:
		w.dst = make([]byte, 4)
	}
	w.attrs = append(w.attrs, unix.RTA_DST)
	if m, ok, err := axU32(ax, "metric"); err != nil {
		return err
	} else if ok {
		w.priority = m
		w.attrs = append(w.attrs, unix.RTA_PRIORITY)
	}
	if _, ok := ax["oif"]; ok {
		w.attrs = append(w.attrs, unix.RTA_OIF)
	}
	if w.gw, err = axAddr(ax, "gw"); err != nil {
		return err
	} else if w.gw != nil {
		w.attrs = append(w.attrs, unix.RTA_GATEWAY)
	}
	if via, err := axAddr(ax, "via"); err != nil {
		return err
	} else if via != nil {
		fam, _, err := axU32(ax, "via_family")
		if err != nil || fam > 0xFFFF {
			return fmt.Errorf("axis via_family=%q", ax["via_family"])
		}
		w.via = &RtVia{Family: uint16(fam), Addr: via}
		w.attrs = append(w.attrs, unix.RTA_VIA)
	}
	if w.mp, err = axMP(ax); err != nil {
		return err
	} else if w.mp != nil {
		w.attrs = append(w.attrs, unix.RTA_MULTIPATH)
	}
	return routeWantMore(ax, w)
}

func routeWantMore(ax map[string]string, w *routeWant) error {
	var ok bool
	var err error
	if w.nhid, ok, err = axU32(ax, "nhid"); err != nil {
		return err
	} else if ok {
		w.attrs = append(w.attrs, RtaNhID)
	}
	if w.prefsrc, err = axAddr(ax, "prefsrc"); err != nil {
		return err
	} else if w.prefsrc != nil {
		w.attrs = append(w.attrs, unix.RTA_PREFSRC)
	}
	if w.pref, ok, err = axU8(ax, "pref"); err != nil {
		return err
	} else if ok {
		w.attrs = append(w.attrs, unix.RTA_PREF)
	}
	if w.expires, ok, err = axU32(ax, "expires"); err != nil {
		return err
	} else if ok {
		w.attrs = append(w.attrs, unix.RTA_EXPIRES)
	}
	mtu, hasMTU, err := axU32(ax, "mtu")
	if err != nil {
		return err
	}
	mss, hasMSS, err := axU32(ax, "advmss")
	if err != nil {
		return err
	}
	w.mtu, w.advmss, w.mtuLock = mtu, mss, ax["mtu_lock"] == "1"
	if hasMTU || hasMSS {
		w.attrs = append(w.attrs, unix.RTA_METRICS)
	}
	return nil
}

// routeWantFromAxes is O3's model: the request a row's axes call for.
func routeWantFromAxes(ax map[string]string) (routeWant, error) {
	var w routeWant
	if err := routeWantHdr(ax, &w); err != nil {
		return w, err
	}
	if err := routeWantTable(ax, &w); err != nil {
		return w, err
	}
	if err := routeWantAttrs(ax, &w); err != nil {
		return w, err
	}
	slices.Sort(w.attrs)
	return w, nil
}

// routeAttrWidths is the payload length each fixed-width RTA_* must have.
var routeAttrWidths = map[uint16]int{
	unix.RTA_TABLE: 4, unix.RTA_PRIORITY: 4, unix.RTA_OIF: 4, RtaNhID: 4,
	unix.RTA_PREF: 1, unix.RTA_EXPIRES: 4,
}

// routeRequestProblem is O3: how a captured request differs from the
// model's. body is the message after its nlmsghdr.
func routeRequestProblem(w routeWant, flags uint16, body []byte) error {
	var errs []error
	if got := flags &^ (unix.NLM_F_REQUEST | unix.NLM_F_ACK); got != w.flags {
		errs = append(errs, fmt.Errorf("nlmsg flags %#x, want %#x", got, w.flags))
	}
	var hdr RtMsg
	if _, err := DeserializeRtMsg(body, &hdr); err != nil {
		return err
	}
	if hdr != w.hdr {
		errs = append(errs, fmt.Errorf("rtmsg %+v, want %+v", hdr, w.hdr))
	}
	var attrs []uint16
	err := WalkRTAttrs(body[RtMsgSizeCst:], func(a uint16, val []byte) {
		attrs = append(attrs, a)
		if n, ok := routeAttrWidths[a]; ok && len(val) != n {
			errs = append(errs, fmt.Errorf("attribute %d is %d bytes, want %d", a, len(val), n))
		}
		if a == unix.RTA_TABLE && len(val) == 4 && binary.NativeEndian.Uint32(val) != w.table {
			errs = append(errs, fmt.Errorf("RTA_TABLE %d, want %d", binary.NativeEndian.Uint32(val), w.table))
		}
	})
	if err != nil {
		return err
	}
	slices.Sort(attrs)
	if !slices.Equal(attrs, w.attrs) {
		return errors.Join(append(errs, fmt.Errorf("attributes %v, want %v", attrs, w.attrs))...)
	}
	ri, err := ParseNewRoute(body)
	if err != nil {
		return err
	}
	errs = append(errs, routeValueProblems(w, ri)...)
	return errors.Join(errs...)
}

func routeValueProblems(w routeWant, ri RouteInfo) []error {
	var errs []error
	diff := func(name string, got, want any) {
		errs = append(errs, fmt.Errorf("%s %v, want %v", name, got, want))
	}
	if !bytes.Equal(ri.Dst, w.dst) {
		diff("RTA_DST", ri.Dst, w.dst)
	}
	if ri.HasPriority && ri.Priority != w.priority {
		diff("RTA_PRIORITY", ri.Priority, w.priority)
	}
	if !bytes.Equal(ri.Gateway, w.gw) {
		diff("RTA_GATEWAY", ri.Gateway, w.gw)
	}
	if w.via != nil && (ri.Via == nil || ri.Via.Family != w.via.Family || !bytes.Equal(ri.Via.Addr, w.via.Addr)) {
		diff("RTA_VIA", ri.Via, w.via)
	}
	if ri.NhID != w.nhid {
		diff("RTA_NH_ID", ri.NhID, w.nhid)
	}
	if !bytes.Equal(ri.PrefSrc, w.prefsrc) {
		diff("RTA_PREFSRC", ri.PrefSrc, w.prefsrc)
	}
	if ri.Pref != w.pref {
		diff("RTA_PREF", ri.Pref, w.pref)
	}
	if ri.HasExpires && ri.Expires != uint64(w.expires) {
		diff("RTA_EXPIRES", ri.Expires, w.expires)
	}
	errs = append(errs, routeMPProblems(w.mp, ri.Multipath)...)
	return append(errs, routeMetricProblems(w, ri.Metrics)...)
}

// routeMPProblems compares RTA_MULTIPATH: rtnh_hops is the weight less one
// (ip/iproute.c parse_one_nh), and each entry names d0.
func routeMPProblems(want []routeMP, got []RouteNextHop) []error {
	if len(got) != len(want) {
		return []error{fmt.Errorf("%d multipath entries, want %d", len(got), len(want))}
	}
	var errs []error
	for i := range want {
		g := got[i]
		if int(g.Hops)+1 != want[i].weight || !bytes.Equal(g.Gateway, want[i].gw) || g.Ifindex == 0 {
			errs = append(errs, fmt.Errorf("multipath %d: hops %d gw %v ifindex %d, want weight %d gw %v on a device",
				i, g.Hops, g.Gateway, g.Ifindex, want[i].weight, want[i].gw))
		}
	}
	return errs
}

func routeMetricProblems(w routeWant, m *RouteMetrics) []error {
	var errs []error
	check := func(rtax uint16, want uint32) {
		got, ok := m.Get(rtax)
		if (want != 0) != ok || got != want {
			errs = append(errs, fmt.Errorf("RTAX %d = %d (present %v), want %d", rtax, got, ok, want))
		}
	}
	check(unix.RTAX_MTU, w.mtu)
	check(unix.RTAX_ADVMSS, w.advmss)
	var lock uint32
	if w.mtuLock {
		lock = 1 << unix.RTAX_MTU
	}
	check(unix.RTAX_LOCK, lock)
	return errs
}

// routeDumpHas is O4: whether a dump holds the route a request made. The
// table is the one the route lands in (RT_TABLE_UNSPEC means main), and
// the priority is compared only where the kernel keeps the requested one:
// IPv6 turns a metric of 0 into IP6_RT_PRIO_USER (1024).
func routeDumpHas(w routeWant, metric bool, dump []RouteInfo) bool {
	table := uint32(w.hdr.Table)
	if w.table != 0 {
		table = w.table
	}
	if table == unix.RT_TABLE_UNSPEC {
		table = unix.RT_TABLE_MAIN
	}
	for i := range dump {
		d := &dump[i]
		if d.Family != w.hdr.Family || d.DstLen != w.hdr.DstLen || d.Table != table || d.Type != w.hdr.Type ||
			!bytes.Equal(zeroNil(d.Dst), zeroNil(w.dst)) {
			continue
		}
		kept := w.hdr.Family != unix.AF_INET6 || w.priority != 0
		if metric && kept && d.Priority != w.priority {
			continue
		}
		return true
	}
	return false
}

// zeroNil reads an all-zero address as absent: a request sends a /0's
// RTA_DST as zero bytes, while the kernel's dump of it carries none.
func zeroNil(b []byte) []byte {
	for _, x := range b {
		if x != 0 {
			return b
		}
	}
	return nil
}

// wsRouteRequests returns the route requests (type typ) in msgs.
func wsRouteRequests(msgs []capturedNlMsg, typ uint16) []capturedNlMsg {
	var out []capturedNlMsg
	for _, m := range msgs {
		if m.proto == unix.NETLINK_ROUTE && m.h.Type == typ && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			out = append(out, m)
		}
	}
	return out
}

// wsRouteDump returns the routes a readback's dump replied with.
func wsRouteDump(t *testing.T, msgs []capturedNlMsg) []RouteInfo {
	t.Helper()
	var out []RouteInfo
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		ri, err := ParseNewRoute(m.body)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ri)
	}
	return out
}

// TestWsRouteCorpus runs O3 and O4 over every generated route row. It
// fails if O3 checked nothing, so a corpus or model change that empties
// its scope shows.
func TestWsRouteCorpus(t *testing.T) {
	var checked, roundTrips int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "route4/") && !strings.HasPrefix(row.ID, "route6/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			c, rt := wsRouteRow(t, dir, row)
			checked += c
			roundTrips += rt
		})
	}
	t.Logf("O3 checked %d route requests, O4 %d round trips", checked, roundTrips)
	if checked == 0 {
		t.Error("O3 checked no route request: are there route rows under testdata/*/ws?")
	}
}

func wsRouteRow(t *testing.T, dir string, row wsRow) (checked, roundTrips int) {
	t.Helper()
	w, err := routeWantFromAxes(row.Axes)
	if err != nil {
		t.Fatalf("O3 model: %v", err)
	}
	reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), w.typ)
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
	if err := routeRequestProblem(w, reqs[0].h.Flags, reqs[0].body); err != nil {
		t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
	}
	if rc != 0 {
		return 1, 0
	}
	dump := wsRouteDump(t, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")))
	_, metric := row.Axes["metric"]
	has := routeDumpHas(w, metric, dump)
	if has == (w.typ == unix.RTM_DELROUTE) {
		t.Errorf("O4: after %s the dump's %d routes hold it: %v\n  cmd: %s", row.Axes["verb"], len(dump), has, row.Cmd)
	}
	return 1, 1
}

// rtmsgBody encodes an rtmsg and its attributes.
func rtmsgBody(h RtMsg, attrs ...[]byte) []byte {
	b := []byte{h.Family, h.DstLen, h.SrcLen, h.Tos, h.Table, h.Protocol, h.Scope, h.Type, 0, 0, 0, 0}
	binary.NativeEndian.PutUint32(b[8:], h.Flags)
	return cat(append([][]byte{b}, attrs...)...)
}

func TestRouteDefaultScope(t *testing.T) {
	cases := []struct {
		description string
		fam         uint8
		verb        string
		rtype       uint8
		typed, gw   bool
		expected    uint8
	}{
		{"positive: a device route is scope link", unix.AF_INET, "add", unix.RTN_UNICAST, false, false, unix.RT_SCOPE_LINK},
		{"positive: a gateway route is scope universe", unix.AF_INET, "add", unix.RTN_UNICAST, false, true, unix.RT_SCOPE_UNIVERSE},
		{"positive: a local route is scope host", unix.AF_INET, "add", unix.RTN_LOCAL, true, false, unix.RT_SCOPE_HOST},
		{"positive: a broadcast route is scope link", unix.AF_INET, "replace", unix.RTN_BROADCAST, true, true, unix.RT_SCOPE_LINK},
		{"positive: IPv6 is always universe", unix.AF_INET6, "add", unix.RTN_LOCAL, true, false, unix.RT_SCOPE_UNIVERSE},
		{"positive: a delete is scope nowhere", unix.AF_INET, "del", 0, false, true, unix.RT_SCOPE_NOWHERE},
		{"corner: a delete of a blackhole keeps the initial nowhere", unix.AF_INET, "del", unix.RTN_BLACKHOLE, true, false, unix.RT_SCOPE_NOWHERE},
		{"corner: an added blackhole keeps the initial universe", unix.AF_INET, "add", unix.RTN_BLACKHOLE, true, false, unix.RT_SCOPE_UNIVERSE},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := routeDefaultScope(tc.fam, tc.verb, tc.rtype, tc.typed, tc.gw); got != tc.expected {
				t.Errorf("scope %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestRouteWantFromAxes(t *testing.T) {
	base := map[string]string{"verb": "add", "family": "4", "dst": "198.51.100.0", "dst_len": "24", "oif": "d0"}
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
		expected    func(w routeWant) bool
		wantErr     bool
	}{
		{"positive: a device route", base, func(w routeWant) bool {
			return w.typ == unix.RTM_NEWROUTE && w.hdr.Table == unix.RT_TABLE_MAIN && w.hdr.Protocol == unix.RTPROT_BOOT &&
				slices.Equal(w.attrs, []uint16{unix.RTA_DST, unix.RTA_OIF})
		}, false},
		{"boundary: table 255 fits the header", with("table", "255"), func(w routeWant) bool {
			return w.hdr.Table == 255 && !slices.Contains(w.attrs, unix.RTA_TABLE)
		}, false},
		{"boundary: table 256 goes in RTA_TABLE, the header unspec", with("table", "256"), func(w routeWant) bool {
			return w.hdr.Table == unix.RT_TABLE_UNSPEC && w.table == 256 && slices.Contains(w.attrs, unix.RTA_TABLE)
		}, false},
		{"boundary: table 4294967295", with("table", "4294967295"), func(w routeWant) bool {
			return w.table == 4294967295
		}, false},
		{"positive: a local route without a table goes to local", with("type", "local"), func(w routeWant) bool {
			return w.hdr.Table == unix.RT_TABLE_LOCAL && w.hdr.Type == unix.RTN_LOCAL
		}, false},
		{"positive: -4 default sends four zero bytes", with("dst", "default", "dst_len", "0"), func(w routeWant) bool {
			return bytes.Equal(w.dst, make([]byte, 4)) && slices.Contains(w.attrs, unix.RTA_DST)
		}, false},
		{"positive: -6 default sends sixteen zero bytes", with("family", "6", "dst", "default", "dst_len", "0"), func(w routeWant) bool {
			return bytes.Equal(w.dst, make([]byte, 16)) && slices.Contains(w.attrs, unix.RTA_DST)
		}, false},
		{"corner: 0.0.0.0/0 sends four zero bytes", with("dst", "0.0.0.0", "dst_len", "0"), func(w routeWant) bool {
			return len(w.dst) == 4 && slices.Contains(w.attrs, unix.RTA_DST)
		}, false},
		{"positive: a delete sets no type or protocol", with("verb", "del"), func(w routeWant) bool {
			return w.typ == unix.RTM_DELROUTE && w.flags == 0 && w.hdr.Type == 0 && w.hdr.Protocol == 0
		}, false},
		{"positive: multipath weights", with("oif", "", "mp", "192.0.2.1/1;192.0.2.2/256"), func(w routeWant) bool {
			return len(w.mp) == 2 && w.mp[1].weight == 256 && w.hdr.Scope == unix.RT_SCOPE_UNIVERSE
		}, false},
		{"positive: mtu lock and advmss share RTA_METRICS", with("mtu", "1400", "mtu_lock", "1", "advmss", "1300"), func(w routeWant) bool {
			return w.mtuLock && w.advmss == 1300 && slices.Contains(w.attrs, unix.RTA_METRICS)
		}, false},
		{"negative: an unknown verb", with("verb", "flush"), nil, true},
		{"negative: an unknown type", with("type", "nat"), nil, true},
		{"negative: a table past u32", with("table", "4294967296"), nil, true},
		{"negative: a protocol past u8", with("proto", "256"), nil, true},
		{"negative: a weight of 0", with("mp", "192.0.2.1/0"), nil, true},
		{"negative: no family", with("family", ""), nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			w, err := routeWantFromAxes(tc.axes)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && !tc.expected(w) {
				t.Errorf("want %+v", w)
			}
		})
	}
}

func TestRouteRequestProblem(t *testing.T) {
	ax := map[string]string{"verb": "add", "family": "4", "dst": "198.51.100.0", "dst_len": "24",
		"oif": "d0", "table": "4294967295", "metric": "0", "mtu": "1400", "mtu_lock": "1"}
	w, err := routeWantFromAxes(ax)
	if err != nil {
		t.Fatal(err)
	}
	hdr := w.hdr
	dst := rtattr(unix.RTA_DST, []byte{198, 51, 100, 0})
	oif := rtattr(unix.RTA_OIF, u32le(2))
	table := rtattr(unix.RTA_TABLE, u32le(4294967295))
	prio := rtattr(unix.RTA_PRIORITY, u32le(0))
	metrics := rtattr(unix.RTA_METRICS, cat(rtattr(unix.RTAX_MTU, u32le(1400)), rtattr(unix.RTAX_LOCK, u32le(1<<unix.RTAX_MTU))))
	flags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL)
	wrongTable := hdr
	wrongTable.Table = unix.RT_TABLE_MAIN
	cases := []struct {
		description string
		flags       uint16
		body        []byte
		expected    string // in the error; "" for none
	}{
		{"positive: the request the axes call for", flags, rtmsgBody(hdr, dst, table, prio, oif, metrics), ""},
		{"positive: attribute order does not matter", flags, rtmsgBody(hdr, metrics, oif, prio, table, dst), ""},
		{"negative: RTA_TABLE two bytes wide", flags, rtmsgBody(hdr, dst, rtattr(unix.RTA_TABLE, []byte{0xff, 0xff}), prio, oif, metrics), "is 2 bytes, want 4"},
		{"negative: a large table also in the header", flags, rtmsgBody(wrongTable, dst, table, prio, oif, metrics), "rtmsg"},
		{"negative: RTA_TABLE one short", flags, rtmsgBody(hdr, dst, rtattr(unix.RTA_TABLE, u32le(4294967294)), prio, oif, metrics), "RTA_TABLE 4294967294"},
		{"negative: the metric missing", flags, rtmsgBody(hdr, dst, table, oif, metrics), "attributes"},
		{"negative: an attribute the axes don't name", flags, rtmsgBody(hdr, dst, table, prio, oif, metrics, rtattr(unix.RTA_FLOW, u32le(1))), "attributes"},
		{"negative: NLM_F_REPLACE for an add", flags | unix.NLM_F_REPLACE, rtmsgBody(hdr, dst, table, prio, oif, metrics), "nlmsg flags"},
		{"negative: the lock bit missing", flags, rtmsgBody(hdr, dst, table, prio, oif, rtattr(unix.RTA_METRICS, rtattr(unix.RTAX_MTU, u32le(1400)))), "RTAX 1"},
		{"negative: the destination's last byte wrong", flags, rtmsgBody(hdr, rtattr(unix.RTA_DST, []byte{198, 51, 100, 1}), table, prio, oif, metrics), "RTA_DST"},
		{"boundary: a bare rtmsg is short of what the axes name", flags, rtmsgBody(hdr), "attributes"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := routeRequestProblem(w, tc.flags, tc.body)
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

func TestRouteDumpHas(t *testing.T) {
	want := func(ax map[string]string) routeWant {
		w, err := routeWantFromAxes(ax)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	v4 := want(map[string]string{"verb": "add", "family": "4", "dst": "198.51.100.0", "dst_len": "24", "oif": "d0", "table": "256", "metric": "0"})
	v6 := want(map[string]string{"verb": "add", "family": "6", "dst": "2001:db8:2::", "dst_len": "64", "oif": "d0", "metric": "0"})
	def := want(map[string]string{"verb": "add", "family": "4", "dst": "0.0.0.0", "dst_len": "0", "oif": "d0"})
	route := func(fam, dlen uint8, dst []byte, table uint32, prio uint32) RouteInfo {
		return RouteInfo{Family: fam, DstLen: dlen, Dst: dst, Table: table, Type: unix.RTN_UNICAST, Priority: prio, HasPriority: true}
	}
	cases := []struct {
		description string
		w           routeWant
		metric      bool
		dump        []RouteInfo
		expected    bool
	}{
		{"positive: the route in table 256", v4, true, []RouteInfo{route(unix.AF_INET, 24, []byte{198, 51, 100, 0}, 256, 0)}, true},
		{"negative: the route in main instead", v4, true, []RouteInfo{route(unix.AF_INET, 24, []byte{198, 51, 100, 0}, 254, 0)}, false},
		{"negative: another metric", v4, true, []RouteInfo{route(unix.AF_INET, 24, []byte{198, 51, 100, 0}, 256, 5)}, false},
		{"corner: IPv6 metric 0 is kept as 1024", v6, true, []RouteInfo{route(unix.AF_INET6, 64, netip.MustParseAddr("2001:db8:2::").AsSlice(), 254, 1024)}, true},
		{"corner: 0.0.0.0/0 is the default route, dumped with no RTA_DST", def, false, []RouteInfo{route(unix.AF_INET, 0, nil, 254, 0)}, true},
		{"boundary: an empty dump", v4, true, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := routeDumpHas(tc.w, tc.metric, tc.dump); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
