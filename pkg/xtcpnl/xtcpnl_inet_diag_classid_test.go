package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeClassIDTest struct {
	description string
	filename    string
	c           ClassID
	Func        func(data []byte, c *ClassID) (n int, err error)
}

// TestDeserializeClassID
// go test --run TestDeserializeClassID
func TestDeserializeClassID(t *testing.T) {
	var tests = []DeserializeClassIDTest{
		{
			description: "attribute_class_id",
			filename:    tdAttrClassID_6_6_44,
			c:           ClassID(0),
			Func:        DeserializeClassID,
		},
		{
			description: "attribute_class_id_reflection",
			filename:    tdAttrClassID_6_6_44,
			c:           ClassID(0),
			Func:        deserializeClassIDReflection,
		},
	}
	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Error("Test Failed Open error:", err)
		}
		defer f.Close()

		bs, err := io.ReadAll(f)
		if err != nil {
			t.Error("Test Failed ReadAll error:", err)
		}

		// t.Logf("i:%d, binary.Size(bs):%d", i, binary.Size(bs))
		// t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs))

		buf := bs[RTAttrSizeCst:]

		// t.Logf("i:%d, binary.Size(buf):%d", i, binary.Size(buf))
		// t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf))

		c := new(ClassID)

		_, errD := test.Func(buf, c)
		if errD != nil {
			t.Fatal("Test Failed DeserializeClassID errD", errD)
		}
		// t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*c, test.c) {
			t.Errorf("Test %d %s !reflect.DeepEqual(c:%x, test.test.c:%x)", i, test.description, c, test.c)
		}

	}
}

var (
	resultClassID ClassID
)

// go test -bench=BenchmarkDeserializeClassID
func BenchmarkDeserializeClassID(b *testing.B) {
	DeserializeClassIDBoth(b, DeserializeClassID)
}

func BenchmarkDeserializeClassIDReflection(b *testing.B) {
	DeserializeClassIDBoth(b, deserializeClassIDReflection)
}

func DeserializeClassIDBoth(b *testing.B, fn func(data []byte, tc *ClassID) (n int, err error)) {
	var tests = []DeserializeClassIDTest{
		{
			description: tnAttrTcclass,
			filename:    tdAttrTcclass_6_6_44,
			c:           ClassID(2),
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	c := new(ClassID)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = fn(buf, c)
		if errD != nil {
			b.Error("Test Failed DeserializeClassIDBoth errD", errD)
		}
	}
	resultClassID = *c
}
