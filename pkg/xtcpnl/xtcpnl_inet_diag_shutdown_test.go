package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeShutdownTest struct {
	description string
	filename    string
	s           Shutdown
	Func        func(data []byte, s *Shutdown) (n int, err error)
}

// TestDeserializeShutdown
// go test --run TestDeserializeShutdown
func TestDeserializeShutdown(t *testing.T) {
	var tests = []DeserializeShutdownTest{
		{
			description: "attribute_shutdown",
			filename:    "./testdata/6_6_44/attribute_shutdown",
			s:           Shutdown(0),
			Func: func(data []byte, s *Shutdown) (n int, err error) {
				return DeserializeShutdown(data, s)
			},
		},
		{
			description: "attribute_shutdown_reflection",
			filename:    "./testdata/6_6_44/attribute_shutdown",
			s:           Shutdown(0),
			Func: func(data []byte, s *Shutdown) (n int, err error) {
				return DeserializeShutdownReflection(data, s)
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

		s := new(Shutdown)

		_, errD := test.Func(buf, s)
		if errD != nil {
			t.Fatal("Test Failed DeserializeShutdown errD", errD)
		}

		//t.Logf("i:%d, n:%d", i, n)

		if !reflect.DeepEqual(*s, test.s) {
			t.Errorf("Test %d %s !reflect.DeepEqual(s:%x, test.test.s:%x)", i, test.description, s, test.s)
		}

	}
}

var (
	resultShutdown Shutdown
)

// go test -bench=BenchmarkDeserializeShutdown
func BenchmarkDeserializeShutdown(b *testing.B) {
	f := func(data []byte, s *Shutdown) (n int, err error) {
		return DeserializeShutdown(data, s)
	}
	DeserializeShutdownBoth(b, f)
}

func BenchmarkDeserializeShutdownReflection(b *testing.B) {
	f := func(data []byte, s *Shutdown) (n int, err error) {
		return DeserializeShutdownReflection(data, s)
	}
	DeserializeShutdownBoth(b, f)
}

func DeserializeShutdownBoth(b *testing.B, Func func(data []byte, s *Shutdown) (n int, err error)) {
	var tests = []DeserializeShutdownTest{
		{
			description: "attribute_shutdown",
			filename:    "./testdata/6_6_44/attribute_shutdown",
			s:           Shutdown(2),
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	s := new(Shutdown)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, s)
		if errD != nil {
			b.Error("Test Failed DeserializeShutdownBoth errD", errD)
		}

	}
	resultShutdown = *s
}
