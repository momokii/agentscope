# CODING_STANDARDS — Stack-Agnostic Standards

> Always loaded. Concise by design. Evolves into project-specific conventions
> once the stack is chosen (TASK-001).

## Principles

- Clarity over cleverness. Optimize for the next reader (human or agent).
- One responsibility per function, file, and module.
- Explicit over implicit. No magic globals, hidden side effects, or silent defaults.
- Fail fast and loudly. Surface errors at the boundary with actionable messages.

## Naming (General)

- Follow the ecosystem's idiom once known (don't fight the language).
- Functions: descriptive verb-noun (`createUser`, `validateToken`, `renderHeader`).
- Constants: `UPPER_SNAKE_CASE`.
- Booleans: prefixed `is/has/should/can` (`isAuthenticated`, `hasPermission`).
- Avoid single-letter names except short loop indices; avoid negated booleans.

## Error Handling

- Never swallow errors. No empty catch, no silent `except: pass`.
- Log with enough context to reproduce (operation, identifiers, correlation ID)
  but without sensitive data.
- Never expose internals or stack traces to end users; generic client errors,
  details server-side only.
- Validate at the boundary; reject invalid input early with clear messages.

## Testing

- Every feature has tests; every bug fix has a regression test.
- Tests run with a single command (defined in `CLAUDE.md` once stack exists).
- Security-relevant behavior has negative tests (unauth, wrong role, malformed,
  oversized input).
- Tests are deterministic, isolated, and fast. No reliance on shared external
  state unless intentional and documented.

## Quality Tooling

- Configure formatter + linter + type checker (where language supports it) as
  part of project setup (TASK-001).
- Run them before declaring done. Never disable to make a change pass.
- Prefer existing ecosystem tooling over custom scripts.

## Documentation

- Public functions/modules documented (purpose, params, returns, errors).
- Non-obvious decisions explained with a *why* comment, not just *what*.
- Keep docs next to code; update docs in the same change as code.

## Version Control

- Small, focused commits with clear messages (what + why).
- No unrelated changes in one commit. No secrets in history.
- Branch per task; rebase/merge per team convention once defined.

> Self-update: when the stack is confirmed, replace generics with concrete
> language/framework conventions, linter config references, and patterns
> observed in actual code (cite representative file paths).
