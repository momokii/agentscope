package db

import (
	"context"
	"fmt"
	"time"

	"opencode-dashboard/internal/ports"
)

// queryMessageMetrics is the off-tick throughput/spend scan. It is
// deliberately message-level: session.cost/tokens are cumulative and
// roll up subagent children, so a completing child looks like a burst on
// the parent. One scan from local midnight (the expensive part, ~3.5s on
// the live 8.2GB WAL db) serves both numbers; the 60-second window is a
// conditional over those rows. json_valid keeps one corrupt blob from
// failing the whole refresh (the refresher then keeps last-good values).
const queryMessageMetrics = `
SELECT
  COALESCE(SUM(CASE WHEN time_created >= ?
                    THEN CASE WHEN json_valid(data)
                              THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                              ELSE 0 END
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0)
  FROM message
 WHERE time_created >= ?`

// MessageMetrics implements ports.DBReader. The caller owns cadence and
// timeout; this method performs exactly one indexed-unfriendly time-range
// scan and never runs inside a collector tick.
func (r *Reader) MessageMetrics(ctx context.Context, now time.Time) (ports.MessageMetrics, error) {
	nowMs := now.UnixMilli()
	since60s := nowMs - int64((60 * time.Second).Milliseconds())
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var m ports.MessageMetrics
	err := withBusyRetry(ctx, "message metrics", func() error {
		return r.db.QueryRowContext(ctx, queryMessageMetrics, since60s, midnight.UnixMilli()).
			Scan(&m.TokensLast60s, &m.CostSinceMidnight)
	})
	if err != nil {
		return ports.MessageMetrics{}, fmt.Errorf("db: message metrics: %w", err)
	}
	return m, nil
}
