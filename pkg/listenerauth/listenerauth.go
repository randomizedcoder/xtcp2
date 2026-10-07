// Package listenerauth authenticates callers of the xtcp2 listener endpoints
// with a bearer token, over both HTTP middleware and gRPC interceptors.
//
// Two modes carry a credential. RAW_TOKEN compares the presented token against
// a configured secret; HMAC_UTC_MINUTE accepts an HMAC-SHA256 of the current
// UTC minute, signed with a shared key and checked across a bounded skew
// window (MaxSignedTokenSkewMinutes). DISABLED and UNSPECIFIED authenticate
// nothing and are the zero value, so a missing configuration fails open by
// design rather than by accident.
//
// Every comparison goes through subtle.ConstantTimeCompare, and every failure
// is followed by a cryptographically random delay (Jitter) so that neither the
// token's bytes nor its length are recoverable from response timing.
package listenerauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	AuthorizationHeader = "Authorization"
	authorizationMeta   = "authorization"
	bearerPrefix        = "Bearer "

	DefaultSignedTokenSkewMinutes uint32 = 1
	MaxSignedTokenSkewMinutes     uint32 = 5

	DefaultFailureJitterMin = 20 * time.Millisecond
	DefaultFailureJitterMax = 200 * time.Millisecond
)

var (
	ErrMissingCredentials   = errors.New("missing bearer credentials")
	ErrDuplicateCredentials = errors.New("duplicate authorization credentials")
	ErrMalformedCredentials = errors.New("malformed bearer credentials")
	ErrInvalidCredentials   = errors.New("invalid bearer credentials")
)

type Authenticator struct {
	mode      xtcp_config.ListenerAuthMode
	rawToken  string
	hmacKey   string
	skew      uint32
	jitterMin time.Duration
	jitterMax time.Duration
	now       func() time.Time
}

func New(cfg *xtcp_config.ListenerAuth) (*Authenticator, error) {
	return newWithClock(cfg, time.Now)
}

func ParseMode(s string) (xtcp_config.ListenerAuthMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "disabled", "disable", "off", "none":
		return xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED, nil
	case "raw", "bearer":
		return xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, nil
	case "hmac-utc-minute", "hmac_utc_minute", "hmac":
		return xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE, nil
	default:
		return xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED, fmt.Errorf("unsupported listener auth mode %q", s)
	}
}

func ModeString(mode xtcp_config.ListenerAuthMode) string {
	switch mode {
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN:
		return "raw"
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE:
		return "hmac-utc-minute"
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED, xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED:
		return "disabled"
	default:
		return mode.String()
	}
}

func newWithClock(cfg *xtcp_config.ListenerAuth, now func() time.Time) (*Authenticator, error) {
	if cfg == nil || cfg.GetMode() == xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED ||
		cfg.GetMode() == xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED {
		return &Authenticator{mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED, now: now}, nil
	}
	a := &Authenticator{
		mode:      cfg.GetMode(),
		rawToken:  cfg.GetRawToken(),
		hmacKey:   cfg.GetHmacSharedKey(),
		skew:      DefaultSignedTokenSkewMinutes,
		jitterMin: durationOrDefault(cfg.GetFailureJitterMin(), DefaultFailureJitterMin),
		jitterMax: durationOrDefault(cfg.GetFailureJitterMax(), DefaultFailureJitterMax),
		now:       now,
	}
	a.skew = cfg.GetSignedTokenSkewMinutes()
	if a.skew > MaxSignedTokenSkewMinutes {
		return nil, fmt.Errorf("listener auth signed token skew %d exceeds max %d", a.skew, MaxSignedTokenSkewMinutes)
	}
	if a.jitterMax < a.jitterMin {
		return nil, fmt.Errorf("listener auth failure jitter max %s is less than min %s", a.jitterMax, a.jitterMin)
	}
	switch a.mode {
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN:
		if a.rawToken == "" {
			return nil, errors.New("listener auth raw mode requires a token")
		}
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE:
		if a.hmacKey == "" {
			return nil, errors.New("listener auth hmac-utc-minute mode requires a shared key")
		}
	default:
		return nil, fmt.Errorf("unsupported listener auth mode %s", a.mode.String())
	}
	return a, nil
}

func durationOrDefault(d *durationpb.Duration, def time.Duration) time.Duration {
	if d == nil {
		return def
	}
	return d.AsDuration()
}

func (a *Authenticator) Enabled() bool {
	return a != nil && a.mode != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED &&
		a.mode != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED
}

func (a *Authenticator) AuthenticateValues(ctx context.Context, values []string) error {
	if !a.Enabled() {
		return nil
	}
	token, err := bearerToken(values)
	if err != nil {
		return err
	}
	if a.validToken(token) {
		return nil
	}
	return ErrInvalidCredentials
}

func (a *Authenticator) AuthenticateHTTP(r *http.Request) error {
	return a.AuthenticateValues(r.Context(), r.Header.Values(AuthorizationHeader))
}

// Jitter sleeps for a random interval to blunt timing analysis of a failed
// authentication, returning early if ctx is canceled.
//
// It deliberately returns nothing. Its only failure was context cancellation,
// and all three callers are about to return Unauthorized or Unauthenticated
// regardless of whether the delay completed — a cancellation there means the
// client hung up, which changes nothing about the response. So every call site
// discarded the error, and `_ = a.Jitter(ctx)` three times states that decision
// three times in the voice of an oversight. Stating it once here, in the
// signature, is the same argument made where callers read it.
//
// Jitter is therefore best-effort by contract. If a caller ever needs to know
// whether the full delay elapsed, that is a new method, not a resurrected
// error: the delay is a security measure, so "it was cut short" must not become
// something a caller can be tempted to retry or report.
func (a *Authenticator) Jitter(ctx context.Context) {
	if !a.Enabled() {
		return
	}
	d, err := CryptoJitterDuration(a.jitterMin, a.jitterMax)
	if err != nil {
		d = a.jitterMin
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (a *Authenticator) WrapHTTP(next http.Handler) http.Handler {
	if !a.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := a.AuthenticateHTTP(r); err != nil {
			a.Jitter(r.Context())
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *Authenticator) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := a.authenticateGRPC(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamServerInterceptor returns interceptStream as a method value rather than
// a closure, which is deliberate and is the shape contextcheck requires.
//
// Measured, because the reasoning is not obvious from the message: contextcheck
// reported `StreamServerInterceptor->StreamServerInterceptor$1 should pass the
// context parameter` at the grpc.StreamInterceptor call site in pkg/xtcp. It
// treats an anonymous function as part of its enclosing function, so a closure
// with no ctx parameter that calls a context-consuming function looks like a
// function consuming a context it was never given. The context actually passed
// is irrelevant to it — ss.Context() and context.Background() were flagged
// identically — and hoisting only the body into a named ctx-taking helper does
// not silence it either, because the closure remains.
//
// A named method is analyzed on its own, and this one derives its context from
// its own ss parameter, which is exactly the provenance contextcheck is asking
// about. So this is the finding being answered rather than suppressed: no
// exclusion was added for it in either golangci config.
//
// UnaryServerInterceptor above keeps its closure and is not flagged, because
// grpc.UnaryServerInterceptor's signature carries a ctx and so the closure
// receives one. The asymmetry between the two is grpc-go's, not ours.
//
// ss.Context() is also the only context that can be used here. It is the
// per-RPC context, and the credentials this authenticates live in its incoming
// metadata; a context from anywhere else would carry none and reject every
// stream.
func (a *Authenticator) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return a.interceptStream
}

// interceptStream has grpc.StreamServerInterceptor's exact signature, so that
// StreamServerInterceptor can hand it over as a method value. Being named also
// makes it directly callable, which is how TestInterceptStream_table drives it
// without standing up a gRPC server.
func (a *Authenticator) interceptStream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := a.authenticateGRPC(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}

func (a *Authenticator) authenticateGRPC(ctx context.Context) error {
	if !a.Enabled() {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		a.Jitter(ctx)
		return status.Error(codes.Unauthenticated, ErrMissingCredentials.Error())
	}
	if err := a.AuthenticateValues(ctx, md.Get(authorizationMeta)); err != nil {
		a.Jitter(ctx)
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return nil
}

// validToken reports whether token authenticates under the configured mode.
//
// Every ListenerAuthMode is spelled out, and there is deliberately no default
// clause: DISABLED and UNSPECIFIED validate nothing, which was previously true
// only by falling out of the switch, and a mode value outside the enum reaches
// the trailing return false. Adding a default here would make a future enum
// value permissive by accident instead of failing the exhaustive linter.
func (a *Authenticator) validToken(token string) bool {
	switch a.mode {
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN:
		return constantTimeStringEqual(token, a.rawToken)
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_HMAC_UTC_MINUTE:
		now := a.now().UTC().Truncate(time.Minute)
		for delta := -int(a.skew); delta <= int(a.skew); delta++ {
			candidate := SignedToken(a.hmacKey, now.Add(time.Duration(delta)*time.Minute))
			if constantTimeStringEqual(token, candidate) {
				return true
			}
		}
	case xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_DISABLED,
		xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED:
		return false
	}
	return false
}

func bearerToken(values []string) (string, error) {
	if len(values) == 0 {
		return "", ErrMissingCredentials
	}
	if len(values) != 1 {
		return "", ErrDuplicateCredentials
	}
	value := values[0]
	if !strings.HasPrefix(value, bearerPrefix) {
		return "", ErrMalformedCredentials
	}
	token := strings.TrimPrefix(value, bearerPrefix)
	if token == "" || token != strings.TrimSpace(token) || strings.ContainsAny(token, " \t\r\n") {
		return "", ErrMalformedCredentials
	}
	return token, nil
}

func constantTimeStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func SignedToken(sharedKey string, t time.Time) string {
	minute := strconv.FormatInt(t.UTC().Unix()/60, 10)
	mac := hmac.New(sha256.New, []byte(sharedKey))
	_, _ = mac.Write([]byte(minute))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// CryptoJitterDuration returns a cryptographically random duration in the
// inclusive range [lo, hi]. The parameters are lo/hi rather than min/max
// because those two names shadow the builtins (gocritic builtinShadow).
func CryptoJitterDuration(lo, hi time.Duration) (time.Duration, error) {
	if lo < 0 || hi < 0 {
		return 0, errors.New("jitter durations must be non-negative")
	}
	if hi < lo {
		return 0, errors.New("jitter max is less than min")
	}
	if hi == lo {
		return lo, nil
	}
	span := hi - lo
	n, err := rand.Int(rand.Reader, big.NewInt(int64(span)+1))
	if err != nil {
		return 0, err
	}
	return lo + time.Duration(n.Int64()), nil
}

type ClientAuth struct {
	RawToken      string
	HMACSharedKey string
}

func ClientAuthFromValues(rawToken, rawTokenFile, hmacKey, hmacKeyFile string) (ClientAuth, error) {
	auth := ClientAuth{RawToken: rawToken, HMACSharedKey: hmacKey}
	if rawTokenFile != "" {
		v, err := readSecretFile(rawTokenFile)
		if err != nil {
			return ClientAuth{}, err
		}
		auth.RawToken = v
	}
	if hmacKeyFile != "" {
		v, err := readSecretFile(hmacKeyFile)
		if err != nil {
			return ClientAuth{}, err
		}
		auth.HMACSharedKey = v
	}
	if auth.RawToken != "" && auth.HMACSharedKey != "" {
		return ClientAuth{}, errors.New("raw token and HMAC shared key are mutually exclusive")
	}
	return auth, nil
}

func ClientAuthFromEnvAndFlags(rawFlag, rawFileFlag, hmacFlag, hmacFileFlag, rawEnv, rawFileEnv, hmacEnv, hmacFileEnv string) (ClientAuth, error) {
	raw := rawFlag
	rawFile := rawFileFlag
	if v, ok := os.LookupEnv(rawEnv); ok {
		raw = v
	}
	if v, ok := os.LookupEnv(rawFileEnv); ok {
		rawFile = v
	}
	hmacKey := hmacFlag
	hmacFile := hmacFileFlag
	if v, ok := os.LookupEnv(hmacEnv); ok {
		hmacKey = v
	}
	if v, ok := os.LookupEnv(hmacFileEnv); ok {
		hmacFile = v
	}
	return ClientAuthFromValues(raw, rawFile, hmacKey, hmacFile)
}

func (a ClientAuth) BearerToken(now time.Time) (string, bool) {
	if a.RawToken != "" {
		return a.RawToken, true
	}
	if a.HMACSharedKey != "" {
		return SignedToken(a.HMACSharedKey, now), true
	}
	return "", false
}

func ContextWithClientAuth(ctx context.Context, auth ClientAuth, now time.Time) context.Context {
	token, ok := auth.BearerToken(now)
	if !ok {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, authorizationMeta, bearerPrefix+token)
}

func HTTPRequestWithClientAuth(req *http.Request, auth ClientAuth, now time.Time) {
	token, ok := auth.BearerToken(now)
	if !ok {
		return
	}
	req.Header.Set(AuthorizationHeader, bearerPrefix+token)
}

func readSecretFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
