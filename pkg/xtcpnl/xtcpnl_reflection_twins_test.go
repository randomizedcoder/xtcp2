package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. This file is the
// canonical rationale; xtcpnl_perf_gate_test.go is the gate that fails on
// convergence.

// Reflection twin decoders — test-only.
//
// Each function here is a second, independent implementation of the
// corresponding hand-rolled little-endian Deserialize* decoder in the
// shipped package. They exist for two reasons:
//
//  1. Correctness cross-check. Two implementations decoding the same
//     captured bytes to the same struct catches transposed or mis-sized
//     fields, which a single implementation plus a hand-written `want`
//     cannot — the decoder and its test can be wrong together.
//  2. Benchmark contrast. The paired Benchmark*/Benchmark*Reflection
//     pairs show what the manual decoders buy.
//
// They live in a _test.go file deliberately: `binary.Read` uses
// reflection, and the shipped library must contain none. Nothing outside
// this package ever called them, so the move is invisible to consumers.
//
// A twin can only confirm that a Go struct decodes consistently with
// itself; it can never discover a field the struct is missing. That job
// belongs to the kernel-source layout oracle — see
// docs/netlink/coverage-expansion.md.

import (
	"bytes"
	"encoding/binary"
)

func deserializeBBRInfoReflection(data []byte, b *BBRInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, b)
	if err != nil {
		return 0, err
	}

	return BBRInfoReadCst, err
}

func deserializeCGroupIDReflection(data []byte, c *CGroupID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}

	return CGroupIDSizeCst, err
}

func deserializeClassIDReflection(data []byte, c *ClassID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func deserializeDCTCPInfoReflection(data []byte, d *DCTCPInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, d)
	if err != nil {
		return 0, err
	}

	return DCTCPInfoReadCst, err
}

func deserializeIfAddrmsgReflection(data []byte, m *IfAddrmsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return IfAddrmsgReadCst, err
}

func deserializeIfInfomsgReflection(data []byte, m *IfInfomsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return IfInfomsgReadCst, err
}

func deserializeInetDiagMsgReflection(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, idm)
	if err != nil {
		return 0, err
	}

	return InetDiagMsgReadCst, err
}

func deserializeInetDiagReqV2Reflection(data []byte, inetdiagreqv2 *InetDiagReqV2, s *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, inetdiagreqv2)
	if err != nil {
		return 0, err
	}

	return InetDiagReqV2SizeCst, err
}

func deserializeInetDiagSockIDReflection(data []byte, sockid *InetDiagSockID) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, sockid)
	if err != nil {
		return 0, err
	}

	return InetDiagSockIDReadCst, err
}

func deserializeMemInfoReflection(data []byte, mi *MemInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, mi)
	if err != nil {
		return 0, err
	}

	return MemInfoReadCst, err
}

func deserializeNdMsgReflection(data []byte, m *NdMsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return NdMsgReadCst, err
}

func deserializeNdaCacheInfoReflection(data []byte, c *NdaCacheInfo) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}

	return NdaCacheInfoSizeCst, err
}

func deserializeNlMsgHdrReflection(data []byte, nlmsghr *NlMsgHdr) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, nlmsghr)
	if err != nil {
		return 0, err
	}

	return NlMsgHdrReadCst, err
}

func deserializePcapHeaderReflection(data []byte, ph *PcapHeader) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, ph)
	if err != nil {
		return 0, err
	}

	return PcapHeaderSizeCst, err
}

func deserializePcapRecordHeaderReflection(data []byte, prh *PcapRecordHeader) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, prh)
	if err != nil {
		return 0, err
	}

	return PcapRecordHeaderSizeCst, err
}

func deserializePragueInfoReflection(data []byte, p *PragueInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, p)
	if err != nil {
		return 0, err
	}

	return PragueInfoReadCst, err
}

func deserializeRTAttrReflection(data []byte, rta *RTAttr) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, rta)
	if err != nil {
		return 0, err
	}

	return RTAttrReadCst, err
}

func deserializeRtMsgReflection(data []byte, m *RtMsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return RtMsgReadCst, err
}

func deserializeShutdownReflection(data []byte, s *Shutdown) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, s)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func deserializeSkMemInfoReflection(data []byte, sm *SkMemInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, sm)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

func deserializeSockOptReflection(data []byte, c *SockOpt) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, c)
	if err != nil {
		return 0, err
	}
	n = len(data)

	return n, err
}

// The five TCPInfo twins below all returned MemInfoReadCst (16) — a
// copy-paste slip carried in from the production code they were moved out of.
// No caller ever read the count, which is why it survived. Each now returns
// the size constant for the kernel version it models.
//
// Note these counts are the *wire* sizes, while binary.Read consumes
// binary.Size(struct) bytes, which is larger: the kernel's bitfields are
// modeled as separate Go fields (see tcpInfoFixture). That mismatch is
// unfixable inside a binary.Read twin and is the reason the TCPInfo twins are
// timing controls and error-path tests rather than a layout oracle.

func deserializeTCPInfo4_19_219Reflection(data []byte, t *TCPInfo4_19_219) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return TCPInfo4_19_219_SizeCst, err
}

func deserializeTCPInfo5_4_281Reflection(data []byte, t *TCPInfo5_4_281) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return TCPInfo5_4_281_SizeCst, err
}

func deserializeTCPInfoReflection(data []byte, mi *TCPInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, mi)
	if err != nil {
		return 0, err
	}

	return TCPInfo7_0_3_SizeCst, err
}

func deserializeTCPInfo6_6_44Reflection(data []byte, t *TCPInfo6_6_44) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return TCPInfo6_6_44_SizeCst, err
}

func deserializeTCPInfo6_10_3Reflection(data []byte, t *TCPInfo6_10_3) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return TCPInfo6_10_3_SizeCst, err
}

func deserializeTCPInfo7_0_3Reflection(data []byte, t *TCPInfo7_0_3) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, t)
	if err != nil {
		return 0, err
	}

	return TCPInfo7_0_3_SizeCst, err
}

func deserializeTrafficClassReflection(data []byte, tc *TrafficClass) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, tc)
	if err != nil {
		return 0, err
	}

	return TrafficClassSizeCst, err
}

func deserializeTypeOfServiceReflection(data []byte, tos *TypeOfService) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, tos)
	if err != nil {
		return 0, err
	}

	return TypeOfServiceSizeCst, err
}

func deserializeVegasInfoReflection(data []byte, vi *VegasInfo) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, vi)
	if err != nil {
		return 0, err
	}

	return VegasInfoReadCst, err
}
