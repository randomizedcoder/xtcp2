package identity

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// TLSConfig loads a leaf key pair and a private CA bundle. Both ends pin TLS
// 1.3 and trust only that CA; servers additionally require and verify a client
// certificate so every RPC arrives with a peer identity for FromPeer.
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
	c := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		ServerName:   serverName,
	}
	if server {
		c.ClientCAs = roots
		c.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return c, nil
}

// Leaf returns the parsed leaf certificate of a config built by TLSConfig, so
// a client can check its own identity before dialing anything.
func Leaf(c *tls.Config) (*x509.Certificate, error) {
	if c == nil || len(c.Certificates) == 0 || len(c.Certificates[0].Certificate) == 0 {
		return nil, errors.New("tls config has no leaf certificate")
	}
	return x509.ParseCertificate(c.Certificates[0].Certificate[0])
}
