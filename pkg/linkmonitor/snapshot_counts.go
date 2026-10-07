package linkmonitor

import "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"

// LinkCounts retains coherent exact counts from one publication. An unknown
// current count preserves its last contribution but cannot produce a delta.
type LinkCounts struct {
	current, expected model.Optional[uint64]
}

// Current returns the up count and whether inventory evidence is complete.
func (c LinkCounts) Current() (uint64, bool) { return c.current.Value, c.current.Present }

// Expected returns the persisted baseline and whether one is available.
func (c LinkCounts) Expected() (uint64, bool) { return c.expected.Value, c.expected.Present }

// Delta returns the difference as a sign and exact unsigned magnitude, avoiding
// int64 overflow. Unknown inputs return known=false; zero is never negative.
func (c LinkCounts) Delta() (negative bool, magnitude uint64, known bool) {
	if !c.current.Present || !c.expected.Present {
		return false, 0, false
	}
	if c.current.Value < c.expected.Value {
		return true, c.expected.Value - c.current.Value, true
	}
	return false, c.current.Value - c.expected.Value, true
}

// Counts returns the current count, baseline and delta from this snapshot.
func (s Snapshot) Counts() LinkCounts {
	if s.root == nil {
		return LinkCounts{}
	}
	return s.root.counts
}

// Namespace returns the namespace identity associated with the device keys.
func (s Snapshot) Namespace() uint64 {
	if s.root == nil {
		return 0
	}
	return s.root.namespace
}
