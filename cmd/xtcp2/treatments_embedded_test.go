//go:build treatment

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With the tag the embedded agent is registered and -treatmentConfig is a
// runtime path: an unreadable config is an error, not a fatal build mismatch.
func TestEmbeddedTreatmentAgentRegistered(t *testing.T) {
	if embeddedTreatmentAgent == nil {
		t.Fatal("treatment tag did not register the embedded agent")
	}
	if err := checkTreatmentFlags("/etc/xtcp2/controller-agent.json", ""); err != nil {
		t.Fatalf("tagged build refused -treatmentConfig: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"host","location":"dc","controller":"127.0.0.1:1","certificate":"/nonexistent.crt","private_key":"/nonexistent.key","ca":"/nonexistent-ca.crt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	stop, err := startTreatments(context.Background(), path, "", "dc")
	if err == nil || stop != nil || !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("missing credentials must fail at start: %v", err)
	}
	if activeTreatments.Load() != nil {
		t.Fatal("failed start left a feeder active")
	}
}

// The embedded agent's location rule runs before any credential is read.
func TestEmbeddedTreatmentAgentLocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(`{"agent_id":"host","location":"dc1","controller":"127.0.0.1:1","certificate":"/nonexistent.crt","private_key":"/nonexistent.key","ca":"/nonexistent-ca.crt"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, description string
		daemon            string
		expected          string
	}{
		{"unset", "positive: a daemon without a location adopts the agent's and proceeds to credentials", "", "nonexistent"},
		{"equal", "positive: an equal location proceeds to credentials", "dc1", "nonexistent"},
		{"differs", "negative: a different location is refused before any credential is read", "dc2", `"dc2"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop, err := startTreatments(context.Background(), path, "", tc.daemon)
			if err == nil || stop != nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("%s: err=%v, want %q", tc.description, err, tc.expected)
			}
			if tc.name == "differs" && strings.Contains(err.Error(), "nonexistent") {
				t.Fatalf("%s: credentials were read before the location check", tc.description)
			}
			if activeTreatments.Load() != nil {
				t.Fatal("failed start left a feeder active")
			}
		})
	}
}
