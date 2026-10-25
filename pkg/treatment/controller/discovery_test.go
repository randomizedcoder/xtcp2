package controller

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/protobuf/proto"
)

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDiscoveryWindow(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name         string
		window, want time.Duration
		invalid      bool
	}{
		{"production-default", 0, time.Hour, false},
		{"short-window", time.Minute, time.Minute, false},
		{"maximum", 7 * 24 * time.Hour, 7 * 24 * time.Hour, false},
		{"negative", -time.Second, 0, true},
		{"too-short", 59 * time.Second, 0, true},
		{"too-long", 7*24*time.Hour + time.Second, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			ch := &ClickHouse{URL: "http://fixture.invalid", Table: "xtcp.rows", Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				params := r.URL.Query()
				wantEnd := now.Add(-20 * time.Second)
				if params.Get("param_start") != strconv.FormatInt(wantEnd.Add(-tc.want).UnixNano(), 10) || params.Get("param_end") != strconv.FormatInt(wantEnd.UnixNano(), 10) {
					t.Fatalf("wrong discovery bounds: %s", r.URL.RawQuery)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}}
			s := &Server{Store: emptyDiscoveryStore(t), Now: func() time.Time { return now }, DiscoveryWindow: tc.window}
			err := s.Discover(context.Background(), ch, []DiscoveryTemplate{{ID: "near", Location: "test-dc", CongestionControl: "cubic", MaximumRTTMs: 15}})
			if (err != nil) != tc.invalid || called == tc.invalid {
				t.Fatalf("err=%v query-called=%t invalid=%t", err, called, tc.invalid)
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
			called := false
			ch := &ClickHouse{URL: "http://fixture.invalid", Table: "xtcp.rows", Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}}
			err := (&Server{Store: emptyDiscoveryStore(t)}).Discover(context.Background(), ch, []DiscoveryTemplate{template})
			if (err != nil) != tc.invalid || called == tc.invalid {
				t.Fatalf("err=%v queried=%t", err, called)
			}
			if tc.invalid {
				return
			}
			want := treatment.DefaultPolicy()
			want.MaximumRttIncrease = tc.want
			if !proto.Equal(template.policy(), want) {
				t.Fatalf("wrong policy: %v", template.policy())
			}
		})
	}
}

func TestDiscoveryRouteSettings(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		want         *pb.TCPSettings
		invalid      bool
	}{
		{"algorithm-only", "", &pb.TCPSettings{CongestionControl: proto.String("cubic")}, false},
		{"nearby", `,"rto_min_ms":5,"quick_ack":true,"initial_congestion_window":100,"initial_receive_window":100`, &pb.TCPSettings{CongestionControl: proto.String("cubic"), RtoMinMs: proto.Uint32(5), QuickAck: proto.Bool(true), InitialCongestionWindow: proto.Uint32(100), InitialReceiveWindow: proto.Uint32(100)}, false},
		{"explicit-off", `,"quick_ack":false`, &pb.TCPSettings{CongestionControl: proto.String("cubic"), QuickAck: proto.Bool(false)}, false},
		{"zero-rto", `,"rto_min_ms":0`, nil, true},
		{"large-rto", `,"rto_min_ms":120001`, nil, true},
		{"zero-cwnd", `,"initial_congestion_window":0`, nil, true},
		{"large-rwnd", `,"initial_receive_window":1025`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var template DiscoveryTemplate
			if err := json.Unmarshal([]byte(`{"id":"near","location":"test-dc","congestion_control":"cubic","maximum_rtt_ms":15`+tc.fields+`}`), &template); err != nil {
				t.Fatal(err)
			}
			called := false
			ch := &ClickHouse{URL: "http://fixture.invalid", Table: "xtcp.rows", Client: &http.Client{Transport: discoveryTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}}
			err := (&Server{Store: emptyDiscoveryStore(t)}).Discover(context.Background(), ch, []DiscoveryTemplate{template})
			if (err != nil) != tc.invalid || called == tc.invalid {
				t.Fatalf("err=%v query-called=%t", err, called)
			}
			if tc.invalid {
				return
			}
			if !proto.Equal(template.settings(), tc.want) {
				t.Fatalf("got %v, want %v", template.settings(), tc.want)
			}
			if err := treatment.ValidateTreatment(&pb.Treatment{Id: template.ID, Revision: 1, Destination: "192.0.2.1/32", Settings: template.settings()}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
