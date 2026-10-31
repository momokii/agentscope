package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"opencode-dashboard/internal/history"
)

// fakeHistory serves canned history payloads for route tests.
type fakeHistory struct {
	history history.History
	err     error
}

func (f *fakeHistory) Query(_ context.Context, _, _ int64) (history.History, error) {
	return f.history, f.err
}

func Test_History_range_validation_and_shape(t *testing.T) {
	s, _, baseURL := testServer(t, defaultTestConfig())
	fake := &fakeHistory{history: history.History{
		From: 1000,
		To:   2000,
		Points: []history.Point{
			{Ts: 1000, Cost24h: 1.25, Tokens24h: 100, Active: 2},
			{Ts: 2000, Cost24h: 4.5, Tokens24h: 300, Active: 3},
		},
		Projects: []history.ProjectHistory{
			{Directory: "/proj/a", Sessions: 2, Cost24h: 4.5, Spark: []history.Point{{Ts: 2000, Cost24h: 4.5}}},
		},
	}}
	s.SetHistorySource(fake)

	client := &http.Client{Timeout: 10 * time.Second}
	get := func(path string) (int, []byte) {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return resp.StatusCode, body
	}

	if code, _ := get("/api/history?range=bogus"); code != http.StatusBadRequest {
		t.Fatalf("unknown range status = %d, want 400", code)
	}
	code, body := get("/api/history?range=7d")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", code, body)
	}
	var decoded struct {
		From   int64 `json:"from"`
		To     int64 `json:"to"`
		Points []struct {
			Ts      int64   `json:"ts"`
			Cost24h float64 `json:"cost_24h"`
			Active  int     `json:"active"`
		} `json:"points"`
		Projects []struct {
			Directory string  `json:"directory"`
			Cost24h   float64 `json:"cost_24h"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(decoded.Points) != 2 || decoded.Points[1].Cost24h != 4.5 {
		t.Errorf("points = %+v, want 2 with trailing cost 4.5", decoded.Points)
	}
	if len(decoded.Projects) != 1 || decoded.Projects[0].Directory != "/proj/a" {
		t.Errorf("projects = %+v, want [/proj/a]", decoded.Projects)
	}
}

func Test_History_unwired_is_503(t *testing.T) {
	_, _, baseURL := testServer(t, defaultTestConfig())
	// No SetHistorySource: zero value serves 503 like the other on-demand backends.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/history?range=7d")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
