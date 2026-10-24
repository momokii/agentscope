// Package model is the frozen contract layer for opencode mission control.
//
// It owns the exact JSON shapes served by the REST API and SSE hub:
//
//	GET /api/healthz            → {healthy, version, db_path, last_poll_ms, stale}
//	GET /api/fleet              → {seq, sessions, kpis}
//	GET /api/sessions/:id       → full session + messages + parts + todos + children
//	GET /api/sessions/:id/tools → tool-call list w/ status/duration/tokens
//	GET /api/events (SSE)       → Snapshot ticks + keepalive
//
// Consumers: internal/db and internal/runtime produce SessionRows;
// internal/collector joins them into Snapshots; internal/hub serializes
// Snapshots to the wire. The React SPA (web/) builds against these shapes.
//
// Stability rule: field names and JSON tags are frozen. Any change requires
// sign-off from all four Wave-1 builders. No interface{}/any appears in any
// contract field — every JSON shape is explicit and typed.
package model
