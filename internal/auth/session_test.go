package auth

import (
	"crypto/tls"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			return c
		}
	}
	return nil
}

// Given: the exact configured Basic credentials.
// When: the gate accepts the request.
// Then: it issues a 32-byte hex, HttpOnly, SameSite=Lax, Path=/ cookie and
// does not mark it Secure over plain HTTP.
func Test_Middleware_issues_session_cookie_on_basic_auth(t *testing.T) {
	// Given
	req := basicReq(t, "ops", "s3cr3t-pw")

	// When
	rec := do(req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	c := sessionCookie(t, rec)
	if c == nil {
		t.Fatal("successful Basic auth issued no session cookie")
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Secure {
		t.Error("cookie must not be Secure on a plain-http request")
	}
	if c.MaxAge != int(DefaultSessionTTL/time.Second) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(DefaultSessionTTL/time.Second))
	}
	if len(c.Value) != 2*sessionTokenBytes {
		t.Fatalf("token length = %d, want %d", len(c.Value), 2*sessionTokenBytes)
	}
	if _, err := hex.DecodeString(c.Value); err != nil {
		t.Fatalf("token is not hex: %v", err)
	}
}

// Given: a cookie minted by a successful Basic request.
// When: a later request carries only that cookie (no Authorization header).
// Then: it is authorized, on both API and SPA paths.
func Test_Middleware_accepts_session_cookie_without_basic(t *testing.T) {
	// Given
	gated := Middleware(okHandler(), "ops", "s3cr3t-pw")
	first := httptest.NewRecorder()
	gated.ServeHTTP(first, basicReq(t, "ops", "s3cr3t-pw"))
	c := sessionCookie(t, first)
	if c == nil {
		t.Fatal("no cookie to reuse")
	}

	// When / Then
	for _, target := range []string{"/api/fleet", "/"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(c)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s with cookie only = %d, want 200", target, rec.Code)
		}
	}
}

// Given: a session cookie that was never issued by this gate.
// When: it is presented without Basic credentials.
// Then: the gate challenges.
func Test_Middleware_rejects_unknown_session_cookie(t *testing.T) {
	// Given
	req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "not-a-real-token"})

	// When
	rec := do(req)

	// Then
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("missing WWW-Authenticate on 401")
	}
}

// Given: a token issued at t0 with a one-hour TTL.
// When: the same cookie is presented after the TTL, or Basic is attempted
// again with the clock advanced.
// Then: the token is rejected and purged from the store.
func Test_Middleware_rejects_expired_session_cookie(t *testing.T) {
	// Given
	now := time.UnixMilli(1800000000000)
	store := newSessionStore(4, time.Hour, func() time.Time { return now })
	gated := middlewareWithStore(okHandler(), "ops", "s3cr3t-pw", store)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, basicReq(t, "ops", "s3cr3t-pw"))
	c := sessionCookie(t, rec)
	if c == nil {
		t.Fatal("no cookie to expire")
	}

	// When
	now = now.Add(time.Hour + time.Second)
	req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
	req.AddCookie(c)
	expired := httptest.NewRecorder()
	gated.ServeHTTP(expired, req)

	// Then
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired cookie code = %d, want 401", expired.Code)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.tokens) != 0 {
		t.Errorf("expired token was not purged: %d entries", len(store.tokens))
	}
}

// Given: a store capped at four tokens.
// When: six tokens are issued.
// Then: the store stays bounded and the oldest tokens are evicted first.
func Test_SessionStore_bounded_evicts_oldest(t *testing.T) {
	// Given
	now := time.UnixMilli(1800000000000)
	store := newSessionStore(4, time.Hour, func() time.Time { return now })
	var issued []string
	for i := 0; i < 6; i++ {
		issued = append(issued, store.issue())
		now = now.Add(time.Second)
	}

	// When / Then
	if len(store.tokens) != 4 {
		t.Fatalf("store size = %d, want cap 4", len(store.tokens))
	}
	for _, token := range issued[:2] {
		if store.valid(token) {
			t.Errorf("evicted token still valid: %s", token)
		}
	}
	for _, token := range issued[4:] {
		if !store.valid(token) {
			t.Errorf("newest token was evicted: %s", token)
		}
	}
}

// Given: a TLS-terminated request.
// When: the gate accepts it.
// Then: the cookie is marked Secure. Over plain HTTP it is not (covered in
// the issue test above), so a tailnet plain-http front end keeps working.
func Test_Middleware_cookie_secure_only_over_tls(t *testing.T) {
	// Given
	req := basicReq(t, "ops", "s3cr3t-pw")
	req.TLS = &tls.ConnectionState{}

	// When
	rec := do(req)

	// Then
	c := sessionCookie(t, rec)
	if c == nil {
		t.Fatal("no cookie issued over TLS")
	}
	if !c.Secure {
		t.Error("cookie must be Secure when the request arrived over TLS")
	}
}

// Given: a valid session cookie plus a wrong Basic header.
// When: the gate evaluates the request.
// Then: the valid cookie authorizes it (credentials are OR-ed), so a stale
// password prompt cannot deadlock the event stream.
func Test_Middleware_valid_cookie_survives_wrong_basic(t *testing.T) {
	// Given
	gated := Middleware(okHandler(), "ops", "s3cr3t-pw")
	first := httptest.NewRecorder()
	gated.ServeHTTP(first, basicReq(t, "ops", "s3cr3t-pw"))
	c := sessionCookie(t, first)
	if c == nil {
		t.Fatal("no cookie to reuse")
	}
	req := basicReq(t, "ops", "wrong-pw")
	req.AddCookie(c)

	// When
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)

	// Then
	if rec.Code != http.StatusOK {
		t.Fatalf("valid cookie plus wrong Basic = %d, want 200", rec.Code)
	}
}
