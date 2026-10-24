package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"opencode-dashboard/internal/model"
)

// handleEvents serves GET /api/events as text/event-stream. Framing per
// frame: `event: snapshot` + `data: <Snapshot JSON>` + `id: <seq>`, with a
// `retry:` hint on connect and `: ping` keepalive comments. Reconnects
// carrying Last-Event-ID get an immediate full-snapshot re-sync when the
// hub holds a newer tick.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// No CORS-*: same-origin embedded SPA (see doc.go).
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", DefaultRetryMs); err != nil {
		return
	}
	flusher.Flush()

	sub, err := s.hub.Subscribe()
	if err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "hub closed")
		return
	}
	defer sub.Unsubscribe()

	// Full-snapshot re-sync for reconnects: EventSource replays
	// Last-Event-ID automatically; anything older than the latest tick
	// gets the whole fleet state, not a delta.
	lastSent := parseLastEventID(r)
	if snap, ok := s.hub.Latest(); ok && snap.Seq > lastSent {
		if writeSnapshotFrame(w, flusher, snap) != nil {
			return
		}
		lastSent = snap.Seq
	}

	keepalive := time.NewTicker(s.hub.KeepaliveInterval())
	defer keepalive.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case fr, ok := <-sub.Ch():
			if !ok {
				return
			}
			if fr.seq <= lastSent {
				continue
			}
			if writeFrame(w, flusher, fr.seq, fr.payload) != nil {
				return
			}
			lastSent = fr.seq
		}
	}
}

// parseLastEventID reads the SSE resume cursor: Last-Event-ID header first
// (what EventSource sends), then ?lastEventId / ?last_event_id query
// fallbacks. Unparseable values resume from zero (full re-sync).
func parseLastEventID(r *http.Request) uint64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("lastEventId")
	}
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	if raw == "" {
		return 0
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// writeSnapshotFrame marshals one Snapshot into an SSE snapshot frame.
func writeSnapshotFrame(w http.ResponseWriter, flusher http.Flusher, snap model.Snapshot) error {
	payload, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("hub: marshal snapshot seq %d: %w", snap.Seq, err)
	}
	return writeFrame(w, flusher, snap.Seq, payload)
}

// writeFrame emits one wire frame: event + data + id lines and a flush.
// The trailing blank line is wire-mandatory for SSE parsing.
func writeFrame(w http.ResponseWriter, flusher http.Flusher, seq uint64, payload []byte) error {
	if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\nid: %d\n\n", payload, seq); err != nil {
		return fmt.Errorf("hub: write frame seq %d: %w", seq, err)
	}
	flusher.Flush()
	return nil
}
