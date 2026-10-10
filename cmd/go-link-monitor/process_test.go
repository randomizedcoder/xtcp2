package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCommandProcess(t *testing.T) {
	if os.Getenv("LINKMONITOR_SIGNAL_HELPER") == "1" {
		if err := runSignalHelper(); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		termination                                  os.Signal
	}{
		{"term", "positive", "real SIGUSR1 then SIGTERM", "request processed and graceful exit", syscall.SIGTERM},
		{"interrupt", "corner", "real SIGUSR1 then interrupt", "request processed and graceful exit", os.Interrupt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, binary, "-test.run=^TestCommandProcess$")
			child.Env = append(os.Environ(), "LINKMONITOR_SIGNAL_HELPER=1")
			child.Stderr = os.Stderr
			output, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			lines := bufio.NewScanner(output)
			if !lines.Scan() || lines.Text() != "started" {
				t.Fatal("child not started", lines.Err())
			}
			if err := child.Process.Signal(syscall.SIGUSR1); err != nil {
				t.Fatal(err)
			}
			if !lines.Scan() || lines.Text() != "rebaseline" {
				t.Fatal("request not handled", lines.Err())
			}
			if err := child.Process.Signal(tc.termination); err != nil {
				t.Fatal(err)
			}
			if err := child.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func runSignalHelper() error {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM, os.Interrupt)
	defer signal.Stop(signals)
	m := &fakeMonitor{
		run:        func(ctx context.Context) error { fmt.Println("started"); <-ctx.Done(); return nil },
		rebaseline: func() error { fmt.Println("rebaseline"); return nil },
	}
	var lc net.ListenConfig
	return serve(context.Background(), m, prometheus.NewRegistry(), quietLogger(), signals, lc.Listen, "127.0.0.1:0")
}
