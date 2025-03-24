package xtcpnl

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
)

var (
	resultN NlMsgHdr
	resultR InetDiagReqV2
)

func BenchmarkDecodeNetlinkDagRequestFromBytes(b *testing.B) {
	var tests = []DecodeFromBytesSerializeToTest{
		{
			description: "verify_request",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes",
		},
	}

	bs, err := Readfile(tests[0].filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	var (
		nlh NlMsgHdr
		req InetDiagReqV2
	)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nlh, req = DecodeNetlinkDagRequestFromBytes(bs)
	}
	resultN = nlh
	resultR = req
}

var (
	resultB []byte
)

func BenchmarkSerializeNetlinkDagRequest(b *testing.B) {
	var tests = []DecodeFromBytesSerializeToTest{
		{
			description: "verify_request",
			filename:    "./testdata/6_6_44/netlink_sock_diag_request_bytes",
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	var (
		nlh NlMsgHdr
		req InetDiagReqV2
	)
	nlh, req = DecodeNetlinkDagRequestFromBytes(bs)

	requestBytes := make([]byte, InetDiagRequestSizeCst)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SerializeNetlinkDiagRequest(nlh, req, &requestBytes)
	}
	resultB = requestBytes
}

var (
	resultNH NlMsgHdr
)

func BenchmarkDeserializeNlMsgHdr(b *testing.B) {
	f := func(data []byte, nlmsghr *NlMsgHdr) (n int, err error) {
		return DeserializeNlMsgHdr(data, nlmsghr)
	}
	DeserializeNlMsgHdrBoth(b, f)
}

func BenchmarkDeserializeNlMsgHdrReflection(b *testing.B) {
	f := func(data []byte, nlmsghr *NlMsgHdr) (n int, err error) {
		return DeserializeNlMsgHdrRelection(data, nlmsghr)
	}
	DeserializeNlMsgHdrBoth(b, f)
}

func DeserializeNlMsgHdrBoth(b *testing.B, Func func(data []byte, nlmsghr *NlMsgHdr) (n int, err error)) {
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
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	var buf []byte
	if strings.HasSuffix(test.filename, ".pcap") {
		buf = bs[PcapNetlinkOffsetCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst]
	} else {
		buf = bs
	}

	nlh := new(NlMsgHdr)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeNlMsgHdrLengthAndType(buf, nlh)
		_, errD = Func(buf, nlh)
		if errD != nil {
			b.Error("Test Failed DeserializeNlMsgHdrLengthAndType err", errD)
		}
	}
	resultNH = *nlh
}

var (
	resultsIDR InetDiagReqV2
	resultsSID InetDiagSockID
)

// go test -bench=BenchmarkDeserializeInetDiagReqV2
func BenchmarkDeserializeInetDiagReqV2(b *testing.B) {

	f := func(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagReqV2(data, inetdiagreqv2, s)
	}

	DeserializeInetDiagReqV2Both(b, f)

}

func BenchmarkDeserializeInetDiagReqV2Reflection(b *testing.B) {

	f := func(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagReqV2Relection(data, inetdiagreqv2, s)
	}

	DeserializeInetDiagReqV2Both(b, f)

}

func DeserializeInetDiagReqV2Both(b *testing.B, Func func(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error)) {
	var tests = []DeserializeInetDiagReqV2Test{
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

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	var buf []byte
	if strings.HasSuffix(test.filename, ".pcap") {
		buf = bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
	} else {
		buf = bs[NlMsgHdrSizeCst : NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
	}

	idr := new(InetDiagReqV2)
	s := new(InetDiagSockID)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeInetDiagReqV2(buf, idr, s)
		_, errD = Func(buf, idr, s)
		if errD != nil {
			b.Error("Test Failed DeserializeInetDiagReqV2 err", errD)
		}
	}
	resultsIDR = *idr
	resultsSID = *s
}

var (
	resultIDM   InetDiagMsg
	resultsSIDD InetDiagSockID
)

// go test -bench=BenchmarkDeserializeInetDiagMsg

func BenchmarkDeserializeInetDiagMsg(b *testing.B) {

	f := func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagMsg(data, idm, s)
	}

	DeserializeInetDiagMsgBoth(b, f)
}

func BenchmarkDeserializeInetDiagMsgReflection(b *testing.B) {

	f := func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagMsgViaReflection(data, idm, s)
	}

	DeserializeInetDiagMsgBoth(b, f)
}

func DeserializeInetDiagMsgBoth(b *testing.B, Func func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error)) {
	var tests = []DeserializeInetDiagMsgTest{
		{
			description: "port4018",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4018.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 9854, // honestly not sure if this correct. wireshark doesn't decode this. The timer seems about correct
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   204403,

			debugLevel: 11,
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	var buf []byte
	if strings.HasSuffix(test.filename, ".pcap") {
		buf = bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagMsgSizeCst]
	}

	idm := new(InetDiagMsg)
	s := new(InetDiagSockID)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = Func(buf, idm, s)
		if errD != nil {
			b.Error("Test Failed DeserializeInetDiagMsg err", errD)
		}
	}
	resultIDM = *idm
	resultsSIDD = *s
}

var (
	resultS InetDiagSockID
)

// go test -bench=BenchmarkDeserializeInetDiagSockID

func BenchmarkDeserializeInetDiagSockID(b *testing.B) {
	f := func(data []byte, sockid *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagSockID(data, sockid)
	}
	DeserializeInetDiagSockIDBoth(b, f)
}

func BenchmarkDeserializeInetDiagSockIDReflection(b *testing.B) {
	f := func(data []byte, sockid *InetDiagSockID) (n int, err error) {
		return DeserializeInetDiagSockIDReflection(data, sockid)
	}
	DeserializeInetDiagSockIDBoth(b, f)
}

func DeserializeInetDiagSockIDBoth(b *testing.B, Func func(data []byte, sockid *InetDiagSockID) (n int, err error)) {
	var tests = []DeserializeInetDiagSockIDTest{
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
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs[PcapInetDiagSockIDOffsetCst : PcapInetDiagSockIDOffsetCst+InetDiagSockIDSizeCst]

	s := new(InetDiagSockID)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeInetDiagSockID(buf, s)
		_, errD = Func(buf, s)
		if errD != nil {
			b.Error("Test Failed DeserializeInetDiagSockID err", errD)
		}
	}
	resultS = *s
}

var (
	resultRTA RTAttr
)

// go test -bench=BenchmarkDeserializeRTAttr

func BenchmarkDeserializeRTAttr(b *testing.B) {
	f := func(data []byte, rta *RTAttr) (n int, err error) {
		return DeserializeRTAttr(data, rta)
	}
	DeserializeRTAttrBoth(b, f)
}

func BenchmarkDeserializeRTAttrReflection(b *testing.B) {
	f := func(data []byte, rta *RTAttr) (n int, err error) {
		return DeserializeRTAttrReflection(data, rta)
	}
	DeserializeRTAttrBoth(b, f)
}

func DeserializeRTAttrBoth(b *testing.B, Func func(data []byte, rta *RTAttr) (n int, err error)) {
	var tests = []DeserializeRTAttrTest{
		{
			description: "attribute_info",
			filename:    "./testdata/6_6_44/attribute_info",
			length:      244,
			tyype:       2,
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	rta := new(RTAttr)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		//_, errD = DeserializeRTAttr(buf, rta)
		_, errD = Func(buf, rta)
		if errD != nil {
			b.Error("Test Failed DeserializeRTAttr errD", errD)
		}

	}
	resultRTA = *rta
}
