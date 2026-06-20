package xtcp

import (
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/recordfmt"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// registerTextEnvelopeMarshallers wires the line/tabular envelope marshallers
// (jsonl, humanize, csv, tsv) into x.EnvelopeMarshallers. Each delegates to
// pkg/recordfmt and adds the daemon's error counting. csv/tsv resolve their
// column set from -columns once here; an invalid spec fatals at init only when
// one of them is the selected format, so a stray -columns alongside protoJson
// is harmless. The header is emitted once per process (atomic guard).
func (x *XTCP) registerTextEnvelopeMarshallers() {
	x.EnvelopeMarshallers.Store(MarshallerJSONL, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		b, err := recordfmt.MarshalEnvelopeJSONL(e)
		if err != nil {
			x.marshalErr("envelopeJSONLMarshal", err)
		}
		return &b
	})
	x.EnvelopeMarshallers.Store(MarshallerHumanize, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		b, err := recordfmt.MarshalEnvelopeHumanizedJSONL(e)
		if err != nil {
			x.marshalErr("envelopeHumanizedJSONLMarshal", err)
		}
		return &b
	})

	cols := recordfmt.AllColumns()
	if x.config.MarshalTo == MarshallerCSV || x.config.MarshalTo == MarshallerTSV {
		c, err := recordfmt.SelectColumns(x.config.CsvColumns)
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
		b, err := recordfmt.MarshalEnvelopeTable(e, cols, ',', csvHeader.CompareAndSwap(false, true))
		if err != nil {
			x.marshalErr("envelopeCSVMarshal", err)
		}
		return &b
	})
	x.EnvelopeMarshallers.Store(MarshallerTSV, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		b, err := recordfmt.MarshalEnvelopeTable(e, cols, '\t', tsvHeader.CompareAndSwap(false, true))
		if err != nil {
			x.marshalErr("envelopeTSVMarshal", err)
		}
		return &b
	})
}
