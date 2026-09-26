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

	// sync.Pool.Put deliberately throws the value away roughly one time
	// in four when the race detector is on — go/src/sync/pool.go:
	//
	//	if race.Enabled {
	//		if runtime_randn(4) == 0 {
	//			// Randomly drop x on floor.
	//			return
	//
	// so a single Put/Get round trip is not a reliable signal, and this
	// test failed ~25% of `go test -race` runs when it asserted pointer
	// identity after one Put. Any reuse at all proves the pool is wired;
	// zero reuse across N rounds has probability 0.25^N (~9e-13 at N=20),
	// which is far below the flake floor of everything else in the suite.
	const tries = 20
	for range tries {
		a := p.Get()
		*a = append((*a)[:0], 'x')
		p.Put(a)
		if p.Get() == a {
			return // reused: pooling is wired
		}
	}
	t.Fatalf("no reuse across %d Put/Get rounds; pooling not wired", tries)
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
