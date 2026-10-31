package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
)

type fakeExport struct {
	payload model.SessionExport
	err     error
	ids     []string
}

func (f *fakeExport) SessionExport(ctx context.Context, id string) (model.SessionExport, error) {
	f.ids = append(f.ids, id)
	if f.err != nil {
		return model.SessionExport{}, f.err
	}
	return f.payload, nil
}

func exportFixture() model.SessionExport {
	parent := "ses_demo1"
	return model.SessionExport{
		Session: model.ExportSession{
			Info: model.ExportSessionInfo{
				ID: "ses_demo1", Slug: "slug", ProjectID: "global", Directory: "/tmp",
				Title: "demo", Agent: "build",
				Model: model.ExportSessionModel{ID: "m1", ProviderID: "opencode"},
				Cost:  0.01,
				Tokens: model.ExportTokens{
					Input: 100, Output: 50, Reasoning: 5,
					Cache: model.ExportTokenCache{Read: 20, Write: 2},
				},
				Time: model.ExportSessionTime{Created: 1, Updated: 2},
			},
			Messages: []model.ExportMessage{
				{
					Info: model.ExportMessageInfo{
						ID: "msg_1", SessionID: "ses_demo1", Role: "user",
						Time: model.ExportMessageTime{Created: 1},
					},
					Parts: []json.RawMessage{
						json.RawMessage(`{"id":"prt_1","messageID":"msg_1","sessionID":"ses_demo1","type":"text","text":"hello"}`),
					},
				},
			},
		},
		Children: []model.ExportSession{
			{
				Info: model.ExportSessionInfo{
					ID: "ses_child1", Slug: "child", ProjectID: "global", Directory: "/tmp",
					Title: "child", Agent: "explore",
					Model:    model.ExportSessionModel{ID: "m1", ProviderID: "opencode"},
					ParentID: &parent,
				},
				Messages: []model.ExportMessage{},
			},
		},
	}
}

func Test_SessionExport_shape(t *testing.T) {
	// Given: a server wired to a canned export payload.
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetExportSource(&fakeExport{payload: exportFixture()})

	// When
	var got SessionExportResponse
	decodeGet(t, baseURL+"/api/sessions/ses_demo1/export", &got)

	// Then: session info, raw parts, and child all survive the wire.
	if got.Session.Info.ID != "ses_demo1" || got.Session.Info.Tokens.Cache.Read != 20 {
		t.Fatalf("session info = %+v", got.Session.Info)
	}
	if len(got.Session.Messages) != 1 || len(got.Session.Messages[0].Parts) != 1 {
		t.Fatalf("session messages = %+v", got.Session.Messages)
	}
	var part map[string]any
	if err := json.Unmarshal(got.Session.Messages[0].Parts[0], &part); err != nil {
		t.Fatalf("part decode: %v", err)
	}
	if part["id"] != "prt_1" || part["text"] != "hello" {
		t.Fatalf("part = %+v", part)
	}
	if len(got.Children) != 1 || got.Children[0].Info.ID != "ses_child1" {
		t.Fatalf("children = %+v", got.Children)
	}
	if got.Children[0].Info.ParentID == nil || *got.Children[0].Info.ParentID != "ses_demo1" {
		t.Fatalf("child parentID = %v", got.Children[0].Info.ParentID)
	}
}

func Test_SessionExport_nil_parts_normalized_to_empty(t *testing.T) {
	// Given: a payload with part-less messages (pi usage-only turns hit
	// this; the SPA iterates parts unconditionally).
	payload := exportFixture()
	payload.Session.Messages[0].Parts = nil
	payload.Children[0].Messages = []model.ExportMessage{{Info: model.ExportMessageInfo{ID: "m-c"}}}
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetExportSource(&fakeExport{payload: payload})

	// When
	var got SessionExportResponse
	decodeGet(t, baseURL+"/api/sessions/ses_demo1/export", &got)

	// Then: null arrays arrive as [] (never null) for both session and child.
	if got.Session.Messages[0].Parts == nil {
		t.Fatalf("session parts = nil, want []")
	}
	if got.Children[0].Messages[0].Parts == nil {
		t.Fatalf("child parts = nil, want []")
	}
}

func Test_SessionExport_unknown_id_404(t *testing.T) {
	// Given
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetExportSource(&fakeExport{err: db.ErrNotFound})

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_nope/export")

	// Then
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func Test_SessionExport_unwired_503(t *testing.T) {
	// Given: no export source wired.
	_, _, baseURL := testServer(t, defaultTestConfig())

	// When
	status, _, _ := getScreen(t, baseURL+"/api/sessions/ses_x/export")

	// Then
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
}

func Test_SessionExport_internal_error_500(t *testing.T) {
	// Given
	srv, _, baseURL := testServer(t, defaultTestConfig())
	srv.SetExportSource(&fakeExport{err: context.DeadlineExceeded})

	// When
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/api/sessions/ses_x/export")
	if err != nil {
		t.Fatalf("GET export: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// Then
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
