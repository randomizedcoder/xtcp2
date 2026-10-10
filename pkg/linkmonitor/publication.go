package linkmonitor

import (
	"math"
	"math/bits"
	"strconv"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const devicesPerPage = 32

type devicePage [devicesPerPage]*deviceBlock

type deviceChecks struct {
	maximumSpeed, maximumWidth, fullDuplex, rdmaReadiness model.Check
}

type deviceBlock struct {
	view       DeviceView
	observed   model.Stamp
	collectors [model.CollectorNetstat + 1]*collectorSnapshot
}

// collectorSnapshot contains no mutable working state or error objects. Its
// sample block and schema are shared across publications until a new result.
type collectorSnapshot struct {
	errors                   [6]uint64
	filtered, stale          model.Optional[uint64]
	support                  model.Support
	reason                   model.ErrorReason
	lastAttempt, lastSuccess model.Stamp
	discontinuities          uint64
	block                    *collectorBlock
	attempted, succeeded     bool
	hasSuccess, fresh        bool
	duration                 model.Optional[time.Duration]
}

type publicationState struct {
	health              Health
	expected            model.Optional[uint64]
	now                 model.Stamp
	baselineWriteErrors uint64
}

func (r *reducer) markDirty(index int) {
	page := index / devicesPerPage
	for len(r.dirtyPages) <= page {
		r.dirtyPages = append(r.dirtyPages, 0)
	}
	r.dirtyPages[page] |= uint32(1) << (index % devicesPerPage)
}

// publish is called only by the session's reducer, once per completed turn.
// It never walks numeric arrays, sorts labels or mutates an older publication.
func (r *reducer) publish(m *Monitor, state publicationState) error {
	previous := m.root.Load()
	version := uint64(1)
	if previous != nil {
		if previous.version == math.MaxUint64 {
			return errSequenceExhausted
		}
		version = previous.version + 1
	}
	r.expire(state.now.Monotonic)
	pageCount := (len(r.slots) + devicesPerPage - 1) / devicesPerPage
	next := &snapshotRoot{
		version: version, namespace: r.namespace, health: r.publicationHealth(state.health.Running, state.expected.Present),
		lastResync:          r.lastResync,
		baselineWriteErrors: state.baselineWriteErrors,
		counts:              LinkCounts{current: model.Optional[uint64]{Value: r.upCount, Present: r.lastResync.Present && r.uncertain == 0 && r.rdmaUncertain == 0}, expected: state.expected},
		host:                freezeCollector(&r.host), pages: make([]*devicePage, pageCount),
	}
	if previous != nil {
		copy(next.pages, previous.pages)
	}
	for pageIndex, mask := range r.dirtyPages {
		if pageIndex >= pageCount {
			break
		}
		if mask != 0 {
			next.pages[pageIndex] = r.freezePage(next.pages[pageIndex], pageIndex, mask)
		}
	}
	next.freezeExceptions(previous, r.exceptions)
	next.resyncs = r.resyncs
	if err := next.freezeDescriptors(previous); err != nil {
		return err
	}
	r.resetDirty(pageCount)
	m.root.Store(next)
	return nil
}

func (r *reducer) freezePage(previous *devicePage, pageIndex int, mask uint32) *devicePage {
	page := new(devicePage)
	if previous != nil {
		*page = *previous
	}
	for mask != 0 {
		offset := bits.TrailingZeros32(mask)
		mask &^= uint32(1) << offset
		index := pageIndex*devicesPerPage + offset
		page[offset] = nil
		if index < len(r.slots) {
			page[offset] = freezeDevice(r.slots[index])
		}
	}
	return page
}

func (r *reducer) resetDirty(pageCount int) {
	if pageCount == 0 {
		r.dirtyPages = nil
		return
	}
	r.dirtyPages = r.dirtyPages[:pageCount]
	if cap(r.dirtyPages) > 64 && pageCount <= cap(r.dirtyPages)/4 {
		r.dirtyPages = make([]uint32, pageCount)
	} else {
		clear(r.dirtyPages)
	}
}

func freezeDevice(slot *deviceSlot) *deviceBlock {
	d := &slot.device
	checks := effectiveChecks(slot)
	identity := "netdev:" + strconv.FormatUint(uint64(d.Key.Index), 10)
	name := d.Name
	if d.Key.Kind == model.DeviceNativeRDMA {
		identity = "rdma:" + d.Key.RDMADevice + ":" + strconv.FormatUint(uint64(d.Key.Port), 10)
		name = identity
	}
	block := &deviceBlock{
		view: DeviceView{
			identity: identity, name: name, generation: d.Token.Generation,
			up: d.Up.Value, upKnown: d.Up.Present, eligibility: d.Eligibility,
			maximumSpeed: checks.maximumSpeed, maximumWidth: checks.maximumWidth,
			fullDuplex: checks.fullDuplex, rdmaReadiness: checks.rdmaReadiness,
			upTransitions: slot.upTransitions, downTransitions: slot.downTransitions,
		},
		observed: slot.observed,
	}
	for kind := range slot.collectors {
		block.collectors[kind] = freezeCollector(&slot.collectors[kind])
	}
	block.view.collectors = &block.collectors
	return block
}

func freezeCollector(state *collectorState) *collectorSnapshot {
	if state.publication == nil {
		state.publication = &collectorSnapshot{
			errors: state.errors, filtered: state.filtered, stale: state.stale,
			support: state.support, reason: state.reason,
			lastAttempt: state.lastAttempt, lastSuccess: state.lastSuccess,
			discontinuities: state.discontinuities, block: state.block,
			attempted: state.attempted, succeeded: state.succeeded,
			hasSuccess: state.hasSuccess, fresh: state.fresh, duration: state.duration,
		}
	}
	return state.publication
}
