package xtcpnl

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
			_, err := DeserializeClassIDReflection(short, c)
			return err
		}},
		{"CGroupID", func() error {
			c := new(CGroupID)
			_, err := DeserializeCGroupIDReflection(short, c)
			return err
		}},
		{"DCTCPInfo", func() error {
			d := new(DCTCPInfo)
			_, err := DeserializeDCTCPInfoReflection(short, d)
			return err
		}},
		{"PragueInfo", func() error {
			p := new(PragueInfo)
			_, err := DeserializePragueInfoReflection(short, p)
			return err
		}},
		{"VegasInfo", func() error {
			v := new(VegasInfo)
			_, err := DeserializeVegasInfoReflection(short, v)
			return err
		}},
		{"SockOpt", func() error {
			s := new(SockOpt)
			_, err := DeserializeSockOptReflection(short, s)
			return err
		}},
		{"BBRInfo", func() error {
			b := new(BBRInfo)
			_, err := DeserializeBBRInfoReflection(short, b)
			return err
		}},
		{"Shutdown", func() error {
			// Shutdown is a single byte; use 0-byte buffer.
			s := new(Shutdown)
			_, err := DeserializeShutdownReflection([]byte{}, s)
			return err
		}},
		{"TrafficClass", func() error {
			// TrafficClass is a single byte; 1-byte buffer is "full".
			// Use 0-byte to force the EOF branch.
			tc := new(TrafficClass)
			_, err := DeserializeTrafficClassReflection([]byte{}, tc)
			return err
		}},
		{"TypeOfService", func() error {
			tos := new(TypeOfService)
			_, err := DeserializeTypeOfServiceReflection([]byte{}, tos)
			return err
		}},
		{"SkMemInfo", func() error {
			sm := new(SkMemInfo)
			_, err := DeserializeSkMemInfoReflection(short, sm)
			return err
		}},
		{"PcapHeader", func() error {
			p := new(PcapHeader)
			_, err := DeserializePcapHeaderReflection(short, p)
			return err
		}},
		{"PcapRecordHeader", func() error {
			p := new(PcapRecordHeader)
			_, err := DeserializePcapRecordHeaderReflection(short, p)
			return err
		}},
		{"InetDiagMsgViaReflection", func() error {
			idm := new(InetDiagMsg)
			s := new(InetDiagSockID)
			_, err := DeserializeInetDiagMsgViaReflection(short, idm, s)
			return err
		}},
		{"TCPInfo6_10_3", func() error {
			ti := new(TCPInfo6_10_3)
			_, err := DeserializeTCPInfoTCPInfoTCPInfo6_10_3Reflection(short, ti)
			return err
		}},
		{"TCPInfo6_6_44", func() error {
			ti := new(TCPInfo6_6_44)
			_, err := DeserializeTCPInfoTCPInfo6_6_44Reflection(short, ti)
			return err
		}},
		{"TCPInfo5_4_281", func() error {
			ti := new(TCPInfo5_4_281)
			_, err := DeserializeTCPInfo5_4_281Reflection(short, ti)
			return err
		}},
		{"TCPInfo4_19_219", func() error {
			ti := new(TCPInfo4_19_219)
			_, err := DeserializeTCPInfo4_19_219Reflection(short, ti)
			return err
		}},
	}

	for _, c := range checks {
		if err := c.fn(); err == nil {
			t.Errorf("%s reflection on short buffer should error, got nil", c.name)
		}
	}
}
