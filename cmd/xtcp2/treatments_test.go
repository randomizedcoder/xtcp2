package main

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"golang.org/x/sys/unix"
)

func TestSIGHUPReconcilesWithoutRebaselineOrShutdown(t *testing.T) {
	sigs := make(chan os.Signal, 3)
	complete := make(chan struct{}, 1)
	complete <- struct{}{}
	reconciled := make(chan struct{}, 1)
	rebaselined := make(chan struct{}, 1)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		awaitSignalAndShutdown(sigs, cancel, complete, time.Second, false, func() { rebaselined <- struct{}{} }, func() { reconciled <- struct{}{} })
		close(done)
	}()
	sigs <- syscall.SIGHUP
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("SIGHUP did not reconcile")
	}
	select {
	case <-ctx.Done():
		t.Fatal("SIGHUP shut down daemon")
	case <-rebaselined:
		t.Fatal("SIGHUP changed link baseline")
	default:
	}
	sigs <- syscall.SIGUSR1
	select {
	case <-rebaselined:
	case <-time.After(time.Second):
		t.Fatal("SIGUSR1 regression")
	}
	sigs <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SIGTERM did not stop")
	}
}

func TestCheckTreatmentFlags(t *testing.T) {
	saved := embeddedTreatmentAgent
	t.Cleanup(func() { embeddedTreatmentAgent = saved })
	for _, tc := range []struct {
		name, description string
		config, dir       string
		compiled          bool
		expected          string
	}{
		{"none", "positive: neither flag is the observation-only daemon", "", "", false, ""},
		{"attribution", "positive: the journal reader is always available", "", "/var/lib/xtcp2/routes", false, ""},
		{"embedded-tagged", "positive: -treatmentConfig works when the agent is compiled in", "/etc/xtcp2/a.json", "", true, ""},
		{"embedded-untagged", "negative: -treatmentConfig without the build tag is a fatal artifact mismatch", "/etc/xtcp2/a.json", "", false, "not compiled into this binary"},
		{"both", "negative: both modes at once is a configuration error even when tagged", "/etc/xtcp2/a.json", "/var/lib/xtcp2/routes", true, "mutually exclusive"},
		{"both-untagged", "corner: mutual exclusion is reported before the missing tag", "/etc/xtcp2/a.json", "/var/lib/xtcp2/routes", false, "mutually exclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			embeddedTreatmentAgent = nil
			if tc.compiled {
				embeddedTreatmentAgent = func(context.Context, string) (*treatmentFeeder, func(), error) { return nil, nil, nil }
			}
			err := checkTreatmentFlags(tc.config, tc.dir)
			if tc.expected == "" {
				if err != nil {
					t.Fatalf("%s: %v", tc.description, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("%s: expected %q, got %v", tc.description, tc.expected, err)
			}
			if tc.name == "embedded-untagged" && !errors.Is(err, errTreatmentNotCompiled) {
				t.Fatal("missing tag must be the sentinel error so main can make it fatal")
			}
		})
	}
}

// Attribution mode installs a stamper fed from the journal directory and
// SIGHUP re-reads it; stopping removes the feeder.
func TestStartTreatmentsAttributionMode(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1700000000, 0)
	cubic := "cubic"
	d := &treatment.DesiredState{AgentID: "host-1", Revision: 1, IssuedAt: now, Assignments: []treatment.Assignment{{ExperimentID: "trial-1", Generation: 1, Mode: treatment.ModeStable, Treatment: treatment.Treatment{ID: "cubic", Revision: 1, Destination: netip.MustParsePrefix("192.0.2.1/32"), Settings: treatment.TCPSettings{CongestionControl: &cubic}}}}}
	write := func(revision uint64) {
		d.Revision = revision
		err := reconcile.Journal{Dir: dir}.WithLock(func(s *reconcile.State, save func() error) error {
			*s = reconcile.State{AgentID: "host-1", Location: "dc1", BootID: "boot-1", Desired: d, Records: []reconcile.Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[0], Status: treatment.ApplyVerified, AppliedAt: now}}}}
			return save()
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write(1)
	if treatmentStamper() != nil {
		t.Fatal("stamper active before start")
	}
	stop, err := startTreatments(context.Background(), "", dir, "dc1")
	if err != nil {
		t.Fatal(err)
	}
	stamp := treatmentStamper()
	if stamp == nil {
		t.Fatal("attribution mode installed no stamper")
	}
	var st unix.Stat_t
	if err := unix.Stat("/proc/self/ns/net", &st); err != nil {
		t.Skip("cannot resolve own network namespace")
	}
	r := &flat.XtcpFlatRecord{Location: "dc1", NetnsInode: st.Ino, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 2, 1}, InetDiagCong: "cubic"}
	deadline := time.Now().Add(5 * time.Second)
	for stamp(r); r.TreatmentStateRevision != 1 && time.Now().Before(deadline); stamp(r) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.TreatmentStateRevision != 1 || r.AgentId != "host-1" || r.TreatmentAssignmentId != "trial-1" || !r.TreatmentAlgorithmExposed {
		t.Fatalf("journal not attributed: %+v", r)
	}
	write(2)
	reconcileTreatments()
	for stamp(r); r.TreatmentStateRevision != 2 && time.Now().Before(deadline); stamp(r) {
		time.Sleep(10 * time.Millisecond)
	}
	if r.TreatmentStateRevision != 2 {
		t.Fatal("SIGHUP did not re-read the journal")
	}
	stop()
	if treatmentStamper() != nil {
		t.Fatal("stop left the feeder active")
	}
	if _, err := startTreatments(context.Background(), "", "", "dc1"); err != nil {
		t.Fatal("no-mode start must succeed")
	}
}
