# AGENTS.md — harness-neutral entry point

This file is the front door for any coding agent (Codex, Cursor, opencode,
Claude Code, …). It summarizes the repo and points at the authoritative docs;
when this file and the source disagree, trust the source and the docs listed at
the bottom.

## What this is

Internal-only, read-only realtime monitor for opencode agent sessions running
inside zellij on one VM. A single Go binary serves REST + SSE and embeds a React
SPA; it is deployed as one Docker container with `./deploy.sh` and reached over
Tailscale. There is no multi-host support, no write/action surface, and no
public exposure.

## Quick start

```bash
./deploy.sh                # prereqs → build → up → verified banner
./deploy.sh --port 18082   # pin the listen port
./deploy.sh --down         # tear down and verify nothing runs/listens
```

Required host env for a correct deploy:

| Var | Why |
|---|---|
| `BIND_ADDR` | Listen address; default `127.0.0.1`. Non-loopback is opt-in and refused without valid auth |
| `DASHBOARD_AUTH` | `user:pass` enables the Basic gate (+ `ocd_session` cookie for SSE); unset = open tailnet-trust mode |
| `TZ` | Local zone for `cost_today` (local-midnight spend) and log stamps |
| `UID` / `GID` | Container user must equal the host user (`export UID`; `GID=$(id -g)`) |
| `XDG_RUNTIME_DIR` | Must equal the host value; the zellij CLI resolves sockets from it |

Open the printed URL — `http://127.0.0.1:8080` by default. If
`DASHBOARD_AUTH` is set, the browser prompts for Basic auth; the same
credentials gate `/api/*`. To expose on the tailnet, use
`tailscale serve --bg --https=443 http://127.0.0.1:$PORT` (never funnel).

## Verify before changing anything

```bash
go build ./...
go vet ./...
go test -count=1 ./...
npm run lint --prefix web          # tsc --noEmit
npm test --prefix web -- --run     # vitest
npm run build --prefix web         # → web/dist
grep -rE '#[0-9a-fA-F]{6}' web/src # must hit only src/tokens.css
```

`./deploy.sh` rebuilds `web/dist` when `web/src` is newer; otherwise the
committed `dist` is reused so no-Node hosts still work.

## Where things live

- `cmd/dashboard/` — entrypoint, startup self-checks, auth wiring, `-healthcheck`
- `internal/model/` — `SessionRow`, `Snapshot`, KPIs, status derivation
- `internal/ports/` — frozen seams (`DBReader`, `RuntimeSource`, `SnapshotSink`)
- `internal/db/` — read-only WAL reader and indexed per-session queries
- `internal/runtime/` — zellij CLI + `/proc` join, orphans/zombies, on-demand screen
- `internal/collector/` — poll loop, event tail, metrics, project roll-up
- `internal/hub/` — SSE fan-out, REST handlers, response shapes
- `internal/auth/` — Basic gate and `ocd_session` cookie bridge
- `web/src/views/` — `Projects`, `Fleet`, `SessionDetail`, `Compare`, `GlobalTimeline`
- `web/src/components/` — KPI strip, ticker, filters, pagination, subagents
- `web/src/lib/` — SSE client, filtering/sorting, routing, pins, formatting
- `web/DESIGN.md` — design tokens and UI rules (authoritative for color)
- `docs/` — security, VM runbook, Docker verify, deferred actions
- `.omo/plans/` — authoritative work plan
- `.claude/state/` — current status, task queue, decisions, history
- `deploy.sh` — one-command deploy/teardown
- `docker-compose.yml` — host-networked single container, read-only mounts
- `Dockerfile` — zellij + Go build → distroless runtime

## Non-negotiable rules

- **Read-only v1.** No abort, resurrect, prompt, message, or shell endpoint.
  `docs/ACTIONS_DEFERRED.md` is a design sketch, not shipped code.
- **Never mount `auth.json`** or credential/account tables; no query touches them.
- **Loopback by default.** A non-loopback `BIND_ADDR` requires valid
  `DASHBOARD_AUTH` and the binary refuses to start otherwise.
- **Funnel is banned.** Use `tailscale serve`; Basic auth is base64, not encryption.
- **All UI color comes from `web/DESIGN.md` tokens.** No 6-digit hex outside
  `web/src/tokens.css`; status is never conveyed by color alone.
- **Statuses are recency-gated** by `ACTIVE_WINDOW` (default `2m`): only recent
  `time_updated` rows may show running/waiting-input/error; older rows derive idle.
- **Per-directory presence is pane-derived inside containers** (`/proc/<pid>/cwd`
  is denied), so it is a presence signal, not an exact process count.

## Current state

v1 is feature-complete on `main`. The plan is 19/20 checked; the only open plan
item is **F2**, the human-run VM truth matrix in `docs/VM_DEPLOY.md` §0–§3
(`[~]`, not done — agents must not fabricate it by killing live sessions).

Drill-down and the Live pane tab are live: `GET /api/sessions/:id/export`
serves the real session plus children (export-shaped), and
`GET /api/sessions/:id/screen` returns live pane text (409 when a session has no
pane). The fixture in `web/src/testutils/` is test-only.

## Where to read more

- `README.md` — product, config, API, runbooks
- `.claude/state/CURRENT_STATUS.md` — live state and known gaps
- `.claude/state/TASK_QUEUE.md` — next work and backlog
- `.claude/state/DECISIONS_LOG.md` — decisions and rationale
- `.claude/state/HISTORY.md` — past session summaries
- `docs/SECURITY.md` — threat model and Security Gate
- `docs/VM_DEPLOY.md` — VM deploy + F2 truth matrix
- `docs/ACTIONS_DEFERRED.md` — why v1 is read-only
- `.omo/plans/opencode-dashboard.md` — authoritative work plan
