package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// orderLog records cross-fake call order to prove the join sequence
// (DBReader.Sessions -> RuntimeSource.Enrich -> SnapshotSink.Publish).
type orderLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *orderLog) add(call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, call)
}

func (l *orderLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

type fakeDB struct {
	log *orderLog
	mu  sync.Mutex

	rows    []model.SessionRow
	sessErr error
	updMs   int64
	evID    int64
	wmErr   error

	sessCalls int
	wmCalls   int
}

func (f *fakeDB) Sessions(ctx context.Context) ([]model.SessionRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessCalls++
	if f.log != nil {
		f.log.add("db.sessions")
	}
	if f.sessErr != nil {
		return nil, f.sessErr
	}
	out := make([]model.SessionRow, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeDB) PollState(ctx context.Context) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wmCalls++
	if f.log != nil {
		f.log.add("db.pollstate")
	}
	if f.wmErr != nil {
		return 0, 0, f.wmErr
	}
	return f.updMs, f.evID, nil
}

// SessionDetail keeps fakeDB satisfying ports.DBReader after the interface
// grew the drill-down seam. The collector never calls it (fleet ticks only);
// the stub reports unavailability so a mistaken call fails loudly.
func (f *fakeDB) SessionDetail(_ context.Context, id string) (model.SessionDetail, error) {
	return model.SessionDetail{}, fmt.Errorf("fakeDB: no detail for %s", id)
}

func (f *fakeDB) setWatermark(updMs, evID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updMs, f.evID = updMs, evID
}

func (f *fakeDB) setSessionsErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessErr = err
}

func (f *fakeDB) setTokenTotal(id string, total int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].Tokens.Total = total
			f.rows[i].Tokens.Out = total
		}
	}
}

func (f *fakeDB) sessionCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessCalls
}

func (f *fakeDB) watermarkCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wmCalls
}

type fakeRuntime struct {
	log       *orderLog
	enrichErr error
	panes     int
	procs     int
}

func (f *fakeRuntime) LiveCounts() (int, int) { return f.panes, f.procs }

func (f *fakeRuntime) Enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error) {
	if f.log != nil {
		f.log.add("runtime.enrich")
	}
	if f.enrichErr != nil {
		return nil, f.enrichErr
	}
	for i := range rows {
		rows[i].Pane = model.PaneInfo{CWD: "/live/" + rows[i].ID}
		rows[i].Orphan = false
	}
	return rows, nil
}

type fakeSink struct {
	log *orderLog
	mu  sync.Mutex

	snaps        []model.Snapshot
	publishErr   error
	failTimes    int
	blockFor     time.Duration
	publishCalls int
}

func (f *fakeSink) Publish(ctx context.Context, snap model.Snapshot) error {
	f.mu.Lock()
	f.publishCalls++
	blockFor := f.blockFor
	fail := f.failTimes > 0
	if fail {
		f.failTimes--
	}
	err := f.publishErr
	f.mu.Unlock()
	if f.log != nil {
		f.log.add("sink.publish")
	}
	if blockFor > 0 {
		select {
		case <-time.After(blockFor):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if fail {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps = append(f.snaps, snap)
	return nil
}

func (f *fakeSink) setBlock(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blockFor = d
}

func (f *fakeSink) published() []model.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.Snapshot(nil), f.snaps...)
}

// stubClock scripts Now() returns so lastPollMs and freshness are
// deterministic. Calls beyond the script repeat the final time.
type stubClock struct {
	mu    sync.Mutex
	times []time.Time
	i     int
}

func (s *stubClock) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.times[s.i]
	if s.i < len(s.times)-1 {
		s.i++
	}
	return t
}

func testConfig() ports.Config {
	return ports.Config{PollInterval: time.Second}
}

func rowFixture(id string, status model.SessionStatus, total int64, cost float64) model.SessionRow {
	return model.SessionRow{
		ID:      id,
		Title:   "title-" + id,
		Project: "proj",
		Model:   "p/m",
		Status:  status,
		Tokens:  model.TokenCounts{In: total / 2, Out: total - total/2, Total: total},
		Cost:    cost,
	}
}

func newTestCollector(db *fakeDB, rt *fakeRuntime, sink *fakeSink) *Collector {
	c, err := NewCollector(db, rt, sink, testConfig())
	if err != nil {
		panic(err)
	}
	return c
}

func Test_Collector_Tick_publishes_joined_snapshot_when_watermark_moves(t *testing.T) {
	// Given
	log := &orderLog{}
	db := &fakeDB{log: log, rows: []model.SessionRow{
		rowFixture("ses-b", model.SessionIdle, 100, 1.5),
		rowFixture("ses-a", model.SessionRunning, 200, 2.5),
	}, updMs: 7, evID: 9}
	rt := &fakeRuntime{log: log}
	sink := &fakeSink{log: log}
	c := newTestCollector(db, rt, sink)

	// When
	published, err := c.Tick(context.Background())

	// Then
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !published {
		t.Fatal("expected a publish on first tick")
	}
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published snapshots = %d, want 1", len(got))
	}
	snap := got[0]
	if snap.Seq != 1 {
		t.Errorf("seq = %d, want 1", snap.Seq)
	}
	if snap.Stale {
		t.Error("fresh tick must not be stale")
	}
	if len(snap.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(snap.Sessions))
	}
	if snap.Sessions[0].ID != "ses-a" {
		t.Errorf("first session = %s, want ses-a (running sorts first)", snap.Sessions[0].ID)
	}
	for _, s := range snap.Sessions {
		if s.Pane.CWD == "" {
			t.Errorf("session %s missing runtime enrichment", s.ID)
		}
	}
	wantOrder := []string{"db.pollstate", "db.sessions", "runtime.enrich", "sink.publish"}
	if order := log.snapshot(); fmt.Sprint(order) != fmt.Sprint(wantOrder) {
		t.Errorf("join order = %v, want %v", order, wantOrder)
	}
	if snap.KPIs.Active != 1 || snap.KPIs.SSEConnected != true {
		t.Errorf("kpis = %+v, want active=1 sse=true", snap.KPIs)
	}
	if snap.KPIs.CostToday != 4.0 {
		t.Errorf("costToday = %v, want 4.0", snap.KPIs.CostToday)
	}
}

func Test_Collector_Tick_skips_publish_when_watermark_unchanged(t *testing.T) {
	// Given
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 10, 0.5)}, updMs: 3, evID: 4}
	c := newTestCollector(db, &fakeRuntime{}, &fakeSink{})
	ctx := context.Background()
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	sessCalls, wmCalls := db.sessionCalls(), db.watermarkCalls()

	// When (watermark identical)
	published, err := c.Tick(ctx)

	// Then
	if err != nil {
		t.Fatalf("second Tick: %v", err)
	}
	if published {
		t.Error("unchanged watermark must skip the publish")
	}
	if db.sessionCalls() != sessCalls {
		t.Error("skip must avoid the full Sessions fetch")
	}
	if db.watermarkCalls() != wmCalls+1 {
		t.Error("skip still costs exactly one PollState check")
	}
}

func Test_Collector_Tick_resumes_after_watermark_moves(t *testing.T) {
	// Given
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 10, 0.5)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	ctx := context.Background()
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("first Tick: %v", err)
	}
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("skipped Tick: %v", err)
	}

	// When
	db.setWatermark(1, 2)
	published, err := c.Tick(ctx)

	// Then
	if err != nil || !published {
		t.Fatalf("moved watermark must publish (published=%v err=%v)", published, err)
	}
	got := sink.published()
	if len(got) != 2 || got[1].Seq != 2 {
		t.Fatalf("published seqs = %v, want [1 2]", seqsOf(got))
	}
}

func seqsOf(snaps []model.Snapshot) []uint64 {
	out := make([]uint64, len(snaps))
	for i, s := range snaps {
		out[i] = s.Seq
	}
	return out
}

func Test_Collector_StaleFrame_marks_and_recovers(t *testing.T) {
	// Given
	dbErr := errors.New("db down")
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionRunning, 10, 1)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	ctx := context.Background()
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("good tick: %v", err)
	}

	// When: source fails on a moved watermark
	db.setWatermark(2, 1)
	db.setSessionsErr(dbErr)
	published, err := c.Tick(ctx)

	// Then: stale frame published (seq advances, last-good sessions kept)
	if err != nil {
		t.Fatalf("stale tick must not surface an error: %v", err)
	}
	if !published {
		t.Fatal("source error must still publish a stale frame")
	}
	got := sink.published()
	if len(got) != 2 {
		t.Fatalf("published = %d, want 2", len(got))
	}
	stale := got[1]
	if !stale.Stale {
		t.Error("error tick must set Stale=true")
	}
	if stale.Seq != 2 {
		t.Errorf("stale seq = %d, want 2 (no gaps)", stale.Seq)
	}
	if len(stale.Sessions) != 1 || stale.Sessions[0].ID != "ses-1" {
		t.Error("stale frame must carry last-good sessions")
	}

	// When: source heals on the next moved watermark
	db.setWatermark(3, 1)
	db.setSessionsErr(nil)
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("recovery tick: %v", err)
	}

	// Then: automatic recovery, seq still gapless
	got = sink.published()
	if len(got) != 3 || got[2].Stale {
		t.Fatalf("recovery frame must be fresh, got %+v", got[2])
	}
	if got[2].Seq != 3 {
		t.Errorf("recovery seq = %d, want 3", got[2].Seq)
	}
}

func Test_Collector_PublishError_marks_next_frame_stale(t *testing.T) {
	// Given
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 5, 0.25)}, updMs: 1, evID: 1}
	sink := &fakeSink{publishErr: errors.New("sink wedged"), failTimes: 1}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	ctx := context.Background()

	// When: publish fails (watermark must NOT advance, or the frame is lost)
	db.setWatermark(2, 1)
	if _, err := c.Tick(ctx); err == nil {
		t.Fatal("failed publish must return an error")
	}
	if n := len(sink.published()); n != 0 {
		t.Fatalf("failed publish stored %d frames, want 0", n)
	}

	// When: next tick retries the same watermark and succeeds
	published, err := c.Tick(ctx)

	// Then: retry proceeds, gap flagged stale, then heals
	if err != nil || !published {
		t.Fatalf("retry must publish (published=%v err=%v)", published, err)
	}
	got := sink.published()
	if len(got) != 1 || !got[0].Stale {
		t.Fatalf("post-failure frame must be Stale=true, got %+v", got)
	}
	db.setWatermark(3, 1)
	if _, err := c.Tick(ctx); err != nil {
		t.Fatalf("heal tick: %v", err)
	}
	got = sink.published()
	if len(got) != 2 || got[1].Stale || got[1].Seq != 2 {
		t.Fatalf("healed frame must be fresh seq=2, got %+v", got[1])
	}
}

func Test_Collector_SlowTick_marks_stale_on_freshness_budget(t *testing.T) {
	// Given: a tick whose wall time exceeds 2x the 1s test interval
	t0 := time.Now()
	clock := &stubClock{times: []time.Time{t0, t0.Add(3 * time.Second)}}
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 5, 0.1)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	c.clock = clock

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	if !got[0].Stale {
		t.Error("tick slower than 2x interval must be stale")
	}
	if got[0].LastPollMs != 3000 {
		t.Errorf("lastPollMs = %d, want 3000", got[0].LastPollMs)
	}
}

func Test_Collector_Sort_running_first_exited_last(t *testing.T) {
	// Given: every status plus an exited running pane (liveness beats status)
	rows := []model.SessionRow{
		rowFixture("arch", model.SessionArchived, 1, 0),
		rowFixture("exit-run", model.SessionRunning, 1, 0),
		rowFixture("idle", model.SessionIdle, 1, 0),
		rowFixture("err", model.SessionError, 1, 0),
		rowFixture("wait", model.SessionWaitingInput, 1, 0),
		rowFixture("run", model.SessionRunning, 1, 0),
	}
	db := &fakeDB{rows: rows, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	sess := got[0].Sessions
	// Mark exit-run exited post-enrich: rerun sort directly for determinism.
	sess[0].Pane.Exited = false
	for i := range sess {
		if sess[i].ID == "exit-run" {
			sess[i].Pane.Exited = true
		}
	}
	sortSessions(sess)
	want := []string{"run", "wait", "idle", "err", "arch", "exit-run"}
	for i, id := range want {
		if sess[i].ID != id {
			t.Fatalf("order[%d] = %s, want %s (full=%v)", i, sess[i].ID, id, idsOf(sess))
		}
	}
}

func idsOf(rows []model.SessionRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func Test_Collector_KPI_counts(t *testing.T) {
	// Given: one of each countable status
	db := &fakeDB{rows: []model.SessionRow{
		rowFixture("r1", model.SessionRunning, 60, 1),
		rowFixture("r2", model.SessionRunning, 60, 2),
		rowFixture("w1", model.SessionWaitingInput, 60, 4),
		rowFixture("e1", model.SessionError, 60, 8),
		rowFixture("i1", model.SessionIdle, 60, 16),
		rowFixture("a1", model.SessionArchived, 60, 32),
	}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then
	kpis := sink.published()[0].KPIs
	if kpis.Active != 2 {
		t.Errorf("active = %d, want 2", kpis.Active)
	}
	if kpis.WaitingInput != 1 {
		t.Errorf("waitingInput = %d, want 1", kpis.WaitingInput)
	}
	if kpis.Errors != 1 {
		t.Errorf("errors = %d, want 1", kpis.Errors)
	}
	if kpis.CostToday != 63 {
		t.Errorf("costToday = %v, want 63", kpis.CostToday)
	}
	if !kpis.SSEConnected {
		t.Error("sseConnected must be true")
	}
	if kpis.TokensPerHour < 0 {
		t.Errorf("tokensPerHour = %v, must be >= 0", kpis.TokensPerHour)
	}
}

func Test_Collector_Spark_capped_at_60_samples(t *testing.T) {
	// Given
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionRunning, 0, 0)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)
	ctx := context.Background()

	// When: 75 ticks, each moving the watermark and growing tokens
	for i := 1; i <= 75; i++ {
		db.setWatermark(int64(i), 1)
		db.setTokenTotal("ses-1", int64(i*10))
		if _, err := c.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	// Then
	got := sink.published()
	if len(got) != 75 {
		t.Fatalf("published = %d, want 75", len(got))
	}
	spark := got[74].Sessions[0].Spark
	if len(spark) != 60 {
		t.Fatalf("spark samples = %d, want cap 60", len(spark))
	}
}

func Test_Collector_SlowSink_never_stalls(t *testing.T) {
	// Given: a sink wedged 300ms against a 20ms poll loop
	cfg := ports.Config{PollInterval: 20 * time.Millisecond}
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 1, 0)}, updMs: 1, evID: 1}
	sink := &fakeSink{blockFor: 300 * time.Millisecond}
	c, err := NewCollector(db, &fakeRuntime{}, sink, cfg)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	// When
	start := time.Now()
	_, tickErr := c.Tick(context.Background())
	elapsed := time.Since(start)

	// Then: publish timeout trips long before the 300ms wedge
	if tickErr == nil {
		t.Fatal("wedged sink must surface a publish error")
	}
	if !errors.Is(tickErr, ErrPublishTimeout) {
		t.Fatalf("err = %v, want wrapped ErrPublishTimeout", tickErr)
	}
	if elapsed >= 300*time.Millisecond {
		t.Fatalf("tick stalled %v on a slow sink", elapsed)
	}

	// When: the sink heals, the next tick proceeds
	sink.setBlock(0)
	db.setWatermark(2, 1)
	published, err := c.Tick(context.Background())

	// Then
	if err != nil || !published {
		t.Fatalf("healed tick must publish (published=%v err=%v)", published, err)
	}
}

func Test_Collector_Run_stops_on_cancel(t *testing.T) {
	// Given
	db := &fakeDB{rows: []model.SessionRow{rowFixture("ses-1", model.SessionIdle, 1, 0)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c, err := NewCollector(db, &fakeRuntime{}, sink, ports.Config{PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	// When
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Then
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on cancel")
	}
	if n := len(sink.published()); n < 1 {
		t.Errorf("Run published %d frames, want >= 1", n)
	}
}

func Test_NewCollector_rejects_nil_dependencies(t *testing.T) {
	// Given
	db := &fakeDB{}
	rt := &fakeRuntime{}
	sink := &fakeSink{}

	// When/Then
	for name, args := range map[string]struct {
		db   ports.DBReader
		rt   ports.RuntimeSource
		sink ports.SnapshotSink
	}{
		"nil db":      {nil, rt, sink},
		"nil runtime": {db, nil, sink},
		"nil sink":    {db, rt, nil},
	} {
		if _, err := NewCollector(args.db, args.rt, args.sink, testConfig()); !errors.Is(err, ErrNilDependency) {
			t.Errorf("%s: err = %v, want ErrNilDependency", name, err)
		}
	}
}

func Test_Collector_Tick_gates_ancient_rows_to_idle(t *testing.T) {
	// Given: recent and ancient rows across statuses (UpdatedRelativeMs is
	// the db-owned now-minus-time_updated verdict; the 2m default window
	// applies since testConfig leaves ActiveWindow unset)
	ancientMs := int64((time.Hour).Milliseconds())
	rows := []model.SessionRow{
		rowFixture("run-recent", model.SessionRunning, 1, 0),
		rowFixture("run-ancient", model.SessionRunning, 1, 0),
		rowFixture("wait-ancient", model.SessionWaitingInput, 1, 0),
		rowFixture("err-ancient", model.SessionError, 1, 0),
		rowFixture("arch-ancient", model.SessionArchived, 1, 0),
	}
	rows[0].UpdatedRelativeMs = 1_000
	for i := 1; i < len(rows); i++ {
		rows[i].UpdatedRelativeMs = ancientMs
	}
	db := &fakeDB{rows: rows, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then: only the recent running row survives; archived still wins
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	want := map[string]model.SessionStatus{
		"run-recent":   model.SessionRunning,
		"run-ancient":  model.SessionIdle,
		"wait-ancient": model.SessionIdle,
		"err-ancient":  model.SessionIdle,
		"arch-ancient": model.SessionArchived,
	}
	for _, s := range got[0].Sessions {
		if w, ok := want[s.ID]; ok && s.Status != w {
			t.Errorf("%s status = %q, want %q", s.ID, s.Status, w)
		}
	}
	if got[0].KPIs.Active != 1 {
		t.Errorf("kpis.active = %d, want 1", got[0].KPIs.Active)
	}
}

func Test_Collector_KPI_reports_live_ground_truth(t *testing.T) {
	// Given: a runtime source observing 3 live panes and 2 live procs
	db := &fakeDB{rows: []model.SessionRow{rowFixture("r1", model.SessionRunning, 60, 1)}, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{panes: 3, procs: 2}, sink)

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then: ground-truth counts flow into the KPI strip additively
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	if got[0].KPIs.LivePanes != 3 || got[0].KPIs.LiveProcesses != 2 {
		t.Errorf("kpis live = (%d,%d), want (3,2)",
			got[0].KPIs.LivePanes, got[0].KPIs.LiveProcesses)
	}
	if got[0].KPIs.Active != 1 {
		t.Errorf("kpis.active = %d, want 1 (existing fields unchanged)", got[0].KPIs.Active)
	}
}

func Test_Collector_Status_canonicalized_through_derive(t *testing.T) {
	// Given: all five contract statuses cross the seam
	statuses := []model.SessionStatus{
		model.SessionRunning, model.SessionIdle, model.SessionWaitingInput,
		model.SessionError, model.SessionArchived,
	}
	var rows []model.SessionRow
	for i, s := range statuses {
		rows = append(rows, rowFixture(fmt.Sprintf("ses-%d", i), s, 1, 0))
	}
	db := &fakeDB{rows: rows, updMs: 1, evID: 1}
	sink := &fakeSink{}
	c := newTestCollector(db, &fakeRuntime{}, sink)

	// When
	if _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then: every status survives the DeriveStatus round-trip and stays valid
	seen := map[model.SessionStatus]bool{}
	for _, s := range sink.published()[0].Sessions {
		if !s.Status.IsValid() {
			t.Errorf("session %s has invalid status %q", s.ID, s.Status)
		}
		seen[s.Status] = true
	}
	for _, s := range statuses {
		if !seen[s] {
			t.Errorf("status %q lost in join", s)
		}
	}
}

// Given: a config without an explicit DBTimeout.
// When: the collector is built.
// Then: the 10s default applies (headroom past the old 5s constant that
// starved every tick on the 8.2GB live db).
func Test_Collector_DBTimeout_defaults_when_unset(t *testing.T) {
	c, err := NewCollector(&fakeDB{}, &fakeRuntime{}, &fakeSink{}, testConfig())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	if c.dbTimeout != 10*time.Second {
		t.Errorf("dbTimeout = %v, want 10s default", c.dbTimeout)
	}
}

// deadlineProbeDB blocks until the caller's context expires and records the
// budget it observed, proving the configured knob reaches the DB calls.
type deadlineProbeDB struct {
	mu       sync.Mutex
	observed []time.Duration
}

func (d *deadlineProbeDB) PollState(context.Context) (int64, int64, error) {
	return 0, 0, nil
}

func (d *deadlineProbeDB) Sessions(ctx context.Context) ([]model.SessionRow, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.mu.Lock()
		d.observed = append(d.observed, time.Until(dl))
		d.mu.Unlock()
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (d *deadlineProbeDB) SessionDetail(_ context.Context, id string) (model.SessionDetail, error) {
	return model.SessionDetail{}, fmt.Errorf("deadlineProbeDB: no detail for %s", id)
}

// Given: DBTimeout set to 60ms with a DB that never answers.
// When: a tick runs.
// Then: the Sessions call carries a sub-second deadline (the knob, not the
// old 5s constant) and the tick degrades to a stale frame instead of
// stalling the loop.
func Test_Collector_DBTimeout_flows_to_db_calls(t *testing.T) {
	// Given
	db := &deadlineProbeDB{}
	sink := &fakeSink{}
	cfg := testConfig()
	cfg.DBTimeout = 60 * time.Millisecond
	c, err := NewCollector(db, &fakeRuntime{}, sink, cfg)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	// When
	published, _ := c.Tick(context.Background())

	// Then
	if !published {
		t.Error("tick with expired DB budget must still publish a stale frame")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(db.observed) != 1 {
		t.Fatalf("Sessions calls = %d, want 1", len(db.observed))
	}
	if db.observed[0] >= time.Second {
		t.Errorf("Sessions budget = %v, want the configured ~60ms (old 5s constant?)", db.observed[0])
	}
	got := sink.published()
	if len(got) != 1 || !got[0].Stale {
		t.Errorf("published %d frames, want exactly 1 stale frame", len(got))
	}
}
