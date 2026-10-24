package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
)

// DetailSource is the on-demand drill-down backend behind
// GET /api/sessions/:id and GET /api/sessions/:id/tools. Production
// implementation: *db.Reader.SessionDetail (read-only WAL snapshot).
// Tests inject fakes. The collector never implements this: detail content
// is fetched per user view only, never per tick.
type DetailSource interface {
	SessionDetail(ctx context.Context, id string) (model.SessionDetail, error)
}

// SetDetailSource wires the on-demand detail backend. Call before Run;
// the zero value (nil) serves 503 detail-unavailable. Production wiring
// lives in cmd/dashboard/main.go next to SetScreenSource.
func (s *Server) SetDetailSource(d DetailSource) {
	s.detail = d
}

// SessionMessage is one transcript entry in the detail response.
type SessionMessage struct {
	Role    string            `json:"role"`
	Content string            `json:"content"`
	Model   string            `json:"model"`
	Tokens  model.TokenCounts `json:"tokens"`
}

// SessionTodo is one todo entry in the detail response.
type SessionTodo struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// SessionDetailResponse is GET /api/sessions/:id: the session row plus its
// messages, todos, and subagent children.
type SessionDetailResponse struct {
	Session  model.SessionRow   `json:"session"`
	Messages []SessionMessage   `json:"messages"`
	Todos    []SessionTodo      `json:"todos"`
	Children []model.SessionRow `json:"children"`
}

// ToolCall is one tool-call entry in the tools response.
type ToolCall struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	DurationMs int64             `json:"duration_ms"`
	Tokens     model.TokenCounts `json:"tokens"`
}

// SessionToolsResponse is GET /api/sessions/:id/tools.
type SessionToolsResponse struct {
	SessionID string     `json:"session_id"`
	Tools     []ToolCall `json:"tools"`
}

// handleSessionDetail serves GET /api/sessions/:id from the live detail
// backend: content-side row, chronological messages, todos, children, and
// the per-tool roll-up. Id-keyed directly on the seam — no snapshot lookup
// for data. Pane/orphan overlay reads the hub's latest Snapshot (the
// collector-owned DB+runtime join, already in memory) when present, exactly
// like the screen route's directory resolution; unknown ids report 404 via
// the seam's typed ErrNotFound. No CORS-* (same-origin embedded SPA).
func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	if s.detail == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "detail unavailable")
		return
	}
	d, err := s.detail.SessionDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "unknown session")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	overlay := s.paneOverlay()
	messages := make([]SessionMessage, 0, len(d.Messages))
	for _, m := range d.Messages {
		messages = append(messages, SessionMessage{
			Role:    m.Role,
			Content: m.Content,
			Model:   m.Model,
			Tokens:  m.Tokens,
		})
	}
	todos := make([]SessionTodo, 0, len(d.Todos))
	for _, t := range d.Todos {
		todos = append(todos, SessionTodo{
			Content:  t.Content,
			Status:   t.Status,
			Priority: t.Priority,
		})
	}
	children := make([]model.SessionRow, 0, len(d.Children))
	for _, c := range d.Children {
		children = append(children, overlay(c))
	}
	resp := SessionDetailResponse{
		Session:  overlay(d.Session),
		Messages: messages,
		Todos:    todos,
		Children: children,
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

// handleSessionTools serves GET /api/sessions/:id/tools from the same live
// backend: one entry per tool part with status, duration, and tokens.
// Unknown ids report 404 like the detail route.
func (s *Server) handleSessionTools(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	if s.detail == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "detail unavailable")
		return
	}
	d, err := s.detail.SessionDetail(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "unknown session")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	tools := make([]ToolCall, 0, len(d.Tools))
	for _, t := range d.Tools {
		tools = append(tools, ToolCall{
			Name:       t.Name,
			Status:     t.Status,
			DurationMs: t.DurationMs,
			Tokens:     t.Tokens,
		})
	}
	resp := SessionToolsResponse{SessionID: id, Tools: tools}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, payload)
}

// paneOverlay returns a func applying the latest snapshot's pane/orphan
// join onto content-side rows. Rows missing from the snapshot keep their
// zero Pane values; the detail seam never invents presence.
func (s *Server) paneOverlay() func(model.SessionRow) model.SessionRow {
	snap, ok := s.hub.Latest()
	if !ok {
		return func(row model.SessionRow) model.SessionRow { return row }
	}
	byID := make(map[string]model.SessionRow, len(snap.Sessions))
	for _, row := range snap.Sessions {
		byID[row.ID] = row
	}
	return func(row model.SessionRow) model.SessionRow {
		if live, found := byID[row.ID]; found {
			row.Pane = live.Pane
			row.Orphan = live.Orphan
		}
		return row
	}
}
