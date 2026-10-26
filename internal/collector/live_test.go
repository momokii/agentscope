package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// liveProbeScript reads the REAL opencode.db through a read-only URI
// (mode=ro: the open itself cannot write, lock, or disturb live TUIs) using
// only indexed aggregates plus one LIMIT-bounded recent slice. stdlib-only:
// the module has no sqlite driver, so the probe shells to python3's stdlib
// sqlite3. This is Manual-QA scaffolding, not the W1-1 reader: status mapping
// is a documented heuristic (archived / recently-updated-implies-running /
// compacting-implies-waiting / else idle) and the slice caps at 500 rows.
// Full-fleet indexed reads with real signal detection belong to W1-1.
const liveProbeScript = `
import json, sqlite3, sys, time
db = sys.argv[1]
con = sqlite3.connect("file:%s?mode=ro" % db, uri=True, timeout=10)
con.execute("PRAGMA query_only = ON")
cur = con.cursor()
cur.execute("SELECT max(time_updated) FROM session")
updated = cur.fetchone()[0] or 0
cur.execute("SELECT max(rowid) FROM event")
evrow = cur.fetchone()[0] or 0
cur.execute("""SELECT id, COALESCE(title,''), COALESCE(directory,''), COALESCE(model,''),
  COALESCE(cost,0), COALESCE(tokens_input,0), COALESCE(tokens_output,0),
  COALESCE(tokens_reasoning,0), COALESCE(tokens_cache_read,0),
  COALESCE(time_updated,0), time_compacting, time_archived, parent_id
  FROM session ORDER BY time_updated DESC LIMIT 500""")
rows = cur.fetchall()
cur.execute("SELECT parent_id, count(*) FROM session WHERE parent_id IS NOT NULL GROUP BY parent_id")
kids = dict(cur.fetchall())
now = int(time.time() * 1000)
out = []
for (sid, title, d, m, cost, ti, to, tr, tc, upd, comp, arch, par) in rows:
    if arch is not None:
        st = "archived"
    elif now - (upd or 0) < 120000:
        st = "running"
    elif comp is not None:
        st = "waiting-input"
    else:
        st = "idle"
    tin = (ti or 0) + (tc or 0)
    tot = tin + (to or 0) + (tr or 0)
    out.append({"id": sid, "title": title, "dir": d, "model": m, "cost": cost,
      "in": tin, "out": to or 0, "total": tot, "upd": upd or 0,
      "status": st, "kids": kids.get(sid, 0)})
print(json.dumps({"updated": updated, "event_rowid": evrow, "now_ms": now, "sessions": out}))
`

type liveSessionJSON struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Dir    string  `json:"dir"`
	Model  string  `json:"model"`
	Cost   float64 `json:"cost"`
	In     int64   `json:"in"`
	Out    int64   `json:"out"`
	Total  int64   `json:"total"`
	Upd    int64   `json:"upd"`
	Status string  `json:"status"`
	Kids   int     `json:"kids"`
}

type liveProbeJSON struct {
	Updated    int64             `json:"updated"`
	EventRowID int64             `json:"event_rowid"`
	NowMs      int64             `json:"now_ms"`
	Sessions   []liveSessionJSON `json:"sessions"`
}

// liveDBReader is the two-tick Manual-QA stand-in for the W1-1 DBReader: it
// hits the real database path read-only and maps rows to SessionRows.
type liveDBReader struct {
	dbPath string
	last   liveProbeJSON
}

func (l *liveDBReader) probe(ctx context.Context) (liveProbeJSON, error) {
	cmd := exec.CommandContext(ctx, "python3", "-", l.dbPath)
	cmd.Stdin = strings.NewReader(liveProbeScript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return liveProbeJSON{}, fmt.Errorf("collector live probe: %w: %s", err, stderr.String())
	}
	var parsed liveProbeJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return liveProbeJSON{}, fmt.Errorf("collector live probe decode: %w", err)
	}
	return parsed, nil
}

func (l *liveDBReader) PollState(ctx context.Context) (int64, int64, error) {
	probed, err := l.probe(ctx)
	if err != nil {
		return 0, 0, err
	}
	l.last = probed
	return probed.Updated, probed.EventRowID, nil
}

func (l *liveDBReader) Sessions(ctx context.Context) ([]model.SessionRow, error) {
	rows := make([]model.SessionRow, 0, len(l.last.Sessions))
	for _, s := range l.last.Sessions {
		status := model.SessionStatus(s.Status)
		if !status.IsValid() {
			status = model.SessionIdle
		}
		rows = append(rows, model.SessionRow{
			ID:                s.ID,
			Title:             s.Title,
			Directory:         s.Dir,
			Model:             s.Model,
			Status:            status,
			Tokens:            model.TokenCounts{In: s.In, Out: s.Out, Total: s.Total},
			Cost:              s.Cost,
			RawCost:           s.Cost,
			RawTokensTotal:    s.Total,
			UpdatedRelativeMs: l.last.NowMs - s.Upd,
			SubagentCount:     s.Kids,
		})
	}
	return rows, nil
}

// SessionDetail keeps liveDBReader satisfying ports.DBReader. The Manual-QA
// probe only ticks the fleet path; drill-down coverage lives in db tests.
func (l *liveDBReader) SessionDetail(_ context.Context, id string) (model.SessionDetail, error) {
	return model.SessionDetail{}, fmt.Errorf("liveDBReader: no detail for %s", id)
}

// MessageMetrics keeps liveDBReader satisfying ports.DBReader. This probe
// shells to a limited python read; message-level metrics have their own
// db-level coverage, so the Manual-QA path reports an honest zero sample.
func (l *liveDBReader) MessageMetrics(_ context.Context, _ time.Time) (ports.MessageMetrics, error) {
	return ports.MessageMetrics{}, nil
}

// liveRuntime is the two-tick Manual-QA stand-in for the W1-2 RuntimeSource:
// it scans /proc for live opencode processes (self excluded) and marks rows
// whose directory matches a live cwd as present, everything else orphan.
// Pane-title EXITED matching and the zellij join belong to W1-2.
type liveRuntime struct{}

// LiveCounts keeps liveRuntime satisfying ports.RuntimeSource. The Manual-QA
// probe never asserts ground-truth counts; the db-reader path is the point.
func (liveRuntime) LiveCounts() (int, int) { return 0, 0 }

// ProcsByDir keeps liveRuntime satisfying ports.RuntimeSource. This stand-in
// does not attribute processes to directories; the real runtime source does.
func (liveRuntime) ProcsByDir() map[string]int { return nil }

func (liveRuntime) Enrich(ctx context.Context, rows []model.SessionRow) ([]model.SessionRow, error) {
	liveCWDs := liveOpencodeCWDs()
	self, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", os.Getpid()))
	for i := range rows {
		if cwd, ok := liveCWDs[rows[i].Directory]; ok && rows[i].Directory != "" {
			rows[i].Pane = model.PaneInfo{CWD: cwd}
			rows[i].Orphan = false
			continue
		}
		if rows[i].Directory == self {
			rows[i].Pane = model.PaneInfo{CWD: self}
			rows[i].Orphan = false
			continue
		}
		rows[i].Orphan = true
	}
	return rows, nil
}

// liveOpencodeCWDs returns the working directories of live (non-zombie,
// non-self) processes whose cmdline names opencode.
func liveOpencodeCWDs() map[string]string {
	out := map[string]string{}
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		cmd := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if !strings.Contains(cmd, "opencode") || strings.Contains(cmd, ".test") {
			continue
		}
		status, err := os.ReadFile(filepath.Join("/proc", e.Name(), "status"))
		if err != nil || strings.Contains(string(status), "State:\tZ") {
			continue
		}
		if cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd")); err == nil {
			out[cwd] = cwd
		}
	}
	return out
}

// Test_Live_TwoTicks wires the REAL read-only db path plus the REAL /proc
// presence probe through the Collector into an in-memory sink for EXACTLY 2
// ticks, then prints both frames. Env-gated: OPENDASH_LIVE=1. Read-only
// against the live db (mode=ro URI + PRAGMA query_only).
func Test_Live_TwoTicks(t *testing.T) {
	if os.Getenv("OPENDASH_LIVE") != "1" {
		t.Skip("set OPENDASH_LIVE=1 to run the live two-tick wiring proof")
	}
	dbPath := os.Getenv("OPENDASH_DB")
	if dbPath == "" {
		dbPath = os.ExpandEnv("$HOME/.local/share/opencode/opencode.db")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("live db not found at %s: %v", dbPath, err)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatalf("python3 required for the read-only probe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	sink := &fakeSink{}
	c, err := NewCollector(&liveDBReader{dbPath: dbPath}, liveRuntime{}, sink, ports.DefaultConfig())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	published1, err := c.Tick(ctx)
	if err != nil {
		t.Fatalf("live tick 1: %v", err)
	}
	time.Sleep(6 * time.Second)
	published2, err := c.Tick(ctx)
	if err != nil {
		t.Fatalf("live tick 2: %v", err)
	}

	got := sink.published()
	fmt.Printf("LIVE tick1 published=%v tick2 published=%v frames=%d\n", published1, published2, len(got))
	for _, snap := range got {
		running, idle := 0, 0
		for _, s := range snap.Sessions {
			if s.Status == model.SessionRunning {
				running++
			}
			if s.Status == model.SessionIdle {
				idle++
			}
		}
		fmt.Printf("LIVE seq=%d sessions=%d running=%d idle=%d stale=%v lastPollMs=%d costToday=%.4f tokensPerHour=%.1f\n",
			snap.Seq, len(snap.Sessions), running, idle, snap.Stale, snap.LastPollMs,
			snap.KPIs.CostToday, snap.KPIs.TokensPerHour)
	}
	if len(got) == 0 {
		t.Fatal("live proof published zero frames")
	}
}
