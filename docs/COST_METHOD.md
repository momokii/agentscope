# How cost and tokens are counted

Every spend/token number on the dashboard derives from **harness-recorded
figures** — the dashboard never prices tokens itself. Both harnesses record
in USD at write time, so fleet sums mix providers without conversion.
What differs is *where* the numbers come from and *what each field contains*.

## Sources

| Number | Opencode (WAL db) | Pi (JSONL) |
|---|---|---|
| Fleet row tokens / cost | `session` table rollups (`cost`, `tokens_input/output/reasoning/cache_read`), maintained by opencode itself | Summed by the dashboard from per-message `usage` records (`input/output/reasoning/cacheRead/cacheWrite/totalTokens/cost.total`) |
| `tokens/hr` sample | `SUM(message.data.tokens.total)` over messages created in the trailing 60s | `SUM(usage.totalTokens)` over usage events in the trailing 60s |
| `cost_today` sample | `SUM(message.data.cost)` over messages created since local midnight | `SUM(usage.cost.total)` over usage events since local midnight |
| Windowed cost/tokens (24h/7d/30d) | One indexed conditional-sum pass (`internal/db/windows.go`) | In-memory sum over the per-file usage events (`PiWindowMetrics`) |
| Detail tabs (per-turn cost/tokens) | `ExportMessage.info.tokens` / `.cost` natively | Rebuilt from the matching `usage` record (`usageTokensJSON`); identical sums to the fleet row by construction |

Live evidence (checked 2026-11-01): an opencode assistant message carries
`tokens: {total: 34742, input: 34687, output: 55, reasoning: 0,
cache: {write: 0, read: 0}}, cost: 0.0104721`; a pi usage record carries
`{input: 2913, output: 23, cacheRead: 320, cacheWrite: 0, reasoning: 21,
totalTokens: 3256}` — note `2913+23+320 = 3256`: reasoning is already
inside output and is never added twice.

## Per-harness differences (all verified against live data)

1. **Who rolls up.** Opencode rows read opencode's own session-table
   rollups. Pi rows are summed by the dashboard from raw usage records.
   Both agree with their detail-tab sums; cross-check accordingly.
2. **Cache write.** The opencode `session` table has no `cache_write`
   column, so opencode `In`/`Total` never count cache writes (message-level
   `tokens.total` may still include them). Pi `totalTokens` is
   writer-reported and includes `cacheWrite`.
3. **Reasoning placement.** Opencode keeps `reasoning` separate from
   `output` everywhere. Pi folds reasoning into `output` (its `totalTokens`
   excludes it to avoid double-counting). Consequence: compare `Total`
   and `Cost` across harnesses, not `In`/`Out` splits.
4. **Pricing.** USD figures are priced by each harness's provider stack at
   write time (e.g. `opencode-go/muse-spark-*` vs pi providers). Same
   currency, different pricers — model labels always show `provider/model`.

## `tokens/hr`: what the number means

`tokens_per_hour = (message tokens in the trailing 60s, both harnesses) × 60.`

- It is an **extrapolated now-rate**, not a measured hourly total: a quiet
  minute reads 0, a burst minute reads 60× the burst. Read it as
  "generating at this pace right now".
- It sums **message-level** figures, never session-total deltas — so a
  completing subagent (whose whole history lands on the parent at once)
  cannot fake a spike. This is the same reason fleet `CostTotal` sums
  pre-roll-up `RawCost` (children counted once).
- It refreshes on the **60s metrics loop**, independent of the 10s poll
  tick. `metrics_age_ms` on the KPI strip tells you how stale the sample
  is; a failed refresh keeps last-good values and warns once in the logs.

## Windowed spend and midnight

`cost_today` (and the budget alert) use **local midnight** on the machine
running the dashboard (`TZ`, default `Asia/Jakarta` in compose). The
24h/7d/30d windows are rolling from `now`. Custom `from` date filters fall
back to lifetime totals — per-day attribution beyond the fixed presets is
not computed.

## Where approximations live

- **Compare lines** are activity-scaled sketches (spark-bucket shape scaled
  to each session's lifetime totals), not audited per-bucket history. Cards
  show exact totals; lines show burn-rate shape.
- **History** (`#/history`) records one fleet + per-project sample per
  metrics tick — recorded values only, never averaged or interpolated.
