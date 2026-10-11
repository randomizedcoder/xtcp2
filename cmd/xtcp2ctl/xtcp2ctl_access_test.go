package main

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
)

// startPolicyServer serves the fake ConfigService behind a two-credential
// policy: "read-token" reaches Get only, "admin-token" reaches everything.
func startPolicyServer(t *testing.T) {
	t.Helper()
	raw := func(token string) *xtcp_config.ListenerAuth {
		return &xtcp_config.ListenerAuth{
			Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawToken: token,
			FailureJitterMin: durationpb.New(time.Millisecond), FailureJitterMax: durationpb.New(time.Millisecond),
		}
	}
	policy, err := listenerauth.NewPolicy(raw("read-token"), raw("admin-token"), configAccess)
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(grpc.UnaryInterceptor(policy.UnaryServerInterceptor()))
	xtcp_config.RegisterConfigServiceServer(srv, &fakeConfigServer{})
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

func TestAdminCredentialSelection(t *testing.T) {
	tests := []struct {
		category    string
		description string
		env         map[string]string
		args        []string
		expectedRC  int
		expectedErr string
	}{
		{"positive", "the read token reads the config", map[string]string{"XTCP2_GRPC_AUTH_TOKEN": "read-token"},
			[]string{"get"}, 0, ""},
		{"positive", "an admin command sends the admin token when it is set", map[string]string{
			"XTCP2_GRPC_AUTH_TOKEN": "read-token", "XTCP2_GRPC_ADMIN_TOKEN": "admin-token"},
			[]string{"trigger-poll"}, 0, ""},
		{"negative", "the read token alone cannot trigger a poll", map[string]string{"XTCP2_GRPC_AUTH_TOKEN": "read-token"},
			[]string{"trigger-poll"}, 1, "PermissionDenied"},
		{"negative", "no credential is Unauthenticated", map[string]string{},
			[]string{"get"}, 1, "Unauthenticated"},
		{"boundary", "a read command keeps the shared token even when an admin token is set", map[string]string{
			"XTCP2_GRPC_AUTH_TOKEN": "read-token", "XTCP2_GRPC_ADMIN_TOKEN": "wrong-admin"},
			[]string{"get"}, 0, ""},
		{"corner", "with only the admin token set, a read command uses it", map[string]string{"XTCP2_GRPC_ADMIN_TOKEN": "admin-token"},
			[]string{"get"}, 0, ""},
		{"corner", "the shared credential still works for admin commands without split auth env", map[string]string{
			"XTCP2_GRPC_AUTH_TOKEN": "admin-token"},
			[]string{"trigger-poll"}, 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: rc=%d %s", tc.category, tc.description, tc.expectedRC, tc.expectedErr)
			for _, k := range []string{"XTCP2_GRPC_AUTH_TOKEN", "XTCP2_GRPC_AUTH_TOKEN_FILE", "XTCP2_GRPC_HMAC_SHARED_KEY",
				"XTCP2_GRPC_HMAC_SHARED_KEY_FILE", "XTCP2_GRPC_ADMIN_TOKEN", "XTCP2_GRPC_ADMIN_TOKEN_FILE",
				"XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY", "XTCP2_GRPC_ADMIN_HMAC_SHARED_KEY_FILE"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			startPolicyServer(t)
			rc, _, stderr := run(t, tc.args...)
			if rc != tc.expectedRC {
				t.Fatalf("rc = %d, want %d; stderr %s", rc, tc.expectedRC, stderr)
			}
			if !strings.Contains(stderr, tc.expectedErr) {
				t.Fatalf("stderr %q does not contain %q", stderr, tc.expectedErr)
			}
		})
	}
}
