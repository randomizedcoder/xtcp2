// Package rdmaevents owns read-only verbs contexts and bounded event delivery.
// No provider objects escape the adapter; the monitor owns policy and retries.
package rdmaevents

import (
	"context"
	"errors"
)

const (
	// Limit bounds discovery, contexts and identity storage.
	Limit = 65536
	// Batch bounds work per ready descriptor.
	Batch = 64
)

var (
	ErrUnavailable = errors.New("rdmaevents: build has no verbs event support")
	ErrIdentity    = errors.New("rdmaevents: identity changed")
	ErrLimit       = errors.New("rdmaevents: bound exceeded")
	ErrLost        = errors.New("rdmaevents: event coverage lost")
)

// Identity binds an HCA name to discovery's resolved hardware path.
type Identity struct{ Name, Hardware string }

// Kind is an owned interpretation, never a C union or provider pointer.
type Kind uint8

const (
	Refresh Kind = iota
	Topology
	Fatal
	Unknown
)

// Event contains only scalar evidence. Port zero means refresh the whole HCA.
type Event struct {
	Err    error
	Device string
	Port   uint32
	Kind   Kind
}

// Handle has one I/O owner. Next copies and acknowledges before returning.
// Close is called only after all Next calls have returned.
type Handle interface {
	FD() int
	Next() (Event, error)
	Close() error
}

// Provider acquires contexts synchronously. The caller retains occupancy until
// Open returns, including when the context's logical deadline has expired.
type Provider interface {
	Open(context.Context, Identity) (Handle, error)
}

// Source owns a fixed subscription generation and all its contexts.
type Source interface {
	Run(context.Context, func(Event) bool) error
	Close() error
}
