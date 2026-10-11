package linkmonitorgrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// fakeMonitor serves empty snapshots; Await blocks until ctx ends unless
// awaitErr is set or done is true.
type fakeMonitor struct {
	submitErr error
	awaitErr  error
	done      bool
	submitted []linkmonitor.ControlKind
}

func (f *fakeMonitor) Snapshot() linkmonitor.Snapshot { return linkmonitor.Snapshot{} }

func (f *fakeMonitor) Submit(k linkmonitor.ControlKind) (linkmonitor.ControlTicket, error) {
	f.submitted = append(f.submitted, k)
	return linkmonitor.ControlTicket{}, f.submitErr
}

func (f *fakeMonitor) Await(ctx context.Context, _ linkmonitor.ControlTicket) error {
	switch {
	case f.awaitErr != nil:
		return f.awaitErr
	case f.done:
		return nil
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeMonitor) ControlProgress(linkmonitor.ControlKind) linkmonitor.ControlProgress {
	return linkmonitor.ControlProgress{}
}

func newTestServer(t *testing.T, m Monitor, opts ...Option) *Server {
	t.Helper()
	s, err := NewServer(m, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// call invokes method by name and returns its status code.
func call(ctx context.Context, s *Server, method string, wait time.Duration, reason string) error {
	var w *durationpb.Duration
	if wait != 0 {
		w = durationpb.New(wait)
	}
	var err error
	switch method {
	case "GetStatus":
		_, err = s.GetStatus(ctx, &pb.GetStatusRequest{})
	case "ListInterfaces":
		_, err = s.ListInterfaces(ctx, &pb.ListInterfacesRequest{})
	case "ListCollectors":
		_, err = s.ListCollectors(ctx, &pb.ListCollectorsRequest{})
	case "Rebaseline":
		_, err = s.Rebaseline(ctx, &pb.RebaselineRequest{Reason: reason, Wait: w})
	case "Resync":
		_, err = s.Resync(ctx, &pb.ResyncRequest{Reason: reason, Wait: w})
	}
	return err
}

func TestServerDisabled(t *testing.T) {
	s := newTestServer(t, nil)
	for _, method := range []string{"GetStatus", "ListInterfaces", "ListCollectors", "Rebaseline", "Resync"} {
		t.Run(method, func(t *testing.T) {
			t.Logf("negative: %s on a server without a monitor; expected: FailedPrecondition", method)
			err := call(t.Context(), s, method, 0, "test")
			if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), ErrDisabled.Error()) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestServerControls(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		category    string
		description string
		monitor     *fakeMonitor
		ctx         context.Context
		method      string
		wait        time.Duration
		reason      string
		expected    codes.Code
		submits     int
	}{
		{"positive", "rebaseline without wait is accepted", &fakeMonitor{}, nil, "Rebaseline", 0, "maintenance", codes.OK, 1},
		{"positive", "resync with a completed wait succeeds", &fakeMonitor{done: true}, nil, "Resync", time.Second, "maintenance", codes.OK, 1},
		{"negative", "an empty reason is rejected before submitting", &fakeMonitor{}, nil, "Rebaseline", 0, "", codes.InvalidArgument, 0},
		{"negative", "a wait above 60s is rejected", &fakeMonitor{}, nil, "Resync", 61 * time.Second, "x", codes.InvalidArgument, 0},
		{"negative", "a negative wait is rejected", &fakeMonitor{}, nil, "Resync", -time.Second, "x", codes.InvalidArgument, 0},
		{"negative", "a stopped monitor is Unavailable", &fakeMonitor{submitErr: linkmonitor.ErrNotRunning}, nil, "Rebaseline", 0, "x", codes.Unavailable, 1},
		{"negative", "an unexpected submit error is Internal", &fakeMonitor{submitErr: errors.New("boom")}, nil, "Resync", 0, "x", codes.Internal, 1},
		{"negative", "an expired wait is DeadlineExceeded", &fakeMonitor{}, nil, "Rebaseline", 10 * time.Millisecond, "x", codes.DeadlineExceeded, 1},
		{"negative", "shutdown during a wait is Unavailable", &fakeMonitor{awaitErr: linkmonitor.ErrNotRunning}, nil, "Rebaseline", time.Second, "x", codes.Unavailable, 1},
		{"boundary", "a 512-byte reason is accepted", &fakeMonitor{}, nil, "Resync", 0, strings.Repeat("r", 512), codes.OK, 1},
		{"boundary", "a 513-byte reason is rejected", &fakeMonitor{}, nil, "Resync", 0, strings.Repeat("r", 513), codes.InvalidArgument, 0},
		{"boundary", "a 60s wait is accepted", &fakeMonitor{done: true}, nil, "Resync", 60 * time.Second, "x", codes.OK, 1},
		{"corner", "a canceled caller is Canceled, not DeadlineExceeded", &fakeMonitor{}, canceled, "Resync", time.Second, "x", codes.Canceled, 1},
		{"corner", "a multi-byte reason counts bytes, not runes", &fakeMonitor{}, nil, "Resync", 0, strings.Repeat("é", 257), codes.InvalidArgument, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ctx := tc.ctx
			if ctx == nil {
				ctx = t.Context()
			}
			err := call(ctx, newTestServer(t, tc.monitor), tc.method, tc.wait, tc.reason)
			if status.Code(err) != tc.expected {
				t.Fatalf("code = %s, want %s (%v)", status.Code(err), tc.expected, err)
			}
			if len(tc.monitor.submitted) != tc.submits {
				t.Fatalf("submits = %d, want %d", len(tc.monitor.submitted), tc.submits)
			}
		})
	}
}

type unixAuth struct{ credentials.CommonAuthInfo }

func (unixAuth) AuthType() string     { return "unix" }
func (unixAuth) PeerIdentity() string { return "uid=1000" }

func TestServerAuditLog(t *testing.T) {
	tcp := &net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
	tests := []struct {
		category, description string
		ctx                   func(context.Context) context.Context
		expected              string
	}{
		{"positive", "a TCP peer is logged by address", func(ctx context.Context) context.Context {
			return peer.NewContext(ctx, &peer.Peer{Addr: tcp})
		}, "192.0.2.1:4242"},
		{"positive", "transport identity wins over the address", func(ctx context.Context) context.Context {
			return peer.NewContext(ctx, &peer.Peer{Addr: tcp, AuthInfo: unixAuth{}})
		}, "uid=1000"},
		{"corner", "no peer information is logged as unknown", func(ctx context.Context) context.Context { return ctx }, "unknown"},
		{"corner", "a peer without an address is logged as unknown", func(ctx context.Context) context.Context {
			return peer.NewContext(ctx, &peer.Peer{})
		}, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected peer: %s", tc.category, tc.description, tc.expected)
			var buf bytes.Buffer
			s := newTestServer(t, &fakeMonitor{}, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
			if _, err := s.Rebaseline(tc.ctx(t.Context()), &pb.RebaselineRequest{Reason: "INC-1234"}); err != nil {
				t.Fatal(err)
			}
			var rec map[string]any
			if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
				t.Fatalf("audit record %q: %v", buf.String(), err)
			}
			want := map[string]any{"rpc": "Rebaseline", "reason": "INC-1234", "peer": tc.expected, "code": "OK", "completed": false, "level": "INFO"}
			for k, v := range want {
				if rec[k] != v {
					t.Fatalf("%s = %v, want %v (record %v)", k, rec[k], v, rec)
				}
			}
		})
	}
}

func TestServerAuditSkipsInvalidRequests(t *testing.T) {
	t.Log("negative: a request rejected by validation; expected: no audit record")
	var buf bytes.Buffer
	s := newTestServer(t, &fakeMonitor{}, WithLogger(slog.New(slog.NewJSONHandler(&buf, nil))))
	if _, err := s.Resync(t.Context(), &pb.ResyncRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("audit record for an invalid request: %s", buf.String())
	}
}

func TestServerMetrics(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	s := newTestServer(t, &fakeMonitor{}, WithRegisterer(reg))
	_ = call(t.Context(), s, "GetStatus", 0, "")
	_ = call(t.Context(), s, "Resync", 0, "")
	tests := []struct {
		category, description, method, code string
		expected                            float64
	}{
		{"positive", "a successful read is counted", "GetStatus", "OK", 1},
		{"negative", "a rejected control is counted by code", "Resync", "InvalidArgument", 1},
		{"corner", "an uncalled method has no count", "Rebaseline", "OK", 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v", tc.category, tc.description, tc.expected)
			if got := testutil.ToFloat64(s.requests.WithLabelValues(tc.method, tc.code)); got != tc.expected {
				t.Fatalf("count = %v", got)
			}
		})
	}
	if _, err := NewServer(&fakeMonitor{}, WithRegisterer(reg)); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if n, err := testutil.GatherAndCount(reg); err != nil || n == 0 {
		t.Fatalf("gather = %d, %v", n, err)
	}
}
