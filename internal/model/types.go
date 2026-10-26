package model

// SessionStatus is the dashboard-level lifecycle state of one session.
// It is derived by DeriveStatus (see status.go) from DB signals — it is NOT
// the opencode in-memory SessionStatus (idle|retry|busy), which is per-server
// instance only and was verified to lie about other processes.
//
// Consumers: internal/db detects the raw signals; internal/collector calls
// DeriveStatus per session; internal/hub and web/ switch on these values.
type SessionStatus string

const (
	// SessionRunning means the session is actively generating: an assistant
	// message with time.completed IS NULL, a tool part with state running,
	// or fresh message.part.updated.1 event rows.
	SessionRunning SessionStatus = "running"
	// SessionIdle means the last assistant turn completed and no in-flight
	// work was observed.
	SessionIdle SessionStatus = "idle"
	// SessionWaitingInput is a HEURISTIC (no explicit flag exists in the
	// opencode schema): latest message role is user with no in-flight
	// assistant turn, or the session is compacting/archiving. May
	// misclassify; the UI must label it as heuristic.
	SessionWaitingInput SessionStatus = "waiting-input"
	// SessionError means a terminal failure was observed (message error
	// union: ProviderAuthError, MessageAbortedError, ContextOverflowError,
	// etc.).
	SessionError SessionStatus = "error"
	// SessionArchived means session.time_archived is set.
	SessionArchived SessionStatus = "archived"
)

// IsValid reports whether s is one of the five contract statuses.
func (s SessionStatus) IsValid() bool {
	switch s {
	case SessionRunning, SessionIdle, SessionWaitingInput, SessionError, SessionArchived:
		return true
	default:
		return false
	}
}

// TokenCounts is the token roll-up for one session, including subagent
// children (joined via session.parent_id). All values are cumulative totals.
// Consumer: internal/db computes the roll-up; web/ renders the fleet row.
type TokenCounts struct {
	// In is cumulative input tokens (incl. cache reads).
	In int64 `json:"in"`
	// Out is cumulative output tokens.
	Out int64 `json:"out"`
	// Total is In + Out (+ reasoning where reported).
	Total int64 `json:"total"`
}

// PaneInfo is the zellij/proc presence join for one session.
// Consumer: internal/runtime fills this; orphans (no pane) report Exited
// true with CWD empty and SessionRow.Orphan set.
type PaneInfo struct {
	// CWD is the live pane working directory from /proc/<pid>/cwd.
	CWD string `json:"cwd"`
	// Exited is true when the zellij pane title carries the EXITED suffix
	// or the backing pid is gone (zombie State Z counts as exited).
	Exited bool `json:"exited"`
}

// SessionRow is one fleet-table row: GET /api/fleet → sessions[] and
// GET /api/events snapshot ticks. Field list frozen per the work plan
// (Lane A §2 + Lane C §6). Producers: internal/db (content/cost/tokens)
// joined with internal/runtime (pane/orphan) by internal/collector.
type SessionRow struct {
	// ID is the opencode session id (ses_*).
	ID string `json:"id"`
	// Title is the session title.
	Title string `json:"title"`
	// Project is the project worktree/name (often "global").
	Project string `json:"project"`
	// Directory is session.directory (cwd the session was started in).
	Directory string `json:"directory"`
	// Model is the session model as "providerID/modelID".
	Model string `json:"model"`
	// Status is the derived lifecycle state (see SessionStatus).
	Status SessionStatus `json:"status"`
	// Tokens is the cost/token roll-up incl. subagent children.
	Tokens TokenCounts `json:"tokens"`
	// Cost is cumulative USD cost.
	Cost float64 `json:"cost"`
	// RawCost is this session's own cumulative cost before subagent
	// roll-up. Not serialized: the wire contract's cost keeps its rolled-up
	// meaning, while aggregate KPIs/projects avoid counting children twice.
	RawCost float64 `json:"-"`
	// RawTokensTotal is Tokens.Total before subagent roll-up. Not
	// serialized for the same double-counting reason as RawCost.
	RawTokensTotal int64 `json:"-"`
	// UpdatedRelativeMs is now minus session.time_updated, in milliseconds,
	// computed at snapshot build time. Drives the fleet "relative time" cell.
	UpdatedRelativeMs int64 `json:"updated_relative_ms"`
	// Spark holds recent tokens/min samples for the 72x24 fleet sparkline.
	Spark []float64 `json:"spark"`
	// SubagentCount is the number of child sessions (parent_id = ID).
	SubagentCount int `json:"subagent_count"`
	// Pane is the zellij/proc presence join.
	Pane PaneInfo `json:"pane"`
	// Orphan is true when the session has no live zellij pane (matched only
	// via /proc cmdline scan); live text is unavailable (dump-screen 409).
	Orphan bool `json:"orphan"`
}

// KPIs is the fleet KPI strip: GET /api/fleet → kpis and Snapshot.KPIs.
// Consumer: internal/collector aggregates per tick; web/ renders KpiStrip.
type KPIs struct {
	// Active is the count of sessions with status running.
	Active int `json:"active"`
	// WaitingInput is the count of sessions with heuristic waiting-input.
	WaitingInput int `json:"waiting_input"`
	// TokensPerHour is fleet-wide token throughput: the measured message
	// tokens created in the last 60 seconds, scaled to an hour (x60). It is
	// never derived from session roll-up deltas — a finishing subagent
	// would inject its whole history as one tick's delta.
	TokensPerHour float64 `json:"tokens_per_hour"`
	// CostToday is message-level USD spend since local midnight.
	CostToday float64 `json:"cost_today"`
	// CostTotal is lifetime USD spend: the sum of each session's own
	// cumulative cost (message-level truth, no subagent double count).
	CostTotal float64 `json:"cost_total"`
	// MetricsAgeMs is milliseconds since the last successful message-level
	// metrics refresh. -1 means no refresh has completed yet (unknown).
	MetricsAgeMs int64 `json:"metrics_age_ms"`
	// Errors is the count of sessions with status error.
	Errors int `json:"errors"`
	// LivePanes is the count of live (non-exited) zellij panes observed by
	// the runtime source on the last tick. Compare against Active: running
	// rows far above live panes means stale status, not hidden work.
	LivePanes int `json:"live_panes"`
	// LiveProcesses is the count of alive opencode processes observed via
	// /proc on the last tick. Ground truth for "processes actually alive".
	LiveProcesses int `json:"live_processes"`
	// SSEConnected is true when the hub's event stream is healthy.
	SSEConnected bool `json:"sse_connected"`
}

// ProjectSummary is one per-directory aggregate in Snapshot.Projects.
// Projects are keyed by session.directory (cleaned); directories with no
// sessions are excluded, and a directory with no live process reports
// live_procs 0. Idle counts every session that is neither running nor
// waiting-input (idle, error, archived). Name is the directory basename
// (the directory itself when it has no basename). Field names are frozen.
type ProjectSummary struct {
	Directory      string  `json:"directory"`
	Name           string  `json:"name"`
	Sessions       int     `json:"sessions"`
	Running        int     `json:"running"`
	WaitingInput   int     `json:"waiting_input"`
	Idle           int     `json:"idle"`
	TokensTotal    int64   `json:"tokens_total"`
	CostTotal      float64 `json:"cost_total"`
	LiveProcs      int     `json:"live_procs"`
	LastActivityMs int64   `json:"last_activity_ms"`
}

// Snapshot is one collector tick: GET /api/fleet body and each
// GET /api/events SSE data frame. Consumer: internal/collector builds it
// with a monotonically increasing Seq; internal/hub fans it out and
// re-syncs reconnecting viewers via Last-Event-ID.
type Snapshot struct {
	// Seq is the monotonically increasing tick number.
	Seq uint64 `json:"seq"`
	// Sessions is the full fleet row set, sorted running→exited.
	Sessions []SessionRow `json:"sessions"`
	// Projects is the per-directory aggregate, sorted live_procs desc,
	// running desc, last_activity_ms asc.
	Projects []ProjectSummary `json:"projects"`
	// KPIs is the aggregated fleet strip for this tick.
	KPIs KPIs `json:"kpis"`
	// LastPollMs is the duration of the last collector poll in milliseconds.
	LastPollMs int64 `json:"last_poll_ms"`
	// Stale is true when the frame exceeds the freshness budget (collector
	// self-heal signal; UI shows the paused/stale chip).
	Stale bool `json:"stale"`
}
