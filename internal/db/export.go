package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"opencode-dashboard/internal/model"
)

// Export caps. Messages/parts reuse the existing detail limits (Q8/Q9);
// direct children are capped at exportChildLimit because each child costs
// two more indexed per-session reads, and the largest family observed live
// holds 663 children. Newest children win; the detail route's own child list
// is intentionally unchanged.
const exportChildLimit = 100

// Export session query (Q7-shaped, extended with the export-only columns):
// slug, agent, and tokens_cache_write. PK / parent_id equality only.
const (
	queryExportSessionBase = `
SELECT s.id, s.slug, s.project_id, s.directory, s.title, s.agent, s.model,
       s.cost, s.tokens_input, s.tokens_output, s.tokens_reasoning,
       s.tokens_cache_read, s.tokens_cache_write, s.time_created, s.time_updated,
       s.parent_id
  FROM session s`

	queryExportSessionOne = queryExportSessionBase + `
 WHERE s.id = ?`

	queryExportSessionChildren = queryExportSessionBase + `
 WHERE s.parent_id = ?
 ORDER BY s.time_updated DESC
 LIMIT ?`
)

// exportSessionRow is one scalar session row for the export payload.
type exportSessionRow struct {
	id               string
	slug             string
	projectID        string
	directory        string
	title            string
	agent            sql.Null[string]
	modelJSON        sql.Null[string]
	cost             float64
	tokensIn         int64
	tokensOut        int64
	tokensReasoning  int64
	tokensCacheRead  int64
	tokensCacheWrite int64
	createdMs        int64
	updatedMs        int64
	parentID         sql.Null[string]
}

// exportModelJSON is the session.model column shape used by the export route.
type exportModelJSON struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant"`
}

// SessionExport returns the export-shaped payload for one session and its
// direct children (newest-first, capped at exportChildLimit). Unknown ids
// report ErrNotFound via errors.Is, exactly like SessionDetail. Every query
// is indexed per session and LIMIT-bounded; no LIKE or data scans.
func (r *Reader) SessionExport(ctx context.Context, id string) (model.SessionExport, error) {
	if ctx.Err() != nil {
		return model.SessionExport{}, ctx.Err()
	}
	self, err := r.exportSessionOne(ctx, id)
	if err != nil {
		return model.SessionExport{}, err
	}
	children, err := r.exportSessionChildren(ctx, id)
	if err != nil {
		return model.SessionExport{}, err
	}
	session, err := r.exportOne(ctx, self)
	if err != nil {
		return model.SessionExport{}, err
	}
	out := model.SessionExport{
		Session:  session,
		Children: make([]model.ExportSession, 0, len(children)),
	}
	for i := range children {
		child, err := r.exportOne(ctx, children[i])
		if err != nil {
			return model.SessionExport{}, err
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}

// exportSessionOne loads the export scalar row for id or reports ErrNotFound.
func (r *Reader) exportSessionOne(ctx context.Context, id string) (exportSessionRow, error) {
	var out exportSessionRow
	found := false
	err := withBusyRetry(ctx, "export session "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryExportSessionOne, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scanExportSessionRow(rows, &out); err != nil {
				return err
			}
			found = true
		}
		return rows.Err()
	})
	if err != nil {
		return exportSessionRow{}, err
	}
	if !found {
		return exportSessionRow{}, fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return out, nil
}

// exportSessionChildren loads direct-child scalar rows, newest-first.
func (r *Reader) exportSessionChildren(ctx context.Context, id string) ([]exportSessionRow, error) {
	var out []exportSessionRow
	err := withBusyRetry(ctx, "export children "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryExportSessionChildren, id, exportChildLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		out = nil
		for rows.Next() {
			var s exportSessionRow
			if err := scanExportSessionRow(rows, &s); err != nil {
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

// exportOne builds one export session: info block plus chronological
// messages with their raw parts. Corrupt message/part blobs are skipped so a
// single bad row never fails the view.
func (r *Reader) exportOne(ctx context.Context, row exportSessionRow) (model.ExportSession, error) {
	messages, err := r.exportMessages(ctx, row.id)
	if err != nil {
		return model.ExportSession{}, err
	}
	parts, err := r.exportParts(ctx, row.id)
	if err != nil {
		return model.ExportSession{}, err
	}
	for i := range messages {
		if p, ok := parts[messages[i].Info.ID]; ok {
			messages[i].Parts = p
		} else {
			messages[i].Parts = []json.RawMessage{}
		}
	}
	return model.ExportSession{
		Info:     exportSessionInfo(row),
		Messages: messages,
	}, nil
}

// scanExportSessionRow scans one queryExportSessionBase row.
func scanExportSessionRow(rows *sql.Rows, s *exportSessionRow) error {
	return rows.Scan(
		&s.id, &s.slug, &s.projectID, &s.directory, &s.title, &s.agent,
		&s.modelJSON, &s.cost, &s.tokensIn, &s.tokensOut, &s.tokensReasoning,
		&s.tokensCacheRead, &s.tokensCacheWrite, &s.createdMs, &s.updatedMs,
		&s.parentID,
	)
}

// exportSessionInfo maps one scalar row onto the export info block.
func exportSessionInfo(s exportSessionRow) model.ExportSessionInfo {
	var modelInfo exportModelJSON
	if s.modelJSON.Valid {
		_ = json.Unmarshal([]byte(s.modelJSON.V), &modelInfo)
	}
	var parentID *string
	if s.parentID.Valid && s.parentID.V != "" {
		p := s.parentID.V
		parentID = &p
	}
	return model.ExportSessionInfo{
		ID:        s.id,
		Slug:      s.slug,
		ProjectID: s.projectID,
		Directory: s.directory,
		Title:     s.title,
		Agent:     s.agent.V,
		Model: model.ExportSessionModel{
			ID:         modelInfo.ID,
			ProviderID: modelInfo.ProviderID,
			Variant:    modelInfo.Variant,
		},
		Cost: s.cost,
		Tokens: model.ExportTokens{
			Input:     s.tokensIn,
			Output:    s.tokensOut,
			Reasoning: s.tokensReasoning,
			Cache: model.ExportTokenCache{
				Read:  s.tokensCacheRead,
				Write: s.tokensCacheWrite,
			},
		},
		Time: model.ExportSessionTime{
			Created: s.createdMs,
			Updated: s.updatedMs,
		},
		ParentID: parentID,
	}
}
