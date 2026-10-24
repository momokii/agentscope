# web/DESIGN.md — opencode mission control · token contract (k9sOps)

> Status: Wave 2 token contract (W2-4). Single source of truth for W2-1 (Fleet/KpiStrip)
> and W2-2 (Ticker/CmdBar). Docs-only: no components, no stylesheets, no framework choice.
> Every color, font, space, radius, and motion value used by W2-1/W2-2 MUST trace to a token
> in section 2–7 below. No ad-hoc values.
>
> Traceability rule (binding): exact hex values appear ONLY in the section 2 token table,
> each exactly once. Everywhere else — including section 0 — colors are referenced by
> token name (e.g. `var(--ink-950)`). Rationale recorded in section 10, conflict C1.

## 0. Visual direction record — A. k9sOps (selected)

- Direction: **A. k9sOps** per `.omo/plans/opencode-dashboard.md` D5 (plan default) and
  research `research-dashboard-design.md` section 3, option A. Options B (LinearDark) and
  C (CRT terminal) rejected: B is tuned for long transcript reading (Wave 3 concern, not
  fleet landing); C carries the highest execution risk. k9sOps is closest to lazydocker/k9s
  and cheapest to execute well.
- Surface: near-black app background on token `--ink-950`; panels `--ink-900`; cards
  `--ink-800`. Hard 1px borders in `--ink-600` on tables; cards use 6px radius
  (`--radius-card`), tables use zero radius (`--radius-table`).
- Accent: single cyan, token `--accent` (exact hex in section 2). No second accent.
- Type: mono-forward for all data (token `--font-mono`, tabular-nums mandatory); neutral
  sans for prose only (token `--font-sans`). Titles capped at `--fs-20`. Dense tables at
  12px/16px (`--fs-table` / `--lh-table`).
- Status language: glyph + tonal wash + text label, never color alone (section 6).
- Theme: dark ops only. No light theme (plan F3).

## 1. Anti-slop rules (verbatim, binding on all Wave 2+ UI)

1. No purple/blue gradients.
2. No glassmorphism.
3. No hero.
4. No three-equal-cards.
5. No Inter-plus-slate-900 default (prose face is `--font-sans`, never Inter; ink ramp is
   section 2 tokens, never slate-900).
6. No `border-l-2` status stripes — state is encoded as tonal wash + glyph + label,
   never a colored edge. The only colored edge permitted anywhere is the keyboard
   `:focus-visible` ring (token `--focus-ring`).
7. Lucide-only icons. No emojis as icons.
8. Status is never color-alone: shape (glyph) + text label always accompany color.

## 2. Token table (machine-checkable — copy-paste source for W2-1/W2-2)

Hex values exist ONLY in this table. Components MUST consume them via `var()` and MUST
NOT contain any other hex literal (wave acceptance: `grep -rE '#[0-9a-f]{6}' web/src`
returns empty outside the token definition file).

```css
:root {
  /* ONE neutral ink ramp (zinc-flavored, cool) — 8 steps */
  --ink-950: #0a0c10; /* app background (near-black) */
  --ink-900: #11141a; /* panel background (fleet table, CmdBar, inspector rail) */
  --ink-800: #1a1f28; /* raised surface (cards, chips, kbd, table header) */
  --ink-700: #2a313d; /* hover wash base, chart track, ProgressCircle track */
  --ink-600: #3f4756; /* hairline borders, 1px, all tables/cards/rails */
  --ink-400: #8b94a3; /* secondary text, footer keymap, axis ticks */
  --ink-200: #d4d9e0; /* primary text */
  --ink-100: #f0f3f7; /* emphasized text, KPI values, chart stacked segment */

  /* ONE accent (cyan) */
  --accent: #22d3ee; /* active/running emphasis, cost series, LIVE-on, links */

  /* Semantic set — fixed meanings, do not reassign */
  --ok: #34d399;   /* healthy / idle-ok */
  --warn: #fbbf24; /* waiting-input / stale */
  --err: #f87171;  /* error */
  --run: var(--accent); /* running / active — resolves to the single accent (see section 10, C2) */
  --mute: #71717a; /* archived / paused chrome */
}
```

Derived (non-hex, computed from tokens above — no new hues):

```css
:root {
  /* Tonal state washes: color-mix over transparent, never a second literal */
  --wash-run: color-mix(in oklch, var(--run) 14%, transparent);
  --wash-ok: color-mix(in oklch, var(--ok) 14%, transparent);
  --wash-warn: color-mix(in oklch, var(--warn) 14%, transparent);
  --wash-err: color-mix(in oklch, var(--err) 14%, transparent);
  --wash-mute: color-mix(in oklch, var(--mute) 16%, transparent);
  --wash-accent: color-mix(in oklch, var(--accent) 12%, transparent);

  /* Type */
  --font-mono: "JetBrains Mono", "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  --font-sans: "Geist", "Geist Sans", system-ui, -apple-system, "Segoe UI", sans-serif;
  --tnum: tabular-nums; /* apply via font-variant-numeric on ALL --font-mono data text */

  /* Type scale (px) — titles capped at 20 */
  --fs-11: 11px;
  --fs-12: 12px;
  --fs-13: 13px;
  --fs-16: 16px;
  --fs-20: 20px;
  --fs-24: 24px; /* reserved: single oversized fleet-total figure ONLY, never section titles */
  --fs-table: var(--fs-12);
  --lh-table: 16px;
  --label-tracking: 0.08em; /* stat-card / badge labels: 11px uppercase + this tracking */

  /* Spacing & rhythm (4px base) */
  --space-base: 4px;
  --gap: 12px;          /* the single grid gap */
  --row-fleet: 32px;    /* fleet row height */
  --row-dense: 28px;    /* dense tables, ticker rows */
  --spark-w: 72px;      /* inline sparkline width */
  --spark-h: 24px;      /* inline sparkline height, never full charts in rows */

  /* Borders & radius */
  --border: 1px solid var(--ink-600);
  --radius-table: 0;
  --radius-card: 6px;
  --radius-chip: 4px;

  /* Focus (only permitted colored edge) */
  --focus-ring: 0 0 0 2px var(--ink-950), 0 0 0 4px var(--accent);

  /* Motion */
  --dur-fast: 150ms; /* ceiling for ALL transitions */
  --ease-out: cubic-bezier(0.2, 0, 0, 1);
}
```

On-screen hue budget (max 5): cyan (`--accent`/`--run`), green (`--ok`), amber (`--warn`),
red (`--err`), gray (`--mute` + ink ramp). Near-white ink text (`--ink-100`/`--ink-200`)
does not count as a hue. Charts reuse this same ramp (section 8).

## 3. Typography

- Exactly 2 families: `--font-mono` (data: numbers, badges, tickers, tables, CmdBar,
  timestamps, costs) with `font-variant-numeric: tabular-nums` mandatory; `--font-sans`
  (prose: session titles, transcript body in Wave 3, empty states, docs strings).
- Scale: `--fs-11` / `--fs-12` / `--fs-13` / `--fs-16` / `--fs-20` / `--fs-24`.
  View/section titles capped at `--fs-20`. `--fs-24` is reserved for one oversized
  fleet-total figure and MUST NOT be used for titles (ops UI has no hero type).
- Dense tables: `--fs-table` (12px) with `--lh-table` (16px leading).
- Fleet row titles: `--font-sans` 13px, 2-line clamp, `--ink-200`.
- Labels (stat-card label, badge label, footer keymap): `--fs-11`, uppercase,
  `letter-spacing: var(--label-tracking)`, `--ink-400`.

## 4. Spacing & stat-card anatomy

- Base unit `--space-base` (4px); all padding/margins are multiples of 4px.
- Single grid gap `--gap` (12px) between fleet table, KPI strip, ticker, rails.
- Fleet rows exactly `--row-fleet` (32px); dense tables and ticker rows exactly
  `--row-dense` (28px).
- Stat-card anatomy (KpiStrip — all six KPIs share it, no three-equal marketing cards):
  label `--fs-11` uppercase tracked `--ink-400` / value `--fs-20` `--font-mono` + tnum
  `--ink-100` / delta `--fs-12` `--font-mono` colored by semantics
  (`--ok` down-good, `--err` up-bad, `--warn` stale) WITH a directional glyph
  (up/down arrow Lucide icon) + sign, never color alone.

## 5. Borders, radius, surfaces

- All structural borders: `var(--border)` — hard 1px `--ink-600`, no colored edges.
- Tables: `--radius-table` (0). Cards/KPI: `--radius-card` (6px). Chips/badges/kbd:
  `--radius-chip` (4px).
- Surfaces: app `--ink-950`, panels `--ink-900`, raised `--ink-800`, hover wash
  `--wash-accent` or `--ink-700` tonal layering (never a colored outline).
- Selected row: tonal wash (`--wash-accent`) + glyph affordance (check / caret), never a
  `border-l-2` stripe. Focused (keyboard): `var(--focus-ring)` — the sole exception.

## 6. Status system (state = color + glyph + label, always all three)

| State | Color token | Glyph | Label text |
|---|---|---|---|
| running | `--run` (accent cyan) | ● (filled circle, pulses — the ONE permitted loop) | RUNNING + working spinner in fleet gutter |
| idle | `--ok` | ◌ (hollow circle) | IDLE |
| waiting-input | `--warn` | ▲ (triangle — visually distinct from idle by shape, not just color) | NEEDS INPUT |
| error | `--err` | ✕ | ERROR (+ count where applicable) |
| archived | `--mute` | ▪ (filled small square) | ARCHIVED |
| stale (data, not session) | `--warn` | ◌ | STALE · last update Xs ago (relative time, never raw ms) |
| paused (LIVE off) | `--mute` | ‖ | PAUSED |

- Compound fleet badge anatomy: `[glyph] LABEL` on a tonal wash of the state color
  (`--wash-run` / `--wash-ok` / `--wash-warn` / `--wash-err` / `--wash-mute`), 11px
  uppercase tracked mono, `--radius-chip`, 1px `--ink-600` border. waiting-input MUST be
  shape-distinguishable from idle at grayscale (▲ vs ◌) — operator's scarcest signal.
- Unseen-count and subagent-expander chips (`↳N`) use `--ink-800` bg + `--ink-200` text.
- SSE connection dot: connected `--ok`, reconnecting `--warn` (pulse) + "reconnecting…"
  banner, failed `--err`. Stale-data chip on fleet header per heartbeat rule (15–30s).
- Relative time: `<60s` "Xs ago", `<60m` "Xm ago", else Today/Yesterday/date grouping;
  absolute timestamp in tooltip.

## 7. Motion

- GPU-composited properties ONLY: `transform`, `opacity` (and `filter` where the frontend
  skill axiom set applies). Never animate layout properties (width/height/top/left).
- Streaming series: `isAnimationActive=false` on ALL live Recharts series; sparkline
  updates capped at 1Hz; charts use incremental patches, never full redraws; frequently
  changing components isolated to avoid full-tree redraws.
- Transitions (hover, select, tab switch, drawer open): `≤ var(--dur-fast)` (150ms),
  `var(--ease-out)`, opacity/transform only.
- `prefers-reduced-motion: reduce` MUST disable the running-pulse, SSE-dot pulse, and all
  transitions (media query in the token definition file; static state colors remain).
- Every animation maps to a state change: running-pulse ↔ running↔non-running transitions
  only; pill slide ↔ tail-follow pin/unpin; banner fade ↔ SSE connect/reconnect. No
  decorative motion, no hover that changes nothing, no motion on non-interactive elements.

## 8. Component mapping (what W2-1/W2-2 implement against)

Research widget coverage (`research-dashboard-design.md` section 6): badges, sparklines,
Tracker strips, BarList, donuts, ProgressCircle — all mapped below. No widget may
introduce a color, font, or space outside sections 2–7.

- Fleet status badge → section 6 compound badge (wash + glyph + label tokens).
- Model chip → `--ink-800` bg, `--ink-200` text 11px mono, `var(--border)`,
  `--radius-chip`. Subagent expander `↳N` same chip + `--ink-400` count.
- Sparkline (72×24 inline, per fleet row) → stroke `--accent`, track transparent;
  error markers `--err` dots; update cap 1Hz; `isAnimationActive=false`.
- KPI value → `--fs-20` mono tnum `--ink-100`; KPI delta → `--fs-12` mono + semantic
  color + Lucide directional icon; KPI label → 11px uppercase tracked `--ink-400`.
  waiting-input KPI uses `--warn` + ▲; errors KPI `--err` + ✕; SSE KPI dot per section 6.
- Ticker rows → `--row-dense` (28px), 12px mono `--ink-200`, status glyph per section 6,
  timestamp relative `--ink-400`; new-row entry = opacity/transform ≤150ms only.
- CmdBar (`:` / Cmd+K) → panel `--ink-900`, `var(--border)`, `--radius-card`;
  selected hit `--wash-accent` + check glyph; 1–9 quick-switch slot chips `--ink-800`;
  pin state = filled Lucide pin icon + `--accent` label text (icon + label, not color).
- Footer keymap → 11px `--ink-400`; kbd chips `--ink-800` bg, `--radius-chip`,
  1px `--ink-600` border, mono 11px `--ink-200`.
- Inspector rail (Wave 3 builds against same tokens) → `--ink-900` bg, 1px left
  `var(--border)`, tabs 12px mono; agent-lane chips reuse model-chip tokens with
  per-lane glyph prefix (letter, not color, as distinguisher).
- Chart series: cost = `--accent` line; errors = `--err` (bars/dots); token in/out
  stacked bars = `--ink-200` / `--ink-400` tonal pair; latency p50/p95 overlay =
  `--ink-200` solid / `--ink-400` dashed; waiting-input markers `--warn` ▲;
  drag-zoom ReferenceArea = `--wash-accent` fill. Donut (cost-by-model) slice order:
  `--accent`, `--ink-200`, `--ok`, `--warn`, `--mute` (stays within 5-hue budget).
- BarList (top models/tools by cost) → label 12px `--ink-200`, value 12px mono
  `--ink-100`, bar `--accent` on `--ink-800` track, error rows `--err` value + ✕.
- Tracker strip (session lifecycle) → per-segment state colors from section 6
  (`--run`/`--ok`/`--warn`/`--err`/`--mute`), segment borders `--ink-950` 1px gaps.
- ProgressCircle (context-window %) → fill `--accent`, track `--ink-700`,
  numeric label 12px mono `--ink-100`; >85% switches to `--warn` + "NEAR LIMIT" label
  (color + label, never color alone).

## 9. Traceability & acceptance

- Wave acceptance (plan W2 + F3): `web/DESIGN.md` exists; every color/font/space in
  W2-1/W2-2 traces to a token above; after components land,
  `grep -rE '#[0-9a-f]{6}' web/src` MUST return empty outside the token definition file
  (tokens live in exactly one file — the W2-1 stack-setup stylesheet/constants module —
  which is the only file permitted to contain the section 2 hexes).
- Self-check method (docs-only wave, no build): ran
  `grep -oE '#[0-9a-fA-F]{6}' web/DESIGN.md | sort | uniq -c` from the repo root and
  confirmed every match has count exactly 1 and every match sits on a section 2 token
  definition line (`--name: <hex>;` or `--run: var(--accent);` alias carrying no hex).
  Result recorded below at delivery time; the standing rule is: any edit that adds a
  second occurrence of any hex, or any hex outside section 2, fails this gate.
- Self-check result: 13 unique hexes, each count 1, all in section 2 (8 ink + 1 accent +
  3 semantic + 1 mute; `--run` is a `var()` alias and contributes no hex).

## 10. Resolved conflicts & decisions

- C1 — Task checklist (a) sketches the direction record with an inline hex while the
  verification checklist requires each hex to appear exactly once as a token definition.
  Resolution: hexes live ONLY in section 2; section 0 references token names. The
  direction content (near-black bg, single cyan accent, mono-forward, 1px table borders,
  6px card radius, glyphs) is fully recorded via token references, and the full
  name→hex table is pasted in the delivery report.
- C2 — Research rule 5 assigns running/active to "blue/violet" while the k9sOps direction
  and this task allow ONE accent (cyan) and max 5 hues. A separate blue plus cyan would
  be 6 hues. Resolution: `--run` resolves to `var(--accent)` — cyan sits in the
  blue-cyan family, satisfying "blue = running/active" with zero added hues.
- C3 — Research option A text says "zero radius on tables / 6px on cards" while the task
  text says "hard 1px borders on tables / 6px radius on cards". No conflict in substance:
  encoded as `--radius-table: 0` + `var(--border)` 1px on tables, `--radius-card: 6px`.
- C4 — Frontend skill greenfield workflow prescribes multi-lane design research
  (Layer A/B reads, lazyweb, StyleGallery, Imagen drafts). Not run: this task binds to
  the already-decided Lane C direction (plan D5) with an explicit no-freestyle mandate;
  the applicable skill content is the shared axioms (section 1 rules 6–8, GPU-only
  motion, motion-must-mean-something) plus the DESIGN.md-as-contract gate, which this
  file satisfies. Recorded here instead of silently skipped.
- Frontend skill rules applied: DESIGN.md contract first (this file); no coloured accent
  borders (rule 6, section 5); Lucide-only icons (rules 7–8, sections 1/6/8);
  GPU-composited animation only + slop-animation ban (section 7).
