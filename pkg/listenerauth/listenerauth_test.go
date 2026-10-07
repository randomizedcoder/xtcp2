package listenerauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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

// TestJitter drives Jitter directly, which nothing did before. Despite its
// name, TestAuthenticatorValidationAndJitter above only exercises New's
// validation and CryptoJitterDuration — it never calls Jitter — which is why
// dropping Jitter's error return broke no test.
//
// Every row is a clock assertion, because the clock is all Jitter exposes. It
// returns nothing by contract, so "it waited" and "it came back early" are the
// only observable states and the elapsed band is the whole expected outcome. A
// panic aborts the test binary with the row's description on the preceding RUN
// line, so no row asserts that separately.
//
// The bands are deliberately asymmetric. A floor is the real claim and is
// exact: Jitter cannot return before its minimum, and no amount of host load
// makes a sleep finish early. A ceiling can only ever be slack, because load on
// this host is high enough to have turned a 30 s test red already
// (TODO-SOON.md §22), so every ceiling below is far looser than the duration it
// separates from. The draw itself is already pinned to its inclusive band 100
// times over in TestAuthenticatorValidationAndJitter, so no row re-asserts it.
//
// The rows run serially for the same reason: t.Parallel() here would have each
// clock assertion measure the others' scheduling delay.
//
// This table uses `description` + `expected`. The older tables in this file
// predate that standard and still carry `name` + `expectedOutcome`.
func TestJitter(t *testing.T) {
	// Separates "slept" from "returned immediately". Every row that configures
	// a sleep and still expects to return early configures 10 s, so this is
	// twenty times looser than the gap it has to resolve.
	const noSleepCeilingCst = 500 * time.Millisecond

	// Long enough that the scheduler reliably gets to the cancel, short enough
	// that the 10 s it pre-empts is unambiguous.
	const cancelDelayCst = 50 * time.Millisecond

	// newJitter builds a RAW_TOKEN authenticator through New with the given
	// bounds, so rows using it cover only configurations that are reachable in
	// production. Spelled as a helper because inline it is six lines a row for
	// one varying pair.
	newJitter := func(lo, hi time.Duration) func(t *testing.T) *Authenticator {
		return func(t *testing.T) *Authenticator {
			a, err := New(&xtcp_config.ListenerAuth{
				Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
				RawToken:         "secret",
				FailureJitterMin: durationpb.New(lo),
				FailureJitterMax: durationpb.New(hi),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			return a
		}
	}

	tests := []struct {
		description string
		// auth builds the row's authenticator. Most rows go through New, so
		// they are configurations production can reach; jitterMin > jitterMax
		// cannot appear among them at all, because New rejects it and
		// TestAuthenticatorValidationAndJitter owns that row.
		//
		// Two rows build an Authenticator literal instead, for the same reason
		// TestValidToken does: a disabled mode with non-zero bounds is
		// unconstructible through New, which discards the bounds before it
		// reaches them. Those two rows are the only ones that can witness the
		// Enabled guard, because every reachable disabled authenticator has
		// zero bounds and would return immediately with the guard deleted.
		auth func(t *testing.T) *Authenticator
		// ctx builds the row's context. A function rather than a value so the
		// canceled-during row starts its timer when the row runs, not when the
		// table is built.
		ctx func(t *testing.T) context.Context
		// expectedEnabled is stated rather than derived from cfg: it is the
		// premise each elapsed band rests on, and a row whose authenticator
		// came out the other way is measuring something else.
		expectedEnabled bool
		expectedMin     time.Duration // elapsed at least this; 0 means no floor
		expectedMax     time.Duration // elapsed at most this
	}{
		{
			description:     "positive: an enabled authenticator sleeps for at least jitterMin before returning",
			auth:            newJitter(50*time.Millisecond, 100*time.Millisecond),
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: true,
			expectedMin:     50 * time.Millisecond,
			expectedMax:     10 * time.Second,
		},
		{
			description: "negative: a DISABLED authenticator with 10 s bounds does not sleep, which is the Enabled guard isolated — New cannot build this, because DISABLED returns before it reads the bounds",
			auth: func(*testing.T) *Authenticator {
				return &Authenticator{
					mode:      xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED,
					jitterMin: 10 * time.Second,
					jitterMax: 10 * time.Second,
				}
			},
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: false,
			expectedMin:     0,
			expectedMax:     noSleepCeilingCst,
		},
		{
			description: "negative: an UNSPECIFIED authenticator with 10 s bounds does not sleep either, so both halves of Enabled are load-bearing and not just the DISABLED one",
			auth: func(*testing.T) *Authenticator {
				return &Authenticator{
					mode:      xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED,
					jitterMin: 10 * time.Second,
					jitterMax: 10 * time.Second,
				}
			},
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: false,
			expectedMin:     0,
			expectedMax:     noSleepCeilingCst,
		},
		{
			description: "negative: a nil config builds the disabled authenticator production actually gets, and it does not sleep — note this row alone could not catch the Enabled guard being deleted, since its bounds are zero",
			auth: func(t *testing.T) *Authenticator {
				a, err := New(nil)
				if err != nil {
					t.Fatalf("New(nil): %v", err)
				}
				return a
			},
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: false,
			expectedMin:     0,
			expectedMax:     noSleepCeilingCst,
		},
		{
			description:     "boundary: jitterMin == jitterMax sleeps exactly that long, the hi == lo branch of CryptoJitterDuration",
			auth:            newJitter(25*time.Millisecond, 25*time.Millisecond),
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: true,
			expectedMin:     25 * time.Millisecond,
			expectedMax:     10 * time.Second,
		},
		{
			description:     "boundary: jitterMin == jitterMax == 0 returns immediately through the timer rather than through the Enabled guard, so an enabled authenticator with no jitter still costs nothing",
			auth:            newJitter(0, 0),
			ctx:             func(*testing.T) context.Context { return context.Background() },
			expectedEnabled: true,
			expectedMin:     0,
			expectedMax:     noSleepCeilingCst,
		},
		{
			description: "corner: an already-canceled ctx returns promptly instead of sleeping 10 s, which is the behavior the dropped error return used to report and is now observable only on the clock",
			auth:        newJitter(10*time.Second, 10*time.Second),
			ctx: func(*testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expectedEnabled: true,
			expectedMin:     0,
			expectedMax:     noSleepCeilingCst,
		},
		{
			description: "corner: a ctx canceled mid-sleep returns at the cancel and not at the 10 s timer, so the select waits on both and not merely on an already-closed Done",
			auth:        newJitter(10*time.Second, 10*time.Second),
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				go func() {
					time.Sleep(cancelDelayCst)
					cancel()
				}()
				return ctx
			},
			expectedEnabled: true,
			expectedMin:     cancelDelayCst,
			expectedMax:     2 * time.Second,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a := tc.auth(t)
			if got := a.Enabled(); got != tc.expectedEnabled {
				t.Fatalf("Enabled() = %v, want %v; the elapsed band below assumes the other one", got, tc.expectedEnabled)
			}
			ctx := tc.ctx(t)
			start := time.Now()
			a.Jitter(ctx)
			elapsed := time.Since(start)
			if elapsed < tc.expectedMin {
				t.Fatalf("Jitter returned after %s, want at least %s", elapsed, tc.expectedMin)
			}
			if elapsed > tc.expectedMax {
				t.Fatalf("Jitter returned after %s, want at most %s", elapsed, tc.expectedMax)
			}
		})
	}
}

// fakeServerStream is the slice of grpc.ServerStream that interceptStream
// actually touches, which is Context and nothing else. The other five methods
// exist to satisfy the interface and panic if called, so that an interceptor
// which starts using one is caught here rather than reaching production with an
// untested path.
type fakeServerStream struct{ ctx context.Context }

func (f fakeServerStream) Context() context.Context  { return f.ctx }
func (fakeServerStream) SetHeader(metadata.MD) error { panic("fakeServerStream: unexpected SetHeader") }
func (fakeServerStream) SendHeader(metadata.MD) error {
	panic("fakeServerStream: unexpected SendHeader")
}
func (fakeServerStream) SetTrailer(metadata.MD) { panic("fakeServerStream: unexpected SetTrailer") }
func (fakeServerStream) SendMsg(any) error      { panic("fakeServerStream: unexpected SendMsg") }
func (fakeServerStream) RecvMsg(any) error      { panic("fakeServerStream: unexpected RecvMsg") }

// TestInterceptStream_table drives interceptStream directly, which is the test
// the contextcheck fix made possible: the body used to be an anonymous function
// reachable only by standing up a gRPC server, and neither interceptor had any
// test at all.
//
// `expected` is the whole outcome of an interception: whether the handler ran,
// the gRPC status code the caller sees, and — for the rows that reach it — that
// srv and ss arrive at the handler unchanged. A row that only checked the code
// would not notice an interceptor that authenticated correctly and then called
// the handler with the wrong stream.
//
// Every row configures a 1 ms failure jitter. The real default is 20-200 ms and
// authenticateGRPC jitters on each rejection, so the defaults would make this
// table sleep for most of a second to prove nothing it does not already prove
// in TestJitter.
//
// This table uses `description` + `expected`. The older tables in this file
// predate that standard and still carry `name` + `expectedOutcome`.
func TestInterceptStream_table(t *testing.T) {
	rawCfg := func(token string) *xtcp_config.ListenerAuth {
		return &xtcp_config.ListenerAuth{
			Mode:             xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN,
			RawToken:         token,
			FailureJitterMin: durationpb.New(time.Millisecond),
			FailureJitterMax: durationpb.New(time.Millisecond),
		}
	}
	errHandler := errors.New("handler failed")

	tests := []struct {
		description string
		cfg         *xtcp_config.ListenerAuth
		// md is the incoming metadata. nil means the context carries none at
		// all, which is a different rejection from carrying the wrong value.
		md metadata.MD
		// handlerErr is what the handler returns if it is reached.
		handlerErr            error
		expectedHandlerCalled bool
		expectedCode          codes.Code
		// expectedErrIs is checked where the error must be the handler's own,
		// passed back untouched rather than re-wrapped as a status.
		expectedErrIs error
	}{
		{
			description:           "positive: a stream carrying the configured bearer token authenticates and reaches the handler with srv and ss unchanged",
			cfg:                   rawCfg("secret"),
			md:                    metadata.Pairs(authorizationMeta, "Bearer secret"),
			expectedHandlerCalled: true,
			expectedCode:          codes.OK,
		},
		{
			description:           "negative: a stream with no incoming metadata is Unauthenticated and the handler never runs",
			cfg:                   rawCfg("secret"),
			md:                    nil,
			expectedHandlerCalled: false,
			expectedCode:          codes.Unauthenticated,
		},
		{
			description:           "negative: a wrong token of the same length is Unauthenticated, so a constant-time comparison regression cannot hide behind a length check",
			cfg:                   rawCfg("secret"),
			md:                    metadata.Pairs(authorizationMeta, "Bearer sekret"),
			expectedHandlerCalled: false,
			expectedCode:          codes.Unauthenticated,
		},
		{
			description:           "boundary: metadata present but with an empty authorization value is Unauthenticated, not an accidental pass",
			cfg:                   rawCfg("secret"),
			md:                    metadata.Pairs(authorizationMeta, ""),
			expectedHandlerCalled: false,
			expectedCode:          codes.Unauthenticated,
		},
		{
			description:           "corner: a disabled authenticator lets every stream through, which is defense in depth rather than dead code — pkg/xtcp only installs the interceptor when Enabled, and this row pins what happens if that guard is ever dropped",
			cfg:                   nil,
			md:                    nil,
			expectedHandlerCalled: true,
			expectedCode:          codes.OK,
		},
		{
			description:           "corner: the handler's own error is returned untouched, so a failing RPC is not reported as an authentication failure",
			cfg:                   rawCfg("secret"),
			md:                    metadata.Pairs(authorizationMeta, "Bearer secret"),
			handlerErr:            errHandler,
			expectedHandlerCalled: true,
			expectedCode:          codes.Unknown,
			expectedErrIs:         errHandler,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a, err := New(tc.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			ctx := context.Background()
			if tc.md != nil {
				ctx = metadata.NewIncomingContext(ctx, tc.md)
			}
			srv := &struct{ name string }{name: "server"}
			ss := fakeServerStream{ctx: ctx}
			info := &grpc.StreamServerInfo{FullMethod: "/xtcp.Test/Stream"}

			called := false
			handler := func(gotSrv any, gotSS grpc.ServerStream) error {
				called = true
				if gotSrv != any(srv) {
					t.Errorf("handler got srv %v, want the one passed to the interceptor", gotSrv)
				}
				if gotSS != grpc.ServerStream(ss) {
					t.Errorf("handler got a different ServerStream than the interceptor was given")
				}
				return tc.handlerErr
			}

			err = a.interceptStream(srv, ss, info, handler)

			if called != tc.expectedHandlerCalled {
				t.Fatalf("handler called = %v, want %v (err = %v)", called, tc.expectedHandlerCalled, err)
			}
			if got := status.Code(err); got != tc.expectedCode {
				t.Fatalf("status code = %s (err %v), want %s", got, err, tc.expectedCode)
			}
			if tc.expectedErrIs != nil && !errors.Is(err, tc.expectedErrIs) {
				t.Fatalf("err = %v, want errors.Is %v", err, tc.expectedErrIs)
			}
		})
	}
}
