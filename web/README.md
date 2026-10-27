# web/ — opencode mission control SPA

React 19 + Vite 6 + TypeScript 5.6.3 SPA, built to `dist/` and embedded in the Go
single binary via `web/embed.go`. Styling is hand-rolled CSS against the token
contract in `web/DESIGN.md`: `src/tokens.css` is the ONLY file that may contain
6-digit hex literals (wave gate:
`grep -rE '#[0-9a-f]{6}' web/src` hits `src/tokens.css` only).

## Scripts

- `npm run dev` — Vite dev server. **No `/api` proxy is configured** in
  `vite.config.ts`; for live data use the built SPA served by the Go binary
  (`./deploy.sh` or `go run ./cmd/dashboard`), or add a proxy yourself.
- `npm run build` — strict typecheck (`tsc --noEmit`) + `vite build` → `dist/`
- `npm run preview -- --port 18082` — serve the production build
- `npm test -- --run` — vitest (jsdom for `.tsx`, node-compatible `sse` tests)
- `npm run lint` — `tsc --noEmit` (strict, `noUnusedLocals`)

## Layout

```
src/
  main.tsx            entry (#root + tokens/app css)
  App.tsx             shell: SSE lifecycle, hash routing, activity ring, global keys, CmdBar + BottomBar
  tokens.css          DESIGN section 2 verbatim — the single hex-bearing file
  app.css             all component styles, var() only
  views/Projects.tsx  default landing: directory cards ranked by live presence, drill-in
  views/Fleet.tsx     fleet table (status chips, recency buckets, date filter, pagination, pins)
  views/SessionDetail.tsx  6 tabs (Transcript/Turns/Timeline/Tools/Cost/Raw) — FIXTURE-BACKED today
  views/Compare.tsx   2–4 session comparison
  views/GlobalTimeline.tsx  all-session swimlanes
  views/SessionStub.tsx     legacy summary view (no longer routed; kept for its test)
  components/
    KpiStrip.tsx      KPI cards (active/waiting/tokens/costs/errors/live procs+panes/metrics age/SSE)
    StatusFilterBar.tsx, RecencyFilterBar.tsx, Pagination.tsx
    StatusBadge.tsx, Sparkline.tsx, RelativeTime.tsx, StatusDistribution.tsx
    SubagentPanel.tsx, ProjectHeader.tsx
    BottomBar.tsx, EventTicker.tsx, CmdBar.tsx
  lib/
    sse.ts            transport (reconnect/backoff/resume) — no rendering
    fleetSort.ts      sort / status+recency filters / date presets / PAGE_SIZE=50
    projects.ts       project naming, sorting, filtering, header resolution
    routing.ts        hash routes (`#/`, `#/projects/:dir`, `#/sessions/:id[/:tab]`, compare, timeline)
    activity.ts       pure diffSnapshots + ring buffer (no tool polling)
    pins.ts           pin persistence (`opencode-dashboard:pins:v1`)
    format.ts         USD / compact / relative-time formatting
    transcript.ts     fixture-shaped transcript flattening, turns, costs
    compare.ts        comparison helpers
    useCommandPalette.ts  `:`/Cmd+K open-state + isTypingTarget guard
  testutils/
    fixture.ts        Snapshot fixture mirroring Go `model` json tags
    sessionDetail.fixture.ts  current SessionDataProvider (fixture — NOT live data)
```

> **Data-seam note:** `SessionDetail.tsx` reads through `SessionDataProvider` and
> currently uses `fixtureProvider`. The backend `GET /api/sessions/:id` is live; a
> fetch-backed provider swap is TASK-102. Do not treat drill-down content as real
> session data in this build.

## Keyboard map (global handler ignores inputs/textareas/selects/contentEditable)

| Key | Action |
|---|---|
| `:` or Cmd/Ctrl+K | open/close the jump bar |
| `1`–`9` | jump to pinned slot (pin order) + highlight the row |
| `p` | pin/unpin the selected session |
| `l` | toggle LIVE (pause/resume snapshot stream) |
| `esc` | close jump bar → back from a session/compare/timeline to the Projects/Fleet root |

## Extension points

- **Live drill-down:** replace `fixtureProvider` with a fetch-backed
  `SessionDataProvider` (TASK-102). The backend route and response shape already exist.
- **Mobile layout:** no phone-specific breakpoint is shipped; add one without
  introducing new colors (tokens only).
- **Ticker boundary:** the ticker stays session-level by design. Per-session tool-call
  polling is banned from the ticker; use the on-demand `/tools` endpoint from a detail view.
- **Prop drilling stays ≤2 levels:** `App` owns snapshot/status/live/route/pins state;
  `Fleet` owns table state; everything below is pure props.
