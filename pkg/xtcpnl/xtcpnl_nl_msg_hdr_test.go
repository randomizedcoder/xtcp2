package xtcpnl

import (
	"io"
	"os"
	"strings"
	"testing"
)

type DeserializeNlMsgHdrTest struct {
	description string
	filename    string
	length      int
	tyype       int
	flags       int
	seq         int
	pid         int
}

func TestDeserializeNlMsgHdr(t *testing.T) {
	var tests = []DeserializeNlMsgHdrTest{
		{
			description: "request_all_response",
			filename:    "./testdata/6_6_44/large_netlink_sock_diag_protocol_export",
			length:      448,
			tyype:       20,
			flags:       2,
			seq:         123456,
			pid:         2469,
		},
		{
			description: "request_all_example2",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example2",
			length:      72,
			tyype:       20,
			flags:       769,
			seq:         123456,
			pid:         0,
		},
		{
			description: "request_all_example3",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example3",
			length:      72,
			tyype:       20,
			flags:       769,
			seq:         123456,
			pid:         0,
		},
		{
			description: "end_of_dump",
			filename:    "./testdata/6_6_44/netlink_end_of_dump_bytes",
			length:      20,
			tyype:       3,
			flags:       2,
			seq:         123456,
			pid:         2469,
		},
		{
			description: "end_of_dump2",
			filename:    "./testdata/6_10_3/netlink_end_of_dump_bytes",
			length:      20,
			tyype:       3,
			flags:       2,
			seq:         123456,
			pid:         1403,
		},
		{
			description: "6_10_3 end_of_dump_pcap",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
			length:      20,
			tyype:       3,
			flags:       2,
			seq:         123456,
			pid:         1438,
		},
		{
			description: "end_of_dump_pcap",
			filename:    "./testdata/6_6_44/netlink_sock_diag_response_dump_done.pcap",
			length:      20,
			tyype:       3,
			flags:       2,
			seq:         123456,
			pid:         2318,
		},
		{
			description: "4_19_319_end_of_dump_pcap",
			filename:    "./testdata/4_19_319/netlink_sock_diag_response_dump_done.pcap",
			length:      20,
			tyype:       3,
			flags:       2,
			seq:         123456,
			pid:         2883,
		},
		{
			description: "reply_single_packet.pcap",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet.pcap",
			length:      448,
			tyype:       20,
			flags:       2,
			seq:         123456,
			pid:         2316,
		},
		{
			description: "6_10_3 reply_single_packet.pcap",
			filename:    "./testdata/6_10_3/netlink_sock_diag_reply_single_packet_port4322.pcap",
			length:      456,
			tyype:       20,
			flags:       2,
			seq:         123456,
			pid:         1438,
		},
		{
			description: "5_15_164 reply_single_packet.pcap",
			filename:    "./testdata/5_15_164/netlink_sock_diag_reply_single_packet_port4000.pcap",
			length:      440,
			tyype:       20,
			flags:       2,
			seq:         123456,
			pid:         1573,
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
			buf = bs[PcapNetlinkOffsetCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst]
		} else {
			buf = bs
		}

		// if len(bs) != test.readLength {
		// 	t.Error("Test Failed incorrect file length test.length len(bs), test.readLength,", len(bs), test.readLength)
		// }

		nlh := new(NlMsgHdr)

		var errD error
		_, errD = DeserializeNlMsgHdr(buf, nlh)
		if errD != nil {
			t.Fatal("Test Failed DeserializeNlMsgHdrLengthAndType err", errD)
		}

		if int(nlh.Len) != test.length {
			t.Error("Test Failed decoded Len incorrect, received {}, expected {}", int(nlh.Len), test.length)
		}

		if int(nlh.Type) != test.tyype {
			t.Error("Test Failed decoded Type incorrect, received {}, expected {}", int(nlh.Type), test.tyype)
		}

		if int(nlh.Flags) != test.flags {
			t.Error("Test Failed decoded Flags incorrect, received {}, expected {}", int(nlh.Type), test.tyype)
		}

		if int(nlh.Seq) != test.seq {
			t.Error("Test Failed decoded Seq incorrect, received {}, expected {}", int(nlh.Seq), test.seq)
		}

		if int(nlh.Pid) != test.pid {
			t.Error("Test Failed decoded Pid incorrect, received {}, expected {}", int(nlh.Pid), test.pid)
		}

	}
}
