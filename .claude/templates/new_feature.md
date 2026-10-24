# New Feature Checklist

## Before Starting
- [ ] Task exists in TASK_QUEUE.md with acceptance criteria; dependencies complete
- [ ] Relevant docs read; baseline tests pass
- [ ] Active environment identified (development)

## Design
- [ ] Scope defined: list every file to create or modify
- [ ] Edge cases and security implications identified before coding
- [ ] New dependency or schema change: user confirmation obtained; legitimacy verified,
      audited, pinned, and logged in DECISIONS_LOG.md

## Implementation
- [ ] Follows CODING_STANDARDS.md
- [ ] All failure paths handled; no sensitive data in logs

## Security Gate (see SECURITY_STANDARDS.md)
- [ ] No secrets in code, logs, or tests
- [ ] Input validated at the boundary; output encoded; queries parameterized
- [ ] AuthN/AuthZ enforced, default deny, object-level checks
- [ ] Errors to clients are generic
- [ ] Works correctly with development AND production configuration; dev-only features gated
- [ ] `.env.example` updated if variables were added

## Testing
- [ ] Unit tests for new logic; integration tests where external systems are touched
- [ ] Negative/security tests included
- [ ] Lint and type checks pass; all tests pass

## Completion
- [ ] TASK_QUEUE.md, CURRENT_STATUS.md, DECISIONS_LOG.md updated (/wrap-up)
- [ ] Affected docs updated
