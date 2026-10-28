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
	// MessageMetrics computes throughput and spend from individual message
	// rows as of now (the caller supplies the clock). Session totals roll
	// up subagent children, so diffing them counts a finishing child's
	// entire history as one tick's work; per-message timestamps are the
	// only honest throughput source.
	MessageMetrics(ctx context.Context, now time.Time) (MessageMetrics, error)
	// WindowMetrics computes trailing-window token/cost sums (24h/7d/30d
	// plus local-midnight cost) globally and per session, as of now.
	// Off-tick only (same budget rules as MessageMetrics); never a tick
	// query. A missing store reports an error like PiSessions.
	WindowMetrics(ctx context.Context, now time.Time) (WindowMetrics, error)
}

// PiReader is the pi JSONL session-store source (Wave 5 owns the
// implementation in internal/pi). It mirrors the content side of DBReader
// for the second harness: one SessionRow per session file with
// Harness=pi, title/tokens/cost/model filled and status derived via
// model.DeriveStatus under the same recency gate. Pane/orphan fields stay
// zero — RuntimeSource fills those for both harnesses.
//
// A nil PiReader disables pi monitoring: the collector skips the merge
// and an opencode-only host behaves exactly as before (no new failure
// modes, no config required).
type PiReader interface {
	// PiSessions returns one SessionRow per pi session file (Harness=pi).
	// A missing/unreadable store root reports an error the collector
	// treats as "pi disabled" (never fatal, never stale-marking); a
	// corrupt single file is skipped in isolation, never failing the tick.
	PiSessions(ctx context.Context) ([]model.SessionRow, error)
	// PiMessageMetrics returns the message-level throughput/spend sample
	// over the JSONL store: usage tokens created in the trailing 60
	// seconds and usage cost since local midnight (the same window
	// semantics as DBReader.MessageMetrics, so the collector can sum both
	// harnesses into one honest sample). Same error contract as
	// PiSessions.
	PiMessageMetrics(ctx context.Context, now time.Time) (MessageMetrics, error)
	// PiWindowMetrics returns trailing-window usage sums (24h/7d/30d plus
	// local-midnight cost) globally and per session file, as of now — the
	// same shape as DBReader.WindowMetrics. Same error contract as
	// PiSessions.
	PiWindowMetrics(ctx context.Context, now time.Time) (WindowMetrics, error)
}

// WindowMetrics is one off-tick windowed sample over both harnesses:
// global trailing-24h/7d/30d token/cost sums plus the sparse per-session
// map (session id → window sums, only sessions with window activity) that
// lets the UI attribute windowed spend to projects under date filters.
// Same error contract as the sibling sample methods: any source failure
// keeps the collector's last-good sample.
type WindowMetrics struct {
	// Global trailing-window token sums.
	Tokens24h int64
	Tokens7d  int64
	Tokens30d int64
	// Global trailing-window cost sums (plus midnight for today).
	Cost24h      float64
	Cost7d       float64
	Cost30d      float64
	CostMidnight float64
	// BySession maps session id → window sums (sparse: only sessions
	// with window activity). The collector sums harnesses into it.
	BySession map[string]model.WindowSums
}

// MessageMetrics is one message-level sample: tokens created in the last
// 60 seconds and USD cost since local midnight.
type MessageMetrics struct {
	// TokensLast60s is the sum of message token totals created in the
	// trailing 60 seconds.
	TokensLast60s int64
	// CostSinceMidnight is the sum of message cost created since local
	// midnight in now's location.
	CostSinceMidnight float64
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
	// ProcsByDir reports the last observed count of ALIVE opencode
	// processes per cleaned working directory, from the same /proc scan
	// Enrich ran. Empty CWDs cannot be attributed and are omitted; a scan
	// fault keeps the last-known map. Never guessed from sessions.
	ProcsByDir() map[string]int
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
	// PiDataDir is the read-only pi session-store root
	// (~/.pi/agent/sessions). Env: PI_DATA_DIR. Empty after load means
	// "pi monitoring disabled" (opencode-only host): the collector
	// skips the pi merge instead of failing. PI_CODING_AGENT_SESSION_DIR
	// is deliberately NOT honored — the dashboard reads one fixed root
	// (logged at startup) rather than chasing per-process overrides.
	PiDataDir string
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
		PiDataDir:    defaultPiDataDir(),
	}
}

// LoadConfigFromEnv returns DefaultConfig overlaid with PORT, DATA_DIR,
// PI_DATA_DIR, POLL_INTERVAL, DB_TIMEOUT, ACTIVE_WINDOW, and BIND_ADDR from
// the environment. Malformed POLL_INTERVAL, DB_TIMEOUT, or ACTIVE_WINDOW falls
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
	if v := os.Getenv("PI_DATA_DIR"); v != "" {
		cfg.PiDataDir = v
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

// defaultPiDataDir resolves $HOME/.pi/agent/sessions (the pi default
// session store; PI_CODING_AGENT_SESSION_DIR overrides are not honored —
// see Config.PiDataDir). Split out so tests can cover the fallback
// without touching process env beyond HOME.
func defaultPiDataDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		return ".pi/agent/sessions"
	}
	return home + "/.pi/agent/sessions"
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
