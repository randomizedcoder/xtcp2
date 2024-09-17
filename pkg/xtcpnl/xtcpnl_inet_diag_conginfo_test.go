package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

type DeserializeCongInfoTest struct {
	description string
	filename    string
	ci          CongInfo
	cong        string
	Func        func(data []byte, ci *CongInfo) (n int, err error)
}

// TestDeserializeCongInfo
// go test --run TestDeserializeCongInfo
func TestDeserializeCongInfo(t *testing.T) {
	var tests = []DeserializeCongInfoTest{
		{
			description: "attribute_cong_cubic",
			filename:    "./testdata/6_10_3/attribute_cong_cubic",
			ci: CongInfo{
				Cong: []byte{0x63, 0x75, 0x62, 0x69, 0x63, 0x0},
			},
			cong: "cubic\x00",
			Func: func(data []byte, ci *CongInfo) (n int, err error) {
				return DeserializeCongInfo(data, ci)
			},
		},
		{
			description: "attribute_cong_bbr",
			filename:    "./testdata/6_6_44/attribute_cong_bbr",
			ci: CongInfo{
				Cong: []byte{0x62, 0x62, 0x72, 0x00},
			},
			cong: "bbr\x00",
			Func: func(data []byte, ci *CongInfo) (n int, err error) {
				return DeserializeCongInfo(data, ci)
			},
		},
		{
			description: "attribute_cong_vegas",
			filename:    "./testdata/6_6_44/attribute_cong_vegas",
			ci: CongInfo{
				Cong: []byte{0x76, 0x65, 0x67, 0x61, 0x73, 0x00},
			},
			cong: "vegas\x00",
			Func: func(data []byte, ci *CongInfo) (n int, err error) {
				return DeserializeCongInfo(data, ci)
			},
		},
		{
			description: "attribute_cong_dctcp",
			filename:    "./testdata/6_6_44/attribute_cong_dctcp",
			ci: CongInfo{
				Cong: []byte{0x64, 0x63, 0x74, 0x63, 0x70, 0x00},
			},
			cong: "dctcp\x00",
			Func: func(data []byte, ci *CongInfo) (n int, err error) {
				return DeserializeCongInfo(data, ci)
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

		ci := new(CongInfo)

		_, errD := test.Func(buf, ci)
		if errD != nil {
			t.Fatal("Test Failed DeserializeMemInfo errD", errD)
		}
		//t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(ci.Cong, test.ci.Cong) {
			t.Errorf("Test %d %s !reflect.DeepEqual(ci.Cong:%x, test.ci.Cong:%x)", i, test.description, ci.Cong, test.ci.Cong)
		}

		str := string(ci.Cong[:])
		//if str != test.cong {
		if strings.Compare(str, test.cong) != 0 {
			//t.Errorf("Test %d %s str:%sX != test.cong:%sX", i, test.description, str, test.cong)
			t.Errorf("Test %d %s strings.Compare(str:%s, test.cong:%s)!=0:%d, len(str):%d, len(test.cong):%d", i, test.description, str, test.cong, strings.Compare(str, test.cong), len(str), len(test.cong))
		}

	}
}

var (
	resultCI CongInfo
)

// go test -bench=BenchmarkDeserializeCongInfo
func BenchmarkDeserializeCongInfo(b *testing.B) {
	f := func(data []byte, ci *CongInfo) (n int, err error) {
		return DeserializeCongInfo(data, ci)
	}
	DeserializeCongInfoBoth(b, f)
}

// func BenchmarkDeserializeCongInfoReflection(b *testing.B) {
// 	f := func(data []byte, ci *CongInfo) (n int, err error) {
// 		return DeserializeCongInfoReflection(data, ci)
// 	}
// 	DeserializeCongInfoBoth(b, f)
// }

func DeserializeCongInfoBoth(b *testing.B, Func func(data []byte, ci *CongInfo) (n int, err error)) {
	var tests = []DeserializeCongInfoTest{
		{
			description: "attribute_cong_cubic",
			filename:    "./testdata/6_10_3/attribute_cong_cubic",
			ci: CongInfo{
				Cong: []byte{0x63, 0x75, 0x62, 0x69, 0x63, 0x0},
			},
			cong: "cubic\x00",
			Func: func(data []byte, ci *CongInfo) (n int, err error) {
				return DeserializeCongInfo(data, ci)
			},
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	ci := new(CongInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeMemInfoBoth(buf, rta)
		_, errD = Func(buf, ci)
		if errD != nil {
			b.Error("Test Failed DeserializeCongInfoBoth errD", errD)
		}

	}
	resultCI = *ci

	// if resultMI.Fmem != test.mi.Fmem {
	// 	b.Errorf("Test %s resultMI.Fmem:%d != test.mi.Fmem:%d", test.description, resultMI.Fmem, test.mi.Fmem)
	// }
}
