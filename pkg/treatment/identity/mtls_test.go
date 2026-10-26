package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// authProbe trusts the interceptor: it only reports what identity reached
// the handler, so a wrong answer here is the interceptor's fault.
type authProbe struct {
	pb.UnimplementedAgentControlServiceServer
	pb.UnimplementedOperatorServiceServer
}

func (authProbe) CheckIn(ctx context.Context, r *pb.CheckInRequest) (*pb.CheckInResponse, error) {
	id, ok := FromContext(ctx)
	if !ok || id.Role != RoleAgent || id.ID != r.AgentId {
		return nil, status.Error(codes.Internal, "interceptor did not resolve the agent")
	}
	return &pb.CheckInResponse{}, nil
}
func (authProbe) Inspect(ctx context.Context, _ *pb.InspectRequest) (*pb.InspectResponse, error) {
	id, ok := FromContext(ctx)
	if !ok || id.Role != RoleOperator {
		return nil, status.Error(codes.Internal, "interceptor did not resolve the operator")
	}
	return &pb.InspectResponse{}, nil
}

var probePolicy = Policy{
	"/xtcp_treatment.v1.AgentControlService/CheckIn": RoleAgent,
	"/xtcp_treatment.v1.OperatorService/Inspect":     RoleOperator,
}

func TestMTLSHandshakesAndRoleBoundaries(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	caKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	caFile := filepath.Join(dir, "ca.crt")
	if e = os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	issue := func(name, identity string, server bool) *tls.Config {
		t.Helper()
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(len(name) + 2)), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
		if server {
			leaf.DNSNames = []string{"controller.test"}
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		} else {
			u, e := url.Parse(identity)
			if e != nil {
				t.Fatal(e)
			}
			leaf.URIs = []*url.URL{u}
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		der, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, caKey)
		if e != nil {
			t.Fatal(e)
		}
		keyDER, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			t.Fatal(e)
		}
		certFile, keyFile := filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
		if e = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); e != nil {
			t.Fatal(e)
		}
		config, e := TLSConfig(certFile, keyFile, caFile, "controller.test", server)
		if e != nil {
			t.Fatal(e)
		}
		return config
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(issue("server", "", true))), grpc.ChainUnaryInterceptor(probePolicy.UnaryInterceptor()), grpc.ChainStreamInterceptor(probePolicy.StreamInterceptor()))
	pb.RegisterAgentControlServiceServer(server, authProbe{})
	pb.RegisterOperatorServiceServer(server, authProbe{})
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); listener.Close() })
	connect := func(config *tls.Config) *grpc.ClientConn {
		t.Helper()
		conn, e := grpc.NewClient("passthrough:///controller.test", grpc.WithTransportCredentials(credentials.NewTLS(config)), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	agentTLS := issue("agent", "spiffe://xtcp/agent/dc1/host-1", false)
	agent := connect(agentTLS)
	if _, e = pb.NewAgentControlServiceClient(agent).CheckIn(ctx, &pb.CheckInRequest{AgentId: "host-1"}); e != nil {
		t.Fatal("authenticated agent", e)
	}
	if _, e = pb.NewAgentControlServiceClient(agent).CheckIn(ctx, &pb.CheckInRequest{AgentId: "host-2"}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("cross-host access", e)
	}
	if _, e = pb.NewOperatorServiceClient(agent).Inspect(ctx, &pb.InspectRequest{}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("agent operator access", e)
	}
	operator := connect(issue("operator", "spiffe://xtcp/operator/alice", false))
	if _, e = pb.NewOperatorServiceClient(operator).Inspect(ctx, &pb.InspectRequest{}); e != nil {
		t.Fatal("authenticated operator", e)
	}
	if _, e = pb.NewOperatorServiceClient(operator).Propose(ctx, &pb.ProposeRequest{}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("undeclared method must fail closed", e)
	}
	for name, config := range map[string]*tls.Config{"missing_client_certificate": agentTLS.Clone(), "wrong_server_name": agentTLS.Clone(), "untrusted_server": agentTLS.Clone()} {
		t.Run(name, func(t *testing.T) {
			switch name {
			case "missing_client_certificate":
				config.Certificates = nil
			case "wrong_server_name":
				config.ServerName = "wrong.test"
			case "untrusted_server":
				config.RootCAs = x509.NewCertPool()
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, e := pb.NewAgentControlServiceClient(connect(config)).CheckIn(ctx, &pb.CheckInRequest{AgentId: "host-1"})
			if e == nil {
				t.Fatal("invalid TLS was accepted")
			}
		})
	}
}
