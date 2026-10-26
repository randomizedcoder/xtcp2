package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/attribution"
)

// The treatment feature reaches the daemon in one of two ways, selected by
// flag and never both:
//
//   - -treatmentAttribution <dir>: always compiled. A standalone
//     xtcp2-treatment-agent owns the journal in <dir>; the daemon only reads
//     the snapshot and stamps provenance onto records. No gRPC client, no
//     TLS material, no kernel writes in this process.
//   - -treatmentConfig <file>: compiled only with the `treatment` build tag
//     (treatments_embedded.go), which registers embeddedTreatmentAgent. The
//     agent runs in-process and feeds the same stamper.
//
// Asking an untagged binary for -treatmentConfig is fatal, like asking a slim
// build for an enricher it lacks: a missing build tag is a property of the
// artifact, identical on every host, and must not degrade silently.

// treatmentFeeder is whichever mode is running.
type treatmentFeeder struct {
	stamp func(*flat.XtcpFlatRecord)
	wake  func()
}

var activeTreatments atomic.Pointer[treatmentFeeder]

// embeddedTreatmentAgent is nil unless built with -tags treatment.
var embeddedTreatmentAgent func(ctx context.Context, path string) (*treatmentFeeder, func(), error)

var errTreatmentNotCompiled = errors.New("-treatmentConfig requested but the treatment agent is not compiled into this binary; rebuild with '-tags treatment' (or use a full `xtcp2` Nix attribute), or run xtcp2-treatment-agent beside this daemon and point it at the journal with -treatmentAttribution")

// checkTreatmentFlags rejects the combinations that are configuration
// errors regardless of the controller's availability.
func checkTreatmentFlags(configPath, attributionDir string) error {
	if configPath != "" && attributionDir != "" {
		return errors.New("-treatmentConfig and -treatmentAttribution are mutually exclusive: either embed the agent or read a standalone agent's journal")
	}
	if configPath != "" && embeddedTreatmentAgent == nil {
		return errTreatmentNotCompiled
	}
	return nil
}

// treatmentStamper is the hook installed on the collector, nil when no mode
// is active.
func treatmentStamper() func(*flat.XtcpFlatRecord) {
	if f := activeTreatments.Load(); f != nil {
		return f.stamp
	}
	return nil
}

// reconcileTreatments is the SIGHUP action: check in now, or re-read the
// journal now.
func reconcileTreatments() {
	if f := activeTreatments.Load(); f != nil {
		f.wake()
	}
}

// startTreatments starts the selected mode and returns its stop function.
// Errors from here are runtime conditions (unreadable config, bad journal
// directory, controller unreachable) and must not stop socket telemetry.
func startTreatments(parent context.Context, configPath, attributionDir, location string) (func(), error) {
	if err := checkTreatmentFlags(configPath, attributionDir); err != nil {
		return nil, err
	}
	switch {
	case configPath != "":
		ctx, cancel := context.WithCancel(parent)
		f, stop, err := embeddedTreatmentAgent(ctx, configPath)
		if err != nil {
			cancel()
			return nil, err
		}
		activeTreatments.Store(f)
		return func() { cancel(); stop(); activeTreatments.CompareAndSwap(f, nil) }, nil
	case attributionDir != "":
		return startAttribution(parent, attributionDir, location)
	}
	return func() {}, nil
}

func startAttribution(parent context.Context, dir, location string) (func(), error) {
	var stamper attribution.Stamper
	w, err := attribution.NewWatcher(attribution.WatchConfig{Dir: dir, Location: location}, &stamper)
	if err != nil {
		return nil, fmt.Errorf("treatment attribution: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	f := &treatmentFeeder{stamp: stamper.Stamp, wake: w.Wake}
	activeTreatments.Store(f)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); w.Run(ctx) }()
	return func() { cancel(); wg.Wait(); activeTreatments.CompareAndSwap(f, nil) }, nil
}
