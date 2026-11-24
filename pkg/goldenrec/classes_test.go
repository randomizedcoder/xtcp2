package goldenrec

import (
	"reflect"
	"strings"
	"testing"
)

// TestCheck covers the class-table checker on small structs; pkg/xtcp's
// TestGoldenClassesComplete runs it on ParquetRow.
func TestCheck(t *testing.T) {
	type two struct {
		A uint32 `parquet:"a"`
		B uint32 `parquet:"b"`
	}
	type extra struct {
		A   uint32 `parquet:"a"`
		B   uint32 `parquet:"b"`
		New uint32 `parquet:"new_col"`
	}
	type skipped struct {
		A     uint32 `parquet:"a"`
		B     uint32 `parquet:"b"`
		Plain uint32
		Dash  uint32 `parquet:"-"`
	}
	type options struct {
		A  uint32 `parquet:"a,delta"`
		Ns string `parquet:"netns,dict,zstd"`
	}
	ab := Table{{Strict, []string{"a"}}, {Presence, []string{"b"}}}
	cases := []struct {
		name        string
		category    string
		description string
		typ         reflect.Type
		table       Table
		expected    []string // substrings of the error; nil = no error
	}{
		{"matches", "positive", "every column in exactly one class",
			reflect.TypeFor[two](), ab, nil},
		{"unclassified_column", "negative", "a field no class lists",
			reflect.TypeFor[extra](), ab, []string{"unclassified column new_col"}},
		{"stale_entry", "negative", "a class entry with no field",
			reflect.TypeFor[two](), append(Table{{Volatile, []string{"gone_col"}}}, ab...),
			[]string{"class entry gone_col has no column"}},
		{"duplicate_class", "negative", "a column in two classes names both",
			reflect.TypeFor[two](), append(Table{{Strict, []string{"b"}}}, ab...),
			[]string{"column b is in classes strict and presence"}},
		{"not_a_struct", "negative", "a non-struct type", reflect.TypeFor[int](), ab, []string{"is not a struct"}},
		{"empty_struct", "boundary", "no columns and an empty table", reflect.TypeFor[struct{}](), nil, nil},
		{"untagged_field", "corner", "untagged and \"-\" fields are not columns",
			reflect.TypeFor[skipped](), ab, nil},
		{"tag_options", "corner", "a tag's options are cut from its name",
			reflect.TypeFor[options](), Table{{Strict, []string{"a"}}, {Netns, []string{"netns"}}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.typ, tc.table)
			if tc.expected == nil {
				if err != nil {
					t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("[%s] %s: no error, expected %q", tc.category, tc.description, tc.expected)
			}
			for _, s := range tc.expected {
				if !strings.Contains(err.Error(), s) {
					t.Fatalf("[%s] %s: err %v, expected it to contain %q", tc.category, tc.description, err, s)
				}
			}
		})
	}
}
