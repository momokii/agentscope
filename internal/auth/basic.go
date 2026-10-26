// Package auth owns the Wave-4 optional HTTP Basic gate for
// opencode mission control.
//
// Trust model: the dashboard binds 127.0.0.1 and is fronted by
// `tailscale serve`, so an unset DASHBOARD_AUTH means tailnet-trust mode
// (open on loopback, documented — not accidental). A set DASHBOARD_AUTH
// gates EVERYTHING (API + embedded SPA) behind one shared credential.
//
// A successful Basic check also mints a bounded, HttpOnly session cookie
// (see session.go) so the native EventSource used by the SPA can authorize
// its stream: EventSource cannot send an Authorization header. Cookie and
// Basic are OR-ed; neither replaces the other. There is still no per-user
// management, OAuth, or persistent session storage. See docs/SECURITY.md.
//
// The package never logs: no log/slog/fmt-print imports anywhere here,
// so credentials cannot leak through log pipelines. basic_test.go pins
// that invariant (source scan) and asserts 401 bodies/headers never echo
// credentials.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// Realm is the Basic challenge realm. It names the protected area only;
// it never carries secrets.
const Realm = "opencode-dashboard"

// EnvVar is the ONLY accepted config source. Auth is never read from URL
// query params, request bodies, or any other input.
const EnvVar = "DASHBOARD_AUTH"

// ErrInvalidAuthConfig marks a malformed DASHBOARD_AUTH value. Startup
// must fail closed on it: refuse to serve, never silently run open.
var ErrInvalidAuthConfig = errors.New("auth: invalid " + EnvVar)

// AuthConfigError is the typed fail-closed error for a malformed
// DASHBOARD_AUTH value. Reason is operator-actionable; it never contains
// the credential value.
type AuthConfigError struct {
	Reason string
}

func (e *AuthConfigError) Error() string {
	return "auth: invalid " + EnvVar + ": " + e.Reason
}

// Is lets errors.Is(err, ErrInvalidAuthConfig) match *AuthConfigError.
func (e *AuthConfigError) Is(target error) bool {
	return target == ErrInvalidAuthConfig
}

// Credentials is the parsed gate identity. Zero value = open
// (tailnet-trust mode); Enabled=false is the explicit representation.
type Credentials struct {
	Username string
	Password string
	Enabled  bool
}

// ParseCredentials parses a raw "user:pass" value into Credentials.
// Empty value = disabled (open, tailnet-trust mode), nil error.
// Anything else must be "user:pass" with a non-empty user and a non-empty
// password; passwords may contain colons (split on the FIRST colon).
// Malformed input returns a *AuthConfigError wrapping
// ErrInvalidAuthConfig so startup can fail closed via errors.Is.
func ParseCredentials(raw string) (Credentials, error) {
	if raw == "" {
		return Credentials{}, nil
	}
	user, pass, found := strings.Cut(raw, ":")
	if !found {
		return Credentials{}, &AuthConfigError{Reason: "want user:pass format (no colon found)"}
	}
	if user == "" {
		return Credentials{}, &AuthConfigError{Reason: "username is empty"}
	}
	if pass == "" {
		return Credentials{}, &AuthConfigError{Reason: "password is empty"}
	}
	return Credentials{Username: user, Password: pass, Enabled: true}, nil
}

// CredentialsFromEnv reads EnvVar exactly once at startup. Empty/unset =
// open tailnet-trust mode; malformed = fail-closed typed error.
func CredentialsFromEnv() (Credentials, error) {
	return ParseCredentials(os.Getenv(EnvVar))
}

// Middleware gates the entire handler chain (API + embedded SPA) behind
// HTTP Basic auth. Comparison is constant-time over SHA-256 digests so
// neither value length nor prefix leaks through timing.
//
// A request is authorized by EITHER valid Basic credentials OR a session
// cookie issued after an earlier successful Basic check. A successful Basic
// check also sets the session cookie (HttpOnly, SameSite=Lax, Path=/,
// Secure only over TLS).
//
// Failure (missing/malformed credentials, unknown/expired cookie) → 401
// with WWW-Authenticate: Basic realm="opencode-dashboard" and an empty body.
// The 401 body and headers never echo the username or password.
func Middleware(next http.Handler, username, password string) http.Handler {
	return middlewareWithStore(next, username, password,
		newSessionStore(maxSessionTokens, DefaultSessionTTL, time.Now))
}

// middlewareWithStore is Middleware with an injectable token store (tests
// pin expiry/capacity without sleeping).
func middlewareWithStore(next http.Handler, username, password string, store *sessionStore) http.Handler {
	wantUser := sha256.Sum256([]byte(username))
	wantPass := sha256.Sum256([]byte(password))
	// Copies escape the stack into the closure; they are digests, never
	// the raw secrets, and this package has no logging path.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotUser, gotPass, ok := r.BasicAuth(); ok {
			gotUserSum := sha256.Sum256([]byte(gotUser))
			gotPassSum := sha256.Sum256([]byte(gotPass))
			userOK := subtle.ConstantTimeCompare(wantUser[:], gotUserSum[:]) == 1
			passOK := subtle.ConstantTimeCompare(wantPass[:], gotPassSum[:]) == 1
			if userOK && passOK {
				setSessionCookie(w, r, store)
				next.ServeHTTP(w, r)
				return
			}
		}
		if c, err := r.Cookie(SessionCookieName); err == nil && store.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		challenge(w)
	})
}

// challenge sends the 401 + WWW-Authenticate pair. Body is empty on
// purpose: nothing about the gate (usernames, hints) leaves the process.
func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="`+Realm+`"`)
	w.WriteHeader(http.StatusUnauthorized)
}
