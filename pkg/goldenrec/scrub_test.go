package goldenrec

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// isDocumentation reports an address Scrub may leave in a fixture.
func isDocumentation(a netip.Addr) bool {
	return a.IsLoopback() || a.IsUnspecified() ||
		netip.MustParsePrefix("192.0.2.0/24").Contains(a) ||
		netip.MustParsePrefix("2001:db8::/32").Contains(a)
}

// addrOf decodes a rec address column the way Normalize does.
func addrOf(b []byte, family uint32) (netip.Addr, bool) {
	c := decodeAddr(reflect.ValueOf(b), uint64(family))
	return c.addr, c.kind == addrV4 || c.kind == addrV6 || c.kind == addrKept
}

func normalizedJSON(t *testing.T, rows []rec) string {
	t.Helper()
	b, err := json.Marshal(mustNormalize(t, rows))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScrub(t *testing.T) {
	const second = int64(1_000_000_000)
	base := rec{Family: afInet, State: 1, Src: v4("10.0.0.1"), Dst: v4("10.0.0.2")}
	with := func(f func(*rec)) rec { r := base; f(&r); return r }
	// tied: rows that tie on every non-identity column, numbered so the
	// first-seen order differs from the raw order (cookie 900 is seen first).
	tied := []rec{
		with(func(r *rec) { r.State = 0; r.Cookie = 900 }),
		with(func(r *rec) { r.Cookie = 100; r.Src = v4("10.0.0.9") }),
		with(func(r *rec) { r.Cookie = 900; r.Src = v4("10.0.0.10") }),
		with(func(r *rec) { r.Cookie = 12; r.Netns = "netns:[4026532001]" }),
		with(func(r *rec) { r.Cookie = 3; r.Netns = "netns:[4026531999]" }),
		with(func(r *rec) { r.Family = afInet6; r.Src = v6("2600::10"); r.Dst = v6("2600::9") }),
	}
	type expected struct {
		err   string
		holds func(in, out []rec, rep Report) error
	}
	cases := []struct {
		name        string
		category    string
		description string
		input       []rec
		expected    expected
	}{
		{"normalize_invariant", "positive", "Normalize reads the same before and after, ties included",
			tied, expected{holds: func(in, out []rec, _ Report) error {
				return nil // checked for every row below
			}}},
		{"addresses_documentation", "positive", "every address becomes documentation, loopback or unspecified",
			slices.Concat(tied, []rec{with(func(r *rec) { r.Src = v4("127.0.0.1"); r.Dst = v4("0.0.0.0") })}),
			expected{holds: func(_, out []rec, _ Report) error {
				for i, r := range out {
					for _, b := range [][]byte{r.Src, r.Dst} {
						if a, ok := addrOf(b, r.Family); !ok || !isDocumentation(a) {
							return fmt.Errorf("row %d: unscrubbed address %v", i, a)
						}
					}
				}
				return nil
			}}},
		{"hostname_placeholder", "positive", "hostname fleet-node-17 becomes golden-host",
			[]rec{with(func(r *rec) { r.Host = "fleet-node-17" })},
			expected{holds: func(_, out []rec, _ Report) error {
				return want(out[0].Host, ScrubHostname)
			}}},
		{"container_ids_fixed", "positive", "two random 64-hex ids become ordinals 1 and 2 in 64 hex digits",
			[]rec{
				with(func(r *rec) { r.ID = strings.Repeat("ab", 32); r.Container = "golden-1" }),
				with(func(r *rec) { r.ID = strings.Repeat("cd", 32); r.Container = "golden-2" }),
			},
			expected{holds: func(_, out []rec, _ Report) error {
				if err := want(out[0].ID, strings.Repeat("0", 63)+"1"); err != nil {
					return err
				}
				return want(out[1].ID, strings.Repeat("0", 63)+"2")
			}}},
		{"docker_netns_fixed", "positive", "random Docker sandbox names become ordinals in 12 hex digits",
			[]rec{
				with(func(r *rec) { r.Netns = "a0988a378664"; r.Container = "golden-1" }),
				with(func(r *rec) { r.Netns = "949bb0b0beab"; r.Container = "golden-2" }),
				with(func(r *rec) { r.Netns = "netns:[4026532001]"; r.State = 2 }),
			},
			expected{holds: func(_, out []rec, _ Report) error {
				for i, w := range []string{"000000000001", "000000000002", "netns:[3]"} {
					if err := want(out[i].Netns, w); err != nil {
						return fmt.Errorf("row %d: %w", i, err)
					}
				}
				return nil
			}}},
		{"more_addresses_than_doc_range", "negative", "300 distinct v4 addresses do not fit 192.0.2.0/24",
			manyV4(base, 300), expected{err: "more than 254 distinct v4 addresses"}},
		{"malformed_address", "negative", "a 3-byte address is left as is and reported",
			[]rec{with(func(r *rec) { r.Src = []byte{1, 2, 3} })},
			expected{holds: func(_, out []rec, rep Report) error {
				if !reflect.DeepEqual(out[0].Src, []byte{1, 2, 3}) {
					return fmt.Errorf("source rewritten to %v", out[0].Src)
				}
				return want(strings.Join(rep.Invalid, ";"), "inet_diag_msg_socket_source row 0: invalid:3")
			}}},
		{"timestamps_rebased", "boundary", "offsets 0, +1 ms and +2 s keep their offsets from 2026-01-01",
			[]rec{
				with(func(r *rec) { r.Ts = 1_700_000_000 * second; r.Date = "2023-11-14" }),
				with(func(r *rec) { r.Ts = 1_700_000_000*second + second/1000; r.Date = "2023-11-14" }),
				with(func(r *rec) { r.Ts = 1_700_000_002 * second; r.Date = "2023-11-14" }),
			},
			expected{holds: func(_, out []rec, _ Report) error {
				for i, off := range []int64{0, second / 1000, 2 * second} {
					if out[i].Ts != scrubEpoch+off {
						return fmt.Errorf("row %d: timestamp %d, expected %d", i, out[i].Ts, scrubEpoch+off)
					}
					if err := want(out[i].Date, "2026-01-01"); err != nil {
						return err
					}
				}
				return nil
			}}},
		{"empty_rows", "boundary", "no rows", []rec{}, expected{holds: func(_, out []rec, _ Report) error {
			if len(out) != 0 {
				return fmt.Errorf("%d rows", len(out))
			}
			return nil
		}}},
		{"single_row", "boundary", "one row's cookie becomes 1",
			[]rec{with(func(r *rec) { r.Cookie = 0xdeadbeef })},
			expected{holds: func(_, out []rec, _ Report) error {
				return want(fmt.Sprint(out[0].Cookie), "1")
			}}},
		{"idempotent", "corner", "scrubbing a scrubbed capture changes nothing",
			tied, expected{holds: func(_, out []rec, _ Report) error {
				again := append([]rec(nil), out...)
				if _, err := Scrub(again, Columns); err != nil {
					return err
				}
				if !reflect.DeepEqual(again, out) {
					return fmt.Errorf("second scrub changed rows:\n%v\n%v", out, again)
				}
				return nil
			}}},
		{"address_already_documentation", "corner", "192.0.2.5 is mapped like any address, not kept to collide",
			[]rec{
				with(func(r *rec) { r.Src = v4("10.0.0.1"); r.Dst = v4("192.0.2.5") }),
				with(func(r *rec) { r.State = 2; r.Src = v4("10.0.0.7"); r.Dst = v4("10.0.0.8") }),
			},
			expected{holds: func(_, out []rec, _ Report) error {
				seen := make(map[netip.Addr]bool)
				for _, r := range out {
					for _, b := range [][]byte{r.Src, r.Dst} {
						a, _ := addrOf(b, r.Family)
						if seen[a] {
							return fmt.Errorf("two addresses mapped to %v", a)
						}
						seen[a] = true
					}
				}
				return nil
			}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := make([]rec, len(tc.input))
			for i, r := range tc.input {
				r.Src, r.Dst = append([]byte(nil), r.Src...), append([]byte(nil), r.Dst...)
				out[i] = r
			}
			rep, err := Scrub(out, Columns)
			if tc.expected.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected.err) {
					t.Fatalf("[%s] %s: err %v, expected one containing %q", tc.category, tc.description, err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if err := tc.expected.holds(tc.input, out, rep); err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			// Every scrub must leave Normalize's reading unchanged.
			if got, want := normalizedJSON(t, out), normalizedJSON(t, tc.input); got != want {
				t.Fatalf("[%s] %s: Normalize changed:\n got %s\nwant %s", tc.category, tc.description, got, want)
			}
		})
	}
}

func want(got, expected string) error {
	if got != expected {
		return fmt.Errorf("got %q, expected %q", got, expected)
	}
	return nil
}

func TestCheckScrubbed(t *testing.T) {
	base := rec{Family: afInet, State: 1, Src: v4("10.0.0.1"), Dst: v4("10.0.0.2"), Host: "node-17"}
	scrubbed := []rec{base}
	if _, err := Scrub(scrubbed, Columns); err != nil {
		t.Fatal(err)
	}
	with := func(f func(*rec)) []rec { r := scrubbed[0]; f(&r); return []rec{r} }
	cases := []struct {
		name        string
		category    string
		description string
		input       []rec
		expected    string // the findings joined by ";"
	}{
		{"scrubbed", "positive", "Scrub's output has nothing to report", scrubbed, ""},
		{"real_address", "negative", "a Docker bridge address injected after the scrub",
			with(func(r *rec) { r.Src = v4("172.17.0.2") }),
			"inet_diag_msg_socket_source row 0: unscrubbed address 172.17.0.2"},
		{"real_hostname", "negative", "a real hostname", with(func(r *rec) { r.Host = "node-17" }),
			`hostname row 0: unscrubbed "node-17"`},
		{"empty_rows", "boundary", "no rows", []rec{}, ""},
		{"kept_addresses", "corner", "loopback and unspecified addresses are allowed as they are",
			with(func(r *rec) { r.Src = v4("127.0.0.1"); r.Dst = v4("0.0.0.0") }), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad, err := CheckScrubbed(tc.input, Columns)
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if got := strings.Join(bad, ";"); got != tc.expected {
				t.Fatalf("[%s] %s: %q, expected %q", tc.category, tc.description, got, tc.expected)
			}
		})
	}
}
