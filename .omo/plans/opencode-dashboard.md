# Work Plan — opencode mission control (session monitoring dashboard)

> Status: draft with decision-complete defaults — AWAITING USER REVIEW before implementation.
> Research: `.omo/notepads/opencode-dashboard/research-opencode-internals.md`,
> `research-runtime-observability.md`, `research-dashboard-design.md`.
> Every technical claim traces to those reports; gaps are marked ASSUMPTION.

## Goal

Build **opencode mission control**: a web-based, **internal-only**, Docker-deployed realtime
monitoring dashboard for opencode agent sessions running on a **single VM** (opencode inside
zellij panes, reached via tailscale).

Operator story: open one URL, see everything in <2s — every session's status (running / idle /
waiting-input / error / archived), tokens/cost, transcript + tool calls + subagents, per-pane live
text — with zero ability to leak credentials and zero public exposure.

Non-goals for v1: sending prompts, abort/resurrect actions (deferred wave), multi-host fleet,
public internet access, heavy auth providers.

## Decisions & Defaults (user may override any row)

| # | Decision | Source |
|---|---|---|
| D1 | Stack: **Go backend (SSE hub + poll collector) + embedded React+Vite SPA (Tailwind + Tremor Raw/Recharts), single binary, SQLite WAL read-only** | Lane C §5 tradeoff table + recommended default |
| D2 | Primary source = **`opencode.db` SQLite WAL read-only**; realtime = `event`-table tail (dedupe) + `log/opencode.log` tail; presence = zellij CLI + `/proc` join | Lane A §8 data strategy 1–3; Lane A §2 schema; Lane B §3 proc mapping |
| D3 | **Poll-based collector v1 at 5–15s**; event-tail/streamer (`zellij subscribe`, WAL inotify, `/event` SSE) deferred to live drill-down wave | Lane B §6 (poll RECOMMENDED v1, streamer phase 2) |
| D4 | **Fleet overview = default landing** (KPI strip + fleet table + event ticker) | Lane C §4 IA; Lane C §1a fleet steal list |
| D5 | Visual direction **DEFAULT A "k9sOps"** (near-black `#0a0c10`, single cyan accent, mono-forward, hard 1px borders, glyphs ●▲✕◌) | Lane C §3 rule 8, option A |
| D6 | Transport: **SSE server→client only**, snapshot re-sync on reconnect (Last-Event-ID); commands over plain POST; no WebSockets | Lane C §2 live-update rules |
| D7 | Read-only v1; abort/resurrect deferred | Lane A §4 (`POST /session/:id/abort` exists but per-instance); Lane B §7 (resurrect re-runs, warn user) |
| D8 | Single-host with named seam for multi-host later (`Collector` interface, `host_id` plumbed but constant) | Lane C §5 baseline discipline |

## Architecture

```
┌─ Host (VM) ─────────────────────────────────────────────┐
│ zellij server (PPID 1) → pane shells → opencode procs   │
│ ~/.local/share/opencode/opencode.db (+wal/shm) :ro      │
│ ~/.local/share/opencode/storage/ :ro                    │
│ ~/.cache/zellij/ (EXITED) :ro                           │
│ /run/user/<uid>/zellij/ sockets (CLI via XDG_RUNTIME_DIR)│
│ /proc (pid=host liveness)                               │
└──────────────┬──────────────────────────────────────────┘
               │ pid:host + :ro mounts
┌─ Container ──▼──────────────────────────────────────────┐
│ Go single binary:                                       │
│  collector (poll 5-15s): DB reader + zellij -j +        │
│    /proc join → Snapshot{seq}                           │
│  SSE hub (/api/events) + REST (/api/sessions,           │
│    /api/sessions/:id, /api/fleet, /healthz)             │
│  embedded React SPA (served from binary)                │
└──────────────┬──────────────────────────────────────────┘
               │ 127.0.0.1:PORT only
        tailscale serve (NOT funnel) → tailnet browsers
```

**Collector API contract (frozen before Wave 2; UI builds against this):**

```
GET /api/healthz            → {healthy, version, db_path, last_poll_ms, stale}
GET /api/fleet              → {seq, sessions:[SessionRow], kpis:{...}}
GET /api/sessions/:id       → full session + messages + parts + todos + children
GET /api/sessions/:id/tools → tool-call list w/ status/duration/tokens
GET /api/events (SSE)       → Snapshot{seq} ticks @250-1000ms aggregate + keepalive 15-30s
```

`SessionRow`: `{id, title, project/cwd, model, status(running|idle|waiting-input|error|archived),
tokens{in,out,total}, cost, updated_relative, spark(tokens/min), subagent_count, pane{CWD,exited},
orphan bool}` — Lane A §2 + Lane C §6.

Status derivation (Lane A §4): RUNNING = assistant msg `time.completed IS NULL` OR tool part
`state.status=running` OR fresh `message.part.updated.1` event rows; WAITING-INPUT = heuristic
(latest role=user w/ no in-flight, or compacting/archived timestamps); error from message `error`
union; never trust `/session/status` alone (per-instance only — verified `{}` on idle server while
TUIs busy).

## TODOs

**Wave 1 — Collector (DB reader + zellij/proc join + SSE hub + frozen API contract)**

- [x] 1. **W1-1** `internal/db/reader.go` — read-only WAL snapshot queries: indexed per-session slices (`WHERE session_id=? LIMIT`), `MAX(time_updated)`, `MAX(event.rowid)` tail with dedupe by `(aggregate_id,seq)`; running detection (`completed IS NULL`, tool `running`); cost/token roll-up incl. subagent children via `parent_id`. **Accept:** opens 8.2GB-class DB read-only without lock/timeout; no full `part.data LIKE` scan; returns SessionRow fields. **Verify:** `go test ./internal/db/ -run TestRunningDetection -v`
- [x] 2. **W1-2** `internal/runtime/zellij.go + proc.go` — `list-sessions -s` (parse EXITED suffix), `list-panes -j` per live session (COMMAND/CWD/EXITED), socket-dir listing = liveness #1, `/proc/<pid>/cwd` + `State:` (R/S/D/Z; Z=dead), pane→opencode PPID chain, orphan scan (`/proc/*/cmdline` match `opencode`) for non-zellij sessions. **Accept:** union of zellij panes + orphans; orphans flagged `dump-screen: unavailable`. **Verify:** `go test ./internal/runtime/ -v`
- [x] 3. **W1-3** `internal/hub/sse.go + api/handlers.go` — SSE hub (goroutine/viewer, tick aggregation 250–1000ms, keepalive 15–30s, Last-Event-ID resume + full-snapshot re-sync), REST handlers per contract above, bind `127.0.0.1` only. **Accept:** contract routes return fixture data; SSE reconnect test passes; nothing listens on 0.0.0.0. **Verify:** `go test ./internal/hub/ -v && ss -tlnp | grep $PORT`
- [x] 4. **W1-4** `internal/collector/poll.go` — 5–15s tick loop: DB snapshot + zellij/proc join → `Snapshot{seq}`; stale-frame self-heal; `dump-screen` on-demand only (not per tick). **Accept:** stale chip data (`last_poll_ms`) emitted; tick p95 <1s on tens of sessions. **Verify:** `go test ./internal/collector/ -v`

**Wave 2 — Fleet UI (default landing)**

_Depends on: frozen API contract from Wave 1 (mock server acceptable)._

Anti-slop acceptance for the whole wave (Lane C §3): `web/DESIGN.md` token file exists, every
color/font/space traces to a token; no purple/blue gradients, no glassmorphism, no hero, no 3-cards,
no `border-l-2` status stripes (state = tonal wash+glyph+label); Lucide-only icons; 2 type families
w/ tabular-nums mono; 32px fleet rows; GPU-only motion; `isAnimationActive=false` on streaming series.

- [x] 5. **W2-1** `web/src/views/Fleet.tsx + components/KpiStrip.tsx` — KPI strip (active, waiting-input amber, tokens/hr, cost today, errors, SSE status dot) + fleet table (status badge, title 2-line clamp, project/cwd, model chip, tokens mono + 72×24 sparkline, cost, relative time, ↳N subagent expander), sorted running→exited, recency grouping + search, LIVE toggle w/ paused/stale chip. **Accept:** matches Lane C §6 fleet-row mapping; waiting-input visually distinct (shape+label, never color alone). **Verify:** `pnpm --dir web test Fleet --run` + `pnpm --dir web lint`
- [x] 6. **W2-2** `web/src/components/EventTicker.tsx + CmdBar.tsx` — bottom/right event ticker (latest tool calls fleet-wide) + `:`/Cmd+K jump bar (fuzzy id/title/cwd) + persistent footer keymap + pin + 1-9 quick-switch slots. **Accept:** deep-linkable URLs; pin survives reload. **Verify:** `pnpm --dir web test Ticker CmdBar --run`
- [x] 7. **W2-3** `web/src/lib/sse.ts` — EventSource client: auto-reconnect w/ backoff, "reconnecting…" banner, Last-Event-ID resume, full-snapshot re-sync, 1Hz max sparkline updates, incremental patches (isolate changing components). **Accept:** kill-server-5s test shows banner → resume → snapshot, no scroll-yank. **Verify:** `pnpm --dir web test sse --run`
- [x] 8. **W2-4** `web/DESIGN.md` — tokens (ink ramp, cyan accent, semantic green/amber/red/blue-violet/gray), type scale 11/12/13/16/20/24, 4px rhythm, k9sOps direction recorded. **Accept:** reviewer can trace every hex in W2-1/2 to a token. **Verify:** `grep -rE '#[0-9a-f]{6}' web/src --include='*.tsx' | grep -v tokens` returns empty

**Wave 3 — Session drill-down (detail + compare + global timeline)**

_Depends on: Wave 1 API; Wave 2 shell/nav._

- [x] 9. **W3-1** `web/src/views/SessionDetail.tsx (Transcript/Turns tabs)` — Transcript = full-fidelity messages w/ subagent turns interleaved chronologically + agent-lane chips; Turns = per-assistant-turn cards (input summary, tool count, tokens, duration); tail-follow default, scroll-up pins w/ "↓ N new" pill, Esc resumes. **Accept:** `opencode export`-parity content for a fixture session; virtualized list. **Verify:** `pnpm --dir web test SessionDetail --run`
- [x] 10. **W3-2** `web/src/views/SessionDetail.tsx (Timeline/Tools/Cost/Raw tabs)` — Timeline (tool bars ∝ duration, subagent lanes, token-rate underlay, Recharts ReferenceArea drag-zoom, click→inspector); Tools table + top-5 error/latency + right-rail I/O inspector; Cost tab (cumulative line, in/out bars, cost-by-model donut, context ProgressCircle, per-subagent roll-up); Raw JSON tab (collapsible, deep-linkable). **Accept:** every chart point drill-downs to filtered session/span. **Verify:** `pnpm --dir web test Timeline Tools Cost --run`
- [x] 11. **W3-3** `web/src/views/Compare.tsx + GlobalTimeline.tsx` — Compare 2–4 sessions (KPI cards + overlaid cost/token lines); Global timeline swimlanes (all sessions, one axis). **Accept:** shareable URLs per view+tab+span. **Verify:** `pnpm --dir web test Compare GlobalTimeline --run`
- [x] 12. **W3-4** `internal/runtime/screen.go + GET /api/sessions/:id/screen` — on-demand `dump-screen --full` for the viewed pane only (hybrid per Lane B §6); orphan sessions return 409 w/ DB-only notice. **Accept:** live text <2s for focused pane; never polled per tick. **Verify:** `go test ./internal/runtime/ -run TestDumpScreen -v`

**Wave 4 — Docker / security / packaging (incl. VM-confirm)**

- [x] 13. **W4-1** `Dockerfile + docker-compose.yml` — exact spec below (pid:host, UID parity, XDG parity, baked zellij CLI same major, :ro mounts, `127.0.0.1:PORT:PORT`). **Accept:** `docker compose up` on laptop shows fleet w/ local sessions. **Verify:** `docker compose config && docker compose up -d && curl -s 127.0.0.1:$PORT/api/healthz`
- [x] 14. **W4-2** `docs/SECURITY.md + internal/auth/basic.go` — 127.0.0.1-only serve, optional `OPENCODE_SERVER_PASSWORD`-style basic auth, `auth.json`/credential tables never mounted/exposed, tailscale-serve-not-funnel runbook, tailnet ACL note. **Accept:** Security Gate checklist passes; `auth.json` unmounted provably. **Verify:** `docker inspect` shows no auth.json mount; `curl` from 0.0.0.0 refused
- [x] 15. **W4-3** VM-confirm runbook `docs/VM_DEPLOY.md` — `echo $XDG_RUNTIME_DIR`, `id -u`, zellij version match, tailscale serve steps, EXITED-session semantics note (resurrect re-runs, does not resume). **Accept:** named human runs steps on VM, pastes `healthz` output. **Verify:** VM `curl -s 127.0.0.1:$PORT/api/healthz` → `{"healthy":true,...}`
- [x] 16. **W4-4 (deferred-actions design, no code)** `docs/ACTIONS_DEFERRED.md` — abort/resurrect API sketch (`POST /session/:id/abort` per-instance routing problem stated; resurrect warning text) for a later wave. **Accept:** states why v1 is read-only. **Verify:** doc review only

## Final Verification Wave

- [x] F1. **Data fidelity:** fixture `opencode export <ses>` vs `/api/sessions/:id` field diff = zero for tokens/cost/message count; dedupe check on `event` tail. **Verify:** `go test ./internal/... && diff <(export) <(api)`
- [~] F2. **Runtime truth:** 3 live zellij panes + 1 orphan + 1 EXITED session displayed correctly w/ statuses; zombie (Z) shown dead; pane CWD vs DB directory both shown on mismatch. **Verify:** manual matrix checklist in `docs/VM_DEPLOY.md`
- [x] F3. **Design gate:** DESIGN.md traceability grep clean; status never color-only; streaming animation off; dark ops only. **Verify:** `pnpm --dir web lint && <token-grep>`
- [x] F4. **Security/deploy gate:** `docker compose up` from clean checkout, no secrets in image, 127.0.0.1-only publish, tailscale serve (not funnel) documented + confirmed. **Verify:** W4-2/W4-3 commands

## Data strategy (Lane A §8 rules)

1. Primary = `opencode.db` WAL **read-only**; poll `MAX(time_updated)` + `MAX(event.rowid)` or inotify `-wal`.
2. Realtime = tail `event` by rowid (**dedupe by id / (aggregate_id,seq)** — duplicates observed, Lane A U5) and/or `log/opencode.log`; per-session SSE via one `opencode serve` on fixed 127.0.0.1 port (later wave).
3. Presence = `pgrep -a opencode` + `ss -tlnp` joined to `session.directory`; `/session/status` covers **only the queried instance** — dashboard runs its own join.
4. **Never expose `auth.json`, `credential`/`account*` tables, or server password. Serve on 127.0.0.1 only.**
5. **8.2GB danger: every query indexed/sliced (per-session + LIMIT); full `part.data LIKE` scans (>60s observed) are banned** — enforce via query allowlist in `internal/db/`.

## Docker plug-n-play spec (Lane B §4 + §5)

```yaml
services:
  dashboard:
    pid: "host"
    user: "1000:1000"                 # ASSUMPTION: substitute VM uid/gid; verify with `id -u`
    environment:
      XDG_RUNTIME_DIR: /run/user/1000 # ASSUMPTION: parity with host; verify `echo $XDG_RUNTIME_DIR`
      HOME: /home/hostuser             # ASSUMPTION: substitute VM username
    volumes:
      - /run/user/1000/zellij:/run/user/1000/zellij:ro
      - /home/hostuser/.local/share/opencode/opencode.db:/data/opencode/opencode.db:ro
      - /home/hostuser/.local/share/opencode/storage:/data/opencode/storage:ro
      - /home/hostuser/.cache/zellij:/data/zellij-cache:ro
    ports: ["127.0.0.1:PORT:PORT"]     # never 0.0.0.0
```

Hard rules: bake `zellij` CLI (same major as host) into image; auto-detect UID at run time
(`user: "${UID}:${GID}"`); sockets are 0700 → same-user effectively mandatory; **never mount
`auth.json`**; tailscale `serve <port>` fronts localhost; **`funnel` is banned**.

## Security constraints

- Read-only v1: no prompt injection path, no shell exec from UI.
- Secrets: `auth.json`, `credential`/`account*`, server password never enter container/image/logs/API.
- Network: 127.0.0.1-only listen + publish; tailnet exposure only via `tailscale serve`; optional basic auth + tailnet ACLs.
- Supply chain: distroless/single-binary target; `docker history` clean of secrets; `:ro` on every mount.

## Adversarial review (top risks)

| # | Risk | Mitigation | Fails verification when |
|---|---|---|---|
| R1 | 8.2GB WAL DB locks/slows dashboard or disturbs live TUIs | Read-only open; indexed/sliced allowlist only; 5–15s poll, never per-second scans | W1-1/W1-4 query p95 blows budget or SQLITE_BUSY/timeout |
| R2 | Status lies: per-instance `/session/status` + heuristic waiting-input (no pending table) | DB-derived signals primary; waiting-input labeled heuristic in UI | F2 waiting-input fixture misclassified |
| R3 | zellij socket-version skew + UID/XDG mismatch in container → blind dashboard | Bake matching zellij major; UID/XDG parity checks at startup w/ loud error | W4-1 compose-up shows zero panes despite live sessions |
| R4 | SSE fan-out + Recharts redraw jank | 250–1000ms server aggregation, 1Hz sparkline cap, `isAnimationActive=false` | W2-3 reconnect/resume or perf budget fails |
| R5 | Secret leak (auth.json mount, credential tables in Raw tab, password in logs) | Mount allowlist, API field denylist test, Raw tab strips payloads, image secret scan | F4/W4-2 grep hits image/API output — gate blocks release |

## Open UNVERIFIED items (each carries a verification step in Waves 3–4)

- U1 Lane A: TUI processes hold 2 localhost listeners with an unknown protocol ("Endpoint not supported" string absent from binary) — follow up before trusting any per-instance HTTP route.
- U2 Lane A: `pending` (waiting-permission) state — heuristic until verified against a live prompt.
- U3 Lane A: `session_message`/`session_input` queue semantics — empty at research time.
- VM `UID/XDG_RUNTIME_DIR/HOME`, anything tailscale on VM, `zellij subscribe` streaming end-to-end (Lane B UNVERIFIED).

## Wave 5 — Pi harness monitoring (multi-harness fleet)

> Approved 2026-10-27. Research: live host inspection (pi 1.4.0) + pi docs
> (`session-format.md`, `sessions.md`, `message-types.md`, `terminal-setup.md`,
> `environment-variables.md`). Goal: the dashboard monitors **pi** sessions
> alongside opencode, with visible harness differentiation, reusing every
> frozen seam (collector tick, SSE, recency gate, read-only posture).

**Research facts (verified live, not assumed):**
- Sessions are JSONL at `~/.pi/agent/sessions/<slug-cwd>/<ts>_<uuid>.jsonl`
  (override: `PI_CODING_AGENT_SESSION_DIR`); header has `id`/`cwd`; entries
  form a tree (`message`, `model_change`, `session_info`, `compaction`,
  `usage`, ...). Store scale observed: 6 files / 1.4 MB (cheap to parse).
- Assistant messages carry `usage{totalTokens, cost.total}` + `stopReason`
  (`stop|length|toolUse|error|aborted|deferred|pending`); `usage` entries add
  non-conversation usage. Tokens/cost/model/status all derivable per file.
- Procs: argv[0] basename exactly `pi`; `/proc/<pid>/cwd` readable on host,
  denied in-container (same constraint as opencode — same fallback).
- Panes: pi sets its own title (`π - <basename>` + spinner) but the join
  does NOT parse titles: `list-panes -j` reports `pane_command: "pi"` and a
  real `pane_cwd`, so pane→directory matching works unchanged.
- `~/.pi/agent/auth.json` exists (0600) → same rule as opencode's: mount
  only the `sessions/` subdir `:ro`, never the agent root.

**Design (additive only, no frozen-shape breakage):**
- `model.SessionRow.Harness` (`"opencode"` default-normalized, `"pi"` for pi;
  serialized `harness`). Status derivation shared: pi stopReason maps onto
  the existing five states (last-entry `user`/`toolUse`/streaming → running,
  `stop`/`length` → waiting-input, `error` → error, `aborted` → idle), same
  `ACTIVE_WINDOW` recency gate. Pi has no archived concept.
- New `internal/pi` reader (`ports.PiReader`): directory scan + streaming
  JSONL parse (mtime cache: skip unchanged files per tick); title from
  `session_info`/`--name` else first user text else basename; model as
  `provider/modelId`; fork children via `parentSession` path refs.
- Runtime generalizes command matching (`ScanCommand` core; pane harness
  from `pane_command` basename, `terminal_command` fallback — never title).
  Claims key becomes (directory, harness): newest session per pair claims
  presence. Global counts stay combined; only agent-command panes count.
- Collector merges opencode+pi rows pre-Enrich; `ports.Config.PiDataDir`
  (`PI_DATA_DIR`, default `$HOME/.pi/agent/sessions` — `PI_CODING_AGENT_SESSION_DIR`
  is deliberately NOT honored: the dashboard reads one fixed root, logged at
  startup). Missing dir → pi disabled gracefully, opencode unaffected.
- UI: harness chip (π / OC glyph + label, never color-alone; new tokens in
  `tokens.css` + `DESIGN.md` table) in fleet rows + detail header; harness
  filter (all/opencode/pi) URL-serialized; TS `MIRROR` updated.
- Screen: `DumpSessionScreen`/`ResolvePane` gain harness to disambiguate a
  directory hosting both harnesses; hub resolves harness from snapshot row.
- Deploy: compose `:ro` mount `~/.pi/agent/sessions:/data/pi-sessions`,
  `PI_DATA_DIR` env; SECURITY.md §2/§3 rows; README config/API notes.

**Out of scope (P6 follow-up TASK):** pi transcript drill-down (JSONL→export
shape for Transcript/Turns/Timeline/Tools/Cost/Raw tabs), Compare/Timeline
enablement. This wave: pi rows link to the Live pane tab; detail tabs show
the honest empty state until P6.

- [x] P1. **model + pi source** — `Harness` type/field + `internal/pi` reader
  with mtime cache, status mapping, fork children. **Verify:** `go test ./internal/pi/ ./internal/model/`
- [x] P2. **runtime generalization** — `ScanCommand`, pane `Harness()`,
  (dir,harness) claims, harness-scoped resolve/dump. **Verify:** `go test ./internal/runtime/`
- [x] P3. **collector + config** — merge rows, `PiDataDir`, nil-safe disable.
  **Verify:** `go test ./internal/collector/ ./internal/ports/`
- [x] P4. **frontend** (+ pi screen-tab fallback in SessionDetail) — TS mirror, harness chip, filter + URL state, detail
  header, pi rows → screen tab. **Verify:** `npm test --prefix web -- --run`,
  `npm run lint --prefix web`, token gate
- [x] P5. **deploy + docs** — compose mount/env, SECURITY.md, README,
  `.env.example`, full gates, `./deploy.sh` redeploy + live evidence.
- [ ] P6. **(BACKLOG)** pi transcript drill-down via export-shaped route.

**Risks:** R-pi1 title-format drift (mitigated: join never reads titles);
R-pi2 JSONL schema drift (mitigated: unknown entry types ignored, defensive
parse with per-file error isolation — one corrupt file never fails a tick).
