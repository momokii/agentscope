package model

import "encoding/json"

// ExportSession is one session in the `opencode export` wire shape consumed
// by the SPA drill-down (GET /api/sessions/:id/export). It mirrors the export
// command's top-level `{info, messages}` object: a session-info block plus
// chronological messages, each carrying its raw parts.
//
// Producer: internal/db.SessionExport (read-only WAL snapshot, indexed
// per-session slices + LIMIT). Consumer: internal/hub maps it onto the
// additive export route; the SPA's liveProvider computes the display-only
// token total from the export token fields.
type ExportSession struct {
	Info     ExportSessionInfo `json:"info"`
	Messages []ExportMessage   `json:"messages"`
}

// SessionExport is the route payload for one viewed session and its direct
// children. Children are capped in db (exportChildLimit) to bound the
// per-view N+1 message/part reads.
type SessionExport struct {
	Session  ExportSession
	Children []ExportSession
}

// ExportSessionInfo mirrors the export session info block. The DB session
// model column carries id/providerID; variant is filled when the stored JSON
// has it (real rows often omit it, matching `opencode export`).
type ExportSessionInfo struct {
	ID        string             `json:"id"`
	Slug      string             `json:"slug"`
	ProjectID string             `json:"projectID"`
	Directory string             `json:"directory"`
	Title     string             `json:"title"`
	Agent     string             `json:"agent"`
	Model     ExportSessionModel `json:"model"`
	Cost      float64            `json:"cost"`
	Tokens    ExportTokens       `json:"tokens"`
	Time      ExportSessionTime  `json:"time"`
	ParentID  *string            `json:"parentID"`
}

// ExportSessionModel is the stored model descriptor. Variant is omitted when
// the DB row does not carry one (the export command defaults it).
type ExportSessionModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Variant    string `json:"variant,omitempty"`
}

// ExportTokens is the export session token shape. It deliberately omits the
// per-message `total`: `opencode export` session info carries only these
// four fields, and the SPA derives total = input + output + reasoning +
// cache.read (matching the fleet roll-up formula).
type ExportTokens struct {
	Input     int64            `json:"input"`
	Output    int64            `json:"output"`
	Reasoning int64            `json:"reasoning"`
	Cache     ExportTokenCache `json:"cache"`
}

// ExportTokenCache is the read/write cache split.
type ExportTokenCache struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

// ExportSessionTime is the session create/update window in epoch ms.
type ExportSessionTime struct {
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
}

// ExportMessage is one chronological message plus its raw part blobs.
// Parts are the stored part.data JSON with row identity injected
// (id/messageID/sessionID), because the DB blob omits those keys; every
// other field is passed through untouched.
type ExportMessage struct {
	Info  ExportMessageInfo `json:"info"`
	Parts []json.RawMessage `json:"parts"`
}

// ExportMessageInfo is the decoded message envelope. Tokens stays raw so the
// per-message cumulative `total` and cache.write fields survive verbatim.
type ExportMessageInfo struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"sessionID"`
	Role       string            `json:"role"`
	Time       ExportMessageTime `json:"time"`
	Agent      string            `json:"agent,omitempty"`
	ModelID    string            `json:"modelID,omitempty"`
	ProviderID string            `json:"providerID,omitempty"`
	Tokens     json.RawMessage   `json:"tokens,omitempty"`
	Cost       *float64          `json:"cost,omitempty"`
}

// ExportMessageTime is the message create/complete window in epoch ms.
// Completed is absent while a turn is in flight.
type ExportMessageTime struct {
	Created   int64  `json:"created"`
	Completed *int64 `json:"completed,omitempty"`
}
