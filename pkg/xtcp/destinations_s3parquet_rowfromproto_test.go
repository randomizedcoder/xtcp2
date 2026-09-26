//go:build dest_s3parquet

package xtcp

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// destinations_s3parquet_rowfromproto_test.go guards rowFromProto's
// field-by-field mapping.
//
// Why this test exists. rowFromProto is a single keyed composite literal of
// ~163 assignments. A keyed literal with a field omitted is legal Go, so
// dropping or mis-wiring one assignment COMPILES, passes every other test in
// this package, and silently writes zeros into that Parquet column for as
// long as it takes someone to notice the data is wrong. The three schema
// tests in destinations_s3parquet_schema_test.go only reflect over the
// ParquetRow *struct* — none of them calls rowFromProto — and
// BenchmarkRowFromProto discards its result. So before this file, nothing
// asserted anything about rowFromProto's output.
//
// That also makes the drift-defense claim in rowFromProto's doc comment
// false for omissions: only a field with no ParquetRow counterpart at all
// is a compile error. This test is the guard that actually holds.
//
// The test is reflection-driven rather than a list of 163 assertions so it
// cannot go stale: a proto field added to both the message and ParquetRow is
// covered automatically, and a ParquetRow field that rowFromProto forgets is
// reported by name.

// rowFromProtoSkip lists ParquetRow fields that must NOT be populated by
// rowFromProto. event_date is derived and stamped by the caller
// (destinations_s3parquet.go, the row-append path), not copied from the
// proto — the same allowlist the schema test keeps as derivedColumns.
var rowFromProtoSkip = map[string]bool{"EventDate": true}

// sentinelFor produces a deterministic, field-distinct value of type t.
// seq is the field's ordinal, used so that a swapped pair of assignments
// (two fields of the same type crossed over) is detected, not just an
// omission.
func sentinelFor(t *testing.T, typ reflect.Type, seq int) reflect.Value {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		return reflect.ValueOf(fmt.Sprintf("v%d", seq)).Convert(typ)
	case reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(uint64(seq)).Convert(typ)
	case reflect.Int32, reflect.Int64:
		return reflect.ValueOf(int64(seq)).Convert(typ)
	case reflect.Slice:
		if typ.Elem().Kind() != reflect.Uint8 {
			t.Fatalf("unhandled slice element kind %s", typ.Elem().Kind())
		}
		return reflect.ValueOf([]byte{byte(seq), byte(seq >> 8), 0xA5}).Convert(typ)
	default:
		t.Fatalf("sentinelFor: unhandled kind %s (type %s) — extend this switch", typ.Kind(), typ)
		return reflect.Value{}
	}
}

// extremeFor produces the maximum (or minimum, when low is true) value
// representable in typ. Used by the boundary rows: a copy that silently
// narrows uint64 -> uint32 or loses the sign of an int32 shows up here and
// nowhere else.
func extremeFor(t *testing.T, typ reflect.Type, low bool) reflect.Value {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		if low {
			return reflect.ValueOf("").Convert(typ)
		}
		return reflect.ValueOf("ÿ\U0001f600 max").Convert(typ)
	case reflect.Uint32:
		if low {
			return reflect.ValueOf(uint64(0)).Convert(typ)
		}
		return reflect.ValueOf(uint64(math.MaxUint32)).Convert(typ)
	case reflect.Uint64:
		if low {
			return reflect.ValueOf(uint64(0)).Convert(typ)
		}
		return reflect.ValueOf(uint64(math.MaxUint64)).Convert(typ)
	case reflect.Int32:
		if low {
			return reflect.ValueOf(int64(math.MinInt32)).Convert(typ)
		}
		return reflect.ValueOf(int64(math.MaxInt32)).Convert(typ)
	case reflect.Int64:
		if low {
			return reflect.ValueOf(int64(math.MinInt64)).Convert(typ)
		}
		return reflect.ValueOf(int64(math.MaxInt64)).Convert(typ)
	case reflect.Slice:
		if low {
			return reflect.ValueOf([]byte(nil)).Convert(typ)
		}
		return reflect.ValueOf([]byte{0xff, 0xff, 0xff, 0xff}).Convert(typ)
	default:
		t.Fatalf("extremeFor: unhandled kind %s (type %s) — extend this switch", typ.Kind(), typ)
		return reflect.Value{}
	}
}

// sameValue compares a proto-side value against the ParquetRow-side value
// it should have become. The two are not always the identical Go type: the
// two enum columns are stored as plain int32, so compare after converting
// the proto value into the row field's type.
func sameValue(protoVal, rowVal reflect.Value) bool {
	if protoVal.Kind() == reflect.Slice {
		return bytes.Equal(protoVal.Bytes(), rowVal.Bytes())
	}
	if !protoVal.Type().ConvertibleTo(rowVal.Type()) {
		return false
	}
	return protoVal.Convert(rowVal.Type()).Interface() == rowVal.Interface()
}

// TestRowFromProto_everyFieldMapped fills every proto field that has a
// same-named ParquetRow field, runs rowFromProto, and asserts each column
// received exactly that value.
//
// Run this against the UNSPLIT rowFromProto first: if it fails before any
// refactor, the test is wrong, not the code.
func TestRowFromProto_everyFieldMapped(t *testing.T) {
	t.Parallel()

	rowType := reflect.TypeOf(ParquetRow{})
	protoType := reflect.TypeOf(xtcp_flat_record.XtcpFlatRecord{})

	cases := []struct {
		name        string
		description string
		category    string
		// fill returns the value to write into the proto field of type typ,
		// for the field at ordinal seq. Returning the zero Value means
		// "leave the field alone".
		fill func(t *testing.T, typ reflect.Type, seq int) reflect.Value
		// wantAllZero asserts the whole row (minus the skip list) came back
		// zero, rather than comparing field by field.
		wantAllZero bool
	}{
		{
			name:        "distinct_sentinel_per_field",
			description: "positive: every proto field carrying a distinct sentinel lands in its own column, so no assignment is missing, duplicated or crossed",
			category:    "positive",
			fill: func(t *testing.T, typ reflect.Type, seq int) reflect.Value {
				return sentinelFor(t, typ, seq+1)
			},
		},
		{
			name:        "type_maximum",
			description: "boundary: the maximum value of each field type survives the copy without truncation (a uint64 column narrowed to uint32 would fail here)",
			category:    "boundary",
			fill: func(t *testing.T, typ reflect.Type, _ int) reflect.Value {
				return extremeFor(t, typ, false)
			},
		},
		{
			name:        "type_minimum",
			description: "boundary: the minimum value of each field type survives, including the negative enum columns whose sign a uint32 copy would destroy",
			category:    "boundary",
			fill: func(t *testing.T, typ reflect.Type, _ int) reflect.Value {
				return extremeFor(t, typ, true)
			},
		},
		{
			name:        "all_zero_record",
			description: "corner: an all-zero record produces an all-zero row — rowFromProto injects no defaults of its own",
			category:    "corner",
			fill: func(_ *testing.T, _ reflect.Type, _ int) reflect.Value {
				return reflect.Value{}
			},
			wantAllZero: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()

			rec := &xtcp_flat_record.XtcpFlatRecord{}
			recValue := reflect.ValueOf(rec).Elem()

			// Field name -> the proto value we wrote, for the comparison pass.
			written := make(map[string]reflect.Value, rowType.NumField())

			seq := 0
			for i := 0; i < rowType.NumField(); i++ {
				rf := rowType.Field(i)
				if rowFromProtoSkip[rf.Name] {
					continue
				}
				pf, ok := protoType.FieldByName(rf.Name)
				if !ok {
					t.Fatalf("ParquetRow field %q has no same-named field on XtcpFlatRecord; "+
						"either it is derived (add it to rowFromProtoSkip, and to derivedColumns "+
						"in the schema test) or the proto was renamed", rf.Name)
				}
				target := recValue.FieldByIndex(pf.Index)
				if !target.CanSet() {
					t.Fatalf("proto field %q is not settable", rf.Name)
				}
				seq++
				v := tc.fill(t, pf.Type, seq)
				if v.IsValid() {
					target.Set(v)
				}
				written[rf.Name] = target
			}

			got := rowFromProto(rec)
			gotValue := reflect.ValueOf(got)

			for i := 0; i < rowType.NumField(); i++ {
				rf := rowType.Field(i)
				rowVal := gotValue.Field(i)

				if rowFromProtoSkip[rf.Name] {
					// Negative case, asserted on every row: EventDate is the
					// caller's to stamp. rowFromProto writing it here would
					// mean the derived column is produced in two places.
					if !rowVal.IsZero() {
						t.Errorf("%s: %s = %v, want the zero value — it is caller-stamped, not copied",
							tc.description, rf.Name, rowVal.Interface())
					}
					continue
				}

				if tc.wantAllZero {
					if !rowVal.IsZero() {
						t.Errorf("%s: %s = %v, want zero", tc.description, rf.Name, rowVal.Interface())
					}
					continue
				}

				protoVal, ok := written[rf.Name]
				if !ok {
					t.Fatalf("internal: no recorded proto value for %s", rf.Name)
				}
				if !sameValue(protoVal, rowVal) {
					t.Errorf("%s: %s = %#v, want %#v (proto field not copied, or copied from the wrong field)",
						tc.description, rf.Name, rowVal.Interface(), protoVal.Interface())
				}
			}
		})
	}
}

// TestRowFromProto_enumColumnsMapByValue pins the two non-verbatim
// assignments: EnrichSocketDestLocality and InetDiagCongEnum are proto enums
// stored as plain int32. The cast must preserve the enum's numeric value. If
// either were ever copied by ordinal — or the enum renumbered — the Parquet
// column would disagree with the ClickHouse enum and with every other sink,
// silently.
func TestRowFromProto_enumColumnsMapByValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		description  string
		category     string
		locality     xtcp_flat_record.XtcpFlatRecord_Locality
		cong         xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm
		wantLocality int32
		wantCong     int32
	}{
		{
			description:  "positive: a named locality and a named cong algorithm map to their own enum numbers",
			category:     "positive",
			locality:     xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE,
			cong:         xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3,
			wantLocality: int32(xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE),
			wantCong:     int32(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3),
		},
		{
			description:  "boundary: the zero/unspecified member of each enum maps to 0, not to a sentinel",
			category:     "boundary",
			locality:     xtcp_flat_record.XtcpFlatRecord_LOCALITY_UNSPECIFIED,
			cong:         xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED,
			wantLocality: 0,
			wantCong:     0,
		},
		{
			description:  "corner: an enum number with no generated name still round-trips by value, so a proto-side addition cannot be lost before the schema catches up",
			category:     "corner",
			locality:     xtcp_flat_record.XtcpFlatRecord_Locality(9999),
			cong:         xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm(31337),
			wantLocality: 9999,
			wantCong:     31337,
		},
		{
			description:  "negative: a negative enum number is preserved rather than wrapping through an unsigned column",
			category:     "negative",
			locality:     xtcp_flat_record.XtcpFlatRecord_Locality(-7),
			cong:         xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm(math.MinInt32),
			wantLocality: -7,
			wantCong:     math.MinInt32,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.category, func(t *testing.T) {
			t.Parallel()
			got := rowFromProto(&xtcp_flat_record.XtcpFlatRecord{
				EnrichSocketDestLocality: tc.locality,
				InetDiagCongEnum:         tc.cong,
			})
			if got.EnrichSocketDestLocality != tc.wantLocality {
				t.Errorf("%s: EnrichSocketDestLocality = %d, want %d",
					tc.description, got.EnrichSocketDestLocality, tc.wantLocality)
			}
			if got.InetDiagCongEnum != tc.wantCong {
				t.Errorf("%s: InetDiagCongEnum = %d, want %d",
					tc.description, got.InetDiagCongEnum, tc.wantCong)
			}
		})
	}
}
