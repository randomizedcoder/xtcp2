//go:build goip_write

package goip

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// bareudpCorpusArgv is each KC4-T03 bareudp row's argv, keyed by slug.
func bareudpCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":          "dstport 6635 ethertype mpls_uc",
		"add_ethertype_ipv4":   "dstport 6635 ethertype ipv4",
		"add_ethertype_number": "dstport 6635 ethertype 0x1234",
		"add_srcportmin":       "dstport 6635 ethertype ip srcportmin 1000",
		"add_multiproto":       "dstport 6635 ethertype mpls_uc multiproto",
		"add_abbrev":           "d 6635 e ip s 1 m",
		"add_no_option":        "",
		"add_multiproto_ipv6":  "dstport 6635 ethertype ipv6 multiproto",
		"add_unknown":          "dstport 1 ethertype ip foo",
		"add_no_dstport":       "ethertype ip",
		"add_no_ethertype":     "dstport 6635",
		"add_ethertype_bad":    "dstport 6635 ethertype foo",
		"add_dstport_twice":    "dstport 1 dstport 2 ethertype ip",
		"add_no_value":         kindKeyDstport,
	} {
		out[slug] = kindAddArgv("b0", kindBareudpCst, w)
	}
	out["add_port_busy"] = kindAddArgv("b1", kindBareudpCst, "dstport 6635 ethertype mpls_uc")
	out["set_same"] = stackedSetArgv("b0", kindBareudpCst, "dstport 6635 ethertype ip")
	return out
}

// amtCorpusArgv is each KC4-T03 amt row's argv, keyed by slug.
func amtCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_gateway":              amtGateway,
		"add_relay":                amtRelay,
		"add_ports":                amtGateway + " relay_port 3000 gateway_port 3001 max_tunnels 10",
		"add_max_tunnels_zero":     amtRelay + " max_tunnels 0",
		"add_mode_twice":           "mode gateway mode relay dev d0 local 10.0.0.1",
		"add_no_option":            "",
		"add_no_link":              "mode relay local 10.0.0.1",
		"add_gateway_no_discovery": "mode gateway dev d0 local 10.0.0.1",
		"add_on_loopback":          "mode relay dev lo local 10.0.0.1",
		"add_local_loopback":       "mode relay dev d0 local 127.0.0.1",
		"add_relay_port_zero":      amtRelay + " relay_port 0",
		"add_discovery_multicast":  "mode gateway dev d0 local 10.0.0.1 discovery 224.0.0.1",
		"add_abbrev":               "mod relay dev d0 local 10.0.0.1",
		"add_mode_bad":             "mode foo",
		"add_local_bad":            "local foo",
		"add_dev_missing":          "dev nosuch",
		"add_local_twice":          "local 10.0.0.1 local 10.0.0.3",
		"add_port_over":            "relay_port 70000",
		"add_no_value":             "mode",
	} {
		out[slug] = kindAddArgv("a0", kindAmtCst, w)
	}
	out["set_same"] = stackedSetArgv("a0", kindAmtCst, amtRelay)
	return out
}

// gtpCorpusArgv is each KC4-T04 row's argv, keyed by slug.
func gtpCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_ggsn":              "role ggsn",
		"add_sgsn":              "role sgsn",
		"add_hsize":             "role ggsn hsize 4096",
		"add_hsize_max":         "role ggsn hsize 131071",
		"add_hsize_zero":        "role ggsn hsize 0",
		"add_restart_count":     "role ggsn restart_count 255",
		"add_unknown_skipped":   "role ggsn foo",
		"add_no_option":         "",
		"add_no_role":           "hsize 10",
		"add_abbrev":            "rol ggsn",
		"add_role_bad":          "role foo",
		"add_hsize_over":        "role ggsn hsize 131072",
		"add_restart_count_hex": "role ggsn restart_count 0x1",
		"add_role_twice":        "role ggsn role sgsn",
		"add_no_value":          "role",
	} {
		out[slug] = kindAddArgv("g0", kindGtpCst, w)
	}
	out["add_second"] = kindAddArgv("g1", kindGtpCst, "role sgsn")
	for slug, w := range map[string]string{"set_same": "role ggsn", "set_no_option": "", "set_no_role": "hsize 5"} {
		out[slug] = stackedSetArgv("g0", kindGtpCst, w)
	}
	return out
}

// smallOverlays are the three kinds' rows: the corpus, the argv table, the
// row count and the rows that sent a request.
var smallOverlays = []struct {
	kind  string
	dir   string
	argv  func() map[string][]string
	rows  int
	sent  int
	named map[string]string
}{
	{kindBareudpCst, bareudpKindDir, bareudpCorpusArgv, 16, 10, map[string]string{
		"add_abbrev":           "positive: bareudp's after-loop order",
		"add_ethertype_number": "boundary: an ethertype by number",
		"add_no_option":        "corner: no word, no data",
	}},
	{kindAmtCst, amtKindDir, amtCorpusArgv, 20, 13, map[string]string{
		"add_ports":      "positive: amt's word order",
		"add_mode_twice": "corner: a repeated mode",
		"add_no_option":  "corner: no word, no data",
	}},
	{kindGtpCst, gtpKindDir, gtpCorpusArgv, 19, 11, map[string]string{
		"add_hsize_max":       "boundary: the largest hash",
		"add_unknown_skipped": "corner: gtp skips a word",
		"add_no_option":       "corner: no word, no data",
		"set_same":            "corner: gtp's set is its add",
	}},
}

// TestSmallOverlayCorpusArgv holds each argv table to its corpus: one entry
// per row, 16, 20 and 19.
func TestSmallOverlayCorpusArgv(t *testing.T) {
	for _, k := range smallOverlays {
		t.Run(k.kind, func(t *testing.T) {
			argv := k.argv()
			slugs := kindRowSlugs(t, k.dir)
			for _, s := range slugs {
				if _, ok := argv[s]; !ok {
					t.Errorf("row %s has no argv", s)
				}
			}
			if len(argv) != len(slugs) || len(slugs) != k.rows {
				t.Errorf("%d argv entries for %d rows, want %d each", len(argv), len(slugs), k.rows)
			}
		})
	}
}

// TestLinkSmallOverlayWriteMatchesIp replays every row that sent a
// request: 10 bareudp, 13 amt and 11 gtp.
func TestLinkSmallOverlayWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	for _, k := range smallOverlays {
		t.Run(k.kind, func(t *testing.T) {
			replayKindRows(t, k.dir, k.argv(), k.named, k.sent)
		})
	}
}

// TestLinkSmallOverlayRefusals is goip's status and first stderr line for
// each refusal the three parse_opts make. Only amt's dev sends a get.
func TestLinkSmallOverlayRefusals(t *testing.T) {
	useLLMap720(t)
	// The slugs repeat across kinds, so each row names its kind.
	named := func(kind string, rows []kindRefusal) []kindRefusal {
		for i := range rows {
			rows[i].description = strings.Replace(rows[i].description, ": ", ": "+kind+"/", 1)
		}
		return rows
	}
	tests := named(kindBareudpCst, corpusRefusals(t, bareudpKindDir, bareudpCorpusArgv(),
		"add_unknown", "add_dstport_twice", "add_ethertype_bad", "add_no_dstport", "add_no_ethertype", "add_no_value"))
	tests = append(tests, named(kindAmtCst, corpusRefusals(t, amtKindDir, amtCorpusArgv(),
		"add_abbrev", "add_local_bad", "add_mode_bad", "add_dev_missing", "add_local_twice", "add_no_value",
		"add_port_over"))...)
	tests = append(tests, named(kindGtpCst, corpusRefusals(t, gtpKindDir, gtpCorpusArgv(),
		"add_abbrev", "add_no_role", "set_no_role", "add_hsize_over", "add_no_value", "add_restart_count_hex",
		"add_role_bad", "add_role_twice"))...)
	none := bareudpKindDir + "add_default/ip.pcap"
	tests = append(tests,
		kindRefusal{"negative: bareudp with neither required word", kindAddArgv("b0", kindBareudpCst, "srcportmin 1"),
			ExitMinusOne, `argument "dstport" is required`, none, []int{}},
		kindRefusal{"boundary: a restart count past a u8", kindAddArgv("g0", kindGtpCst, "role ggsn restart_count 256"),
			ExitMinusOne, `argument "256" is wrong: invalid restart_count`, none, []int{}},
		kindRefusal{"corner: amt's local takes the discovery's family",
			kindAddArgv("a0", kindAmtCst, "discovery 10.0.0.2 local 2001:db8::1"),
			ExitUsage, `Error: inet address is expected rather than "2001:db8::1"`, none, []int{}},
		kindRefusal{"corner: amt's unknown word", kindAddArgv("a0", kindAmtCst, "foo"),
			ExitUsage, `amt: unknown command "foo"?`, none, []int{}},
		kindRefusal{"corner: multiproto's two forms are one word", kindAddArgv("b0", kindBareudpCst, "dstport 1 ethertype ip m n"),
			ExitMinusOne, `either "n" is duplicate, or "n" is garbage`, none, []int{}},
	)
	for _, kind := range []string{kindBareudpCst, kindAmtCst, kindGtpCst} {
		tests = append(tests, kindRefusal{"corner: help is not goip's: " + kind, kindAddArgv("x0", kind, "help"),
			ExitUsage, "not implemented", none, []int{}})
	}
	runKindRefusals(t, tests)
}

// smallOverlayEncode is the kind's parser, then its encoder; d0 is ifindex
// 7.
func smallOverlayEncode(kind, args string) ([]byte, error) {
	words := strings.Fields(args)
	switch kind {
	case kindBareudpCst:
		s, err := parseBareudpArgs(words)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeBareudpInfoData(s)
	case kindAmtCst:
		s, err := parseAmtArgs(words, func(name string) (int32, error) {
			if name == "d0" {
				return 7, nil
			}
			return 0, nil
		})
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeAmtInfoData(s)
	default:
		s, err := parseGtpArgs(words)
		if err != nil {
			return nil, err
		}
		return xtcpnl.EncodeGtpInfoData(s)
	}
}

// TestParseSmallOverlayArgs covers what no capture reaches.
func TestParseSmallOverlayArgs(t *testing.T) {
	u32 := func(atype uint16, v byte) []byte { return vxAttr(atype, v, 0, 0, 0) }
	sockets := vxAttr(unix.IFLA_GTP_CREATE_SOCKETS, 1)
	ggsn := u32(unix.IFLA_GTP_ROLE, 0)
	tests := []struct {
		description string
		kind        string
		args        string
		expected    []byte
	}{
		{"positive: a named ethertype by number", kindBareudpCst, "dstport 1 ethertype 0x0800",
			slices.Concat(vxAttr(unix.IFLA_BAREUDP_PORT, 0, 1), vxAttr(unix.IFLA_BAREUDP_ETHERTYPE, 8, 0))},
		{"positive: a skipped word before the role", kindGtpCst, "foo role ggsn",
			slices.Concat(sockets, ggsn, vxAttr(unix.IFLA_GTP_PDP_HASHSIZE, 0, 4, 0, 0))},
		{"negative: amt's any", kindAmtCst, "mode relay dev d0 local any",
			slices.Concat(u32(xtcpnl.IflaAmtMode, 1), u32(xtcpnl.IflaAmtLink, 7))},
		{"boundary: amt's zero local", kindAmtCst, "local 0.0.0.0", vxAttr(xtcpnl.IflaAmtLocalIP, 0, 0, 0, 0)},
		{"corner: gtp's restart count is decimal", kindGtpCst, "role ggsn restart_count 010",
			slices.Concat(sockets, ggsn, vxAttr(unix.IFLA_GTP_RESTART_COUNT, 10), vxAttr(unix.IFLA_GTP_PDP_HASHSIZE, 0, 4, 0, 0))},
		{"corner: gtp's hsize isn't", kindGtpCst, "role ggsn hsize 0x10",
			slices.Concat(sockets, ggsn, u32(unix.IFLA_GTP_PDP_HASHSIZE, 16))},
		{"corner: amt sends a multicast discovery", kindAmtCst, "discovery 224.0.0.1",
			vxAttr(xtcpnl.IflaAmtDiscoveryIP, 224, 0, 0, 1)},
		{"corner: gtp's help is a strcmp", kindGtpCst, "hel role ggsn",
			slices.Concat(sockets, ggsn, vxAttr(unix.IFLA_GTP_PDP_HASHSIZE, 0, 4, 0, 0))},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := smallOverlayEncode(tc.kind, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("got  % x\nwant % x", got, tc.expected)
			}
		})
	}
}

// TestParseSmallOverlayArgsErrors covers the refusals no table row reaches
// through runLink: the statuses' sentinels.
func TestParseSmallOverlayArgsErrors(t *testing.T) {
	tests := []struct {
		description string
		kind        string
		args        string
		expected    error
	}{
		{"negative: amt's missing dev is exit(nodev)", kindAmtCst, "dev nosuch", ErrLinkUsage},
		{"negative: gtp's role with no value", kindGtpCst, "role", ErrLinkUsage},
		{"boundary: srcportmin past a u16", kindBareudpCst, "dstport 1 ethertype ip srcportmin 65536", ErrLinkUsage},
		{"corner: amt's any after a discovery can't be sent", kindAmtCst, "discovery 10.0.0.2 local any", ErrNotImplemented},
		{"corner: amt's help is a strcmp", kindAmtCst, "hel", ErrInvalidArgument},
		{"corner: bareudp's missing ethertype", kindBareudpCst, "dstport 1", ErrLinkUsage},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := smallOverlayEncode(tc.kind, tc.args)
			if !errors.Is(err, tc.expected) {
				t.Errorf("got %v, want %v", err, tc.expected)
			}
		})
	}
}

// kindAddArgv is the corpus's `plain` proc: `add <dev> type <kind> <words>`.
func kindAddArgv(dev, kind, words string) []string {
	return append([]string{"add", dev, "type", kind}, strings.Fields(words)...)
}

// The corpus's AMT_RELAY and AMT_GATEWAY.
const (
	amtRelay   = "mode relay dev d0 local 10.0.0.1"
	amtGateway = "mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2"
)
