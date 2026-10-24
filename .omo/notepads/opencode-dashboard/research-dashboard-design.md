# Research: opencode mission-control dashboard — UX patterns, anti-slop system, realtime stack

Date: 2026-10-24. Scope: presentation + visualization + stack ONLY (data layer is sibling lanes').
Scale: one operator, one VM, tens of concurrent sessions max. Optimize for density + glanceability.

Skill routing used (frontend skill): this is an internal ops dashboard, so Layer A = operational
"taste" discipline + the skill's shared axioms (DESIGN.md contract, no coloured accent borders,
Lucide-only icons, GPU-composited motion only, motion-must-mean-something). Note: taste-skill.md
itself declares "Not dashboards, not data tables" — pulled only its anti-default discipline
(no AI-purple gradients, no generic glassmorphism, no Inter+slate-900 default), which applies fully.

## 1. UX pattern catalog (fleet list / session detail / visualization)

### 1a. Fleet / session list with live status
| Product | URL / ref | What to steal |
|---|---|---|
| lazydocker | https://github.com/jesseduffield/lazydocker — Containers panel: master list (left) + live detail tabs (right: logs / stats / env / config / top); sort running→exited, ASCII graphs in detail; auto-group compose services | Master-detail split; per-row status sorted by liveness; live ASCII/CPU graphs inside detail pane; project grouping (map: opencode project dir = compose project) |
| k9s | https://k9scli.io — `:resource` command bar, vim nav, always-visible keymap footer, Pulse/XRay views, context-aware skins | `:` command palette to jump to session (fuzzy id/title/cwd); persistent footer keymap; Pulse-style cluster health strip at top of fleet view; per-project color tint (dev vs prod cwd) |
| opencode web home | https://opencode.ai/docs/web/ + packages/app/src/pages/home.tsx (sst/opencode): groups sessions Today/Yesterday/Older, search filter, per-row working/permission/error/unseen badges | Row anatomy to copy: title + project name + working spinner / permission-request / error / unseen-count badges; recency grouping; limit list (HOME_SESSION_LIMIT) with search, not infinite scroll |
| Braintrust Logs | https://www.braintrust.dev/docs/observe — searchable/filterable trace table, custom columns, saved views, LIVE toggle + time-range zoom | Fleet table with custom columns, saved views ("needs input", "errored", "expensive"), LIVE toggle in time-range selector, click-chart-point → filtered trace list |
| Helicone Requests | https://docs.helicone.ai/features/sessions + /requests — auto-updating table, sortable columns, click row → request drawer | Fleet table auto-appends new sessions; click row opens session drawer/pane without leaving fleet context |

Steal list (fleet): master-detail layout; status-sorted rows; per-row compound status badge
(working / waiting-input / error / stale); `:` or Cmd+K jump bar; recency grouping + search;
LIVE toggle with explicit paused/stale indicator.

### 1b. Session detail with full context (transcript, tool calls, tokens/cost, subagents)
| Product | URL / ref | What to steal |
|---|---|---|
| LangSmith thread view | https://docs.langchain.com/langsmith/view-traces — Trajectory (conversation layer: inputs/outputs/reasoning/tool calls/subagent activity, press T) / Turns (per-turn summary cards) / Details (per-run inputs, timing, tokens, errors, press D) | EXACTLY the drill-down this task needs: three switchable lenses over one session — Transcript (=Trajectory), Turns summary (=Turns), Run inspector (=Details). Click message/tool call → Details at that run; "return to trajectory" affordance |
| LangSmith Trajectories | https://www.langchain.com/blog/langsmith-trajectories-tracing (2026-09) — chronological conversational view flattening main agent + subagents in first-appeared order | Interleaved subagent timeline flattened chronologically with agent-lane color chips; subagent handoffs as expandable steps under the requesting turn |
| Braintrust trace viewer | https://www.braintrust.dev/docs/observe/examine-traces — Spans (nested hierarchy) / Timeline (execution flow + token efficiency) / Thread (conversation); per-row inline metrics (duration, tokens, cost); cost propagated parent-ward; Shift+F fullscreen; View-in-thread from any span | Dual Spans-hierarchy + Thread-conversation views; per-span inline metrics (duration/tokens/cost); cost roll-up from subagent spans to session total; fullscreen trace mode; click span → jump to its position in thread |
| Helicone Sessions | https://docs.helicone.ai/guides/cookbooks/ai-agents — session view: timeline of reasoning→tool execution, hierarchical session paths (/task/research/web_search), per-op status/timing/model, full conversation context | Path-style grouping of tool calls by phase; per-tool success/fail + I/O + latency; timeline viz of reasoning→tool chains with decision points visible |
| opencode TUI session dialog | sst/opencode packages/tui/src/component/dialog-session-list.tsx — pinned sessions, quick-switch slots 1-9, working gutter (spinner), debounced search, Today-grouped | Pin sessions; number-key quick-switch between watched sessions; working spinner in list gutter — operator watches 2-3 hot sessions |

Steal list (detail): Trajectory/Turns/Details (or Spans/Timeline/Thread) tab switch; flattened
subagent lane; per-tool-call expandable I/O + latency + status; cost roll-up; pin + quick-switch.

### 1c. Visualization widgets
| Product | URL / ref | What to steal |
|---|---|---|
| LangSmith dashboards | https://docs.langchain.com/langsmith/dashboards — prebuilt sections: Traces (count/latency/error), LLM calls, Cost & Tokens (total/per-trace/by-type), Tools (top-5 error+latency), Run Types, Feedback | Fleet header strip = these six KPI cards; per-tool top-5 error/latency table; cost-by-model donut |
| Braintrust dashboards | https://www.braintrust.dev/docs/observe/dashboards — time-series charts with click→"see traces / open sample traces / filter to value", LIVE auto-refresh toggle, drag-to-zoom | Every chart point is a drill-down menu (click spike → sessions behind it); LIVE toggle; drag-zoom on token/cost timelines |
| btop/bottom/oha (TUI canon) | bee-tui research notes (https://docs.rs/crate/bee-tui — 02-tui-design-patterns): braille sparklines, per-core meters, expand-pane (`e`) to fullscreen, oha percentile overlays | Activity sparklines (braille/block chars or 24px canvas) per fleet row; `e`/click to expand any widget fullscreen; p50/p95 latency overlay on duration charts |
| Tremor blocks | https://blocks.tremor.so/ — KPI cards, bar lists, trackers, sparklines, data bars, chart tooltips; light+dark by default | Widget vocabulary: KPI card + delta, BarList (top models/tools by cost), Tracker (status timeline strip per session), SparkChart (tokens/min), ProgressCircle (context-window %) |

Steal list (viz): KPI strip; per-row sparklines; Tracker strip for session lifecycle;
BarList for cost-by-model/tool; click-any-point drill-down; expand-to-fullscreen widgets.

## 2. Live-update UX (explicit rules)
- Transport default: SSE (server→client only). Diamonds: native auto-reconnect via EventSource +
  Last-Event-ID resume; HTTP-native (proxy-friendly); no sticky sessions. WS only if operator must
  send high-frequency upstream input on the same channel — not our case (commands go over plain POST).
  Refs: https://vercel.com/i/websocket-vs-server-sent-events ;
  https://www.shellprop.com/blog/server-sent-events-vs-websockets-nextjs ;
  https://nextlevelnextjs.com/blog/adding-sse-and-websockets-to-a-self-hosted-next-js-stack
- Status transitions: `running → idle → waiting-input → error/archived`. Render as compound badge
  (opencode web home.tsx pattern: working spinner / permission-request / error / unseen count).
  waiting-input must be visually distinct from idle (operator's scarcest signal) — shape + label,
  never color alone (TUI canon anti-pattern: "color-only signaling").
- Stale-data indicators: heartbeat every 15-30s; if no event for N sec show "STALE · last update Xs
  ago" chip on fleet header + per-row relative timestamps ("updated 12s ago", never raw ms).
  On SSE drop: banner "reconnecting…" with backoff; on resume, re-sync full snapshot (Last-Event-ID).
  Ref: nanostatus SSE keepalive + auto-reconnect (https://github.com/gjames8/nanostatus).
- Auto-scroll vs pinned transcript: default tail-follow; ANY user scroll-up pins (show "↓ N new —
  jump to live" pill); Esc or pill-click resumes follow. Never yank scroll on new tokens.
- Relative-time formatting: <60s "Xs ago", <60m "Xm ago", else Today/Yesterday/date grouping
  (opencode home.tsx groupSessions pattern). Absolute timestamp in tooltip.
- Event volume: aggregate to 250-1000ms ticks server-side; perceived realtime is <1s
  (https://letsbuildsolutions.com/blog/web-engineering/...-nextjs/). Per-row sparkline updates at
  most 1Hz; charts use incremental patches, never full redraws (Recharts perf: isolate frequently
  changing components, disable animation on streaming series — context7 /recharts/recharts docs).

## 3. Anti-slop design system (concrete rules for a dark internal ops UI)
From frontend skill shared axioms + taste-skill anti-default discipline:
1. DESIGN.md contract first: every color/font/space value traces to a token. No ad-hoc hex.
2. NO AI-slop tells: no purple/blue gradients, no generic glassmorphism, no centered marketing hero,
   no three-equal-feature-cards, no Inter+slate-900 default, no `border-l-2 border-primary` status
   stripes (skill axiom: state via tonal washes + glyph + label, never colored edge except
   focus-visible rings), no emojis as icons (Lucide only).
3. Typography: exactly 2 families — 1 grotesk/mono for data (tabular-nums mandatory: JetBrains Mono /
   IBM Plex Mono / Geist Mono) + 1 neutral sans for prose (Inter is fine ONLY if paired with a
   distinctive mono + custom accent; better: Geist / Söhne-like). Scale: 11/12/13/16/20/24, titles
   capped at 20px — ops UI has no hero type. Dense tables at 12px/16px leading.
4. Spacing rhythm: 4px base, row height 32px fleet / 28px dense tables; single 12px gap grid;
   stat cards share one anatomy (label 11px uppercase tracked / value 20px mono / delta 12px).
5. Color discipline (dark ops): ONE neutral ink ramp (zinc/oklch) + ONE accent (cyan or amber) +
   semantic set with fixed meanings: green=healthy/idle-ok, amber=waiting-input/stale, red=error,
   blue/violet=running/active, gray=archived. Status ALWAYS pairs color with glyph+text label.
   Max 5 hues on screen at once; charts reuse the same semantic ramp (cost=accent, errors=red).
6. Density without clutter: fleet rows 32px with 2-line clamp titles; sparklines 72x24 inline, never
   full charts in rows; detail panes use tabs (Transcript/Turns/Timeline/Tools/Cost), not one long
   scroll; right-rail inspector pattern (Braintrust) for selected tool call/span.
7. Motion: GPU-composited only (transform/opacity); streaming series animate NOTHING per-tick
   (isAnimationActive=false on live series); transitions ≤150ms; respect prefers-reduced-motion;
   every animation maps to a state change (badge pulse on running→ only).

Visual directions (pick ONE, record in DESIGN.md):
- A. "k9sOps" — near-black (#0a0c10), single cyan accent, mono-forward, hard 1px borders, zero radius
  on tables / 6px on cards, status glyphs (●▲✕◌). Closest to lazydocker/k9s; cheapest to execute well.
- B. "LinearDark" — zinc-950, subtle indigo accent, 8px radius, soft tonal washes, Geist-like type,
  generous micro-hierarchy. Friendlier for long transcript reading.
- C. "CRT terminal" — phosphor-green/amber accents on black, scanline-free but scanline-evoking
  header, block/braille sparklines, keyboard-first command bar prominent. Highest character, highest
  risk — only if operator loves TUIs.

## 4. Low-fidelity IA (information architecture)
- Default landing = Fleet overview (operator opens it and sees everything in <2s):
  header KPI strip (active sessions, waiting-input count, tokens/hr, cost today, error count, SSE status) /
  fleet table (status badge, title, project/cwd, model, tokens, cost, last-activity relative, sparkline) /
  right or bottom: event ticker (latest tool calls across fleet).
- Views: (1) Fleet [default] (2) Session detail `/:id` (tabs: Transcript / Turns / Timeline / Tools /
  Cost+Tokens / Raw JSON) (3) Compare (2-4 sessions side-by-side: tokens, cost, duration, tool counts)
  (4) Timeline/global (all sessions' activity on one time axis — the "everything shown" view).
- Nav: left rail icons (Fleet, Global timeline, Compare tray, Saved views, Settings) + Cmd+K / `:`
  jump bar (k9s command-mode steal). Deep-link every view+tab+selected-span (shareable URLs).

## 5. Stack recommendation (single operator, one VM, tens of sessions)

Tradeoff table:
| Option | Live-update ergonomics | Deploy / image size | Long-lived conns | Charts | Verdict |
|---|---|---|---|---|---|
| Go backend + SSE + embedded Vite SPA (React + Tremor Raw/Recharts) | Excellent: one SSE hub, goroutine per viewer, OOB HTML or JSON events, auto-reconnect | Single binary; distroless ~16MB image / 4.4MB minimal (nanostatus); no Node at runtime; SQLite WAL, zero deps | Native: goroutines cheap, 1 ticker fan-out; must exclude SSE from request-count middleware | Recharts/Tremor Raw (copy-paste, Tailwind v4) — richest dashboard vocabulary | **RECOMMENDED DEFAULT** |
| Next.js (self-hosted standalone) + SSE route handlers | Good, but needs force-dynamic + heartbeat; serverless timeouts don't apply self-hosted | Node image ~200MB+; standalone output; still need separate store/pubsub for fan-out | Fine self-hosted (persistent process); must raise fd limits, disable buffering (X-Accel-Buffering) | Same React chart ecosystem | Heavier than value at this scale; pick only if team is Next-only |
| SvelteKit + SSE | Good; Svelte 5 runes ideal for streaming state | Node runtime unless static-export+Go (goilerplate pattern ~53MB with Go embed) | Same as Next self-hosted | Svelte chart libs thinner than Recharts/Tremor | Good second choice; weaker chart catalogue |
| Go + htmx + SSE (hx-sse, OOB swaps) | Great for tables/badges; zero client JS; server renders fragments | Smallest: Go binary only, no build step | Native Go, same as recommended | Weak — charts need hand-rolled canvas; "detailed viz" suffers | Reject for this brief (viz-heavy), fine for a minimal status page |
| WS-based (Socket.io/Pusher/Ably) | Bidirectional (unneeded) | Extra service/vendor, sticky sessions, heartbeat code | Heaviest ops | — | Reject: no bidirectional need; SSE covers it |

Recommended default: **Go backend (SSE hub + SQLite/jsonl event store, single binary with embedded
frontend) + React+Vite SPA (Tailwind + Tremor Raw/Recharts) served from the binary.**
Reasons: (a) single-container/single-binary deploy matches one-VM scale — precedents: nanostatus
Go+React+SSE→~16MB distroless (https://github.com/gjames8/nanostatus), updu Go+SvelteKit+SSE+SQLite
single binary (https://github.com/nwpeckham88/updu), astral Rust+Svelte+SSE 7.1MB/8.88MB RSS
(https://github.com/arifintahu/astral), docker-stats-dashboard Go-agent→SvelteKit→SSE
(https://github.com/neozhu/docker-stats-dashboard); (b) SSE is the documented right default for
read-only dashboards (server pushes, client listens; auto-reconnect; HTTP-native);
(c) Tremor Raw/Recharts gives KPI/BarList/Tracker/Spark primitives copy-pasteable without a component
library lock-in (https://tremor.so, https://github.com/tremorlabs/tremor); (d) Recharts live-update
ergonomics confirmed via context7 library `/recharts/recharts`: disable per-component animation on
streaming series (`isAnimationActive`), isolate frequently-changing components to avoid full redraws,
HighlightAndZoom ReferenceArea pattern for drag-zoom timelines. Do NOT add k8s/external DB SaaS/auth
provider: SQLite WAL + optional OPENCODE_SERVER_PASSWORD-style basic auth (opencode web precedent,
https://opencode.ai/docs/web/) is sufficient; justify anything heavier against this baseline.

## 6. Data-to-widget mapping (session record → widgets)
Record: id, title, model, status, tokens {in/out/total}, cost, messages[], tool calls[],
subagents[], cwd, timestamps {created/updated}.
- Fleet row: status→compound badge; title→row title (2-line clamp); model→chip;
  tokens.total→mono number + 72x24 sparkline (tokens/min); cost→mono + BarList aggregate in header;
  cwd→project label (group key); updated→relative time; subagents.length→"↳N" expander chip.
- Fleet header KPIs: active count; waiting-input count (amber); Σ tokens/hr area chart;
  Σ cost today + cost-by-model donut; error count; SSE connection status dot.
- Session detail Transcript tab: messages[] full fidelity (system/user/assistant/tool) with subagent
  turns interleaved chronologically + agent-lane chips (LangSmith Trajectory steal).
- Turns tab: one card per assistant-turn: input summary, tool calls count, tokens, duration, expandable.
- Timeline tab: horizontal time axis: message dots, tool-call bars (duration ∝ width), subagent lanes,
  token-rate area underlay; drag-zoom (Recharts ReferenceArea pattern); click→inspector.
- Tools tab: table of tool calls (name, status glyph, duration, tokens) + top-5 error/latency summary
  + click row→right-rail inspector (I/O JSON, error, timing) — Braintrust Spans steal.
- Cost+Tokens tab: cumulative cost line, in/out stacked bars, cost-by-model donut, context-window
  ProgressCircle (used/total), per-subagent cost roll-up table.
- Raw tab: full session JSON (collapsible), deep-linkable.
- Global timeline view: all sessions as swimlanes on one axis; Compare view: 2-4 sessions' KPI cards
  + overlaid cost/token lines.

## 7. Verification log
- Patterns: LangSmith view-traces/trajectories/dashboards docs; Helicone sessions/requests docs;
  Braintrust examine-traces/dashboards/observe docs; lazydocker GitHub; k9s docs; opencode web docs +
  sst/opencode home.tsx + dialog-session-list.tsx; Tremor site/blocks/installation;
  bee-tui TUI design-patterns research notes; kiroxy Svelte-dashboard measurements (Svelte 5 20.7KB gz).
- Stack: Vercel WS-vs-SSE guide; ShellProp SSE-vs-WS; StackNotice 2026; NextLevelNextJS self-host
  SSE+WS; goilerplate htmx-SSE realtime; trippw Go realtime dashboards SSE; self-host Next.js SSE guide
  (timeouts/force-dynamic); nanostatus/updu/astral/docker-stats-dashboard repos (single-binary+SSE+SQLite).
- Skill: frontend skill loaded — applied shared axioms + anti-default discipline; taste-skill.md
  scoped to non-dashboard work (noted, only discipline reused); Layer-A choice for ops UI recorded.
- context7: library ID `/recharts/recharts` used for live-update ergonomics
  (isAnimationActive per-component, isolate-components perf pattern, HighlightAndZoom ReferenceArea).
  Tremor Raw reference: https://context7.com/tremorlabs/tremor/llms.txt (copy-paste Recharts+Radix comps).

## Decisions
- Default stack = Go SSE hub + embedded React SPA (Tremor Raw/Recharts), single binary, SQLite WAL.
  Rationale: smallest ops surface for one VM; richest chart vocabulary; multiple single-binary
  SSE precedents with measured sizes.
- Fleet overview is the default landing view, not session detail or marketing content.
- SSE + snapshot re-sync on reconnect; no WebSockets unless a bidirectional need appears.
