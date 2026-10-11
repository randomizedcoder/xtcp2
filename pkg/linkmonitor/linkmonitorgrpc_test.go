package linkmonitor_test

import (
	"context"
	"net"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

// grpcFixture publishes two Ethernet devices (eth1 up, eth2 down) with an
// expected count of 2, and serves them over bufconn.
func grpcFixture(t *testing.T) (*linkmonitor.PrometheusFixture, pb.LinkMonitorServiceClient) {
	t.Helper()
	f := linkmonitor.NewPrometheusFixture(t)
	f.Device(1, "eth1", true)
	f.Device(2, "eth2", false)
	if err := f.Samples(1, model.CollectorCarrier, model.Sample{
		Descriptor: "go_link_monitor_interface_carrier", Kind: model.SampleGauge, Number: model.Unsigned(1),
	}); err != nil {
		t.Fatal(err)
	}
	f.Exceptions("eth1")
	f.Publish(2)
	srv, err := linkmonitorgrpc.NewServer(f.Monitor)
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	pb.RegisterLinkMonitorServiceServer(s, srv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return f, pb.NewLinkMonitorServiceClient(conn)
}

func TestGRPCGetStatus(t *testing.T) {
	_, client := grpcFixture(t)
	resp, err := client.GetStatus(t.Context(), &pb.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	c := resp.GetCounts()
	tests := []struct {
		category, description string
		ok                    bool
	}{
		{"positive", "current counts the one up device", c.Current != nil && c.GetCurrent() == 1},
		{"positive", "expected is the published baseline", c.Expected != nil && c.GetExpected() == 2},
		{"positive", "delta is current minus expected", c.Delta != nil && c.GetDelta() == -1},
		{"positive", "the publication version is reported", resp.GetSnapshotVersion() > 0},
		{"positive", "the last resync time is the published stamp", resp.GetLastSuccessfulResync().AsTime().Equal(time.Unix(10, 500000000))},
		{"corner", "the fixture records no baseline time", resp.BaselineRecordedAt == nil},
		{"corner", "control progress starts at zero", resp.GetRebaseline().GetRequested() == 0 && resp.GetResync().GetCompleted() == 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: true", tc.category, tc.description)
			if !tc.ok {
				t.Fatalf("unexpected status %v", resp)
			}
		})
	}
}

func TestGRPCListInterfaces(t *testing.T) {
	_, client := grpcFixture(t)
	tests := []struct {
		category, description string
		req                   *pb.ListInterfacesRequest
		expected              []string
		code                  codes.Code
	}{
		{"positive", "no filter lists every device", &pb.ListInterfacesRequest{}, []string{"eth1", "eth2"}, codes.OK},
		{"positive", "an exact name selects one device", &pb.ListInterfacesRequest{Names: []string{"eth2"}}, []string{"eth2"}, codes.OK},
		{"positive", "eligible_only keeps included devices", &pb.ListInterfacesRequest{EligibleOnly: true}, []string{"eth1", "eth2"}, codes.OK},
		{"negative", "an unknown name selects nothing", &pb.ListInterfacesRequest{Names: []string{"eth9"}}, nil, codes.OK},
		{"negative", "an empty name is invalid", &pb.ListInterfacesRequest{Names: []string{""}}, nil, codes.InvalidArgument},
		{"boundary", "a 65-byte name is invalid", &pb.ListInterfacesRequest{Names: []string{string(make([]byte, 65))}}, nil, codes.InvalidArgument},
		{"corner", "a name is not a prefix match", &pb.ListInterfacesRequest{Names: []string{"eth"}}, nil, codes.OK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v %s", tc.category, tc.description, tc.expected, tc.code)
			resp, err := client.ListInterfaces(t.Context(), tc.req)
			if status.Code(err) != tc.code {
				t.Fatalf("code = %s, want %s (%v)", status.Code(err), tc.code, err)
			}
			var names []string
			for _, i := range resp.GetInterfaces() {
				names = append(names, i.GetName())
			}
			if len(names) != len(tc.expected) {
				t.Fatalf("names = %v, want %v", names, tc.expected)
			}
			for i := range names {
				if names[i] != tc.expected[i] {
					t.Fatalf("names = %v, want %v", names, tc.expected)
				}
			}
		})
	}
	resp, err := client.ListInterfaces(t.Context(), &pb.ListInterfacesRequest{Names: []string{"eth1"}})
	if err != nil {
		t.Fatal(err)
	}
	eth1 := resp.GetInterfaces()[0]
	if eth1.Up == nil || !eth1.GetUp() || !eth1.GetMaximumSpeedException() ||
		eth1.GetEligibility() != pb.Eligibility_ELIGIBILITY_INCLUDED || eth1.GetIdentity() == "" {
		t.Fatalf("eth1 mapped incorrectly: %v", eth1)
	}
}

func TestGRPCListCollectors(t *testing.T) {
	_, client := grpcFixture(t)
	tests := []struct {
		category, description string
		names                 []string
		host                  bool
		interfaces            map[string]bool
	}{
		{"positive", "no filter includes every device and the host", nil, true, map[string]bool{"eth1": true, "eth2": true}},
		{"positive", "a name filter excludes the host", []string{"eth1"}, false, map[string]bool{"eth1": true}},
		{"negative", "an unknown name returns nothing", []string{"eth9"}, false, map[string]bool{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: host=%v interfaces=%v", tc.category, tc.description, tc.host, tc.interfaces)
			resp, err := client.ListCollectors(t.Context(), &pb.ListCollectorsRequest{Names: tc.names})
			if err != nil {
				t.Fatal(err)
			}
			host, seen := false, map[string]bool{}
			for _, c := range resp.GetCollectors() {
				if c.GetInterface() == "" {
					host = host || c.GetName() == "netstat"
					continue
				}
				seen[c.GetInterface()] = true
			}
			if host != tc.host || len(seen) != len(tc.interfaces) {
				t.Fatalf("host=%v interfaces=%v", host, seen)
			}
			for name := range tc.interfaces {
				if !seen[name] {
					t.Fatalf("missing %s in %v", name, seen)
				}
			}
		})
	}
	resp, err := client.ListCollectors(t.Context(), &pb.ListCollectorsRequest{Names: []string{"eth1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range resp.GetCollectors() {
		if c.GetName() == "carrier" {
			if c.Success == nil || !c.GetSuccess() || !c.GetFresh() || c.GetSupport() != pb.Support_SUPPORT_SUPPORTED {
				t.Fatalf("carrier collector mapped incorrectly: %v", c)
			}
			return
		}
	}
	t.Fatal("carrier collector missing")
}

func TestGRPCControls(t *testing.T) {
	tests := []struct {
		category    string
		description string
		run         func(*testing.T, *linkmonitor.PrometheusFixture, pb.LinkMonitorServiceClient) (generation uint64, completed bool, err error)
		generation  uint64
		completed   bool
		code        codes.Code
	}{
		{"positive", "a rebaseline without wait returns its accepted generation", func(t *testing.T, _ *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			r, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "test"})
			return r.GetGeneration(), r.GetCompleted(), err
		}, 1, false, codes.OK},
		{"positive", "a waited rebaseline returns once its pass completes", func(t *testing.T, f *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			go func() {
				for !f.RunControls() {
					time.Sleep(time.Millisecond)
				}
			}()
			r, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "test", Wait: durationpb.New(5 * time.Second)})
			if r.Expected == nil || r.GetExpected() != 2 {
				t.Errorf("expected count = %v", r.Expected)
			}
			return r.GetGeneration(), r.GetCompleted(), err
		}, 1, true, codes.OK},
		{"positive", "a waited resync returns once its pass completes", func(t *testing.T, f *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			go func() {
				for !f.RunControls() {
					time.Sleep(time.Millisecond)
				}
			}()
			r, err := c.Resync(t.Context(), &pb.ResyncRequest{Reason: "test", Wait: durationpb.New(5 * time.Second)})
			if r.GetLastSuccessfulResync() == nil {
				t.Error("last resync missing")
			}
			return r.GetGeneration(), r.GetCompleted(), err
		}, 1, true, codes.OK},
		{"negative", "a wait that expires is DeadlineExceeded", func(t *testing.T, _ *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			r, err := c.Resync(t.Context(), &pb.ResyncRequest{Reason: "test", Wait: durationpb.New(20 * time.Millisecond)})
			return r.GetGeneration(), r.GetCompleted(), err
		}, 0, false, codes.DeadlineExceeded},
		{"negative", "a stopped monitor is Unavailable", func(t *testing.T, f *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			f.StopControls()
			r, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "test"})
			return r.GetGeneration(), r.GetCompleted(), err
		}, 0, false, codes.Unavailable},
		{"negative", "stopping during a wait is Unavailable", func(t *testing.T, f *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			go func() {
				time.Sleep(20 * time.Millisecond)
				f.StopControls()
			}()
			r, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "test", Wait: durationpb.New(5 * time.Second)})
			return r.GetGeneration(), r.GetCompleted(), err
		}, 0, false, codes.Unavailable},
		{"corner", "a request after a completed pass gets and waits for the next generation", func(t *testing.T, f *linkmonitor.PrometheusFixture, c pb.LinkMonitorServiceClient) (uint64, bool, error) {
			if _, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "first"}); err != nil {
				return 0, false, err
			}
			f.RunControls()
			go func() {
				for !f.RunControls() {
					time.Sleep(time.Millisecond)
				}
			}()
			r, err := c.Rebaseline(t.Context(), &pb.RebaselineRequest{Reason: "second", Wait: durationpb.New(5 * time.Second)})
			return r.GetGeneration(), r.GetCompleted(), err
		}, 2, true, codes.OK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: generation %d completed %v %s", tc.category, tc.description, tc.generation, tc.completed, tc.code)
			f, client := grpcFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.StartControls(ctx)
			generation, completed, err := tc.run(t, f, client)
			if status.Code(err) != tc.code {
				t.Fatalf("code = %s, want %s (%v)", status.Code(err), tc.code, err)
			}
			if generation != tc.generation || completed != tc.completed {
				t.Fatalf("generation=%d completed=%v, want %d %v", generation, completed, tc.generation, tc.completed)
			}
		})
	}
}
