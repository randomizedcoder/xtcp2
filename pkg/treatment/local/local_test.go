package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigContracts(t *testing.T) {
	for _, tc := range []struct{ name, description, input, wantError string }{
		{"defaults", "positive: minimal config receives safe defaults", `{"agent_id":"host","location":"dc","controller":"localhost:9000"}`, ""},
		{"explicit", "positive: explicit private route settings are preserved", `{"agent_id":"host","location":"dc","controller":"localhost:9000","table":10003,"rule_priority":30001,"protocol":243}`, ""},
		{"unknown", "negative: typos cannot silently disable settings", `{"agent_id":"host","location":"dc","controller":"localhost:9000","enable_aply":true}`, "unknown field"},
		{"trailing", "negative: two JSON documents are rejected", `{} {}`, "trailing"},
		{"missing", "negative: enrollment fields are required", `{}`, "required"},
		{"bad-id", "negative: path-like agent identity is rejected", `{"agent_id":"../host","location":"dc","controller":"localhost:9000"}`, "required"},
		{"malformed", "corner: truncated JSON is rejected", `{`, "EOF"},
		{"oversized", "boundary: oversized config cannot bypass the limit", `{"agent_id":"host","location":"dc","controller":"` + strings.Repeat("x", 65537) + `"}`, "EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.json")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := LoadConfig(path)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: expected %q, got %v", tc.description, tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantTable, wantPriority, wantProtocol := uint32(10002), uint32(30000), uint8(242)
			if tc.name == "explicit" {
				wantTable++
				wantPriority++
				wantProtocol++
			}
			if c.Table != wantTable || c.RulePriority != wantPriority || c.Protocol != wantProtocol || c.EnableApply || c.Directory != DefaultDirectory {
				t.Fatalf("%s: unexpected config %+v", tc.description, c)
			}
			if !strings.Contains(c.String(), "agent=host") {
				t.Fatal("config diagnostic missing identity")
			}
		})
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestNewHostContracts(t *testing.T) {
	testIdentity := &Identity{BootID: "boot", NetnsInode: 123}
	for _, tc := range []struct {
		name, description string
		change            func(*Config)
		wantError         string
	}{
		{"offline", "positive: offline construction needs no TLS, DNS or controller", func(*Config) {}, ""},
		{"identity", "negative: invalid agent identity fails before initialization", func(c *Config) { c.AgentID = "../host" }, "identity"},
		{"location", "negative: invalid location fails before initialization", func(c *Config) { c.Location = "" }, "identity"},
		{"bad-prefix", "negative: malformed protected prefixes fail closed", func(c *Config) { c.ProtectedPrefixes = []string{"bad"} }, "ParsePrefix"},
		{"no-exclusions", "negative: application requires management exclusions", func(c *Config) { c.EnableApply = true }, "protected_prefixes"},
		{"protected", "positive: explicit exclusions allow backend construction without writes", func(c *Config) { c.EnableApply = true; c.ProtectedPrefixes = []string{"192.0.2.0/24"} }, ""},
		{"defaults", "boundary: a config built in code without the loader still gets the table defaults", func(c *Config) { c.Table, c.RulePriority, c.Protocol = 0, 0, 0 }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{AgentID: "host", Location: "dc", Directory: t.TempDir(), Table: 10002, RulePriority: 30000, Protocol: 242}
			tc.change(&c)
			h, err := New(c, testIdentity)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: got %v want %q", tc.description, err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if h.Identity != *testIdentity || h.Reconciler.BootID != "boot" || h.Reconciler.AgentID != "host" || h.Reconciler.Location != "dc" || h.Reconciler.Journal.Dir != c.Directory {
				t.Fatalf("%s: reconciler not wired from config: %+v", tc.description, h.Reconciler)
			}
			if h.Config.Table != 10002 || h.Config.RulePriority != 30000 || h.Config.Protocol != 242 {
				t.Fatalf("%s: defaults not applied: %+v", tc.description, h.Config)
			}
			before := len(h.backend.Protected)
			h.Protect(mustPrefix("198.51.100.1/32"))
			if len(h.backend.Protected) != before+1 {
				t.Fatal("Protect did not extend the exclusions")
			}
		})
	}
}

// Plan validates the domain treatment before goip sees it, so an invalid
// treatment never reaches the kernel planner.
func TestRouteBackendPlanValidates(t *testing.T) {
	h, err := New(Config{AgentID: "host", Location: "dc", Directory: t.TempDir()}, &Identity{BootID: "boot", NetnsInode: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.backend.Plan(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "treatment requires") {
		t.Fatalf("nil treatment planned: %v", err)
	}
}
