package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Certificates bind agent identity AND enrollment location. Merely presenting
// any certificate from the CA does not confer operator permissions.
// URI SANs: spiffe://xtcp/agent/<location>/<id> or spiffe://xtcp/operator/<id>.
type Identity struct{ Role, ID, Location string }

func CertificateIdentity(cert *x509.Certificate) (Identity, error) {
	var found []Identity
	for _, u := range cert.URIs {
		if u.Scheme != "spiffe" || u.Host != "xtcp" || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		p := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(p) == 3 && p[0] == "agent" && treatment.ValidID(p[1]) && treatment.ValidID(p[2]) {
			found = append(found, Identity{Role: p[0], Location: p[1], ID: p[2]})
		}
		if len(p) == 2 && p[0] == "operator" && treatment.ValidID(p[1]) {
			found = append(found, Identity{Role: p[0], ID: p[1]})
		}
	}
	if len(found) != 1 {
		return Identity{}, errors.New("exactly one xtcp identity URI SAN required")
	}
	return found[0], nil
}
func identity(ctx context.Context) (Identity, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Identity{}, status.Error(codes.Unauthenticated, "mTLS required")
	}
	t, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(t.State.VerifiedChains) == 0 || len(t.State.PeerCertificates) == 0 {
		return Identity{}, status.Error(codes.Unauthenticated, "verified client certificate required")
	}
	id, e := CertificateIdentity(t.State.PeerCertificates[0])
	if e != nil {
		return Identity{}, status.Error(codes.PermissionDenied, e.Error())
	}
	return id, nil
}
func AgentIdentity(ctx context.Context, want string) (Identity, error) {
	id, e := identity(ctx)
	if e != nil {
		return id, e
	}
	if id.Role != "agent" || id.ID != want {
		return Identity{}, status.Error(codes.PermissionDenied, "agent identity mismatch")
	}
	return id, nil
}
func OperatorIdentity(ctx context.Context) (string, error) {
	id, e := identity(ctx)
	if e != nil {
		return "", e
	}
	if id.Role != "operator" {
		return "", status.Error(codes.PermissionDenied, "operator certificate required")
	}
	return id.ID, nil
}

func TLSConfig(certFile, keyFile, caFile, serverName string, server bool) (*tls.Config, error) {
	cert, e := tls.LoadX509KeyPair(certFile, keyFile)
	if e != nil {
		return nil, e
	}
	ca, e := os.ReadFile(caFile)
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("CA file has no certificates")
	}
	c := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, RootCAs: roots, ServerName: serverName}
	if server {
		c.ClientCAs = roots
		c.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return c, nil
}
