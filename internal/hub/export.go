package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
)

// ExportSource is the on-demand `opencode export`-shaped backend behind
// GET /api/sessions/:id/export. Production implementation:
// *db.Reader.SessionExport (read-only WAL snapshot). Tests inject fakes.
// This is a separate seam from DetailSource so the frozen `/api/sessions/:id`
// and `/tools` routes keep their existing contract unchanged.
type ExportSource interface {
	SessionExport(ctx context.Context, id string) (model.SessionExport, error)
}

// SetExportSource wires the on-demand export backend. Call before Run; the
// zero value (nil) serves 503 export-unavailable.
func (s *Server) SetExportSource(e ExportSource) {
	s.export = e
}

// SessionExportResponse is GET /api/sessions/:id/export: the viewed session
// plus its direct children, both in `opencode export` wire shape.
type SessionExportResponse struct {
	Session  model.ExportSession   `json:"session"`
	Children []model.ExportSession `json:"children"`
}

// handleSessionExport serves GET /api/sessions/:id/export from the live
// read-only DB. Unknown ids report 404 via db.ErrNotFound; an unwired seam
// reports 503; every other failure is a generic 500. No CORS-* (same-origin
// embedded SPA, see doc.go).
func (s *Server) handleSessionExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	if s.export == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "export unavailable")
		return
	}
	payload, err := s.export.SessionExport(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "unknown session")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	if payload.Children == nil {
		payload.Children = []model.ExportSession{}
	}
	if payload.Session.Messages == nil {
		payload.Session.Messages = []model.ExportMessage{}
	}
	// The SPA iterates parts unconditionally; a null array would throw
	// mid-render. Real turns can be part-less (pi usage-only turns, and
	// the db path emits the same shape), so normalize both harnesses here.
	for i := range payload.Session.Messages {
		if payload.Session.Messages[i].Parts == nil {
			payload.Session.Messages[i].Parts = []json.RawMessage{}
		}
	}
	for i := range payload.Children {
		if payload.Children[i].Messages == nil {
			payload.Children[i].Messages = []model.ExportMessage{}
		}
		for j := range payload.Children[i].Messages {
			if payload.Children[i].Messages[j].Parts == nil {
				payload.Children[i].Messages[j].Parts = []json.RawMessage{}
			}
		}
	}
	resp := SessionExportResponse{Session: payload.Session, Children: payload.Children}
	body, err := json.Marshal(resp)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, body)
}
