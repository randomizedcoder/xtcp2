package testkit

import (
	"context"
	"sync"
)

// Barrier announces worker entry and permits explicit release or cancellation.
type Barrier struct {
	entered, released      chan struct{}
	enterOnce, releaseOnce sync.Once
}

// NewBarrier creates a closed-until-released worker boundary.
func NewBarrier() *Barrier {
	return &Barrier{entered: make(chan struct{}), released: make(chan struct{})}
}

// Entered closes on the first Wait, so tests can act after acquisition started.
func (b *Barrier) Entered() <-chan struct{} { return b.entered }

// Wait announces entry and waits for release or context cancellation.
func (b *Barrier) Wait(ctx context.Context) error {
	b.enterOnce.Do(func() { close(b.entered) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.released:
		return ctx.Err()
	}
}

// Release is safe to call repeatedly, including during test cleanup.
func (b *Barrier) Release() { b.releaseOnce.Do(func() { close(b.released) }) }
