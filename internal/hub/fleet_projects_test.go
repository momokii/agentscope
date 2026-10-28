package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
)

// Given: a published snapshot carrying projects and the additive KPI fields.
// When: /api/fleet is fetched.
// Then: the frozen JSON names are present and the project values survive.
func Test_Fleet_includes_projects_and_additive_kpis(t *testing.T) {
	// Given
	_, h, baseURL := testServer(t, defaultTestConfig())
	snap := fixtureSnapshot(61, 1)
	snap.Projects = []model.ProjectSummary{{
		Directory: "/work/a", Name: "a", Sessions: 2, Running: 1, WaitingInput: 1,
		Idle: 0, TokensTotal: 300, CostTotal: 1.25, LiveProcs: 2, LastActivityMs: 700,
	}}
	snap.KPIs.CostTotal = 78.4
	snap.KPIs.MetricsAgeMs = 123
	snap.KPIs.Cost24h = 4.5
	snap.SessionWindows = map[string]model.WindowSums{
		"ses_1": {Tokens24h: 900, Cost24h: 4.5},
	}
	if err := h.Publish(context.Background(), snap); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// When
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/fleet")
	if err != nil {
		t.Fatalf("GET fleet: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read fleet: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode fleet: %v", err)
	}
	var decoded FleetResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode typed fleet: %v", err)
	}

	// Then
	if _, ok := raw["projects"]; !ok {
		t.Fatal("fleet response missing projects")
	}
	if len(decoded.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(decoded.Projects))
	}
	got := decoded.Projects[0]
	if got.Directory != "/work/a" || got.LiveProcs != 2 || got.CostTotal != 1.25 {
		t.Errorf("project = %+v, want /work/a live=2 cost=1.25", got)
	}
	if decoded.KPIs.CostTotal != 78.4 || decoded.KPIs.MetricsAgeMs != 123 {
		t.Errorf("kpis = %+v, want cost_total=78.4 metrics_age_ms=123", decoded.KPIs)
	}
	if decoded.KPIs.Cost24h != 4.5 {
		t.Errorf("kpis.cost_24h = %v, want 4.5", decoded.KPIs.Cost24h)
	}
	if got, ok := decoded.SessionWindows["ses_1"]; !ok || got.Tokens24h != 900 || got.Cost24h != 4.5 {
		t.Errorf("session_windows = %+v, want ses_1 {900 4.5}", decoded.SessionWindows)
	}
}
