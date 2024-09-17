package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeTrafficClassTest struct {
	description string
	filename    string
	tc          TrafficClass
	Func        func(data []byte, tc *TrafficClass) (n int, err error)
}

// TestDeserializeTrafficClass
// go test --run TestDeserializeTrafficClass
func TestDeserializeTrafficClass(t *testing.T) {
	var tests = []DeserializeTrafficClassTest{
		{
			description: "attribute_tcclass",
			filename:    "./testdata/6_6_44/attribute_tcclass",
			tc:          TrafficClass(2),
			Func: func(data []byte, tc *TrafficClass) (n int, err error) {
				return DeserializeTrafficClass(data, tc)
			},
		},
		{
			description: "attribute_tcclass_reflection",
			filename:    "./testdata/6_6_44/attribute_tcclass",
			tc:          TrafficClass(2),
			Func: func(data []byte, tc *TrafficClass) (n int, err error) {
				return DeserializeTrafficClassReflection(data, tc)
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

		tc := new(TrafficClass)

		_, errD := test.Func(buf, tc)
		if errD != nil {
			t.Fatal("Test Failed DeserializeTypeOfService errD", errD)
		}

		//t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*tc, test.tc) {
			t.Errorf("Test %d %s !reflect.DeepEqual(tc:%x, test.test.tc:%x)", i, test.description, tc, test.tc)
		}

	}
}

var (
	resultTC TrafficClass
)

// go test -bench=BenchmarkDeserializeTrafficClass
func BenchmarkDeserializeTrafficClass(b *testing.B) {
	f := func(data []byte, tc *TrafficClass) (n int, err error) {
		return DeserializeTrafficClass(data, tc)
	}
	DeserializeTrafficClassBoth(b, f)
}

func BenchmarkDeserializeTrafficClassReflection(b *testing.B) {
	f := func(data []byte, tc *TrafficClass) (n int, err error) {
		return DeserializeTrafficClassReflection(data, tc)
	}
	DeserializeTrafficClassBoth(b, f)
}

func DeserializeTrafficClassBoth(b *testing.B, Func func(data []byte, tc *TrafficClass) (n int, err error)) {
	var tests = []DeserializeTrafficClassTest{
		{
			description: "attribute_tcclass",
			filename:    "./testdata/6_6_44/attribute_tcclass",
			tc:          TrafficClass(2),
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	tc := new(TrafficClass)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, tc)
		if errD != nil {
			b.Error("Test Failed DeserializeTrafficClassBoth errD", errD)
		}
	}
	resultTC = *tc
}
