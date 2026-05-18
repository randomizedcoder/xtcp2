package xtcp

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// DeserializeAttribute: dispatches on Type using x.RTATypeDeserializer.
// Two branches: known type (call deserializer) and unknown type (skip).

func TestDeserializeAttribute_known(t *testing.T) {
	called := 0
	x := &XTCP{
		RTATypeDeserializer: map[int]func(buf []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error){
			42: func([]byte, *xtcp_flat_record.XtcpFlatRecord) error {
				called++
				return nil
			},
		},
	}
	rec := &xtcp_flat_record.XtcpFlatRecord{}
	err := x.DeserializeAttribute(DeserializeAttributeArgs{
		Type: 42, buf: []byte{0, 0}, xtcpRecord: rec,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if called != 1 {
		t.Errorf("deserializer called %d times, want 1", called)
	}
}

func TestDeserializeAttribute_unknown(t *testing.T) {
	x := &XTCP{
		RTATypeDeserializer: map[int]func(buf []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error){},
		debugLevel:          1500, // hit the debugLevel > 1000 log branch
	}
	rec := &xtcp_flat_record.XtcpFlatRecord{}
	err := x.DeserializeAttribute(DeserializeAttributeArgs{
		Type: 999, buf: []byte{}, xtcpRecord: rec,
	})
	if err != nil {
		t.Errorf("unknown-type DeserializeAttribute should not error, got %v", err)
	}
}
