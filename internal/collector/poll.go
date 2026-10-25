// Package collector wires ports.DBReader + ports.RuntimeSource into
// model.Snapshot ticks for ports.SnapshotSink (W1-4).
//
// Design decisions (frozen by the W1-4 contract):
//   - Watermark skip: PollState (MAX(time_updated), MAX(event.rowid)) is the
//     cheap freshness check. When both values equal the last PUBLISHED tick,
//     the full join is skipped: no Sessions/Enrich/Publish calls and seq is
//     unchanged. Seq advances by exactly 1 per published Snapshot, so a skip
//     emits no gap. The watermark is stored only on successful publish, so a
//     failed publish can never wedge the loop into perpetual skips.
//   - Fail-open watermark: a PollState error does NOT skip the tick; the full
//     join is attempted and the frame (good or stale) still publishes.
//   - Publish failure backoff is the normal tick wait: no in-tick retry loop
//     (busy retry banned). The next published frame carries Stale=true to mark
//     the gap, then recovery is automatic.
//   - Enrich failure degrades, it does not blank: fresh DB rows publish with
//     zero Pane values and Stale=true. Sessions failure publishes the last
//     good frame (or an empty frame when none exists) with Stale=true.
//   - Per-source context timeouts (DB via cfg.DBTimeout, runtime 8s) bound
//     every tick; the publish path has its own timeout so a wedged sink
//     marks the frame stale instead of stalling the loop. dump-screen is
//     never called per tick.
//   - main() is deliberately NOT provided here: Wave 4 owns the runnable
//     binary path (bind addr, signal handling, hub wiring). This package
//     exposes NewCollector/Run/Tick only.
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

var (
	// ErrNilDependency is returned by NewCollector when a required port is nil.
	ErrNilDependency = errors.New("collector: nil dependency")
	// ErrPublishTimeout is returned (wrapped) when the sink does not accept a
	// Snapshot within the publish budget. The tick is marked stale and the
	// loop continues; the next tick retries.
	ErrPublishTimeout = errors.New("collector: publish timeout")
)

const (
	// defaultPollInterval applies when cfg.PollInterval <= 0 (plan default).
	defaultPollInterval = 10 * time.Second
	// defaultDBTimeout applies when cfg.DBTimeout <= 0: the batched fleet
	// pass costs ~2s steady-state (~4s cold on the 8.2GB live WAL db), so
	// 10s leaves headroom while staying under the 2x-interval staleness
	// line. Operators tune it via the DB_TIMEOUT env knob (see ports.Config).
	defaultDBTimeout = 10 * time.Second
	// runtimeTimeout bounds the RuntimeSource call per tick.
	runtimeTimeout = 8 * time.Second
	// freshnessFactor scales the poll interval into the staleness budget: a
	// tick slower than 2x the interval (or with any source error) is stale.
	freshnessFactor = 2
	// minPublishTimeout / maxPublishTimeout clamp the derived publish budget
	// so tiny test intervals stay usable and huge intervals stay bounded.
	minPublishTimeout = 50 * time.Millisecond
	maxPublishTimeout = 5 * time.Second
)

// Collector joins DB rows with runtime presence into Snapshot ticks.
//
// It is intentionally NOT safe for concurrent use: drive it from a single
// goroutine via Run (or call Tick sequentially from tests). The publish path
// spawns a short-lived waiter goroutine per tick that always rejoins before
// Tick returns, so no goroutine outlives a tick.
type Collector struct {
	db   ports.DBReader
	rt   ports.RuntimeSource
	sink ports.SnapshotSink
	cfg  ports.Config

	clock          Clock
	dbTimeout      time.Duration
	publishTimeout time.Duration

	seq           uint64
	havePublished bool
	wmUpdateMs    int64
	wmEventRowID  int64

	prevTotals  map[string]int64
	sparks      map[string]*tokenRing
	freshTicks  int
	windowStart time.Time

	lastSessions []model.SessionRow
	lastKPIs     model.KPIs
	haveLastGood bool
	publishStale bool
}

// NewCollector validates the ports seam (interfaces only — never sibling
// implementations) and returns a Collector ready for Run/Tick.
func NewCollector(db ports.DBReader, rt ports.RuntimeSource, sink ports.SnapshotSink, cfg ports.Config) (*Collector, error) {
	switch {
	case db == nil:
		return nil, fmt.Errorf("collector: db: %w", ErrNilDependency)
	case rt == nil:
		return nil, fmt.Errorf("collector: runtime: %w", ErrNilDependency)
	case sink == nil:
		return nil, fmt.Errorf("collector: sink: %w", ErrNilDependency)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	dbTimeout := cfg.DBTimeout
	if dbTimeout <= 0 {
		dbTimeout = defaultDBTimeout
	}
	// Logged (not magic): the per-source DB budget is operator-visible at
	// startup and tunable via DB_TIMEOUT without a rebuild.
	slog.Info("collector config",
		slog.Duration("db_timeout", dbTimeout),
		slog.Duration("poll_interval", cfg.PollInterval),
	)
	return &Collector{
		db:             db,
		rt:             rt,
		sink:           sink,
		cfg:            cfg,
		clock:          realClock{},
		dbTimeout:      dbTimeout,
		publishTimeout: clampPublishTimeout(cfg.PollInterval),
		prevTotals:     make(map[string]int64),
		sparks:         make(map[string]*tokenRing),
	}, nil
}

// Run ticks immediately once, then on every cfg.PollInterval, until ctx ends.
// Tick errors are logged and swallowed — a failing source marks frames stale
// but never crashes the loop. Run returns nil on clean cancellation.
func (c *Collector) Run(ctx context.Context) error {
	c.tickOnce(ctx)
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.tickOnce(ctx)
		}
	}
}

func (c *Collector) tickOnce(ctx context.Context) {
	if _, err := c.Tick(ctx); err != nil {
		slog.ErrorContext(ctx, "collector tick failed",
			slog.Any("err", err),
			slog.Uint64("seq", c.seq),
		)
	}
}

// Tick performs one poll join. It reports whether a Snapshot was published.
// A false return with nil error means the watermark proved nothing moved and
// the tick was correctly skipped. Any error is already a stale-frame
// situation (frame published stale, or publish itself failed); callers must
// still continue ticking.
func (c *Collector) Tick(ctx context.Context) (bool, error) {
	start := c.clock.Now()
	interval := c.cfg.PollInterval

	wmUpdateMs, wmEventRowID, wmErr := c.pollState(ctx)
	if wmErr != nil {
		slog.ErrorContext(ctx, "collector pollstate failed", slog.Any("err", wmErr))
	} else if c.havePublished && wmUpdateMs == c.wmUpdateMs && wmEventRowID == c.wmEventRowID {
		return false, nil
	}

	rows, sessErr := c.sessions(ctx)
	if sessErr != nil {
		slog.ErrorContext(ctx, "collector db sessions failed", slog.Any("err", sessErr))
		return c.publishStaleFrame(ctx, start)
	}

	enriched, enrichErr := c.enrich(ctx, rows)
	if enrichErr != nil {
		slog.ErrorContext(ctx, "collector runtime enrich failed", slog.Any("err", enrichErr))
		enriched = rows
	}

	// UpdatedRelativeMs is owned by DBReader (only it holds
	// session.time_updated at this seam); the collector preserves it untouched.
	if c.freshTicks == 0 {
		c.windowStart = start
	}
	for i := range enriched {
		enriched[i].Status = model.DeriveStatus(projectSignals(enriched[i].Status))
		c.observeSpark(enriched[i].ID, enriched[i].Tokens.Total)
		enriched[i].Spark = c.sparks[enriched[i].ID].rates(c.cfg.PollInterval)
	}
	sortSessions(enriched)

	elapsed := c.clock.Now().Sub(start)
	stale := sessErr != nil || enrichErr != nil || wmErr != nil || c.publishStale ||
		elapsed > freshnessFactor*interval
	kpis := buildKPIs(enriched, c.tokensPerHour(interval, start))

	snap := model.Snapshot{
		Seq:        c.seq + 1,
		Sessions:   enriched,
		KPIs:       kpis,
		LastPollMs: elapsed.Milliseconds(),
		Stale:      stale,
	}
	if err := c.publish(ctx, snap); err != nil {
		slog.ErrorContext(ctx, "collector publish failed",
			slog.Any("err", err),
			slog.Uint64("seq", snap.Seq),
		)
		c.publishStale = true
		return false, fmt.Errorf("collector: publish seq %d: %w", snap.Seq, err)
	}

	c.seq = snap.Seq
	c.havePublished = true
	if wmErr == nil {
		c.wmUpdateMs = wmUpdateMs
		c.wmEventRowID = wmEventRowID
	}
	c.lastSessions = snap.Sessions
	c.lastKPIs = snap.KPIs
	c.haveLastGood = true
	c.publishStale = false
	c.freshTicks++
	return true, nil
}

// publishStaleFrame emits the last good frame (or an empty frame when the
// collector has never published) marked stale after a Sessions failure.
func (c *Collector) publishStaleFrame(ctx context.Context, start time.Time) (bool, error) {
	elapsed := c.clock.Now().Sub(start)
	sessions := append([]model.SessionRow(nil), c.lastSessions...)
	snap := model.Snapshot{
		Seq:        c.seq + 1,
		Sessions:   sessions,
		KPIs:       c.lastKPIs,
		LastPollMs: elapsed.Milliseconds(),
		Stale:      true,
	}
	if snap.Sessions == nil {
		snap.Sessions = []model.SessionRow{}
	}
	if err := c.publish(ctx, snap); err != nil {
		c.publishStale = true
		return false, fmt.Errorf("collector: publish stale seq %d: %w", snap.Seq, err)
	}
	c.seq = snap.Seq
	c.havePublished = true
	c.lastSessions = snap.Sessions
	c.lastKPIs = snap.KPIs
	c.haveLastGood = true
	return true, nil
}

func (c *Collector) pollState(ctx context.Context) (int64, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, c.dbTimeout)
	defer cancel()
	updMs, evID, err := c.db.PollState(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("collector: pollstate: %w", err)
	}
	return updMs, evID, nil
}

func (c *Collector) sessions(ctx context.Context) ([]model.SessionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, c.dbTimeout)
	defer cancel()
	rows, err := c.db.Sessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("collector: sessions: %w", err)
	}
	return rows, nil
}

func (c *Collector) enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error) {
	ctx, cancel := context.WithTimeout(ctx, runtimeTimeout)
	defer cancel()
	out, err := c.rt.Enrich(ctx, rows)
	if err != nil {
		return nil, fmt.Errorf("collector: enrich: %w", err)
	}
	return out, nil
}

// publish delivers one Snapshot with a bounded wait: a wedged sink trips
// ErrPublishTimeout instead of stalling the loop. Slow-sink proof lives in
// poll_test.go (Test_Collector_SlowSink_never_stalls).
func (c *Collector) publish(ctx context.Context, snap model.Snapshot) error {
	pctx, cancel := context.WithTimeout(ctx, c.publishTimeout)
	defer cancel()
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		done <- result{err: c.sink.Publish(pctx, snap)}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return fmt.Errorf("collector: publish: %w", r.err)
		}
		return nil
	case <-pctx.Done():
		if ctx.Err() != nil {
			return fmt.Errorf("collector: publish cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("collector: publish seq %d: %w", snap.Seq, ErrPublishTimeout)
	}
}
