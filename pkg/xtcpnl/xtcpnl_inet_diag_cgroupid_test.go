package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeCGroupIDTest struct {
	description string
	filename    string
	c           *CGroupID
	Func        func(data []byte, c *CGroupID) (n int, err error)
}

// TestDeserializeCGroupID
// go test --run TestDeserializeCGroupID
func TestDeserializeCGroupID(t *testing.T) {
	x := CGroupID(4354)
	y := CGroupID(3945)

	var tests = []DeserializeCGroupIDTest{
		{
			description: "attribute_cgroup_id",
			filename:    "./testdata/6_6_44/attribute_cgroup_id",
			c:           &x,
			// c: &CGroupID{
			// 	ID: 0,
			// },
			Func: func(data []byte, c *CGroupID) (n int, err error) {
				return DeserializeCGroupID(data, c)
			},
		},
		{
			description: "attribute_cgroup_id",
			filename:    "./testdata/6_6_44/attribute_cgroup_id",
			c:           &x,
			// c: &CGroupID{
			// 	ID: 0,
			// },
			Func: func(data []byte, c *CGroupID) (n int, err error) {
				return DeserializeCGroupIDReflection(data, c)
			},
		},
		{
			description: "attribute_cgroup_id_21",
			filename:    "./testdata/6_10_3/attribute_cgroup_21",
			c:           &y,
			// c: &CGroupID{
			// 	ID: 21,
			// },
			Func: func(data []byte, c *CGroupID) (n int, err error) {
				return DeserializeCGroupID(data, c)
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

		c := new(CGroupID)

		_, errD := test.Func(buf, c)
		if errD != nil {
			t.Fatal("Test Failed DeserializeCGroupID errD", errD)
		}
		//t.Logf("i:%d, n:%d", i, n)

		if !reflect.DeepEqual(*c, *test.c) {
			t.Errorf("Test %d %s !reflect.DeepEqual(*c:%x:%d, *test.c:%x:%d)", i, test.description, *c, *c, *test.c, *test.c)
		}

	}
}

var (
	resultCGroupID CGroupID
)

// go test -bench=BenchmarkDeserializeCGroupID
func BenchmarkDeserializeCGroupID(b *testing.B) {
	f := func(data []byte, c *CGroupID) (n int, err error) {
		return DeserializeCGroupID(data, c)
	}
	DeserializeCGroupIDBoth(b, f)
}

func BenchmarkDeserializeCGroupIDReflection(b *testing.B) {
	f := func(data []byte, c *CGroupID) (n int, err error) {
		return DeserializeCGroupIDReflection(data, c)
	}
	DeserializeCGroupIDBoth(b, f)
}

func DeserializeCGroupIDBoth(b *testing.B, Func func(data []byte, tc *CGroupID) (n int, err error)) {
	var tests = []DeserializeCGroupIDTest{
		{
			description: "attribute_cgroup_id",
			filename:    "./testdata/6_6_44/attribute_cgroup_id",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	c := new(CGroupID)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, c)
		if errD != nil {
			b.Error("Test Failed DeserializeCGroupIDBoth errD", errD)
		}
	}
	resultCGroupID = *c
}
