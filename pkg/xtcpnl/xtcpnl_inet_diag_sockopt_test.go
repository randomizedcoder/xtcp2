package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeSockOptTest struct {
	description string
	filename    string
	s           *SockOpt
	Func        func(data []byte, s *SockOpt) (n int, err error)
}

// TestDeserializeSockOpt
// go test --run TestDeserializeSockOpt
func TestDeserializeSockOpt(t *testing.T) {
	t1 := SockOpt(82)
	t2 := SockOpt(82)
	t3 := SockOpt(82)
	var tests = []DeserializeSockOptTest{
		{
			description: "attribute_sockopt",
			filename:    "./testdata/6_10_3/attribute_sockopt_4305",
			s:           &t1,
			Func: func(data []byte, s *SockOpt) (n int, err error) {
				return DeserializeSockOpt(data, s)
			},
		},
		{
			description: "attribute_sockopt_reflection",
			filename:    "./testdata/6_10_3/attribute_sockopt_4305",
			s:           &t2,
			Func: func(data []byte, s *SockOpt) (n int, err error) {
				return DeserializeSockOptReflection(data, s)
			},
		},
		{
			description: "attribute_sockopt_5200",
			filename:    "./testdata/6_10_3/attribute_sockopt_5200",
			s:           &t3,
			Func: func(data []byte, s *SockOpt) (n int, err error) {
				return DeserializeSockOpt(data, s)
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

		s := new(SockOpt)

		_, errD := test.Func(buf, s)
		if errD != nil {
			t.Fatal("Test Failed DeserializeSockOpt errD", errD)
		}
		//t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*s, *test.s) {
			t.Errorf("Test %d %s !reflect.DeepEqual(*s:%x:%d, *test.s:%x:%d)", i, test.description, *s, *s, *test.s, *test.s)
		}

	}
}

var (
	resultSockOpt SockOpt
)

// go test -bench=BenchmarkDeserializeSockOpt
func BenchmarkDeserializeSockOpt(b *testing.B) {
	f := func(data []byte, s *SockOpt) (n int, err error) {
		return DeserializeSockOpt(data, s)
	}
	DeserializeSockOptBoth(b, f)
}

func BenchmarkDeserializeSockOptReflection(b *testing.B) {
	f := func(data []byte, s *SockOpt) (n int, err error) {
		return DeserializeSockOptReflection(data, s)
	}
	DeserializeSockOptBoth(b, f)
}

func DeserializeSockOptBoth(b *testing.B, Func func(data []byte, tc *SockOpt) (n int, err error)) {
	var tests = []DeserializeSockOptTest{
		{
			description: "attribute_sockopt",
			filename:    "./testdata/6_10_3/attribute_sockopt_4305",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	c := new(SockOpt)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, c)
		if errD != nil {
			b.Error("Test Failed DeserializeSockOptBoth errD", errD)
		}
	}
	resultSockOpt = *c
}
