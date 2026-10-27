# opencode mission control

Single-container, read-only monitoring dashboard for opencode agent sessions running on one VM (opencode inside zellij panes, reached via tailscale). Open one URL and see every session's status (running / idle / waiting-input / error / archived), tokens/cost, and directory-level projects — with zero ability to leak credentials and zero public exposure.

**Scope caveat (read this before demoing):** fleet rows, KPIs, projects, the event ticker, and session drill-down are live in the SPA. `GET /api/sessions/:id/export` returns the real session plus children (export-shaped), and the **Live pane** tab calls `GET /api/sessions/:id/screen` on view (409 + `{db_only,hint}` when a session has no pane). See [Known limitations](#known-limitations).

**Operator story:** you have three opencode TUIs burning tokens across projects and no idea which one is stuck waiting on you. You open the dashboard, the Projects landing ranks folders by live presence, you drill into the folder and the fleet table shows every session with live status plus any orphan (opencode running outside zellij), and the stuck one glows amber waiting-input — all without touching `auth.json`, provider keys, or the public internet. The transcript tabs render live export data and pane text is available from the Live pane tab.

## 60-second quickstart

```bash
./deploy.sh            # one command: prereqs → build → up → verified banner
```

Open the printed URL (`http://127.0.0.1:8080` by default). You should see the **Projects** landing (one ranked card per directory with live presence and session counts), a KPI strip (active / waiting-input / tokens-hr / cost today / cost total / errors / live procs / live panes / metrics age / SSE dot), and a live event ticker. Click a project to drill into its Fleet table (status chips, recency buckets, pagination), then a row for the session view (detail tabs and the Live pane tab render live data; unknown ids show an empty state). Empty lists are fine on a host with no opencode running. Teardown: `./deploy.sh --down`.

To expose it on your tailnet afterwards: `tailscale serve --bg --https=443 http://127.0.0.1:8080` (serve, never funnel — see security).

## Features

- **Projects landing (default):** one card per `session.directory`, ranked by live
  process/pane presence and recent activity, with session/running/waiting/tokens/cost
  totals and a drill-in to that project's sessions.
- **Fleet table:** status chips (running / waiting-input / idle / error / archived) with
  counts, a Live-only toggle, search, pins, subagent expansion, and per-row
  tokens/sparkline/cost/relative time.
- **Multi-harness (Wave 5):** pi sessions (JSONL store) merge into the same
  fleet next to opencode rows. Every row carries a harness chip (OC / π,
  glyph + label, never color alone); a harness slice (all / opencode / pi)
  filters the fleet and is URL-serialized. Pi rows deep-link to the Live
  pane tab (transcript drill-down is the P6 follow-up; other tabs show the
  honest empty state until then).
- **Recency honesty:** idle sessions split into Recent ≤24h / Stale 1–7d / Old >7d,
  defaulting to Active+Recent; date presets Today / 24h / 7d / 30d plus a custom `from`.
- **Pagination and URL state:** 50 rows/page; status, live-only, harness, bucket,
  date, `from`, and page are serialized in the hash so links survive reload/back-forward.
- **Live updates:** SSE snapshot stream with reconnect/backoff/resume, a stale/paused
  chip, a KPI strip, and an event ticker.
- **Session view:** Transcript / Turns / Timeline / Tools / Cost / Raw tabs plus a
  Subagents panel, all backed by live `/api/sessions/:id/export` data, and a **Live
  pane** tab backed by `/api/sessions/:id/screen`.
- **Fleet-wide analysis:** Compare 2–4 sessions and a Global timeline (all sessions).
- **Read-only by construction:** no prompt, abort, resurrect, or shell route.

## Architecture

```
┌─ Host (netns shared: pid:host + network_mode:host) ──┐
│ zellij server → pane shells → opencode procs          │
│ ~/.local/share/opencode/ (db + wal/shm + storage) :ro │
│ ~/.cache/zellij/ (EXITED metadata) :ro                │
│ $XDG_RUNTIME_DIR/zellij/ sockets (0700) :rw (CLI mints a client socket per call) │
│ /proc (pid:host liveness)                             │
└──────────────┬───────────────────────────────────────┘
               │ pid:host + net:host + :ro mounts (:rw socket dir only — no NAT)
┌─ Container ──▼───────────────────────────────────────┐
│ Go single binary (distroless, no Node at runtime):    │
│  binds HOST 127.0.0.1:$PORT directly (asserted in-code)│
│  collector (poll 10s): DB reader + zellij/proc join   │
│  SSE hub (/api/events) + REST (/api/fleet, …)         │
│  embedded SPA (web/dist served from the binary)       │
└──────────────┬───────────────────────────────────────┘
               │ host loopback only — nothing on non-loopback
  tailscale serve (NOT funnel) → tailnet browsers
```

## Configuration

| Var | Source | Default | Effect |
|---|---|---|---|
| `PORT` | host env / `--port N` | `8080` | THE listen port (host mode: binary binds host `127.0.0.1:$PORT` directly, no mapping). Occupied → script auto-bumps and says so |
| `BIND_ADDR` | host env → compose | `127.0.0.1` | Listen address. Any non-loopback value (e.g. `0.0.0.0`) is an explicit opt-in and the binary **refuses to start unless `DASHBOARD_AUTH` is valid**. Prefer `tailscale serve`; never expose non-loopback on a public host |
| `POLL_INTERVAL` | host env → compose | `10s` | Collector tick (DB+zellij+proc join → snapshot). First tick may report `stale:true` — normal warm-up |
| `DB_TIMEOUT` | host env → compose | `10s` | Per-source bound for `PollState`/`Sessions` each tick. Malformed/non-positive falls back to default. A tick slower than 2× the poll interval is published as `stale`, never blanked |
| `ACTIVE_WINDOW` | host env → compose | `2m` | Status recency gate: only sessions whose `time_updated` is inside this window may show running/waiting-input/error; older rows derive `idle` (archived wins). Malformed/non-positive falls back to default |
| `DATA_DIR` | compose `environment` | `/data/opencode` | Read-only opencode data root in-container (parent dir mount keeps `-wal`/`-shm` sidecars visible) |
| `PI_DATA_DIR` | compose `environment` | `/data/pi-sessions` | Read-only pi session-store root in-container (`sessions/` subdir only — `auth.json` never mounted). Default `$HOME/.pi/agent/sessions` locally. Missing/unreadable store = pi merge degrades warn-once, opencode fleet unaffected |
| `DASHBOARD_AUTH` | host env | unset (= open tailnet-trust mode) | `user:pass` (split on first colon) enables the Basic gate; malformed (no colon/empty part) = fail-closed refuse-to-serve. Successful Basic also mints the `ocd_session` cookie used by SSE. Value never logged |
| `TZ` | host env (`deploy.sh` detects) | host zone | Local-time attribution for `cost_today` (message spend since **local midnight**) and log stamps. Compose also mounts `/etc/localtime:ro`, so Go resolves the host zone even when `TZ` is empty |
| `UID` / `GID` | host (`export UID`; `GID=$(id -g)`) | `1000:1000` | Container user — MUST equal host user (zellij sockets are 0700, same-user effectively mandatory) |
| `XDG_RUNTIME_DIR` | host env | `/run/user/<uid>` | Must equal host value — the zellij CLI resolves sockets from it; mismatch = blind dashboard |
| `HOME` | host env | — | Only used to expand the `:ro` volume sources to your home (home itself is never mounted) |

Pinned at build: Go `1.27.3`, zellij `0.44.3` (major must match host), `gcr.io/distroless/static-debian12:nonroot`, image `opencode-dashboard:0.1.0`.

## API

Same-origin SPA + JSON; no CORS headers by design.

| Route | Shape / semantics |
|---|---|
| `GET /api/healthz` | `{healthy, version, db_path, last_poll_ms, stale}` — `db_path` is the configured `DATA_DIR`. `stale:true` during warm-up (or when a tick exceeds 2× `POLL_INTERVAL` / a source fails) is a diagnostic, not necessarily an outage |
| `GET /api/fleet` | `{seq, sessions:[SessionRow…], projects:[ProjectSummary…], kpis:{…}}` — `seq` rises per published tick; empty `sessions` is fine with no opencode running. `projects[]` groups by cleaned `session.directory` and is sorted live_procs desc → live_panes desc → running desc → last_activity asc → directory |
| `GET /api/sessions/:id` | Full detail: `{session, messages, todos, children}` from the live read-only DB. `404` unknown id, `503` backend unwired |
| `GET /api/sessions/:id/tools` | `{session_id, tools:[{name,status,duration_ms,tokens}]}`. `404` / `503` as above |
| `GET /api/sessions/:id/screen` | Live pane text (`text/plain`, `X-Pane-Id` / `X-Captured-At` headers, `?full=0` = viewport-only; full scrollback is the default). `409` orphan or pane-died-between-resolve-and-dump (`{error, db_only:true, hint}` — transcript/DB still available); `404` unknown; `503` backend unwired / no snapshot yet; `504` dump timed out (10s) |
| `GET /api/events` (SSE) | `text/event-stream`: snapshot ticks emitted at a 500ms fan-out cadence + 20s keepalive; resume via `Last-Event-ID` / `?lastEventId=` with full-snapshot re-sync on reconnect; `503` hub closed. Under `DASHBOARD_AUTH`, authenticate with Basic once — the `ocd_session` cookie then authorizes `EventSource` (which cannot send an `Authorization` header) |
| `GET /api/debug/hub` | Drop counters + cadence (diagnostic only, not part of the frozen contract) |
| `GET /` | Embedded SPA (API routes take precedence) |

### KPI semantics

| Field | Meaning |
|---|---|
| `active` / `waiting_input` / `errors` | Session counts by derived status. `waiting-input` is a documented heuristic (no pending/permission table exists) |
| `tokens_per_hour` | Message tokens created in the trailing 60s × 60. Measured from message rows (not rolled-up session totals); a background refresher runs every 60s and the value decays to 0 when idle |
| `metrics_age_ms` | Age of that message-level sample. `-1` = no successful refresh yet (unknown, not zero) |
| `cost_today` | Message-level USD spend since **local midnight** (`TZ` or `/etc/localtime`; without a zone the container is UTC and “today” starts early) |
| `cost_total` | Lifetime USD spend, summing each session's own pre-roll-up cost (subagent children are not double-counted) |
| `live_processes` | Exact count of alive opencode processes observed via `/proc` on the last tick |
| `live_panes` | Count of non-exited zellij panes observed on the last tick. Compare with `active`: far more running rows than panes means stale status, not hidden work |
| `sse_connected` | Hub event-stream health |

`ProjectSummary` carries `{directory, name, sessions, running, waiting_input, idle, tokens_total, cost_total, live_procs, live_panes, last_activity_ms}`. `live_procs` is `/proc`-derived on the host; in a container where `/proc/<pid>/cwd` is denied, it falls back to non-exited zellij panes running opencode (presence, not an exact count).

v1 is read-only: no abort/resurrect/prompt/message endpoints. Why + future sketch: `docs/ACTIONS_DEFERRED.md`.

## Development

```bash
go test -count=1 ./...            # full backend suite (229 tests / 10 packages at HEAD)
go test -count=1 ./internal/db/  # per-package, e.g. db / runtime / hub / collector / auth
go vet ./...                      # static checks
go build ./cmd/dashboard          # single binary (embeds web/dist)
```

```bash
npm run lint --prefix web         # tsc --noEmit (type gate)
npm test --prefix web -- --run    # vitest run (193 tests / 26 files at HEAD)
npm run build --prefix web        # tsc + vite build → web/dist (committed fallback)
```

`deploy.sh` rebuilds `web/dist` automatically when `web/src/**` is newer than `web/dist/index.html` (and fails loud with install instructions if `npm` is missing); otherwise the committed `dist` is reused so no-Node hosts still work.

## Project structure

| Path | One line |
|---|---|
| `deploy.sh` | One-click deploy: prereqs → dist → `up --build` → health wait → curl proofs → banner / `--down` teardown |
| `docker-compose.yml` | Plug-n-play spec: `pid:host` + `network_mode:host`, UID parity, `:ro` allowlist (`:rw` socket dir only), no `ports:` stanza, exec healthcheck |
| `Dockerfile` | 3-stage build: pinned zellij CLI → Go static binary (embeds SPA) → distroless runtime |
| `cmd/dashboard/` | Entrypoint: loopback assert, loud self-checks, seam wiring, API+SPA on one listener, `-healthcheck` probe |
| `internal/ports/` | Frozen seams: `DBReader` / `RuntimeSource` / `SnapshotSink` / env `Config` |
| `internal/db/` | Read-only WAL reader: indexed per-session slices only (full `part.data LIKE` scans banned — 8.2 GB budget) |
| `internal/runtime/` | zellij CLI + `/proc` join: liveness #1, orphans, zombie-`Z`=dead, on-demand `dump-screen` |
| `internal/collector/` | Poll loop → `Snapshot{seq}`; stale-frame self-heal; never per-tick screen |
| `internal/hub/` | SSE fan-out (resume + re-sync) + REST handlers + frozen response shapes |
| `internal/model/` | `SessionRow`, `Snapshot`, status derivation |
| `internal/auth/` | `DASHBOARD_AUTH` gate: Basic-or-cookie-or-open, fail-closed, zero logging (test-pinned) |
| `web/src/views/` | `Projects` (default landing), `Fleet` (filters/pagination), `SessionDetail` (6 live tabs + Live pane), `Compare`, `GlobalTimeline` |
| `web/src/components/` | KpiStrip, EventTicker, CmdBar, StatusBadge, Sparkline, status/recency filters, Pagination, SubagentPanel |
| `web/src/lib/` | `sse.ts` transport (reconnect/backoff/resume), sorting/filtering, projects, pins, routing, formatting |
| `web/src/testutils/` | Snapshot + `sessionDetail.fixture.ts` (test-only `SessionDataProvider` implementation) |
| `web/dist/` | Committed SPA build (fallback so containers build without Node) |
| `docs/` | `DOCKER_VERIFY.md` (rerun playbook) · `VM_DEPLOY.md` (VM runbook + F2 truth matrix) · `SECURITY.md` (threat model + gate checklist) · `ACTIONS_DEFERRED.md` (why v1 is read-only) |
| `.omo/` | Work plan + research notepads (decisions, issues, runtime-observability findings) |

## Security (summary)

Read-only v1: no prompt injection path, no shell exec from the UI. Mount allowlist: the opencode parent dir (DB + `-wal`/`-shm` + storage), zellij cache, and `/etc/localtime` are `:ro`; only the zellij socket dir is `:rw` because the CLI mints a per-call client socket. `auth.json` and credential/account stores are never mounted and no query touches those tables. Loopback-only by default (Go bind assert on the configured address; compose carries no `ports:` stanza; `tailscale serve` fronting) — **funnel is banned**: Basic auth is base64 (reversible, not encryption) and must never face the public internet. A non-loopback `BIND_ADDR` is allowed only with valid auth. When `DASHBOARD_AUTH` is set, Basic gates the API + SPA and a successful check mints an HttpOnly `ocd_session` cookie (12h, bounded 32-token store, `SameSite=Lax`, `Secure` over TLS) so SSE can authenticate. No secrets in image/API/logs. Full model: `docs/SECURITY.md` (§5 checklist runnable verbatim).

## Runbooks

- **VM deploy + F2 truth matrix:** `docs/VM_DEPLOY.md` (§0 prereqs, §1 deploy, §3 live-pane/orphan/EXITED/zombie checks).
- **Deferred actions (abort/resurrect):** `docs/ACTIONS_DEFERRED.md` — design sketch only, no code.

## Troubleshooting

| Symptom | Cause → fix |
|---|---|
| Blind dashboard (fleet empty, zero panes, looks healthy) | UID/XDG/major skew: `id -u` vs `docker exec <ctr> id -u`; `echo $XDG_RUNTIME_DIR` vs container value; host `zellij --version` major vs baked `0.44.3`; `ls $XDG_RUNTIME_DIR/zellij/contract_version_1/` must list live sessions → `docker compose down`, fix env, `up -d --build` (details: `docs/VM_DEPLOY.md` §4) |
| Stale fleet (`stale:true` persists) | WAL sidecars hidden (mount must be the parent `opencode/` dir, never the bare `.db` file) or DB budget pressure (`DB_TIMEOUT` exceeded; first 2–3 polls normal — wait, don't redeploy): `docker inspect <ctr> --format '{{json .Mounts}}'` must equal the allowlist (`:ro` everywhere except the socket dir, which is `:rw` so the CLI can mint its client socket). Check the collector log for `context deadline exceeded`; raise `DB_TIMEOUT` or lower `POLL_INTERVAL` pressure rather than widening mounts |
| `cost_today` looks wrong (too high early in the day) | Container has no zone: `cost_today` uses local midnight, so a UTC container starts “today” hours early on e.g. a WIB host. `deploy.sh` forwards `TZ` from the host and compose mounts `/etc/localtime:ro`; verify `docker exec <ctr> date` and `printenv TZ` match the host |
| Project `live_procs`/`live_panes` disagree or look stale | Per-directory presence is a signal, not a process census. On the host `live_procs` is `/proc/<pid>/cwd`-derived; in-container that readlink is denied, so it falls back to non-exited zellij panes running opencode and may briefly include a just-exited pane. `live_processes` remains the exact `/proc` count |
| Port taken (`address already in use`) | Squatted host port (8080 was squatted on the dev host): `./deploy.sh` auto-bumps and says so — or pin one: `./deploy.sh --port 18082` (host mode: `$PORT` IS the listen port, no container/host split) |
| `0.0.0.0` confusion (bound/published openly) | Default posture forbids it: the binary binds `127.0.0.1` unless `BIND_ADDR` is explicitly set, and a non-loopback value refuses to start without valid `DASHBOARD_AUTH`. Compose has NO `ports:` stanza (host mode — nothing to misconfigure); `ss -tlnp \| grep $PORT` must show the dashboard binary directly (no `docker-proxy` line) |
| zellij major skew, no loud FATAL | Socket contract dir (`contract_version_1`) is version-sensitive; skew has no dedicated FATAL — it surfaces as blindness (boot log line `zellij detected` shows baked version). Majors differ → rebuild image with matching `ZELLIJ_VERSION`, do not proceed |

## Known limitations

- **Read-only v1.** No abort, resurrect, prompt, message, or shell endpoint. Design
  sketch + open questions: `docs/ACTIONS_DEFERRED.md`.
- **Single host.** The collector joins one host's zellij + `/proc`; there is no `host_id`
  plumbing or multi-host fan-in yet.
- **Drill-down is live.** `GET /api/sessions/:id/export` returns the real session plus
  children (export-shaped), and the SPA renders it; the **Live pane** tab calls
  `GET /api/sessions/:id/screen` on view (409 + `{db_only,hint}` when a session has no
  pane). The fixture in `web/src/testutils/` is test-only. Expect a loading state and an
  empty state for unknown ids.
- **`waiting-input` is a heuristic.** opencode has no pending/permission table; the state
  means “user spoke last, nothing in flight, recent” (plus compacting).
- **Recency window can read idle during a long silent tool call.** `ACTIVE_WINDOW` (2m)
  gates status on `time_updated`; a genuinely busy turn that produces no events for a
  full window will show idle.
- **Per-directory presence is pane-derived in-container.** `/proc/<pid>/cwd` is denied
  under ptrace/Yama, so `projects[].live_procs` falls back to non-exited zellij panes
  running opencode; `live_processes` remains the exact `/proc` count.
- **Metrics can be up to ~60 s stale.** `tokens_per_hour` / `cost_today` come from a
  background message-level refresh; read `metrics_age_ms` before trusting them.
- **F2 VM truth matrix is still outstanding.** The manual 3-pane / orphan / EXITED /
  zombie / CWD-mismatch matrix in `docs/VM_DEPLOY.md` §3 is `[~]` in
  `.omo/plans/opencode-dashboard.md`.


## License

MIT — see [LICENSE](./LICENSE).

Bundled-dependency licenses live in their own packages (`go.mod` / `package-lock.json` record them; `web/dist/` is built output of already-listed deps).
