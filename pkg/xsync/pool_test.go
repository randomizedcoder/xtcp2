package xsync

import (
	"sync"
	"testing"
)

func TestPool_GetReturnsNewValue(t *testing.T) {
	calls := 0
	p := NewPool(func() *int {
		calls++
		v := 42
		return &v
	})

	got := p.Get()
	if got == nil || *got != 42 {
		t.Fatalf("Get() = %v, want pointer to 42", got)
	}
	if calls != 1 {
		t.Fatalf("New called %d times, want 1", calls)
	}
}

func TestPool_PutThenGetReuses(t *testing.T) {
	p := NewPool(func() *[]byte {
		b := make([]byte, 8)
		return &b
	})

	a := p.Get()
	*a = append((*a)[:0], 'x')
	p.Put(a)

	// After Put, the next Get should return the same backing pointer
	// (sync.Pool is best-effort, but in a single goroutine with no GC
	// in between it reliably hands the value straight back).
	b := p.Get()
	if b != a {
		t.Fatalf("Get after Put returned a different pointer; pooling not wired")
	}
}

func TestPool_GetType(t *testing.T) {
	type rec struct{ n int }
	p := NewPool(func() *rec { return &rec{n: 7} })
	r := p.Get()
	if r.n != 7 {
		t.Fatalf("Get().n = %d, want 7", r.n)
	}
}

func TestPool_ConcurrentGetPut(t *testing.T) {
	p := NewPool(func() *int { v := 0; return &v })
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			for range 1000 {
				v := p.Get()
				*v++
				p.Put(v)
			}
		})
	}
	wg.Wait()
}
