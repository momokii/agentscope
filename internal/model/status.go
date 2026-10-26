package model

// StatusSignals is the primitive DB-derived input set for DeriveStatus.
// Every field is a primitive (bool/int/string) so the derivation stays a
// pure function with no DB, clock, or I/O dependency. Producer:
// internal/db reads these signals (indexed per-session slices only — never a
// full part.data scan); internal/collector maps them per session and calls
// DeriveStatus.
//
// Signal sources (research-opencode-internals §4):
//   - InFlightAssistant: a message with role=assistant whose time.completed
//     IS NULL (verified live) AND whose own time_created is recent (within
//     the active window — see RecentActivity). Producers gate this at
//     detection time so an ancient aborted message (completed never set,
//     e.g. a killed TUI) can never mark a session running on its own.
//   - RunningToolParts: count of tool parts with state.status=running.
//   - HasFreshEvents: fresh message.part.updated.1 event rows arrived since
//     the previous tick (delta-only on the fleet path; the seed tick reports
//     false). Detail-path rows keep tail semantics; the recency gate below
//     keeps both honest.
//   - LatestRole: role of the newest message ("user", "assistant", or "").
//   - HasError: a message error union member observed (ProviderAuthError,
//     MessageAbortedError, ContextOverflowError, etc.).
//   - Compacting/Archived: session.time_compacting / time_archived set.
//   - RecentActivity: session.time_updated is inside the active window.
//     opencode bumps session.time_updated on every message/part event, which
//     is what makes this gate sound: a session with no bumps for a full
//     window cannot be generating, no matter what stale rows its slices
//     still hold (stale in-flight rows, stale running tools, stale fresh
//     tails — all observed live on 70+ day old sessions).
type StatusSignals struct {
	// InFlightAssistant is true when a recent assistant message is still
	// generating (time.completed absent and time_created inside the window).
	InFlightAssistant bool
	// RunningToolParts counts tool parts with state.status == running.
	RunningToolParts int
	// HasFreshEvents is true when fresh event rows arrived since the
	// previous tick (fleet incremental path) or the newest tail holds a
	// fresh row (detail path).
	HasFreshEvents bool
	// LatestRole is the role of the newest message; "" when none exists.
	LatestRole string
	// HasError is true when the newest message is an assistant turn carrying
	// a terminal message error. Older errors are ignored: they describe a
	// past completed turn, not a permanent session flag (error-staleness).
	HasError bool
	// Compacting is true when session.time_compacting is set.
	Compacting bool
	// Archived is true when session.time_archived is set.
	Archived bool
	// RecentActivity is true when session.time_updated is inside the
	// active window (ports.Config.ActiveWindow, default 2m). Callers
	// compute it from now minus time_updated — DeriveStatus itself takes
	// no clock, so it stays pure and table-testable.
	RecentActivity bool
}

// DeriveStatus maps StatusSignals to a SessionStatus. It is pure: no I/O,
// no clock, no globals — safe to unit test exhaustively and to call from the
// collector hot loop. Recency arrives as data (RecentActivity), computed by
// the caller from now minus session.time_updated against the active window.
//
// Precedence (first match wins):
//  1. Archived — time_archived set (wins over everything, even staleness).
//  2. Idle on staleness — no session.time_updated bump inside the window
//     means nothing is generating, regardless of stale signal rows.
//  3. Running — recent in-flight assistant turn, any running tool part, or
//     fresh event rows arrived since the previous tick. Running outranks
//     error: error describes the last completed turn, not a permanent flag,
//     so a session generating right now is running even when the previous
//     turn errored.
//  4. Error — the newest assistant turn carried a terminal error (and
//     nothing is generating now).
//  5. Waiting-input (HEURISTIC — no explicit flag exists in the schema):
//     compacting, or latest message is user-role with nothing in flight.
//  6. Idle — everything else.
//
// Callers: internal/db (fleet + detail paths) and internal/collector
// (re-affirmation round-trip). Never trust
// GET /session/status alone — it covers only its own server instance.
func DeriveStatus(s StatusSignals) SessionStatus {
	if s.Archived {
		return SessionArchived
	}
	if !s.RecentActivity {
		return SessionIdle
	}
	if s.InFlightAssistant || s.RunningToolParts > 0 || s.HasFreshEvents {
		return SessionRunning
	}
	if s.HasError {
		return SessionError
	}
	// Waiting-input is a heuristic: the user spoke last and nothing is
	// generating, or the session is mid-compaction. There is no
	// pending/permission table to consult in schema v1.18.
	if s.Compacting || (s.LatestRole == "user" && !s.InFlightAssistant) {
		return SessionWaitingInput
	}
	return SessionIdle
}
