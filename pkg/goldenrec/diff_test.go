package goldenrec

import (
	"strings"
	"testing"
)

// goldenRecs is a small capture: one connection, one listener and one
// accepted connection, in three containers.
func goldenRecs() []rec {
	return []rec{
		{Family: afInet, State: 1, Mss: 1448, Rtt: 10, Bbr: 5, Cookie: 11, SPort: 40000, DPort: 443,
			Src: v4("10.0.0.1"), Dst: v4("10.0.0.2"), Ts: 100, Container: "golden-1"},
		{Family: afInet, State: 10, Mss: 536, Cookie: 12, SPort: 80,
			Src: v4("10.0.0.3"), Ts: 100, Container: "golden-2"},
		{Family: afInet, State: 1, Mss: 1448, Rtt: 20, Bbr: 7, Cookie: 13, SPort: 22, DPort: 50000,
			Src: v4("10.0.0.4"), Dst: v4("10.0.0.5"), Ts: 100, Container: "golden-3"},
	}
}

// edit returns a copy of rows with f applied to row i.
func edit(rows []rec, i int, f func(*rec)) []rec {
	out := append([]rec(nil), rows...)
	f(&out[i])
	return out
}

// inNetns returns a copy of rows with row 0 moved to netns and f, when
// set, applied to it.
func inNetns(rows []rec, netns string, f func(*rec)) []rec {
	return edit(rows, 0, func(r *rec) {
		r.Netns = netns
		if f != nil {
			f(r)
		}
	})
}

func mustNormalize(t *testing.T, rows []rec) []Row {
	t.Helper()
	n, err := Normalize(rows, Columns)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDiff(t *testing.T) {
	g := goldenRecs()
	noisy := make([]rec, len(g))
	for i, r := range g {
		r.Rtt *= 7
		r.Cookie += 1000
		r.Ts += 12345
		r.Counter = 99
		if r.SPort >= ephemeralPortMin {
			r.SPort++
		}
		if r.DPort >= ephemeralPortMin {
			r.DPort += 3
		}
		r.Src = v4("10.9.9." + string(rune('1'+i)))
		noisy[i] = r
	}
	listener := g[1]

	type expected struct {
		n        int    // differences
		contains string // in Format(diffs, 50), when set
		err      string
	}
	cases := []struct {
		name        string
		category    string
		description string
		golden      []rec
		candidate   []rec
		opts        Options
		expected    expected
	}{
		{"identical", "positive", "golden against itself", g, g, Options{}, expected{}},
		{"noise_only", "positive", "RTTs, ephemeral ports, cookies, addresses and timestamps changed",
			g, noisy, Options{}, expected{}},
		{"docker_netns_renamed", "positive", "a container's random Docker sandbox name differs between runs",
			edit(g, 0, func(r *rec) { r.Netns = "a0988a378664" }), edit(g, 0, func(r *rec) { r.Netns = "949bb0b0beab" }),
			Options{}, expected{}},
		{"docker_netns_to_named", "negative", "a socket moves from a Docker sandbox to a named netns",
			edit(g, 0, func(r *rec) { r.Netns = "a0988a378664" }), edit(g, 0, func(r *rec) { r.Netns = "golden-ns" }),
			Options{}, expected{n: 1, contains: `netns: golden "netns:docker", candidate "golden-ns"`}},
		{"columns_filter", "positive", "a non-container change is ignored under ^container_",
			g, edit(g, 0, func(r *rec) { r.Mss = 0 }), Options{Columns: "^container_"}, expected{}},
		{"missing_attribute", "negative", "tcp_info_snd_mss zeroed in one row",
			g, edit(g, 0, func(r *rec) { r.Mss = 0 }), Options{},
			expected{n: 1, contains: `inet_diag_msg_socket_destination_port=443: tcp_info_snd_mss: golden "1448", candidate "0"`}},
		{"missing_row", "negative", "the candidate has one socket fewer",
			g, g[:2], Options{}, expected{n: 1, contains: "row only in golden: "}},
		{"presence_flip", "negative", "bbr_info_bw_lo goes from nonzero to 0",
			g, edit(g, 2, func(r *rec) { r.Bbr = 0 }), Options{},
			expected{n: 1, contains: `bbr_info_bw_lo: golden "nonzero", candidate "0"`}},
		{"container_name_changed", "negative", "golden-2 renamed x reads as one container_name difference",
			g, edit(g, 1, func(r *rec) { r.Container = "x" }), Options{Columns: "^container_"},
			expected{n: 1, contains: `container_name: golden "golden-2", candidate "x"`}},
		{"bad_filter", "negative", "a columns filter that does not compile",
			g, g, Options{Columns: "("}, expected{err: "columns filter"}},
		{"both_empty", "boundary", "no rows on either side", nil, nil, Options{}, expected{}},
		{"one_empty", "boundary", "every golden row is reported when the candidate is empty",
			g, nil, Options{}, expected{n: 3, contains: "row only in golden"}},
		{"duplicate_keys", "corner", "two identical listeners against three match as a multiset",
			[]rec{listener, listener}, []rec{listener, listener, listener}, Options{},
			expected{n: 1, contains: "row only in candidate"}},
		{"extra_row", "corner", "the candidate has one socket more",
			g, append(append([]rec(nil), g...), rec{Family: afInet6, State: 10, SPort: 443, Src: v6("::1")}), Options{},
			expected{n: 1, contains: "row only in candidate: inet_diag_msg_family=10"}},
		{"lossy_counters_ignored", "positive", "a netem pair's counters that vary run to run are not compared",
			inNetns(g, "golden-bulk-a", func(r *rec) { r.Dsack = 3 }), inNetns(g, "golden-bulk-a", nil),
			Options{}, expected{}},
		{"lossy_presence_kept", "negative", "a netem pair's receive bytes going to zero is still a difference",
			inNetns(g, "golden-bulk-a", func(r *rec) { r.RcvBytes = 4194304 }), inNetns(g, "golden-bulk-a", nil),
			Options{}, expected{n: 1, contains: `tcp_info_bytes_received: golden "nonzero", candidate "0"`}},
		{"lossy_retrans_kept", "negative", "a netem pair that stops retransmitting is still a difference",
			inNetns(g, "golden-bulk-a", func(r *rec) { r.Retrans = 3 }), inNetns(g, "golden-bulk-a", nil),
			Options{}, expected{n: 1, contains: `tcp_info_total_retrans: golden "nonzero", candidate "0"`}},
		{"lossy_strict_kept", "negative", "a netem pair's MSS is still compared",
			inNetns(g, "golden-bulk-a", nil), inNetns(g, "golden-bulk-a", func(r *rec) { r.Mss = 1398 }),
			Options{}, expected{n: 1, contains: `tcp_info_snd_mss: golden "1448", candidate "1398"`}},
		{"lossy_elsewhere", "negative", "the same counter flip outside a netem pair is a difference",
			inNetns(g, "golden-cc-a", func(r *rec) { r.Retrans = 3 }), inNetns(g, "golden-cc-a", nil),
			Options{}, expected{n: 1, contains: `tcp_info_total_retrans: golden "nonzero", candidate "0"`}},
		{"lossy_disabled", "boundary", "an empty LossyNetns compares every namespace",
			inNetns(g, "golden-bulk-a", func(r *rec) { r.Retrans = 3 }), inNetns(g, "golden-bulk-a", nil),
			Options{LossyNetns: []string{}}, expected{n: 1, contains: "tcp_info_total_retrans"}},
		{"lossy_custom", "corner", "a named LossyNetns replaces the default",
			inNetns(g, "golden-bulk-a", func(r *rec) { r.Retrans = 3 }), inNetns(g, "golden-bulk-a", nil),
			Options{LossyNetns: []string{"other"}}, expected{n: 1, contains: "tcp_info_total_retrans"}},
		{"lossy_row_missing", "corner", "a socket missing from a netem pair is still reported",
			inNetns(g, "golden-bulk-a", nil), inNetns(g, "golden-bulk-a", nil)[:2],
			Options{}, expected{n: 1, contains: "row only in golden"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diffs, err := Diff(mustNormalize(t, tc.golden), mustNormalize(t, tc.candidate), Columns, tc.opts)
			if tc.expected.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected.err) {
					t.Fatalf("[%s] %s: err %v, expected one containing %q", tc.category, tc.description, err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			out := Format(diffs, 50)
			if len(diffs) != tc.expected.n {
				t.Fatalf("[%s] %s: %d differences, expected %d:\n%s", tc.category, tc.description, len(diffs), tc.expected.n, out)
			}
			if !strings.Contains(out, tc.expected.contains) {
				t.Fatalf("[%s] %s: output lacks %q:\n%s", tc.category, tc.description, tc.expected.contains, out)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	many := func(n int) []Difference {
		d := make([]Difference, n)
		for i := range d {
			d[i] = Difference{Key: "k", OnlyIn: "golden"}
		}
		return d
	}
	cases := []struct {
		name, category, description string
		diffs                       []Difference
		limit                       int
		expectedLines               int
		expectedLast                string
	}{
		{"under_cap", "positive", "3 differences are all printed", many(3), 50, 3, "row only in golden: k"},
		{"output_cap", "boundary", "500 differences print 50 lines then a count", many(500), 50, 51, "and 450 more"},
		{"exactly_cap", "boundary", "50 differences print 50 lines and no count", many(50), 50, 50, "row only in golden: k"},
		{"none", "negative", "no differences print nothing", nil, 50, 0, ""},
		{"zero_limit", "corner", "a limit of 0 prints only the count", many(2), 0, 1, "and 2 more"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Format(tc.diffs, tc.limit)
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			if out == "" {
				lines = nil
			}
			if len(lines) != tc.expectedLines {
				t.Fatalf("[%s] %s: %d lines, expected %d", tc.category, tc.description, len(lines), tc.expectedLines)
			}
			if tc.expectedLines > 0 && lines[len(lines)-1] != tc.expectedLast {
				t.Fatalf("[%s] %s: last line %q, expected %q", tc.category, tc.description, lines[len(lines)-1], tc.expectedLast)
			}
		})
	}
}

func TestLossyColumns(t *testing.T) {
	idx, err := Columns.index()
	if err != nil {
		t.Fatal(err)
	}
	for c := range lossyColumns {
		if _, ok := idx[c]; !ok {
			t.Errorf("lossyColumns names %q, which is not a column", c)
		}
	}
	cases := []struct {
		name, category, description, input string
		expected                           bool
	}{
		{"dsack_varies", "positive", "a counter netem sets at random is exempt", "tcp_info_dsack_dups", true},
		{"reordering_varies", "positive", "the reordering a transfer settles on is exempt", reorderingColumn, true},
		{"bytes_received_kept", "negative", "the transfer's size is fixed, so it is compared", "tcp_info_bytes_received", false},
		{"total_retrans_kept", "negative", "2 % loss over 4 MiB always retransmits, so it is compared", "tcp_info_total_retrans", false},
		{"ca_state_kept", "boundary", "an idle connection is back in Open, so ca_state is compared", caStateColumn, false},
		{"dctcp_alpha_varies", "corner", "a per-algorithm info column follows the marks netem drops", "dctcp_info_alpha", true},
		{"unknown_kept", "corner", "a name that is no column is not exempt", "nope", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lossyColumns[tc.input]; got != tc.expected {
				t.Errorf("[%s] %s: lossyColumns[%q] = %v, expected %v", tc.category, tc.description, tc.input, got, tc.expected)
			}
		})
	}
}
