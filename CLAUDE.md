# CLAUDE.md — Agent Entry Point

> Auto-loaded by Claude Code at session start. Keep under ~150 lines.
> Link to deeper docs instead of duplicating them.

## Project Snapshot

- **Purpose:** Not yet defined. Update after the first working session.
- **Stack:** Not yet chosen. Stack-agnostic until TASK-001 completes.
- **Phase:** Initialization — `.claude/` infrastructure created, no product code yet.

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

| Purpose | Command | Status |
|---|---|---|
| Run dev server | — | not yet defined |
| Run tests | — | not yet defined |
| Run linter | — | not yet defined |
| Type check | — | not yet defined |
| Health check | — | not yet defined |
| Build | — | not yet defined |

> Agent: fill this table in once the stack exists (TASK-001). Keep commands single-entry.

## On-Demand References

| When you need | Read |
|---|---|
| Next work / backlog | `.claude/state/TASK_QUEUE.md` |
| Past decisions + rationale | `.claude/state/DECISIONS_LOG.md` |
| Session history | `.claude/state/HISTORY.md` |
| Dev/staging/prod rules, Compose pattern | `.claude/ENVIRONMENT_GUIDE.md` |
| Full resume steps (Steps 1–11) | `.claude/HOW_TO_RESUME.md` |
| Feature checklist | `.claude/templates/new_feature.md` |
| Endpoint checklist | `.claude/templates/new_endpoint.md` |
| Test checklist | `.claude/templates/new_test.md` |
| Bug-fix checklist | `.claude/templates/bug_fix.md` |
| Orientation / file map | `.claude/README.md` |
| Slash commands | `.claude/commands/resume.md`, `wrap-up.md`, `security-check.md` |

## Maintenance Rules for This File

- Keep under ~150 lines. Link, never duplicate.
- Fix immediately when inaccurate (snapshot, stack, commands).
- Imports stay at exactly the 4 files above unless user approves a change.
- Project-specific updates go here only if always relevant; else on-demand file.
- Local overrides belong in `CLAUDE.local.md` (gitignored), never here.
