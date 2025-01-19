package xtcp

import (
	"bytes"
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
)

func (x *XTCP) InitMarshallers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.Marshalers.Store("proto", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoMarshal(e)
	})

	x.Marshalers.Store("protojson", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoJsonMarshal(e)
	})

	x.Marshalers.Store("prototext", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoTextMarshal(e)
	})

	x.Marshalers.Store("msgpack", func(e *xtcp_flat_record.Envelope) (buf *[]byte) {
		return x.protoMsgPackMarshal(e)
	})

	if f, ok := x.Marshalers.Load(x.config.MarshalTo); ok {
		x.Marshaler = f.(func(e *xtcp_flat_record.Envelope) (buf *[]byte))
	} else {
		log.Fatalf("InitMarshalers XTCP Marshal must be one of proto, protojson, or prototext:%s", x.config.MarshalTo)
	}
}

// protoMarshal marshals to protobuf and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
func (x *XTCP) protoMarshal(e *xtcp_flat_record.Envelope) (buf *[]byte) {

	var myBuf []byte
	buffer := bytes.NewBuffer(myBuf)

	// https://pkg.go.dev/google.golang.org/protobuf@v1.36.3/encoding/protodelim#MarshalTo
	n, err := protodelim.MarshalTo(buffer, e)
	if err != nil {
		x.pC.WithLabelValues("protoMarshal", "MarshalTo", "error").Inc()
		if x.debugLevel > 10 {
			log.Println("protodelim.MarshalTo() err: ", err)
		}
	}

	if x.debugLevel > 10 {
		log.Printf("protodelim.MarshalTo() n:%d", n)
	}

	// b, err := proto.Marshal(e)
	// if err != nil {
	// 	x.pC.WithLabelValues("protoMarshal", "Marshal", "error").Inc()
	// 	if x.debugLevel > 1000 {
	// 		log.Println("proto.Marshal(x) err: ", err)
	// 	}
	// }
	// buf = &b

	b := buffer.Bytes()
	buf = &b

	return buf
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
