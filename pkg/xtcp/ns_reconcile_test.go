package xtcp

import (
	"context"
	"sync"
	"testing"
)

// const (
// 	debugLevelForTest = 111
// )

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
				"key1": "value1",
				"key2": "value2",
			},
			destEntries: map[interface{}]interface{}{
				"key2": "old_value2",
				"key3": "value3",
			},
			expectedDest: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			deletes: 2,
			stores:  2,
		},
		{
			name: "No changes needed",
			srcEntries: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			destEntries: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expectedDest: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			deletes: 0,
			stores:  0,
		},
		{
			name: "Add missing keys and remove extra keys, one extra",
			srcEntries: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			destEntries: map[interface{}]interface{}{
				"key2": "old_value2",
				"key3": "value3",
				"key4": "value4",
			},
			expectedDest: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			deletes: 3,
			stores:  2,
		},
		{
			name: "Add missing keys and remove extra keys, one less",
			srcEntries: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			destEntries: map[interface{}]interface{}{
				"key2": "old_value2",
			},
			expectedDest: map[interface{}]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			deletes: 1,
			stores:  2,
		},
	}

	var x XTCP

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
