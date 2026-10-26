//go:build treatment

package main

import (
	"context"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/agent"
)

// Built with -tags treatment: the controller agent runs inside the daemon
// and stamps records in-process. This file is the only place cmd/xtcp2
// imports pkg/treatment/agent, so an untagged build links neither the gRPC
// client nor the TLS identity code.
func init() { embeddedTreatmentAgent = startEmbeddedTreatmentAgent }

func startEmbeddedTreatmentAgent(ctx context.Context, path string) (*treatmentFeeder, func(), error) {
	c, err := agent.LoadConfig(path)
	if err != nil {
		return nil, nil, err
	}
	a, err := agent.New(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.Run(ctx) }()
	return &treatmentFeeder{stamp: a.Stamp, wake: a.Wake}, func() { wg.Wait(); a.Close() }, nil
}
