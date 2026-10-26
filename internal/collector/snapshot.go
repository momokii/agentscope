package collector

import (
	"sort"
	"time"

	"opencode-dashboard/internal/model"
)

// sparkCap bounds the per-session tokens/min ring buffer (60 samples feed
// SessionRow.Spark for the 72x24 fleet sparkline; older samples age out).
const sparkCap = 60

// Clock abstracts time for the tick loop. Production uses realClock;
// tests inject a stub to make lastPollMs and freshness deterministic.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func clampPublishTimeout(interval time.Duration) time.Duration {
	t := interval / 2
	if t < minPublishTimeout {
		return minPublishTimeout
	}
	if t > maxPublishTimeout {
		return maxPublishTimeout
	}
	return t
}

// tokenRing is a FIFO of per-tick token deltas for one session, capped at
// sparkCap samples. Spark renders each delta as a tokens/min rate against
// the (stable) configured poll interval so published frames are
// deterministic; tick-to-tick poll jitter is far below display precision.
type tokenRing struct {
	deltas []int64
}

func (r *tokenRing) push(delta int64) {
	r.deltas = append(r.deltas, delta)
	if len(r.deltas) > sparkCap {
		r.deltas = r.deltas[len(r.deltas)-sparkCap:]
	}
}

func (r *tokenRing) rates(interval time.Duration) []float64 {
	perMin := interval.Minutes()
	if perMin <= 0 {
		perMin = defaultPollInterval.Minutes()
	}
	out := make([]float64, len(r.deltas))
	for i, d := range r.deltas {
		out[i] = float64(d) / perMin
	}
	return out
}

// observeSpark records one tick's token delta for a session. Deltas floor at
// zero (counters only move forward) so a reset upstream never renders a
// negative rate.
func (c *Collector) observeSpark(id string, total int64) {
	ring, ok := c.sparks[id]
	if !ok {
		ring = &tokenRing{}
		c.sparks[id] = ring
	}
	prev, seen := c.prevTotals[id]
	c.prevTotals[id] = total
	if !seen {
		ring.push(0)
	} else {
		delta := total - prev
		if delta < 0 {
			delta = 0
		}
		ring.push(delta)
	}
}

// projectSignals maps a DB-provided status back onto the primitive signals
// that derive it, so the single derivation authority (model.DeriveStatus,
// called exactly once per row in Tick) stays the only place status logic
// lives. Why the round-trip: the frozen ports seam carries SessionRow, not
// StatusSignals — W1-1 owns signal detection over the WAL db and ships the
// derived status. Re-affirming through DeriveStatus canonicalizes any value
// to the valid set instead of reimplementing precedence. recent carries the
// row's recency (UpdatedRelativeMs inside the active window): every one of
// the five contract statuses round-trips to itself when recent; ancient rows
// gate to idle (archived still wins) exactly as the db path derives.
func projectSignals(status model.SessionStatus, recent bool) model.StatusSignals {
	switch status {
	case model.SessionRunning:
		return model.StatusSignals{InFlightAssistant: true, RecentActivity: recent}
	case model.SessionError:
		return model.StatusSignals{HasError: true, RecentActivity: recent}
	case model.SessionArchived:
		return model.StatusSignals{Archived: true}
	case model.SessionWaitingInput:
		return model.StatusSignals{LatestRole: "user", RecentActivity: recent}
	default:
		return model.StatusSignals{RecentActivity: recent}
	}
}

// statusRank orders the fleet table running-first; exited panes sink below
// every live row regardless of status. Unknown statuses sort with idle.
func statusRank(s model.SessionStatus) int {
	switch s {
	case model.SessionRunning:
		return 0
	case model.SessionWaitingInput:
		return 1
	case model.SessionIdle:
		return 2
	case model.SessionError:
		return 3
	case model.SessionArchived:
		return 4
	default:
		return 2
	}
}

// sortSessions orders rows running to exited with a deterministic ID
// tiebreak so consecutive ticks render stably.
func sortSessions(rows []model.SessionRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := 0, 0
		if rows[i].Pane.Exited {
			ri = 100
		}
		if rows[j].Pane.Exited {
			rj = 100
		}
		ri += statusRank(rows[i].Status)
		rj += statusRank(rows[j].Status)
		if ri != rj {
			return ri < rj
		}
		return rows[i].ID < rows[j].ID
	})
}

// buildKPIs aggregates one tick's fleet strip. Throughput and today's cost
// come from the background refresher's message-level sample: session totals
// roll up subagent children, so a completing child completes its whole
// history at once and a delta of those totals is a lie. CostTotal sums each
// row's own pre-roll-up cost, so children are not counted twice. SSEConnected
// reports the collector-to-sink path the collector itself owns. livePanes and
// liveProcesses are the runtime source's ground truth (see
// ports.RuntimeSource.LiveCounts) so the operator can compare sessions
// running against processes actually alive.
func buildKPIs(rows []model.SessionRow, metrics kpiMetrics, livePanes, liveProcesses int) model.KPIs {
	kpis := model.KPIs{
		TokensPerHour: float64(metrics.tokensLast60s) * 60,
		CostToday:     metrics.costSinceMidnight,
		MetricsAgeMs:  metrics.ageMs,
		SSEConnected:  true,
		LivePanes:     livePanes,
		LiveProcesses: liveProcesses,
	}
	for _, row := range rows {
		switch row.Status {
		case model.SessionRunning:
			kpis.Active++
		case model.SessionWaitingInput:
			kpis.WaitingInput++
		case model.SessionError:
			kpis.Errors++
		}
		kpis.CostTotal += row.RawCost
	}
	return kpis
}
