package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"

	"opencode-dashboard/internal/model"
)

// warnTap records degradation lines so tests can assert warn-once behavior.
type warnTap struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnTap) fn() func(string) {
	return func(msg string) {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.msgs = append(w.msgs, msg)
	}
}

func (w *warnTap) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.msgs)
}

func (w *warnTap) joined() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.msgs, "\n")
}

func tapSource(t *testing.T, src *Source) *warnTap {
	t.Helper()
	tap := &warnTap{}
	src.warn = tap.fn()
	return tap
}

func Test_Enrich_pane_failure_still_counts_processes(t *testing.T) {
	// Given: zellij CLI exploding, but a live opencode proc at the row dir.
	f := &fakeZellij{sessionsErr: errBoom(), sessionsOut: ""}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 10, "/proj/alpha")
	})
	tap := tapSource(t, src)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then: graceful degradation, but the surviving source still counts.
	if err != nil {
		t.Fatalf("degraded Enrich must return nil error, got %v", err)
	}
	if panes, procs := src.LiveCounts(); panes != 0 || procs != 1 {
		t.Errorf("LiveCounts = (%d,%d), want (0,1)", panes, procs)
	}
	if !got[0].Orphan || got[0].Pane.CWD != "" {
		t.Errorf("proc-backed row with no pane must read orphan: %+v", got[0])
	}
	if got := tap.count(); got != 1 {
		t.Errorf("want exactly 1 pane-failure warning, got %d: %q", got, tap.joined())
	}
	if msg := tap.joined(); !strings.Contains(msg, "pane listing") {
		t.Errorf("warning must name the pane source, got %q", msg)
	}
}

func Test_Enrich_proc_failure_still_counts_panes(t *testing.T) {
	// Given: healthy zellij panes, but an unreadable /proc root.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)
	src.procRoot = "/nonexistent-proc-root-xyz"
	tap := tapSource(t, src)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then: pane claim survives the dead proc scanner.
	if err != nil {
		t.Fatalf("degraded Enrich must return nil error, got %v", err)
	}
	if got[0].Pane.CWD != "/proj/alpha" {
		t.Errorf("Pane.CWD = %q, want /proj/alpha", got[0].Pane.CWD)
	}
	if panes, procs := src.LiveCounts(); panes != 2 || procs != 0 {
		t.Errorf("LiveCounts = (%d,%d), want (2,0)", panes, procs)
	}
	if got := tap.count(); got != 1 {
		t.Errorf("want exactly 1 proc-failure warning, got %d: %q", got, tap.joined())
	}
	if msg := tap.joined(); !strings.Contains(msg, "process scan") {
		t.Errorf("warning must name the process source, got %q", msg)
	}
}

func Test_Enrich_both_sources_fail_yields_zeros(t *testing.T) {
	// Given: zellij CLI exploding AND an unreadable /proc root.
	f := &fakeZellij{sessionsErr: errBoom(), sessionsOut: ""}
	src := testSource(t, f, nil, nil)
	src.procRoot = "/nonexistent-proc-root-xyz"
	tap := tapSource(t, src)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then: rows pass through untouched, counts stay zero, nil error.
	if err != nil {
		t.Fatalf("degraded Enrich must return nil error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "ses-1" || got[0].Orphan || got[0].Pane.CWD != "" {
		t.Errorf("rows must pass through untouched: %+v", got)
	}
	if panes, procs := src.LiveCounts(); panes != 0 || procs != 0 {
		t.Errorf("LiveCounts = (%d,%d), want (0,0)", panes, procs)
	}
	if got := tap.count(); got != 2 {
		t.Errorf("want exactly 2 warnings (one per source), got %d: %q", got, tap.joined())
	}
}

func Test_Enrich_degradation_warns_once(t *testing.T) {
	// Given: a permanently failing pane source.
	f := &fakeZellij{sessionsErr: errBoom(), sessionsOut: ""}
	src := testSource(t, f, nil, nil)
	tap := tapSource(t, src)
	rows := []model.SessionRow{row("ses-1", "/proj/alpha")}

	// When: sequential ticks plus concurrent ticks (collector may overlap).
	if _, err := src.Enrich(context.Background(), rows); err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = src.Enrich(context.Background(), rows)
		}()
	}
	wg.Wait()
	if _, err := src.Enrich(context.Background(), rows); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	// Then: exactly one line for the pane source, however many ticks ran.
	if got := tap.count(); got != 1 {
		t.Errorf("want exactly 1 warning across all ticks, got %d: %q", got, tap.joined())
	}
	if msg := tap.joined(); !strings.Contains(msg, "XDG_RUNTIME_DIR") || !strings.Contains(msg, "pid:host") {
		t.Errorf("warning must carry the remediation hint, got %q", msg)
	}
}
