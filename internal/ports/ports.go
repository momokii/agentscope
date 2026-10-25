// Package ports owns the Wave-1 seam contracts: the interfaces the four
// parallel builders implement against and the environment config they share.
//
// Import rule: ports imports model only. It never imports db, runtime, hub,
// or collector. Implementations (db, runtime) satisfy the source interfaces;
// the fan-out sink is satisfied by hub; collector wires all three.
//
// Consumers: W1-1 implements DBReader, W1-2 implements RuntimeSource, W1-3
// implements SnapshotSink + serves Config.BindAddr, W1-4 ticks on
// Config.PollInterval and stitches rows into model.Snapshot.
package ports

import (
	"context"
	"os"
	"time"

	"opencode-dashboard/internal/model"
)

// DBReader is the read-only opencode.db source (W1-1 owns the implementation).
// All queries must be indexed/sliced (per-session + LIMIT); full part.data
// scans against the 8.2GB WAL db are banned. Open read-only; never disturb
// live TUIs.
type DBReader interface {
	// Sessions returns one SessionRow per session with content-side fields
	// filled (id, title, project, directory, model, tokens incl. subagent
	// roll-up via parent_id, cost) and status signals applied. Pane/orphan
	// fields are left zero — RuntimeSource fills those.
	Sessions(ctx context.Context) ([]model.SessionRow, error)
	// SessionDetail returns the drill-down payload for one session:
	// content-side row plus chronological messages, todos, direct children,
	// and tool calls. Unknown ids report an error matching db.ErrNotFound
	// via errors.Is (callers must not compare strings); hub maps it to 404.
	// Pane/orphan fields stay zero — hub overlays them from its snapshot.
	SessionDetail(ctx context.Context, id string) (model.SessionDetail, error)
	// PollState returns the watermark for the cheapest freshness check:
	// MAX(session time_updated) and MAX(event rowid). The collector uses it
	// to skip a full snapshot when nothing moved.
	PollState(ctx context.Context) (lastUpdateMs int64, lastEventRowID int64, err error)
}

// RuntimeSource is the zellij + /proc presence join (W1-2 owns the
// implementation). It enriches DB rows with live pane truth and flags
// orphans (opencode procs with no zellij pane → dump-screen unavailable).
type RuntimeSource interface {
	// Enrich overlays pane liveness onto rows in place order: fills Pane and
	// Orphan per session. Union of zellij panes (EXITED suffix aware) plus
	// /proc cmdline orphans; zombie (State Z) pids count as exited.
	// Directory-scoped honesty: at most the directory's most recently
	// updated session claims the pane (or the orphan); the rest pass
	// through DB-only.
	Enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error)
	// LiveCounts reports the last observed ground truth: live (non-exited)
	// zellij panes and alive opencode processes. Last-known on infra
	// faults (the tick's Stale flag marks the uncertainty); zero before
	// the first successful Enrich.
	LiveCounts() (panes int, procs int)
}

// SnapshotSink receives fully joined snapshots (W1-3 hub owns the
// implementation: SSE fan-out at 250–1000ms aggregate ticks, keepalive
// 15–30s, Last-Event-ID resume). Caller: internal/collector after each poll
// tick; must be non-blocking per tick or drop-and-mark-stale, never stall
// the poll loop.
type SnapshotSink interface {
	// Publish delivers one Snapshot tick. Seq must increase monotonically;
	// reconnection re-sync replays the latest full snapshot.
	Publish(ctx context.Context, snap model.Snapshot) error
}

// Config is the process environment contract shared by all Wave-1 builders.
// Loaded once at startup via LoadConfigFromEnv; passed down, never re-read.
type Config struct {
	// PollInterval is the collector tick period. Default 10s (plan allows
	// 5–15s). Env: POLL_INTERVAL (Go duration string, e.g. "10s").
	PollInterval time.Duration
	// DBTimeout bounds each DBReader call per collector tick (PollState,
	// Sessions). Default 10s: the batched fleet pass costs ~2s steady-state
	// (~4s cold on the 8.2GB live WAL db, measured 2026-10-25), and a tick
	// slower than 2x PollInterval is marked stale anyway, so the budget
	// stays well under the staleness line while leaving headroom past the
	// old 5s constant that starved every tick. Env: DB_TIMEOUT (Go duration
	// string, e.g. "15s"); malformed or non-positive falls back to default.
	DBTimeout time.Duration
	// ActiveWindow is the recency gate for session status: only sessions
	// with time_updated inside this window may report running, waiting-input
	// or error; anything older reports idle (archived still wins). Default
	// 2m. Env: ACTIVE_WINDOW (Go duration string, e.g. "2m"); malformed or
	// non-positive falls back to default. opencode bumps time_updated on
	// every message/part event, so a quiet window means a dead session.
	ActiveWindow time.Duration
	// BindAddr is the listen address. Default 127.0.0.1 (loopback-only).
	// Env: BIND_ADDR. Any non-loopback value (e.g. 0.0.0.0, ::, a LAN IP)
	// is an explicit operator opt-in and the binary refuses to serve it
	// unless HTTP Basic auth is enabled (DASHBOARD_AUTH=user:pass) —
	// enforced at startup next to the loopback assert in
	// cmd/dashboard/selfcheck.go. Tailnet-only assumption: never bind
	// non-loopback on a publicly reachable host — full transcripts are
	// readable by anyone who can reach the port.
	BindAddr string
	// Port is the listen port. Env: PORT (default "8080").
	Port string
	// DataDir is the read-only opencode data root
	// (~/.local/share/opencode). Env: DATA_DIR.
	DataDir string
}

// DefaultActiveWindow is the default status recency gate: a session quiet
// for a full 2m cannot be generating (see Config.ActiveWindow).
const DefaultActiveWindow = 2 * time.Minute

// DefaultConfig returns the plan defaults: 10s poll, 127.0.0.1 bind,
// PORT/8080, and $HOME/.local/share/opencode as the data root.
func DefaultConfig() Config {
	return Config{
		PollInterval: 10 * time.Second,
		DBTimeout:    10 * time.Second,
		ActiveWindow: DefaultActiveWindow,
		BindAddr:     "127.0.0.1",
		Port:         "8080",
		DataDir:      defaultDataDir(),
	}
}

// LoadConfigFromEnv returns DefaultConfig overlaid with PORT, DATA_DIR,
// POLL_INTERVAL, DB_TIMEOUT, ACTIVE_WINDOW, and BIND_ADDR from the
// environment. Malformed POLL_INTERVAL, DB_TIMEOUT, or ACTIVE_WINDOW falls
// back to its default; empty PORT/DATA_DIR/BIND_ADDR keep their defaults
// (unset BIND_ADDR behaves exactly as before: loopback-only).
func LoadConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("BIND_ADDR"); v != "" {
		cfg.BindAddr = v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.PollInterval = d
		}
	}
	if v := os.Getenv("DB_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.DBTimeout = d
		}
	}
	if v := os.Getenv("ACTIVE_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.ActiveWindow = d
		}
	}
	return cfg
}

// Addr returns the bind address:port pair the hub must listen on.
func (c Config) Addr() string {
	return c.BindAddr + ":" + c.Port
}

// defaultDataDir resolves $HOME/.local/share/opencode. Split out so tests
// can cover the fallback without touching process env beyond HOME.
func defaultDataDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ".local/share/opencode"
	}
	return home + "/.local/share/opencode"
}
