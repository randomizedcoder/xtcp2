package linkmonitor

import "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"

type exceptionTarget struct {
	key        model.DeviceKey
	generation uint64
}

type exceptionDevice struct {
	target      exceptionTarget
	eligibility model.Eligibility
	names       []string // Current primary name, native selector, IPoIB/RDMA aliases.
}

type exceptionStatus uint8

const (
	exceptionUnmatched exceptionStatus = iota
	exceptionMatched
	exceptionAmbiguous
)

type exceptionResolution struct {
	selector     string
	status       exceptionStatus
	target       exceptionTarget
	expectedBits uint64 // 0: any speed is exempt
}

// Resolve against each current inventory, never a saved ifindex-to-name mapping.
// Duplicate aliases of the same lifetime are harmless; different targets are
// ambiguous and exempt neither. Matching alone never changes the raw checks.
// Selectors are canonical (see normalizeExceptions); one with an expected
// speed is looked up by its name and carries that speed.
func resolveExceptions(selectors []string, inventory []exceptionDevice) []exceptionResolution {
	index := make(map[string]exceptionResolution)
	for i := range inventory {
		device := &inventory[i]
		if device.eligibility == model.Excluded {
			continue
		}
		for _, name := range device.names {
			entry, exists := index[name]
			if device.eligibility == model.EligibilityUnknown || (exists && (entry.status == exceptionAmbiguous || entry.target != device.target)) {
				index[name] = exceptionResolution{status: exceptionAmbiguous}
			} else {
				index[name] = exceptionResolution{status: exceptionMatched, target: device.target}
			}
		}
	}
	result := make([]exceptionResolution, len(selectors))
	for i, selector := range selectors {
		name, expectedBits, err := parseException(selector)
		if err != nil {
			name, expectedBits = selector, 0 // never matches an inventory name
		}
		result[i] = index[name]
		result[i].selector, result[i].expectedBits = selector, expectedBits
	}
	return result
}
