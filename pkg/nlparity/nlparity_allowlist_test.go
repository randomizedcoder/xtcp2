package nlparity

// go test ./pkg/nlparity/ -run TestAllowlist
//
// The allowlist's own tests. Two tables and a census:
//
//   - TestAllowlistLoad validates the loader against constructed documents.
//     Constructed is right here and not a fixture: every row is about a
//     MALFORMED file, and a malformed file is not something a capture produces.
//   - TestAllowlistSuppression is the one that matters. It asserts that the
//     values-only rule holds for every unsuppressible class at a locus that IS
//     allowlisted, which is the exact shape of the bug the rule exists to stop.
//   - TestAllowlistCommitted censuses the committed file, so the two owed
//     version-skew entries cannot be silently deleted or left without an
//     ip_version.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// doc builds an allowlist document from entries, so a row can state the one
// field it is about instead of a whole JSON literal.
func doc(t *testing.T, gated []string, entries ...Entry) []byte {
	t.Helper()
	if entries == nil {
		entries = []Entry{}
	}
	if gated == nil {
		gated = []string{}
	}
	b, err := json.Marshal(map[string]any{
		"_comment":       []string{"test document"},
		"entries":        entries,
		"gated_commands": gated,
	})
	if err != nil {
		t.Fatalf("doc: marshal: %v", err)
	}
	return b
}

// okEntry is a minimal valid entry; rows mutate one field of it.
func okEntry() Entry {
	return Entry{
		Command: "link show",
		Locus:   "request:RTM_GETLINK:IFLA_EXT_MASK",
		Kind:    KindAcceptedDivergence,
		Reason:  "a reason, which is mandatory",
	}
}

func TestAllowlistLoad(t *testing.T) {
	skew := func() Entry {
		e := okEntry()
		e.Kind = KindVersionSkew
		e.IPVersion = "7.1.0"
		return e
	}

	tests := []struct {
		description string
		input       []byte
		wantErr     error
		wantEntries int
		wantGated   []string
	}{
		{
			description: "positive: a valid document loads every entry and its gated list",
			input: doc(t, []string{"link show"}, okEntry(), func() Entry {
				e := okEntry()
				e.Locus = "request:RTM_GETADDR:ifa_family"
				return e
			}()),
			wantEntries: 2,
			wantGated:   []string{"link show"},
		},
		{
			description: "positive: a version-skew entry with an ip_version loads",
			input:       doc(t, nil, skew()),
			wantEntries: 1,
		},
		{
			description: "negative: an unknown kind is refused rather than becoming a fifth category",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.Kind = "probably-fine"
				return e
			}()),
			wantErr: ErrAllowlistBadKind,
		},
		{
			description: "negative: an entry with no reason is a refusal to explain",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.Reason = ""
				return e
			}()),
			wantErr: ErrAllowlistNoReason,
		},
		{
			description: "negative: a version-skew entry with no ip_version cannot expire when the pin moves",
			input: doc(t, nil, func() Entry {
				e := skew()
				e.IPVersion = ""
				return e
			}()),
			wantErr: ErrAllowlistNoIPVersion,
		},
		{
			description: "negative: an ip_version on a non-skew kind claims to be about a release and is not",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.IPVersion = "7.1.0"
				return e
			}()),
			wantErr: ErrAllowlistIPVersionUnked,
		},
		{
			description: "negative: an entry with no command matches every command or none, so it is refused",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.Command = ""
				return e
			}()),
			wantErr: ErrAllowlistNoCommand,
		},
		{
			description: "negative: an entry with no locus would suppress a whole command",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.Locus = ""
				return e
			}()),
			wantErr: ErrAllowlistNoLocus,
		},
		{
			description: "negative: a document that is not JSON at all",
			input:       []byte("{entries: [] }"),
			wantErr:     ErrAllowlistBadJSON,
		},
		{
			description: "boundary: empty entries and empty gated_commands are valid - it is the honest starting state",
			input:       doc(t, nil),
			wantEntries: 0,
			wantGated:   nil,
		},
		{
			description: "boundary: an entry whose reason is a single space is present, so it loads",
			input: doc(t, nil, func() Entry {
				e := okEntry()
				e.Reason = " "
				return e
			}()),
			wantEntries: 1,
		},
		{
			description: "boundary: a gated command with no entries is the goal state, not an error",
			input:       doc(t, []string{"link show"}),
			wantEntries: 0,
			wantGated:   []string{"link show"},
		},
		{
			description: "corner: two entries sharing a locus are ambiguous precedence, so the load fails",
			input:       doc(t, nil, okEntry(), okEntry()),
			wantErr:     ErrAllowlistDuplicate,
		},
		{
			description: "corner: the same locus under a DIFFERENT command is not a duplicate",
			input: doc(t, nil, okEntry(), func() Entry {
				e := okEntry()
				e.Command = "addr show"
				return e
			}()),
			wantEntries: 2,
		},
		{
			description: "corner: the duplicate check sees past a command/locus split, so \"a\"+\"bc\" != \"ab\"+\"c\"",
			input: doc(t, nil, Entry{Command: "a", Locus: "bc", Kind: KindSideSocket, Reason: "r"},
				Entry{Command: "ab", Locus: "c", Kind: KindSideSocket, Reason: "r"}),
			wantEntries: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := LoadAllowlist(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want errors.Is(_, %v)", err, tc.wantErr)
				}
				if got != nil {
					t.Fatalf("got a non-nil Allowlist alongside an error; there is no partially-valid result")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.Entries) != tc.wantEntries {
				t.Fatalf("len(Entries) = %d, want %d", len(got.Entries), tc.wantEntries)
			}
			for _, c := range tc.wantGated {
				if !got.IsGated(c) {
					t.Fatalf("IsGated(%q) = false, want true", c)
				}
			}
			if len(tc.wantGated) == 0 && len(got.GatedCommands) != 0 {
				t.Fatalf("GatedCommands = %v, want empty", got.GatedCommands)
			}
		})
	}
}

func TestAllowlistSuppression(t *testing.T) {
	// One entry, and every row below asks about THAT locus. A row that expects
	// no suppression is therefore asserting the rule, not a missing entry.
	const cmd = "link show"
	const locus = "reply:RTM_NEWLINK:ifindex=2:IFLA_QDISC"
	a, err := LoadAllowlist(doc(t, []string{cmd}, Entry{
		Command: cmd,
		Locus:   locus,
		Kind:    KindVolatileFallback,
		Reason:  "the qdisc name moved between the two control captures",
	}))
	if err != nil {
		t.Fatalf("LoadAllowlist: %v", err)
	}

	tests := []struct {
		description string
		command     string
		locus       string
		class       DivergenceClass
		want        bool
	}{
		{
			description: "positive: a value divergence at an allowlisted locus is suppressed",
			command:     cmd, locus: locus, class: DivergenceValue, want: true,
		},
		{
			description: "positive: the same locus is suppressed for a volatile-fallback entry, so the kind does not change the values-only rule",
			command:     cmd, locus: locus, class: DivergenceValue, want: true,
		},
		{
			description: "negative: a PRESENCE divergence at the same locus is still reported - the whole rule",
			command:     cmd, locus: locus, class: DivergencePresence, want: false,
		},
		{
			description: "negative: a transaction-count divergence is never suppressible",
			command:     cmd, locus: locus, class: DivergenceTransactionCount, want: false,
		},
		{
			description: "negative: a key-set divergence is never suppressible",
			command:     cmd, locus: locus, class: DivergenceKeySet, want: false,
		},
		{
			description: "negative: a key-order divergence is never suppressible",
			command:     cmd, locus: locus, class: DivergenceKeyOrder, want: false,
		},
		{
			description: "negative: an attribute-order divergence is never suppressible",
			command:     cmd, locus: locus, class: DivergenceAttrOrder, want: false,
		},
		{
			description: "negative: a value divergence at an UNLISTED locus is reported",
			command:     cmd, locus: "reply:RTM_NEWLINK:ifindex=3:IFLA_QDISC", class: DivergenceValue, want: false,
		},
		{
			description: "boundary: the same locus under a different command does not match",
			command:     "addr show", locus: locus, class: DivergenceValue, want: false,
		},
		{
			description: "boundary: an empty command and locus match nothing, since neither can be stored",
			command:     "", locus: "", class: DivergenceValue, want: false,
		},
		{
			description: "corner: a class value outside the declared set is unsuppressible, not silently a value",
			command:     cmd, locus: locus, class: DivergenceClass(200), want: false,
		},
		{
			description: "corner: a locus that is a PREFIX of the entry's does not match, so a divergence that moves goes red",
			command:     cmd, locus: "reply:RTM_NEWLINK:ifindex=2", class: DivergenceValue, want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := a.Suppresses(tc.command, tc.locus, tc.class); got != tc.want {
				t.Fatalf("Suppresses(%q, %q, %s) = %v, want %v",
					tc.command, tc.locus, tc.class, got, tc.want)
			}
			// The entry is a fact about the file; suppression is a decision
			// about a finding. Asserting both on the same row is what documents
			// that they are different questions.
			_, found := a.Lookup(tc.command, tc.locus)
			wantFound := tc.command == cmd && tc.locus == locus
			if found != wantFound {
				t.Fatalf("Lookup(%q, %q) found = %v, want %v", tc.command, tc.locus, found, wantFound)
			}
		})
	}
}

func TestAllowlistCommitted(t *testing.T) {
	a, err := EmbeddedAllowlist()
	if err != nil {
		t.Fatalf("EmbeddedAllowlist: %v", err)
	}

	// The owed skews, by the commit each entry's reason must cite.
	//
	// Keyed by command AND locus, which is what Entry.key() is keyed on. It
	// used to be keyed by locus alone, and that stopped working the moment
	// faceb326's two entries were repointed to the locus a comparator
	// actually derives: they are `stdout:keyword:qlen` on two different
	// commands now, so a locus-keyed map would have silently held one of them.
	//
	// None of these four loci is prose, and that is the property being
	// checked. The request loci end in `dump` and `get` rather than in
	// `ll_init_map` and `ll_link_get`, because a locus must be spelled the way
	// the differ DERIVES it — see requestRole in nlparity_diff.go. The stdout
	// loci are `stdout:keyword:qlen` rather than a description of the cause,
	// because internal/goipparity's StdoutLoci() is a closed set and that is
	// the member of it this skew lands on. In both cases the iproute2 call
	// site and the mechanism live in the reason, where prose belongs, and an
	// entry whose locus is prose is an entry that matches nothing.
	wantSkew := []struct{ command, locus, commit string }{
		{"neigh show", "request:RTM_GETLINK:IFLA_EXT_MASK:dump", "de91e928"},
		{"link show dev", "request:RTM_GETLINK:IFLA_EXT_MASK:get", "de91e928"},
		{"link show", "stdout:keyword:qlen", "faceb326"},
		{"-6 addr show", "stdout:keyword:qlen", "faceb326"},
	}

	// Every command a measured-clean Tier C run has earned, in the order the
	// runs happened. Hoisted to function scope because two rows below read it
	// and they assert opposite directions: one that each of these IS gated,
	// one that NOTHING ELSE is. Keeping a single list is what makes the pair
	// a containment check rather than two lists that can drift apart.
	earned := []string{
		"link show",
		"-4 link show",
		"-6 link show",
		"link show dev",
		"addr show",
		"-4 addr show",
		"-6 addr show",
		"addr show dev",
		"route show",
		"route show table all",
		"-6 route show",
		"route show dev",
		"neigh show",
		"neigh show dev",
		"neigh show proxy",
		"-d link show",
		"-d addr show",
		"-d route show",
		"-d neigh show",
		"rule show",
		"-4 rule show",
		"-6 rule show",
		"-d rule show",
		"-s link show",
		// The `-s` sweep's three quiet rows, gated on runs 4 and 5 at
		// fb67da4 and quiet on all five: 0/0/0 in the first three runs,
		// 0/0 in these two. `-s -6 addr show` is the one whose quiet is
		// structural rather than sampled — `-s` cannot reach a PF_INET6
		// link dump's wire at all, inet6_fill_ifinfo hardcoding
		// ext_filter_mask to 0.
		//
		// The sweep's other two are deliberately still out: `-s addr show`
		// is noisy with variance (2/2/6 then 2/2) and so belongs to the
		// `-s link show` bar rather than this one, and `-s neigh show` is
		// stable at nl=2, which is the weaker evidence for a noisy row.
		// They are the ungated surface the row below names.
		"-s -6 addr show",
		"-s route show",
		"-s rule show",
		// Seven of the sixteen rows ntable/netconf/stats/vrf added, gated on
		// runs 8 and 9 - the first live grounding of those objects. Both runs
		// reported GOIP_PARITY_PASS with stdout=0 and no unsuppressed findings,
		// txns matching ip: ntable 2/2 on all three forms, netconf 2/2, vrf
		// 1/1. vrf is the pristine pair (nl=0); ntable and netconf are noisy on
		// the IFLA_STATS/type-64 counters D_control absorbs, nl moving 5->4 and
		// 4->6 across runs. The OTHER nine rows (the stats family and the two
		// netconf-dev forms) diverge on the wire - RTM_GETSTATS type(94) and a
		// per-name RTM_GETLINK that goip answers from a plain dump - so they
		// stay ungated with that reason in goip-parity-allowlist.json's _comment.
		"ntable show",
		"-s ntable show",
		"-j ntable show",
		"netconf show",
		"-j netconf show",
		"vrf show",
		"-j vrf show",
		// nexthop and addrlabel, gated on runs 10 and 11 - the first live
		// grounding of those two objects, which were replay-grounded only.
		// Both runs GOIP_PARITY_PASS with stdout=0 and no unsuppressed findings,
		// txns matching ip: nexthop 3/3, addrlabel 1/1. All five are pristine
		// (control: nl=0), the vrf bar; the -j forms do not reach the wire and
		// -6 addrlabel show is byte-identical to addrlabel show.
		"nexthop show",
		"-j nexthop show",
		"addrlabel show",
		"-6 addrlabel show",
		"-j addrlabel show",
		// The twenty-one link write rows, gated on the phase 7 runs: all
		// GOIP_PARITY_PASS twice with identical verdicts, txns matching ip
		// with goip's GOIP_WRITE_OUT file counted as its write, control nl=0
		// on the nine rows with no lookups and nl=2 to 4 on the rest.
		"link add d0 type dummy",
		"link add v0 type veth peer name v1",
		"link add bond0 type bond",
		"link add bond0 type bond mode 802.3ad lacp_rate fast miimon 100 xmit_hash_policy layer3+4",
		"link add bond0 type bond mode 802.3ad lacp_rate fast lacp_active on ad_select bandwidth ad_actor_sys_prio 100 ad_user_port_key 7 ad_actor_system 02:00:00:00:00:0a min_links 1",
		"link add bond0 type bond mode active-backup arp_interval 100 arp_ip_target 10.0.0.1,10.0.0.2 arp_validate all",
		"link add bond0 type bond mode 4",
		"link set la1 down master bond0",
		"link set la1 master bond0",
		"link set la1 nomaster",
		"link set la1 type bond_slave queue_id 1",
		"link set la1 type bond_slave actor_port_prio 200",
		"link set bond0 type bond lacp_rate slow",
		"link set bond0 type bond mode balance-rr",
		"link set bond0 type bond lacp_rate fast",
		"link add bond0 type bond arp_interval 100 miimon 100",
		"link replace bond0 type bond",
		"link set bond0 up",
		"link set bond0 down",
		"link set d0 name d1 mtu 9000 alias uplink",
		"link del bond0",
		// The two bond `-d link show` read rows, the first read rows with a
		// Setup, gated on two runs after the phase 7 runs: GOIP_PARITY_PASS
		// both times with stdout=0, txns matching ip (2/2 and 4/4) and
		// control nl=0, the setup leaving the veth peers down so nothing
		// moves between sides.
		"-d link show dev bond0",
		"-d link show dev la1",
		// Six stats point gets, gated on the two runs after those: the five
		// bond xstats rows on the same bond, and `stats show group link dev`,
		// which left the permanent set once goip resolved its name with a
		// get by name rather than a link dump. All GOIP_PARITY_PASS both
		// times, stdout=0, control nl=0, txns 2/2.
		"stats show group link dev",
		"stats show dev bond0 group xstats",
		"stats show dev la1 group xstats_slave",
		"stats show dev bond0 group xstats subgroup bond suite 802.3ad",
		"stats show dev la1 group xstats_slave subgroup bond suite 802.3ad",
		"-j stats show dev la1 group xstats_slave",
		// The 26 route write rows (route-write-path.md phase 5), gated on
		// the two runs after those: GOIP_PARITY_PASS both times, stdout=0,
		// identical on both. A dev row is txns 2/2 with control nl=2 (the
		// lookup reply's random MAC and cacheinfo of a fresh dummy), a
		// dev-less row 1/1 with control nl=0.
		"route replace 192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1",
		"route replace 198.51.100.0/24 dev d0",
		"route add 198.51.100.0/24 dev d0",
		"route add 198.51.100.0/24 via 192.0.2.254 dev d0",
		"route add 198.51.100.0/24 via 192.0.2.254",
		"route replace default via 192.0.2.254",
		"route change 198.51.100.0/24 dev d0 metric 100",
		"route append 198.51.100.0/24 dev d0 metric 200",
		"route prepend 198.51.100.0/24 dev d0 metric 300",
		"route test 198.51.100.0/24 dev d0",
		"route del 198.51.100.0/24 dev d0",
		"route del 198.51.100.0/24",
		"route del 203.0.113.99/32",
		"route add 198.51.100.0/24 dev d0 table 100",
		"route add 198.51.100.0/24 dev d0 table 300",
		"route add blackhole 198.51.100.0/24",
		"route add unreachable 198.51.100.0/24",
		"route add local 192.0.2.77 dev d0",
		"route add 198.51.100.0/24 dev d0 src 192.0.2.1 proto static scope link metric 50",
		"route add 198.51.100.0/24 via 203.0.113.1 dev d0 onlink",
		"route add 198.51.100.0/24 via inet6 2001:db8::2 dev d0",
		"-6 route replace 2001:db8:1::/64 via 2001:db8::2 dev d0",
		"-6 route del 2001:db8:1::/64 dev d0",
		"route replace 198.51.100.0/24 dev d0 mtu lock 1400 advmss 1360 window 65535 rtt 3ms rttvar 10ms ssthresh 100 cwnd 10 reordering 5 hoplimit 64 initcwnd 10 initrwnd 20 features ecn tcp_usec_ts fastopen_no_cookie 1",
		"route replace 198.51.100.0/24 dev d0 rtt 24 rttvar 40 rto_min 200",
		"route replace 198.51.100.0/24 dev d0 initcwnd 10 initcwnd 20",
		// A1-T02's four addr-show state rows, each on its own device with a
		// Setup, gated on the two runs after those: GOIP_PARITY_PASS, txns 3/3,
		// control nl=0 stdout=0 and zero findings on both.
		"addr show dev metric0",
		"addr show dev peer4",
		"addr show dev peer6",
		"addr show dev autojoin0",
		// A2-T03's selector rows, each on its addr-show state with a Setup,
		// gated on the two runs after those: GOIP_PARITY_PASS on both, with the
		// same txns, control and zero findings each time. `addr show dynamic`
		// is not here: its lifetime counts down between the sides.
		"addr show to 192.0.2.0/24",
		"addr show scope link",
		"addr show scope all",
		"addr show up",
		"addr show down",
		"addr show permanent",
		"addr show -tentative",
		"addr show tentative",
		"addr show secondary",
		"addr show primary",
		`addr show label "d0:*"`,
		"addr show group default",
		"addr show master bond0",
		"-4 addr show master bond0",
		"addr show nomaster",
		"addr show vrf vrf0",
		"addr show vrf d1",
		"addr show type dummy",
		"addr show type bond_slave",
		"addr show proto 99",
		"addr show novf",
		"addr show d0",
		// A7-T02's address write rows, each on its corpus Setup, gated on the
		// two runs after A7-T01: GOIP_PARITY_PASS on both, with the same txns,
		// control and zero findings each time. The lifetime write is here: its
		// request carries the fixed 3600/1800, and only readback counts down.
		"addr add 192.0.2.10/24 dev d0",
		"addr add 10.0.0.1 peer 10.0.0.2/32 dev d0",
		"addr add 192.0.2.10/24 brd + dev d0",
		"addr add 192.0.2.10/24 brd - dev d0",
		"addr add 192.0.2.10/31 brd + dev d0",
		"addr add 192.0.2.10/24 brd + label d0:1 dev d0",
		"addr add 192.0.2.10/24 metric 50 dev d0",
		"addr add 192.0.2.10/24 pref 7 dev d0",
		"addr add 192.0.2.10/24 proto kernel_lo dev d0",
		"addr add 192.0.2.10/24 valid_lft 3600 preferred_lft 1800 dev d0",
		"addr add 192.0.2.2/24 dev d0",
		"addr add 127.0.0.2/8 dev d0",
		"addr add 224.0.0.251/32 autojoin dev d0",
		"-6 addr add 2001:db8::5/64 nodad dev d0",
		"-6 addr add 2001:db8::5/64 nodad noprefixroute dev d0",
		"addr add nodad 2001:db8::5/64 dev d0",
		"-6 addr add 2001:db8::5/48 mngtmpaddr dev d0",
		"-6 addr add 2001:db8::5/64 nodad optimistic dev d0",
		"addr add 192.0.2.1/24 dev d0",
		"addr change 192.0.2.10/24 dev d0",
		"addr replace 192.0.2.1/24 metric 9 dev d0",
		"addr del 192.0.2.1/24 dev d0",
		"addr del 192.0.2.1 dev d0",
		"addr del 192.0.2.99/24 dev d0",
		"addr add 192.0.2.2/24 scope link dev d0",
		"-6 addr add ff02::fb/128 dev d0",
		"addr add 192.0.2.10/24 label d0:0123456789abc dev d0",
		// A8-T03's address flush rows, gated on two runs with GOIP_PARITY_PASS
		// on both and the same txns, control, datagrams and zero findings each
		// time.
		"addr flush dev d0",
		"addr flush dev d0 primary",
		"addr flush dev d0 secondary",
		"addr flush dev d0 scope link",
		"-6 addr flush dev d0",
		"-s addr flush dev d0",
		"addr flush d0",
		// A9-T03's address restore rows, each reading its own slug's blob,
		// gated on two runs with GOIP_PARITY_PASS on both, all three sides
		// at their full datagram count and zero findings each time.
		"addr restore < restore_fresh.save",
		"addr restore < restore_existing.save",
		// A3-T03's output-mode rows: -br, -o and -br -j on mixed_links, and
		// -o -6 on v6_peer, gated on two runs with GOIP_PARITY_PASS on both and
		// the same txns, control and zero findings each time.
		"-br addr show",
		"-o addr show",
		"-br -j addr show",
		"-o -6 addr show",
		// G0-T01's twelve family, table and `-j` read rows, queued since the
		// JSON facet work, gated on two runs with GOIP_PARITY_PASS on both and
		// the same txns and zero findings each time; control moved only on
		// `-4 neigh show`, ifindex 3's counters, which D_control subtracts.
		"-0 addr show",
		"route show table main",
		"route show table local",
		"-4 route show",
		"-4 neigh show",
		"-6 neigh show",
		"-j addr show",
		"-j link show",
		"-j route show",
		"-j neigh show",
		"-j rule show",
		"-j -s link show",
		// G0-T02's six stats dump rows, once goip named each record with
		// ll_index_to_name's get by index in place of an up-front link dump:
		// txns ip=6 goip=6 and GOIP_PARITY_PASS with zero findings on two
		// runs. Until then they were the six stats rows of the permanent set.
		"stats show group link",
		"-s stats show group link",
		"-j stats show group link",
		"stats show group xstats",
		"-s stats show group xstats",
		"-j stats show group xstats",
		// G0-T03's four. The two `-s` forms held out since the `-s` sweep
		// gate on G0-T01's bar: every run since has passed them with zero
		// findings and txns 2/2, and `-4 neigh show`, the same link and
		// neighbor dumps, showed D_control absorbing a moved count. The two
		// netconf-dev rows gate once goip sent ll_name_to_index's get by
		// name and the segmenter closed an ACKed point get on its ACK:
		// txns 3/3, no finding and no hygiene orphan on two runs.
		"-s addr show",
		"-s neigh show",
		"netconf show dev",
		"-4 netconf show dev",
		// G1-T01's six `route get` rows, new with goip's route get: txns
		// 2/2 (3/3 from/iif, 1/1 unreachable) and GOIP_PARITY_PASS with zero
		// findings on their first two runs.
		"route get 192.0.2.5",
		"route get fibmatch 192.0.2.5",
		"route get 192.0.2.5 notify",
		"-6 route get 2001:db8::5",
		"route get 192.0.2.5 from 198.51.100.2 iif d1",
		"route get 203.0.113.1",
		// G1-T02's two `neigh get` rows: txns 2/2 and GOIP_PARITY_PASS with
		// zero findings on their first two runs.
		"neigh get 192.0.2.2 dev d0",
		"neigh get 192.0.2.9 dev d0",
		// G1-T03's two `nexthop get` and two `nexthop bucket` rows, likewise.
		"nexthop get id 1",
		"nexthop get id 9",
		"nexthop bucket show id 20",
		"nexthop bucket get id 20 index 0",
		// G1-T04's two `link xstats` rows and `link afstats`, likewise.
		"link xstats type bridge",
		"link xstats type bridge_slave",
		"link afstats",
		// G2-T01's `route save` row and its two `route showdump` rows.
		"route save table 100",
		"route showdump < route_save.save",
		"-d route showdump < route_save.save",
		// G2-T02's `route restore` row.
		"route restore < route_save.save",
		// G2-T03's four `route flush` rows.
		"route flush table 100",
		"-s route flush table 100",
		"route flush tab 100",
		"route flush t 100",
		// G3-T01's nine `neigh` write and flush rows.
		"neigh add 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent",
		"neigh a 192.0.2.2 lladdr 02:00:00:00:00:02 dev d0 nud permanent",
		"neigh change 192.0.2.2 lladdr 02:00:00:00:00:03 dev d0 nud reachable",
		"neigh change 192.0.2.9 lladdr 02:00:00:00:00:09 dev d0",
		"neigh replace 192.0.2.3 lladdr 02:00:00:00:00:03 dev d0 nud stale",
		"neigh del 192.0.2.2 dev d0",
		"neigh flush dev d0",
		"-s neigh flush dev d0",
		"neigh f dev d0",
		// G3-T02's twenty `rule` write, flush, save and restore rows.
		"rule add pref 101 from 192.0.2.0/24 table 100",
		"rule add pref 102 to 198.51.100.0/24 table 100",
		"rule add pref 103 tos 0x10 table 100",
		"rule add pref 104 dscp 10 table 100",
		"rule add pref 105 fwmark 0x7/0xff table 100",
		"rule add pref 106 iif d9 table 100",
		"rule add pref 107 oif d9 table 100",
		"rule add pref 108 uidrange 1000-2000 table 100",
		"rule add pref 109 ipproto tcp sport 1000-2000 dport 80 table 100",
		"rule add pref 110 not from 203.0.113.0/24 goto 112",
		"rule add pref 111 table main suppress_prefixlength 0",
		"rule add pref 112 protocol static table 100",
		"rule add pref 113 l3mdev",
		"-6 rule add pref 114 flowlabel 0x12345 table 100",
		"rule add pref 100 from 192.0.2.0/24 table 100",
		"rule del pref 100",
		"rule del pref 4242",
		"rule flush",
		"rule save",
		"rule restore < rule_save.save",
		// G3-T03's eight `nexthop` write and flush rows.
		"nexthop add id 1 via 192.0.2.2 dev d0",
		"nexthop add id 10 group 1,3/2",
		"nexthop add id 20 group 1/2 type resilient buckets 1024",
		"nexthop add id 1 blackhole",
		"nexthop replace id 1 via 192.0.2.4 dev d0",
		"nexthop del id 1",
		"nexthop flush",
		"nexthop f",
		// G3-T04's three `addrlabel` write and flush rows.
		"addrlabel add prefix 2001:db8::/32 label 99",
		"addrlabel del prefix 2001:db8::/32 label 99",
		"addrlabel flush",
		// G3-T05's three `ntable change` rows and its `stats set` row.
		"ntable change name arp_cache dev d0 queue 64",
		"ntable change name nosuch_cache queue 64",
		"ntable change name arp_cache thresh1 64",
		"stats set dev d0 l3_stats on",
	}

	tests := []struct {
		description string
		check       func(t *testing.T, a *Allowlist)
	}{
		{
			description: "positive: the committed file loads, so every validation rule holds on it",
			check: func(t *testing.T, a *Allowlist) {
				if len(a.Entries) == 0 {
					t.Fatalf("no entries; the two owed version-skew entries should be here")
				}
			},
		},
		{
			description: "positive: both owed skews are present, at all four command/locus pairs",
			check: func(t *testing.T, a *Allowlist) {
				for _, w := range wantSkew {
					e, ok := a.Lookup(w.command, w.locus)
					if !ok {
						t.Fatalf("no entry for command %q at locus %q", w.command, w.locus)
					}
					if e.Kind != KindVersionSkew {
						t.Fatalf("%q/%q kind = %q, want %q",
							w.command, w.locus, e.Kind, KindVersionSkew)
					}
					if !strings.Contains(e.Reason, w.commit) {
						t.Fatalf("%q/%q reason does not cite %s",
							w.command, w.locus, w.commit)
					}
				}
			},
		},
		{
			// MEASURED: the two ll_map loci do not hold the same pinned value.
			// ll_link_get is already RTEXT_FILTER_VF|SKIP_STATS = 0x09 at
			// 7.1.0, but ll_init_map calls rtnl_linkdump_req(rth, AF_UNSPEC),
			// which forwards RTEXT_FILTER_VF alone — netlink_route_getneigh.pcap
			// records 01000000 on the wire. So 7bd7f335 moves the dump locus
			// 0x01 -> 0x09 and does not touch the single-get, and only the dump
			// entry may cite it. This row is what stops the two reasons from
			// being copies of each other.
			description: "positive: the two ll_map loci hold different pinned values, 0x01 and 0x09",
			check: func(t *testing.T, a *Allowlist) {
				dump, ok := findByLocus(a, "request:RTM_GETLINK:IFLA_EXT_MASK:dump")
				if !ok {
					t.Fatal("no entry at the dump locus")
				}
				get, ok := findByLocus(a, "request:RTM_GETLINK:IFLA_EXT_MASK:get")
				if !ok {
					t.Fatal("no entry at the get locus")
				}
				if !strings.Contains(dump.Reason, "7bd7f335") {
					t.Error("the dump entry does not cite 7bd7f335, the commit that takes its mask 0x01 -> 0x09")
				}
				if !strings.Contains(dump.Reason, "0x01") {
					t.Error("the dump entry does not state its pinned value of 0x01; " +
						"extrapolating link show's 0x09 to ll_init_map is the error this row exists to prevent")
				}
				// The get entry is only required to state its own pinned
				// value. It is deliberately NOT asserted to omit 7bd7f335 or
				// 0x01: both reasons contrast the two loci on purpose, and a
				// grep over prose would be a test of wording rather than of
				// fact. What is a fact is the value each locus carries.
				if !strings.Contains(get.Reason, "0x09") {
					t.Error("the get entry does not state its pinned value of 0x09")
				}
			},
		},
		{
			// The count is of entries, and it used to be described as a count
			// of loci — true when the two were spelled apart by prose, and
			// false once both were repointed to `stdout:keyword:qlen`, the
			// locus the stdout comparator actually derives. They are separate
			// entries because faceb326's halves land on different COMMANDS,
			// which is what Entry.key() distinguishes; the assertion was
			// always on the entry count and now says so.
			//
			// Three, not two, as of the `-s` sweep — and the row earned its
			// keep by failing when it went to three. `-s -6 addr show` joined
			// the command table and GOIP_PARITY_WARN addr_show_v6_stats
			// reported the ioctl absence for it, so the ioctl half now
			// covers two commands and the zero-suppression half still covers
			// one. The number is maintained by hand on purpose: it is the
			// only thing that makes a fourth entry appearing quietly
			// impossible, and "this skew reaches one more command than it
			// used to" is a fact worth a failing test rather than a silent
			// increment.
			description: "boundary: faceb326 has exactly three entries, which is the measured fact one would hide",
			check: func(t *testing.T, a *Allowlist) {
				n := 0
				for i := range a.Entries {
					if strings.Contains(a.Entries[i].Reason, "faceb326") {
						n++
					}
				}
				if n != 3 {
					t.Fatalf("entries citing faceb326 = %d, want 3", n)
				}
			},
		},
		{
			description: "boundary: every version-skew entry names the pinned release, so it expires when the pin moves",
			check: func(t *testing.T, a *Allowlist) {
				for i := range a.Entries {
					if a.Entries[i].Kind == KindVersionSkew && a.Entries[i].IPVersion != "7.1.0" {
						t.Fatalf("entry %q ip_version = %q, want the pinned 7.1.0",
							a.Entries[i].Locus, a.Entries[i].IPVersion)
					}
				}
			},
		},
		{
			// This row used to assert gated_commands was EMPTY, on the grounds
			// that a command may not be gated before its tier is built. Tier C
			// is built - nix run .#microvm-x86_64-goip-parity - and it reported
			// each name below clean on two consecutive runs whose every line
			// was identical. So the row asserts the state that measurement
			// earned rather than the state that preceded it. Emptiness was
			// never the property worth protecting; gating without evidence
			// was.
			//
			// "Clean" is not "silent", and the list stopped being uniform
			// once `neigh show dev` joined it. Many of these reported control
			// nl=0 stdout=0 with nothing suppressed at all; `-4 addr show`,
			// `neigh show`, `neigh show dev`, `neigh show proxy` and `-d
			// neigh show` reported nl=2 or more, `-6 addr show` reported
			// nl=1 on two runs of five and also carries an allow-suppressed
			// qlen locus. Which names are noisy is a sample and not a
			// property - `-6 addr show` was quiet on the first two runs and
			// noisy on the next two - so this list is deliberately not
			// partitioned into quiet and noisy names. A control-suppressed
			// locus is absorbed before Result.Findings exists, so it is not
			// a finding and does not weaken the gate; the argument is
			// spelled out in goip-parity-allowlist.json's _comment. What
			// every name here does share is zero FINDINGS.
			//
			// The list is spelled out rather than counted. A count would pass
			// for any twenty-four names, and the point of the row is which
			// twenty-four.
			//
			// The four `rule show` names are the newest, and they are the
			// quietest entries in the list: all four measured `control: nl=0
			// stdout=0` with nothing suppressed on both ungated runs, and one
			// transaction per side rather than two. That last number is the
			// interesting one - they are the only commands here whose ip side
			// opens a single socket, because ip/iprule.c has no ll_init_map
			// and needs none, FRA_IIFNAME and FRA_OIFNAME being strings on the
			// wire.
			//
			// `-s link show` is the newest name and the last one in the
			// table to gate. It was held out through every revision of this
			// row above, as the permanently-noisy command whose steady state
			// had to stay observable, and it is in now on three back-to-back
			// runs of one unmodified tree. Those runs did NOT agree, and that
			// is why they count: its control went nl=2, nl=2, nl=6 while
			// Findings stayed empty all three times, which is the only shape
			// of evidence that separates `D_control absorbed the delta` from
			// `there was no delta to absorb`. Three identical quiet runs
			// would have been the weaker result. The argument and the six
			// loci are in goip-parity-allowlist.json's _comment.
			//
			// The three newest names are the `-s` sweep's quiet rows -
			// `-s -6 addr show`, `-s route show`, `-s rule show` - gated on
			// runs 4 and 5 and quiet on all five runs, 0/0/0 then 0/0. They
			// gate on the ORDINARY bar, the identical-runs one, which is
			// worth saying beside the paragraph above: `-s link show` needed
			// disagreeing runs only because it is permanently noisy, and
			// that inversion applies to noisy rows and not to quiet ones.
			// Reading it as the new general rule would hold a quiet command
			// out forever waiting for noise it will never produce.
			//
			// This list was briefly EVERY command in the table, during the
			// interval when twenty-four was the whole of it, which made the
			// row read as if it could be `len(GatedCommands) ==
			// len(Commands())`. It still cannot, and not only because the
			// `-s` sweep took the table to twenty-nine: internal/goipparity
			// owns the table and imports this package, so reading it back
			// here is an import cycle. Spelling the names is also what makes
			// adding one a deliberate edit rather than a silently satisfied
			// count.
			description: "positive: every command a measured-clean Tier C run earned is gated",
			check: func(t *testing.T, a *Allowlist) {
				for _, c := range earned {
					if !a.IsGated(c) {
						t.Fatalf("GatedCommands = %v, want it to include %q",
							a.GatedCommands, c)
					}
				}
			},
		},
		{
			// The row above asserts what IS gated; this asserts what is
			// deliberately NOT, because GOIP_PARITY_UNGATED_CLEAN only means
			// something while something is ungated. It counts StatusWarn on
			// commands outside gated_commands
			// (internal/goipparity/compare.go:265-266, reported at :334-338),
			// so a list that grew to cover the whole table would make it
			// vacuously
			// true - green because nothing is left to warn about, which is
			// indistinguishable from green because nothing warned. That
			// happened once already, for the interval when nine commands
			// were the whole table, and goip-parity-allowlist.json's
			// _comment records it.
			//
			// `-s link show` used to be the one command held out, and this
			// row asserted its absence. It is gated now, so for one interval
			// the ungated surface was EMPTY and UNGATED_CLEAN vacuously true
			// - the second such interval, the first being when nine commands
			// were the whole table. The `-s` sweep's other five commands
			// ended it; the row below names them.
			//
			// What this row asserts is the direction that has teeth either
			// way: nothing is gated that `earned` does not record evidence
			// for. That is the failure mode once the list covers most of the
			// table - not a command sneaking out of the gate, but a command
			// sneaking INTO it by a one-line JSON edit with no measured run
			// behind it. The row above checks earned ⊆ gated; this checks
			// gated ⊆ earned, and the pair is set equality written so each
			// direction fails with its own message.
			description: "negative: nothing is gated that no measured run earned",
			check: func(t *testing.T, a *Allowlist) {
				isEarned := make(map[string]bool, len(earned))
				for _, c := range earned {
					isEarned[c] = true
				}
				for _, c := range a.GatedCommands {
					if !isEarned[c] {
						t.Fatalf("gated_commands contains %q, which `earned` does "+
							"not list; gate a command only after a Tier C run "+
							"measured it clean, and record the run alongside the "+
							"name", c)
					}
				}
			},
		},
		{
			// The row that makes GOIP_PARITY_UNGATED_CLEAN load-bearing
			// again, by asserting the ungated surface is non-empty AND
			// chosen.
			//
			// The sentinel counts StatusWarn on commands outside
			// gated_commands (internal/goipparity/compare.go:265-266,
			// reported at :334-338), so it is evidence of something only
			// while some command is outside. It has been vacuous twice - once
			// when nine commands were the whole table, once when the
			// twenty-fourth was gated - and both intervals ended the same
			// way, by a new command being compared ungated first.
			//
			// Until G0-T03 this row held out the two `-s` sweep commands
			// whose noise was unresolved. They gated there, with the two
			// netconf-dev rows, which leaves one command ungated: `addr show
			// dynamic`, whose finite valid_lft counts down between ip_a and
			// goip, so it can never gate (addr-write-path.md, "Gating").
			// That is the ungated surface now, and the reason it is out is
			// a property of the command, not of a run, so no clean run can
			// sweep it in. internal/goipparity's TestUngatedSurfaceIsNotVacuous
			// pins the set by name.
			description: "negative: addr show dynamic, whose valid_lft counts down, is deliberately NOT gated, so UNGATED_CLEAN still reads a row",
			check: func(t *testing.T, a *Allowlist) {
				heldOut := []string{
					"addr show dynamic",
				}
				for _, c := range heldOut {
					if a.IsGated(c) {
						t.Fatalf("gated_commands includes %q, which prints a "+
							"finite lifetime that counts down between ip_a and "+
							"goip, so no run can earn it; gating it also empties "+
							"the ungated surface that GOIP_PARITY_UNGATED_CLEAN "+
							"reads", c)
					}
				}
			},
		},
		{
			// A duplicate would be silently harmless - IsGated is a set lookup
			// - and that is exactly why it is worth failing on: the list is
			// read by humans deciding what to gate next, and a name appearing
			// twice makes it look like two decisions were taken.
			description: "negative: no gated command is blank or listed twice",
			check: func(t *testing.T, a *Allowlist) {
				seen := map[string]bool{}
				for _, c := range a.GatedCommands {
					if c == "" {
						t.Fatalf("gated_commands contains an empty name: %v",
							a.GatedCommands)
					}
					if seen[c] {
						t.Fatalf("gated_commands names %q twice", c)
					}
					seen[c] = true
				}
			},
		},
		{
			description: "negative: nothing in the committed file suppresses a presence divergence",
			check: func(t *testing.T, a *Allowlist) {
				for i := range a.Entries {
					e := a.Entries[i]
					if a.Suppresses(e.Command, e.Locus, DivergencePresence) {
						t.Fatalf("entry %q suppresses a presence divergence", e.Locus)
					}
				}
			},
		},
		{
			description: "negative: no committed entry is a volatile-fallback, since each one would be an open bug against D_control",
			check: func(t *testing.T, a *Allowlist) {
				for i := range a.Entries {
					if a.Entries[i].Kind == KindVolatileFallback {
						t.Fatalf("entry %q is a volatile-fallback; each is a bug report, not a resting place",
							a.Entries[i].Locus)
					}
				}
			},
		},
		{
			description: "corner: the file's _comment header survives loading, so a rewrite cannot drop it",
			check: func(t *testing.T, a *Allowlist) {
				if len(a.Comment) == 0 {
					t.Fatalf("_comment is empty; the rationale header is part of the file")
				}
			},
		},
		{
			description: "corner: EntriesFor groups the two de91e928 loci under their two different commands",
			check: func(t *testing.T, a *Allowlist) {
				for _, cmd := range []string{"neigh show", "link show dev"} {
					if got := len(a.EntriesFor(cmd)); got != 1 {
						t.Fatalf("EntriesFor(%q) = %d entries, want 1", cmd, got)
					}
				}
				if got := len(a.EntriesFor("no such command")); got != 0 {
					t.Fatalf("EntriesFor(unknown) = %d, want 0", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) { tc.check(t, a) })
	}
}

// findByLocus searches on locus alone, which Lookup deliberately cannot do:
// the comparator always knows its command, and a locus-only lookup would be a
// way to accidentally suppress across commands.
func findByLocus(a *Allowlist, locus string) (Entry, bool) {
	for i := range a.Entries {
		if a.Entries[i].Locus == locus {
			return a.Entries[i], true
		}
	}
	return Entry{}, false
}
