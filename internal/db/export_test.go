package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func Test_SessionExport_maps_session_messages_parts_and_children(t *testing.T) {
	// Given: a parent with two messages/parts and one child with one message.
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_parent", projectID: "global", title: "p",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode","variant":"default"}`,
		cost: 1.25, in: 100, out: 50, reas: 10, cr: 20, updatedAgo: 1000})
	if _, err := db.Exec(`UPDATE session SET agent=?, tokens_cache_write=? WHERE id=?`,
		"build", 7, "ses_parent"); err != nil {
		t.Fatalf("parent extras: %v", err)
	}
	seedSession(t, db, sessionSeed{id: "ses_child", projectID: "global", parentID: "ses_parent",
		title: "c", directory: "/tmp", modelJSON: `{"id":"m2","providerID":"opencode"}`,
		cost: 0.5, in: 10, out: 5, updatedAgo: 900})
	seedMessage(t, db, "msg_u1", "ses_parent", 100,
		`{"role":"user","time":{"created":100},"agent":"build","model":{"providerID":"opencode","modelID":"m1"}}`)
	seedMessage(t, db, "msg_a1", "ses_parent", 200,
		`{"role":"assistant","time":{"created":200,"completed":250},"agent":"build",`+
			`"providerID":"opencode","modelID":"m1","cost":0.25,`+
			`"tokens":{"input":90,"output":10,"reasoning":4,"cache":{"read":10,"write":7},"total":114}}`)
	seedPart(t, db, "prt_text", "msg_u1", "ses_parent", 100, `{"type":"text","text":"hello"}`)
	seedPart(t, db, "prt_tool", "msg_a1", "ses_parent", 200,
		`{"type":"tool","tool":"bash","callID":"call1","state":{"status":"completed"},"time":{"start":210,"end":260}}`)
	seedMessage(t, db, "msg_c1", "ses_child", 300, `{"role":"assistant","time":{"created":300}}`)
	seedPart(t, db, "prt_c1", "msg_c1", "ses_child", 300, `{"type":"text","text":"child"}`)
	r := fixtureReader(db)

	// When
	got, err := r.SessionExport(context.Background(), "ses_parent")

	// Then: session info block is export-shaped with raw token fields.
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	info := got.Session.Info
	if info.ID != "ses_parent" || info.Slug != "slug-ses_parent" || info.ProjectID != "global" {
		t.Errorf("info identity = %+v", info)
	}
	if info.Directory != "/tmp" || info.Title != "p" || info.Agent != "build" {
		t.Errorf("info scalars = %+v", info)
	}
	if info.Model.ID != "m1" || info.Model.ProviderID != "opencode" || info.Model.Variant != "default" {
		t.Errorf("info model = %+v", info.Model)
	}
	if info.Cost != 1.25 || info.Tokens.Input != 100 || info.Tokens.Output != 50 ||
		info.Tokens.Reasoning != 10 || info.Tokens.Cache.Read != 20 || info.Tokens.Cache.Write != 7 {
		t.Errorf("info cost/tokens = %+v", info)
	}
	if info.Time.Updated-info.Time.Created != 1000 {
		t.Errorf("info time = %+v", info.Time)
	}
	if info.ParentID != nil {
		t.Errorf("parent session parentID = %v, want nil", *info.ParentID)
	}

	// Then: messages chronological with identity injected and raw tokens preserved.
	if len(got.Session.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Session.Messages))
	}
	user := got.Session.Messages[0]
	if user.Info.ID != "msg_u1" || user.Info.SessionID != "ses_parent" || user.Info.Role != "user" {
		t.Errorf("user info = %+v", user.Info)
	}
	if user.Info.Time.Created != 100 || user.Info.Agent != "build" {
		t.Errorf("user time/agent = %+v", user.Info)
	}
	if user.Info.ModelID != "m1" || user.Info.ProviderID != "opencode" {
		t.Errorf("user nested model fallback = %+v", user.Info)
	}
	if len(user.Parts) != 1 {
		t.Fatalf("user parts = %d, want 1", len(user.Parts))
	}
	assertPartIdentity(t, user.Parts[0], "prt_text", "msg_u1", "ses_parent", "text", "hello")

	assistant := got.Session.Messages[1]
	if assistant.Info.Role != "assistant" || assistant.Info.Cost == nil || *assistant.Info.Cost != 0.25 {
		t.Errorf("assistant info = %+v", assistant.Info)
	}
	if assistant.Info.Time.Completed == nil || *assistant.Info.Time.Completed != 250 {
		t.Errorf("assistant completed = %v", assistant.Info.Time.Completed)
	}
	var tokens map[string]any
	if err := json.Unmarshal(assistant.Info.Tokens, &tokens); err != nil {
		t.Fatalf("raw tokens decode: %v", err)
	}
	if tokens["total"] != float64(114) {
		t.Errorf("raw tokens total = %v, want 114", tokens["total"])
	}
	if len(assistant.Parts) != 1 {
		t.Fatalf("assistant parts = %d, want 1", len(assistant.Parts))
	}
	assertPartIdentity(t, assistant.Parts[0], "prt_tool", "msg_a1", "ses_parent", "tool", "")

	// Then: one child in export shape with its own message and parent link.
	if len(got.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(got.Children))
	}
	child := got.Children[0]
	if child.Info.ID != "ses_child" || child.Info.Model.ID != "m2" {
		t.Errorf("child info = %+v", child.Info)
	}
	if child.Info.ParentID == nil || *child.Info.ParentID != "ses_parent" {
		t.Errorf("child parentID = %v, want ses_parent", child.Info.ParentID)
	}
	if len(child.Messages) != 1 || len(child.Messages[0].Parts) != 1 {
		t.Fatalf("child messages/parts = %d/%d", len(child.Messages), len(child.Messages[0].Parts))
	}
}

func Test_SessionExport_children_newest_first(t *testing.T) {
	// Given: two children with different update times.
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_parent", projectID: "global", title: "p",
		directory: "/tmp", updatedAgo: 5000})
	seedSession(t, db, sessionSeed{id: "ses_old", projectID: "global", parentID: "ses_parent",
		title: "old", directory: "/tmp", updatedAgo: 4000})
	seedSession(t, db, sessionSeed{id: "ses_new", projectID: "global", parentID: "ses_parent",
		title: "new", directory: "/tmp", updatedAgo: 100})
	r := fixtureReader(db)

	// When
	got, err := r.SessionExport(context.Background(), "ses_parent")

	// Then
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	if len(got.Children) != 2 || got.Children[0].Info.ID != "ses_new" || got.Children[1].Info.ID != "ses_old" {
		t.Fatalf("children order = %+v, want [ses_new ses_old]", got.Children)
	}
}

func Test_SessionExport_unknown_id_reports_not_found(t *testing.T) {
	// Given
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	r := fixtureReader(db)

	// When
	_, err := r.SessionExport(context.Background(), "ses_missing")

	// Then
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("SessionExport err = %v, want ErrNotFound", err)
	}
}

func Test_SessionExport_empty_session_returns_empty_slices(t *testing.T) {
	// Given: a session with no messages, parts, or children.
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_lonely", projectID: "global", title: "L",
		directory: "/tmp", updatedAgo: 100})
	r := fixtureReader(db)

	// When
	got, err := r.SessionExport(context.Background(), "ses_lonely")

	// Then: present but empty, never nil.
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	if got.Session.Messages == nil || got.Children == nil {
		t.Errorf("slices must be non-nil: %+v", got)
	}
	if len(got.Session.Messages) != 0 || len(got.Children) != 0 {
		t.Errorf("empty session payload = %+v", got)
	}
}

func assertPartIdentity(t *testing.T, raw json.RawMessage, id, messageID, sessionID, typ, text string) {
	t.Helper()
	var part map[string]any
	if err := json.Unmarshal(raw, &part); err != nil {
		t.Fatalf("part decode: %v", err)
	}
	if part["id"] != id || part["messageID"] != messageID || part["sessionID"] != sessionID {
		t.Errorf("part identity = %v/%v/%v, want %s/%s/%s",
			part["id"], part["messageID"], part["sessionID"], id, messageID, sessionID)
	}
	if part["type"] != typ {
		t.Errorf("part type = %v, want %s", part["type"], typ)
	}
	if text != "" && part["text"] != text {
		t.Errorf("part text = %v, want %s", part["text"], text)
	}
}
