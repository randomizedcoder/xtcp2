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
	selector string
	status   exceptionStatus
	target   exceptionTarget
}

// Resolve against each current inventory, never a saved ifindex-to-name mapping.
// Duplicate aliases of the same lifetime are harmless; different targets are
// ambiguous and exempt neither. Matching alone never changes the raw checks.
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
		result[i] = index[selector]
		result[i].selector = selector
	}
	return result
}
