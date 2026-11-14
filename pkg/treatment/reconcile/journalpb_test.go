package reconcile

import (
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	jpb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment_journal"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
)

func populatedState(now time.Time) *State {
	d := fixtures.Desired(now)
	pred := d.Assignments[0].Treatment
	pred.ID = "old"
	pred.Settings = treatment.TCPSettings{CongestionControl: ptr("bbr"), CongestionLock: true, RTOMinMs: ptr[uint32](20), InitialCongestionWindow: ptr[uint32](10), InitialReceiveWindow: ptr[uint32](2), QuickAck: ptr(false), ECN: ptr(true)}
	d.Assignments[0].Predecessor = &pred
	d.Assignments = append(d.Assignments, treatment.Assignment{ExperimentID: "stable-6", Generation: 2, Mode: treatment.ModeStable, Treatment: treatment.Treatment{ID: "v6", Revision: 3, Destination: netip.MustParsePrefix("2001:db8::1/128"), Settings: treatment.TCPSettings{QuickAck: ptr(true)}}})
	return &State{
		AgentID: "host-1", Location: "dc1", BootID: "boot-1", CollectionID: fixtures.CollectionID, Desired: d, LocallySuspended: true, SuspensionReason: "incident 7", UpdatedAt: now,
		Records: []Record{
			{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[0], DesiredRevision: 1, Status: treatment.ApplyVerified, ReceivedAt: now, AttemptedAt: now.Add(time.Second), AppliedAt: now.Add(2 * time.Second), VerifiedAt: now.Add(3 * time.Second)}, InstalledRoute: []byte{0x18, 0, 0, 0, 1, 2, 3}, InstalledBootID: "boot-1"},
			{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[1], DesiredRevision: 1, Status: treatment.ApplyPending, ReceivedAt: now}},
			{ApplicationRecord: treatment.ApplicationRecord{Assignment: treatment.Assignment{ExperimentID: "gone", Generation: 3, Mode: treatment.ModeRetired, Treatment: fixtures.Treatment()}, Status: treatment.ApplyRemoved, RemovedAt: now, Error: "x", ExpiredLatched: true}},
		},
	}
}

// TestJournalRoundTrip converts State -> journal text -> State and requires
// deep equality, and journal -> State -> journal and requires proto equality,
// so neither direction drops, smears or aliases a field.
func TestJournalRoundTrip(t *testing.T) {
	now := time.Unix(1700000000, 123456789).UTC()
	for _, tc := range []struct {
		name, description string
		state             *State
	}{
		{"populated", "positive: header, hold, predecessor, v4 and v6 destinations, installed route bytes and every timestamp survive", populatedState(now)},
		{"unset-optional", "boundary: a stable assignment without lease, predecessor or optional settings stays unset", func() *State {
			d := fixtures.Desired(now)
			d.Assignments[0].Mode = treatment.ModeStable
			d.Assignments[0].LeaseExpiresAt = time.Time{}
			return &State{AgentID: "host-1", Desired: d}
		}()},
		{"empty", "boundary: a journal with no desired state and no records is the zero State", &State{}},
		{"tombstone-only", "corner: records without desired state (after a controller reset) survive", &State{AgentID: "host-1", Records: []Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: fixtures.Desired(now).Assignments[0], Status: treatment.ApplyRemoved, RemovedAt: now}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := MarshalText(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			out, err := UnmarshalText(b)
			if err != nil || !reflect.DeepEqual(tc.state, out) {
				t.Fatalf("%s: %v\nin  %+v\nout %+v", tc.description, err, tc.state, out)
			}
			again, err := MarshalText(out)
			if err != nil || string(again) != string(b) {
				t.Fatalf("%s: text differs after a second trip: %v", tc.description, err)
			}
			if !proto.Equal(toJournal(tc.state), toJournal(out)) {
				t.Fatalf("%s: journal messages differ", tc.description)
			}
			if tc.state.Records != nil {
				clone := out.Clone()
				clone.Records[0].Assignment.Treatment.ID = "mutated"
				if len(clone.Records[0].InstalledRoute) > 0 {
					clone.Records[0].InstalledRoute[0] = 0xff
				}
				if out.Records[0].Assignment.Treatment.ID == "mutated" || len(out.Records[0].InstalledRoute) > 0 && out.Records[0].InstalledRoute[0] == 0xff {
					t.Fatal("Clone aliases the original")
				}
			}
		})
	}
}

func TestJournalEnumsMirrorDomain(t *testing.T) {
	_, modes, applies, _ := treatment.EnumCounts()
	for _, tc := range []struct {
		description string
		journal     map[int32]string
		prefix      string
		domain      int
		name        func(int32) string
	}{
		{"assignment modes", jpb.AssignmentMode_name, "ASSIGNMENT_MODE_", modes, func(i int32) string { return treatment.AssignmentMode(i).String() }},
		{"apply statuses", jpb.ApplyStatus_name, "APPLY_STATUS_", applies, func(i int32) string { return treatment.ApplyStatus(i).String() }},
	} {
		if len(tc.journal) != tc.domain {
			t.Errorf("%s: journal defines %d values, domain defines %d", tc.description, len(tc.journal), tc.domain)
		}
		for i, name := range tc.journal {
			want := strings.ToLower(strings.TrimPrefix(name, tc.prefix))
			if got := tc.name(i); got != want {
				t.Errorf("%s %d: domain name %q, journal %q", tc.description, i, got, name)
			}
		}
	}
}

func TestUnmarshalTextLimits(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	for _, tc := range []struct {
		name, description string
		build             func() []byte
		expected          string
	}{
		{"max-records", "boundary: twice MaxAssignments records load", func() []byte {
			s := &State{AgentID: "host-1"}
			for i := range treatment.MaxAssignments * 2 {
				tr := fixtures.Treatment()
				tr.Destination = netip.PrefixFrom(netip.AddrFrom4([4]byte{192, 0, byte(i >> 8), byte(i)}), 32)
				s.Records = append(s.Records, Record{ApplicationRecord: treatment.ApplicationRecord{Assignment: treatment.Assignment{ExperimentID: "e" + strconv.Itoa(i), Generation: 1, Mode: treatment.ModeStable, Treatment: tr}}})
			}
			b, _ := MarshalText(s)
			return b
		}, ""},
		{"too-many-records", "boundary: one record over the cap is refused", func() []byte {
			s := &State{AgentID: "host-1"}
			for i := range treatment.MaxAssignments*2 + 1 {
				tr := fixtures.Treatment()
				tr.Destination = netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
				s.Records = append(s.Records, Record{ApplicationRecord: treatment.ApplicationRecord{Assignment: treatment.Assignment{ExperimentID: "e" + strconv.Itoa(i), Generation: 1, Mode: treatment.ModeStable, Treatment: tr}}})
			}
			b, _ := MarshalText(s)
			return b
		}, "too many journal records"},
		{"oversized", "boundary: a byte over MaxStateBytes is refused before parsing", func() []byte { return make([]byte, treatment.MaxStateBytes+1) }, "too large"},
		{"bad-destination", "negative: an unparsable destination fails treatment validation, not decoding", func() []byte {
			p := toJournal(&State{AgentID: "host-1", Records: []Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: fixtures.Desired(now).Assignments[0]}}}})
			p.Records[0].Assignment.Treatment.Destination = "nonsense"
			b, _ := textOptions.Marshal(p)
			return b
		}, "invalid journal treatment"},
		{"bad-lease", "negative: an invalid lease timestamp names the field", func() []byte {
			p := toJournal(&State{AgentID: "host-1", Desired: fixtures.Desired(now)})
			p.Desired.Assignments[0].LeaseExpiresAt.Nanos = -1
			b, _ := textOptions.Marshal(p)
			return b
		}, "lease_expires_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalText(tc.build())
			if tc.expected == "" {
				if err != nil {
					t.Fatalf("%s: %v", tc.description, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.expected) {
				t.Fatalf("%s: expected %q, got %v", tc.description, tc.expected, err)
			}
		})
	}
	if _, err := MarshalText(nil); err == nil {
		t.Fatal("nil state marshaled")
	}
}
