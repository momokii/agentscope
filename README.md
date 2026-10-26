# opencode mission control

Single-container, read-only monitoring dashboard for opencode agent sessions running on one VM (opencode inside zellij panes, reached via tailscale). Open one URL and see everything in <2s: every session's status (running / idle / waiting-input / error / archived), tokens/cost, transcript + tool calls + subagents, per-pane live text — with zero ability to leak credentials and zero public exposure.

**Operator story:** you have three opencode TUIs burning tokens across projects and no idea which one is stuck waiting on you. You open the dashboard, the fleet table shows all panes with live status plus one orphan (opencode running outside zellij), the stuck one glows amber waiting-input, you click through to its transcript — all without touching `auth.json`, provider keys, or the public internet.

## 60-second quickstart

```bash
./deploy.sh            # one command: prereqs → build → up → verified banner
```

Open the printed URL (`http://127.0.0.1:8080` by default). You should see: a KPI strip (active / waiting-input / tokens-hr / cost / errors / SSE dot), the fleet table (one row per session, sorted running-first), and a live event ticker. Empty sessions list is fine on a host with no opencode running. Teardown: `./deploy.sh --down`.

To expose it on your tailnet afterwards: `tailscale serve --bg --https=443 http://127.0.0.1:8080` (serve, never funnel — see security).

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
| `POLL_INTERVAL` | host env → compose | `10s` | Collector tick (DB+zellij+proc join → snapshot). First tick may report `stale:true` — normal warm-up |
| `DATA_DIR` | compose `environment` | `/data/opencode` | Read-only opencode data root in-container (parent dir mount keeps `-wal`/`-shm` sidecars visible) |
| `DASHBOARD_AUTH` | host env | unset (= open tailnet-trust mode) | `user:pass` (split on first colon) enables Basic gate; malformed (no colon/empty part) = fail-closed refuse-to-serve. Value never logged |
| `UID` / `GID` | host (`export UID`; `GID=$(id -g)`) | `1000:1000` | Container user — MUST equal host user (zellij sockets are 0700, same-user effectively mandatory) |
| `XDG_RUNTIME_DIR` | host env | `/run/user/<uid>` | Must equal host value — the zellij CLI resolves sockets from it; mismatch = blind dashboard |
| `HOME` | host env | — | Only used to expand the `:ro` volume sources to your home (home itself is never mounted) |

Pinned at build: Go `1.27.3`, zellij `0.44.3` (major must match host), `gcr.io/distroless/static-debian12:nonroot`, image `opencode-dashboard:0.1.0`.

## API

Same-origin SPA + JSON; no CORS headers by design.

| Route | Shape / semantics |
|---|---|
| `GET /api/healthz` | `{healthy, version, db_path, last_poll_ms, stale}` — `stale:true` on the first tick only is normal (collector warming up on multi-GB DBs) |
| `GET /api/fleet` | `{seq, sessions:[SessionRow…], kpis:{…}}` — `seq` rises every poll; empty `sessions` is fine with no opencode running |
| `GET /api/sessions/:id` | Full detail: row + messages + parts + todos + children. `404` unknown id, `503` backend unwired |
| `GET /api/sessions/:id/tools` | Tool-call list w/ status/duration/tokens. `404` / `503` as above |
| `GET /api/sessions/:id/screen` | Live pane text (`text/plain`, `X-Pane-Id` / `X-Captured-At` headers, `?full=0` = viewport-only). `409` orphan or pane-died-between-resolve-and-dump (`{error, db_only:true, hint}` — transcript/DB still available); `404` unknown; `503` backend unwired / no snapshot yet; `504` dump timed out |
| `GET /api/events` (SSE) | `text/event-stream`: snapshot ticks + 15–30s keepalive; resume via `Last-Event-ID` / `?lastEventId=` with full-snapshot re-sync on reconnect; `503` hub closed |
| `GET /api/debug/hub` | Drop counters + cadence (diagnostic only, not part of the frozen contract) |
| `GET /` | Embedded SPA (API routes take precedence) |

v1 is read-only: no abort/resurrect/prompt/message endpoints. Why + future sketch: `docs/ACTIONS_DEFERRED.md`.

## Development

```bash
go test -count=1 ./...            # full backend suite (142 tests)
go test -count=1 ./internal/db/  # per-package, e.g. db / runtime / hub / collector / auth
go vet ./...                      # static checks
go build ./cmd/dashboard          # single binary (embeds web/dist)
```

```bash
npm run lint --prefix web         # tsc --noEmit (type gate)
npm test --prefix web -- --run    # vitest run (127 tests)
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
| `internal/auth/` | `DASHBOARD_AUTH` gate: Basic-or-open, fail-closed, zero logging (test-pinned) |
| `web/src/views/` | Fleet (default landing), SessionDetail (6 tabs), Compare, GlobalTimeline |
| `web/src/components/` | KpiStrip, EventTicker, CmdBar, StatusBadge, Sparkline |
| `web/src/lib/` | `sse.ts` transport (reconnect/backoff/resume), sorting, pins, routing, formatting |
| `web/dist/` | Committed SPA build (fallback so containers build without Node) |
| `docs/` | `DOCKER_VERIFY.md` (rerun playbook) · `VM_DEPLOY.md` (VM runbook + F2 truth matrix) · `SECURITY.md` (threat model + gate checklist) · `ACTIONS_DEFERRED.md` (why v1 is read-only) |
| `.omo/` | Work plan + research notepads (decisions, issues, runtime-observability findings) |

## Security (summary)

Read-only v1: no prompt injection path, no shell exec from the UI. Four-path mount allowlist, every mount `:ro` except the zellij socket dir (opencode parent dir for WAL sidecars, storage, zellij cache stay `:ro`; socket dir is `:rw` because the CLI mints a per-call client socket) — `auth.json` and credential/account stores are never mounted and no query touches those tables. Loopback-only ×3 (Go bind assert on host loopback, compose carries no `ports:` stanza to misconfigure, `tailscale serve` fronting) — **funnel is banned**: Basic auth is base64 (reversible, not encryption) and must never face the public internet. No secrets in image/API/logs. Full model: `docs/SECURITY.md` (§5 checklist runnable verbatim).

## Runbooks

- **VM deploy + F2 truth matrix:** `docs/VM_DEPLOY.md` (§0 prereqs, §1 deploy, §3 live-pane/orphan/EXITED/zombie checks).
- **Deferred actions (abort/resurrect):** `docs/ACTIONS_DEFERRED.md` — design sketch only, no code.

## Troubleshooting

| Symptom | Cause → fix |
|---|---|
| Blind dashboard (fleet empty, zero panes, looks healthy) | UID/XDG/major skew: `id -u` vs `docker exec <ctr> id -u`; `echo $XDG_RUNTIME_DIR` vs container value; host `zellij --version` major vs baked `0.44.3`; `ls $XDG_RUNTIME_DIR/zellij/contract_version_1/` must list live sessions → `docker compose down`, fix env, `up -d --build` (details: `docs/VM_DEPLOY.md` §4) |
| Stale fleet (`stale:true` persists) | WAL sidecars hidden (mount must be the parent `opencode/` dir, never the bare `.db` file) or 8.2 GB budget pressure (first 2–3 polls normal — wait, don't redeploy): `docker inspect <ctr> --format '{{json .Mounts}}'` must equal the allowlist (`:ro` everywhere except the socket dir, which is `:rw` so the CLI can mint its client socket) |
| Port taken (`address already in use`) | Squatted host port (8080 was squatted on the dev host): `./deploy.sh` auto-bumps and says so — or pin one: `./deploy.sh --port 18082` (host mode: `$PORT` IS the listen port, no container/host split) |
| `0.0.0.0` confusion (bound/published openly) | Forbidden by design: Go asserts loopback at startup; compose has NO `ports:` stanza (host mode — nothing to misconfigure); `ss -tlnp \| grep $PORT` must show `127.0.0.1:$PORT` held by the dashboard binary (no `docker-proxy` line) and NO `0.0.0.0:$PORT` line |
| zellij major skew, no loud FATAL | Socket contract dir (`contract_version_1`) is version-sensitive; skew has no dedicated FATAL — it surfaces as blindness (boot log line `zellij detected` shows baked version). Majors differ → rebuild image with matching `ZELLIJ_VERSION`, do not proceed |

## License

MIT — see [LICENSE](./LICENSE).

Bundled-dependency licenses live in their own packages (`go.mod` / `package-lock.json` record them; `web/dist/` is built output of already-listed deps).
