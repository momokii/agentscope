package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

func Test_Collector_publishes_message_metrics_and_age(t *testing.T) {
	// Given: a successful refresh at t0 and a tick at the same instant
	t0 := time.UnixMilli(1800000000000)
	db := &fakeDB{
		rows:    []model.SessionRow{rowFixture("ses-1", model.SessionRunning, 50, 1)},
		updMs:   1,
		evID:    1,
		metrics: ports.MessageMetrics{TokensLast60s: 1234, CostSinceMidnight: 0.25},
	}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	c.clock = &stubClock{times: []time.Time{t0, t0, t0}}

	// When
	c.refreshMetrics(context.Background())
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	kpis := got[0].KPIs
	if kpis.TokensPerHour != 1234*60 {
		t.Errorf("TokensPerHour = %v, want %v", kpis.TokensPerHour, float64(1234*60))
	}
	if kpis.CostToday != 0.25 {
		t.Errorf("CostToday = %v, want 0.25", kpis.CostToday)
	}
	if kpis.CostTotal != 1 {
		t.Errorf("CostTotal = %v, want 1", kpis.CostTotal)
	}
	if kpis.MetricsAgeMs != 0 {
		t.Errorf("MetricsAgeMs = %d, want 0", kpis.MetricsAgeMs)
	}
}

func Test_Collector_metrics_failure_keeps_last_good_and_never_marks_stale(t *testing.T) {
	// Given: one good sample at t0, then the DB starts failing
	t0 := time.UnixMilli(1800000000000)
	db := &fakeDB{
		rows:    []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 5, 1)},
		updMs:   1,
		evID:    1,
		metrics: ports.MessageMetrics{TokensLast60s: 100, CostSinceMidnight: 0.1},
	}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	c.clock = &stubClock{times: []time.Time{t0, t0.Add(time.Second), t0.Add(time.Second)}}
	c.refreshMetrics(context.Background())

	// When: a refresh fails and the fleet tick publishes a moved watermark
	db.metricsErr = errors.New("metrics query exploded")
	c.refreshMetrics(context.Background())
	db.setWatermark(2, 1)
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then: last-good values survive, age grows, the frame is not stale
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	kpis := got[0].KPIs
	if kpis.TokensPerHour != 100*60 || kpis.CostToday != 0.1 {
		t.Errorf("metrics = (%v,%v), want last-good (6000,0.1)", kpis.TokensPerHour, kpis.CostToday)
	}
	if kpis.MetricsAgeMs != 1000 {
		t.Errorf("MetricsAgeMs = %d, want 1000", kpis.MetricsAgeMs)
	}
	if got[0].Stale {
		t.Error("a metrics failure must not mark the fleet stale")
	}
	if !c.metricsWarned.Load() {
		t.Error("metrics failure must be recorded for warn-once logging")
	}
}

func Test_Collector_Tick_not_blocked_by_slow_metrics_refresh(t *testing.T) {
	// Given: a metrics query that blocks until released
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	db := &fakeDB{
		rows:           []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 1, 0)},
		updMs:          1,
		evID:           1,
		metricsBlock:   release,
		metricsStarted: started,
	}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.metricsLoop(ctx)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics refresh never started")
	}

	// When
	start := time.Now()
	_, err := c.Tick(context.Background())
	elapsed := time.Since(start)

	// Then: the tick completes while the refresh is still blocked
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("tick waited %v on a blocked metrics refresh", elapsed)
	}
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics loop did not stop on cancel")
	}
}

func Test_Collector_metricsLoop_refreshes_on_interval(t *testing.T) {
	// Given: a loop configured with a short test interval
	started := make(chan struct{}, 4)
	db := &fakeDB{metricsStarted: started}
	c := newTestCollector(db, &fakeRuntime{}, &fakeSink{})
	c.metricsInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.metricsLoop(ctx)
	}()

	// When: the initial refresh plus one ticker refresh complete
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("refresh %d never started", i+1)
		}
	}

	// Then
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics loop did not stop on cancel")
	}
}
