package xtcpnl

import (
	"math"
	"reflect"
	"testing"
)

// TestTcQencodeRoundTrip is each tc struct's Append against its decoder:
// decode(Append(x)) is x and the length is the struct's size (gotc 17P).
//
// go test ./pkg/xtcpnl/ -run TestTcQencodeRoundTrip
func TestTcQencodeRoundTrip(t *testing.T) {
	rate := TcRateSpec{CellLog: 3, Linklayer: 2, Overhead: 10, CellAlign: -1, Mpu: 64, Rate: 125000}
	maxRate := TcRateSpec{CellLog: math.MaxUint8, Linklayer: math.MaxUint8, Overhead: math.MaxUint16,
		CellAlign: math.MinInt16, Mpu: math.MaxUint16, Rate: math.MaxUint32}
	type enc interface{ Append([]byte) []byte }
	tests := []struct {
		description string
		x           enc
		size        int
		decode      func([]byte) (any, error)
	}{
		{"positive: tc_ratespec", rate, TcRateSpecSizeCst,
			func(b []byte) (any, error) { r := tcRd{b: b}; return r.rateSpec(), tcNeed(b, TcRateSpecSizeCst, "") }},
		{"positive: tc_sizespec with a negative overhead", TcSizespec{CellLog: 5, SizeLog: 2, CellAlign: -1,
			Overhead: -4, Linklayer: 2, Mpu: 64, Mtu: 2048, Tsize: 128}, TcSizespecSizeCst,
			func(b []byte) (any, error) { return DecodeTcSizespec(b) }},
		{"positive: tc_estimator with a negative interval", TcEstimator{Interval: -2, EwmaLog: 3}, TcEstimatorSizeCst,
			func(b []byte) (any, error) { return DecodeTcEstimator(b) }},
		{"positive: tc_fifo_qopt", TcFifoQopt{Limit: 100}, TcFifoQoptSizeCst,
			func(b []byte) (any, error) { return DecodeTcFifoQopt(b) }},
		{"positive: tc_prio_qopt", TcPrioQopt{Bands: 3, Priomap: [16]uint8{1, 2, 2, 2, 1, 2, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1}},
			TcPrioQoptSizeCst, func(b []byte) (any, error) { return DecodeTcPrioQopt(b) }},
		{"positive: tc_htb_opt", TcHtbOpt{Rate: rate, Ceil: maxRate, Buffer: 1, Cbuffer: 2, Quantum: 1500, Level: 0, Prio: 7},
			TcHtbOptSizeCst, func(b []byte) (any, error) { return DecodeTcHtbOpt(b) }},
		{"positive: tc_htb_glob", TcHtbGlob{Version: 3, Rate2Quantum: 20, Defcls: 0x10, DirectPkts: 50},
			TcHtbGlobSizeCst, func(b []byte) (any, error) { return DecodeTcHtbGlob(b) }},
		{"positive: tc_tbf_qopt", TcTbfQopt{Rate: rate, Peakrate: rate, Limit: 10000, Buffer: 80000, Mtu: 2000},
			TcTbfQoptSizeCst, func(b []byte) (any, error) { return DecodeTcTbfQopt(b) }},
		{"positive: tc_sfq_qopt", TcSfqQopt{Quantum: 1514, PerturbPeriod: -1, Limit: 127, Divisor: 1024, Flows: 128},
			TcSfqQoptSizeCst, func(b []byte) (any, error) { return DecodeTcSfqQopt(b) }},
		{"positive: tc_sfq_qopt_v1 with stats", TcSfqQoptV1{V0: TcSfqQopt{Quantum: 1514, Limit: 127}, Depth: 127,
			Headdrop: 1, Limit: 100000, QthMin: 8000, QthMax: 60000, Wlog: 1, Plog: 2, ScellLog: 3, Flags: 3,
			MaxP: 858993459, Stats: TcSfqredStats{1, 2, 3, 4, 5, 6}}, TcSfqQoptV1SizeCst,
			func(b []byte) (any, error) { return DecodeTcSfqQoptV1(b) }},
		{"positive: tc_netem_qopt", TcNetemQopt{Latency: 1, Limit: 1000, Loss: 2, Gap: 3, Duplicate: 4, Jitter: 5},
			TcNetemQoptSizeCst, func(b []byte) (any, error) { return DecodeTcNetemQopt(b) }},
		{"positive: tc_netem_corr", TcNetemCorr{1, 2, 3}, TcNetemCorrSizeCst,
			func(b []byte) (any, error) { return DecodeTcNetemCorr(b) }},
		{"positive: tc_netem_reorder", TcNetemProbCorr{Probability: math.MaxUint32, Correlation: 7}, TcNetemProbCorrSizeCst,
			func(b []byte) (any, error) { return DecodeTcNetemProbCorr(b) }},
		{"positive: tc_netem_rate with negative overheads", TcNetemRate{Rate: 125000, PacketOverhead: -20, CellSize: 100,
			CellOverhead: -5}, TcNetemRateSizeCst, func(b []byte) (any, error) { return DecodeTcNetemRate(b) }},
		{"positive: tc_netem_slot", TcNetemSlot{MinDelay: 800000, MaxDelay: 1000000, MaxPackets: 10, MaxBytes: 3000,
			DistDelay: -1, DistJitter: math.MinInt64}, TcNetemSlotSizeCst,
			func(b []byte) (any, error) { return DecodeTcNetemSlot(b) }},
		{"positive: tc_netem_gimodel", TcNetemGimodel{1, 2, 3, 4, 5}, TcNetemGimodelSizeCst,
			func(b []byte) (any, error) { return DecodeTcNetemGimodel(b) }},
		{"positive: tc_netem_gemodel", TcNetemGemodel{1, 2, 3, 4}, TcNetemGemodelSizeCst,
			func(b []byte) (any, error) { return DecodeTcNetemGemodel(b) }},
		{"boundary: tc_ratespec at every maximum", maxRate, TcRateSpecSizeCst,
			func(b []byte) (any, error) { r := tcRd{b: b}; return r.rateSpec(), tcNeed(b, TcRateSpecSizeCst, "") }},
		{"boundary: the zero tc_htb_opt", TcHtbOpt{}, TcHtbOptSizeCst,
			func(b []byte) (any, error) { return DecodeTcHtbOpt(b) }},
		{"corner: tc_estimator at the signed char minimum", TcEstimator{Interval: math.MinInt8, EwmaLog: math.MaxUint8},
			TcEstimatorSizeCst, func(b []byte) (any, error) { return DecodeTcEstimator(b) }},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			prefix := []byte{0xaa}
			b := tc.x.Append(prefix)
			if len(b)-1 != tc.size {
				t.Fatalf("Append wrote %d bytes, want %d", len(b)-1, tc.size)
			}
			if b[0] != 0xaa {
				t.Fatal("Append changed what was already in the slice")
			}
			got, err := tc.decode(b[1:])
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.x) {
				t.Errorf("decode(Append(x)) = %+v, want %+v", got, tc.x)
			}
		})
	}
}

// TestDecodeTcEstimator is the decoder's length check.
func TestDecodeTcEstimator(t *testing.T) {
	tests := []struct {
		description string
		in          []byte
		expected    TcEstimator
		ok          bool
	}{
		{"positive: interval 0 ewma 3", []byte{0, 3}, TcEstimator{0, 3}, true},
		{"negative: one byte is short", []byte{0}, TcEstimator{}, false},
		{"boundary: interval -2 is 0xfe", []byte{0xfe, 2}, TcEstimator{-2, 2}, true},
		{"corner: trailing bytes are ignored", []byte{3, 3, 9}, TcEstimator{3, 3}, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := DecodeTcEstimator(tc.in)
			if (err == nil) != tc.ok || got != tc.expected {
				t.Errorf("got %+v, %v; want %+v ok=%t", got, err, tc.expected, tc.ok)
			}
		})
	}
}
