package xtcp

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// ───────────────────────────────────────────────────────────────────────
// destinations_core dispatch — RegisterDestination / IsKnownScheme /
// CompiledInSchemes / lookupDestinationFactory / destinationLookupError
// ───────────────────────────────────────────────────────────────────────

// fakeDest satisfies the Destination interface without doing any I/O.
type fakeDest struct{}

func (fakeDest) Send(_ context.Context, _ *[]byte) (int, error) { return 0, nil }
func (fakeDest) Close() error                                   { return nil }

func TestRegisterDestination_lookupFound(t *testing.T) {
	// Pick a scheme name unlikely to collide with anything real.
	scheme := "test_register_dest_scheme"
	RegisterDestination(scheme, func(_ context.Context, _ *XTCP) (Destination, error) {
		return fakeDest{}, nil
	})
	f, status := lookupDestinationFactory(scheme)
	if status != destLookupFound {
		t.Fatalf("status = %d, want destLookupFound", status)
	}
	d, err := f(context.Background(), nil)
	if err != nil {
		t.Fatalf("factory err: %v", err)
	}
	if _, ok := d.(fakeDest); !ok {
		t.Errorf("factory returned wrong type: %T", d)
	}
}

func TestIsKnownScheme(t *testing.T) {
	// At least these should be in knownSchemes.
	for _, s := range []string{schemeNull, schemeUDP, schemeUnix, schemeUnixgram} {
		if !IsKnownScheme(s) {
			t.Errorf("%q should be known", s)
		}
	}
	if IsKnownScheme("definitely-not-a-real-scheme") {
		t.Error("garbage scheme should not be known")
	}
}

func TestCompiledInSchemes(t *testing.T) {
	got := CompiledInSchemes()
	if len(got) == 0 {
		t.Fatal("CompiledInSchemes returned empty list (destinations_*.go init() should populate it)")
	}
	// Result must be sorted.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("CompiledInSchemes not sorted: %v", got)
			break
		}
	}
	// Must contain at least null (always compiled).
	found := false
	for _, s := range got {
		if s == schemeNull {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CompiledInSchemes missing %q: %v", schemeNull, got)
	}
}

func TestLookupDestinationFactory_unknown(t *testing.T) {
	_, status := lookupDestinationFactory("garbage_not_a_scheme")
	if status != destLookupUnknown {
		t.Errorf("status = %d, want destLookupUnknown", status)
	}
}

func TestDestinationLookupError(t *testing.T) {
	if err := destinationLookupError("foo", destLookupUnknown); err == nil ||
		!strings.Contains(err.Error(), "unknown destination") {
		t.Errorf("unknown error wrong: %v", err)
	}
	// destLookupNotCompiledIn requires the scheme to be in knownSchemes;
	// pick one we know is in the list but build a fake scenario by
	// asking for a known scheme with that status.
	if err := destinationLookupError("kafka", destLookupNotCompiledIn); err == nil ||
		!strings.Contains(err.Error(), "not compiled into this binary") {
		t.Errorf("not-compiled-in error wrong: %v", err)
	}
	// destLookupFound returns nil per the comment.
	if err := destinationLookupError("null", destLookupFound); err != nil {
		t.Errorf("found should return nil; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// deserializers dispatch — GetAllDeserializers + InitDeserializers
// ───────────────────────────────────────────────────────────────────────

func TestGetAllDeserializers(t *testing.T) {
	got := GetAllDeserializers()
	// Should include at least the canonical ones referenced in the dispatch.
	want := []string{"meminfo", "info", "vegas", "cong", "tos", "tc",
		"skmem", "shut", "dctcp", "bbr", "classid", "cgroup", "sockopt"}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GetAllDeserializers missing %q; got %v", w, got)
		}
	}
}

func TestInitDeserializers_dispatch(t *testing.T) {
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{
			EnabledDeserializers: &xtcp_config.EnabledDeserializers{
				Enabled: map[string]bool{
					"info":   true,
					"bbr":    true,
					"vegas":  true,
					"skmem":  true,
					"shut":   true,
					"dctcp":  true,
					"cgroup": true,
				},
			},
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDeserializers(&wg)
	wg.Wait()
	if x.RTATypeDeserializer == nil {
		t.Fatal("RTATypeDeserializer map nil after Init")
	}
	if len(x.RTATypeDeserializer) < 5 {
		t.Errorf("expected ≥5 dispatch entries, got %d", len(x.RTATypeDeserializer))
	}
	if x.RTATypeDeserializerStr == nil {
		t.Error("RTATypeDeserializerStr map nil")
	}
}

func TestInitDeserializers_emptyEnabled(t *testing.T) {
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{
			EnabledDeserializers: &xtcp_config.EnabledDeserializers{
				Enabled: map[string]bool{},
			},
		},
	}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDeserializers(&wg)
	wg.Wait()
	if x.RTATypeDeserializer == nil {
		t.Fatal("map should be initialized even when no deserializers enabled")
	}
	if len(x.RTATypeDeserializer) != 0 {
		t.Errorf("expected 0 dispatch entries, got %d", len(x.RTATypeDeserializer))
	}
}

// ───────────────────────────────────────────────────────────────────────
// zeroizers dispatch — InitZeroizers + ZeroXTCPCongRecord
// ───────────────────────────────────────────────────────────────────────

func TestInitZeroizers(t *testing.T) {
	x := &XTCP{}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitZeroizers(&wg)
	wg.Wait()
	if x.xtcpRecordZeroizer == nil {
		t.Fatal("xtcpRecordZeroizer map nil")
	}
	// At least BBR1, DCTCP, VEGAS should be registered.
	for _, alg := range []xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm{
		xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1,
		xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP,
		xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS,
	} {
		if _, ok := x.xtcpRecordZeroizer[alg]; !ok {
			t.Errorf("zeroizer for %v not registered", alg)
		}
	}
}

func TestZeroXTCPCongRecord_dispatch(t *testing.T) {
	x := &XTCP{}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitZeroizers(&wg)
	wg.Wait()
	// A record with BBR1 cong algo + non-zero BBR fields should have
	// those fields zeroed.
	rec := &xtcp_flat_record.XtcpFlatRecord{
		CongestionAlgorithmEnum: xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1,
		BbrInfoBwLo:             123456,
	}
	x.ZeroXTCPCongRecord(rec)
	if rec.BbrInfoBwLo != 0 {
		t.Errorf("BbrInfoBwLo should be zeroed, got %d", rec.BbrInfoBwLo)
	}
}

func TestZeroXTCPCongRecord_unknownCong(t *testing.T) {
	x := &XTCP{}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitZeroizers(&wg)
	wg.Wait()
	// An unknown cong algorithm should be a no-op (no panic, no mutation).
	rec := &xtcp_flat_record.XtcpFlatRecord{
		CongestionAlgorithmEnum: xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED,
		BbrInfoBwLo:             123456,
	}
	x.ZeroXTCPCongRecord(rec)
	if rec.BbrInfoBwLo != 123456 {
		t.Errorf("unknown cong should be a no-op; BbrInfoBwLo changed to %d", rec.BbrInfoBwLo)
	}
}
