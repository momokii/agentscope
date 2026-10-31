package history

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func kpisWith(cost24h float64, active int) model.KPIs {
	return model.KPIs{Cost24h: cost24h, Active: active, LiveProcesses: 1}
}

func rowWith(id, dir string, status model.SessionStatus) model.SessionRow {
	return model.SessionRow{ID: id, Directory: dir, Status: status, Tokens: model.TokenCounts{Total: 100}, Cost: 0.01}
}

func Test_Open_missing_dir_is_disabled_not_created(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := Open(missing); err == nil {
		t.Fatal("missing dir should fail (never silently write into an ephemeral overlay)")
	} else if !isDisabled(err) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	if _, err := OpenWithRetention(missing, -5); !isDisabled(err) {
		t.Fatalf("negative retention should still fail closed, got %v", err)
	}
}

func isDisabled(err error) bool {
	if err == nil {
		return false
	}
	for err != nil {
		if err == ErrDisabled {
			return true
		}
		un, ok := err.(interface{ Unwrap() []error })
		if ok {
			found := false
			for _, e := range un.Unwrap() {
				if isDisabled(e) {
					found = true
				}
			}
			return found
		}
		u, ok2 := err.(interface{ Unwrap() error })
		if !ok2 {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func Test_Record_roundtrip_and_sparse_projects(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	windows := map[string]model.WindowSums{
		"ses_a": {Tokens24h: 100, Cost24h: 1.5},
		// ses_b has no window activity: contributes zeros, still counted.
	}
	rows := []model.SessionRow{
		rowWith("ses_a", "/proj/alpha", model.SessionRunning),
		rowWith("ses_b", "/proj/alpha", model.SessionIdle),
		rowWith("ses_c", "/proj/beta", model.SessionIdle),
	}
	if err := s.Record(ctx, now, kpisWith(1.5, 1), rows, windows); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Query(ctx, now.Add(-time.Hour).Unix(), now.Add(time.Hour).Unix())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got.Points) != 1 {
		t.Fatalf("points = %d, want 1", len(got.Points))
	}
	if got.Points[0].Cost24h != 1.5 || got.Points[0].Active != 1 {
		t.Errorf("point = %+v, want cost 1.5 active 1", got.Points[0])
	}
	if len(got.Projects) != 2 {
		t.Fatalf("projects = %d, want alpha+beta", len(got.Projects))
	}
	byDir := map[string]ProjectHistory{}
	for _, p := range got.Projects {
		byDir[p.Directory] = p
	}
	if byDir["/proj/alpha"].Cost24h != 1.5 || byDir["/proj/alpha"].Sessions != 2 || byDir["/proj/alpha"].Running != 1 {
		t.Errorf("alpha = %+v, want cost 1.5 sessions 2 running 1", byDir["/proj/alpha"])
	}
	if len(byDir["/proj/alpha"].Spark) != 1 {
		t.Errorf("alpha spark = %d points, want 1", len(byDir["/proj/alpha"].Spark))
	}
	if byDir["/proj/beta"].Cost24h != 0 || byDir["/proj/beta"].Sessions != 1 {
		t.Errorf("beta = %+v, want zero-cost presence", byDir["/proj/beta"])
	}
}

func Test_Query_clamps_and_decimates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		k := kpisWith(float64(i), i%3)
		if err := s.Record(ctx, ts, k, nil, nil); err != nil {
			t.Fatalf("Record %d: %v", i, err)
		}
	}
	got, err := s.Query(ctx, base.Unix(), base.Add(9*time.Hour).Unix())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got.Points) != 10 {
		t.Fatalf("points = %d, want all 10 (under cap)", len(got.Points))
	}
	// Decimation keeps endpoints and stays within budget.
	many := make([]Point, 1000)
	for i := range many {
		many[i] = Point{Ts: int64(i)}
	}
	if got := decimate(many, 360); len(got) > 360 {
		t.Fatalf("decimated = %d, want <= 360", len(got))
	} else if got[0].Ts != 0 || got[len(got)-1].Ts != 999 {
		t.Fatalf("decimation must keep both edges, got %d..%d", got[0].Ts, got[len(got)-1].Ts)
	}
	if got := decimate(nil, 10); len(got) != 0 {
		t.Fatalf("decimate nil = %d, want empty", len(got))
	}
}

func Test_Retention_evicts_old_samples(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenWithRetention(dir, 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	old := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fresh := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if err := s.Record(ctx, old, kpisWith(9, 9), []model.SessionRow{rowWith("s", "/p", model.SessionIdle)}, nil); err != nil {
		t.Fatalf("Record old: %v", err)
	}
	if err := s.Record(ctx, fresh, kpisWith(1, 1), nil, nil); err != nil {
		t.Fatalf("Record fresh: %v", err)
	}
	got, err := s.Query(ctx, old.Add(-time.Hour).Unix(), fresh.Add(time.Hour).Unix())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got.Points) != 1 || got.Points[0].Cost24h != 1 {
		t.Fatalf("retention should leave only the fresh row: %+v", got.Points)
	}
}

func Test_ParseRange(t *testing.T) {
	if r, err := ParseRange(""); err != nil || r != Range7d {
		t.Errorf("empty = %q,%v, want 7d default", r, err)
	}
	if r, err := ParseRange("30d"); err != nil || r != Range30d {
		t.Errorf("30d = %q,%v", r, err)
	}
	if _, err := ParseRange("90d"); err == nil {
		t.Error("90d should be rejected (closed contract)")
	}
}

func Test_Record_nil_store_is_disabled(t *testing.T) {
	var s *Store
	if err := s.Record(context.Background(), time.Now(), model.KPIs{}, nil, nil); !isDisabled(err) {
		t.Fatalf("nil store Record should report disabled, got %v", err)
	}
	if _, err := s.Query(context.Background(), 0, 1); !isDisabled(err) {
		t.Fatalf("nil store Query should report disabled, got %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("nil Close should be nil, got %v", err)
	}
}
