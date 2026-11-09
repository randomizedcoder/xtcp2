package xtcpnl

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The bareudp codec's tests, against the bareudp rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP3-T03 The bareudp and amt
// parsers").

var bareudpKind = kindRows{kind: bareudpKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// bareudpCorpusWords are the option words after `type bareudp` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 6 accepted and
// 4 refused by the kernel.
func bareudpCorpusWords() map[string]string {
	return map[string]string{
		"add_default":          "dstport 6635 ethertype mpls_uc",
		"add_ethertype_ipv4":   "dstport 6635 ethertype ipv4",
		"add_ethertype_number": "dstport 6635 ethertype 0x1234",
		"add_srcportmin":       "dstport 6635 ethertype ip srcportmin 1000",
		"add_multiproto":       "dstport 6635 ethertype mpls_uc multiproto",
		"add_abbrev":           "d 6635 e ip s 1 m",
		"add_no_option":        "",
		"add_multiproto_ipv6":  "dstport 6635 ethertype ipv6 multiproto",
		"add_port_busy":        "dstport 6635 ethertype mpls_uc",
		"set_same":             "dstport 6635 ethertype ip",
	}
}

// bareudpProtos are the ll_proto names the corpus uses; ipv4 is
// ll_proto_a2n's alias for ip, which ll_proto_n2a prints.
var bareudpProtos = map[string]uint64{"ip": 0x0800, "ipv4": 0x0800, "ipv6": 0x86dd, "mpls_uc": 0x8847}

// bareudpKeywords are bareudp_parse_opt's keywords in its order, all
// compared with matches.
var bareudpKeywords = []string{"dstport", "ethertype", "srcportmin", "multiproto", "nomultiproto"}

// bareudpArgs parses option words as bareudp_parse_opt does, for
// well-formed words.
func bareudpArgs(t testing.TB, words string) []BareudpSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string) uint64 {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var out []BareudpSetting
	for i := 0; i < len(argv); i++ {
		kw := ""
		for _, k := range bareudpKeywords {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		switch kw {
		case "dstport":
			i++
			out = append(out, BareudpSetting{Option: BareudpOptionDstPort, Value: num(argv[i])})
		case "ethertype":
			i++
			v, ok := bareudpProtos[argv[i]]
			if !ok {
				v = num(argv[i])
			}
			out = append(out, BareudpSetting{Option: BareudpOptionEtherType, Value: v})
		case "srcportmin":
			i++
			out = append(out, BareudpSetting{Option: BareudpOptionSrcPortMin, Value: num(argv[i])})
		case "multiproto", "nomultiproto":
			out = append(out, BareudpSetting{Option: BareudpOptionMultiproto, Value: vxlanOn(kw)})
		default:
			t.Fatalf("no bareudp keyword for %q", argv[i])
		}
	}
	return out
}

// bareudpView renders b as bareudp_print_opt prints it under -j.
func bareudpView(b *BareudpInfo) map[string]any {
	out := map[string]any{"multiproto": b.Multiproto}
	if b.HasPort {
		out["dstport"] = float64(b.Port)
	}
	if b.HasEtherType {
		out["ethertype"] = fmt.Sprintf("[%d]", b.EtherType)
		for _, name := range []string{"ip", "ipv6", "mpls_uc"} {
			if bareudpProtos[name] == uint64(b.EtherType) {
				out["ethertype"] = name
			}
		}
	}
	if b.HasSrcPortMin {
		out["srcportmin"] = float64(b.SrcPortMin)
	}
	return out
}

func TestBareudpDumpMatchesReadback(t *testing.T) {
	slugs := bareudpKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := bareudpView(ParseBareudpInfoData(bareudpKind.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
			want := bareudpKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 6 {
		t.Errorf("%d accepted bareudp rows, want 6", len(slugs))
	}
}

func TestParseBareudpInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		unknown  []uint16
	}{
		{
			description: "positive: a bareudp's dump",
			nest:        bareudpKind.dump(t, "add_default", pcap),
			expected: map[string]any{"dstport": float64(6635), "ethertype": "mpls_uc",
				"srcportmin": float64(0), "multiproto": false},
			exact: true,
		},
		{
			description: "positive: the multiproto flag",
			nest:        bareudpKind.dump(t, "add_multiproto", pcap),
			expected:    map[string]any{"multiproto": true},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short PORT",
			nest:        rtattr(unix.IFLA_BAREUDP_PORT, []byte{0x19}),
			expected:    map[string]any{"multiproto": false},
			exact:       true,
		},
		{
			description: "boundary: an empty nest prints only nomultiproto",
			nest:        []byte{},
			expected:    map[string]any{"multiproto": false},
			exact:       true,
		},
		{
			description: "corner: an ethertype with no name prints as a number",
			nest:        bareudpKind.dump(t, "add_ethertype_number", pcap),
			expected:    map[string]any{"ethertype": "[4660]"},
		},
		{
			description: "corner: the ethertype is big-endian, SRCPORT_MIN host order",
			nest: slices.Concat(rtattr(unix.IFLA_BAREUDP_ETHERTYPE, []byte{0x08, 0x00}),
				rtattr(unix.IFLA_BAREUDP_SRCPORT_MIN, []byte{0xe8, 0x03})),
			expected: map[string]any{"ethertype": "ip", "srcportmin": float64(1000), "multiproto": false},
			exact:    true,
		},
		{
			description: "corner: an id past the last IFLA_BAREUDP_* is listed",
			nest:        rtattr(9, []byte{1}),
			expected:    map[string]any{"multiproto": false},
			exact:       true,
			unknown:     []uint16{9},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b := ParseBareudpInfoData(tt.nest)
			if (b == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", b == nil, tt.nilInfo)
			}
			if b == nil {
				return
			}
			got := bareudpView(b)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			if !slices.Equal(b.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", b.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeBareudpInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []BareudpSetting
		expected    []byte
		err         error
	}
	port := rtattr(unix.IFLA_BAREUDP_PORT, []byte{0x19, 0xeb})
	ip := rtattr(unix.IFLA_BAREUDP_ETHERTYPE, []byte{0x08, 0x00})
	args := func(words string) []BareudpSetting { return bareudpArgs(t, words) }
	tests := []row{
		{
			description: "negative: a dstport past 65535",
			settings:    args("dstport 70000 ethertype ip"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []BareudpSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: no ethertype",
			settings:    args("dstport 6635"),
			err:         ErrKindSettingMissing,
		},
		{
			description: "negative: multiproto then nomultiproto is a duplicate",
			settings:    args("dstport 6635 ethertype ip multiproto nomultiproto"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "boundary: srcportmin 65535",
			settings:    args("dstport 6635 ethertype ip srcportmin 65535"),
			expected:    slices.Concat(port, ip, rtattr(unix.IFLA_BAREUDP_SRCPORT_MIN, []byte{0xff, 0xff})),
		},
		{
			description: "corner: ethertype 0x1234 encodes big-endian",
			settings:    args("dstport 6635 ethertype 0x1234"),
			expected:    slices.Concat(port, rtattr(unix.IFLA_BAREUDP_ETHERTYPE, []byte{0x12, 0x34})),
		},
		{
			description: "corner: the words' order does not matter; PORT goes first",
			settings:    args("multiproto srcportmin 1 ethertype ip dstport 6635"),
			expected: slices.Concat(port, ip, rtattr(unix.IFLA_BAREUDP_SRCPORT_MIN, []byte{1, 0}),
				rtattr(unix.IFLA_BAREUDP_MULTIPROTO_MODE, nil)),
		},
		{
			description: "corner: nomultiproto sends nothing",
			settings:    args("dstport 6635 ethertype ip nomultiproto"),
			expected:    slices.Concat(port, ip),
		},
	}
	words := bareudpCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + bareudpKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := bareudpKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    bareudpArgs(t, w),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 10 {
		t.Errorf("%d bareudp rows sent a request, want 10", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeBareudpInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoBareudpAmtKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		bareudp     bool
		amt         bool
	}{
		{description: "positive: a bareudp decodes Bareudp", row: "bareudp/add_default", bareudp: true},
		{description: "positive: an amt decodes Amt", row: "amt/add_gateway", amt: true},
		{description: "negative: a geneve decodes neither", row: "geneve/add_default"},
		{description: "corner: an amt relay with every port decodes Amt", row: "amt/add_ports", amt: true},
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
			if got := li.Bareudp != nil; got != tt.bareudp {
				t.Errorf("Bareudp %v for kind %q, want %v", got, li.Kind, tt.bareudp)
			}
			if got := li.Amt != nil; got != tt.amt {
				t.Errorf("Amt %v for kind %q, want %v", got, li.Kind, tt.amt)
			}
		})
	}
}

func BenchmarkParseBareudpInfoData(b *testing.B) {
	nest := bareudpKind.dump(b, "add_srcportmin", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseBareudpInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
