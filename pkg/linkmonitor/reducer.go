package linkmonitor

import (
	"errors"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var (
	errSequenceExhausted = errors.New("monitor identity sequence exhausted")
	errCollectionBusy    = errors.New("collector attempt already running")
)

// reducer is owned by one goroutine. Workers receive values and return owned
// results; only immutable publications will cross the reader boundary in P03-T02.
type reducer struct {
	namespace, epoch, revision, generation, attempt uint64
	index                                           map[model.DeviceKey]int
	slots                                           []*deviceSlot
	upCount, uncertain                              uint64
	host                                            collectorState
	dirtyPages                                      []uint32
}

type deviceSlot struct {
	device                         model.Device
	observed                       model.Stamp
	counted                        bool
	upTransitions, downTransitions uint64
	collectors                     [model.CollectorNetstat + 1]collectorState
	checks                         deviceChecks
}

func newReducer(namespace uint64) *reducer {
	return &reducer{namespace: namespace, epoch: 1, index: make(map[model.DeviceKey]int)}
}

// observe accepts an authoritative ordered observation. Event observations use
// zero generation/revision; query observations include both to reject stale data.
// It returns false for stale observations or an unchanged record.
func (r *reducer) observe(observation model.Observation) (bool, error) {
	d := observation.Device
	if !r.validKey(d.Key) {
		return false, fmt.Errorf("invalid device identity")
	}
	if d.Token.SourceEpoch != r.epoch {
		return false, nil
	}
	i, exists := r.index[d.Key]
	if !exists {
		if d.Token.Generation != 0 || d.Token.Revision != 0 {
			return false, nil
		}
		return r.insert(observation)
	}
	slot := r.slots[i]
	if !observationMatches(d.Token, slot.device.Token) {
		return false, nil
	}
	if d.HardwareID == "" {
		d.HardwareID = slot.device.HardwareID
	}
	replaced := d.HardwareID != "" && slot.device.HardwareID != "" && d.HardwareID != slot.device.HardwareID
	d.Token = slot.device.Token
	d.Token.SourceEpoch = r.epoch
	if !replaced && d == slot.device {
		if slot.observed != observation.Observed {
			r.markDirty(i)
		}
		slot.observed = observation.Observed
		return false, nil
	}
	if r.revision == math.MaxUint64 || (replaced && r.generation == math.MaxUint64) {
		return false, errSequenceExhausted
	}
	r.revision++
	d.Token.Revision, d.Token.SourceEpoch = r.revision, r.epoch
	if replaced {
		r.generation++
		d.Token.Generation = r.generation
		r.unaccount(slot)
		slot = &deviceSlot{}
		r.slots[i] = slot
	} else {
		countTransition(slot, d)
		r.unaccount(slot)
	}
	slot.device, slot.observed = d, observation.Observed
	r.account(slot)
	r.markDirty(i)
	return true, nil
}

func (r *reducer) insert(observation model.Observation) (bool, error) {
	if r.revision == math.MaxUint64 || r.generation == math.MaxUint64 {
		return false, errSequenceExhausted
	}
	r.revision++
	r.generation++
	observation.Device.Token = model.Token{Generation: r.generation, Revision: r.revision, SourceEpoch: r.epoch}
	slot := &deviceSlot{device: observation.Device, observed: observation.Observed}
	r.index[slot.device.Key] = len(r.slots)
	r.slots = append(r.slots, slot)
	r.account(slot)
	r.markDirty(len(r.slots) - 1)
	return true, nil
}

func (r *reducer) validKey(key model.DeviceKey) bool {
	if key.Namespace != r.namespace {
		return false
	}
	switch key.Kind {
	case model.DeviceEthernet:
		return key.Index > 0 && key.RDMADevice == "" && key.Port == 0
	case model.DeviceNativeRDMA:
		return key.Index == 0 && key.Port > 0 && validName(key.RDMADevice, 63)
	default:
		return false
	}
}

func observationMatches(incoming, current model.Token) bool {
	return (incoming.Generation == 0 || incoming.Generation == current.Generation) &&
		(incoming.Revision == 0 || incoming.Revision == current.Revision)
}

func countDecision(d model.Device) model.Optional[bool] {
	if d.Eligibility == model.Excluded {
		return presentValue(false)
	}
	if d.Eligibility == model.Eligible {
		return d.Up
	}
	return model.Optional[bool]{}
}

func (r *reducer) unaccount(slot *deviceSlot) {
	if slot.counted {
		r.upCount--
	}
	if !countDecision(slot.device).Present {
		r.uncertain--
	}
}

func (r *reducer) account(slot *deviceSlot) {
	decision := countDecision(slot.device)
	if decision.Present {
		slot.counted = decision.Value
	} else {
		r.uncertain++
	}
	// Unknown evidence preserves the last contribution; uncertainty prevents the
	// caller from publishing a healthy count or learning a new baseline.
	if slot.counted {
		r.upCount++
	}
}

func countTransition(slot *deviceSlot, next model.Device) {
	old := slot.device
	if old.Eligibility != model.Eligible || next.Eligibility != model.Eligible || !old.Up.Present || !next.Up.Present || old.Up.Value == next.Up.Value {
		return
	}
	if next.Up.Value {
		slot.upTransitions++
	} else {
		slot.downTransitions++
	}
}

// remove handles authoritative deletion. Dense swap removal avoids retaining
// per-ifindex tombstones and does not invalidate jobs for the moved device.
func (r *reducer) remove(key model.DeviceKey, token model.Token) (bool, error) {
	i, exists := r.index[key]
	if !exists || token.SourceEpoch != r.epoch || !observationMatches(token, r.slots[i].device.Token) {
		return false, nil
	}
	if r.revision == math.MaxUint64 {
		return false, errSequenceExhausted
	}
	r.revision++
	r.unaccount(r.slots[i])
	last := len(r.slots) - 1
	r.markDirty(i)
	r.markDirty(last)
	if i != last {
		r.slots[i] = r.slots[last]
		r.index[r.slots[i].device.Key] = i
	}
	r.slots[last] = nil
	r.slots = r.slots[:last]
	delete(r.index, key)
	r.trim()
	return true, nil
}

func (r *reducer) trim() {
	if len(r.slots) == 0 {
		r.slots = nil
		r.index = make(map[model.DeviceKey]int)
		return
	}
	if cap(r.slots) <= 64 || len(r.slots) > cap(r.slots)/4 {
		return
	}
	next := make([]*deviceSlot, len(r.slots))
	copy(next, r.slots)
	r.slots = next
	r.index = make(map[model.DeviceKey]int, len(next))
	for i, slot := range next {
		r.index[slot.device.Key] = i
	}
}

// loseEvents invalidates old-epoch results without guessing missing transitions.
// Recovery/converged inventory is the coordinator's responsibility in P05.
func (r *reducer) loseEvents() error {
	if r.epoch == math.MaxUint64 || r.revision == math.MaxUint64 {
		return errSequenceExhausted
	}
	r.epoch++
	r.revision++
	return nil
}
