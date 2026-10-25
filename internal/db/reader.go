// Package db implements ports.DBReader: read-only opencode.db WAL snapshot
// queries (W1-1).
//
// QUERY ALLOWLIST (binding per the work plan, Data strategy rule 5).
// Every query below is indexed and sliced (per-session key + LIMIT). Full
// part.data LIKE scans are BANNED: a single unindexed scan was observed to
// take >60s against the 8.2GB live WAL database and to disturb live TUIs.
// Do NOT add a query that scans message/part/event without a per-session
// equality predicate and a LIMIT — the sole exception is Q4c, whose rowid
// high-water bound (plus small overlap) keeps every tick O(delta); and do
// NOT add LIKE/JSON scans over data.
//
// Allowed queries:
//
//	Q1 sessions: session (+ project LEFT JOIN) scalar columns only, no data
//	   blobs, ORDER BY time_updated DESC. Full-table but tiny rows; the
//	   session table holds thousands of rows, not gigabytes.
//	Q2 message signals: WHERE session_id=? ORDER BY time_created,id LIMIT 20,
//	   data slices decoded in Go (uses message_session_time_created_id_idx).
//	   On-demand detail path only (one session + its children).
//	Q2b batched message signals: index-only id scan over message
//	   (ORDER BY session_id,time_created,id — covered, no blobs) capped at
//	   20 ids per session in Go, then PK blob fetches in 500-id chunks
//	   decoded immediately (never retained). Same per-session order and
//	   decoders as Q2; replaces ~1970 queries with ~45.
//	Q3 running tools: WHERE session_id=? ORDER BY time_updated,id LIMIT 50,
//	   data slices decoded in Go (uses part_session_idx). SQL-side JSON
//	   extraction is banned here too: it evaluates over the session's whole
//	   part set before LIMIT (one live session holds 32k parts).
//	   On-demand detail path only.
//	Q3b batched running tools: index-only id scan over part capped at 50
//	   per session in Go, then chunked PK blob fetches counted on arrival
//	   (running count only, blobs never retained). Same order/decoders as
//	   Q3; replaces ~1970 queries with ~120.
//	Q4 fresh events: WHERE aggregate_id=? ORDER BY seq DESC LIMIT 10
//	   (uses event_aggregate_seq_idx). On-demand detail path only.
//	Q4b batched fresh events (full scan; detail path + first fleet tick):
//	   one ordered scan (aggregate_id,seq,rowid — no blobs) with
//	   per-aggregate 10-row caps and first-wins dedupe in Go; same semantics
//	   as Q4, one query instead of ~1970.
//	Q4c incremental fresh events (fleet tick only): after one Q4b-shaped seed
//	   scan, each tick fetches only rows newer than the last tick's
//	   high-water mark (WHERE rowid > ?, a rowid PK range scan, plus a small
//	   overlap so a write racing the seed snapshot cannot slip a row) and
//	   merges them into a per-aggregate top-10 cache. The merge is exact:
//	   truncation composes (top-10(A∪B) == top-10(top-10(A) ∪ B)) and new rows
//	   carry higher rowids, so first-wins dedupe by (aggregate_id,seq) is
//	   preserved. A regressed MAX(rowid) (db swapped under a running
//	   process) reseeds from scratch.
//	Q5/Q6 poll watermarks: MAX(session.time_updated), MAX(event.rowid).
//	Q7 session one/children: same scalar columns as Q1 with WHERE s.id=?
//	   (PK) / WHERE s.parent_id=? (session_parent_idx), children capped by
//	   ORDER BY time_updated DESC without LIMIT (child sets are tiny).
//	Q8 detail messages: WHERE session_id=? ORDER BY time_created,id ASC
//	   LIMIT 10000 (covers the 6967-message max observed live).
//	Q9 detail parts: WHERE session_id=? ORDER BY time_created,id ASC
//	   LIMIT 40000 (covers the 32666-part max observed live).
//	Q10 detail todos: WHERE session_id=? ORDER BY position LIMIT 500
//	   (uses todo_session_idx; max observed live is 16).
//	Q11 detail child counts: WHERE parent_id IN (<family ids>) GROUP BY
//	   parent_id (session_parent_idx; one row per direct child at most).
//
// The database is opened read-only (mode=ro URI + PRAGMA query_only=ON) so
// the reader never disturbs live TUI writers. SQLITE_BUSY from the live
// writer is absorbed by a short bounded retry, then surfaced as ErrBusy.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"

	_ "modernc.org/sqlite"
	sqlite "modernc.org/sqlite"
)

// sqliteBusy is the SQLite result code for SQLITE_BUSY.
const sqliteBusy = 5

// Sentinel errors for DBReader failures. Callers branch with errors.Is.
var (
	// ErrBusy means the live opencode writer held the WAL lock past the
	// bounded retry budget. The collector must treat this as "skip tick",
	// never as fatal, and never extend the retry (that would stall the
	// poll loop).
	ErrBusy = errors.New("db: sqlite busy")
	// ErrOpen means the database file could not be opened read-only.
	ErrOpen = errors.New("db: open failed")
	// ErrNotFound means no session row matched the requested id. Detail
	// callers branch with errors.Is and map it to 404; it is never a
	// stale-frame or retry signal.
	ErrNotFound = errors.New("db: session not found")
)

// Reader implements ports.DBReader over a read-only opencode.db handle.
// Zero value is not usable; construct with New.
type Reader struct {
	db  *sql.DB
	now func() time.Time

	// Event-tail cache for the fleet tick (see Q4c in the allowlist and
	// batchEventSignalsIncremental): per-aggregate newest-first tails plus
	// the highest event rowid incorporated. Guarded by mu; Sessions drives
	// it sequentially, detail paths never touch it.
	mu             sync.Mutex
	eventCache     map[string]*eventTail
	eventWatermark int64
	eventSeeded    bool
}

// Compile-time seam check: Reader must satisfy the frozen contract exactly.
var _ ports.DBReader = (*Reader)(nil)

// New opens <dataDir>/opencode.db read-only (WAL-safe: mode=ro keeps the
// -wal/-shm snapshot visible while forbidding writes) and returns a Reader.
// It never writes, never copies the database, and never reads anything
// outside <dataDir>/opencode.db (+ its -wal/-shm sidecars).
func New(dataDir string) (*Reader, error) {
	path := filepath.Join(dataDir, "opencode.db")
	// mode=ro: read-only VFS open. NOT immutable=1: immutable would ignore
	// the WAL and serve stale pages; ro still applies the live WAL.
	dsn := "file:" + path + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w: %w", path, err, ErrOpen)
	}
	// Belt-and-braces: the SQLite-level read-only flag even if the URI
	// parameter were ever dropped by a driver upgrade.
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pragma query_only: %w: %w", err, ErrOpen)
	}
	// Let SQLite itself wait briefly on the live writer's WAL lock before
	// surfacing SQLITE_BUSY to the Go-level retry below.
	if _, err := db.Exec("PRAGMA busy_timeout=2000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pragma busy_timeout: %w: %w", err, ErrOpen)
	}
	// WAL readers never block each other; a small pool keeps a stuck poll
	// tick from head-of-line blocking PollState behind Sessions.
	db.SetMaxOpenConns(4)
	// Fail fast at open: verify the file is a readable database now rather
	// than on the first poll tick.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		if isBusy(err) {
			return nil, fmt.Errorf("ping %s: %w: %w", path, err, ErrBusy)
		}
		return nil, fmt.Errorf("ping %s: %w: %w", path, err, ErrOpen)
	}
	return &Reader{db: db, now: time.Now, eventCache: make(map[string]*eventTail)}, nil
}

// Close releases the read-only handle.
func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// isBusy reports whether err is (or wraps) an SQLITE_BUSY failure.
func isBusy(err error) bool {
	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		return sqlErr.Code() == sqliteBusy
	}
	// Fallback for wrapped driver strings across driver versions.
	return strings.Contains(strings.ToLower(err.Error()), "sqlite_busy") ||
		strings.Contains(strings.ToLower(err.Error()), "database is locked")
}

// withBusyRetry runs fn, retrying SQLITE_BUSY with a short bounded backoff.
// Budget: 3 attempts (~25ms, ~50ms, ~100ms sleeps). It never hangs the poll
// loop: on exhaustion it returns ErrBusy so the collector skips the tick.
func withBusyRetry(ctx context.Context, op string, fn func() error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = fn(); err == nil {
			return nil
		}
		if !isBusy(err) {
			return fmt.Errorf("%s: %w", op, err)
		}
		backoff := time.Duration(25<<attempt) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("%s: %w: %w", op, err, ErrBusy)
}

// Sessions returns one SessionRow per session with content-side fields
// filled (id, title, project, directory, model, tokens incl. subagent
// roll-up via parent_id, cost) and status derived via model.DeriveStatus.
// Pane/orphan fields are left zero for RuntimeSource (W1-2) to fill.
//
// Signals come from one batched pass: messages and parts are scanned in
// full every tick (Q2b+Q3b), while the event tail is incremental after the
// first tick (Q4c delta + merge — same flags as the Q4b full scan, O(delta)
// instead of O(table)). Same capped slices, same orderings, same pure
// decoders as the singular path — DeriveStatus inputs are unchanged, only
// the query shape changed (N+1 → batched + incremental events).
func (r *Reader) Sessions(ctx context.Context) ([]model.SessionRow, error) {
	base, err := r.sessionScalars(ctx)
	if err != nil {
		return nil, err
	}
	batch, err := r.fleetBatchSignals(ctx, base)
	if err != nil {
		return nil, err
	}
	rows := make([]model.SessionRow, 0, len(base))
	nowMs := r.now().UnixMilli()
	parents := make(map[string]string, len(base))
	for i := range base {
		var signals model.StatusSignals
		modelFallback := ""
		for j, msg := range batch.msgs[base[i].id] {
			applyMessagePayload(msg, j == 0, &signals, &modelFallback)
		}
		signals.RunningToolParts = batch.tools[base[i].id]
		signals.HasFreshEvents = batch.fresh[base[i].id]
		if base[i].parentID.Valid && base[i].parentID.V != "" {
			parents[base[i].id] = base[i].parentID.V
		}
		rows = append(rows, base[i].toRow(signals, modelFallback, nowMs))
	}
	rollUpSubagents(rows, parents)
	return rows, nil
}

// PollState returns the cheapest freshness watermark: MAX(session
// time_updated) and MAX(event rowid). The collector skips a full snapshot
// when neither moved.
func (r *Reader) PollState(ctx context.Context) (lastUpdateMs int64, lastEventRowID int64, err error) {
	if ctx.Err() != nil {
		return 0, 0, ctx.Err()
	}
	err = withBusyRetry(ctx, "poll state", func() error {
		if err := r.db.QueryRowContext(ctx, queryMaxSessionUpdated).Scan(&lastUpdateMs); err != nil {
			return err
		}
		return r.db.QueryRowContext(ctx, queryMaxEventRowID).Scan(&lastEventRowID)
	})
	if err != nil {
		return 0, 0, err
	}
	return lastUpdateMs, lastEventRowID, nil
}
