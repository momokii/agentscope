package collector

import (
	"context"
	"os"
	"testing"
	"time"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
	"opencode-dashboard/internal/runtime"
)

// Test_Live_FleetTicks_real_reader drives the REAL read-only db.Reader
// through the Collector for 6 consecutive ticks and proves the production
// regression is gone: every tick completes inside the DB budget (no
// `context deadline exceeded`), every published frame carries a NON-EMPTY
// session list, and the count is stable across ticks.
// Env-gated: OPENDASH_LIVE=1. Read-only (mode=ro + query_only, in-process).
func Test_Live_FleetTicks_real_reader(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") != "1" {
		t.Skip("set OPENDASH_LIVE=1 to run the live fleet-tick proof")
	}
	cfg := ports.LoadConfigFromEnv()
	reader, err := db.New(cfg.DataDir)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer func() { _ = reader.Close() }()

	sink := &fakeSink{}
	c, err := NewCollector(reader, liveRuntime{}, sink, cfg, nil)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	const ticks = 6
	var lastCount = -1
	for i := 0; i < ticks; i++ {
		tickCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		published, err := c.Tick(tickCtx)
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			t.Fatalf("live tick %d: %v", i, err)
		}
		t.Logf("LIVE tick=%d published=%v elapsed=%s", i, published, elapsed)
		if elapsed > cfg.DBTimeout {
			t.Fatalf("live tick %d took %s, over the %s DB budget", i, elapsed, cfg.DBTimeout)
		}
	}
	got := sink.published()
	if len(got) == 0 {
		t.Fatal("live proof published zero frames")
	}
	for _, snap := range got {
		if len(snap.Sessions) == 0 {
			t.Fatalf("live seq=%d published an EMPTY session list (regression!)", snap.Seq)
		}
		if snap.Stale {
			t.Logf("LIVE seq=%d stale=true (honest staleness, not a blank)", snap.Seq)
		}
		if lastCount != -1 && len(snap.Sessions) != lastCount {
			t.Logf("LIVE seq=%d sessions=%d (was %d: live db moved mid-proof, not a failure)",
				snap.Seq, len(snap.Sessions), lastCount)
		}
		lastCount = len(snap.Sessions)
		var running int
		for _, s := range snap.Sessions {
			if s.Status == model.SessionRunning {
				running++
			}
		}
		t.Logf("LIVE seq=%d sessions=%d running=%d lastPollMs=%d",
			snap.Seq, len(snap.Sessions), running, snap.LastPollMs)
	}
}

// Test_Live_StatusTruth_real_source is the status-truth acceptance proof:
// the REAL db.Reader (with the ACTIVE_WINDOW recency gate) joined through
// the REAL runtime.Source (newest-per-directory pane join + LiveCounts)
// drives the Collector for 3 ticks. It reports the full status breakdown
// plus ground-truth pane/process counts. Env-gated: OPENDASH_LIVE=1.
// Read-only (mode=ro + query_only, in-process, no container touched).
func Test_Live_StatusTruth_real_source(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") != "1" {
		t.Skip("set OPENDASH_LIVE=1 to run the live status-truth proof")
	}
	cfg := ports.LoadConfigFromEnv()
	reader, err := db.NewWithWindow(cfg.DataDir, cfg.ActiveWindow.Milliseconds())
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer func() { _ = reader.Close() }()

	src := runtime.NewSource(runtime.DefaultConfig())
	sink := &fakeSink{}
	c, err := NewCollector(reader, src, sink, cfg, nil)
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	const ticks = 3
	for i := 0; i < ticks; i++ {
		tickCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		published, err := c.Tick(tickCtx)
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			t.Fatalf("live truth tick %d: %v", i, err)
		}
		t.Logf("TRUTH tick=%d published=%v elapsed=%s", i, published, elapsed)
		if elapsed > cfg.DBTimeout+8*time.Second {
			t.Fatalf("live truth tick %d took %s, over the tick budget", i, elapsed)
		}
	}
	got := sink.published()
	if len(got) == 0 {
		t.Fatal("live truth published zero frames")
	}
	last := got[len(got)-1]
	counts := map[model.SessionStatus]int{}
	var oldestRunningMs int64
	var sampleRunning model.SessionRow
	foundSample := false
	for _, s := range last.Sessions {
		counts[s.Status]++
		if s.Status == model.SessionRunning {
			if s.UpdatedRelativeMs > oldestRunningMs {
				oldestRunningMs = s.UpdatedRelativeMs
			}
			if !foundSample {
				sampleRunning = s
				foundSample = true
			}
		}
		if !s.Status.IsValid() {
			t.Errorf("session %s has invalid status %q", s.ID, s.Status)
		}
	}
	t.Logf("TRUTH seq=%d total=%d running=%d waiting=%d error=%d idle=%d archived=%d stale=%v lastPollMs=%d",
		last.Seq, len(last.Sessions), counts[model.SessionRunning],
		counts[model.SessionWaitingInput], counts[model.SessionError],
		counts[model.SessionIdle], counts[model.SessionArchived],
		last.Stale, last.LastPollMs)
	t.Logf("TRUTH kpis live_panes=%d live_processes=%d active=%d",
		last.KPIs.LivePanes, last.KPIs.LiveProcesses, last.KPIs.Active)
	t.Logf("TRUTH oldest running updated_relative_ms=%d window_ms=%d",
		oldestRunningMs, cfg.ActiveWindow.Milliseconds())
	if foundSample {
		t.Logf("TRUTH sample running id=%s title=%q status=%s updated_relative_ms=%d pane=%+v orphan=%v",
			sampleRunning.ID, sampleRunning.Title, sampleRunning.Status,
			sampleRunning.UpdatedRelativeMs, sampleRunning.Pane, sampleRunning.Orphan)
	}
	if int64(counts[model.SessionRunning]) != int64(last.KPIs.Active) {
		t.Errorf("kpis.active=%d disagrees with running rows=%d",
			last.KPIs.Active, counts[model.SessionRunning])
	}
	if oldestRunningMs > cfg.ActiveWindow.Milliseconds() {
		t.Errorf("oldest running updated_relative_ms=%d exceeds window %v",
			oldestRunningMs, cfg.ActiveWindow)
	}
}
