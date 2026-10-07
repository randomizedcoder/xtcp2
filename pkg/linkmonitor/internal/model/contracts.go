package model

import (
	"context"
	"time"
)

// EventSource owns its reader. Run preserves event order and stops delivery when
// visit returns false. Close wakes blocked I/O; callers join Run before reuse.
type EventSource interface {
	Run(context.Context, func(Event) bool) error
	Close() error
}

// Collector executes one synchronous attempt. It never starts hidden retries.
// An uncancellable syscall retains its worker slot until Collect returns.
type Collector interface {
	Collect(context.Context, Job) Result
}

// InventorySource uses a dedicated path independent of optional collector slots.
type InventorySource interface {
	Dump(context.Context) (Candidate, error)
	Query(context.Context, DeviceKey) (Observation, error)
}

// Baseline is the versioned durable count, not a persisted device inventory.
type Baseline struct {
	Version    int
	Count      uint64
	RecordedAt time.Time
}

// SaveOutcome explicitly distinguishes uncertain durability from a failed write.
type SaveOutcome uint8

const (
	SaveFailed SaveOutcome = iota
	SaveDurable
	SaveIndeterminate
)

// SaveResult retains cleanup errors along with the write outcome.
type SaveResult struct {
	Outcome SaveOutcome
	Err     error
}

// BaselineStore owns a lifetime lock. LoadAndLock distinguishes missing from
// invalid content. Save is serial; it does not update the reducer's baseline.
// Close must wait until ongoing storage activity no longer needs the lock.
type BaselineStore interface {
	LoadAndLock(context.Context) (Baseline, bool, error)
	Save(context.Context, Baseline) SaveResult
	Close() error
}

// Clock supplies monotonic scheduling and separately reportable wall time.
type Clock interface {
	Now() Stamp
	NewTimer(time.Duration) Timer
}

// Timer is owned by one scheduler. Reset replaces its deadline, never appends
// another pending wake. Stop removes a pending wake; C remains open.
type Timer interface {
	C() <-chan time.Time
	Reset(time.Duration)
	Stop()
}
