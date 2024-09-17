package xtcpnl

import (
	"encoding/binary"
	"testing"
)

type VerifySizeOfStructsTest struct {
	description string

	PcapHeaderSize       int
	PcapRecordHeaderSize int
	NlMsgHdrSize         int
	InetDiagReqV2Size    int
	InetDiagMsgSize      int
	InetDiagSockIDSize   int
	RTAttrSize           int
	MemInfoSize          int
	BBRInfoSize          int
	CGroupIDSize         int
	ClassIDSize          int
	DCTCPInfoSize        int
	PragueInfoSize       int
	ShutdownSize         int
	SkMemInfoSize        int
	SockOptSize          int
	TrafficClassSize     int
	TypeOfServiceSize    int
	VegasInfoSize        int

	TCPInfo6_10_3_Size   int
	TCPInfo6_6_44_Size   int
	TCPInfo5_4_281_Size  int
	TCPInfo4_19_219_Size int
	TCPInfo4_15_Size     int

	debugLevel int
}

// not doing CongInfo.  it's variable length

// TestVerifySizeOfStructs ensures the reflected size of the structs matches
// the constants... This test found that I had miss counted the length of some
// structs
// go test --run TestVerifySizeOfStructs
func TestVerifySizeOfStructs(t *testing.T) {
	var tests = []VerifySizeOfStructsTest{
		{
			description: "verify_sizes",

			PcapHeaderSize:       PcapHeaderSizeCst,
			PcapRecordHeaderSize: PcapRecordHeaderSizeCst,
			NlMsgHdrSize:         NlMsgHdrSizeCst,
			InetDiagReqV2Size:    InetDiagReqV2SizeCst,
			InetDiagMsgSize:      InetDiagMsgSizeCst,
			InetDiagSockIDSize:   InetDiagSockIDSizeCst,
			RTAttrSize:           RTAttrSizeCst,
			MemInfoSize:          MemInfoSizeCst,

			BBRInfoSize:       BBRInfoSizeCst,
			CGroupIDSize:      CGroupIDSizeCst,
			ClassIDSize:       ClassIDSizeCst,
			DCTCPInfoSize:     DCTCPInfoSizeCst,
			PragueInfoSize:    PragueInfoSizeCst,
			ShutdownSize:      ShutdownSizeCst,
			SkMemInfoSize:     SkMemInfoSizeCst,
			SockOptSize:       SockOptSizeCst,
			TrafficClassSize:  TrafficClassSizeCst,
			TypeOfServiceSize: TypeOfServiceSizeCst,
			VegasInfoSize:     VegasInfoSizeCst,

			TCPInfo6_10_3_Size:   TCPInfo6_10_3_SizeCst,
			TCPInfo6_6_44_Size:   TCPInfo6_6_44_SizeCst,
			TCPInfo5_4_281_Size:  TCPInfo5_4_281_SizeCst,
			TCPInfo4_19_219_Size: TCPInfo4_19_219_SizeCst,
			TCPInfo4_15_Size:     TCPInfo4_15_SizeCst,

			debugLevel: 111,
		},
	}

	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s", i, test.description)

		p := new(PcapHeader)
		if binary.Size(*p) != test.PcapHeaderSize {
			t.Errorf("Test i:%d Failed: binary.Size(*p):%d != test.PcapHeaderSize:%d", i, binary.Size(*p), test.PcapHeaderSize)
		}

		rp := new(PcapRecordHeader)
		if binary.Size(*rp) != test.PcapRecordHeaderSize {
			t.Errorf("Test i:%d Failed: binary.Size(*rp):%d != test.PcapRecordHeaderSize:%d", i, binary.Size(*rp), test.PcapRecordHeaderSize)
		}

		nlmh := new(NlMsgHdr)
		if binary.Size(*nlmh) != test.NlMsgHdrSize {
			t.Errorf("Test i:%d Failed: binary.Size(*nlmh):%d != test.NlMsgHdrSize:%d", i, binary.Size(*nlmh), test.NlMsgHdrSize)
		}

		ir := new(InetDiagReqV2)
		if binary.Size(*ir) != test.InetDiagReqV2Size {
			t.Errorf("Test i:%d Failed: binary.Size(*ir):%d != test.InetDiagReqV2Size:%d", i, binary.Size(*ir), test.InetDiagReqV2Size)
		}

		idm := new(InetDiagMsg)
		if binary.Size(*idm) != test.InetDiagMsgSize {
			t.Errorf("Test i:%d Failed: binary.Size(*idm):%d != test.InetDiagMsgSize:%d", i, binary.Size(*idm), test.InetDiagMsgSize)
		}

		sid := new(InetDiagSockID)
		if binary.Size(*sid) != test.InetDiagSockIDSize {
			t.Errorf("Test i:%d Failed: binary.Size(*sid):%d != test.InetDiagSockIDSize:%d", i, binary.Size(*sid), test.InetDiagSockIDSize)
		}

		r := new(RTAttr)
		if binary.Size(*r) != test.RTAttrSize {
			t.Errorf("Test i:%d Failed: binary.Size(*r):%d != test.RTAttrSize:%d", i, binary.Size(*r), test.RTAttrSize)
		}

		m := new(MemInfo)
		if binary.Size(*m) != test.MemInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*m):%d != test.MemInfoSize:%d", i, binary.Size(*m), test.MemInfoSize)
		}

		bi := new(BBRInfo)
		if binary.Size(*bi) != test.BBRInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*bi):%d != test.BBRInfoSize:%d", i, binary.Size(*bi), test.BBRInfoSize)
		}

		cgi := new(CGroupID)
		if binary.Size(*cgi) != test.CGroupIDSize {
			t.Errorf("Test i:%d Failed: binary.Size(*cgi):%d != test.CGroupIDSize:%d", i, binary.Size(*cgi), test.CGroupIDSize)
		}

		ci := new(ClassID)
		if binary.Size(*ci) != test.ClassIDSize {
			t.Errorf("Test i:%d Failed: binary.Size(*ci):%d != test.ClassIDSize:%d", i, binary.Size(*ci), test.ClassIDSize)
		}

		di := new(DCTCPInfo)
		if binary.Size(*di) != test.DCTCPInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*di):%d != test.DCTCPInfoSize:%d", i, binary.Size(*di), test.DCTCPInfoSize)
		}

		pi := new(PragueInfo)
		if binary.Size(*pi) != test.PragueInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*pi):%d != test.PragueInfoSize:%d", i, binary.Size(*pi), test.PragueInfoSize)
		}

		s := new(Shutdown)
		if binary.Size(*s) != test.ShutdownSize {
			t.Errorf("Test i:%d Failed: binary.Size(*s):%d != test.ShutdownSize:%d", i, binary.Size(*s), test.ShutdownSize)
		}

		skmem := new(SkMemInfo)
		if binary.Size(*skmem) != test.SkMemInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*skmem):%d != test.SkMemInfoSize:%d", i, binary.Size(*skmem), test.SkMemInfoSize)
		}

		so := new(SockOpt)
		if binary.Size(*so) != test.SockOptSize {
			t.Errorf("Test i:%d Failed: binary.Size(*so):%d != test.SockOptSize:%d", i, binary.Size(*so), test.SockOptSize)
		}

		tc := new(TrafficClass)
		if binary.Size(*tc) != test.TrafficClassSize {
			t.Errorf("Test i:%d Failed: binary.Size(*tc):%d != test.TrafficClassSize:%d", i, binary.Size(*tc), test.TrafficClassSize)
		}

		tos := new(TypeOfService)
		if binary.Size(*tos) != test.TypeOfServiceSize {
			t.Errorf("Test i:%d Failed: binary.Size(*tos):%d != test.TypeOfServiceSize:%d", i, binary.Size(*tos), test.TypeOfServiceSize)
		}

		vi := new(VegasInfo)
		if binary.Size(*vi) != test.VegasInfoSize {
			t.Errorf("Test i:%d Failed: binary.Size(*vi):%d != test.VegasInfoSize:%d", i, binary.Size(*tos), test.VegasInfoSize)
		}

		//------------

		t1 := new(TCPInfo6_10_3)
		if binary.Size(*t1) != test.TCPInfo6_10_3_Size {
			t.Errorf("Test i:%d Failed: binary.Size(*t1):%d != test.TCPInfo6_10_3_Size:%d", i, binary.Size(*t1), test.TCPInfo6_10_3_Size)
		}

		t2 := new(TCPInfo6_6_44)
		if binary.Size(*t2) != test.TCPInfo6_6_44_Size {
			t.Errorf("Test i:%d Failed: binary.Size(*t2):%d != test.TCPInfo6_6_44_Size:%d", i, binary.Size(*t2), test.TCPInfo6_6_44_Size)
		}

		t3 := new(TCPInfo5_4_281)
		if binary.Size(*t3) != test.TCPInfo5_4_281_Size {
			t.Errorf("Test i:%d Failed: binary.Size(*t3):%d != test.TCPInfo5_4_281_Size:%d", i, binary.Size(*t3), test.TCPInfo5_4_281_Size)
		}

		t4 := new(TCPInfo4_19_219)
		if binary.Size(*t4) != test.TCPInfo4_19_219_Size {
			t.Errorf("Test i:%d Failed: binary.Size(*t4):%d != test.TCPInfo4_19_219_Size:%d", i, binary.Size(*t4), test.TCPInfo4_19_219_Size)
		}

		t5 := new(TCPInfo4_15)
		if binary.Size(*t5) != test.TCPInfo4_15_Size {
			t.Errorf("Test i:%d Failed: binary.Size(*t5):%d != test.TCPInfo4_15_Size:%d", i, binary.Size(*t5), test.TCPInfo4_15_Size)
		}

		//------------

	}
}
