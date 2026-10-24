package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// okHandler is the downstream protected handler.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// basicReq builds a GET with an explicit Basic Authorization header.
func basicReq(t *testing.T, user, pass string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
	req.SetBasicAuth(user, pass)
	return req
}

// do runs req through Middleware and returns the recorder.
func do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	Middleware(okHandler(), "ops", "s3cr3t-pw").ServeHTTP(rec, req)
	return rec
}

func Test_Middleware_passes_through_when_credentials_match(t *testing.T) {
	// Given: a request carrying the exact configured credentials
	req := basicReq(t, "ops", "s3cr3t-pw")

	// When: the gate evaluates it
	rec := do(req)

	// Then: downstream runs, no challenge issued
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want downstream response", rec.Body.String())
	}
	if rec.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("WWW-Authenticate set on success: %q", rec.Header().Get("WWW-Authenticate"))
	}
}

func Test_Middleware_rejects_wrong_password_with_challenge(t *testing.T) {
	// Given: right user, wrong password
	req := basicReq(t, "ops", "wrong-pw")

	// When
	rec := do(req)

	// Then: 401 + realm challenge, downstream never runs
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	want := `Basic realm="opencode-dashboard"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, want)
	}
	if rec.Body.String() != "" {
		t.Fatalf("401 body = %q, want empty (no hints)", rec.Body.String())
	}
}

func Test_Middleware_rejects_wrong_username_with_challenge(t *testing.T) {
	// Given: unknown user, right password
	req := basicReq(t, "intruder", "s3cr3t-pw")

	// When
	rec := do(req)

	// Then: 401 — username and password are indistinguishable to the caller
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("WWW-Authenticate = %q, want Basic challenge", got)
	}
}

func Test_Middleware_rejects_missing_header(t *testing.T) {
	// Given: no Authorization header at all
	req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)

	// When
	rec := do(req)

	// Then: 401 + challenge
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate on 401")
	}
}

func Test_Middleware_rejects_malformed_header(t *testing.T) {
	// Given: a non-Basic Authorization scheme
	req := httptest.NewRequest(http.MethodGet, "/api/fleet", nil)
	req.Header.Set("Authorization", "Bearer sometoken")

	// When
	rec := do(req)

	// Then: 401 — Basic-only gate, no fallback schemes
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
}

func Test_Middleware_never_echoes_credentials(t *testing.T) {
	// Given: the real password and several wrong guesses
	secrets := []string{"ops", "s3cr3t-pw"}
	requests := []*http.Request{
		basicReq(t, "ops", "wrong-guess"),
		basicReq(t, "ops", "s3cr3t-pw-extra"),
		httptest.NewRequest(http.MethodGet, "/api/fleet", nil),
	}

	// When: each is rejected (or, for control, accepted)
	for i, req := range requests {
		rec := do(req)

		// Then: neither the 401 body nor any response header contains
		// the username or the password
		body, _ := io.ReadAll(rec.Result().Body)
		for _, secret := range secrets {
			if strings.Contains(string(body), secret) {
				t.Fatalf("request %d: 401 body leaks %q", i, secret)
			}
			for k, vs := range rec.Result().Header {
				for _, v := range vs {
					if strings.Contains(v, secret) {
						t.Fatalf("request %d: header %s leaks %q", i, k, secret)
					}
				}
			}
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d: code = %d, want 401", i, rec.Code)
		}
	}
}

func Test_ParseCredentials_empty_means_open_tailnet_trust(t *testing.T) {
	// Given: unset-style empty value
	// When
	creds, err := ParseCredentials("")

	// Then: disabled, no error — open mode is explicit, not accidental
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if creds.Enabled {
		t.Fatal("Enabled = true, want false for empty value")
	}
}

func Test_ParseCredentials_accepts_user_colon_pass(t *testing.T) {
	// Given: a well-formed value
	// When
	creds, err := ParseCredentials("ops:s3cr3t-pw")

	// Then
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !creds.Enabled || creds.Username != "ops" || creds.Password != "s3cr3t-pw" {
		t.Fatalf("creds = %+v, want enabled ops", creds)
	}
}

func Test_ParseCredentials_allows_colons_in_password(t *testing.T) {
	// Given: password containing colons
	// When
	creds, err := ParseCredentials("ops:a:b:c")

	// Then: split on FIRST colon only
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if creds.Password != "a:b:c" {
		t.Fatalf("password = %q, want %q", creds.Password, "a:b:c")
	}
}

func Test_ParseCredentials_fails_closed_without_colon(t *testing.T) {
	// Given: no colon separator
	// When
	_, err := ParseCredentials("justausername")

	// Then: typed fail-closed error, matchable via errors.Is
	if err == nil {
		t.Fatal("err = nil, want fail-closed error")
	}
	if !errors.Is(err, ErrInvalidAuthConfig) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidAuthConfig", err)
	}
	var cfgErr *AuthConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("err type = %T, want *AuthConfigError", err)
	}
	if strings.Contains(err.Error(), "justausername") {
		t.Fatalf("error echoes input value: %q", err.Error())
	}
}

func Test_ParseCredentials_fails_closed_on_empty_user(t *testing.T) {
	// Given: empty username
	// When
	_, err := ParseCredentials(":nonempty")

	// Then: fail closed
	if !errors.Is(err, ErrInvalidAuthConfig) {
		t.Fatalf("err = %v, want ErrInvalidAuthConfig", err)
	}
}

func Test_ParseCredentials_fails_closed_on_empty_password(t *testing.T) {
	// Given: empty password (typo-class, fail closed by policy)
	// When
	_, err := ParseCredentials("ops:")

	// Then: fail closed
	if !errors.Is(err, ErrInvalidAuthConfig) {
		t.Fatalf("err = %v, want ErrInvalidAuthConfig", err)
	}
}

func Test_CredentialsFromEnv_reads_env_var(t *testing.T) {
	// Given: DASHBOARD_AUTH set in the environment
	t.Setenv(EnvVar, "ops:from-env")

	// When
	creds, err := CredentialsFromEnv()

	// Then
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !creds.Enabled || creds.Username != "ops" || creds.Password != "from-env" {
		t.Fatalf("creds = %+v, want enabled ops", creds)
	}
}

func Test_Package_emits_no_logs(t *testing.T) {
	// Given: every non-test .go file in this package
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	// When: scanning for any logging/output import or call
	// Then: none exist — credentials have no log pipeline to leak through.
	// (If this fails, the auth package grew a logging path: remove it,
	// do not widen this list.)
	banned := []string{
		"\"log\"", "\"log/slog\"", "\"fmt\"", // import paths
		"log.", "slog.", "fmt.Print", // call sites
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(raw)
		for _, b := range banned {
			if strings.Contains(src, b) {
				t.Fatalf("%s contains %q: auth package must not log", f, b)
			}
		}
	}
}

// Test_Timing_compare_uses_constant_time documents the timing-safety
// mechanism asserted by inspection: Middleware hashes both sides with
// SHA-256 (hiding length) and compares digests with
// subtle.ConstantTimeCompare. This test pins the mechanism's presence in
// source so a future edit cannot silently swap it for ==.
func Test_Timing_compare_uses_constant_time(t *testing.T) {
	// Given: the middleware source
	raw, err := os.ReadFile("basic.go")
	if err != nil {
		t.Fatalf("read basic.go: %v", err)
	}
	src := string(raw)

	// When/Then: both pins hold
	for _, pin := range []string{"subtle.ConstantTimeCompare", "sha256.Sum256"} {
		if !strings.Contains(src, pin) {
			t.Fatalf("basic.go missing %q: timing-safe compare regressed", pin)
		}
	}
}
