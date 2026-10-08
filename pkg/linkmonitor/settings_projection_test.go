package linkmonitor

import (
	"math"
	"reflect"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func settingsModes(speed uint32, duplex uint8, supported, advertised uint32) *xtcpnl.EthtoolLinkModes {
	size := uint32(32)
	return &xtcpnl.EthtoolLinkModes{Speed: &speed, Duplex: &duplex, Ours: &xtcpnl.EthtoolBitset{Compact: true, Size: &size, Mask: []uint32{supported}, Value: []uint32{advertised}}}
}

func sampleValue(samples []model.Sample, suffix string) (uint64, bool) {
	for _, sample := range samples {
		if sample.Descriptor == interfaceMetricPrefix+suffix {
			return sample.Number.Uint64()
		}
	}
	return 0, false
}

func TestSettingsModernModes(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		index                                        uint32
		noMask                                       bool
		maximum                                      uint64
		known                                        bool
	}{
		{"high speed", "positive", "800GE bit above legacy bitmap", "correct modern maximum", 115, false, 800000000000, true},
		{"highest reviewed", "boundary", "1.6TE highest reviewed mode", "exact widened maximum", 124, false, 1600000000000, true},
		{"future", "boundary", "unknown future supported bit", "maximum unknown", 125, false, 0, false},
		{"nomask", "corner", "advertisement-only bitmap", "advertisement visible without supported maximum", 12, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			size := tc.index + 1
			words := (size + 31) / 32
			bits := &xtcpnl.EthtoolBitset{Compact: true, NoMask: tc.noMask, Size: &size, Value: make([]uint32, words)}
			bits.Value[tc.index/32] = 1 << (tc.index % 32)
			if !tc.noMask {
				bits.Mask = append([]uint32(nil), bits.Value...)
			}
			samples, _ := settingsSamples(presentValue(true), &xtcpnl.EthtoolLinkModes{Ours: bits})
			maximum, known := sampleValue(samples, "max_speed_bits_per_second")
			if maximum != tc.maximum || known != tc.known {
				t.Fatalf("%s: maximum %d/%v", tc.expectedOutcome, maximum, known)
			}
			if tc.noMask {
				if _, ok := sampleValue(samples, "advertised_speed_bits_per_second"); !ok {
					t.Fatal(tc.expectedOutcome)
				}
			}
		})
	}
}

func TestSettingsModeDuplexLabels(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		index                                        uint32
		duplex                                       string
	}{
		{"full", "positive", "10GE full-duplex mode", "full label", 12, "full"},
		{"legacy half", "positive", "10baseT half-duplex mode", "half label", 0, "half"},
		{"FX", "boundary", "half-duplex FX beyond legacy bitmap", "half label", 90, "half"},
		{"T1S", "corner", "short-reach half-duplex mode", "half label", 100, "half"},
		{"P2MP", "boundary", "highest reviewed half-duplex bit", "half label", 101, "half"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := &xtcpnl.EthtoolLinkModes{Ours: &xtcpnl.EthtoolBitset{Bits: []xtcpnl.EthtoolBit{{Index: &tc.index, Value: true}}}}
			samples := modeSamples(m)
			if len(samples) < 2 {
				t.Fatal(tc.expectedOutcome)
			}
			for _, sample := range samples[:2] {
				if sample.Labels[0].Value != tc.duplex {
					t.Fatalf("%s: labels %+v", tc.expectedOutcome, sample.Labels)
				}
			}
		})
	}
}

func TestSettingsProjection(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		speed                                        uint32
		duplex                                       uint8
		up                                           bool
		mask, advertised                             uint32
		wantSpeed, wantDuplex                        model.Check
		active                                       bool
	}{
		{"maximum", "positive", "10GE full at supported maximum", "both checks pass", 10000, 1, true, 1 << 12, 1 << 12, model.CheckPass, model.CheckPass, true},
		{"below", "negative", "1GE negotiated on 10GE hardware", "speed fails, duplex passes", 1000, 1, true, 1 << 12, 1 << 12, model.CheckFail, model.CheckPass, true},
		{"half", "negative", "half duplex at maximum", "duplex fails independently", 10000, 0, true, 1 << 12, 1 << 12, model.CheckPass, model.CheckFail, true},
		{"zero", "boundary", "speed zero during negotiation", "speed unknown and absent", 0, 1, true, 1 << 12, 0, model.CheckUnknown, model.CheckPass, false},
		{"sentinel", "boundary", "unknown speed sentinel", "speed unknown and absent", math.MaxUint32, 255, true, 1 << 12, 0, model.CheckUnknown, model.CheckUnknown, false},
		{"advertised", "corner", "only lower mode advertised", "maximum still uses supported mask", 1000, 1, true, 1<<12 | 1<<5, 1 << 5, model.CheckFail, model.CheckPass, true},
		{"down", "corner", "down interface returns old negotiated data", "checks not applicable and active speed omitted", 10000, 1, false, 1 << 12, 1 << 12, model.CheckNotApplicable, model.CheckNotApplicable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			samples, checks := settingsSamples(presentValue(tc.up), settingsModes(tc.speed, tc.duplex, tc.mask, tc.advertised))
			if checks.Speed != tc.wantSpeed || checks.Duplex != tc.wantDuplex {
				t.Fatalf("%s: checks %+v", tc.expectedOutcome, checks)
			}
			value, present := sampleValue(samples, "speed_bits_per_second")
			if present != tc.active || (present && value != uint64(tc.speed)*1000000) {
				t.Fatalf("%s: active %d/%v", tc.expectedOutcome, value, present)
			}
			if maximum, ok := sampleValue(samples, "max_speed_bits_per_second"); !ok || maximum != 10000000000 {
				t.Fatalf("%s: maximum %d/%v", tc.expectedOutcome, maximum, ok)
			}
		})
	}
}

func TestSettingsConfigurationProjection(t *testing.T) {
	zero, maximum, split := uint32(0), uint32(math.MaxUint32), uint8(2)
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		message                                      xtcpnl.EthtoolMessage
		count                                        int
		suffix                                       string
		value                                        uint64
	}{
		{"absent", "negative", "all channel attributes absent", "omit all gauges", xtcpnl.EthtoolMessage{Channels: &xtcpnl.EthtoolChannels{}}, 0, "channels", 0},
		{"zero", "boundary", "present zero RX channel count", "emit one zero gauge", xtcpnl.EthtoolMessage{Channels: &xtcpnl.EthtoolChannels{RX: &zero}}, 1, "channels", 0},
		{"maximum", "boundary", "maximum uint32 ring count", "preserve exact unsigned value", xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{RX: &maximum}}, 1, "ring_entries", math.MaxUint32},
		{"extended", "positive", "netlink ring buffer length", "emit extension in bytes", xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{RXBufLen: &maximum}}, 1, "ring_rx_buffer_bytes", math.MaxUint32},
		{"all rings", "positive", "all legacy and extended ring fields", "emit all seventeen fields", xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{
			RX: &zero, TX: &zero, RXMini: &zero, RXJumbo: &zero, RXMax: &maximum, TXMax: &maximum, RXMiniMax: &maximum, RXJumboMax: &maximum,
			RXBufLen: &maximum, CQESize: &maximum, TXPushBufLen: &maximum, TXPushBufLenMax: &maximum, HDSThresh: &maximum, HDSThreshMax: &maximum,
			TCPDataSplit: &split, TXPush: pointer(uint8(1)), RXPush: pointer(uint8(0)),
		}}, 17, "ring_header_split_threshold_max_bytes", math.MaxUint32},
		{"enum", "corner", "TCP data split enum is 2", "preserve enum rather than boolean", xtcpnl.EthtoolMessage{Rings: &xtcpnl.EthtoolRings{TCPDataSplit: &split}}, 1, "ring_tcp_data_split", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			samples := configurationSamples(&tc.message)
			if len(samples) != tc.count {
				t.Fatalf("%s: sample count %d", tc.expectedOutcome, len(samples))
			}
			if tc.count != 0 {
				if value, ok := sampleValue(samples, tc.suffix); !ok || value != tc.value {
					t.Fatalf("%s: value %d/%v", tc.expectedOutcome, value, ok)
				}
			}
			first, err := freezeSamples(nil, samples)
			if err != nil {
				t.Fatal(err)
			}
			second, err := freezeSamples(first, samples)
			if err != nil || first.schema != second.schema || !reflect.DeepEqual(first.values, second.values) {
				t.Fatalf("schema reuse: %v", err)
			}
		})
	}
}
