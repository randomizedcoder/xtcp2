//go:build dest_s3parquet

package xtcp

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// parquetTagName extracts the column name from a parquet struct tag
// (everything before the first comma). Returns "" if the tag is missing.
func parquetTagName(field reflect.StructField) string {
	tag := field.Tag.Get("parquet")
	if tag == "" {
		return ""
	}
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		return tag[:comma]
	}
	return tag
}

// TestS3ParquetSchema_matchesProto asserts the set of parquet-tag column
// names on ParquetRow is exactly the field-name set on the proto's
// XtcpFlatRecord. A proto field addition that isn't mirrored in the
// struct fails this test with a precise diff. Drift defense for the
// hand-written-struct approach (plan D3).
func TestS3ParquetSchema_matchesProto(t *testing.T) {
	protoNames := make(map[string]bool)
	desc := (&xtcp_flat_record.XtcpFlatRecord{}).ProtoReflect().Descriptor()
	for i := 0; i < desc.Fields().Len(); i++ {
		protoNames[string(desc.Fields().Get(i).Name())] = true
	}

	parquetNames := make(map[string]bool)
	rv := reflect.TypeOf(ParquetRow{})
	for i := 0; i < rv.NumField(); i++ {
		name := parquetTagName(rv.Field(i))
		if name == "" {
			t.Errorf("ParquetRow.%s has no `parquet:` tag", rv.Field(i).Name)
			continue
		}
		if parquetNames[name] {
			t.Errorf("duplicate parquet column name %q", name)
		}
		parquetNames[name] = true
	}

	if len(protoNames) != len(parquetNames) {
		t.Errorf("proto has %d fields, ParquetRow has %d columns", len(protoNames), len(parquetNames))
	}

	var missing, extra []string
	for n := range protoNames {
		if !parquetNames[n] {
			missing = append(missing, n)
		}
	}
	for n := range parquetNames {
		if !protoNames[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("proto fields NOT mirrored in ParquetRow: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("ParquetRow columns NOT in proto: %v", extra)
	}
}

// TestS3ParquetSchema_compilesViaParquetGo asserts parquet-go can derive
// a Schema from ParquetRow via reflection (no unsupported types). Cheaper
// to run than a full file write, and pins the exact column count.
func TestS3ParquetSchema_compilesViaParquetGo(t *testing.T) {
	schema := parquet.SchemaOf(ParquetRow{})
	if schema == nil {
		t.Fatal("parquet.SchemaOf returned nil")
	}
	got := len(schema.Columns())
	want := reflect.TypeOf(ParquetRow{}).NumField()
	if got != want {
		t.Errorf("schema has %d columns, struct has %d fields", got, want)
	}
}

// TestS3ParquetSchema_columnTypes asserts a representative sample of
// proto field types map to the expected Parquet physical kinds. Catches
// regressions if someone changes a struct field type in a way that
// breaks downstream readers.
func TestS3ParquetSchema_columnTypes(t *testing.T) {
	schema := parquet.SchemaOf(ParquetRow{})

	leafByName := map[string]parquet.LeafColumn{}
	for _, path := range schema.Columns() {
		if len(path) != 1 {
			t.Errorf("unexpected nested column path: %v", path)
			continue
		}
		leaf, ok := schema.Lookup(path...)
		if !ok {
			t.Errorf("column %q in Columns() but not Lookup-able", path[0])
			continue
		}
		leafByName[path[0]] = leaf
	}

	cases := []struct {
		col      string
		wantKind parquet.Kind
	}{
		{"timestamp_ns", parquet.Double},
		{"hostname", parquet.ByteArray},
		{"netns", parquet.ByteArray},
		{"inet_diag_msg_socket_source", parquet.ByteArray},
		{"nsid", parquet.Int32},
		{"socket_fd", parquet.Int64},
		{"congestion_algorithm_enum", parquet.Int32},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.col, func(t *testing.T) {
			leaf, ok := leafByName[tc.col]
			if !ok {
				t.Fatalf("column %q not in schema", tc.col)
			}
			if got := leaf.Node.Type().Kind(); got != tc.wantKind {
				t.Errorf("column %q kind = %v, want %v", tc.col, got, tc.wantKind)
			}
		})
	}
}
