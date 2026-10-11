package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestEnvOverrideAdminListenerAuth(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "admin.token")
	if err := os.WriteFile(tokenFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type want struct {
		mode  xtcp_config.ListenerAuthMode
		token string
		hmac  string
	}
	tests := []struct {
		category    string
		description string
		env         map[string]string
		expected    want
	}{
		{"positive", "admin mode and raw token come from their own variables", map[string]string{
			"LISTENER_ADMIN_AUTH_MODE": "raw", "LISTENER_ADMIN_RAW_TOKEN": "inline"},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, "inline", ""}},
		{"positive", "the _FILE variable wins and is trimmed", map[string]string{
			"LISTENER_ADMIN_AUTH_MODE": "raw", "LISTENER_ADMIN_RAW_TOKEN": "inline", "LISTENER_ADMIN_RAW_TOKEN_FILE": tokenFile},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, "from-file", ""}},
		{"negative", "an unparseable admin mode leaves the flag value", map[string]string{
			"LISTENER_ADMIN_AUTH_MODE": "bogus"},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, "", ""}},
		{"boundary", "no admin variables leaves the admin credential unset", map[string]string{},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, "", ""}},
		{"corner", "read variables never populate the admin credential", map[string]string{
			"LISTENER_AUTH_MODE": "hmac", "LISTENER_HMAC_SHARED_KEY": "read-key"},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, "", ""}},
		{"corner", "admin HMAC key is separate from the read key", map[string]string{
			"LISTENER_ADMIN_AUTH_MODE": "hmac", "LISTENER_ADMIN_HMAC_SHARED_KEY": "admin-key", "LISTENER_HMAC_SHARED_KEY": "read-key"},
			want{xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE, "", "admin-key"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %+v", tc.category, tc.description, tc.expected)
			for _, k := range []string{"LISTENER_AUTH_MODE", "LISTENER_RAW_TOKEN", "LISTENER_RAW_TOKEN_FILE",
				"LISTENER_HMAC_SHARED_KEY", "LISTENER_HMAC_SHARED_KEY_FILE", "LISTENER_ADMIN_AUTH_MODE",
				"LISTENER_ADMIN_RAW_TOKEN", "LISTENER_ADMIN_RAW_TOKEN_FILE", "LISTENER_ADMIN_HMAC_SHARED_KEY",
				"LISTENER_ADMIN_HMAC_SHARED_KEY_FILE", "LISTENER_SIGNED_SKEW_MINUTES",
				"LISTENER_AUTH_FAILURE_JITTER_MIN", "LISTENER_AUTH_FAILURE_JITTER_MAX"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c := &xtcp_config.XtcpConfig{ListenerAuth: &xtcp_config.ListenerAuth{
				SignedTokenSkewMinutes: proto.Uint32(2),
				FailureJitterMax:       durationpb.New(7 * time.Millisecond),
			}}
			envOverrideListenerAuth(c, 0)
			a := c.GetAdminListenerAuth()
			got := want{a.GetMode(), a.GetRawToken(), a.GetHmacSharedKey()}
			if got != tc.expected {
				t.Fatalf("admin = %+v, want %+v", got, tc.expected)
			}
			if a.GetSignedTokenSkewMinutes() != 2 || a.GetFailureJitterMax().AsDuration() != 7*time.Millisecond {
				t.Fatalf("admin timing not shared with read credential: %+v", a)
			}
		})
	}
}
