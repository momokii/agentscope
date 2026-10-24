package model

// SessionDetail is the on-demand drill-down payload for one session behind
// GET /api/sessions/:id (and the tool half behind :id/tools). Producer:
// internal/db.SessionDetail reads the live WAL database read-only
// (indexed per-session slices + LIMIT caps only); internal/hub maps these
// domain structs onto the frozen wire shapes without changing field names.
//
// Pane/orphan stay zero here: hub overlays them from its latest Snapshot
// (the collector-owned DB+runtime join), mirroring the screen route.
type SessionDetail struct {
	// Session is the content-side row (status derived, subagent roll-up
	// applied over the session family, SubagentCount = direct children).
	Session SessionRow
	// Messages is one entry per message row, chronological
	// (time_created ASC, id ASC), capped at detailMessageLimit in db.
	Messages []DetailMessage
	// Todos mirrors the todo table ordered by position, capped in db.
	Todos []DetailTodo
	// Children is one content-side row per direct child (parent_id = id),
	// same derivation and roll-up semantics as the fleet rows.
	Children []SessionRow
	// Tools is one entry per tool part, chronological, capped in db.
	Tools []DetailTool
}

// DetailMessage is one transcript entry: role/model/tokens come from the
// message.data blob, Content joins the message's text parts in order.
// Messages without text parts (pure tool/reasoning turns) carry "".
type DetailMessage struct {
	// ID is the message id (ordering/dedup key; not on the frozen wire).
	ID string
	// Role is the message role ("user", "assistant", ...).
	Role string
	// Content is the message's text parts joined with "\n".
	Content string
	// Model renders as "providerID/modelID" (flat or nested shape).
	Model string
	// Tokens mirrors the TokenCounts formula (In includes cache reads).
	Tokens TokenCounts
}

// DetailTodo mirrors one todo row ordered by position.
type DetailTodo struct {
	Content  string
	Status   string
	Priority string
}

// DetailTool mirrors one tool part: the tool name, its raw state status,
// and the part time window. Tokens stays zero: part rows carry no per-tool
// token attribution (tokens live at message/session grain), and inventing
// a split would falsify the export-vs-API diff.
type DetailTool struct {
	Name       string
	Status     string
	DurationMs int64
	Tokens     TokenCounts
}
