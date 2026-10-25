package db

import (
	"context"
	"encoding/json"
	"strings"

	"opencode-dashboard/internal/model"
)

// Per-session slice limits. Each bounds one allowlisted query (see the
// allowlist in reader.go).
//
// JSON is decoded in Go on these bounded slices only — never via SQL-side
// json_extract over a session's whole part set: one live session holds 32k
// parts and SQL-side extraction parsed hundreds of MB per tick. The
// recency-ordering rationale for each cap lives in signals_batch.go, where
// the batched fleet pass enforces the same slices.
const (
	messageScanLimit = 20
	partScanLimit    = 50
	eventScanLimit   = 10
)

// Fresh-event type that marks a session as generating (research §4).
// session.updated.1/message.updated.1 rows are deliberately excluded: they
// also fire for idle bookkeeping, which would pin every session running.
const freshEventType = "message.part.updated.1"

// Allowlisted singular queries (see reader.go header). Placeholders only
// ever bind session ids / row limits — no LIKE, no data scans. The fleet
// tick uses the batched variants in signals_batch.go; these serve the
// on-demand detail path (small families only).
const (
	querySessions = `
SELECT s.id, s.project_id, s.parent_id, s.title, s.directory, s.model,
       s.cost, s.tokens_input, s.tokens_output, s.tokens_reasoning,
       s.tokens_cache_read, s.time_created, s.time_updated,
       s.time_compacting, s.time_archived, p.name, p.worktree
  FROM session s LEFT JOIN project p ON p.id = s.project_id
 ORDER BY s.time_updated DESC`

	queryMessageSignals = `
SELECT time_created, data
  FROM message
 WHERE session_id = ?
 ORDER BY time_created DESC, id DESC
 LIMIT ?`

	queryRunningTools = `
SELECT data
  FROM part
 WHERE session_id = ?
 ORDER BY time_updated DESC, id DESC
 LIMIT ?`

	querySessionEvents = `
SELECT aggregate_id, seq, type
  FROM event
 WHERE aggregate_id = ?
 ORDER BY seq DESC
 LIMIT ?`

	queryMaxSessionUpdated = `SELECT COALESCE(MAX(time_updated),0) FROM session`
	queryMaxEventRowID     = `SELECT COALESCE(MAX(rowid),0) FROM event`
)

// messagePayload is the decoded subset of one message.data blob used for
// status signals. Completed is a pointer so absent/null (still generating)
// is distinguishable from a set timestamp. Recent is NOT decoded: the
// caller stamps it from the time_created column against the active window,
// so an ancient aborted message (completed never set, e.g. a killed TUI)
// cannot count as in flight.
type messagePayload struct {
	Role string `json:"role"`
	Time struct {
		Completed *int64 `json:"completed"`
	} `json:"time"`
	Error *struct {
		Name string `json:"name"`
	} `json:"error"`
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Recent     bool   `json:"-"`
}

// partPayload is the decoded subset of one part.data blob: tool running
// state only. Oversized blobs (inline tool output) still parse exactly;
// the per-session LIMIT bounds how many are ever touched.
type partPayload struct {
	Type  string `json:"type"`
	State *struct {
		Status string `json:"status"`
	} `json:"state"`
}

// decodeMessagePayload parses one message.data blob. False means corrupt:
// callers skip the blob so one corrupt row never fails a snapshot or view.
func decodeMessagePayload(data string) (messagePayload, bool) {
	var msg messagePayload
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return messagePayload{}, false
	}
	return msg, true
}

// applyMessagePayload folds one decoded message (newest indicates whether
// it is the session's newest row, ordering newest-first) into the
// DeriveStatus inputs. Shared by the singular and batched paths so both
// compute byte-identical signals. An in-flight candidate counts only when
// the message itself is recent (msg.Recent, stamped by the caller from
// time_created): a still-generating flag on a 70-day-old aborted message is
// stale, not work.
func applyMessagePayload(msg messagePayload, newest bool, signals *model.StatusSignals, modelFallback *string) {
	if newest {
		signals.LatestRole = msg.Role
	}
	if msg.Role == "assistant" && msg.Time.Completed == nil && msg.Recent {
		signals.InFlightAssistant = true
	}
	if msg.Error != nil && msg.Error.Name != "" {
		signals.HasError = true
	}
	if *modelFallback == "" && msg.ProviderID != "" && msg.ModelID != "" {
		*modelFallback = msg.ProviderID + "/" + msg.ModelID
	}
}

// decodePartPayload parses one part.data blob. False means corrupt.
func decodePartPayload(data string) (partPayload, bool) {
	var part partPayload
	if err := json.Unmarshal([]byte(data), &part); err != nil {
		return partPayload{}, false
	}
	return part, true
}

// maybeRunningTool is an exact prefilter for the running-tool verdict: a
// part that decodes to {type:tool, state.status:running} necessarily
// contains the literal `"running"` — neither encoding/json nor serde_json
// \u-escape plain ASCII alphanumerics inside string values — so a blob
// without the substring cannot be running. Blobs WITH it still take the
// full parse: prose or completed output can mention running, and corrupt
// blobs fail the parse either way. Measured 2026-10-25 on the live db:
// 953ms full decode of 67k part blobs vs 70ms prefiltered, same verdict.
func maybeRunningTool(data string) bool {
	return strings.Contains(data, `"running"`)
}

// applyPartPayload folds one decoded part into the DeriveStatus inputs.
func applyPartPayload(part partPayload, signals *model.StatusSignals) {
	if part.Type == "tool" && part.State != nil && part.State.Status == "running" {
		signals.RunningToolParts++
	}
}

// applyEventType folds one event row into the DeriveStatus inputs with
// first-wins dedupe by (aggregate_id,seq): duplicate rows were observed
// live (Lane A U5). Shared by both paths.
func applyEventType(typ string, key eventKey, seen map[eventKey]struct{}, signals *model.StatusSignals) {
	if _, dup := seen[key]; dup {
		return
	}
	seen[key] = struct{}{}
	if typ == freshEventType {
		signals.HasFreshEvents = true
	}
}

// sessionSignals collects the DeriveStatus inputs for one session and the
// newest-first model fallback (providerID/modelID) used when the
// session.model column is empty. Rows with malformed data blobs are skipped.
//
// Used by the on-demand detail path (bounded: small families only; huge
// families switch to batchSignals — see detail.go). The fleet tick always
// uses batchSignals: same decoders, same per-session orderings, ~200
// indexed queries instead of ~5900.
func (r *Reader) sessionSignals(ctx context.Context, sessionID string) (model.StatusSignals, string, error) {
	var signals model.StatusSignals
	modelFallback := ""

	err := withBusyRetry(ctx, "message signals "+sessionID, func() error {
		rows, err := r.db.QueryContext(ctx, queryMessageSignals, sessionID, messageScanLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		nowMs := r.now().UnixMilli()
		first := true
		for rows.Next() {
			var createdMs int64
			var data string
			if err := rows.Scan(&createdMs, &data); err != nil {
				return err
			}
			msg, ok := decodeMessagePayload(data)
			if !ok {
				continue
			}
			msg.Recent = nowMs-createdMs <= r.activeWindowMs
			applyMessagePayload(msg, first, &signals, &modelFallback)
			first = false
		}
		return rows.Err()
	})
	if err != nil {
		return model.StatusSignals{}, "", err
	}

	err = withBusyRetry(ctx, "running tools "+sessionID, func() error {
		rows, err := r.db.QueryContext(ctx, queryRunningTools, sessionID, partScanLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data string
			if err := rows.Scan(&data); err != nil {
				return err
			}
			part, ok := decodePartPayload(data)
			if !ok {
				continue
			}
			applyPartPayload(part, &signals)
		}
		return rows.Err()
	})
	if err != nil {
		return model.StatusSignals{}, "", err
	}

	err = withBusyRetry(ctx, "fresh events "+sessionID, func() error {
		rows, err := r.db.QueryContext(ctx, querySessionEvents, sessionID, eventScanLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := make(map[eventKey]struct{})
		for rows.Next() {
			var aggID string
			var seq int64
			var typ string
			if err := rows.Scan(&aggID, &seq, &typ); err != nil {
				return err
			}
			applyEventType(typ, eventKey{aggregateID: aggID, seq: seq}, seen, &signals)
		}
		return rows.Err()
	})
	if err != nil {
		return model.StatusSignals{}, "", err
	}
	return signals, modelFallback, nil
}

// eventKey dedupes event-tail rows by (aggregate_id,seq).
type eventKey struct {
	aggregateID string
	seq         int64
}
