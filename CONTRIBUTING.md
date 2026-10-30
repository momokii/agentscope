# Contributing to opencode mission control

Thanks for stopping by. This project is small on purpose: one Go binary, one
embedded React SPA, one Docker container, read-only by design. Contributions
are welcome — issues and focused PRs.

## Ground rules

- **Small, focused commits** (what + why in the message). No unrelated
  changes in one commit. **Never commit secrets** (no tokens, passwords,
  session content, or machine-specific paths).
- **Every feature has tests; every bug fix has a regression test.**
- **Quality gates must pass** before a PR is reviewable (see below).
- **Never weaken a control** (auth, TLS verification, input validation,
  linters, type checks) to make something pass. If blocked, say so in the
  PR instead.
- **Never work around a denied/refused permission** (shell redirection,
  editing settings, other tools). Report it instead.

## Development

```bash
go build ./...             # backend must compile
go vet ./...               # static checks
go test -count=1 ./...    # full backend suite (9 packages)

npm run lint --prefix web  # tsc --noEmit (type gate)
npm test --prefix web -- --run   # vitest (252 tests / 31 files)
npm run build --prefix web       # tsc + vite build → web/dist (committed fallback)
grep -rE '#[0-9a-fA-F]{6}' web/src  # must hit only src/tokens.css
```

`./deploy.sh` rebuilds `web/dist` when `web/src/**` is newer than
`web/dist/index.html`; otherwise the committed `dist` is reused so no-Node
hosts still work. Keep `web/dist` committed and in sync.

## Conventions that will get your PR merged faster

- **Frozen seams:** `internal/ports` is the contract surface (`DBReader` /
  `PiReader` / `RuntimeSource` / `SnapshotSink` / `Config`). Implementations
  live behind it; never import sibling implementations across packages.
- **DB queries stay indexed and sliced** (per-session key + `LIMIT`).
  Full-table scans over message/part payloads are banned — the live DB is
  ~8 GB with active writers. Off-tick scans (metrics) are the only exception,
  and they degrade to last-good values on failure.
- **Status derivation stays pure** (`model.DeriveStatus`): recency arrives as
  data, never as a clock read inside the function.
- **UI follows `web/DESIGN.md`:** tokens via `var()`, no hex outside
  `web/src/tokens.css`, status is never color-alone (glyph + label), Lucide
  icons only, mono-forward data type with tabular numbers.
- **Docs ship with code:** update `README.md` / `docs/*` in the same change,
  and record load-bearing decisions in `.claude/state/DECISIONS_LOG.md`.
- **Security Gate:** any change touching mounts, network, auth, queries, or
  dependencies must pass `docs/SECURITY.md` §5 (runnable verbatim) — paste
  the row results in the PR.

## Review process

Maintainers check: scope discipline, zero regression (previously passing tests
still pass), gates green, Security Gate where applicable, docs updated.
Large architectural changes (new endpoints, new mounts, non-loopback binds,
dependency additions) need discussion in the issue **before** code.
