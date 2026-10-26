package collector

import (
	"encoding/json"
	"testing"

	"opencode-dashboard/internal/model"
)

func projectRow(id, dir string, status model.SessionStatus, tokens int64, cost float64, ageMs int64) model.SessionRow {
	return model.SessionRow{
		ID:                id,
		Directory:         dir,
		Status:            status,
		RawTokensTotal:    tokens,
		RawCost:           cost,
		UpdatedRelativeMs: ageMs,
	}
}

func Test_buildProjects_groups_by_cleaned_directory_and_joins_procs(t *testing.T) {
	// Given: three session directories, a trailing-slash duplicate, an empty
	// directory row, and a proc-only directory with no sessions
	rows := []model.SessionRow{
		projectRow("a1", "/work/a", model.SessionRunning, 100, 1, 5000),
		projectRow("a2", "/work/a/", model.SessionWaitingInput, 50, 0.5, 1000),
		projectRow("a3", "/work/a", model.SessionError, 25, 0.25, 9000),
		projectRow("a4", "/work/a", model.SessionIdle, 5, 0.05, 7000),
		projectRow("d1", "/work/d", model.SessionRunning, 10, 0.1, 500),
		projectRow("b1", "/work/b", model.SessionRunning, 10, 0.1, 2000),
		projectRow("c1", "/work/c", model.SessionIdle, 10, 0.1, 2000),
		projectRow("empty", "", model.SessionRunning, 999, 9.99, 0),
	}
	procs := map[string]int{"/work/a": 2, "/work/d": 1, "/work/b": 1, "/work/c": 1, "/work/orphan": 5}
	panes := map[string]int{"/work/a": 3, "/work/d": 1, "/work/b": 1, "/work/c": 1, "/work/orphan": 7}

	// When
	got := buildProjects(rows, procs, panes)

	// Then: live_procs desc, running desc, last_activity asc
	wantDirs := []string{"/work/a", "/work/d", "/work/b", "/work/c"}
	if len(got) != len(wantDirs) {
		t.Fatalf("projects = %d, want %d: %+v", len(got), len(wantDirs), got)
	}
	for i, dir := range wantDirs {
		if got[i].Directory != dir {
			t.Fatalf("project[%d] = %q, want %q (full=%+v)", i, got[i].Directory, dir, got)
		}
	}

	a := got[0]
	if a.Name != "a" || a.Sessions != 4 || a.Running != 1 || a.WaitingInput != 1 || a.Idle != 2 {
		t.Errorf("project a = %+v, want name=a sessions=4 running=1 waiting=1 idle=2", a)
	}
	if a.TokensTotal != 180 || a.CostTotal != 1.8 || a.LiveProcs != 2 || a.LastActivityMs != 1000 {
		t.Errorf("project a totals = %+v, want tokens=180 cost=1.8 live=2 last=1000", a)
	}
	if a.LivePanes != 3 {
		t.Errorf("project a live_panes = %d, want the pane-derived 3", a.LivePanes)
	}
	if got[1].LiveProcs != 1 || got[1].Running != 1 {
		t.Errorf("project d = %+v, want live=1 running=1", got[1])
	}
}

func Test_buildProjects_sorts_by_live_panes_then_running_then_activity_then_directory(t *testing.T) {
	procs := map[string]int{
		"/w/panes2": 1, "/w/run2": 1, "/w/run1": 1,
		"/w/act100": 1, "/w/act200a": 1, "/w/act200b": 1,
	}
	panes := map[string]int{
		"/w/panes2": 2, "/w/run2": 1, "/w/run1": 1,
		"/w/act100": 1, "/w/act200a": 1, "/w/act200b": 1,
	}
	rows := []model.SessionRow{
		projectRow("p1", "/w/panes2", model.SessionIdle, 1, 0, 900),
		projectRow("r2a", "/w/run2", model.SessionRunning, 1, 0, 900),
		projectRow("r2b", "/w/run2", model.SessionRunning, 1, 0, 900),
		projectRow("r1", "/w/run1", model.SessionRunning, 1, 0, 900),
		projectRow("a100", "/w/act100", model.SessionIdle, 1, 0, 100),
		projectRow("a200a", "/w/act200a", model.SessionIdle, 1, 0, 200),
		projectRow("a200b", "/w/act200b", model.SessionIdle, 1, 0, 200),
	}

	// When
	got := buildProjects(rows, procs, panes)

	// Then
	want := []string{"/w/panes2", "/w/run2", "/w/run1", "/w/act100", "/w/act200a", "/w/act200b"}
	if len(got) != len(want) {
		t.Fatalf("projects = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, dir := range want {
		if got[i].Directory != dir {
			t.Fatalf("project[%d] = %q, want %q (full=%+v)", i, got[i].Directory, dir, got)
		}
	}
}

func Test_Collector_Tick_publishes_projects_with_runtime_procs(t *testing.T) {
	// Given: two session directories; only /work/a has live processes
	db := &fakeDB{
		rows: []model.SessionRow{
			projectRow("a1", "/work/a", model.SessionRunning, 10, 0.1, 1000),
			projectRow("b1", "/work/b", model.SessionIdle, 5, 0.05, 2000),
		},
		updMs: 1,
		evID:  1,
	}
	rt := &fakeRuntime{
		procsByDir: map[string]int{"/work/a": 2},
		panesByDir: map[string]int{"/work/a": 3},
	}
	sink := &fakeSink{}
	c := newTestCollector(db, rt, sink)

	// When
	if _, err := c.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	// Then
	got := sink.published()
	if len(got) != 1 {
		t.Fatalf("published = %d, want 1", len(got))
	}
	projects := got[0].Projects
	if len(projects) != 2 || projects[0].Directory != "/work/a" || projects[0].LiveProcs != 2 {
		t.Fatalf("projects = %+v, want /work/a first with live_procs=2", projects)
	}
	if projects[0].LivePanes != 3 {
		t.Fatalf("project /work/a live_panes = %d, want 3", projects[0].LivePanes)
	}
	if projects[1].Directory != "/work/b" || projects[1].LiveProcs != 0 || projects[1].LivePanes != 0 {
		t.Fatalf("projects = %+v, want /work/b second with live_procs=0 live_panes=0", projects)
	}
}

func Test_ProjectSummary_and_KPI_JSON_names_are_frozen(t *testing.T) {
	// Given
	project, err := json.Marshal(model.ProjectSummary{Directory: "/x", Name: "x"})
	if err != nil {
		t.Fatalf("marshal project: %v", err)
	}
	kpis, err := json.Marshal(model.KPIs{})
	if err != nil {
		t.Fatalf("marshal kpis: %v", err)
	}

	// When
	var projectObj map[string]json.RawMessage
	if err := json.Unmarshal(project, &projectObj); err != nil {
		t.Fatalf("unmarshal project: %v", err)
	}
	var kpiObj map[string]json.RawMessage
	if err := json.Unmarshal(kpis, &kpiObj); err != nil {
		t.Fatalf("unmarshal kpis: %v", err)
	}

	// Then
	wantProject := []string{
		"directory", "name", "sessions", "running", "waiting_input", "idle",
		"tokens_total", "cost_total", "live_procs", "live_panes", "last_activity_ms",
	}
	if len(projectObj) != len(wantProject) {
		t.Errorf("project keys = %d, want %d: %v", len(projectObj), len(wantProject), projectObj)
	}
	for _, key := range wantProject {
		if _, ok := projectObj[key]; !ok {
			t.Errorf("project JSON missing %q", key)
		}
	}
	for _, key := range []string{"cost_total", "metrics_age_ms", "cost_today", "tokens_per_hour"} {
		if _, ok := kpiObj[key]; !ok {
			t.Errorf("kpis JSON missing %q", key)
		}
	}
}
