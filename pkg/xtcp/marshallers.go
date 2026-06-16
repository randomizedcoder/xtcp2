package xtcp

import (
	"log"
	"strings"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
)

// Canonical Marshaller names — referenced by tests and config-validation
// alike, kept here so a typo in any one site is a compile error.
const (
	MarshallerProtobufList = "protobufList"
	MarshallerProtoJSON    = "protoJson"
	MarshallerProtoText    = "protoText"
	MarshallerMsgPack      = "msgpack"
)

// Envelope-size safety valves. Two independent thresholds — the
// first to trip wins. Row count is the primary knob: cheap (O(1) per
// append) and predictable. proto.Size is a secondary safety net for
// pathological per-record sizes (a record with a huge bytes field)
// because the row count alone won't catch those.
//
// Note: proto.Size measures the UNCOMPRESSED serialized bytes.
// franz-go applies ZSTD/LZ4/Snappy after handoff, so the actual
// on-wire Kafka message is typically 3-8x smaller. Treat the bytes
// cap as a conservative upper bound, not the wire size.
//
// proto.Size is O(message size) so we only call it every Nth append,
// mirroring the `Modulus` pattern used elsewhere in this package.
const (
	EnvelopeFlushThresholdBytesCst = 768 * 1024
	EnvelopeFlushThresholdRowsCst  = 10000
	envelopeSizeCheckModulus       = 64
)

var (
	// validMarshallersMap is the union of per-record (protoJson,
	// protoText, msgpack — debug formats) and per-envelope (protobufList
	// — production wire format) marshaller names. InitMarshallers and
	// InitEnvelopeMarshallers each only populate their own registry; the
	// per-record map will miss the protobufList key on purpose.
	validMarshallersMap = map[string]bool{
		MarshallerProtobufList: true, // https://clickhouse.com/docs/en/interfaces/formats/ProtobufList
		MarshallerProtoJSON:    true,
		MarshallerProtoText:    true,
		MarshallerMsgPack:      true,
	}
)

func validMarshallers() (marshallers string) {
	for key := range validMarshallersMap {
		marshallers = marshallers + key + ","
	}
	return strings.TrimSuffix(marshallers, ",")
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
		x.Marshaller, _ = f.(func(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte)) //nolint:errcheck // Marshallers.Store sites all use this signature
	}
}

// InitEnvelopeMarshallers registers per-Envelope marshallers and stores
// the chosen function in x.EnvelopeMarshaller. Currently the only entry
// is protobufList — additional batched formats would register here.
//
// Any destination is permitted: kafka receives the bytes via Produce,
// null discards them (used in tests and -dest null deployments), other
// destinations get the length-delimited Envelope as one record. A
// downstream consumer that expects per-record bytes won't decode this
// correctly, but that's a deployment choice, not a daemon-side guard.
func (x *XTCP) InitEnvelopeMarshallers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.EnvelopeMarshallers.Store(MarshallerProtobufList, func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protobufListMarshal(e)
	})

	if f, ok := x.EnvelopeMarshallers.Load(x.config.MarshalTo); ok {
		x.EnvelopeMarshaller, _ = f.(func(e *xtcp_flat_record.Envelope) (buf *[]byte)) //nolint:errcheck // EnvelopeMarshallers.Store sites all use this signature
	}
}

// protobufListMarshal marshals an Envelope as length-delimited protobuf:
// varint(envelope_size) || envelope_bytes. ClickHouse's
// kafka_format='ProtobufList' expects exactly this on the wire. No
// Confluent schema-registry header is prepended; schema-registry
// registration in destinations_kafka is informational only (ClickHouse
// does not consult the registry to decode messages).
// https://clickhouse.com/docs/en/interfaces/formats#protobuflist
func (x *XTCP) protobufListMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {

	buf = x.destBytesPool.Get()
	*buf = (*buf)[:0]

	writer := &ByteSliceWriter{Buf: buf}
	if _, err := protodelim.MarshalTo(writer, e); err != nil {
		x.pC.WithLabelValues("protoMarshal", "MarshalTo", "error").Inc()
		if x.debugLevel > 10 {
			log.Println("protodelim.MarshalTo(envelope) err: ", err)
		}
	}

	return buf
}

type ByteSliceWriter struct {
	Buf *[]byte
}

func (w *ByteSliceWriter) Write(b []byte) (n int, err error) {
	*w.Buf = append(*w.Buf, b...)
	return len(b), nil
}

// protoJsonMarshal marshals to JSON.
// https://pkg.go.dev/google.golang.org/protobuf/encoding/protojson
func (x *XTCP) protoJsonMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b := []byte(protojson.Format(r))
	buf = &b
	return buf
}

// protoTextMarshal marshals to prototext.
// https://pkg.go.dev/google.golang.org/protobuf/encoding/prototext
func (x *XTCP) protoTextMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b := []byte(prototext.Format(r))
	buf = &b
	return buf
}

// protoMsgPackMarshal marshals to MsgPack via reflection.
// https://msgpack.uptrace.dev/
// TODO consider https://github.com/shamaton/msgpackgen for codegen-based throughput.
func (x *XTCP) protoMsgPackMarshal(r *xtcp_flat_record.XtcpFlatRecord) (buf *[]byte) {
	b, err := msgpack.Marshal(r)
	if err != nil {
		x.pC.WithLabelValues("protoMsgPackMarshal", "Marshal", "error").Inc()
		if x.debugLevel > 1000 {
			log.Println("protoMsgPackMarshal err: ", err)
		}
	}
	buf = &b
	return buf
}
