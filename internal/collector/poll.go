// Package collector wires ports.DBReader + ports.RuntimeSource into
// model.Snapshot ticks for ports.SnapshotSink (W1-4).
//
// Design decisions (frozen by the W1-4 contract):
//   - Watermark skip: PollState (MAX(time_updated), MAX(event.rowid)) is the
//     cheap freshness check. When both values equal the last PUBLISHED tick
//     AND no message-metrics refresh has landed since the last publish, the
//     full join is skipped: no Sessions/Enrich/Publish calls and seq is
//     unchanged. Seq advances by exactly 1 per published Snapshot, so a skip
//     emits no gap. The metrics sequence forces a publish after each refresh
//     so tokens_per_hour can decay to 0 while nothing is running. The
//     watermark is stored only on successful publish, so a failed publish can
//     never wedge the loop into perpetual skips.
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
	"sync"
	"sync/atomic"
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
// Tick is not safe to call concurrently with itself: drive it from a single
// goroutine via Run (or call Tick sequentially from tests). The only
// concurrent writer is the message-metrics refresher, and its state is
// guarded by metricsMu. The publish path spawns a short-lived waiter
// goroutine per tick that always rejoins before Tick returns, so no
// publish goroutine outlives a tick.
type Collector struct {
	db   ports.DBReader
	rt   ports.RuntimeSource
	sink ports.SnapshotSink
	cfg  ports.Config
	// pi is the optional pi session source (nil = pi monitoring
	// disabled). A nil pi keeps opencode-only hosts exactly on the old
	// path: no extra work, no new failure modes.
	pi ports.PiReader

	clock          Clock
	dbTimeout      time.Duration
	activeWindow   time.Duration
	publishTimeout time.Duration

	seq           uint64
	havePublished bool
	wmUpdateMs    int64
	wmEventRowID  int64
	// lastPiSig is the signature of the pi rows in the last published
	// tick (empty when pi is disabled or contributed nothing). The DB
	// watermark cannot see pi-only activity, so the skip path re-checks
	// the (cheap, stat-cached) pi scan against this signature instead of
	// skipping blindly.
	lastPiSig string
	// piWarned rate-limits the pi-unavailable notice to once per process:
	// an opencode-only host must not log every tick.
	piWarned atomic.Bool

	prevTotals map[string]int64
	sparks     map[string]*tokenRing

	// Message-level metrics are refreshed by their own goroutine (see
	// metrics.go) and read by every tick under metricsMu. The sequence
	// lets a tick publish when a refresh lands even if the DB watermark
	// did not move (otherwise tokens_per_hour could never decay to 0).
	metricsMu               sync.Mutex
	metrics                 metricsSample
	metricsInterval         time.Duration
	metricsWarned           atomic.Bool
	lastPublishedMetricsSeq uint64

	lastSessions []model.SessionRow
	lastProjects []model.ProjectSummary
	haveLastGood bool
	publishStale bool
}

// NewCollector validates the ports seam (interfaces only — never sibling
// implementations) and returns a Collector ready for Run/Tick. pi is the
// optional pi session source: nil disables the pi merge (opencode-only
// hosts), non-nil merges pi rows pre-Enrich every tick.
func NewCollector(db ports.DBReader, rt ports.RuntimeSource, sink ports.SnapshotSink, cfg ports.Config, pi ports.PiReader) (*Collector, error) {
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
	activeWindow := cfg.ActiveWindow
	if activeWindow <= 0 {
		activeWindow = ports.DefaultActiveWindow
	}
	// Logged (not magic): the per-source DB budget and the status recency
	// gate are operator-visible at startup and tunable via DB_TIMEOUT and
	// ACTIVE_WINDOW without a rebuild.
	slog.Info("collector config",
		slog.Duration("db_timeout", dbTimeout),
		slog.Duration("poll_interval", cfg.PollInterval),
		slog.Duration("active_window", activeWindow),
		slog.Duration("metrics_interval", defaultMetricsInterval),
	)
	return &Collector{
		db:              db,
		rt:              rt,
		sink:            sink,
		cfg:             cfg,
		pi:              pi,
		clock:           realClock{},
		dbTimeout:       dbTimeout,
		activeWindow:    activeWindow,
		publishTimeout:  clampPublishTimeout(cfg.PollInterval),
		metricsInterval: defaultMetricsInterval,
		prevTotals:      make(map[string]int64),
		sparks:          make(map[string]*tokenRing),
	}, nil
}

// Run ticks immediately once, then on every cfg.PollInterval, until ctx ends.
// Tick errors are logged and swallowed — a failing source marks frames stale
// but never crashes the loop. Run returns nil on clean cancellation. The
// message-metrics refresher runs in its own goroutine (see metricsLoop) so a
// slow metrics scan can never delay a tick.
func (c *Collector) Run(ctx context.Context) error {
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		c.metricsLoop(ctx)
	}()
	c.tickOnce(ctx)
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			<-metricsDone
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
	} else if c.havePublished && wmUpdateMs == c.wmUpdateMs && wmEventRowID == c.wmEventRowID &&
		c.metricsSequence() == c.lastPublishedMetricsSeq && c.piUnchanged(ctx) {
		return false, nil
	}

	rows, sessErr := c.sessions(ctx)
	if sessErr != nil {
		slog.ErrorContext(ctx, "collector db sessions failed", slog.Any("err", sessErr))
		return c.publishStaleFrame(ctx, start)
	}
	piRows := c.piSessions(ctx)
	rows = append(rows, piRows...)
	// Harness normalization (defense in depth): the wire contract
	// guarantees a populated harness, and both real sources set it —
	// but a future source forgetting it must degrade to opencode, never
	// to an empty value the UI cannot render.
	for i := range rows {
		if !rows[i].Harness.IsValid() {
			rows[i].Harness = model.HarnessOpencode
		}
	}

	enriched, enrichErr := c.enrich(ctx, rows)
	if enrichErr != nil {
		slog.ErrorContext(ctx, "collector runtime enrich failed", slog.Any("err", enrichErr))
		enriched = rows
	}

	// UpdatedRelativeMs is owned by DBReader (only it holds
	// session.time_updated at this seam); the collector preserves it untouched.
	windowMs := c.activeWindow.Milliseconds()
	for i := range enriched {
		recent := enriched[i].UpdatedRelativeMs <= windowMs
		enriched[i].Status = model.DeriveStatus(projectSignals(enriched[i].Status, recent))
		c.observeSpark(enriched[i].ID, enriched[i].Tokens.Total)
		enriched[i].Spark = c.sparks[enriched[i].ID].rates(c.cfg.PollInterval)
	}
	sortSessions(enriched)

	now := c.clock.Now()
	elapsed := now.Sub(start)
	stale := sessErr != nil || enrichErr != nil || wmErr != nil || c.publishStale ||
		elapsed > freshnessFactor*interval
	livePanes, liveProcs := c.rt.LiveCounts()
	metrics, metricsAgeMs := c.metricsValue(now)
	kpis := buildKPIs(enriched, kpiMetrics{
		tokensLast60s:     metrics.tokensLast60s,
		costSinceMidnight: metrics.costSinceMidnight,
		ageMs:             metricsAgeMs,
	}, livePanes, liveProcs)
	projects := buildProjects(enriched, c.rt.ProcsByDir(), panesByDir(c.rt))

	snap := model.Snapshot{
		Seq:        c.seq + 1,
		Sessions:   enriched,
		Projects:   projects,
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
	c.lastPiSig = piSignature(piRows)
	if wmErr == nil {
		c.wmUpdateMs = wmUpdateMs
		c.wmEventRowID = wmEventRowID
	}
	c.lastSessions = snap.Sessions
	c.lastProjects = snap.Projects
	c.lastPublishedMetricsSeq = metrics.seq
	c.haveLastGood = true
	c.publishStale = false
	return true, nil
}

// publishStaleFrame emits the last good frame (or an empty frame when the
// collector has never published) marked stale after a Sessions failure.
func (c *Collector) publishStaleFrame(ctx context.Context, start time.Time) (bool, error) {
	now := c.clock.Now()
	elapsed := now.Sub(start)
	sessions := append([]model.SessionRow(nil), c.lastSessions...)
	projects := append([]model.ProjectSummary(nil), c.lastProjects...)
	livePanes, liveProcs := c.rt.LiveCounts()
	metrics, metricsAgeMs := c.metricsValue(now)
	kpis := buildKPIs(sessions, kpiMetrics{
		tokensLast60s:     metrics.tokensLast60s,
		costSinceMidnight: metrics.costSinceMidnight,
		ageMs:             metricsAgeMs,
	}, livePanes, liveProcs)
	snap := model.Snapshot{
		Seq:        c.seq + 1,
		Sessions:   sessions,
		Projects:   projects,
		KPIs:       kpis,
		LastPollMs: elapsed.Milliseconds(),
		Stale:      true,
	}
	if snap.Sessions == nil {
		snap.Sessions = []model.SessionRow{}
	}
	if snap.Projects == nil {
		snap.Projects = []model.ProjectSummary{}
	}
	if err := c.publish(ctx, snap); err != nil {
		c.publishStale = true
		return false, fmt.Errorf("collector: publish stale seq %d: %w", snap.Seq, err)
	}
	c.seq = snap.Seq
	c.havePublished = true
	c.lastSessions = snap.Sessions
	c.lastProjects = snap.Projects
	c.lastPublishedMetricsSeq = metrics.seq
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
