package linkmonitorctl

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeService struct {
	pb.UnimplementedLinkMonitorServiceServer
	fail           error
	lastInterfaces *pb.ListInterfacesRequest
	lastCollectors *pb.ListCollectorsRequest
	lastRebaseline *pb.RebaselineRequest
	lastResync     *pb.ResyncRequest
}

func u64(v uint64) *uint64 { return &v }
func i64(v int64) *int64   { return &v }
func boolp(v bool) *bool   { return &v }

var resyncAt = timestamppb.New(time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC))

func (f *fakeService) GetStatus(context.Context, *pb.GetStatusRequest) (*pb.GetStatusResponse, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return &pb.GetStatusResponse{
		SnapshotVersion: 7, Health: &pb.Health{Running: true, Ready: true},
		Counts:               &pb.LinkCounts{Current: u64(1), Expected: u64(2), Delta: i64(-1)},
		LastSuccessfulResync: resyncAt,
		Rebaseline:           &pb.ControlProgress{Requested: 3, Completed: 2},
	}, nil
}

func (f *fakeService) ListInterfaces(_ context.Context, in *pb.ListInterfacesRequest) (*pb.ListInterfacesResponse, error) {
	f.lastInterfaces = in
	return &pb.ListInterfacesResponse{Interfaces: []*pb.Interface{{
		Name: "eth1", Up: boolp(true), Eligibility: pb.Eligibility_ELIGIBILITY_INCLUDED,
		MaximumSpeed: pb.CheckStatus_CHECK_STATUS_FAIL, FullDuplex: pb.CheckStatus_CHECK_STATUS_NOT_APPLICABLE,
		MaximumSpeedException: true, UpTransitions: 4, DownTransitions: 3,
	}}}, nil
}

func (f *fakeService) ListCollectors(_ context.Context, in *pb.ListCollectorsRequest) (*pb.ListCollectorsResponse, error) {
	f.lastCollectors = in
	return &pb.ListCollectorsResponse{Collectors: []*pb.Collector{
		{Interface: "eth1", Name: "carrier", Support: pb.Support_SUPPORT_SUPPORTED, Success: boolp(true), Fresh: true,
			LastSuccess: resyncAt, Duration: durationpb.New(3 * time.Millisecond)},
		{Name: "netstat", ErrorReason: "permission"},
	}}, nil
}

func (f *fakeService) Rebaseline(_ context.Context, in *pb.RebaselineRequest) (*pb.RebaselineResponse, error) {
	f.lastRebaseline = in
	if f.fail != nil {
		return nil, f.fail
	}
	return &pb.RebaselineResponse{Generation: 4, Completed: in.GetWait() != nil, Expected: u64(2)}, nil
}

func (f *fakeService) Resync(_ context.Context, in *pb.ResyncRequest) (*pb.ResyncResponse, error) {
	f.lastResync = in
	return &pb.ResyncResponse{Generation: 9, Completed: in.GetWait() != nil, LastSuccessfulResync: resyncAt}, nil
}

func startFake(t *testing.T, fake *fakeService) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterLinkMonitorServiceServer(srv, fake)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestRun(t *testing.T) {
	tests := []struct {
		category    string
		description string
		args        []string
		fail        error
		expected    int
		stdout      []string // substrings
		stderr      []string
		check       func(*testing.T, *fakeService)
	}{
		{"positive", "status renders counts and unknowns", []string{"status"}, nil, 0,
			[]string{"current=1 expected=2 delta=-1", "recorded_at=unknown", "last_resync: 2026-10-11T01:02:03Z", "rebaseline 2/3", "snapshot:    7"}, nil, nil},
		{"positive", "interfaces renders checks as words", []string{"interfaces"}, nil, 0,
			[]string{"eth1", "included", "fail", "not_applicable", "unknown", "4/3"}, nil, nil},
		{"positive", "interfaces passes repeated names and eligible-only", []string{"interfaces", "-name", "eth1", "-name", "eth2", "-eligible-only"}, nil, 0, nil, nil,
			func(t *testing.T, f *fakeService) {
				if got := f.lastInterfaces.GetNames(); len(got) != 2 || got[1] != "eth2" || !f.lastInterfaces.GetEligibleOnly() {
					t.Fatalf("request = %v", f.lastInterfaces)
				}
			}},
		{"positive", "collectors renders the host row", []string{"collectors"}, nil, 0,
			[]string{"(host)", "netstat", "permission", "carrier", "supported", "3ms"}, nil, nil},
		{"positive", "rebaseline without wait reports acceptance", []string{"rebaseline", "-reason", "INC-1"}, nil, 0,
			[]string{"ok: rebaseline generation 4 accepted"}, nil,
			func(t *testing.T, f *fakeService) {
				if f.lastRebaseline.GetReason() != "INC-1" || f.lastRebaseline.Wait != nil {
					t.Fatalf("request = %v", f.lastRebaseline)
				}
			}},
		{"positive", "rebaseline with wait reports completion", []string{"rebaseline", "-reason", "INC-1", "-wait", "30s"}, nil, 0,
			[]string{"generation 4 completed; expected=2"}, nil,
			func(t *testing.T, f *fakeService) {
				if f.lastRebaseline.GetWait().AsDuration() != 30*time.Second {
					t.Fatalf("wait = %v", f.lastRebaseline.GetWait())
				}
			}},
		{"positive", "resync with wait reports the resync time", []string{"resync", "-reason", "x", "-wait", "1s"}, nil, 0,
			[]string{"resync generation 9 completed; last_resync=2026-10-11T01:02:03Z"}, nil, nil},
		{"positive", "json output is protojson", []string{"status", "-format", "json"}, nil, 0,
			[]string{`"snapshotVersion"`, `"7"`, `"delta"`}, nil, nil},
		{"positive", "help exits zero", []string{"status", "-h"}, nil, 0, nil, []string{"-format"}, nil},
		{"negative", "no command prints usage", nil, nil, 2, nil, []string{"Commands:", "rebaseline  admin"}, nil},
		{"negative", "an unknown command is a usage error", []string{"frobnicate"}, nil, 2, nil, []string{`unknown command "frobnicate"`}, nil},
		{"negative", "rebaseline without a reason is a usage error", []string{"rebaseline"}, nil, 2, nil, []string{"-reason is required"},
			func(t *testing.T, f *fakeService) {
				if f.lastRebaseline != nil {
					t.Fatal("RPC sent without a reason")
				}
			}},
		{"negative", "a whitespace reason is a usage error", []string{"resync", "-reason", "  "}, nil, 2, nil, []string{"-reason is required"}, nil},
		{"negative", "a bad format is a usage error", []string{"status", "-format", "yaml"}, nil, 2, nil, []string{"text or json"}, nil},
		{"negative", "extra arguments are a usage error", []string{"status", "now"}, nil, 2, nil, []string{"unexpected arguments"}, nil},
		{"negative", "an unknown flag is a usage error", []string{"status", "-name", "eth1"}, nil, 2, nil, []string{"flag provided but not defined"}, nil},
		{"negative", "an RPC error exits 1 with code and message", []string{"rebaseline", "-reason", "x"}, status.Error(codes.PermissionDenied, "permission denied"), 1,
			nil, []string{"rebaseline: PermissionDenied: permission denied"}, nil},
		{"boundary", "a negative wait is a usage error", []string{"resync", "-reason", "x", "-wait", "-1s"}, nil, 2, nil, []string{"must not be negative"}, nil},
		{"corner", "an RPC error on a read command also exits 1", []string{"status"}, status.Error(codes.Unavailable, "down"), 1, nil, []string{"status: Unavailable: down"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: exit %d", tc.category, tc.description, tc.expected)
			fake := &fakeService{fail: tc.fail}
			conn := startFake(t, fake)
			var stdout, stderr bytes.Buffer
			if got := Run(t.Context(), conn, "prog", tc.args, &stdout, &stderr); got != tc.expected {
				t.Fatalf("exit = %d, want %d\nstdout: %s\nstderr: %s", got, tc.expected, stdout.String(), stderr.String())
			}
			for _, want := range tc.stdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout missing %q:\n%s", want, stdout.String())
				}
			}
			for _, want := range tc.stderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr missing %q:\n%s", want, stderr.String())
				}
			}
			if tc.check != nil {
				tc.check(t, fake)
			}
		})
	}
}

func TestRunJSONEveryCommand(t *testing.T) {
	for _, args := range [][]string{
		{"status"}, {"interfaces"}, {"collectors"},
		{"rebaseline", "-reason", "x"}, {"resync", "-reason", "x"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Logf("positive: %s -format json; expected: valid JSON object", args[0])
			var stdout, stderr bytes.Buffer
			if got := Run(t.Context(), startFake(t, &fakeService{}), "prog", append(args, "-format", "json"), &stdout, &stderr); got != 0 {
				t.Fatalf("exit = %d: %s", got, stderr.String())
			}
			var obj map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &obj); err != nil {
				t.Fatalf("invalid JSON %q: %v", stdout.String(), err)
			}
		})
	}
}

func TestAccess(t *testing.T) {
	tests := []struct {
		category, description string
		args                  []string
		expected              xtcp_auth.Access
	}{
		{"positive", "status reads", []string{"status"}, xtcp_auth.Access_ACCESS_READ},
		{"positive", "rebaseline is admin", []string{"rebaseline", "-reason", "x"}, xtcp_auth.Access_ACCESS_ADMIN},
		{"positive", "resync is admin", []string{"resync"}, xtcp_auth.Access_ACCESS_ADMIN},
		{"negative", "an unknown command reads (Run rejects it before dialing matters)", []string{"nope"}, xtcp_auth.Access_ACCESS_READ},
		{"corner", "no command reads", nil, xtcp_auth.Access_ACCESS_READ},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := Access(tc.args); got != tc.expected {
				t.Fatalf("Access = %s", got)
			}
		})
	}
}

func TestCommandsListed(t *testing.T) {
	t.Log("corner: every command appears in the usage order exactly once; expected: equal sets")
	if len(commandOrder) != len(commands) {
		t.Fatalf("commandOrder has %d entries, commands %d", len(commandOrder), len(commands))
	}
	for _, name := range commandOrder {
		if _, ok := commands[name]; !ok {
			t.Fatalf("%s listed but not defined", name)
		}
	}
}
