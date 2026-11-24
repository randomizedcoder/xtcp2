package goldenrec

import (
	"reflect"
	"strings"
	"testing"
)

// nsrec is a record with the nsid pair, named like ParquetRow's columns.
type nsrec struct {
	Family    uint32 `parquet:"inet_diag_msg_family"`
	State     uint32 `parquet:"inet_diag_msg_state"`
	Container string `parquet:"container_name"`
	Nsid      uint32 `parquet:"nsid"`
	Assigned  bool   `parquet:"nsid_assigned"`
}

// go test ./pkg/goldenrec/ -run TestNormalizeNsid
func TestNormalizeNsid(t *testing.T) {
	c := func(name string, state, nsid uint32, assigned bool) nsrec {
		return nsrec{Family: afInet, State: state, Container: name, Nsid: nsid, Assigned: assigned}
	}
	tests := []struct {
		name        string
		category    string
		description string
		input       [][]nsrec // one or more captures of the same workload
		expected    []string  // nsid per sorted row, the same for every capture
	}{
		{"assigned_zero_numbered", "positive", "an assigned 0 is an identity",
			[][]nsrec{{c("c1", 1, 0, true), c("c2", 1, 1, true)}}, []string{"#1", "#2"}},
		{"swapped_ids_same", "positive", "run-to-run id order doesn't matter",
			[][]nsrec{
				{c("c1", 1, 0, true), c("c2", 1, 1, true)},
				{c("c1", 1, 1, true), c("c2", 1, 0, true)},
			}, []string{"#1", "#2"}},
		{"same_ns_same_number", "positive", "two sockets in one namespace share its number",
			[][]nsrec{{c("c1", 1, 3, true), c("c1", 2, 3, true)}}, []string{"#1", "#1"}},
		{"unassigned_dash", "negative", "an unassigned nsid is not numbered",
			[][]nsrec{{c("c1", 1, 0, false)}}, []string{"-"}},
		{"all_unassigned", "boundary", "no namespace has an id",
			[][]nsrec{{c("c1", 1, 0, false), c("c2", 1, 0, false)}}, []string{"-", "-"}},
		{"stray_value", "corner", "a nonzero id with presence false (a writer bug) is ignored, not numbered",
			[][]nsrec{{c("c1", 1, 5, false)}}, []string{"-"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for run, rows := range tc.input {
				got, err := Normalize(rows, Columns)
				if err != nil {
					t.Fatalf("%s (%s): %s\ninput run %d %+v\nerror %v", tc.name, tc.category, tc.description, run, rows, err)
				}
				vals := make([]string, 0, len(got))
				for _, r := range got {
					v, ok := r["nsid"]
					if !ok {
						v = absent
					}
					vals = append(vals, v)
				}
				if !reflect.DeepEqual(vals, tc.expected) {
					t.Errorf("%s (%s): %s\ninput run %d %+v\nexpected %q\nactual   %q",
						tc.name, tc.category, tc.description, run, rows, tc.expected, vals)
				}
			}
		})
	}
}

// go test ./pkg/goldenrec/ -run TestDiffNsid
func TestDiffNsid(t *testing.T) {
	c := func(name string, nsid uint32, assigned bool) nsrec {
		return nsrec{Family: afInet, State: 1, Container: name, Nsid: nsid, Assigned: assigned}
	}
	golden := []nsrec{c("c1", 0, true), c("c2", 1, true), c("c3", 0, false)}
	tests := []struct {
		name        string
		category    string
		description string
		input       []nsrec // the candidate, diffed against golden
		expected    string  // in Format(diffs); "" = no differences
	}{
		{"ids_swapped", "positive", "the kernel numbering the containers the other way round is no difference",
			[]nsrec{c("c1", 1, true), c("c2", 0, true), c("c3", 0, false)}, ""},
		{"assigned_lost", "negative", "a namespace that loses its id is reported",
			[]nsrec{c("c1", 0, false), c("c2", 1, true), c("c3", 0, false)}, `golden "true", candidate "false"`},
		{"zero_vs_none", "boundary", "an assigned 0 against no id is a difference, though both read 0",
			[]nsrec{c("c1", 0, true), c("c2", 1, true), c("c3", 0, true)}, `golden "false", candidate "true"`},
		{"stray_value", "corner", "a stray value without the flag is no difference",
			[]nsrec{c("c1", 0, true), c("c2", 1, true), c("c3", 9, false)}, ""},
	}
	g, err := Normalize(golden, Columns)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cand, err := Normalize(tc.input, Columns)
			if err != nil {
				t.Fatal(err)
			}
			diffs, err := Diff(g, cand, Columns, Options{})
			if err != nil {
				t.Fatal(err)
			}
			got := Format(diffs, 50)
			if (tc.expected == "") != (len(diffs) == 0) || !strings.Contains(got, tc.expected) {
				t.Errorf("%s (%s): %s\ninput %+v\nexpected %q\nactual   %q",
					tc.name, tc.category, tc.description, tc.input, tc.expected, got)
			}
		})
	}
}
