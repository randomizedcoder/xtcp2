package goldenrec

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

// rec is a small record with one column of each class, named like
// ParquetRow's, so Columns classifies it.
type rec struct {
	Family    uint32 `parquet:"inet_diag_msg_family"`
	State     uint32 `parquet:"inet_diag_msg_state"`
	Mss       uint32 `parquet:"tcp_info_snd_mss"`
	Rtt       uint32 `parquet:"tcp_info_rtt"`
	Retrans   uint32 `parquet:"tcp_info_total_retrans"`
	Bbr       uint32 `parquet:"bbr_info_bw_lo"`
	Dsack     uint32 `parquet:"tcp_info_dsack_dups"`
	RwndLim   uint64 `parquet:"tcp_info_rwnd_limited"`
	RcvBytes  uint64 `parquet:"tcp_info_bytes_received"`
	Cookie    uint64 `parquet:"inet_diag_msg_socket_cookie"`
	SPort     uint32 `parquet:"inet_diag_msg_socket_source_port"`
	DPort     uint32 `parquet:"inet_diag_msg_socket_destination_port"`
	Src       []byte `parquet:"inet_diag_msg_socket_source,zstd"`
	Dst       []byte `parquet:"inet_diag_msg_socket_destination,zstd"`
	Ts        int64  `parquet:"timestamp_ns"`
	Date      string `parquet:"event_date"`
	Counter   uint64 `parquet:"record_counter"`
	Netns     string `parquet:"netns"`
	Egress    string `parquet:"enrich_socket_dest_egress_ifname"`
	Host      string `parquet:"hostname"`
	Container string `parquet:"container_name"`
	ID        string `parquet:"container_id"`
	Note      string // untagged: never a column
}

// v4 is a 16-byte address column holding a v4 address, as xtcp2 writes it.
func v4(s string) []byte {
	b := make([]byte, 16)
	a := netip.MustParseAddr(s).As4()
	copy(b, a[:])
	return b
}

func v6(s string) []byte {
	a := netip.MustParseAddr(s).As16()
	return a[:]
}

const absent = "<absent>"

func TestNormalize(t *testing.T) {
	type unknown struct {
		X uint32 `parquet:"not_a_column"`
	}
	base := rec{Family: afInet, State: 1, Mss: 1448, Rtt: 10, Src: v4("127.0.0.1"), Dst: v4("127.0.0.1")}
	with := func(f func(*rec)) rec { r := base; f(&r); return r }
	cases := []struct {
		name        string
		category    string
		description string
		input       any
		column      string   // the column checked, per output row
		expected    []string // its value per sorted row; absent = no key
		wantErr     string
	}{
		{"strict_kept", "positive", "tcp_info_snd_mss 1448 is kept exactly",
			[]rec{base}, "tcp_info_snd_mss", []string{"1448"}, ""},
		{"presence_nonzero", "positive", "RTTs 1234 and 5678 both read nonzero",
			[]rec{with(func(r *rec) { r.Rtt = 1234 }), with(func(r *rec) { r.Rtt = 5678; r.State = 2 })},
			"tcp_info_rtt", []string{"nonzero", "nonzero"}, ""},
		{"ordinal_numbering", "positive", "cookies 900, 100, 900 in sorted rows are #1, #2, #1",
			[]rec{with(func(r *rec) { r.Cookie = 900 }), with(func(r *rec) { r.Cookie = 100; r.State = 2 }),
				with(func(r *rec) { r.Cookie = 900; r.State = 3 })},
			"inet_diag_msg_socket_cookie", []string{"#1", "#2", "#1"}, ""},
		{"ephemeral_port", "positive", "source port 45123 is eph",
			[]rec{with(func(r *rec) { r.SPort = 45123 })}, "inet_diag_msg_socket_source_port", []string{"eph"}, ""},
		{"address_to_doc", "positive", "10.1.2.3 in two rows maps to 192.0.2.1 both times",
			[]rec{with(func(r *rec) { r.Src = v4("10.1.2.3") }), with(func(r *rec) { r.Src = v4("10.1.2.3"); r.State = 2 })},
			"inet_diag_msg_socket_source", []string{"192.0.2.1", "192.0.2.1"}, ""},
		{"volatile_dropped", "positive", "timestamp_ns is not in the output",
			[]rec{with(func(r *rec) { r.Ts = 1; r.Counter = 7 })}, "timestamp_ns", []string{absent}, ""},
		{"unknown_column_input", "negative", "a column no class lists is an error, not a silent drop",
			[]unknown{{1}}, "", nil, "unclassified column not_a_column"},
		{"not_a_slice", "negative", "rows that are not a slice of structs",
			rec{}, "", nil, "must be a slice of structs"},
		{"nil_address", "negative", "a nil address reads as empty",
			[]rec{with(func(r *rec) { r.Src = nil })}, "inet_diag_msg_socket_source", []string{""}, ""},
		{"short_address", "negative", "a 3-byte address reads invalid:3",
			[]rec{with(func(r *rec) { r.Src = []byte{1, 2, 3} })}, "inet_diag_msg_socket_source", []string{"invalid:3"}, ""},
		{"too_many_v4", "negative", "255 distinct v4 addresses overflow 192.0.2.0/24",
			manyV4(base, maxDocV4+1), "", nil, "more than 254 distinct v4 addresses"},
		{"presence_zero", "boundary", "a zero counter reads 0",
			[]rec{base}, "tcp_info_total_retrans", []string{"0"}, ""},
		{"port_split", "boundary", "32767 is kept, 32768 is eph",
			[]rec{with(func(r *rec) { r.SPort = 32767 }), with(func(r *rec) { r.SPort = 32768 })},
			"inet_diag_msg_socket_source_port", []string{"32767", "eph"}, ""},
		{"empty_rows", "boundary", "no rows", []rec{}, "tcp_info_snd_mss", []string{}, ""},
		{"max_v4", "boundary", "exactly 254 distinct v4 addresses fit",
			manyV4(base, maxDocV4), "", nil, ""},
		{"loopback_kept", "corner", "127.0.0.1 and ::1 are kept (family 10 sorts before 2)",
			[]rec{base, with(func(r *rec) { r.Family = afInet6; r.Src = v6("::1") })},
			"inet_diag_msg_socket_source", []string{"::1", "127.0.0.1"}, ""},
		{"v4_in_16_bytes", "corner", "with no family, 16 bytes with only the first 4 set read as v4",
			[]rec{with(func(r *rec) { r.Family = 0; r.Src = v4("10.9.9.9") })},
			"inet_diag_msg_socket_source", []string{"192.0.2.1"}, ""},
		{"anonymous_netns", "corner", "netns:[4026532] becomes an ordinal",
			[]rec{with(func(r *rec) { r.Netns = "netns:[4026532]" })}, "netns", []string{"netns:anon:#1"}, ""},
		{"docker_netns", "positive", "a Docker sandbox name, 12 hex digits, becomes an ordinal",
			[]rec{with(func(r *rec) { r.Netns = "a0988a378664" })}, "netns", []string{"netns:docker:#1"}, ""},
		{"eleven_hex_netns", "boundary", "11 hex digits are a stable name, kept",
			[]rec{with(func(r *rec) { r.Netns = "a0988a37866" })}, "netns", []string{"a0988a37866"}, ""},
		{"twelve_non_hex_netns", "corner", "12 characters that are not all lower-case hex are kept",
			[]rec{with(func(r *rec) { r.Netns = "A0988A378664" })}, "netns", []string{"A0988A378664"}, ""},
		{"named_netns", "corner", "a container or named netns is kept",
			[]rec{with(func(r *rec) { r.Netns = "golden-1" })}, "netns", []string{"golden-1"}, ""},
		{"host_placeholder", "corner", "a hostname reads <host>",
			[]rec{with(func(r *rec) { r.Host = "node-17" })}, "hostname", []string{"<host>"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.input, Columns)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("[%s] %s: err %v, expected one containing %q", tc.category, tc.description, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if tc.column == "" {
				return
			}
			vals := make([]string, 0, len(got))
			for _, r := range got {
				v, ok := r[tc.column]
				if !ok {
					v = absent
				}
				vals = append(vals, v)
			}
			if !reflect.DeepEqual(vals, tc.expected) {
				t.Fatalf("[%s] %s: %s = %q, expected %q", tc.category, tc.description, tc.column, vals, tc.expected)
			}
		})
	}
}

// manyV4 is n rows, each with its own non-documentation v4 source.
func manyV4(base rec, n int) []rec {
	rows := make([]rec, n)
	for i := range rows {
		rows[i] = base
		rows[i].Src = v4(netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}).String())
	}
	return rows
}

// TestNormalizeOrderIndependent is the shuffled-input corner row of the
// table above, kept separate because it compares two whole outputs.
func TestNormalizeOrderIndependent(t *testing.T) {
	rows := []rec{
		{Family: afInet, State: 1, Cookie: 5, Src: v4("10.0.0.1"), Netns: "netns:[9]"},
		{Family: afInet, State: 10, Cookie: 7, Src: v4("10.0.0.2"), Netns: "netns:[8]"},
		{Family: afInet6, State: 1, Cookie: 6, Src: v6("2600::1"), Netns: "golden-1"},
		{Family: afInet, State: 1, Cookie: 9, Src: v4("10.0.0.1"), Netns: "netns:[9]"},
	}
	cases := []struct {
		name, category, description string
		order                       []int
		expected                    string // "same" as the identity order
	}{
		{"identity", "positive", "rows in their given order", []int{0, 1, 2, 3}, "same"},
		{"reversed", "corner", "the same rows reversed", []int{3, 2, 1, 0}, "same"},
		{"tied_rows_swapped", "corner", "the two rows that tie on every non-identity column swapped", []int{3, 1, 2, 0}, "same"},
		{"single_row", "boundary", "one row against itself", []int{2}, "single"},
	}
	want, err := Normalize(rows, Columns)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(want)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := make([]rec, 0, len(tc.order))
			for _, i := range tc.order {
				in = append(in, rows[i])
			}
			got, err := Normalize(in, Columns)
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := json.Marshal(got)
			if tc.expected == "same" && string(gotJSON) != string(wantJSON) {
				t.Fatalf("[%s] %s:\n got %s\nwant %s", tc.category, tc.description, gotJSON, wantJSON)
			}
			if tc.expected == "single" && len(got) != 1 {
				t.Fatalf("[%s] %s: %d rows", tc.category, tc.description, len(got))
			}
		})
	}
}

// TestNormalizeV4Mapped pins how a dual-stack socket's v4-mapped address
// (::ffff:a.b.c.d, family AF_INET6) normalizes: into the same documentation
// number as the plain IPv4 address, kept in its mapped form, so an IPv4
// peer reads the same whichever family's socket reached it. Before,
// v4_mapped_doc and v4_mapped_zero failed: the mapped form was scrubbed
// into 2001:db8::/32 like any IPv6 address.
func TestNormalizeV4Mapped(t *testing.T) {
	base := rec{Family: afInet, State: 1, Mss: 1448, Rtt: 10, Src: v4("127.0.0.1"), Dst: v4("127.0.0.1")}
	with := func(f func(*rec)) rec { r := base; f(&r); return r }
	mapped := func(r *rec, s string) { r.Family = afInet6; r.Src = v6(s) }
	tests := []struct {
		name        string
		category    string
		description string
		input       []rec
		expected    []string // inet_diag_msg_socket_source per sorted row
	}{
		{
			name: "v4_mapped_doc", category: "positive",
			description: "::ffff:10.1.2.3 and 10.1.2.3 share one documentation number",
			input: []rec{
				with(func(r *rec) { r.Src = v4("10.1.2.3") }),
				with(func(r *rec) { mapped(r, "::ffff:10.1.2.3") }),
			},
			expected: []string{"::ffff:192.0.2.1", "192.0.2.1"},
		},
		{
			name: "v4_mapped_short", category: "negative",
			description: "a 15-byte address is malformed, not mapped",
			input:       []rec{with(func(r *rec) { r.Family = afInet6; r.Src = v6("::ffff:10.1.2.3")[:15] })},
			expected:    []string{"invalid:15"},
		},
		{
			name: "v4_mapped_zero", category: "boundary",
			description: "::ffff:0.0.0.0 is the unspecified IPv4 address, kept",
			input:       []rec{with(func(r *rec) { mapped(r, "::ffff:0.0.0.0") })},
			expected:    []string{"::ffff:0.0.0.0"},
		},
		{
			name: "v4_mapped_loopback", category: "corner",
			description: "::ffff:127.0.0.1 is loopback, kept",
			input:       []rec{with(func(r *rec) { mapped(r, "::ffff:127.0.0.1") })},
			expected:    []string{"::ffff:127.0.0.1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.input, Columns)
			if err != nil {
				t.Fatalf("%s (%s): %s: %v", tc.name, tc.category, tc.description, err)
			}
			vals := make([]string, 0, len(got))
			for _, r := range got {
				vals = append(vals, r["inet_diag_msg_socket_source"])
			}
			if !reflect.DeepEqual(vals, tc.expected) {
				t.Errorf("%s (%s): %s\n got      %q\n expected %q", tc.name, tc.category, tc.description, vals, tc.expected)
			}
		})
	}
}
