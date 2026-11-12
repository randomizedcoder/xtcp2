package listeners

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
)

// testReader reads a token file without the mode check, so the table can use
// t.TempDir files; secretfile.Read's own rules are tested in its package.
func testReader(label, path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", label, err)
	}
	return strings.TrimSpace(string(b)), nil
}

func raw(token string) *xtcp_config.ListenerAuth {
	return &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawToken: token}
}

func disabled() *xtcp_config.ListenerAuth {
	return &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED}
}

func spec(t *testing.T, s string) *xtcp_config.Listener {
	t.Helper()
	l, err := ParseSpec(s)
	if err != nil {
		t.Fatalf("ParseSpec(%q): %v", s, err)
	}
	return l
}

// TestListeners is Test FA's pure rows: the default, the cross-listener rules
// and the credential resolution. Bind-level rows (404 per class, per-endpoint
// credentials over the wire) are in cmd/xtcp2 and pkg/xtcp.
func TestListeners(t *testing.T) {
	dir := t.TempDir()
	tokA := filepath.Join(dir, "a")
	tokB := filepath.Join(dir, "b")
	for p, v := range map[string]string{tokA: "token-a\n", tokB: "token-b"} {
		if err := os.WriteFile(p, []byte(v), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	type want struct {
		err      string // substring; "" = no error
		names    []string
		warnings int
	}
	cases := []struct {
		name        string
		category    string
		description string
		cfg         func(t *testing.T) *xtcp_config.XtcpConfig
		expected    want
	}{
		{"default_listeners", "positive", "no listener config, token present: one UDS serving the metrics classes",
			func(*testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t")}
			}, want{names: []string{DefaultName}}},
		{"default_no_token", "negative", "defaults with no token: an error naming the env var and disabled",
			func(*testing.T) *xtcp_config.XtcpConfig { return &xtcp_config.XtcpConfig{} },
			want{err: "LISTENER_RAW_TOKEN_FILE"}},
		{"default_disabled_on_uds", "boundary", "typed disabled auth is allowed on the default UDS",
			func(*testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: disabled()}
			}, want{names: []string{DefaultName}}},
		{"grpc_uds", "positive", "a gRPC read listener on a UDS",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=grpc,serves=grpc-read,address=/run/xtcp2/grpc.sock")}}
			}, want{names: []string{"grpc"}}},
		{"grpc_admin_no_admin_auth", "negative", "grpc-admin next to grpc-read with only auth set",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=grpc,serves=grpc-read+grpc-admin,address=/run/xtcp2/grpc.sock")}}
			}, want{err: "needs an admin credential"}},
		{"grpc_admin_only_with_auth", "boundary", "a listener serving only admin classes may use its auth",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=admin,serves=grpc-admin,address=/run/xtcp2/admin.sock,tokenFile="+tokA)}}
			}, want{names: []string{"admin"}}},
		{"tcp_needs_explicit_network", "negative", "a host:port address with no network",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics,address=:9088")}}
			}, want{err: "not a socket path"}},
		{"tcp_disabled_auth_offloopback", "negative", "disabled auth on all-interfaces TCP",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics,network=tcp,address=:9088,auth=disabled")}}
			}, want{err: "auth disabled on TCP"}},
		{"tcp_disabled_auth_loopback", "boundary", "disabled auth on loopback TCP is allowed with a warning",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics,network=tcp,address=127.0.0.1:9088,auth=disabled")}}
			}, want{names: []string{"m"}, warnings: 1}},
		{"health_only_no_auth_tcp", "boundary", "a health-only TCP listener may run without auth",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=h,serves=health,network=tcp,address=:9090,auth=disabled")}}
			}, want{names: []string{"h"}}},
		{"class_on_two_listeners", "negative", "metrics on two listeners names both",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=one,serves=metrics,address=/run/a.sock"),
					spec(t, "name=two,serves=metrics,address=/run/b.sock")}}
			}, want{err: `also served by listener "one"`}},
		{"grpc_and_http_mixed", "negative", "metrics and grpc-read on one listener",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics+grpc-read,address=/run/a.sock")}}
			}, want{err: "mixes gRPC and HTTP"}},
		{"per_endpoint_credentials", "positive", "metrics and metrics-detail on two listeners with their own token files",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=metrics,serves=health+metrics,address=/run/a.sock,tokenFile="+tokA),
					spec(t, "name=detail,serves=metrics-detail,address=/run/b.sock,tokenFile="+tokB)}}
			}, want{names: []string{"metrics", "detail"}}},
		{"pprof_requires_admin", "negative", "debug next to metrics without admin_auth",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics+debug,address=/run/a.sock")}}
			}, want{err: "serves debug, which needs an admin credential"}},
		{"pprof_with_admin", "positive", "debug with an admin token file",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics+debug,address=/run/a.sock,adminAuth=raw,adminTokenFile="+tokB)}}
			}, want{names: []string{"m"}}},
		{"ip_needs_ip_paths", "negative", "ip class without -linkMonitorIPPaths",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=ip,serves=ip,address=/run/ip.sock")}}
			}, want{err: "needs -linkMonitorIPPaths"}},
		{"inline_token_refused", "corner", "a listener credential given inline instead of as a file",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				l := spec(t, "name=m,serves=metrics,address=/run/a.sock")
				l.Auth = raw("inline")
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{l}}
			}, want{err: "inline"}},
		{"duplicate_names", "corner", "two listeners with one name",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{ListenerAuth: raw("t"), Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics,address=/run/a.sock"),
					spec(t, "name=m,serves=health,address=/run/b.sock")}}
			}, want{err: "name used by more than one"}},
		{"token_file_missing", "negative", "a listener token file that does not exist",
			func(t *testing.T) *xtcp_config.XtcpConfig {
				return &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{
					spec(t, "name=m,serves=metrics,address=/run/a.sock,tokenFile="+filepath.Join(dir, "none"))}}
			}, want{err: "raw_token_file"}},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			specs, warnings, err := Resolve(tc.cfg(t), testReader)
			if tc.expected.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.expected.err) {
					t.Fatalf("%s: err = %v, want one containing %q", tc.description, err, tc.expected.err)
				}
				if !errors.Is(err, ErrConfig) {
					t.Errorf("%s: error is not ErrConfig: %v", tc.description, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			var names []string
			for _, s := range specs {
				names = append(names, s.Name)
			}
			if strings.Join(names, ",") != strings.Join(tc.expected.names, ",") {
				t.Errorf("%s: listeners %v, want %v", tc.description, names, tc.expected.names)
			}
			if len(warnings) != tc.expected.warnings {
				t.Errorf("%s: warnings %q, want %d", tc.description, warnings, tc.expected.warnings)
			}
		})
	}
}

// TestDefaultShape pins the default listener exactly.
func TestDefaultShape(t *testing.T) {
	specs, _, err := Resolve(&xtcp_config.XtcpConfig{ListenerAuth: raw("t")}, testReader)
	if err != nil {
		t.Fatal(err)
	}
	s := specs[0]
	if s.Endpoint.Network != listener.NetworkUnix || s.Endpoint.Address != DefaultSocketPath ||
		s.Endpoint.UnixSocketMode != 0o600 || !s.Endpoint.UnlinkStaleUnixSocket {
		t.Errorf("default endpoint = %+v", s.Endpoint)
	}
	if s.Auth.GetMode() != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN {
		t.Errorf("default auth mode = %v, want raw", s.Auth.GetMode())
	}
	for _, c := range []xtcp_config.EndpointClass{Health, Metrics, MetricsDetail} {
		if !s.Serves(c) {
			t.Errorf("default listener does not serve %s", ClassName(c))
		}
	}
	for _, c := range []xtcp_config.EndpointClass{IP, Debug, GRPCRead, GRPCAdmin} {
		if s.Serves(c) {
			t.Errorf("default listener serves %s", ClassName(c))
		}
	}
}

// TestResolveLoopbackAndTokens: loopback TCP gets TTL 1 and the loopback
// accept filter; per-listener token files are read into the spec.
func TestResolveLoopbackAndTokens(t *testing.T) {
	tok := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	c := &xtcp_config.XtcpConfig{Ipv4Ttl: 3, Ipv6HopLimit: 3, Listeners: []*xtcp_config.Listener{
		spec(t, "name=lo,serves=metrics,network=tcp,address=127.0.0.1:9099,tokenFile="+tok),
		spec(t, "name=wide,serves=health,network=tcp,address=:9098,auth=disabled"),
	}}
	specs, _, err := Resolve(c, testReader)
	if err != nil {
		t.Fatal(err)
	}
	lo, wide := specs[0], specs[1]
	if !lo.Loopback || lo.IPv4TTL != 1 || lo.IPv6HopLimit != 1 {
		t.Errorf("loopback spec = %+v", lo)
	}
	if lo.ClientAuth().RawToken != "secret" {
		t.Errorf("token file not read: %+v", lo.ClientAuth())
	}
	if wide.Loopback || wide.IPv4TTL != 3 {
		t.Errorf("wide spec = %+v", wide)
	}
	if wide.ClientAuth().RawToken != "" {
		t.Errorf("disabled listener sends a credential: %+v", wide.ClientAuth())
	}
}

// TestInferredAuthMode: an unset mode follows the credential file given, so
// adminTokenFile= alone is an admin credential and hmacKeyFile= alone is HMAC.
func TestInferredAuthMode(t *testing.T) {
	dir := t.TempDir()
	tok := filepath.Join(dir, "tok")
	if err := os.WriteFile(tok, []byte("secret\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, category, description string
		spec                        string
		auth, admin                 xtcp_config.ListenerAuthMode
		wantErr                     bool
	}{
		{"admin_token_file", "positive", "adminTokenFile alone makes a raw admin credential",
			"name=g,serves=grpc-read+grpc-admin,address=/run/g.sock,tokenFile=" + tok + ",adminTokenFile=" + tok,
			xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, false},
		{"hmac_key_file", "positive", "hmacKeyFile alone means hmac-utc-minute",
			"name=m,serves=metrics,address=/run/m.sock,hmacKeyFile=" + tok,
			xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE, xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, false},
		{"no_admin", "negative", "no admin file and no admin mode: grpc-admin beside grpc-read is refused",
			"name=g,serves=grpc-read+grpc-admin,address=/run/g.sock,tokenFile=" + tok,
			0, 0, true},
		{"explicit_mode_wins", "boundary", "a typed mode is kept even with a token file",
			"name=m,serves=metrics,address=/run/m.sock,auth=hmac-utc-minute,hmacKeyFile=" + tok + ",tokenFile=" + tok,
			xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE, xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, false},
		{"both_files", "corner", "a token and a key with no mode is raw",
			"name=m,serves=metrics,address=/run/m.sock,hmacKeyFile=" + tok + ",tokenFile=" + tok,
			xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, false},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			c := &xtcp_config.XtcpConfig{Listeners: []*xtcp_config.Listener{spec(t, tc.spec)}}
			specs, _, err := Resolve(c, testReader)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: no error", tc.description)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if got := specs[0].Auth.GetMode(); got != tc.auth {
				t.Errorf("%s: auth mode %v, want %v", tc.description, got, tc.auth)
			}
			if got := specs[0].AdminAuth.GetMode(); got != tc.admin {
				t.Errorf("%s: admin mode %v, want %v", tc.description, got, tc.admin)
			}
		})
	}
}
