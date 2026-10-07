// Package listener builds the network listeners the xtcp2 gRPC, Prometheus and
// healthcheck endpoints are served on, for both TCP and unix domain sockets.
//
// It converts an xtcp_config.ListenerEndpoint into a concrete net.Listener,
// applying the IPv4 TTL / IPv6 hop-limit socket options for TCP and, for unix
// sockets, the parent-directory check, the stale-socket unlink policy and the
// socket mode (DefaultUnixSocketMode, 0o600). A unix listener returned from
// Listen unlinks its own path on Close, once, and only if the path is still a
// socket.
package listener

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/ipsockopt"
)

const (
	NetworkTCP  = "tcp"
	NetworkUnix = "unix"

	DefaultUnixSocketMode os.FileMode = 0o600
)

type Endpoint struct {
	Network               string
	Address               string
	UnixSocketMode        os.FileMode
	UnlinkStaleUnixSocket bool
}

func ParseNetwork(s string) (xtcp_config.ListenerNetwork, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "unspecified":
		return xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNSPECIFIED, nil
	case NetworkTCP:
		return xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP, nil
	case NetworkUnix:
		return xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX, nil
	default:
		return xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNSPECIFIED, fmt.Errorf("unsupported listener network %q", s)
	}
}

func NetworkString(n xtcp_config.ListenerNetwork, defaultNetwork string) (string, error) {
	switch n {
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNSPECIFIED:
		if defaultNetwork == "" {
			return NetworkTCP, nil
		}
		return defaultNetwork, nil
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP:
		return NetworkTCP, nil
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX:
		return NetworkUnix, nil
	default:
		return "", fmt.Errorf("unsupported listener network %s", n.String())
	}
}

func EndpointFromProto(ep *xtcp_config.ListenerEndpoint, defaultNetwork, defaultAddress string) (Endpoint, error) {
	network := defaultNetwork
	address := defaultAddress
	mode := DefaultUnixSocketMode
	unlinkStale := true

	if ep != nil {
		var err error
		network, err = NetworkString(ep.GetNetwork(), defaultNetwork)
		if err != nil {
			return Endpoint{}, err
		}
		if ep.GetAddress() != "" {
			address = ep.GetAddress()
		}
		if ep.GetUnixSocketMode() != 0 {
			mode = os.FileMode(ep.GetUnixSocketMode())
		}
		unlinkStale = ep.GetUnlinkStaleUnixSocket()
	}
	if network == "" {
		network = NetworkTCP
	}
	if address == "" {
		return Endpoint{}, fmt.Errorf("%s listener address is empty", network)
	}
	return Endpoint{
		Network:               network,
		Address:               address,
		UnixSocketMode:        mode,
		UnlinkStaleUnixSocket: unlinkStale,
	}, nil
}

func ProtoEndpoint(networkValue, address string, unixSocketMode uint, unlinkStale bool) (*xtcp_config.ListenerEndpoint, error) {
	network, err := ParseNetwork(networkValue)
	if err != nil {
		return nil, err
	}
	if network == xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNSPECIFIED && address == "" {
		return nil, nil
	}
	return &xtcp_config.ListenerEndpoint{
		Network:               network,
		Address:               address,
		UnixSocketMode:        uint32(unixSocketMode),
		UnlinkStaleUnixSocket: unlinkStale,
	}, nil
}

func Listen(ctx context.Context, ep Endpoint, ipv4TTL, ipv6HopLimit uint32) (net.Listener, error) {
	switch ep.Network {
	case NetworkTCP:
		lc := net.ListenConfig{Control: ipsockopt.Control(ipv4TTL, ipv6HopLimit)}
		return lc.Listen(ctx, NetworkTCP, ep.Address)
	case NetworkUnix:
		return listenUnix(ctx, ep)
	default:
		return nil, fmt.Errorf("unsupported listener network %q", ep.Network)
	}
}

func listenUnix(ctx context.Context, ep Endpoint) (net.Listener, error) {
	if ep.Address == "" {
		return nil, fmt.Errorf("unix listener address is empty")
	}
	if parent := filepath.Dir(ep.Address); parent != "." {
		if st, err := os.Stat(parent); err != nil {
			return nil, fmt.Errorf("unix listener parent %q: %w", parent, err)
		} else if !st.IsDir() {
			return nil, fmt.Errorf("unix listener parent %q is not a directory", parent)
		}
	}
	if st, err := os.Lstat(ep.Address); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("unix listener path %q exists and is not a socket", ep.Address)
		}
		if !ep.UnlinkStaleUnixSocket {
			return nil, fmt.Errorf("unix listener path %q already exists", ep.Address)
		}
		if err := os.Remove(ep.Address); err != nil {
			return nil, fmt.Errorf("remove stale unix listener socket %q: %w", ep.Address, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat unix listener path %q: %w", ep.Address, err)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, NetworkUnix, ep.Address)
	if err != nil {
		return nil, err
	}
	mode := ep.UnixSocketMode
	if mode == 0 {
		mode = DefaultUnixSocketMode
	}
	if err := os.Chmod(ep.Address, mode); err != nil {
		// The chmod error stays first so errors.Is still matches it: a caller
		// diagnosing "socket came up with the wrong mode" must not have to dig
		// past a close error to find the cause. Join reports the close failure
		// too, which previously vanished and would have left the listener bound
		// to a path this function claims it did not create.
		return nil, errors.Join(fmt.Errorf("chmod unix listener socket %q: %w", ep.Address, err), ln.Close())
	}
	return &unixListener{Listener: ln, path: ep.Address}, nil
}

type unixListener struct {
	net.Listener
	path string
	once sync.Once
}

// Close closes the listener and unlinks its socket path, reporting both.
//
// The unlink error is joined rather than discarded, which is a real behavior
// change and the point of it: a socket that fails to unlink was previously
// invisible, and it leaves behind a path that the next bind trips over with a
// much less obvious "already exists" error at a point far from the cause.
//
// The Lstat guard decides whether to unlink at all, and both of its refusals
// are deliberate non-errors: a path that is already gone is the normal outcome
// when something else cleaned up, and a path that is no longer a socket belongs
// to somebody else and must not be removed. Neither joins anything, so Close
// does not invent an error for a state it is choosing to tolerate.
//
// Be careful what that guard is worth, though, because it is less than it
// looks. net.UnixListener.Close unlinks its own path first, unconditionally and
// whatever is sitting there, and it discards the result of that unlink. So for
// every listener listenUnix built, the path is already gone one line above and
// this guard finds nothing — see the measured rows in
// TestUnixListenerClose_table, where a regular file and another process's
// socket are both removed by the stdlib before the guard is consulted. What is
// left for the Remove here is the case the stdlib's unlink failed, which is
// also the only case the join can report. Narrow, and the narrowness is the
// reason it is reported rather than discarded: it fires when something is
// actually wrong with the directory.
//
// once makes a second Close a no-op here, so the unlink error is reported from
// the call that attempted it and is never doubled — and so a path rebound by
// somebody else between two Closes is not unlinked out from under them.
func (l *unixListener) Close() error {
	err := l.Listener.Close()
	l.once.Do(func() {
		if st, statErr := os.Lstat(l.path); statErr == nil && st.Mode()&os.ModeSocket != 0 {
			err = errors.Join(err, os.Remove(l.path))
		}
	})
	return err
}
