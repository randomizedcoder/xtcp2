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
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"testing"
)

// TestReflectionShortBuffers exercises the binary.Read EOF path of every
// *Reflection variant by passing a 1-byte buffer. All Reflection helpers
// should return an err (typically io.ErrUnexpectedEOF) and we just check
// that err != nil.

func TestReflectionShortBuffers(t *testing.T) {
	short := []byte{0x01}

	checks := []struct {
		name string
		fn   func() error
	}{
		{"ClassID", func() error {
			c := new(ClassID)
			_, err := deserializeClassIDReflection(short, c)
			return err
		}},
		{"CGroupID", func() error {
			c := new(CGroupID)
			_, err := deserializeCGroupIDReflection(short, c)
			return err
		}},
		{"DCTCPInfo", func() error {
			d := new(DCTCPInfo)
			_, err := deserializeDCTCPInfoReflection(short, d)
			return err
		}},
		{"PragueInfo", func() error {
			p := new(PragueInfo)
			_, err := deserializePragueInfoReflection(short, p)
			return err
		}},
		{"VegasInfo", func() error {
			v := new(VegasInfo)
			_, err := deserializeVegasInfoReflection(short, v)
			return err
		}},
		{"SockOpt", func() error {
			s := new(SockOpt)
			_, err := deserializeSockOptReflection(short, s)
			return err
		}},
		{"BBRInfo", func() error {
			b := new(BBRInfo)
			_, err := deserializeBBRInfoReflection(short, b)
			return err
		}},
		{"Shutdown", func() error {
			// Shutdown is a single byte; use 0-byte buffer.
			s := new(Shutdown)
			_, err := deserializeShutdownReflection([]byte{}, s)
			return err
		}},
		{"TrafficClass", func() error {
			// TrafficClass is a single byte; 1-byte buffer is "full".
			// Use 0-byte to force the EOF branch.
			tc := new(TrafficClass)
			_, err := deserializeTrafficClassReflection([]byte{}, tc)
			return err
		}},
		{"TypeOfService", func() error {
			tos := new(TypeOfService)
			_, err := deserializeTypeOfServiceReflection([]byte{}, tos)
			return err
		}},
		{"SkMemInfo", func() error {
			sm := new(SkMemInfo)
			_, err := deserializeSkMemInfoReflection(short, sm)
			return err
		}},
		{"PcapHeader", func() error {
			p := new(PcapHeader)
			_, err := deserializePcapHeaderReflection(short, p)
			return err
		}},
		{"PcapRecordHeader", func() error {
			p := new(PcapRecordHeader)
			_, err := deserializePcapRecordHeaderReflection(short, p)
			return err
		}},
		{"InetDiagMsgViaReflection", func() error {
			idm := new(InetDiagMsg)
			s := new(InetDiagSockID)
			_, err := deserializeInetDiagMsgReflection(short, idm, s)
			return err
		}},
		{"TCPInfo6_10_3", func() error {
			ti := new(TCPInfo6_10_3)
			_, err := deserializeTCPInfo6_10_3Reflection(short, ti)
			return err
		}},
		{"TCPInfo6_6_44", func() error {
			ti := new(TCPInfo6_6_44)
			_, err := deserializeTCPInfo6_6_44Reflection(short, ti)
			return err
		}},
		{"TCPInfo5_4_281", func() error {
			ti := new(TCPInfo5_4_281)
			_, err := deserializeTCPInfo5_4_281Reflection(short, ti)
			return err
		}},
		{"TCPInfo4_19_219", func() error {
			ti := new(TCPInfo4_19_219)
			_, err := deserializeTCPInfo4_19_219Reflection(short, ti)
			return err
		}},
	}

	for _, c := range checks {
		if err := c.fn(); err == nil {
			t.Errorf("%s reflection on short buffer should error, got nil", c.name)
		}
	}
}
