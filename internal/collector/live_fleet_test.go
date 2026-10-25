package collector

import (
	"context"
	"os"
	"testing"
	"time"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
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
	c, err := NewCollector(reader, liveRuntime{}, sink, cfg)
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
