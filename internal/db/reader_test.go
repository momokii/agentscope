package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"

	_ "modernc.org/sqlite"
)

var fixedNow = time.UnixMilli(1800000000000)

const fixtureSchema = `
CREATE TABLE project (id TEXT PRIMARY KEY, worktree TEXT, name TEXT);
CREATE TABLE session (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT,
  slug TEXT NOT NULL, directory TEXT NOT NULL, title TEXT NOT NULL, version TEXT NOT NULL,
  share_url TEXT, summary_additions INTEGER, summary_deletions INTEGER, summary_files INTEGER,
  summary_diffs TEXT, revert TEXT, permission TEXT,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL,
  time_compacting INTEGER, time_archived INTEGER, workspace_id TEXT, path TEXT,
  agent TEXT, model TEXT, cost REAL DEFAULT 0 NOT NULL,
  tokens_input INTEGER DEFAULT 0 NOT NULL, tokens_output INTEGER DEFAULT 0 NOT NULL,
  tokens_reasoning INTEGER DEFAULT 0 NOT NULL, tokens_cache_read INTEGER DEFAULT 0 NOT NULL,
  tokens_cache_write INTEGER DEFAULT 0 NOT NULL, metadata TEXT);
CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);
CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);
CREATE TABLE event (id TEXT PRIMARY KEY, aggregate_id TEXT NOT NULL,
  seq INTEGER NOT NULL, type TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX message_session_time_created_id_idx ON message (session_id,time_created,id);
CREATE INDEX part_session_idx ON part (session_id);
-- NOTE: the live schema carries UNIQUE(event.aggregate_id, event.seq), but
-- duplicate (aggregate_id,seq) rows were observed on the real database
-- (research U5). The fixture deliberately relaxes the index so the reader's
-- defensive dedupe path is covered.
CREATE INDEX event_aggregate_seq_idx ON event (aggregate_id,seq);
CREATE TABLE todo (session_id TEXT NOT NULL, content TEXT NOT NULL,
  status TEXT NOT NULL, priority TEXT NOT NULL, position INTEGER NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL,
  PRIMARY KEY (session_id, position));
CREATE INDEX todo_session_idx ON todo (session_id);
`

func openFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "fixture.db")+"?mode=rwc")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(fixtureSchema); err != nil {
		t.Fatalf("fixture schema: %v", err)
	}
	return db
}

func fixtureReader(db *sql.DB) *Reader {
	return &Reader{
		db:             db,
		now:            func() time.Time { return fixedNow },
		activeWindowMs: ports.DefaultActiveWindow.Milliseconds(),
	}
}

func seedProject(t *testing.T, db *sql.DB, id, worktree, name string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO project (id, worktree, name) VALUES (?,?,?)`, id, worktree, name); err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

type sessionSeed struct {
	id         string
	projectID  string
	parentID   string // "" = NULL (top-level)
	title      string
	directory  string
	modelJSON  string
	nullModel  bool // insert SQL NULL (observed live) instead of modelJSON
	cost       float64
	in, out    int64
	reas, cr   int64
	updatedAgo int64 // ms before fixedNow
	compacting bool
	archived   bool
}

func seedSession(t *testing.T, db *sql.DB, s sessionSeed) {
	t.Helper()
	var parent, compacting, archived, modelVal any
	if s.parentID != "" {
		parent = s.parentID
	}
	if !s.nullModel {
		modelVal = s.modelJSON
	}
	if s.compacting {
		compacting = fixedNow.UnixMilli()
	}
	if s.archived {
		archived = fixedNow.UnixMilli()
	}
	updated := fixedNow.UnixMilli() - s.updatedAgo
	if _, err := db.Exec(
		`INSERT INTO session (id, project_id, parent_id, slug, directory, title, version,
		  time_created, time_updated, time_compacting, time_archived, model, cost,
		  tokens_input, tokens_output, tokens_reasoning, tokens_cache_read)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.id, s.projectID, parent, "slug-"+s.id, s.directory, s.title, "v",
		updated-1000, updated, compacting, archived, modelVal, s.cost,
		s.in, s.out, s.reas, s.cr); err != nil {
		t.Fatalf("seed session %s: %v", s.id, err)
	}
}

func seedMessage(t *testing.T, db *sql.DB, id, sessionID string, created int64, data string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES (?,?,?,?,?)`, id, sessionID, created, created, data); err != nil {
		t.Fatalf("seed message %s: %v", id, err)
	}
}

func seedPart(t *testing.T, db *sql.DB, id, msgID, sessionID string, updated int64, data string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES (?,?,?,?,?,?)`, id, msgID, sessionID, updated, updated, data); err != nil {
		t.Fatalf("seed part %s: %v", id, err)
	}
}

func seedEvent(t *testing.T, db *sql.DB, id, aggID string, seq int64, typ string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO event (id, aggregate_id, seq, type, data) VALUES (?,?,?,?,'{}')`,
		id, aggID, seq, typ); err != nil {
		t.Fatalf("seed event %s: %v", id, err)
	}
}

const (
	assistantDone     = `{"role":"assistant","time":{"created":1,"completed":2},"providerID":"opencode","modelID":"m1"}`
	assistantInflight = `{"role":"assistant","time":{"created":3},"providerID":"opencode","modelID":"m1"}`
	userMsg           = `{"role":"user","time":{"created":4}}`
	errorMsg          = `{"role":"assistant","time":{"created":1,"completed":2},"error":{"name":"MessageAbortedError"}}`
	toolRunning       = `{"type":"tool","tool":"bash","state":{"status":"running"}}`
	toolCompleted     = `{"type":"tool","tool":"bash","state":{"status":"completed"}}`
	textPart          = `{"type":"text"}`
)

func findRow(t *testing.T, rows []model.SessionRow, id string) model.SessionRow {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("session %s missing from %d rows", id, len(rows))
	return model.SessionRow{}
}

func Test_Sessions_status(t *testing.T) {
	base := sessionSeed{
		id: "ses_base", projectID: "global", title: "t",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		updatedAgo: 5000,
	}
	tests := []struct {
		name    string
		seed    func(t *testing.T, db *sql.DB)
		want    model.SessionStatus
		session string
	}{
		{
			name: "running when assistant message in flight",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", fixedNow.UnixMilli()-1000, assistantInflight)
			},
			want: model.SessionRunning, session: "ses_base",
		},
		{
			name: "running when tool part running",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", 1, assistantDone)
				seedPart(t, db, "prt_1", "msg_1", "ses_base", 2, toolRunning)
			},
			want: model.SessionRunning, session: "ses_base",
		},
		{
			name: "idle on seed tick with no fresh events",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", 1, assistantDone)
				seedPart(t, db, "prt_1", "msg_1", "ses_base", 2, toolCompleted)
				// No event rows: the seed tick reports no fresh events
				// (delta-only semantics), so a quiet session derives idle.
			},
			want: model.SessionIdle, session: "ses_base",
		},
		{
			name: "idle when turn completed and no signals",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", 1, assistantDone)
				seedPart(t, db, "prt_1", "msg_1", "ses_base", 2, textPart)
				seedEvent(t, db, "ev_1", "ses_base", 7, "session.updated.1")
			},
			want: model.SessionIdle, session: "ses_base",
		},
		{
			name: "waiting-input when user spoke last",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", 1, assistantDone)
				seedMessage(t, db, "msg_2", "ses_base", 4, userMsg)
			},
			want: model.SessionWaitingInput, session: "ses_base",
		},
		{
			name: "error when error union present",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				seedSession(t, db, base)
				seedMessage(t, db, "msg_1", "ses_base", 1, errorMsg)
			},
			want: model.SessionError, session: "ses_base",
		},
		{
			name: "archived wins over running signals",
			seed: func(t *testing.T, db *sql.DB) {
				seedProject(t, db, "global", "/", "")
				s := base
				s.archived = true
				seedSession(t, db, s)
				seedMessage(t, db, "msg_1", "ses_base", 1, assistantInflight)
				seedPart(t, db, "prt_1", "msg_1", "ses_base", 2, toolRunning)
			},
			want: model.SessionArchived, session: "ses_base",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: a fixture db seeded for one status case
			db := openFixture(t)
			tt.seed(t, db)
			r := fixtureReader(db)

			// When: reading the snapshot
			rows, err := r.Sessions(context.Background())

			// Then: exactly the derived status
			if err != nil {
				t.Fatalf("Sessions: %v", err)
			}
			got := findRow(t, rows, tt.session)
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
			if !got.Status.IsValid() {
				t.Errorf("status %q fails IsValid", got.Status)
			}
			if got.Pane != (model.PaneInfo{}) {
				t.Errorf("pane must stay zero for db rows, got %+v", got.Pane)
			}
			if got.Orphan {
				t.Errorf("orphan must stay false for db rows")
			}
		})
	}
}

func Test_Sessions_rolls_up_subagent_cost_and_tokens(t *testing.T) {
	// Given: a parent with own usage plus two children via parent_id
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_parent", projectID: "global", title: "p",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		cost: 1.5, in: 100, out: 50, reas: 10, cr: 20, updatedAgo: 1000})
	seedSession(t, db, sessionSeed{id: "ses_c1", projectID: "global", parentID: "ses_parent",
		title: "c1", directory: "/tmp", cost: 0.5, in: 10, out: 5, updatedAgo: 900})
	seedSession(t, db, sessionSeed{id: "ses_c2", projectID: "global", parentID: "ses_parent",
		title: "c2", directory: "/tmp", cost: 0.25, in: 4, out: 1, reas: 2, cr: 3, updatedAgo: 800})
	seedMessage(t, db, "msg_1", "ses_parent", 1, assistantDone)
	r := fixtureReader(db)

	// When
	rows, err := r.Sessions(context.Background())

	// Then: 	parent In = 100+20(cache_read); c1 In = 10; c2 In = 4+3.
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	parent := findRow(t, rows, "ses_parent")
	if parent.Tokens.In != 137 || parent.Tokens.Out != 56 || parent.Tokens.Total != 205 {
		t.Errorf("parent tokens = %+v, want {In:137 Out:56 Total:205}", parent.Tokens)
	}
	if parent.Cost != 2.25 {
		t.Errorf("parent cost = %v, want 2.25", parent.Cost)
	}
	if parent.SubagentCount != 2 {
		t.Errorf("parent subagents = %d, want 2", parent.SubagentCount)
	}
	child := findRow(t, rows, "ses_c1")
	if child.Tokens.In != 10 || child.Tokens.Out != 5 || child.Cost != 0.5 {
		t.Errorf("child row must keep own usage, got %+v cost %v", child.Tokens, child.Cost)
	}
}

func Test_Sessions_dedupes_duplicate_event_rows(t *testing.T) {
	// Given: two rows sharing (aggregate_id,seq) plus a fresh-type row,
	// mirroring the duplicate part-update rows observed live
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_dup", projectID: "global", title: "d",
		directory: "/tmp", updatedAgo: 1000})
	seedMessage(t, db, "msg_1", "ses_dup", 1, assistantDone)
	if _, err := db.Exec(
		`INSERT INTO event (id, aggregate_id, seq, type, data) VALUES
		 ('ev_a','ses_dup',3,'message.part.updated.1','{}'),
		 ('ev_b','ses_dup',3,'message.part.updated.1','{}'),
		 ('ev_c','ses_dup',4,'session.updated.1','{}')`); err != nil {
		t.Fatalf("seed duplicate events: %v", err)
	}
	r := fixtureReader(db)

	// When: the seed tick runs (delta-only: pre-existing tails are history)
	rows, err := r.Sessions(context.Background())

	// Then: seed tick carries no fresh events, duplicates harmless
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if got := findRow(t, rows, "ses_dup"); got.Status != model.SessionIdle {
		t.Errorf("seed status = %q, want idle (no fresh events on seed tick)", got.Status)
	}

	// When: genuinely new fresh rows arrive and the tick re-runs
	seedEvent(t, db, "ev_d", "ses_dup", 5, "message.part.updated.1")
	rows, err = r.Sessions(context.Background())

	// Then: the delta tick marks the touched aggregate running
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if got := findRow(t, rows, "ses_dup"); got.Status != model.SessionRunning {
		t.Errorf("delta status = %q, want running", got.Status)
	}
}

func Test_Sessions_fills_content_fields(t *testing.T) {
	// Given: sessions covering model/project label variants
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedProject(t, db, "p1", "/home/u/proj-foo", "")
	seedProject(t, db, "p2", "/home/u/bar", "Named")
	seedSession(t, db, sessionSeed{id: "ses_a", projectID: "global", title: "A",
		directory: "/tmp", modelJSON: `{"id":"m1","providerID":"opencode"}`,
		cost: 0.1, in: 90, out: 10, cr: 10, updatedAgo: 2000})
	seedSession(t, db, sessionSeed{id: "ses_b", projectID: "p1", title: "B",
		directory: "/tmp", modelJSON: "", updatedAgo: 1000})
	seedMessage(t, db, "msg_b", "ses_b", 1,
		`{"role":"assistant","time":{"created":1,"completed":2},"providerID":"zai","modelID":"glm-5"}`)
	seedSession(t, db, sessionSeed{id: "ses_c", projectID: "p2", title: "C",
		directory: "/tmp", modelJSON: "", updatedAgo: 1000})
	// NULL model column (observed live) with no messages: Model stays "".
	seedSession(t, db, sessionSeed{id: "ses_d", projectID: "global", title: "D",
		directory: "/tmp", nullModel: true, updatedAgo: 500})
	r := fixtureReader(db)

	// When
	rows, err := r.Sessions(context.Background())

	// Then
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	a := findRow(t, rows, "ses_a")
	if a.Model != "opencode/m1" {
		t.Errorf("model = %q, want opencode/m1", a.Model)
	}
	if a.Project != "global" {
		t.Errorf("project = %q, want global", a.Project)
	}
	if a.Tokens.In != 100 || a.Tokens.Out != 10 || a.Tokens.Total != 110 {
		t.Errorf("tokens = %+v, want {100 10 110}", a.Tokens)
	}
	if a.UpdatedRelativeMs != 2000 {
		t.Errorf("updated_relative_ms = %d, want 2000", a.UpdatedRelativeMs)
	}
	b := findRow(t, rows, "ses_b")
	if b.Model != "zai/glm-5" {
		t.Errorf("model fallback = %q, want zai/glm-5", b.Model)
	}
	if b.Project != "proj-foo" {
		t.Errorf("project = %q, want proj-foo (worktree basename)", b.Project)
	}
	c := findRow(t, rows, "ses_c")
	if c.Project != "Named" {
		t.Errorf("project = %q, want Named", c.Project)
	}
	d := findRow(t, rows, "ses_d")
	if d.Model != "" {
		t.Errorf("NULL model with no messages: model = %q, want empty", d.Model)
	}
}

func Test_PollState_watermark(t *testing.T) {
	// Given: a fixture with sessions and events
	db := openFixture(t)
	seedProject(t, db, "global", "/", "")
	seedSession(t, db, sessionSeed{id: "ses_1", projectID: "global", title: "x",
		directory: "/tmp", updatedAgo: 7000})
	seedEvent(t, db, "ev_1", "ses_1", 1, "session.created.1")
	seedEvent(t, db, "ev_2", "ses_1", 2, "session.updated.1")
	r := fixtureReader(db)

	// When
	lastUpdateMs, lastRowID, err := r.PollState(context.Background())

	// Then: watermarks equal the maxima
	if err != nil {
		t.Fatalf("PollState: %v", err)
	}
	if want := fixedNow.UnixMilli() - 7000; lastUpdateMs != want {
		t.Errorf("lastUpdateMs = %d, want %d", lastUpdateMs, want)
	}
	if lastRowID != 2 {
		t.Errorf("lastEventRowID = %d, want 2 (MAX rowid)", lastRowID)
	}
}

func Test_PollState_empty_db(t *testing.T) {
	// Given: schema only, no rows
	db := openFixture(t)
	r := fixtureReader(db)

	// When
	lastUpdateMs, lastRowID, err := r.PollState(context.Background())

	// Then: zero watermarks, no error
	if err != nil {
		t.Fatalf("PollState: %v", err)
	}
	if lastUpdateMs != 0 || lastRowID != 0 {
		t.Errorf("empty watermarks = (%d,%d), want (0,0)", lastUpdateMs, lastRowID)
	}
}

// TestLiveProbe opens the REAL <DATA_DIR>/opencode.db read-only, prints the
// row count plus one SessionRow as JSON, and exits. Env-gated so normal
// `go test` never touches the live database: OPENDASH_LIVE=1 go test
// ./internal/db/ -run TestLiveProbe -v
func TestLiveProbe(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") == "" {
		t.Skip("OPENDASH_LIVE unset: live probe stays off the real db")
	}
	cfg := ports.LoadConfigFromEnv()
	r, err := New(cfg.DataDir)
	if err != nil {
		t.Fatalf("open live db: %v", err)
	}
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	lastUpdateMs, lastRowID, err := r.PollState(ctx)
	if err != nil {
		t.Fatalf("live PollState: %v", err)
	}
	rows, err := r.Sessions(ctx)
	if err != nil {
		t.Fatalf("live Sessions: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("live db returned zero sessions")
	}
	one, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatalf("marshal probe row: %v", err)
	}
	t.Logf("PROBE sessions=%d lastUpdateMs=%d lastEventRowID=%d row0=%s",
		len(rows), lastUpdateMs, lastRowID, one)
}
