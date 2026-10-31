package history

import (
	"context"
	"fmt"
)

// HistoryRange is a validated trailing window for GET /api/history.
// The hub rejects anything else with 400 (unknown ranges would silently
// change meaning, so the contract is closed, not best-effort).
type HistoryRange string

const (
	Range7d  HistoryRange = "7d"
	Range30d HistoryRange = "30d"
)

// ParseRange validates the raw ?range= value. Empty means the default.
func ParseRange(raw string) (HistoryRange, error) {
	switch raw {
	case "", "7d":
		return Range7d, nil
	case "30d":
		return Range30d, nil
	default:
		return "", fmt.Errorf("history: unknown range %q (want 7d|30d)", raw)
	}
}

// WindowSeconds returns the trailing window in seconds.
func (r HistoryRange) WindowSeconds() int64 {
	if r == Range30d {
		return 30 * 24 * 3600
	}
	return 7 * 24 * 3600
}

// History is one GET /api/history response: the decimated fleet series
// plus the top projects by trailing-24h cost, each with its own decimated
// series for sparklines. All values are exact recorded samples (decimation
// picks real rows, never averages), so peaks survive the downsampling.
type History struct {
	From     int64            `json:"from"`
	To       int64            `json:"to"`
	Points   []Point          `json:"points"`
	Projects []ProjectHistory `json:"projects"`
}

// ProjectHistory is one directory's history slice for the table.
type ProjectHistory struct {
	Directory string  `json:"directory"`
	Sessions  int     `json:"sessions"`
	Running   int     `json:"running"`
	Cost24h   float64 `json:"cost_24h"`
	Cost7d    float64 `json:"cost_7d"`
	Cost30d   float64 `json:"cost_30d"`
	Spark     []Point `json:"spark"`
}

// maxProjectsPerView bounds the per-view directory fan-out: the table shows
// the top-N directories by trailing-24h cost so one pathological host (say
// 2000 stale directories) cannot bloat the response.
const maxProjectsPerView = 12

// maxResponsePoints bounds decimated series so a 30d view stays light.
// 360 points render a smooth line at any viewport width.
const maxResponsePoints = 360

// Query returns decimated fleet points plus per-project slices for
// [from,to] (unix seconds, inclusive). Points are real recorded rows
// (stride-decimated, never averaged). An empty store yields empty slices,
// never an error — the UI renders the honest empty state.
func (s *Store) Query(ctx context.Context, from, to int64) (History, error) {
	out := History{From: from, To: to, Points: []Point{}, Projects: []ProjectHistory{}}
	if s == nil || s.db == nil {
		return out, ErrDisabled
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	fleet, err := s.queryFleet(ctx, from, to)
	if err != nil {
		return out, err
	}
	out.Points = decimate(fleet, maxResponsePoints)
	projects, err := s.queryProjects(ctx, from, to)
	if err != nil {
		return out, err
	}
	out.Projects = projects
	return out, nil
}

func (s *Store) queryFleet(ctx context.Context, from, to int64) ([]Point, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, tokens_24h, cost_24h, tokens_7d, cost_7d, tokens_30d, cost_30d, active, waiting, errors, live_procs, live_panes FROM fleet_samples WHERE ts BETWEEN ? AND ? ORDER BY ts ASC`, from, to)
	if err != nil {
		return nil, fmt.Errorf("history: query fleet: %w", err)
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Ts, &p.Tokens24h, &p.Cost24h, &p.Tokens7d, &p.Cost7d, &p.Tokens30d, &p.Cost30d, &p.Active, &p.Waiting, &p.Errors, &p.LiveProcs, &p.LivePanes); err != nil {
			return nil, fmt.Errorf("history: scan fleet: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// queryProjects returns the top directories by trailing-24h cost in range,
// each with a decimated per-directory series (for table sparklines) plus
// the latest window totals.
func (s *Store) queryProjects(ctx context.Context, from, to int64) ([]ProjectHistory, error) {
	top, err := s.db.QueryContext(ctx, `SELECT directory, MAX(sessions), MAX(running), MAX(tokens_24h), MAX(cost_24h), MAX(tokens_7d), MAX(cost_7d), MAX(tokens_30d), MAX(cost_30d) FROM project_samples WHERE ts BETWEEN ? AND ? GROUP BY directory ORDER BY MAX(cost_24h) DESC LIMIT ?`, from, to, maxProjectsPerView)
	if err != nil {
		return nil, fmt.Errorf("history: top projects: %w", err)
	}
	defer top.Close()
	type row struct {
		dir       string
		sessions  int
		running   int
		tokens24h int64
		cost24h   float64
		tokens7d  int64
		cost7d    float64
		tokens30d int64
		cost30d   float64
	}
	var dirs []row
	for top.Next() {
		var r row
		if err := top.Scan(&r.dir, &r.sessions, &r.running, &r.tokens24h, &r.cost24h, &r.tokens7d, &r.cost7d, &r.tokens30d, &r.cost30d); err != nil {
			return nil, fmt.Errorf("history: scan project top: %w", err)
		}
		dirs = append(dirs, r)
	}
	if err := top.Err(); err != nil {
		return nil, err
	}
	out := make([]ProjectHistory, 0, len(dirs))
	for _, d := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		series, err := s.db.QueryContext(ctx, `SELECT ts, tokens_24h, cost_24h FROM project_samples WHERE ts BETWEEN ? AND ? AND directory = ? ORDER BY ts ASC`, from, to, d.dir)
		if err != nil {
			return nil, fmt.Errorf("history: project series %s: %w", d.dir, err)
		}
		var raw []Point
		for series.Next() {
			var p Point
			p.Directory = d.dir
			if err := series.Scan(&p.Ts, &p.Tokens24h, &p.Cost24h); err != nil {
				series.Close()
				return nil, fmt.Errorf("history: scan project series: %w", err)
			}
			raw = append(raw, p)
		}
		series.Close()
		if err := series.Err(); err != nil {
			return nil, err
		}
		out = append(out, ProjectHistory{
			Directory: d.dir,
			Sessions:  d.sessions,
			Running:   d.running,
			Cost24h:   d.cost24h,
			Cost7d:    d.cost7d,
			Cost30d:   d.cost30d,
			Spark:     decimate(raw, 60),
		})
	}
	return out, nil
}

// decimate thins a chronological series to at most n points by stride,
// always keeping the first and last samples so the range edges stay exact.
func decimate(points []Point, n int) []Point {
	if n <= 0 || len(points) <= n {
		return points
	}
	if n == 1 {
		return points[len(points)-1:]
	}
	stride := float64(len(points)-1) / float64(n-1)
	out := make([]Point, 0, n)
	prev := -1
	for i := 0; i < n; i++ {
		idx := int(float64(i) * stride)
		if idx == prev {
			continue
		}
		prev = idx
		out = append(out, points[idx])
	}
	if out[len(out)-1] != points[len(points)-1] {
		out = append(out, points[len(points)-1])
	}
	return out
}
