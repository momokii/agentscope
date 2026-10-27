package collector

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"opencode-dashboard/internal/model"
)

// piSessions runs the optional pi merge for one tick. A nil source returns
// nothing (opencode-only host, zero behavior change). A store failure
// degrades to "no pi rows this tick" with a warn-once line — it never marks
// the frame stale and never fails the tick, because pi is best-effort
// presence next to the authoritative DB join. The pi reader never caches
// failures, so a store that appears later flows on the next tick.
func (c *Collector) piSessions(ctx context.Context) []model.SessionRow {
	if c.pi == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.dbTimeout)
	defer cancel()
	rows, err := c.pi.PiSessions(ctx)
	if err != nil {
		if c.piWarned.CompareAndSwap(false, true) {
			slog.WarnContext(ctx, "collector pi store unreadable, pi rows omitted (retrying next tick)",
				slog.Any("err", err))
		}
		return nil
	}
	return rows
}

// piUnchanged reports whether the pi scan matches the last published tick.
// The DB watermark (MAX(time_updated), MAX(event rowid)) cannot see
// pi-only activity, so the skip path consults the cheap stat-cached pi
// scan instead of skipping blindly. It compares the derived signature —
// recomputed per tick from the clock, so a pi session aging out of the
// recency window still un-skips the tick.
func (c *Collector) piUnchanged(ctx context.Context) bool {
	if c.pi == nil {
		return true
	}
	return piSignature(c.piSessions(ctx)) == c.lastPiSig
}

// piSignature fingerprints one tick's pi rows order-independently: session
// identity plus the derived status and title (a /name rename republishes).
// Tokens/cost are excluded — they flow through the metrics refresher and
// must not un-skip ticks by themselves.
func piSignature(rows []model.SessionRow) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, r.ID+"\x00"+string(r.Status)+"\x00"+r.Title)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x01")
}
