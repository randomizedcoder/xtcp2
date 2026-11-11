package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

func TestExitCodeFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		description string
		err         error
		expected    int
	}{
		{"nil", "positive", "no error exits 0", nil, exitOK},
		{"not_compiled_in", "negative", "a feature not in this build is a usage error",
			xtcp.ErrNotCompiledIn, exitUsage},
		{"wrapped_not_compiled_in", "boundary", "the sentinel is found through wrapping",
			fmt.Errorf("-writeFiles 5: %w", xtcp.ErrNotCompiledIn), exitUsage},
		{"other_error", "corner", "any other error is a plain failure",
			errors.New("boom"), exitFailure},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeFor(tc.err); got != tc.expected {
				t.Errorf("%s: exit = %d, want %d", tc.description, got, tc.expected)
			}
		})
	}
}

// TestRunMain_debugWritersNotCompiledIn drives the startup refusal of the
// build-tag-gated debug writers. Each row's expectation follows what the test
// binary was built with, so the table holds under every flavor leg: without
// the tag the run is a usage error before the daemon starts; with it the
// (stubbed) daemon runs.
func TestRunMain_debugWritersNotCompiledIn(t *testing.T) {
	fileIn := xtcp.CheckBuildSupport(&xtcp_config.XtcpConfig{Dest: "file:/x"}) == nil
	cases := []struct {
		name        string
		category    string
		description string
		args        []string
		compiledIn  bool
	}{
		{"capture_not_compiled", "negative", "-writeFiles 5 needs -tags capture",
			[]string{"-dest", "null", "-writeFiles", "5"}, xtcp.CaptureCompiledIn},
		{"file_not_compiled", "negative", "-dest file: needs -tags dest_file",
			[]string{"-dest", "file:" + filepath.Join(t.TempDir(), "out.jsonl"), "-marshal", "jsonl"}, fileIn},
		{"writefiles_one", "boundary", "the smallest capture budget is refused too",
			[]string{"-dest", "null", "-writeFiles", "1"}, xtcp.CaptureCompiledIn},
		{"writefiles_zero_default", "corner", "-writeFiles 0 starts on every build",
			[]string{"-dest", "null", "-writeFiles", "0"}, true},
		{"stderr_dest", "positive", "the stderr destination needs no tag",
			[]string{"-dest", "stderr", "-marshal", "jsonl"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			envHelperReset(t)
			prevArgs := os.Args
			os.Args = append([]string{"xtcp2"}, tc.args...)
			t.Cleanup(func() { os.Args = prevArgs })

			prevProm := promHandlerStarter
			promHandlerStarter = func(context.Context, *sync.WaitGroup, promParams) error { return nil }
			t.Cleanup(func() { promHandlerStarter = prevProm })

			prevDaemon := daemonRunner
			started := false
			daemonRunner = func(context.Context, context.CancelFunc, *xtcp_config.XtcpConfig) { started = true }
			t.Cleanup(func() { daemonRunner = prevDaemon })

			wantRC, wantStarted := exitUsage, false
			if tc.compiledIn {
				wantRC, wantStarted = exitOK, true
			}
			captureLog(t, func() {
				if rc := runMain(t.Context()); rc != wantRC {
					t.Errorf("%s: rc = %d, want %d", tc.description, rc, wantRC)
				}
			})
			if started != wantStarted {
				t.Errorf("%s: daemon started = %v, want %v", tc.description, started, wantStarted)
			}
		})
	}
}

// TestVersionString_captureSuffix: a binary carrying the capture writer says
// so in its version line; every other binary does not.
func TestVersionString_captureSuffix(t *testing.T) {
	if got := strings.HasSuffix(versionString(), "+capture"); got != xtcp.CaptureCompiledIn {
		t.Errorf("version %q: +capture suffix = %v, want %v", versionString(), got, xtcp.CaptureCompiledIn)
	}
}
