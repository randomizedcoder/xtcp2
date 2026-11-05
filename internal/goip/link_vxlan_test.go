package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The `type vxlan` tests (docs/netlink/goip-link-kinds.md, "KG3-T01 The
// vxlan words"), against KC4-T01's rows of the 7.3.0 kind corpus.

// vxlanKindDir is KC4-T01's rows.
const vxlanKindDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/kinds/vxlan/"

// vxlanBase is the corpus's $VXLAN_BASE, which most accepted adds start with.
const vxlanBase = "id 42 dstport 4789 "

// vxlanAddArgv is the corpus's `plain` proc: `add <dev> type vxlan <words>`.
func vxlanAddArgv(dev, words string) []string {
	return append([]string{"add", dev, "type", kindVxlanCst}, strings.Fields(words)...)
}

// vxlanCorpusArgv is each KC4-T01 row's argv after `ip link`, keyed by slug.
func vxlanCorpusArgv() map[string][]string {
	out := map[string][]string{}
	for slug, w := range map[string]string{
		"add_default":                      "id 42",
		"add_dstport":                      vxlanBase,
		"add_dstport_zero":                 "id 42 dstport 0",
		"add_id_zero":                      "id 0 dstport 4789",
		"add_id_max":                       "id 16777215 dstport 4789",
		"add_vni_abbrev":                   "v 42 dstport 4789",
		"add_remote":                       vxlanBase + "remote 10.0.0.1",
		"add_remote6":                      vxlanBase + "remote 2001:db8::1",
		"add_group":                        vxlanBase + "group 239.1.1.1 dev d0",
		"add_group6":                       vxlanBase + "group ff05::1 dev d0",
		"add_local":                        vxlanBase + "local 10.0.0.1",
		"add_local6_link_local":            vxlanBase + "local fe80::1 dev d0",
		"add_ttl":                          vxlanBase + "ttl 64",
		"add_ttl_auto":                     vxlanBase + "ttl auto",
		"add_ttl_inherit":                  vxlanBase + "ttl inherit",
		"add_tos":                          vxlanBase + "tos 10",
		"add_tos_inherit":                  vxlanBase + "tos inherit",
		"add_df_set":                       vxlanBase + "df set",
		"add_df_unset":                     vxlanBase + "df unset",
		"add_df_inherit":                   vxlanBase + "df inherit",
		"add_flowlabel":                    vxlanBase + "remote 2001:db8::1 flowlabel 0x12345",
		"add_" + xtcpnl.VxlanKeywordAgeing: vxlanBase + xtcpnl.VxlanKeywordAgeing + " 600",
		"add_" + xtcpnl.VxlanKeywordAgeing + "_none": vxlanBase + xtcpnl.VxlanKeywordAgeing + " none",
		"add_maxaddress":            vxlanBase + "maxaddress 100",
		"add_srcport":               vxlanBase + "srcport 1000 2000",
		"add_external":              "external",
		"add_vnifilter":             "external vnifilter dstport 4789",
		"add_gpe":                   "external gpe",
		"add_reserved_bits":         vxlanBase + "reserved_bits 0",
		"add_no_abbrev":             vxlanBase + "no",
		"add_no_option":             "",
		"add_link_local_no_dev":     "id 42 local fe80::1",
		"add_scope_mix":             "id 42 local fe80::1 remote 2001:db8::1",
		"add_flowlabel_v4":          "id 42 flowlabel 5",
		"add_srcport_inverted":      "id 42 srcport 2000 1000",
		"add_gpe_no_external":       "id 42 gpe",
		"add_reserved_bits_overlap": "id 42 reserved_bits 0x0800000000000000",
		"add_no_id":                 "dstport 4789",
		"add_group_no_dev":          "id 42 group 239.1.1.1",
		"add_group_and_remote":      "id 42 group 239.1.1.1 remote 10.0.0.1",
		"add_external_and_id":       "external id 42",
		"add_vnifilter_no_external": "id 42 vnifilter",
		"add_remote_bad":            "id 42 remote nosuch",
		"add_strcmp_abbrev":         "id 42 localbyp",
		"add_unknown":               "id 42 bogus",
		"add_id_over":               "id 16777216",
		"add_remote_multicast":      "id 42 remote 239.1.1.1",
		"add_dev_missing":           "id 42 dev nosuch",
		"add_ttl_over":              "id 42 ttl 256",
		"add_srcport_one":           "id 42 srcport 1000",
		"add_learning_twice":        "id 42 learning nolearning",
		"add_no_value":              "id",
	} {
		out[slug] = vxlanAddArgv("vx0", w)
	}
	for _, flag := range strings.Fields("proxy rsc l2miss l3miss noudpcsum udp6zerocsumtx udp6zerocsumrx " +
		"remcsumtx remcsumrx nolearning nolocalbypass gbp mcroute") {
		out["add_"+flag] = vxlanAddArgv("vx0", vxlanBase+flag)
	}
	out["add_duplicate_vni"] = vxlanAddArgv("vx1", vxlanBase)
	for slug, w := range map[string]string{
		"set_changeable": "ttl 32 tos inherit df set " + xtcpnl.VxlanKeywordAgeing + " 600 nolearning remote 10.0.0.2",
		"set_dev":        "dev d0",
		"set_id_same":    "id 42",
		"set_id":         "id 43",
		"set_dstport":    "dstport 4789",
		"set_proxy":      "proxy",
		"set_family":     "remote 2001:db8::1",
	} {
		out[slug] = stackedSetArgv("vx0", kindVxlanCst, w)
	}
	return out
}

// TestVxlanCorpusArgv holds the argv table to the corpus: one entry per
// row, 73 in all.
func TestVxlanCorpusArgv(t *testing.T) {
	argv := vxlanCorpusArgv()
	slugs := kindRowSlugs(t, vxlanKindDir)
	for _, s := range slugs {
		if _, ok := argv[s]; !ok {
			t.Errorf("row %s has no argv", s)
		}
	}
	if len(argv) != len(slugs) || len(slugs) != 73 {
		t.Errorf("%d argv entries for %d rows, want 73 each", len(argv), len(slugs))
	}
}

// TestLinkVxlanWriteMatchesIp replays every KC4-T01 row that sent a
// request: the 46 the kernel took and the 12 it refused.
func TestLinkVxlanWriteMatchesIp(t *testing.T) {
	useLLMap720(t)
	replayKindRows(t, vxlanKindDir, vxlanCorpusArgv(), map[string]string{
		"add_group":        "positive: the lower device in word order",
		"add_id_max":       "boundary: the top id",
		"add_dstport_zero": "boundary: dstport 0",
		"add_default":      "corner: no dstport",
		"add_vni_abbrev":   "corner: an abbreviation",
		"add_no_abbrev":    "corner: no is nolearning",
		"add_external":     "corner: external",
		"set_changeable":   "corner: a set sends learning when given",
		"set_dev":          "corner: a set sends no learning otherwise",
	}, 58)
}

// TestLinkVxlanWarning holds goip's stderr to ip's for every row that sent
// a request: the four dstport lines for an add with neither dstport nor
// gpe, ahead of the kernel's error on a refused row, and nothing otherwise.
func TestLinkVxlanWarning(t *testing.T) {
	useLLMap720(t)
	argv := vxlanCorpusArgv()
	warned := 0
	for _, slug := range kindRowSlugs(t, vxlanKindDir) {
		dir := vxlanKindDir + slug
		rc := kindRowRC(t, dir)
		if rc != "0" && rc != "2" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "ip.err"))
		if err != nil {
			t.Fatal(err)
		}
		want := string(b)
		if rc == "2" {
			// The kernel's error is the last line.
			lines := strings.SplitAfter(want, "\n")
			want = strings.Join(lines[:len(lines)-2], "")
		}
		t.Run(slug, func(t *testing.T) {
			t.Setenv(writeOutEnvCst, filepath.Join(t.TempDir(), "write.bin"))
			got, err := runLinkStderr(t, newLinkReplay(t, filepath.Join(dir, "ip.pcap")), unix.AF_UNSPEC, argv[slug])
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("stderr %q, want %q", got, want)
			}
		})
		if want != "" {
			warned++
		}
	}
	if warned != 7 {
		t.Errorf("%d rows warned, want 7", warned)
	}
}

// runLinkStderr is runLinkWith with stderr kept.
func runLinkStderr(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &errOut, family: family}
	err := runLink(c, args)
	return errOut.String(), err
}

// TestLinkVxlanRefusals is goip's status and first stderr line for each
// refusal vxlan_parse_opt makes. Only dev sends a get.
func TestLinkVxlanRefusals(t *testing.T) {
	useLLMap720(t)
	tests := corpusRefusals(t, vxlanKindDir, vxlanCorpusArgv(),
		"add_external_and_id", "add_group_and_remote", "add_group_no_dev", "add_no_id", "add_remote_bad",
		"add_strcmp_abbrev", "add_unknown", "add_vnifilter_no_external",
		"add_dev_missing", "add_id_over", "add_learning_twice", "add_no_value", "add_remote_multicast",
		"add_srcport_one", "add_ttl_over")
	none := vxlanKindDir + "add_default/ip.pcap"
	d0 := vxlanKindDir + "add_group/ip.pcap"
	vx0 := func(words string) []string { return vxlanAddArgv("vx0", words) }
	tests = append(tests,
		kindRefusal{"negative: a local of the other family", vx0("id 42 remote 10.0.0.1 local 2001:db8::1"),
			ExitUsage, `Error: inet address is expected rather than "2001:db8::1"`, none, []int{}},
		kindRefusal{"boundary: a flow label past 20 bits", vx0("id 42 flowlabel 0x100000"),
			ExitMinusOne, `argument "0x100000" is wrong: invalid flowlabel`, none, []int{}},
		kindRefusal{"boundary: a dstport past a u16", vx0("id 42 dstport 65536"),
			ExitMinusOne, `argument "65536" is wrong: dst port`, none, []int{}},
		kindRefusal{"corner: the conflict comes before the value", vx0("id 42 group 239.1.1.1 dev d0 remote"),
			ExitUsage, "vxlan: both group and remote cannot be specified", d0, []int{0}},
		kindRefusal{"corner: remote twice", vx0("id 42 remote 10.0.0.1 remote 10.0.0.2"),
			ExitMinusOne, `either "remote" is duplicate, or "10.0.0.2" is garbage`, none, []int{}},
		kindRefusal{"corner: reserved_bits' duplicate names its value", vx0("id 42 reserved_bits 0 reserved_bits 1"),
			ExitMinusOne, `either "1" is duplicate, or "1" is garbage`, none, []int{}},
		kindRefusal{"corner: any is no address", vx0("id 42 local any"),
			ExitMinusOne, `argument "any" is wrong: invalid local address`, none, []int{}},
		kindRefusal{"corner: a df word by prefix", vx0("id 42 df se"),
			ExitMinusOne, `argument "se" is wrong: DF must be 'unset', 'set' or 'inherit'`, none, []int{}},
		kindRefusal{"corner: help is not goip's", vx0("help"),
			ExitUsage, "not implemented", none, []int{}},
	)
	runKindRefusals(t, tests)
}

// vxAttr is one attribute as the encoder pads it.
func vxAttr(atype uint16, val ...byte) []byte {
	ab := xtcpnl.NewAttrBuilder(make([]byte, 64))
	if err := ab.PutBytes(atype, val); err != nil {
		panic(err)
	}
	return ab.Bytes()
}

// TestParseVxlanArgs covers what no capture reaches: the words' encoding
// under -4 and -6, the abbreviations and the zero words.
func TestParseVxlanArgs(t *testing.T) {
	id42 := vxAttr(unix.IFLA_VXLAN_ID, 42, 0, 0, 0)
	learn := func(v byte) []byte { return vxAttr(unix.IFLA_VXLAN_LEARNING, v) }
	tests := []struct {
		description string
		family      uint8
		args        string
		expected    []byte
		warned      bool
	}{
		{"positive: port is the source range", unix.AF_UNSPEC, "id 42 port 1000 2000",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_PORT_RANGE, 0x03, 0xe8, 0x07, 0xd0), id42, learn(1)), true},
		{"positive: gpe's port", unix.AF_UNSPEC, "external gpe",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_COLLECT_METADATA, 1), vxAttr(unix.IFLA_VXLAN_GPE), learn(0),
				vxAttr(unix.IFLA_VXLAN_PORT, 0x12, 0xb6)), false},
		{"negative: -4 with an address sends no default", unix.AF_INET, "id 42 remote 10.0.0.1",
			slices.Concat(id42, vxAttr(unix.IFLA_VXLAN_GROUP, 10, 0, 0, 1), learn(1)), true},
		{"boundary: an empty range", unix.AF_UNSPEC, "id 42 srcport 0 0", slices.Concat(id42, learn(1)), true},
		{"boundary: the zero address", unix.AF_UNSPEC, "id 42 local 0.0.0.0",
			slices.Concat(id42, vxAttr(unix.IFLA_VXLAN_LOCAL, 0, 0, 0, 0), learn(1)), true},
		{"corner: -4 with no address", unix.AF_INET, "id 42",
			slices.Concat(id42, vxAttr(unix.IFLA_VXLAN_GROUP, 0, 0, 0, 0), learn(1)), true},
		{"corner: -6 with no address", unix.AF_INET6, "id 42",
			slices.Concat(id42, vxAttr(unix.IFLA_VXLAN_GROUP6, make([]byte, 16)...), learn(1)), true},
		{"corner: external then learning", unix.AF_UNSPEC, "external learning",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_COLLECT_METADATA, 1), learn(1)), true},
		{"corner: learning then external", unix.AF_UNSPEC, "learning external",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_COLLECT_METADATA, 1), learn(0)), true},
		{"corner: tos is hex", unix.AF_UNSPEC, "id 42 tos 10",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_TOS, 0x10), id42, learn(1)), true},
		{"corner: d is dev", unix.AF_UNSPEC, "id 42 d d0",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_LINK, 7, 0, 0, 0), id42, learn(1)), true},
		{"corner: the zero words", unix.AF_UNSPEC, "id 42 ttl auto " + xtcpnl.VxlanKeywordAgeing + " none maxaddress unlimited",
			slices.Concat(vxAttr(unix.IFLA_VXLAN_TTL, 0), vxAttr(xtcpnl.IflaVxlanAgeing, 0, 0, 0, 0),
				vxAttr(unix.IFLA_VXLAN_LIMIT, 0, 0, 0, 0), id42, learn(1)), true},
		{"corner: a v4-mapped multicast address is a remote", unix.AF_UNSPEC, "id 42 remote ::ffff:239.1.1.1",
			slices.Concat(id42, vxAttr(unix.IFLA_VXLAN_GROUP6, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 239, 1, 1, 1), learn(1)), true},
	}
	resolve := func(name string) (int32, error) {
		if name == "d0" {
			return 7, nil
		}
		return 0, nil
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var errOut bytes.Buffer
			settings, err := parseVxlanArgs(&errOut, tc.family, false, strings.Fields(tc.args), resolve)
			if err != nil {
				t.Fatal(err)
			}
			got, err := xtcpnl.EncodeVxlanInfoData(settings, false)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.expected) {
				t.Errorf("got  % x\nwant % x", got, tc.expected)
			}
			if warned := errOut.Len() > 0; warned != tc.warned {
				t.Errorf("warned %v, want %v: %q", warned, tc.warned, errOut.String())
			}
		})
	}
}

// TestParseVxlanArgsErrors covers the refusals no table row reaches
// through runLink: the statuses' sentinels.
func TestParseVxlanArgsErrors(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    error
	}{
		{"negative: a missing dev is exit(nodev)", "id 42 dev nosuch", ErrLinkUsage},
		{"negative: srcport's second value missing", "id 42 srcport 1", ErrLinkUsage},
		{"boundary: ttl 255 is the top", "id 42 ttl 0x100", ErrLinkUsage},
		{"corner: any with the other address's family can't be sent", "id 42 local 10.0.0.1 remote any", ErrNotImplemented},
		{"corner: a remote after a v4 local must be v4", "id 42 local 10.0.0.1 remote ::1", ErrInvalidArgument},
	}
	resolve := func(string) (int32, error) { return 0, nil }
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := parseVxlanArgs(&bytes.Buffer{}, unix.AF_UNSPEC, false, strings.Fields(tc.args), resolve)
			if !errors.Is(err, tc.expected) {
				t.Errorf("got %v, want %v", err, tc.expected)
			}
		})
	}
}
