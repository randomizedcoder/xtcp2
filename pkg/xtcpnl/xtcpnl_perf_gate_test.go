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
// indicates a problem rather than a license to use it. This file is that
// check: see xtcpnl_reflection_twins_test.go for the full rationale.

import (
	"encoding/binary"
	"testing"
	"time"
)

// Performance gate for the hand-rolled netlink decoders.
//
// pkg/xtcpnl ships no reflection: every Deserialize* in the library reads
// fields at fixed byte offsets. The binary.Read twins in
// xtcpnl_reflection_twins_test.go are kept deliberately as the control
// group, and this test asserts the two properties that justify writing the
// decoders out by hand:
//
//  1. The manual decoder allocates nothing. It runs once per attribute per
//     socket per poll, so a single allocation here is multiplied by the
//     socket count of the whole host.
//
//  2. The reflection twin stays far slower. If reflection ever comes close,
//     something is wrong — either the manual decoder has regressed into
//     doing real per-field work, or the fixture stopped exercising it and
//     both are returning early. Measured ratios range from 17x on the
//     widest struct under host load to over 300x on the narrowest, so the
//     gate trips on convergence long before the two actually meet.
//
// The ratio, rather than an absolute ns/op bound, is what makes property 2
// usable in CI: both halves run back to back on the same machine in the
// same pass, so a loaded or slow host scales them together and the
// comparison survives. An absolute bound would flake under host load, which
// this repo already sees elsewhere (the microVM lifecycle deadline).
//
// The one thing a ratio does NOT survive is the race detector, which scales
// the two halves differently rather than together — so property 2 is skipped
// under -race and property 1 is not. See xtcpnl_perf_gate_race_test.go.
//
// go test ./pkg/xtcpnl/... -run TestDecoderPerformanceGate -v

const (
	// Iterations per timed run. Large enough that a ~3 ns decode is well
	// clear of the monotonic clock's resolution, small enough that the whole
	// table stays inside a normal unit-test budget.
	perfGateIterationsCst = 200_000

	// Runs handed to testing.AllocsPerRun. It averages over the runs, so a
	// decoder that allocates on even a subset of inputs reports >= 1.
	perfGateAllocRunsCst = 1_000

	// Minimum acceptable manual-vs-reflection speedup. Measured ratios are
	// 20x-60x; 5x is low enough that ordinary run-to-run noise never trips
	// it, and high enough that genuine convergence does.
	perfGateMinSpeedupCst = 5.0
)

// perfGateSink defeats dead-code elimination. The decoders write through a
// pointer so the calls are already observable, but accumulating the return
// value removes any doubt that the timed loop does the work.
var perfGateSink int

// decoderPerfTest is one manual/reflection pair measured over the same
// captured bytes.
//
// manual and reflection close over a destination struct allocated once
// outside the table — allocating inside the closure would measure the
// allocator rather than the decoder, and would defeat the whole point of
// property 1.
//
// prepare slices the fixture down to the bytes the decoder expects. The
// raw attribute fixtures are already exactly the payload, so most rows
// leave it nil; the .pcap fixtures need the pcap, record and cooked
// headers skipped, which is what PcapNetlinkOffsetCst and friends are for.
//
// prepareReflection is for the rows where the twin cannot be handed the same
// bytes as the manual decoder. Splitting a kernel bitfield into separate Go
// fields makes the struct wider in Go than on the wire, so binary.Read wants
// more bytes than the kernel sends — see tcpInfoFixture. Zero-padding the
// input is the only way to run the control group at all, and it does not
// flatter it: the twin still walks the same number of fields.
type decoderPerfTest struct {
	description       string
	filename          string
	prepare           func(bs []byte) []byte // nil means decode the whole fixture
	prepareReflection func(bs []byte) []byte // nil means use prepare's result
	manual            func(data []byte) (n int, err error)
	reflection        func(data []byte) (n int, err error)
	wantAllocs        float64 // expected allocations per manual decode
	minSpeedup        float64 // expected manual-vs-reflection ratio, at minimum
}

// padTo returns data zero-padded to size, or data itself if already long
// enough. Used only by prepareReflection.
func padTo(data []byte, size int) []byte {
	if len(data) >= size {
		return data
	}
	padded := make([]byte, size)
	copy(padded, data)
	return padded
}

// timeDecoder returns how long `iterations` calls of fn over data take.
func timeDecoder(fn func(data []byte) (n int, err error), data []byte, iterations int) time.Duration {
	start := time.Now()
	for i := 0; i < iterations; i++ {
		n, _ := fn(data)
		perfGateSink += n
	}
	return time.Since(start)
}

// TestDecoderPerformanceGate asserts the manual decoders allocate nothing
// and stay far ahead of their reflection twins.
//
// go test ./pkg/xtcpnl/... -run TestDecoderPerformanceGate
func TestDecoderPerformanceGate(t *testing.T) {
	// Destinations allocated once and reused across every decode, mirroring
	// how pkg/xtcp reuses pooled structs on the real hot path.
	var (
		nlh    NlMsgHdr
		rta    RTAttr
		idm    InetDiagMsg
		sockid InetDiagSockID
		reqv2  InetDiagReqV2
		info   TCPInfo
		bbr    BBRInfo
		vegas  VegasInfo
		dctcp  DCTCPInfo
		mem    MemInfo
		skmem  SkMemInfo
		shut   Shutdown
		sopt   SockOpt
		cid    ClassID
		cgid   CGroupID
		tclass TrafficClass
		tos    TypeOfService
	)

	var tests = []decoderPerfTest{
		{
			description: "positive: NlMsgHdr, the 16-byte header decoded once per message on the poll path",
			filename:    tdLargeSockDiagExport_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeNlMsgHdr(b, &nlh) },
			reflection:  func(b []byte) (int, error) { return deserializeNlMsgHdrReflection(b, &nlh) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			// DeserializeRTAttr reads only nla_len and nla_type out of the
			// leading four bytes, so its timing does not depend on which
			// fixture supplies them. This row uses the same bytes as
			// BenchmarkDeserializeRTAttr.
			description: "positive: RTAttr, the 4-byte attribute header decoded once per attribute",
			filename:    tdAttrInfo_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeRTAttr(b, &rta) },
			reflection:  func(b []byte) (int, error) { return deserializeRTAttrReflection(b, &rta) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: InetDiagMsg, the per-socket record header and its sock ID",
			filename:    tdReplyPort4018_6_6_44,
			prepare: func(bs []byte) []byte {
				return bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagMsgSizeCst]
			},
			manual: func(b []byte) (int, error) { return DeserializeInetDiagMsg(b, &idm, &sockid) },
			reflection: func(b []byte) (int, error) {
				return deserializeInetDiagMsgReflection(b, &idm, &sockid)
			},
			wantAllocs: 0,
			minSpeedup: perfGateMinSpeedupCst,
		},
		{
			description: "positive: InetDiagSockID, the socket 4-tuple plus cookie",
			filename:    tdReplyPort443V6b_6_6_44,
			prepare: func(bs []byte) []byte {
				return bs[PcapInetDiagSockIDOffsetCst : PcapInetDiagSockIDOffsetCst+InetDiagSockIDSizeCst]
			},
			manual:     func(b []byte) (int, error) { return DeserializeInetDiagSockID(b, &sockid) },
			reflection: func(b []byte) (int, error) { return deserializeInetDiagSockIDReflection(b, &sockid) },
			wantAllocs: 0,
			minSpeedup: perfGateMinSpeedupCst,
		},
		{
			description: "positive: InetDiagReqV2, the dump request we build, decoded back",
			filename:    tdReqSinglePktV6_6_6_44,
			prepare: func(bs []byte) []byte {
				return bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
			},
			manual: func(b []byte) (int, error) { return DeserializeInetDiagReqV2(b, &reqv2, &sockid) },
			reflection: func(b []byte) (int, error) {
				return deserializeInetDiagReqV2Reflection(b, &reqv2, &sockid)
			},
			wantAllocs: 0,
			minSpeedup: perfGateMinSpeedupCst,
		},
		{
			description: "positive: TCPInfo on a 6.10 capture, no AccECN trailer, 248-byte tail path",
			filename:    tdAttrInfo_6_10_3,
			// Strip the 4-byte nla header so the length lands exactly on
			// TCPInfo6_10_3_SizeCst and the decoder takes the real
			// older-kernel branch rather than falling through.
			prepare:           func(bs []byte) []byte { return bs[RTAttrSizeCst:] },
			prepareReflection: func(bs []byte) []byte { return padTo(bs[RTAttrSizeCst:], binary.Size(TCPInfo{})) },
			manual:            func(b []byte) (int, error) { return DeserializeTCPInfo(b, &info) },
			reflection:        func(b []byte) (int, error) { return deserializeTCPInfoReflection(b, &info) },
			wantAllocs:        0,
			minSpeedup:        perfGateMinSpeedupCst,
		},
		{
			description: "positive: TCPInfo on a 7.0.3 capture, full 280-byte AccECN trailer decoded",
			filename:    tdAttrInfo26546_7_0_3,
			// The widest struct in the package, on the longest tcp_info the
			// corpus has. This is the row that would go quiet if the AccECN
			// tail were ever dropped back out of the decoder.
			prepare:           func(bs []byte) []byte { return bs[RTAttrSizeCst:] },
			prepareReflection: func(bs []byte) []byte { return padTo(bs[RTAttrSizeCst:], binary.Size(TCPInfo{})) },
			manual:            func(b []byte) (int, error) { return DeserializeTCPInfo(b, &info) },
			reflection:        func(b []byte) (int, error) { return deserializeTCPInfoReflection(b, &info) },
			wantAllocs:        0,
			minSpeedup:        perfGateMinSpeedupCst,
		},
		{
			description: "positive: BBRInfo, congestion-control private state",
			filename:    tdAttrBbrinfo_6_10_3,
			manual:      func(b []byte) (int, error) { return DeserializeBBRInfo(b, &bbr) },
			reflection:  func(b []byte) (int, error) { return deserializeBBRInfoReflection(b, &bbr) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: VegasInfo, congestion-control private state",
			filename:    tdAttrVegasinfo_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeVegasInfo(b, &vegas) },
			reflection:  func(b []byte) (int, error) { return deserializeVegasInfoReflection(b, &vegas) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: DCTCPInfo, congestion-control private state",
			filename:    tdAttrDctcpinfo_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeDCTCPInfo(b, &dctcp) },
			reflection:  func(b []byte) (int, error) { return deserializeDCTCPInfoReflection(b, &dctcp) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: MemInfo, four-counter socket memory accounting",
			filename:    tdAttrMeminfo_4_19_319,
			manual:      func(b []byte) (int, error) { return DeserializeMemInfo(b, &mem) },
			reflection:  func(b []byte) (int, error) { return deserializeMemInfoReflection(b, &mem) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: SkMemInfo, the wider sk_meminfo counter array",
			filename:    tdAttrSkmeminfo2_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeSkMemInfo(b, &skmem) },
			reflection:  func(b []byte) (int, error) { return deserializeSkMemInfoReflection(b, &skmem) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "boundary: Shutdown, a single byte and the smallest decoder in the package",
			filename:    tdAttrShutdown_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeShutdown(b, &shut) },
			reflection:  func(b []byte) (int, error) { return deserializeShutdownReflection(b, &shut) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: SockOpt, packed socket-option bitfield",
			filename:    tdAttrSockopt_6_10_3,
			manual:      func(b []byte) (int, error) { return DeserializeSockOpt(b, &sopt) },
			reflection:  func(b []byte) (int, error) { return deserializeSockOptReflection(b, &sopt) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "boundary: ClassID, a bare __u32",
			filename:    tdAttrClassID_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeClassID(b, &cid) },
			reflection:  func(b []byte) (int, error) { return deserializeClassIDReflection(b, &cid) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "boundary: CGroupID, a bare __u64",
			filename:    tdAttrCgroupID_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeCGroupID(b, &cgid) },
			reflection:  func(b []byte) (int, error) { return deserializeCGroupIDReflection(b, &cgid) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: TrafficClass, the tclass byte",
			filename:    tdAttrTcclass_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeTrafficClass(b, &tclass) },
			reflection:  func(b []byte) (int, error) { return deserializeTrafficClassReflection(b, &tclass) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
		{
			description: "positive: TypeOfService, the tos byte",
			filename:    tdAttrTos2_6_6_44,
			manual:      func(b []byte) (int, error) { return DeserializeTypeOfService(b, &tos) },
			reflection:  func(b []byte) (int, error) { return deserializeTypeOfServiceReflection(b, &tos) },
			wantAllocs:  0,
			minSpeedup:  perfGateMinSpeedupCst,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			bs, err := Readfile(test.filename)
			if err != nil {
				t.Fatalf("%s: reading fixture %s: %v", test.description, test.filename, err)
			}

			data := bs
			if test.prepare != nil {
				data = test.prepare(bs)
			}

			reflectData := data
			if test.prepareReflection != nil {
				reflectData = test.prepareReflection(bs)
			}

			// Decode once up front. A fixture sliced too short would make
			// both decoders return their Err*Small immediately, which times
			// as absurdly fast and would pass the ratio check for entirely
			// the wrong reason.
			n, derr := test.manual(data)
			if derr != nil {
				t.Fatalf("%s: manual decode of %s failed: %v", test.description, test.filename, derr)
			}
			if n <= 0 {
				t.Fatalf("%s: manual decode consumed %d bytes, want > 0", test.description, n)
			}
			if _, rerr := test.reflection(reflectData); rerr != nil {
				t.Fatalf("%s: reflection decode of %s failed: %v", test.description, test.filename, rerr)
			}

			gotAllocs := testing.AllocsPerRun(perfGateAllocRunsCst, func() {
				n, _ := test.manual(data)
				perfGateSink += n
			})
			if gotAllocs > test.wantAllocs {
				t.Errorf("%s: manual decode allocated %.2f times per call, want <= %.0f; "+
					"an allocation here is multiplied by the host's socket count",
					test.description, gotAllocs, test.wantAllocs)
			}

			manualTime := timeDecoder(test.manual, data, perfGateIterationsCst)
			reflectTime := timeDecoder(test.reflection, reflectData, perfGateIterationsCst)

			speedup := float64(reflectTime) / float64(manualTime)
			switch {
			case perfGateRaceEnabled:
				// Measured and reported, but not asserted. -race instruments
				// every memory access, which taxes the manual decoder's ~70
				// individual field writes far more heavily, proportionally,
				// than it taxes binary.Read's already-slow reflect work. The
				// ratio therefore collapses on a decoder that has not
				// changed: 75x to 4.5x on the 280-byte TCPInfo, purely from
				// turning the detector on. See xtcpnl_perf_gate_race_test.go.
				t.Logf("race detector on: speedup %.1fx measured but NOT asserted "+
					"(the %.1fx floor applies to ordinary builds only)",
					speedup, test.minSpeedup)
			case speedup < test.minSpeedup:
				t.Errorf("%s: manual decode only %.1fx faster than its reflection twin "+
					"(manual %v, reflection %v over %d iterations), want >= %.1fx; "+
					"reflection coming close means the manual decoder has regressed",
					test.description, speedup, manualTime, reflectTime,
					perfGateIterationsCst, test.minSpeedup)
			}
			t.Logf("%.1fx faster than reflection (manual %v, reflection %v), %.0f allocs/op",
				speedup, manualTime, reflectTime, gotAllocs)
		})
	}
}
