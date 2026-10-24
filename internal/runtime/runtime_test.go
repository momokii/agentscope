package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
)

// fakeZellij serves canned CLI output and counts list-panes calls, proving
// the one-query-per-live-session budget.
type fakeZellij struct {
	sessionsOut string
	sessionsErr error
	panes       map[string]string
	panesErr    map[string]error
	block       bool
	paneCalls   []string
}

func (f *fakeZellij) run(ctx context.Context, bin string, args []string) ([]byte, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(args) == 2 && args[0] == "list-sessions" {
		return []byte(f.sessionsOut), f.sessionsErr
	}
	if len(args) == 5 && args[0] == "--session" {
		f.paneCalls = append(f.paneCalls, args[1])
		if err, ok := f.panesErr[args[1]]; ok {
			return nil, err
		}
		return []byte(f.panes[args[1]]), nil
	}
	return []byte("[]"), nil
}

func row(id, dir string) model.SessionRow {
	return model.SessionRow{ID: id, Directory: dir, Status: model.SessionIdle}
}

func errBoom() error {
	return errors.New("boom: zellij exploded")
}

// testSource wires a fake zellij CLI, a fake socket dir liveness set, and a
// fixture /proc tree. Socket liveness is injected by pre-creating entries in
// a temp dir used as SocketDir.
func testSource(t *testing.T, f *fakeZellij, liveSockets []string, procSetup func(root string)) *Source {
	t.Helper()
	sockDir := t.TempDir()
	for _, name := range liveSockets {
		if _, err := createSocketFile(sockDir, name); err != nil {
			t.Fatal(err)
		}
	}
	procRoot := t.TempDir()
	if procSetup != nil {
		procSetup(procRoot)
	}
	return NewSourceWithRunner(Config{
		SocketDir: sockDir,
		ProcRoot:  procRoot,
		Timeout:   2 * time.Second,
	}, f.run)
}

func Test_Enrich_pane_match_sets_cwd_and_live(t *testing.T) {
	// Given: live session with an opencode pane at /proj/alpha, backed by a
	// sleeping opencode proc.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSource(t, f, []string{"main"}, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 10, "/proj/alpha")
	})

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got[0].Pane.CWD != "/proj/alpha" {
		t.Errorf("Pane.CWD = %q, want /proj/alpha", got[0].Pane.CWD)
	}
	if got[0].Pane.Exited {
		t.Errorf("live pane must not read exited: %+v", got[0].Pane)
	}
	if got[0].Orphan {
		t.Errorf("pane-backed session must not read orphan")
	}
	if got[0].Directory != "/proj/alpha" {
		t.Errorf("Directory must never be overwritten: %q", got[0].Directory)
	}
}

func Test_Enrich_exited_pane_in_live_session_shows_dead(t *testing.T) {
	// Given: live session whose only pane at /proj/beta is exited.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-2", "/proj/beta")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !got[0].Pane.Exited {
		t.Errorf("exited:true pane must show dead: %+v", got[0].Pane)
	}
	if got[0].Pane.CWD != "/proj/beta" {
		t.Errorf("Pane.CWD = %q, want /proj/beta", got[0].Pane.CWD)
	}
}

func Test_Enrich_zombie_backing_proc_shows_dead(t *testing.T) {
	// Given: pane not exited, but its only backing opencode proc is a zombie.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePanesJSON},
	}
	src := testSource(t, f, []string{"main"}, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "Z (zombie)", 10, "/proj/alpha")
	})

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !got[0].Pane.Exited {
		t.Errorf("zombie-backed pane must show dead: %+v", got[0].Pane)
	}
}

func Test_Enrich_orphan_flagged_with_empty_cwd(t *testing.T) {
	// Given: no zellij sessions at all, but an opencode proc lives at the
	// session directory (opencode outside zellij).
	f := &fakeZellij{sessionsOut: "\n", panes: map[string]string{}}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 200, "opencode", "S (sleeping)", 1, "/proj/orphan")
	})

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-9", "/proj/orphan")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !got[0].Orphan {
		t.Errorf("proc-backed session with no pane must read orphan")
	}
	if got[0].Pane.CWD != "" {
		t.Errorf("orphan Pane.CWD = %q, want empty (dump-screen unavailable)", got[0].Pane.CWD)
	}
	if !got[0].Pane.Exited {
		t.Errorf("orphan must report Exited per model contract")
	}
}

func Test_Enrich_mismatch_preserves_both_values(t *testing.T) {
	// Given: pane CWD "/proj/alpha/" (trailing slash, as zellij may report)
	// matches Directory "/proj/alpha" after Clean.
	panesJSON := `[{"id": 1, "title": "OC | x", "terminal_command": "opencode",
		"pane_command": "opencode", "pane_cwd": "/proj/alpha/",
		"exited": false, "exit_status": null, "tab_id": 0, "tab_name": "Tab #1"}]`
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": panesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got[0].Pane.CWD != "/proj/alpha/" {
		t.Errorf("Pane.CWD must keep the raw live value, got %q", got[0].Pane.CWD)
	}
	if got[0].Directory != "/proj/alpha" {
		t.Errorf("Directory must never be overwritten, got %q", got[0].Directory)
	}
}

func Test_Enrich_unmatched_row_passes_through(t *testing.T) {
	// Given: pane at /proj/other attaches to nothing; row keeps DB values.
	panesJSON := `[{"id": 1, "title": "shell", "terminal_command": "zsh",
		"pane_command": "zsh", "pane_cwd": "/proj/other",
		"exited": false, "exit_status": null, "tab_id": 0, "tab_name": "Tab #1"}]`
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": panesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/mine")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got[0].Orphan || got[0].Pane.CWD != "" || got[0].Pane.Exited {
		t.Errorf("unmatched row must stay DB-only: %+v", got[0])
	}
	if got[0].Directory != "/proj/mine" {
		t.Errorf("Directory must never be overwritten, got %q", got[0].Directory)
	}
}

func Test_Enrich_zellij_failure_degrades_to_db_rows(t *testing.T) {
	// Given: zellij CLI exploding on every call.
	f := &fakeZellij{sessionsErr: errBoom(), sessionsOut: ""}
	src := testSource(t, f, nil, nil)
	rows := []model.SessionRow{row("ses-1", "/proj/alpha")}

	// When
	got, err := src.Enrich(context.Background(), rows)

	// Then: graceful degradation, never a crash.
	if err != nil {
		t.Fatalf("degraded Enrich must return nil error, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "ses-1" || got[0].Orphan || got[0].Pane.CWD != "" {
		t.Errorf("rows must pass through untouched: %+v", got)
	}
}

func Test_Enrich_zellij_timeout_degrades_gracefully(t *testing.T) {
	// Given: a wedged zellij server (runner blocks until ctx deadline).
	f := &fakeZellij{block: true}
	src := testSource(t, f, nil, nil)
	src.timeout = 50 * time.Millisecond
	src.zellij.cfg.Timeout = 50 * time.Millisecond

	// When
	start := time.Now()
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("timed-out Enrich must return nil error, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("Enrich hung past the timeout budget")
	}
	if len(got) != 1 || got[0].Pane.CWD != "" || got[0].Orphan {
		t.Errorf("rows must pass through untouched: %+v", got)
	}
}

func Test_Enrich_queries_live_sessions_only_once_each(t *testing.T) {
	// Given: two live sessions, one EXITED, one without a socket entry.
	f := &fakeZellij{
		sessionsOut: "live-a\nlive-b\ndead-c (EXITED - attach to resurrect)\nghost-d\n",
		panes: map[string]string{
			"live-a": `[]`,
			"live-b": `[]`,
		},
	}
	src := testSource(t, f, []string{"live-a", "live-b"}, nil)

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("s", "/x")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(f.paneCalls) != 2 {
		t.Fatalf("got list-panes calls %v, want exactly one per live session", f.paneCalls)
	}
	seen := map[string]bool{}
	for _, name := range f.paneCalls {
		seen[name] = true
	}
	if !seen["live-a"] || !seen["live-b"] {
		t.Errorf("must query each live session once: %v", f.paneCalls)
	}
}

func Test_Enrich_preserves_row_order(t *testing.T) {
	// Given
	f := &fakeZellij{sessionsOut: "\n", panes: map[string]string{}}
	src := testSource(t, f, nil, nil)
	rows := []model.SessionRow{row("c", "/3"), row("a", "/1"), row("b", "/2")}

	// When
	got, err := src.Enrich(context.Background(), rows)

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	for i, want := range []string{"c", "a", "b"} {
		if got[i].ID != want {
			t.Errorf("row %d = %q, want %q", i, got[i].ID, want)
		}
	}
}
