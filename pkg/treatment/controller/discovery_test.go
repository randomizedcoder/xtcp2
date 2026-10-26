package controller

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/memory"
	"google.golang.org/protobuf/proto"
)

func TestDiscoveryWindow(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name, description string
		window, want      time.Duration
		invalid           bool
	}{
		{"production-default", "positive: an unset window scans the last hour", 0, time.Hour, false},
		{"short-window", "boundary: one minute is the shortest scan", time.Minute, time.Minute, false},
		{"maximum", "boundary: seven days is the longest scan", 7 * 24 * time.Hour, 7 * 24 * time.Hour, false},
		{"negative", "negative: a negative window never reaches the warehouse", -time.Second, 0, true},
		{"too-short", "negative: under a minute is refused", 59 * time.Second, 0, true},
		{"too-long", "negative: over seven days is refused", 7*24*time.Hour + time.Second, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &fakeWarehouse{now: func() time.Time { return now }}
			s := &Server{Store: memory.New(), Now: func() time.Time { return now }, DiscoveryWindow: tc.window}
			err := s.Discover(context.Background(), w, []DiscoveryTemplate{{ID: "near", Location: "test-dc", CongestionControl: "cubic", MaximumRTTMs: 15}})
			if (err != nil) != tc.invalid || (len(w.queries) > 0) == tc.invalid {
				t.Fatalf("%s: err=%v queries=%d invalid=%t", tc.description, err, len(w.queries), tc.invalid)
			}
			if tc.invalid {
				return
			}
			wantEnd := now.Add(-evidence.SettleDelay)
			q := w.queries[0]
			if !q.End.Equal(wantEnd) || !q.Start.Equal(wantEnd.Add(-tc.want)) || q.Location != "test-dc" || q.MaximumRTTMs != 15 {
				t.Fatalf("%s: wrong discovery bounds: %+v", tc.description, q)
			}
		})
	}
}

func TestDiscoveryWarehouseFailures(t *testing.T) {
	now := time.Unix(1800000000, 0)
	agents := []string{"a", "b", "c", "d", "e", "f"}
	candidate := evidence.Candidate{Destination: netip.MustParsePrefix("192.0.2.1/32"), ASN: 64512, Agents: agents, RTTMs: 5}
	failing := func() *fakeWarehouse {
		return &fakeWarehouse{candidates: []evidence.Candidate{candidate}, evidenceErr: errors.New("baseline timeout")}
	}
	for _, tc := range []struct {
		name, description string
		warehouse         *fakeWarehouse
		nilWarehouse      bool
		enrolled          []string
		wantErr           string
		wantWindows       int
		wantPopulation    []string
	}{
		{"candidates-fail", "negative: a failed scan is reported and takes no baseline", &fakeWarehouse{candidateErr: errors.New("warehouse offline")}, false, agents, "warehouse offline", 0, nil},
		{"baseline-fails", "negative: a failed baseline is reported and proposes nothing", failing(), false, agents, "baseline timeout", 1, agents},
		{"no-warehouse", "corner: discovery without a warehouse is a configuration error", nil, true, agents, "requires a warehouse", 0, nil},
		{"no-candidates", "boundary: an empty scan is not an error", &fakeWarehouse{}, false, agents, "", 0, nil},
		{"unenrolled", "negative: candidate hosts that never checked in take no baseline and are not an error", failing(), false, nil, "", 0, nil},
		{"two-enrolled", "boundary: fewer than three enrolled candidate hosts cannot form a cohort", failing(), false, agents[:2], "", 0, nil},
		{"three-enrolled", "corner: the baseline measures only the enrolled candidates, each with its session", failing(), false, []string{"a", "c", "f"}, "baseline timeout", 1, []string{"a", "c", "f"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{Store: memory.New(), Now: func() time.Time { return now }}
			for _, id := range tc.enrolled {
				enroll(t, s, id)
			}
			var w evidence.Warehouse
			if !tc.nilWarehouse {
				w = tc.warehouse
			}
			err := s.Discover(context.Background(), w, []DiscoveryTemplate{{ID: "near", Location: "dc1", CongestionControl: "cubic", MaximumRTTMs: 15}})
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("%s: err=%v want %q", tc.description, err, tc.wantErr)
			}
			if tc.warehouse != nil && len(tc.warehouse.windows) != tc.wantWindows {
				t.Fatalf("%s: baseline windows=%d want %d", tc.description, len(tc.warehouse.windows), tc.wantWindows)
			}
			if tc.wantWindows > 0 {
				win := tc.warehouse.windows[0]
				if !slices.Equal(win.Agents, tc.wantPopulation) || len(win.Sessions) != len(tc.wantPopulation) {
					t.Fatalf("%s: population %v with %d sessions, want %v with one session each", tc.description, win.Agents, len(win.Sessions), tc.wantPopulation)
				}
			}
			if n := len(experimentsOf(t, s.Store)); n != 0 {
				t.Fatalf("%s: proposals=%d", tc.description, n)
			}
		})
	}
}

func TestDiscoveryRTTPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   *float64
		want    float64
		invalid bool
	}{
		{"production-default", nil, .1, false},
		{"zero", proto.Float64(0), 0, false},
		{"soak", proto.Float64(1), 1, false},
		{"negative", proto.Float64(-.1), 0, true},
		{"over-maximum", proto.Float64(1.01), 0, true},
		{"nan", proto.Float64(math.NaN()), 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := DiscoveryTemplate{ID: "near", Location: "test-dc", CongestionControl: "cubic", MaximumRTTMs: 15, MaximumRTTIncrease: tc.limit}
			w := &fakeWarehouse{}
			err := (&Server{Store: memory.New()}).Discover(context.Background(), w, []DiscoveryTemplate{template})
			if (err != nil) != tc.invalid || (len(w.queries) > 0) == tc.invalid {
				t.Fatalf("err=%v queried=%d", err, len(w.queries))
			}
			if tc.invalid {
				return
			}
			want := treatment.DefaultPolicy()
			want.MaximumRTTIncrease = tc.want
			if template.policy() != want {
				t.Fatalf("wrong policy: %+v", template.policy())
			}
		})
	}
}

func TestDiscoveryRouteSettings(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		want         treatment.TCPSettings
		invalid      bool
	}{
		{"algorithm-only", "", treatment.TCPSettings{CongestionControl: proto.String("cubic")}, false},
		{"nearby", `,"rto_min_ms":5,"quick_ack":true,"initial_congestion_window":100,"initial_receive_window":100`, treatment.TCPSettings{CongestionControl: proto.String("cubic"), RTOMinMs: proto.Uint32(5), QuickAck: proto.Bool(true), InitialCongestionWindow: proto.Uint32(100), InitialReceiveWindow: proto.Uint32(100)}, false},
		{"explicit-off", `,"quick_ack":false`, treatment.TCPSettings{CongestionControl: proto.String("cubic"), QuickAck: proto.Bool(false)}, false},
		{"zero-rto", `,"rto_min_ms":0`, treatment.TCPSettings{}, true},
		{"large-rto", `,"rto_min_ms":120001`, treatment.TCPSettings{}, true},
		{"zero-cwnd", `,"initial_congestion_window":0`, treatment.TCPSettings{}, true},
		{"large-rwnd", `,"initial_receive_window":1025`, treatment.TCPSettings{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var template DiscoveryTemplate
			if err := json.Unmarshal([]byte(`{"id":"near","location":"test-dc","congestion_control":"cubic","maximum_rtt_ms":15`+tc.fields+`}`), &template); err != nil {
				t.Fatal(err)
			}
			w := &fakeWarehouse{}
			err := (&Server{Store: memory.New()}).Discover(context.Background(), w, []DiscoveryTemplate{template})
			if (err != nil) != tc.invalid || (len(w.queries) > 0) == tc.invalid {
				t.Fatalf("err=%v queried=%d", err, len(w.queries))
			}
			if tc.invalid {
				return
			}
			if got := template.settings(); !got.Equal(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if err := treatment.ValidateTreatment(&treatment.Treatment{ID: template.ID, Revision: 1, Destination: netip.MustParsePrefix("192.0.2.1/32"), Settings: template.settings()}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
