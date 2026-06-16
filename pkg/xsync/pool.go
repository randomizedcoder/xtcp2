// Package xsync provides thin, generic, type-safe wrappers over the
// standard library's sync.Pool (and, later, sync.Map). The point is to
// move the single unavoidable type assertion into one provably-correct
// place so call sites get a typed value back instead of a scattered
// `v, _ := p.Get().(*T)` plus an errcheck suppression at every site.
package xsync

import "sync"

// Pool is a typed wrapper over sync.Pool. The zero value is usable as a
// struct field once Init has set its New function — the wrapper embeds
// sync.Pool by value, so a Pool field has the same memory layout (and
// no extra indirection) as a plain sync.Pool field.
//
// A failed assertion in Get is unreachable by construction — New always
// returns T and Put only accepts T — so the only way to trip the panic
// is a programming error (the pool was miswired), never load or memory
// pressure. Failing loud there is deliberate: a silent zero value would
// surface later as a far harder-to-debug nil dereference.
type Pool[T any] struct {
	p sync.Pool
}

// Init sets the pool's New function. Call it once before first use on a
// Pool used as a struct field (the zero value is otherwise New-less and
// Get would panic). Pool must not be copied after Init.
func (p *Pool[T]) Init(newFn func() T) {
	p.p.New = func() any { return newFn() }
}

// NewPool returns an already-Init'd *Pool[T] — a convenience for
// function-local pools that are naturally held by pointer.
func NewPool[T any](newFn func() T) *Pool[T] {
	p := &Pool[T]{}
	p.Init(newFn)
	return p
}

// Get returns a value from the pool, allocating via New if empty.
func (p *Pool[T]) Get() T {
	v, ok := p.p.Get().(T)
	if !ok {
		panic("xsync.Pool: Get returned a value of the wrong type — New/Put miswired")
	}
	return v
}

// Put returns v to the pool for reuse.
func (p *Pool[T]) Put(v T) {
	p.p.Put(v)
}
