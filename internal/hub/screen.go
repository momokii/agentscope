package hub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/runtime"
)

// ScreenDumper is the on-demand live-pane source behind
// GET /api/sessions/:id/screen. Production implementation:
// *runtime.Source.DumpSessionScreen (session-scoped dump-screen). Tests
// inject fakes. The collector never implements this: screen content is
// fetched per user view only, never per tick.
type ScreenDumper interface {
	DumpSessionScreen(ctx context.Context, sessionDirectory string, harness model.Harness, full bool) (runtime.ResolvedPane, string, error)
}

// orphanScreenBody is the 409 shape for sessions with no live zellij pane:
// honest DB-only notice, never a fake empty screen.
type orphanScreenBody struct {
	Error  string `json:"error"`
	DBOnly bool   `json:"db_only"`
	Hint   string `json:"hint"`
}

// orphanScreenHint is the fixed 409 hint text.
const orphanScreenHint = "session has no live zellij pane; transcript and DB data remain available"

// SetScreenSource wires the on-demand screen backend. Call before Run;
// the zero value (nil) serves 503 screen-unavailable. There is no
// production wiring yet (no main package); Wave 3 drill-down views consume
// this endpoint once the server constructor passes a *runtime.Source.
func (s *Server) SetScreenSource(d ScreenDumper) {
	s.screen = d
}

// handleSessionScreen serves GET /api/sessions/:id/screen: the on-demand
// live-pane text for the drill-down viewer (hybrid per Lane B §6).
//
// Session→directory resolution reads the hub's latest Snapshot (already the
// DB+runtime join built by the collector each tick) instead of opening the
// WAL database per view: a per-view DB open risks SQLITE_BUSY against live
// TUI writers and disturbs them (Data strategy rule 5), while directories
// are stable across ticks, so snapshot staleness (≤ one poll interval) is
// harmless for this mapping. No db-package lookup exists for this seam, and
// adding one would trade a clean in-memory read for a risky per-view DB
// open — hence resolution lives here, documented.
//
// Responses: live pane → 200 text/plain (whole dump, no truncation — the
// viewer clamps display) with X-Pane-Id / X-Captured-At; orphan, unmapped,
// or died-between-resolve-and-dump pane → 409 orphanScreenBody.
// No CORS-* (same-origin embedded SPA, see doc.go).
func (s *Server) handleSessionScreen(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing session id")
		return
	}
	if s.screen == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "screen unavailable")
		return
	}
	snap, ok := s.hub.Latest()
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "no snapshot")
		return
	}
	var self model.SessionRow
	var found bool
	for _, row := range snap.Sessions {
		if row.ID == id {
			self, found = row, true
			break
		}
	}
	if !found {
		writeJSONError(w, http.StatusNotFound, "unknown session")
		return
	}
	// Claimant-only screen: the snapshot join awards each (directory,
	// harness) pane to exactly one session. Serving the directory pane
	// for a non-claimant would display ANOTHER session's (or a plain
	// shell's) text as this session's live pane — a lie the fleet's
	// orphan/Pane fields already let us refuse. Only the claimant row
	// (live, non-exited pane claim) reaches dump-screen; everyone else
	// gets the honest 409, naming the owner when one exists.
	if self.Orphan || self.Pane.CWD == "" || self.Pane.Exited {
		writeOrphanScreenWithOwner(w, snap.Sessions, self)
		return
	}
	pane, text, err := s.screen.DumpSessionScreen(r.Context(), self.Directory, self.Harness, parseScreenFull(r))
	if err != nil {
		switch {
		case errors.Is(err, runtime.ErrNoLivePane), errors.Is(err, runtime.ErrPaneGone):
			writeOrphanScreen(w)
		case errors.Is(err, runtime.ErrScreenTimeout):
			writeJSONError(w, http.StatusGatewayTimeout, "screen timeout")
		case errors.Is(err, runtime.ErrZellijMissing):
			writeJSONError(w, http.StatusServiceUnavailable, "screen unavailable")
		default:
			writeJSONError(w, http.StatusInternalServerError, "internal")
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Pane-Id", pane.PaneID)
	w.Header().Set("X-Captured-At", pane.CapturedAt.UTC().Format(time.RFC3339))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}

// parseScreenFull reads ?full=: drill-down wants scrollback, so the default
// is full; ?full=0|false|no|off selects viewport-only.
func parseScreenFull(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("full"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// writeOrphanScreen sends the 409 DB-only notice for orphan sessions.
func writeOrphanScreen(w http.ResponseWriter) {
	writeOrphanScreenHint(w, orphanScreenHint)
}

// writeOrphanScreenWithOwner names the session that DOES hold this
// (directory, harness) pair's live pane, when the snapshot knows one — so
// the viewer can navigate to the owner instead of staring at a dead end.
// The shape is unchanged (hint stays free text); empty owner keeps the
// fixed hint byte-for-byte.
func writeOrphanScreenWithOwner(w http.ResponseWriter, rows []model.SessionRow, self model.SessionRow) {
	hint := orphanScreenHint
	for _, row := range rows {
		if row.ID == self.ID || row.Orphan || row.Pane.CWD == "" || row.Pane.Exited {
			continue
		}
		if filepath.Clean(row.Directory) == filepath.Clean(self.Directory) && row.Harness == self.Harness {
			hint = "session has no live pane of its own; the directory's live pane belongs to session " + row.ID
			break
		}
	}
	writeOrphanScreenHint(w, hint)
}

func writeOrphanScreenHint(w http.ResponseWriter, hint string) {
	payload, err := json.Marshal(orphanScreenBody{
		Error:  "no live pane",
		DBOnly: true,
		Hint:   hint,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSONBytes(w, http.StatusConflict, payload)
}
