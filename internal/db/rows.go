package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"path"
	"strings"

	"opencode-dashboard/internal/model"
	"opencode-dashboard/internal/ports"
)

// sessionScalars is one Q1 row: scalar session columns plus the joined
// project label columns. No message/part/event payload is fetched here.
type sessionScalars struct {
	id         string
	projectID  string
	parentID   sql.Null[string]
	title      string
	directory  string
	modelJSON  sql.Null[string]
	cost       float64
	tokensIn   int64
	tokensOut  int64
	tokensReas int64
	tokensCr   int64
	createdMs  int64
	updatedMs  int64
	compacting sql.Null[int64]
	archived   sql.Null[int64]
	projName   sql.Null[string]
	projWT     sql.Null[string]
}

// sessionScalars loads all Q1 rows. One full-table pass over scalar columns
// only (no blobs); the session table holds thousands of rows.
func (r *Reader) sessionScalars(ctx context.Context) ([]sessionScalars, error) {
	var out []sessionScalars
	err := withBusyRetry(ctx, "list sessions", func() error {
		rows, err := r.db.QueryContext(ctx, querySessions)
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
		if err := rows.Err(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sessionModelJSON is the session.model column shape
// ({id, providerID, variant}); id is the model id.
type sessionModelJSON struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
}

// formatModel renders "providerID/modelID" from the session.model column,
// falling back to the newest message's provider/model when the column is
// empty (observed on real rows), else "".
func formatModel(modelJSON string, fallback string) string {
	var parsed sessionModelJSON
	if err := json.Unmarshal([]byte(modelJSON), &parsed); err == nil {
		if parsed.ProviderID != "" && parsed.ID != "" {
			return parsed.ProviderID + "/" + parsed.ID
		}
	}
	return fallback
}

// projectLabel renders the fleet project cell: the project name when set,
// else the worktree basename (e.g. "gf-fun-app"), else the raw project id
// ("global" for the common case). Contract: "project worktree/name".
func projectLabel(projectID string, name sql.Null[string], worktree sql.Null[string]) string {
	if name.Valid && strings.TrimSpace(name.V) != "" {
		return name.V
	}
	if worktree.Valid && worktree.V != "" && worktree.V != "/" {
		if base := path.Base(worktree.V); base != "" && base != "/" {
			return base
		}
	}
	return projectID
}

// derivedInput bundles one row's DeriveStatus inputs with the model-label
// fallback both the fleet and detail paths compute before toRow, keeping
// the derivation call itself narrow.
type derivedInput struct {
	signals       model.StatusSignals
	modelFallback string
}

// toRow builds the content side of one SessionRow. Pane/orphan stay zero
// for RuntimeSource; Spark stays nil for the collector. It stamps
// RecentActivity from nowMs minus the session's time_updated against the
// reader's active window, so DeriveStatus gates running/waiting-input/error
// on real recency — the single place both fleet and detail paths derive.
func (r *Reader) toRow(s sessionScalars, in derivedInput, nowMs int64) model.SessionRow {
	signals := in.signals
	signals.Compacting = s.compacting.Valid
	signals.Archived = s.archived.Valid
	relativeMs := nowMs - s.updatedMs
	if relativeMs < 0 {
		relativeMs = 0
	}
	windowMs := r.activeWindowMs
	if windowMs <= 0 {
		windowMs = ports.DefaultActiveWindow.Milliseconds()
	}
	signals.RecentActivity = relativeMs <= windowMs
	tokensIn := s.tokensIn + s.tokensCr
	return model.SessionRow{
		ID:        s.id,
		Title:     s.title,
		Project:   projectLabel(s.projectID, s.projName, s.projWT),
		Directory: s.directory,
		Model:     formatModel(s.modelJSON.V, in.modelFallback),
		Status:    model.DeriveStatus(signals),
		Tokens: model.TokenCounts{
			In:    tokensIn,
			Out:   s.tokensOut,
			Total: tokensIn + s.tokensOut + s.tokensReas,
		},
		Cost:              s.cost,
		UpdatedRelativeMs: relativeMs,
	}
}

// rollUpSubagents folds each child session's tokens and cost into its
// top-level parent (joined via session.parent_id, indexed by
// session_parent_idx) and records per-parent child counts. parents maps
// child session id -> parent session id. Unknown parents (orphaned child
// rows) and self-parents are ignored; the child rows themselves are kept.
// It mutates rows in place; row order is preserved.
func rollUpSubagents(rows []model.SessionRow, parents map[string]string) {
	byID := make(map[string]int, len(rows))
	for i := range rows {
		byID[rows[i].ID] = i
	}
	counts := make(map[string]int)
	for childID, parentID := range parents {
		ci, ok := byID[childID]
		if !ok {
			continue
		}
		pi, ok := byID[parentID]
		if !ok || ci == pi {
			continue
		}
		parent := &rows[pi]
		child := &rows[ci]
		parent.Tokens.In += child.Tokens.In
		parent.Tokens.Out += child.Tokens.Out
		parent.Tokens.Total += child.Tokens.Total
		parent.Cost += child.Cost
		counts[parentID]++
	}
	for i := range rows {
		rows[i].SubagentCount = counts[rows[i].ID]
	}
}
