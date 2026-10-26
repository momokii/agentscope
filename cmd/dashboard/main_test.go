package main

import (
	"errors"
	"io/fs"
	"net"
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
// When: the loopback assertion runs with auth disabled.
// Then: it rejects each with ErrNonLoopbackBind (startup must refuse).
func Test_AssertLoopback_RejectsNonLoopback(t *testing.T) {
	t.Setenv(auth.EnvVar, "")
	for _, addr := range []string{"", "0.0.0.0", "::", "192.168.1.10", "10.0.0.5", "example.com"} {
		cfg := ports.DefaultConfig()
		cfg.BindAddr = addr
		if err := assertLoopback(cfg); !errors.Is(err, ErrNonLoopbackBind) {
			t.Errorf("bind %q must be rejected with ErrNonLoopbackBind, got %v", addr, err)
		}
	}
}

// Given: the bind/auth guard matrix (BIND_ADDR x DASHBOARD_AUTH).
// When: assertLoopback runs (the single guard decision site).
// Then: loopback serves with or without auth; non-loopback serves only
// with valid auth; unset or malformed auth on non-loopback refuses with
// ErrNonLoopbackBind and never echoes credential values.
func Test_AssertLoopback_NonLoopbackRequiresAuth(t *testing.T) {
	tests := []struct {
		name    string
		bind    string
		authRaw *string // nil = leave env alone; caller sets explicitly per row
		wantErr bool
	}{
		{name: "loopback no-auth serves", bind: "127.0.0.1", authRaw: strPtr(""), wantErr: false},
		{name: "loopback authed serves", bind: "127.0.0.1", authRaw: strPtr("op:throwaway-fixture-1"), wantErr: false},
		{name: "wildcard authed serves", bind: "0.0.0.0", authRaw: strPtr("op:throwaway-fixture-2"), wantErr: false},
		{name: "wildcard no-auth refuses", bind: "0.0.0.0", authRaw: strPtr(""), wantErr: true},
		{name: "wildcard malformed-auth refuses", bind: "0.0.0.0", authRaw: strPtr("no-colon-fixture"), wantErr: true},
		{name: "v6 wildcard no-auth refuses", bind: "::", authRaw: strPtr(""), wantErr: true},
		{name: "hostname no-auth refuses", bind: "example.com", authRaw: strPtr(""), wantErr: true},
		{name: "rfc1918 tailnet-range no-auth refuses", bind: "100.64.0.1", authRaw: strPtr(""), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.authRaw != nil {
				t.Setenv(auth.EnvVar, *tt.authRaw)
			}
			cfg := ports.DefaultConfig()
			cfg.BindAddr = tt.bind
			err := assertLoopback(cfg)
			if tt.wantErr {
				if !errors.Is(err, ErrNonLoopbackBind) {
					t.Fatalf("bind %q auth %q must refuse with ErrNonLoopbackBind, got %v", tt.bind, *tt.authRaw, err)
				}
				if !strings.Contains(err.Error(), "BIND_ADDR") || !strings.Contains(err.Error(), "DASHBOARD_AUTH") {
					t.Fatalf("refusal must name both env vars, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("bind %q must serve, got %v", tt.bind, err)
			}
		})
	}
}

// Given: a non-loopback bind with malformed auth carrying a secret-like
// fragment.
// When: the guard refuses.
// Then: the error text contains neither the raw value nor the password
// fragment (no credential logging).
func Test_AssertLoopback_MalformedAuth_NeverEchoesSecrets(t *testing.T) {
	const secretPass = "onlypass-leakcheck-9c4e2b"
	t.Setenv(auth.EnvVar, ":"+secretPass)
	cfg := ports.DefaultConfig()
	cfg.BindAddr = "0.0.0.0"
	err := assertLoopback(cfg)
	if !errors.Is(err, ErrNonLoopbackBind) {
		t.Fatalf("must refuse with ErrNonLoopbackBind, got %v", err)
	}
	if !errors.Is(err, auth.ErrInvalidAuthConfig) {
		t.Fatalf("malformed auth must chain ErrInvalidAuthConfig, got %v", err)
	}
	if strings.Contains(err.Error(), secretPass) {
		t.Fatalf("refusal must never echo credential values: %v", err)
	}
}

func strPtr(s string) *string { return &s }

func probePort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split test server addr: %v", err)
	}
	return port
}

// Given: an authed server gating /api/healthz behind Basic auth (password
// carries colons to pin first-colon split) and the probe env carrying the
// same DASHBOARD_AUTH value.
// When: runHealthcheck probes the local port.
// Then: it attaches Basic credentials and exits 0.
func Test_ProbeWithAuth_AgainstAuthedServer_ReturnsExitZero(t *testing.T) {
	const user = "op"
	const pass = "p:a:ss-fixture-1"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	srv := httptest.NewServer(auth.Middleware(inner, user, pass))
	defer srv.Close()
	t.Setenv("PORT", probePort(t, srv))
	t.Setenv(auth.EnvVar, user+":"+pass)
	got := runHealthcheck()
	if got != 0 {
		t.Fatalf("authed probe against authed server = %d, want 0", got)
	}
}

// Given: the same authed server but the probe env is empty (no creds).
// When: runHealthcheck probes without credentials.
// Then: the gate 401s and the probe exits non-zero.
func Test_ProbeWithoutAuth_AgainstAuthedServer_ReturnsNonZero(t *testing.T) {
	const user = "op"
	const pass = "probe-noauth-fixture-2"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	})
	srv := httptest.NewServer(auth.Middleware(inner, user, pass))
	defer srv.Close()
	t.Setenv("PORT", probePort(t, srv))
	t.Setenv(auth.EnvVar, "")
	got := runHealthcheck()
	if got == 0 {
		t.Fatal("unauthenticated probe against authed server = 0, want non-zero")
	}
}

// Given: an open server (no gate) and empty probe env.
// When: runHealthcheck probes.
// Then: it exits 0 (default path untouched by the auth change).
func Test_ProbeAgainstOpenServer_ReturnsExitZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := r.BasicAuth(); ok {
			t.Error("open-mode probe must not send credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	}))
	defer srv.Close()
	t.Setenv("PORT", probePort(t, srv))
	t.Setenv(auth.EnvVar, "")
	got := runHealthcheck()
	if got != 0 {
		t.Fatalf("open probe against open server = %d, want 0", got)
	}
}

// Given: DASHBOARD_AUTH malformed (no colon / empty user / empty password)
// against an open server.
// When: runHealthcheck probes.
// Then: it does not crash and behaves as unset (exit 0). The server would
// have refused to start on malformed auth anyway, so the probe invents no
// new failure mode.
func Test_ProbeMalformedEnv_DoesNotCrash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"healthy":true}`))
	}))
	defer srv.Close()
	t.Setenv("PORT", probePort(t, srv))
	for _, raw := range []string{"no-colon-fixture", ":onlypass", "someuser:"} {
		t.Setenv(auth.EnvVar, raw)
		got := runHealthcheck()
		if got != 0 {
			t.Errorf("malformed-env probe = %d, want 0 (treated as unset)", got)
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

// Given: DASHBOARD_AUTH unset (empty) and a request that happens to carry
// Basic credentials.
// When: gateWithAuth wraps a stub mux.
// Then: it passes through open and issues no session cookie (open mode must
// be byte-for-byte unchanged).
func Test_GateWithAuth_OpenMode_IssuesNoCookie(t *testing.T) {
	t.Setenv(auth.EnvVar, "")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	gated, err := gateWithAuth(inner)
	if err != nil {
		t.Fatalf("open mode must not error: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("any", "thing")
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("open mode = %d, want 200", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			t.Fatalf("open mode issued a session cookie: %+v", c)
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
