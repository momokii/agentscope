---
description: Perform the wrap-up checklist and update all state files in one step.
---

# /wrap-up

Perform the Wrap-Up checklist from `AGENT_RULES.md`. Do **all writes together
in one step** (`.claude/` is a protected path — minimize prompts):

- Update `state/CURRENT_STATUS.md` (keep under ~80 lines; move older summaries
  to `state/HISTORY.md`).
- Update `state/TASK_QUEUE.md` (mark done, add discovered tasks).
- Log significant decisions in `state/DECISIONS_LOG.md`.
- Update `CODING_STANDARDS.md`, `SECURITY_STANDARDS.md`, `ENVIRONMENT_GUIDE.md`,
  `CLAUDE.md` (Commands + Snapshot), `README.md` if they changed.

Print a short summary of what was updated.
