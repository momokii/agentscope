# TASK_QUEUE — Ordered Backlog

## TASK-001: Establish project foundation
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Depends On:** user confirmation of purpose and stack
- **Scope:** confirm purpose and stack with the user; extend `.gitignore` for the
  stack; set up formatter, linter, type check, and test tooling; set up the
  Compose dev/prod pattern if containerized; update `CODING_STANDARDS.md`,
  `SECURITY_STANDARDS.md`, `ENVIRONMENT_GUIDE.md`, `HOW_TO_RESUME.md` commands,
  the `CLAUDE.md` Commands table, and propose a stack-specific `settings.json`
  allow-list.
- **Acceptance Criteria:** one command each to run, test, lint, and health-check;
  all `.claude/` files reflect the real stack.
- **Security Concerns:** new tooling + dependencies must be verified, pinned,
  audited, and logged; no secrets in config.
- Perform as part of, or immediately before, the user's first real task.

## TASK-002: Add automated security guardrails
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Depends On:** TASK-001
- **Scope:** propose and, with user confirmation, add pre-commit secret scanning,
  dependency audit, static analysis, image scanning if containerized, and a CI
  pipeline running tests, lint, type checks and scans.
- **Acceptance Criteria:** scans run locally and in CI; failures block merge.
- **Security Concerns:** guardrails themselves must not leak secrets in logs.

---

## Task Template (copy for every new task)

```markdown
| Field               | Value                                               |
|---------------------|-----------------------------------------------------|
| Task ID             | TASK-XXX                                            |
| Name                | [Task name]                                         |
| Priority            | High / Medium / Low                                 |
| Status              | TODO / IN PROGRESS / DONE / BLOCKED                 |
| Complexity          | S / M / L                                           |
| Depends On          | [Task IDs]                                          |
| Scope               | [Exact description of what must be built]           |
| Acceptance Criteria | [Measurable definition of done]                     |
| Security Concerns   | [Security considerations for this task]             |
```
