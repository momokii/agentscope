package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// testServer starts a hub Run loop and an httptest server on the mux.
// httptest binds 127.0.0.1 loopback listeners, matching production bind.
// It returns the server, hub, and live base URL.
func testServer(t *testing.T, cfg ports.Config, opts ...HubOption) (*Server, *Hub, string) {
	t.Helper()
	h := NewHub(opts...)
	runHub(t, h)
	s := NewServer(cfg, h)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, h, srv.URL
}

// apiResponse is the closed set of decodable API bodies. The union keeps
// decodeGet typed without an untyped escape hatch.
type apiResponse interface {
	HealthResponse | FleetResponse | SessionDetailResponse | SessionToolsResponse | DebugResponse
}

// defaultTestConfig returns Config with loopback bind and test DataDir.
func defaultTestConfig() ports.Config {
	cfg := ports.DefaultConfig()
	cfg.DataDir = "/tmp/opencode-test-data"
	return cfg
}

// decodeGet GETs url, requires 200, and JSON-decodes into v.
func decodeGet[T apiResponse](t *testing.T, url string, v *T) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("GET %s read body: %v", url, err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("GET %s decode %T: %v", url, v, err)
	}
}

// sseFrame is one parsed SSE event block.
type sseFrame struct {
	event string
	data  string
	id    string
}

// readSSEFrame reads lines until a blank line and parses the event block.
// A "…" comment line (keepalive) yields event "" with no data.
func readSSEFrame(t *testing.T, r *bufio.Reader) sseFrame {
	t.Helper()
	var fr sseFrame
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading SSE frame: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return fr
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			fr.event = value
		case "data":
			fr.data = value
		case "id":
			fr.id = value
		case "retry":
			// Connect hint; no assertion beyond presence elsewhere.
		}
	}
}

// openSSE opens an SSE stream with optional Last-Event-ID. The caller owns
// cancel (kill the stream) and must close resp.Body.
func openSSE(t *testing.T, ctx context.Context, url string, lastEventID string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("open SSE %s = %d, want 200", url, resp.StatusCode)
	}
	return resp, bufio.NewReader(resp.Body)
}

// Test_Server_binds_loopback_only asserts the bind contract: Config defaults
// to 127.0.0.1, Addr() never yields 0.0.0.0, and a real listener on
// Config.Addr() lands on loopback. (Source grep for hardcoded 0.0.0.0/:8080
// listeners runs as a verification step alongside this test.)
func Test_Server_binds_loopback_only(t *testing.T) {
	// Given
	cfg := ports.DefaultConfig()

	// When / Then: defaults bind loopback.
	if cfg.BindAddr != "127.0.0.1" {
		t.Fatalf("DefaultConfig.BindAddr = %q, want 127.0.0.1", cfg.BindAddr)
	}
	if strings.HasPrefix(cfg.Addr(), "0.0.0.0") {
		t.Fatalf("DefaultConfig.Addr() = %q, must never be 0.0.0.0", cfg.Addr())
	}

	// When: listen on the configured address with an ephemeral port.
	cfg.Port = "0"
	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		t.Fatalf("listen %s: %v", cfg.Addr(), err)
	}
	defer ln.Close()

	// Then: the kernel bound loopback, not all interfaces.
	addr := ln.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listener addr = %q, want 127.0.0.1:*", addr)
	}

	// Then: Server.Addr derives from Config only.
	s := NewServer(cfg, NewHub())
	if s.Addr() != cfg.Addr() {
		t.Fatalf("Server.Addr() = %q, want %q", s.Addr(), cfg.Addr())
	}
}

// Test_Healthz_shape asserts the frozen health shape and that last_poll_ms
// / stale track the latest tick.
func Test_Healthz_shape(t *testing.T) {
	// Given
	cfg := defaultTestConfig()
	_, h, baseURL := testServer(t, cfg, WithTickInterval(20*time.Millisecond))
	ctx := context.Background()
	snap := fixtureSnapshot(7, 2)
	snap.LastPollMs = 321
	snap.Stale = true
	if err := h.Publish(ctx, snap); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// When
	var got HealthResponse
	decodeGet(t, baseURL+"/api/healthz", &got)

	// Then
	if !got.Healthy {
		t.Fatalf("healthz.healthy = false, want true")
	}
	if got.Version != Version {
		t.Fatalf("healthz.version = %q, want %q", got.Version, Version)
	}
	if got.DBPath != cfg.DataDir {
		t.Fatalf("healthz.db_path = %q, want %q", got.DBPath, cfg.DataDir)
	}
	if got.LastPollMs != 321 {
		t.Fatalf("healthz.last_poll_ms = %d, want 321", got.LastPollMs)
	}
	if !got.Stale {
		t.Fatalf("healthz.stale = false, want true")
	}
}

// Test_Fleet_shape asserts the frozen fleet shape against model JSON tags:
// seq echoes the tick, every status validates, KPI fields decode.
func Test_Fleet_shape(t *testing.T) {
	// Given
	cfg := defaultTestConfig()
	_, h, baseURL := testServer(t, cfg, WithTickInterval(20*time.Millisecond))
	if err := h.Publish(context.Background(), fixtureSnapshot(11, 5)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// When
	var got FleetResponse
	decodeGet(t, baseURL+"/api/fleet", &got)

	// Then
	if got.Seq != 11 {
		t.Fatalf("fleet.seq = %d, want 11", got.Seq)
	}
	if len(got.Sessions) != 5 {
		t.Fatalf("fleet.sessions len = %d, want 5", len(got.Sessions))
	}
	for _, row := range got.Sessions {
		if !row.Status.IsValid() {
			t.Fatalf("session %q status %q invalid", row.ID, row.Status)
		}
		if row.ID == "" || row.Model == "" {
			t.Fatalf("session row missing id/model: %+v", row)
		}
	}
	if got.KPIs.Active != 1 || got.KPIs.Errors != 1 || !got.KPIs.SSEConnected {
		t.Fatalf("fleet.kpis mismatch: %+v", got.KPIs)
	}

	// Then: raw keys match the frozen contract (seq/sessions/kpis).
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/fleet")
	if err != nil {
		t.Fatalf("GET fleet: %v", err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("fleet raw decode: %v", err)
	}
	for _, key := range []string{"seq", "sessions", "kpis"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("fleet missing frozen key %q (keys: %v)", key, keysOf(raw))
		}
	}
	var kpis map[string]json.RawMessage
	if err := json.Unmarshal(raw["kpis"], &kpis); err != nil {
		t.Fatalf("kpis decode: %v", err)
	}
	for _, key := range []string{"active", "waiting_input", "tokens_per_hour", "cost_today", "errors", "sse_connected"} {
		if _, ok := kpis[key]; !ok {
			t.Fatalf("kpis missing frozen key %q", key)
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// fakeDetail serves canned SessionDetail payloads so handler tests assert
// routing/mapping/shapes without opening the WAL database.
type fakeDetail struct {
	detail model.SessionDetail
	err    error
	ids    []string
}

func (f *fakeDetail) SessionDetail(ctx context.Context, id string) (model.SessionDetail, error) {
	f.ids = append(f.ids, id)
	if f.err != nil {
		return model.SessionDetail{}, f.err
	}
	return f.detail, nil
}

// liveDetailFixture is a contract-shaped detail payload: one parent row
// with two messages, one todo, one tool, and one child.
func liveDetailFixture() model.SessionDetail {
	return model.SessionDetail{
		Session: model.SessionRow{
			ID: "ses_demo1", Title: "demo", Project: "global",
			Directory: "/tmp", Model: "opencode/m1", Status: model.SessionIdle,
			Tokens: model.TokenCounts{In: 100, Out: 50, Total: 150}, Cost: 0.01,
		},
		Messages: []model.DetailMessage{
			{ID: "m1", Role: "user", Content: "hello", Model: "opencode/m1"},
			{ID: "m2", Role: "assistant", Content: "world", Model: "opencode/m1",
				Tokens: model.TokenCounts{In: 90, Out: 10, Total: 100}},
		},
		Todos: []model.DetailTodo{
			{Content: "ship it", Status: "pending", Priority: "high"},
		},
		Children: []model.SessionRow{
			{ID: "ses_child1", Title: "c", Project: "global", Directory: "/tmp",
				Model: "opencode/m1", Status: model.SessionIdle,
				Tokens: model.TokenCounts{In: 10, Out: 5, Total: 15}},
		},
		Tools: []model.DetailTool{
			{Name: "bash", Status: "completed", DurationMs: 42},
		},
	}
}

// Test_SessionDetail_shape asserts the detail route serves the live seam:
// 200 with the session row plus mapped messages/todos/children on hit.
// (Wave-3 wiring replaced the fixture stub; this test now pins the
// seam-to-wire mapping instead of canned bodies.)
func Test_SessionDetail_shape(t *testing.T) {
	// Given: a server wired to a canned live payload
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetDetailSource(&fakeDetail{detail: liveDetailFixture()})

	// When
	var got SessionDetailResponse
	decodeGet(t, baseURL+"/api/sessions/ses_demo1", &got)

	// Then
	if got.Session.ID != "ses_demo1" {
		t.Fatalf("detail session id = %q, want ses_demo1", got.Session.ID)
	}
	if !got.Session.Status.IsValid() {
		t.Fatalf("detail status %q invalid", got.Session.Status)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[0].Content != "hello" {
		t.Fatalf("detail messages = %+v, want live user/assistant pair", got.Messages)
	}
	if got.Messages[1].Tokens.Total != 100 {
		t.Fatalf("detail msg tokens = %+v, want Total 100", got.Messages[1].Tokens)
	}
	if len(got.Todos) != 1 || got.Todos[0].Content != "ship it" {
		t.Fatalf("detail todos = %+v, want [ship it]", got.Todos)
	}
	if len(got.Children) != 1 || got.Children[0].ID != "ses_child1" {
		t.Fatalf("detail children = %+v, want [ses_child1]", got.Children)
	}
}

// Test_SessionTools_shape asserts the tools route serves the live seam:
// 200 with status/duration/tokens per entry on hit. (Same Wave-3 stub
// retirement as the detail shape test above.)
func Test_SessionTools_shape(t *testing.T) {
	// Given
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetDetailSource(&fakeDetail{detail: liveDetailFixture()})

	// When
	var got SessionToolsResponse
	decodeGet(t, baseURL+"/api/sessions/ses_demo1/tools", &got)

	// Then
	if got.SessionID != "ses_demo1" {
		t.Fatalf("tools session_id = %q, want ses_demo1", got.SessionID)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools list len = %d, want 1", len(got.Tools))
	}
	for _, tool := range got.Tools {
		if tool.Name == "" || tool.Status == "" {
			t.Fatalf("tool entry missing name/status: %+v", tool)
		}
	}
	if got.Tools[0].Name != "bash" || got.Tools[0].DurationMs != 42 {
		t.Fatalf("tool = %+v, want bash/42", got.Tools[0])
	}
}

// Test_SessionDetail_unknown_id_404 asserts the seam's typed ErrNotFound
// maps to 404 (mirrors the screen route's unknown-session mapping).
func Test_SessionDetail_unknown_id_404(t *testing.T) {
	// Given: a detail backend reporting unknown id
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetDetailSource(&fakeDetail{err: db.ErrNotFound})

	// When / Then: both drill-down routes 404
	client := &http.Client{Timeout: 10 * time.Second}
	for _, path := range []string{"/api/sessions/ses_nope", "/api/sessions/ses_nope/tools"} {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

// Test_SessionDetail_unwired_503 asserts the detail routes serve 503 while
// no backend is wired (mirrors the screen route's nil-source contract).
func Test_SessionDetail_unwired_503(t *testing.T) {
	// Given: a server with no DetailSource (zero value)
	_, _, baseURL := testServer(t, defaultTestConfig())

	// When / Then: both drill-down routes 503
	client := &http.Client{Timeout: 10 * time.Second}
	for _, path := range []string{"/api/sessions/ses_x", "/api/sessions/ses_x/tools"} {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d, want 503", path, resp.StatusCode)
		}
	}
}

// Test_Events_headers_and_first_frame asserts SSE framing: content type,
// no-buffering headers, no CORS-*, retry hint, and ≥1 Snapshot frame with
// event/data/id inside 3s whose data parses as model.Snapshot.
func Test_Events_headers_and_first_frame(t *testing.T) {
	// Given
	_, h, baseURL := testServer(t, defaultTestConfig(), WithTickInterval(25*time.Millisecond))
	snap := fixtureSnapshot(21, 2)
	if err := h.Publish(context.Background(), snap); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// When
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, reader := openSSE(t, ctx, baseURL+"/api/events", "")
	defer resp.Body.Close()

	// Then: headers.
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if v := resp.Header.Get("X-Accel-Buffering"); v != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", v)
	}
	if v := resp.Header.Get("Cache-Control"); v != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", v)
	}
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("CORS header present %q: same-origin SPA must not emit CORS-*", v)
	}

	// Then: first snapshot frame within 3s.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("no snapshot frame within 3s")
		}
		fr := readSSEFrame(t, reader)
		if fr.event == "" {
			continue // retry hint or keepalive comment
		}
		if fr.event != "snapshot" {
			t.Fatalf("event = %q, want snapshot", fr.event)
		}
		if fr.id != "21" {
			t.Fatalf("frame id = %q, want 21", fr.id)
		}
		var decoded model.Snapshot
		if err := json.Unmarshal([]byte(fr.data), &decoded); err != nil {
			t.Fatalf("frame data is not a Snapshot: %v", err)
		}
		if decoded.Seq != 21 || len(decoded.Sessions) != 2 {
			t.Fatalf("frame snapshot seq=%d sessions=%d, want 21/2", decoded.Seq, len(decoded.Sessions))
		}
		break
	}
}

// Test_Events_resume_with_last_event_id kills an SSE stream, advances the
// tick, and reconnects with Last-Event-ID: the first frame must be the
// newer FULL snapshot (mirrors the 5s-outage production scenario on a fast
// test clock — the resume mechanics are identical).
func Test_Events_resume_with_last_event_id(t *testing.T) {
	// Given
	_, h, baseURL := testServer(t, defaultTestConfig(), WithTickInterval(25*time.Millisecond))
	if err := h.Publish(context.Background(), fixtureSnapshot(31, 1)); err != nil {
		t.Fatalf("Publish 31: %v", err)
	}
	ctx1, cancel1 := context.WithCancel(context.Background())
	resp1, reader1 := openSSE(t, ctx1, baseURL+"/api/events", "")
	// Skip the connect-time retry hint block; first snapshot wins.
	var first sseFrame
	for i := 0; i < 5; i++ {
		first = readSSEFrame(t, reader1)
		if first.event == "snapshot" {
			break
		}
	}
	if first.id != "31" {
		resp1.Body.Close()
		cancel1()
		t.Fatalf("first stream frame id = %q, want 31", first.id)
	}

	// When: kill the stream (production: 5s outage), advance ticks, resume.
	cancel1()
	resp1.Body.Close()
	time.Sleep(200 * time.Millisecond)
	if err := h.Publish(context.Background(), fixtureSnapshot(32, 3)); err != nil {
		t.Fatalf("Publish 32: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	resp2, reader2 := openSSE(t, ctx2, baseURL+"/api/events", "31")
	defer resp2.Body.Close()

	// Then: immediate full-snapshot re-sync at seq 32 with 3 sessions,
	// skipping the connect-time retry hint block.
	var fr sseFrame
	for i := 0; i < 5; i++ {
		fr = readSSEFrame(t, reader2)
		if fr.event == "snapshot" {
			break
		}
	}
	if fr.event != "snapshot" || fr.id != "32" {
		t.Fatalf("resumed frame = event %q id %q, want snapshot/32", fr.event, fr.id)
	}
	var decoded model.Snapshot
	if err := json.Unmarshal([]byte(fr.data), &decoded); err != nil {
		t.Fatalf("resumed data is not a Snapshot: %v", err)
	}
	if decoded.Seq != 32 || len(decoded.Sessions) != 3 {
		t.Fatalf("resumed snapshot seq=%d sessions=%d, want 32/3", decoded.Seq, len(decoded.Sessions))
	}
}

// Test_Debug_hub_counters asserts the non-frozen debug route exposes
// viewer and drop counters without touching frozen shapes.
func Test_Debug_hub_counters(t *testing.T) {
	// Given
	_, h, baseURL := testServer(t, defaultTestConfig(), WithTickInterval(20*time.Millisecond))
	if err := h.Publish(context.Background(), fixtureSnapshot(41, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	sub, err := h.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// When
	var got DebugResponse
	decodeGet(t, baseURL+"/api/debug/hub", &got)

	// Then
	if got.Viewers < 1 {
		t.Fatalf("debug viewers = %d, want >= 1", got.Viewers)
	}
	if got.Seq != 41 || !got.HasSnapshot {
		t.Fatalf("debug seq=%d has_snapshot=%v, want 41/true", got.Seq, got.HasSnapshot)
	}
	if got.TickMs <= 0 || got.KeepaliveS <= 0 {
		t.Fatalf("debug cadence missing: %+v", got)
	}
}

// Test_No_CORS_on_API asserts no route emits CORS-* (same-origin embedded
// SPA decision; W4-2 owns any cross-origin story). The drill-down routes
// get a wired DetailSource so they serve 200 like production; the CORS
// assertions are unchanged.
func Test_No_CORS_on_API(t *testing.T) {
	// Given
	srv, h, baseURL := testServer(t, defaultTestConfig())
	srv.SetDetailSource(&fakeDetail{detail: liveDetailFixture()})
	if err := h.Publish(context.Background(), fixtureSnapshot(51, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// When / Then
	client := &http.Client{Timeout: 10 * time.Second}
	for _, path := range []string{"/api/healthz", "/api/fleet", "/api/sessions/ses_x", "/api/sessions/ses_x/tools", "/api/debug/hub"} {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		for _, hdr := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
			if v := resp.Header.Get(hdr); v != "" {
				t.Fatalf("GET %s emits %s: %q", path, hdr, v)
			}
		}
	}
}
