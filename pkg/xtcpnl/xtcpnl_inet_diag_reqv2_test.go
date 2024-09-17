package xtcpnl

import (
	"io"
	"os"
	"strings"
	"testing"
)

type DeserializeInetDiagReqV2Test struct {
	description string
	filename    string
	length      int
	family      int
	protocol    int
	ext         int
	pad         int
	states      int
}

func TestDeserializeInetDiagReqV2(t *testing.T) {
	var tests = []DeserializeInetDiagReqV2Test{
		{
			description: "verify_request_all",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example2",
			length:      72,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "verify_request_all_example3",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example3",
			length:      72,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "6_10_3 request",
			filename:    "./testdata/6_10_3/netlink_sock_diag_request_single_packet.pcap",
			length:      128,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "request_v4",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_single_packet_v4.pcap",
			length:      128,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "5_15_164 request_v4",
			filename:    "./testdata/5_15_164/netlink_sock_diag_request_single_packet.pcap",
			length:      128,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "4_19_319_request_v4",
			filename:    "./testdata/4_19_319/netlink_sock_diag_request_single_packet_v4.pcap",
			length:      128,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
		},
		{
			description: "request_v6",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_single_packet_v6.pcap",
			length:      128,
			family:      2,
			protocol:    6,
			ext:         127,
			pad:         0,
			states:      4282318848,
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

		var buf []byte
		if strings.HasSuffix(test.filename, ".pcap") {
			buf = bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
		} else {
			buf = bs[NlMsgHdrSizeCst : NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
		}

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

		if len(bs) != test.length {
			t.Errorf("Test %d %s Failed len(bs):%d != test.length:%d", i, test.description, len(bs), test.length)
		}

		idr := new(InetDiagReqV2)
		s := new(InetDiagSockID)

		_, errD := DeserializeInetDiagReqV2(buf, idr, s)
		if errD != nil {
			t.Fatal("Test Failed DeserializeInetDiagReqV2 err", errD)
		}

		if int(idr.SDiagFamily) != test.family {
			t.Errorf("Test %d %s int(idr.SDiagFamily):%d != test.family:%d", i, test.description, int(idr.SDiagFamily), test.family)
		}

		if int(idr.SDiagProtocol) != test.protocol {
			t.Error("Test Failed decoded SDiagProtocol, received {}, expected {}", int(idr.SDiagProtocol), test.protocol)
		}

		if int(idr.IDiagExt) != test.ext {
			t.Error("Test Failed decoded IDiagExt incorrect, received {}, expected {}", int(idr.IDiagExt), test.ext)
		}

		if int(idr.Pad) != test.pad {
			t.Error("Test Failed decoded Pad incorrect, received {}, expected {}", int(idr.Pad), test.pad)
		}

		if int(idr.IDiagStates) != test.states {
			t.Error("Test Failed decoded IDiagStates incorrect, received {}, expected {}", int(idr.IDiagStates), test.states)
		}

	}
}
