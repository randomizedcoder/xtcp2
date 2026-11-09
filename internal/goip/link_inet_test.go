package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The `ip link set … inet` tests (docs/netlink/goip-link-kinds.md,
// "KG0-T01 `inet` in goip's keyword chain"), against KC1's rows of the
// 7.3.0 rtnl corpus, which ran `ip link set dev d0 inet …` with d0 at
// ifindex 3.

// inetRtnlDir holds KC1-T01's rows.
const inetRtnlDir = "../../pkg/xtcpnl/testdata/7_3_0/link-write/rtnl/"

// inetCorpusArgs are KC1-T01's words after `inet`, keyed by slug
// (rtnl-write-corpus.exp).
func inetCorpusArgs() map[string]string {
	return map[string]string{
		"inet_forwarding_on":    "forwarding on",
		"inet_rp_filter_max":    "rp_filter 2",
		"inet_rp_filter_over":   "rp_filter 3",
		"inet_tag_hex":          "tag 0x10",
		"inet_arp_ignore_octal": "arp_ignore 010",
		"inet_medium_id_neg1":   "medium_id -1",
		"inet_medium_id_neg2":   "medium_id -2",
		"inet_several":          "forwarding on proxy_arp on rp_filter 1 arp_ignore 2",
		"inet_every_param": "forwarding on proxy_arp on accept_redirects on secure_redirects on " +
			"send_redirects on shared_media on rp_filter 1 accept_source_route on bootp_relay on " +
			"log_martians on tag 7 arp_filter on medium_id 1 disable_xfrm on disable_policy on " +
			"force_igmp_version 2 arp_announce 1 arp_ignore 1 promote_secondaries on arp_accept 1 " +
			"arp_notify on accept_local on src_vmark on proxy_arp_pvlan on route_localnet on bc_forwarding on " +
			"igmpv2_unsolicited_report_interval 1000 igmpv3_unsolicited_report_interval 1000 " +
			"ignore_routes_with_linkdown on drop_unicast_in_l2_multicast on drop_gratuitous_arp on " +
			"arp_evict_nocarrier on",
		"inet_repeated":      "forwarding on forwarding off",
		"inet_then_mtu":      "forwarding on mtu 1400",
		"inet_then_garbage":  "forwarding on bogus",
		"inet_bad_on_off":    "forwarding maybe",
		"inet_bad_number":    "rp_filter x",
		"inet_no_param":      "",
		"inet_unknown_param": "bogus on",
		"inet_abbreviated":   "forw on",
		"inet_no_value":      "forwarding",
	}
}

// inetArgv is `link set dev d0 inet <words>`.
func inetArgv(words string) []string {
	return append([]string{"set", "dev", "d0", "inet"}, strings.Fields(words)...)
}

// useLLMap720 makes the index-cache lookups 7.2.0's, which captured the
// 7.3.0 corpus, for the rest of t (main_test.go defaults to 7.1.0's).
func useLLMap720(t *testing.T) {
	t.Helper()
	prev := req.LLMapNameOnly
	req.LLMapNameOnly = true
	t.Cleanup(func() { req.LLMapNameOnly = prev })
}

// runInet runs `ip link` args against the d0 lookup of inet_forwarding_on's
// capture, returning goip's write (nil for none), its lookups and its error.
func runInet(t *testing.T, args []string) ([]byte, [][]byte, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	src := newLinkReplay(t, inetRtnlDir+"inet_forwarding_on/ip.pcap")
	_, err := runLinkWith(t, src, unix.AF_UNSPEC, args)
	got, rerr := os.ReadFile(out)
	if rerr != nil {
		got = nil
	}
	return got, src.gets, err
}

// TestLinkSetInetMatchesIp replays every KC1 row the kernel answered: goip
// sends ip's d0 lookup and writes ip's request, byte for byte apart from
// nlmsg_seq and nlmsg_pid. The kernel's EINVAL on the two refused rows is
// the kernel's, not goip's to know.
func TestLinkSetInetMatchesIp(t *testing.T) {
	useLLMap720(t)
	tests := []struct {
		description string
		slug        string
	}{
		{description: "positive: one on/off param", slug: "inet_forwarding_on"},
		{description: "positive: several params, in argument order", slug: "inet_several"},
		{description: "positive: every param", slug: "inet_every_param"},
		{description: "positive: a numeric param at the top of the kernel's range", slug: "inet_rp_filter_max"},
		{description: "negative: a request the kernel refuses is built anyway", slug: "inet_rp_filter_over"},
		{description: "negative: one under medium_id's minimum, built anyway", slug: "inet_medium_id_neg2"},
		{description: "boundary: base 0, hex", slug: "inet_tag_hex"},
		{description: "boundary: base 0, octal", slug: "inet_arp_ignore_octal"},
		{description: "boundary: the one signed param", slug: "inet_medium_id_neg1"},
		{description: "corner: the pushed-back word is the next keyword", slug: "inet_then_mtu"},
		{description: "corner: a repeated param", slug: "inet_repeated"},
	}
	words := inetCorpusArgs()
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := inetRtnlDir + tc.slug + "/ip.pcap"
			c, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			reqs := c.Requests()
			if len(reqs) != 2 {
				t.Fatalf("capture holds %d requests, want the lookup and the write", len(reqs))
			}
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			src := newLinkReplay(t, pcap)
			if _, err := runLinkWith(t, src, unix.AF_UNSPEC, inetArgv(words[tc.slug])); err != nil {
				t.Fatalf("goip: %v", err)
			}
			if len(src.gets) != 1 || !bytes.Equal(src.gets[0], wireBytesOf(reqs[0])) {
				t.Errorf("lookups % x, want % x", src.gets, wireBytesOf(reqs[0]))
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if want := wireBytesOf(reqs[1]); !bytes.Equal(zeroSeqPidCopy(got), want) {
				t.Errorf("write:\n got %x\nwant %x", zeroSeqPidCopy(got), want)
			}
		})
	}
	if len(tests) != 11 {
		t.Errorf("%d kernel-answered rows, want 11", len(tests))
	}
}

// ipErrLine is a refused row's first stderr line, less invarg's and
// duparg2's trailing period.
func ipErrLine(t *testing.T, slug string) string {
	t.Helper()
	b, err := os.ReadFile(inetRtnlDir + slug + "/ip.err")
	if err != nil {
		t.Fatal(err)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSuffix(line, ".")
}

// TestLinkSetInetRefusals is goip's status and text for each refusal ip
// makes inside iplink_parse, before it sends anything: no lookup, no write.
func TestLinkSetInetRefusals(t *testing.T) {
	useLLMap720(t)
	type row struct {
		description string
		args        []string
		code        int
		stderr      string
	}
	var tests []row
	words := inetCorpusArgs()
	for _, slug := range []string{"inet_then_garbage", "inet_bad_on_off", "inet_bad_number", "inet_no_param",
		"inet_unknown_param", "inet_abbreviated", "inet_no_value"} {
		rc, err := os.ReadFile(inetRtnlDir + slug + "/ip.rc")
		if err != nil {
			t.Fatal(err)
		}
		code := ExitUsage
		if strings.TrimSpace(string(rc)) == "255" {
			code = ExitMinusOne
		}
		tests = append(tests, row{"negative: " + slug, inetArgv(words[slug]), code, ipErrLine(t, slug)})
	}
	set := func(words string) []string { return append([]string{"set", "dev", "d0"}, strings.Fields(words)...) }
	tests = append(tests,
		row{"negative: an on/off param given a number", inetArgv("forwarding 1"), ExitUsage,
			`Error: argument of "forwarding" must be one of "off", "on", not "1"`},
		row{"negative: a numeric param given on", inetArgv("rp_filter on"), ExitMinusOne,
			`Error: argument "on" is wrong: Invalid "rp_filter" value`},
		row{"boundary: a value past a C int", inetArgv("tag 2147483648"), ExitMinusOne,
			`Error: argument "2147483648" is wrong: Invalid "tag" value`},
		row{"boundary: inet as the last word", inetArgv(""), ExitMinusOne,
			`Command line is not complete. Try option "help"`},
		row{"corner: ine is a device name", set("ine forwarding on"), ExitMinusOne,
			`Error: either "dev" is duplicate, or "ine" is garbage`},
		row{"corner: a param word outside inet", set("forwarding on"), ExitMinusOne,
			`Error: either "dev" is duplicate, or "forwarding" is garbage`},
		row{"corner: an existing refusal moves to 255", set("mtu 1400 mtu 1500"), ExitMinusOne,
			`Error: duplicate "mtu": "1500" is the second value`},
	)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, gets, err := runInet(t, tc.args)
			if err == nil {
				t.Fatal("no error")
			}
			if code := exitCodeOf(err); code != tc.code {
				t.Errorf("exit %d, want %d (%v)", code, tc.code, err)
			}
			if !strings.Contains(err.Error(), tc.stderr) {
				t.Errorf("error %q, want %q in it", err, tc.stderr)
			}
			if len(gets) != 0 || got != nil {
				t.Errorf("%d lookups and a write of %d bytes, want nothing sent", len(gets), len(got))
			}
		})
	}
}

// writeAttrs is the attributes of goip's written RTM_NEWLINK, in order.
func writeAttrs(t *testing.T, msg []byte) []xtcpnl.LinkAttr {
	t.Helper()
	var out []xtcpnl.LinkAttr
	body := msg[xtcpnl.NlMsgHdrSizeCst+xtcpnl.IfInfomsgSizeCst:]
	if err := xtcpnl.WalkRTAttrs(body, func(atype uint16, val []byte) {
		out = append(out, xtcpnl.LinkAttr{Type: atype, Payload: slices.Clone(val)})
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLinkSetInetBuilt covers requests no KC1 row captured, against
// xtcpnl.LinkAttrInetDevconf, which KP0 pinned to ip's bytes.
func TestLinkSetInetBuilt(t *testing.T) {
	useLLMap720(t)
	id := func(name string) uint16 {
		p := lookupInetParam(name)
		if p == nil {
			t.Fatalf("no param %q", name)
		}
		return p.ID
	}
	inet := func(s ...xtcpnl.InetDevconfSetting) xtcpnl.LinkAttr {
		a, err := xtcpnl.LinkAttrInetDevconf(s)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	tests := []struct {
		description string
		args        string
		expected    []xtcpnl.LinkAttr
	}{
		{description: "positive: one numeric param", args: "inet arp_announce 2",
			expected: []xtcpnl.LinkAttr{inet(xtcpnl.InetDevconfSetting{ID: id("arp_announce"), Value: 2})}},
		{description: "corner: inet twice sends two IFLA_AF_SPECs", args: "inet forwarding on inet proxy_arp on",
			expected: []xtcpnl.LinkAttr{inet(xtcpnl.InetDevconfSetting{ID: 1, Value: 1}),
				inet(xtcpnl.InetDevconfSetting{ID: 3, Value: 1})}},
		{description: "corner: a generic keyword before inet", args: "mtu 1400 inet forwarding on",
			expected: []xtcpnl.LinkAttr{xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400),
				inet(xtcpnl.InetDevconfSetting{ID: 1, Value: 1})}},
		{description: "boundary: the highest id", args: "inet arp_evict_nocarrier off",
			expected: []xtcpnl.LinkAttr{inet(xtcpnl.InetDevconfSetting{ID: 33, Value: 0})}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, gets, err := runInet(t, append([]string{"set", "dev", "d0"}, strings.Fields(tc.args)...))
			if err != nil {
				t.Fatal(err)
			}
			if len(gets) != 1 {
				t.Errorf("%d lookups, want d0's", len(gets))
			}
			attrs := writeAttrs(t, got)
			if len(attrs) != len(tc.expected) {
				t.Fatalf("attributes %v, want %v", attrs, tc.expected)
			}
			for i := range attrs {
				if attrs[i].Type != tc.expected[i].Type || !bytes.Equal(attrs[i].Payload, tc.expected[i].Payload) {
					t.Errorf("attribute %d: got %d % x, want %d % x", i, attrs[i].Type, attrs[i].Payload,
						tc.expected[i].Type, tc.expected[i].Payload)
				}
			}
		})
	}
}

// TestErrLinkUsageExitCode pins exitCodeOf's arm order: a link usage
// refusal wraps ErrInvalidArgument and still exits 255.
func TestErrLinkUsageExitCode(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: a link usage refusal", err: linkUsage(incompleteErr()), expected: ExitMinusOne},
		{description: "negative: a plain argument error", err: incompleteErr(), expected: ExitUsage},
		{description: "corner: wrapped again", err: errors.Join(linkUsage(invargErr("x", "y"))), expected: ExitMinusOne},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf = %d, want %d", got, tc.expected)
			}
		})
	}
}
