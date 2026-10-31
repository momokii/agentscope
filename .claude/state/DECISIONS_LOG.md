# DECISIONS_LOG — Decisions and Rationale

> Decisions that shaped the shipped code. Verify against the cited paths before changing
> one; several are load-bearing for the 8.2 GB live DB and the container topology.

---
**Decision:** Go single binary + SSE (no WebSockets, no Node at runtime)
**Date:** 2026-10-24
**Context:** One internal dashboard on one VM; must be cheap to deploy and embed the SPA.
**Rationale:** `net/http` + SSE covers server→client snapshots; the React build is embedded
via `go:embed` (`web/embed.go`), so the runtime image is distroless with one process.
**Alternatives Rejected:** WebSockets (bidirectional not needed, more proxy friction);
separate Node server (extra runtime, extra attack surface).
**Security Implications:** No shell/exec path from the browser; one listener.
**Impact:** `cmd/dashboard/main.go`, `internal/hub/sse.go`, `web/embed.go`.

---
**Decision:** SQLite opened read-only, indexed/sliced allowlist only
**Date:** 2026-10-24
**Context:** The live `opencode.db` is ~8.2 GB with WAL sidecars and active TUI writers.
**Rationale:** `mode=ro` + `PRAGMA query_only=ON` never disturbs writers; per-session
queries with `LIMIT` keep ticks bounded. Full `part.data LIKE` scans (>60 s observed) are
banned. `SQLITE_BUSY` is a bounded retry, then `ErrBusy` (skip tick, never fatal).
**Alternatives Rejected:** Copying the DB (staleness, disk); `immutable=1` (ignores WAL).
**Security Implications:** Reader cannot write; credential/account tables are never queried.
**Impact:** `internal/db/reader.go` (allowlist Q1–Q12), `internal/ports/ports.go`.

---
**Decision:** Poll collector + incremental event tail, with `DB_TIMEOUT`
**Date:** 2026-10-25 (commit `28d2088`)
**Context:** A full tick on the live DB takes ~2 s steady-state / ~4 s cold; the original
5 s budget starved under WAL contention.
**Rationale:** Keep a 10 s poll, seed the event tail once, then read only
`rowid > watermark` with an overlap window; watermark-skip quiet ticks. `DB_TIMEOUT`
(default 10 s) bounds each source call. A tick slower than 2× the interval is marked
`stale`, never blanked.
**Alternatives Rejected:** `time_updated` as the sole incremental key (1401 sessions had
newer messages but an unchanged key); per-session N+1 queries (21 s).
**Security Implications:** Bounded work per tick prevents a reader-induced DoS.
**Impact:** `internal/collector/poll.go`, `internal/db/signals_batch.go`,
`internal/db/signals_events.go`, `internal/ports/ports.go`.

---
**Decision:** Status is recency-gated; error comes only from the newest assistant turn
**Date:** 2026-10-26 (commits `08f2f86`, `b595687`)
**Context:** DB signal rows persist forever; a naive derivation reported most of ~1,963
sessions “running” (median age 58 days).
**Rationale:** Only `time_updated` inside `ACTIVE_WINDOW` (default 2 m) may report
running/waiting-input/error; archived wins. Running outranks error. `HasError` reads the
newest assistant turn only. `waiting-input` remains a documented heuristic (no pending
table exists). The derivation is pure (`DeriveStatus`) and unit-tested.
**Alternatives Rejected:** Trusting `/session/status` (per-process only; returned `{}`
while TUIs were busy); trusting any-age in-flight rows.
**Security Implications:** None (read-only), but honest status avoids operator confusion.
**Impact:** `internal/model/status.go`, `internal/db/signals*.go`, `internal/collector`.

---
**Decision:** Host networking (`network_mode: host` + `pid: host`), no `ports:` stanza
**Date:** 2026-10-25 (commit `6c8c5e3`)
**Context:** The bridge-publish model was healthy-but-unreachable: the binary bound
container loopback, while bridge DNAT targeted the container eth0 IP.
**Rationale:** Host mode makes the `127.0.0.1` bind land on host loopback with no NAT hop;
the healthcheck becomes honest and `ss` shows the binary directly. This extends the
already-accepted `pid: host` grant for a monitoring agent.
**Alternatives Rejected:** `0.0.0.0` inside a bridge container (weakens the loopback
invariant; later replaced by an explicitly guarded `BIND_ADDR` opt-in).
**Security Implications:** Network namespace is shared; exposure boundary is unchanged
because the default bind is loopback and `funnel` is banned.
**Impact:** `docker-compose.yml`, `cmd/dashboard/selfcheck.go`, `docs/SECURITY.md`.

---
**Decision:** Zellij socket dir mounted read-write (all other mounts read-only)
**Date:** 2026-10-26 (commit `1004a12`)
**Context:** The zellij CLI creates a per-invocation client socket inside
`$XDG_RUNTIME_DIR`; a `:ro` mount made `list-panes`/`dump-screen` fail with EROFS.
**Rationale:** `:rw` only for the socket dir; opencode data, zellij cache and
`/etc/localtime` stay `:ro`. The dashboard issues only read-only zellij queries
(`list-sessions`, `list-panes`, `dump-screen`).
**Alternatives Rejected:** `:ro` socket dir (blind panes); mounting the home dir.
**Security Implications:** A compromised container could create client sockets, but not
write app data or credentials. Same-UID parity is mandatory (sockets are 0700).
**Impact:** `docker-compose.yml`, `docs/SECURITY.md` §2, `docs/VM_DEPLOY.md`.

---
**Decision:** Per-directory presence falls back to zellij panes when `/proc/<pid>/cwd`
is unreadable in-container
**Date:** 2026-10-26 (commit `ef84984`)
**Context:** With `pid: host` + non-root, `readlink /proc/<pid>/cwd` is denied by
ptrace/Yama, even though status/stat/cmdline are readable.
**Rationale:** Keep the proc row (do not drop it); global `live_processes` stays the exact
`/proc` count, while `projects[].live_procs` falls back to non-exited zellij panes whose
command is opencode. This is a **presence signal**, not an exact count, and may briefly
include a just-exited pane. `live_panes` is carried independently.
**Alternatives Rejected:** Dropping cwd-less procs (live_processes was 0); guessing
presence from sessions.
**Security Implications:** None.
**Impact:** `internal/runtime/{runtime,proc}.go`, `internal/collector/project.go`,
`web/src/views/Projects.tsx`.

---
**Decision:** KPIs are message-level truth; `cost_total` is separate
**Date:** 2026-10-27 (commit `07705be`)
**Context:** Session totals roll up subagent children, so a finishing child injects its
whole history as one tick and fabricated huge token/cost numbers.
**Rationale:** `tokens_per_hour` = message tokens created in the last 60 s × 60 (background
60 s refresher; `metrics_age_ms` exposes freshness, `-1` before the first refresh).
`cost_today` = message cost since **local midnight**; `cost_total` = lifetime sum of each
session's own pre-roll-up cost (`RawCost` is non-serialized to avoid double counting).
**Alternatives Rejected:** Deltas of rolled-up session totals; summing cumulative session
cost for “today”.
**Security Implications:** Off-tick scan is bounded (5 s query timeout) and never blocks a
poll; failures keep last-good values.
**Impact:** `internal/collector/{metrics,snapshot}.go`, `internal/db/metrics.go`,
`internal/model/types.go`, `internal/ports/ports.go`.

---
**Decision:** Cookie bridge for SSE under Basic auth
**Date:** 2026-10-27 (commit `07705be`)
**Context:** Native `EventSource` cannot send an `Authorization` header, so the stream
401-looped behind a Basic gate even though the page loaded.
**Rationale:** A successful Basic check mints `ocd_session` (32-byte random token,
HttpOnly, `SameSite=Lax`, `Path=/`, 12 h TTL, bounded 32-token in-memory store, `Secure`
only when the request is TLS). Middleware accepts valid Basic **or** a valid cookie. Open
mode issues no cookie.
**Alternatives Rejected:** `fetch`-based SSE library (new dependency, same-origin cookie is
simpler); credentials in URLs (browsers strip them).
**Security Implications:** The cookie is only issued after Basic; no persistent user store;
expired/unknown tokens are 401. The gate password is never persisted.
**Impact:** `internal/auth/{basic,session}.go`, `cmd/dashboard/health.go`, `docs/SECURITY.md`.

---
**Decision:** Project-centric UI with pagination and recency buckets
**Date:** 2026-10-27 (commits `07705be`, `6a3fc5d`, `6f72903`)
**Context:** thousands of sessions buried the fleet table; the flat idle mass hid old
sessions.
**Rationale:** Default landing is Projects ranked by live presence/activity, with drill-in
to the project’s sessions. Fleet keeps status chips + Live-only, splits idle into
Recent ≤24 h / Stale 1–7 d / Old >7 d, defaults to Active+Recent, paginates 50/page, and
serializes filters/pagination in the URL. Date presets Today/24 h/7 d/30 d + custom `from`.
**Alternatives Rejected:** One unpaginated table (thousands of rows); client-side “top N”.
**Security Implications:** None; URL state carries no secrets.
**Impact:** `web/src/views/{Projects,Fleet}.tsx`, `web/src/lib/{fleetSort,routing,projects}.ts`,
`internal/collector/project.go`.

---
**Decision:** Token-gated design system (one hex-bearing file)
**Date:** 2026-10-24 (commit `f825c1e`)
**Context:** Anti-slop gate: every color/space must trace to `web/DESIGN.md`.
**Rationale:** `web/src/tokens.css` is the only file allowed to contain 6-digit hex;
components use `var(--token)` and status is never color-only (glyph + label).
**Alternatives Rejected:** Tailwind utility sprawl; per-component hex.
**Security Implications:** None.
**Impact:** `web/DESIGN.md`, `web/src/tokens.css`, the token gate in `CLAUDE.md`.

---
**Decision:** Read-only v1; actions deferred; frontend detail stays fixture-backed for now
**Date:** 2026-10-24 / 2026-10-27
**Context:** Abort/resurrect needs per-instance owner discovery and a new security model;
the detail API landed after the SPA view was built against a fixture.
**Rationale:** No action routes ship until `docs/ACTIONS_DEFERRED.md` questions are
answered. The SPA detail seam (`SessionDataProvider`) deliberately allows a later
fetch-backed swap without view changes; today it still returns fixture data, and the
screen endpoint has no view. Both are handoff gaps (TASK-102), not live UI features.
**Alternatives Rejected:** Shipping an abort button that can target the wrong process.
**Security Implications:** Read-only posture is preserved; this is also a known handoff gap
(TASK-102), not a feature to describe as live.
**Impact:** Superseded 2026-10-27 — `docs/ACTIONS_DEFERRED.md`,
`web/src/views/SessionDetail.tsx`, `web/src/testutils/sessionDetail.fixture.ts`.

---

**Decision:** Live drill-down shipped — export-backed detail + Live pane tab
**Date:** 2026-10-27
**Context:** A monitoring tool must never render fixture data; the export and screen
routes now serve real session data, so the earlier fixture-driven drill-down is obsolete.
**Rationale:** `/api/sessions/:id/export` (info + messages with raw parts, per-session
capped) feeds a fetch-backed provider; `/screen` is wired in a Live pane tab with honest
409/504/503 handling. The fixture remains test-only.
**Alternatives Rejected:** Keeping the fixture-driven drill-down; adding a separate export
view instead of reusing the existing `SessionDataProvider` seam.
**Security Implications:** Read-only GET only; `/screen` surfaces live pane text and fails
closed (409/504/503) when no pane exists or capture fails.
**Impact:** `web/src/lib/sessionProvider.ts`, `web/src/views/SessionDetail.tsx`,
`web/src/views/ScreenTab.tsx`, `web/src/testutils/sessionDetail.fixture.ts` (test-only).
**Evidence:** Export-vs-CLI parity (871 messages, identical tokens/cost, 11 children);
Go/web suites green.

---

## Template (copy for every new decision)

---
**Decision:** Fleet rows carry Harness; presence claims key on (directory, harness)
**Date:** 2026-10-27
**Context:** Wave 5 adds pi sessions to the same fleet; both agents can be
live in one directory (different processes).
**Rationale:** Additive `SessionRow.Harness` (opencode default-normalized,
serialized `harness`); runtime claims per pair so panes/orphans never cross
harnesses; global counts stay combined. Backend normalizes empty→opencode;
TS mirrors with the same boundary rule.
**Alternatives Rejected:** Separate pi view (splits the operator story);
harness-agnostic claims (an opencode pane would mask a live pi session).
**Security Implications:** None (read-only; pi `auth.json` never mounted).
**Impact:** `internal/{model,db,pi,runtime,collector,hub}`, `web/src` fleet/detail,
compose mount, SECURITY.md §2/§3/§5.

---
**Decision:** Trailing pi user message derives running, not waiting-input
**Date:** 2026-10-27
**Context:** Pi never persists streaming ("pending") messages, so an open
turn sits at user-last for the whole generation.
**Rationale:** Marking it waiting-input would paint every active generation
amber; an open turn with no completed reply is in-flight work by definition.
Matches the opencode heuristic's direction (user spoke last + recent).
**Alternatives Rejected:** Mirroring opencode's latest-user→waiting-input
literally (misleading for pi's persistence model).
**Security Implications:** None.
**Impact:** `internal/pi/parse.go` signalsOf.

---
**Decision:** Pane classification reads pane_command, never pane titles
**Date:** 2026-10-27
**Context:** Pi sets its own pane title (`π - <dir>` + spinner) but titles
are undocumented UI; `list-panes -j` reports `pane_command` + `pane_cwd`.
**Rationale:** Foreground-process matching is observed ground truth and
survives title-format drift; unknown foregrounds stay legacy-claimable.
**Alternatives Rejected:** Title regex (`π -` prefix — breaks on format drift
or basename collisions).
**Security Implications:** None.
**Impact:** `internal/runtime/{zellij,runtime,screen}.go`.

```markdown
---
**Decision:** [What was decided]
**Date:** [YYYY-MM-DD]
**Context:** [Why it was needed]
**Rationale:** [Why this option]
**Alternatives Rejected:** [Other options and why not]
**Security Implications:** [Any security impact]
**Impact:** [What it affects downstream]
---
```

---
**Decision:** Alerts are client-only with Notification gated behind explicit opt-in + secure-context check
**Date:** 2026-10-26
**Context:** Waiting-input flips and budget crosses are the highest-value signals, but the dashboard must stay read-only and must not mislead on tailnet plain-HTTP (where Notification is denied by the platform).
**Rationale:** Detection runs per applied snapshot in App (both harnesses); surfacing requires opt-in. Browser Notification only when granted AND isSecureContext; otherwise in-app toasts + tab title badge always fire. Verified live that the tailnet IP context is non-secure.
**Alternatives Rejected:** Server-side push/SSE-triggered notify (new backend surface for a v1 that forbids it); silent fallback (would look broken with no explanation).
**Security Implications:** No new network calls, no secrets, no permission prompt before opt-in.
**Impact:** `web/src/lib/alerts.ts`, `AlertsBell`, `Toasts`, App wiring.
