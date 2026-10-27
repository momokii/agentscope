package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/runtime"
)

// fakeScreen serves canned dump-screen results and records resolutions, so
// handler tests assert routing/headers/shapes without running zellij.
type fakeScreen struct {
	pane  runtime.ResolvedPane
	text  string
	err   error
	dirs  []string
	fulls []bool
}

func (f *fakeScreen) DumpSessionScreen(ctx context.Context, dir string, harness model.Harness, full bool) (runtime.ResolvedPane, string, error) {
	f.dirs = append(f.dirs, dir)
	f.fulls = append(f.fulls, full)
	return f.pane, f.text, f.err
}

func publishScreenSnapshot(t *testing.T, h *Hub, rows []model.SessionRow) {
	t.Helper()
	snap := model.Snapshot{Seq: 61, Sessions: rows}
	if err := h.Publish(context.Background(), snap); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func screenRow(id, dir string) model.SessionRow {
	return model.SessionRow{ID: id, Directory: dir, Status: model.SessionIdle, Model: "m"}
}

func getScreen(t *testing.T, url string) (int, http.Header, []byte) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s read body: %v", url, err)
	}
	return resp.StatusCode, resp.Header, body
}

func Test_Screen_returns_text_with_headers(t *testing.T) {
	// Given: a live session in the snapshot and a wired screen backend.
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	fixed := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	fake := &fakeScreen{
		pane: runtime.ResolvedPane{Session: "main", PaneID: "7", CWD: "/proj/x", CapturedAt: fixed},
		text: "live pane text\nsecond line",
	}
	s.SetScreenSource(fake)
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, hdr, body := getScreen(t, baseURL+"/api/sessions/ses_live/screen")

	// Then: 200 plain text, verbatim body, identity headers, no CORS-*.
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", status, body)
	}
	if string(body) != "live pane text\nsecond line" {
		t.Fatalf("body = %q, want verbatim dump", body)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", ct)
	}
	if v := hdr.Get("X-Pane-Id"); v != "7" {
		t.Fatalf("X-Pane-Id = %q, want 7", v)
	}
	if v := hdr.Get("X-Captured-At"); v != fixed.Format(time.RFC3339) {
		t.Fatalf("X-Captured-At = %q, want %q", v, fixed.Format(time.RFC3339))
	}
	if v := hdr.Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("CORS header present %q: same-origin SPA must not emit CORS-*", v)
	}

	// Then: directory resolved from the snapshot row, full defaults true.
	if len(fake.dirs) != 1 || fake.dirs[0] != "/proj/x" {
		t.Fatalf("resolved dirs = %v, want [/proj/x]", fake.dirs)
	}
	if len(fake.fulls) != 1 || !fake.fulls[0] {
		t.Fatalf("full flags = %v, want [true] (scrollback default)", fake.fulls)
	}
}

func Test_Screen_viewport_flag_passes_through(t *testing.T) {
	// Given
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	fake := &fakeScreen{text: "viewport"}
	s.SetScreenSource(fake)
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_live/screen?full=0")

	// Then
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if len(fake.fulls) != 1 || fake.fulls[0] {
		t.Fatalf("full flags = %v, want [false]", fake.fulls)
	}
}

func Test_Screen_orphan_returns_409(t *testing.T) {
	// Given: snapshot row flagged orphan (no live pane).
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	fake := &fakeScreen{text: "must never be called"}
	s.SetScreenSource(fake)
	row := screenRow("ses_orphan", "/proj/gone")
	row.Orphan = true
	publishScreenSnapshot(t, h, []model.SessionRow{row})

	// When
	status, _, body := getScreen(t, baseURL+"/api/sessions/ses_orphan/screen")

	// Then: 409 honest DB-only notice, dump backend untouched.
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %q)", status, body)
	}
	var got struct {
		Error  string `json:"error"`
		DBOnly bool   `json:"db_only"`
		Hint   string `json:"hint"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("409 body is not JSON: %v (%q)", err, body)
	}
	if got.Error != "no live pane" || !got.DBOnly || got.Hint != orphanScreenHint {
		t.Fatalf("409 body = %+v, want {no live pane,true,hint}", got)
	}
	if len(fake.dirs) != 0 {
		t.Fatalf("orphan must never reach the dump backend, got %d calls", len(fake.dirs))
	}
}

func Test_Screen_unknown_session_returns_404(t *testing.T) {
	// Given
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	s.SetScreenSource(&fakeScreen{text: "x"})
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_missing/screen")

	// Then
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func Test_Screen_pane_gone_at_dump_returns_409(t *testing.T) {
	// Given: pane died between resolve and dump (dump races pane death).
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	fake := &fakeScreen{err: fmt.Errorf("dump-screen: %w", runtime.ErrPaneGone)}
	s.SetScreenSource(fake)
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, _, body := getScreen(t, baseURL+"/api/sessions/ses_live/screen")

	// Then: same honest 409 as the orphan path, never a fake empty 200.
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %q)", status, body)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("409 body is not JSON: %v", err)
	}
	for _, key := range []string{"error", "db_only", "hint"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("409 body missing key %q: %q", key, body)
		}
	}
}

func Test_Screen_timeout_returns_504(t *testing.T) {
	// Given
	cfg := defaultTestConfig()
	s, h, baseURL := testServer(t, cfg)
	fake := &fakeScreen{err: fmt.Errorf("dump-screen: %w", runtime.ErrScreenTimeout)}
	s.SetScreenSource(fake)
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_live/screen")

	// Then
	if status != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", status)
	}
}

func Test_Screen_unwired_returns_503(t *testing.T) {
	// Given: no screen backend wired (zero value server).
	_, h, baseURL := testServer(t, defaultTestConfig())
	publishScreenSnapshot(t, h, []model.SessionRow{screenRow("ses_live", "/proj/x")})

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_live/screen")

	// Then
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
}
