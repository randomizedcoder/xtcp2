package xtcp_flat_record

// Differential conformance tests: the vtprotobuf-generated, reflection-free
// path (MarshalVT/SizeVT/UnmarshalVT) must agree with the canonical protobuf
// runtime (proto.Marshal/proto.Size/proto.Unmarshal) for every message.
//
// xtcp2 serializes the production Kafka/ClickHouse path with vtprotobuf
// (pkg/recordfmt), so a vtprotobuf upgrade that ever diverged from the runtime
// — wrong size, different bytes, a decode mismatch — would silently corrupt the
// wire format. These tests pay the reflection price (test-only) to catch that.
//
// For each message we assert:
//   1. SizeVT() == proto.Size()
//   2. len(MarshalVT()) == SizeVT()
//   3. MarshalVT() is byte-identical to the runtime's deterministic Marshal
//      (the ClickHouse ProtobufList contract)
//   4. every (encoder, decoder) pairing — {vt,runtime} × {vt,runtime} —
//      round-trips back to a message equal to the original.

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// vtMessage is the method set vtprotobuf generates, plus proto.Message.
// *XtcpFlatRecord and *Envelope both satisfy it.
type vtMessage interface {
	proto.Message
	MarshalVT() ([]byte, error)
	SizeVT() int
	UnmarshalVT([]byte) error
}

// diffCheck asserts vtprotobuf and the protobuf runtime agree for one message.
// newEmpty returns a fresh zero message of the same concrete type, used as the
// target for the unmarshal comparisons.
func diffCheck[T vtMessage](t *testing.T, label string, msg T, newEmpty func() T) {
	t.Helper()

	vtSize := msg.SizeVT()
	if refSize := proto.Size(msg); vtSize != refSize {
		t.Errorf("%s: SizeVT()=%d, proto.Size()=%d", label, vtSize, refSize)
	}

	vtBytes, err := msg.MarshalVT()
	if err != nil {
		t.Fatalf("%s: MarshalVT: %v", label, err)
	}
	// Deterministic so field ordering is directly comparable.
	refBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	if err != nil {
		t.Fatalf("%s: proto.Marshal: %v", label, err)
	}

	if len(vtBytes) != vtSize {
		t.Errorf("%s: len(MarshalVT)=%d, SizeVT()=%d", label, len(vtBytes), vtSize)
	}
	if !bytes.Equal(vtBytes, refBytes) {
		t.Errorf("%s: MarshalVT bytes differ from proto.Marshal\n vt =%x\n ref=%x", label, vtBytes, refBytes)
	}

	// Every encoder × decoder pairing must reproduce the original.
	for _, src := range []struct {
		name string
		data []byte
	}{{"vtBytes", vtBytes}, {"refBytes", refBytes}} {
		viaRef := newEmpty()
		if err := proto.Unmarshal(src.data, viaRef); err != nil {
			t.Errorf("%s: proto.Unmarshal(%s): %v", label, src.name, err)
		} else if !proto.Equal(msg, viaRef) {
			t.Errorf("%s: proto.Unmarshal(%s) != original", label, src.name)
		}

		viaVT := newEmpty()
		if err := viaVT.UnmarshalVT(src.data); err != nil {
			t.Errorf("%s: UnmarshalVT(%s): %v", label, src.name, err)
		} else if !proto.Equal(msg, viaVT) {
			t.Errorf("%s: UnmarshalVT(%s) != original", label, src.name)
		}
	}
}

// setField sets fd on m to a value for its kind. rng==nil yields a deterministic
// non-zero value (used to populate every field); otherwise a random one,
// including range extremes so varint widths vary. Strings stay ASCII (proto3
// requires valid UTF-8) and floats stay finite (NaN breaks proto.Equal).
func setField(m protoreflect.Message, fd protoreflect.FieldDescriptor, rng *rand.Rand, seed int) {
	i64 := func() int64 {
		if rng == nil {
			return int64(seed + 1)
		}
		return int64(rng.Uint64())
	}
	u64 := func() uint64 {
		if rng == nil {
			return uint64(seed + 1)
		}
		return rng.Uint64()
	}
	switch fd.Kind() {
	case protoreflect.BoolKind:
		m.Set(fd, protoreflect.ValueOfBool(true))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		m.Set(fd, protoreflect.ValueOfInt32(int32(i64())))
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		m.Set(fd, protoreflect.ValueOfInt64(i64()))
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		m.Set(fd, protoreflect.ValueOfUint32(uint32(u64())))
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		m.Set(fd, protoreflect.ValueOfUint64(u64()))
	case protoreflect.FloatKind:
		m.Set(fd, protoreflect.ValueOfFloat32(float32(seed)+0.5))
	case protoreflect.DoubleKind:
		m.Set(fd, protoreflect.ValueOfFloat64(float64(i64())+0.25))
	case protoreflect.StringKind:
		m.Set(fd, protoreflect.ValueOfString(fmt.Sprintf("field-%d-%s", seed, string(rune('a'+seed%26)))))
	case protoreflect.BytesKind:
		m.Set(fd, protoreflect.ValueOfBytes([]byte{byte(seed), 0x00, 0xAB, 0xFF, byte(seed * 7)}))
	case protoreflect.EnumKind:
		vals := fd.Enum().Values()
		idx := seed % vals.Len()
		if rng != nil {
			idx = rng.Intn(vals.Len())
		}
		m.Set(fd, protoreflect.ValueOfEnum(vals.Get(idx).Number()))
	default:
		// XtcpFlatRecord is flat scalars only. If a future schema adds a
		// message/repeated/map field, fail loudly so this test is extended
		// to populate it rather than silently skipping coverage.
		panic(fmt.Sprintf("setField: unhandled kind %s for %s", fd.Kind(), fd.FullName()))
	}
}

// fullRecord sets every field of XtcpFlatRecord, so the wire format is
// exercised end to end.
func fullRecord() *XtcpFlatRecord {
	m := &XtcpFlatRecord{}
	refl := m.ProtoReflect()
	fds := refl.Descriptor().Fields()
	for i := 0; i < fds.Len(); i++ {
		setField(refl, fds.Get(i), nil, i)
	}
	return m
}

// randomRecord randomly populates a subset of fields, exercising
// presence/absence combinations and a range of values.
func randomRecord(rng *rand.Rand) *XtcpFlatRecord {
	m := &XtcpFlatRecord{}
	refl := m.ProtoReflect()
	fds := refl.Descriptor().Fields()
	for i := 0; i < fds.Len(); i++ {
		if rng.Intn(3) == 0 {
			continue // leave zero → proto3 omits it
		}
		setField(refl, fds.Get(i), rng, i)
	}
	return m
}

func TestVTProtoConformance_XtcpFlatRecord_fixtures(t *testing.T) {
	newEmpty := func() *XtcpFlatRecord { return &XtcpFlatRecord{} }
	cases := map[string]*XtcpFlatRecord{
		"empty": {},
		"minimal": {Hostname: "h", InetDiagMsgState: 1,
			CongestionAlgorithmEnum: XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC},
		"ipv4": {InetDiagMsgFamily: 2,
			InetDiagMsgSocketSource: []byte{10, 0, 0, 5}, InetDiagMsgSocketSourcePort: 443},
		"ipv6": {InetDiagMsgFamily: 10,
			InetDiagMsgSocketSource: bytes.Repeat([]byte{0xfe, 0x80}, 8)},
		"maxvals": {TcpInfoBytesAcked: ^uint64(0), TcpInfoMaxPacingRate: ^uint64(0),
			TimestampNs: 1.7976931348623157e308},
		"full": fullRecord(),
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) { diffCheck(t, name, m, newEmpty) })
	}
}

func TestVTProtoConformance_Envelope_fixtures(t *testing.T) {
	newEmpty := func() *Envelope { return &Envelope{} }
	cases := map[string]*Envelope{
		"empty":  {},
		"oneRow": {Row: []*XtcpFlatRecord{fullRecord()}},
		"mixed": {Row: []*XtcpFlatRecord{
			fullRecord(), {}, {Hostname: "b"}, fullRecord(),
		}},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) { diffCheck(t, name, m, newEmpty) })
	}
}

func TestVTProtoConformance_random(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) // fixed seed → deterministic
	const iters = 500
	for i := range iters {
		rec := randomRecord(rng)
		diffCheck(t, fmt.Sprintf("record#%d", i), rec, func() *XtcpFlatRecord { return &XtcpFlatRecord{} })

		// also wrap a random number of records in an Envelope
		env := &Envelope{}
		for j := 0; j < rng.Intn(5); j++ {
			env.Row = append(env.Row, randomRecord(rng))
		}
		diffCheck(t, fmt.Sprintf("envelope#%d", i), env, func() *Envelope { return &Envelope{} })
	}
}
