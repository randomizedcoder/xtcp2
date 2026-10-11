package listenerauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	readMethod  = "/test.v1.Svc/Read"
	adminMethod = "/test.v1.Svc/Admin"
	bareMethod  = "/test.v1.Svc/Unannotated"
)

func policyRaw(token string) *xtcp_config.ListenerAuth {
	return &xtcp_config.ListenerAuth{
		Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
		RawToken:         token,
		FailureJitterMin: durationpb.New(time.Millisecond),
		FailureJitterMax: durationpb.New(time.Millisecond),
	}
}

func testMethods() Methods {
	return Methods{
		readMethod:  xtcp_auth.Access_ACCESS_READ,
		adminMethod: xtcp_auth.Access_ACCESS_ADMIN,
		bareMethod:  xtcp_auth.Access_ACCESS_UNSPECIFIED,
	}
}

func TestPolicyUnary(t *testing.T) {
	disabled := &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED}
	tests := []struct {
		category    string
		description string
		read        *xtcp_config.ListenerAuth
		admin       *xtcp_config.ListenerAuth
		method      string
		token       string // "" sends no authorization metadata
		expected    codes.Code
	}{
		{"positive", "split: the read token reaches a READ method", policyRaw("r"), policyRaw("a"), readMethod, "r", codes.OK},
		{"positive", "split: the admin token reaches a READ method", policyRaw("r"), policyRaw("a"), readMethod, "a", codes.OK},
		{"positive", "split: the admin token reaches an ADMIN method", policyRaw("r"), policyRaw("a"), adminMethod, "a", codes.OK},
		{"positive", "compatibility: with no admin credential the read token reaches an ADMIN method", policyRaw("r"), nil, adminMethod, "r", codes.OK},
		{"positive", "compatibility: an explicitly DISABLED admin credential is the same as unset", policyRaw("r"), disabled, adminMethod, "r", codes.OK},
		{"negative", "split: the read token on an ADMIN method is PermissionDenied, not Unauthenticated", policyRaw("r"), policyRaw("a"), adminMethod, "r", codes.PermissionDenied},
		{"negative", "split: a wrong token on a READ method is Unauthenticated", policyRaw("r"), policyRaw("a"), readMethod, "x", codes.Unauthenticated},
		{"negative", "split: a wrong token on an ADMIN method is Unauthenticated", policyRaw("r"), policyRaw("a"), adminMethod, "x", codes.Unauthenticated},
		{"negative", "split: no credentials on an ADMIN method is Unauthenticated", policyRaw("r"), policyRaw("a"), adminMethod, "", codes.Unauthenticated},
		{"negative", "compatibility: a wrong token is Unauthenticated with a single credential", policyRaw("r"), nil, readMethod, "x", codes.Unauthenticated},
		{"boundary", "split: the read token is a prefix of the admin token and still cannot reach ADMIN", policyRaw("adm"), policyRaw("admin"), adminMethod, "adm", codes.PermissionDenied},
		{"corner", "split: an unannotated method requires admin (fail closed)", policyRaw("r"), policyRaw("a"), bareMethod, "r", codes.PermissionDenied},
		{"corner", "split: a method absent from the table requires admin (fail closed)", policyRaw("r"), policyRaw("a"), "/other.v1.Svc/New", "r", codes.PermissionDenied},
		{"corner", "split: read auth disabled leaves READ methods open without credentials", nil, policyRaw("a"), readMethod, "", codes.OK},
		{"corner", "split: read auth disabled still requires the admin token for ADMIN", nil, policyRaw("a"), adminMethod, "", codes.Unauthenticated},
		{"corner", "split: read auth disabled; an arbitrary token is not promoted to admin", nil, policyRaw("a"), adminMethod, "r", codes.Unauthenticated},
		{"corner", "nothing configured: every method is open", nil, nil, adminMethod, "", codes.OK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			p, err := NewPolicy(tc.read, tc.admin, testMethods())
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(authorizationMeta, "Bearer "+tc.token))
			}
			called := false
			_, err = p.UnaryServerInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: tc.method},
				func(context.Context, any) (any, error) { called = true; return nil, nil })
			if got := status.Code(err); got != tc.expected {
				t.Fatalf("code = %s, want %s (err %v)", got, tc.expected, err)
			}
			if called != (tc.expected == codes.OK) {
				t.Fatalf("handler called = %v with code %s", called, tc.expected)
			}
		})
	}
}

type policyStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s policyStream) Context() context.Context { return s.ctx }

func TestPolicyStream(t *testing.T) {
	tests := []struct {
		category    string
		description string
		method      string
		token       string
		expected    codes.Code
	}{
		{"positive", "a read stream with the read token passes", readMethod, "r", codes.OK},
		{"negative", "an admin stream with the read token is PermissionDenied", adminMethod, "r", codes.PermissionDenied},
		{"negative", "a stream without metadata is Unauthenticated", readMethod, "", codes.Unauthenticated},
		{"corner", "an admin stream with the admin token passes", adminMethod, "a", codes.OK},
	}
	p, err := NewPolicy(policyRaw("r"), policyRaw("a"), testMethods())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ctx := context.Background()
			if tc.token != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(authorizationMeta, "Bearer "+tc.token))
			}
			err := p.StreamServerInterceptor()(nil, policyStream{ctx: ctx}, &grpc.StreamServerInfo{FullMethod: tc.method},
				func(any, grpc.ServerStream) error { return nil })
			if got := status.Code(err); got != tc.expected {
				t.Fatalf("code = %s, want %s", got, tc.expected)
			}
		})
	}
}

func TestPolicyHTTP(t *testing.T) {
	tests := []struct {
		category    string
		description string
		path        string
		token       string
		expected    int
	}{
		{"positive", "the read token scrapes /metrics", "/metrics", "r", http.StatusOK},
		{"positive", "the admin token reaches pprof", "/debug/pprof/heap", "a", http.StatusOK},
		{"negative", "the read token on pprof is forbidden", "/debug/pprof/heap", "r", http.StatusForbidden},
		{"negative", "no token on /metrics is unauthorized", "/metrics", "", http.StatusUnauthorized},
		{"boundary", "the bare pprof prefix without a trailing slash is not admin", "/debug/pprof", "r", http.StatusOK},
		{"corner", "health checks accept the admin token too", "/readyz", "a", http.StatusOK},
	}
	p, err := NewPolicy(policyRaw("r"), policyRaw("a"), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := p.WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		PathAccess("/debug/pprof/"))
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set(AuthorizationHeader, "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.expected {
				t.Fatalf("status = %d, want %d", rec.Code, tc.expected)
			}
			if rec.Code == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal("401 without WWW-Authenticate: Bearer")
			}
		})
	}
}

func TestServiceMethods(t *testing.T) {
	sd := xtcp_config.File_xtcp_config_v1_xtcp_config_proto.Services().ByName("ConfigService")
	methods := ServiceMethods(sd)
	tests := []struct {
		category    string
		description string
		method      string
		expected    xtcp_auth.Access
	}{
		{"positive", "Get only reads", "/xtcp_config.v1.ConfigService/Get", xtcp_auth.Access_ACCESS_READ},
		{"positive", "Set changes the whole configuration", "/xtcp_config.v1.ConfigService/Set", xtcp_auth.Access_ACCESS_ADMIN},
		{"positive", "TriggerPoll drives work", "/xtcp_config.v1.ConfigService/TriggerPoll", xtcp_auth.Access_ACCESS_ADMIN},
		{"negative", "an unknown method is enforced as admin", "/xtcp_config.v1.ConfigService/Nope", xtcp_auth.Access_ACCESS_ADMIN},
		{"corner", "health Check from StandardMethods is read", "/grpc.health.v1.Health/Check", xtcp_auth.Access_ACCESS_READ},
	}
	p, err := NewPolicy(nil, nil, StandardMethods().Merge(methods))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := p.MethodAccess(tc.method); got != tc.expected {
				t.Fatalf("access = %s, want %s", got, tc.expected)
			}
		})
	}
	for i := range sd.Methods().Len() {
		name := "/xtcp_config.v1.ConfigService/" + string(sd.Methods().Get(i).Name())
		if methods[name] == xtcp_auth.Access_ACCESS_UNSPECIFIED {
			t.Errorf("%s has no explicit (xtcp_auth.v1.access) option", name)
		}
	}
}

// TestPolicyNoAllocsOnLookup guards the request-path lookup: the method table
// is built once and classifying a call allocates nothing.
func TestPolicyNoAllocsOnLookup(t *testing.T) {
	p, err := NewPolicy(nil, nil, testMethods())
	if err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = p.MethodAccess(adminMethod) }); n != 0 {
		t.Fatalf("MethodAccess allocates %.0f times per call", n)
	}
}

func BenchmarkPolicyUnary(b *testing.B) {
	p, err := NewPolicy(policyRaw("r"), policyRaw("a"), testMethods())
	if err != nil {
		b.Fatal(err)
	}
	intercept := p.UnaryServerInterceptor()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(authorizationMeta, "Bearer a"))
	info := &grpc.UnaryServerInfo{FullMethod: adminMethod}
	handler := func(context.Context, any) (any, error) { return nil, nil }
	b.ReportAllocs()
	for b.Loop() {
		if _, err := intercept(ctx, nil, info, handler); err != nil {
			b.Fatal(err)
		}
	}
}
