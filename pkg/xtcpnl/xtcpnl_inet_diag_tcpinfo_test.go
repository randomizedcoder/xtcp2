package xtcpnl

import (
	"io"
	"os"
	"testing"
)

type DeserializeTCPInfoTest struct {
	description string
	filename    string
	tcpinfo     TCPInfo
	Func        func(data []byte, t *TCPInfo) (n int, err error)
}

// TestDeserializeTCPInfo
// go test --run TestDeserializeTCPInfo
func TestDeserializeTCPInfo(t *testing.T) {
	var tests = []DeserializeTCPInfoTest{
		//ESTAB     0      10              127.0.0.1:47867          127.0.0.1:4262  users:(("tcp_client",pid=1424,fd=268))  timer:(on,201ms,0) uid:1000 ino:14801 sk:6cb cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
		// skmem:(r0,rb1000000,t0,tb2626560,f3190,w906,o0,bl0,d0) ts sack ecn ecnseen cubic wscale:9,9 rto:201 rtt:0.249/0.239 ato:40 mss:65483 pmtu:65535 rcvmss:536 advmss:65483 cwnd:10 bytes_sent:4350 bytes_acked:4341 bytes_received:4340 segs_out:438 segs_in:436 data_segs_out:435 data_segs_in:434 send 21038714859bps lastrcv:15 lastack:15 pacing_rate 42056317104bps delivery_rate 74837714280bps delivered:435 app_limited busy:107ms unacked:1 rcv_space:434517 rcv_ssthresh:434517 minrtt:0.007 snd_wnd:458752 rcv_wnd:458752
		{
			description: "6_10_3 dport4262",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_1000_sockets_sleep1ms_packet8_dport4262_info",
			tcpinfo: TCPInfo{
				State:              1,
				CaState:            0,
				Retransmits:        0,
				Probes:             0,
				Backoff:            0,
				Options:            31,
				ScaleTemp:          153,
				FlagsTemp:          1,
				Rto:                201000, // rto:201
				Ato:                40000,  // ato:40
				SndMss:             65483,  // mss:65483
				RcvMss:             536,    // rcvmss:536
				Unacked:            1,      // unacked:1
				Sacked:             0,
				Lost:               0,
				Retrans:            0,
				Fackets:            0, // This is unused
				LastDataSent:       0,
				LastAckSent:        0,
				LastDataRecv:       15,     // lastrcv:15
				LastAckRecv:        15,     // lastack:15
				Pmtu:               65535,  // pmtu:65535
				RcvSsthresh:        434517, // rcv_space:434517
				Rtt:                249,    // rtt:0.249/0.239
				Rttvar:             239,
				SndSsthresh:        2147483647, // is this correct?
				SndCwnd:            10,
				AdvMss:             65483, // advmss:65483
				Reordering:         3,
				RcvRtt:             0,
				RcvSpace:           434517, // rcv_space:434517
				TotalRetrans:       0,
				PacingRate:         5257039638,           // pacing_rate 42056317104bps / 8 = 5257039638
				MaxPacingRate:      18446744073709551615, // ? correct?
				BytesAcked:         4341,
				BytesReceived:      4340,
				SegsOut:            438, // segs_out:438
				SegsIn:             436, // segs_in:436
				NotSentBytes:       0,
				MinRtt:             7,          // minrtt:0.007
				DataSegsIn:         434,        // data_segs_in:434
				DataSegsOut:        435,        // data_segs_out:435
				DeliveryRate:       9354714285, // delivery_rate 74837714280bps / 8 = 9354714285
				BusyTime:           107000,     // app_limited busy:107ms
				RwndLimited:        0,
				SndbufLimited:      0,
				Delivered:          435, // delivered:435
				DeliveredCe:        0,
				BytesSent:          4350, // bytes_sent:4350
				BytesRetrans:       0,
				DsackDups:          0,
				ReordSeen:          0,
				RcvOoopack:         0,
				SndWnd:             458752, // snd_wnd:458752
				RcvWnd:             458752, // rcv_wnd:458752
				Rehash:             0,
				TotalRTO:           0,
				TotalRTORecoveries: 0,
				TotalRTOTime:       0,
			},
			Func: func(data []byte, t *TCPInfo) (n int, err error) {
				return DeserializeTCPInfo(data, t)
			},
		},
		//ESTAB  0      10              127.0.0.1:42839          127.0.0.1:4355  users:(("tcp_client",pid=1421,fd=361))  timer:(on,340ms,0) uid:1000 ino:13300 sk:1010 cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0x2 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
		//skmem:(r0,rb1000000,t0,tb2626560,f3190,w906,o0,bl0,d398) ts sack ecn ecnseen cubic wscale:9,9 rto:394 rtt:189.965/45.629 ato:40 mss:65483 pmtu:65535 rcvmss:536 advmss:65483 cwnd:2 ssthresh:2 bytes_sent:126380 bytes_retrans:2360 bytes_acked:124011 bytes_received:124010 segs_out:13415 segs_in:13341 data_segs_out:12638 data_segs_in:12566 send 5515374bps lastsnd:54 lastrcv:55 lastack:55 pacing_rate 6618424bps delivery_rate 34924266664bps delivered:12550 app_limited busy:2383736ms unacked:1 retrans:0/236 dsack_dups:148 rcv_space:434517 rcv_ssthresh:434517 minrtt:0.015 snd_wnd:458752 rcv_wnd:458752 rehash:2
		{
			description: "6_10_3 dport4262",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_2000_sockets_netem_approx30mins_packet4_dport4355_info",
			tcpinfo: TCPInfo{
				State:              1,
				CaState:            0,
				Retransmits:        0,
				Probes:             0,
				Backoff:            0,
				Options:            31,
				ScaleTemp:          153,
				FlagsTemp:          1,
				Rto:                394000, // rto:394
				Ato:                40000,  // ato:40
				SndMss:             65483,  // mss:65483
				RcvMss:             536,    // rcvmss:536
				Unacked:            1,      // unacked:1
				Sacked:             0,
				Lost:               0,
				Retrans:            0,  // retrans:0/236 - This is Retrans/TotalRetrans
				Fackets:            0,  // This is unused
				LastDataSent:       54, // lastsnd:54
				LastAckSent:        0,
				LastDataRecv:       55,     // lastrcv:55
				LastAckRecv:        55,     // lastack:55
				Pmtu:               65535,  // pmtu:65535
				RcvSsthresh:        434517, // rcv_ssthresh:434517
				Rtt:                189965, // rtt:189.965/45.629
				Rttvar:             45629,
				SndSsthresh:        2,     // ssthresh:2
				SndCwnd:            2,     // cwnd:2
				AdvMss:             65483, // advmss:65483
				Reordering:         3,
				RcvRtt:             0,
				RcvSpace:           434517,               // rcv_space:434517
				TotalRetrans:       236,                  // retrans:0/236
				PacingRate:         827303,               // pacing_rate 6618424bps / 8 = 827303
				MaxPacingRate:      18446744073709551615, // ? correct?
				BytesAcked:         124011,               // bytes_acked:124011
				BytesReceived:      124010,               // bytes_received:124010
				SegsOut:            13415,                // segs_out:13415
				SegsIn:             13341,                // segs_in:13341
				NotSentBytes:       0,
				MinRtt:             15,         // minrtt:0.015
				DataSegsIn:         12566,      // data_segs_in:12566
				DataSegsOut:        12638,      // data_segs_out:12638
				DeliveryRate:       4365533333, // delivery_rate 34924266664bps / 8 = 4365533333
				BusyTime:           2383736000, // app_limited busy:2383736ms
				RwndLimited:        0,
				SndbufLimited:      0,
				Delivered:          12550, // delivered:12550
				DeliveredCe:        0,
				BytesSent:          126380, // bytes_sent:126380
				BytesRetrans:       2360,   // bytes_retrans:2360
				DsackDups:          148,    // dsack_dups:148
				ReordSeen:          0,
				RcvOoopack:         0,
				SndWnd:             458752, // snd_wnd:458752
				RcvWnd:             458752, // rcv_wnd:458752
				Rehash:             2,      // rehash:2
				TotalRTO:           2,      // ? not sure if this is correct.  These aren't in "ss"
				TotalRTORecoveries: 2,      // ? not sure if this is correct.  These aren't in "ss"
				TotalRTOTime:       238,    // ? not sure if this is correct.  These aren't in "ss"
			},
			Func: func(data []byte, t *TCPInfo) (n int, err error) {
				return DeserializeTCPInfo(data, t)
			},
		},
		{
			// ESTAB  0      0               127.0.0.1:64113          127.0.0.1:5865  users:(("tcp_client",pid=4352,fd=1871)) timer:(keepalive,5.123ms,0) uid:1000 ino:95551 sk:a4e cgroup:/user.slice/user-1000.slice/session-1.scope <-> tos:0 class_id:0 cgroup:/user.slice/user-1000.slice/session-1.scope
			// skmem:(r0,rb1000000,t4,tb1000000,f0,w0,o0,bl0,d23001) ts sack ecn ecnseen bbr wscale:9,9 rto:515 rtt:258.135/53.541 ato:40 mss:1448 pmtu:1500 rcvmss:1448 advmss:1448 cwnd:6 ssthresh:141 bytes_sent:87874252 bytes_retrans:7916836 bytes_acked:79957417 bytes_received:79953300 segs_out:128380 segs_in:127478 data_segs_out:83109 data_segs_in:78740 bbr:(bw:252016bps,mrtt:0.02,pacing_gain:1,cwnd_gain:2) send 269254bps lastsnd:171 lastrcv:224 lastack:41 pacing_rate 249496bps delivery_rate 252664bps delivered:81381 app_limited busy:7404077ms retrans:0/7053 dsack_dups:5662 reord_seen:1892 rcv_rtt:298.752 rcv_space:19464 rcv_ssthresh:498552 minrtt:0.007 rcv_ooopack:11211 snd_wnd:498688 rcv_wnd:498688 rehash:132 		{
			description: "6_10_3 dport5865",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_2000_sockets_netem_bbr_approx60mins_packet10_dport5865_info",
			tcpinfo: TCPInfo{
				State:              1,
				CaState:            0,
				Retransmits:        0,
				Probes:             0,
				Backoff:            0,
				Options:            31,
				ScaleTemp:          153,
				FlagsTemp:          1,
				Rto:                515000, // rto:515
				Ato:                40000,  // ato:40
				SndMss:             1448,   // mss:1448
				RcvMss:             1448,   // rcvmss:1448
				Unacked:            0,      // ?
				Sacked:             0,
				Lost:               0,
				Retrans:            0,   // retrans:0/7053 - This is Retrans/TotalRetrans
				Fackets:            0,   // This is unused
				LastDataSent:       171, // lastsnd:171
				LastAckSent:        0,
				LastDataRecv:       224,    // lastrcv:224
				LastAckRecv:        41,     // lastack:41
				Pmtu:               1500,   // pmtu:1500
				RcvSsthresh:        498552, // rcv_ssthresh:498552
				Rtt:                258135, // rtt:258.135/53.541
				Rttvar:             53541,
				SndSsthresh:        141,                  // ssthresh:141
				SndCwnd:            6,                    // cwnd:6
				AdvMss:             1448,                 // advmss:1448
				Reordering:         3,                    // ?
				RcvRtt:             298752,               // rcv_rtt:298.752
				RcvSpace:           19464,                // rcv_space:19464
				TotalRetrans:       7053,                 // retrans:0/7053
				PacingRate:         31187,                // pacing_rate 249496bps.  Kernel value is in Bps, not bps. 249496/31187=8
				MaxPacingRate:      18446744073709551615, // ? correct?
				BytesAcked:         79957417,             // bytes_acked:79957417
				BytesReceived:      79953300,             // bytes_received:79953300
				SegsOut:            128380,               // segs_out:128380
				SegsIn:             127478,               // segs_in:127478
				NotSentBytes:       0,
				MinRtt:             7,          // minrtt:0.007
				DataSegsIn:         78740,      // data_segs_in:78740
				DataSegsOut:        83109,      // data_segs_out:83109
				DeliveryRate:       31583,      // delivery_rate 252664bps / 8 = 31583
				BusyTime:           7404077000, // app_limited busy:7404077ms
				RwndLimited:        0,
				SndbufLimited:      0,
				Delivered:          81381, // delivered:81381
				DeliveredCe:        0,
				BytesSent:          87874252, // bytes_sent:87874252
				BytesRetrans:       7916836,  // bytes_retrans:7916836
				DsackDups:          5662,     // dsack_dups:5662
				ReordSeen:          1892,     // reord_seen:1892
				RcvOoopack:         11211,
				SndWnd:             498688, // snd_wnd:498688
				RcvWnd:             498688, // rcv_wnd:498688
				Rehash:             132,    // rehash:132
				TotalRTO:           132,    // ? not sure if this is correct.  These aren't in "ss"
				TotalRTORecoveries: 126,    // ? not sure if this is correct.  These aren't in "ss"
				TotalRTOTime:       45581,  // ? not sure if this is correct.  These aren't in "ss"
			},
			Func: func(data []byte, t *TCPInfo) (n int, err error) {
				return DeserializeTCPInfo(data, t)
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

		tcpinfo := new(TCPInfo)

		_, errD := test.Func(buf, tcpinfo)
		if errD != nil {
			t.Fatal("Test Failed test.Func errD", errD)
		}
		if tcpinfo.State != test.tcpinfo.State {
			t.Errorf("Test %d %s tcpinfo.State:%d != test.tcpinfo.State:%d", i, test.description, tcpinfo.State, test.tcpinfo.State)
		}

		if tcpinfo.CaState != test.tcpinfo.CaState {
			t.Errorf("Test %d %s tcpinfo.CaState:%d != test.tcpinfo.CaState:%d", i, test.description, tcpinfo.CaState, test.tcpinfo.CaState)
		}

		if tcpinfo.Retransmits != test.tcpinfo.Retransmits {
			t.Errorf("Test %d %s tcpinfo.Retransmits:%d != test.tcpinfo.Retransmits:%d", i, test.description, tcpinfo.Retransmits, test.tcpinfo.Retransmits)
		}

		if tcpinfo.Probes != test.tcpinfo.Probes {
			t.Errorf("Test %d %s tcpinfo.Probes:%d != test.tcpinfo.Probes:%d", i, test.description, tcpinfo.Probes, test.tcpinfo.Probes)
		}

		if tcpinfo.Backoff != test.tcpinfo.Backoff {
			t.Errorf("Test %d %s tcpinfo.Backoff:%d != test.tcpinfo.Backoff:%d", i, test.description, tcpinfo.Backoff, test.tcpinfo.Backoff)
		}

		if tcpinfo.Options != test.tcpinfo.Options {
			t.Errorf("Test %d %s tcpinfo.Options:%d != test.tcpinfo.Options:%d", i, test.description, tcpinfo.Options, test.tcpinfo.Options)
		}

		if tcpinfo.ScaleTemp != test.tcpinfo.ScaleTemp {
			t.Errorf("Test %d %s tcpinfo.ScaleTemp:%d != test.tcpinfo.ScaleTemp:%d", i, test.description, tcpinfo.ScaleTemp, test.tcpinfo.ScaleTemp)
		}

		if tcpinfo.FlagsTemp != test.tcpinfo.FlagsTemp {
			t.Errorf("Test %d %s tcpinfo.FlagsTemp:%d != test.tcpinfo.FlagsTemp:%d", i, test.description, tcpinfo.FlagsTemp, test.tcpinfo.FlagsTemp)
		}

		if tcpinfo.Rto != test.tcpinfo.Rto {
			t.Errorf("Test %d %s tcpinfo.Rto:%d != test.tcpinfo.Rto:%d", i, test.description, tcpinfo.Rto, test.tcpinfo.Rto)
		}

		if tcpinfo.Ato != test.tcpinfo.Ato {
			t.Errorf("Test %d %s tcpinfo.Ato:%d != test.tcpinfo.Ato:%d", i, test.description, tcpinfo.Ato, test.tcpinfo.Ato)
		}

		if tcpinfo.SndMss != test.tcpinfo.SndMss {
			t.Errorf("Test %d %s tcpinfo.SndMss:%d != test.tcpinfo.SndMss:%d", i, test.description, tcpinfo.SndMss, test.tcpinfo.SndMss)
		}

		if tcpinfo.RcvMss != test.tcpinfo.RcvMss {
			t.Errorf("Test %d %s tcpinfo.RcvMss:%d != test.tcpinfo.RcvMss:%d", i, test.description, tcpinfo.RcvMss, test.tcpinfo.RcvMss)
		}

		if tcpinfo.Unacked != test.tcpinfo.Unacked {
			t.Errorf("Test %d %s tcpinfo.Unacked:%d != test.tcpinfo.Unacked:%d", i, test.description, tcpinfo.Unacked, test.tcpinfo.Unacked)
		}

		if tcpinfo.Sacked != test.tcpinfo.Sacked {
			t.Errorf("Test %d %s tcpinfo.Sacked:%d != test.tcpinfo.Sacked:%d", i, test.description, tcpinfo.Sacked, test.tcpinfo.Sacked)
		}

		if tcpinfo.Lost != test.tcpinfo.Lost {
			t.Errorf("Test %d %s tcpinfo.Lost:%d != test.tcpinfo.Lost:%d", i, test.description, tcpinfo.Lost, test.tcpinfo.Lost)
		}

		if tcpinfo.Retrans != test.tcpinfo.Retrans {
			t.Errorf("Test %d %s tcpinfo.Retrans:%d != test.tcpinfo.Retrans:%d", i, test.description, tcpinfo.Retrans, test.tcpinfo.Retrans)
		}

		if tcpinfo.Fackets != test.tcpinfo.Fackets {
			t.Errorf("Test %d %s tcpinfo.Fackets:%d != test.tcpinfo.Fackets:%d", i, test.description, tcpinfo.Fackets, test.tcpinfo.Fackets)
		}

		if tcpinfo.LastDataSent != test.tcpinfo.LastDataSent {
			t.Errorf("Test %d %s tcpinfo.LastDataSent:%d != test.tcpinfo.LastDataSent:%d", i, test.description, tcpinfo.LastDataSent, test.tcpinfo.LastDataSent)
		}

		if tcpinfo.LastAckSent != test.tcpinfo.LastAckSent {
			t.Errorf("Test %d %s tcpinfo.LastAckSent:%d != test.tcpinfo.LastAckSent:%d", i, test.description, tcpinfo.LastAckSent, test.tcpinfo.LastAckSent)
		}

		if tcpinfo.LastDataRecv != test.tcpinfo.LastDataRecv {
			t.Errorf("Test %d %s tcpinfo.LastDataRecv:%d != test.tcpinfo.LastDataRecv:%d", i, test.description, tcpinfo.LastDataRecv, test.tcpinfo.LastDataRecv)
		}

		if tcpinfo.LastAckRecv != test.tcpinfo.LastAckRecv {
			t.Errorf("Test %d %s tcpinfo.LastAckRecv:%d != test.tcpinfo.LastAckRecv:%d", i, test.description, tcpinfo.LastAckRecv, test.tcpinfo.LastAckRecv)
		}

		if tcpinfo.Pmtu != test.tcpinfo.Pmtu {
			t.Errorf("Test %d %s tcpinfo.Pmtu:%d != test.tcpinfo.Pmtu:%d", i, test.description, tcpinfo.Pmtu, test.tcpinfo.Pmtu)
		}

		if tcpinfo.RcvSsthresh != test.tcpinfo.RcvSsthresh {
			t.Errorf("Test %d %s tcpinfo.RcvSsthresh:%d != test.tcpinfo.RcvSsthresh:%d", i, test.description, tcpinfo.RcvSsthresh, test.tcpinfo.RcvSsthresh)
		}

		if tcpinfo.Rtt != test.tcpinfo.Rtt {
			t.Errorf("Test %d %s tcpinfo.Rtt:%d != test.tcpinfo.Rtt:%d", i, test.description, tcpinfo.Rtt, test.tcpinfo.Rtt)
		}

		if tcpinfo.Rttvar != test.tcpinfo.Rttvar {
			t.Errorf("Test %d %s tcpinfo.Rttvar:%d != test.tcpinfo.Rttvar:%d", i, test.description, tcpinfo.Rttvar, test.tcpinfo.Rttvar)
		}

		if tcpinfo.SndSsthresh != test.tcpinfo.SndSsthresh {
			t.Errorf("Test %d %s tcpinfo.SndSsthresh:%d != test.tcpinfo.SndSsthresh:%d", i, test.description, tcpinfo.SndSsthresh, test.tcpinfo.SndSsthresh)
		}

		if tcpinfo.SndCwnd != test.tcpinfo.SndCwnd {
			t.Errorf("Test %d %s tcpinfo.SndCwnd:%d != test.tcpinfo.SndCwnd:%d", i, test.description, tcpinfo.SndCwnd, test.tcpinfo.SndCwnd)
		}

		if tcpinfo.AdvMss != test.tcpinfo.AdvMss {
			t.Errorf("Test %d %s tcpinfo.AdvMss:%d != test.tcpinfo.AdvMss:%d", i, test.description, tcpinfo.AdvMss, test.tcpinfo.AdvMss)
		}

		if tcpinfo.Reordering != test.tcpinfo.Reordering {
			t.Errorf("Test %d %s tcpinfo.Reordering:%d != test.tcpinfo.Reordering:%d", i, test.description, tcpinfo.Reordering, test.tcpinfo.Reordering)
		}

		if tcpinfo.RcvRtt != test.tcpinfo.RcvRtt {
			t.Errorf("Test %d %s tcpinfo.RcvRtt:%d != test.tcpinfo.RcvRtt:%d", i, test.description, tcpinfo.RcvRtt, test.tcpinfo.RcvRtt)
		}

		if tcpinfo.RcvSpace != test.tcpinfo.RcvSpace {
			t.Errorf("Test %d %s tcpinfo.RcvSpace:%d != test.tcpinfo.RcvSpace:%d", i, test.description, tcpinfo.RcvSpace, test.tcpinfo.RcvSpace)
		}

		if tcpinfo.TotalRetrans != test.tcpinfo.TotalRetrans {
			t.Errorf("Test %d %s tcpinfo.TotalRetrans:%d != test.tcpinfo.TotalRetrans:%d", i, test.description, tcpinfo.TotalRetrans, test.tcpinfo.TotalRetrans)
		}

		if tcpinfo.PacingRate != test.tcpinfo.PacingRate {
			t.Errorf("Test %d %s tcpinfo.PacingRate:%d != test.tcpinfo.PacingRate:%d", i, test.description, tcpinfo.PacingRate, test.tcpinfo.PacingRate)
		}

		if tcpinfo.MaxPacingRate != test.tcpinfo.MaxPacingRate {
			t.Errorf("Test %d %s tcpinfo.MaxPacingRate:%d != test.tcpinfo.MaxPacingRate:%d", i, test.description, tcpinfo.MaxPacingRate, test.tcpinfo.MaxPacingRate)
		}

		if tcpinfo.BytesAcked != test.tcpinfo.BytesAcked {
			t.Errorf("Test %d %s tcpinfo.BytesAcked:%d != test.tcpinfo.BytesAcked:%d", i, test.description, tcpinfo.BytesAcked, test.tcpinfo.BytesAcked)
		}

		if tcpinfo.BytesReceived != test.tcpinfo.BytesReceived {
			t.Errorf("Test %d %s tcpinfo.BytesReceived:%d != test.tcpinfo.BytesReceived:%d", i, test.description, tcpinfo.BytesReceived, test.tcpinfo.BytesReceived)
		}

		if tcpinfo.SegsOut != test.tcpinfo.SegsOut {
			t.Errorf("Test %d %s tcpinfo.SegsOut:%d != test.tcpinfo.SegsOut:%d", i, test.description, tcpinfo.SegsOut, test.tcpinfo.SegsOut)
		}

		if tcpinfo.SegsIn != test.tcpinfo.SegsIn {
			t.Errorf("Test %d %s tcpinfo.SegsIn:%d != test.tcpinfo.SegsIn:%d", i, test.description, tcpinfo.SegsIn, test.tcpinfo.SegsIn)
		}

		if tcpinfo.NotSentBytes != test.tcpinfo.NotSentBytes {
			t.Errorf("Test %d %s tcpinfo.NotSentBytes:%d != test.tcpinfo.NotSentBytes:%d", i, test.description, tcpinfo.NotSentBytes, test.tcpinfo.NotSentBytes)
		}

		if tcpinfo.MinRtt != test.tcpinfo.MinRtt {
			t.Errorf("Test %d %s tcpinfo.MinRtt:%d != test.tcpinfo.MinRtt:%d", i, test.description, tcpinfo.MinRtt, test.tcpinfo.MinRtt)
		}

		if tcpinfo.DataSegsIn != test.tcpinfo.DataSegsIn {
			t.Errorf("Test %d %s tcpinfo.DataSegsIn:%d != test.tcpinfo.DataSegsIn:%d", i, test.description, tcpinfo.DataSegsIn, test.tcpinfo.DataSegsIn)
		}

		if tcpinfo.DataSegsOut != test.tcpinfo.DataSegsOut {
			t.Errorf("Test %d %s tcpinfo.DataSegsOut:%d != test.tcpinfo.DataSegsOut:%d", i, test.description, tcpinfo.DataSegsOut, test.tcpinfo.DataSegsOut)
		}

		if tcpinfo.DeliveryRate != test.tcpinfo.DeliveryRate {
			t.Errorf("Test %d %s tcpinfo.DeliveryRate:%d != test.tcpinfo.DeliveryRate:%d", i, test.description, tcpinfo.DeliveryRate, test.tcpinfo.DeliveryRate)
		}

		if tcpinfo.BusyTime != test.tcpinfo.BusyTime {
			t.Errorf("Test %d %s tcpinfo.BusyTime:%d != test.tcpinfo.BusyTime:%d", i, test.description, tcpinfo.BusyTime, test.tcpinfo.BusyTime)
		}

		if tcpinfo.RwndLimited != test.tcpinfo.RwndLimited {
			t.Errorf("Test %d %s tcpinfo.RwndLimited:%d != test.tcpinfo.RwndLimited:%d", i, test.description, tcpinfo.RwndLimited, test.tcpinfo.RwndLimited)
		}

		if tcpinfo.SndbufLimited != test.tcpinfo.SndbufLimited {
			t.Errorf("Test %d %s tcpinfo.SndbufLimited:%d != test.tcpinfo.SndbufLimited:%d", i, test.description, tcpinfo.SndbufLimited, test.tcpinfo.SndbufLimited)
		}

		if tcpinfo.Delivered != test.tcpinfo.Delivered {
			t.Errorf("Test %d %s tcpinfo.Delivered:%d != test.tcpinfo.Delivered:%d", i, test.description, tcpinfo.Delivered, test.tcpinfo.Delivered)
		}

		if tcpinfo.DeliveredCe != test.tcpinfo.DeliveredCe {
			t.Errorf("Test %d %s tcpinfo.DeliveredCe:%d != test.tcpinfo.DeliveredCe:%d", i, test.description, tcpinfo.DeliveredCe, test.tcpinfo.DeliveredCe)
		}

		if tcpinfo.BytesSent != test.tcpinfo.BytesSent {
			t.Errorf("Test %d %s tcpinfo.BytesSent:%d != test.tcpinfo.BytesSent:%d", i, test.description, tcpinfo.BytesSent, test.tcpinfo.BytesSent)
		}

		if tcpinfo.BytesRetrans != test.tcpinfo.BytesRetrans {
			t.Errorf("Test %d %s tcpinfo.BytesRetrans:%d != test.tcpinfo.BytesRetrans:%d", i, test.description, tcpinfo.BytesRetrans, test.tcpinfo.BytesRetrans)
		}

		if tcpinfo.DsackDups != test.tcpinfo.DsackDups {
			t.Errorf("Test %d %s tcpinfo.DsackDups:%d != test.tcpinfo.DsackDups:%d", i, test.description, tcpinfo.DsackDups, test.tcpinfo.DsackDups)
		}

		if tcpinfo.ReordSeen != test.tcpinfo.ReordSeen {
			t.Errorf("Test %d %s tcpinfo.ReordSeen:%d != test.tcpinfo.ReordSeen:%d", i, test.description, tcpinfo.ReordSeen, test.tcpinfo.ReordSeen)
		}

		if tcpinfo.RcvOoopack != test.tcpinfo.RcvOoopack {
			t.Errorf("Test %d %s tcpinfo.RcvOoopack:%d != test.tcpinfo.RcvOoopack:%d", i, test.description, tcpinfo.RcvOoopack, test.tcpinfo.RcvOoopack)
		}

		if tcpinfo.SndWnd != test.tcpinfo.SndWnd {
			t.Errorf("Test %d %s tcpinfo.SndWnd:%d != test.tcpinfo.SndWnd:%d", i, test.description, tcpinfo.SndWnd, test.tcpinfo.SndWnd)
		}

		if tcpinfo.RcvWnd != test.tcpinfo.RcvWnd {
			t.Errorf("Test %d %s tcpinfo.RcvWnd:%d != test.tcpinfo.RcvWnd:%d", i, test.description, tcpinfo.RcvWnd, test.tcpinfo.RcvWnd)
		}

		if tcpinfo.Rehash != test.tcpinfo.Rehash {
			t.Errorf("Test %d %s tcpinfo.Rehash:%d != test.tcpinfo.Rehash:%d", i, test.description, tcpinfo.Rehash, test.tcpinfo.Rehash)
		}

		if tcpinfo.TotalRTO != test.tcpinfo.TotalRTO {
			t.Errorf("Test %d %s tcpinfo.TotalRTO:%d != test.tcpinfo.TotalRTO:%d", i, test.description, tcpinfo.TotalRTO, test.tcpinfo.TotalRTO)
		}

		if tcpinfo.TotalRTORecoveries != test.tcpinfo.TotalRTORecoveries {
			t.Errorf("Test %d %s tcpinfo.TotalRTORecoveries:%d != test.tcpinfo.TotalRTORecoveries:%d", i, test.description, tcpinfo.TotalRTORecoveries, test.tcpinfo.TotalRTORecoveries)
		}

		if tcpinfo.TotalRTOTime != test.tcpinfo.TotalRTOTime {
			t.Errorf("Test %d %s tcpinfo.TotalRTOTime:%d != test.tcpinfo.TotalRTOTime:%d", i, test.description, tcpinfo.TotalRTOTime, test.tcpinfo.TotalRTOTime)
		}

	}
}
