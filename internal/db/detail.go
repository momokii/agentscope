package db

import (
	"context"
	"database/sql"
	"fmt"

	"opencode-dashboard/internal/model"
)

// On-demand drill-down caps. Per-session indexed + LIMIT (allowlist holds);
// sized to cover every session observed live (max 6967 messages, 32666
// parts, 16 todos) with headroom. A session exceeding a cap truncates its
// oldest rows first (queries order oldest-first), never fails the view.
const (
	detailMessageLimit = 10000
	detailPartLimit    = 40000
	detailTodoLimit    = 500
)

// Detail session queries (Q7, see the allowlist in reader.go): the same
// scalar columns as Q1 with a PK / parent_id equality predicate.
const (
	queryDetailSessionBase = `
SELECT s.id, s.project_id, s.parent_id, s.title, s.directory, s.model,
       s.cost, s.tokens_input, s.tokens_output, s.tokens_reasoning,
       s.tokens_cache_read, s.time_created, s.time_updated,
       s.time_compacting, s.time_archived, p.name, p.worktree
  FROM session s LEFT JOIN project p ON p.id = s.project_id`

	queryDetailSessionOne = queryDetailSessionBase + `
 WHERE s.id = ?`

	queryDetailSessionChildren = queryDetailSessionBase + `
 WHERE s.parent_id = ?
 ORDER BY s.time_updated DESC`
)

// detailBatchThreshold is the family size above which SessionDetail reads
// signals from one batched pass instead of per-session queries. Under it,
// the singular path is faster (fewer total queries); over it, batching wins
// (one 600-child family cost ~2000 queries / 14s live, batch costs ~5s).
const detailBatchThreshold = 32

// SessionDetail returns the drill-down payload for one session: content-side
// row (status derived, family roll-up applied), chronological messages with
// joined text parts, todos by position, direct children, and tool calls.
// Unknown ids report ErrNotFound (errors.Is). Pane/orphan stay zero — the
// hub overlays them from its latest snapshot.
func (r *Reader) SessionDetail(ctx context.Context, id string) (model.SessionDetail, error) {
	if ctx.Err() != nil {
		return model.SessionDetail{}, ctx.Err()
	}
	self, err := r.detailSessionOne(ctx, id)
	if err != nil {
		return model.SessionDetail{}, err
	}
	children, err := r.detailSessionChildren(ctx, id)
	if err != nil {
		return model.SessionDetail{}, err
	}
	messages, tools, err := r.detailTranscript(ctx, id)
	if err != nil {
		return model.SessionDetail{}, err
	}
	todos, err := r.detailTodos(ctx, id)
	if err != nil {
		return model.SessionDetail{}, err
	}

	// Status + roll-up reuse the fleet derivation exactly: the same capped
	// slices, same toRow, same rollUpSubagents as Sessions(). Small
	// families use the singular path (bounded N+1, still indexed + LIMIT
	// per query); huge families switch to the batched pass (see
	// detailBatchThreshold).
	family := make([]sessionScalars, 0, len(children)+1)
	family = append(family, self)
	family = append(family, children...)
	nowMs := r.now().UnixMilli()
	rows, _, err := r.detailFamilyRows(ctx, family, nowMs)
	if err != nil {
		return model.SessionDetail{}, err
	}
	if err := r.detailChildCounts(ctx, rows[1:]); err != nil {
		return model.SessionDetail{}, err
	}
	return model.SessionDetail{
		Session:  rows[0],
		Messages: messages,
		Todos:    todos,
		Children: rows[1:],
		Tools:    tools,
	}, nil
}

// detailSessionOne loads the session scalars for id or reports ErrNotFound.
func (r *Reader) detailSessionOne(ctx context.Context, id string) (sessionScalars, error) {
	var out sessionScalars
	found := false
	err := withBusyRetry(ctx, "detail session "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailSessionOne, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scanSessionScalarsRow(rows, &out); err != nil {
				return err
			}
			found = true
		}
		return rows.Err()
	})
	if err != nil {
		return sessionScalars{}, err
	}
	if !found {
		return sessionScalars{}, fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return out, nil
}

// detailSessionChildren loads direct-child scalars (parent_id = id).
func (r *Reader) detailSessionChildren(ctx context.Context, id string) ([]sessionScalars, error) {
	var out []sessionScalars
	err := withBusyRetry(ctx, "detail children "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailSessionChildren, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = nil
		for rows.Next() {
			var s sessionScalars
			if err := scanSessionScalarsRow(rows, &s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// detailFamilyRows derives one SessionRow per family member plus the
// child->parent map for rollUpSubagents. Both strategies share toRow and the
// batch/singular decoders, so statuses match the fleet rows exactly.
func (r *Reader) detailFamilyRows(ctx context.Context, family []sessionScalars, nowMs int64) ([]model.SessionRow, map[string]string, error) {
	parents := make(map[string]string, len(family))
	for i := range family {
		if family[i].parentID.Valid && family[i].parentID.V != "" {
			parents[family[i].id] = family[i].parentID.V
		}
	}
	rows := make([]model.SessionRow, 0, len(family))
	if len(family) > detailBatchThreshold {
		batch, err := r.batchSignals(ctx)
		if err != nil {
			return nil, nil, err
		}
		for i := range family {
			var in derivedInput
			for j, msg := range batch.msgs[family[i].id] {
				applyMessagePayload(msg, j == 0, &in.signals, &in.modelFallback)
			}
			in.signals.RunningToolParts = batch.tools[family[i].id]
			in.signals.HasFreshEvents = batch.fresh[family[i].id]
			rows = append(rows, r.toRow(family[i], in, nowMs))
		}
		rollUpSubagents(rows, parents)
		return rows, parents, nil
	}
	for i := range family {
		signals, fallback, err := r.sessionSignals(ctx, family[i].id)
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, r.toRow(family[i], derivedInput{signals: signals, modelFallback: fallback}, nowMs))
	}
	rollUpSubagents(rows, parents)
	return rows, parents, nil
}

// scanSessionScalarsRow scans one Q1-shaped row. Shared by the fleet and
// detail paths so column order stays single-sourced.
func scanSessionScalarsRow(rows *sql.Rows, s *sessionScalars) error {
	return rows.Scan(
		&s.id, &s.projectID, &s.parentID, &s.title, &s.directory,
		&s.modelJSON, &s.cost, &s.tokensIn, &s.tokensOut,
		&s.tokensReas, &s.tokensCr, &s.createdMs, &s.updatedMs,
		&s.compacting, &s.archived, &s.projName, &s.projWT,
	)
}
