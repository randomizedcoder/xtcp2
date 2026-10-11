package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
	grpchealth "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// Environment fallbacks for the admin listener and both credentials. The
// credential variables are shared with `go-link-monitor ctl`, so a client on
// the same host presents the same files the daemon checks.
const (
	adminNetworkEnvironment    = "GO_LINK_MONITOR_ADMIN_NETWORK"
	adminAddressEnvironment    = "GO_LINK_MONITOR_ADMIN_ADDR"
	adminSocketModeEnvironment = "GO_LINK_MONITOR_ADMIN_UNIX_SOCKET_MODE"
	readModeEnvironment        = "GO_LINK_MONITOR_AUTH_READ_MODE"
	readTokenEnvironment       = "GO_LINK_MONITOR_AUTH_READ_TOKEN_FILE"
	readHMACEnvironment        = "GO_LINK_MONITOR_AUTH_READ_HMAC_KEY_FILE"
	adminModeEnvironment       = "GO_LINK_MONITOR_AUTH_ADMIN_MODE"
	adminTokenEnvironment      = "GO_LINK_MONITOR_AUTH_ADMIN_TOKEN_FILE"
	adminHMACEnvironment       = "GO_LINK_MONITOR_AUTH_ADMIN_HMAC_KEY_FILE"

	defaultAdminSocketMode = "0660"
	defaultAdminSocket     = "/run/go-link-monitor/admin.sock"
)

// adminOptions configures the gRPC admin listener and the two credentials.
type adminOptions struct {
	network, address, socketMode string
	insecure                     bool
	read, admin                  credentialOptions
}

// credentialOptions names one credential. Secrets come only from files, never
// from flags or environment values, so they stay out of process listings.
type credentialOptions struct {
	mode, tokenFile, hmacKeyFile string
}

type envFallback struct {
	flag, environment string
	target            *string
}

// defineAdminFlags registers the admin and auth flags and returns their
// environment fallbacks.
func defineAdminFlags(flags *flag.FlagSet, o *adminOptions) []envFallback {
	flags.StringVar(&o.network, "admin.network", listener.NetworkUnix,
		"Admin gRPC listener network: unix or tcp; overrides "+adminNetworkEnvironment)
	flags.StringVar(&o.address, "admin.address", "",
		"Admin gRPC socket path or TCP address (empty disables, e.g. "+defaultAdminSocket+"); overrides "+adminAddressEnvironment)
	flags.StringVar(&o.socketMode, "admin.unix-socket-mode", defaultAdminSocketMode,
		"Admin Unix socket permission bits (octal); overrides "+adminSocketModeEnvironment)
	flags.BoolVar(&o.insecure, "admin.insecure", false,
		"Allow a non-loopback TCP admin listener without an admin credential")
	defineCredentialFlags(flags, "read", "metrics and read RPCs", &o.read)
	defineCredentialFlags(flags, "admin", "admin RPCs", &o.admin)
	return []envFallback{
		{"admin.network", adminNetworkEnvironment, &o.network},
		{"admin.address", adminAddressEnvironment, &o.address},
		{"admin.unix-socket-mode", adminSocketModeEnvironment, &o.socketMode},
		{"auth.read-mode", readModeEnvironment, &o.read.mode},
		{"auth.read-token-file", readTokenEnvironment, &o.read.tokenFile},
		{"auth.read-hmac-key-file", readHMACEnvironment, &o.read.hmacKeyFile},
		{"auth.admin-mode", adminModeEnvironment, &o.admin.mode},
		{"auth.admin-token-file", adminTokenEnvironment, &o.admin.tokenFile},
		{"auth.admin-hmac-key-file", adminHMACEnvironment, &o.admin.hmacKeyFile},
	}
}

func defineCredentialFlags(flags *flag.FlagSet, name, scope string, c *credentialOptions) {
	env := strings.ToUpper(name)
	flags.StringVar(&c.mode, "auth."+name+"-mode", "",
		"Credential for "+scope+": disabled, raw or hmac-utc-minute; overrides GO_LINK_MONITOR_AUTH_"+env+"_MODE")
	flags.StringVar(&c.tokenFile, "auth."+name+"-token-file", "",
		"File holding the "+name+" raw bearer token; overrides GO_LINK_MONITOR_AUTH_"+env+"_TOKEN_FILE")
	flags.StringVar(&c.hmacKeyFile, "auth."+name+"-hmac-key-file", "",
		"File holding the "+name+" HMAC shared key; overrides GO_LINK_MONITOR_AUTH_"+env+"_HMAC_KEY_FILE")
}

// listenerAuth reads the credential's secret file once, at startup. An unset
// mode with no files is disabled; files without a mode are an error.
func (c credentialOptions) listenerAuth(name string) (*xtcp_config.ListenerAuth, error) {
	if c.mode == "" {
		if c.tokenFile != "" || c.hmacKeyFile != "" {
			return nil, fmt.Errorf("auth.%s-mode is required when a %s secret file is set", name, name)
		}
		return nil, nil
	}
	mode, err := listenerauth.ParseMode(c.mode)
	if err != nil {
		return nil, fmt.Errorf("auth.%s-mode: %w", name, err)
	}
	secrets, err := listenerauth.ClientAuthFromValues("", c.tokenFile, "", c.hmacKeyFile)
	if err != nil {
		return nil, fmt.Errorf("auth.%s secret: %w", name, err)
	}
	return &xtcp_config.ListenerAuth{Mode: mode, RawToken: secrets.RawToken, HmacSharedKey: secrets.HMACSharedKey}, nil
}

func enabled(a *xtcp_config.ListenerAuth) bool {
	return a != nil && a.GetMode() != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED &&
		a.GetMode() != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED
}

// accessConfig is the validated result of adminOptions.
type accessConfig struct {
	policy   *listenerauth.Policy
	endpoint *listener.Endpoint // nil when the admin listener is disabled
}

// resolve validates the admin options, reads the secrets and builds the
// access policy, all before any listener exists.
func (o adminOptions) resolve() (accessConfig, error) {
	read, err := o.read.listenerAuth("read")
	if err != nil {
		return accessConfig{}, err
	}
	admin, err := o.admin.listenerAuth("admin")
	if err != nil {
		return accessConfig{}, err
	}
	policy, err := listenerauth.NewPolicy(read, admin,
		listenerauth.StandardMethods().Merge(listenerauth.ServiceMethods(linkmonitorgrpc.Descriptor())))
	if err != nil {
		return accessConfig{}, err
	}
	if o.address == "" {
		if enabled(admin) {
			return accessConfig{}, errors.New("auth.admin-mode requires admin.address")
		}
		return accessConfig{policy: policy}, nil
	}
	mode, err := strconv.ParseUint(o.socketMode, 8, 32)
	if err != nil || mode > 0o777 {
		return accessConfig{}, fmt.Errorf("admin.unix-socket-mode %q must be octal permission bits", o.socketMode)
	}
	ep := listener.Endpoint{Network: o.network, Address: o.address, UnixSocketMode: os.FileMode(mode), UnlinkStaleUnixSocket: true}
	switch o.network {
	case listener.NetworkUnix:
	case listener.NetworkTCP:
		if !loopback(o.address) && !enabled(admin) && !o.insecure {
			return accessConfig{}, fmt.Errorf("admin.address %q is not loopback: set auth.admin-mode or admin.insecure", o.address)
		}
	default:
		return accessConfig{}, fmt.Errorf("admin.network %q must be unix or tcp", o.network)
	}
	return accessConfig{policy: policy, endpoint: &ep}, nil
}

// loopback reports whether a TCP listen address binds only loopback. An empty
// host binds every interface.
func loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// grpcServer is the part of *grpc.Server the lifecycle drives.
type grpcServer interface {
	Serve(net.Listener) error
	GracefulStop()
	Stop()
}

// adminEndpoint binds and serves the admin gRPC listener.
type adminEndpoint struct {
	listen func(context.Context) (net.Listener, error)
	server grpcServer
	name   string
}

// newAdminEndpoint builds the gRPC server for ep. Metrics for the service go
// to reg, which the caller serves on the full exposition.
func newAdminEndpoint(ep listener.Endpoint, policy *listenerauth.Policy, m linkmonitorgrpc.Monitor,
	logger *slog.Logger, reg prometheus.Registerer,
) (*adminEndpoint, error) {
	svc, err := linkmonitorgrpc.NewServer(m, linkmonitorgrpc.WithLogger(logger), linkmonitorgrpc.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	var opts []grpc.ServerOption
	if ep.Network == listener.NetworkUnix {
		opts = append(opts, grpc.Creds(newPeerCredentials()))
	}
	if policy.Enabled() {
		opts = append(opts, grpc.ChainUnaryInterceptor(policy.UnaryServerInterceptor()),
			grpc.ChainStreamInterceptor(policy.StreamServerInterceptor()))
	}
	s := grpc.NewServer(opts...)
	pb.RegisterLinkMonitorServiceServer(s, svc)
	health := grpchealth.NewServer()
	health.SetServingStatus(pb.LinkMonitorService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s, health)
	reflection.Register(s)
	return &adminEndpoint{
		listen: func(ctx context.Context) (net.Listener, error) { return listener.Listen(ctx, ep, 0, 0) },
		server: s,
		name:   ep.Network + ":" + ep.Address,
	}, nil
}
