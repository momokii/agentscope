package pi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// entryType is the JSONL entry discriminator (pi session-format.md).
// Only the types below influence the fleet row; everything else is parsed
// as far as timestamp/role and otherwise ignored.
const (
	entrySession        = "session"
	entryMessage        = "message"
	entryModelChange    = "model_change"
	entryUsage          = "usage"
	entryCompaction     = "compaction"
	entryBranchSummary  = "branch_summary"
	entrySessionInfo    = "session_info"
	entryThinkingChange = "thinking_level_change"
)

// piUsage mirrors the pi Usage interface (message-types.md): token counts
// plus cost. reasoning is already included in output — never added again.
type piUsage struct {
	Input       int64   `json:"input"`
	Output      int64   `json:"output"`
	CacheRead   int64   `json:"cacheRead"`
	CacheWrite  int64   `json:"cacheWrite"`
	Reasoning   int64   `json:"reasoning"`
	TotalTokens int64   `json:"totalTokens"`
	Cost        piCost  `json:"cost"`
}

// piCost mirrors the cost member of pi Usage.
type piCost struct {
	Total float64 `json:"total"`
}

// piMessage mirrors the AgentMessage subset the fleet row needs: role,
// provider/model attribution, stop reason, and usage. Content blocks are
// deliberately NOT decoded here — the fleet path needs only the first user
// text (for untitled sessions) and never the full transcript (P6 owns the
// export-shaped drill-down).
type piMessage struct {
	Role       string          `json:"role"`
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	StopReason string          `json:"stopReason"`
	Usage      *piUsage        `json:"usage"`
	Content    json.RawMessage `json:"content"`
}

// piEntry is one JSONL line: the tree link plus the type-specific payload.
// Payload structs decode only the fields the fleet row needs; unknown
// fields are ignored by encoding/json, so pi schema additions are safe.
type piEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ParentID  string `json:"parentId"`
	Timestamp string `json:"timestamp"`

	Message      *piMessage `json:"message"`
	Provider     string     `json:"provider"`
	ModelID      string     `json:"modelId"`
	Model        string     `json:"model"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind"`
	Usage        *piUsage   `json:"usage"`
	ParentSess   string     `json:"parentSession"`
	CWD          string     `json:"cwd"`
	Version      int        `json:"version"`
}

// usageEvent is one message-level token/cost observation with its entry
// timestamp: the raw material for the message-level metrics sample
// (tokens in the trailing 60s, cost since local midnight). Assistant
// message usage, usage entries, and compaction/summary usage all record
// here — the same set pi counts in session totals.
type usageEvent struct {
	tsMs   int64
	tokens int64
	cost   float64
}

// maxUsageEventsPerFile caps the retained per-file event list. Sessions
// with more usage observations in a day are absurd (the observed max is
// hundreds); beyond the cap the oldest events drop, degrading only the
// midnight-cost tail for that file — never the tick.
const maxUsageEventsPerFile = 50000

// fileData is the cached parse of one session file: everything a tick needs
// to rebuild the row except the clock (UpdatedRelativeMs is recomputed per
// tick from lastTsMs).
type fileData struct {
	id          string
	name        string
	cwd         string
	firstUser   string
	lastRole    string
	lastStop    string
	openToolUse bool
	hasError    bool
	lastModel   string
	lastTsMs    int64
	tokensIn    int64
	tokensOut   int64
	tokensTotal int64
	cost        float64
	parentID    string
	events      []usageEvent
}

// parseSessionFile streams one JSONL session file into fileData. A malformed
// line aborts only this file (the caller skips it in isolation); unknown
// entry types are ignored. Timestamps are ISO 8601; unparseable ones fall
// back to the file mtime so a bad clock never yields a zero time.
func parseSessionFile(path string, fallbackMs int64) (fileData, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileData{}, fmt.Errorf("pi: open %s: %w", path, err)
	}
	defer f.Close()

	var d fileData
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	headerSeen := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if err := d.applyLine(line, fallbackMs); err != nil {
			return fileData{}, fmt.Errorf("pi: parse %s: %w", path, err)
		}
		headerSeen = headerSeen || true
	}
	if err := sc.Err(); err != nil {
		return fileData{}, fmt.Errorf("pi: scan %s: %w", path, err)
	}
	if !headerSeen {
		return fileData{}, fmt.Errorf("pi: parse %s: empty file", path)
	}
	if d.id == "" {
		return fileData{}, fmt.Errorf("pi: parse %s: missing session header", path)
	}
	return d, nil
}

// applyLine folds one JSONL line into the accumulator.
func (d *fileData) applyLine(line []byte, fallbackMs int64) error {
	var e piEntry
	dec := json.NewDecoder(strings.NewReader(string(line)))
	if err := dec.Decode(&e); err != nil {
		return err
	}
	tsMs := parseTimeMs(e.Timestamp, fallbackMs)
	switch e.Type {
	case entrySession:
		d.id = e.ID
		d.cwd = e.CWD
		d.parentID = parentIDFromPath(e.ParentSess)
		if d.lastTsMs == 0 {
			d.lastTsMs = tsMs
		}
	case entryMessage:
		d.applyMessage(e, tsMs)
	case entryModelChange:
		if e.Provider != "" && e.ModelID != "" {
			d.lastModel = e.Provider + "/" + e.ModelID
		}
	case entryUsage, entryCompaction, entryBranchSummary:
		if e.Usage != nil {
			d.addUsage(e.Usage, tsMs)
		}
		d.lastTsMs = tsMs
	case entrySessionInfo:
		if strings.TrimSpace(e.Name) != "" {
			d.name = e.Name
		}
		d.lastTsMs = tsMs
	default:
		// Unknown entry types (label, custom, thinking_level_change, ...)
		// never affect the fleet row, but a newer pi writing them still
		// bumps recency when they carry a timestamp.
		if tsMs > d.lastTsMs {
			d.lastTsMs = tsMs
		}
	}
	return nil
}

// applyMessage folds a message entry: role tail, title seed, model
// attribution, usage sums, and the open-turn/error signals.
func (d *fileData) applyMessage(e piEntry, tsMs int64) {
	if e.Message == nil {
		return
	}
	m := e.Message
	d.lastTsMs = tsMs
	switch m.Role {
	case "user":
		d.lastRole = "user"
		d.lastStop = ""
		d.openToolUse = false
		d.hasError = false
		if d.firstUser == "" {
			d.firstUser = firstText(m.Content)
		}
	case "assistant":
		d.lastRole = "assistant"
		d.lastStop = m.StopReason
		d.openToolUse = m.StopReason == "toolUse"
		d.hasError = m.StopReason == "error" || m.StopReason == "aborted"
		if m.Provider != "" && m.Model != "" {
			d.lastModel = m.Provider + "/" + m.Model
		}
		if m.Usage != nil {
			d.addUsage(m.Usage, tsMs)
		}
	case "toolResult":
		// A tool result extends the open assistant turn; it does not end
		// it. Role tail keeps "assistant" so DeriveStatus sees the turn as
		// ongoing, and usage from nested model work counts toward totals.
		d.lastRole = "assistant"
		if m.Usage != nil {
			d.addUsage(m.Usage, tsMs)
		}
	default:
		// system / custom_message and future roles: recency only.
	}
}

// addUsage folds one pi usage record into the token/cost sums and the
// message-level event list. Input counts cache reads (tokensIn parallels
// the db roll-up where In includes cache reads); reasoning is part of
// output and never double-counted.
func (d *fileData) addUsage(u *piUsage, tsMs int64) {
	d.events = append(d.events, usageEvent{tsMs: tsMs, tokens: u.TotalTokens, cost: u.Cost.Total})
	if len(d.events) > maxUsageEventsPerFile {
		d.events = d.events[len(d.events)-maxUsageEventsPerFile:]
	}
	d.tokensIn += u.Input + u.CacheRead
	d.tokensOut += u.Output
	d.tokensTotal += u.TotalTokens
	d.cost += u.Cost.Total
}

// parentIDFromPath extracts the parent session id from a parentSession file
// path (`.../<timestamp>_<uuid>.jsonl` → `<uuid>`). Fork/clone children
// reference their origin this way; unparseable values yield "" (no link).
func parentIDFromPath(p string) string {
	base := strings.TrimSuffix(filepath.Base(p), ".jsonl")
	if i := strings.LastIndex(base, "_"); i >= 0 && i+1 < len(base) {
		return base[i+1:]
	}
	return ""
}

// firstText extracts plain text from a user message content payload (string
// or [{type:text,text}...]), collapsed to one line for untitled sessions.
func firstText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return cleanTitle(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(b.Text)
		}
	}
	return cleanTitle(sb.String())
}

// cleanTitle collapses whitespace and caps title seeds so one pasted
// traceback cannot bloat the fleet row.
func cleanTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// parseTimeMs parses an ISO 8601 timestamp to epoch millis, falling back
// to fallbackMs (usually the file mtime) on any failure.
func parseTimeMs(ts string, fallbackMs int64) int64 {
	if ts == "" {
		return fallbackMs
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UnixMilli()
	}
	return fallbackMs
}

// titleOf renders the fleet title: explicit /name, else the first user
// text, else an id-based fallback (never empty — the UI renders it raw).
func (d *fileData) titleOf() string {
	if strings.TrimSpace(d.name) != "" {
		return d.name
	}
	if d.firstUser != "" {
		return d.firstUser
	}
	if len(d.id) >= 8 {
		return "pi session " + d.id[:8]
	}
	return "pi session"
}

// projectOf renders the project cell from the session cwd (basename, like
// the db projectLabel worktree fallback); empty cwd yields "global".
func projectOf(cwd string) string {
	if base := filepath.Base(filepath.Clean(cwd)); base != "" && base != "/" && base != "." {
		return base
	}
	return "global"
}

// signalsOf maps the parsed tail onto model.StatusSignals under the given
// recency window (compare db.toRow: RecentActivity is data, DeriveStatus
// stays pure).
//
// A trailing user message counts as in-flight: pi never persists streaming
// ("pending") assistant messages, so an open turn with no completed reply
// sits at user-last for the whole generation — marking it waiting-input
// would paint every active generation amber. Waiting-input for pi therefore
// fires only via the shared DeriveStatus user-role path when a turn opens
// without in-flight work, matching the opencode heuristic's direction.
func (d *fileData) signalsOf(nowMs, windowMs int64) model.StatusSignals {
	relativeMs := nowMs - d.lastTsMs
	if relativeMs < 0 {
		relativeMs = 0
	}
	if windowMs <= 0 {
		windowMs = ports.DefaultActiveWindow.Milliseconds()
	}
	return model.StatusSignals{
		InFlightAssistant: d.openToolUse || d.lastRole == "user",
		HasFreshEvents:    false,
		LatestRole:        d.lastRole,
		HasError:          d.hasError,
		RecentActivity:    relativeMs <= windowMs,
	}
}

// toRow builds the fleet row. Pane/orphan stay zero for RuntimeSource;
// Spark stays nil for the collector; SubagentCount is filled by the
// reader's family pass after all files parse.
func (d *fileData) toRow(nowMs, windowMs int64) model.SessionRow {
	signals := d.signalsOf(nowMs, windowMs)
	relativeMs := nowMs - d.lastTsMs
	if relativeMs < 0 {
		relativeMs = 0
	}
	modelLabel := d.lastModel
	if modelLabel == "" {
		modelLabel = "unknown"
	}
	return model.SessionRow{
		ID:        d.id,
		Harness:   model.HarnessPi,
		Title:     d.titleOf(),
		Project:   projectOf(d.cwd),
		Directory: d.cwd,
		Model:     modelLabel,
		Status:    model.DeriveStatus(signals),
		Tokens: model.TokenCounts{
			In:    d.tokensIn,
			Out:   d.tokensOut,
			Total: d.tokensTotal,
		},
		Cost:              d.cost,
		RawCost:           d.cost,
		RawTokensTotal:    d.tokensTotal,
		UpdatedRelativeMs: relativeMs,
	}
}
