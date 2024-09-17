package xtcpnl

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
			filename:    "./testdata/6_6_44/attribute_class_id",
			c:           ClassID(0),
			Func: func(data []byte, c *ClassID) (n int, err error) {
				return DeserializeClassID(data, c)
			},
		},
		{
			description: "attribute_class_id_reflection",
			filename:    "./testdata/6_6_44/attribute_class_id",
			c:           ClassID(0),
			Func: func(data []byte, c *ClassID) (n int, err error) {
				return DeserializeClassIDReflection(data, c)
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

		c := new(ClassID)

		_, errD := test.Func(buf, c)
		if errD != nil {
			t.Fatal("Test Failed DeserializeClassID errD", errD)
		}
		//t.Logf("i:%d, n:%d", i, n)

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
	f := func(data []byte, c *ClassID) (n int, err error) {
		return DeserializeClassID(data, c)
	}
	DeserializeClassIDBoth(b, f)
}

func BenchmarkDeserializeClassIDReflection(b *testing.B) {
	f := func(data []byte, c *ClassID) (n int, err error) {
		return DeserializeClassIDReflection(data, c)
	}
	DeserializeClassIDBoth(b, f)
}

func DeserializeClassIDBoth(b *testing.B, Func func(data []byte, tc *ClassID) (n int, err error)) {
	var tests = []DeserializeClassIDTest{
		{
			description: "attribute_tcclass",
			filename:    "./testdata/6_6_44/attribute_tcclass",
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
		_, errD = Func(buf, c)
		if errD != nil {
			b.Error("Test Failed DeserializeClassIDBoth errD", errD)
		}
	}
	resultClassID = *c
}
