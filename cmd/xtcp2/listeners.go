package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/pprof" // registered explicitly on a debug listener's mux in httpHandler
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/health"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"github.com/randomizedcoder/xtcp2/pkg/listeners"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// listenersEnv overrides -listener: specs separated by ';'.
const listenersEnv = "LISTENERS"

// listenerFlag collects the repeatable -listener flag.
type listenerFlag []string

func (l *listenerFlag) String() string {
	if l == nil {
		return ""
	}
	return strings.Join(*l, ";")
}

func (l *listenerFlag) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// The listeners the legacy flags expand to, by name.
const (
	legacyProm = "prometheus"
	legacyGRPC = "grpc"
	legacyIP   = "ip"
)

// legacyListenerFlags and legacyListenerEnv are the per-listener settings
// that -listener replaces, by the listener they configure.
var (
	legacyListenerFlags = map[string][]string{
		legacyProm: {"promListen", "promListenNetwork", "promUnixSocketMode", "promUnlinkStaleUnixSocket"},
		legacyGRPC: {"grpcPort", "grpcListenNetwork", "grpcListenAddress", "grpcUnixSocketMode", "grpcUnlinkStaleUnixSocket"},
		legacyIP:   {"linkMonitorIPListen"},
	}
	legacyListenerEnv = map[string][]string{
		legacyProm: {"PROM_LISTEN", "PROM_LISTEN_NETWORK", "PROM_UNIX_SOCKET_MODE", "PROM_UNLINK_STALE_UNIX_SOCKET"},
		legacyGRPC: {"GRPC_PORT", "GRPC_LISTEN_NETWORK", "GRPC_LISTEN_ADDRESS", "GRPC_UNIX_SOCKET_MODE", "GRPC_UNLINK_STALE_UNIX_SOCKET"},
		legacyIP:   {"LINK_MONITOR_IP_LISTEN"},
	}
)

// legacyListenerSettings returns, per legacy listener, the flags and env vars
// given for it.
func legacyListenerSettings(fs *flag.FlagSet) map[string][]string {
	seen := map[string][]string{}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	for name, flags := range legacyListenerFlags {
		for _, f := range flags {
			if given[f] {
				seen[name] = append(seen[name], "-"+f)
			}
		}
	}
	for name, keys := range legacyListenerEnv {
		for _, k := range keys {
			if v, ok := os.LookupEnv(k); ok && v != "" {
				seen[name] = append(seen[name], k)
			}
		}
	}
	return seen
}

// configureListeners sets c.Listeners from -listener / LISTENERS, or from
// the legacy per-listener settings, which it expands with a deprecation
// line. Giving both forms is a usage error: which one wins would be a guess.
// With neither, c.Listeners stays empty and the daemon serves
// listeners.Default.
func configureListeners(c *xtcp_config.XtcpConfig, specs []string, legacy map[string][]string, promListen string) error {
	list := strings.Join(specs, ";")
	if v, ok := os.LookupEnv(listenersEnv); ok && strings.TrimSpace(v) != "" {
		list = v
	}
	ls, err := listeners.ParseList(list)
	if err != nil {
		return err
	}
	var used []string
	for _, name := range []string{legacyProm, legacyGRPC, legacyIP} {
		used = append(used, legacy[name]...)
	}
	if len(ls) > 0 && len(used) > 0 {
		return fmt.Errorf("%w: -listener/%s and the deprecated %s are both set; describe every listener with -listener",
			listeners.ErrConfig, listenersEnv, strings.Join(used, ", "))
	}
	if len(ls) > 0 {
		c.Listeners = ls
		return nil
	}
	if len(used) == 0 {
		return nil
	}
	ls, err = expandLegacyListeners(c, legacy, promListen)
	if err != nil {
		return err
	}
	c.Listeners = ls
	forms := make([]string, 0, len(ls))
	for _, l := range ls {
		forms = append(forms, "-listener '"+listeners.FormatSpec(l)+"'")
	}
	log.Printf("DEPRECATED: %s; use %s", strings.Join(used, ", "), strings.Join(forms, " "))
	return nil
}

// expandLegacyListeners maps the legacy settings onto listeners. The
// Prometheus listener serves health, metrics, metrics-detail and, with
// -linkMonitorIPPaths and no ip listener of its own, ip; it no longer serves
// pprof, which needs a debug listener now. Without it the default socket
// stays. The gRPC listener serves everything under the one credential, or
// read and admin under their own when an admin credential is set. The ip
// listener keeps its loopback-only, no-auth shape.
func expandLegacyListeners(c *xtcp_config.XtcpConfig, legacy map[string][]string, promListen string) ([]*xtcp_config.Listener, error) {
	var out []*xtcp_config.Listener
	m := c.GetLinkMonitor()
	ipOwn := len(legacy["ip"]) > 0 && m.GetIpListen() != ""
	if len(legacy[legacyProm]) > 0 {
		ep := legacyEndpoint(c.GetPrometheusListener(), promListen)
		serves := []xtcp_config.EndpointClass{listeners.Health, listeners.Metrics, listeners.MetricsDetail}
		if m.GetIpPaths() && !ipOwn {
			serves = append(serves, listeners.IP)
		}
		out = append(out, &xtcp_config.Listener{Name: legacyProm, Endpoint: ep, Serves: serves})
	} else {
		out = append(out, listeners.Default()...)
	}
	if len(legacy[legacyGRPC]) > 0 {
		ep := legacyEndpoint(c.GetGrpcListener(), fmt.Sprintf(":%d", c.GetGrpcPort()))
		serves := []xtcp_config.EndpointClass{listeners.GRPCAdmin}
		if authEnabled(c.GetAdminListenerAuth()) {
			serves = []xtcp_config.EndpointClass{listeners.GRPCRead, listeners.GRPCAdmin}
		}
		out = append(out, &xtcp_config.Listener{Name: legacyGRPC, Endpoint: ep, Serves: serves})
	}
	if ipOwn && m.GetEnable() {
		if _, err := listener.ParseLoopback(m.GetIpListen()); err != nil {
			return nil, fmt.Errorf("%w: linkMonitorIPListen: %w", listeners.ErrConfig, err)
		}
		out = append(out, &xtcp_config.Listener{
			Name: "ip",
			Endpoint: &xtcp_config.ListenerEndpoint{
				Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP,
				Address: m.GetIpListen(),
			},
			Auth:   &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED},
			Serves: []xtcp_config.EndpointClass{listeners.IP},
		})
	}
	return out, nil
}

// authEnabled reports whether a enforces a credential.
func authEnabled(a *xtcp_config.ListenerAuth) bool {
	m := a.GetMode()
	return m != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED &&
		m != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED
}

// legacyEndpoint is a legacy listener's endpoint: as configured, with an
// empty network meaning TCP, as it always did, and an empty address
// defaulted.
func legacyEndpoint(ep *xtcp_config.ListenerEndpoint, address string) *xtcp_config.ListenerEndpoint {
	out := &xtcp_config.ListenerEndpoint{UnlinkStaleUnixSocket: unlinkStaleSocketCst}
	if ep != nil {
		out = proto.CloneOf(ep)
	}
	if out.GetNetwork() == xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNSPECIFIED {
		out.Network = xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP
	}
	if out.GetAddress() == "" {
		out.Address = address
	}
	return out
}

// listenerActive is 1 per class each listener serves.
var listenerActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "xtcp2_listener_active",
	Help: "1 for each endpoint class a listener serves: name, network (unix or tcp) and class.",
}, []string{"name", "network", "class"})

// listenersStarter binds and serves the listeners. Tests swap it for a stub
// that binds nothing.
var listenersStarter = startListeners

// startListeners binds every listener before serving any, so a bad address
// or a port in use fails startup cleanly. It serves the HTTP listeners on
// workers that join wg and drain when ctx ends, and returns the gRPC ones
// for the daemon to serve.
func startListeners(ctx context.Context, wg *sync.WaitGroup, specs []listeners.Spec, promPath string) ([]xtcp.GRPCListener, error) {
	type bound struct {
		spec    listeners.Spec
		ln      net.Listener
		handler http.Handler
	}
	var all []bound
	closeAll := func() {
		for i := range all {
			if err := all[i].ln.Close(); err != nil {
				log.Printf("listener %q: close: %v", all[i].spec.Name, err)
			}
		}
	}
	for _, s := range specs {
		var h http.Handler
		if !s.GRPC() {
			var err error
			if h, err = httpHandler(s, promPath); err != nil {
				closeAll()
				return nil, err
			}
		}
		ln, err := s.Listen(ctx, rateLimitedRejectLog(s.Name, ipRejectLogEvery))
		if err != nil {
			closeAll()
			return nil, err
		}
		all = append(all, bound{s, ln, h})
	}
	var grpcLns []xtcp.GRPCListener
	for i := range all {
		b := &all[i]
		for _, cl := range b.spec.Classes {
			listenerActive.WithLabelValues(b.spec.Name, b.spec.Endpoint.Network, listeners.ClassName(cl)).Set(1)
		}
		log.Print(b.spec.Describe())
		if b.handler == nil {
			grpcLns = append(grpcLns, xtcp.GRPCListener{Spec: b.spec, Listener: b.ln})
			continue
		}
		wg.Add(1)
		go serveHTTP(ctx, wg, "listener "+b.spec.Name, newHTTPServer(b.handler), b.ln, httpShutdownGrace, httpServeFailed(b.spec))
	}
	return grpcLns, nil
}

// httpServeFailed is what a listener's serve failure does. Losing health or
// metrics stops the daemon, which must not run unobserved; losing the rest
// is logged.
func httpServeFailed(s listeners.Spec) func(error) {
	if s.Serves(listeners.Health) || s.Serves(listeners.Metrics) {
		return func(err error) { fatalf("listener %q: %v", s.Name, err) }
	}
	return func(err error) {
		log.Printf("listener %q stopped: %v; its paths are unavailable until xtcp2 restarts", s.Name, err)
	}
}

// ipPatterns are the ip class's routes.
var ipPatterns = []string{"/ip", "/ip/", "/i", "/i/"}

// httpHandler builds the mux for one HTTP listener from the classes it
// serves, behind its auth policy. A path outside them is 404.
//
// A dedicated mux, not http.DefaultServeMux: pprof is registered explicitly
// under debug, rather than by the side-effect import that registers it on
// DefaultServeMux (gosec G108).
func httpHandler(s listeners.Spec, promPath string) (http.Handler, error) {
	mux := http.NewServeMux()
	if s.Serves(listeners.Health) {
		mux.HandleFunc("/healthz", health.Healthz)
		mux.HandleFunc("/readyz", health.Readyz)
	}
	if s.Serves(listeners.Metrics) {
		if s.Serves(listeners.IP) && slices.Contains(ipPatterns, promPath) {
			return nil, fmt.Errorf("%w: listener %q: -promPath %s is an ip route", listeners.ErrConfig, s.Name, promPath)
		}
		mux.Handle(promPath, promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
			EnableOpenMetrics:   promEnableOpenMetrics,
			MaxRequestsInFlight: promMaxRequestsInFlight,
		}))
	}
	if s.Serves(listeners.Debug) {
		// pprof.Index also serves the named runtime profiles (goroutine,
		// heap, ...). cmdline is never served: it is the process's argv.
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", http.NotFound)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}
	// The link monitor's detail paths are every other path, once the daemon
	// has attached them; the patterns above are more specific and win. A
	// promPath of "/" on this listener already serves every path.
	if s.Serves(listeners.MetricsDetail) && (promPath != "/" || !s.Serves(listeners.Metrics)) {
		mux.Handle("/", &linkMonitorHTTP)
	}
	if s.Serves(listeners.IP) {
		for _, p := range ipPatterns {
			mux.Handle(p, linkMonitorHTTP.ipRoutes())
		}
	}
	policy, err := s.Policy(nil)
	if err != nil {
		return nil, fmt.Errorf("%w: listener %q: %w", listeners.ErrConfig, s.Name, err)
	}
	return policy.WrapHTTP(mux, listenerauth.PathAccess("/debug/pprof/")), nil
}

// closeGRPCListeners closes the gRPC listeners once the daemon has
// stopped. The daemon's servers close the ones they served, so an error
// here is expected and ignored; this catches a daemon that never served.
func closeGRPCListeners(ls []xtcp.GRPCListener) {
	for i := range ls {
		if err := ls[i].Listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("listener %q: close: %v", ls[i].Spec.Name, err)
		}
	}
}
