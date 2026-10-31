# CURRENT_STATUS

## Project Phase
v1 feature-complete + **Wave 5 multi-harness (opencode + pi)** shipped and
redeployed (`:8081`, fleet 1966 opencode + 6 pi, all gates green). Still open:
the human-run **F2 VM truth matrix** (`docs/VM_DEPLOY.md` §3; plan F2 is `[~]`)
and the human-run **F2 VM truth matrix** (`docs/VM_DEPLOY.md` §3; plan F2 is `[~]`).
Wave 6 v1.1.0 shipped (alerts, transcript search, export download, CI) and
redeployed (`:8082`); Wave 7 V2-B fleet history (`#/history`, `GET
/api/history`) is implemented, and V2-C P6 pi transcripts are implemented
(`internal/pi/export*.go`, exportMux fallthrough in `cmd/dashboard/main.go`).

## Completed
- [x] Collector: 10s poll, batched indexed DB reads, incremental event tail, per-source
  timeouts (`DB_TIMEOUT` 10s), watermark skip, stale-frame degradation.
- [x] Status truth: recency-gated lifecycle (`ACTIVE_WINDOW` 2m); error only from the
  newest assistant turn; running outranks error; archived wins.
- [x] Runtime join: zellij CLI + `/proc`; zombie `Z` = dead; orphan detection; newest
  session per (directory,harness) claims the pane; on-demand `dump-screen`.
- [x] Wave 5 pi harness: `internal/pi` JSONL reader (mtime cache, shared status
  gate), `SessionRow.Harness` (OC/π chips, harness filter URL-serialized),
  `PI_DATA_DIR`, plus P6 export-shaped pi transcripts (toolCall+toolResult
  merged, thinking/text parts, per-part ids synthesized; `exportMux`
  falls through db→pi on `ErrNotFound`).
- [x] APIs: healthz, fleet (+`projects`/KPIs), session detail, tools, export
  (`opencode export` shape for the session + capped children), screen, SSE
  (`Last-Event-ID` resume), hub debug; embedded SPA fallback.
- [x] KPIs: message-level `tokens_per_hour` (last 60s × 60), `cost_today` (local
  midnight), `cost_total`, `metrics_age_ms`, `live_processes`, `live_panes`.
- [x] UI: Projects landing (ranked, drill-in), Fleet table with status chips/Live-only,
  Recent/Stale/Old buckets, date presets + custom from, 50/page pagination, pins,
  Cmd+K, event ticker, Compare, Global timeline. Filter/page state is URL-serialized.
- [x] Drill-down: `SessionDetail` reads live export data through `liveProvider`
  (loading/empty states, no fixture in production); all tabs plus an on-demand
  **Live pane** tab (`/screen?full=1`, 200 text / 409 DB-only hint / 504 / 503).
- [x] Deploy/security: one-click `./deploy.sh`, host networking, read-only data mounts,
  rw zellij socket dir, optional Basic auth + `ocd_session` cookie for SSE, guarded
  `BIND_ADDR`, TZ forwarding.
- [x] Baselines at HEAD: Go 9 test packages green; web 252 tests / 31 files;
  `npm run build` green; token gate hits only `web/src/tokens.css`.
  Open-source pass: README rewritten end-to-end + CONTRIBUTING.md +
  docs/screenshots (landing, project, activity, live pane).

## In Progress
- [ ] **F2 VM truth matrix** (human, on the VM): 3 live panes, orphan, EXITED, zombie,
  CWD-vs-directory mismatch. Agents must not fabricate it by killing live sessions.

## Blocked
None.

## Open Questions
- F2 results on the target VM (record values, then flip plan F2 to `[x]`).
- Deferred actions wave (abort/resurrect) is design-only: `docs/ACTIONS_DEFERRED.md`.
- Multi-host is a seam idea only (`host_id` not plumbed; collector is single-host).
- Narrow/phone layout and event-tail/dedupe stress tests are not implemented.
- No CI pipeline / automated secret-dependency-image scans yet (original TASK-002).

## Security Notes
- Read-only v1: no prompt path, no shell exec from the UI, no action endpoints.
- Mounts: opencode parent dir + zellij cache + `/etc/localtime` `:ro`; zellij socket dir
  `:rw` (the CLI mints a per-call client socket). `auth.json` and credential/account
  tables are never mounted or queried.
- Auth: unset `DASHBOARD_AUTH` = open tailnet-trust mode; malformed = fail closed.
  Successful Basic mints an HttpOnly `ocd_session` cookie (12h, bounded 32-token store,
  `SameSite=Lax`, `Secure` only over TLS) so `EventSource` can authenticate.
- Bind: default `127.0.0.1`; any non-loopback `BIND_ADDR` refuses to start without
  valid auth. Tailscale `serve` only — `funnel` is banned.

## Last Updated
2026-10-27 — live drill-down export + Live pane wiring landed; export payload matched
`opencode export` token/cost/message/part counts on a real session. F2 remains
human-gated. (Keep this file under ~80 lines; move history to `HISTORY.md`.)

