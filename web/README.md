# web/ — opencode mission control SPA (Wave 2)

React + Vite + TypeScript SPA, embedded in the Go single binary in a later
wave. Styling is hand-rolled CSS against the token contract in
`web/DESIGN.md`: `src/tokens.css` is the ONLY file that may contain
6-digit hex literals (wave gate:
`grep -rE '#[0-9a-f]{6}' web/src` hits `src/tokens.css` only).

## Scripts

- `npm run dev` — Vite dev server (proxies `/api/*` to the Go backend; see `vite.config.ts`)
- `npm run build` — strict typecheck (`tsc --noEmit`) + `vite build` → `dist/`
- `npm run preview -- --port 18082` — serve the production build
- `npm test -- --run` — vitest (jsdom for `.tsx`, node-compatible `sse` tests)
- `npm run lint` — `tsc --noEmit` (strict, `noUnusedLocals`)

## Layout

```
src/
  main.tsx            entry (#root + tokens/app css)
  App.tsx             shell: SSE lifecycle, hash routing, activity ring, global keys, CmdBar + BottomBar wiring
  tokens.css          DESIGN section 2 verbatim — the single hex-bearing file
  app.css             all component styles, var() only
  views/Fleet.tsx     fleet table (search + expansion + pins + selection state; pure children)
  views/SessionStub.tsx  `#/sessions/:id` summary stub over Snapshot data (Wave 3 owns drill-down)
  components/
    KpiStrip.tsx      six stat cards (DESIGN section 4 anatomy)
    StatusBadge.tsx   glyph+wash+label badge (Wave 3 reuses this)
    Sparkline.tsx     72×24 inline polyline (Wave 3 reuses this)
    RelativeTime.tsx  "Xs ago" cell + absolute tooltip (Wave 3 reuses this)
    BottomBar.tsx     event ticker mount + persistent footer keymap
    EventTicker.tsx   activity ring list (newest-first, cap 30 + show more, paused freeze)
    CmdBar.tsx        `:`/Cmd+K fuzzy jump bar (id/title/cwd/model, up/down/enter/esc)
  lib/
    sse.ts            READ-ONLY transport (W2-3, tested — do not modify)
    fleetSort.ts      sort / Today-Yesterday-Older grouping / search filter
    format.ts         USD / compact / relative-time formatting
    activity.ts       PURE diffSnapshots(prev, next) + ring buffer (cap 100); NO tool polling
    pins.ts           pin persistence (`opencode-dashboard:pins:v1`, corrupt → empty) + usePins
    routing.ts        hash routing (`#/` + `#/sessions/:id`, ~30 lines, no router lib)
    useCommandPalette.ts  `:`/Cmd+K open-state + isTypingTarget guard
  testutils/fixture.ts  Snapshot fixture mirroring Go `model` json tags exactly
```

## Keyboard map (global handler ignores inputs/textareas/selects/contentEditable)

| Key | Action |
|---|---|
| `:` or Cmd/Ctrl+K | open/close the jump bar |
| `1`–`9` | jump to pinned slot (pin order) + highlight the row |
| `p` | pin/unpin the selected session |
| `l` | toggle LIVE (pause/resume snapshot stream) |
| `esc` | close jump bar → back from `#/sessions/:id` to `#/` |

## Extension points (downstream waves)

- Wave 3 builds Transcript/Timeline/Tools/Cost/Raw detail views reusing
  `StatusBadge` / `Sparkline` / `RelativeTime` + `routing.ts` hash builders.
  The ticker stays session-level by design: per-session tool-call rows
  (`/api/sessions/:id/tools` polling) are banned from the ticker — any
  request for per-tool rows points at Wave 3.
- Prop drilling stays ≤2 levels: `App` owns Snapshot/status/live/route/pins
  state; `Fleet` owns view state; everything below is pure props.
