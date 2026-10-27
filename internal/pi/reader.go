package pi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// sessionSuffix is the only filename shape the reader opens. Anything else
// under the store root (lock files, trash, future formats) is ignored —
// and auth.json can never match it (negative-tested).
const sessionSuffix = ".jsonl"

// maxSessionBytes caps one session file read. The observed store is tiny
// (1.4 MB total); the cap only stops a pathological file from stalling a
// tick. Oversized files are skipped in isolation like corrupt ones.
const maxSessionBytes = 64 << 20

// Reader implements ports.PiReader over a pi JSONL session store.
// Zero value is not usable; construct with New.
type Reader struct {
	dir string
	now func() time.Time
	// activeWindowMs is the status recency gate (ports.Config.ActiveWindow):
	// only sessions with entry activity inside the window may derive
	// running/waiting-input/error. Non-positive means the default.
	activeWindowMs int64

	// Cache: per-path parse results keyed by mtime+size, so unchanged
	// files cost one stat per tick. Guarded by mu; Sessions drives it
	// sequentially.
	mu    sync.Mutex
	cache map[string]cachedFile
}

// cachedFile is one parsed session file plus the identity that proves it
// is still current.
type cachedFile struct {
	mtime int64
	size  int64
	data  fileData
}

// Compile-time seam check: Reader must satisfy the frozen contract exactly.
var _ ports.PiReader = (*Reader)(nil)

// New opens the pi session store at dir (no I/O yet — the first Sessions
// call scans). Use NewWithWindow to override the status recency window
// (production wires ports.Config.ActiveWindow).
func New(dir string) *Reader {
	return NewWithWindow(dir, ports.DefaultActiveWindow.Milliseconds())
}

// NewWithWindow is New with an explicit status recency window in
// milliseconds. Non-positive falls back to the default.
func NewWithWindow(dir string, activeWindowMs int64) *Reader {
	if activeWindowMs <= 0 {
		activeWindowMs = ports.DefaultActiveWindow.Milliseconds()
	}
	return &Reader{
		dir:            dir,
		now:            time.Now,
		activeWindowMs: activeWindowMs,
		cache:          make(map[string]cachedFile),
	}
}

// PiSessions returns one SessionRow per session file (Harness=pi), newest
// activity first. A missing/unreadable store root yields an error the
// collector treats as "pi disabled" (see ports.PiReader); single corrupt,
// oversized, or unreadable files are skipped in isolation with the rest of
// the tick intact. Deleted files drop out of the cache and the fleet.
func (r *Reader) PiSessions(ctx context.Context) ([]model.SessionRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := r.listSessionFiles()
	if err != nil {
		return nil, err
	}
	nowMs := r.now().UnixMilli()
	datas := make([]fileData, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		seen[p] = struct{}{}
		d, ok := r.cachedOrParse(p)
		if !ok {
			continue
		}
		datas = append(datas, d)
	}
	r.evictUnseen(seen)

	rows := make([]model.SessionRow, 0, len(datas))
	for _, d := range datas {
		rows = append(rows, d.toRow(nowMs, r.activeWindowMs))
	}
	applyForkChildren(rows, datas)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].UpdatedRelativeMs < rows[j].UpdatedRelativeMs
	})
	return rows, nil
}

// listSessionFiles walks the store root one level per slug directory and
// returns every *.jsonl path, sorted for deterministic ticks. A missing
// root is not an error here — NewReader-style constructors in db fail fast,
// but pi is optional: the collector disables the merge on error.
func (r *Reader) listSessionFiles() ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("pi: read store %s: %w", r.dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(r.dir, e.Name())
		files, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), sessionSuffix) {
				continue
			}
			out = append(out, filepath.Join(sub, f.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// cachedOrParse returns the cached parse for path when mtime+size match,
// else (re)parses the file. Unparseable files report ok=false and are
// skipped; the cache never stores failures, so a file fixed on disk
// recovers on the next tick.
func (r *Reader) cachedOrParse(path string) (fileData, bool) {
	st, err := os.Stat(path)
	if err != nil {
		r.mu.Lock()
		delete(r.cache, path)
		r.mu.Unlock()
		return fileData{}, false
	}
	if st.Size() > maxSessionBytes {
		return fileData{}, false
	}
	mtime := st.ModTime().UnixNano()
	size := st.Size()
	r.mu.Lock()
	if c, ok := r.cache[path]; ok && c.mtime == mtime && c.size == size {
		r.mu.Unlock()
		return c.data, true
	}
	r.mu.Unlock()
	d, err := parseSessionFile(path, st.ModTime().UnixMilli())
	if err != nil {
		return fileData{}, false
	}
	r.mu.Lock()
	r.cache[path] = cachedFile{mtime: mtime, size: size, data: d}
	r.mu.Unlock()
	return d, true
}

// evictUnseen drops cache entries for files that vanished from the store
// (deleted sessions leave the fleet on the next tick).
func (r *Reader) evictUnseen(seen map[string]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for p := range r.cache {
		if _, ok := seen[p]; !ok {
			delete(r.cache, p)
		}
	}
}

// applyForkChildren records direct fork/clone child counts (parentSession
// path refs) and keeps child rows in the fleet — unlike db.rollUpSubagents
// there is no token/cost roll-up: each pi session file already carries its
// own full usage, so summing a child into its parent would double-count.
func applyForkChildren(rows []model.SessionRow, datas []fileData) {
	byID := make(map[string]int, len(rows))
	for i := range rows {
		byID[rows[i].ID] = i
	}
	counts := make(map[string]int)
	for _, d := range datas {
		if d.parentID == "" || d.parentID == d.id {
			continue
		}
		if _, ok := byID[d.parentID]; !ok {
			continue
		}
		counts[d.parentID]++
	}
	for i := range rows {
		rows[i].SubagentCount = counts[rows[i].ID]
	}
}
