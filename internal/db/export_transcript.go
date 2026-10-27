package db

import (
	"context"
	"encoding/json"

	"opencode-dashboard/internal/model"
)

// Q8-shaped message read with the row time_created as a fallback for a
// message blob that omits time.created (observed on some rows).
const queryExportMessages = `
SELECT id, time_created, data
  FROM message
 WHERE session_id = ?
 ORDER BY time_created ASC, id ASC
 LIMIT ?`

// exportMessageData is the decoded subset of one message.data blob needed by
// the export envelope. Tokens stays raw so `total` and cache.write pass
// through verbatim.
type exportMessageData struct {
	Role       string `json:"role"`
	Agent      string `json:"agent"`
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
	Model      *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Time *struct {
		Created   int64  `json:"created"`
		Completed *int64 `json:"completed"`
	} `json:"time"`
	Tokens json.RawMessage `json:"tokens"`
	Cost   *float64        `json:"cost"`
}

// exportMessages loads chronological messages with identity fields injected
// from the row (the DB blob omits id/sessionID; `opencode export` adds them).
func (r *Reader) exportMessages(ctx context.Context, id string) ([]model.ExportMessage, error) {
	messages := []model.ExportMessage{}
	err := withBusyRetry(ctx, "export messages "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryExportMessages, id, detailMessageLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		messages = messages[:0]
		for rows.Next() {
			var msgID, data string
			var createdAt int64
			if err := rows.Scan(&msgID, &createdAt, &data); err != nil {
				return err
			}
			var decoded exportMessageData
			if err := json.Unmarshal([]byte(data), &decoded); err != nil {
				continue
			}
			messages = append(messages, model.ExportMessage{
				Info: exportMessageInfo(msgID, id, createdAt, decoded),
			})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// exportParts groups the session's raw part blobs by message id, injecting
// the row identity (id/messageID/sessionID) that the DB blob omits. Every
// other part field is passed through untouched.
func (r *Reader) exportParts(ctx context.Context, id string) (map[string][]json.RawMessage, error) {
	parts := make(map[string][]json.RawMessage)
	err := withBusyRetry(ctx, "export parts "+id, func() error {
		rows, err := r.db.QueryContext(ctx, queryDetailParts, id, detailPartLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		parts = make(map[string][]json.RawMessage)
		for rows.Next() {
			var partID, messageID, data string
			if err := rows.Scan(&partID, &messageID, &data); err != nil {
				return err
			}
			injected, ok := injectPartIdentity(data, partID, messageID, id)
			if !ok {
				continue
			}
			parts[messageID] = append(parts[messageID], injected)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return parts, nil
}

// exportMessageInfo maps one decoded message blob, injecting id/sessionID and
// falling back to the row time_created when the blob omits time.created.
func exportMessageInfo(id, sessionID string, createdAt int64, m exportMessageData) model.ExportMessageInfo {
	created := createdAt
	var completed *int64
	if m.Time != nil {
		if m.Time.Created != 0 {
			created = m.Time.Created
		}
		completed = m.Time.Completed
	}
	providerID, modelID := m.ProviderID, m.ModelID
	if (providerID == "" || modelID == "") && m.Model != nil {
		if providerID == "" {
			providerID = m.Model.ProviderID
		}
		if modelID == "" {
			modelID = m.Model.ModelID
		}
	}
	return model.ExportMessageInfo{
		ID:         id,
		SessionID:  sessionID,
		Role:       m.Role,
		Time:       model.ExportMessageTime{Created: created, Completed: completed},
		Agent:      m.Agent,
		ModelID:    modelID,
		ProviderID: providerID,
		Tokens:     m.Tokens,
		Cost:       m.Cost,
	}
}

// injectPartIdentity copies the raw part JSON and adds the row identity
// fields. It returns false for a malformed blob or a JSON null.
func injectPartIdentity(raw, id, messageID, sessionID string) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj == nil {
		return nil, false
	}
	set := func(key, value string) {
		encoded, err := json.Marshal(value)
		if err == nil {
			obj[key] = encoded
		}
	}
	set("id", id)
	set("messageID", messageID)
	set("sessionID", sessionID)
	encoded, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	return json.RawMessage(encoded), true
}
