package xtcp

import (
	"fmt"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// dispatchTable + InitDeserializers refactor — table-driven coverage
// (positive / negative / boundary / corner / adversarial) plus
// benchmarks and a race-detector-friendly concurrency test.
//
// gocyclo took InitDeserializers from 17 → 5 by replacing 13 repeated
// `if _, exists := Enabled[key]; exists { ... }` blocks with a single
// walk over dispatchTable. These tests pin the resulting behavior
// from every direction so a future refactor cannot silently regress.

// allKnownDispatchKeys is the canonical key list the production
// dispatchTable advertises. Anchored as a frozen literal so the test
// fails loudly if a new entry is added to dispatchTable without a
// matching test update.
var allKnownDispatchKeys = []string{
	dsKeyMemInfo, dsKeyInfo, dsKeyVegas, dsKeyCong, dsKeyTos, dsKeyTc,
	dsKeySkmem, dsKeyShut, dsKeyDctcp, dsKeyBbr, dsKeyClassID, dsKeyCgroup,
	dsKeySockopt,
}

// expectedEnumFor returns the INET_DIAG enum the dispatchTable maps a
// given key to. Mirrors the table in deserializers.go; if the two
// diverge, the table-driven tests below catch it.
func expectedEnumFor(key string) (int, bool) {
	switch key {
	case dsKeyMemInfo:
		return xtcpnl.MemInfoEmumValueCst, true
	case dsKeyInfo:
		return xtcpnl.TCPInfoEmumValueCst, true
	case dsKeyVegas:
		return xtcpnl.VegasInfoEnumValueCst, true
	case dsKeyCong:
		return xtcpnl.CongInfoEmumValueCst, true
	case dsKeyTos:
		return xtcpnl.TypeOfServiceEmumValueCst, true
	case dsKeyTc:
		return xtcpnl.TrafficClassEmumValueCst, true
	case dsKeySkmem:
		return xtcpnl.SkMemInfoEnumValueCst, true
	case dsKeyShut:
		return xtcpnl.ShutdownEmumValueCst, true
	case dsKeyDctcp:
		return xtcpnl.DCTCPInfoEnumValueCst, true
	case dsKeyBbr:
		return xtcpnl.BBRInfoEnumValueCst, true
	case dsKeyClassID:
		return xtcpnl.ClassIDEnumValueCst, true
	case dsKeyCgroup:
		return xtcpnl.CGroupIDEnumValueCst, true
	case dsKeySockopt:
		return xtcpnl.SockOptEnumValueCst, true
	}
	return 0, false
}

// runInit is a tiny harness so each table row reads top-to-bottom
// without re-typing the wg.Add/Done dance.
func runInit(t *testing.T, x *XTCP) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitDeserializers(&wg)
	wg.Wait()
}

// makeXTCPWithEnabled builds an XTCP fixture with the given keys
// flipped on in EnabledDeserializers. Missing fields stay nil so the
// nil-deref path (bug 77) cannot accidentally be hidden.
func makeXTCPWithEnabled(keys ...string) *XTCP {
	enabled := make(map[string]bool, len(keys))
	for _, k := range keys {
		enabled[k] = true
	}
	return &XTCP{
		config: &xtcp_config.XtcpConfig{
			EnabledDeserializers: &xtcp_config.EnabledDeserializers{
				Enabled: enabled,
			},
		},
	}
}

func TestInitDeserializers_table(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		category string // positive | negative | boundary | corner | adversarial
		build    func() *XTCP
		// wantKeys: keys we expect to land in RTATypeDeserializerStr.
		// wantCount: explicit map-length assertion (use len(wantKeys)
		//            when set, but lets us still check 0-or-more cases).
		wantKeys  []string
		wantCount int
	}{
		// ── positive ─────────────────────────────────────────────────
		{
			name:      "positive_single_info",
			category:  "positive",
			build:     func() *XTCP { return makeXTCPWithEnabled(dsKeyInfo) },
			wantKeys:  []string{dsKeyInfo},
			wantCount: 1,
		},
		{
			name:     "positive_tcp_core_subset",
			category: "positive",
			build: func() *XTCP {
				return makeXTCPWithEnabled(dsKeyInfo, dsKeyBbr, dsKeyVegas, dsKeySkmem, dsKeyShut, dsKeyDctcp, dsKeyCgroup)
			},
			wantKeys:  []string{dsKeyInfo, dsKeyBbr, dsKeyVegas, dsKeySkmem, dsKeyShut, dsKeyDctcp, dsKeyCgroup},
			wantCount: 7,
		},
		{
			name:      "positive_all_thirteen_keys",
			category:  "positive",
			build:     func() *XTCP { return makeXTCPWithEnabled(allKnownDispatchKeys...) },
			wantKeys:  allKnownDispatchKeys,
			wantCount: len(allKnownDispatchKeys),
		},

		// ── negative ─────────────────────────────────────────────────
		{
			name:      "negative_unknown_key_only",
			category:  "negative",
			build:     func() *XTCP { return makeXTCPWithEnabled("not_a_real_inet_diag_attribute") },
			wantCount: 0,
		},
		{
			name:     "negative_unknown_key_mixed_with_real",
			category: "negative",
			build: func() *XTCP {
				return makeXTCPWithEnabled("fakeAttr", dsKeyInfo, "another_fake")
			},
			wantKeys:  []string{dsKeyInfo}, // only the real one registers
			wantCount: 1,
		},
		{
			name:     "negative_value_false_should_still_register",
			category: "negative",
			// The dispatch logic uses `_, exists := ...; exists` not the
			// map *value*, so any presence registers. Pin that contract.
			build: func() *XTCP {
				return &XTCP{
					config: &xtcp_config.XtcpConfig{
						EnabledDeserializers: &xtcp_config.EnabledDeserializers{
							Enabled: map[string]bool{
								dsKeyInfo: false,
								dsKeyBbr:  false,
							},
						},
					},
				}
			},
			wantKeys:  []string{dsKeyInfo, dsKeyBbr},
			wantCount: 2,
		},

		// ── boundary ────────────────────────────────────────────────
		{
			name:      "boundary_empty_enabled_map",
			category:  "boundary",
			build:     func() *XTCP { return makeXTCPWithEnabled() },
			wantCount: 0,
		},
		{
			name:      "boundary_only_first_table_entry",
			category:  "boundary",
			build:     func() *XTCP { return makeXTCPWithEnabled(dsKeyMemInfo) },
			wantKeys:  []string{dsKeyMemInfo},
			wantCount: 1,
		},
		{
			name:      "boundary_only_last_table_entry",
			category:  "boundary",
			build:     func() *XTCP { return makeXTCPWithEnabled(dsKeySockopt) },
			wantKeys:  []string{dsKeySockopt},
			wantCount: 1,
		},

		// ── corner ──────────────────────────────────────────────────
		{
			name:     "corner_nil_enabled_deserializers",
			category: "corner",
			// Bug 77 regression — pre-fix this nil-derefed on the
			// first Enabled[key] lookup.
			build: func() *XTCP {
				return &XTCP{config: &xtcp_config.XtcpConfig{}}
			},
			wantCount: 0,
		},
		{
			name:     "corner_nil_enabled_map_inside_struct",
			category: "corner",
			// Different shape from above: the *EnabledDeserializers
			// pointer is non-nil but its inner map is nil. Range over
			// nil map is fine — must not panic and must register 0.
			build: func() *XTCP {
				return &XTCP{
					config: &xtcp_config.XtcpConfig{
						EnabledDeserializers: &xtcp_config.EnabledDeserializers{
							Enabled: nil,
						},
					},
				}
			},
			wantCount: 0,
		},
		{
			name:      "corner_empty_string_key",
			category:  "corner",
			build:     func() *XTCP { return makeXTCPWithEnabled("") },
			wantCount: 0,
		},
		{
			name:     "corner_case_sensitivity",
			category: "corner",
			// Dispatch is case-sensitive — "INFO" should not match
			// the canonical "info" key.
			build:     func() *XTCP { return makeXTCPWithEnabled("INFO", "Bbr", "VEGAS") },
			wantCount: 0,
		},
		{
			name:     "corner_whitespace_around_key",
			category: "corner",
			// Leading/trailing whitespace should not silently match.
			build:     func() *XTCP { return makeXTCPWithEnabled(" info", "info ", "\tinfo", "info\n") },
			wantCount: 0,
		},
		{
			name:     "corner_debug_level_high_does_not_change_dispatch",
			category: "corner",
			// debugLevel>10 enters the log-loop at the bottom — must
			// still produce the same dispatch entries.
			build: func() *XTCP {
				x := makeXTCPWithEnabled(dsKeyInfo, dsKeyBbr)
				x.debugLevel = 11
				return x
			},
			wantKeys:  []string{dsKeyInfo, dsKeyBbr},
			wantCount: 2,
		},

		// ── adversarial ─────────────────────────────────────────────
		{
			name:     "adversarial_giant_enabled_map_with_junk",
			category: "adversarial",
			// 10 000 garbage keys + the 13 real ones. The walk should
			// still register exactly 13 (it iterates dispatchTable, not
			// the input map — O(13) regardless of input size).
			build: func() *XTCP {
				enabled := make(map[string]bool, 10013)
				for i := 0; i < 10000; i++ {
					enabled[fmt.Sprintf("garbage_%d", i)] = true
				}
				for _, k := range allKnownDispatchKeys {
					enabled[k] = true
				}
				return &XTCP{
					config: &xtcp_config.XtcpConfig{
						EnabledDeserializers: &xtcp_config.EnabledDeserializers{
							Enabled: enabled,
						},
					},
				}
			},
			wantKeys:  allKnownDispatchKeys,
			wantCount: len(allKnownDispatchKeys),
		},
		{
			name:     "adversarial_non_ascii_keys",
			category: "adversarial",
			build: func() *XTCP {
				return makeXTCPWithEnabled("infö", "ＩＮＦＯ", "🚀info", "info\x00")
			},
			wantCount: 0,
		},
		{
			name:     "adversarial_extremely_long_key",
			category: "adversarial",
			build: func() *XTCP {
				huge := make([]byte, 1<<16) // 64 KiB
				for i := range huge {
					huge[i] = 'a'
				}
				return makeXTCPWithEnabled(string(huge))
			},
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		tc := tc // pin for parallel sub-tests
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := tc.build()
			runInit(t, x)

			if x.RTATypeDeserializer == nil {
				t.Fatal("RTATypeDeserializer map nil — Init must always allocate")
			}
			if x.RTATypeDeserializerStr == nil {
				t.Fatal("RTATypeDeserializerStr map nil — Init must always allocate")
			}
			if got, want := len(x.RTATypeDeserializer), tc.wantCount; got != want {
				t.Errorf("dispatch entry count = %d, want %d (entries: %v)",
					got, want, x.RTATypeDeserializerStr)
			}
			if len(x.RTATypeDeserializer) != len(x.RTATypeDeserializerStr) {
				t.Errorf("func map and str map disagree: %d vs %d",
					len(x.RTATypeDeserializer), len(x.RTATypeDeserializerStr))
			}
			for _, k := range tc.wantKeys {
				enum, ok := expectedEnumFor(k)
				if !ok {
					t.Fatalf("test bug: wantKey %q not in expectedEnumFor", k)
				}
				if fn, present := x.RTATypeDeserializer[enum]; !present || fn == nil {
					t.Errorf("expected key %q (enum %d) registered with non-nil fn; got present=%v fnNil=%v",
						k, enum, present, fn == nil)
				}
				if got := x.RTATypeDeserializerStr[enum]; got != k {
					t.Errorf("RTATypeDeserializerStr[%d] = %q, want %q", enum, got, k)
				}
			}
		})
	}
}

// TestInitDeserializers_idempotent verifies a re-init does not duplicate
// or drop entries — important because production code is the only
// caller today, but Init is exported as a public method and may be
// re-invoked in test contexts. With the table-walk refactor each Init
// re-allocates both maps fresh, so the post-condition is "second Init
// looks identical to first Init."
func TestInitDeserializers_idempotent(t *testing.T) {
	x := makeXTCPWithEnabled(dsKeyInfo, dsKeyBbr, dsKeyDctcp)
	runInit(t, x)
	firstFuncs := len(x.RTATypeDeserializer)
	firstStrs := len(x.RTATypeDeserializerStr)
	runInit(t, x) // re-init
	if got := len(x.RTATypeDeserializer); got != firstFuncs {
		t.Errorf("RTATypeDeserializer after re-init = %d, want %d", got, firstFuncs)
	}
	if got := len(x.RTATypeDeserializerStr); got != firstStrs {
		t.Errorf("RTATypeDeserializerStr after re-init = %d, want %d", got, firstStrs)
	}
}

// TestInitDeserializers_concurrentDifferentInstances exercises the
// race detector. Two goroutines initializing *separate* XTCP fixtures
// should never race — they only share the read-only dispatchTable.
// Run with `go test -race`.
func TestInitDeserializers_concurrentDifferentInstances(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan string, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			x := makeXTCPWithEnabled(allKnownDispatchKeys...)
			var iwg sync.WaitGroup
			iwg.Add(1)
			x.InitDeserializers(&iwg)
			iwg.Wait()
			if len(x.RTATypeDeserializer) != len(allKnownDispatchKeys) {
				errCh <- fmt.Sprintf("goroutine %d: dispatch len = %d, want %d",
					id, len(x.RTATypeDeserializer), len(allKnownDispatchKeys))
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Error(e)
	}
}

// TestInitDeserializers_concurrentGetAll exercises the read side of
// dispatchTable: many goroutines concurrently call GetAllDeserializers
// while others run Init. Catches any future code that decides to
// mutate dispatchTable lazily.
func TestInitDeserializers_concurrentGetAll(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = GetAllDeserializers()
				}
			}
		}()
	}
	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				x := makeXTCPWithEnabled(dsKeyInfo, dsKeyBbr)
				var iwg sync.WaitGroup
				iwg.Add(1)
				x.InitDeserializers(&iwg)
				iwg.Wait()
			}
		}()
	}
	close(stop)
	wg.Wait()
}

// TestGetAllDeserializers_matchesDispatchTable pins the contract that
// GetAllDeserializers enumerates exactly the dispatchTable. If a new
// entry is added to the table, this test fails until the constant
// `allKnownDispatchKeys` above is updated — that's deliberate.
func TestGetAllDeserializers_matchesDispatchTable(t *testing.T) {
	got := GetAllDeserializers()
	if len(got) != len(allKnownDispatchKeys) {
		t.Fatalf("dispatch length drifted: got %d (%v), want %d (%v)",
			len(got), got, len(allKnownDispatchKeys), allKnownDispatchKeys)
	}
	for i, k := range allKnownDispatchKeys {
		if got[i] != k {
			t.Errorf("dispatch[%d] = %q, want %q", i, got[i], k)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks — measure the table-walk cost vs the old 13-branch chain.
// ───────────────────────────────────────────────────────────────────────

func BenchmarkInitDeserializers_empty(b *testing.B) {
	b.ReportAllocs()
	x := makeXTCPWithEnabled()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		x.InitDeserializers(&wg)
		wg.Wait()
	}
}

func BenchmarkInitDeserializers_singleKey(b *testing.B) {
	b.ReportAllocs()
	x := makeXTCPWithEnabled(dsKeyInfo)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		x.InitDeserializers(&wg)
		wg.Wait()
	}
}

func BenchmarkInitDeserializers_allKeys(b *testing.B) {
	b.ReportAllocs()
	x := makeXTCPWithEnabled(allKnownDispatchKeys...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		x.InitDeserializers(&wg)
		wg.Wait()
	}
}

func BenchmarkInitDeserializers_giantEnabledMap(b *testing.B) {
	b.ReportAllocs()
	enabled := make(map[string]bool, 10013)
	for i := 0; i < 10000; i++ {
		enabled[fmt.Sprintf("garbage_%d", i)] = true
	}
	for _, k := range allKnownDispatchKeys {
		enabled[k] = true
	}
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{
			EnabledDeserializers: &xtcp_config.EnabledDeserializers{
				Enabled: enabled,
			},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		x.InitDeserializers(&wg)
		wg.Wait()
	}
}

func BenchmarkGetAllDeserializers(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetAllDeserializers()
	}
}
