// Package hub implements ports.SnapshotSink plus the Wave-1 REST/SSE API
// (W1-3): an SSE fan-out hub with tick-aggregated Snapshot frames and the
// frozen Collector API contract (GET /api/healthz, /api/fleet,
// /api/sessions/:id, /api/sessions/:id/tools, /api/events) plus the Wave-3
// on-demand live-pane route (GET /api/sessions/:id/screen → text/plain, or
// 409 DB-only for orphans; see screen.go).
//
// Transport decisions (plan §D6, research-dashboard-design §2):
//   - SSE server→client only; commands (later waves) go over plain POST.
//   - Same-origin embedded SPA, so no CORS-* headers are emitted (W4-2 owns
//     any cross-origin story; adding CORS now would widen the surface).
//   - Bind address comes ONLY from ports.Config.Addr() (127.0.0.1 by design;
//     tailscale serve fronts it). No env override, no 0.0.0.0 listener.
package hub
