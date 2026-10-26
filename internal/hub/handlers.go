package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// Version is the Wave-1 API version reported by /api/healthz.
const Version = "0.1.0"

// shutdownTimeout bounds graceful drain of listeners and viewers.
const shutdownTimeout = 5 * time.Second

// HealthResponse is GET /api/healthz. Field list frozen per the work plan:
// {healthy, version, db_path, last_poll_ms, stale}.
type HealthResponse struct {
	Healthy    bool   `json:"healthy"`
	Version    string `json:"version"`
	DBPath     string `json:"db_path"`
	LastPollMs int64  `json:"last_poll_ms"`
	Stale      bool   `json:"stale"`
}

// FleetResponse is GET /api/fleet. Field list frozen per the work plan:
// {seq, sessions, projects, kpis}. Sessions reuse model.SessionRow tags so
// Wave 2 consumes identical shapes from REST and SSE frames.
type FleetResponse struct {
	Seq      uint64                 `json:"seq"`
	Sessions []model.SessionRow     `json:"sessions"`
	Projects []model.ProjectSummary `json:"projects"`
	KPIs     model.KPIs             `json:"kpis"`
}

// DebugResponse is GET /api/debug/hub. It is deliberately NOT part of the
// frozen contract: drop counters and cadence live here so the frozen shapes
// never carry hub internals.
type DebugResponse struct {
	Viewers        int    `json:"viewers"`
	DroppedFrames  int64  `json:"dropped_frames"`
	DroppedViewers int64  `json:"dropped_viewers"`
	Seq            uint64 `json:"seq"`
	HasSnapshot    bool   `json:"has_snapshot"`
	TickMs         int64  `json:"tick_ms"`
	KeepaliveS     int64  `json:"keepalive_s"`
}

// Server is the Wave-1 HTTP surface: frozen REST routes plus the SSE hub.
// It binds ports.Config.Addr() ONLY (127.0.0.1 by design) and emits no
// CORS-* headers (same-origin embedded SPA — see doc.go).
type Server struct {
	cfg     ports.Config
	hub     *Hub
	version string
	mux     *http.ServeMux
	srv     *http.Server
	// screen is the on-demand live-pane backend (see screen.go). Nil until
	// SetScreenSource wires it; the screen route serves 503 while nil.
	screen ScreenDumper
	// detail is the on-demand drill-down backend (see detail.go). Nil
	// until SetDetailSource wires it; the detail and tools routes serve
	// 503 while nil.
	detail DetailSource
}

// NewServer builds a Server reporting Version.
func NewServer(cfg ports.Config, h *Hub) *Server {
	return NewServerWithVersion(cfg, h, Version)
}

// NewServerWithVersion builds a Server reporting an explicit version
// (tests pin non-default versions without touching the frozen shape).
func NewServerWithVersion(cfg ports.Config, h *Hub, version string) *Server {
	s := &Server{cfg: cfg, hub: h, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/fleet", s.handleFleet)
	mux.HandleFunc("GET /api/sessions/{id}/tools", s.handleSessionTools)
	mux.HandleFunc("GET /api/sessions/{id}/screen", s.handleSessionScreen)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleSessionDetail)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/debug/hub", s.handleDebug)
	s.mux = mux
	// WriteTimeout is intentionally unset: SSE streams stay open far past
	// any sane write deadline and a timeout would kill them mid-stream.
	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// Addr returns the bind address:port pair. It always derives from
// ports.Config.Addr() — never a literal.
func (s *Server) Addr() string {
	return s.cfg.Addr()
}

// Handler exposes the full handler chain for httptest-driven tests.
// Before any Use call it is the route mux; after Use it includes every
// wrapped middleware (auth, ...), so tests exercise the served chain.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

// Use wraps the served chain with one middleware (outermost last).
// Wave-4 wiring (see internal/auth + docs/SECURITY.md): W4-1's main.go
// applies the gate when DASHBOARD_AUTH is set — exactly:
//
//	creds, err := auth.CredentialsFromEnv() // err != nil → fail closed, refuse to serve
//	if creds.Enabled {
//	    srv.Use(func(next http.Handler) http.Handler {
//	        return auth.Middleware(next, creds.Username, creds.Password)
//	    })
//	}
//
// Use must be called before Run. It only re-points srv.Handler; routes
// stay registered on the inner mux.
func (s *Server) Use(mw func(http.Handler) http.Handler) {
	s.srv.Handler = mw(s.srv.Handler)
}

// Hub returns the underlying fan-out hub.
func (s *Server) Hub() *Hub {
	return s.hub
}

// Run serves on cfg.Addr() with the hub fan-out loop attached, and blocks
// until ctx is cancelled. On cancel it drains viewers (hub), closes the
// listener, and gives in-flight requests shutdownTimeout to finish.
func (s *Server) Run(ctx context.Context) error {
	hubCtx, stopHub := context.WithCancel(ctx)
	defer stopHub()
	hubDone := make(chan struct{})
	go func() {
		defer close(hubDone)
		_ = s.hub.Run(hubCtx)
	}()

	// The ONLY listener in this package. Address comes from Config by
	// design; grep must show no 0.0.0.0 or literal :PORT listener here.
	ln, err := net.Listen("tcp", s.cfg.Addr())
	if err != nil {
		return fmt.Errorf("hub: listen %s: %w", s.cfg.Addr(), err)
	}
	errCh := make(chan error, 1)
	go func() {
		if serr := s.srv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("hub: serve: %w", serr)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		stopHub()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if serr := s.srv.Shutdown(shutdownCtx); serr != nil {
			return fmt.Errorf("hub: shutdown: %w", serr)
		}
		<-hubDone
		return nil
	case serr := <-errCh:
		stopHub()
		<-hubDone
		return serr
	}
}

// handleHealthz serves GET /api/healthz → frozen HealthResponse.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	_ = r
	resp := HealthResponse{
		Healthy: true,
		Version: s.version,
		DBPath:  s.cfg.DataDir,
	}
	if snap, ok := s.hub.Latest(); ok {
		resp.LastPollMs = snap.LastPollMs
		resp.Stale = snap.Stale
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

// handleFleet serves GET /api/fleet → frozen FleetResponse built from the
// latest tick (empty fleet before the first Publish).
func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	_ = r
	resp := FleetResponse{
		Sessions: []model.SessionRow{},
		Projects: []model.ProjectSummary{},
	}
	if snap, ok := s.hub.Latest(); ok {
		resp.Seq = snap.Seq
		resp.Sessions = snap.Sessions
		resp.Projects = snap.Projects
		resp.KPIs = snap.KPIs
		if resp.Sessions == nil {
			resp.Sessions = []model.SessionRow{}
		}
		if resp.Projects == nil {
			resp.Projects = []model.ProjectSummary{}
		}
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

// handleDebug serves GET /api/debug/hub → drop counters and cadence.
// Non-frozen by design; Wave 2 must not consume it.
func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	_ = r
	resp := DebugResponse{
		Viewers:        s.hub.ViewerCount(),
		DroppedFrames:  s.hub.DroppedFrames(),
		DroppedViewers: s.hub.DroppedViewers(),
		HasSnapshot:    false,
		TickMs:         s.hub.TickInterval().Milliseconds(),
		KeepaliveS:     int64(s.hub.KeepaliveInterval().Seconds()),
	}
	if snap, ok := s.hub.Latest(); ok {
		resp.Seq = snap.Seq
		resp.HasSnapshot = true
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

// writeJSONBytes sends a pre-marshalled JSON body. Generic client errors
// only; server details never leave the process.
func writeJSONBytes(w http.ResponseWriter, status int, payload []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

// writeJSONError sends a generic {"code","message"} error body.
func writeJSONError(w http.ResponseWriter, status int, code string) {
	payload, err := json.Marshal(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: http.StatusText(status)})
	if err != nil {
		status = http.StatusInternalServerError
		payload = []byte(`{"code":"internal","message":"Internal Server Error"}`)
	}
	writeJSONBytes(w, status, payload)
}
