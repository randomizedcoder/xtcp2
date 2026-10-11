package listenerauth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ErrInsufficientAccess reports a credential that authenticated, but only for
// read methods, presented to an admin method.
var ErrInsufficientAccess = errors.New("credential does not grant admin access")

// Methods maps a full gRPC method name ("/pkg.Service/Method") to the access
// class it requires. A method missing from the map requires ACCESS_ADMIN.
type Methods map[string]xtcp_auth.Access

// StandardMethods covers the gRPC services xtcp2 registers from grpc-go
// itself: health checking and server reflection only read.
func StandardMethods() Methods {
	return Methods{
		"/grpc.health.v1.Health/Check":                                   xtcp_auth.Access_ACCESS_READ,
		"/grpc.health.v1.Health/List":                                    xtcp_auth.Access_ACCESS_READ,
		"/grpc.health.v1.Health/Watch":                                   xtcp_auth.Access_ACCESS_READ,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo":      xtcp_auth.Access_ACCESS_READ,
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo": xtcp_auth.Access_ACCESS_READ,
	}
}

// ServiceMethods reads the (xtcp_auth.v1.access) option of every method in sd.
// Methods without the option map to ACCESS_UNSPECIFIED, which is enforced as
// ACCESS_ADMIN.
func ServiceMethods(sd protoreflect.ServiceDescriptor) Methods {
	out := make(Methods, sd.Methods().Len())
	for i := range sd.Methods().Len() {
		md := sd.Methods().Get(i)
		access := xtcp_auth.Access_ACCESS_UNSPECIFIED
		if opts := md.Options(); opts != nil {
			if a, ok := proto.GetExtension(opts, xtcp_auth.E_Access).(xtcp_auth.Access); ok {
				access = a
			}
		}
		out["/"+string(sd.FullName())+"/"+string(md.Name())] = access
	}
	return out
}

// Merge returns a copy of m with every entry of other added; other wins.
func (m Methods) Merge(other Methods) Methods {
	out := make(Methods, len(m)+len(other))
	for k, v := range m {
		out[k] = v
	}
	for k, v := range other {
		out[k] = v
	}
	return out
}

// Policy authorizes calls with a read credential and an optional admin
// credential.
//
// When the admin credential is unset (or DISABLED), the read credential
// reaches every method, exactly as a lone Authenticator does. When it is set,
// the read credential reaches ACCESS_READ methods only and the admin
// credential reaches everything. A disabled read credential leaves read
// methods open while admin methods still require the admin credential.
//
// The method table is built once by NewPolicy. Each call does one map lookup
// and the token comparisons; there is no descriptor reflection on the request
// path.
type Policy struct {
	read    *Authenticator
	admin   *Authenticator
	methods Methods
}

// NewPolicy validates both credentials and copies methods.
func NewPolicy(read, admin *xtcp_config.ListenerAuth, methods Methods) (*Policy, error) {
	r, err := New(read)
	if err != nil {
		return nil, err
	}
	a, err := New(admin)
	if err != nil {
		return nil, err
	}
	return &Policy{read: r, admin: a, methods: Methods{}.Merge(methods)}, nil
}

// Enabled reports whether any credential is enforced.
func (p *Policy) Enabled() bool {
	return p.read.Enabled() || p.split()
}

func (p *Policy) split() bool {
	return p.admin.Enabled()
}

// MethodAccess reports the access class enforced for fullMethod.
func (p *Policy) MethodAccess(fullMethod string) xtcp_auth.Access {
	return effective(p.methods[fullMethod])
}

func effective(a xtcp_auth.Access) xtcp_auth.Access {
	if a == xtcp_auth.Access_ACCESS_READ {
		return a
	}
	return xtcp_auth.Access_ACCESS_ADMIN
}

// Authorize checks the Authorization values of one call against need. It
// returns nil, an authentication error from this package, or
// ErrInsufficientAccess. It never sleeps; callers add Jitter on failure.
func (p *Policy) Authorize(ctx context.Context, values []string, need xtcp_auth.Access) error {
	if !p.split() {
		return p.read.AuthenticateValues(ctx, values)
	}
	if effective(need) == xtcp_auth.Access_ACCESS_READ {
		if !p.read.Enabled() {
			return nil
		}
		if p.read.AuthenticateValues(ctx, values) == nil {
			return nil
		}
		return p.admin.AuthenticateValues(ctx, values)
	}
	err := p.admin.AuthenticateValues(ctx, values)
	if err == nil {
		return nil
	}
	if p.read.Enabled() && p.read.AuthenticateValues(ctx, values) == nil {
		return ErrInsufficientAccess
	}
	return err
}

func (p *Policy) jitter(ctx context.Context) {
	if p.read.Enabled() {
		p.read.Jitter(ctx)
		return
	}
	p.admin.Jitter(ctx)
}

// UnaryServerInterceptor authorizes each unary call by its method's access.
func (p *Policy) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := p.authorizeGRPC(ctx, info.FullMethod); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns interceptStream as a method value for the
// same contextcheck reason documented on Authenticator.StreamServerInterceptor.
func (p *Policy) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return p.interceptStream
}

func (p *Policy) interceptStream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := p.authorizeGRPC(ss.Context(), info.FullMethod); err != nil {
		return err
	}
	return handler(srv, ss)
}

func (p *Policy) authorizeGRPC(ctx context.Context, fullMethod string) error {
	if !p.Enabled() {
		return nil
	}
	md, _ := metadata.FromIncomingContext(ctx)
	err := p.Authorize(ctx, md.Get(authorizationMeta), p.MethodAccess(fullMethod))
	if err == nil {
		return nil
	}
	p.jitter(ctx)
	if errors.Is(err, ErrInsufficientAccess) {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return status.Error(codes.Unauthenticated, "unauthenticated")
}

// PathAccess classifies HTTP requests: a path under any of adminPrefixes is
// ACCESS_ADMIN and every other path is ACCESS_READ. The prefixes are fixed at
// construction.
func PathAccess(adminPrefixes ...string) func(*http.Request) xtcp_auth.Access {
	prefixes := append([]string(nil), adminPrefixes...)
	return func(r *http.Request) xtcp_auth.Access {
		for _, prefix := range prefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				return xtcp_auth.Access_ACCESS_ADMIN
			}
		}
		return xtcp_auth.Access_ACCESS_READ
	}
}

// WrapHTTP authorizes each request by access(r). Failures get jitter and 401,
// or 403 for a read credential on an admin path.
func (p *Policy) WrapHTTP(next http.Handler, access func(*http.Request) xtcp_auth.Access) http.Handler {
	if !p.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := p.Authorize(r.Context(), r.Header.Values(AuthorizationHeader), access(r))
		if err == nil {
			next.ServeHTTP(w, r)
			return
		}
		p.jitter(r.Context())
		if errors.Is(err, ErrInsufficientAccess) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	})
}

// ForAccess picks the credential a client sends to a method of class access:
// the admin credential for admin methods when it is set, and the shared one
// for read methods unless only the admin credential is set.
func ForAccess(shared, admin ClientAuth, access xtcp_auth.Access) ClientAuth {
	haveAdmin := admin.RawToken != "" || admin.HMACSharedKey != ""
	haveShared := shared.RawToken != "" || shared.HMACSharedKey != ""
	if haveAdmin && (effective(access) == xtcp_auth.Access_ACCESS_ADMIN || !haveShared) {
		return admin
	}
	return shared
}
