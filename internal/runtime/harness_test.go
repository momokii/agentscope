package runtime

import (
	"context"
	"testing"

	"opencode-dashboard/internal/model"
)

// fixtureMixedPanes serves one directory hosting BOTH agents: an exited
// opencode pane and a live pi pane at the same CWD. Claims must route per
// harness — the pi row must never inherit the dead opencode pane.
const fixtureMixedPanes = `[
  {"id": 1, "title": "OC | old",
   "terminal_command": "opencode", "pane_command": "opencode",
   "pane_cwd": "/proj/mix", "exited": true, "exit_status": 1,
   "tab_id": 0, "tab_name": "Tab #1"},
  {"id": 2, "title": "⠙ π - mix",
   "terminal_command": null, "pane_command": "pi",
   "pane_cwd": "/proj/mix", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"}
]`

// fixturePiOnlyPanes serves a live pi pane: an opencode row must not claim
// it, even as the only pane at its directory.
const fixturePiOnlyPanes = `[
  {"id": 5, "title": "π - solo",
   "terminal_command": null, "pane_command": "pi",
   "pane_cwd": "/proj/solo", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"}
]`

func piRow(id, dir string) model.SessionRow {
	return model.SessionRow{ID: id, Directory: dir, Status: model.SessionIdle, Harness: model.HarnessPi}
}

func Test_Pane_Harness_prefers_foreground_command(t *testing.T) {
	cases := []struct {
		name     string
		terminal string
		foreg    string
		want     model.Harness
	}{
		{"opencode pane", "opencode", "opencode", model.HarnessOpencode},
		{"pi pane launched from shell", "", "pi", model.HarnessPi},
		{"foreground wins over launcher", "opencode", "pi", model.HarnessPi},
		{"terminal fallback when foreground absent", "opencode", "", model.HarnessOpencode},
		{"shell pane is unknown", "zsh", "zsh", ""},
		{"make pane is unknown", "make", "make", ""},
		{"empty pane is unknown", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Command: firstNonEmpty(tc.terminal, tc.foreg), PaneCommand: tc.foreg}
			if got := p.Harness(); got != tc.want {
				t.Errorf("Harness() = %q, want %q", got, tc.want)
			}
		})
	}
}

func Test_ScanAgents_matches_both_harnesses(t *testing.T) {
	root := t.TempDir()
	writeFixtureProc(t, root, 101, "opencode", "S (sleeping)", 10, "/proj/a")
	writeFixtureProc(t, root, 102, "pi", "S (sleeping)", 10, "/proj/b")
	writeFixtureProc(t, root, 103, "zsh", "S (sleeping)", 10, "/proj/c")
	writeFixtureProc(t, root, 104, "/usr/local/bin/pi", "S (sleeping)", 10, "/proj/d")

	got, err := ScanAgents(root)
	if err != nil {
		t.Fatalf("ScanAgents: %v", err)
	}
	byPID := map[int]ProcInfo{}
	for _, pr := range got {
		byPID[pr.PID] = pr
	}
	if len(got) != 3 {
		t.Fatalf("ScanAgents found %d procs, want 3 (opencode + pi x2, no zsh)", len(got))
	}
	if byPID[101].Command != "opencode" || byPID[102].Command != "pi" {
		t.Errorf("Command not populated: %+v", byPID)
	}
	if byPID[104].Command != "pi" {
		t.Errorf("argv0 path basename must resolve: %+v", byPID[104])
	}
	// ScanOpencode keeps its narrow contract.
	oc, err := ScanOpencode(root)
	if err != nil {
		t.Fatalf("ScanOpencode: %v", err)
	}
	if len(oc) != 1 || oc[0].PID != 101 {
		t.Fatalf("ScanOpencode = %+v, want only pid 101", oc)
	}
}

func Test_Enrich_mixed_directory_claims_per_harness(t *testing.T) {
	// Given: /proj/mix hosts a dead opencode pane and a live pi pane.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixtureMixedPanes},
	}
	src := testSource(t, f, []string{"main"}, func(root string) {
		writeFixtureProc(t, root, 300, "pi", "S (sleeping)", 10, "/proj/mix")
	})

	// When
	rows := []model.SessionRow{
		rowAge("ses-oc", "/proj/mix", 50_000),
		piRow("ses-pi", "/proj/mix"),
	}
	rows[1].UpdatedRelativeMs = 10_000
	got, err := src.Enrich(context.Background(), rows)

	// Then: opencode row inherits its dead pane; pi row claims the live one.
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !got[0].Pane.Exited || got[0].Pane.CWD != "/proj/mix" {
		t.Errorf("opencode row must claim its exited pane: %+v", got[0].Pane)
	}
	if got[0].Orphan {
		t.Errorf("opencode row with pane must not read orphan")
	}
	if got[1].Pane.Exited || got[1].Pane.CWD != "/proj/mix" {
		t.Errorf("pi row must claim the live pi pane: %+v", got[1].Pane)
	}
	if got[1].Orphan {
		t.Errorf("pi row with pane must not read orphan")
	}
}

func Test_Enrich_opencode_row_ignores_pi_pane(t *testing.T) {
	// Given: only a pi pane lives at /proj/solo.
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixturePiOnlyPanes},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{row("ses-oc", "/proj/solo")})

	// Then: no claim, no orphan — DB-only.
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got[0].Orphan || got[0].Pane.CWD != "" || got[0].Pane.Exited {
		t.Errorf("opencode row must not claim a pi pane: %+v", got[0])
	}
}

func Test_Enrich_pi_orphan_flagged_per_harness(t *testing.T) {
	// Given: no panes at all, but a pi proc lives at the session directory.
	f := &fakeZellij{sessionsOut: "\n", panes: map[string]string{}}
	src := testSource(t, f, nil, func(root string) {
		writeFixtureProc(t, root, 400, "pi", "S (sleeping)", 1, "/proj/piorphan")
		writeFixtureProc(t, root, 401, "opencode", "S (sleeping)", 1, "/proj/ocorphan")
	})

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{
		piRow("ses-pi", "/proj/piorphan"),
		row("ses-oc", "/proj/ocorphan"),
	})

	// Then: each harness orphans only on its own proc.
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if !got[0].Orphan || !got[0].Pane.Exited || got[0].Pane.CWD != "" {
		t.Errorf("pi row must read orphan: %+v", got[0])
	}
	if !got[1].Orphan {
		t.Errorf("opencode row must read orphan: %+v", got[1])
	}
}

func Test_Enrich_unknown_pane_stays_claimable_by_either_harness(t *testing.T) {
	// Given: a plain shell pane at /proj/sh (legacy presence behavior).
	panesJSON := `[{"id": 9, "title": "shell",
		"terminal_command": "zsh", "pane_command": "zsh",
		"pane_cwd": "/proj/sh", "exited": false, "exit_status": null,
		"tab_id": 0, "tab_name": "Tab #1"}]`
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": panesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{piRow("ses-pi", "/proj/sh")})

	// Then: legacy claim still applies (unknown foreground ≠ agent pane).
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if got[0].Pane.CWD != "/proj/sh" {
		t.Errorf("pi row should claim unknown-foreground pane (legacy): %+v", got[0].Pane)
	}
}

// fixtureBothLivePanes serves one directory hosting BOTH agents live, with
// distinct pane ids so resolution routing is observable.
const fixtureBothLivePanes = `[
  {"id": 11, "title": "OC | live",
   "terminal_command": "opencode", "pane_command": "opencode",
   "pane_cwd": "/proj/both", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"},
  {"id": 12, "title": "π - both",
   "terminal_command": null, "pane_command": "pi",
   "pane_cwd": "/proj/both", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"}
]`

func Test_Enrich_live_shell_pane_excluded_from_counts_but_claimable(t *testing.T) {
	// Given: one live shell pane (no agent) and one live opencode pane.
	panesJSON := `[
  {"id": 1, "title": "shell",
   "terminal_command": "zsh", "pane_command": "zsh",
   "pane_cwd": "/proj/sh", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"},
  {"id": 2, "title": "OC | x",
   "terminal_command": "opencode", "pane_command": "opencode",
   "pane_cwd": "/proj/oc", "exited": false, "exit_status": null,
   "tab_id": 0, "tab_name": "Tab #1"}
]`
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": panesJSON},
	}
	src := testSource(t, f, []string{"main"}, nil)

	// When
	got, err := src.Enrich(context.Background(), []model.SessionRow{
		row("ses-sh", "/proj/sh"),
		row("ses-oc", "/proj/oc"),
	})

	// Then: the shell pane still satisfies the legacy claim, but the
	// fleet-wide pane count covers agent panes only.
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	byID := map[string]model.SessionRow{got[0].ID: got[0], got[1].ID: got[1]}
	if byID["ses-sh"].Pane.CWD != "/proj/sh" {
		t.Errorf("shell pane must stay legacy-claimable: %+v", byID["ses-sh"].Pane)
	}
	if panes, _ := src.LiveCounts(); panes != 1 {
		t.Errorf("LiveCounts panes = %d, want 1 (shell excluded)", panes)
	}
}

func Test_ResolvePane_harness_disambiguates_shared_directory(t *testing.T) {
	// Given: /proj/both hosts a live opencode pane (id 11) and a live pi
	// pane (id 12).
	f := &fakeZellij{
		sessionsOut: "main\n",
		panes:       map[string]string{"main": fixtureBothLivePanes},
	}
	src := testSourceWithDump(t, f, &fakeDump{})

	oc, err := src.ResolvePane(context.Background(), "/proj/both", model.HarnessOpencode)
	if err != nil {
		t.Fatalf("opencode resolve: %v", err)
	}
	if oc.PaneID != "11" {
		t.Errorf("opencode resolved pane %q, want 11", oc.PaneID)
	}
	pi, err := src.ResolvePane(context.Background(), "/proj/both", model.HarnessPi)
	if err != nil {
		t.Fatalf("pi resolve: %v", err)
	}
	if pi.PaneID != "12" {
		t.Errorf("pi resolved pane %q, want 12", pi.PaneID)
	}
}
