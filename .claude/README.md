# `.claude/` Agent Infrastructure — Orientation

> General-first by design. Starts stack-agnostic, then evolves into
> project-specific content as the stack, patterns, and decisions become known.

## What This Repository Is

Blank repository bootstrapped for iterative AI-assisted development.
No product code yet. This folder provides the agent operating system:
behavioral rules, standards, environment discipline, resume protocol,
checklists, and living state. Updated after the first working session.

## How Context Loads

- Root `CLAUDE.md` is **auto-loaded** at the start of every session and
  persists through context compaction.
- `CLAUDE.md` imports exactly 4 files (plain-text `@path` lines, never in
  code fences): `AGENT_RULES.md`, `CODING_STANDARDS.md`,
  `SECURITY_STANDARDS.md`, `state/CURRENT_STATUS.md`.
- Everything else here (`README.md`, `HOW_TO_RESUME.md`,
  `ENVIRONMENT_GUIDE.md`, `state/*`, `templates/*`, `commands/*`) is
  **read on demand** per the tables in `CLAUDE.md`.
- Confirm loading with `/context` — memory files should appear there.

## File Map

| File | Purpose |
|---|---|
| `../CLAUDE.md` | Auto-loaded entry point; snapshot + imports + protocol summary |
| `settings.json` | Shared permissions baseline (deny → ask → allow); committed |
| `README.md` | This orientation (humans + agents) |
| `AGENT_RULES.md` | Non-negotiable behavioral rules; always loaded |
| `CODING_STANDARDS.md` | Stack-agnostic standards; evolves; always loaded |
| `SECURITY_STANDARDS.md` | Security requirements + Security Gate; always loaded |
| `ENVIRONMENT_GUIDE.md` | dev/staging/prod definitions, Compose pattern, agent behavior |
| `HOW_TO_RESUME.md` | 11-step session-start protocol |
| `commands/resume.md` | `/resume` — run resume protocol through baselines, then report |
| `commands/wrap-up.md` | `/wrap-up` — perform wrap-up checklist in one step |
| `commands/security-check.md` | `/security-check` — review diff against Security Gate |
| `state/CURRENT_STATUS.md` | Living state, under ~80 lines; always loaded |
| `state/TASK_QUEUE.md` | Ordered backlog with task template |
| `state/DECISIONS_LOG.md` | Decisions + rationale + security implications |
| `state/HISTORY.md` | Append-only past session summaries |
| `templates/new_feature.md` | Checklist for features |
| `templates/new_endpoint.md` | Checklist for API endpoints |
| `templates/new_test.md` | Checklist for tests |
| `templates/bug_fix.md` | Checklist for bug fixes |

## `settings.json` Design Notes (Validated 2026-10-24)

Source: `https://code.claude.com/docs/en/permissions`. Evaluation order
is deny → ask → allow; first match wins, so deny always beats allow.
No `defaultMode` is set — Claude Code's built-in default applies.

- `.env.*` is **not** denied with a wildcard, because that would also block
  `.env.example`, which the agent must read/edit. Specific files denied instead.
- Read/Edit deny rules cover Claude's built-in file tools, recognized Bash
  file commands (`cat`, `head`, `tail`, `sed`, `tee`), and redirect targets.
  They do **not** cover indirect readers such as `grep -r` from the holding
  directory, or scripts that open files themselves. For OS-level enforcement,
  evaluate Claude Code's sandboxing feature. Not enabled without confirmation.
- Bash rules match command text as written and are **not a security boundary**.
  `Bash(curl *)` misses `/usr/bin/curl` or `sh -c 'curl ...'`; `Bash(git push *)`
  misses `git -C . push`. Same for path variants.
- Plain `docker compose up *` only matches default (development) invocations.
  Explicit `-f ... prod ...` invocations do not match allow and therefore prompt.
- Stack-specific commands (test runner, linter, package manager) are **not**
  pre-allowed. Propose an allow-list after stack choice; add with confirmation.
  Never add blanket `Bash(*)`.
- `.pre-commit-config.yaml`, `.husky/`, `.devcontainer/`, `.mcp.json` are also
  protected paths — edits prompt. Expected.
- Never create or modify `.claude/settings.local.json` (personal overrides).

## Protected Path: `.claude/`

In every permission mode except bypass, Claude Code never auto-approves
writes to `.claude/` (except `.claude/worktrees` and Claude's auto-memory).
`allow` rules like `Edit(.claude/**)` do **not** pre-approve them. Writes
prompt once (session-scoped "allow edits in this project's .claude folder"
option avoids repeats) or, in auto mode, route to the classifier.
Batch all `.claude/` writes into as few steps as practical. Never work around
via shell redirection, extra permission rules, or settings edits. If refused,
report to the user.

## Shared vs Local

- **Shared (committed):** everything in this folder except `settings.local.json`,
  plus root `CLAUDE.md`, `.gitignore`, `.env.example`.
- **Local (gitignored, never commit):** `CLAUDE.local.md` (personal prefs),
  `.claude/settings.local.json` (personal permission overrides).
- Never put secrets or personal data in shared files.
- Claude's own auto-memory is local to a machine — team-relevant knowledge
  must live in these versioned files instead.

## Self-Update Directive

Every file starts general by design. As facts become known:

1. Replace general content with project-specific content (stack, commands,
   patterns, Compose files, tooling).
2. Priority files: `CODING_STANDARDS.md`, `SECURITY_STANDARDS.md`,
   `ENVIRONMENT_GUIDE.md`, `CLAUDE.md` Commands + Snapshot, `HOW_TO_RESUME.md`.
3. Propose a stack-specific `settings.json` allow-list; add with confirmation.
4. Guardrail: extend/sharpen rules, never weaken `AGENT_RULES.md`,
   `SECURITY_STANDARDS.md`, or loosen `settings.json` without explicit approval.
5. Keep every file concise; fix stale content immediately.
