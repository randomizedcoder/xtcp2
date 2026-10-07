package listener

import (
	"context"
	"errors"
	"io/fs"
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

// errCauses returns the causes errors.Join packed into err, or nothing if err
// is not a join at all. It is how the tables below tell "the listener's own
// error, un-joined" apart from "a join that happens to carry one cause":
// errors.Join discards nil arguments but still wraps a lone survivor, so the
// two states are indistinguishable by message and differ only here.
func errCauses(err error) []error {
	j, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	return j.Unwrap()
}

// TestUnixListenerClose_table covers unixListener.Close, whose discarded
// os.Remove is now joined into the returned error.
//
// The measured fact this table is built around, and which the plan for this
// change did not have: net.UnixListener.Close unlinks its path
// *unconditionally* and *discards* its own unlink error (src/net/unixsock.go,
// `if l.path[0] != '@' && l.unlink { syscall.Unlink(l.path) }`, whose result is
// never read). For any listener that came from listenUnix, that runs one line
// before our Lstat, so by the time the ModeSocket guard looks there is nothing
// at the path — whatever was there, socket or not, and whoever owned it.
//
// So the rows split in two, and each says which half it is in:
//
//   - rows built through Listen are the production path end to end. They show
//     the observable outcome, which is that the path is always gone and the
//     error is always the listener's own. Our os.Remove is unreachable on this
//     path, so none of them can exercise the join.
//   - rows built as a unixListener literal embed a TCP listener, which leaves
//     the path alone, so the guard and the Remove are the only actors. These
//     are the rows that can prove the guard refuses a non-socket and that the
//     join reports a failed unlink. The literal is the same technique
//     TestValidToken uses for states New cannot construct.
//
// This table uses `description` + `expected`. The older tables in this file
// predate that standard and still carry `description` + `expectedOutcome` +
// `want`.
func TestUnixListenerClose_table(t *testing.T) {
	ctx := context.Background()

	// standIn is a TCP listener used as the embedded net.Listener in the
	// literal rows: it is a listener that does not unlink our path, which no
	// unix listener is. Its Close is tolerated as already-closed because one
	// row closes it before measuring.
	standIn := func(t *testing.T) net.Listener {
		ln, err := net.Listen(NetworkTCP, "127.0.0.1:0")
		if err != nil {
			t.Fatalf("stand-in tcp listener: %v", err)
		}
		t.Cleanup(func() {
			if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("close stand-in tcp listener: %v", err)
			}
		})
		return ln
	}

	// foreignSocket binds a second listener at path, so the path holds a live
	// socket this test's subject does not own.
	foreignSocket := func(t *testing.T, path string) {
		ln, err := net.Listen(NetworkUnix, path)
		if err != nil {
			t.Fatalf("bind foreign socket %q: %v", path, err)
		}
		t.Cleanup(func() {
			if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Errorf("close foreign socket: %v", err)
			}
		})
	}

	// lockedDirSocket puts a live foreign socket in a directory with no write
	// bit, which is what makes os.Remove fail — the only way to reach the join
	// this change added. The chmod back is registered after the socket's own
	// cleanup so that it runs first (t.Cleanup is LIFO), or neither the unlink
	// nor t.TempDir's teardown could proceed.
	lockedDirSocket := func(t *testing.T) string {
		if os.Geteuid() == 0 {
			t.Skip("root ignores the directory write bit, so os.Remove cannot be made to fail this way")
		}
		dir := filepath.Join(t.TempDir(), "locked")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
		path := filepath.Join(dir, "foreign.sock")
		foreignSocket(t, path)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("strip write bit from %q: %v", dir, err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Errorf("restore write bit on %q: %v", dir, err)
			}
		})
		return path
	}

	// liveUnix is the production path: a listener from Listen, at a fresh path.
	liveUnix := func(t *testing.T, name string) (net.Listener, string) {
		path := filepath.Join(t.TempDir(), name)
		ln, err := Listen(ctx, Endpoint{
			Network:               NetworkUnix,
			Address:               path,
			UnlinkStaleUnixSocket: true,
		}, 0, 0)
		if err != nil {
			t.Fatalf("listen unix %q: %v", path, err)
		}
		return ln, path
	}

	tests := []struct {
		description string
		// build returns the listener under test and the path it claims.
		build func(t *testing.T) (net.Listener, string)
		// closeTwice calls Close once and requires it to succeed before the
		// measured call, so the measured call is the second one.
		closeTwice bool
		// betweenCloses runs after the first Close and before the measured one.
		// Only the rebind row needs it, and it is what makes once observable at
		// all: on every other row the first Close has already unlinked the
		// path, so a second unlink attempt would find nothing and look exactly
		// like once having suppressed it.
		betweenCloses func(t *testing.T, path string)
		// expectedErrIs holds every target errors.Is must match. Empty means
		// Close must return nil.
		expectedErrIs []error
		// expectedCauses is how many causes the returned error carries. 0 means
		// it is not a join at all — the listener's own error, untouched.
		expectedCauses int
		// expectedFirstIs is the first cause, which is the one a caller logs.
		// Only meaningful where expectedCauses is 2, and nil elsewhere.
		expectedFirstIs    error
		expectedPathExists bool
	}{
		{
			description: "positive: Listen path, a live socket closes cleanly and its path is unlinked",
			build: func(t *testing.T) (net.Listener, string) {
				return liveUnix(t, "live.sock")
			},
			expectedPathExists: false,
		},
		{
			description: "negative: Listen path, the socket was already removed before Close, and that is not an error — the stdlib discards its own failed unlink and our Lstat guard then declines to join an ENOENT of its own",
			build: func(t *testing.T) (net.Listener, string) {
				ln, path := liveUnix(t, "vanished.sock")
				if err := os.Remove(path); err != nil {
					t.Fatalf("pre-remove %q: %v", path, err)
				}
				return ln, path
			},
			expectedPathExists: false,
		},
		{
			description: "boundary: Listen path, a second Close returns the listener's own net.ErrClosed un-joined, because once already ran and no second unlink is attempted",
			build: func(t *testing.T) (net.Listener, string) {
				return liveUnix(t, "twice.sock")
			},
			closeTwice:         true,
			expectedErrIs:      []error{net.ErrClosed},
			expectedCauses:     0,
			expectedPathExists: false,
		},
		{
			description: "boundary: literal, once stops a second Close from unlinking a path that was rebound in between — without it the second Close would delete a socket that is no longer ours and report having done so",
			build: func(t *testing.T) (net.Listener, string) {
				path := filepath.Join(t.TempDir(), "rebound.sock")
				foreignSocket(t, path)
				return &unixListener{Listener: standIn(t), path: path}, path
			},
			closeTwice:         true,
			betweenCloses:      func(t *testing.T, path string) { foreignSocket(t, path) },
			expectedErrIs:      []error{net.ErrClosed},
			expectedCauses:     0,
			expectedPathExists: true,
		},
		{
			description: "corner: Listen path, a regular file at the path is removed anyway and Close still succeeds, because the stdlib unlinked it before the ModeSocket guard could refuse — the guard protects nothing on this path",
			build: func(t *testing.T) (net.Listener, string) {
				ln, path := liveUnix(t, "replaced.sock")
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove %q: %v", path, err)
				}
				if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
					t.Fatalf("write regular file %q: %v", path, err)
				}
				return ln, path
			},
			expectedPathExists: false,
		},
		{
			description: "corner: Listen path, a live socket belonging to another listener is removed anyway, for the same reason — Close reports nothing, and the other listener loses its path",
			build: func(t *testing.T) (net.Listener, string) {
				ln, path := liveUnix(t, "foreign.sock")
				if err := os.Remove(path); err != nil {
					t.Fatalf("remove %q: %v", path, err)
				}
				foreignSocket(t, path)
				return ln, path
			},
			expectedPathExists: false,
		},
		{
			description: "corner: literal, a regular file at the path survives Close and is not joined as an error, which is the ModeSocket guard isolated from the stdlib unlink that normally precedes it",
			build: func(t *testing.T) (net.Listener, string) {
				path := filepath.Join(t.TempDir(), "regular.sock")
				if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
					t.Fatalf("write regular file %q: %v", path, err)
				}
				return &unixListener{Listener: standIn(t), path: path}, path
			},
			expectedCauses:     0,
			expectedPathExists: true,
		},
		{
			description: "corner: literal, a live socket owned by another listener IS removed, because the guard distinguishes socket from non-socket and not whose socket it is — recorded as the behavior, not endorsed as the design",
			build: func(t *testing.T) (net.Listener, string) {
				path := filepath.Join(t.TempDir(), "foreign.sock")
				foreignSocket(t, path)
				return &unixListener{Listener: standIn(t), path: path}, path
			},
			expectedCauses:     0,
			expectedPathExists: false,
		},
		{
			description: "negative: literal, an unlink that fails with EACCES is reported instead of vanishing, which is the entire point of joining it — and the path is left behind for the next bind to trip over, which is what the error is warning about",
			build: func(t *testing.T) (net.Listener, string) {
				path := lockedDirSocket(t)
				return &unixListener{Listener: standIn(t), path: path}, path
			},
			expectedErrIs:      []error{fs.ErrPermission},
			expectedCauses:     1,
			expectedPathExists: true,
		},
		{
			description: "corner: literal, both the listener close and the unlink fail, so the chain carries both causes with the listener's own error first — the order a caller logs",
			build: func(t *testing.T) (net.Listener, string) {
				path := lockedDirSocket(t)
				ln := standIn(t)
				if err := ln.Close(); err != nil {
					t.Fatalf("pre-close stand-in listener: %v", err)
				}
				return &unixListener{Listener: ln, path: path}, path
			},
			expectedErrIs:      []error{net.ErrClosed, fs.ErrPermission},
			expectedCauses:     2,
			expectedFirstIs:    net.ErrClosed,
			expectedPathExists: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ln, path := tc.build(t)
			if tc.closeTwice {
				if err := ln.Close(); err != nil {
					t.Fatalf("first Close: %v", err)
				}
			}
			if tc.betweenCloses != nil {
				tc.betweenCloses(t, path)
			}
			err := ln.Close()

			if len(tc.expectedErrIs) == 0 {
				if err != nil {
					t.Fatalf("Close() = %v, want nil", err)
				}
			}
			for _, target := range tc.expectedErrIs {
				if !errors.Is(err, target) {
					t.Fatalf("Close() = %v, want errors.Is %v", err, target)
				}
			}
			if got := errCauses(err); len(got) != tc.expectedCauses {
				t.Fatalf("Close() carried %d causes %v, want %d", len(got), got, tc.expectedCauses)
			}
			if tc.expectedFirstIs != nil {
				if got := errCauses(err); !errors.Is(got[0], tc.expectedFirstIs) {
					t.Fatalf("Close()'s first cause = %v, want errors.Is %v", got[0], tc.expectedFirstIs)
				}
			}
			_, statErr := os.Lstat(path)
			if exists := statErr == nil; exists != tc.expectedPathExists {
				t.Fatalf("after Close, %q exists = %v (stat err %v), want exists = %v",
					path, exists, statErr, tc.expectedPathExists)
			}
		})
	}
}

// TestListenUnixChmodJoin_table covers the other half of this change: the
// chmod failure in listenUnix now joins ln.Close()'s result instead of
// discarding it.
//
// Reaching that branch at all needs a path that binds but cannot be chmod'd,
// and Linux's abstract namespace is exactly that. An address beginning with
// "@" is turned into a leading NUL by syscall.SockaddrUnix.sockaddr, so the
// socket exists only in the abstract namespace with no filesystem entry, and
// the chmod that follows the bind fails with ENOENT. That makes this a real
// reachable configuration rather than an injected fault, and incidentally
// states a limitation worth knowing: xtcp2 cannot serve on an abstract unix
// socket, because every unix listener it builds is chmod'd.
//
// This table uses `description` + `expected`.
func TestListenUnixChmodJoin_table(t *testing.T) {
	tests := []struct {
		description string
		address     func(t *testing.T) string
		// expectedErrIs is empty where Listen must succeed.
		expectedErrIs  []error
		expectedPrefix string // the error's leading text, so the cause stays first
		expectedCauses int    // 1, because ln.Close() succeeds and errors.Join drops the nil
	}{
		{
			description: "positive: a filesystem path binds, chmods and returns a listener, so the row below is isolating the chmod and nothing else",
			address: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "ok.sock")
			},
		},
		{
			description: "negative: an abstract-namespace address binds but cannot be chmod'd, and the returned error leads with the chmod cause while still carrying the close — errors.Is finds ENOENT, so joining did not bury it",
			address: func(*testing.T) string {
				return "@xtcp2-listener-chmod-join"
			},
			expectedErrIs:  []error{fs.ErrNotExist},
			expectedPrefix: `chmod unix listener socket "@xtcp2-listener-chmod-join"`,
			expectedCauses: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			address := tc.address(t)
			ln, err := Listen(context.Background(), Endpoint{
				Network:               NetworkUnix,
				Address:               address,
				UnlinkStaleUnixSocket: true,
			}, 0, 0)

			if len(tc.expectedErrIs) == 0 {
				if err != nil {
					t.Fatalf("Listen(%q) = %v, want nil", address, err)
				}
				if ln == nil {
					t.Fatalf("Listen(%q) returned a nil listener with a nil error", address)
				}
				if err := ln.Close(); err != nil {
					t.Fatalf("close %q: %v", address, err)
				}
				return
			}

			if ln != nil {
				t.Errorf("Listen(%q) returned a listener alongside an error; the bind was not undone", address)
			}
			for _, target := range tc.expectedErrIs {
				if !errors.Is(err, target) {
					t.Fatalf("Listen(%q) = %v, want errors.Is %v", address, err, target)
				}
			}
			if !strings.HasPrefix(err.Error(), tc.expectedPrefix) {
				t.Fatalf("Listen(%q) = %q, want it to lead with %q", address, err.Error(), tc.expectedPrefix)
			}
			if got := errCauses(err); len(got) != tc.expectedCauses {
				t.Fatalf("Listen(%q) carried %d causes %v, want %d", address, len(got), got, tc.expectedCauses)
			}
		})
	}
}
