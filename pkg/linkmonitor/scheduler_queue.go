package linkmonitor

import (
	"container/list"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type jobUrgency uint8

const (
	urgencyEvent jobUrgency = iota
	urgencyReconcile
	urgencyPeriodic
	urgencyCount
)

// readyQueue rotates devices, then jobs within each device. A job has one
// removable queue entry, so promotion never leaves stale entries behind.
type readyQueue struct {
	devices list.List
	index   map[model.DeviceKey]*readyDevice
	peak    int
}

type readyDevice struct {
	element *list.Element
	jobs    list.List
}

func (q *readyQueue) push(job *scheduledJob) {
	if q.index == nil {
		q.index = make(map[model.DeviceKey]*readyDevice)
	}
	device := q.index[job.key.Device]
	if device == nil {
		device = &readyDevice{}
		device.element = q.devices.PushBack(job.key.Device)
		q.index[job.key.Device] = device
		q.peak = max(q.peak, len(q.index))
	}
	job.ready = device.jobs.PushBack(job)
}

func (q *readyQueue) remove(job *scheduledJob) {
	if job.ready == nil {
		return
	}
	device := q.index[job.key.Device]
	device.jobs.Remove(job.ready)
	job.ready = nil
	if device.jobs.Len() == 0 {
		q.devices.Remove(device.element)
		delete(q.index, job.key.Device)
		q.trim()
	}
}

func (q *readyQueue) pop() *scheduledJob {
	head := q.devices.Front()
	if head == nil {
		return nil
	}
	key, ok := head.Value.(model.DeviceKey)
	if !ok {
		panic("invalid ready device")
	}
	device := q.index[key]
	job, ok := device.jobs.Front().Value.(*scheduledJob)
	if !ok {
		panic("invalid ready job")
	}
	q.remove(job)
	if device.jobs.Len() != 0 {
		q.devices.MoveToBack(device.element)
	}
	return job
}

func (q *readyQueue) trim() {
	if len(q.index) == 0 {
		q.index, q.peak = nil, 0
		return
	}
	if q.peak <= 64 || len(q.index) > q.peak/4 {
		return
	}
	next := make(map[model.DeviceKey]*readyDevice, len(q.index))
	for key, device := range q.index {
		next[key] = device
	}
	q.index, q.peak = next, len(next)
}
