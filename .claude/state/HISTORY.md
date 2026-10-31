# HISTORY — Session Log

Condensed chronology. Commit hashes are on `main`; older notepads hold the blow-by-blow
(`.omo/notepads/opencode-dashboard/issues.md`).

## 2026-10-24 — Bootstrap, research, plan
- `a4e3e6d` scaffold + `.claude/` infrastructure.
- `dc1a918` research briefs + decision-complete work plan (Go+SSE, SQLite WAL read-only,
  poll collector, project UI, single-host).
- `9ba8eb7` Wave 1: DB reader, zellij/proc runtime, SSE hub, collector.
- `f825c1e` Waves 2+3: Fleet/KPI/ticker/CmdBar/pins, SessionDetail tabs, Compare,
  Global timeline, design tokens.
- `755f958`, `3cf520e` Wave 4: entrypoint, Dockerfile, compose, Basic auth, security +
  VM runbooks, F1/F3/F4 gates (F2 human-gated).

## 2026-10-25 — Deploy truth, auth, live DB budget
- `6c8c5e3`, `9674041` host networking replaces bridge publish (healthy-but-unreachable
  fix); one-click `deploy.sh` + root README.
- `88fb570` deploy UX; `77ccacc`, `5306912` guarded `BIND_ADDR` opt-in.
- `15f4c7d` pass `DASHBOARD_AUTH` through compose; `a6c108d` authenticated healthcheck
  and deploy proofs.
- `28d2088` incremental event tail + `DB_TIMEOUT` after prod fleet came up empty from
  per-tick timeouts on the 8.2 GB WAL DB.

## 2026-10-26 — Status truth + runtime-in-container fixes
- `08f2f86` recency-gated statuses (`ACTIVE_WINDOW`), delta-only fresh events,
  newest-session-per-directory pane claim, live proc/pane KPIs (the false “almost
  everything running” mass collapsed to single digits).
- `1004a12` rw zellij socket dir + independent pane/proc scans; `ef84984` keep procs whose
  `/proc/<pid>/cwd` read is denied, with pane-presence fallback per directory.
- `9531c1f` project model pin; `b595687` error only from the newest assistant turn.

## 2026-10-27 — Metrics truth + project UX
- `07705be` cookie-authenticated SSE, message-level `tokens_per_hour`/`cost_today`,
  `cost_total`/`metrics_age_ms`, `Snapshot.Projects`, Projects landing, pagination, date
  presets, recency buckets.
- `6a3fc5d` local-midnight cost + TZ forwarding + `/etc/localtime:ro`, pane-based project
  presence, compose/deploy config corrections.
- `6f72903` label project presence as panes when pane-derived.
- Docs reconciliation (this change, uncommitted): `CLAUDE.md`, `.claude/state/*`,
  `.claude/README.md`, `README.md`, `web/README.md`, `docs/{VM_DEPLOY,DOCKER_VERIFY,SECURITY}.md`.
  Recorded the SPA detail fixture gap and corrected the test baseline to Go 229/10,
  web 193/26.

## 2026-10-27 — Wave 5 multi-harness (pi monitoring) + global pi default model
- P1 `e2da795`: `SessionRow.Harness` + `internal/pi` JSONL reader (mtime cache,
  shared DeriveStatus gate, fork children, corrupt-file isolation).
- P2 `91ffcb4`: `ScanAgents`, pane `Harness()` from `pane_command` (never titles),
  per-(directory,harness) claims, harness-scoped screen resolve.
- P3 `ddda0f2`: collector merge pre-Enrich + pi-aware watermark signature +
  `PI_DATA_DIR` (nil-safe; opencode-only hosts byte-identical).
- P4 `fe5c707`: TS mirror + boundary normalization, OC/π chips, harness filter
  (`?harness=`), pi rows → Live pane tab; 215 web tests green.
- P5 `d936e6b`: compose `:ro` sessions mount, SECURITY §2/§3/§5, README,
  `.env.example` rewrite; fixed SessionDetail pi screen-tab fallback (null-data
  empty state had made the screen tab unreachable) after review.
- Redeployed `:8081` (clean rebuild after `--down`; note: healthy-stack re-runs
  skip rebuild by design): fleet 1966 opencode + 6 pi, live pi running + pane
  claimed, screen 200, export 404 (P6), 5-mount allowlist verified.
- Whole-implementation check: full gates green (go build/vet/test, tsc, vitest
  217/217, token gate, security greps rows 1–4); one web flake observed once,
  green on 3 consecutive reruns; SECURITY row-2 note corrected for pre-existing
  `credentials: "same-origin"` fetch hits.
- Global pi default model switched `kimi-k3` → `muse-spark-1.3-contributor`
  (`~/.pi/agent/settings.json`, same `opencode-go` provider; backup
  `/tmp/settings.json.bak`). Live sessions keep their own model; new sessions
  pick it up.

## 2026-10-27 — Redeploy non-loopback (operator-requested tailnet access)
- User reported `http://100.124.184.116:8081` (VM tailnet IP) unreachable after
  the Wave 5 redeploy: it had used the default loopback-only bind, while the
  previous setup bound non-loopback. Confirmed `.116` is this VM's tailnet IP
  (tailscale exit-node container) with no `tailscale serve` configured.
- Redeployed `BIND_ADDR=0.0.0.0` + generated `DASHBOARD_AUTH` (Basic gate;
  password handed to operator, never committed). Live proofs: 401 without
  auth on both 127.0.0.1 and the tailnet IP; 200 + full fleet
  (1966 opencode + 6 pi) with auth. Now on `:8082` (8080/8081 squatted).

## 2026-10-27 — Validity audit + screen fix + pi metrics + diagrams (operator round)
- Validity audit: `buildProjects`/`buildKPIs` already RawCost-based (no double
  count, both harnesses); found `tokens_per_hour`/`cost_today` opencode-only.
  Fixed in TASK-302 (pi usage events + combined refresher sample).
- Bug: `/screen` served the directory pane for 110 non-claimant sessions
  ( чужой pane/shell text as own Live pane). Fixed in TASK-301: claimant-only
  200, 409 + owner hint otherwise (live proof included).
- TASK-303: `ProjectInsight` (distribution bar + harness split + aggregate
  token-rate area) in project drill-in; fleet throughput strip; `lib/rate`
  sums, hand-rolled SVG, DESIGN tokens only. Web 230/230 green.
- Redeployed `:8082` (non-loopback + auth preserved): fleet 1966+6,
  live_processes=4 (1 opencode + 3 pi), owner-hint 409 live, claimant 200,
  served bundle matches committed dist.

## 2026-10-27 — Validity + UX round (operator review feedback)
- TASK-311: project drill-in no longer shows the fleet-wide KPI strip (scoped
  header + insight only). TASK-312: metrics age ticks live (snapshot age +
  frame age); KPI min-width 136px fits full labels; delta reads "msg metrics".
- TASK-313: per-dot tooltips + HTML tick axis on RateChart (SVG text smeared
  under preserveAspectRatio=none — caught in screenshots, fixed, re-shot).
- TASK-314: Projects landing gains active-since presets + 12/page pagination.
- TASK-315: activity events carry title/project/harness (linked + right-side
  source); ticker show-more replaced by Prev/Next paging; session links
  de-underlined (accent on hover).
- TASK-316: DataGuide (?) glossary in Fleet (both modes) + Projects.
- Verified with headless-Chromium screenshots + DOM probes: KPI labels fit,
  axis crisp, guide renders, pi screen tab live, ticker style correct,
  deployed bundle byte-identical to committed dist.
- Redeployed `:8082` (non-loopback + auth preserved). Web 236/236, gates green.

## 2026-10-27 — Operator feedback round 2 (validity + UX)
- TASK-321: KPI live_panes counted every pane incl. shells (9) while cards
  summed agent panes (6) — fixed to agent-only (KPI == Σ cards live);
  zombie/shell semantics documented; model + runtime docs updated.
- TASK-322: RateChart dots gained a persistent hover/focus readout + 8px
  hit targets (native <title> was the only per-tick value).
- TASK-323: page-size select 5/10/20/25/50 on fleet + landing pagination.
- TASK-324: Active/Waiting/Errors now follow the date/bucket/harness/search
  scope (fleet strip, landing header, project header + insight); lifetime
  tokens/cost and live presence stay global and labeled. Live proof:
  landing 22→3 projects, 6→4 procs under 24h.
- TASK-325: Refresh button beside Live (both toolbars) → GET /api/fleet
  through the snapshot pipeline; works paused.
- TASK-326: DataGuide updated (agent panes, KPI scoping, readout).
- Verified headless-Chromium (KPI scoping live, refresh button, page sizes)
  + full web suite 243/243. Redeployed :8082, committed per task.

## 2026-10-28 — Faceted counts follow every filter except their own
- Follow-up to the round-2 review: project detail showed insight "IDLE 1"
  vs distribution "IDLE 13" (statusScope ignored the bucket). Reworked to
  true faceting — status/harness/bucket chips each count rows left by all
  OTHER filters; KPIs follow the status scope; project header+insight
  follow directory+date+bucket+search. Live probe: status ALL 2 / harness
  ALL 2 / buckets 2·2·12·0·14, all coherent.
- No errors in the gate suite (248 web + full backend green); continued
  after a transient harness hiccup the user reported.

## 2026-10-28 — Final verification pass (all tasks)
- Full gates: go build/vet/test (9 pkgs), tsc, vitest 248/248, token gate,
  security greps rows 1–4 — all green. Tree clean, commits per step kept.
- Live proofs on :8082 (non-loopback + Basic gate): 401/200 auth, fleet
  1966 opencode + 6 pi, cost windows monotone (total 84.15 / 24h 4.97 /
  7d 15.02 / 30d 15.02 / today 0.71), Σ per-session 24h == global exact,
  live_panes 6 == Σ project panes 6, claimant screen 200, non-claimant
  409 + owner hint, pi export 404 (P6), 5-mount allowlist, muse-spark-1.3
  global default, deployed bundle byte-identical to committed dist.
- UI probes (headless Chromium, authed): 24h KPI card switches to
  "Cost 24h $4.98", tooltips on badges/KPIs/cards/refresh/guide all render,
  metrics age ticks live, page selector + refresh + guide present.
- Open backlog (explicitly deferred, not defects): TASK-206 pi transcript
  drill-down (P6), TASK-101 human-run F2 VM truth matrix, TASK-103/104/105/106.

## 2026-10-28 — TODAY cost + logo + tooltips verification round
- TODAY preset folded the standalone Cost-today card into the windowed Cost
  card (single "Cost today" card, no duplication); delta compares same-window
  values. Live probe: ALL shows Cost total $84.36, TODAY shows Cost today $0.91.
- Logo: terminal-prompt mark (accent chevron + cursor on ink-950) as inline
  header brand + /logo.svg tab favicon (200, gated like everything else).
- Verified live: page selector chip-styled (ink-800/ink-600/mono, dark
  dropdown, 5/10/20/25/50), tooltips on badges/KPIs/cells/cards, header logo,
  windowed project costs (reconcile exact vs global).

## 2026-10-28 — Activity ticker rework (scoped + deduped)
- The fleet-wide ticker showed in every view, spamming repeat token-movers.
  Now: project drill-ins see their directory, session views their session,
  with a scope chip; per-session movers collapse to the newest (status
  history still accumulates). Live probe: "opencode-dashboard activity
  ticker" with project-only rows + source context.

## 2026-10-26 — Activity ticker collapse toggle
- Operator questioned the ticker's value. Answer: "what just changed" feed,
  now scoped + deduped — plus a header toggle that hides the list entirely,
  persisted in localStorage across reloads. Verified live (hide → 0 rows →
  reload still 0 rows, Show button present).

## 2026-10-28 — Activity tab, per-tab guides, open-source docs
- TASK-336: `#/activity` full-page log (search, harness slice, paging,
  deep-links, guide blurb) reusing the ticker row component; nav item added.
- TASK-337: DataGuide `blurb` intros on Compare / Timeline / SessionDetail
  (+ pi screen fallback) explaining each view's purpose.
- TASK-338: README rewritten end-to-end for open source (features,
  screenshots, quickstart, config, architecture, API, data semantics, UI
  tour, dev, structure, security, runbooks, troubleshooting, roadmap,
  contributing, license) + CONTRIBUTING.md + 4 docs/screenshots.
- Deployed :8082 (non-loopback + auth); live-verified activity tab +
  guide blurbs via screenshots.

## 2026-10-26 — Honest guides + ticker removal (operator review)
- Operator rightly called out copy-pasted guides: every tab now explains
  ITSELF (Compare cards/lines/picker/click; Timeline lanes/bars/dots/area;
  SessionDetail per-tab purposes; Activity kinds/collapse/scope/browse),
  each verified against the rendering source — including correcting a wrong
  Timeline blurb before it shipped.
- Footer ticker deleted (Activity tab is the log now); BottomBar keeps the
  keymap only; dead SessionStub removed. Live probes: no .ticker in footer,
  tailored guides render per tab. Web 257/257, redeployed :8082.

## 2026-10-26 — Open-source docs pass + final consistency proof
- README rewritten end to end (features, screenshots, quickstart, config,
  architecture, API, data semantics, UI tour, dev, structure, security,
  runbooks, troubleshooting, limitations, roadmap, contributing, license);
  CONTRIBUTING.md added; 6 docs screenshots captured from the live deployment
  (landing, project, activity, screen, compare, timeline).
- Removed dead SessionStub + EventTicker leftovers; verified nothing references
  them. Full gates green (Go 9 pkgs, tsc, 261 web tests, token gate).
- Proved deployed == fresh local build of current tree (byte-identical JS),
  so docs screenshots and behavior claims match what's online.

## 2026-10-26 — Ideas backlog recorded (no code)
- Operator asked for the update-ideas list to live in docs so "what's your
  update ideas" is answerable anytime. Recorded TASK-340 (browser
  notifications + daily budget, client-only on the SSE stream) and TASK-341
  (in-tab transcript search, client-side), synced the README roadmap.
  Priority order stands: notifications/budget first, then CI, then P6.

## 2026-10-26 — v1.0.0 released (tagged, deployed)
- Bumped version to 1.0.0 across binary (`internal/hub` Version const),
  image (`compose` + `deploy.sh`), web (`package.json`), and docs.
- Full gates green on the release tree; redeployed :8082 (non-loopback +
  auth); live proofs: healthz reports 1.0.0, fleet 1975 opencode + 8 pi,
  deployed bundle byte-identical to committed dist.
- Tagged v1.0.0 locally. Not yet pushed anywhere — no git remote is
  configured (publishing to GitHub needs `gh auth login` + repo creation
  on the operator's account).

## 2026-10-26 — v1.0.0 final sign-off (release verification)
- Re-ran everything from scratch: backend 9 pkgs green, tsc, vitest 252/32,
  token gate, security greps, dead-ref + ticker-legacy grep — all clean.
- Live deployment proofs: healthz 1.0.0 + 401-gated; fleet 1975 opencode +
  8 pi; cost windows monotone; Σ per-session 24h == global exact; KPI panes
  == Σ project panes; claimant screen 200, non-claimant 409+owner, pi
  export 404; SSE frames carry harness; 5-mount allowlist exact; served
  bundle byte-identical to committed dist and to a fresh local build.
- UI probes on the live build: no footer ticker, Activity nav + view,
  per-tab guides, page-size select, refresh button, scoped KPIs, windowed
  cost cards. Committed screenshots re-shot footer-free and verified current.
- v1.0.0 tagged. Open backlog (explicit, not defects): P6 pi transcripts,
  notifications/budget (340), transcript search (341), F2 matrix, phone
  layout, stress tests, CI, actions, multi-host.

## 2026-10-26 — End-to-end audit: post draft + recommendations verified
- Verified every factual claim slated for the LinkedIn draft against source:
  Go 1.27.3/modernc/CGO-off/distroless, React 19/Vite 6/TS 5.6, no chart lib,
  14 GET-only routes, 9 backend pkgs + 252 web tests green, reconciliation
  proofs live. Found + fixed stale test counts in README/CONTRIBUTING/
  CLAUDE/state (261/28-era → 252/31).
- Headless probe proved a deployment constraint for TASK-340: Notification
  API needs a secure context (localhost yes, tailnet-IP http no) — recorded
  in the task with the tailscale-serve-https path + in-app fallback.
- Added TASK-342 (export JSON download). Rank stands: notifications/budget
  first (with caveat), then CI, then P6.

## 2026-10-26 — Deploy-path hardening (audit fixes)
- Found by end-to-end audit: `DB_TIMEOUT`/`ACTIVE_WINDOW` were documented
  as "host env → compose" but never forwarded (silently ignored); fixed +
  proven live via collector config log. `XDG_RUNTIME_DIR` now passed through
  with the socket mount following it (was hardcoded, breaking non-default
  hosts). Removed healthy-stack rebuild skip (served stale code after pulls).
  `--down` no longer fails on unrelated port squatters; loopback proof
  honors the non-loopback opt-in; proof curls carry explicit `http://`.
  Full gates + redeploy green; README always-build note added.

## 2026-10-26 — Wave 6 shipped as v1.1.0
- TASK-340: opt-in bell + panel + test, budget input with localStorage, in-app toasts, title badge, per-view... (Wave 5.5: alerts). Secure-context probe proved tailnet HTTP denies Notification — fallback path is load-bearing.
- TASK-341: transcript search (per-part text extraction, match count, clear).
- TASK-342: one-click JSON export (fleet snapshot, session detail).
- TASK-107: `.github/workflows/ci.yml` — exact gate suite, pinned actions, Security Gate greps, dist-freshness check. Run pending a remote.
- v1.1.0: version bumped (hub, compose, deploy, web, README); redeployed + verified live.
