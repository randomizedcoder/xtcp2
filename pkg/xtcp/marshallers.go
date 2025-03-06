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

var (
	//protoSingle, protoDelim, protoJson, protoText, msgpack
	validMarshallersMap = map[string]bool{
		// "protobuf":       true, // https://clickhouse.com/docs/en/interfaces/formats/Protobuf
		// "protobufSingle": true, // https://clickhouse.com/docs/en/interfaces/formats/ProtobufSingle
		"protobufList": true, // https://clickhouse.com/docs/en/interfaces/formats/ProtobufList
		"protoJson":    true,
		"protoText":    true,
		"msgpack":      true,
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
		log.Fatalf("InitMarshallers XTCP MarshalTo invalid:%s, must be one of:%s", x.config.MarshalTo, validMarshallers())
	}

	// x.Marshallers.Store("protobuf", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	// 	return x.protobufMarshal(e)
	// })

	// x.Marshallers.Store("protobufSingle", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	// 	return x.protobufSingleMarshal(e)
	// })

	x.Marshallers.Store("protobufList", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protobufListMarshal(e)
	})

	x.Marshallers.Store("protoJson", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoJsonMarshal(e)
	})

	x.Marshallers.Store("protoText", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoTextMarshal(e)
	})

	x.Marshallers.Store("msgpack", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoMsgPackMarshal(e)
	})

	if f, ok := x.Marshallers.Load(x.config.MarshalTo); ok {
		x.Marshaller = f.(func(e *xtcp_flat_record.Envelope) (buf *[]byte))
	} else {
		log.Fatalf("InitMarshalers XTCP Marshal must be one of protoSingle, protoDelim, protoJson, protoText, msgpack:%s", x.config.MarshalTo)
	}
}

// // protobufMarshal marshals to protobuf and does error handling
// // this is the length delimited protobuf
// // https://clickhouse.com/docs/en/interfaces/formats#protobuf
// // https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
// func (x *XTCP) protobufMarshal(e *xtcp_flat_record.Envelope) (bufPtr *[]byte) {

// 	buf := x.destBytesPool.Get().([]byte)

// 	writer := &ByteSliceWriter{Buf: &buf}

// 	// https://pkg.go.dev/google.golang.org/protobuf@v1.36.3/encoding/protodelim#MarshalTo
// 	n, err := protodelim.MarshalTo(writer, e)
// 	if err != nil {
// 		x.pC.WithLabelValues("protoMarshal", "MarshalTo", "error").Inc()
// 		if x.debugLevel > 10 {
// 			log.Println("protodelim.MarshalTo() err: ", err)
// 		}
// 	}

// 	if x.debugLevel > 10 {
// 		log.Printf("protodelim.MarshalTo() n:%d", n)
// 	}

// 	bufPtr = writer.Buf

// 	return bufPtr
// }

// // protobufSingleMarshal marshals to protobuf and does error handling
// // this does NOT have the length varint in front
// // https://clickhouse.com/docs/en/interfaces/formats#protobufsingle
// // https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
// func (x *XTCP) protobufSingleMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {

// 	b, err := proto.Marshal(e)
// 	if err != nil {
// 		x.pC.WithLabelValues("protoMarshal", "Marshal", "error").Inc()
// 		if x.debugLevel > 1000 {
// 			log.Println("proto.Marshal(x) err: ", err)
// 		}
// 	}
// 	buf = &b

// 	return buf
// }

type ByteSliceWriter struct {
	Buf *[]byte
}

func (w *ByteSliceWriter) Write(b []byte) (n int, err error) {
	*w.Buf = append(*w.Buf, b...)
	return len(b), nil
}

func (x *XTCP) protobufListMarshal(e *xtcp_flat_record.Envelope) (bufPtr *[]byte) {

	buf := x.destBytesPool.Get().([]byte)

	writer := &ByteSliceWriter{Buf: &buf}

	// https://pkg.go.dev/google.golang.org/protobuf@v1.36.3/encoding/protodelim#MarshalTo
	n, err := protodelim.MarshalTo(writer, e)
	if err != nil {
		x.pC.WithLabelValues("protoMarshal", "MarshalTo", "error").Inc()
		if x.debugLevel > 10 {
			log.Println("protodelim.MarshalTo() err: ", err)
		}
	}

	if x.debugLevel > 10 {
		log.Printf("protobufListMarshal protodelim.MarshalTo() n:%d", n)
	}

	bufPtr = writer.Buf

	return bufPtr
}

// protoJsonMarshal marshals to json and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
func (x *XTCP) protoJsonMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b := []byte(protojson.Format(e))
	buf = &b
	return buf
}

// protoTextMarshal marshals to json and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/encoding/prototext#Marshal
func (x *XTCP) protoTextMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b := []byte(prototext.Format(e))
	buf = &b
	return buf
}

// protoMsgPackMarshal marshals to MsgPack and does error handling
// Please note this uses reflection and so is likely to be pretty slow...
// https://msgpack.uptrace.dev/
// https://github.com/msgpack/msgpack
// TODO look at https://github.com/shamaton/msgpackgen for high performance msgpack
func (x *XTCP) protoMsgPackMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {
	b, err := msgpack.Marshal(e)
	if err != nil {
		x.pC.WithLabelValues("protoMsgPackMarshal", "Marshal", "error").Inc()
		if x.debugLevel > 1000 {
			log.Println("protoMsgPackMarshal err: ", err)
		}
	}
	buf = &b
	return buf
}
