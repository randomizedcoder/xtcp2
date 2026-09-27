package listener

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

func TestProtoEndpoint_table(t *testing.T) {
	tests := []struct {
		description     string
		network         string
		address         string
		mode            uint
		unlinkStale     bool
		wantNil         bool
		wantNetwork     xtcp_config.ListenerNetwork
		expectedOutcome string
		wantErr         string
	}{
		{
			description:     "empty network and address leaves endpoint unset",
			wantNil:         true,
			expectedOutcome: "no proto endpoint is created",
		},
		{
			description:     "tcp network is parsed",
			network:         "tcp",
			address:         "127.0.0.1:0",
			wantNetwork:     xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP,
			expectedOutcome: "endpoint uses TCP",
		},
		{
			description:     "unix network is parsed case-insensitively",
			network:         "UNIX",
			address:         "/tmp/xtcp.sock",
			mode:            0o660,
			unlinkStale:     true,
			wantNetwork:     xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX,
			expectedOutcome: "endpoint uses UDS with the requested mode",
		},
		{
			description:     "unknown network is rejected",
			network:         "udp",
			address:         "127.0.0.1:0",
			expectedOutcome: "configuration is rejected before listen",
			wantErr:         "unsupported listener network",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ProtoEndpoint(tt.network, tt.address, tt.mode, tt.unlinkStale)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected outcome %q: err=%v, want substring %q", tt.expectedOutcome, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected outcome %q: unexpected err=%v", tt.expectedOutcome, err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected outcome %q: endpoint=%+v, want nil", tt.expectedOutcome, got)
				}
				return
			}
			if got.GetNetwork() != tt.wantNetwork {
				t.Fatalf("expected outcome %q: network=%v, want %v", tt.expectedOutcome, got.GetNetwork(), tt.wantNetwork)
			}
			if got.GetAddress() != tt.address {
				t.Fatalf("expected outcome %q: address=%q, want %q", tt.expectedOutcome, got.GetAddress(), tt.address)
			}
			if got.GetUnixSocketMode() != uint32(tt.mode) {
				t.Fatalf("expected outcome %q: mode=%#o, want %#o", tt.expectedOutcome, got.GetUnixSocketMode(), tt.mode)
			}
		})
	}
}

func TestEndpointFromProto_table(t *testing.T) {
	tests := []struct {
		description     string
		proto           *xtcp_config.ListenerEndpoint
		defaultNetwork  string
		defaultAddress  string
		want            Endpoint
		expectedOutcome string
		wantErr         string
	}{
		{
			description:    "nil proto derives legacy tcp endpoint",
			defaultNetwork: NetworkTCP,
			defaultAddress: ":8889",
			want: Endpoint{
				Network:               NetworkTCP,
				Address:               ":8889",
				UnixSocketMode:        DefaultUnixSocketMode,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "legacy TCP defaults are preserved",
		},
		{
			description: "explicit unix endpoint uses provided path and mode",
			proto: &xtcp_config.ListenerEndpoint{
				Network:               xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX,
				Address:               "/run/xtcp2/grpc.sock",
				UnixSocketMode:        0o660,
				UnlinkStaleUnixSocket: true,
			},
			defaultNetwork: NetworkTCP,
			defaultAddress: ":8889",
			want: Endpoint{
				Network:               NetworkUnix,
				Address:               "/run/xtcp2/grpc.sock",
				UnixSocketMode:        0o660,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "UDS config overrides legacy TCP",
		},
		{
			description: "explicit tcp address overrides default address",
			proto: &xtcp_config.ListenerEndpoint{
				Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP,
				Address: "127.0.0.1:7777",
			},
			defaultNetwork: NetworkTCP,
			defaultAddress: ":8889",
			want: Endpoint{
				Network:               NetworkTCP,
				Address:               "127.0.0.1:7777",
				UnixSocketMode:        DefaultUnixSocketMode,
				UnlinkStaleUnixSocket: false,
			},
			expectedOutcome: "TCP config overrides legacy address",
		},
		{
			description:     "empty resolved address is rejected",
			proto:           &xtcp_config.ListenerEndpoint{Network: xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX},
			defaultNetwork:  NetworkTCP,
			expectedOutcome: "missing UDS path fails before listen",
			wantErr:         "listener address is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EndpointFromProto(tt.proto, tt.defaultNetwork, tt.defaultAddress)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected outcome %q: err=%v, want substring %q", tt.expectedOutcome, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected outcome %q: unexpected err=%v", tt.expectedOutcome, err)
			}
			if got != tt.want {
				t.Fatalf("expected outcome %q: got=%+v, want=%+v", tt.expectedOutcome, got, tt.want)
			}
		})
	}
}

func TestListenUnix_table(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	stalePath := filepath.Join(tmp, "stale.sock")
	stale, err := net.Listen(NetworkUnix, stalePath)
	if err != nil {
		t.Fatalf("create stale unix socket: %v", err)
	}
	if err := stale.Close(); err != nil {
		t.Fatalf("close stale unix socket: %v", err)
	}

	regularPath := filepath.Join(tmp, "regular.sock")
	if err := os.WriteFile(regularPath, []byte("not a socket"), 0o600); err != nil {
		t.Fatalf("create regular file: %v", err)
	}

	tests := []struct {
		description     string
		endpoint        Endpoint
		expectedOutcome string
		wantMode        os.FileMode
		wantErr         string
	}{
		{
			description: "new unix socket gets default secure mode",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				Address:               filepath.Join(tmp, "default.sock"),
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "listener starts and socket mode is 0600",
			wantMode:        0o600,
		},
		{
			description: "new unix socket gets explicit group mode",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				Address:               filepath.Join(tmp, "group.sock"),
				UnixSocketMode:        0o660,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "listener starts and socket mode is 0660",
			wantMode:        0o660,
		},
		{
			description: "stale unix socket is unlinked when configured",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				Address:               stalePath,
				UnixSocketMode:        0o600,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "old socket is replaced",
			wantMode:        0o600,
		},
		{
			description: "regular file at socket path is rejected",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				Address:               regularPath,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "non-socket path is left untouched",
			wantErr:         "exists and is not a socket",
		},
		{
			description: "missing parent directory is rejected",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				Address:               filepath.Join(tmp, "missing", "grpc.sock"),
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "startup fails with parent-directory error",
			wantErr:         "unix listener parent",
		},
		{
			description: "empty unix path is rejected",
			endpoint: Endpoint{
				Network:               NetworkUnix,
				UnlinkStaleUnixSocket: true,
			},
			expectedOutcome: "startup fails before filesystem operations",
			wantErr:         "address is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			ln, err := Listen(ctx, tt.endpoint, 0, 0)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected outcome %q: err=%v, want substring %q", tt.expectedOutcome, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected outcome %q: unexpected err=%v", tt.expectedOutcome, err)
			}
			st, err := os.Lstat(tt.endpoint.Address)
			if err != nil {
				t.Fatalf("expected outcome %q: stat socket: %v", tt.expectedOutcome, err)
			}
			if st.Mode().Perm() != tt.wantMode {
				t.Fatalf("expected outcome %q: mode=%#o, want %#o", tt.expectedOutcome, st.Mode().Perm(), tt.wantMode)
			}
			if err := ln.Close(); err != nil {
				t.Fatalf("expected outcome %q: close listener: %v", tt.expectedOutcome, err)
			}
			if _, err := os.Lstat(tt.endpoint.Address); !os.IsNotExist(err) {
				t.Fatalf("expected outcome %q: socket should be removed on close, stat err=%v", tt.expectedOutcome, err)
			}
		})
	}
}

func TestListenTCP_table(t *testing.T) {
	tests := []struct {
		description     string
		endpoint        Endpoint
		expectedOutcome string
		wantErr         string
	}{
		{
			description:     "tcp listener binds an OS-picked port",
			endpoint:        Endpoint{Network: NetworkTCP, Address: "127.0.0.1:0"},
			expectedOutcome: "listener starts on TCP",
		},
		{
			description:     "unsupported network is rejected",
			endpoint:        Endpoint{Network: "udp", Address: "127.0.0.1:0"},
			expectedOutcome: "configuration fails before bind",
			wantErr:         "unsupported listener network",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			ln, err := Listen(context.Background(), tt.endpoint, 0, 0)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected outcome %q: err=%v, want substring %q", tt.expectedOutcome, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected outcome %q: unexpected err=%v", tt.expectedOutcome, err)
			}
			if err := ln.Close(); err != nil {
				t.Fatalf("expected outcome %q: close listener: %v", tt.expectedOutcome, err)
			}
		})
	}
}
