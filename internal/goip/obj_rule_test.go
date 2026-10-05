package goip

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// ruleDumpPcap is `ip rule show`: two datagrams, one request and one multipart
// reply, and the smallest capture in the corpus.
//
// Small for a structural reason rather than an incidental one.
// iprule_list_flush_or_save calls no ll_init_map, because FRA_IIFNAME and
// FRA_OIFNAME arrive as strings and there is no index to resolve — so unlike
// every other object here there is no side transaction, and unlike
// neighDumpPcap there is no portid to filter on either. Every row below replays
// the whole file.
const ruleDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getrule.pcap"

// ruleDumpPcap6 is `ip -6 rule show`. One byte of the request differs and the
// reply set is a different table entirely: rules are per family, and IPv6 ships
// two kernel defaults where IPv4 ships three.
const ruleDumpPcap6 = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getrule6.pcap"

// ruleDumpPcapMesh is the mesh namespace's rule dump, which is the untouched
// kernel default set. It is the control for every row above it: the twenty
// rules in the clean capture are the topology's doing, not the kernel's.
const ruleDumpPcapMesh = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getrule.pcap"

// ruleDumpPcapMesh6, ruleDumpPcapTunnel and ruleDumpPcapTunnel6 are the other
// three namespaces' rule dumps.
//
// The capture writes a full sidecar set per namespace whether or not the
// namespace has anything interesting in it, so these four files and the six
// tunnel/mesh sidecars they answer are cheap to leave unread — which is exactly
// how `dumps/ip_link_json` and `dumps/ip_addr_json` sat committed and unread
// until someone went looking, and one of them turned out to disagree with the
// renderer. Every rule fixture the capture writes therefore has a row below,
// including the boring ones. A fixture nobody reads is not evidence.
const ruleDumpPcapMesh6 = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getrule6.pcap"
const ruleDumpPcapTunnel = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getrule.pcap"
const ruleDumpPcapTunnel6 = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getrule6.pcap"

// There is deliberately no `-4` pcap. iprule_list_flush_or_save substitutes
// AF_INET for AF_UNSPEC before building the request (ip/iprule.c:748-752), so
// `ip rule show` and `ip -4 rule show` emit identical bytes; the byte-level
// claim is a parity row, and the output-level claim is the ip_rule_v4 row
// below.

const ruleSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// TestRuleShowMatchesCapturedSidecars replays the committed RTM_GETRULE dumps
// and compares goip's output with the `ip rule show` sidecars captured
// alongside them.
//
// Every text row is BYTE exact, with no multiset escape. That is not strictness
// for its own sake — it is what a rule listing allows and a neighbor listing
// does not. The kernel walks fib_rules by ascending preference
// (fib_nl_dumprule iterates the ops list in order), so a rule dump has a
// defined order that a neighbor dump, walked out of a hash table, does not.
// goip therefore sorts nothing here, and a re-capture cannot reorder these
// lines the way it reorders ip_neigh.
//
// go test ./internal/goip/ -run TestRuleShowMatchesCapturedSidecars
func TestRuleShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		// pcap overrides ruleDumpPcap for a row whose command has a capture of
		// its own.
		pcap    string
		sidecar string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
	}{
		{
			description: "positive: `rule show` reproduces ip_rule line for line",
			args:        []string{"rule", "show"},
			sidecar:     "ip_rule",
		},
		{
			// `ip rule` with no verb lists (ip/iprule.c:1238-1239), like
			// `ip route` and unlike `ip link`.
			description: "positive: a bare `rule` is a list",
			args:        []string{"rule"},
			sidecar:     "ip_rule",
		},
		{
			description: "positive: `rule list` is the same listing as `rule show`",
			args:        []string{"rule", "list"},
			sidecar:     "ip_rule",
		},
		{
			description: "positive: `rule lst` is the third accepted spelling",
			args:        []string{"rule", "lst"},
			sidecar:     "ip_rule",
		},
		{
			// The verbs go through matches(), so each abbreviates to its
			// shortest unambiguous prefix — and which prefix wins is decided by
			// do_iprule's ORDER, not alphabetically. "l" resolves to list
			// because list is tested first; "ls" resolves to lst because it is
			// not a prefix of "list" or "show".
			description: "boundary: `rule l` abbreviates list, the first verb in do_iprule's chain",
			args:        []string{"rule", "l"},
			sidecar:     "ip_rule",
		},
		{
			description: "boundary: `rule ls` abbreviates lst and not list, list having needed a third letter",
			args:        []string{"rule", "ls"},
			sidecar:     "ip_rule",
		},
		{
			description: "boundary: `rule s` abbreviates show, no earlier verb in the chain starting with s",
			args:        []string{"rule", "s"},
			sidecar:     "ip_rule",
		},
		{
			// The identity claim, at the output level. `ip rule show` and
			// `ip -4 rule show` send the same 28 bytes because AF_UNSPEC is
			// substituted away, so the two sidecars are byte-identical files —
			// and asserting against both is what turns that into a test rather
			// than an observation. The byte-level half is a parity row, since
			// no output comparison can see a request.
			description: "positive: `-4 rule show` reproduces ip_rule_v4, which is ip_rule byte for byte",
			args:        []string{"-4", "rule", "show"},
			sidecar:     "ip_rule_v4",
		},
		{
			// The `-d` half of the presence-versus-value pair, and the cheapest
			// -d in goip: one token per line, on all twenty.
			//
			// Every rule the topology added carries FRA_PROTOCOL with value
			// ZERO and the kernel's own three carry RTPROT_KERNEL, so
			// print_rule's guard suppresses all twenty without -d
			// (ip/iprule.c:551-557). A decoder that dropped the attribute
			// instead of recording its presence agrees with `ip` on every line
			// of ip_rule and disagrees on every line of ip_rule_n — which is
			// why both files are asserted rather than the one that differs.
			description: "positive: `-d rule show` reproduces ip_rule_n, adding a proto token to every line",
			args:        []string{"-d", "rule", "show"},
			sidecar:     "ip_rule_n",
		},
		{
			// The JSON half. Compared in order, not as a multiset: unlike the
			// neighbor listings there is nothing to normalize, so an entry that
			// moved is a real failure.
			description:    "positive: `-json rule show` reproduces ip_rule_json's keys and values",
			args:           []string{"-json", "rule", "show"},
			sidecar:        "ip_rule_json",
			jsonEquivalent: true,
		},
		{
			description: "positive: `-6 rule show` reproduces ip_rule6 against its own capture",
			args:        []string{"-6", "rule", "show"},
			pcap:        ruleDumpPcap6,
			sidecar:     "ip_rule6",
		},
		{
			// The line that proves the token order, because it is the one place
			// in print_rule where a `-d` token is not last: proto goes BETWEEN
			// the action and the flowlabel (:551-557 then :583-596). A renderer
			// that appended proto at the end of the line passes every row above
			// and fails this one.
			description: "corner: `-6 -d rule show` puts the proto token before flowlabel, not at the end",
			args:        []string{"-6", "-d", "rule", "show"},
			pcap:        ruleDumpPcap6,
			sidecar:     "ip_rule6_n",
		},
		{
			// The control. No rule was ever added in the mesh namespace, so its
			// dump is the kernel's own three and nothing else.
			description: "corner: the mesh namespace prints only the three kernel default rules",
			args:        []string{"rule", "show"},
			pcap:        ruleDumpPcapMesh,
			sidecar:     "mesh/ip_rule",
		},
		{
			// And its `-d` form, which is the only place the kernel-owned
			// protocol name is asserted without eighteen user rules around it.
			description: "positive: the mesh namespace's -d listing names RTPROT_KERNEL on all three lines",
			args:        []string{"-d", "rule", "show"},
			pcap:        ruleDumpPcapMesh,
			sidecar:     "mesh/ip_rule_n",
		},
		{
			// The identity claim again, in a namespace where the listing is
			// three lines instead of twenty. It is worth repeating precisely
			// because the clean listing is long: with twenty lines, ip_rule and
			// ip_rule_v4 agreeing is overwhelming evidence; with three, it is
			// the same claim resting on almost nothing, so if the substitution
			// at ip/iprule.c:748-752 ever stopped happening the short listing
			// is where it would be easiest to miss.
			description: "positive: the mesh namespace's `-4 rule show` is its plain listing too",
			args:        []string{"-4", "rule", "show"},
			pcap:        ruleDumpPcapMesh,
			sidecar:     "mesh/ip_rule_v4",
		},
		{
			// Two lines, not three. fib_default_rules_init adds a `default`
			// rule at 32767 for IPv4 only (net/ipv4/fib_rules.c); IPv6 has no
			// equivalent (net/ipv6/fib6_rules.c). This is that asymmetry with
			// the topology's own rules subtracted away, which is the only place
			// it is visible as a property of the KERNEL rather than of what the
			// capture happened to add.
			description: "corner: the mesh namespace's IPv6 listing has two kernel defaults, not three",
			args:        []string{"-6", "rule", "show"},
			pcap:        ruleDumpPcapMesh6,
			sidecar:     "mesh/ip_rule6",
		},
		{
			description: "positive: the mesh namespace's `-6 -d` listing names RTPROT_KERNEL on both lines",
			args:        []string{"-6", "-d", "rule", "show"},
			pcap:        ruleDumpPcapMesh6,
			sidecar:     "mesh/ip_rule6_n",
		},
		{
			// The JSON shape with every optional key absent. The clean
			// ip_rule_json row exercises which keys appear; this one exercises
			// which do NOT — a renderer that emitted zero-valued keys rather
			// than omitting them produces the same twenty entries there and
			// three wrong ones here.
			description:    "boundary: the mesh namespace's JSON carries only priority, src and table",
			args:           []string{"-json", "rule", "show"},
			pcap:           ruleDumpPcapMesh,
			sidecar:        "mesh/ip_rule_json",
			jsonEquivalent: true,
		},
		{
			// The tunnel namespace is a second, independently captured control.
			// It has no rules of its own either, so every row below should
			// reproduce its mesh counterpart — and TestRuleSidecarsAreIdentical
			// asserts the two files really are the same bytes. Replaying both
			// is what makes that a claim about goip and not only about the
			// capture.
			description: "positive: the tunnel namespace prints the same three kernel default rules",
			args:        []string{"rule", "show"},
			pcap:        ruleDumpPcapTunnel,
			sidecar:     "tunnel/ip_rule",
		},
		{
			description: "positive: the tunnel namespace's `-4 rule show` is its plain listing too",
			args:        []string{"-4", "rule", "show"},
			pcap:        ruleDumpPcapTunnel,
			sidecar:     "tunnel/ip_rule_v4",
		},
		{
			description: "positive: the tunnel namespace's -d listing names RTPROT_KERNEL on all three lines",
			args:        []string{"-d", "rule", "show"},
			pcap:        ruleDumpPcapTunnel,
			sidecar:     "tunnel/ip_rule_n",
		},
		{
			description: "corner: the tunnel namespace's IPv6 listing has two kernel defaults, not three",
			args:        []string{"-6", "rule", "show"},
			pcap:        ruleDumpPcapTunnel6,
			sidecar:     "tunnel/ip_rule6",
		},
		{
			description: "positive: the tunnel namespace's `-6 -d` listing names RTPROT_KERNEL on both lines",
			args:        []string{"-6", "-d", "rule", "show"},
			pcap:        ruleDumpPcapTunnel6,
			sidecar:     "tunnel/ip_rule6_n",
		},
		{
			description:    "boundary: the tunnel namespace's JSON carries only priority, src and table",
			args:           []string{"-json", "rule", "show"},
			pcap:           ruleDumpPcapTunnel,
			sidecar:        "tunnel/ip_rule_json",
			jsonEquivalent: true,
		},

		// ---------------------------------------------------------------
		// `-s` on rules: every row below is a NEGATIVE, and that is the
		// point of the group.
		//
		// `grep -n show_stats ip/iprule.c` returns NOTHING — the file does
		// not reference the variable once, against 5 hits in iproute.c, 4
		// in ipneigh.c and 15 in ipaddress.c. So `-s` changes neither the
		// request nor the output for rules, and goip's silent acceptance of
		// the flag is correct rather than merely harmless.
		//
		// "Correct by accident" and "correct on purpose" look the same in a
		// diff, which is why each row compares against the SAME sidecar its
		// non-`-s` counterpart uses rather than against the `ip_rule_stats`
		// golden. A `-s` that started emitting a token fails here.
		//
		// The two claims are different and both are needed.  These rows say
		// GOIP's `-s` changes nothing, measured against a transcript of `ip`
		// WITHOUT `-s`; TestRuleSidecarsAreIdentical compares `ip_rule_stats`
		// against `ip_rule` and says IPROUTE2's `-s` changes nothing. Only
		// the second can fail if a future iproute2 starts reading show_stats
		// in iprule.c, and only the first can fail if goip starts emitting a
		// token of its own.
		// ---------------------------------------------------------------
		{
			description: "negative: `-s rule show` is byte-identical to `rule show` — iprule.c never reads show_stats",
			args:        []string{"-s", "rule", "show"},
			sidecar:     "ip_rule",
		},
		{
			description: "negative: `-s -6 rule show` adds nothing to the IPv6 listing either",
			args:        []string{"-s", "-6", "rule", "show"},
			pcap:        ruleDumpPcap6,
			sidecar:     "ip_rule6",
		},
		{
			description:    "negative: `-s` adds no JSON key — a stats block would surface here as an extra member even if it printed nothing in text",
			args:           []string{"-s", "-json", "rule", "show"},
			sidecar:        "ip_rule_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: `-s -d rule show` equals `-d rule show`, so -s composes with -d by changing nothing",
			args:        []string{"-s", "-d", "rule", "show"},
			sidecar:     "ip_rule_n",
		},
		{
			description: "corner: the reverse order `-d -s` is a no-op too, which rules out an option-loop ordering effect rather than only a rendering one",
			args:        []string{"-d", "-s", "rule", "show"},
			sidecar:     "ip_rule_n",
		},
		{
			description: "negative: `-s` is a no-op in the tunnel namespace as well — a second, independently captured topology",
			args:        []string{"-s", "rule", "show"},
			pcap:        ruleDumpPcapTunnel,
			sidecar:     "tunnel/ip_rule",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(ruleSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			pcap := tc.pcap
			if pcap == "" {
				pcap = ruleDumpPcap
			}
			t.Setenv("GOIP_REPLAY", pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestRuleSidecarsAreIdentical asserts the two files the identity claim rests
// on really are the same bytes.
//
// It is separate from the rows above because it is a claim about the FIXTURES
// rather than about goip: if a future capture makes ip_rule and ip_rule_v4
// differ, the `-4` row above would still pass — it reads ip_rule_v4 — and the
// premise underneath it would be quietly gone. This fails instead, and names
// what changed.
func TestRuleSidecarsAreIdentical(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		want        bool
	}{
		{
			// iprule_list_flush_or_save substitutes AF_INET for AF_UNSPEC
			// before building the request, so the two commands are the same
			// command.
			description: "positive: ip_rule and ip_rule_v4 are byte-identical, the -4 being a no-op",
			a:           "ip_rule", b: "ip_rule_v4", want: true,
		},
		{
			// The negative that keeps the row above honest. -d adds a token to
			// every line here, unlike the neighbor case where ip_neigh and
			// ip_neigh_n happen to match — so a comparison helper that always
			// said "identical" would be caught.
			description: "negative: ip_rule and ip_rule_n differ, because -d adds a proto token to every line",
			a:           "ip_rule", b: "ip_rule_n", want: false,
		},
		{
			description: "negative: the v6 listing is a different table, not a rendering of the same one",
			a:           "ip_rule", b: "ip_rule6", want: false,
		},
		{
			// Two namespaces, same kernel, same defaults. The tunnel namespace
			// had no rule added either, so its listing must match the mesh one
			// exactly — which is what makes "the mesh set is the kernel's own"
			// a measurement rather than an assumption about one namespace.
			description: "corner: the mesh and tunnel namespaces print identical listings, neither having been given a rule",
			a:           "mesh/ip_rule", b: "tunnel/ip_rule", want: true,
		},
		{
			// The same claim for the other four forms the capture writes per
			// namespace. Spelled out one per row rather than looped, because a
			// loop would let a missing file pass as "nothing to compare".
			description: "corner: the mesh and tunnel -d listings are identical too",
			a:           "mesh/ip_rule_n", b: "tunnel/ip_rule_n", want: true,
		},
		{
			description: "corner: the mesh and tunnel IPv6 listings are identical too",
			a:           "mesh/ip_rule6", b: "tunnel/ip_rule6", want: true,
		},
		{
			description: "corner: the mesh and tunnel `-6 -d` listings are identical too",
			a:           "mesh/ip_rule6_n", b: "tunnel/ip_rule6_n", want: true,
		},
		{
			description: "corner: the mesh and tunnel JSON listings are identical too",
			a:           "mesh/ip_rule_json", b: "tunnel/ip_rule_json", want: true,
		},
		{
			// The `-4` no-op, asserted once more where the listing is three
			// lines rather than twenty — see the mesh `-4` row in
			// TestRuleShowMatchesCapturedSidecars for why the short case is the
			// one worth restating.
			description: "positive: mesh/ip_rule and mesh/ip_rule_v4 are byte-identical, the -4 being a no-op there too",
			a:           "mesh/ip_rule", b: "mesh/ip_rule_v4", want: true,
		},
		{
			description: "positive: tunnel/ip_rule and tunnel/ip_rule_v4 are byte-identical, the -4 being a no-op there too",
			a:           "tunnel/ip_rule", b: "tunnel/ip_rule_v4", want: true,
		},
		{
			// The negative for the namespace pair: an empty namespace still has
			// a three-versus-two split between families, so "identical across
			// namespaces" must not collapse into "identical across families".
			description: "negative: the mesh IPv4 and IPv6 listings differ, IPv6 having no default rule",
			a:           "mesh/ip_rule", b: "mesh/ip_rule6", want: false,
		},
		{
			// The `-s` no-op, as a claim about IPROUTE2. The `-s` rows in
			// TestRuleShowMatchesCapturedSidecars replay a pcap through goip
			// and compare against an `ip` transcript taken WITHOUT `-s`, so
			// they catch a goip that started emitting a token and nothing
			// else; if a future iprule.c began reading show_stats, every one
			// of them would still pass. This row reads two real `ip`
			// transcripts, one given `-s` and one not, which is the only
			// offline evidence that the empty `grep -n show_stats
			// ip/iprule.c` is upstream's and not an assumption.
			description: "positive: ip_rule_stats and ip_rule are byte-identical — iprule.c never reads show_stats",
			a:           "ip_rule_stats", b: "ip_rule", want: true,
		},
		{
			description: "corner: the mesh namespace's `-s` listing is a no-op too, on an independently captured topology",
			a:           "mesh/ip_rule_stats", b: "mesh/ip_rule", want: true,
		},
		{
			description: "corner: the tunnel namespace's `-s` listing is a no-op too",
			a:           "tunnel/ip_rule_stats", b: "tunnel/ip_rule", want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(ruleSidecarDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(ruleSidecarDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.want {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v\n %s = %q\n %s = %q",
					tt.a, tt.b, got, tt.want, tt.a, a, tt.b, b)
			}
		})
	}
}

// TestRunRuleArgs covers the CLI surface: the verbs runRule accepts, and the
// selectors it refuses rather than silently ignores.
//
// Refusing matters more for `ip rule` than for any other object in goip,
// because a rule selector cannot change a request byte.
// iprule_list_flush_or_save parses every one of them into `filter` and
// filter_nlmsg applies them to REPLIES (ip/iprule.c:98-243), while the dump
// request stays the same 28 bytes — the kernel will not even accept an
// attribute on it (net/core/fib_rules.c:1278-1281). So answering
// `ip rule show pref 100` with all twenty rules is a wrong answer that LOOKS
// like a superset, and the parity harness compares stdout, so it would surface
// as a rendering divergence on nineteen lines rather than as a missing
// selector.
//
// go test ./internal/goip/ -run TestRunRuleArgs
func TestRunRuleArgs(t *testing.T) {
	const firstLine = "0:\tfrom all lookup local\n"

	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `rule show` renders the first captured rule",
			args:             []string{"rule", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// `ip rule` with no verb lists (ip/iprule.c:1238-1239), like
			// `ip route` and unlike `ip link`.
			description:      "positive: a bare `rule` is a list",
			args:             []string{"rule"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// The object abbreviates too, and "ru" is the shortest prefix that
			// reaches it: "r" is ambiguous with "route" and "raw", so
			// ip/ip.c's table walk takes the first match rather than erroring.
			description:      "positive: `ru s` abbreviates both the object and the verb",
			args:             []string{"ru", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// The verbs go through matches(), so each abbreviates to its
			// shortest unambiguous prefix — and which prefix wins is decided by
			// do_iprule's ORDER, not alphabetically. "l" resolves to list
			// because list is tested first.
			description:      "boundary: `rule l` abbreviates list, the first verb in the chain",
			args:             []string{"rule", "l"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "ls" is not a prefix of "list" — that would need a third letter —
			// and not of "show", so it can only be lst.
			description:      "boundary: `rule ls` abbreviates lst and not list",
			args:             []string{"rule", "ls"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "sa" is NOT a prefix of "show", so it cannot resolve to the
			// listing however short it is. iproute2 gives it to `save`, which
			// goip does not implement, and the two therefore agree that this
			// command line is not a listing. The row exists because the naive
			// reading — "s abbreviates show, so sa does too" — is wrong.
			description:      "negative: `rule sa` is save, not show, and save is not implemented",
			args:             []string{"rule", "sa"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A write verb must stay unreachable: goip is read-only, and `add`
			// reaching a handler at all would be the wrong kind of bug.
			description:      "negative: the write verb `add` is refused",
			args:             []string{"rule", "add"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `rule flush` is refused, being the destructive half of the same function",
			args:             []string{"rule", "flush"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: an unknown rule verb is refused",
			args:             []string{"rule", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The selector group. Each is parsed by iproute2 and applied
			// client-side, and each is refused here rather than ignored. They
			// are ExitUsage and not ExitFailure because the feature is absent,
			// not the argument wrong — the same split obj_neigh_test.go draws
			// between an unknown verb and an unresolvable device.
			description:      "negative: the `pref` selector is refused rather than ignored",
			args:             []string{"rule", "show", "pref", "100"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: the `from` selector is refused",
			args:             []string{"rule", "show", "from", "192.0.2.0/24"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// Worth its own row because it is the selector that looks cheapest:
			// FRA_IIFNAME is a string on the wire, so `iif` needs no name table
			// and could be filtered with a string compare. It is still refused,
			// because the point is not difficulty — it is that a selector goip
			// applies and the netlink tier cannot see is a rendering diff
			// waiting to happen.
			description:      "negative: the `iif` selector is refused, though it needs no name table",
			args:             []string{"rule", "show", "iif", "goip0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The verb is consumed first, so this must fail on `table` rather
			// than on `l`.
			description:      "corner: a selector after an abbreviated verb is still refused",
			args:             []string{"rule", "l", "table", "main"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// Neither verb nor selector, and it takes the selector path,
			// because parseRuleShowArgs refuses its first argument whatever it
			// is. The right answer: goip has nothing to say about either beyond
			// "not implemented".
			description:      "negative: a word that is neither verb nor selector is refused",
			args:             []string{"rule", "show", "wombat"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", ruleDumpPcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("Run(%q) = %d, want %d; stderr=%s",
					tt.args, code, tt.wantCode, stderr.String())
			}
			if tt.wantStdoutPrefix != "" && !strings.HasPrefix(stdout.String(), tt.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), tt.wantStdoutPrefix)
			}
			if tt.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tt.wantStderrSubstr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderrSubstr)
			}
		})
	}
}
