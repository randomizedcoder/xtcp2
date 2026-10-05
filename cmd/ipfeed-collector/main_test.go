package main

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// setOrUnset sets key to val when set is true, otherwise ensures it is unset,
// registering cleanup so the surrounding environment is restored.
func setOrUnset(t *testing.T, key, val string, set bool) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, prev) //nolint:errcheck,gosec // test env restore; failure is not actionable
		} else {
			os.Unsetenv(key) //nolint:errcheck,gosec // test env restore; failure is not actionable
		}
	})
	if set {
		os.Setenv(key, val) //nolint:errcheck,gosec // test env setup; failure is not actionable
	} else {
		os.Unsetenv(key) //nolint:errcheck,gosec // test env setup; failure is not actionable
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(newDiscard(), &slog.HandlerOptions{Level: slog.LevelError}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func newDiscard() discardWriter                   { return discardWriter{} }

func TestEnvHelpers(t *testing.T) {
	t.Run("str", func(t *testing.T) {
		tests := []struct {
			name, desc, class string
			set               bool
			val, def, want    string
		}{
			{"positive_set", "positive: env value is used when present", "positive", true, "envv", "def", "envv"},
			{"boundary_unset", "boundary: default is used when unset", "boundary", false, "", "def", "def"},
			{"corner_empty_set", "corner: an explicitly empty env value overrides the default", "corner", true, "", "def", ""},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				const key = "IPFEED_TEST_STR"
				setOrUnset(t, key, tc.val, tc.set)
				if got := envStr(key, tc.def); got != tc.want {
					t.Errorf("%s: got %q, want %q", tc.desc, got, tc.want)
				}
			})
		}
	})

	t.Run("dur", func(t *testing.T) {
		tests := []struct {
			name, desc, class, val string
			set                    bool
			def, want              time.Duration
		}{
			{"positive_parse", "positive: a valid duration parses", "positive", "90m", true, time.Hour, 90 * time.Minute},
			{"negative_invalid", "negative: an invalid duration falls back to the default", "negative", "banana", true, time.Hour, time.Hour},
			{"boundary_unset", "boundary: default when unset", "boundary", "", false, 6 * time.Hour, 6 * time.Hour},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				const key = "IPFEED_TEST_DUR"
				setOrUnset(t, key, tc.val, tc.set)
				if got := envDur(key, tc.def); got != tc.want {
					t.Errorf("%s: got %v, want %v", tc.desc, got, tc.want)
				}
			})
		}
	})

	t.Run("int", func(t *testing.T) {
		tests := []struct {
			description string
			val         string // env value (ignored when set is false)
			set         bool
			def         int
			expected    int
		}{
			// positive
			{description: "positive: a valid positive integer parses", val: "42", set: true, def: 8, expected: 42},
			{description: "positive: a negative integer parses (validation is the caller's job)", val: "-3", set: true, def: 8, expected: -3},
			{description: "positive: an explicit leading plus sign parses", val: "+7", set: true, def: 8, expected: 7},
			// negative
			{description: "negative: a non-numeric value falls back to the default", val: "eight", set: true, def: 8, expected: 8},
			{description: "negative: a float falls back to the default (Atoi is integer-only)", val: "1.5", set: true, def: 8, expected: 8},
			{description: "negative: an empty value falls back to the default", val: "", set: true, def: 8, expected: 8},
			// boundary
			{description: "boundary: unset uses the default", set: false, def: 8, expected: 8},
			{description: "boundary: zero parses as zero, not the default", val: "0", set: true, def: 8, expected: 0},
			{description: "boundary: max int parses", val: strconv.Itoa(math.MaxInt), set: true, def: 8, expected: math.MaxInt},
			{description: "boundary: min int parses", val: strconv.Itoa(math.MinInt), set: true, def: 8, expected: math.MinInt},
			// corner
			{description: "corner: surrounding whitespace is not trimmed and falls back to the default", val: " 42 ", set: true, def: 8, expected: 8},
			{description: "corner: a value overflowing int falls back to the default", val: "99999999999999999999999", set: true, def: 8, expected: 8},
			{description: "corner: hex is not accepted and falls back to the default", val: "0x10", set: true, def: 8, expected: 8},
		}
		for _, tc := range tests {
			t.Run(tc.description, func(t *testing.T) {
				const key = "IPFEED_TEST_INT"
				setOrUnset(t, key, tc.val, tc.set)
				if got := envInt(key, tc.def); got != tc.expected {
					t.Errorf("envInt(%q=%q, def=%d) = %d, want %d", key, tc.val, tc.def, got, tc.expected)
				}
			})
		}
	})

	t.Run("bool", func(t *testing.T) {
		tests := []struct {
			description string
			val         string // env value (ignored when set is false)
			set         bool
			def         bool
			expected    bool
		}{
			// positive
			{description: "positive: 'true' parses to true", val: "true", set: true, def: false, expected: true},
			{description: "positive: 'false' parses to false over a true default", val: "false", set: true, def: true, expected: false},
			{description: "positive: '1' parses to true", val: "1", set: true, def: false, expected: true},
			{description: "positive: '0' parses to false", val: "0", set: true, def: true, expected: false},
			{description: "positive: 'TRUE' (upper case) parses to true", val: "TRUE", set: true, def: false, expected: true},
			{description: "positive: 't' short form parses to true", val: "t", set: true, def: false, expected: true},
			// negative
			{description: "negative: a non-bool value falls back to the default (false)", val: "notabool", set: true, def: false, expected: false},
			{description: "negative: a non-bool value falls back to the default (true)", val: "notabool", set: true, def: true, expected: true},
			{description: "negative: 'yes' is not a Go bool and falls back to the default", val: "yes", set: true, def: false, expected: false},
			// boundary
			{description: "boundary: unset uses the default (false)", set: false, def: false, expected: false},
			{description: "boundary: unset uses the default (true)", set: false, def: true, expected: true},
			// corner
			{description: "corner: an explicitly empty value falls back to the default", val: "", set: true, def: true, expected: true},
			{description: "corner: whitespace around the value is not trimmed and falls back to the default", val: " true", set: true, def: false, expected: false},
		}
		for _, tc := range tests {
			t.Run(tc.description, func(t *testing.T) {
				const key = "IPFEED_TEST_BOOL"
				setOrUnset(t, key, tc.val, tc.set)
				if got := envBool(key, tc.def); got != tc.expected {
					t.Errorf("envBool(%q=%q, def=%v) = %v, want %v", key, tc.val, tc.def, got, tc.expected)
				}
			})
		}
	})
}

// TestReadyzURL covers address normalization for the self-probe.
func TestReadyzURL(t *testing.T) {
	tests := []struct {
		name, desc, class, in, want string
	}{
		{"positive_hostport", "positive: an explicit host:port is preserved", "positive",
			"10.0.0.5:9000", "http://10.0.0.5:9000/readyz"},
		{"boundary_empty", "boundary: empty addr defaults to :8080 on loopback", "boundary",
			"", "http://127.0.0.1:8080/readyz"},
		{"corner_wildcard_host", "corner: a wildcard bind host maps to loopback for the probe", "corner",
			"0.0.0.0:8080", "http://127.0.0.1:8080/readyz"},
		{"corner_colon_port_only", "corner: a bare :port keeps the port on loopback", "corner",
			":8081", "http://127.0.0.1:8081/readyz"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := readyzURL(tc.in); got != tc.want {
				t.Errorf("%s: readyzURL(%q) = %q, want %q", tc.desc, tc.in, got, tc.want)
			}
		})
	}
}

// TestHealthcheck verifies the self-probe maps readiness states to nil/err.
func TestHealthcheck(t *testing.T) {
	tests := []struct {
		name, desc, class string
		status            int  // status the fake /readyz returns
		closed            bool // if true, hit a closed server (connection refused)
		wantErr           bool
	}{
		{"positive_ready", "positive: a 200 from /readyz means ready (nil error)", "positive",
			http.StatusOK, false, false},
		{"negative_not_ready", "negative: a 503 means not ready (error)", "negative",
			http.StatusServiceUnavailable, false, true},
		{"corner_conn_refused", "corner: a closed server (no daemon) is an error", "corner",
			0, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			url := srv.URL + "/readyz"
			if tc.closed {
				srv.Close() // dial target now refuses connections
			} else {
				defer srv.Close()
			}
			err := healthcheck(url, 2*time.Second)
			if tc.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", tc.desc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
		})
	}
}

// TestParseFlagsDaemonValidation covers the daemon/interval validation.
func TestParseFlagsDaemonValidation(t *testing.T) {
	tests := []struct {
		name    string
		desc    string
		class   string
		args    []string
		wantErr bool
	}{
		{"positive_singleshot", "positive: no flags is a valid single-shot config", "positive",
			[]string{}, false},
		{"positive_daemon", "positive: daemon with a valid interval", "positive",
			[]string{"-daemon", "-interval", "6h"}, false},
		{"positive_daemon_jitter", "positive: daemon with explicit startup and interval jitter", "positive",
			[]string{"-daemon", "-interval", "6h", "-startup-jitter", "5m", "-interval-jitter-pct", "20"}, false},
		{"negative_daemon_zero", "negative: daemon with a zero interval is rejected", "negative",
			[]string{"-daemon", "-interval", "0"}, true},
		{"negative_startup_jitter_negative", "negative: startup jitter cannot be negative", "negative",
			[]string{"-startup-jitter", "-1s"}, true},
		{"negative_interval_jitter_negative", "negative: interval jitter percent cannot be negative", "negative",
			[]string{"-interval-jitter-pct", "-1"}, true},
		{"negative_interval_jitter_above_100", "negative: interval jitter percent cannot exceed 100", "negative",
			[]string{"-interval-jitter-pct", "101"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFlags(tc.args)
			if tc.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", tc.desc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
		})
	}
}

// TestParseFlagsOutFile covers the -out-file flag and its IPFEED_OUT_FILE env
// fallback, plus flag-over-env precedence.
func TestParseFlagsOutFile(t *testing.T) {
	tests := []struct {
		name, desc, class string
		env               string // IPFEED_OUT_FILE (unset if envSet is false)
		envSet            bool
		args              []string
		want              string
	}{
		{"positive_flag", "positive: -out-file sets the exact path", "positive",
			"", false, []string{"-out-file", "/data/feeds.parquet"}, "/data/feeds.parquet"},
		{"positive_env", "positive: IPFEED_OUT_FILE is used when the flag is absent", "positive",
			"/env/feeds.parquet", true, []string{}, "/env/feeds.parquet"},
		{"corner_flag_over_env", "corner: the flag wins over the env var", "corner",
			"/env/feeds.parquet", true, []string{"-out-file", "/flag/feeds.parquet"}, "/flag/feeds.parquet"},
		{"boundary_unset", "boundary: empty by default (dir+timestamp behavior)", "boundary",
			"", false, []string{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setOrUnset(t, "IPFEED_OUT_FILE", tc.env, tc.envSet)
			f, err := parseFlags(tc.args)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
			if f.outFile != tc.want {
				t.Errorf("%s: outFile = %q, want %q", tc.desc, f.outFile, tc.want)
			}
		})
	}
}

// TestParseFlagsS3Creds covers credential/region resolution across the
// IPFEED_S3_* and standard AWS_* env names, including their precedence.
func TestParseFlagsS3Creds(t *testing.T) {
	// field selects which resolved value the case asserts on.
	const (
		access = "access"
		secret = "secret"
		region = "region"
	)
	tests := []struct {
		name, desc, class string
		field             string
		ipfeed            string // IPFEED_S3_* value ("" = unset)
		aws               string // AWS_* value ("" = unset)
		args              []string
		want              string
	}{
		{"positive_aws_access", "positive: AWS_ACCESS_KEY_ID is used as a fallback", "positive",
			access, "", "AKIA_AWS", nil, "AKIA_AWS"},
		{"corner_ipfeed_over_aws", "corner: IPFEED_S3_ACCESS_KEY wins over AWS_ACCESS_KEY_ID", "corner",
			access, "AKIA_IPFEED", "AKIA_AWS", nil, "AKIA_IPFEED"},
		{"corner_flag_over_all", "corner: the flag wins over both env names", "corner",
			access, "AKIA_IPFEED", "AKIA_AWS", []string{"-s3-access-key", "AKIA_FLAG"}, "AKIA_FLAG"},
		{"positive_aws_secret", "positive: AWS_SECRET_ACCESS_KEY is used as a fallback", "positive",
			secret, "", "sekret_aws", nil, "sekret_aws"},
		{"positive_aws_region", "positive: AWS_REGION is used as a fallback", "positive",
			region, "", "eu-west-1", nil, "eu-west-1"},
		{"boundary_region_default", "boundary: region defaults to us-east-1 when nothing is set", "boundary",
			region, "", "", nil, "us-east-1"},
		{"corner_ipfeed_region_over_aws", "corner: IPFEED_S3_REGION wins over AWS_REGION", "corner",
			region, "ap-south-1", "eu-west-1", nil, "ap-south-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ipfeedKey, awsKey string
			switch tc.field {
			case access:
				ipfeedKey, awsKey = "IPFEED_S3_ACCESS_KEY", "AWS_ACCESS_KEY_ID"
			case secret:
				ipfeedKey, awsKey = "IPFEED_S3_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"
			case region:
				ipfeedKey, awsKey = "IPFEED_S3_REGION", "AWS_REGION"
			}
			setOrUnset(t, ipfeedKey, tc.ipfeed, tc.ipfeed != "")
			setOrUnset(t, awsKey, tc.aws, tc.aws != "")

			f, err := parseFlags(tc.args)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
			var got string
			switch tc.field {
			case access:
				got = f.s3.AccessKey
			case secret:
				got = f.s3.SecretKey
			case region:
				got = f.s3.Region
			}
			if got != tc.want {
				t.Errorf("%s: %s = %q, want %q", tc.desc, tc.field, got, tc.want)
			}
		})
	}
}

// TestRunDaemon verifies the loop runs the immediate cycle plus ticks, marks
// ready only on success, and stops on context cancellation.
func TestRunDaemon(t *testing.T) {
	tests := []struct {
		name      string
		desc      string
		class     string
		stopAfter int // cancel ctx once collect has been called this many times
		failEvery int // return an error on calls where call%failEvery==0 (0 = never)
		wantCalls int
		wantReady bool
	}{
		{"positive_runs_n_cycles", "positive: immediate cycle + ticks until canceled", "positive",
			3, 0, 3, true},
		{"boundary_single_cycle", "boundary: cancel after the first (immediate) cycle", "boundary",
			1, 0, 1, true},
		{"negative_all_fail_not_ready", "negative: if every cycle fails, ready is never set", "negative",
			2, 1, 2, false},
		{"corner_intermittent_failure", "corner: a failing cycle does not stop the loop; a later success sets ready", "corner",
			3, 3, 3, true}, // call 3 fails, but calls 1-2 succeeded -> ready
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var calls atomic.Int64
			var ready atomic.Bool
			collect := func(context.Context) error {
				n := int(calls.Add(1))
				var err error
				if tc.failEvery > 0 && n%tc.failEvery == 0 {
					err = context.DeadlineExceeded // any non-nil error
				}
				if n >= tc.stopAfter {
					cancel()
				}
				return err
			}
			readyFn := func() { ready.Store(true) }

			// Tiny interval so ticks fire quickly; correctness does not depend
			// on timing because collect cancels the context deterministically.
			err := runDaemon(ctx, daemonSchedule{Interval: time.Millisecond}, readyFn, collect, discardLogger())
			if err != nil {
				t.Fatalf("%s: runDaemon returned error: %v", tc.desc, err)
			}
			if got := int(calls.Load()); got != tc.wantCalls {
				t.Errorf("%s: calls = %d, want %d", tc.desc, got, tc.wantCalls)
			}
			if ready.Load() != tc.wantReady {
				t.Errorf("%s: ready = %v, want %v", tc.desc, ready.Load(), tc.wantReady)
			}
		})
	}
}

func TestDaemonJitter(t *testing.T) {
	tests := []struct {
		description string
		interval    time.Duration
		pct         int
		jitter      time.Duration
		want        time.Duration
	}{
		{"positive: 20 percent jitter shifts around the same mean interval", 100 * time.Second, 20, 7 * time.Second, 97 * time.Second},
		{"boundary: zero percent keeps the interval unchanged", 100 * time.Second, 0, 7 * time.Second, 100 * time.Second},
		{"boundary: zero interval stays zero", 0, 20, 7 * time.Second, 0},
		{"corner: percent above 100 is clamped", 100 * time.Second, 150, 25 * time.Second, 75 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := jitteredInterval(tc.interval, tc.pct, func(time.Duration) time.Duration { return tc.jitter })
			if got != tc.want {
				t.Fatalf("jitteredInterval(%s,%d) = %s, want %s", tc.interval, tc.pct, got, tc.want)
			}
		})
	}
}

func TestRunDaemonStartupJitter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var slept atomic.Int64
	var calls atomic.Int64
	schedule := daemonSchedule{
		Interval:         time.Hour,
		StartupJitterMax: 5 * time.Minute,
		Jitter:           func(max time.Duration) time.Duration { return max / 2 },
		Sleep: func(_ context.Context, d time.Duration) bool {
			slept.Store(int64(d))
			return true
		},
	}
	collect := func(context.Context) error {
		calls.Add(1)
		cancel()
		return nil
	}
	if err := runDaemon(ctx, schedule, nil, collect, discardLogger()); err != nil {
		t.Fatalf("runDaemon: %v", err)
	}
	if got, want := time.Duration(slept.Load()), 150*time.Second; got != want {
		t.Fatalf("startup jitter sleep = %s, want %s", got, want)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("collect calls = %d, want 1", got)
	}
}
