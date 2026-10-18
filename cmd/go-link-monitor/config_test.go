package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func TestOptions(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          string
		want                                         []string
	}{
		{"defaults", "positive", "no flags or environment", "all fields selected and no exemptions", nil, "", nil},
		{"environment", "positive", "environment selectors", "environment is used", nil, "eno1,rdma:mlx5_0:1", []string{"eno1", "rdma:mlx5_0:1"}},
		{"override", "corner", "flag replaces invalid environment", "only explicit selectors used", []string{"--max-speed-exceptions=eno2"}, "invalid,", []string{"eno2"}},
		{"clear", "boundary", "explicit empty flag", "environment is cleared", []string{"-max-speed-exceptions="}, "eno1", nil},
		{"normalize", "positive", "whitespace and repeated selector", "valid list accepted by library", []string{"-max-speed-exceptions= eno1 ,eno1"}, "", []string{" eno1 ", "eno1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(map[string]string{exceptionEnvironment: tc.env}))
			if err != nil || !reflect.DeepEqual(opts.monitor.MaxSpeedExceptions, tc.want) {
				t.Fatal(opts, err)
			}
			if _, err := linkmonitor.New(opts.monitor, linkmonitor.Options{}); err != nil {
				t.Fatal(err)
			}
			if opts.address != ":9101" || opts.monitor.StatsInclude != ".*" || opts.monitor.StatsExclude != "^$" || opts.monitor.NetstatFields != ".*" {
				t.Fatal("default selection changed", opts)
			}
		})
	}
}

func TestOptionsMapping(t *testing.T) {
	t.Log("positive/boundary: every configurable field; expected: exact values including zero settle")
	args := []string{"-metrics-addr=127.0.0.1:0", "-baseline-file=/tmp/example", "-settle=0", "-resync=2m",
		"-stats-interval=2s", "-stats-include=rx", "-stats-exclude=queue", "--collector.netstat.fields=^Tcp_", "-io-backend=io_uring",
		"-errors.window=0", "-flap-window=2s", "-errors.classes=", "-errors.exclude=.*@eno1"}
	var output bytes.Buffer
	opts, err := parseOptions(args, &output, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	want := linkmonitor.DefaultConfig()
	want.BaselineFile, want.Settle, want.Resync, want.StatsInterval = "/tmp/example", 0, 2*time.Minute, 2*time.Second
	want.StatsInclude, want.StatsExclude, want.NetstatFields, want.IOBackend = "rx", "queue", "^Tcp_", linkmonitor.IOBackendIOUring
	want.ErrorWindow, want.FlapWindow, want.ErrorClasses, want.ErrorExclude = 0, 2*time.Second, []string{}, ".*@eno1"
	if opts.address != "127.0.0.1:0" || !reflect.DeepEqual(opts.monitor, want) {
		t.Fatal(opts)
	}
}

func TestProblemOptionsEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		errorWindow, flapWindow                      time.Duration
		classes                                      []string
		exclude                                      string
	}{
		{"defaults", "positive", "no flags or environment", "180s windows, link and rdma", nil, nil, 180 * time.Second, 180 * time.Second, []string{"link", "rdma"}, ""},
		{"environment", "positive", "every problem variable", "environment values used", nil, map[string]string{
			errorWindowEnvironment: "5m", flapWindowEnvironment: "0", errorClassEnvironment: "link,drop", errorExcludeEnviron: "x"},
			5 * time.Minute, 0, []string{"link", "drop"}, "x"},
		{"flag wins", "corner", "flags and variables both set", "flag values", []string{"-errors.window=1m", "-errors.classes=rdma"}, map[string]string{
			errorWindowEnvironment: "5m", errorClassEnvironment: "link"}, time.Minute, 180 * time.Second, []string{"rdma"}, ""},
		{"empty classes", "boundary", "explicit empty classes flag", "no error classes", []string{"-errors.classes="}, map[string]string{errorClassEnvironment: "link"},
			180 * time.Second, 180 * time.Second, []string{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			m := opts.monitor
			if m.ErrorWindow != tc.errorWindow || m.FlapWindow != tc.flapWindow || !reflect.DeepEqual(m.ErrorClasses, tc.classes) || m.ErrorExclude != tc.exclude {
				t.Fatalf("problem options = %v %v %q %q", m.ErrorWindow, m.FlapWindow, m.ErrorClasses, m.ErrorExclude)
			}
			if _, err := linkmonitor.New(m, linkmonitor.Options{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		code                                         int
		contains                                     string
	}{
		{"help", "positive", "help with invalid environment", "no runtime acquisition", []string{"-help"}, 0, "-stats-include"},
		{"version", "positive", "version with invalid configuration", "identity without runtime acquisition", []string{"-version", "-baseline-file="}, 0, "revision="},
		{"unknown", "negative", "unknown option", "usage failure", []string{"-bogus"}, 2, "invalid command"},
		{"positional", "negative", "trailing argument", "usage failure", []string{"foo"}, 2, "positional"},
		{"duration", "negative", "unparseable duration", "usage failure", []string{"-resync=never"}, 2, "invalid command"},
		{"negative", "boundary", "negative settle", "validation failure", []string{"-settle=-1s"}, 2, "nonnegative"},
		{"zero", "boundary", "zero statistics interval", "validation failure", []string{"-stats-interval=0"}, 2, "positive"},
		{"overflow", "boundary", "freshness arithmetic overflow", "validation failure", []string{"-resync=2562047h"}, 2, "representable"},
		{"regexp", "negative", "malformed filter", "validation failure", []string{"-stats-include=[", "-max-speed-exceptions="}, 2, "regexp"},
		{"empty element", "corner", "empty selector in nonempty list", "validation failure", []string{"-max-speed-exceptions=eno1,"}, 2, "exception"},
		{"environment", "negative", "invalid environment selector", "validation failure", nil, 2, "exception"},
		{"backend", "negative", "unknown backend", "validation failure", []string{"-io-backend=other"}, 2, "backend"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out, diagnostic bytes.Buffer
			code := command(tc.args, &out, &diagnostic, environment(map[string]string{exceptionEnvironment: "invalid,"}))
			if code != tc.code || !strings.Contains(out.String()+diagnostic.String(), tc.contains) {
				t.Fatal(code, out.String(), diagnostic.String())
			}
		})
	}
}

// TestRDMAUMADBackendOptions is the umad half of the design's backend
// selection table for the command.
func TestRDMAUMADBackendOptions(t *testing.T) {
	core := !linkmonitor.RDMALibibumadAvailable
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		want                                         linkmonitor.RDMAUMADBackend
		wantNewErr                                   bool
	}{
		{"default", "positive", "no flag or environment", "auto", nil, nil, linkmonitor.RDMAUMADAuto, false},
		{"environment", "positive", "environment says native", "native", nil,
			map[string]string{rdmaUMADEnvironment: "native"}, linkmonitor.RDMAUMADNative, false},
		{"env_and_flag", "boundary", "environment says native, flag says libibumad", "flag wins; libibumad is refused by New in a core build",
			[]string{"-rdma.umad-backend=libibumad"}, map[string]string{rdmaUMADEnvironment: "native"}, linkmonitor.RDMAUMADLibibumad, core},
		{"empty_flag", "boundary", "explicit empty flag with the environment set", "empty, which New treats as auto",
			[]string{"-rdma.umad-backend="}, map[string]string{rdmaUMADEnvironment: "native"}, "", false},
		{"unknown_value", "corner", "-rdma.umad-backend=foo", "parsed, refused by New naming the valid values",
			[]string{"-rdma.umad-backend=foo"}, nil, "foo", true},
		{"events_value", "corner", "an events backend name given to the umad flag", "refused: the two flags have separate names",
			[]string{"-rdma.umad-backend=netlink"}, nil, "netlink", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if opts.monitor.RDMAUMADBackend != tc.want {
				t.Fatalf("backend %q, want %q", opts.monitor.RDMAUMADBackend, tc.want)
			}
			_, err = linkmonitor.New(opts.monitor, linkmonitor.Options{})
			if (err != nil) != tc.wantNewErr || (err != nil && !errors.Is(err, linkmonitor.ErrRDMAUMADBackend)) {
				t.Fatalf("New: %v, want error %v", err, tc.wantNewErr)
			}
			if tc.name == "unknown_value" && !strings.Contains(err.Error(), "auto, libibumad or native") {
				t.Fatalf("error does not name the valid values: %v", err)
			}
		})
	}
	var out, diagnostic bytes.Buffer
	if code := command([]string{"-rdma.umad-backend=foo"}, &out, &diagnostic, environment(nil)); code != 2 ||
		!strings.Contains(diagnostic.String()+out.String(), "RDMA umad backend") {
		t.Fatalf("command: %d %s %s", code, out.String(), diagnostic.String())
	}
}

// TestRDMAEventsBackendOptions is the design's backend selection table for the
// command: flag and environment parsing, then New's validation, which is
// where a backend the build lacks is refused.
func TestRDMAEventsBackendOptions(t *testing.T) {
	core := !linkmonitor.RDMAVerbsAvailable
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		want                                         linkmonitor.RDMAEventsBackend
		wantNewErr                                   bool
	}{
		{"default", "positive", "no flag or environment", "auto", nil, nil, linkmonitor.RDMAEventsAuto, false},
		{"environment", "positive", "environment says netlink", "netlink", nil,
			map[string]string{rdmaEventsEnvironment: "netlink"}, linkmonitor.RDMAEventsNetlink, false},
		{"env_and_flag", "boundary", "environment says netlink, flag says verbs", "flag wins; verbs is refused by New in a core build",
			[]string{"-rdma.events-backend=verbs"}, map[string]string{rdmaEventsEnvironment: "netlink"}, linkmonitor.RDMAEventsVerbs, core},
		{"empty_flag", "boundary", "explicit empty flag with the environment set", "empty, which New treats as auto",
			[]string{"-rdma.events-backend="}, map[string]string{rdmaEventsEnvironment: "netlink"}, "", false},
		{"unknown_value", "corner", "-rdma.events-backend=foo", "parsed, refused by New naming the valid values",
			[]string{"-rdma.events-backend=foo"}, nil, "foo", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(tc.env))
			if err != nil {
				t.Fatal(err)
			}
			if opts.monitor.RDMAEventsBackend != tc.want {
				t.Fatalf("backend %q, want %q", opts.monitor.RDMAEventsBackend, tc.want)
			}
			_, err = linkmonitor.New(opts.monitor, linkmonitor.Options{})
			if (err != nil) != tc.wantNewErr || (err != nil && !errors.Is(err, linkmonitor.ErrRDMAEventsBackend)) {
				t.Fatalf("New: %v, want error %v", err, tc.wantNewErr)
			}
			if tc.name == "unknown_value" && !strings.Contains(err.Error(), "auto, verbs or netlink") {
				t.Fatalf("error does not name the valid values: %v", err)
			}
		})
	}
	var out, diagnostic bytes.Buffer
	if code := command([]string{"-rdma.events-backend=foo"}, &out, &diagnostic, environment(nil)); code != 2 ||
		!strings.Contains(diagnostic.String()+out.String(), "RDMA events backend") {
		t.Fatalf("command: %d %s %s", code, out.String(), diagnostic.String())
	}
}
