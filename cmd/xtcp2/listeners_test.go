package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listeners"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// stubListeners makes a runMain test bind nothing, and lets the default
// listener resolve without a token file. It returns the specs runMain asked
// to start.
func stubListeners(t *testing.T) *[]listeners.Spec {
	t.Helper()
	t.Setenv("LISTENER_AUTH_MODE", "disabled")
	var started []listeners.Spec
	prev := listenersStarter
	listenersStarter = func(_ context.Context, _ *sync.WaitGroup, specs []listeners.Spec, _ string) ([]xtcp.GRPCListener, error) {
		started = specs
		return nil, nil
	}
	t.Cleanup(func() { listenersStarter = prev })
	return &started
}

// TestConfigureListeners covers -listener / LISTENERS and the expansion of
// the deprecated per-listener settings.
func TestConfigureListeners(t *testing.T) {
	adminRaw := &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN}
	tests := []struct {
		name, category, description string
		specs                       []string
		env                         map[string]string
		legacy                      map[string][]string
		config                      func(c *xtcp_config.XtcpConfig)
		expected                    []string // FormatSpec of each listener; nil = none configured
		wantErr                     string
	}{
		{name: "none", category: "positive", description: "nothing set leaves the default to the daemon"},
		{name: "flag", category: "positive", description: "-listener sets the listeners",
			specs:    []string{"name=m,serves=health+metrics,address=/run/m.sock"},
			expected: []string{"name=m,serves=health+metrics,address=/run/m.sock"}},
		{name: "env_overrides_flag", category: "boundary", description: "LISTENERS wins over -listener",
			specs:    []string{"name=m,serves=health,address=/run/m.sock"},
			env:      map[string]string{"LISTENERS": "name=a,serves=health,address=/a.sock;name=b,serves=metrics,address=/b.sock"},
			expected: []string{"name=a,serves=health,address=/a.sock", "name=b,serves=metrics,address=/b.sock"}},
		{name: "both_forms", category: "negative", description: "-listener with a deprecated flag is a usage error",
			specs:   []string{"name=m,serves=health,address=/run/m.sock"},
			legacy:  map[string][]string{"prometheus": {"-promListen"}},
			wantErr: "both set"},
		{name: "bad_spec", category: "negative", description: "an unparsable spec is a usage error",
			specs: []string{"name=m,bogus=1"}, wantErr: "unknown key"},
		{name: "legacy_prom", category: "positive", description: "-promListen becomes a typed TCP listener without pprof",
			legacy:   map[string][]string{"prometheus": {"-promListen"}},
			expected: []string{"name=prometheus,serves=health+metrics+metrics-detail,address=:9088,network=tcp"}},
		{name: "legacy_prom_unix_ip", category: "corner", description: "a unix prom listener keeps /ip when ip has no listener of its own",
			legacy: map[string][]string{"prometheus": {"PROM_LISTEN_NETWORK"}},
			config: func(c *xtcp_config.XtcpConfig) {
				c.PrometheusListener = &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX, Address: "/run/p.sock", UnixSocketMode: 0o660}
				c.LinkMonitor = &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true}
			},
			expected: []string{"name=prometheus,serves=health+metrics+metrics-detail+ip,address=/run/p.sock,network=unix,mode=0660"}},
		{name: "legacy_grpc_keeps_default", category: "positive", description: "-grpcPort adds a gRPC listener next to the default socket, one credential for all",
			legacy: map[string][]string{"grpc": {"-grpcPort"}},
			expected: []string{"name=metrics,serves=health+metrics+metrics-detail,address=/run/xtcp2/metrics.sock,network=unix,mode=0600",
				"name=grpc,serves=grpc-admin,address=:8889,network=tcp"}},
		{name: "legacy_grpc_admin_split", category: "boundary", description: "with an admin credential the gRPC listener serves read and admin",
			legacy: map[string][]string{"grpc": {"GRPC_PORT"}},
			config: func(c *xtcp_config.XtcpConfig) { c.AdminListenerAuth = adminRaw },
			expected: []string{"name=metrics,serves=health+metrics+metrics-detail,address=/run/xtcp2/metrics.sock,network=unix,mode=0600",
				"name=grpc,serves=grpc-read+grpc-admin,address=:8889,network=tcp"}},
		{name: "legacy_ip_listen", category: "positive", description: "-linkMonitorIPListen becomes a loopback listener without auth",
			legacy: map[string][]string{"ip": {"-linkMonitorIPListen"}},
			config: func(c *xtcp_config.XtcpConfig) {
				c.LinkMonitor = &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: "127.0.0.1:9089"}
			},
			expected: []string{"name=metrics,serves=health+metrics+metrics-detail,address=/run/xtcp2/metrics.sock,network=unix,mode=0600",
				"name=ip,serves=ip,address=127.0.0.1:9089,network=tcp,auth=disabled"}},
		{name: "legacy_ip_not_loopback", category: "negative", description: "a non-loopback ip address is refused",
			legacy: map[string][]string{"ip": {"LINK_MONITOR_IP_LISTEN"}},
			config: func(c *xtcp_config.XtcpConfig) {
				c.LinkMonitor = &xtcp_config.LinkMonitorConfig{Enable: true, IpPaths: true, IpListen: "0.0.0.0:9089"}
			},
			wantErr: "loopback"},
		{name: "legacy_ip_monitor_off", category: "corner", description: "with the monitor off the ip listener is dropped",
			legacy: map[string][]string{"ip": {"-linkMonitorIPListen"}},
			config: func(c *xtcp_config.XtcpConfig) {
				c.LinkMonitor = &xtcp_config.LinkMonitorConfig{IpPaths: true, IpListen: "127.0.0.1:9089"}
			},
			expected: []string{"name=metrics,serves=health+metrics+metrics-detail,address=/run/xtcp2/metrics.sock,network=unix,mode=0600"}},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Setenv(listenersEnv, "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c := &xtcp_config.XtcpConfig{GrpcPort: grpcPortCst}
			if tc.config != nil {
				tc.config(c)
			}
			var err error
			out := captureLog(t, func() { err = configureListeners(c, tc.specs, tc.legacy, promListenCst) })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || exitCodeFor(err) != exitUsage {
					t.Fatalf("%s: err = %v (exit %d), want %q and exit 64", tc.description, err, exitCodeFor(err), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			var got []string
			for _, l := range c.GetListeners() {
				got = append(got, listeners.FormatSpec(l))
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s:\n got %q\nwant %q", tc.description, got, tc.expected)
			}
			if deprecated := strings.Contains(out, "DEPRECATED"); deprecated != (len(tc.legacy) > 0) {
				t.Errorf("%s: deprecation logged = %v, want %v (%q)", tc.description, deprecated, len(tc.legacy) > 0, out)
			}
		})
	}
}

// TestRunMain_listeners covers the listener outcomes of a run: what is
// started, and the usage errors that stop it before anything binds.
func TestRunMain_listeners(t *testing.T) {
	tests := []struct {
		name, category, description string
		args                        []string
		env                         map[string]string
		expected                    int
		started                     []string // names of the listeners asked to start
	}{
		{"default", "positive", "no listener config starts the default socket",
			nil, map[string]string{"LISTENER_AUTH_MODE": "disabled"}, exitOK, []string{"metrics"}},
		{"default_needs_token", "negative", "an empty LISTENER_AUTH_MODE is unset, so the default socket still needs a token",
			nil, map[string]string{"LISTENER_AUTH_MODE": ""}, exitUsage, nil},
		{"both_forms", "negative", "-listener with -promListen is a usage error",
			[]string{"-listener", "name=m,serves=health,address=/run/m.sock,auth=disabled", "-promListen", ":9000"},
			nil, exitUsage, nil},
		{"tcp_no_auth", "negative", "auth disabled on a non-loopback TCP listener is refused",
			[]string{"-listener", "name=m,serves=metrics,address=:9000,network=tcp,auth=disabled"}, nil, exitUsage, nil},
		{"missing_token_file", "negative", "a token file that does not exist is a usage error",
			[]string{"-listener", "name=m,serves=metrics,address=/run/m.sock,tokenFile=/nonexistent/token"}, nil, exitUsage, nil},
		{"health_only_tcp", "boundary", "a health-only TCP listener may run without auth",
			[]string{"-listener", "name=h,serves=health,address=:9000,network=tcp,auth=disabled"}, nil, exitOK, []string{"h"}},
		{"debug_without_admin", "corner", "debug next to metrics needs an admin credential",
			[]string{"-listener", "name=m,serves=metrics+debug,address=/run/m.sock,auth=disabled"}, nil, exitUsage, nil},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			envHelperReset(t)
			prevArgs := os.Args
			os.Args = append([]string{"xtcp2", "-dest", "null"}, tc.args...)
			t.Cleanup(func() { os.Args = prevArgs })
			started := stubListeners(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			prevDaemon := daemonRunner
			daemonRunner = func(context.Context, context.CancelFunc, *xtcp_config.XtcpConfig, []xtcp.GRPCListener) {}
			t.Cleanup(func() { daemonRunner = prevDaemon })
			var rc int
			captureLog(t, func() { rc = runMain(t.Context()) })
			if rc != tc.expected {
				t.Fatalf("%s: rc = %d, want %d", tc.description, rc, tc.expected)
			}
			names := make([]string, 0, len(*started))
			for _, s := range *started {
				names = append(names, s.Name)
			}
			if !slices.Equal(names, tc.started) {
				t.Errorf("%s: started %v, want %v", tc.description, names, tc.started)
			}
		})
	}
}
