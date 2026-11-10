package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The mpls rows' oracles (tools/nlcapgen/matrix_mpls.go, NC2-T02). O3
// checks each route request against the row's axes: an AF_MPLS route's
// RTA_DST is one label, RTA_NEWDST the as stack, RTA_VIA a next hop in
// another family, RTA_TTL_PROPAGATE a u8, and each RTA_MULTIPATH hop its
// own stack and via. O4 checks the MPLS table read back after a write.

// mplsViaWant parses an axis's "inet:10.0.0.2" into what RTA_VIA holds.
func mplsViaWant(s string) (RtVia, error) {
	fam, addr, ok := strings.Cut(s, ":")
	a, err := netip.ParseAddr(addr)
	if !ok || err != nil {
		return RtVia{}, fmt.Errorf("via axis %q", s)
	}
	switch fam {
	case "inet":
		return RtVia{Family: unix.AF_INET, Addr: a.AsSlice()}, nil
	case "inet6":
		return RtVia{Family: unix.AF_INET6, Addr: a.AsSlice()}, nil
	}
	return RtVia{}, fmt.Errorf("via axis %q", s)
}

func mplsViaProblem(want string, got *RtVia) error {
	w, err := mplsViaWant(want)
	if err != nil {
		return err
	}
	if got == nil || got.Family != w.Family || !slices.Equal(got.Addr, w.Addr) {
		return fmt.Errorf("RTA_VIA %+v, want %s", got, want)
	}
	return nil
}

// mplsStackProblem decodes a label stack and compares it with want, ip's
// slash form.
func mplsStackProblem(what string, raw []byte, want string) error {
	ls, err := ParseMPLSLabels(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if got := labelsString(ls); got != want {
		return fmt.Errorf("%s %s, want %s", what, got, want)
	}
	return nil
}

// mplsRequestWant is the request a row's verb sends: its type and the
// NLM_F_ bits that tell add, replace, del and get apart.
func mplsRequestWant(verb string) (uint16, uint16, error) {
	const bits = unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_REPLACE
	switch verb {
	case "add":
		return unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_EXCL, nil
	case "replace":
		return unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_REPLACE, nil
	case "del":
		return unix.RTM_DELROUTE, 0, nil
	case "get":
		return unix.RTM_GETROUTE, 0, nil
	}
	return 0, bits, fmt.Errorf("verb axis %q", verb)
}

// mplsRequestProblem checks one request against an mpls row's axes. oif
// is d0's index.
func mplsRequestProblem(ax map[string]string, typ, flags uint16, body []byte, oif int32) error {
	wantType, wantFlags, err := mplsRequestWant(ax["verb"])
	if err != nil {
		return err
	}
	const bits = unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_REPLACE
	if typ != wantType || flags&bits != wantFlags {
		return fmt.Errorf("type %d flags %#x, want %d with %#x", typ, flags&bits, wantType, wantFlags)
	}
	if len(body) < RtMsgSizeCst {
		return fmt.Errorf("body of %d bytes", len(body))
	}
	var types []uint16
	var newdsts [][]byte
	var ttl []byte
	if err := WalkRTAttrs(body[RtMsgSizeCst:], func(t uint16, v []byte) {
		types = append(types, t)
		switch t {
		case unix.RTA_NEWDST:
			newdsts = append(newdsts, v)
		case unix.RTA_TTL_PROPAGATE:
			ttl = v
		}
	}); err != nil {
		return err
	}
	if s, ok := ax["newdst"]; ok {
		// as on an IP route: get_addr in the route's family, so no label
		// stack, which ParseNewRoute would read it as.
		if body[0] != unix.AF_INET || len(newdsts) != 1 || hex.EncodeToString(newdsts[0]) != s {
			return fmt.Errorf("family %d RTA_NEWDST %x, want AF_INET and %s", body[0], newdsts, s)
		}
		return nil
	}
	r, err := ParseNewRoute(body)
	if err != nil {
		return err
	}
	if s, ok := ax["encap"]; ok {
		if r.Family != unix.AF_INET || r.Encap == nil || r.Encap.MPLS == nil || labelsString(r.Encap.MPLS.Labels) != s {
			return fmt.Errorf("family %d encap %+v, want AF_INET and mpls %s", r.Family, r.Encap, s)
		}
		return nil
	}
	if r.Family != unix.AF_MPLS || r.DstLen != 20 {
		return fmt.Errorf("family %d dst_len %d, want AF_MPLS and 20", r.Family, r.DstLen)
	}
	if err := mplsStackProblem("RTA_DST", r.Dst, ax["dst"]); err != nil {
		return err
	}
	want := []uint16{unix.RTA_DST}
	for i, k := range []string{"as", "as2"} {
		s, ok := ax[k]
		if !ok {
			continue
		}
		want = append(want, unix.RTA_NEWDST)
		if len(newdsts) <= i {
			return fmt.Errorf("%d RTA_NEWDST, want %s", len(newdsts), k)
		}
		if err := mplsStackProblem("RTA_NEWDST", newdsts[i], s); err != nil {
			return err
		}
	}
	if s, ok := ax["ttl_propagate"]; ok {
		want = append(want, unix.RTA_TTL_PROPAGATE)
		if len(ttl) != 1 || strconv.Itoa(int(ttl[0])) != s {
			return fmt.Errorf("RTA_TTL_PROPAGATE %x, want the u8 %s", ttl, s)
		}
	}
	if hops, ok := ax["hops"]; ok {
		want = append(want, unix.RTA_MULTIPATH)
		if err := mplsHopsProblem(hops, r.Multipath, oif); err != nil {
			return err
		}
	} else {
		if s, ok := ax["via"]; ok {
			want = append(want, unix.RTA_VIA)
			if err := mplsViaProblem(s, r.Via); err != nil {
				return err
			}
		}
		if _, ok := ax["oif"]; ok {
			want = append(want, unix.RTA_OIF)
			if int32(r.Oif) != oif {
				return fmt.Errorf("RTA_OIF %d, want %d", r.Oif, oif)
			}
		}
	}
	if !slices.Equal(types, want) {
		return fmt.Errorf("attributes %v, want %v", types, want)
	}
	return nil
}

// mplsHopsProblem checks RTA_MULTIPATH against "300@inet:10.0.0.2,…": each
// hop's stack (empty for none), via and device.
func mplsHopsProblem(spec string, got []RouteNextHop, oif int32) error {
	hops := strings.Split(spec, ",")
	if len(got) != len(hops) {
		return fmt.Errorf("%d hops, want %d", len(got), len(hops))
	}
	for i, h := range hops {
		as, via, _ := strings.Cut(h, "@")
		if s := labelsString(got[i].NewDst); s != as {
			return fmt.Errorf("hop %d: as %q, want %q", i, s, as)
		}
		if err := mplsViaProblem(via, got[i].Via); err != nil {
			return fmt.Errorf("hop %d: %w", i, err)
		}
		if got[i].Ifindex != oif {
			return fmt.Errorf("hop %d: ifindex %d, want %d", i, got[i].Ifindex, oif)
		}
	}
	return nil
}

// mplsTableProblem checks the MPLS table dumped after a write that
// succeeded: the label is there after an add or replace, with the stack
// the kernel kept (the last as given; none at the top with nexthops), and
// gone after a del.
func mplsTableProblem(ax map[string]string, dump []capturedNlMsg) error {
	var found *RouteInfo
	for _, m := range dump {
		if m.h.Type != unix.RTM_NEWROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		r, err := ParseNewRoute(m.body)
		if err != nil {
			return err
		}
		if ls, err := ParseMPLSLabels(r.Dst); err == nil && labelsString(ls) == ax["dst"] {
			found = &r
		}
	}
	if ax["verb"] == "del" {
		if found != nil {
			return fmt.Errorf("label %s still in the table", ax["dst"])
		}
		return nil
	}
	if found == nil {
		return fmt.Errorf("label %s not in the table", ax["dst"])
	}
	want := ax["as"]
	if s, ok := ax["as2"]; ok {
		want = s
	}
	if _, ok := ax["hops"]; ok {
		want = ""
	}
	if got := labelsString(found.NewDst); got != want {
		return fmt.Errorf("label %s swaps to %q, want %q", ax["dst"], got, want)
	}
	return nil
}

func TestWsMplsCorpus(t *testing.T) {
	var checked, tables int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if filepath.Base(filepath.Dir(dir)) != "mpls" {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
			var reqs []capturedNlMsg
			for _, typ := range []uint16{unix.RTM_NEWROUTE, unix.RTM_DELROUTE, unix.RTM_GETROUTE} {
				for _, m := range wsRouteRequests(msgs, typ) {
					if m.h.Flags&unix.NLM_F_DUMP != unix.NLM_F_DUMP {
						reqs = append(reqs, m)
					}
				}
			}
			if row.Axes == nil || row.Expect.RefusedBy == wsRefusedIproute2 || row.Expect.RefusedBy == "ack-then-fail" {
				// show rows and the ones ip refuses write nothing.
				if len(reqs) != 0 {
					t.Errorf("O3: %d route requests, want none\n  cmd: %s", len(reqs), row.Cmd)
				}
				return
			}
			if len(reqs) == 0 && row.Expect.Errno == "ENODEV" && len(wsRouteRequests(msgs, unix.RTM_GETLINK)) > 0 {
				return // refused at ip's lookup of the device, before the route
			}
			if len(reqs) != 1 {
				t.Fatalf("O3: %d route requests, want 1\n  cmd: %s", len(reqs), row.Cmd)
			}
			if err := mplsRequestProblem(row.Axes, reqs[0].h.Type, reqs[0].h.Flags, reqs[0].body, wsLinkIndexOf(msgs, "d0")); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
			checked++
			_, isIP := row.Axes["newdst"]
			if wsReadRC(t, dir) != 0 || row.Axes["verb"] == "get" || isIP || row.Axes["encap"] != "" {
				return
			}
			if err := mplsTableProblem(row.Axes, pcapNlMsgs(t, filepath.Join(dir, "netlink_route_show.pcap"))); err != nil {
				t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
			}
			tables++
		})
	}
	t.Logf("O3 checked %d mpls requests, O4 %d tables", checked, tables)
	if checked == 0 || tables == 0 {
		t.Error("O3 or O4 checked nothing: are there mpls rows under testdata/*/ws?")
	}
}

func TestMplsRequestProblem(t *testing.T) {
	u32 := func(n uint32) []byte { return binary.LittleEndian.AppendUint32(nil, n) }
	rt := func(attrs ...[]byte) []byte {
		h := []byte{unix.AF_MPLS, 20, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0, 0, 0, 0}
		return cat(append([][]byte{h}, attrs...)...)
	}
	dst := rtattr(unix.RTA_DST, mplsStack(100))
	via := rtattr(unix.RTA_VIA, append([]byte{unix.AF_INET, 0}, 10, 0, 0, 2))
	oif := rtattr(unix.RTA_OIF, u32(3))
	ax := func(kv ...string) map[string]string {
		m := map[string]string{"verb": "add", "dst": "100", "via": "inet:10.0.0.2", "oif": "d0"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	via3 := rtattr(unix.RTA_VIA, append([]byte{unix.AF_INET, 0}, 10, 0, 0, 3))
	hop := func(label uint32, v []byte) []byte { return rtnh(t, 3, rtattr(unix.RTA_NEWDST, mplsStack(label)), v) }
	mp := func(hops ...[]byte) []byte { return rtattr(unix.RTA_MULTIPATH, cat(hops...)) }
	hops := map[string]string{"verb": "add", "dst": "100", "hops": "300@inet:10.0.0.2,400@inet:10.0.0.3"}
	cases := []struct {
		description string
		ax          map[string]string
		typ, flags  uint16
		body        []byte
		expected    string // "" or a substring of the error
	}{
		{"positive: a captured as 200/300", ax("as", "200/300"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_NEWDST, mplsStack(200, 300)), via, oif), ""},
		{"positive: ttl-propagate as a u8", ax("ttl_propagate", "1"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_TTL_PROPAGATE, []byte{1}), via, oif), ""},
		{"negative: the bottom-of-stack bit on every label", ax("as", "200/300"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_NEWDST, append(mplsEnt(200, 0, true, 0), mplsEnt(300, 0, true, 0)...)), via, oif), "bottom-of-stack"},
		{"negative: RTA_TTL_PROPAGATE as a u32", ax("ttl_propagate", "1"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_TTL_PROPAGATE, u32(1)), via, oif), "the u8 1"},
		{"negative: the stack in the wrong order", ax("as", "200/300"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_NEWDST, mplsStack(300, 200)), via, oif), "want 200/300"},
		{"negative: a via in the wrong family", ax("via", "inet6:2001:db8::2"), unix.RTM_NEWROUTE, add, rt(dst, via, oif), "want inet6"},
		{"negative: the via after the device", ax(), unix.RTM_NEWROUTE, add, rt(dst, oif, via), "attributes"},
		{"negative: a replace's flags on an add", ax(), unix.RTM_NEWROUTE,
			unix.NLM_F_REQUEST | unix.NLM_F_CREATE | unix.NLM_F_REPLACE, rt(dst, via, oif), "flags"},
		{"negative: another device", ax(), unix.RTM_NEWROUTE, add, rt(dst, via, rtattr(unix.RTA_OIF, u32(4))), "RTA_OIF 4"},
		{"positive: two hops, each with its stack and via", hops, unix.RTM_NEWROUTE, add,
			rt(dst, mp(hop(300, via), hop(400, via3))), ""},
		{"negative: the hops' stacks swapped", hops, unix.RTM_NEWROUTE, add,
			rt(dst, mp(hop(400, via), hop(300, via3))), "hop 0: as"},
		{"negative: a hop on another device", hops, unix.RTM_NEWROUTE, add,
			rt(dst, mp(hop(300, via), rtnh(t, 4, rtattr(unix.RTA_NEWDST, mplsStack(400)), via3))), "hop 1: ifindex 4"},
		{"boundary: as twice, both sent", ax("as", "200", "as2", "300"), unix.RTM_NEWROUTE, add,
			rt(dst, rtattr(unix.RTA_NEWDST, mplsStack(200)), rtattr(unix.RTA_NEWDST, mplsStack(300)), via, oif), ""},
		{"corner: a del sends what the add would", ax("verb", "del", "as", "200"), unix.RTM_DELROUTE, unix.NLM_F_REQUEST | unix.NLM_F_ACK,
			rt(dst, rtattr(unix.RTA_NEWDST, mplsStack(200)), via, oif), ""},
		{"corner: as on an IPv4 route is an address", map[string]string{"verb": "add", "newdst": "c8000000"}, unix.RTM_NEWROUTE, add,
			cat([]byte{unix.AF_INET, 24, 0, 0, 254, 3, 0, 1, 0, 0, 0, 0}, rtattr(unix.RTA_NEWDST, []byte{200, 0, 0, 0})), ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, mplsRequestProblem(tc.ax, tc.typ, tc.flags, tc.body, 3), tc.expected)
		})
	}
}

func TestMplsTableProblem(t *testing.T) {
	route := func(label uint32, attrs ...[]byte) capturedNlMsg {
		h := []byte{unix.AF_MPLS, 20, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0, 0, 0, 0}
		body := cat(append([][]byte{h, rtattr(unix.RTA_DST, mplsStack(label))}, attrs...)...)
		return capturedNlMsg{proto: unix.NETLINK_ROUTE, h: NlMsgHdr{Type: unix.RTM_NEWROUTE, Flags: unix.NLM_F_MULTI}, body: body}
	}
	swap := func(ls ...uint32) []byte { return rtattr(unix.RTA_NEWDST, mplsStack(ls...)) }
	cases := []struct {
		description string
		ax          map[string]string
		dump        []capturedNlMsg
		expected    string // "" or a substring of the error
	}{
		{"positive: an add's label with its stack", map[string]string{"verb": "add", "dst": "100", "as": "200/300"},
			[]capturedNlMsg{route(16), route(100, swap(200, 300))}, ""},
		{"positive: a del's label gone", map[string]string{"verb": "del", "dst": "100"}, []capturedNlMsg{route(16)}, ""},
		{"negative: a del's label still there", map[string]string{"verb": "del", "dst": "100"}, []capturedNlMsg{route(100)}, "still in the table"},
		{"negative: an add's label missing", map[string]string{"verb": "add", "dst": "100"}, []capturedNlMsg{route(16)}, "not in the table"},
		{"negative: the stack the kernel didn't keep", map[string]string{"verb": "add", "dst": "100", "as": "200"},
			[]capturedNlMsg{route(100, swap(300))}, "swaps to"},
		{"boundary: as twice keeps the last", map[string]string{"verb": "add", "dst": "100", "as": "200", "as2": "300"},
			[]capturedNlMsg{route(100, swap(300))}, ""},
		{"corner: with nexthops, no stack at the top", map[string]string{"verb": "add", "dst": "100", "as": "200", "hops": "300@inet:10.0.0.2"},
			[]capturedNlMsg{route(100)}, ""},
		{"corner: an empty dump", map[string]string{"verb": "replace", "dst": "100"}, nil, "not in the table"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, mplsTableProblem(tc.ax, tc.dump), tc.expected)
		})
	}
}
