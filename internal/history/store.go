// Package history owns the dashboard's own memory: trailing-window
// spend/presence samples persisted to a local SQLite file (Wave 8, v2.0).
//
// Why a separate store: every v1 number is live-only — a refresh wipes
// history. Operators ask "what did we spend this week?", which needs
// calendar history the live WAL/pi files cannot answer cheaply per view.
// The store records one fleet row plus one row per active directory per
// metrics refresh (60s cadence, ~1.5k rows/day — trivial for SQLite) and
// serves windowed ranges to the History view.
//
// Safety posture (matches the read-only dashboard, extended minimally):
//   - The file lives OUTSIDE the agent data dirs (see compose
//     DATA_HISTORY_DIR mount, default ~/.local/share/opencode-dashboard):
//     only dashboard-computed aggregates are ever written — never agent
//     content, never credentials, never transcripts.
//   - A missing/unwritable store degrades loudly at startup (fail-closed
//     open with last-known-empty? No — the collector treats it as
//     "history disabled", warn-once, and /api/history answers 503).
//     History must never break the fleet tick.
//   - Retention is bounded in-store (default 120 days, single DELETE per
//     record batch on an indexed column).
//   - Window sums reuse ports.WindowMetrics semantics verbatim (trailing
//     24h/7d/30d + midnight), so history numbers reconcile with the live
//     KPIs by construction.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"opencode-dashboard/internal/model"

	_ "modernc.org/sqlite"
)

// DefaultRetentionDays bounds history growth: one DELETE per record batch
// on an indexed column keeps the file small essentially forever.
const DefaultRetentionDays = 120

// ErrDisabled means the history store is unavailable (missing/unwritable
// path). Callers treat it as "history disabled": never fatal, never
// stale-marking — identical contract to a missing pi store.
var ErrDisabled = errors.New("history: store unavailable")

// Store is the dashboard-owned SQLite history. Zero value is not usable;
// construct with Open. All methods are safe for the collector's sequential
// tick use; callers must not share a Store across goroutines.
type Store struct {
	db           *sql.DB
	retentionCut time.Duration
}

// Open creates (never requires pre-existing) the history database at
// dir/history.db and applies the schema. The directory itself is NOT
// created: a missing dir almost always means a missing :rw mount, and
// silently writing into an ephemeral overlay would fake durability.
// Callers surface ErrDisabled so the dashboard degrades loudly-but-safely.
func Open(dir string) (*Store, error) {
	return OpenWithRetention(dir, DefaultRetentionDays)
}

// OpenWithRetention is Open with an explicit retention window in days
// (non-positive falls back to the default).
func OpenWithRetention(dir string, retentionDays int) (*Store, error) {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	path := filepath.Join(dir, "history.db")
	// mode=rwc: create the FILE if missing, never the directory (see Open).
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rwc")
	if err != nil {
		return nil, fmt.Errorf("history: open %s: %w: %w", path, err, ErrDisabled)
	}
	db.SetMaxOpenConns(1)
	s := &Store{
		db:           db,
		retentionCut: time.Duration(retentionDays) * 24 * time.Hour,
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("history: ping %s: %w: %w", path, err, ErrDisabled)
	}
	return s, nil
}

// Close releases the history handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// schemaVersion is applied idempotently on every open so a fresh file and
// an existing file converge to the same schema without migrations.
const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS fleet_samples (
  ts          INTEGER PRIMARY KEY,  -- unix seconds, one row per metrics refresh
  tokens_24h  INTEGER NOT NULL DEFAULT 0,
  cost_24h    REAL    NOT NULL DEFAULT 0,
  tokens_7d   INTEGER NOT NULL DEFAULT 0,
  cost_7d     REAL    NOT NULL DEFAULT 0,
  tokens_30d  INTEGER NOT NULL DEFAULT 0,
  cost_30d    REAL    NOT NULL DEFAULT 0,
  active      INTEGER NOT NULL DEFAULT 0,
  waiting     INTEGER NOT NULL DEFAULT 0,
  errors      INTEGER NOT NULL DEFAULT 0,
  live_procs  INTEGER NOT NULL DEFAULT 0,
  live_panes  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS project_samples (
  ts         INTEGER NOT NULL,       -- unix seconds (matches a fleet row)
  directory  TEXT    NOT NULL,
  sessions   INTEGER NOT NULL DEFAULT 0,
  running    INTEGER NOT NULL DEFAULT 0,
  tokens_24h INTEGER NOT NULL DEFAULT 0,
  cost_24h   REAL    NOT NULL DEFAULT 0,
  tokens_7d  INTEGER NOT NULL DEFAULT 0,
  cost_7d    REAL    NOT NULL DEFAULT 0,
  tokens_30d INTEGER NOT NULL DEFAULT 0,
  cost_30d   REAL    NOT NULL DEFAULT 0,
  PRIMARY KEY (ts, directory)
);
CREATE INDEX IF NOT EXISTS idx_project_ts ON project_samples(ts);
CREATE TABLE IF NOT EXISTS schema_meta (
  k TEXT PRIMARY KEY,
  v INTEGER NOT NULL
);`

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("history: migrate: %w: %w", err, ErrDisabled)
	}
	if _, err := s.db.Exec(`INSERT INTO schema_meta(k, v) VALUES ('version', ?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, schemaVersion); err != nil {
		return fmt.Errorf("history: stamp version: %w: %w", err, ErrDisabled)
	}
	return nil
}

// Point is one decimated history sample for charts: the exact recorded
// values (no averaging — decimation picks real rows so peaks survive).
type Point struct {
	Ts        int64   `json:"ts"`
	Tokens24h int64   `json:"tokens_24h"`
	Cost24h   float64 `json:"cost_24h"`
	Tokens7d  int64   `json:"tokens_7d,omitempty"`
	Cost7d    float64 `json:"cost_7d,omitempty"`
	Tokens30d int64   `json:"tokens_30d,omitempty"`
	Cost30d   float64 `json:"cost_30d,omitempty"`
	Active    int     `json:"active,omitempty"`
	Waiting   int     `json:"waiting,omitempty"`
	Errors    int     `json:"errors,omitempty"`
	LiveProcs int     `json:"live_procs,omitempty"`
	LivePanes int     `json:"live_panes,omitempty"`
	Sessions  int     `json:"sessions,omitempty"`
	Running   int     `json:"running,omitempty"`
	Directory string  `json:"directory,omitempty"`
}

// Record persists one fleet row plus one row per active directory per
// metrics refresh, then enforces retention. Sessions without window activity
// contribute presence (sessions/running counts) with zero spend — a quiet
// directory still shows up. Callers pass the published KPIs, the fleet rows,
// and the sparse per-session window map; nil maps are treated as empty.
func (s *Store) Record(ctx context.Context, now time.Time, kpis model.KPIs, rows []model.SessionRow, windows map[string]model.WindowSums) error {
	if s == nil || s.db == nil {
		return ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ts := now.Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("history: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `INSERT INTO fleet_samples(ts, tokens_24h, cost_24h, tokens_7d, cost_7d, tokens_30d, cost_30d, active, waiting, errors, live_procs, live_panes) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		ts, kpis.Tokens24h, kpis.Cost24h, kpis.Tokens7d, kpis.Cost7d, kpis.Tokens30d, kpis.Cost30d,
		kpis.Active, kpis.WaitingInput, kpis.Errors, kpis.LiveProcesses, kpis.LivePanes); err != nil {
		return fmt.Errorf("history: record fleet: %w", err)
	}
	projAgg := make(map[string]*struct {
		sessions int
		running  int
		t24      int64
		c24      float64
		t7       int64
		c7       float64
		t30      int64
		c30      float64
	})
	order := make([]string, 0)
	for i := range rows {
		dir := rows[i].Directory
		if dir == "" {
			continue
		}
		agg, ok := projAgg[dir]
		if !ok {
			agg = &struct {
				sessions int
				running  int
				t24      int64
				c24      float64
				t7       int64
				c7       float64
				t30      int64
				c30      float64
			}{}
			projAgg[dir] = agg
			order = append(order, dir)
		}
		agg.sessions++
		if rows[i].Status == model.SessionRunning {
			agg.running++
		}
		if w, ok := windows[rows[i].ID]; ok {
			agg.t24 += w.Tokens24h
			agg.c24 += w.Cost24h
			agg.t7 += w.Tokens7d
			agg.c7 += w.Cost7d
			agg.t30 += w.Tokens30d
			agg.c30 += w.Cost30d
		}
	}
	for _, dir := range order {
		agg := projAgg[dir]
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_samples(ts, directory, sessions, running, tokens_24h, cost_24h, tokens_7d, cost_7d, tokens_30d, cost_30d) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			ts, dir, agg.sessions, agg.running, agg.t24, agg.c24, agg.t7, agg.c7, agg.t30, agg.c30); err != nil {
			return fmt.Errorf("history: record project %s: %w", dir, err)
		}
	}
	cutoff := now.Add(-s.retentionCut).Unix()
	if _, err := tx.ExecContext(ctx, `DELETE FROM fleet_samples WHERE ts < ?`, cutoff); err != nil {
		return fmt.Errorf("history: prune fleet: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_samples WHERE ts < ?`, cutoff); err != nil {
		return fmt.Errorf("history: prune projects: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("history: commit: %w", err)
	}
	return nil
}
