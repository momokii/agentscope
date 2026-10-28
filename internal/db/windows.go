package db

import (
	"context"
	"fmt"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// Window queries (off-tick metrics refresher only — never the poll tick).
// Both scan the message table with a 30-day high-water bound; the refresher
// owns cadence (60s) and timeout, and any failure keeps last-good values.
//
// Q13 global windows: one pass with conditional sums (tokens 24h/7d/30d,
// cost midnight/24h/7d/30d). Same json_valid discipline as Q12: one corrupt
// blob contributes zero instead of failing the refresh.
// Q14 per-session windows: conditional sums grouped by session_id over the
// same 30d range (sparse — only sessions with window activity return rows).
// The collector maps session → directory for project attribution.
const queryGlobalWindows = `
SELECT
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0)
  FROM message WHERE time_created >= ?`

const querySessionWindows = `
SELECT session_id,
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.tokens.total'),0) AS INTEGER)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0),
  COALESCE(SUM(CASE WHEN time_created >= ? AND json_valid(data)
                    THEN CAST(COALESCE(json_extract(data,'$.cost'),0) AS REAL)
                    ELSE 0 END),0)
  FROM message WHERE time_created >= ? GROUP BY session_id`

// WindowMetrics implements the windowed half of ports.DBReader: trailing
// 24h/7d/30d token/cost sums globally plus the sparse per-session map, and
// local-midnight cost for the today preset. The caller owns cadence and
// timeout; this method performs indexed time-range scans only (the 30d
// high-water bound keeps every tick of the scan O(window)) and never runs
// inside a collector tick.
func (r *Reader) WindowMetrics(ctx context.Context, now time.Time) (ports.WindowMetrics, error) {
	nowMs := now.UnixMilli()
	d24 := nowMs - int64((24 * time.Hour).Milliseconds())
	d7 := nowMs - int64((7 * 24 * time.Hour).Milliseconds())
	d30 := nowMs - int64((30 * 24 * time.Hour).Milliseconds())
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()

	var m ports.WindowMetrics
	err := withBusyRetry(ctx, "window metrics", func() error {
		if err := r.db.QueryRowContext(ctx, queryGlobalWindows,
			d24, d7, d30, midnight, d24, d7, d30, d30).Scan(
			&m.Tokens24h, &m.Tokens7d, &m.Tokens30d,
			&m.CostMidnight, &m.Cost24h, &m.Cost7d, &m.Cost30d); err != nil {
			return err
		}
		rows, err := r.db.QueryContext(ctx, querySessionWindows,
			d24, d7, d30, d24, d7, d30, d30)
		if err != nil {
			return err
		}
		defer rows.Close()
		m.BySession = make(map[string]model.WindowSums)
		for rows.Next() {
			var id string
			var s model.WindowSums
			if err := rows.Scan(&id, &s.Tokens24h, &s.Tokens7d, &s.Tokens30d,
				&s.Cost24h, &s.Cost7d, &s.Cost30d); err != nil {
				return err
			}
			if s.Tokens24h|s.Tokens7d|s.Tokens30d != 0 ||
				s.Cost24h+s.Cost7d+s.Cost30d != 0 {
				m.BySession[id] = s
			}
		}
		return rows.Err()
	})
	if err != nil {
		return ports.WindowMetrics{}, fmt.Errorf("db: window metrics: %w", err)
	}
	return m, nil
}
