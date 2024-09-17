package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializeDCTCPInfoTest struct {
	description string
	filename    string
	d           DCTCPInfo
	Func        func(data []byte, d *DCTCPInfo) (n int, err error)
}

// TestDeserializeDCTCPInfo
// go test --run TestDeserializeDCTCPInfo
func TestDeserializeDCTCPInfo(t *testing.T) {
	var tests = []DeserializeDCTCPInfoTest{
		// ESTAB     0      0               127.0.0.1:33283          127.0.0.1:4033  users:(("tcp_client",pid=2822,fd=39))  timer:(keepalive,12sec,0) uid:1000 ino:205541 sk:94a2 cgroup:/user.slice/user-1000.slice/session-3.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-3.scope
		// skmem:(r0,rb1000000,t0,tb2626560,f0,w0,o0,bl0,d0) ts sack ecn ecnseen dctcp wscale:9,9 rto:201 rtt:0.131/0.08 ato:40 mss:32768 pmtu:65535 rcvmss:536 advmss:65483 cwnd:10 bytes_sent:90 bytes_acked:91 bytes_received:90 segs_out:20 segs_in:11 data_segs_out:9 data_segs_in:9 dctcp:(ce_state:0,alpha:540,ab_ecn:0,ab_tot:32768) send 20010992366bps lastsnd:506 lastrcv:506 lastack:506 pacing_rate 39907745000bps delivery_rate 9362285712bps delivered:10 app_limited busy:1ms rcv_space:65535 rcv_ssthresh:65535 minrtt:0.028 snd_wnd:65536 rcv_wnd:65536
		// dctcp:(ce_state:0,alpha:540,ab_ecn:0,ab_tot:32768)
		{
			description: "attribute_dctcpinfo_4033",
			filename:    "./testdata/6_6_44/attribute_dctcpinfo_4033",
			d: DCTCPInfo{
				Enabled: 1,
				CEState: 0,
				Alpha:   654, // ?? 540
				ABECN:   0,
				ABTOT:   32768,
			},
			Func: func(data []byte, d *DCTCPInfo) (n int, err error) {
				return DeserializeDCTCPInfo(data, d)
			},
		},
		{
			description: "attribute_dctcpinfo_4033_reflection",
			filename:    "./testdata/6_6_44/attribute_dctcpinfo_4033",
			d: DCTCPInfo{
				Enabled: 1,
				CEState: 0,
				Alpha:   654, // ?? 540
				ABECN:   0,
				ABTOT:   32768,
			},
			Func: func(data []byte, d *DCTCPInfo) (n int, err error) {
				return DeserializeDCTCPInfoReflection(data, d)
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

		d := new(DCTCPInfo)

		_, errD := test.Func(buf, d)
		if errD != nil {
			t.Fatal("Test Failed DeserializeDCTCPInfo errD", errD)
		}

		if d.Enabled != test.d.Enabled {
			t.Errorf("Test %d %s d.Enabled:%d != test.d.Enabled:%d", i, test.description, d.Enabled, test.d.Enabled)
		}

		if d.CEState != test.d.CEState {
			t.Errorf("Test %d %s d.CEState:%d != test.d.CEState:%d", i, test.description, d.CEState, test.d.CEState)
		}

		if d.Alpha != test.d.Alpha {
			t.Errorf("Test %d %s d.Alpha:%d != test.d.Alpha:%d", i, test.description, d.Alpha, test.d.Alpha)
		}

		if d.ABECN != test.d.ABECN {
			t.Errorf("Test %d %s d.ABECN:%d != test.d.ABECN:%d", i, test.description, d.ABECN, test.d.ABECN)
		}

		if d.ABTOT != test.d.ABTOT {
			t.Errorf("Test %d %s d.ABTOT:%d != test.d.ABTOT:%d", i, test.description, d.ABTOT, test.d.ABTOT)
		}

	}
}

var (
	resultDCTCPInfo DCTCPInfo
)

// go test -bench=BenchmarkDeserializeDCTCPInfo

func BenchmarkDeserializeDCTCPInfo(b *testing.B) {
	f := func(data []byte, d *DCTCPInfo) (n int, err error) {
		return DeserializeDCTCPInfo(data, d)
	}
	DeserializeDCTCPInfoBoth(b, f)
}

func BenchmarkDeserializeDCTCPInfoReflection(b *testing.B) {
	f := func(data []byte, d *DCTCPInfo) (n int, err error) {
		return DeserializeDCTCPInfoReflection(data, d)
	}
	DeserializeDCTCPInfoBoth(b, f)
}

func DeserializeDCTCPInfoBoth(b *testing.B, Func func(data []byte, d *DCTCPInfo) (n int, err error)) {
	var tests = []DeserializeMemInfoTest{
		{
			description: "attribute_dctcpinfo_4033_reflection",
			filename:    "./testdata/6_6_44/attribute_dctcpinfo_4033",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	d := new(DCTCPInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		_, errD = Func(buf, d)
		if errD != nil {
			b.Error("Test Failed DeserializeDCTCPInfoBoth errD", errD)
		}

	}
	resultDCTCPInfo = *d

}
