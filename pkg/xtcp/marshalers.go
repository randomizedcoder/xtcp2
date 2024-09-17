package xtcp

import (
	"log"

	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

func (x *XTCP) InitMarshalers() {

	x.Marshalers.Store("proto", func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
		return x.protoMarshal(xtcpRecord)
	})

	x.Marshalers.Store("protojson", func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
		return x.protoJsonMarshal(xtcpRecord)
	})

	x.Marshalers.Store("prototext", func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
		return x.protoTextMarshal(xtcpRecord)
	})

	x.Marshalers.Store("msgpack", func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
		return x.protoMsgPackMarshal(xtcpRecord)
	})

	if f, ok := x.Marshalers.Load(*x.config.Marshal); ok {
		x.Marshaler = f.(func(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte))
	} else {
		log.Fatalf("InitMarshalers XTCP Marshal must be one of proto, protojson, or prototext:%s", *x.config.Marshal)
	}
}

// protoMarshal marshals to protobuf and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
func (x *XTCP) protoMarshal(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
	b, err := proto.Marshal(xtcpRecord)
	if err != nil {
		x.pC.WithLabelValues("protoMarshal", "Marshal", "error").Inc()
		if x.debugLevel > 1000 {
			log.Println("proto.Marshal(x) err: ", err)
		}
	}
	buf = &b
	return buf
}

// protoJsonMarshal marshals to json and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/proto?tab=doc#Marshal
func (x *XTCP) protoJsonMarshal(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
	b := []byte(protojson.Format(xtcpRecord))
	buf = &b
	return buf
}

// protoTextMarshal marshals to json and does error handling
// https://pkg.go.dev/google.golang.org/protobuf/encoding/prototext#Marshal
func (x *XTCP) protoTextMarshal(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
	b := []byte(prototext.Format(xtcpRecord))
	buf = &b
	return buf
}

// protoMsgPackMarshal marshals to MsgPack and does error handling
// Please note this uses reflection and so is likely to be pretty slow...
// https://msgpack.uptrace.dev/
// https://github.com/msgpack/msgpack
func (x *XTCP) protoMsgPackMarshal(xtcpRecord *xtcppb.FlatXtcpRecord) (buf *[]byte) {
	b, err := msgpack.Marshal(xtcpRecord)
	if err != nil {
		x.pC.WithLabelValues("protoMsgPackMarshal", "Marshal", "error").Inc()
		if x.debugLevel > 1000 {
			log.Println("protoMsgPackMarshal err: ", err)
		}
	}
	buf = &b
	return buf
}

// TODO look at https://github.com/shamaton/msgpackgen for high performance msgpack
