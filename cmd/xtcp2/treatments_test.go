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

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
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
				embeddedTreatmentAgent = func(context.Context, string, string) (*treatmentFeeder, func(), error) { return nil, nil, nil }
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

// The location rule for the embedded agent, with the daemon's label resolved
// the way main resolves it: -location first, LOCATION from the environment
// over it. The standalone counterpart (keep the daemon's label, adopt when
// there is none) is pkg/treatment/attribution's TestWatcherRefresh rows.
func TestCheckTreatmentLocation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		flag, env         string
		agent             string
		expected          []string // substrings of the error; empty means start
	}{
		{"flag-unset", "positive: no -location and no LOCATION adopts the agent's certificate-bound location", "", "", "dc1", nil},
		{"flag-equal", "positive: -location equal to the agent's starts", "dc1", "", "dc1", nil},
		{"env-equal", "positive: LOCATION equal to the agent's starts", "", "dc1", "dc1", nil},
		{"flag-differs", "negative: -location differing from the agent's refuses, naming both", "dc2", "", "dc1", []string{`"dc2"`, `"dc1"`}},
		{"env-differs", "negative: LOCATION differing from the agent's refuses the same way; the flag alone is not what counts", "", "dc2", "dc1", []string{`"dc2"`, `"dc1"`}},
		{"env-over-flag", "corner: LOCATION overrides -location, as it does for every record, so the override is what is compared", "dc1", "dc2", "dc1", []string{`"dc2"`}},
		{"case", "boundary: labels are compared exactly; DC1 is not dc1", "DC1", "", "dc1", []string{`"DC1"`, `"dc1"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("LOCATION", tc.env)
			} else {
				os.Unsetenv("LOCATION")
			}
			c := &xtcp_config.XtcpConfig{Location: tc.flag}
			envOverrideLabeling(c, 0)
			err := checkTreatmentLocation(c.Location, tc.agent)
			if len(tc.expected) == 0 {
				if err != nil {
					t.Fatalf("%s: %v", tc.description, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s: expected an error", tc.description)
			}
			for _, want := range tc.expected {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%s: error %q does not name %s", tc.description, err, want)
				}
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
