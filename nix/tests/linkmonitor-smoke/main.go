// Command linkmonitor-smoke validates the public library without an HTTP server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (result error) {
	absentSysfs := flag.Bool("sandbox", false, "require absent sysfs; distinguish complete empty inventory from explicit unavailable inventory")
	flag.Parse()
	if *absentSysfs {
		if _, err := os.Stat("/sys"); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("negative fixture requires absent /sys: %v", err)
		}
	}
	dir, err := os.MkdirTemp("", "linkmonitor-smoke-")
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, os.RemoveAll(dir)) }()
	cfg := linkmonitor.DefaultConfig()
	cfg.BaselineFile, cfg.Settle = filepath.Join(dir, "baseline.json"), 0
	handler := &diagnostics{Handler: slog.NewTextHandler(os.Stderr, nil)}
	m, err := linkmonitor.New(cfg, linkmonitor.Options{Logger: slog.New(handler)})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	result = waitPublication(ctx, m, handler, *absentSysfs)
	cancel()
	return errors.Join(result, <-done)
}

type diagnostics struct {
	slog.Handler
	failures atomic.Uint64
}

func (h *diagnostics) Handle(ctx context.Context, record slog.Record) error {
	if record.Message == "link inventory reconciliation failed" {
		h.failures.Add(1)
	}
	return h.Handler.Handle(ctx, record)
}

func waitPublication(ctx context.Context, m *linkmonitor.Monitor, handler *diagnostics, absentSysfs bool) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("publication timeout: health=%+v", m.Health())
		case <-ticker.C:
			s := m.Snapshot()
			n := 0
			s.RangeSamples(func(linkmonitor.SampleView) bool { n++; return true })
			_, reconciled := s.LastSuccessfulResync()
			if s.Namespace() == 0 || n == 0 || (!absentSysfs && !reconciled) || (absentSysfs && !reconciled && handler.failures.Load() == 0) {
				continue
			}
			if absentSysfs {
				if err := sandboxOutcome(s, reconciled); err != nil {
					return err
				}
			}
			fmt.Printf("PASS: public lifecycle namespace=%d samples=%d health=%+v\n", s.Namespace(), n, s.Health())
			fmt.Println("Software publication only; no physical RDMA success is asserted.")
			return nil
		}
	}
}

func sandboxOutcome(s linkmonitor.Snapshot, reconciled bool) error {
	if reconciled {
		count, known := s.Counts().Current()
		if !known || count != 0 {
			return fmt.Errorf("sysfs-free inventory must prove zero eligible links")
		}
		fmt.Println("PASS: authoritative empty inventory; no hardware validation")
		return nil
	}
	if s.Health().CollectionHealthy || s.Health().Ready || s.Health().BaselineReady {
		return fmt.Errorf("failed inventory incorrectly accepted as healthy or learned")
	}
	if _, known := s.Counts().Current(); known {
		return fmt.Errorf("failed inventory fabricated a count")
	}
	fmt.Println("PASS: unavailable inventory stays unknown and unhealthy; host collector continues")
	return nil
}
