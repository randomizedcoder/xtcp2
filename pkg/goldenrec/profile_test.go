package goldenrec

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// profileInput is the rows of one capture.
type profileInput struct {
	rows []rec
}

// columnExpect is what TestProfile checks of one column; nil Values means
// none recorded.
type columnExpect struct {
	column string
	want   ColumnProfile
}

// go test ./pkg/goldenrec/ -run TestProfile
func TestProfile(t *testing.T) {
	base := rec{Family: afInet, State: 1, Mss: 1448, Src: v4("127.0.0.1"), Dst: v4("127.0.0.1")}
	with := func(f func(*rec)) rec { r := base; f(&r); return r }
	many := make([]rec, 100)
	for i := range many {
		many[i] = with(func(r *rec) { r.Mss = uint32(1000 + i) })
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       profileInput
		expected    []columnExpect
		rows        int
	}{
		{"counts", "positive", "per-column stats",
			profileInput{[]rec{base, with(func(r *rec) { r.Rtt = 1 }), with(func(r *rec) { r.Rtt = 1 })}},
			[]columnExpect{{"tcp_info_rtt", ColumnProfile{Class: "presence", Nonzero: 2, Distinct: "2"}}}, 3},
		{"enum_values", "positive", "a numeric Strict column's value set, in numeric order",
			profileInput{[]rec{base, with(func(r *rec) { r.State = 10 }), base}},
			[]columnExpect{{"inet_diag_msg_state", ColumnProfile{Class: "strict", Nonzero: 3, Distinct: "2", Values: []string{"1", "10"}}}}, 3},
		{"volatile_skipped", "negative", "volatile columns carry no values",
			profileInput{[]rec{with(func(r *rec) { r.Ts = 5 }), with(func(r *rec) { r.Ts = 6 })}},
			[]columnExpect{{"timestamp_ns", ColumnProfile{Class: "volatile", Nonzero: 2}}}, 2},
		{"tenant_strings_hidden", "negative", "a container name is counted, never recorded",
			profileInput{[]rec{with(func(r *rec) { r.Container = "customer-db" })}},
			[]columnExpect{{"container_name", ColumnProfile{Class: "strict", Nonzero: 1, Distinct: "1"}}}, 1},
		{"empty", "boundary", "zero rows",
			profileInput{nil},
			[]columnExpect{{"inet_diag_msg_state", ColumnProfile{Class: "strict", Distinct: "0"}}}, 0},
		{"distinct_at_cap", "boundary", "exactly 64 distinct values are counted exactly, without a value set",
			profileInput{many[:64]},
			[]columnExpect{{"tcp_info_snd_mss", ColumnProfile{Class: "strict", Nonzero: 64, Distinct: "64"}}}, 64},
		{"distinct_cap", "corner", "a high-cardinality Strict column",
			profileInput{many},
			[]columnExpect{{"tcp_info_snd_mss", ColumnProfile{Class: "strict", Nonzero: 100, Distinct: "64+"}}}, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ProfileOf(tc.input.rows, Columns)
			if err != nil {
				t.Fatalf("%s (%s): %s: %v", tc.name, tc.category, tc.description, err)
			}
			if p.Rows != tc.rows {
				t.Errorf("%s (%s): %s\nrows: expected %d, actual %d", tc.name, tc.category, tc.description, tc.rows, p.Rows)
			}
			for _, e := range tc.expected {
				if got := p.Columns[e.column]; !reflect.DeepEqual(got, e.want) {
					t.Errorf("%s (%s): %s\ninput %d rows, column %s\nexpected %+v\nactual   %+v",
						tc.name, tc.category, tc.description, len(tc.input.rows), e.column, e.want, got)
				}
			}
		})
	}
}

// go test ./pkg/goldenrec/ -run TestProfileKinds
func TestProfileKinds(t *testing.T) {
	r := func(family uint32, container, netns string) rec {
		return rec{Family: family, State: 1, Container: container, Netns: netns}
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       []rec
		expected    [2]map[string]int // families, netns kinds
	}{
		{"mixed", "positive", "each namespace kind and family is counted",
			[]rec{r(afInet, "golden-1", "a0988a378664"), r(afInet6, "", "default"), r(afInet, "", "golden-ns"),
				r(afInet, "", "netns:[4026532]"), r(afInet, "", "949bb0b0beab")},
			[2]map[string]int{{"2": 4, "10": 1}, {"container": 1, "host": 1, "named": 1, "anon": 1, "docker": 1}}},
		{"no_family", "negative", "a row without a family counts under 0",
			[]rec{r(0, "", "default")}, [2]map[string]int{{"0": 1}, {"host": 1}}},
		{"empty", "boundary", "no rows", nil, [2]map[string]int{{}, {}}},
		{"empty_netns", "corner", "a row with no netns name is unknown, not host",
			[]rec{r(afInet, "", "")}, [2]map[string]int{{"2": 1}, {"unknown": 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ProfileOf(tc.input, Columns)
			if err != nil {
				t.Fatal(err)
			}
			got := [2]map[string]int{p.Families, p.NetnsKinds}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("%s (%s): %s\ninput %+v\nexpected %v\nactual   %v", tc.name, tc.category, tc.description, tc.input, tc.expected, got)
			}
		})
	}
}

// go test ./pkg/goldenrec/ -run TestDiffProfiles
func TestDiffProfiles(t *testing.T) {
	prof := func(rows int, cols map[string]ColumnProfile, fams, kinds map[string]int) Profile {
		return Profile{Rows: rows, Columns: cols, Families: fams, NetnsKinds: kinds}
	}
	st := func(nonzero int, distinct string, values ...string) ColumnProfile {
		return ColumnProfile{Class: "strict", Nonzero: nonzero, Distinct: distinct, Values: values}
	}
	fam := map[string]int{"2": 10, "10": 5}
	kinds := map[string]int{"host": 3, "container": 12}
	a := prof(48, map[string]ColumnProfile{
		"inet_diag_msg_state":    st(48, "2", "1", "10"),
		"enrich_socket_dest_asn": st(0, "1", "0"),
		"tcp_info_snd_mss":       st(48, "64+"),
	}, fam, kinds)
	b := prof(900, map[string]ColumnProfile{
		"inet_diag_msg_state":    st(900, "2", "1", "10"),
		"enrich_socket_dest_asn": st(0, "1", "0"),
		"tcp_info_snd_mss":       st(900, "12"),
	}, map[string]int{"2": 700, "10": 200}, map[string]int{"host": 100, "container": 800})
	edit := func(p Profile, f func(*Profile)) Profile {
		q := p
		q.Columns = make(map[string]ColumnProfile, len(p.Columns))
		for k, v := range p.Columns {
			q.Columns[k] = v
		}
		q.Families = map[string]int{}
		for k, v := range p.Families {
			q.Families[k] = v
		}
		f(&q)
		return q
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       [2]Profile
		expected    []string
	}{
		{"same_shape", "positive", "different row counts, same shape", [2]Profile{a, b}, nil},
		{"populated_only", "positive", "a column only one side fills",
			[2]Profile{a, edit(b, func(p *Profile) { p.Columns["enrich_socket_dest_asn"] = st(40, "3", "0", "13335", "15169") })},
			[]string{"populated-only-in B: enrich_socket_dest_asn", "value-only-in B: enrich_socket_dest_asn=13335",
				"value-only-in B: enrich_socket_dest_asn=15169"}},
		{"value_only", "negative", "an enum value missing on one side",
			[2]Profile{a, edit(b, func(p *Profile) { p.Columns["inet_diag_msg_state"] = st(900, "1", "1") })},
			[]string{"value-only-in A: inet_diag_msg_state=10"}},
		{"column_only", "negative", "a column one capture lacks (another schema)",
			[2]Profile{a, edit(b, func(p *Profile) { delete(p.Columns, "tcp_info_snd_mss") })},
			[]string{"column-only-in A: tcp_info_snd_mss"}},
		{"family_missing", "boundary", "a family absent on one side",
			[2]Profile{a, edit(b, func(p *Profile) { delete(p.Families, "10") })},
			[]string{"family-missing B: 10"}},
		{"capped_distinct", "corner", "64+ against an exact count is no finding", [2]Profile{a, b}, nil},
		{"empty_side", "corner", "an empty capture is one finding, not one per column",
			[2]Profile{a, prof(0, nil, map[string]int{}, map[string]int{})}, []string{"no-rows B"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, f := range DiffProfiles(tc.input[0], tc.input[1]) {
				got = append(got, f.String())
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s (%s): %s\nexpected %q\nactual   %q", tc.name, tc.category, tc.description, tc.expected, got)
			}
		})
	}
}

// TestProfileClassesComplete: every column of a profiled row gets its class
// name, so a profile names no class the table lacks.
func TestProfileClassesComplete(t *testing.T) {
	p, err := ProfileOf([]rec{{Family: afInet}}, Columns)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range p.Columns {
		if strings.HasPrefix(c.Class, "class(") {
			t.Errorf("column %s: class %s", name, c.Class)
		}
	}
	if n := len(columnsOf(reflect.TypeFor[rec]())); len(p.Columns) != n {
		t.Errorf("profile has %d columns, rec has %d", len(p.Columns), n)
	}
}
