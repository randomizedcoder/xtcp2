package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializePragueInfoTest struct {
	description string
	filename    string
	p           PragueInfo
	Func        func(data []byte, p *PragueInfo) (n int, err error)
}

// TestDeserializePragueInfo - I don't have example data, so these tests don't do anything.  FIX ME!!
// go test --run TestDeserializePragueInfo
func TestDeserializePragueInfo(t *testing.T) {
	var tests = []DeserializePragueInfoTest{
		{
			description: "attribute_pragueinfo_fake_fixme",
			filename:    "./testdata/attribute_pragueinfo_fake_fixme",
			p: PragueInfo{
				Alpha:     0,
				FracCwnd:  0,
				RateBytes: 0,
				MaxBurst:  0,
				Round:     0,
				RttTarget: 0,
			},
			Func: func(data []byte, p *PragueInfo) (n int, err error) {
				return DeserializePragueInfo(data, p)
			},
		},
		{
			description: "attribute_pragueinfo_fake_fixme_reflection",
			filename:    "./testdata/attribute_pragueinfo_fake_fixme",
			p: PragueInfo{
				Alpha:     0,
				FracCwnd:  0,
				RateBytes: 0,
				MaxBurst:  0,
				Round:     0,
				RttTarget: 0,
			},
			Func: func(data []byte, p *PragueInfo) (n int, err error) {
				return DeserializePragueInfoReflection(data, p)
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

		p := new(PragueInfo)

		_, errD := test.Func(buf, p)
		if errD != nil {
			t.Fatal("Test Failed DeserializePragueInfo errD", errD)
		}

		if p.Alpha != test.p.Alpha {
			t.Errorf("Test %d %s p.Alpha:%d != test.p.Alpha:%d", i, test.description, p.Alpha, test.p.Alpha)
		}

		if p.FracCwnd != test.p.FracCwnd {
			t.Errorf("Test %d %s p.FracCwnd:%d != test.p.FracCwnd:%d", i, test.description, p.FracCwnd, test.p.FracCwnd)
		}

		if p.RateBytes != test.p.RateBytes {
			t.Errorf("Test %d %s p.RateBytes:%d != test.p.RateBytes:%d", i, test.description, p.RateBytes, test.p.RateBytes)
		}

		if p.MaxBurst != test.p.MaxBurst {
			t.Errorf("Test %d %s p.MaxBurst:%d != test.p.MaxBurst:%d", i, test.description, p.MaxBurst, test.p.MaxBurst)
		}

		if p.Round != test.p.Round {
			t.Errorf("Test %d %s p.Round:%d != test.p.Round:%d", i, test.description, p.Round, test.p.Round)
		}

		if p.RttTarget != test.p.RttTarget {
			t.Errorf("Test %d %s p.RttTarget:%d != test.p.RttTarget:%d", i, test.description, p.RttTarget, test.p.RttTarget)
		}

	}
}

var (
	resultPragueInfo PragueInfo
)

// go test -bench=BenchmarkDeserializeMemInfo

func BenchmarkDeserializePragueInfo(b *testing.B) {
	f := func(data []byte, p *PragueInfo) (n int, err error) {
		return DeserializePragueInfo(data, p)
	}
	DeserializePragueInfoBoth(b, f)
}

func BenchmarkDeserializePragueInfoReflection(b *testing.B) {
	f := func(data []byte, p *PragueInfo) (n int, err error) {
		return DeserializePragueInfoReflection(data, p)
	}
	DeserializePragueInfoBoth(b, f)
}

func DeserializePragueInfoBoth(b *testing.B, Func func(data []byte, d *PragueInfo) (n int, err error)) {
	var tests = []DeserializePragueInfoTest{
		{
			description: "attribute_pragueinfo_fake_fixme",
			filename:    "./testdata/attribute_pragueinfo_fake_fixme",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	p := new(PragueInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		_, errD = Func(buf, p)
		if errD != nil {
			b.Error("Test Failed DeserializePragueInfoBoth errD", errD)
		}

	}
	resultPragueInfo = *p

}
