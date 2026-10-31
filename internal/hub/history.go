package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"opencode-dashboard/internal/history"
)

// HistorySource is the read backend behind GET /api/history (V2-B).
// Production implementation: *history.Store opened on the dashboard-owned
// data dir. Tests inject fakes. The hub never writes: recording happens in
// the collector's tick loop (see collector wiring).
type HistorySource interface {
	Query(ctx context.Context, from, to int64) (history.History, error)
}

// SetHistorySource wires the history backend. Call before Run; the route
// serves 503 history-unavailable while nil (same contract as the other
// on-demand backends).
func (s *Server) SetHistorySource(h HistorySource) {
	s.history = h
}

// handleHistory serves GET /api/history?range=7d|30d: decimated fleet
// points plus per-project slices, all exact recorded samples (never
// averaged). Unknown ranges are 400 (closed contract); an unwired backend
// is 503; failures are 500 with an empty body shape preserved as nulls
// (the UI renders its honest empty state).
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if s.history == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "history unavailable")
		return
	}
	rng, err := history.ParseRange(r.URL.Query().Get("range"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "unknown range (want 7d|30d)")
		return
	}
	now := time.Now().Unix()
	payload, err := s.history.Query(r.Context(), now-rng.WindowSeconds(), now)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusOK, body)
}
