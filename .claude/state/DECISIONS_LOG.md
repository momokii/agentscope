# DECISIONS_LOG — Decisions and Rationale

---
**Decision:** Root `CLAUDE.md` as auto-loaded entry point importing 4 compact files
**Date:** 2026-10-24
**Context:** Claude Code auto-loads only `CLAUDE.md`; other `.claude/` files are on-demand
**Rationale:** Single entry point guarantees zero-manual-briefing sessions; imports stay
compact to protect context budget; deeper docs linked, not pasted
**Alternatives Rejected:** Single giant file (blows context); no imports (manual briefing each session)
**Security Implications:** Security rules always loaded via `SECURITY_STANDARDS.md` import
**Impact:** All sessions start with rules, standards, and living state
---

---
**Decision:** Security-first `settings.json` baseline (deny → ask → allow, no defaultMode)
**Date:** 2026-10-24
**Context:** Permissions are enforcement; `CLAUDE.md` text is context only
**Rationale:** Deny secrets + destructive git + sudo/chmod-777; ask on push/network/destructive
Docker; allow only safe read-only git + dev Compose introspection. Validated against
`https://code.claude.com/docs/en/permissions` (deny→ask→allow order; Bash text-match
limits; Read/Edit coverage incl. redirections, excl. indirect readers)
**Alternatives Rejected:** Broad `Bash(*)` allow (unsafe); `.env.*` wildcard deny (would block `.env.example`)
**Security Implications:** Bash rules are text-match, not a boundary — sandboxing recommended for OS-level enforcement
**Impact:** Safe defaults; stack-specific allow-list proposed after TASK-001 with confirmation
---

---
**Decision:** Docker Compose v2 dev/prod file pattern
**Date:** 2026-10-24
**Context:** Need one dev and one prod path without hardcoded conditionals
**Rationale:** `compose.yaml` base + auto-loaded `compose.override.yaml` (dev) +
explicit `-f compose.prod.yaml` (prod); matches upstream Compose v2 behavior
**Alternatives Rejected:** Single file with env conditionals (drifts); `docker-compose` v1 syntax (obsolete)
**Security Implications:** Prod file pins images, drops mounts/debug ports, enforces limits
**Impact:** Documented in `ENVIRONMENT_GUIDE.md`; verified once real Compose files exist
---

## Template (copy for every new decision)

```markdown
---
**Decision:** [What was decided]
**Date:** [YYYY-MM-DD]
**Context:** [Why it was needed]
**Rationale:** [Why this option]
**Alternatives Rejected:** [Other options and why not]
**Security Implications:** [Any security impact]
**Impact:** [What it affects downstream]
---
```
