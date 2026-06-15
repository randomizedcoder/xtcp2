package xtcp

import (
	"context"
	"sync"
	"testing"
)

// Test-fixture keys/values reused across every TestReconcileMaps row.
const (
	testKey1   = "key1"
	testKey2   = "key2"
	testKey3   = "key3"
	testKey4   = "key4"
	testValue1 = "value1"
	testValue2 = "value2"
	testValue3 = "value3"
	testValue4 = "value4"

	// testOldValue2 mimics a stale entry already present in the destination
	// map when reconcileMaps runs — it should be replaced by testValue2.
	testOldValue2 = "old_value2"
)

func TestReconcileMaps(t *testing.T) {
	tests := []struct {
		name         string
		srcEntries   map[interface{}]interface{}
		destEntries  map[interface{}]interface{}
		expectedDest map[interface{}]interface{}
		deletes      int
		stores       int
	}{
		{
			name: "Add missing keys and remove extra keys",
			srcEntries: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			destEntries: map[interface{}]interface{}{
				testKey2: testOldValue2,
				testKey3: testValue3,
			},
			expectedDest: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			deletes: 2,
			stores:  2,
		},
		{
			name: "No changes needed",
			srcEntries: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			destEntries: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			expectedDest: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			deletes: 0,
			stores:  0,
		},
		{
			name: "Add missing keys and remove extra keys, one extra",
			srcEntries: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			destEntries: map[interface{}]interface{}{
				testKey2: testOldValue2,
				testKey3: testValue3,
				testKey4: testValue4,
			},
			expectedDest: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			deletes: 3,
			stores:  2,
		},
		{
			name: "Add missing keys and remove extra keys, one less",
			srcEntries: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			destEntries: map[interface{}]interface{}{
				testKey2: testOldValue2,
			},
			expectedDest: map[interface{}]interface{}{
				testKey1: testValue1,
				testKey2: testValue2,
			},
			deletes: 1,
			stores:  2,
		},
	}

	var x XTCP

	// In production, discoverAllNamespaces builds srcMap with nil
	// values (see pkg/xtcp/ns_discover.go: nsMap.Store(nsName, nil)).
	// Without the !srcValue==nil short-circuit, reconcileMaps treats
	// nil != netNSitem as drift and deletes every entry every cycle,
	// causing nsAdd to spawn a new netNamespaceInstance goroutine
	// while the existing one (still holding a netlink socketFD) is
	// orphaned. This regression test asserts that nil src values
	// don't trigger a delete.
	t.Run("production_nil_src_values_preserve_dest", func(t *testing.T) {
		srcMap := &sync.Map{}
		srcMap.Store("/run/netns/foo", nil) // discover's actual shape
		srcMap.Store("/run/netns/bar", nil)
		destMap := &sync.Map{}
		destMap.Store("/run/netns/foo", "netNSitem-foo") // simulates netNSitem
		destMap.Store("/run/netns/bar", "netNSitem-bar")

		dels, stores := x.reconcileMaps(context.Background(), srcMap, destMap, true)
		if dels != 0 {
			t.Errorf("expected 0 deletes (nil src values must not count as drift); got %d", dels)
		}
		if stores != 0 {
			t.Errorf("expected 0 stores (dest already has these keys); got %d", stores)
		}
		// destMap should still have the original netNSitem values.
		if v, ok := destMap.Load("/run/netns/foo"); !ok || v != "netNSitem-foo" {
			t.Errorf("destMap[foo] = (%v, %v); want netNSitem-foo, true", v, ok)
		}
	})

	// Bug 41 regression: a backstop delete (key in dest, not in src)
	// must call netNSitem.cancel() so the orphaned per-ns goroutine
	// + netlinkers + socketFD wind down. testing=false uses the
	// production path which now invokes cancel before Delete.
	t.Run("backstop_delete_calls_netNSitem_cancel", func(t *testing.T) {
		srcMap := &sync.Map{} // empty src → every dest entry is "gone"
		destMap := &sync.Map{}

		// Build a netNSitem with an observable cancel func.
		var cancelCalled bool
		nsName := "/run/netns/stale"
		destMap.Store(nsName, netNSitem{
			name:   &nsName,
			cancel: func() { cancelCalled = true },
		})

		// Need a stub XTCP with the pC CounterVec the production
		// path increments via nsAdd → but with empty src nothing is
		// added. Just enough to call reconcileMaps; the cancel branch
		// runs before any counter increments.
		x2 := newPollerFixture(t) // reuses the test helper with metrics
		dels, _ := x2.reconcileMaps(context.Background(), srcMap, destMap, false)
		if dels != 1 {
			t.Errorf("dels = %d, want 1", dels)
		}
		if !cancelCalled {
			t.Error("netNSitem.cancel() was not called — bug 41 regression")
		}
		if _, ok := destMap.Load(nsName); ok {
			t.Errorf("destMap still has %q after backstop delete", nsName)
		}
	})

	// Bug 41 negative case: testing=true callers may pass arbitrary
	// value types (raw strings, in the table tests above). The cancel
	// branch must skip the type-assertion safely instead of panicking.
	t.Run("backstop_delete_non_netNSitem_value_is_safe", func(t *testing.T) {
		srcMap := &sync.Map{}
		destMap := &sync.Map{}
		destMap.Store("k", "raw-string-not-netNSitem")
		// testing=true is the path the table tests use. Even if a
		// caller forgets and passes testing=false with non-netNSitem
		// values, no panic should occur.
		dels, _ := x.reconcileMaps(context.Background(), srcMap, destMap, false)
		if dels != 1 {
			t.Errorf("dels = %d, want 1", dels)
		}
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srcMap := &sync.Map{}
			for k, v := range test.srcEntries {
				srcMap.Store(k, v)
			}

			destMap := &sync.Map{}
			for k, v := range test.destEntries {
				destMap.Store(k, v)
			}

			deletes, stores := x.reconcileMaps(context.Background(), srcMap, destMap, true)

			actualDest := make(map[interface{}]interface{})
			destMap.Range(func(key, value interface{}) bool {
				actualDest[key] = value
				return true
			})

			if len(actualDest) != len(test.expectedDest) {
				t.Errorf("expected %d entries, got %d", len(test.expectedDest), len(actualDest))
			}

			for k, v := range test.expectedDest {
				if actualDest[k] != v {
					t.Errorf("key %v: expected value %v, got %v", k, v, actualDest[k])
				}
			}

			if deletes != test.deletes {
				t.Errorf("expected %d deletes, got %d", test.deletes, deletes)
			}

			if stores != test.stores {
				t.Errorf("expected %d stores, got %d", test.stores, stores)
			}
		})
	}
}
