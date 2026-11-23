//go:build dest_s3parquet

package xtcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// roundTripEnvelope runs one delimited envelope through the S3 worker's row
// path (processItem in destinations_s3parquet.go): unmarshal, rowFromProto,
// the event_date stamp, a Parquet write, and then reads the object back. An
// unmarshal error is returned (the worker drops such an envelope and counts
// it); a Parquet write or read failure fails the test, since every decoded
// row must be writable.
func roundTripEnvelope(tb testing.TB, data []byte) (written, read []ParquetRow, err error) {
	tb.Helper()
	var env xtcp_flat_record.Envelope
	if err := protodelim.UnmarshalFrom(bytes.NewReader(data), &env); err != nil {
		return nil, nil, err
	}
	written = make([]ParquetRow, 0, len(env.Row))
	for _, r := range env.Row {
		row := rowFromProto(r)
		row.EventDate = utcDateFromNs(r.TimestampNs)
		written = append(written, row)
	}
	var buf bytes.Buffer
	w := parquet.NewGenericWriter[ParquetRow](&buf)
	if _, err := w.Write(written); err != nil {
		tb.Fatalf("parquet write of %d rows: %v", len(written), err)
	}
	if err := w.Close(); err != nil {
		tb.Fatalf("parquet close: %v", err)
	}
	rd := parquet.NewGenericReader[ParquetRow](bytes.NewReader(buf.Bytes()))
	defer func() {
		if err := rd.Close(); err != nil {
			tb.Errorf("parquet reader close: %v", err)
		}
	}()
	read = make([]ParquetRow, rd.NumRows())
	n, err := rd.Read(read)
	if err != nil && !errors.Is(err, io.EOF) {
		tb.Fatalf("parquet read: %v", err)
	}
	return written, read[:n], nil
}

// checkRoundTrip asserts what must hold for any envelope that decodes: every
// row comes back, each column equal to what was written, and event_date is
// the UTC day of timestamp_ns.
func checkRoundTrip(tb testing.TB, written, read []ParquetRow) {
	tb.Helper()
	if len(read) != len(written) {
		tb.Fatalf("read %d rows, wrote %d", len(read), len(written))
	}
	for i := range written {
		if !reflect.DeepEqual(normalizeRow(written[i]), normalizeRow(read[i])) {
			tb.Fatalf("row %d changed in the round trip:\nwrote %+v\nread  %+v", i, written[i], read[i])
		}
		if want := utcDateFromNs(written[i].TimestampNs); read[i].EventDate != want {
			tb.Fatalf("row %d: EventDate %q, expected %q", i, read[i].EventDate, want)
		}
	}
}

// normalizeRow maps empty byte slices to nil: Parquet does not keep the
// difference between a nil and an empty BYTE_ARRAY, and neither do readers.
func normalizeRow(r ParquetRow) ParquetRow {
	v := reflect.ValueOf(&r).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Slice && f.Len() == 0 && f.CanSet() {
			f.Set(reflect.Zero(f.Type()))
		}
	}
	return r
}

// delimited marshals rows into one length-delimited envelope, the form the
// worker's queue carries.
func delimited(tb testing.TB, rows ...*xtcp_flat_record.XtcpFlatRecord) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if _, err := protodelim.MarshalTo(&buf, &xtcp_flat_record.Envelope{Row: rows}); err != nil {
		tb.Fatalf("marshal envelope: %v", err)
	}
	return buf.Bytes()
}

// sentinelRecord sets every string field to a distinct value and every
// numeric field to its ordinal, so a crossed or dropped column shows in the
// round trip.
func sentinelRecord(tb testing.TB) *xtcp_flat_record.XtcpFlatRecord {
	tb.Helper()
	rec := &xtcp_flat_record.XtcpFlatRecord{}
	v := reflect.ValueOf(rec).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !v.Type().Field(i).IsExported() {
			continue
		}
		switch f.Kind() {
		case reflect.String:
			f.SetString("s" + v.Type().Field(i).Name)
		case reflect.Int32, reflect.Int64:
			f.SetInt(int64(i + 1))
		case reflect.Uint32, reflect.Uint64:
			f.SetUint(uint64(i + 1))
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Slice:
			if f.Type().Elem().Kind() == reflect.Uint8 {
				f.SetBytes([]byte{byte(i), 0xA5})
			}
		default:
			tb.Fatalf("field %s: kind %s has no sentinel; add one", v.Type().Field(i).Name, f.Kind())
		}
	}
	rec.TimestampNs = 1_700_000_000_000_000_000
	return rec
}

// realDumpEnvelope decodes the captured netlink dump that FuzzDeserialize
// also seeds from, and returns its records as one delimited envelope.
func realDumpEnvelope(tb testing.TB) []byte {
	tb.Helper()
	x := newTestDeserializeXTCP(tb)
	x.dest = newRecordingDest(x)
	x.currentEnvelope = &xtcp_flat_record.Envelope{}
	buf := loadRealMultipart(tb)
	ns := "fuzz-ns"
	if _, err := x.Deserialize(context.Background(), DeserializeArgs{
		ns:             &ns,
		NLPacket:       &buf,
		xtcpRecordPool: &x.xtcpRecordPool,
		nlhPool:        &x.nlhPool,
		rtaPool:        &x.rtaPool,
		pC:             x.pC,
		pH:             x.pH,
	}); err != nil {
		tb.Fatalf("deserialize the real dump: %v", err)
	}
	if len(x.currentEnvelope.Row) == 0 {
		tb.Fatal("the real dump decoded to zero rows")
	}
	return delimited(tb, x.currentEnvelope.Row...)
}

// invalidUTF8Envelope marshals a valid record, then overwrites its hostname
// bytes in place with 0xff, which proto3 string fields must reject.
func invalidUTF8Envelope(tb testing.TB) []byte {
	tb.Helper()
	b := delimited(tb, &xtcp_flat_record.XtcpFlatRecord{Hostname: "HOSTNAME", TimestampNs: 1})
	i := bytes.Index(b, []byte("HOSTNAME"))
	if i < 0 {
		tb.Fatal("hostname bytes not found in the envelope")
	}
	b[i], b[i+1] = 0xff, 0xfe
	return b
}

type rowRoundTripWant struct {
	unmarshalErr bool
	rows         int
	eventDates   []string // checked when set
}

// rowRoundTripCase is one envelope. The rows drive both
// TestRowFromProtoRoundTrip (exact expectations) and FuzzRowFromProto (seeds).
type rowRoundTripCase struct {
	name        string
	cat         string
	description string
	envelope    func(tb testing.TB) []byte
	expected    rowRoundTripWant
}

func rowRoundTripCases() []rowRoundTripCase {
	const midnight = 1_700_006_400_000_000_000 // 2023-11-15T00:00:00Z
	rec := func(host string, ts int64) *xtcp_flat_record.XtcpFlatRecord {
		return &xtcp_flat_record.XtcpFlatRecord{Hostname: host, TimestampNs: ts}
	}
	return []rowRoundTripCase{
		{"empty_envelope", "B", "an envelope with zero rows",
			func(tb testing.TB) []byte { return delimited(tb) },
			rowRoundTripWant{rows: 0}},
		{"sentinel_all_fields", "P", "every field carries a distinct sentinel; every column must come back equal",
			func(tb testing.TB) []byte { return delimited(tb, sentinelRecord(tb)) },
			rowRoundTripWant{rows: 1, eventDates: []string{"2023-11-14"}}},
		{"three_rows", "P", "three rows with distinct hostnames keep their order",
			func(tb testing.TB) []byte {
				return delimited(tb, rec("a", 1), rec("b", 2), rec("c", 3))
			},
			rowRoundTripWant{rows: 3}},
		{"hostile_strings", "N", "ESC, U+FFFD, C1 and a 1 kB string in string fields round-trip byte for byte",
			func(tb testing.TB) []byte {
				r := rec("\x1b[2J\x1b]0;x\x07", midnight)
				r.Netns = "�\u0085"
				r.Label = strings.Repeat("L", 1024)
				r.ContainerName = "web\x00null"
				return delimited(tb, r)
			},
			rowRoundTripWant{rows: 1, eventDates: []string{"2023-11-15"}}},
		{"invalid_utf8_rejected", "N", "invalid UTF-8 in a proto3 string fails the unmarshal, so the worker drops the envelope",
			invalidUTF8Envelope,
			rowRoundTripWant{unmarshalErr: true}},
		{"truncated_prefix", "N", "a varint length prefix cut short",
			func(testing.TB) []byte { return []byte{0x80} },
			rowRoundTripWant{unmarshalErr: true}},
		{"length_over_body", "N", "the prefix says 1 MB but only 10 bytes follow",
			func(testing.TB) []byte {
				b := binary.AppendUvarint(nil, 1<<20)
				return append(b, make([]byte, 10)...)
			},
			rowRoundTripWant{unmarshalErr: true}},
		{"zero_timestamp", "B", "timestamp_ns 0 is the Unix epoch",
			func(tb testing.TB) []byte { return delimited(tb, rec("z", 0)) },
			rowRoundTripWant{rows: 1, eventDates: []string{"1970-01-01"}}},
		{"max_timestamp", "B", "timestamp_ns MaxInt64 formats without a panic",
			func(tb testing.TB) []byte { return delimited(tb, rec("m", math.MaxInt64)) },
			rowRoundTripWant{rows: 1, eventDates: []string{"2262-04-11"}}},
		{"negative_timestamp", "C", "a negative timestamp_ns is before the epoch",
			func(tb testing.TB) []byte { return delimited(tb, rec("n", -1)) },
			rowRoundTripWant{rows: 1, eventDates: []string{"1969-12-31"}}},
		{"day_boundary", "C", "rows 1 ns either side of midnight UTC get different event dates",
			func(tb testing.TB) []byte {
				return delimited(tb, rec("a", midnight-1), rec("b", midnight))
			},
			rowRoundTripWant{rows: 2, eventDates: []string{"2023-11-14", "2023-11-15"}}},
		{"real_dump", "P", "records decoded from the captured netlink dump",
			realDumpEnvelope,
			rowRoundTripWant{rows: -1}}, // -1: as many as the dump holds
	}
}

func TestRowFromProtoRoundTrip(t *testing.T) {
	for _, tc := range rowRoundTripCases() {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.envelope(t)
			written, read, err := roundTripEnvelope(t, data)
			if (err != nil) != tc.expected.unmarshalErr {
				t.Fatalf("%s: unmarshal err %v, expected error %v", tc.description, err, tc.expected.unmarshalErr)
			}
			if err != nil {
				return
			}
			checkRoundTrip(t, written, read)
			if tc.expected.rows >= 0 && len(read) != tc.expected.rows {
				t.Fatalf("%s: %d rows, expected %d", tc.description, len(read), tc.expected.rows)
			}
			if tc.expected.rows < 0 && len(read) == 0 {
				t.Fatalf("%s: no rows", tc.description)
			}
			for i, want := range tc.expected.eventDates {
				if read[i].EventDate != want {
					t.Fatalf("%s: row %d EventDate %q, expected %q", tc.description, i, read[i].EventDate, want)
				}
			}
			if tc.name == "three_rows" {
				for i, want := range []string{"a", "b", "c"} {
					if read[i].Hostname != want {
						t.Fatalf("%s: row %d Hostname %q, expected %q", tc.description, i, read[i].Hostname, want)
					}
				}
			}
		})
	}
}

// FuzzRowFromProto feeds arbitrary bytes to the S3 worker's row path. Rows
// come from tenant-influenced records (container names, netns names,
// interface names), so any envelope that decodes must write to Parquet and
// read back unchanged.
//
//	go test -tags dest_s3parquet -run '^$' -fuzz '^FuzzRowFromProto$' -fuzztime=30s ./pkg/xtcp/
func FuzzRowFromProto(f *testing.F) {
	for _, tc := range rowRoundTripCases() {
		f.Add(tc.envelope(f))
	}
	// The golden capture: rows from a real poll with containers.
	if b := goldenSeed(f); b != nil {
		f.Add(b)
	}
	// A bare record, not delimited: the fuzzer can learn the framing from it.
	if b, err := proto.Marshal(&xtcp_flat_record.Envelope{}); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		written, read, err := roundTripEnvelope(t, data)
		if err != nil {
			return
		}
		checkRoundTrip(t, written, read)
	})
}
