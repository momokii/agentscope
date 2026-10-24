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
//     IS NULL (verified live).
//   - RunningToolParts: count of tool parts with state.status=running.
//   - HasFreshEvents: fresh message.part.updated.1 event rows for the
//     session (order by event.rowid DESC; dedupe by (aggregate_id,seq)).
//   - LatestRole: role of the newest message ("user", "assistant", or "").
//   - HasError: a message error union member observed (ProviderAuthError,
//     MessageAbortedError, ContextOverflowError, etc.).
//   - Compacting/Archived: session.time_compacting / time_archived set.
type StatusSignals struct {
	// InFlightAssistant is true when an assistant message is still generating.
	InFlightAssistant bool
	// RunningToolParts counts tool parts with state.status == running.
	RunningToolParts int
	// HasFreshEvents is true when recent event rows mention the session.
	HasFreshEvents bool
	// LatestRole is the role of the newest message; "" when none exists.
	LatestRole string
	// HasError is true when a terminal message error was observed.
	HasError bool
	// Compacting is true when session.time_compacting is set.
	Compacting bool
	// Archived is true when session.time_archived is set.
	Archived bool
}

// DeriveStatus maps StatusSignals to a SessionStatus. It is pure: no I/O,
// no clock, no globals — safe to unit test exhaustively and to call from the
// collector hot loop.
//
// Precedence (first match wins):
//  1. Archived — time_archived set.
//  2. Error — terminal message error observed.
//  3. Running — in-flight assistant turn, any running tool part, or fresh
//     event rows.
//  4. Waiting-input (HEURISTIC — no explicit flag exists in the schema):
//     compacting, or latest message is user-role with nothing in flight.
//  5. Idle — everything else.
//
// Callers: internal/collector (per-session per tick). Never trust
// GET /session/status alone — it covers only its own server instance.
func DeriveStatus(s StatusSignals) SessionStatus {
	if s.Archived {
		return SessionArchived
	}
	if s.HasError {
		return SessionError
	}
	if s.InFlightAssistant || s.RunningToolParts > 0 || s.HasFreshEvents {
		return SessionRunning
	}
	// Waiting-input is a heuristic: the user spoke last and nothing is
	// generating, or the session is mid-compaction. There is no
	// pending/permission table to consult in schema v1.18.
	if s.Compacting || (s.LatestRole == "user" && !s.InFlightAssistant) {
		return SessionWaitingInput
	}
	return SessionIdle
}
