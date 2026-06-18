package xtcp

import (
	"bytes"
	"encoding/csv"
	"log"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/encoding/protojson"
)

// registerTextEnvelopeMarshallers wires the line/tabular envelope marshallers
// (jsonl, csv, tsv) into x.EnvelopeMarshallers. Each is an envelope marshaller
// that iterates Envelope.Row and owns its trailing-newline framing (writerDest
// and the tcp/http sinks write bytes verbatim).
//
// csv/tsv resolve their column set from -columns once here; an invalid spec
// fatals at init only when one of them is the selected format, so a stray
// -columns alongside protoJson is harmless.
func (x *XTCP) registerTextEnvelopeMarshallers() {
	x.EnvelopeMarshallers.Store(MarshallerJSONL, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeJSONLMarshal(e)
	})

	cols := flatColumns()
	if x.config.MarshalTo == MarshallerCSV || x.config.MarshalTo == MarshallerTSV {
		c, err := selectColumns(x.config.CsvColumns)
		if err != nil {
			x.callFatalf("InitEnvelopeMarshallers -columns: %v", err)
			return
		}
		cols = c
	}

	// Separate header-written guards so csv and tsv each emit their header
	// exactly once per process on whichever stream they feed.
	var csvHeader, tsvHeader atomic.Bool
	x.EnvelopeMarshallers.Store(MarshallerCSV, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeDelimitedMarshal(e, cols, ',', &csvHeader)
	})
	x.EnvelopeMarshallers.Store(MarshallerTSV, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeDelimitedMarshal(e, cols, '\t', &tsvHeader)
	})
}

// envelopeJSONLMarshal emits one compact JSON object per row, each on its own
// line (NDJSON / ClickHouse JSONEachRow). Values are raw (machine) — use
// csv/tsv for humanized addresses/states.
func (x *XTCP) envelopeJSONLMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	var b bytes.Buffer
	for _, r := range e.Row {
		line, err := protojson.Marshal(r)
		if err != nil {
			x.pC.WithLabelValues("envelopeJSONLMarshal", "Marshal", "error").Inc()
			if x.debugLevel > 10 {
				log.Println("envelopeJSONLMarshal protojson.Marshal err: ", err)
			}
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	out := b.Bytes()
	return &out
}

// envelopeDelimitedMarshal renders the envelope's rows as delimited text
// (CSV or TSV depending on comma), humanized, with the header written once.
// encoding/csv already terminates every record with '\n', so the block is
// self-framing.
func (x *XTCP) envelopeDelimitedMarshal(e *xtcp_flat_record.Envelope, cols []flatCol, comma rune, headerWritten *atomic.Bool) (buf *[]byte) {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.Comma = comma

	if headerWritten.CompareAndSwap(false, true) {
		if err := w.Write(flatRecordHeader(cols)); err != nil {
			x.pC.WithLabelValues("envelopeDelimitedMarshal", "header", "error").Inc()
			if x.debugLevel > 10 {
				log.Println("envelopeDelimitedMarshal header err: ", err)
			}
		}
	}

	for _, r := range e.Row {
		if err := w.Write(flatRecordValues(r, cols, true)); err != nil {
			x.pC.WithLabelValues("envelopeDelimitedMarshal", "row", "error").Inc()
			if x.debugLevel > 10 {
				log.Println("envelopeDelimitedMarshal row err: ", err)
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		x.pC.WithLabelValues("envelopeDelimitedMarshal", "flush", "error").Inc()
		if x.debugLevel > 10 {
			log.Println("envelopeDelimitedMarshal flush err: ", err)
		}
	}

	out := b.Bytes()
	return &out
}
