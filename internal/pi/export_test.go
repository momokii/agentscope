package pi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/db"
)

// ejoin builds one JSONL line from a map (keeps fixtures readable).
func ejoin(t *testing.T, e map[string]interface{}) string {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func writeExportFile(t *testing.T, dir, sub, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sub, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func usageObj(input, output int64, total float64) map[string]interface{} {
	return map[string]interface{}{
		"input": input, "output": output, "cacheRead": 0, "cacheWrite": 0,
		"reasoning": 0, "totalTokens": input + output,
		"cost": map[string]interface{}{"total": total},
	}
}

// richSession builds one file: header, named session, one assistant turn
// with text + thinking + one tool call, one matching tool result, and a
// trailing user message (so completed timestamps are deterministic).
func richSession(t *testing.T, dir, id, name, cwd string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "session", "version": 3, "id": id,
		"timestamp": "2026-10-27T12:00:00.000Z", "cwd": cwd,
	}))
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "session_info", "id": "si", "parentId": nil,
		"timestamp": "2026-10-27T12:00:01.000Z", "name": name,
	}))
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "message", "id": "m1", "parentId": "si",
		"timestamp": "2026-10-27T12:00:02.000Z",
		"message": map[string]interface{}{
			"role": "user", "content": "list the repo root",
			"timestamp": 1,
		},
	}))
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "message", "id": "m2", "parentId": "m1",
		"timestamp": "2026-10-27T12:00:03.000Z",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "I will list files."},
				map[string]interface{}{"type": "text", "text": "On it."},
				map[string]interface{}{"type": "toolCall", "id": "ls:0", "name": "ls", "arguments": map[string]interface{}{"path": "."}},
			},
			"provider": "opencode-go", "model": "muse-spark-1.3-contributor",
			"stopReason": "toolUse", "usage": usageObj(100, 50, 0.01),
			"timestamp": 2,
		},
	}))
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "message", "id": "m3", "parentId": "m2",
		"timestamp": "2026-10-27T12:00:04.000Z",
		"message": map[string]interface{}{
			"role": "toolResult", "toolCallId": "ls:0", "toolName": "ls",
			"content": []interface{}{map[string]interface{}{"type": "text", "text": "a.txt\nb.txt"}},
			"isError": false, "durationMs": 16, "timestamp": 3,
		},
	}))
	sb.WriteString(ejoin(t, map[string]interface{}{
		"type": "message", "id": "m4", "parentId": "m3",
		"timestamp": "2026-10-27T12:00:05.000Z",
		"message": map[string]interface{}{
			"role": "user", "content": "thanks", "timestamp": 4,
		},
	}))
	stamp := "2026-10-27T12-00-00-000Z"
	writeExportFile(t, dir, "s", stamp+"_"+id+".jsonl", sb.String())
}

func Test_SessionExport_merges_tool_call_and_result(t *testing.T) {
	dir := t.TempDir()
	richSession(t, dir, "aaaabbbb-cccc-dddd-eeee-ffffffffffff", "Fix login", "/x")
	r := New(dir)

	got, err := r.SessionExport(context.Background(), "aaaabbbb-cccc-dddd-eeee-ffffffffffff")
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	if got.Session.Info.ID != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Errorf("info id = %q", got.Session.Info.ID)
	}
	if got.Session.Info.Title != "Fix login" {
		t.Errorf("title = %q", got.Session.Info.Title)
	}
	if got.Session.Info.Model.ID != "muse-spark-1.3-contributor" || got.Session.Info.Model.ProviderID != "opencode-go" {
		t.Errorf("model = %+v", got.Session.Info.Model)
	}
	if len(got.Session.Messages) != 3 {
		t.Fatalf("messages = %d, want user+assistant+user (toolResult folded)", len(got.Session.Messages))
	}
	asst := got.Session.Messages[1]
	if asst.Info.Role != "assistant" {
		t.Fatalf("second message role = %q", asst.Info.Role)
	}
	// Completed = next *message* ts (toolResults fold into messages, so the
	// next message is m4 at +2000ms, not the m3 toolResult at +1000ms).
	if asst.Info.Time.Completed == nil || *asst.Info.Time.Completed != asst.Info.Time.Created+2000 {
		t.Errorf("assistant completed = %+v, want m4 ts", asst.Info.Time.Completed)
	}
	var toolPart map[string]interface{}
	for _, p := range asst.Parts {
		var part map[string]interface{}
		if err := json.Unmarshal(p, &part); err != nil {
			t.Fatalf("part decode: %v", err)
		}
		if part["type"] == "tool" {
			toolPart = part
		}
	}
	if toolPart == nil {
		t.Fatal("no merged tool part found")
	}
	if toolPart["tool"] != "ls" || toolPart["callID"] != "ls:0" {
		t.Errorf("tool identity = %+v", toolPart)
	}
	st := toolPart["state"].(map[string]interface{})
	if st["status"] != "completed" {
		t.Errorf("tool status = %v, want completed", st["status"])
	}
	if out, _ := st["output"].(string); out != "a.txt\nb.txt" {
		t.Errorf("tool output = %q", out)
	}
	if len(got.Children) != 0 {
		t.Errorf("children = %d, want none", len(got.Children))
	}
}

func Test_SessionExport_unknown_id_is_not_found(t *testing.T) {
	dir := t.TempDir()
	writeExportFile(t, dir, "s", "2026-10-27T12-00-00-000Z_good-1.jsonl",
		`{"type":"session","version":3,"id":"good-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}`+"\n")
	r := New(dir)
	_, err := r.SessionExport(context.Background(), "nope")
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("unknown id err = %v, want ErrNotFound", err)
	}
}

func Test_SessionExport_open_turn_stays_open(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	sb.WriteString(`{"type":"session","version":3,"id":"open-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}` + "\n")
	sb.WriteString(`{"type":"message","id":"u","parentId":null,"timestamp":"2026-10-27T12:00:01.000Z","message":{"role":"user","content":"go","timestamp":1}}` + "\n")
	sb.WriteString(`{"type":"message","id":"a","parentId":"u","timestamp":"2026-10-27T12:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"bash:0","name":"bash","arguments":{}}],"provider":"p","model":"m","stopReason":"toolUse","timestamp":2}}` + "\n")
	writeExportFile(t, dir, "s", "2026-10-27T12-00-00-000Z_open-1.jsonl", sb.String())
	r := New(dir)

	got, err := r.SessionExport(context.Background(), "open-1")
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	if len(got.Session.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Session.Messages))
	}
	asst := got.Session.Messages[1]
	if asst.Info.Time.Completed != nil {
		t.Errorf("open turn completed = %v, want nil", asst.Info.Time.Completed)
	}
	var toolPart map[string]interface{}
	for _, p := range asst.Parts {
		var part map[string]interface{}
		if err := json.Unmarshal(p, &part); err != nil {
			t.Fatal(err)
		}
		if part["type"] == "tool" {
			toolPart = part
		}
	}
	if toolPart == nil {
		t.Fatal("expected open tool part")
	}
	if toolPart["state"].(map[string]interface{})["status"] != "running" {
		t.Errorf("open tool status = %+v, want running", toolPart["state"])
	}
}

func Test_SessionExport_children_linked_with_parent_id(t *testing.T) {
	dir := t.TempDir()
	parent := `{"type":"session","version":3,"id":"parent-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}` + "\n" +
		`{"type":"message","id":"u","parentId":null,"timestamp":"2026-10-27T12:00:01.000Z","message":{"role":"user","content":"hi","timestamp":1}}` + "\n"
	writeExportFile(t, dir, "s", "2026-10-27T12-00-00-000Z_parent-1.jsonl", parent)
	child := `{"type":"session","version":3,"id":"child-1","timestamp":"2026-10-27T12:02:00.000Z","cwd":"/x","parentSession":"/store/2026-10-27T12-00-00-000Z_parent-1.jsonl"}` + "\n" +
		`{"type":"message","id":"u2","parentId":null,"timestamp":"2026-10-27T12:02:01.000Z","message":{"role":"user","content":"fork","timestamp":2}}` + "\n"
	writeExportFile(t, dir, "s", "2026-10-27T12-02-00-000Z_child-1.jsonl", child)
	r := New(dir)

	got, err := r.SessionExport(context.Background(), "parent-1")
	if err != nil {
		t.Fatalf("SessionExport: %v", err)
	}
	if len(got.Children) != 1 || got.Children[0].Info.ID != "child-1" {
		t.Fatalf("children = %+v, want [child-1]", got.Children)
	}
	if got.Session.Info.Title != "hi" {
		t.Errorf("parent title falls back to first user text, got %q", got.Session.Info.Title)
	}
	if got.Children[0].Info.ParentID == nil || *got.Children[0].Info.ParentID != "parent-1" {
		t.Errorf("child parentID = %+v, want parent-1", got.Children[0].Info.ParentID)
	}
}
