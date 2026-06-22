package recordfmt

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"slices"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/encoding/protowire"
)

// Framing note: the text/line formats below terminate their output with a
// newline so consecutive flushes stay separated on a stream (one object/row
// per line); the binary formats (protobufList, msgpack) do not. Sinks write
// the bytes verbatim — framing is the marshaller's job.

// MarshalEnvelopeProtobufList encodes the Envelope as length-delimited
// protobuf — varint(size) || bytes — exactly what ClickHouse's ProtobufList
// input format reads. Binary; no trailing newline.
func MarshalEnvelopeProtobufList(e *xtcp_flat_record.Envelope) ([]byte, error) {
	return AppendEnvelopeProtobufList(nil, e)
}

// AppendEnvelopeProtobufList appends the length-delimited Envelope encoding to
// dst and returns the extended slice. Lets a caller reuse a pooled buffer
// (pass dst[:0]) on a hot path; pass nil for a fresh allocation.
//
// Uses vtprotobuf's reflection-free SizeVT/MarshalToSizedBufferVT: write the
// varint length prefix, grow dst by exactly that many bytes, then marshal the
// Envelope into the tail in place. The output is identical to the protobuf
// runtime's length-delimited encoding (the ClickHouse ProtobufList contract).
func AppendEnvelopeProtobufList(dst []byte, e *xtcp_flat_record.Envelope) ([]byte, error) {
	size := e.SizeVT()
	dst = protowire.AppendVarint(dst, uint64(size))
	n := len(dst)
	dst = slices.Grow(dst, size)[:n+size]
	if _, err := e.MarshalToSizedBufferVT(dst[n : n+size]); err != nil {
		return dst[:n], fmt.Errorf("recordfmt: Envelope.MarshalToSizedBufferVT: %w", err)
	}
	return dst, nil
}

// MarshalEnvelopeJSON encodes the whole Envelope as one compact JSON object,
// newline-terminated.
func MarshalEnvelopeJSON(e *xtcp_flat_record.Envelope) ([]byte, error) {
	b, err := protojson.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: protojson.Marshal(envelope): %w", err)
	}
	return append(b, '\n'), nil
}

// MarshalEnvelopeText encodes the whole Envelope as protobuf text,
// newline-terminated.
func MarshalEnvelopeText(e *xtcp_flat_record.Envelope) ([]byte, error) {
	return append([]byte(prototext.Format(e)), '\n'), nil
}

// MarshalEnvelopeMsgPack encodes the whole Envelope as MessagePack. Binary.
func MarshalEnvelopeMsgPack(e *xtcp_flat_record.Envelope) ([]byte, error) {
	b, err := msgpack.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: msgpack.Marshal(envelope): %w", err)
	}
	return b, nil
}

// MarshalEnvelopeJSONL encodes each row as a compact JSON object on its own
// line (NDJSON). Raw/machine values.
func MarshalEnvelopeJSONL(e *xtcp_flat_record.Envelope) ([]byte, error) {
	var b bytes.Buffer
	for _, r := range e.GetRow() {
		line, err := MarshalJSON(r)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// MarshalEnvelopeHumanizedJSONL encodes each row as a humanized JSON object on
// its own line (NDJSON with readable addresses/state/congestion/timestamp).
func MarshalEnvelopeHumanizedJSONL(e *xtcp_flat_record.Envelope) ([]byte, error) {
	var b bytes.Buffer
	for _, r := range e.GetRow() {
		line, err := MarshalHumanizedJSON(r)
		if err != nil {
			return nil, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// MarshalEnvelopeTable encodes the rows as delimited text (CSV when comma is
// ',', TSV when '\t'), humanized. When includeHeader is set the header row is
// written first. encoding/csv terminates every record with '\n'.
func MarshalEnvelopeTable(e *xtcp_flat_record.Envelope, cols []Column, comma rune, includeHeader bool) ([]byte, error) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = comma
	if includeHeader {
		if err := w.Write(Header(cols)); err != nil {
			return nil, fmt.Errorf("recordfmt: csv header: %w", err)
		}
	}
	for _, r := range e.GetRow() {
		if err := w.Write(Row(r, cols, true)); err != nil {
			return nil, fmt.Errorf("recordfmt: csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("recordfmt: csv flush: %w", err)
	}
	return b.Bytes(), nil
}
