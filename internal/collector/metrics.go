package collector

import (
	"context"
	"log/slog"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

const (
	defaultMetricsInterval = 60 * time.Second
	metricsQueryTimeout    = 5 * time.Second
)

// metricsSample is the last good message-level sample. ok=false means no
// refresh has completed, so metrics_age_ms reports -1 (unknown) rather
// than pretending the zeros are fresh.
type metricsSample struct {
	tokensLast60s     int64
	costSinceMidnight float64
	windows           ports.WindowMetrics
	at                time.Time
	seq               uint64
	ok                bool
}

// windowBudget bounds each window-metrics call. The 30d conditional-sum
// scan is one indexed pass (measured in the hundreds of ms on a 9GB
// store); the budget keeps a pathological WAL stall from wedging the
// 60s refresher, and any overrun keeps last-good values.
const windowBudget = 15 * time.Second

// metricsLoop refreshes independently of the poll tick: its query scans
// the live message table (~3.5s) and must never sit in the tick path. A
// failure keeps the previous sample; it never marks the fleet stale.
func (c *Collector) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(c.metricsInterval)
	defer ticker.Stop()
	for {
		c.refreshMetrics(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Collector) refreshMetrics(ctx context.Context) {
	now := c.clock.Now()
	queryCtx, cancel := context.WithTimeout(ctx, metricsQueryTimeout)
	defer cancel()
	m, err := c.db.MessageMetrics(queryCtx, now)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if c.metricsWarned.CompareAndSwap(false, true) {
			slog.WarnContext(ctx, "collector message metrics refresh failed; keeping last good values", slog.Any("err", err))
		}
		return
	}
	// Pi contributes its own message-level sample when the source is
	// wired: the two windows share semantics, so the fleet sample is the
	// plain sum. Either side failing keeps last-good (the pi half degrades
	// warn-once per process via the shared flag, like the pi tick merge).
	if c.pi != nil {
		pm, perr := c.pi.PiMessageMetrics(queryCtx, now)
		if perr != nil {
			if ctx.Err() != nil {
				return
			}
			if c.piWarned.CompareAndSwap(false, true) {
				slog.WarnContext(ctx, "collector pi metrics refresh failed; keeping last good values", slog.Any("err", perr))
			}
			return
		}
		m.TokensLast60s += pm.TokensLast60s
		m.CostSinceMidnight += pm.CostSinceMidnight
	}

	// Windowed spend (24h/7d/30d + midnight): one indexed pass per source,
	// summed across harnesses, per-session maps merged (summed on the
	// impossible id collision). Either side failing keeps last-good — the
	// same contract as the 60s sample above.
	wm, ok := c.refreshWindows(ctx, now)
	if !ok {
		return
	}
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	c.metrics = metricsSample{
		tokensLast60s:     m.TokensLast60s,
		costSinceMidnight: m.CostSinceMidnight,
		windows:           wm,
		at:                now,
		seq:               c.metrics.seq + 1,
		ok:                true,
	}
}

// refreshWindows fetches and combines the windowed spend sample. It
// returns false (keeping last-good) when any source fails or the context
// ends. Warn-once per process per side: a missing pi store must not log
// every 60 seconds.
func (c *Collector) refreshWindows(ctx context.Context, now time.Time) (ports.WindowMetrics, bool) {
	wctx, cancel := context.WithTimeout(ctx, windowBudget)
	defer cancel()
	w, err := c.db.WindowMetrics(wctx, now)
	if err != nil {
		if ctx.Err() != nil {
			return ports.WindowMetrics{}, false
		}
		if c.metricsWarned.CompareAndSwap(false, true) {
			slog.WarnContext(ctx, "collector window metrics refresh failed; keeping last good values", slog.Any("err", err))
		}
		return ports.WindowMetrics{}, false
	}
	if c.pi != nil {
		pw, err := c.pi.PiWindowMetrics(wctx, now)
		if err != nil {
			if ctx.Err() != nil {
				return ports.WindowMetrics{}, false
			}
			if c.piWarned.CompareAndSwap(false, true) {
				slog.WarnContext(ctx, "collector pi window metrics refresh failed; keeping last good values", slog.Any("err", err))
			}
			return ports.WindowMetrics{}, false
		}
		w.Tokens24h += pw.Tokens24h
		w.Tokens7d += pw.Tokens7d
		w.Tokens30d += pw.Tokens30d
		w.Cost24h += pw.Cost24h
		w.Cost7d += pw.Cost7d
		w.Cost30d += pw.Cost30d
		w.CostMidnight += pw.CostMidnight
		if w.BySession == nil {
			w.BySession = make(map[string]model.WindowSums)
		}
		for id, s := range pw.BySession {
			prev := w.BySession[id]
			prev.Tokens24h += s.Tokens24h
			prev.Tokens7d += s.Tokens7d
			prev.Tokens30d += s.Tokens30d
			prev.Cost24h += s.Cost24h
			prev.Cost7d += s.Cost7d
			prev.Cost30d += s.Cost30d
			w.BySession[id] = prev
		}
	}
	return w, true
}

// metricsValue returns the current sample and its age in milliseconds.
// -1 means no successful refresh has happened yet.
func (c *Collector) metricsValue(now time.Time) (metricsSample, int64) {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	if !c.metrics.ok {
		return metricsSample{}, -1
	}
	age := now.Sub(c.metrics.at).Milliseconds()
	if age < 0 {
		age = 0
	}
	return c.metrics, age
}

func (c *Collector) metricsSequence() uint64 {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	return c.metrics.seq
}

type kpiMetrics struct {
	tokensLast60s     int64
	costSinceMidnight float64
	windows           ports.WindowMetrics
	ageMs             int64
}
