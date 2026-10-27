package runtime

import (
	"context"
	"reflect"
	"testing"

	"opencode-dashboard/internal/model"
)

// paneFallbackJSON mixes the shapes Enrich must handle: two opencode panes in
// one directory (one with a trailing slash), an opencode pane elsewhere, an
// exited opencode pane, a shell pane, and a CWD-less plugin pane.
const paneFallbackJSON = `[
  {"id": 1, "terminal_command": "/usr/local/bin/opencode", "pane_command": "opencode", "pane_cwd": "/proj/alpha", "exited": false},
  {"id": 2, "terminal_command": null, "pane_command": "opencode", "pane_cwd": "/proj/alpha/", "exited": false},
  {"id": 3, "terminal_command": "opencode", "pane_command": "opencode", "pane_cwd": "/proj/gamma", "exited": false},
  {"id": 4, "terminal_command": "opencode", "pane_command": "opencode", "pane_cwd": "/proj/beta", "exited": true},
  {"id": 5, "terminal_command": "zsh", "pane_command": "zsh", "pane_cwd": "/proj/shell", "exited": false},
  {"id": 6, "terminal_command": null, "pane_command": null, "pane_cwd": null, "exited": false}
]`

// Given: two alive opencode processes in /proj/alpha, one zombie there, one
// alive in /proj/beta, and one alive process with an unreadable CWD.
// When: Enrich runs the /proc scan.
// Then: ProcsByDir counts only live processes per cleaned directory and
// omits the unattributable one.
func Test_Enrich_ProcsByDir_counts_live_only(t *testing.T) {
	// Given
	f := &fakeZellij{sessionsOut: "\n", panes: map[string]string{}}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 101, "opencode", "R (running)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 102, "opencode", "Z (zombie)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 200, "opencode", "S (sleeping)", 1, "/proj/beta")
		writeFixtureProc(t, root, 300, "opencode", "S (sleeping)", 1, "")
	})

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	got := src.ProcsByDir()
	if len(got) != 2 || got["/proj/alpha"] != 2 || got["/proj/beta"] != 1 {
		t.Fatalf("ProcsByDir = %v, want alpha=2 beta=1", got)
	}
}

// Given: live opencode processes whose /proc cwd is unreadable (the
// in-container EACCES case), plus zellij panes that do carry CWD and command.
// When: Enrich resolves per-directory attribution.
// Then: ProcsByDir falls back to opencode panes, counting non-exited panes
// only, ignoring shell and CWD-less panes, and leaving the global process
// count as the real /proc count.
func Test_Enrich_ProcsByDir_falls_back_to_pane_presence_when_proc_cwd_unreadable(t *testing.T) {
	// Given
	f := &fakeZellij{
		sessionsOut: "work\n",
		panes:       map[string]string{"work": paneFallbackJSON},
	}
	src := testSource(t, f, []string{"work"}, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 1, "")
		writeFixtureProc(t, root, 101, "opencode", "S (sleeping)", 1, "")
	})

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	want := map[string]int{"/proj/alpha": 2, "/proj/gamma": 1}
	if got := src.ProcsByDir(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ProcsByDir = %v, want pane fallback %v", got, want)
	}
	if got := src.PanesByDir(); !reflect.DeepEqual(got, want) {
		t.Fatalf("PanesByDir = %v, want %v", got, want)
	}
	livePanes, liveProcs := src.LiveCounts()
	if liveProcs != 2 {
		t.Errorf("LiveCounts procs = %d, want the real /proc count 2", liveProcs)
	}
	// Agent panes only: the live zsh shell pane is excluded so the KPI
	// reconciles with the per-project pane columns.
	if livePanes != 3 {
		t.Errorf("LiveCounts panes = %d, want 3 live agent panes (shell excluded)", livePanes)
	}
}

// Given: one opencode proc with a readable CWD and one without, plus panes
// that would attribute more directories.
// When: Enrich resolves attribution.
// Then: the non-empty proc-derived map wins over pane presence, while
// PanesByDir still reports the full pane-derived signal.
func Test_Enrich_ProcsByDir_prefers_proc_attribution_when_non_empty(t *testing.T) {
	// Given
	f := &fakeZellij{
		sessionsOut: "work\n",
		panes:       map[string]string{"work": paneFallbackJSON},
	}
	src := testSource(t, f, []string{"work"}, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 1, "/proj/alpha")
		writeFixtureProc(t, root, 101, "opencode", "S (sleeping)", 1, "")
	})

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got, want := src.ProcsByDir(), map[string]int{"/proj/alpha": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ProcsByDir = %v, want proc-derived %v", got, want)
	}
	if got, want := src.PanesByDir(), map[string]int{"/proj/alpha": 2, "/proj/gamma": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PanesByDir = %v, want %v", got, want)
	}
}

// Given: a live opencode process with an unreadable CWD and a broken pane
// listing.
// When: Enrich runs.
// Then: no presence is fabricated: both per-directory maps stay empty while
// the real process count is still reported.
func Test_Enrich_ProcsByDir_does_not_fabricate_when_pane_listing_fails(t *testing.T) {
	// Given
	f := &fakeZellij{sessionsErr: errBoom()}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 100, "opencode", "S (sleeping)", 1, "")
	})

	// When
	_, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-1", "/proj/alpha")})

	// Then
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got := src.ProcsByDir(); len(got) != 0 {
		t.Fatalf("ProcsByDir = %v, want empty (no attribution source)", got)
	}
	if got := src.PanesByDir(); len(got) != 0 {
		t.Fatalf("PanesByDir = %v, want empty (listing failed)", got)
	}
	if _, liveProcs := src.LiveCounts(); liveProcs != 1 {
		t.Errorf("LiveCounts procs = %d, want 1", liveProcs)
	}
}
