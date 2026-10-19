// Package rdmaevents defines the RDMA port and device events the monitor
// schedules from, and the source contract that delivers them. The netlink
// source in rdmanative implements it; no verbs context or cgo is involved.
package rdmaevents

import (
	"context"
	"errors"
)

const (
	// Limit bounds discovery and identity storage.
	Limit = 65536
	// Batch bounds work per ready descriptor.
	Batch = 64
)

var (
	ErrUnavailable = errors.New("rdmaevents: no RDMA event source")
	ErrLost        = errors.New("rdmaevents: event coverage lost")
)

// Kind is an owned interpretation of a kernel notification.
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

// Source owns a fixed subscription generation and all its descriptors.
type Source interface {
	Run(context.Context, func(Event) bool) error
	Close() error
}
