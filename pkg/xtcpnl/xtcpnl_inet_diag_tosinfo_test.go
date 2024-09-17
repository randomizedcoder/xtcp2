package xtcpnl

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
			filename:    "./testdata/6_6_44/attribute_tos",
			tos:         TypeOfService(0),
			Func: func(data []byte, tos *TypeOfService) (n int, err error) {
				return DeserializeTypeOfService(data, tos)
			},
		},
		{
			description: "attribute_tos_reflection",
			filename:    "./testdata/6_6_44/attribute_tos",
			tos:         TypeOfService(0),
			Func: func(data []byte, tos *TypeOfService) (n int, err error) {
				return DeserializeTypeOfServiceReflection(data, tos)
			},
		},
		{
			description: "attribute_tos2",
			filename:    "./testdata/6_6_44/attribute_tos2",
			tos:         TypeOfService(2),
			Func: func(data []byte, tos *TypeOfService) (n int, err error) {
				return DeserializeTypeOfService(data, tos)
			},
		},
		{
			description: "attribute_tos2_reflection",
			filename:    "./testdata/6_6_44/attribute_tos2",
			tos:         TypeOfService(2),
			Func: func(data []byte, tos *TypeOfService) (n int, err error) {
				return DeserializeTypeOfServiceReflection(data, tos)
			},
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
		//t.Logf("i:%d, n:%d", i, n)

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
	f := func(data []byte, tos *TypeOfService) (n int, err error) {
		return DeserializeTypeOfService(data, tos)
	}
	DeserializeTypeOfServiceBoth(b, f)
}

func BenchmarkDeserializeTypeOfServiceReflection(b *testing.B) {
	f := func(data []byte, tos *TypeOfService) (n int, err error) {
		return DeserializeTypeOfServiceReflection(data, tos)
	}
	DeserializeTypeOfServiceBoth(b, f)
}

func DeserializeTypeOfServiceBoth(b *testing.B, Func func(data []byte, tos *TypeOfService) (n int, err error)) {
	var tests = []DeserializeTypeOfServiceTest{
		{
			description: "attribute_tos2",
			filename:    "./testdata/6_6_44/attribute_tos2",
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
		//_, errD = DeserializeMemInfoBoth(buf, rta)
		_, errD = Func(buf, tos)
		if errD != nil {
			b.Error("Test Failed DeserializeTypeOfServiceBoth errD", errD)
		}

	}
	resultTOS = *tos
}
