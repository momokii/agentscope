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
