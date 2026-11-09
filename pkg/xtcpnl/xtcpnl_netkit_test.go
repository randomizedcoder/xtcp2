package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The netkit codec's tests, against the netkit rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP5-T02 The netkit parser").

var netkitKind = greRow(netkitKindCst)

// netkitCorpusWords are the option words after `type netkit` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 19 accepted
// and 8 refused by the kernel. The address rows put `address` before
// `type`, so it isn't netkit's.
func netkitCorpusWords() map[string]string {
	return map[string]string{
		"add_no_option":       "",
		"add_peer_only":       "peer",
		"add_mode_l2":         "mode l2",
		"add_mode_l3":         "mode l3",
		"add_blackhole":       "blackhole",
		"add_policy_twice":    "forward blackhole",
		"add_scrub_none":      "scrub none",
		"add_peer_blackhole":  "peer blackhole",
		"add_peer_scrub":      "peer scrub none",
		"add_peer_name":       "peer name nt1",
		"add_peer_bare_name":  "peer nt1",
		"add_peer_word":       "peer black",
		"add_peer_mode":       "peer mode l2",
		"add_peer_up":         "peer up",
		"add_l2_address":      "mode l2",
		"add_peer_samename":   "peer name nt0",
		"add_l3_address":      "",
		"add_l3_peer_address": "peer address 02:00:00:00:00:01",
		"add_mcast_address":   "mode l2",
		"set_blackhole":       "blackhole",
		"set_peer_policy":     "peer blackhole",
		"set_no_option":       "",
		"set_peer_only":       "peer",
		"set_mode":            "mode l3",
		"set_scrub":           "scrub none",
		"set_peer_name":       "peer name nt1",
		"set_on_peer":         "blackhole",
	}
}

// netkitValues are netkit_parse_opt's value words.
var (
	netkitModes    = map[string]uint64{"l2": unix.NETKIT_L2, "l3": unix.NETKIT_L3}
	netkitPolicies = map[string]uint64{"forward": unix.NETKIT_PASS, "blackhole": unix.NETKIT_DROP}
	netkitScrubs   = map[string]uint64{"none": NetkitScrubNone, "default": NetkitScrubDefault}
)

// netkitArgs parses option words as netkit_parse_opt does, for well-formed
// words: its own words, then, from the first word after peer that isn't
// one, the peer half iplink_parse takes. A set's peer name becomes the peer
// header's ifindex, an add's its IFLA_IFNAME.
func netkitArgs(t testing.TB, words string, ifindex map[string]uint64, set bool) ([]NetkitSetting, *VethPeer) {
	t.Helper()
	argv := strings.Fields(words)
	var out []NetkitSetting
	seenPeer := false
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		switch {
		case kw == "mode":
			i++
			out = append(out, NetkitSetting{Option: NetkitOptionMode, Value: netkitModes[argv[i]]})
		case kw == "forward" || kw == "blackhole":
			out = append(out, NetkitSetting{Option: NetkitOptionPolicy, Value: netkitPolicies[kw]})
		case kw == "scrub":
			i++
			out = append(out, NetkitSetting{Option: NetkitOptionScrub, Value: netkitScrubs[argv[i]]})
		case kw == "peer":
			seenPeer = true
			out = append(out, NetkitSetting{Option: NetkitOptionPeer})
		case seenPeer:
			return out, peerArgs(t, argv[i:], ifindex, set)
		default:
			t.Fatalf("no netkit keyword for %q", kw)
		}
	}
	return out, nil
}

// peerArgs parses a peer half's words as iplink_parse does, for the words
// the corpus uses: name, a bare name, up and address.
func peerArgs(t testing.TB, argv []string, ifindex map[string]uint64, set bool) *VethPeer {
	t.Helper()
	p := &VethPeer{}
	name := ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "name":
			i++
			name = argv[i]
		case "up":
			p.IfFlags |= unix.IFF_UP
			p.IfChange |= unix.IFF_UP
		case "address":
			i++
			mac, err := net.ParseMAC(argv[i])
			if err != nil {
				t.Fatal(err)
			}
			p.Attrs = append(p.Attrs, LinkAttr{Type: unix.IFLA_ADDRESS, Payload: mac})
		default:
			name = argv[i]
		}
	}
	switch {
	case name == "":
	case set:
		idx, ok := ifindex[name]
		if !ok {
			t.Fatalf("no ifindex for %q", name)
		}
		le := binary.LittleEndian.AppendUint32(nil, uint32(idx&kindU32MaxCst))
		if err := binary.Read(bytes.NewReader(le), binary.LittleEndian, &p.Index); err != nil {
			t.Fatal(err)
		}
	default:
		p.Attrs = append(p.Attrs, LinkAttr{Type: unix.IFLA_IFNAME, Payload: append([]byte(name), 0)})
	}
	return p
}

// netkitName is netkit_print_opt's name for v, or UNKNOWN.
func netkitName(names map[uint32]string, v uint32) string {
	if n, ok := names[v]; ok {
		return n
	}
	return "UNKNOWN"
}

// netkitView renders g as netkit_print_opt prints it under -j.
func netkitView(g *NetkitInfo) map[string]any {
	out := map[string]any{}
	if g.HasMode {
		out["mode"] = netkitName(NetkitModeNames, g.Mode)
	}
	if g.HasPrimary {
		out["type"] = "peer"
		if g.Primary != 0 {
			out["type"] = "primary"
		}
	}
	for _, f := range []struct {
		key   string
		has   bool
		v     uint32
		names map[uint32]string
	}{
		{"policy", g.HasPolicy, g.Policy, NetkitPolicyNames},
		{"peer_policy", g.HasPeerPolicy, g.PeerPolicy, NetkitPolicyNames},
		{"scrub", g.HasScrub, g.Scrub, NetkitScrubNames},
		{"peer_scrub", g.HasPeerScrub, g.PeerScrub, NetkitScrubNames},
	} {
		if f.has {
			out[f.key] = netkitName(f.names, f.v)
		}
	}
	return out
}

func TestNetkitDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := netkitKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := netkitView(ParseNetkitInfoData(netkitKind.dump(t, slug, pcap)))
			want := netkitKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 19 {
		t.Errorf("%d accepted netkit rows, want 19", len(slugs))
	}
}

func TestParseNetkitInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	dump := func(slug string) []byte { return netkitKind.dump(t, slug, pcap) }
	u32 := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.LittleEndian.AppendUint32(nil, v)) }
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
	}{
		{
			description: "positive: a netkit's dump",
			nest:        dump("add_no_option"),
			expected: map[string]any{"mode": "l3", "type": "primary", "policy": "forward", "peer_policy": "forward",
				"scrub": "default", "peer_scrub": "default"},
			exact: true,
		},
		{
			description: "positive: the peer's policy",
			nest:        dump("add_peer_blackhole"),
			expected:    map[string]any{"policy": "forward", "peer_policy": "blackhole"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a policy with no name",
			nest:        u32(unix.IFLA_NETKIT_POLICY, 1),
			expected:    map[string]any{"policy": "UNKNOWN"},
			exact:       true,
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "corner: the peer's own dump",
			nest:        dump("set_on_peer"),
			expected:    map[string]any{"type": "peer"},
		},
		{
			description: "corner: no scrub on a single device",
			nest: slices.Concat(rtattr(unix.IFLA_NETKIT_PRIMARY, []byte{1}), u32(unix.IFLA_NETKIT_POLICY, 0),
				u32(unix.IFLA_NETKIT_MODE, 1)),
			expected: map[string]any{"mode": "l3", "type": "primary", "policy": "forward"},
			exact:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseNetkitInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := netkitView(g)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
		})
	}
}

func TestEncodeNetkitInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []NetkitSetting
		peer        *VethPeer
		// expected nil is no INFO_DATA; empty is an empty one.
		expected []byte
		err      error
	}
	u32 := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.LittleEndian.AppendUint32(nil, v)) }
	args := func(words string) ([]NetkitSetting, *VethPeer) { return netkitArgs(t, words, nil, false) }
	peerUp := func() []byte {
		ifi := EncodeIfInfomsg(unix.AF_UNSPEC, 0, unix.IFF_UP, unix.IFF_UP)
		return rtattr(unix.IFLA_NETKIT_PEER_INFO, ifi)
	}
	tests := []row{
		{
			description: "negative: a second mode",
			settings:    []NetkitSetting{{Option: NetkitOptionMode, Value: 1}, {Option: NetkitOptionMode}},
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: a second peer",
			settings:    []NetkitSetting{{Option: NetkitOptionPeer}, {Option: NetkitOptionPeer}},
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: a policy with no name",
			settings:    []NetkitSetting{{Option: NetkitOptionPolicy, Value: 1}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a peer half with no peer word",
			peer:        &VethPeer{},
			err:         ErrKindSettingMissing,
		},
		{
			description: "negative: a setting with no option",
			settings:    []NetkitSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: peer alone",
			settings:    []NetkitSetting{{Option: NetkitOptionPeer}},
			expected:    []byte{},
		},
		{
			description: "corner: peer splits the policy words",
			settings: []NetkitSetting{{Option: NetkitOptionPolicy, Value: unix.NETKIT_PASS}, {Option: NetkitOptionPeer},
				{Option: NetkitOptionPolicy, Value: unix.NETKIT_DROP}},
			expected: slices.Concat(u32(unix.IFLA_NETKIT_POLICY, 0), u32(unix.IFLA_NETKIT_PEER_POLICY, 2)),
		},
		{
			description: "corner: the peer's header carries its flags",
			settings:    []NetkitSetting{{Option: NetkitOptionPeer}},
			peer:        &VethPeer{IfFlags: unix.IFF_UP, IfChange: unix.IFF_UP},
			expected:    peerUp(),
		},
	}
	named, namedPeer := args("blackhole peer name nt1")
	tests = append(tests, row{
		description: "corner: PEER_INFO follows netkit's words",
		settings:    named,
		peer:        namedPeer,
		expected: slices.Concat(u32(unix.IFLA_NETKIT_POLICY, unix.NETKIT_DROP), rtattr(unix.IFLA_NETKIT_PEER_INFO,
			slices.Concat(EncodeIfInfomsg(unix.AF_UNSPEC, 0, 0, 0), rtattr(unix.IFLA_IFNAME, []byte("nt1\x00"))))),
	})
	peerMode, peerModePeer := args("peer mode l2")
	tests = append(tests, row{
		description: "corner: a netkit word after peer stays netkit's",
		settings:    peerMode,
		peer:        peerModePeer,
		expected:    u32(unix.IFLA_NETKIT_MODE, unix.NETKIT_L2),
	})
	words := netkitCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + netkitKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, has, ok := netkitKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		s, p := netkitArgs(t, w, greIfindex(t, netkitKind, slug), strings.HasPrefix(slug, "set_"))
		var want []byte
		if has {
			want = append([]byte{}, data...)
		}
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    s,
			peer:        p,
			expected:    want,
		})
	}
	if sent != 27 {
		t.Errorf("%d netkit rows sent a request, want 27", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeNetkitInfoData(tt.settings, tt.peer)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if (got == nil) != (tt.expected == nil) || !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x (nil %v)\nwant % x (nil %v)", got, got == nil, tt.expected, tt.expected == nil)
			}
		})
	}
}

func TestLinkInfoNetkitKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a netkit decodes Netkit", row: "netkit/add_no_option", expected: true},
		{description: "positive: a netkit's peer decodes Netkit", row: "netkit/set_on_peer", expected: true},
		{description: "negative: a vxcan does not", row: "vxcan/add_peer"},
		{description: "corner: an l2 netkit decodes Netkit", row: "netkit/add_mode_l2", expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var body []byte
			for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/"+tt.row+"/netlink_route_getlink_detail.pcap") {
				if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
					body = m.body
				}
			}
			li, err := ParseNewLink(body)
			if err != nil {
				t.Fatal(err)
			}
			if got := li.Netkit != nil; got != tt.expected {
				t.Errorf("Netkit %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseNetkitInfoData(b *testing.B) {
	nest := netkitKind.dump(b, "add_peer_blackhole", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseNetkitInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
