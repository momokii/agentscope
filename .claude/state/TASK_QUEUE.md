# TASK_QUEUE — Board at HEAD `6f72903`

> Status: DONE → NEXT → BACKLOG. Verification commands are exact; run them before
> claiming a task complete. Plan detail: `.omo/plans/opencode-dashboard.md`.

## DONE

| ID | Task | Evidence at HEAD |
|---|---|---|
| W1 | Collector + DB/runtime/hub seams (plan W1-1…W1-4) | `internal/{db,runtime,hub,collector}`, `go test -count=1 ./...` |
| W2 | Fleet shell, KPI strip, ticker, CmdBar, pins, hash routing | `web/src/{App.tsx,views/Fleet.tsx,components}` |
| W3 | Session detail tabs, Compare, Global timeline, screen endpoint | `web/src/views/SessionDetail*.tsx`, `internal/runtime/screen.go` |
| W4 | Entrypoint, image, compose, Basic auth, security + VM runbooks | `cmd/dashboard`, `Dockerfile`, `docker-compose.yml`, `docs/*` |
| F1 | Data fidelity (live DB detail, token/cost reconciliation) | plan F1 `[x]`; issues ledger 2026-10-24 |
| F3 | Design gate (token-only hex, no color-only status) | plan F3 `[x]`; token gate below |
| F4 | Security/deploy gate | plan F4 `[x]`; `docs/SECURITY.md` §5 |
| — | Host networking fix (bridge was healthy-but-unreachable) | `6c8c5e3`, compose header |
| — | `BIND_ADDR` opt-in + auth guard | `77ccacc`, `cmd/dashboard/selfcheck.go` |
| — | Authenticated healthcheck + deploy proofs | `a6c108d` |
| — | Incremental event tail + `DB_TIMEOUT` | `28d2088` |
| — | Recency-gated statuses + honest pane join | `08f2f86` |
| — | RW zellij socket + independent scans | `1004a12`, `ef84984` |
| — | Newest-turn error truth | `b595687` |
| — | Cookie SSE + message-level metrics + Projects | `07705be` |
| — | Local-midnight cost, pane-based presence, TZ forwarding | `6a3fc5d`, `6f72903` |
| TASK-102 | Wire SessionDetail + pane text to the live APIs — acceptance: clicking a real fleet row renders its real transcript/turns/timeline/tools/cost/raw and live pane text; unknown ids still render the empty state; fixture tests keep passing (fixture provider remains test-only) | `GET /api/sessions/:id/export` + **Live pane** tab (`/api/sessions/:id/screen`) shipped; `go test -count=1 ./...`, `npm test --prefix web -- --run`, `npm run lint --prefix web`; live export-vs-CLI parity (871 messages, identical tokens/cost, 11 children) |

Verify the fleet baseline with:

```bash
go test -count=1 ./...
npm test --prefix web -- --run
npm run lint --prefix web
npm run build --prefix web
grep -rE '#[0-9a-fA-F]{6}' web/src        # only src/tokens.css
```

## NEXT

### TASK-201: Wave 5 P1 — pi source + Harness model field
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Scope:** `model.Harness` type + `SessionRow.Harness` (`harness` JSON, opencode default-normalized); new `internal/pi` JSONL reader behind `ports.PiReader` (mtime cache, status mapping, fork children via `parentSession`).
- **Acceptance:** pi fixture sessions derive running/waiting-input/error/idle with tokens/cost/model; corrupt file isolated; unknown entry types ignored.
- **Verify:** `go test -count=1 ./internal/pi/ ./internal/model/`
- **Security:** reader opens only `<PiDataDir>/**/*.jsonl`; never `auth.json` (mount excludes it; add negative test).

### TASK-202: Wave 5 P2 — runtime generalization
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Depends On:** TASK-201
- **Scope:** `ScanCommand` core (opencode+pi), pane `Harness()` from `pane_command` (never title), (directory,harness) presence claims, harness-scoped `ResolvePane`/`DumpSessionScreen`.
- **Acceptance:** mixed-harness directory claims both panes independently; shell panes claim nothing; orphans per harness.
- **Verify:** `go test -count=1 ./internal/runtime/`

### TASK-203: Wave 5 P3 — collector merge + PiDataDir config
- **Priority:** High | **Complexity:** S | **Status:** TODO
- **Depends On:** TASK-202
- **Scope:** merge opencode+pi rows pre-Enrich; `ports.Config.PiDataDir` (`PI_DATA_DIR`, default `$HOME/.pi/agent/sessions`); missing dir → pi disabled gracefully.
- **Acceptance:** pi-disabled host behaves exactly as before (fleet identical); merged sort/KPIs/projects correct.
- **Verify:** `go test -count=1 ./internal/collector/ ./internal/ports/`

### TASK-204: Wave 5 P4 — frontend harness differentiation
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Depends On:** TASK-203
- **Scope:** TS mirror `harness`; harness chip (glyph+label) in fleet + detail header; harness filter URL-serialized; pi rows deep-link to screen tab; new tokens in `tokens.css` + `DESIGN.md`.
- **Acceptance:** filter/chip render; URL round-trips; token gate still hits only `tokens.css`.
- **Verify:** `npm test --prefix web -- --run`, `npm run lint --prefix web`, token gate

### TASK-205: Wave 5 P5 — deploy, docs, redeploy
- **Priority:** High | **Complexity:** S | **Status:** TODO
- **Depends On:** TASK-204
- **Scope:** compose `:ro` sessions mount + `PI_DATA_DIR`; SECURITY.md §2/§3; README; `.env.example`; full gates; `./deploy.sh` redeploy with live evidence.
- **Acceptance:** container fleet shows both harnesses; Security Gate re-passes.
- **Verify:** full gate suite + `curl` proofs

### TASK-206: (BACKLOG) Wave 5 P6 — pi transcript drill-down
- **Priority:** Medium | **Complexity:** L | **Status:** TODO
- **Depends On:** TASK-205
- **Scope:** JSONL→export-shaped route feeding Transcript/Turns/Timeline/Tools/Cost/Raw tabs for pi sessions; enable Compare/Timeline.
- **Acceptance:** clicking a pi row renders its real transcript; unknown ids still empty-state.
- **Verify:** web suite + live export parity spot-check

### TASK-101: Run the F2 VM truth matrix (human-gated)
- **Priority:** High | **Complexity:** M | **Status:** TODO
- **Depends On:** a live VM with zellij + opencode; `./deploy.sh` already verified there
- **Scope:** execute `docs/VM_DEPLOY.md` §0–§3 (T1–T7): three live panes, one orphan,
  one EXITED, one zombie, one CWD-vs-directory mismatch. Record observed values.
- **Acceptance:** every row ticked with pasted evidence; plan F2 flips `[~]` → `[x]`.
- **Verify:** `curl -s 127.0.0.1:$PORT/api/healthz`; `curl -s 127.0.0.1:$PORT/api/fleet`;
  `GET /api/sessions/:id/screen` returns 200 for a live pane and 409 for the orphan.
- **Security:** agents must not kill live sessions to fabricate the matrix.

## BACKLOG

### TASK-103: Deferred actions wave (abort/resurrect)
- **Priority:** Low | **Complexity:** L | **Status:** BLOCKED on design
- **Depends On:** TASK-101 (runtime truth), a security-model re-open
- **Scope:** implement only what `docs/ACTIONS_DEFERRED.md` resolves: owner discovery
  (session → pane → PID → per-instance port), auth story, result semantics,
  compaction behavior, UI warning that resurrect re-runs commands.
- **Acceptance:** the doc's §5 acceptance list is met; Security Gate re-passes.
- **Verify:** owner-resolution + abort-mapping tests; manual abort/resurrect proof.
- **Security:** do not ship any action route before re-opening the threat model.

### TASK-104: Multi-host seam
- **Priority:** Low | **Complexity:** L | **Status:** TODO
- **Depends On:** design decision (not in v1)
- **Scope:** add a host dimension (`host_id`) to the collector/snapshot/UI without
  breaking the single-host contract. Not implemented today: the collector is single-host.
- **Acceptance:** two hosts can publish distinguishable snapshots; UI groups by host.
- **Verify:** new seam tests + a two-source integration test.

### TASK-105: Narrow/phone layout
- **Priority:** Low | **Complexity:** M | **Status:** TODO
- **Scope:** responsive layout for the Projects/Fleet/detail views at phone widths.
  Current layout is desktop-first; no phone-specific breakpoints are shipped.
- **Acceptance:** no horizontal overflow at 375px; primary filters remain reachable.
- **Verify:** manual browser check + existing web suite green.

### TASK-106: Event-tail / dedupe stress tests
- **Priority:** Low | **Complexity:** M | **Status:** TODO
- **Scope:** harden tests around the incremental event tail (duplicate rows, backfill,
  eviction, watermark reseed) and recomposition against a full scan.
- **Acceptance:** revert-to-full-scan equality remains proven at larger dummy sizes.
- **Verify:** `go test -count=1 ./internal/db/ ./internal/collector/`.

### TASK-107: CI + automated security scans
- **Priority:** Medium | **Complexity:** M | **Status:** TODO
- **Scope:** CI running Go tests/vet/build, web tests/typecheck/build, token gate, secret
  scan, dependency audit, image scan (original TASK-002). Requires user confirmation for
  new tooling/dependencies.
- **Acceptance:** failures block merge; no secrets in logs.
- **Verify:** pipeline run on a scratch branch.

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
