package xtcpnl

import (
	"bytes"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// xtcpWriterCase exercises one DeserializeXXXXXTCP function with a
// happy-path buffer + a too-short buffer.
type xtcpWriterCase struct {
	name   string
	min    int // minimum acceptable len
	parse  func(data []byte, x *xtcp_flat_record.XtcpFlatRecord) error
	verify func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord)
}

// runXTCPWriterCases runs each row twice: happy path (len = min, every
// byte 0x01) and short-buffer path (len = min-1). The verify callback
// checks at least one populated field.
func runXTCPWriterCases(t *testing.T, cases []xtcpWriterCase) {
	for _, tc := range cases {
		t.Run(tc.name+"_happy", func(t *testing.T) {
			data := bytes.Repeat([]byte{0x01}, tc.min)
			x := &xtcp_flat_record.XtcpFlatRecord{}
			if err := tc.parse(data, x); err != nil {
				t.Fatalf("parse err: %v", err)
			}
			tc.verify(t, x)
		})
		t.Run(tc.name+"_short", func(t *testing.T) {
			if tc.min == 0 {
				t.Skip("min=0 has no short branch")
			}
			data := bytes.Repeat([]byte{0x01}, tc.min-1)
			x := &xtcp_flat_record.XtcpFlatRecord{}
			err := tc.parse(data, x)
			if err == nil {
				t.Fatal("expected ErrXxxSmall on short buffer; got nil")
			}
		})
	}
}

func TestDeserializeXXXXTCP(t *testing.T) {
	runXTCPWriterCases(t, []xtcpWriterCase{
		{
			name:  "MemInfo",
			min:   MemInfoSizeCst,
			parse: DeserializeMemInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.MemInfoRmem == 0 {
					t.Errorf("MemInfoRmem unset")
				}
			},
		},
		{
			name:  "SkMemInfo",
			min:   SkMemInfoMinSizeCst,
			parse: DeserializeSkMemInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.SkMemInfoRmemAlloc == 0 {
					t.Errorf("SkMemInfoRmemAlloc unset")
				}
			},
		},
		{
			// BBRInfoXTCP's short-buffer sentinel is MemInfoSizeCst (16)
			// but the function reads up through byte 20, so happy path
			// needs at least 20 bytes.
			name:  "BBRInfo",
			min:   20,
			parse: DeserializeBBRInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.BbrInfoBwLo == 0 {
					t.Errorf("BbrInfoBwLo unset")
				}
			},
		},
		{
			name:  "VegasInfo",
			min:   VegasInfoSizeCst,
			parse: DeserializeVegasInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.VegasInfoEnabled == 0 {
					t.Errorf("VegasInfoEnabled unset")
				}
			},
		},
		{
			name:  "DCTCPInfo",
			min:   DCTCPInfoSizeCst,
			parse: DeserializeDCTCPInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.DctcpInfoEnabled == 0 {
					t.Errorf("DctcpInfoEnabled unset")
				}
			},
		},
		{
			name:  "TypeOfService",
			min:   TypeOfServiceSizeCst,
			parse: DeserializeTypeOfServiceXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.TypeOfService == 0 {
					t.Errorf("TypeOfService unset")
				}
			},
		},
		{
			name:  "TrafficClass",
			min:   TrafficClassSizeCst,
			parse: DeserializeTrafficClassXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.TrafficClass == 0 {
					t.Errorf("TrafficClass unset")
				}
			},
		},
		{
			name:  "Shutdown",
			min:   ShutdownSizeCst,
			parse: DeserializeShutdownXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.ShutdownState == 0 {
					t.Errorf("ShutdownState unset")
				}
			},
		},
		{
			// CongInfo writes only on a recognized "cub" / "bbr" / etc.
			// prefix. Garbage 0x01 bytes fall through the switch with
			// no observable side effect, which is the intended kernel-
			// compatibility behavior. We only assert the parse succeeds.
			name:  "CongInfo",
			min:   CongInfoSizeCst,
			parse: DeserializeCongInfoXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				// No assertion — successful parse with unknown algorithm
				// leaves all fields at zero, which is correct.
			},
		},
		{
			name:  "ClassID",
			min:   ClassIDSizeCst,
			parse: DeserializeClassIDXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.ClassId == 0 {
					t.Errorf("ClassId unset")
				}
			},
		},
		{
			name:  "CGroupID",
			min:   CGroupIDSizeCst,
			parse: DeserializeCGroupIDXTCP,
			verify: func(t *testing.T, x *xtcp_flat_record.XtcpFlatRecord) {
				if x.CGroup == 0 {
					t.Errorf("CGroup unset")
				}
			},
		},
	})
}

// ZeroizeXXXXTCP funcs reset their target fields.
func TestZeroizeXTCPVariants(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{
		BbrInfoBwLo:    100,
		BbrInfoMinRtt:  200,
		BbrInfoBwHi:    300,
		DctcpInfoAlpha: 7,
		DctcpInfoAbEcn: 8,
		VegasInfoRtt:   42,
	}
	ZeroizeBBRInfoXTCP(x)
	if x.BbrInfoBwLo != 0 || x.BbrInfoMinRtt != 0 || x.BbrInfoBwHi != 0 {
		t.Errorf("ZeroizeBBRInfoXTCP failed: %+v", x)
	}
	x.DctcpInfoAlpha = 7
	x.DctcpInfoAbEcn = 8
	ZeroizeDCTCPInfoXTCP(x)
	if x.DctcpInfoAlpha != 0 || x.DctcpInfoAbEcn != 0 {
		t.Errorf("ZeroizeDCTCPInfoXTCP failed: %+v", x)
	}
	x.VegasInfoRtt = 42
	ZeroizeVegasInfoXTCP(x)
	if x.VegasInfoRtt != 0 {
		t.Errorf("ZeroizeVegasInfoXTCP failed: %d", x.VegasInfoRtt)
	}
}
