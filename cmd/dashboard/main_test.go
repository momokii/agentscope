package main

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashboardweb "opencode-dashboard/web"

	"opencode-dashboard/internal/auth"
	"opencode-dashboard/internal/hub"
	"opencode-dashboard/internal/ports"
)

// Given: the frozen default config (127.0.0.1 bind, no env override).
// When: the loopback assertion runs.
// Then: it accepts (startup must proceed on the default config).
func Test_AssertLoopback_AcceptsDefaultConfig(t *testing.T) {
	cfg := ports.DefaultConfig()
	if err := assertLoopback(cfg); err != nil {
		t.Fatalf("default config must bind loopback: %v", err)
	}
	if got := cfg.Addr(); got != "127.0.0.1:8080" {
		t.Fatalf("default addr = %q, want %q", got, "127.0.0.1:8080")
	}
}

// Given: loopback bind addresses (v4 + v6).
// When: the loopback assertion runs.
// Then: it accepts each.
func Test_AssertLoopback_AcceptsLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "::1"} {
		cfg := ports.DefaultConfig()
		cfg.BindAddr = addr
		if err := assertLoopback(cfg); err != nil {
			t.Errorf("bind %q must be accepted: %v", addr, err)
		}
	}
}

// Given: non-loopback bind addresses (including the catastrophic 0.0.0.0).
// When: the loopback assertion runs.
// Then: it rejects each with ErrNonLoopbackBind (startup must refuse).
func Test_AssertLoopback_RejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"", "0.0.0.0", "::", "192.168.1.10", "10.0.0.5", "example.com"} {
		cfg := ports.DefaultConfig()
		cfg.BindAddr = addr
		if err := assertLoopback(cfg); !errors.Is(err, ErrNonLoopbackBind) {
			t.Errorf("bind %q must be rejected with ErrNonLoopbackBind, got %v", addr, err)
		}
	}
}

// Given: the real `zellij --version` output shape.
// When: the major parser runs.
// Then: it yields the contract-relevant major ("0" for 0.44.3).
func Test_ZellijMajor_ParsesContractMajor(t *testing.T) {
	if got := zellijMajor("zellij 0.44.3"); got != "0" {
		t.Fatalf("major = %q, want %q", got, "0")
	}
	if got := zellijMajor("garbage"); got != "" {
		t.Fatalf("unparseable input must yield empty major, got %q", got)
	}
}

// Given: the embedded production SPA.
// When: an unknown (hash-route) path is requested.
// Then: it serves the same bytes as / with 200 (client-side boot).
func Test_SPA_FallsBackToIndexHTML(t *testing.T) {
	spa, err := fs.Sub(dashboardweb.Dist, "dist")
	if err != nil {
		t.Fatalf("embedded SPA: %v", err)
	}
	h, err := newSPAHandler(spa)
	if err != nil {
		t.Fatalf("newSPAHandler: %v", err)
	}
	get := func(target string) (int, []byte) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, rec.Body.Bytes()
	}
	rootCode, rootBody := get("/")
	if rootCode != http.StatusOK || len(rootBody) == 0 {
		t.Fatalf("GET / = %d (%d bytes), want 200 with body", rootCode, len(rootBody))
	}
	deepCode, deepBody := get("/sessions/abc/def")
	if deepCode != http.StatusOK {
		t.Fatalf("GET deep route = %d, want 200 fallback", deepCode)
	}
	if string(deepBody) != string(rootBody) {
		t.Fatal("deep route must serve the index.html fallback bytes")
	}
}

// Given: DASHBOARD_AUTH unset (empty).
// When: gateWithAuth wraps a stub mux.
// Then: requests pass through open (200, no credentials needed).
func Test_GateWithAuth_OpenMode_PassesThrough(t *testing.T) {
	t.Setenv(auth.EnvVar, "")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	gated, err := gateWithAuth(inner)
	if err != nil {
		t.Fatalf("open mode must not error: %v", err)
	}
	for _, target := range []string{"/api/healthz", "/"} {
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s open mode = %d, want 200", target, rec.Code)
		}
	}
}

// Given: DASHBOARD_AUTH set to a valid user:pass.
// When: requests hit the gated handler with no/wrong/correct credentials.
// Then: 401 + WWW-Authenticate + empty body without-or-with-wrong creds
// (API and SPA paths alike); 200 with correct creds.
func Test_GateWithAuth_GatedMode_ChallengesAndAccepts(t *testing.T) {
	t.Setenv(auth.EnvVar, "gateuser:gatepass-7f3a9c")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	gated, err := gateWithAuth(inner)
	if err != nil {
		t.Fatalf("gated mode must not error: %v", err)
	}
	for _, target := range []string{"/api/healthz", "/"} {
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without creds = %d, want 401", target, rec.Code)
		}
		if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, `Basic realm="`+auth.Realm+`"`) {
			t.Errorf("GET %s WWW-Authenticate = %q, want Basic realm %q", target, got, auth.Realm)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("GET %s 401 body = %d bytes, want empty", target, rec.Body.Len())
		}

		wrong := httptest.NewRequest(http.MethodGet, target, nil)
		wrong.SetBasicAuth("gateuser", "wrongpass")
		recWrong := httptest.NewRecorder()
		gated.ServeHTTP(recWrong, wrong)
		if recWrong.Code != http.StatusUnauthorized {
			t.Errorf("GET %s wrong pass = %d, want 401", target, recWrong.Code)
		}

		okReq := httptest.NewRequest(http.MethodGet, target, nil)
		okReq.SetBasicAuth("gateuser", "gatepass-7f3a9c")
		recOK := httptest.NewRecorder()
		gated.ServeHTTP(recOK, okReq)
		if recOK.Code != http.StatusOK {
			t.Errorf("GET %s correct creds = %d, want 200", target, recOK.Code)
		}
	}
}

// Given: DASHBOARD_AUTH set and the API mux serving the newly-wired detail
// route with no backend (nil DetailSource → 503 when authorized).
// When: requests hit /api/sessions/:id without creds, then with creds.
// Then: 401 without creds (the gate covers the detail route too); 503
// with creds (route reached, backend unwired in the test server).
func Test_GateWithAuth_GatedDetailRoute_Challenges(t *testing.T) {
	t.Setenv(auth.EnvVar, "gateuser:gatepass-7f3a9c")
	srv := hub.NewServer(ports.DefaultConfig(), hub.NewHub())
	top := http.NewServeMux()
	top.Handle("/api/", srv.Handler())
	gated, err := gateWithAuth(top)
	if err != nil {
		t.Fatalf("gated mode must not error: %v", err)
	}

	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/ses_x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET detail without creds = %d, want 401", rec.Code)
	}

	okReq := httptest.NewRequest(http.MethodGet, "/api/sessions/ses_x", nil)
	okReq.SetBasicAuth("gateuser", "gatepass-7f3a9c")
	recOK := httptest.NewRecorder()
	gated.ServeHTTP(recOK, okReq)
	if recOK.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET detail with creds = %d, want 503 (route reached, no backend)", recOK.Code)
	}
}

// Given: DASHBOARD_AUTH malformed (no colon / empty user / empty password).
// When: gateWithAuth parses it.
// Then: it fails closed with ErrInvalidAuthConfig and the error carries
// the Reason only — never the credential value.
func Test_GateWithAuth_Malformed_FailsClosed(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	for _, raw := range []string{"no-colon-here", ":onlypass-leakcheck-4d2e", "someuser:"} {
		t.Setenv(auth.EnvVar, raw)
		_, err := gateWithAuth(inner)
		if !errors.Is(err, auth.ErrInvalidAuthConfig) {
			t.Errorf("raw %q must fail closed with ErrInvalidAuthConfig, got %v", raw, err)
			continue
		}
		if err == nil || strings.Contains(err.Error(), raw) {
			t.Errorf("raw %q error must not echo the credential value: %v", raw, err)
		}
		if strings.Contains(err.Error(), "onlypass-leakcheck-4d2e") {
			t.Errorf("error must not contain the password fragment: %v", err)
		}
	}
}
