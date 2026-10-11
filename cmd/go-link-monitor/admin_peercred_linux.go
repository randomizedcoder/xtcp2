package main

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"google.golang.org/grpc/credentials"
)

// peerCredentials is plaintext transport credentials for the admin Unix
// socket that records the caller's SO_PEERCRED identity, so the audit log
// names a uid and pid rather than an empty socket address.
type peerCredentials struct{}

// peerAuthInfo implements linkmonitorgrpc.PeerIdentity.
type peerAuthInfo struct {
	credentials.CommonAuthInfo
	identity string
}

func (peerAuthInfo) AuthType() string { return "unix-peercred" }

func (a peerAuthInfo) PeerIdentity() string { return a.identity }

func newPeerAuthInfo(identity string) peerAuthInfo {
	return peerAuthInfo{CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}, identity: identity}
}

func (peerCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, newPeerAuthInfo(peerIdentity(conn)), nil
}

// peerIdentity reads SO_PEERCRED once per connection; failures fall back to
// "unix" so the audit record is still written.
func peerIdentity(conn net.Conn) string {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return listener.NetworkUnix
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return listener.NetworkUnix
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || credErr != nil {
		return listener.NetworkUnix
	}
	return fmt.Sprintf("uid=%d pid=%d", cred.Uid, cred.Pid)
}

func (peerCredentials) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, newPeerAuthInfo("server"), nil
}

func (peerCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "insecure"}
}

func (peerCredentials) Clone() credentials.TransportCredentials { return peerCredentials{} }

//nolint:staticcheck // OverrideServerName is deprecated but still part of the interface.
func (peerCredentials) OverrideServerName(string) error { return nil }

func newPeerCredentials() credentials.TransportCredentials { return peerCredentials{} }
