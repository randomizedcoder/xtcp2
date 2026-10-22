package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/pprof"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"golang.org/x/sys/unix"
)

// TestFinishMain covers when finishMain re-execs: only for a pending config
// after a clean run, and each pending config at most once.
func TestFinishMain(t *testing.T) {
	tests := []struct {
		name, category, description, expectedOutcome string
		pending                                      *xtcp_config.XtcpConfig
		code                                         int
		wantTags                                     []string
	}{
		{"pending_clean_run", "positive", "a soft restart is pending and runMain returned 0", "re-exec into the pending config",
			&xtcp_config.XtcpConfig{Tag: "NEW"}, 0, []string{"NEW"}},
		{"nothing_pending", "negative", "no soft restart was requested", "no re-exec; the code is returned",
			nil, 0, nil},
		{"pending_failed_run", "negative", "a soft restart is pending but runMain returned 1", "no re-exec; the code is returned",
			&xtcp_config.XtcpConfig{Tag: "NEW"}, 1, nil},
		{"empty_config", "boundary", "the pending config is an empty XtcpConfig", "still re-execs: the carry is the config, not its contents",
			&xtcp_config.XtcpConfig{}, 0, []string{""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			pendingReexec.Store(tc.pending)
			t.Cleanup(func() { pendingReexec.Store(nil) })
			var gotTags []string
			reexec := func(c *xtcp_config.XtcpConfig) { gotTags = append(gotTags, c.GetTag()) }
			if got := finishMain(tc.code, reexec); got != tc.code {
				t.Errorf("finishMain returned %d, want %d", got, tc.code)
			}
			if !slices.Equal(gotTags, tc.wantTags) {
				t.Errorf("re-exec'd with tags %q, want %q", gotTags, tc.wantTags)
			}
			// corner: the pending config is consumed, so a second call never
			// re-execs again.
			finishMain(tc.code, reexec)
			if !slices.Equal(gotTags, tc.wantTags) {
				t.Errorf("second finishMain re-exec'd again: tags %q", gotTags)
			}
		})
	}
}

// TestReexecProcess runs a soft restart for real in a child copy of the test
// binary: runMain with a stubbed daemon that requests one, then finishMain's
// syscall.Exec into the same test, which prints the tag it was carried and
// whether it inherited an armed ITIMER_PROF. With a CPU profile on
// (-profile.mode=cpu, or the Pyroscope agent) the timer is off in the new
// image only because finishMain runs after runMain's deferred profiler stops.
// An armed timer is what the s3parquet-stress VM tripped over: SIGPROF, back
// at its default action after execve, killed the new image whenever it fired
// before the Go runtime installed its handler, so the soft restart came back
// from systemd as a fresh process with the old config. That death is a race,
// so the negative row accepts it or an armed timer, and asserts nothing else.
func TestReexecProcess(t *testing.T) {
	if mode := os.Getenv("XTCP2_REEXEC_HELPER"); mode != "" {
		if err := runReexecChild(mode); err != nil {
			t.Fatal(err)
		}
		return
	}
	tests := []struct {
		name, category, description, expectedOutcome string
		mode                                         string
		want                                         string
		orKilledBy                                   os.Signal
	}{
		{"no_profile", "positive", "soft restart with no profiler", "the exec'd image loads the carried config; no timer",
			"none", "reexec tag=REEXEC-none itimer=off", nil},
		{"cpu_profile", "positive", "soft restart with -profile.mode=cpu", "the profile stops first; the exec'd image loads the carried config with no timer",
			"cpu", "reexec tag=REEXEC-cpu itimer=off", nil},
		{"pyroscope", "corner", "soft restart with the Pyroscope agent on and its server unreachable", "the agent stops first; the exec'd image loads the carried config with no timer",
			"pyroscope", "reexec tag=REEXEC-pyroscope itimer=off", nil},
		{"exec_during_profile", "negative", "syscall.Exec while a CPU profile is running", "the exec'd image inherits an armed timer, or SIGPROF kills it first",
			"exec_during_profile", "reexec tag=REEXEC-exec_during_profile itimer=armed", syscall.SIGPROF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, binary, "-test.run=^TestReexecProcess$")
			child.Env = append(envWithoutKey(os.Environ(), reconfigureEnvKey), "XTCP2_REEXEC_HELPER="+tc.mode)
			// -profile.mode=cpu writes cpu.pprof to the working directory.
			child.Dir = t.TempDir()
			var stdout, stderr bytes.Buffer
			child.Stdout = &stdout
			child.Stderr = &stderr
			err = child.Run()
			defer func() {
				if t.Failed() {
					t.Logf("child stdout:\n%s\nchild stderr:\n%s", stdout.String(), stderr.String())
				}
			}()
			if tc.orKilledBy != nil && killedBy(err, tc.orKilledBy) {
				return
			}
			checkChildExit(t, err, nil)
			var lines []string
			for s := bufio.NewScanner(&stdout); s.Scan(); {
				lines = append(lines, s.Text())
			}
			if !slices.Contains(lines, tc.want) {
				t.Errorf("child output %q, want a line %q", lines, tc.want)
			}
		})
	}
}

// runReexecChild is the child side of TestReexecProcess. The first image
// requests a soft restart; the image it exec's into finds the carried config
// and prints its tag.
func runReexecChild(mode string) error {
	carried, ok, err := loadReconfigureConfig()
	if err != nil {
		return err
	}
	if ok {
		it, err := unix.Getitimer(unix.ItimerProf)
		if err != nil {
			return err
		}
		timer := "off"
		if it.Value != (unix.Timeval{}) || it.Interval != (unix.Timeval{}) {
			timer = "armed"
		}
		fmt.Printf("reexec tag=%s itimer=%s\n", carried.GetTag(), timer)
		return nil
	}
	newCfg := &xtcp_config.XtcpConfig{Tag: "REEXEC-" + mode}

	if mode == "exec_during_profile" {
		if err := pprof.StartCPUProfile(io.Discard); err != nil {
			return err
		}
		burnCPU(50 * time.Millisecond)
		execSelfWithConfig(newCfg)
		return errors.New("execSelfWithConfig returned")
	}

	args := []string{"xtcp2", "-dest", "null"}
	switch mode {
	case "none":
	case "cpu":
		args = append(args, "-profile.mode=cpu")
	case "pyroscope":
		args = append(args, "-pyroscopeUrl=http://127.0.0.1:1")
	default:
		return fmt.Errorf("unknown re-exec helper mode %q", mode)
	}
	testArgs := os.Args
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	promHandlerStarter = func(context.Context, *sync.WaitGroup, promParams) error { return nil }
	daemonRunner = func(_ context.Context, _ context.CancelFunc, _ *xtcp_config.XtcpConfig) {
		// Run long enough for the profile's timer to be armed, then ask for
		// the restart the way runDaemonDefault does once it has drained.
		burnCPU(50 * time.Millisecond)
		pendingReexec.Store(newCfg)
	}
	code := runMain(context.Background())
	// The exec'd image is this test binary again, so it needs the test's
	// own arguments, not the xtcp2 flags runMain parsed.
	os.Args = testArgs
	code = finishMain(code, execSelfWithConfig)
	return fmt.Errorf("finishMain returned %d without re-exec'ing", code)
}

// killedBy reports whether err is an exit by signal sig.
func killedBy(err error, sig os.Signal) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == sig
}

// burnCPU spins for d so a running CPU profile has samples due.
func burnCPU(d time.Duration) {
	n := 0
	for end := time.Now().Add(d); time.Now().Before(end); {
		n++
	}
	_ = n
}
