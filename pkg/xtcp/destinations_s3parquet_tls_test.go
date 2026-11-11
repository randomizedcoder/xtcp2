//go:build dest_s3parquet

package xtcp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCAPEM returns a freshly generated self-signed CA certificate in PEM.
func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "xtcp2 test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestS3Transport(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	goodCA := write("ca.pem", testCAPEM(t))
	notPEM := write("junk.pem", []byte("not a certificate"))

	cases := []struct {
		name        string
		category    string
		description string
		secure      bool
		caFile      string
		expected    string // "tls", "tls+ca", "plain", "error"
	}{
		{"tls_default", "positive", "TLS with system roots and the 1.2 floor", true, "", "tls"},
		{"tls_ca_file", "positive", "a private CA is trusted on top", true, goodCA, "tls+ca"},
		{"plaintext", "boundary", "plaintext has no TLS config to pin", false, goodCA, "plain"},
		{"ca_missing", "negative", "an unreadable CA file is an error", true, filepath.Join(dir, "nope.pem"), "error"},
		{"ca_not_pem", "corner", "a CA file with no certificate is an error", true, notPEM, "error"},
	}
	for _, tc := range cases {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			tr, err := s3Transport(tc.secure, tc.caFile)
			if tc.expected == "error" {
				if err == nil {
					t.Fatalf("%s: want an error", tc.description)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			if tc.expected == "plain" {
				if tr.TLSClientConfig != nil {
					t.Errorf("%s: plaintext transport has a TLS config", tc.description)
				}
				return
			}
			if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
				t.Fatalf("%s: TLS floor not pinned to 1.2: %+v", tc.description, tr.TLSClientConfig)
			}
			if got := tr.TLSClientConfig.RootCAs != nil; tc.expected == "tls+ca" && !got {
				t.Errorf("%s: CA file not installed as RootCAs", tc.description)
			}
		})
	}
}
