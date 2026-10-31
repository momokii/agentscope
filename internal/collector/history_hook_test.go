package collector

import (
	"context"
	"testing"
	"time"

	"opencode-dashboard/internal/history"
	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// The history hook records one fleet + per-directory sample per published
// tick (throttled to the metrics cadence), and stays silent when unwired.
func Test_Collector_records_history_on_publish(t *testing.T) {
	dir := t.TempDir()
	store, err := history.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	row := rowFixture("ses-1", model.SessionRunning, 50, 1)
	row.Directory = "/proj/alpha"
	db := &fakeDB{
		rows:  []model.SessionRow{row},
		updMs: 1, evID: 1,
		metrics:  ports.MessageMetrics{TokensLast60s: 60, CostSinceMidnight: 0.5},
		wmetrics: ports.WindowMetrics{Tokens24h: 120, Cost24h: 0.25, BySession: map[string]model.WindowSums{"ses-1": {Tokens24h: 120, Cost24h: 0.25}}},
	}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), nil)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	c.SetHistory(store)
	ctx := context.Background()
	// The metrics refresher is a background goroutine in production; tests
	// drive it explicitly so the tick sees a fresh windows sample.
	c.refreshMetrics(ctx)
	// Two ticks: the second must still record (interval forced to zero via
	// lastHistoryAt reset) to prove the hook runs post-publish, not once.
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	c.lastHistoryAt = time.Time{}
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	got, err := store.Query(context.Background(), 0, time.Now().Add(time.Hour).Unix())
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got.Points) < 1 {
		t.Fatalf("expected >=1 fleet sample, got %+v", got)
	}
	if got.Points[0].Cost24h != 0.25 {
		t.Errorf("fleet cost_24h = %v, want 0.25", got.Points[0].Cost24h)
	}
	if len(got.Projects) != 1 || got.Projects[0].Cost24h != 0.25 {
		t.Errorf("projects = %+v, want one project with cost 0.25", got.Projects)
	}
}

func Test_Collector_skips_history_when_unwired(t *testing.T) {
	// Nil history must never fail or stall a tick.
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 5, 1)}, updMs: 1, evID: 1}
	c := newTestCollector(db, &fakeRuntime{}, &fakeSink{})
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("tick without history must succeed: %v", err)
	}
}
