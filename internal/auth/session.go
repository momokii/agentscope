package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// Session cookie bridge for EventSource. Browsers cannot attach an
// Authorization header to `new EventSource(...)`, so a Basic-authenticated
// page load mints an HttpOnly cookie and the same-origin event stream rides
// on that instead. The cookie is only ever issued AFTER a successful Basic
// check (see middlewareWithStore), so it never becomes a second, independent
// credential in gated mode.
const (
	// SessionCookieName is the cookie that carries a session token.
	SessionCookieName = "ocd_session"
	// DefaultSessionTTL bounds how long an issued token stays valid.
	DefaultSessionTTL = 12 * time.Hour
	// maxSessionTokens caps the in-memory store so repeated auth prompts
	// cannot grow memory without bound.
	maxSessionTokens = 32
	// sessionTokenBytes is the raw entropy per token (hex-encoded on the
	// wire → 64 ASCII characters).
	sessionTokenBytes = 32
)

// sessionStore is a bounded, expiring, in-memory token set. It is not a
// user database: tokens carry no identity beyond "passed Basic at issue
// time". Expired entries are purged on every access.
type sessionStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time
	ttl    time.Duration
	cap    int
	now    func() time.Time
}

func newSessionStore(cap int, ttl time.Duration, now func() time.Time) *sessionStore {
	if cap <= 0 {
		cap = maxSessionTokens
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	if now == nil {
		now = time.Now
	}
	return &sessionStore{
		tokens: make(map[string]time.Time),
		ttl:    ttl,
		cap:    cap,
		now:    now,
	}
}

// issue mints a 32-byte random token and records its expiry. An empty
// string means entropy failed; the caller must then skip the cookie (Basic
// still authorized this request, so no security boundary is crossed).
func (s *sessionStore) issue() string {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	if len(s.tokens) >= s.cap {
		s.evictOldestLocked()
	}
	s.tokens[token] = now.Add(s.ttl)
	return token
}

// valid reports whether token is currently in the store. Expired tokens are
// purged before lookup.
func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	expiry, ok := s.tokens[token]
	return ok && now.Before(expiry)
}

// purgeLocked drops every expired token. Caller holds s.mu.
func (s *sessionStore) purgeLocked(now time.Time) {
	for token, expiry := range s.tokens {
		if !now.Before(expiry) {
			delete(s.tokens, token)
		}
	}
}

// evictOldestLocked removes the soonest-expiring token to make room. Caller
// holds s.mu and the store is at capacity.
func (s *sessionStore) evictOldestLocked() {
	var (
		oldestToken string
		oldestExp   time.Time
		found       bool
	)
	for token, expiry := range s.tokens {
		if !found || expiry.Before(oldestExp) {
			oldestToken = token
			oldestExp = expiry
			found = true
		}
	}
	if found {
		delete(s.tokens, oldestToken)
	}
}

// setSessionCookie issues a token and writes the cookie. Secure is set only
// when the request itself arrived over TLS, so a plain-http tailnet keeps
// working while an HTTPS front end still gets the protection.
func setSessionCookie(w http.ResponseWriter, r *http.Request, store *sessionStore) {
	token := store.issue()
	if token == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(store.ttl / time.Second),
	})
}
