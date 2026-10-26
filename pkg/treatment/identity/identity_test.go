package identity

import (
	"crypto/x509"
	"net/url"
	"strings"
	"testing"
)

func certWithURIs(t *testing.T, uris ...string) *x509.Certificate {
	t.Helper()
	c := &x509.Certificate{}
	for _, raw := range uris {
		u, e := url.Parse(raw)
		if e != nil {
			t.Fatal(e)
		}
		c.URIs = append(c.URIs, u)
	}
	return c
}

// go test ./pkg/treatment/identity -run TestFromCertificate
func TestFromCertificate(t *testing.T) {
	long := strings.Repeat("a", 128)
	tests := []struct {
		description string
		uris        []string
		expected    Identity
		wantErr     bool
	}{
		{
			description: "positive: agent SAN yields role, location and id",
			uris:        []string{"spiffe://xtcp/agent/dc1/host-1"},
			expected:    Identity{Role: RoleAgent, Location: "dc1", ID: "host-1"},
		},
		{
			description: "positive: operator SAN yields role and id with empty location",
			uris:        []string{"spiffe://xtcp/operator/alice"},
			expected:    Identity{Role: RoleOperator, ID: "alice"},
		},
		{
			description: "positive: foreign SANs are ignored around the one xtcp SAN",
			uris:        []string{"spiffe://other/agent/dc1/host-1", "https://example.com/x", "spiffe://xtcp/operator/bob"},
			expected:    Identity{Role: RoleOperator, ID: "bob"},
		},
		{
			description: "negative: no SAN at all",
			uris:        nil,
			wantErr:     true,
		},
		{
			description: "negative: two xtcp SANs are ambiguous and rejected",
			uris:        []string{"spiffe://xtcp/agent/dc1/host-1", "spiffe://xtcp/operator/alice"},
			wantErr:     true,
		},
		{
			description: "negative: agent SAN missing the location segment",
			uris:        []string{"spiffe://xtcp/agent/host-1"},
			wantErr:     true,
		},
		{
			description: "negative: operator SAN with an extra segment is not an agent either",
			uris:        []string{"spiffe://xtcp/operator/alice/extra"},
			wantErr:     true,
		},
		{
			description: "negative: unknown role segment",
			uris:        []string{"spiffe://xtcp/root/dc1/host-1"},
			wantErr:     true,
		},
		{
			description: "negative: wrong trust domain host",
			uris:        []string{"spiffe://xtcp2/agent/dc1/host-1"},
			wantErr:     true,
		},
		{
			description: "negative: query string is rejected even with a valid path",
			uris:        []string{"spiffe://xtcp/agent/dc1/host-1?x=1"},
			wantErr:     true,
		},
		{
			description: "negative: fragment is rejected even with a valid path",
			uris:        []string{"spiffe://xtcp/agent/dc1/host-1#f"},
			wantErr:     true,
		},
		{
			description: "boundary: id at the maximum valid length is accepted",
			uris:        []string{"spiffe://xtcp/operator/" + long},
			expected:    Identity{Role: RoleOperator, ID: long},
		},
		{
			description: "boundary: id one byte over the maximum is rejected",
			uris:        []string{"spiffe://xtcp/operator/" + long + "a"},
			wantErr:     true,
		},
		{
			description: "boundary: empty id segment is rejected",
			uris:        []string{"spiffe://xtcp/operator/"},
			wantErr:     true,
		},
		{
			// url.Parse lowercases the scheme, and x509 parses SANs with it,
			// so this is accepted. Recorded so the behavior is deliberate.
			description: "corner: upper-case scheme is normalized by the URL parser and accepted",
			uris:        []string{"SPIFFE://xtcp/agent/dc1/host-1"},
			expected:    Identity{Role: RoleAgent, Location: "dc1", ID: "host-1"},
		},
		{
			description: "corner: escaped path separators are not segment separators",
			uris:        []string{"spiffe://xtcp/agent/dc/host%2Fextra"},
			wantErr:     true,
		},
		{
			description: "corner: a trailing slash adds an empty segment and is rejected",
			uris:        []string{"spiffe://xtcp/agent/dc1/host-1/"},
			wantErr:     true,
		},
		{
			description: "corner: an identical SAN repeated twice still counts as two",
			uris:        []string{"spiffe://xtcp/operator/alice", "spiffe://xtcp/operator/alice"},
			wantErr:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := FromCertificate(certWithURIs(t, tc.uris...))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if got != (Identity{}) {
					t.Fatalf("identity must be zero on error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Fatalf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

// go test ./pkg/treatment/identity -run TestLeaf
func TestLeaf(t *testing.T) {
	if _, e := Leaf(nil); e == nil {
		t.Fatal("nil config must not yield a leaf")
	}
}
