package xtcpnl

import (
	"encoding/hex"
	"io"
	"net"
	"os"
	"testing"
)

type DeserializeInetDiagSockIDTest struct {
	description string
	filename    string
	sport       int
	dport       int
	proto       int
	srcip       net.IP
	dstip       net.IP
	interf      int
	cookie      int
}

// go test -run=TestDeserializeInetDiagSockID

func TestDeserializeInetDiagSockID(t *testing.T) {
	var tests = []DeserializeInetDiagSockIDTest{
		{
			description: "6_10_3 single_packet_response",
			filename:    tdReplyPort4322_6_10_3,
			sport:       54779,
			dport:       4322,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      1536,
		},
		{
			description: "single_packet_response",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet.pcap",
			sport:       26699,
			dport:       4001,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      4106,
		},
		{
			description: "port4001",
			filename:    tdReplyPort4001_6_6_44,
			sport:       26699,
			dport:       4001,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      4106,
		},
		{
			description: tnPort4018,
			filename:    tdReplyPort4018_6_6_44,
			sport:       1789,
			dport:       4018,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      27550,
		},
		{
			description: tnPort4018,
			filename:    "./testdata/5_15_164/netlink_sock_diag_reply_single_packet_port4000.pcap",
			sport:       14385,
			dport:       4000,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      4111,
		},
		{
			description: "4_19_319_port4005",
			filename:    tdReplyPort4005_4_19_319,
			sport:       44585,
			dport:       4005,
			proto:       4,
			srcip:       net.ParseIP("127.0.0.1"),
			dstip:       net.ParseIP("127.0.0.1"),
			interf:      0,
			cookie:      9,
		},
		{
			description: "port443v4",
			filename:    tdReplyPort443V4_6_6_44,
			sport:       36821,
			dport:       443,
			proto:       4,
			srcip:       net.ParseIP("172.16.50.236"),
			dstip:       net.ParseIP("34.96.128.111"),
			interf:      0,
			cookie:      36743,
		},
		{
			description: "port443v6",
			filename:    tdReplyPort443V6_6_6_44,
			sport:       46965,
			dport:       443,
			proto:       6,
			srcip:       net.ParseIP("2603:8000:9c00:9300:e4d4:5b27:2e76:ff0e"),
			dstip:       net.ParseIP("2607:f8b0:4007:817::200a"),
			interf:      0,
			cookie:      94476,
		},
		{
			description: "port443v6_2",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port443v6_2.pcap",
			sport:       43163,
			dport:       443,
			proto:       6,
			srcip:       net.ParseIP("2603:8000:9c00:9300:e4d4:5b27:2e76:ff0e"),
			dstip:       net.ParseIP("2607:f8b0:4007:80f::200a"),
			interf:      0,
			cookie:      2821,
		},
		{
			description: tnSport26546V4,
			filename:    tdResp26546_7_0_3,
			sport:       26546,
			dport:       443,
			proto:       4,
			srcip:       net.ParseIP("10.0.6.188"),
			dstip:       net.ParseIP("140.82.114.25"),
			interf:      0,
			cookie:      45064,
		},
		{
			description: "7_0_3 sport63282 dport443 rcvrtt",
			filename:    "./testdata/7_0_3/netlink_sock_diag_response_7_0_3_sport63282_dport443_rcvrtt.pcap",
			sport:       63282,
			dport:       443,
			proto:       4,
			srcip:       net.ParseIP("10.0.6.188"),
			dstip:       net.ParseIP("3.140.122.174"),
			interf:      0,
			cookie:      24592,
		},
		{
			description: tnSport19000V6,
			filename:    tdResp19000V6_7_0_3,
			sport:       19000,
			dport:       10156,
			proto:       6,
			srcip:       net.ParseIP("::1"),
			dstip:       net.ParseIP("::1"),
			interf:      0,
			cookie:      4215,
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

		buf := bs[PcapInetDiagSockIDOffsetCst : PcapInetDiagSockIDOffsetCst+InetDiagSockIDSizeCst]

		// bsMax := len(bs)
		// if bsMax > 80 {
		// 	bsMax = 80
		// }
		// bufMax := len(buf)
		// if bufMax > 80 {
		// 	bufMax = 80
		// }
		// t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs[:bsMax]))
		// t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf[:bufMax]))

		s := new(InetDiagSockID)

		_, errD := DeserializeInetDiagSockID(buf, s)
		if errD != nil {
			t.Fatal("Test Failed DeserializeInetDiagSockID err:", errD)
		}

		if int(s.SPort) != test.sport {
			t.Errorf("Test %d %s Failed decoded SPort incorrect, received %d, expected %d", i, test.description, int(s.SPort), test.sport)
		}

		if int(s.DPort) != test.dport {
			t.Errorf("Test %d %s Failed decoded DPort, received %d, expected %d", i, test.description, int(s.DPort), test.dport)
		}

		switch test.proto {

		case 4:
			sourceIP := net.IP(s.SrcIP[0:4])
			if !sourceIP.Equal(test.srcip) {
				t.Logf("i:%d, SrcIP hex:%s", i, hex.EncodeToString(s.SrcIP[:]))
				t.Logf("i:%d, sourceIP:%s", i, sourceIP.To16().String())
				t.Errorf("Test %d %s Failed decoded SrcIP incorrect, received %s, expected %s", i, test.description, sourceIP.To4().String(), test.srcip)
			}

			destIP := net.IP(s.DstIP[0:4])
			if !destIP.Equal(test.dstip) {
				t.Logf("i:%d, DstIP hex:%s", i, hex.EncodeToString(s.DstIP[:]))
				t.Logf("i:%d, destIP:%s", i, destIP.To16().String())
				t.Errorf("Test %d %s Failed decoded DstIP incorrect, received %s, expected %s", i, test.description, destIP.To4().String(), test.dstip)
			}

		case 6:
			sourceIP := net.IP(s.SrcIP[0:16])
			if !sourceIP.Equal(test.srcip) {
				t.Logf("i:%d, SrcIP hex:%s", i, hex.EncodeToString(s.SrcIP[:]))
				t.Logf("i:%d, sourceIP:%s", i, sourceIP.To16().String())
				t.Errorf("Test %d %s Failed decoded SrcIP incorrect, received %s, expected %s", i, test.description, sourceIP.To4().String(), test.srcip)
			}

			destIP := net.IP(s.DstIP[0:16])
			if !destIP.Equal(test.dstip) {
				t.Logf("i:%d, DstIP hex:%s", i, hex.EncodeToString(s.DstIP[:]))
				t.Logf("i:%d, destIP:%s", i, destIP.To16().String())
				t.Errorf("Test %d %s Failed decoded DstIP incorrect, received %s, expected %s", i, test.description, destIP.To4().String(), test.dstip)
			}

		default:
			t.Errorf("Test %d %s Failed unknown proto", i, test.description)
		}

		if int(s.Interface) != test.interf {
			t.Errorf("Test %d %s Failed decoded Interface incorrect, received %d, expected %d", i, test.description, int(s.Interface), test.interf)
		}

		if int(s.Cookie) != test.cookie {
			t.Errorf("Test %d %s Failed decoded Cookie incorrect, received %d, expected %d", i, test.description, int(s.Cookie), test.cookie)
		}

	}
}
