package pi

import (
	"encoding/json"
	"strconv"
	"strings"
)

// This file holds the small JSON plumbing for the pi→export translator
// (see export.go for the mapping contract):
//   - decodeBlocks normalizes pi content blocks (thinking lives under
//     "thinking", tool calls carry id/name/arguments, text under "text").
//   - toolCallBuilder merges a call with its later toolResult into one tool
//     part, verbatim where the wire shape matches the SPA's expectations.
//   - asString/numOrString/boolish absorb real-world schema wobble observed
//     in live files: numeric fields sometimes arrive as JSON strings
//     ("durationMs": "16"), and isError as "False" or false.

// block is one normalized pi content block.
type block struct {
	kind   string // "text" | "thinking" | "toolCall" | (other, ignored)
	text   string
	callID string
	name   string
	args   json.RawMessage
}

// decodeBlocks normalizes a pi message content payload: either a bare
// string (user text) or an array of typed blocks. Unknown block types are
// kept with an empty payload and skipped by the caller.
func decodeBlocks(raw json.RawMessage) []block {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil
		}
		return []block{{kind: "text", text: s}}
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	out := make([]block, 0, len(arr))
	for _, item := range arr {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(item, &probe); err != nil || probe.Type == "" {
			continue
		}
		b := block{kind: probe.Type}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		switch probe.Type {
		case "text":
			b.text = unquote(obj["text"])
		case "thinking":
			b.text = unquote(obj["thinking"])
			if b.text == "" {
				b.text = unquote(obj["text"])
			}
		case "toolCall":
			b.callID = unquote(obj["id"])
			b.name = unquote(obj["name"])
			if a, ok := obj["arguments"]; ok {
				b.args = a
			}
		default:
			continue
		}
		out = append(out, b)
	}
	return out
}

// unquote extracts a JSON string value, tolerating absent fields.
func unquote(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// asString coerces a decoded scalar to string for the few pi fields that
// drift between JSON types across writer versions.
func asString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

// numOrString reads an epoch-ms/timestamp field that arrives as a JSON
// number or a numeric string ("1793110235598"); anything else yields ok=false.
func numOrString(raw json.RawMessage) (n int64, ok bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, false
	}
	s = strings.TrimSpace(s)
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f), true
	}
	return 0, false
}

// boolish reads isError-style flags arriving as JSON bool or as
// case-insensitive "true"/"false"/"1"/"0" strings.
func boolish(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return b
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

// finish attaches a toolResult message to an open call: output text,
// terminal status, and the end timestamp (result time preferred;
// durationMs-derived start when the result carries one).
func (tb *toolCallBuilder) finish(m *piMessage, ts int64) {
	var texts []string
	for _, block := range decodeBlocks(m.Content) {
		if block.kind == "text" && block.text != "" {
			texts = append(texts, block.text)
		}
	}
	if tb.name == "" && m.ToolName != "" {
		tb.name = m.ToolName
	}
	out := strings.Join(texts, "\n")
	end := ts
	var start *int64
	if dur, ok := numOrString(m.DurationMs); ok && dur >= 0 {
		s := end - dur
		if s < 0 {
			s = 0
		}
		start = &s
	}
	tb.isError = boolish(m.IsError)
	tb.output = out
	tb.done = true
	tb.endTs = end
	tb.startTs = start
}

// closeout finalizes an unmatched call: open turns keep running status
// (the turn may still be generating); closed turns report completed with
// the owning message's timestamp as the bound.
func (tb *toolCallBuilder) closeout(turnOpen bool) {
	if tb.done {
		return
	}
	if turnOpen {
		tb.status = "running"
		tb.endTs = tb.msgTs
	} else {
		tb.status = "completed"
		tb.endTs = tb.msgTs
	}
}

// partJSON renders one tool part in the exact wire shape the SPA's
// collectToolRuns consumes: {type, id, messageID, sessionID, tool,
// callID, state:{status, input, output, title, time:{start,end}}}.
func (tb *toolCallBuilder) partJSON(sessionID string) json.RawMessage {
	status := tb.status
	if status == "" {
		status = map[bool]string{true: "error", false: "completed"}[tb.isError]
	}
	start := tb.msgTs
	end := tb.endTs
	if tb.startTs != nil {
		start = *tb.startTs
	}
	if end <= 0 {
		end = start
	}
	title := tb.name
	if title == "" {
		title = tb.callID
	}
	body := map[string]interface{}{
		"type":      "tool",
		"id":        tb.msgID + ":" + tb.callID,
		"messageID": tb.msgID,
		"sessionID": sessionID,
		"tool":      tb.name,
		"callID":    tb.callID,
		"state": map[string]interface{}{
			"status": status,
			"input":  json.RawMessage(orEmptyObject(tb.args)),
			"output": tb.output,
			"title":  title,
			"time":   map[string]int64{"start": start, "end": end},
		},
	}
	raw, _ := json.Marshal(body)
	return raw
}

func orEmptyObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// textPartJSON renders a transcript text block with row identity injected
// (the pi file carries no per-part ids, unlike the db rows).
func textPartJSON(sessionID, messageID, text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]interface{}{
		"type":      "text",
		"id":        messageID + ":t",
		"messageID": messageID,
		"sessionID": sessionID,
		"text":      text,
	})
	return raw
}

// reasoningPartJSON renders a thinking block as a collapsible reasoning
// block (the SPA's TranscriptTab renders "reasoning" parts natively).
func reasoningPartJSON(sessionID, messageID, text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]interface{}{
		"type":      "reasoning",
		"id":        messageID + ":r",
		"messageID": messageID,
		"sessionID": sessionID,
		"text":      text,
	})
	return raw
}

// usageTokensJSON renders a pi usage record in the exact wire shape the
// SPA's toTokens expects (input/output/reasoning + cache read/write +
// total; cost stays beside the message, not inside tokens).
func usageTokensJSON(u *piUsage) json.RawMessage {
	if u == nil {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"input":     u.Input,
		"output":    u.Output,
		"reasoning": u.Reasoning,
		"cache":     map[string]int64{"read": u.CacheRead, "write": u.CacheWrite},
		"total":     u.TotalTokens,
	})
	return raw
}

func rawPart(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
