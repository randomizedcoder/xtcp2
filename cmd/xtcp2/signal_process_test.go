package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"testing"
	"time"
)

// signalStep is one real signal sent to the child, and the line the child
// prints in response, if it prints one before its stdin closes.
type signalStep struct {
	sig  os.Signal
	want string
}

// TestSignalProcess sends real signals to a child copy of the test binary,
// to show why runMain routes signals before it parses flags: before any
// Notify, Go drops SIGUSR1 but exits at once on SIGTERM, skipping the graceful
// shutdown. Once notifySignals has returned, every signal is queued.
func TestSignalProcess(t *testing.T) {
	if mode := os.Getenv("XTCP2_SIGNAL_HELPER"); mode != "" {
		if err := runSignalChild(mode); err != nil {
			t.Fatal(err)
		}
		return
	}
	tests := []struct {
		name, category, description, expectedOutcome string
		mode                                         string
		steps                                        []signalStep
		final                                        []string
		killedBy                                     os.Signal
	}{
		{"before_notify_usr1", "corner", "SIGUSR1 before any Notify", "Go drops it; the child runs on and exits 0",
			"none", []signalStep{{syscall.SIGUSR1, ""}}, []string{"alive"}, nil},
		{"before_notify_term", "negative", "SIGTERM before any Notify", "the child is killed by SIGTERM, with no graceful path",
			"none", []signalStep{{syscall.SIGTERM, ""}}, nil, syscall.SIGTERM},
		{"notify_term", "positive", "SIGTERM after notifySignals returns, before any loop reads it", "queued; the child exits 0",
			"notify", []signalStep{{syscall.SIGTERM, ""}}, []string{"queued terminated"}, nil},
		{"notify_usr1", "positive", "SIGUSR1 after notifySignals returns", "queued",
			"notify", []signalStep{{syscall.SIGUSR1, ""}}, []string{"queued user defined signal 1"}, nil},
		{"notify_interrupt", "corner", "SIGINT after notifySignals returns", "queued",
			"notify", []signalStep{{os.Interrupt, ""}}, []string{"queued interrupt"}, nil},
		{"handler_usr1_then_term", "positive", "initSignalHandler, then SIGUSR1 and SIGTERM", "one rebaseline, then cancel and exit 0",
			"handler", []signalStep{{syscall.SIGUSR1, "rebaseline"}, {syscall.SIGTERM, "canceled"}}, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, binary, "-test.run=^TestSignalProcess$")
			child.Env = append(os.Environ(), "XTCP2_SIGNAL_HELPER="+tc.mode)
			var stderr bytes.Buffer
			child.Stderr = &stderr
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if t.Failed() {
					t.Logf("child stderr:\n%s", stderr.String())
				}
			}()
			lines := bufio.NewScanner(stdout)
			awaitLine := func(want string) {
				t.Helper()
				for lines.Scan() {
					if lines.Text() == want {
						return
					}
				}
				t.Fatalf("child never printed %q: %v", want, lines.Err())
			}
			awaitLine("started")
			for _, step := range tc.steps {
				if err := child.Process.Signal(step.sig); err != nil {
					t.Fatal(err)
				}
				if step.want != "" {
					awaitLine(step.want)
				}
			}
			_ = stdin.Close()
			var rest []string
			for lines.Scan() {
				rest = append(rest, lines.Text())
			}
			for _, want := range tc.final {
				if !slices.Contains(rest, want) {
					t.Errorf("child output %q, want a line %q", rest, want)
				}
			}
			checkChildExit(t, child.Wait(), tc.killedBy)
		})
	}
}

// checkChildExit wants a clean exit when killedBy is nil, and death by
// killedBy otherwise.
func checkChildExit(t *testing.T, err error, killedBy os.Signal) {
	t.Helper()
	if killedBy == nil {
		if err != nil {
			t.Errorf("child exit: %v, want 0", err)
		}
		return
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exit: %v, want killed by %v", err, killedBy)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != killedBy {
		t.Errorf("child exit: %v, want killed by %v", err, killedBy)
	}
}

// runSignalChild is the child side of TestSignalProcess. It prints "started"
// once its signal routing for mode is in place, and stdin's close is the
// parent's "signals sent".
func runSignalChild(mode string) error {
	switch mode {
	case "none":
		fmt.Println("started")
		_, _ = io.Copy(io.Discard, os.Stdin)
		time.Sleep(200 * time.Millisecond)
		fmt.Println("alive")
	case "notify":
		c := notifySignals()
		defer signal.Stop(c)
		fmt.Println("started")
		_, _ = io.Copy(io.Discard, os.Stdin)
		select {
		case sig := <-c:
			fmt.Println("queued", sig)
		case <-time.After(2 * time.Second):
			return errors.New("no signal queued")
		}
	case "handler":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		complete := make(chan struct{}, 1)
		initSignalHandler(cancel, complete, func() { fmt.Println("rebaseline") })
		fmt.Println("started")
		<-ctx.Done()
		fmt.Println("canceled")
		complete <- struct{}{}
		// awaitSignalAndShutdown exits the process with 0.
		time.Sleep(5 * time.Second)
		return errors.New("signal loop did not exit the process")
	default:
		return fmt.Errorf("unknown signal helper mode %q", mode)
	}
	return nil
}
