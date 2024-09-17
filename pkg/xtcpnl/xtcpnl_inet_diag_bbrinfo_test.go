package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializeBBRInfoTest struct {
	description string
	filename    string
	b           BBRInfo
	Func        func(data []byte, b *BBRInfo) (n int, err error)
}

// TestDeserializeBBRInfo
// go test --run TestDeserializeBBRInfo
func TestDeserializeBBRInfo(t *testing.T) {
	var tests = []DeserializeBBRInfoTest{
		{
			description: "attribute_bbrinfo",
			filename:    "./testdata/6_6_44/attribute_bbrinfo",
			b: BBRInfo{
				BwLo:       1638398437,
				BwHi:       0,
				MinRtt:     20,
				PacingGain: 739,
				CwndGain:   739,
			},
			Func: func(data []byte, b *BBRInfo) (n int, err error) {
				return DeserializeBBRInfo(data, b)
			},
		},
		{
			description: "attribute_bbrinfo",
			filename:    "./testdata/6_6_44/attribute_bbrinfo",
			b: BBRInfo{
				BwLo:       1638398437,
				BwHi:       0,
				MinRtt:     20,
				PacingGain: 739,
				CwndGain:   739,
			},
			Func: func(data []byte, b *BBRInfo) (n int, err error) {
				return DeserializeBBRInfoReflection(data, b)
			},
		},
		//ESTAB  0      4116            127.0.0.1:33895          127.0.0.1:4879  users:(("tcp_client",pid=4352,fd=885))  timer:(on,431ms,0) uid:1000 ino:91523 sk:a4f cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
		//skmem:(r0,rb1000000,t0,tb1000000,f2284,w5908,o0,bl0,d25253) ts sack ecn ecnseen bbr wscale:9,9 rto:662 rtt:318.242/62.166 ato:40 mss:1448 pmtu:1500 rcvmss:1448 advmss:1448 cwnd:8 ssthresh:138 bytes_sent:93568860 bytes_retrans:10168410 bytes_acked:83396335 bytes_received:83396334 segs_out:136944 segs_in:135387 data_segs_out:90018 data_segs_in:82319 bbr:(bw:120824bps,mrtt:0.03,pacing_gain:1.25,cwnd_gain:2) send 291200bps lastsnd:232 lastrcv:370 lastack:370 pacing_rate 119616bps delivery_rate 121232bps delivered:87669 app_limited busy:7387385ms unacked:4 retrans:0/9231 dsack_dups:7385 reordering:5 reord_seen:2662 rcv_rtt:348.502 rcv_space:19464 rcv_ssthresh:498552 minrtt:0.007 rcv_ooopack:12444 snd_wnd:498688 rcv_wnd:498688 rehash:180
		// bbr:(bw:120824bps,mrtt:0.03,pacing_gain:1.25,cwnd_gain:2) 120824bps / 8 = 15103
		{
			description: "attribute_bbrinfo",
			filename:    "./testdata/6_10_3/attribute_bbrinfo",
			b: BBRInfo{
				BwLo:       15103, // 120824bps / 8 = 15103
				BwHi:       0,
				MinRtt:     30,  // mrtt:0.03
				PacingGain: 320, // pacing_gain:1.25 - ??
				CwndGain:   512, // cwnd_gain:2 - ??
			},
			Func: func(data []byte, b *BBRInfo) (n int, err error) {
				return DeserializeBBRInfo(data, b)
			},
		},
		{
			description: "attribute_bbrinfo",
			filename:    "./testdata/6_10_3/attribute_bbrinfo",
			b: BBRInfo{
				BwLo:       15103,
				BwHi:       0,
				MinRtt:     30,
				PacingGain: 320,
				CwndGain:   512,
			},
			Func: func(data []byte, b *BBRInfo) (n int, err error) {
				return DeserializeBBRInfoReflection(data, b)
			},
		},
		//ESTAB  0      6174            127.0.0.1:31833          127.0.0.1:4305  users:(("tcp_client",pid=4352,fd=311))  timer:(on,546ms,0) uid:1000 ino:85152 sk:366e cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
		//skmem:(r0,rb1000000,t0,tb1000000,f3426,w8862,o0,bl0,d24406) ts sack ecn ecnseen bbr wscale:9,9 rto:811 rtt:399.942/93.744 ato:40 mss:1448 pmtu:1500 rcvmss:1448 advmss:1448 cwnd:10 ssthresh:181 bytes_sent:90417090 bytes_retrans:8234976 bytes_acked:82175941 bytes_received:82175940 segs_out:132244 segs_in:131823 data_segs_out:85300 data_segs_in:81763 bbr:(bw:427392bps,mrtt:27.057,pacing_gain:1.25,cwnd_gain:2) send 289642bps lastsnd:191 lastrcv:222 lastack:222 pacing_rate 528896bps delivery_rate 207288bps delivered:83575 app_limited busy:7513436ms unacked:6 retrans:0/7322 dsack_dups:5919 reord_seen:1890 rcv_rtt:407.226 rcv_space:18410 rcv_ssthresh:498552 minrtt:0.01 rcv_ooopack:12469 snd_wnd:498688 rcv_wnd:498688 rehash:149
		//bbr:(bw:427392bps,mrtt:27.057,pacing_gain:1.25,cwnd_gain:2)
		{
			description: "attribute_bbrinfo_4305",
			filename:    "./testdata/6_10_3/attribute_bbrinfo_4305",
			b: BBRInfo{
				BwLo:       53424, // 427392bps / 8 = 53424
				BwHi:       0,
				MinRtt:     27057, // mrtt:27.057
				PacingGain: 320,   // pacing_gain:1.25 - ??
				CwndGain:   512,   // cwnd_gain:2 - ??
			},
			Func: func(data []byte, b *BBRInfo) (n int, err error) {
				return DeserializeBBRInfo(data, b)
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

		b := new(BBRInfo)

		_, errD := test.Func(buf, b)
		if errD != nil {
			t.Fatal("Test Failed DeserializeBBRInfo errD", errD)
		}

		if b.BwLo != test.b.BwLo {
			t.Errorf("Test %d %s b.BwLo:%d != test.b.BwLo:%d", i, test.description, b.BwLo, test.b.BwLo)
		}

		if b.BwHi != test.b.BwHi {
			t.Errorf("Test %d %s b.BwHi:%d != test.b.BwHi:%d", i, test.description, b.BwHi, test.b.BwHi)
		}

		if b.MinRtt != test.b.MinRtt {
			t.Errorf("Test %d %s b.MinRtt:%d != test.b.MinRtt:%d", i, test.description, b.MinRtt, test.b.MinRtt)
		}

		if b.PacingGain != test.b.PacingGain {
			t.Errorf("Test %d %s b.PacingGain:%d != test.b.PacingGain:%d", i, test.description, b.PacingGain, test.b.PacingGain)
		}

		if b.CwndGain != test.b.CwndGain {
			t.Errorf("Test %d %s b.CwndGain:%d != test.b.CwndGain:%d", i, test.description, b.CwndGain, test.b.CwndGain)
		}

	}
}

var (
	resultBBRInfo BBRInfo
)

// go test -bench=BenchmarkDeserializeMemInfo

func BenchmarkDeserializeBBRInfo(b *testing.B) {
	f := func(data []byte, bi *BBRInfo) (n int, err error) {
		return DeserializeBBRInfo(data, bi)
	}
	DeserializeBBRInfoBoth(b, f)
}

func BenchmarkDeserializeBBRInfoReflection(b *testing.B) {
	f := func(data []byte, bi *BBRInfo) (n int, err error) {
		return DeserializeBBRInfoReflection(data, bi)
	}
	DeserializeBBRInfoBoth(b, f)
}

func DeserializeBBRInfoBoth(b *testing.B, Func func(data []byte, bi *BBRInfo) (n int, err error)) {
	var tests = []DeserializeMemInfoTest{
		{
			description: "attribute_bbrinfo",
			filename:    "./testdata/6_10_3/attribute_bbrinfo",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	bi := new(BBRInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		_, errD = Func(buf, bi)
		if errD != nil {
			b.Error("Test Failed DeserializeBBRInfoBoth errD", errD)
		}

	}
	resultBBRInfo = *bi

}
