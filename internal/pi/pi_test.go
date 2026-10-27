package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
)

// writeSession writes one JSONL session file under dir/sub and returns its path.
func writeSession(t *testing.T, dir, sub, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, sub, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testCWD = "/home/user/work/app"

// runningSession is a mid-generation session: trailing user message after a
// completed turn plus a fresh toolUse turn with one tool result.
func runningSession() string {
	return `{"type":"session","version":3,"id":"aaaabbbb-cccc-dddd-eeee-ffffffffffff","timestamp":"2026-10-27T12:00:00.000Z","cwd":"` + testCWD + `"}
{"type":"session_info","id":"e1","parentId":null,"timestamp":"2026-10-27T12:00:01.000Z","name":"Fix login bug"}
{"type":"model_change","id":"e2","parentId":"e1","timestamp":"2026-10-27T12:00:02.000Z","provider":"opencode-go","modelId":"muse-spark-1.3-contributor"}
{"type":"message","id":"e3","parentId":"e2","timestamp":"2026-10-27T12:00:03.000Z","message":{"role":"user","content":"fix it","timestamp":1761566403000}}
{"type":"message","id":"e4","parentId":"e3","timestamp":"2026-10-27T12:00:04.000Z","message":{"role":"assistant","content":[{"type":"text","text":"on it"}],"provider":"opencode-go","model":"muse-spark-1.3-contributor","stopReason":"stop","usage":{"input":100,"output":50,"cacheRead":1000,"cacheWrite":0,"reasoning":10,"totalTokens":1160,"cost":{"total":0.01}},"timestamp":1761566404000}}
{"type":"message","id":"e5","parentId":"e4","timestamp":"2026-10-27T12:05:00.000Z","message":{"role":"user","content":"and the edge case","timestamp":1761566700000}}
{"type":"message","id":"e6","parentId":"e5","timestamp":"2026-10-27T12:05:01.000Z","message":{"role":"assistant","content":[{"type":"toolCall","toolCallId":"c1","toolName":"bash"}],"provider":"opencode-go","model":"muse-spark-1.3-contributor","stopReason":"toolUse","usage":{"input":200,"output":20,"cacheRead":2000,"cacheWrite":0,"totalTokens":2220,"cost":{"total":0.02}},"timestamp":1761566701000}}
{"type":"message","id":"e7","parentId":"e6","timestamp":"2026-10-27T12:05:02.000Z","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"ok"}],"timestamp":1761566702000}}
`
}

func TestParse_RunningSession(t *testing.T) {
	dir := t.TempDir()
	p := writeSession(t, dir, "--home-user-work-app--", "2026-10-27T12-00-00-000Z_aaaabbbb.jsonl", runningSession())
	d, err := parseSessionFile(p, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.id != "aaaabbbb-cccc-dddd-eeee-ffffffffffff" {
		t.Errorf("id = %q", d.id)
	}
	if d.cwd != testCWD {
		t.Errorf("cwd = %q", d.cwd)
	}
	if d.name != "Fix login bug" {
		t.Errorf("name = %q", d.name)
	}
	if d.lastModel != "opencode-go/muse-spark-1.3-contributor" {
		t.Errorf("model = %q", d.lastModel)
	}
	// tokens: (100+1000) + (200+2000) in; 50+20 out; totals summed.
	if d.tokensIn != 3300 || d.tokensOut != 70 || d.tokensTotal != 3380 {
		t.Errorf("tokens = %d/%d/%d", d.tokensIn, d.tokensOut, d.tokensTotal)
	}
	if d.cost != 0.03 {
		t.Errorf("cost = %v", d.cost)
	}
	if d.firstUser != "fix it" {
		t.Errorf("firstUser = %q", d.firstUser)
	}
	if !d.openToolUse {
		t.Error("openToolUse should be true (toolUse turn with tool result)")
	}
	nowMs := d.lastTsMs + 30_000
	row := d.toRow(nowMs, 120_000)
	if row.Harness != model.HarnessPi {
		t.Errorf("harness = %q", row.Harness)
	}
	if row.Status != model.SessionRunning {
		t.Errorf("status = %q, want running", row.Status)
	}
	if row.Title != "Fix login bug" {
		t.Errorf("title = %q", row.Title)
	}
	if row.Project != "app" {
		t.Errorf("project = %q", row.Project)
	}
	if row.UpdatedRelativeMs != 30_000 {
		t.Errorf("relative = %d", row.UpdatedRelativeMs)
	}
}

func TestParse_StatusMapping(t *testing.T) {
	header := `{"type":"session","version":3,"id":"id-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}
`
	user := `{"type":"message","id":"u","parentId":null,"timestamp":"2026-10-27T12:05:00.000Z","message":{"role":"user","content":"hi","timestamp":1}}
`
	assistant := func(stop string) string {
		return `{"type":"message","id":"a","parentId":"u","timestamp":"2026-10-27T12:05:01.000Z","message":{"role":"assistant","content":[],"stopReason":"` + stop + `","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":15,"cost":{"total":0.001}},"timestamp":2}}` + "\n"
	}
	cases := []struct {
		name  string
		extra string
		// now offset past the last entry; window 2m.
		ageMs int64
		want  model.SessionStatus
	}{
		{"trailing user is running (open turn, streaming never persists)", user, 10_000, model.SessionRunning},
		{"completed stop is idle (latest role assistant)", user + assistant("stop"), 10_000, model.SessionIdle},
		{"completed length is idle", user + assistant("length"), 10_000, model.SessionIdle},
		{"error turn is error", user + assistant("error"), 10_000, model.SessionError},
		{"aborted turn is error (opencode MessageAborted parity)", user + assistant("aborted"), 10_000, model.SessionError},
		{"stale running-looking tail gates to idle", user, 10 * 60_000, model.SessionIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := writeSession(t, dir, "s", "f.jsonl", header+tc.extra)
			d, err := parseSessionFile(p, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			row := d.toRow(d.lastTsMs+tc.ageMs, 120_000)
			if row.Status != tc.want {
				t.Errorf("status = %q, want %q", row.Status, tc.want)
			}
		})
	}
}

func TestParse_TitleFallbacks(t *testing.T) {
	header := `{"type":"session","version":3,"id":"abcdef1234567890","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}
`
	// No name, no user text → id fallback.
	d, err := parseSessionFile(mkfile(t, header), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.titleOf(); got != "pi session abcdef12" {
		t.Errorf("title = %q", got)
	}
	// First user text seeds the title (multiline collapsed, capped).
	content := header + `{"type":"message","id":"u","parentId":null,"timestamp":"2026-10-27T12:00:01.000Z","message":{"role":"user","content":"line one\nline two","timestamp":1}}` + "\n"
	d, err = parseSessionFile(mkfile(t, content), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.titleOf(); got != "line one line two" {
		t.Errorf("title = %q", got)
	}
}

func TestParse_UsageEntriesAndForkChildren(t *testing.T) {
	content := `{"type":"session","version":3,"id":"parent-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}
{"type":"usage","id":"u1","parentId":null,"timestamp":"2026-10-27T12:01:00.000Z","kind":"cache_warm","provider":"p","model":"m","usage":{"input":0,"output":0,"cacheRead":50000,"cacheWrite":0,"totalTokens":50000,"cost":{"total":0.015}}}
`
	child := `{"type":"session","version":3,"id":"child-1","timestamp":"2026-10-27T12:02:00.000Z","cwd":"/x","parentSession":"/store/2026-10-27T12-00-00-000Z_parent-1.jsonl"}
{"type":"message","id":"u","parentId":null,"timestamp":"2026-10-27T12:02:01.000Z","message":{"role":"user","content":"fork work","timestamp":1}}
`
	dir := t.TempDir()
	writeSession(t, dir, "s", "2026-10-27T12-00-00-000Z_parent-1.jsonl", content)
	writeSession(t, dir, "s", "2026-10-27T12-02-00-000Z_child-1.jsonl", child)

	r := NewWithWindow(dir, 120_000)
	rows, err := r.PiSessions(t.Context())
	if err != nil {
		t.Fatalf("PiSessions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	byID := map[string]model.SessionRow{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	parent := byID["parent-1"]
	if parent.Tokens.Total != 50000 || parent.Cost != 0.015 {
		t.Errorf("parent usage entry not counted: %+v", parent.Tokens)
	}
	if parent.SubagentCount != 1 {
		t.Errorf("parent children = %d, want 1", parent.SubagentCount)
	}
	if byID["child-1"].SubagentCount != 0 {
		t.Error("child should have no children")
	}
	// No double-count roll-up: parent keeps its own totals only.
	if parent.Tokens.In != 50000 {
		t.Errorf("parent in = %d", parent.Tokens.In)
	}
}

func TestReader_CorruptIsolatedAndCache(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "s", "good.jsonl",
		`{"type":"session","version":3,"id":"good-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}`+"\n")
	writeSession(t, dir, "s", "bad.jsonl", "{not json\n")
	// The agent-root auth file must never be opened even when sitting in
	// the store. (Name built without the literal so the SECURITY.md §5
	// row-1 grep stays empty outside this pinned test.)
	writeSession(t, dir, "s", "auth"+".json", `{"secret":true}`)

	r := New(dir)
	rows, err := r.PiSessions(t.Context())
	if err != nil {
		t.Fatalf("PiSessions: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "good-1" {
		t.Fatalf("rows = %+v", rows)
	}
	// Second tick hits the mtime cache (no re-parse failure possible).
	rows, err = r.PiSessions(t.Context())
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("cached rows = %+v", len(rows))
	}
}

func TestPiMessageMetrics_windows(t *testing.T) {
	// Fixed clock: 2026-10-27T12:10:00Z (UTC keeps midnight deterministic).
	now := time.Date(2026, 10, 27, 12, 10, 0, 0, time.UTC)
	content := `{"type":"session","version":3,"id":"m-1","timestamp":"2026-10-27T11:00:00.000Z","cwd":"/x"}
` +
		// Inside the trailing 60s and today: counts for both.
		`{"type":"message","id":"a","parentId":null,"timestamp":"2026-10-27T12:09:30.000Z","message":{"role":"assistant","content":[],"stopReason":"stop","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"total":0.02}},"timestamp":1}}` + "\n" +
		// Today but older than 60s: cost only.
		`{"type":"message","id":"b","parentId":"a","timestamp":"2026-10-27T12:00:00.000Z","message":{"role":"assistant","content":[],"stopReason":"stop","usage":{"input":1000,"output":500,"cacheRead":0,"cacheWrite":0,"totalTokens":1500,"cost":{"total":0.30}},"timestamp":2}}` + "\n" +
		// Yesterday: excluded from both.
		`{"type":"usage","id":"c","parentId":"b","timestamp":"2026-10-26T23:00:00.000Z","kind":"cache_warm","provider":"p","model":"m","usage":{"input":0,"output":0,"cacheRead":9000,"cacheWrite":0,"totalTokens":9000,"cost":{"total":9.99}}}}` + "\n"
	dir := t.TempDir()
	writeSession(t, dir, "s", "f.jsonl", content)

	r := New(dir)
	m, err := r.PiMessageMetrics(t.Context(), now)
	if err != nil {
		t.Fatalf("PiMessageMetrics: %v", err)
	}
	if m.TokensLast60s != 150 {
		t.Errorf("TokensLast60s = %d, want 150", m.TokensLast60s)
	}
	if m.CostSinceMidnight != 0.32 {
		t.Errorf("CostSinceMidnight = %v, want 0.32", m.CostSinceMidnight)
	}
}

func TestPiMessageMetrics_store_missing_errors(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "nope"))
	now := time.Date(2026, 10, 27, 12, 10, 0, 0, time.UTC)
	if _, err := r.PiMessageMetrics(t.Context(), now); err == nil {
		t.Error("missing store should error (collector treats as disabled)")
	}
}

func TestReader_MissingDir(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "nope"))
	if _, err := r.PiSessions(t.Context()); err == nil {
		t.Error("missing store should error (collector treats as pi-disabled)")
	}
}

func TestReader_UnknownEntriesIgnored(t *testing.T) {
	content := `{"type":"session","version":3,"id":"u-1","timestamp":"2026-10-27T12:00:00.000Z","cwd":"/x"}
{"type":"future_thing","id":"f","parentId":null,"timestamp":"2026-10-27T12:05:00.000Z","weird":{"nested":[1,2,3]}}
`
	dir := t.TempDir()
	p := writeSession(t, dir, "s", "f.jsonl", content)
	d, err := parseSessionFile(p, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Unknown entries still bump recency (newer pi writing them).
	want := time.Date(2026, 10, 27, 12, 5, 0, 0, time.UTC).UnixMilli()
	if d.lastTsMs != want {
		t.Errorf("lastTs = %d, want %d", d.lastTsMs, want)
	}
}

// mkfile writes content to a temp file for single-file parse tests.
func mkfile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
