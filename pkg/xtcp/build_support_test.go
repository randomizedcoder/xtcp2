package xtcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// TestCheckBuildSupport covers the build-tag-gated debug writers. The rows
// whose outcome depends on the tags compiled into the test binary derive
// their expectation from CaptureCompiledIn and the destination registry, so
// the same table is correct under every flavor leg.
func TestCheckBuildSupport(t *testing.T) {
	t.Parallel()
	_, fileStatus := lookupDestinationFactory(schemeFile)
	fileIn := fileStatus == destLookupFound
	cases := []struct {
		name        string
		category    string
		description string
		cfg         *xtcp_config.XtcpConfig
		expected    bool // true = refused with ErrNotCompiledIn
		wantHint    string
	}{
		{"writefiles_zero", "positive", "capture idle is always fine",
			&xtcp_config.XtcpConfig{Dest: "null", WriteFiles: 0}, false, ""},
		{"stderr_dest", "positive", "stdlib destination needs no tag",
			&xtcp_config.XtcpConfig{Dest: "stderr"}, false, ""},
		{"writefiles_armed", "negative", "capture armed needs -tags capture",
			&xtcp_config.XtcpConfig{Dest: "null", WriteFiles: 5}, !CaptureCompiledIn, "-tags capture"},
		{"file_dest", "negative", "file: needs -tags dest_file",
			&xtcp_config.XtcpConfig{Dest: "file:/x"}, !fileIn, "-tags dest_file"},
		{"writefiles_one", "boundary", "smallest armed budget is still refused",
			&xtcp_config.XtcpConfig{Dest: "null", WriteFiles: 1}, !CaptureCompiledIn, "-tags capture"},
		{"unknown_scheme", "corner", "unknown scheme is left to validateInput",
			&xtcp_config.XtcpConfig{Dest: "bogus:x"}, false, ""},
		{"nil_config", "corner", "nil config asks for nothing",
			nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			err := CheckBuildSupport(tc.cfg)
			if got := errors.Is(err, ErrNotCompiledIn); got != tc.expected {
				t.Fatalf("%s: refused = %v, want %v (err: %v)", tc.description, got, tc.expected, err)
			}
			if err == nil && tc.expected {
				t.Fatalf("%s: want an error", tc.description)
			}
			if err != nil && !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("%s: error %q lacks the rebuild hint %q", tc.description, err, tc.wantHint)
			}
		})
	}
}

// TestConfigService_Set_debugWritersNotCompiledIn: Set re-execs the same
// binary, so it refuses a config needing a debug writer this build lacks and
// leaves the running daemon alone. With the tag compiled in, Set gets past
// the guard (and stops at the missing reconfigure hook: Unavailable).
func TestConfigService_Set_debugWritersNotCompiledIn(t *testing.T) {
	_, fileStatus := lookupDestinationFactory(schemeFile)
	cases := []struct {
		name        string
		category    string
		description string
		mutate      func(*xtcp_config.XtcpConfig)
		compiledIn  bool
	}{
		{"set_write_files", "negative", "write_files=1 needs the capture tag",
			func(c *xtcp_config.XtcpConfig) { c.WriteFiles = 1 }, CaptureCompiledIn},
		{"set_file_dest", "negative", "dest file: needs the dest_file tag",
			func(c *xtcp_config.XtcpConfig) { c.Dest = "file:/tmp/x"; c.MarshalTo = "jsonl" },
			fileStatus == destLookupFound},
		{"set_write_files_zero", "boundary", "write_files=0 passes the guard on every build",
			func(c *xtcp_config.XtcpConfig) { c.WriteFiles = 0 }, true},
		{"set_stdlib_dest", "positive", "a stdlib destination passes the guard",
			func(c *xtcp_config.XtcpConfig) { c.Dest = "stderr" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			c, _ := newConfigServiceFixture(t)
			cfg := validSetConfig()
			tc.mutate(cfg)
			_, err := c.Set(context.Background(), &xtcp_config.SetRequest{Config: cfg})
			want := codes.FailedPrecondition
			if tc.compiledIn {
				want = codes.Unavailable
			}
			if got := status.Code(err); got != want {
				t.Errorf("%s: Set code = %v, want %v (err: %v)", tc.description, got, want, err)
			}
		})
	}
}
