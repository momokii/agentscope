# Bug Fix Checklist

## Reproduce First
- [ ] Documented repro steps; expected vs. actual behavior stated
- [ ] Reproduced in development only; no code touched before reproduction

## Root Cause
- [ ] Root cause identified and documented before fixing
- [ ] Same bug checked in related areas
- [ ] Security impact assessed (data exposure, auth bypass, injection). If yes,
      escalate to the user immediately

## Fix
- [ ] Minimal, targeted fix; no opportunistic refactoring or scope creep

## Verification
- [ ] Bug no longer reproduces; regression test written and passing
- [ ] Full test suite, lint, and type checks pass

## Completion
- [ ] DECISIONS_LOG.md updated if the root cause revealed an important insight
- [ ] CURRENT_STATUS.md updated
