// Package pi implements ports.PiReader: read-only parsing of the pi JSONL
// session store (Wave 5, second harness).
//
// Store layout (pi docs session-format.md; verified against pi 1.4.0 live):
//
//	~/.pi/agent/sessions/<slugified-cwd>/<timestamp>_<uuid>.jsonl
//
// Each file is one session: a JSON header line ({type:session, id, cwd,
// timestamp, optional parentSession}) followed by tree entries (message,
// model_change, thinking_level_change, usage, compaction, branch_summary,
// session_info, label, custom, ...). Assistant messages carry
// usage{totalTokens, cost.total} and stopReason; usage-class entries add
// non-conversation usage. Unknown entry types are ignored so a pi schema
// addition can never break a tick.
//
// Status mapping (same five states as opencode, same recency gate): the
// tail of the entry stream maps onto model.StatusSignals — a trailing user
// message or an open toolUse turn means generating (running), a completed
// stop/length turn means waiting-input only when the newest message is
// user-role (mirroring the opencode heuristic exactly), error/aborted on
// the newest assistant turn means error, and anything outside the active
// window means idle. Pi has no archived concept.
//
// Cost model: sums of per-message usage.cost.total (assistant messages plus
// usage/compaction/branch_summary entries, which pi counts in session
// totals). There is no subagent roll-up: fork/clone children are separate
// session files counted as their own rows (SubagentCount only).
//
// Safety: the reader opens ONLY <PiDataDir>/**/*.jsonl. It never touches
// ~/.pi/agent/auth.json or any other file (the mount allowlist excludes
// them; a negative test pins the suffix allowlist). One corrupt file is
// skipped in isolation and never fails the tick. Files are small (observed
// store: 6 files / 1.4 MB); an mtime+size cache skips unchanged files per
// tick while UpdatedRelativeMs is recomputed fresh every tick.
package pi
