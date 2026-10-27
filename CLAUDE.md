# CLAUDE.md — Agent Entry Point

> Auto-loaded by Claude Code at session start. Keep under ~150 lines.
> Link to deeper docs instead of duplicating them.

## Project Snapshot

- **Purpose:** internal-only, read-only realtime monitoring dashboard for opencode agent
  sessions running inside zellij on one VM. Reached over Tailscale, deployed as a single
  Docker container with `./deploy.sh`.
- **Stack:** Go 1.27.3 single binary (net/http, SSE, `modernc.org/sqlite` pure Go, CGO off)
  + React 19 / Vite 6 / TypeScript 5.6.3 / vitest 3.2.4; hand-rolled CSS driven by
  `web/DESIGN.md` tokens; SPA embedded via `go:embed` (`web/embed.go`); multi-stage
  Dockerfile → `gcr.io/distroless/static-debian12:nonroot` with baked zellij 0.44.3.
- **Phase:** v1 feature-complete on `main`. The drill-down reads live export data
  (`/api/sessions/:id/export`) and the **Live pane** tab calls the screen route
  on demand. Only the human-run **F2 VM truth matrix** is outstanding
  (`.omo/plans/opencode-dashboard.md`).

## Always-Loaded Context

The files below are imported on every session. Keep them compact and always relevant.

@.claude/AGENT_RULES.md
@.claude/CODING_STANDARDS.md
@.claude/SECURITY_STANDARDS.md
@.claude/state/CURRENT_STATUS.md

## Session Protocol (Summary)

Full protocol: `.claude/HOW_TO_RESUME.md`. Short version:

1. Confirm context loaded (this file + 4 imports). If missing, read manually.
2. Read `.claude/state/TASK_QUEUE.md` — pick next task + dependencies.
3. Identify active environment (`APP_ENV`) via `.claude/ENVIRONMENT_GUIDE.md`.
4. Check git state: branch, uncommitted changes, recent commits.
5. Verify environment works (health-check) + run baseline test suite.
6. Plan (goal, files, risks, verification). Confirm if architectural/schema/dep change.
7. Implement using matching `.claude/templates/*` checklist.
8. Verify: tests + lint + type checks + Security Gate.
9. Wrap up: update state files in one step (`/wrap-up`).

## Commands

| Purpose | Command |
|---|---|
| Deploy / run (container) | `./deploy.sh` — add `--port N` to pin, `--down` to tear down |
| Run local binary | `go run ./cmd/dashboard` (needs `XDG_RUNTIME_DIR`, `DATA_DIR`, `zellij`, `opencode.db`; binds `127.0.0.1:$PORT`) |
| Backend tests | `go test -count=1 ./...` — 174 top-level tests (237 incl. subtests) / 8 of 10 packages at HEAD |
| Backend vet / build | `go vet ./...` · `go build ./cmd/dashboard` (embeds `web/dist`) |
| Web tests | `npm test --prefix web -- --run` — 208 tests / 28 files at HEAD |
| Web type check | `npm run lint --prefix web` (`tsc --noEmit`) |
| Web build | `npm run build --prefix web` → `web/dist` (committed fallback) |
| Health check | `curl -s 127.0.0.1:${PORT:-8080}/api/healthz` (add `-u "$DASHBOARD_AUTH"` when gated) |
| Token gate | `grep -rE '#[0-9a-fA-F]{6}' web/src` → must hit only `src/tokens.css` |

Counts are from a full run on 2026-10-27; re-run before quoting them.

## On-Demand References

| When you need | Read |
|---|---|
| Product overview, config, API, runbooks | `README.md` |
| Next work / backlog | `.claude/state/TASK_QUEUE.md` |
| Past decisions + rationale | `.claude/state/DECISIONS_LOG.md` |
| Session history | `.claude/state/HISTORY.md` |
| Dev/staging/prod rules, Compose pattern | `.claude/ENVIRONMENT_GUIDE.md` |
| Full resume steps (Steps 1–11) | `.claude/HOW_TO_RESUME.md` |
| VM deploy + F2 truth matrix | `docs/VM_DEPLOY.md` |
| Docker rerun playbook | `docs/DOCKER_VERIFY.md` |
| Threat model + Security Gate | `docs/SECURITY.md` |
| Why v1 is read-only / actions sketch | `docs/ACTIONS_DEFERRED.md` |
| Authoritative work plan | `.omo/plans/opencode-dashboard.md` |
| Session issues chronology | `.omo/notepads/opencode-dashboard/issues.md` |
| Feature / endpoint / test / bug checklists | `.claude/templates/*.md` |
| Orientation / file map | `.claude/README.md` |
| Slash commands | `.claude/commands/resume.md`, `wrap-up.md`, `security-check.md` |

## Maintenance Rules for This File

- Keep under ~150 lines. Link, never duplicate.
- Fix immediately when inaccurate (snapshot, stack, commands).
- Imports stay at exactly the 4 files above unless user approves a change.
- Project-specific updates go here only if always relevant; else on-demand file.
- Local overrides belong in `CLAUDE.local.md` (gitignored), never here.
