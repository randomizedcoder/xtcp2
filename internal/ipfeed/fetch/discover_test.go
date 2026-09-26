package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiscover(t *testing.T) {
	// A page containing a dated Azure ServiceTags link.
	page := `<html><a href="https://download.microsoft.com/download/a/b/ServiceTags_Public_20260907.json">dl</a></html>`
	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(page))
	}))
	defer pageSrv.Close()
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>no link here</html>"))
	}))
	defer emptySrv.Close()

	c := NewClient(Options{MaxAttempts: 1, Jitter: func(time.Duration) time.Duration { return 0 },
		Sleep: func(context.Context, time.Duration) bool { return true }})

	tests := []struct {
		name    string
		desc    string
		class   string
		mode    string
		url     string
		want    string
		wantErr bool
	}{
		{"positive_azure", "positive: extracts the dated JSON link from the download page", "positive",
			"azure_download_page", pageSrv.URL,
			"https://download.microsoft.com/download/a/b/ServiceTags_Public_20260907.json", false},
		{"boundary_none", "boundary: mode none returns the URL unchanged", "boundary",
			"none", "https://example/x.json", "https://example/x.json", false},
		{"boundary_empty_mode", "boundary: empty mode returns the URL unchanged", "boundary",
			"", "https://example/y.json", "https://example/y.json", false},
		{"negative_no_link", "negative: a page with no matching link errors", "negative",
			"azure_download_page", emptySrv.URL, "", true},
		{"corner_unknown_mode", "corner: an unknown discover mode errors", "corner",
			"bogus", pageSrv.URL, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Discover(context.Background(), tc.mode, tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error, got nil", tc.desc)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.desc, got, tc.want)
			}
		})
	}
}
