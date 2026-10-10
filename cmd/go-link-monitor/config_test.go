package main

import (
	"bytes"
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
			opts, err := parseOptions(tc.args, &out, func(string) string { return tc.env })
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
		"-stats-interval=2s", "-stats-include=rx", "-stats-exclude=queue", "--collector.netstat.fields=^Tcp_", "-io-backend=io_uring"}
	var output bytes.Buffer
	opts, err := parseOptions(args, &output, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	want := linkmonitor.DefaultConfig()
	want.BaselineFile, want.Settle, want.Resync, want.StatsInterval = "/tmp/example", 0, 2*time.Minute, 2*time.Second
	want.StatsInclude, want.StatsExclude, want.NetstatFields, want.IOBackend = "rx", "queue", "^Tcp_", linkmonitor.IOBackendIOUring
	if opts.address != "127.0.0.1:0" || !reflect.DeepEqual(opts.monitor, want) {
		t.Fatal(opts)
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
			code := command(tc.args, &out, &diagnostic, func(string) string { return "invalid," })
			if code != tc.code || !strings.Contains(out.String()+diagnostic.String(), tc.contains) {
				t.Fatal(code, out.String(), diagnostic.String())
			}
		})
	}
}
