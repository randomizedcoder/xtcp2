package linkmonitor

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const eventQueueCapacity = 4096

// eventInbox serializes only bounded scalar enqueue operations. The owner never
// holds its mutex while reconciling or publishing. Loss has a separate wake and
// atomic epoch, so a full data queue cannot hide it.
type eventInbox struct {
	events    chan model.Event
	wake      chan struct{}
	epoch     atomic.Uint64
	produced  atomic.Uint64
	exhausted atomic.Bool
	mu        sync.Mutex
}

func newEventInbox(epoch uint64) *eventInbox {
	q := &eventInbox{events: make(chan model.Event, eventQueueCapacity), wake: make(chan struct{}, 1)}
	q.epoch.Store(epoch)
	return q
}

func (q *eventInbox) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *eventInbox) lose(epoch uint64) {
	if epoch == math.MaxUint64 {
		if q.epoch.Load() == epoch {
			q.exhausted.Store(true)
			q.signal()
		}
		return
	}
	if q.epoch.CompareAndSwap(epoch, epoch+1) {
		q.signal()
	}
}

func (q *eventInbox) push(epoch uint64, event model.Event) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.epoch.Load() != epoch {
		return false
	}
	sequence := q.produced.Load()
	if sequence == math.MaxUint64 {
		q.exhausted.Store(true)
		q.signal()
		return false
	}
	if event.Kind == model.EventLoss {
		q.lose(epoch)
		return false
	}
	d := event.Observation.Device
	if len(d.Name) > 255 || len(d.HardwareID) > 1024 || len(d.Key.RDMADevice) > 63 {
		q.lose(epoch)
		return false
	}
	event.Sequence = sequence + 1
	event.Observation.Device.Token = model.Token{SourceEpoch: epoch}
	select {
	case q.events <- event:
		q.produced.Store(event.Sequence)
		return true
	default:
		q.lose(epoch)
		return false
	}
}

func validEvent(r *reducer, event model.Event) error {
	if err := validCarrier(event.Carrier); err != nil {
		return err
	}
	switch event.Kind {
	case model.EventChange:
		return validateObservation(r, event.Observation)
	case model.EventRemove, model.EventRefresh:
		if !r.validKey(event.Observation.Device.Key) {
			return fmt.Errorf("invalid event identity")
		}
		return nil
	default:
		return fmt.Errorf("invalid event kind")
	}
}

func validateObservation(r *reducer, observation model.Observation) error {
	d := observation.Device
	nameValid := validName(d.Name, 255)
	if d.Key.Kind == model.DeviceNativeRDMA {
		canonical := "rdma:" + d.Key.RDMADevice + ":" + strconv.FormatUint(uint64(d.Key.Port), 10)
		nameValid = d.Name == "" || d.Name == d.Key.RDMADevice || d.Name == canonical
	}
	if !r.validKey(d.Key) || !nameValid || len(d.HardwareID) > 1024 || d.Eligibility > model.Excluded || observation.Observed.Monotonic < 0 {
		return fmt.Errorf("invalid inventory observation")
	}
	return nil
}
