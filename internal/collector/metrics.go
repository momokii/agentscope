package collector

import (
	"context"
	"log/slog"
	"time"
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
	at                time.Time
	seq               uint64
	ok                bool
}

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
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	c.metrics = metricsSample{
		tokensLast60s:     m.TokensLast60s,
		costSinceMidnight: m.CostSinceMidnight,
		at:                now,
		seq:               c.metrics.seq + 1,
		ok:                true,
	}
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
	ageMs             int64
}
