package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
)

type DecodeFromBytesSerializeToTest struct {
	description string
	filename    string
	debugLevel  int
}

// TestDecodeFromBytesSerializeTo decodes bytes saved from a netlink pcap
// into structs, and then serializes back to bytes
// go test -run=TestDecodeFromBytesSerializeTo
func TestDecodeFromBytesSerializeTo(t *testing.T) {
	var tests = []DecodeFromBytesSerializeToTest{
		{
			description: "verify_request",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes",
		},
		{
			description: "verify_request_all",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_a",
		},
		{
			description: "verify_request_all_example2",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example2",
		},
		{
			description: "verify_request_all_example3",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes_example3",
		},
		{
			description: "6_10_3 verify_request",
			filename:    "./testdata/6_10_3/netlink_sock_diag_request_single_packet.pcap",
			debugLevel:  11,
		},
		{
			description: "5_15_164 verify_request",
			filename:    "./testdata/5_15_164/netlink_sock_diag_request_single_packet.pcap",
			debugLevel:  11,
		},
		{
			description: "verify_request_allv4",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_single_packet_v4.pcap",
			debugLevel:  11,
		},
		{
			description: "4_19_319_verify_request_allv4",
			filename:    "./testdata/4_19_319/netlink_sock_diag_request_single_packet_v4.pcap",
			debugLevel:  11,
		},
		{
			description: "verify_request_allv6",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_single_packet_v6.pcap",
			debugLevel:  11,
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
			buf = bs[PcapNetlinkOffsetCst:]
		} else {
			buf = bs
		}

		nlh, req := DecodeNetlinkDagRequestFromBytes(buf)

		if test.debugLevel > 100 {
			t.Logf("test nlh:%v", nlh)
			t.Logf("test req:%v", req)
		}

		requestBytes := make([]byte, InetDiagRequestSizeCst)

		SerializeNetlinkDiagRequest(nlh, req, &requestBytes)

		if test.debugLevel >= 0 {
			t.Logf("i:%d, req:%v", i, req)
		}

		if test.debugLevel > 100 {
			t.Logf("i:%d, hex:%s", i, hex.EncodeToString(bs[0:80]))
			t.Logf("i:%d, hex:%s", i, hex.EncodeToString(buf[0:50]))
			t.Logf("i:%d, hex:%s", i, hex.EncodeToString(requestBytes[0:50]))
		}

		if !bytes.Equal(buf, requestBytes) {
			t.Error("Test Failed: !bytes.Equal(bs, requestBytes) expected {}, received {} ", bs, requestBytes)
		}
	}
}
