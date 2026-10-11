package main

import (
	"context"
	"net"
	"strings"
	"testing"

	linkmonitorpb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const (
	linkReadTokenCst  = "link-read-token"
	linkAdminTokenCst = "link-admin-token"
)

// startLinkServer serves LinkMonitorService on bufconn behind the same
// read/admin Policy the daemon uses. A nil monitor is the daemon with the
// link monitor disabled.
func startLinkServer(t *testing.T, enabled bool) {
	t.Helper()
	var monitor linkmonitorgrpc.Monitor
	if enabled {
		cfg := linkmonitor.DefaultConfig()
		cfg.BaselineFile = t.TempDir() + "/baseline.json"
		m, err := linkmonitor.New(cfg, linkmonitor.Options{})
		if err != nil {
			t.Fatal(err)
		}
		monitor = m
	}
	svc, err := linkmonitorgrpc.NewServer(monitor)
	if err != nil {
		t.Fatal(err)
	}
	raw := xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN
	policy, err := listenerauth.NewPolicy(
		&xtcp_config.ListenerAuth{Mode: raw, RawToken: linkReadTokenCst},
		&xtcp_config.ListenerAuth{Mode: raw, RawToken: linkAdminTokenCst},
		listenerauth.ServiceMethods(linkmonitorgrpc.Descriptor()))
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(policy.UnaryServerInterceptor()))
	linkmonitorpb.RegisterLinkMonitorServiceServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	prevDial := dialFunc
	dialFunc = func(_, _ string) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
			grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	t.Cleanup(func() {
		dialFunc = prevDial
		srv.Stop()
		_ = lis.Close()
	})
}

func TestLink(t *testing.T) {
	tests := []struct {
		category, description, expected string
		disabled                        bool
		adminToken                      bool
		args                            []string
		expectedRC                      int
		stdout, stderr                  string
	}{
		{"positive", "status prints health over the read credential", "rc 0 and a health line",
			false, false, []string{"link", "status"}, 0, "health:      running=false", ""},
		{"positive", "status as protojson", "rc 0 and a JSON health object",
			false, false, []string{"link", "status", "-format", "json"}, 0, `"health"`, ""},
		{"positive", "rebaseline sends the admin credential and reaches the monitor", "rc 1, Unavailable because the monitor is not running",
			false, true, []string{"link", "rebaseline", "-reason", "test"}, 1, "", "Unavailable"},
		{"negative", "rebaseline with only the read credential is refused", "rc 1, PermissionDenied",
			false, false, []string{"link", "rebaseline", "-reason", "test"}, 1, "", "PermissionDenied"},
		{"negative", "a daemon without the link monitor says so", "rc 1, link monitor disabled",
			true, false, []string{"link", "status"}, 1, "", "link monitor disabled"},
		{"boundary", "no link subcommand is a usage error", "rc 2 and usage",
			false, false, []string{"link"}, 2, "", "Usage: xtcp2ctl link"},
		{"corner", "an unknown link subcommand is a usage error", "rc 2",
			false, false, []string{"link", "bogus"}, 2, "", "bogus"},
		{"corner", "a common flag before the subcommand is accepted", "rc 0",
			false, false, []string{"link", "-target", "elsewhere", "collectors"}, 0, "", ""},
		{"corner", "-h prints link usage and succeeds", "rc 0 and the command list",
			false, false, []string{"link", "-h"}, 0, "", "rebaseline"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			for _, k := range []string{"XTCP2_GRPC_AUTH_TOKEN_FILE", "XTCP2_GRPC_HMAC_SHARED_KEY", "XTCP2_GRPC_HMAC_SHARED_KEY_FILE",
				"XTCP2_GRPC_ADMIN_TOKEN", "XTCP2_GRPC_ADMIN_TOKEN_FILE", "XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY", "XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY_FILE"} {
				t.Setenv(k, "")
			}
			t.Setenv("XTCP2_GRPC_AUTH_TOKEN", linkReadTokenCst)
			if tc.adminToken {
				t.Setenv("XTCP2_GRPC_ADMIN_TOKEN", linkAdminTokenCst)
			}
			startLinkServer(t, !tc.disabled)
			rc, out, errStr := run(t, tc.args...)
			if rc != tc.expectedRC {
				t.Fatalf("rc = %d, want %d; stdout=%q stderr=%q", rc, tc.expectedRC, out, errStr)
			}
			if !strings.Contains(out, tc.stdout) {
				t.Errorf("stdout %q does not contain %q", out, tc.stdout)
			}
			if !strings.Contains(errStr, tc.stderr) {
				t.Errorf("stderr %q does not contain %q", errStr, tc.stderr)
			}
		})
	}
}
