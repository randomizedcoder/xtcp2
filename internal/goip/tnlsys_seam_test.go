package goip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

func TestOpenSys(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "tnlsys.out")
	if err := os.WriteFile(good, []byte(`{"tunfds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.out")
	if err := os.WriteFile(bad, []byte(`{"bogus":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		tnlsys      string
		replay      string
		want        string
		wantErr     string
	}{
		{"positive: GOIP_TNLSYS loads the snapshot", good, "", "snapshot-recorded", ""},
		{"positive: GOIP_TNLSYS wins over GOIP_REPLAY", good, "x.pcap", "snapshot-recorded", ""},
		{"positive: no variables asks the kernel", "", "", "live", ""},
		{"negative: a missing sidecar is an error", filepath.Join(dir, "nope"), "", "", "no such file"},
		{"negative: a malformed sidecar is an error", bad, "", "", "unknown field"},
		{"corner: a replay with no sidecar answers nothing, never the host", "", "x.pcap", "snapshot-empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_TNLSYS", tc.tnlsys)
			t.Setenv("GOIP_REPLAY", tc.replay)
			sys, err := openSys()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := "live"
			if s, ok := sys.(*tnlsys.Snapshot); ok {
				got = "snapshot-empty"
				if _, err := s.TunFDs(); err == nil {
					got = "snapshot-recorded"
				}
			} else if _, ok := sys.(tnlsys.Live); !ok {
				got = "other"
			}
			if got != tc.want {
				t.Errorf("sys = %s, want %s", got, tc.want)
			}
		})
	}
}
