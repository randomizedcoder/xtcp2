//go:build treatment

package main

import (
	"context"
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/agent"
)

// Built with -tags treatment: the controller agent runs inside the daemon
// and stamps records in-process. This file is the only place cmd/xtcp2
// imports pkg/treatment/agent, so an untagged build links neither the gRPC
// client nor the TLS identity code.
func init() { embeddedTreatmentAgent = startEmbeddedTreatmentAgent }

func startEmbeddedTreatmentAgent(ctx context.Context, path, location string) (*treatmentFeeder, func(), error) {
	c, err := agent.LoadConfig(path)
	if err != nil {
		return nil, nil, err
	}
	// Before any credential is read: a label disagreement is a deployment
	// error, not a runtime one, and the message should be the first thing
	// in the log.
	if err = checkTreatmentLocation(location, c.Location); err != nil {
		return nil, nil, err
	}
	if location == "" {
		log.Printf("treatment agent: this daemon has no -location/LOCATION; records take the agent's %q", c.Location)
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
