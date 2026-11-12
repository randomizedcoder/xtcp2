// Package listeners maps xtcp2's endpoint classes (health, metrics,
// metrics-detail, ip, debug, grpc-read, grpc-admin) onto the listeners that
// serve them, checks the cross-listener rules, and resolves each listener's
// endpoint and credentials.
//
// With no listener configured the daemon serves one Unix socket,
// DefaultSocketPath, mode 0600, with raw bearer auth, carrying health,
// metrics and metrics-detail. gRPC, pprof and /ip are not served unless a
// listener names them. See docs/security.md#listeners.
package listeners

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// ErrConfig marks a listener configuration the operator must change. The
// daemon exits with EX_USAGE (64) on it.
var ErrConfig = errors.New("listener configuration")

const (
	// DefaultName is the default listener's name.
	DefaultName = "metrics"
	// DefaultSocketPath is the default listener's Unix socket.
	DefaultSocketPath = "/run/xtcp2/metrics.sock"
	// DefaultGRPCSocketPath is where xtcp2ctl and xtcp2client look for the
	// gRPC listener. No gRPC listener runs unless one is configured; this is
	// the documented place for it.
	DefaultGRPCSocketPath = "/run/xtcp2/grpc.sock"

	// loopbackTTL is the TTL and hop limit of a loopback TCP listener:
	// replies never pass a router.
	loopbackTTL = 1
)

// Class shorthands.
const (
	Health        = xtcp_config.EndpointClass_ENDPOINT_CLASS_HEALTH
	Metrics       = xtcp_config.EndpointClass_ENDPOINT_CLASS_METRICS
	MetricsDetail = xtcp_config.EndpointClass_ENDPOINT_CLASS_METRICS_DETAIL
	IP            = xtcp_config.EndpointClass_ENDPOINT_CLASS_IP
	Debug         = xtcp_config.EndpointClass_ENDPOINT_CLASS_DEBUG
	GRPCRead      = xtcp_config.EndpointClass_ENDPOINT_CLASS_GRPC_READ
	GRPCAdmin     = xtcp_config.EndpointClass_ENDPOINT_CLASS_GRPC_ADMIN
)

// classNames is the operator-facing name of each class, as used in -listener
// serves= and in errors.
var classNames = map[xtcp_config.EndpointClass]string{
	Health:        "health",
	Metrics:       "metrics",
	MetricsDetail: "metrics-detail",
	IP:            "ip",
	Debug:         "debug",
	GRPCRead:      "grpc-read",
	GRPCAdmin:     "grpc-admin",
}

// ClassName is the operator-facing name of c.
func ClassName(c xtcp_config.EndpointClass) string {
	if n, ok := classNames[c]; ok {
		return n
	}
	return c.String()
}

// ParseClass parses an operator-facing class name.
func ParseClass(s string) (xtcp_config.EndpointClass, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for c, n := range classNames {
		if n == s {
			return c, nil
		}
	}
	names := make([]string, 0, len(classNames))
	for _, n := range classNames {
		names = append(names, n)
	}
	slices.Sort(names)
	return 0, fmt.Errorf("%w: unknown endpoint class %q; valid: %s", ErrConfig, s, strings.Join(names, ", "))
}

// IsGRPC reports whether c is served by the gRPC server rather than HTTP.
func IsGRPC(c xtcp_config.EndpointClass) bool {
	return c == GRPCRead || c == GRPCAdmin
}

// isAdmin reports whether c needs the admin credential.
func isAdmin(c xtcp_config.EndpointClass) bool {
	return c == Debug || c == GRPCAdmin
}

// Default is the listener set used when none is configured.
func Default() []*xtcp_config.Listener {
	return []*xtcp_config.Listener{{
		Name: DefaultName,
		Endpoint: &xtcp_config.ListenerEndpoint{
			Network:               xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX,
			Address:               DefaultSocketPath,
			UnixSocketMode:        uint32(listener.DefaultUnixSocketMode),
			UnlinkStaleUnixSocket: true,
		},
		Serves: []xtcp_config.EndpointClass{Health, Metrics, MetricsDetail},
	}}
}

// Effective is c's listeners, or Default when it configures none.
func Effective(c *xtcp_config.XtcpConfig) []*xtcp_config.Listener {
	if ls := c.GetListeners(); len(ls) > 0 {
		return ls
	}
	return Default()
}

// network resolves a listener's network: explicit, or unix when the address
// is an absolute path. A TCP address must be typed (network: tcp), so a
// listener never lands on a network interface by accident.
func network(l *xtcp_config.Listener) (string, error) {
	ep := l.GetEndpoint()
	switch ep.GetNetwork() {
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP:
		return listener.NetworkTCP, nil
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX:
		return listener.NetworkUnix, nil
	}
	if filepath.IsAbs(ep.GetAddress()) {
		return listener.NetworkUnix, nil
	}
	return "", fmt.Errorf("%w: listener %q: address %q is not a socket path; set network=tcp to listen on TCP",
		ErrConfig, l.GetName(), ep.GetAddress())
}

// effectiveAuth is the listener's credential, or the daemon-wide one when it
// has none. An unspecified mode is raw: disabled has to be typed.
func effectiveAuth(l, global *xtcp_config.ListenerAuth) *xtcp_config.ListenerAuth {
	a := l
	if a == nil {
		a = global
	}
	out := &xtcp_config.ListenerAuth{}
	if a != nil {
		out = proto.CloneOf(a)
	}
	if out.GetMode() == xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED {
		out.Mode = xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN
	}
	return out
}

// effectiveAdminAuth is the listener's admin credential, or the daemon-wide
// one. Unspecified stays unspecified: there is no admin credential.
func effectiveAdminAuth(l, global *xtcp_config.ListenerAuth) *xtcp_config.ListenerAuth {
	a := l
	if a == nil {
		a = global
	}
	if a == nil {
		return &xtcp_config.ListenerAuth{}
	}
	return proto.CloneOf(a)
}

func enabled(a *xtcp_config.ListenerAuth) bool {
	m := a.GetMode()
	return m != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED &&
		m != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED
}

// isLoopbackTCP reports whether a TCP address is a loopback IP literal.
func isLoopbackTCP(address string) bool {
	_, err := listener.ParseLoopback(address)
	return err == nil
}

// Validate checks c's effective listeners against the rules that span
// listeners and classes. It reads no files and binds nothing, so
// ConfigService.Set can run it too. Warnings are returned for allowed but
// notable shapes (auth disabled on a loopback TCP listener).
func Validate(c *xtcp_config.XtcpConfig) (warnings []string, err error) {
	ls := Effective(c)
	owner := map[xtcp_config.EndpointClass]string{}
	names := map[string]bool{}
	var errs []error
	for _, l := range ls {
		w, e := validateOne(c, l, owner, names)
		warnings = append(warnings, w...)
		errs = append(errs, e...)
	}
	return warnings, errors.Join(errs...)
}

func validateOne(c *xtcp_config.XtcpConfig, l *xtcp_config.Listener, owner map[xtcp_config.EndpointClass]string, names map[string]bool) (warnings []string, errs []error) {
	name := l.GetName()
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: listener %q: "+format, append([]any{ErrConfig, name}, args...)...))
	}
	if names[name] {
		fail("name used by more than one listener")
	}
	names[name] = true
	if len(l.GetServes()) == 0 {
		fail("serves no endpoint class")
	}
	var grpc, http, adminOnly = false, false, true
	for _, cl := range l.GetServes() {
		if prev, ok := owner[cl]; ok && prev != name {
			fail("class %s is also served by listener %q; a class is served by one listener", ClassName(cl), prev)
		}
		owner[cl] = name
		if IsGRPC(cl) {
			grpc = true
		} else {
			http = true
		}
		if !isAdmin(cl) {
			adminOnly = false
		}
		if cl == IP && !c.GetLinkMonitor().GetIpPaths() {
			fail("serves ip, which needs -linkMonitorIPPaths")
		}
	}
	if grpc && http {
		fail("mixes gRPC and HTTP classes; put them on separate listeners")
	}
	for _, a := range []*xtcp_config.ListenerAuth{l.GetAuth(), l.GetAdminAuth()} {
		if a.GetRawToken() != "" || a.GetHmacSharedKey() != "" {
			fail("carries a credential inline; use raw_token_file / hmac_shared_key_file (tokenFile= / hmacKeyFile=)")
		}
	}

	nw, err := network(l)
	if err != nil {
		errs = append(errs, err)
		return warnings, errs
	}
	auth := effectiveAuth(l.GetAuth(), c.GetListenerAuth())
	admin := effectiveAdminAuth(l.GetAdminAuth(), c.GetAdminListenerAuth())
	healthOnly := len(l.GetServes()) == 1 && l.GetServes()[0] == Health
	loopback := nw == listener.NetworkTCP && isLoopbackTCP(l.GetEndpoint().GetAddress())
	if !enabled(auth) {
		switch {
		case nw == listener.NetworkUnix, healthOnly:
		case loopback:
			warnings = append(warnings, fmt.Sprintf("listener %q: auth disabled on loopback TCP %s; any local process can reach it",
				name, l.GetEndpoint().GetAddress()))
		default:
			fail("auth disabled on TCP %s; disabled auth is allowed only on a Unix socket, a loopback address, or a listener serving health alone",
				l.GetEndpoint().GetAddress())
		}
	}
	for _, cl := range l.GetServes() {
		if !isAdmin(cl) || enabled(admin) || (adminOnly && enabled(auth)) {
			continue
		}
		fail("serves %s, which needs an admin credential: set admin_auth (adminAuth=, adminTokenFile=) or "+
			"-listenerAdminAuthMode, or serve only admin classes on a listener with auth", ClassName(cl))
	}
	return warnings, errs
}

// Spec is a validated listener with its endpoint and credentials resolved.
type Spec struct {
	Name     string
	Endpoint listener.Endpoint
	// Auth and AdminAuth carry the secrets read from their files.
	Auth, AdminAuth *xtcp_config.ListenerAuth
	Classes         []xtcp_config.EndpointClass
	// Loopback is a TCP listener on a loopback address: it accepts loopback
	// peers only and sends with TTL/hop limit 1.
	Loopback              bool
	IPv4TTL, IPv6HopLimit uint32
}

// Serves reports whether s serves class c.
func (s Spec) Serves(c xtcp_config.EndpointClass) bool { return slices.Contains(s.Classes, c) }

// GRPC reports whether s is a gRPC listener.
func (s Spec) GRPC() bool { return s.Serves(GRPCRead) || s.Serves(GRPCAdmin) }

// Policy is s's auth policy for the given gRPC method table (nil for HTTP).
func (s Spec) Policy(methods listenerauth.Methods) (*listenerauth.Policy, error) {
	return listenerauth.NewPolicy(s.Auth, s.AdminAuth, methods)
}

// ClientAuth is the credential a local client (the -healthcheck probe) sends
// to s's read paths.
func (s Spec) ClientAuth() listenerauth.ClientAuth {
	if !enabled(s.Auth) {
		return listenerauth.ClientAuth{}
	}
	return listenerauth.ClientAuth{RawToken: s.Auth.GetRawToken(), HMACSharedKey: s.Auth.GetHmacSharedKey()}
}

// SecretReader reads a credential file. label names the config field.
type SecretReader func(label, path string) (string, error)

// Resolve validates c's effective listeners and resolves each: its network,
// socket options and credentials, reading token files with read. The
// returned warnings should be logged; any error is wrapped in ErrConfig.
func Resolve(c *xtcp_config.XtcpConfig, read SecretReader) ([]Spec, []string, error) {
	warnings, err := Validate(c)
	if err != nil {
		return nil, warnings, err
	}
	var specs []Spec
	for _, l := range Effective(c) {
		s, err := resolveOne(c, l, read)
		if err != nil {
			return nil, warnings, err
		}
		specs = append(specs, s)
	}
	return specs, warnings, nil
}

func resolveOne(c *xtcp_config.XtcpConfig, l *xtcp_config.Listener, read SecretReader) (Spec, error) {
	nw, err := network(l)
	if err != nil {
		return Spec{}, err
	}
	ep, err := listener.EndpointFromProto(l.GetEndpoint(), nw, "")
	if err != nil {
		return Spec{}, fmt.Errorf("%w: listener %q: %w", ErrConfig, l.GetName(), err)
	}
	ep.Network = nw
	if l.GetEndpoint().GetUnixSocketMode() == 0 {
		ep.UnixSocketMode = listener.DefaultUnixSocketMode
	}
	auth := effectiveAuth(l.GetAuth(), c.GetListenerAuth())
	admin := effectiveAdminAuth(l.GetAdminAuth(), c.GetAdminListenerAuth())
	for _, f := range []struct {
		label string
		a     *xtcp_config.ListenerAuth
	}{{l.GetName() + ".auth", auth}, {l.GetName() + ".admin_auth", admin}} {
		if err := readFiles(f.label, f.a, read); err != nil {
			return Spec{}, fmt.Errorf("%w: listener %q: %w", ErrConfig, l.GetName(), err)
		}
	}
	if _, err := listenerauth.NewPolicy(auth, admin, nil); err != nil {
		hint := ""
		if l.GetAuth() == nil && auth.GetRawToken() == "" &&
			auth.GetMode() == xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN {
			hint = "; set LISTENER_RAW_TOKEN_FILE to a file holding the bearer token, or LISTENER_AUTH_MODE=disabled " +
				"(allowed on a Unix socket or loopback address)"
		}
		return Spec{}, fmt.Errorf("%w: listener %q: %w%s", ErrConfig, l.GetName(), err, hint)
	}
	s := Spec{
		Name: l.GetName(), Endpoint: ep, Auth: auth, AdminAuth: admin,
		Classes: slices.Clone(l.GetServes()),
		IPv4TTL: c.GetIpv4Ttl(), IPv6HopLimit: c.GetIpv6HopLimit(),
	}
	if nw == listener.NetworkTCP && isLoopbackTCP(ep.Address) {
		s.Loopback, s.IPv4TTL, s.IPv6HopLimit = true, loopbackTTL, loopbackTTL
	}
	return s, nil
}

// readFiles fills a's secrets from its *_file fields.
func readFiles(label string, a *xtcp_config.ListenerAuth, read SecretReader) error {
	if p := a.GetRawTokenFile(); p != "" {
		v, err := read(label+".raw_token_file", p)
		if err != nil {
			return err
		}
		a.RawToken = v
	}
	if p := a.GetHmacSharedKeyFile(); p != "" {
		v, err := read(label+".hmac_shared_key_file", p)
		if err != nil {
			return err
		}
		a.HmacSharedKey = v
	}
	return nil
}

// socketDirMode is the mode of a socket directory Listen creates: the
// socket's own mode then decides who connects.
const socketDirMode = 0o750

// Listen binds s, creating a Unix socket's directory (DefaultSocketPath's
// /run/xtcp2) when it is missing. A loopback TCP listener closes
// connections from non-loopback peers, calling rejected for each.
func (s Spec) Listen(ctx context.Context, rejected func(net.Addr, error)) (net.Listener, error) {
	if s.Endpoint.Network == listener.NetworkUnix {
		if err := os.MkdirAll(filepath.Dir(s.Endpoint.Address), socketDirMode); err != nil {
			return nil, fmt.Errorf("listener %q: %w", s.Name, err)
		}
	}
	ln, err := listener.Listen(ctx, s.Endpoint, s.IPv4TTL, s.IPv6HopLimit)
	if err != nil {
		return nil, fmt.Errorf("listener %q: %w", s.Name, err)
	}
	if s.Loopback {
		ln = listener.LoopbackOnly(ln, rejected)
	}
	return ln, nil
}

// Find returns the spec serving class c, if any.
func Find(specs []Spec, c xtcp_config.EndpointClass) (Spec, bool) {
	for _, s := range specs {
		if s.Serves(c) {
			return s, true
		}
	}
	return Spec{}, false
}

// Describe is a one-line summary of s for the startup log.
func (s Spec) Describe() string {
	cls := make([]string, 0, len(s.Classes))
	for _, c := range s.Classes {
		cls = append(cls, ClassName(c))
	}
	return fmt.Sprintf("listener %q on %s %s serving %s (auth %s)", s.Name, s.Endpoint.Network, s.Endpoint.Address,
		strings.Join(cls, ","), listenerauth.ModeString(s.Auth.GetMode()))
}
