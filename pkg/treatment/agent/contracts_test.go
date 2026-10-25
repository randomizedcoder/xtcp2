package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestLoadConfigContracts(t *testing.T) {
	for _, tc := range []struct{ name, description, input, wantError string }{
		{"defaults", "minimal config receives safe defaults", `{"agent_id":"host","location":"dc","controller":"localhost:9000"}`, ""},
		{"explicit", "explicit private route settings are preserved", `{"agent_id":"host","location":"dc","controller":"localhost:9000","table":10003,"rule_priority":30001,"protocol":243}`, ""},
		{"unknown", "typos cannot silently disable settings", `{"agent_id":"host","location":"dc","controller":"localhost:9000","enable_aply":true}`, "unknown field"},
		{"trailing", "two JSON documents are rejected", `{} {}`, "trailing"},
		{"missing", "enrollment fields are required", `{}`, "required"},
		{"bad-id", "path-like agent identity is rejected", `{"agent_id":"../host","location":"dc","controller":"localhost:9000"}`, "required"},
		{"malformed", "truncated JSON is rejected", `{`, "EOF"},
		{"oversized", "oversized config cannot bypass the limit", `{"agent_id":"host","location":"dc","controller":"` + strings.Repeat("x", 65537) + `"}`, "EOF"},
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
			if c.Table != wantTable || c.RulePriority != wantPriority || c.Protocol != wantProtocol || c.EnableApply || c.Directory != "/var/lib/xtcp2/routes" {
				t.Fatalf("%s: unexpected config %+v", tc.description, c)
			}
		})
	}
}

func TestStampContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description           string
		change                      func(*pb.LocalState, *flat.XtcpFlatRecord)
		wantAssignment, wantExposed bool
	}{
		{"match", "verified matching algorithm is exposed", func(*pb.LocalState, *flat.XtcpFlatRecord) {}, true, true},
		{"different-algorithm", "assigned is distinct from exposed", func(_ *pb.LocalState, r *flat.XtcpFlatRecord) { r.InetDiagCong = "bbr" }, true, false},
		{"foreign-netns", "host routes do not apply in other namespaces", func(_ *pb.LocalState, r *flat.XtcpFlatRecord) { r.NetnsInode++ }, false, false},
		{"unknown-destination", "unmatched socket has no treatment", func(_ *pb.LocalState, r *flat.XtcpFlatRecord) {
			r.InetDiagMsgSocketDestination = []byte{198, 51, 100, 1}
		}, false, false},
		{"malformed-address", "short addresses are ignored", func(_ *pb.LocalState, r *flat.XtcpFlatRecord) { r.InetDiagMsgSocketDestination = []byte{192} }, false, false},
		{"local-hold", "local hold clears attribution", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) { s.LocallySuspended = true }, false, false},
		{"fleet-hold", "controller hold clears attribution", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) { s.Desired.Suspended = true }, false, false},
		{"failed", "failed application is never attributed", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) {
			s.Records[0].Status = pb.ApplyStatus_APPLY_STATUS_FAILED
		}, false, false},
		{"no-applied-time", "unapplied record is not attributed", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) { s.Records[0].AppliedAt = nil }, false, false},
		{"expired", "expired trial loses attribution", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) {
			a := s.Records[0].Assignment
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL
			a.LeaseExpiresAt = timestamppb.New(time.Unix(1, 0))
		}, false, false},
		{"trial", "unexpired trial is exposed", func(s *pb.LocalState, _ *flat.XtcpFlatRecord) {
			a := s.Records[0].Assignment
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL
			a.LeaseExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
		}, true, true},
		{"ipv6", "IPv6 host route matches 16-byte socket address", func(s *pb.LocalState, r *flat.XtcpFlatRecord) {
			s.Records[0].Assignment.Treatment.Destination = "2001:db8::1/128"
			r.InetDiagMsgFamily = 10
			r.InetDiagMsgSocketDestination = []byte{0x20, 1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		}, true, true},
		{"ipv4-padded", "kernel padded IPv4 addresses match", func(_ *pb.LocalState, r *flat.XtcpFlatRecord) {
			r.InetDiagMsgSocketDestination = append(r.InetDiagMsgSocketDestination, make([]byte, 12)...)
		}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, r := stampFixture(1)
			tc.change(s, r)
			a.publish(s)
			r.TreatmentAssignmentId = "old"
			r.TreatmentAlgorithmExposed = true
			r.TreatmentGeneration = 99
			a.Stamp(r)
			if (r.TreatmentAssignmentId != "") != tc.wantAssignment || r.TreatmentAlgorithmExposed != tc.wantExposed {
				t.Fatalf("%s: assignment=%q exposed=%t; want %t/%t", tc.description, r.TreatmentAssignmentId, r.TreatmentAlgorithmExposed, tc.wantAssignment, tc.wantExposed)
			}
			if !tc.wantAssignment && (r.TreatmentGeneration != 0 || r.TreatmentAppliedTimestampNs != 0) {
				t.Fatal("stale attribution was not cleared")
			}
			if r.AgentId != "host" || r.Location != "dc" || r.HostBootId != "boot" || r.CollectionId != "session" || r.TreatmentStateRevision != 1 {
				t.Fatal("collection identity missing")
			}
		})
	}
}

// Reconciliation publishes immutable snapshots while socket workers stamp
// independent records and RPC check-ins clone the current state.
func TestConcurrentPublishStampState(t *testing.T) {
	a, s, _ := stampFixture(16)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 200 {
			next := proto.Clone(s).(*pb.LocalState)
			next.Desired.Revision = uint64(i + 2)
			a.publish(next)
			a.Wake()
		}
	})
	for range 8 {
		wg.Go(func() {
			r := &flat.XtcpFlatRecord{NetnsInode: 123, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 0, 0}, InetDiagCong: "cubic"}
			for range 200 {
				a.Stamp(r)
				if r.TreatmentAssignmentId != "assignment-0" || !r.TreatmentAlgorithmExposed {
					t.Error("inconsistent exposure snapshot")
					return
				}
				state := a.State()
				state.Records[0].Assignment.Id = "caller mutation"
				a.Wake()
			}
		})
	}
	wg.Wait()
	if a.State().Records[0].Assignment.Id != "assignment-0" {
		t.Fatal("State leaked mutable internal snapshot")
	}
	select {
	case <-a.wake:
	default:
		t.Fatal("Wake dropped every notification")
	}
	select {
	case <-a.wake:
		t.Fatal("Wake did not coalesce")
	default:
	}
}

func BenchmarkPublish(b *testing.B) {
	for _, n := range []int{1, 100, 4096} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			a, s, _ := stampFixture(n)
			b.ReportAllocs()
			for b.Loop() {
				a.publish(s)
			}
		})
	}
}
