package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/ports"
)

// Test_Sessions_live_budget is the F1 timing probe: full Sessions() against
// the REAL ~/.local/share/opencode/opencode.db read-only must complete each
// iteration well under the 10s fleet budget (15s poll bound). Env-gated:
// OPENDASH_LIVE=1 go test ./internal/db/ -run Test_Sessions_live_budget -v.
// Normal `go test ./...` never touches the live database.
func Test_Sessions_live_budget(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") == "" {
		t.Skip("OPENDASH_LIVE unset: live budget probe stays off the real db")
	}
	cfg := ports.LoadConfigFromEnv()
	r, err := New(cfg.DataDir)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer func() { _ = r.Close() }()

	const iterations = 3
	const budget = 10 * time.Second
	for i := 0; i < iterations; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		start := time.Now()
		rows, err := r.Sessions(ctx)
		elapsed := time.Since(start)
		cancel()
		if err != nil {
			t.Fatalf("live Sessions iter %d: %v", i, err)
		}
		if len(rows) == 0 {
			t.Fatalf("live Sessions iter %d returned zero sessions", i)
		}
		t.Logf("PROBE iter=%d sessions=%d elapsed=%s", i, len(rows), elapsed)
		if elapsed > budget {
			t.Fatalf("live Sessions iter %d took %s, over %s budget", i, elapsed, budget)
		}
	}
}

// Test_SessionDetail_live_probe prints counts-only SessionDetail rows for the
// F1 export-vs-seam diff: OPENDASH_SEAM_SES=ses_a,ses_b selects sessions,
// `opencode export <ses> --sanitize` is the other side. Env-gated and
// counts-only: no transcript text ever reaches the log.
func Test_SessionDetail_live_probe(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") == "" || os.Getenv("OPENDASH_SEAM_SES") == "" {
		t.Skip("OPENDASH_LIVE and OPENDASH_SEAM_SES unset: seam probe stays off the real db")
	}
	cfg := ports.LoadConfigFromEnv()
	r, err := New(cfg.DataDir)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	for _, id := range strings.Split(os.Getenv("OPENDASH_SEAM_SES"), ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		d, err := r.SessionDetail(ctx, id)
		if err != nil {
			t.Fatalf("live SessionDetail %s: %v", id, err)
		}
		t.Logf("SEAM id=%s messages=%d tools=%d todos=%d children=%d in=%d out=%d total=%d cost=%v status=%s",
			id, len(d.Messages), len(d.Tools), len(d.Todos), len(d.Children),
			d.Session.Tokens.In, d.Session.Tokens.Out, d.Session.Tokens.Total,
			d.Session.Cost, d.Session.Status)
	}
}
