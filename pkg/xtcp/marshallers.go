package xtcp

import (
	"log"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/pkg/recordfmt"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// Canonical Marshaller names. Aliased to recordfmt's format constants so the
// daemon (-marshal) and the xtcp2client (-format) share one source of truth;
// the byte production lives in pkg/recordfmt, these wrappers add the daemon's
// buffer pooling, prometheus error counting, and debug logging.
const (
	MarshallerProtobufList = recordfmt.FormatProtobufList
	MarshallerProtoJSON    = recordfmt.FormatProtoJSON
	MarshallerProtoText    = recordfmt.FormatProtoText
	MarshallerMsgPack      = recordfmt.FormatMsgPack
	MarshallerJSONL        = recordfmt.FormatJSONL    // one JSON record per line (NDJSON / ClickHouse JSONEachRow)
	MarshallerCSV          = recordfmt.FormatCSV      // comma-separated, humanized, header once
	MarshallerTSV          = recordfmt.FormatTSV      // tab-separated, humanized, header once
	MarshallerHumanize     = recordfmt.FormatHumanize // one humanized JSON record per line
)

// Envelope-size safety valves. Two independent thresholds — the
// first to trip wins. Row count is the primary knob: cheap (O(1) per
// append) and predictable. The byte cap is a secondary safety net for
// pathological per-record sizes (a record with a huge bytes field)
// because the row count alone won't catch those.
//
// Note: the byte cap measures the UNCOMPRESSED serialized bytes.
// franz-go applies ZSTD/LZ4/Snappy after handoff, so the actual
// on-wire Kafka message is typically 3-8x smaller. Treat the bytes
// cap as a conservative upper bound, not the wire size.
//
// The byte total is tracked incrementally (see envelopeRowBytes /
// XTCP.currentEnvelopeBytes): each append adds the row's exact
// contribution, so the cap is O(1) per append instead of an
// O(message size) proto.Size walk over the whole growing envelope.
const (
	EnvelopeFlushThresholdBytesCst = 768 * 1024
	EnvelopeFlushThresholdRowsCst  = 10000

	// envelopeRowFieldNumber is the field number of `repeated XtcpFlatRecord
	// row = 10` in the Envelope message. Used to compute each row's wire
	// contribution to proto.Size(Envelope).
	envelopeRowFieldNumber = 10
)

// envelopeRowBytes returns one row's exact contribution to
// proto.Size(Envelope): the repeated-field tag + the length prefix + the
// row's own serialized size. Because Envelope has only the `row` field,
// summing this over all rows equals proto.Size(Envelope) exactly, so the
// running total drives the byte-cap with no per-check reflection walk.
func envelopeRowBytes(r *xtcp_flat_record.XtcpFlatRecord) int {
	return protowire.SizeTag(envelopeRowFieldNumber) + protowire.SizeBytes(proto.Size(r))
}

var (
	// validMarshallersMap is the union of per-record (protoJson, protoText,
	// msgpack — debug formats) and per-envelope (protobufList — production
	// wire format; plus jsonl/csv/tsv/humanize) marshaller names.
	validMarshallersMap = map[string]bool{
		MarshallerProtobufList: true, // https://clickhouse.com/docs/en/interfaces/formats/ProtobufList
		MarshallerProtoJSON:    true,
		MarshallerProtoText:    true,
		MarshallerMsgPack:      true,
		MarshallerJSONL:        true,
		MarshallerCSV:          true,
		MarshallerTSV:          true,
		MarshallerHumanize:     true,
	}
)

func validMarshallers() (marshallers string) {
	for key := range validMarshallersMap {
		marshallers = marshallers + key + ","
	}
	return strings.TrimSuffix(marshallers, ",")
}

// marshalErr records a marshaller error: bumps the prometheus counter and logs
// at debug. Centralizes the daemon-side error handling the recordfmt library
// deliberately leaves to its callers.
func (x *XTCP) marshalErr(op string, err error) {
	x.pC.WithLabelValues(op, "marshal", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("%s: %v", op, err)
	}
}

func (x *XTCP) InitMarshallers(wg *sync.WaitGroup) {

	defer wg.Done()

	if _, ok := validMarshallersMap[x.config.MarshalTo]; !ok {
		x.callFatalf("InitMarshallers XTCP MarshalTo invalid:%s, must be one of:%s", x.config.MarshalTo, validMarshallers())
		return
	}

	x.Marshallers.Store(MarshallerProtoJSON, func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
		return x.protoJsonMarshal(r)
	})

	x.Marshallers.Store(MarshallerProtoText, func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
		return x.protoTextMarshal(r)
	})

	x.Marshallers.Store(MarshallerMsgPack, func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
		return x.protoMsgPackMarshal(r)
	})

	// protobufList is per-envelope, handled in InitEnvelopeMarshallers.
	// A lookup miss here is expected and not an error.
	if f, ok := x.Marshallers.Load(x.config.MarshalTo); ok {
		if m, ok2 := f.(func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte)); ok2 {
			x.Marshaller = m
		}
	}
}

// InitEnvelopeMarshallers registers per-Envelope marshallers and stores the
// chosen function in x.EnvelopeMarshaller. The destination pipeline is
// envelope-based, so every -marshal value resolves here.
//
// Any destination is permitted: kafka receives the bytes via Produce, null
// discards them, other destinations get the marshalled batch as one record.
func (x *XTCP) InitEnvelopeMarshallers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.EnvelopeMarshallers.Store(MarshallerProtobufList, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protobufListMarshal(e)
	})
	x.EnvelopeMarshallers.Store(MarshallerProtoJSON, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeProtoJSONMarshal(e)
	})
	x.EnvelopeMarshallers.Store(MarshallerProtoText, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeProtoTextMarshal(e)
	})
	x.EnvelopeMarshallers.Store(MarshallerMsgPack, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.envelopeMsgPackMarshal(e)
	})

	// jsonl / csv / tsv / humanize — the line/tabular analysis formats
	// (see marshallers_text.go). All delegate to pkg/recordfmt.
	x.registerTextEnvelopeMarshallers()

	if f, ok := x.EnvelopeMarshallers.Load(x.config.MarshalTo); ok {
		if m, ok2 := f.(func(e *xtcp_flat_record.Envelope) (buf *[]byte)); ok2 {
			x.EnvelopeMarshaller = m
		}
	}
}

// protobufListMarshal marshals an Envelope as length-delimited protobuf into a
// pooled buffer (the production hot path). ClickHouse's
// kafka_format='ProtobufList' expects exactly this on the wire.
func (x *XTCP) protobufListMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	buf = x.destBytesPool.Get()
	b, err := recordfmt.AppendEnvelopeProtobufList((*buf)[:0], e)
	if err != nil {
		x.marshalErr("protobufListMarshal", err)
	}
	*buf = b
	return buf
}

func (x *XTCP) envelopeProtoJSONMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b, err := recordfmt.MarshalEnvelopeJSON(e)
	if err != nil {
		x.marshalErr("envelopeProtoJSONMarshal", err)
	}
	return &b
}

func (x *XTCP) envelopeProtoTextMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b, err := recordfmt.MarshalEnvelopeText(e)
	if err != nil {
		x.marshalErr("envelopeProtoTextMarshal", err)
	}
	return &b
}

func (x *XTCP) envelopeMsgPackMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b, err := recordfmt.MarshalEnvelopeMsgPack(e)
	if err != nil {
		x.marshalErr("envelopeMsgPackMarshal", err)
	}
	return &b
}

func (x *XTCP) protoJsonMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b, err := recordfmt.MarshalJSON(r)
	if err != nil {
		x.marshalErr("protoJsonMarshal", err)
	}
	return &b
}

func (x *XTCP) protoTextMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b, err := recordfmt.MarshalText(r)
	if err != nil {
		x.marshalErr("protoTextMarshal", err)
	}
	return &b
}

func (x *XTCP) protoMsgPackMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b, err := recordfmt.MarshalMsgPack(r)
	if err != nil {
		x.marshalErr("protoMsgPackMarshal", err)
	}
	return &b
}
