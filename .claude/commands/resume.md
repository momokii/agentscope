---
description: Run the resume protocol and report state without starting implementation.
---

# /resume

Execute `.claude/HOW_TO_RESUME.md` Steps 1–7, then report in under 15 lines:

1. Confirm context loaded (`CLAUDE.md` + 4 imports) or read manually.
2. Read `state/TASK_QUEUE.md` → current / next task + dependencies.
3. Identify `APP_ENV` via `ENVIRONMENT_GUIDE.md`.
4. Check git state (branch, uncommitted changes, recent commits).
5. Read task-relevant docs if they exist.
6. Verify environment (health-check / startup command).
7. Run baseline test suite.

Report: current state, next task, environment, baseline test result, blockers.
Do not start implementation until the user confirms.
