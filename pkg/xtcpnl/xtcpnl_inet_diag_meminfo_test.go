package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializeMemInfoTest struct {
	description string
	filename    string
	mi          MemInfo
	Func        func(data []byte, mi *MemInfo) (n int, err error)
}

// TestDeserializeMemInfo - unforntunately, most examples I have a zeros :(
// go test --run TestDeserializeMemInfo
func TestDeserializeMemInfo(t *testing.T) {
	var tests = []DeserializeMemInfoTest{
		{
			description: "attribute_meminfo_5908",
			filename:    "./testdata/6_10_3/attribute_meminfo_5908",
			mi: MemInfo{
				Rmem: 0,
				Wmem: 5908,
				Fmem: 2284,
				Tmem: 4,
			},
			Func: func(data []byte, mi *MemInfo) (n int, err error) {
				return DeserializeMemInfo(data, mi)
			},
		},
		{
			description: "attribute_meminfo_1506",
			filename:    "./testdata/6_10_3/attribute_meminfo_1506",
			mi: MemInfo{
				Rmem: 1506,
				Wmem: 0,
				Fmem: 2590,
				Tmem: 2,
			},
			Func: func(data []byte, mi *MemInfo) (n int, err error) {
				return DeserializeMemInfo(data, mi)
			},
		},
		{
			description: "attribute_meminfo",
			filename:    "./testdata/6_6_44/attribute_meminfo",
			mi: MemInfo{
				Rmem: 0,
				Wmem: 0,
				Fmem: 0,
				Tmem: 0,
			},
			Func: func(data []byte, mi *MemInfo) (n int, err error) {
				return DeserializeMemInfo(data, mi)
			},
		},
		{
			description: "4_19_319_attribute_meminfo",
			filename:    "./testdata/4_19_319/attribute_meminfo_f4096",
			mi: MemInfo{
				Rmem: 0,
				Wmem: 0,
				Fmem: 4096,
				Tmem: 0,
			},
			Func: func(data []byte, mi *MemInfo) (n int, err error) {
				return DeserializeMemInfo(data, mi)
			},
		},
		{
			description: "4_19_319_attribute_meminfo",
			filename:    "./testdata/4_19_319/attribute_meminfo_f4096",
			mi: MemInfo{
				Rmem: 0,
				Wmem: 0,
				Fmem: 4096,
				Tmem: 0,
			},
			Func: func(data []byte, mi *MemInfo) (n int, err error) {
				return DeserializeMemInfoReflection(data, mi)
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

		mi := new(MemInfo)

		_, errD := DeserializeMemInfo(buf, mi)
		if errD != nil {
			t.Fatal("Test Failed DeserializeMemInfo errD", errD)
		}

		if mi.Rmem != test.mi.Rmem {
			t.Errorf("Test %d %s mi.Rmem:%d != test.mi.Rmem:%d", i, test.description, mi.Rmem, test.mi.Rmem)
		}

		if mi.Wmem != test.mi.Wmem {
			t.Errorf("Test %d %s mi.Wmem:%d != test.mi.Wmem:%d", i, test.description, mi.Wmem, test.mi.Wmem)
		}

		if mi.Fmem != test.mi.Fmem {
			t.Errorf("Test %d %s mi.Fmem:%d != test.mi.Fmem:%d", i, test.description, mi.Fmem, test.mi.Fmem)
		}

		if mi.Tmem != test.mi.Tmem {
			t.Errorf("Test %d %s mi.Tmem:%d != test.mi.Tmem:%d", i, test.description, mi.Tmem, test.mi.Tmem)
		}

	}
}

var (
	resultMI MemInfo
)

// go test -bench=BenchmarkDeserializeMemInfo

func BenchmarkDeserializeMemInfo(b *testing.B) {
	f := func(data []byte, mi *MemInfo) (n int, err error) {
		return DeserializeMemInfo(data, mi)
	}
	DeserializeMemInfoBoth(b, f)
}

func BenchmarkDeserializeMemInfoReflection(b *testing.B) {
	f := func(data []byte, mi *MemInfo) (n int, err error) {
		return DeserializeMemInfoReflection(data, mi)
	}
	DeserializeMemInfoBoth(b, f)
}

func DeserializeMemInfoBoth(b *testing.B, Func func(data []byte, mi *MemInfo) (n int, err error)) {
	var tests = []DeserializeMemInfoTest{
		{
			description: "attribute_info",
			filename:    "./testdata/4_19_319/attribute_meminfo_f4096",
			mi: MemInfo{
				Rmem: 0,
				Wmem: 0,
				Fmem: 4096,
				Tmem: 0,
			},
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	mi := new(MemInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeMemInfoBoth(buf, rta)
		_, errD = Func(buf, mi)
		if errD != nil {
			b.Error("Test Failed DeserializeMemInfoBoth errD", errD)
		}

	}
	resultMI = *mi

	// if resultMI.Fmem != test.mi.Fmem {
	// 	b.Errorf("Test %s resultMI.Fmem:%d != test.mi.Fmem:%d", test.description, resultMI.Fmem, test.mi.Fmem)
	// }
}
