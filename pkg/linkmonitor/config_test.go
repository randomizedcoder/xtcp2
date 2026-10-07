package linkmonitor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConfiguration(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		change                                func(*Config)
		wantError                             bool
	}{
		{"defaults", "positive", "all default fields are valid", "construct an idle monitor", func(*Config) {}, false},
		{"immediate", "boundary", "zero settle is intentional", "retain zero settle", func(c *Config) { c.Settle = 0 }, false},
		{"empty filters", "boundary", "empty regexps are valid and match everything", "retain empty regexps", func(c *Config) { c.StatsInclude, c.StatsExclude, c.NetstatFields = "", "", "" }, false},
		{"ring", "positive", "explicit ring selection", "validate without probing kernel support", func(c *Config) { c.IOBackend = IOBackendIOUring }, false},
		{"negative settle", "negative", "negative settling time", "reject config", func(c *Config) { c.Settle = -1 }, true},
		{"zero resync", "negative", "zero resync interval", "reject config", func(c *Config) { c.Resync = 0 }, true},
		{"negative resync", "negative", "negative resync interval", "reject config", func(c *Config) { c.Resync = -1 }, true},
		{"zero stats", "negative", "zero collection interval", "reject config", func(c *Config) { c.StatsInterval = 0 }, true},
		{"negative stats", "negative", "negative collection interval", "reject config", func(c *Config) { c.StatsInterval = -1 }, true},
		{"invalid include", "negative", "malformed include regexp", "reject config", func(c *Config) { c.StatsInclude = "[" }, true},
		{"invalid exclude", "negative", "malformed exclude regexp", "reject config", func(c *Config) { c.StatsExclude = "[" }, true},
		{"invalid netstat", "negative", "malformed netstat regexp", "reject config", func(c *Config) { c.NetstatFields = "[" }, true},
		{"missing backend", "negative", "zero backend has no implicit default", "reject config", func(c *Config) { c.IOBackend = "" }, true},
		{"unknown backend", "negative", "unknown backend must not fall back", "reject config", func(c *Config) { c.IOBackend = "auto" }, true},
		{"empty path", "negative", "missing baseline filename", "reject config", func(c *Config) { c.BaselineFile = "" }, true},
		{"NUL path", "negative", "NUL in baseline filename", "reject config", func(c *Config) { c.BaselineFile = "a\x00b" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg := DefaultConfig()
			tc.change(&cfg)
			m, err := New(cfg, Options{})
			if (err != nil) != tc.wantError {
				t.Fatalf("New error = %v, want error %v", err, tc.wantError)
			}
			if tc.wantError {
				if m != nil {
					t.Fatal("invalid configuration returned a monitor")
				}
				return
			}
			if m.Health() != (Health{}) || m.Snapshot().Version() != 0 {
				t.Fatal("construction published running state")
			}
			if m.cfg.Settle != cfg.Settle || m.cfg.NetstatFields != cfg.NetstatFields {
				t.Fatal("explicit values changed")
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	want := Config{
		BaselineFile: "/var/lib/go-link-monitor/baseline.json",
		Settle:       30 * time.Second, Resync: time.Hour, StatsInterval: 15 * time.Second,
		StatsInclude: ".*", StatsExclude: "^$", NetstatFields: ".*", IOBackend: IOBackendPoller,
	}
	if got := DefaultConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults = %#v; want %#v", got, want)
	}
}

func TestExceptionSyntax(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		selectors, want                       []string
		wantError                             bool
	}{
		{"none", "positive", "no exceptions configured", "empty list", nil, []string{}, false},
		{"normalize", "corner", "trim and deduplicate without modifying input", "ordered unique selectors", []string{" eno1 ", "ib0", "eno1", "rdma:mlx5_0:1"}, []string{"eno1", "ib0", "rdma:mlx5_0:1"}, false},
		{"max interface", "boundary", "15-byte interface name", "accept", []string{strings.Repeat("a", 15)}, []string{strings.Repeat("a", 15)}, false},
		{"long interface", "boundary", "16-byte interface name", "reject", []string{strings.Repeat("a", 16)}, nil, true},
		{"empty element", "negative", "empty member within list", "reject", []string{"eno1", " "}, nil, true},
		{"glob", "negative", "wildcard is not an exact name", "reject", []string{"eth*"}, nil, true},
		{"regexp", "negative", "regexp is not an exact name", "reject", []string{"eth[0-9]"}, nil, true},
		{"space", "negative", "embedded whitespace", "reject", []string{"eth 0"}, nil, true},
		{"path", "negative", "sysfs path is not a name", "reject", []string{"../eth0"}, nil, true},
		{"NUL", "negative", "embedded NUL", "reject", []string{"eth\x000"}, nil, true},
		{"missing port", "negative", "RDMA selector without port", "reject", []string{"rdma:mlx5_0"}, nil, true},
		{"port zero", "boundary", "physical port numbering starts at one", "reject", []string{"rdma:mlx5_0:0"}, nil, true},
		{"negative port", "negative", "negative RDMA port", "reject", []string{"rdma:mlx5_0:-1"}, nil, true},
		{"noncanonical port", "corner", "leading-zero port aliases a selector", "reject", []string{"rdma:mlx5_0:01"}, nil, true},
		{"port overflow", "boundary", "port exceeds uint32", "reject", []string{"rdma:mlx5_0:4294967296"}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg := DefaultConfig()
			cfg.MaxSpeedExceptions = tc.selectors
			m, err := New(cfg, Options{})
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v", err, tc.wantError)
			}
			if err == nil && !reflect.DeepEqual(m.cfg.MaxSpeedExceptions, tc.want) {
				t.Fatalf("selectors = %v, want %v", m.cfg.MaxSpeedExceptions, tc.want)
			}
		})
	}
}

func TestConfigOwnershipAndNoIO(t *testing.T) {
	// A regular file where the parent directory would be makes baseline I/O
	// impossible. Construction still succeeds and leaves that file unchanged.
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.BaselineFile = filepath.Join(parent, "baseline.json")
	cfg.MaxSpeedExceptions = []string{"eno1"}
	m, err := New(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxSpeedExceptions[0] = "eno2"
	if m.cfg.MaxSpeedExceptions[0] != "eno1" {
		t.Fatal("configuration aliases caller storage")
	}
	data, err := os.ReadFile(parent)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("constructor touched baseline parent: %q, %v", data, err)
	}
}

func TestFilterSemantics(t *testing.T) {
	tests := []struct {
		name, description, expected, expression, field string
		want                                           bool
	}{
		{"all", "all default host fields", "match", ".*", "TcpExt_FutureCounter", true},
		{"empty", "empty regexp matches all", "match", "", "Tcp_MaxConn", true},
		{"none", "empty-string anchor on nonempty field", "omit", "^$", "Tcp_MaxConn", false},
		{"unanchored", "unanchored substring", "match", "Retrans", "Tcp_RetransSegs", true},
		{"anchored", "whole protocol prefix", "omit", "^Tcp_", "TcpExt_TCPTimeouts", false},
		{"case sensitive", "lowercase differs", "omit", "^tcp_", "Tcp_MaxConn", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			cfg := DefaultConfig()
			cfg.StatsInclude, cfg.StatsExclude, cfg.NetstatFields = tc.expression, tc.expression, tc.expression
			validated, err := validateConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if validated.netstat.MatchString(tc.field) != tc.want || validated.include.MatchString(tc.field) != tc.want || validated.exclude.MatchString(tc.field) != tc.want {
				t.Fatal("filter semantics differ")
			}
		})
	}
}
