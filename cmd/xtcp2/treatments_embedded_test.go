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
