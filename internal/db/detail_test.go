package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"opencode-dashboard/internal/model"
)

func seedTodo(t *testing.T, db *sql.DB, sessionID, content, status, priority string, position int) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO todo (session_id, content, status, priority, position, time_created, time_updated)
		 VALUES (?,?,?,?,?,?,?)`,
		sessionID, content, status, priority, position, 1, 2); err != nil {
		t.Fatalf("seed todo %s/%d: %v", sessionID, position, err)
	}
}

func Test_SessionDetail_unknown_id_reports_not_found(t *testing.T) {
	// Given: a fixture db with one session
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_a", projectID: "global", title: "A",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`, updatedAgo: 1000})
	r := fixtureReader(db)

	// When
	_, err := r.SessionDetail(context.Background(), "ses_missing")

	// Then: ErrNotFound via errors.Is (hub maps it to 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionDetail err = %v, want ErrNotFound", err)
	}
}

func Test_SessionDetail_maps_transcript_todos_tools_children(t *testing.T) {
	// Given: parent + child with messages, parts, todos, events
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_parent", projectID: "global", title: "p",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		cost: 1.0, in: 100, out: 50, updatedAgo: 1000})
	seedSession(t, db, sessionSeed{id: "ses_child", projectID: "global", parentID: "ses_parent",
		title: "c", directory: "/tmp", cost: 0.5, in: 10, out: 5, updatedAgo: 900})
	seedMessage(t, db, "msg_1", "ses_parent", 1, `{"role":"user","time":{"created":1},"model":{"providerID":"opencode","modelID":"m1"}}`)
	seedMessage(t, db, "msg_2", "ses_parent", 2,
		`{"role":"assistant","time":{"created":2,"completed":3},"providerID":"opencode","modelID":"m1",`+
			`"tokens":{"input":90,"output":10,"reasoning":4,"cache":{"read":10,"write":0}}}`)
	seedPart(t, db, "prt_t1", "msg_1", "ses_parent", 1, `{"type":"text","text":"hello"}`)
	seedPart(t, db, "prt_t2", "msg_2", "ses_parent", 2, `{"type":"text","text":"world"}`)
	seedPart(t, db, "prt_tool", "msg_2", "ses_parent", 3,
		`{"type":"tool","tool":"bash","state":{"status":"completed"},"time":{"start":100,"end":250}}`)
	seedPart(t, db, "prt_run", "msg_2", "ses_parent", 4,
		`{"type":"tool","tool":"read","state":{"status":"running"},"time":{"start":300,"end":300}}`)
	seedTodo(t, db, "ses_parent", "second", "pending", "low", 1)
	seedTodo(t, db, "ses_parent", "first", "completed", "high", 0)
	seedEvent(t, db, "ev_1", "ses_parent", 1, "message.part.updated.1")
	r := fixtureReader(db)

	// When
	got, err := r.SessionDetail(context.Background(), "ses_parent")

	// Then: session row carries fleet derivation + family roll-up
	if err != nil {
		t.Fatalf("SessionDetail: %v", err)
	}
	if got.Session.ID != "ses_parent" {
		t.Fatalf("session id = %q, want ses_parent", got.Session.ID)
	}
	if got.Session.Status != model.SessionRunning {
		t.Errorf("status = %q, want running (running tool + fresh events)", got.Session.Status)
	}
	if got.Session.Tokens.In != 110 || got.Session.Tokens.Out != 55 || got.Session.Cost != 1.5 {
		t.Errorf("roll-up tokens/cost = %+v cost %v, want {110 55 165} 1.5", got.Session.Tokens, got.Session.Cost)
	}
	if got.Session.SubagentCount != 1 {
		t.Errorf("subagents = %d, want 1", got.Session.SubagentCount)
	}
	if got.Session.Pane != (model.PaneInfo{}) || got.Session.Orphan {
		t.Errorf("pane/orphan must stay zero, got %+v orphan=%v", got.Session.Pane, got.Session.Orphan)
	}

	// Then: messages chronological with joined text, both model shapes, token formula
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "hello" {
		t.Errorf("msg0 = %+v, want user/hello", got.Messages[0])
	}
	if got.Messages[0].Model != "opencode/m1" {
		t.Errorf("msg0 model = %q, want opencode/m1 (nested shape)", got.Messages[0].Model)
	}
	if got.Messages[0].Tokens != (model.TokenCounts{}) {
		t.Errorf("user msg tokens = %+v, want zeros", got.Messages[0].Tokens)
	}
	if got.Messages[1].Content != "world" || got.Messages[1].Model != "opencode/m1" {
		t.Errorf("msg1 = %+v, want assistant/world/opencode/m1", got.Messages[1])
	}
	wantTokens := model.TokenCounts{In: 100, Out: 10, Total: 114}
	if got.Messages[1].Tokens != wantTokens {
		t.Errorf("msg1 tokens = %+v, want %+v", got.Messages[1].Tokens, wantTokens)
	}

	// Then: todos in position order, tools chronological with durations
	if len(got.Todos) != 2 || got.Todos[0].Content != "first" || got.Todos[1].Content != "second" {
		t.Fatalf("todos = %+v, want [first second]", got.Todos)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(got.Tools))
	}
	if got.Tools[0].Name != "bash" || got.Tools[0].Status != "completed" || got.Tools[0].DurationMs != 150 {
		t.Errorf("tool0 = %+v, want bash/completed/150", got.Tools[0])
	}
	if got.Tools[1].Name != "read" || got.Tools[1].Status != "running" || got.Tools[1].DurationMs != 0 {
		t.Errorf("tool1 = %+v, want read/running/0", got.Tools[1])
	}
	if got.Tools[0].Tokens != (model.TokenCounts{}) {
		t.Errorf("tool tokens = %+v, want zeros (no per-tool attribution)", got.Tools[0].Tokens)
	}

	// Then: one child row with its own usage (roll-up keeps child rows intact)
	if len(got.Children) != 1 || got.Children[0].ID != "ses_child" {
		t.Fatalf("children = %+v, want [ses_child]", got.Children)
	}
	if got.Children[0].Tokens.In != 10 || got.Children[0].Cost != 0.5 {
		t.Errorf("child tokens/cost = %+v %v, want own usage", got.Children[0].Tokens, got.Children[0].Cost)
	}
}

func Test_SessionDetail_empty_session_returns_empty_slices(t *testing.T) {
	// Given: a session with no messages/parts/todos/children
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_lonely", projectID: "global", title: "L",
		directory: "/tmp", updatedAgo: 500})
	// NULL-model session with no messages must still resolve (Model "").
	r := fixtureReader(db)

	// When
	got, err := r.SessionDetail(context.Background(), "ses_lonely")

	// Then: present but empty, never nil (frozen wire shapes stay [])
	if err != nil {
		t.Fatalf("SessionDetail: %v", err)
	}
	if got.Messages == nil || got.Todos == nil || got.Children == nil || got.Tools == nil {
		t.Errorf("slices must be non-nil: %+v", got)
	}
	if got.Session.Status != model.SessionIdle {
		t.Errorf("status = %q, want idle", got.Session.Status)
	}
}
