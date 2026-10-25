package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
)

func TestPostgresDiscoveryScopeAndCooldown(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		asns              []uint64
		prefixes          []string
		want              int
	}{
		{"unfiltered", "eligible destination creates one reviewable proposal", nil, nil, 1},
		{"asn-match", "matching ASN permits discovery", []uint64{64512}, nil, 1},
		{"asn-miss", "foreign ASN is outside scope", []uint64{64513}, nil, 0},
		{"prefix-match", "matching destination prefix permits discovery", nil, []string{"192.0.2.0/24"}, 1},
		{"prefix-miss", "foreign destination prefix is outside scope", nil, []string{"198.51.100.0/24"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
			var agents []string
			var evidence strings.Builder
			for i := range 8 {
				id := fmt.Sprintf("host-%d", i)
				enroll(t, s, id)
				agents = append(agents, id)
				raw, err := json.Marshal(&pb.HostEvidence{AgentId: id, Sockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 5, CollectionHealthy: true})
				if err != nil {
					t.Fatal(err)
				}
				evidence.Write(raw)
				evidence.WriteByte('\n')
			}
			row, err := json.Marshal(map[string]any{"destination_hex": "C0000201000000000000000000000000", "family": 2, "asn": 64512, "agents": agents, "rtt_ms": 5, "retransmission_ratio": 0})
			if err != nil {
				t.Fatal(err)
			}
			c := &ClickHouse{URL: "http://fixture.invalid", Table: "db.rows", Now: s.Now, Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				q, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				body := evidence.String()
				if string(q) == strings.ReplaceAll(discoverySQL, "{{TABLE}}", "db.rows") {
					body = string(row)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			templates := []DiscoveryTemplate{{ID: "nearby", Location: "dc1", CongestionControl: "cubic", MaximumRTTMs: 15, ASNs: tc.asns, Prefixes: tc.prefixes}}
			for range 2 {
				if err := s.Discover(context.Background(), c, templates); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err := s.Store.DB.QueryRow("SELECT count(*) FROM xtcp_control_experiments").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != tc.want {
				t.Fatalf("%s: proposals=%d want %d", tc.description, count, tc.want)
			}
			if count == 0 {
				return
			}
			var state string
			if err := s.Store.DB.QueryRow("SELECT state->>'status' FROM xtcp_control_experiments").Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "EXPERIMENT_STATUS_PROPOSED" {
				t.Fatal("discovery started unreviewed experiment")
			}
			// Terminal destination is still suppressed for 24 hours, including after a
			// controller restart (new Server, same authoritative database).
			if _, err := s.Store.DB.Exec("UPDATE xtcp_control_experiments SET state=jsonb_set(state,'{status}','\"EXPERIMENT_STATUS_ROLLED_BACK\"'),updated_at=$1", now); err != nil {
				t.Fatal(err)
			}
			now = now.Add(23 * time.Hour)
			restarted := &Server{Store: s.Store, Now: s.Now}
			if err := restarted.Discover(context.Background(), c, templates); err != nil {
				t.Fatal(err)
			}
			if err := s.Store.DB.QueryRow("SELECT count(*) FROM xtcp_control_experiments").Scan(&count); err != nil || count != 1 {
				t.Fatalf("cooldown lost across restart: %d %v", count, err)
			}
		})
	}
}
