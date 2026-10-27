package collector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// fakePi serves canned pi rows or a store error, counting calls so merge
// tests can assert the pi scan runs per tick without failing it.
type fakePi struct {
	mu       sync.Mutex
	rows     []model.SessionRow
	err      error
	calls    int
	pmetrics ports.MessageMetrics
	perr     error
}

func (f *fakePi) PiSessions(ctx context.Context) ([]model.SessionRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([]model.SessionRow, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakePi) PiMessageMetrics(ctx context.Context, _ time.Time) (ports.MessageMetrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.perr != nil {
		return ports.MessageMetrics{}, f.perr
	}
	return f.pmetrics, nil
}

func (f *fakePi) setRows(rows []model.SessionRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = rows
}

func piFixture(id, title string, status model.SessionStatus) model.SessionRow {
	r := rowFixture(id, status, 50, 0.5)
	r.Harness = model.HarnessPi
	r.Title = title
	return r
}

// Compile-time check that fakePi satisfies the seam under test.
var _ ports.PiReader = (*fakePi)(nil)

func Test_Collector_Tick_merges_pi_rows(t *testing.T) {
	// Given: one opencode row from the DB and one pi row from the store.
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-oc", model.SessionIdle, 100, 1.5)}, updMs: 1, evID: 1}
	pi := &fakePi{rows: []model.SessionRow{piFixture("pi-1", "pi work", model.SessionRunning)}}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), pi)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	// When
	published, err := c.Tick(context.Background())

	// Then: both harnesses land in one snapshot, running-first.
	if err != nil || !published {
		t.Fatalf("Tick: published=%v err=%v", published, err)
	}
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(got))
	}
	rows := got[0].Sessions
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (opencode + pi)", len(rows))
	}
	if rows[0].ID != "pi-1" || rows[0].Harness != model.HarnessPi {
		t.Errorf("first row must be the running pi session: %+v", rows[0])
	}
	if rows[1].ID != "ses-oc" || rows[1].Harness != model.HarnessOpencode {
		t.Errorf("second row must be the idle opencode session: %+v", rows[1])
	}
	if got[0].KPIs.Active != 1 {
		t.Errorf("KPIs.Active = %d, want 1 (pi running)", got[0].KPIs.Active)
	}
}

func Test_Collector_Tick_pi_error_neither_stales_nor_fails(t *testing.T) {
	// Given: a dead pi store next to a healthy DB.
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-oc", model.SessionIdle, 100, 1.5)}, updMs: 1, evID: 1}
	pi := &fakePi{err: errors.New("pi store boom")}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), pi)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	// When
	published, err := c.Tick(context.Background())

	// Then: the DB frame still publishes fresh, pi rows omitted.
	if err != nil || !published {
		t.Fatalf("Tick: published=%v err=%v", published, err)
	}
	got := sink.published()
	if len(got) != 1 || got[0].Stale {
		t.Fatalf("pi failure must not stale the frame: %+v", got)
	}
	if len(got[0].Sessions) != 1 {
		t.Fatalf("sessions = %+v, want DB row only", got[0].Sessions)
	}
}

func Test_Collector_Tick_pi_change_unskips_watermark(t *testing.T) {
	// Given: a static DB watermark and a pi store that gains a session.
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-oc", model.SessionIdle, 100, 1.5)}, updMs: 1, evID: 1}
	pi := &fakePi{rows: []model.SessionRow{piFixture("pi-1", "one", model.SessionIdle)}}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), pi)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	ctx := context.Background()
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	// When: only the pi side changes (title rename).
	pi.setRows([]model.SessionRow{piFixture("pi-1", "two", model.SessionIdle)})
	published, err := c.Tick(ctx)

	// Then: the tick must publish despite the static DB watermark.
	if err != nil || !published {
		t.Fatalf("pi-only change must un-skip the tick: published=%v err=%v", published, err)
	}
	if n := len(sink.published()); n != 2 {
		t.Fatalf("snapshots = %d, want 2", n)
	}

	// When: nothing changes anywhere.
	published, err = c.Tick(ctx)

	// Then: the tick skips again.
	if err != nil || published {
		t.Fatalf("quiet tick must skip: published=%v err=%v", published, err)
	}
	if n := len(sink.published()); n != 2 {
		t.Fatalf("snapshots = %d, want still 2", n)
	}
}

func Test_Collector_refreshMetrics_sums_pi_sample(t *testing.T) {
	// Given: DB and pi samples over the same instant.
	t0 := time.UnixMilli(1800000000000)
	db := &fakeDB{
		rows:    []model.SessionRow{rowFixture("ses-1", model.SessionRunning, 50, 1)},
		updMs:   1,
		evID:    1,
		metrics: ports.MessageMetrics{TokensLast60s: 1000, CostSinceMidnight: 0.5},
	}
	pi := &fakePi{pmetrics: ports.MessageMetrics{TokensLast60s: 234, CostSinceMidnight: 0.25}}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), pi)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	c.clock = &stubClock{times: []time.Time{t0, t0, t0}}

	// When
	c.refreshMetrics(context.Background())
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then: the fleet sample is the plain sum of both harnesses.
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	kpis := got[0].KPIs
	if kpis.TokensPerHour != 1234*60 {
		t.Errorf("TokensPerHour = %v, want %v", kpis.TokensPerHour, float64(1234*60))
	}
	if kpis.CostToday != 0.75 {
		t.Errorf("CostToday = %v, want 0.75", kpis.CostToday)
	}
}

func Test_Collector_pi_metrics_failure_keeps_last_good(t *testing.T) {
	// Given: one good combined sample, then the pi store starts failing.
	t0 := time.UnixMilli(1800000000000)
	db := &fakeDB{
		rows:    []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 5, 1)},
		updMs:   1,
		evID:    1,
		metrics: ports.MessageMetrics{TokensLast60s: 100, CostSinceMidnight: 0.1},
	}
	pi := &fakePi{pmetrics: ports.MessageMetrics{TokensLast60s: 50, CostSinceMidnight: 0.05}}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, testConfig(), pi)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	c.clock = &stubClock{times: []time.Time{t0, t0, t0, t0.Add(time.Minute), t0.Add(time.Minute)}}
	c.refreshMetrics(context.Background())
	pi.perr = errBoomPi()

	// When: the pi refresh fails.
	c.refreshMetrics(context.Background())

	// Then: last-good combined values persist (150 tokens, ~0.15 cost).
	m, age := c.metricsValue(t0.Add(time.Minute))
	if m.tokensLast60s != 150 || abs(m.costSinceMidnight-0.15) > 1e-9 {
		t.Errorf("sample = %+v, want combined last-good {150 0.15}", m)
	}
	if age < 0 {
		t.Errorf("age = %d, want non-negative (sample exists)", age)
	}
}

func errBoomPi() error {
	return errors.New("pi store boom")
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func Test_piSignature_order_independent_and_status_sensitive(t *testing.T) {
	a := piFixture("pi-1", "A", model.SessionRunning)
	b := piFixture("pi-2", "B", model.SessionIdle)
	if piSignature([]model.SessionRow{a, b}) != piSignature([]model.SessionRow{b, a}) {
		t.Error("signature must be order-independent")
	}
	running := piSignature([]model.SessionRow{a})
	a.Status = model.SessionIdle
	if piSignature([]model.SessionRow{a}) == running {
		t.Error("signature must change when a status flips")
	}
	if piSignature(nil) != piSignature([]model.SessionRow{}) {
		t.Error("nil and empty pi scans must share the empty signature")
	}
}
