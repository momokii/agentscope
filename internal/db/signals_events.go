package db

import (
	"context"
	"fmt"
	"sort"
)

// Incremental event tail (Q4b seed + Q4c delta, see the allowlist in
// reader.go). The fleet tick scans messages and parts in full every tick
// but the event table (970k rows live) only once: after one seed scan each
// tick fetches rows newer than the last tick's high-water mark (a rowid PK
// range scan — O(delta), no blobs) and merges them into a per-aggregate
// top-10 cache. The stateless full scan stays for the on-demand detail path.

// eventOverlap re-fetches this many trailing rowids on every delta tick.
// A write transaction open exactly across the seed snapshot could otherwise
// slip a row under the high-water mark forever; the overlap replays it and
// the first-wins dedupe makes the replay free. Costs ~1ms per tick.
const eventOverlap = 1000

const (
	queryBatchEvents = `
SELECT aggregate_id, seq, type
  FROM event
 ORDER BY aggregate_id, seq DESC, rowid ASC`

	// queryBatchEventsSeed is the Q4b-shaped seed for the incremental cache:
	// same order, plus the rowid the merge needs for its tiebreak. Still a
	// covering index scan (rowid is free in a rowid table), one query.
	queryBatchEventsSeed = `
SELECT rowid, aggregate_id, seq, type
  FROM event
 ORDER BY aggregate_id, seq DESC, rowid ASC`

	// queryBatchEventsDelta is Q4c: rows newer than the last tick's
	// high-water mark. A rowid PK range scan — O(delta), no blobs.
	queryBatchEventsDelta = `
SELECT rowid, aggregate_id, seq, type
  FROM event
 WHERE rowid > ?
 ORDER BY rowid ASC`
)

// batchEventSignals fills out.fresh: per-session fresh-event flags over the
// newest eventScanLimit rows per aggregate with first-wins dedupe (same
// semantics as the singular path; the rowid ASC tiebreak matches the
// engine's typical return order for seq ties). No blobs involved; the
// single ordered scan replaces ~1970 per-session queries.
func (r *Reader) batchEventSignals(ctx context.Context, out *fleetSignals) error {
	return withBusyRetry(ctx, "batch events", func() error {
		rows, err := r.db.QueryContext(ctx, queryBatchEvents)
		if err != nil {
			return err
		}
		defer rows.Close()
		taken := make(map[string]int)
		seen := make(map[eventKey]struct{})
		for rows.Next() {
			var aggID string
			var seq int64
			var typ string
			if err := rows.Scan(&aggID, &seq, &typ); err != nil {
				return err
			}
			if taken[aggID] >= eventScanLimit {
				continue
			}
			taken[aggID]++
			key := eventKey{aggregateID: aggID, seq: seq}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			if typ == freshEventType {
				out.fresh[aggID] = true
			}
		}
		return rows.Err()
	})
}

// eventRow is one cached event-tail entry. Ordering is (seq DESC, rowid
// ASC), matching the Q4/Q4b newest-first convention with the same rowid
// tiebreak the engine's return order exhibits for duplicate seqs.
type eventRow struct {
	seq   int64
	rowid int64
	typ   string
}

// eventTail is the cached per-aggregate tail: at most eventScanLimit rows,
// newest-first, first occurrence of each seq wins (callers feed rowid-ASC,
// so the kept row is always the lowest-rowid one — the same row the full
// scan's first-wins dedupe would keep).
type eventTail struct {
	rows []eventRow
}

// add inserts a row unless its seq is already present (a duplicate, which
// always carries a higher rowid than the kept row) and re-caps the tail.
// A duplicate of an EVICTED row is inserted then truncated right back: the
// rows that evicted it persist (tails only shrink by displacement from
// above), so the outcome equals the full scan's.
func (t *eventTail) add(row eventRow) {
	for _, r := range t.rows {
		if r.seq == row.seq {
			return
		}
	}
	t.rows = append(t.rows, row)
	sort.Slice(t.rows, func(i, j int) bool {
		if t.rows[i].seq != t.rows[j].seq {
			return t.rows[i].seq > t.rows[j].seq
		}
		return t.rows[i].rowid < t.rows[j].rowid
	})
	if len(t.rows) > eventScanLimit {
		t.rows = t.rows[:eventScanLimit]
	}
}

// batchEventSignalsIncremental fills out.fresh exactly as batchEventSignals
// would, but after one seed scan each tick fetches only newer rows and
// merges them into the cache. Exactness: truncation composes, i.e.
// top-10(A∪B) == top-10(top-10(A) ∪ B), and every delta row carries a higher
// rowid than every cached row, so first-wins dedupe is preserved in both
// directions (cached duplicates win by lower rowid; new keys merge by rank).
func (r *Reader) batchEventSignalsIncremental(ctx context.Context, out *fleetSignals, sessionIDs map[string]struct{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.eventCache == nil {
		r.eventCache = make(map[string]*eventTail)
	}
	if !r.eventSeeded {
		if err := r.seedEventCache(ctx); err != nil {
			return err
		}
	} else {
		var curMax int64
		if err := r.db.QueryRowContext(ctx, queryMaxEventRowID).Scan(&curMax); err != nil {
			return fmt.Errorf("batch events watermark: %w", err)
		}
		if curMax < r.eventWatermark-eventOverlap {
			// The database regressed under a running process (restore /
			// swap): the watermark is meaningless, reseed from scratch.
			r.eventCache = make(map[string]*eventTail)
			if err := r.seedEventCache(ctx); err != nil {
				return err
			}
		}
	}
	floor := r.eventWatermark - eventOverlap
	if floor < 0 {
		floor = 0
	}
	high := r.eventWatermark
	err := withBusyRetry(ctx, "batch events delta", func() error {
		rows, err := r.db.QueryContext(ctx, queryBatchEventsDelta, floor)
		if err != nil {
			return err
		}
		defer rows.Close()
		batchSeen := make(map[eventKey]struct{})
		for rows.Next() {
			var rowid, seq int64
			var aggID, typ string
			if err := rows.Scan(&rowid, &aggID, &seq, &typ); err != nil {
				return err
			}
			if rowid > high {
				high = rowid
			}
			key := eventKey{aggregateID: aggID, seq: seq}
			if _, dup := batchSeen[key]; dup {
				continue
			}
			batchSeen[key] = struct{}{}
			tail, ok := r.eventCache[aggID]
			if !ok {
				tail = &eventTail{}
				r.eventCache[aggID] = tail
			}
			tail.add(eventRow{seq: seq, rowid: rowid, typ: typ})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	r.eventWatermark = high
	for aggID := range r.eventCache {
		if _, ok := sessionIDs[aggID]; !ok {
			delete(r.eventCache, aggID)
		}
	}
	for aggID, tail := range r.eventCache {
		for _, row := range tail.rows {
			if row.typ == freshEventType {
				out.fresh[aggID] = true
				break
			}
		}
	}
	return nil
}

// seedEventCache runs one Q4b-shaped full scan into the cache and sets the
// watermark to the highest rowid observed (never a standalone MAX: a MAX
// taken next to the scan could observe rows the scan missed, which would
// then be skipped forever). A racing write's rows land above the watermark
// and arrive via the first delta merge, deduplicated by key.
func (r *Reader) seedEventCache(ctx context.Context) error {
	high := int64(0)
	err := withBusyRetry(ctx, "batch events seed", func() error {
		rows, err := r.db.QueryContext(ctx, queryBatchEventsSeed)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rowid, seq int64
			var aggID, typ string
			if err := rows.Scan(&rowid, &aggID, &seq, &typ); err != nil {
				return err
			}
			if rowid > high {
				high = rowid
			}
			tail, ok := r.eventCache[aggID]
			if !ok {
				tail = &eventTail{}
				r.eventCache[aggID] = tail
			}
			tail.add(eventRow{seq: seq, rowid: rowid, typ: typ})
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	r.eventWatermark = high
	r.eventSeeded = true
	return nil
}
