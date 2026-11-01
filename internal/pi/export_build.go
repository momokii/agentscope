package pi

import (
	"fmt"
	"strings"

	"opencode-dashboard/internal/model"
)

// sessionBuilder folds one session file's entries into export-shaped
// messages plus the session-level roll-ups the info block needs.
// See export.go for the mapping contract.
type sessionBuilder struct {
	sessionID string
	cwd       string
	timestamp string
	name      string
	firstUser string
	msgs      []*exportMessage
	data      fileData
}

func (b *sessionBuilder) apply(e piEntry) error {
	switch e.Type {
	case entrySession:
		b.sessionID = e.ID
		b.cwd = e.CWD
		b.timestamp = e.Timestamp
		b.data.parentID = parentIDFromPath(e.ParentSess)
	case entrySessionInfo:
		if strings.TrimSpace(e.Name) != "" {
			b.name = e.Name
		}
	case entryMessage:
		return b.applyMessage(e)
	case entryModelChange:
		if e.Provider != "" && e.ModelID != "" {
			b.data.lastModel = e.Provider + "/" + e.ModelID
		}
	case entryUsage, entryCompaction, entryBranchSummary:
		ts := parseTimeMs(e.Timestamp, 0)
		b.data.lastTsMs = maxInt64(b.data.lastTsMs, ts)
		if e.Usage != nil {
			b.data.addUsage(e.Usage, ts)
		}
	default:
		// Unknown future entry types: recency only, same as the fleet path.
		ts := parseTimeMs(e.Timestamp, 0)
		b.data.lastTsMs = maxInt64(b.data.lastTsMs, ts)
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// applyMessage folds one message entry. toolResult entries attach to the
// most recent unmatched toolCall with the same callId; assistant messages
// start new ExportMessages whose tool calls convert to tool parts immediately
// (patched in place when results arrive later in the stream).
func (b *sessionBuilder) applyMessage(e piEntry) error {
	if e.Message == nil {
		return nil
	}
	m := e.Message
	ts := parseTimeMs(e.Timestamp, 0)
	if ts > b.data.lastTsMs {
		b.data.lastTsMs = ts
	}
	switch m.Role {
	case "user":
		b.data.lastRole = "user"
		b.data.lastStop = ""
		b.data.openToolUse = false
		b.data.hasError = false
		if b.firstUser == "" {
			b.firstUser = firstText(m.Content)
		}
		if b.firstUser == "" {
			// Untitled user text is still a message; keep it renderable.
		}
		em := &exportMessage{msg: model.ExportMessage{
			Info: model.ExportMessageInfo{
				ID:        e.ID,
				SessionID: b.sessionID,
				Role:      "user",
				Time:      model.ExportMessageTime{Created: ts, Completed: &ts},
			},
		}}
		for _, block := range decodeBlocks(m.Content) {
			if block.kind == "text" {
				em.msg.Parts = append(em.msg.Parts, rawPart(textPartJSON(b.sessionID, e.ID, block.text)))
			}
		}
		b.msgs = append(b.msgs, em)
	case "assistant":
		b.data.lastRole = "assistant"
		b.data.lastStop = m.StopReason
		b.data.openToolUse = m.StopReason == "toolUse"
		b.data.hasError = m.StopReason == "error" || m.StopReason == "aborted"
		if m.Provider != "" && m.Model != "" {
			b.data.lastModel = m.Provider + "/" + m.Model
		}
		if m.Usage != nil {
			b.data.addUsage(m.Usage, ts)
		}
		em := &exportMessage{
			msg: model.ExportMessage{
				Info: model.ExportMessageInfo{
					ID:        e.ID,
					SessionID: b.sessionID,
					Role:      "assistant",
					Time:      model.ExportMessageTime{Created: ts},
				},
			},
			tools:      make(map[string]*toolCallBuilder),
			stopReason: m.StopReason,
		}
		if m.Provider != "" {
			em.msg.Info.ProviderID = m.Provider
		}
		if m.Model != "" {
			em.msg.Info.ModelID = m.Model
		}
		if m.Usage != nil {
			em.msg.Info.Tokens = usageTokensJSON(m.Usage)
			cost := m.Usage.Cost.Total
			em.msg.Info.Cost = &cost
		}
		for _, block := range decodeBlocks(m.Content) {
			switch block.kind {
			case "text":
				if block.text == "" {
					continue
				}
				em.msg.Parts = append(em.msg.Parts, rawPart(textPartJSON(b.sessionID, e.ID, block.text)))
			case "thinking":
				// Empty thinking blocks are provider-encrypted reasoning
				// (thinkingSignature with no visible text — 77% of live pi
				// reasoning parts). A part with no text renders a dead
				// toggle, so drop it at translation (documented in
				// docs/COST_METHOD.md parity notes).
				if block.text == "" {
					continue
				}
				em.msg.Parts = append(em.msg.Parts, rawPart(reasoningPartJSON(b.sessionID, e.ID, block.text)))
			case "toolCall":
				tb := &toolCallBuilder{
					callID: block.callID, name: block.name, args: block.args,
					msgID: e.ID, msgTs: ts,
				}
				// Placeholder keeps original block order; the serialized
				// part is written at finish() time once results (or the
				// turn's end) have fixed the status and output.
				tb.partIdx = len(em.msg.Parts)
				em.msg.Parts = append(em.msg.Parts, nil)
				em.order = append(em.order, tb)
				em.tools[block.callID] = tb
			}
		}
		b.msgs = append(b.msgs, em)
	case "toolResult":
		// Attach to the most recent unmatched toolCall with this callId.
		// Unmatched results are dropped (documented): the SPA only renders
		// user/assistant messages, so an orphan result has no home anyway.
		// ToolCallID stays raw (writers disagree on string vs number).
		callID := unquote(m.ToolCallID)
		for i := len(b.msgs) - 1; i >= 0; i-- {
			em := b.msgs[i]
			if em.msg.Info.Role != "assistant" {
				continue
			}
			if tb, ok := em.tools[callID]; ok && !tb.done {
				tb.finish(m, ts)
				break
			}
		}
	default:
		// system / custom_message and future roles: recency only.
	}
	return nil
}

// finish seals the build: completes message timestamps, finalizes open
// tool calls, assembles the session block, and validates identity.
func (b *sessionBuilder) finish() (sessionBuild, error) {
	if b.sessionID == "" {
		return sessionBuild{}, fmt.Errorf("pi: export: missing session header")
	}
	// completed = next message ts; the final message completes at its own
	// timestamp only for terminal stop reasons, else stays open (a turn
	// that may still be generating).
	for i, em := range b.msgs {
		ts := em.msg.Info.Time.Created
		var next *int64
		switch {
		case i+1 < len(b.msgs):
			n := b.msgs[i+1].msg.Info.Time.Created
			next = &n
		case em.msg.Info.Role == "assistant" && !terminalStop(em.stopReason):
			next = nil
		default:
			next = &ts
		}
		em.msg.Info.Time.Completed = next
		open := em.msg.Info.Role == "assistant" && next == nil
		for _, tb := range em.order {
			tb.closeout(open)
			em.msg.Parts[tb.partIdx] = tb.partJSON(b.sessionID)
		}
	}
	msgs := make([]model.ExportMessage, 0, len(b.msgs))
	for _, em := range b.msgs {
		msgs = append(msgs, em.msg)
	}
	var parentID *string
	if b.data.parentID != "" {
		p := b.data.parentID
		parentID = &p
	}
	provider, modelID := splitModel(b.data.lastModel)
	info := model.ExportSessionInfo{
		ID:        b.sessionID,
		Slug:      "pi-" + shortID(b.sessionID),
		ProjectID: "global",
		Directory: b.cwd,
		Title:     b.sessionTitle(),
		Model: model.ExportSessionModel{
			ID:         modelID,
			ProviderID: provider,
		},
		Cost: b.data.cost,
		Tokens: model.ExportTokens{
			Input:     b.data.input,
			Output:    b.data.output,
			Reasoning: b.data.reasoning,
			Cache:     model.ExportTokenCache{Read: b.data.cacheRead, Write: b.data.cacheWrite},
		},
		Time: model.ExportSessionTime{
			Created: parseTimeMs(b.timestamp, 0),
			Updated: b.data.lastTsMs,
		},
		ParentID: parentID,
	}
	return sessionBuild{msg: model.ExportSession{Info: info, Messages: msgs}, id: b.sessionID}, nil
}

// sessionTitle mirrors the fleet title precedence (explicit name, first
// user text, id fallback) so the drill-down header matches the fleet row.
func (b *sessionBuilder) sessionTitle() string {
	if strings.TrimSpace(b.name) != "" {
		return b.name
	}
	if b.firstUser != "" {
		return b.firstUser
	}
	return "pi session " + shortID(b.sessionID)
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}

func splitModel(combined string) (provider, modelID string) {
	if i := strings.Index(combined, "/"); i >= 0 {
		return combined[:i], combined[i+1:]
	}
	return "", combined
}

func terminalStop(stopReason string) bool {
	switch stopReason {
	case "stop", "length", "error", "aborted":
		return true
	default:
		return false
	}
}
