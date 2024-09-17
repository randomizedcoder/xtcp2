package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializeVegasInfoTest struct {
	description string
	filename    string
	vi          VegasInfo
	Func        func(data []byte, vi *VegasInfo) (n int, err error)
}

// TestDeserializeVegasInfo - unforntunately, most examples I have a zeros :(
// go test --run TestDeserializeVegasInfo
func TestDeserializeVegasInfo(t *testing.T) {
	var tests = []DeserializeVegasInfoTest{
		//vegasinfo
		{
			description: "attribute_vegasinfo",
			filename:    "./testdata/6_6_44/attribute_vegasinfo",
			vi: VegasInfo{
				Enabled: 1,
				RttCnt:  0,
				Rtt:     211,
				MinRtt:  2147483647,
			},
			Func: func(data []byte, vi *VegasInfo) (n int, err error) {
				return DeserializeVegasInfo(data, vi)
			},
		},
		{
			description: "attribute_vegasinfo",
			filename:    "./testdata/6_6_44/attribute_vegasinfo",
			vi: VegasInfo{
				Enabled: 1,
				RttCnt:  0,
				Rtt:     211,
				MinRtt:  2147483647,
			},
			Func: func(data []byte, vi *VegasInfo) (n int, err error) {
				return DeserializeVegasInfoReflection(data, vi)
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

		vi := new(VegasInfo)

		_, errD := test.Func(buf, vi)
		if errD != nil {
			t.Fatal("Test Failed TestDeserializeVegasInfo errD", errD)
		}

		if vi.Enabled != test.vi.Enabled {
			t.Errorf("Test %d %s vi.Enabled:%d != test.vi.Enabled:%d", i, test.description, vi.Enabled, test.vi.Enabled)
		}

		if vi.RttCnt != test.vi.RttCnt {
			t.Errorf("Test %d %s vi.RttCnt:%d != test.vi.RttCnt:%d", i, test.description, vi.RttCnt, test.vi.RttCnt)
		}

		if vi.Rtt != test.vi.Rtt {
			t.Errorf("Test %d %s vi.Rtt:%d != test.vi.Rtt:%d", i, test.description, vi.Rtt, test.vi.Rtt)
		}

		if vi.MinRtt != test.vi.MinRtt {
			t.Errorf("Test %d %s vi.MinRtt:%d != test.vi.MinRtt:%d", i, test.description, vi.MinRtt, test.vi.MinRtt)
		}

	}
}

var (
	resultVI VegasInfo
)

// go test -bench=BenchmarkDeserializeVegasInfo
func BenchmarkDeserializeVegasInfo(b *testing.B) {
	f := func(data []byte, vi *VegasInfo) (n int, err error) {
		return DeserializeVegasInfo(data, vi)
	}
	DeserializeVegasInfoBoth(b, f)
}

func BenchmarkDeserializeVegasInfoReflection(b *testing.B) {
	f := func(data []byte, vi *VegasInfo) (n int, err error) {
		return DeserializeVegasInfoReflection(data, vi)
	}
	DeserializeVegasInfoBoth(b, f)
}

func DeserializeVegasInfoBoth(b *testing.B, Func func(data []byte, vi *VegasInfo) (n int, err error)) {
	var tests = []DeserializeVegasInfoTest{
		{
			description: "attribute_vegasinfo",
			filename:    "./testdata/6_6_44/attribute_vegasinfo",
			vi: VegasInfo{
				Enabled: 1,
				RttCnt:  0,
				Rtt:     211,
				MinRtt:  2147483647,
			},
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	vi := new(VegasInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeMemInfoBoth(buf, rta)
		_, errD = Func(buf, vi)
		if errD != nil {
			b.Error("Test Failed DeserializeMemInfoBoth errD", errD)
		}

	}
	resultVI = *vi

	// if resultMI.Fmem != test.mi.Fmem {
	// 	b.Errorf("Test %s resultMI.Fmem:%d != test.mi.Fmem:%d", test.description, resultMI.Fmem, test.mi.Fmem)
	// }
}
