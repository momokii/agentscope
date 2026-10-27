// Command dashboard is the Wave-4 runnable entrypoint for opencode mission
// control: a single Go binary serving the embedded SPA plus the Wave-1 API.
//
// Wiring (frozen seams only, no new contracts):
//
//	ports.LoadConfigFromEnv → db.NewReader → runtime.NewSource →
//	hub.NewServer (+SetScreenSource +SetDetailSource) → collector.NewCollector → Run loop +
//	combined HTTP listener (API routes take precedence, SPA fallback).
//
// Server.Run is deliberately NOT used: it binds its own listener serving
// API routes only, while this binary must serve API + SPA on ONE loopback
// listener. The hub fan-out loop (srv.Hub().Run) and route mux
// (srv.Handler()) are reused unchanged.
//
// Security posture (W4-2 owns auth; this file only reserves the hook):
//   - binds cfg.Addr() (127.0.0.1 ONLY, asserted at startup — never 0.0.0.0);
//   - tailnet exposure is via `tailscale serve`, never `funnel`;
//   - auth.json / credential tables are never mounted (see
//     docker-compose.yml allowlist) and never read here.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dashboardweb "opencode-dashboard/web"

	"opencode-dashboard/internal/auth"
	"opencode-dashboard/internal/collector"
	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/hub"
	"opencode-dashboard/internal/pi"
	"opencode-dashboard/internal/ports"
	"opencode-dashboard/internal/runtime"
)

const (
	// shutdownTimeout bounds graceful drain (in-flight API + SSE viewers).
	shutdownTimeout = 10 * time.Second
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe 127.0.0.1:$PORT/api/healthz and exit 0/1 (container HEALTHCHECK)")
	flag.Parse()
	if *healthcheck {
		os.Exit(runHealthcheck())
	}
	if err := run(context.Background()); err != nil {
		// LOUD fatal: deploy-time misconfig must never degrade silently.
		_, _ = fmt.Fprintln(os.Stderr, "dashboard: FATAL:", err)
		os.Exit(1)
	}
}

// gateWithAuth applies the W4-2 Basic gate (docs/SECURITY.md §7) to the
// already-routed top mux (API + SPA). Empty/unset DASHBOARD_AUTH returns
// top unchanged (open tailnet-trust mode); malformed config returns a
// fail-closed error matching auth.ErrInvalidAuthConfig via errors.Is.
func gateWithAuth(top http.Handler) (http.Handler, error) {
	creds, err := auth.CredentialsFromEnv()
	if err != nil {
		return nil, fmt.Errorf("dashboard auth: %w", err)
	}
	if !creds.Enabled {
		return top, nil
	}
	return auth.Middleware(top, creds.Username, creds.Password), nil
}

// run loads config, enforces the deploy-time self-checks, wires the frozen
// Wave-1 seams, and serves until SIGINT/SIGTERM.
func run(ctx context.Context) error {
	cfg := ports.LoadConfigFromEnv()
	if err := assertLoopback(cfg); err != nil {
		return err
	}
	info, err := selfChecks(cfg)
	if err != nil {
		return err
	}
	slog.Info("dashboard starting",
		slog.String("addr", cfg.Addr()),
		slog.String("data_dir", cfg.DataDir),
		slog.String("zellij", info.zellijVersion),
		slog.Duration("poll_interval", cfg.PollInterval),
	)

	reader, err := db.NewWithWindow(cfg.DataDir, cfg.ActiveWindow.Milliseconds())
	if err != nil {
		return fmt.Errorf("open DATA_DIR %s read-only: %w — check the :ro mount and UID parity (container UID=%d vs host UID from `id -u`)", cfg.DataDir, err, os.Getuid())
	}
	defer func() { _ = reader.Close() }()

	// Pi is best-effort presence next to the authoritative DB join: an
	// empty PiDataDir disables the merge, and a missing/unreadable store
	// degrades per tick (warn-once) instead of failing startup — an
	// opencode-only host must boot exactly as before.
	var piReader ports.PiReader
	if cfg.PiDataDir != "" {
		piReader = pi.NewWithWindow(cfg.PiDataDir, cfg.ActiveWindow.Milliseconds())
		slog.Info("pi source enabled", slog.String("pi_data_dir", cfg.PiDataDir))
	} else {
		slog.Info("pi source disabled (PI_DATA_DIR empty)")
	}

	source := runtime.NewSource(runtime.DefaultConfig())
	srv := hub.NewServer(cfg, hub.NewHub())
	srv.SetScreenSource(source)
	srv.SetDetailSource(reader)
	srv.SetExportSource(reader)
	col, err := collector.NewCollector(reader, source, srv.Hub(), cfg, piReader)
	if err != nil {
		return fmt.Errorf("wire collector: %w", err)
	}

	spa, err := fs.Sub(dashboardweb.Dist, "dist")
	if err != nil {
		return fmt.Errorf("embedded SPA: %w", err)
	}
	spaHandler, err := newSPAHandler(spa)
	if err != nil {
		return err
	}
	top := http.NewServeMux()
	top.Handle("/api/", srv.Handler())
	top.Handle("/", spaHandler)
	gated, err := gateWithAuth(top)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           gated,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout by design: SSE streams stay open far past any
		// sane deadline and a timeout would kill them mid-stream.
	}

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hubDone := make(chan error, 1)
	go func() { hubDone <- srv.Hub().Run(sigCtx) }()
	colDone := make(chan error, 1)
	go func() { colDone <- col.Run(sigCtx) }()
	serveDone := make(chan error, 1)
	go func() { serveDone <- httpSrv.ListenAndServe() }()

	select {
	case err := <-serveDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve %s: %w", cfg.Addr(), err)
		}
		return nil
	case <-sigCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	// Hub drains SSE viewers on sigCtx cancel; collector stops its poll loop.
	if err := <-hubDone; err != nil {
		return fmt.Errorf("hub drain: %w", err)
	}
	if err := <-colDone; err != nil {
		return fmt.Errorf("collector stop: %w", err)
	}
	return nil
}
