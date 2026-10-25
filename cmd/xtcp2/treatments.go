package main

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/agent"
)

var activeTreatments atomic.Pointer[agent.Agent]

func reconcileTreatments() {
	if a := activeTreatments.Load(); a != nil {
		a.Wake()
	}
}

func startTreatments(parent context.Context, path *string) (func(), error) {
	if path == nil || *path == "" {
		return func() {}, nil
	}
	c, err := agent.LoadConfig(*path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	a, err := agent.New(ctx, c)
	if err != nil {
		cancel()
		return nil, err
	}
	activeTreatments.Store(a)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.Run(ctx) }()
	return func() { cancel(); wg.Wait(); activeTreatments.CompareAndSwap(a, nil); a.Close() }, nil
}
