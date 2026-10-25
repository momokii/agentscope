package db

import (
	"context"
	"fmt"
	"strings"
)

// Batched fleet signal pass (Q2b+Q3b, plus events in signals_events.go —
// see the allowlist in reader.go). Two phases per table: an index-only id
// scan (no blobs — fully covered by the session/time indexes), then chunked
// PK blob fetches decoded immediately into small structs (blob bytes are
// never retained). The part pass skips the JSON parse for blobs that cannot
// be running tools (maybeRunningTool: exact prefilter, ~13x decode win
// measured live).
//
// Per-session subsequences are byte-identical to the singular Q2/Q3/Q4
// orderings, and the same pure decoders from signals.go run over the same
// capped slices — only the query shape changed (N+1 → batched), never the
// signal semantics. Slice rationale (unchanged from the singular path):
// in-flight assistant turns and error unions surface in the newest rows; a
// running tool part is bumped (time_updated) on every progress event so it
// stays near the top of the recency order, and a tool buried deeper with no
// fresh events belongs to a dead session; fresh part-update events are by
// definition at the seq tail. A window-function single query was rejected:
// 20s+ on message (blob sort materialization), versus ~2.5s for two-phase.

// blobFetchChunk bounds one PK blob-fetch IN list. Well under legacy
// variable limits and small enough that one chunk never holds megabytes.
const blobFetchChunk = 500

const (
	queryBatchMessageIDs = `
SELECT session_id, id
  FROM message
 ORDER BY session_id, time_created DESC, id DESC`

	queryBatchPartIDs = `
SELECT session_id, id
  FROM part
 ORDER BY session_id, time_updated DESC, id DESC`
)

// fleetSignals is the batched DeriveStatus input set for every session:
// newest-first decoded message payloads (capped), running tool counts, and
// fresh-event flags. Built by batchSignals in ~200 indexed queries.
type fleetSignals struct {
	msgs  map[string][]messagePayload
	tools map[string]int
	fresh map[string]bool
}

// batchSignals collects DeriveStatus inputs for ALL sessions in batched
// form. Same capped slices, same orderings, same decoders as sessionSignals.
// Stateless (full scan every call): used by the on-demand detail path for
// huge families. The fleet tick uses fleetBatchSignals, which shares the
// message/tool passes and swaps the event pass for the incremental cache.
func (r *Reader) batchSignals(ctx context.Context) (*fleetSignals, error) {
	out := &fleetSignals{
		msgs:  make(map[string][]messagePayload),
		tools: make(map[string]int),
		fresh: make(map[string]bool),
	}
	if err := r.batchMessageSignals(ctx, out); err != nil {
		return nil, err
	}
	if err := r.batchToolSignals(ctx, out); err != nil {
		return nil, err
	}
	if err := r.batchEventSignals(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fleetBatchSignals is the per-tick fleet pass: same message/tool scans as
// batchSignals, but the event tail comes from the incremental cache
// (one full seed scan, then O(delta) ticks). sessionIDs prunes aggregates
// of deleted sessions from the cache. Output is identical to batchSignals.
func (r *Reader) fleetBatchSignals(ctx context.Context, base []sessionScalars) (*fleetSignals, error) {
	out := &fleetSignals{
		msgs:  make(map[string][]messagePayload),
		tools: make(map[string]int),
		fresh: make(map[string]bool),
	}
	if err := r.batchMessageSignals(ctx, out); err != nil {
		return nil, err
	}
	if err := r.batchToolSignals(ctx, out); err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(base))
	for i := range base {
		ids[base[i].id] = struct{}{}
	}
	if err := r.batchEventSignalsIncremental(ctx, out, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// batchMessageSignals fills out.msgs: per-session newest-first payloads
// capped at messageScanLimit. Phase 1 is an index-only id scan (no blobs);
// phase 2 fetches the capped blobs by PK in chunks and decodes immediately.
func (r *Reader) batchMessageSignals(ctx context.Context, out *fleetSignals) error {
	ordered := make(map[string][]string)
	var fetchOrder []string
	err := withBusyRetry(ctx, "batch message ids", func() error {
		rows, err := r.db.QueryContext(ctx, queryBatchMessageIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sessionID, id string
			if err := rows.Scan(&sessionID, &id); err != nil {
				return err
			}
			if len(ordered[sessionID]) >= messageScanLimit {
				continue
			}
			ordered[sessionID] = append(ordered[sessionID], id)
			fetchOrder = append(fetchOrder, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	byID, err := r.fetchBlobsByID(ctx, "batch message blobs", "message", fetchOrder)
	if err != nil {
		return err
	}
	for sessionID, ids := range ordered {
		payloads := make([]messagePayload, 0, len(ids))
		for _, id := range ids {
			data, ok := byID[id]
			if !ok {
				continue
			}
			msg, ok := decodeMessagePayload(data)
			if !ok {
				continue
			}
			payloads = append(payloads, msg)
		}
		out.msgs[sessionID] = payloads
	}
	return nil
}

// batchToolSignals fills out.tools: per-session running-tool counts over
// the newest partScanLimit parts. Phase 1 is index-only; phase 2 decodes
// chunks and counts on arrival (blob bytes are never retained).
func (r *Reader) batchToolSignals(ctx context.Context, out *fleetSignals) error {
	owner := make(map[string]string)
	perSession := make(map[string]int)
	var fetchOrder []string
	err := withBusyRetry(ctx, "batch part ids", func() error {
		rows, err := r.db.QueryContext(ctx, queryBatchPartIDs)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sessionID, id string
			if err := rows.Scan(&sessionID, &id); err != nil {
				return err
			}
			if perSession[sessionID] >= partScanLimit {
				continue
			}
			perSession[sessionID]++
			owner[id] = sessionID
			fetchOrder = append(fetchOrder, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	return r.foldBlobsByID(ctx, "batch part blobs", "part", fetchOrder, func(id, data string) {
		if !maybeRunningTool(data) {
			return
		}
		part, ok := decodePartPayload(data)
		if !ok {
			return
		}
		if part.Type == "tool" && part.State != nil && part.State.Status == "running" {
			out.tools[owner[id]]++
		}
	})
}

// fetchBlobsByID fetches data blobs for ids (PK lookups) in chunks and
// returns them keyed by id. Missing ids (deleted between phases) are absent.
func (r *Reader) fetchBlobsByID(ctx context.Context, op, table string, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	err := r.foldBlobsByID(ctx, op, table, ids, func(id, data string) {
		out[id] = data
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// foldBlobsByID streams PK blob fetches in blobFetchChunk slices and calls
// fn per row. Each chunk is its own retry unit so a transient SQLITE_BUSY
// replays one chunk, never the whole table.
func (r *Reader) foldBlobsByID(ctx context.Context, op, table string, ids []string, fn func(id, data string)) error {
	for start := 0; start < len(ids); start += blobFetchChunk {
		end := start + blobFetchChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := strings.Repeat("?,", len(chunk)-1) + "?"
		query := fmt.Sprintf("SELECT id, data FROM %s WHERE id IN (%s)", table, placeholders)
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		if err := withBusyRetry(ctx, op, func() error {
			rows, err := r.db.QueryContext(ctx, query, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, data string
				if err := rows.Scan(&id, &data); err != nil {
					return err
				}
				fn(id, data)
			}
			return rows.Err()
		}); err != nil {
			return err
		}
	}
	return nil
}
