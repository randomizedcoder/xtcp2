//go:build dest_s3parquet

package xtcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/goldenrec"
)

// parquetColumnCount is ParquetRow's column count. A new column changes it,
// fails TestGoldenClassesComplete until goldenrec.Columns lists it, and
// means the golden fixture needs recapturing (nix run .#capture-golden-records).
const parquetColumnCount = 181

// goldenFixtureRows reads every committed golden capture,
// testdata/golden/<kernel>/records.parquet.
func goldenFixtureRows(tb testing.TB) []ParquetRow {
	tb.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*", "records.parquet"))
	if err != nil {
		tb.Fatal(err)
	}
	var rows []ParquetRow
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			tb.Fatal(err)
		}
		r, err := parquet.Read[ParquetRow](bytes.NewReader(b), int64(len(b)))
		if err != nil {
			tb.Fatalf("%s: %v", p, err)
		}
		rows = append(rows, r...)
	}
	return rows
}

// realDumpRows is the captured netlink dump as ParquetRows.
func realDumpRows(tb testing.TB) []ParquetRow {
	tb.Helper()
	written, _, err := roundTripEnvelope(tb, realDumpEnvelope(tb))
	if err != nil {
		tb.Fatal(err)
	}
	return written
}

func TestGoldenClassesComplete(t *testing.T) {
	rowType := reflect.TypeFor[ParquetRow]()
	stale := append(goldenrec.Table{{Class: goldenrec.Strict, Columns: []string{"gone_col"}}}, goldenrec.Columns...)
	cases := []struct {
		name        string
		category    string
		description string
		check       func() error
		expected    string // substring of the error; "" = none
	}{
		{"every_column_classified", "positive", "each ParquetRow column is in exactly one class",
			func() error { return goldenrec.Check(rowType, goldenrec.Columns) }, ""},
		{"stale_entry", "negative", "a class entry naming no ParquetRow column",
			func() error { return goldenrec.Check(rowType, stale) }, "class entry gone_col has no column"},
		{"column_count", "boundary", "the table lists as many columns as ParquetRow has",
			func() error {
				n := 0
				for _, l := range goldenrec.Columns {
					n += len(l.Columns)
				}
				if n != parquetColumnCount || rowType.NumField() != parquetColumnCount {
					return fmt.Errorf("table %d, struct %d, expected %d", n, rowType.NumField(), parquetColumnCount)
				}
				return nil
			}, ""},
		{"derived_column", "corner", "event_date, derived and not a proto field, is classified volatile",
			func() error {
				for _, l := range goldenrec.Columns {
					if slices.Contains(l.Columns, "event_date") && l.Class != goldenrec.Volatile {
						return fmt.Errorf("event_date is %s", l.Class)
					}
				}
				return nil
			}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.check()
			if tc.expected == "" && err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)) {
				t.Fatalf("[%s] %s: err %v, expected one containing %q", tc.category, tc.description, err, tc.expected)
			}
		})
	}
}

// TestGoldenScrub scrubs whole ParquetRows: the real netlink dump and
// edge-case rows. goldenrec's TestScrub covers each column class.
func TestGoldenScrub(t *testing.T) {
	cases := []struct {
		name        string
		category    string
		description string
		rows        func(tb testing.TB) []ParquetRow
		expected    int // invalid address cells reported
	}{
		{"real_dump", "positive", "rows decoded from the captured netlink dump", realDumpRows, 0},
		{"sentinel_all_fields", "negative", "2-byte sentinel addresses and non-address strings are reported, not mapped",
			func(tb testing.TB) []ParquetRow {
				return []ParquetRow{rowFromProto(sentinelRecord(tb))}
			}, 4},
		{"empty", "boundary", "no rows", func(testing.TB) []ParquetRow { return nil }, 0},
		{"real_dump_reversed", "corner", "the real dump in reverse order",
			func(tb testing.TB) []ParquetRow {
				r := realDumpRows(tb)
				slices.Reverse(r)
				return r
			}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.rows(t)
			before := normalizedJSON(t, in)
			out := slices.Clone(in)
			rep, err := goldenrec.Scrub(out, goldenrec.Columns)
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if len(rep.Invalid) != tc.expected {
				t.Fatalf("[%s] %s: %d invalid cells %q, expected %d", tc.category, tc.description, len(rep.Invalid), rep.Invalid, tc.expected)
			}
			if after := normalizedJSON(t, out); after != before {
				t.Fatalf("[%s] %s: Normalize changed:\n got %s\nwant %s", tc.category, tc.description, after, before)
			}
			for i, r := range out {
				if r.Hostname != "" && r.Hostname != goldenrec.ScrubHostname {
					t.Fatalf("[%s] %s: row %d hostname %q", tc.category, tc.description, i, r.Hostname)
				}
				if err := documentationOnly(r); err != nil && len(rep.Invalid) == 0 {
					t.Fatalf("[%s] %s: row %d: %v", tc.category, tc.description, i, err)
				}
			}
			again := slices.Clone(out)
			if _, err := goldenrec.Scrub(again, goldenrec.Columns); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(again, out) {
				t.Fatalf("[%s] %s: a second scrub changed the rows", tc.category, tc.description)
			}
		})
	}
}

// documentationOnly reports an address in r outside loopback, unspecified
// and the documentation ranges.
func documentationOnly(r ParquetRow) error {
	doc := []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8::/32")}
	ok := func(a netip.Addr) bool {
		return a.IsLoopback() || a.IsUnspecified() || doc[0].Contains(a) || doc[1].Contains(a)
	}
	for _, b := range [][]byte{r.InetDiagMsgSocketSource, r.InetDiagMsgSocketDestination} {
		if len(b) == 0 {
			continue
		}
		a, valid := netip.AddrFromSlice(b)
		if r.InetDiagMsgFamily == 2 && len(b) == 16 {
			a, valid = netip.AddrFromSlice(b[:4])
		}
		if !valid || !ok(a.Unmap()) {
			return fmt.Errorf("unscrubbed address %x", b)
		}
	}
	for _, s := range []string{r.Uplink1LldpMgmtIp, r.Uplink2LldpMgmtIp} {
		if a, err := netip.ParseAddr(s); s != "" && (err != nil || !ok(a)) {
			return fmt.Errorf("unscrubbed address %q", s)
		}
	}
	return nil
}

func normalizedJSON(tb testing.TB, rows []ParquetRow) string {
	tb.Helper()
	n, err := goldenrec.Normalize(rows, goldenrec.Columns)
	if err != nil {
		tb.Fatal(err)
	}
	b, err := json.Marshal(n)
	if err != nil {
		tb.Fatal(err)
	}
	return string(b)
}

// protoFromRow is rowFromProto's inverse, copying each field to the proto
// field of the same name; event_date, derived from timestamp_ns, has none.
func protoFromRow(r ParquetRow) (*xtcp_flat_record.XtcpFlatRecord, error) {
	return protoFromStruct(reflect.ValueOf(r))
}

func protoFromStruct(v reflect.Value) (*xtcp_flat_record.XtcpFlatRecord, error) {
	rec := &xtcp_flat_record.XtcpFlatRecord{}
	pv := reflect.ValueOf(rec).Elem()
	var unmapped []string
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		if name == "EventDate" {
			continue
		}
		f := pv.FieldByName(name)
		if !f.IsValid() || !v.Field(i).Type().ConvertibleTo(f.Type()) {
			unmapped = append(unmapped, name)
			continue
		}
		f.Set(v.Field(i).Convert(f.Type()))
	}
	if len(unmapped) > 0 {
		return nil, fmt.Errorf("no proto field for %s", strings.Join(unmapped, ", "))
	}
	return rec, nil
}

// maxRow sets every integer to its maximum and every byte column to 16
// bytes of 0xff.
func maxRow() ParquetRow {
	var r ParquetRow
	v := reflect.ValueOf(&r).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Uint32, reflect.Uint64:
			f.SetUint(math.MaxUint64 >> (64 - f.Type().Bits()))
		case reflect.Int32, reflect.Int64:
			f.SetInt(math.MaxInt64 >> (64 - f.Type().Bits()))
		case reflect.Slice:
			f.SetBytes(bytes.Repeat([]byte{0xff}, 16))
		case reflect.Bool:
			f.SetBool(true)
		default:
		}
	}
	r.EventDate = ""
	return r
}

func TestProtoFromRowRoundTrip(t *testing.T) {
	type extraField struct {
		ParquetRowOnly uint32
		Hostname       string
	}
	type expected struct {
		err     string // from protoFromStruct or proto.Marshal
		minRows int    // rows that must round-trip
	}
	cases := []struct {
		name        string
		category    string
		description string
		rows        func(tb testing.TB) []reflect.Value
		expected    expected
	}{
		{"golden_rows", "positive", "every row of the committed golden fixture",
			func(tb testing.TB) []reflect.Value { return values(goldenFixtureRows(tb)) }, expected{minRows: 1}},
		{"all_fields_sentinel", "positive", "the all-fields sentinel record",
			func(tb testing.TB) []reflect.Value {
				return values([]ParquetRow{rowFromProto(sentinelRecord(tb))})
			}, expected{minRows: 1}},
		{"unmapped_field", "negative", "a struct field with no proto counterpart is named, not zeroed",
			func(testing.TB) []reflect.Value { return []reflect.Value{reflect.ValueOf(extraField{1, "h"})} },
			expected{err: "no proto field for ParquetRowOnly"}},
		{"zero_row", "boundary", "an empty ParquetRow",
			func(testing.TB) []reflect.Value { return values([]ParquetRow{{}}) }, expected{minRows: 1}},
		{"max_values", "boundary", "every integer at its maximum and 16-byte addresses of 0xff",
			func(testing.TB) []reflect.Value { return values([]ParquetRow{maxRow()}) }, expected{minRows: 1}},
		{"invalid_utf8_string", "corner", "\\xff in container_name fails the proto3 marshal",
			func(testing.TB) []reflect.Value {
				return values([]ParquetRow{{ContainerName: "\xff"}})
			}, expected{err: "invalid UTF-8"}},
		{"event_date_derived", "corner", "event_date comes back as the UTC day of timestamp_ns",
			func(testing.TB) []reflect.Value {
				return values([]ParquetRow{{TimestampNs: 1_767_225_600_000_000_000 - 1, EventDate: "ignored"}})
			}, expected{minRows: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var protos []*xtcp_flat_record.XtcpFlatRecord
			var rows []ParquetRow
			var err error
			for _, v := range tc.rows(t) {
				var p *xtcp_flat_record.XtcpFlatRecord
				if p, err = protoFromStruct(v); err != nil {
					break
				}
				if _, err = proto.Marshal(p); err != nil {
					break
				}
				protos = append(protos, p)
				if r, ok := v.Interface().(ParquetRow); ok {
					rows = append(rows, r)
				}
			}
			if tc.expected.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected.err) {
					t.Fatalf("[%s] %s: err %v, expected one containing %q", tc.category, tc.description, err, tc.expected.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			if len(rows) < tc.expected.minRows {
				t.Fatalf("[%s] %s: %d rows, expected at least %d", tc.category, tc.description, len(rows), tc.expected.minRows)
			}
			for i, p := range protos {
				got, want := rowFromProto(p), rows[i]
				want.EventDate = ""
				if !reflect.DeepEqual(normalizeRow(got), normalizeRow(want)) {
					t.Fatalf("[%s] %s: row %d changed:\n got %+v\nwant %+v", tc.category, tc.description, i, got, want)
				}
			}
			written, read, err := roundTripEnvelope(t, delimited(t, protos...))
			if err != nil {
				t.Fatalf("[%s] %s: %v", tc.category, tc.description, err)
			}
			checkRoundTrip(t, written, read) // includes event_date = utcDateFromNs(timestamp_ns)
		})
	}
}

func values(rows []ParquetRow) []reflect.Value {
	out := make([]reflect.Value, len(rows))
	for i := range rows {
		out[i] = reflect.ValueOf(rows[i])
	}
	return out
}

// goldenSeed is the golden fixture as one envelope, a FuzzRowFromProto seed;
// nil when there is no fixture.
func goldenSeed(tb testing.TB) []byte {
	tb.Helper()
	rows := goldenFixtureRows(tb)
	if len(rows) == 0 {
		return nil
	}
	protos := make([]*xtcp_flat_record.XtcpFlatRecord, 0, len(rows))
	for i := range rows {
		p, err := protoFromRow(rows[i])
		if err != nil {
			tb.Fatal(err)
		}
		protos = append(protos, p)
	}
	return delimited(tb, protos...)
}
