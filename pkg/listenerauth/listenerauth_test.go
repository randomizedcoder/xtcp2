package listenerauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestAuthenticatorRaw(t *testing.T) {
	cases := []struct {
		name            string
		description     string
		values          []string
		expectedOutcome bool
	}{
		{"match", "raw token positive match", []string{"Bearer secret"}, true},
		{"mismatch", "raw token negative mismatch", []string{"Bearer nope"}, false},
		{"missing", "raw token negative missing", nil, false},
		{"malformed", "raw token negative malformed", []string{"Basic secret"}, false},
		{"duplicate", "raw token negative multiple bearer values", []string{"Bearer secret", "Bearer secret"}, false},
	}
	a, err := New(&xtcp_config.ListenerAuth{
		Mode:                   xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
		RawToken:               "secret",
		FailureJitterMin:       durationpb.New(time.Millisecond),
		FailureJitterMax:       durationpb.New(time.Millisecond),
		SignedTokenSkewMinutes: proto.Uint32(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := a.AuthenticateValues(context.Background(), tc.values)
			if (err == nil) != tc.expectedOutcome {
				t.Fatalf("%s: err=%v", tc.description, err)
			}
		})
	}
}

func TestAuthenticatorHMACUTCMinute(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 34, 45, 0, time.UTC)
	cases := []struct {
		name            string
		description     string
		skew            uint32
		tokenMinute     time.Time
		expectedOutcome bool
	}{
		{"current", "HMAC positive current minute", 1, now, true},
		{"previous", "HMAC positive previous minute inside skew", 1, now.Add(-time.Minute), true},
		{"next", "HMAC positive next minute inside skew", 1, now.Add(time.Minute), true},
		{"zero_current", "HMAC boundary skew 0 accepts current", 0, now, true},
		{"zero_previous", "HMAC boundary skew 0 rejects previous", 0, now.Add(-time.Minute), false},
		{"max_inside", "HMAC boundary skew 5 accepts five minutes", 5, now.Add(5 * time.Minute), true},
		{"outside", "HMAC negative outside skew", 1, now.Add(2 * time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := newWithClock(&xtcp_config.ListenerAuth{
				Mode:                   xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
				HmacSharedKey:          "shared",
				SignedTokenSkewMinutes: proto.Uint32(tc.skew),
				FailureJitterMin:       durationpb.New(time.Millisecond),
				FailureJitterMax:       durationpb.New(time.Millisecond),
			}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			token := SignedToken("shared", tc.tokenMinute)
			err = a.AuthenticateValues(context.Background(), []string{"Bearer " + token})
			if (err == nil) != tc.expectedOutcome {
				t.Fatalf("%s: err=%v", tc.description, err)
			}
		})
	}
}

func TestAuthenticatorValidationAndJitter(t *testing.T) {
	cases := []struct {
		name            string
		description     string
		cfg             *xtcp_config.ListenerAuth
		expectedOutcome bool
	}{
		{"raw_empty_secret", "empty required secret for raw mode", &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN}, false},
		{"hmac_empty_secret", "empty required secret for HMAC mode", &xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE}, false},
		{"jitter_equal", "jitter min=max boundary", &xtcp_config.ListenerAuth{
			Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			RawToken:         "x",
			FailureJitterMin: durationpb.New(5 * time.Millisecond),
			FailureJitterMax: durationpb.New(5 * time.Millisecond),
		}, true},
		{"jitter_zero", "explicit zero jitter remains zero", &xtcp_config.ListenerAuth{
			Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			RawToken:         "x",
			FailureJitterMin: durationpb.New(0),
			FailureJitterMax: durationpb.New(0),
		}, true},
		{"jitter_max_less", "jitter max<min validation behavior", &xtcp_config.ListenerAuth{
			Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			RawToken:         "x",
			FailureJitterMin: durationpb.New(6 * time.Millisecond),
			FailureJitterMax: durationpb.New(5 * time.Millisecond),
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if (err == nil) != tc.expectedOutcome {
				t.Fatalf("%s: err=%v", tc.description, err)
			}
		})
	}
	for i := 0; i < 100; i++ {
		got, err := CryptoJitterDuration(2*time.Millisecond, 5*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if got < 2*time.Millisecond || got > 5*time.Millisecond {
			t.Fatalf("crypto jitter duration always in inclusive range: got %s", got)
		}
	}
}

// TestValidToken drives validToken directly across every ListenerAuthMode,
// including the two that newWithClock collapses to DISABLED and one value
// outside the enum. Those three states are unreachable through New, which is
// why each row builds an Authenticator literal instead of a config — and they
// are exactly the states the exhaustive switch in validToken now spells out.
//
// This table uses `description` + `expected`. The older tables in this file
// predate that standard and still carry `name` + `expectedOutcome`.
func TestValidToken(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 34, 45, 0, time.UTC)
	minute := now.UTC().Truncate(time.Minute)
	clock := func() time.Time { return now }

	const rawToken = "s3cr3t-raw-token"
	const hmacKey = "shared"

	// sameLengthWrongToken differs from rawToken in its final byte only, so a
	// regression that replaced ConstantTimeCompare with a length check would
	// still pass this row's sibling positive and fail here.
	const sameLengthWrongToken = "s3cr3t-raw-tokeN"

	// modeOutsideEnum stands in for a ListenerAuthMode a newer proto adds that
	// this build has never seen.
	const modeOutsideEnum = xtcp_config.ListenerAuthMode(99)

	tests := []struct {
		description string
		mode        xtcp_config.ListenerAuthMode
		configured  string // rawToken for raw mode; ignored by the others
		skew        uint32
		token       string
		expected    bool
	}{
		{
			description: "positive: RAW_TOKEN with the configured token authenticates",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			configured:  rawToken,
			token:       rawToken,
			expected:    true,
		},
		{
			description: "positive: HMAC_UTC_MINUTE with a token signed for the current minute authenticates",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
			skew:        1,
			token:       SignedToken(hmacKey, minute),
			expected:    true,
		},
		{
			description: "negative: RAW_TOKEN with a wrong token of the same length is rejected",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			configured:  rawToken,
			token:       sameLengthWrongToken,
			expected:    false,
		},
		{
			description: "negative: DISABLED mode rejects a byte-correct token, because that mode validates nothing",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED,
			configured:  rawToken,
			token:       rawToken,
			expected:    false,
		},
		{
			description: "negative: UNSPECIFIED mode rejects a byte-correct token, for the same reason",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED,
			configured:  rawToken,
			token:       rawToken,
			expected:    false,
		},
		{
			description: "boundary: the empty token under RAW_TOKEN is rejected",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			configured:  rawToken,
			token:       "",
			expected:    false,
		},
		{
			description: "boundary: the empty token under HMAC_UTC_MINUTE is rejected",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
			skew:        1,
			token:       "",
			expected:    false,
		},
		{
			description: "boundary: the empty token under DISABLED is rejected",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED,
			configured:  rawToken,
			token:       "",
			expected:    false,
		},
		{
			description: "boundary: the empty token under UNSPECIFIED is rejected",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED,
			configured:  rawToken,
			token:       "",
			expected:    false,
		},
		{
			description: "boundary: HMAC_UTC_MINUTE accepts a token signed one minute inside skew 1",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
			skew:        1,
			token:       SignedToken(hmacKey, minute.Add(-time.Minute)),
			expected:    true,
		},
		{
			description: "boundary: HMAC_UTC_MINUTE rejects a token signed one minute outside skew 1",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE,
			skew:        1,
			token:       SignedToken(hmacKey, minute.Add(2*time.Minute)),
			expected:    false,
		},
		{
			description: "corner: a ListenerAuthMode outside the enum rejects a byte-correct token, so a future mode cannot become permissive by default",
			mode:        modeOutsideEnum,
			configured:  rawToken,
			token:       rawToken,
			expected:    false,
		},
		{
			description: "corner: RAW_TOKEN with an empty configured token accepts the empty token, which is why newWithClock refuses to build that config at all",
			mode:        xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			configured:  "",
			token:       "",
			expected:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a := &Authenticator{
				mode:      tc.mode,
				rawToken:  tc.configured,
				hmacKey:   hmacKey,
				skew:      tc.skew,
				jitterMin: time.Millisecond,
				jitterMax: time.Millisecond,
				now:       clock,
			}
			if got := a.validToken(tc.token); got != tc.expected {
				t.Fatalf("validToken(%q) under %s = %v, want %v",
					tc.token, ModeString(tc.mode), got, tc.expected)
			}
		})
	}
}

// TestValidTokenEmptyConfigRejectedByNew pins the guard the last row of
// TestValidToken relies on: validToken alone would accept the empty token
// against an empty raw secret, and newWithClock is what makes that config
// unconstructible.
func TestValidTokenEmptyConfigRejectedByNew(t *testing.T) {
	if _, err := New(&xtcp_config.ListenerAuth{
		Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
	}); err == nil {
		t.Fatal("New accepted RAW_TOKEN with an empty token; validToken's empty-secret corner is then reachable in production")
	}
}

func TestWrapHTTPProtectsAllRoutes(t *testing.T) {
	a, err := New(&xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawToken: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := a.WrapHTTP(mux)
	for _, path := range []string{"/metrics", "/healthz", "/readyz", "/debug/pprof/"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		got := httptest.NewRecorder()
		h.ServeHTTP(got, r)
		if got.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated status=%d", path, got.Code)
		}
		r.Header.Set(AuthorizationHeader, "Bearer secret")
		got = httptest.NewRecorder()
		h.ServeHTTP(got, r)
		if got.Code != http.StatusNoContent {
			t.Errorf("%s authenticated status=%d", path, got.Code)
		}
	}
}
