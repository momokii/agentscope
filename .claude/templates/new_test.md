# New Test Checklist

## Before Starting
- [ ] Objective defined: what behavior is verified
- [ ] Type identified: unit / integration / end-to-end / load
- [ ] Active environment confirmed; load or destructive tests NEVER run against
      staging or production

## Implementation
- [ ] Follows the project's test naming and structure conventions
- [ ] Isolated; no reliance on shared external state unless intentional
- [ ] No real secrets or production data; use test doubles or dedicated test credentials
- [ ] Setup and teardown are clean
- [ ] Assertions are specific and meaningful

## Completion
- [ ] Passes reliably (run at least 3 times); included in the standard test command
- [ ] CURRENT_STATUS.md updated
