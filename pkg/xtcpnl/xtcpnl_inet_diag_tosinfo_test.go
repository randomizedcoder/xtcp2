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

type DeserializeTypeOfServiceTest struct {
	description string
	filename    string
	tos         TypeOfService
	Func        func(data []byte, tos *TypeOfService) (n int, err error)
}

// TestDeserializeTypeOfService
// go test --run TestDeserializeTypeOfService
func TestDeserializeTypeOfService(t *testing.T) {
	var tests = []DeserializeTypeOfServiceTest{
		{
			description: "attribute_tos",
			filename:    tdAttrTos_6_6_44,
			tos:         TypeOfService(0),
			Func:        DeserializeTypeOfService,
		},
		{
			description: "attribute_tos_reflection",
			filename:    tdAttrTos_6_6_44,
			tos:         TypeOfService(0),
			Func:        deserializeTypeOfServiceReflection,
		},
		{
			description: "attribute_tos2",
			filename:    tdAttrTos2_6_6_44,
			tos:         TypeOfService(2),
			Func:        DeserializeTypeOfService,
		},
		{
			description: "attribute_tos2_reflection",
			filename:    tdAttrTos2_6_6_44,
			tos:         TypeOfService(2),
			Func:        deserializeTypeOfServiceReflection,
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

		tos := new(TypeOfService)

		_, errD := test.Func(buf, tos)
		if errD != nil {
			t.Fatal("Test Failed DeserializeTypeOfService errD", errD)
		}
		// t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*tos, test.tos) {
			t.Errorf("Test %d %s !reflect.DeepEqual(tos:%x, test.test.tos:%x)", i, test.description, tos, test.tos)
		}

	}
}

var (
	resultTOS TypeOfService
)

// go test -bench=BenchmarkDeserializeTypeOfService
func BenchmarkDeserializeTypeOfService(b *testing.B) {
	DeserializeTypeOfServiceBoth(b, DeserializeTypeOfService)
}

func BenchmarkDeserializeTypeOfServiceReflection(b *testing.B) {
	DeserializeTypeOfServiceBoth(b, deserializeTypeOfServiceReflection)
}

func DeserializeTypeOfServiceBoth(b *testing.B, fn func(data []byte, tos *TypeOfService) (n int, err error)) {
	var tests = []DeserializeTypeOfServiceTest{
		{
			description: "attribute_tos2",
			filename:    tdAttrTos2_6_6_44,
			tos:         TypeOfService(2),
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	tos := new(TypeOfService)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// _, errD = DeserializeMemInfoBoth(buf, rta)
		_, errD = fn(buf, tos)
		if errD != nil {
			b.Error("Test Failed DeserializeTypeOfServiceBoth errD", errD)
		}

	}
	resultTOS = *tos
}
