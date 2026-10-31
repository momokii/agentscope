package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"opencode-dashboard/internal/db"
	"opencode-dashboard/internal/model"
)

// This file implements the export-shaped drill-down for pi sessions
// (Wave 6 P6): GET /api/sessions/:id/export serves the session plus its
// direct fork/clone children in the exact `opencode export` wire shape the
// SPA already renders (Transcript/Turns/Timeline/Tools/Cost/Raw tabs), so
// the frontend needs no pi-specific branch.
//
// Mapping notes (every lossy choice is deliberate and documented):
//   - pi has no separate tool-result *message* lane in the UI contract:
//     toolCall blocks merge with their later toolResult entries (matched on
//     the most recent unmatched callId) into one tool part. toolResults with
//     no matching call are dropped — the SPA drops non-user/assistant
//     messages anyway, so nothing renderable is lost.
//   - assistant error turns (errorMessage) have no message-level renderer in
//     the SPA (parity with the opencode path, which has none either): the
//     turn renders its thinking/text content and the fleet status chip
//     carries the error state.
//   - compaction/branch_summary entries are not rendered as messages; their
//     usage still counts toward session totals (same as the fleet roll-up).
//   - unknown entry types are ignored; a corrupt file fails the export
//     (the fleet path skips the file — a corrupt session is absent from
//     both, never half-rendered).
//   - completed timestamps: a message completes at the next entry's
//     timestamp; the final message completes at its own timestamp only for
//     terminal stop reasons, else it stays open (in-flight turn).

// toolCallBuilder accumulates one tool invocation across the assistant
// message that started it and the later toolResult message that finishes
// it. Emission order is preserved by the owning exportMessage's order
// slice (first-seen), so the fleet is deterministic tick to tick.
type toolCallBuilder struct {
	callID string
	name   string
	args   json.RawMessage
	msgID  string
	msgTs  int64
	// partIdx is the slot in the owning message's Parts slice; written at
	// finish() time (after results/turn-end fix status and output).
	partIdx int
	// Filled by finish() when the toolResult arrives.
	output  string
	isError bool
	done    bool
	status  string
	startTs *int64
	endTs   int64
}

// exportMessage is one in-progress ExportMessage plus (for assistant
// messages) its open tool calls and the stop reason that gates the
// completed timestamp.
type exportMessage struct {
	msg        model.ExportMessage
	tools      map[string]*toolCallBuilder
	order      []*toolCallBuilder
	stopReason string
}

// SessionExport returns the export-shaped drill-down for one pi session
// plus its direct children (newest-first, capped like the db path). Unknown
// ids report db.ErrNotFound so the hub maps them to 404 identically.
func (r *Reader) SessionExport(ctx context.Context, id string) (model.SessionExport, error) {
	path, err := r.resolveSessionPath(ctx, id)
	if err != nil {
		return model.SessionExport{}, err
	}
	mk, err := r.buildSession(ctx, path)
	if err != nil {
		return model.SessionExport{}, err
	}
	children, err := r.exportChildren(ctx, mk.id)
	if err != nil {
		return model.SessionExport{}, err
	}
	return model.SessionExport{Session: mk.msg, Children: children}, nil
}

// exportChildCap mirrors the db path's exportChildLimit: direct children
// only, newest-first, bounded per view.
const exportChildCap = 100

// resolveSessionPath maps a session id to its file: the filename carries
// the id as its uuid suffix (<ts>_<uuid>.jsonl). Candidates are verified
// by reading the header so a renamed file can never impersonate a session.
func (r *Reader) resolveSessionPath(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("pi: export %q: %w", id, db.ErrNotFound)
	}
	paths, err := r.listSessionFiles()
	if err != nil {
		return "", err
	}
	suffix := "_" + id + sessionSuffix
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !strings.HasSuffix(filepath.Base(p), suffix) {
			continue
		}
		header, err := readSessionHeader(p)
		if err != nil {
			continue
		}
		if header.id == id {
			return p, nil
		}
	}
	return "", fmt.Errorf("pi: export %q: %w", id, db.ErrNotFound)
}

// readSessionHeader decodes only the first (header) line of a session file.
func readSessionHeader(path string) (fileHeader, error) {
	f, err := os.Open(path)
	if err != nil {
		return fileHeader{}, err
	}
	defer f.Close()
	var e piEntry
	dec := json.NewDecoder(f)
	if err := dec.Decode(&e); err != nil {
		return fileHeader{}, fmt.Errorf("pi: header %s: %w", path, err)
	}
	return fileHeader{id: e.ID, cwd: e.CWD, parentSession: e.ParentSess, timestamp: e.Timestamp}, nil
}

type fileHeader struct {
	id            string
	cwd           string
	parentSession string
	timestamp     string
}

// sessionBuild is the assembled export payload plus the file metadata the
// info block needs.
type sessionBuild struct {
	msg model.ExportSession
	id  string
}

// buildSession streams one file into an export-shaped session.
func (r *Reader) buildSession(ctx context.Context, path string) (sessionBuild, error) {
	f, err := os.Open(path)
	if err != nil {
		return sessionBuild{}, fmt.Errorf("pi: export open %s: %w", path, err)
	}
	defer f.Close()

	b := &sessionBuilder{sessionID: "", msgs: nil, data: fileData{}}
	dec := json.NewDecoder(f)
	for {
		if err := ctx.Err(); err != nil {
			return sessionBuild{}, err
		}
		var e piEntry
		if err := dec.Decode(&e); err != nil {
			break
		}
		if err := b.apply(e); err != nil {
			return sessionBuild{}, fmt.Errorf("pi: export %s: %w", path, err)
		}
	}
	return b.finish()
}

// exportChildren returns direct fork/clone children (parentSession resolves
// to id), newest-first, capped. Children of children are NOT included (same
// one-level contract as the db path).
func (r *Reader) exportChildren(ctx context.Context, id string) ([]model.ExportSession, error) {
	paths, err := r.listSessionFiles()
	if err != nil {
		return nil, err
	}
	type child struct {
		path string
		ts   int64
	}
	var found []child
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := readSessionHeader(p)
		if err != nil {
			continue
		}
		if parentIDFromPath(header.parentSession) == id {
			found = append(found, child{path: p, ts: parseTimeMs(header.timestamp, 0)})
		}
	}
	// Newest-first by header timestamp for a stable drill-down order.
	for i := 0; i < len(found); i++ {
		for j := i + 1; j < len(found); j++ {
			if found[j].ts > found[i].ts {
				found[i], found[j] = found[j], found[i]
			}
		}
	}
	if len(found) > exportChildCap {
		found = found[:exportChildCap]
	}
	out := make([]model.ExportSession, 0, len(found))
	for _, c := range found {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mk, err := r.buildSession(ctx, c.path)
		if err != nil {
			return nil, err
		}
		out = append(out, mk.msg)
	}
	return out, nil
}
