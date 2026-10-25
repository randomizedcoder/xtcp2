// Package identity is the shared mTLS identity contract between the treatment
// controller, agents, operator tooling and tests. It is deliberately a leaf:
// it knows about certificates, SPIFFE URI SANs and gRPC peer metadata, and
// nothing about persistence, experiments or the wire schema.
//
// Certificates bind agent identity AND enrollment location. Merely presenting
// any certificate from the CA does not confer operator permissions.
// URI SANs: spiffe://xtcp/agent/<location>/<id> or spiffe://xtcp/operator/<id>.
package identity

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Role is the principal class encoded in the certificate URI SAN.
type Role string

const (
	RoleAgent    Role = "agent"
	RoleOperator Role = "operator"
)

// Identity is the parsed principal. Location is empty for operators.
type Identity struct {
	Role     Role
	ID       string
	Location string
}

// ErrNoIdentity is returned when a certificate does not carry exactly one
// xtcp identity URI SAN. Two SANs are as invalid as zero: ambiguity fails.
var ErrNoIdentity = errors.New("exactly one xtcp identity URI SAN required")

// FromCertificate parses the single xtcp SPIFFE URI SAN of a leaf certificate.
func FromCertificate(cert *x509.Certificate) (Identity, error) {
	var found []Identity
	for _, u := range cert.URIs {
		if u.Scheme != "spiffe" || u.Host != "xtcp" || u.RawQuery != "" || u.Fragment != "" {
			continue
		}
		p := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		switch {
		case len(p) == 3 && p[0] == string(RoleAgent) && treatment.ValidID(p[1]) && treatment.ValidID(p[2]):
			found = append(found, Identity{Role: RoleAgent, Location: p[1], ID: p[2]})
		case len(p) == 2 && p[0] == string(RoleOperator) && treatment.ValidID(p[1]):
			found = append(found, Identity{Role: RoleOperator, ID: p[1]})
		}
	}
	if len(found) != 1 {
		return Identity{}, ErrNoIdentity
	}
	return found[0], nil
}

// FromPeer resolves the verified client certificate of the gRPC peer. It is
// the only function here that reads the context; errors already carry gRPC
// status codes so handlers and interceptors can return them unchanged.
func FromPeer(ctx context.Context) (Identity, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return Identity{}, status.Error(codes.Unauthenticated, "mTLS required")
	}
	t, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(t.State.VerifiedChains) == 0 || len(t.State.PeerCertificates) == 0 {
		return Identity{}, status.Error(codes.Unauthenticated, "verified client certificate required")
	}
	id, e := FromCertificate(t.State.PeerCertificates[0])
	if e != nil {
		return Identity{}, status.Error(codes.PermissionDenied, e.Error())
	}
	return id, nil
}

// Agent requires the peer to be the agent named by want. Agents may only
// speak for themselves; the controller passes the request's agent_id here.
func Agent(ctx context.Context, want string) (Identity, error) {
	id, e := FromPeer(ctx)
	if e != nil {
		return id, e
	}
	if id.Role != RoleAgent || id.ID != want {
		return Identity{}, status.Error(codes.PermissionDenied, "agent identity mismatch")
	}
	return id, nil
}

// Operator requires an operator certificate and returns the operator ID,
// which callers record as the audit actor.
func Operator(ctx context.Context) (string, error) {
	id, e := FromPeer(ctx)
	if e != nil {
		return "", e
	}
	if id.Role != RoleOperator {
		return "", status.Error(codes.PermissionDenied, "operator certificate required")
	}
	return id.ID, nil
}
