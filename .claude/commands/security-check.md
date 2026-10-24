---
description: Review current uncommitted changes against the Security Gate.
---

# /security-check

Review current uncommitted changes (`git diff`) against the Security Gate in
`SECURITY_STANDARDS.md`:

1. Run `git diff --stat` + `git diff` to enumerate changes.
2. Check each change against every Security Gate item.
3. Report findings by severity (Critical / High / Medium / Low) with file and
   line references.
4. Cover: secrets exposure, input validation, authN/authZ, dependency risk,
   error handling, dev-only leakage, container/config privilege, test coverage,
   `.env.example` staleness.

Do not auto-fix without asking. If a secret is exposed: stop, tell the user,
recommend rotation.
