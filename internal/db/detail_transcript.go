package db

import (
	"context"
	"encoding/json"
	"strings"

	"opencode-dashboard/internal/model"
)

// Detail transcript queries (Q8–Q11, see the allowlist in reader.go). Every
// one binds a session id (or a bounded child-id list for the single
// grandchild roll-up) with a LIMIT — no LIKE, no data scans.
const (
	queryDetailMessages = `
SELECT id, data
  FROM message
 WHERE session_id = ?
 ORDER BY time_created ASC, id ASC
 LIMIT ?`

	queryDetailParts = `
SELECT id, message_id, data
  FROM part
 WHERE session_id = ?
 ORDER BY time_created ASC, id ASC
 LIMIT ?`

	queryDetailTodos = `
SELECT content, status, priority
  FROM todo
 WHERE session_id = ?
 ORDER BY position ASC, rowid ASC
 LIMIT ?`
)

// detailMessageData is the decoded subset of one message.data blob used for
// the transcript: role, both model shapes (flat on assistant rows, nested
// "model" object on user rows), and message-grain tokens. User rows carry
// no tokens (observed live) and decode to zero TokenCounts.
type detailMessageData struct {
	Role       string `json:"role"`
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Model      *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Tokens *detailTokenData `json:"tokens"`
}

// detailTokenData is the message-grain token shape. Session/column tokens
// differ (separate columns); only message blobs use this shape.
type detailTokenData struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     *struct {
		Read int64 `json:"read"`
	} `json:"cache"`
}

// detailPartData is the decoded subset of one part.data blob: transcript
// text and tool-call fields. Duration derives from the part time window.
type detailPartData struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Tool  string `json:"tool"`
	State *struct {
		Status string `json:"status"`
	} `json:"state"`
	Time *struct {
		Start *int64 `json:"start"`
		End   *int64 `json:"end"`
	} `json:"time"`
}

// detailTranscript loads chronological messages and parts, joins text parts
// per message, and extracts the tool-call list in the same pass. Corrupt
// blobs are skipped (one bad row must not fail the view); messages left
// with no text parts carry "" content.
func (r *Reader) detailTranscript(ctx context.Context, id string) ([]model.DetailMessage, []model.DetailTool, error) {
	type msgRow struct {
		id   string
		data detailMessageData
	}
	var msgs []msgRow
	err := withBusyRetry(ctx, "detail messages "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailMessages, id, detailMessageLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		msgs = nil
		for rows.Next() {
			var msgID, data string
			if err := rows.Scan(&msgID, &data); err != nil {
				return err
			}
			var decoded detailMessageData
			if err := json.Unmarshal([]byte(data), &decoded); err != nil {
				continue
			}
			msgs = append(msgs, msgRow{id: msgID, data: decoded})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}

	textByMessage := make(map[string][]string)
	var tools []model.DetailTool
	err = withBusyRetry(ctx, "detail parts "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailParts, id, detailPartLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var partID, messageID, data string
			if err := rows.Scan(&partID, &messageID, &data); err != nil {
				return err
			}
			_ = partID
			var decoded detailPartData
			if err := json.Unmarshal([]byte(data), &decoded); err != nil {
				continue
			}
			switch decoded.Type {
			case "text":
				if decoded.Text != "" {
					textByMessage[messageID] = append(textByMessage[messageID], decoded.Text)
				}
			case "tool":
				tools = append(tools, detailToolFrom(decoded))
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}

	messages := make([]model.DetailMessage, 0, len(msgs))
	for _, m := range msgs {
		messages = append(messages, model.DetailMessage{
			ID:      m.id,
			Role:    m.data.Role,
			Content: strings.Join(textByMessage[m.id], "\n"),
			Model:   detailModelName(m.data),
			Tokens:  detailMessageTokens(m.data.Tokens),
		})
	}
	if messages == nil {
		messages = []model.DetailMessage{}
	}
	if tools == nil {
		tools = []model.DetailTool{}
	}
	return messages, tools, nil
}

// detailTodos loads todo rows ordered by position.
func (r *Reader) detailTodos(ctx context.Context, id string) ([]model.DetailTodo, error) {
	todos := []model.DetailTodo{}
	err := withBusyRetry(ctx, "detail todos "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailTodos, id, detailTodoLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var todo model.DetailTodo
			if err := rows.Scan(&todo.Content, &todo.Status, &todo.Priority); err != nil {
				return err
			}
			todos = append(todos, todo)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return todos, nil
}

// detailChildCounts fills SubagentCount for direct children (grandchildren
// per child) with one indexed GROUP BY. No children means no query.
func (r *Reader) detailChildCounts(ctx context.Context, children []model.SessionRow) error {
	if len(children) == 0 {
		return nil
	}
	ids := make([]string, len(children))
	args := make([]any, len(children))
	for i := range children {
		ids[i] = children[i].ID
		args[i] = children[i].ID
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := "SELECT parent_id, COUNT(*) FROM session WHERE parent_id IN (" + placeholders + ") GROUP BY parent_id"
	counts := make(map[string]int)
	err := withBusyRetry(ctx, "detail child counts", func() error {
		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var parentID string
			var n int
			if err := rows.Scan(&parentID, &n); err != nil {
				return err
			}
			counts[parentID] = n
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for i := range children {
		children[i].SubagentCount = counts[children[i].ID]
	}
	return nil
}

// detailModelName renders "providerID/modelID" from the flat assistant
// shape first, then the nested user shape, else "".
func detailModelName(m detailMessageData) string {
	if m.ProviderID != "" && m.ModelID != "" {
		return m.ProviderID + "/" + m.ModelID
	}
	if m.Model != nil && m.Model.ProviderID != "" && m.Model.ModelID != "" {
		return m.Model.ProviderID + "/" + m.Model.ModelID
	}
	return ""
}

// detailMessageTokens applies the fleet TokenCounts formula (In includes
// cache reads) at message grain. Nil tokens (user rows) decode to zeros.
func detailMessageTokens(t *detailTokenData) model.TokenCounts {
	if t == nil {
		return model.TokenCounts{}
	}
	var read int64
	if t.Cache != nil {
		read = t.Cache.Read
	}
	in := t.Input + read
	return model.TokenCounts{In: in, Out: t.Output, Total: in + t.Output + t.Reasoning}
}

// detailToolFrom maps one tool part to a DetailTool. Tokens stays zero:
// part rows carry no per-tool token attribution (message/session grain
// only); splitting message tokens across tools would falsify totals.
func detailToolFrom(p detailPartData) model.DetailTool {
	tool := model.DetailTool{Name: p.Tool}
	if p.State != nil {
		tool.Status = p.State.Status
	}
	if p.Time != nil && p.Time.Start != nil && p.Time.End != nil && *p.Time.End >= *p.Time.Start {
		tool.DurationMs = *p.Time.End - *p.Time.Start
	}
	return tool
}
