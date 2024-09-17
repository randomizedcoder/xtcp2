package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
)

type DeserializeSkMemInfoTest struct {
	description string
	filename    string
	sm          SkMemInfo
	Func        func(data []byte, sm *SkMemInfo) (n int, err error)
}

// TestDeserializeSkMemInfo
// go test --run TestDeserializeSkMemInfo
func TestDeserializeSkMemInfo(t *testing.T) {
	var tests = []DeserializeSkMemInfoTest{
		{
			description: "attribute_skmeminfo2",
			filename:    "./testdata/6_6_44/attribute_skmeminfo2",
			sm: SkMemInfo{
				RmemAlloc:  0,
				RcvBuf:     1000000,
				WmemAlloc:  0,
				SndBuf:     1000000,
				FwdAlloc:   0,
				WmemQueued: 0,
				Optmem:     0,
				Backlog:    0,
				Drops:      0,
			},
			Func: func(data []byte, sm *SkMemInfo) (n int, err error) {
				return DeserializeSkMemInfo(data, sm)
			},
		},
		{
			description: "attribute_skmeminfo2",
			filename:    "./testdata/6_6_44/attribute_skmeminfo2",
			sm: SkMemInfo{
				RmemAlloc:  0,
				RcvBuf:     1000000,
				WmemAlloc:  0,
				SndBuf:     1000000,
				FwdAlloc:   0,
				WmemQueued: 0,
				Optmem:     0,
				Backlog:    0,
				Drops:      0,
			},
			Func: func(data []byte, sm *SkMemInfo) (n int, err error) {
				return DeserializeSkMemInfoReflection(data, sm)
			},
		},
		{
			description: "attribute_skmeminfo",
			filename:    "./testdata/6_10_3/attribute_skmeminfo",
			sm: SkMemInfo{
				RmemAlloc:  0,
				RcvBuf:     1000000,
				WmemAlloc:  4,
				SndBuf:     1000000,
				FwdAlloc:   0,
				WmemQueued: 0,
				Optmem:     0,
				Backlog:    0,
				Drops:      23001,
			},
			Func: func(data []byte, sm *SkMemInfo) (n int, err error) {
				return DeserializeSkMemInfo(data, sm)
			},
		},
		// ESTAB  0      6174            127.0.0.1:31833          127.0.0.1:4305  users:(("tcp_client",pid=4352,fd=311))  timer:(on,546ms,0) uid:1000 ino:85152 sk:366e cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
		// skmem:(r0,rb1000000,t0,tb1000000,f3426,w8862,o0,bl0,d24406) ts sack ecn ecnseen bbr wscale:9,9 rto:811 rtt:399.942/93.744 ato:40 mss:1448 pmtu:1500 rcvmss:1448 advmss:1448 cwnd:10 ssthresh:181 bytes_sent:90417090 bytes_retrans:8234976 bytes_acked:82175941 bytes_received:82175940 segs_out:132244 segs_in:131823 data_segs_out:85300 data_segs_in:81763 bbr:(bw:427392bps,mrtt:27.057,pacing_gain:1.25,cwnd_gain:2) send 289642bps lastsnd:191 lastrcv:222 lastack:222 pacing_rate 528896bps delivery_rate 207288bps delivered:83575 app_limited busy:7513436ms unacked:6 retrans:0/7322 dsack_dups:5919 reord_seen:1890 rcv_rtt:407.226 rcv_space:18410 rcv_ssthresh:498552 minrtt:0.01 rcv_ooopack:12469 snd_wnd:498688 rcv_wnd:498688 rehash:149
		// skmem:(r0,rb1000000,t0,tb1000000,f3426,w8862,o0,bl0,d24406)
		{
			description: "attribute_skmeminfo_4305",
			filename:    "./testdata/6_10_3/attribute_skmeminfo_4305",
			sm: SkMemInfo{
				RmemAlloc:  0,
				RcvBuf:     1000000,
				WmemAlloc:  0,
				SndBuf:     1000000,
				FwdAlloc:   3426,
				WmemQueued: 8862,
				Optmem:     0,
				Backlog:    0,
				Drops:      24406,
			},
			Func: func(data []byte, sm *SkMemInfo) (n int, err error) {
				return DeserializeSkMemInfo(data, sm)
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

		sm := new(SkMemInfo)

		_, errD := test.Func(buf, sm)
		if errD != nil {
			t.Fatal("Test Failed DeserializeTypeOfService errD", errD)
		}

		// t.Logf("i:%d, n:%d", i, n)
		// t.Logf("i:%d,  sm:%v", i, sm)

		if !reflect.DeepEqual(*sm, test.sm) {
			t.Errorf("Test %d %s !reflect.DeepEqual(tos:%d, test.test.tos:%d)", i, test.description, sm, test.sm)
		}

	}
}

var (
	resultSM SkMemInfo
)

// go test -bench=BenchmarkDeserializeSkMemInfo
func BenchmarkDeserializeSkMemInfo(b *testing.B) {
	f := func(data []byte, sm *SkMemInfo) (n int, err error) {
		return DeserializeSkMemInfo(data, sm)
	}
	DeserializeSkMemInfoBoth(b, f)
}

func BenchmarkDeserializeSkMemInfoReflection(b *testing.B) {
	f := func(data []byte, sm *SkMemInfo) (n int, err error) {
		return DeserializeSkMemInfoReflection(data, sm)
	}
	DeserializeSkMemInfoBoth(b, f)
}

func DeserializeSkMemInfoBoth(b *testing.B, Func func(data []byte, sm *SkMemInfo) (n int, err error)) {
	var tests = []DeserializeSkMemInfoTest{
		{
			description: "attribute_skmeminfo2",
			filename:    "./testdata/6_6_44/attribute_skmeminfo2",
			sm: SkMemInfo{
				RmemAlloc:  0,
				RcvBuf:     1000000,
				WmemAlloc:  0,
				SndBuf:     1000000,
				FwdAlloc:   0,
				WmemQueued: 0,
				Optmem:     0,
				Backlog:    0,
				Drops:      0,
			},
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	sm := new(SkMemInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, sm)
		if errD != nil {
			b.Error("Test Failed DeserializeSkMemInfoBoth errD", errD)
		}

	}
	resultSM = *sm
}
