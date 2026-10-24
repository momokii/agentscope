/**
 * Compare + GlobalTimeline pure data mapping (W3-3).
 *
 * Snapshot-only by contract: the ONLY per-session history the frozen
 * Snapshot shape carries is `row.spark` (per-bucket activity) plus the
 * row totals (`tokens`, `cost`), `updated_relative_ms`, `status`, and
 * `subagent_count`. There are no per-message timestamps, no per-tool
 * durations, and no parent-linked child rows in this shape — so the
 * shared time base is the spark bucket index (right-aligned = most
 * recent), and subagent nesting is expressed as a `childCount` the views
 * render as nested placeholder lanes. See provenance notes on each
 * function. No rendering, no state — views consume these and draw SVG.
 */

import type { SessionRow, SessionStatus } from "./sse";
import { STATUS_RANK, isExited } from "./fleetSort";

/** One cumulative point on the shared time base (bucket index). */
export interface OverlayPoint {
  /** Shared bucket index: 0 = oldest, length-1 = most recent tick. */
  t: number;
  /** Cumulative cost up to and including this bucket (USD). */
  cost: number;
  /** Cumulative tokens up to and including this bucket. */
  tokens: number;
}

/** One session's overlaid lines. Final point always equals row totals. */
export interface OverlayLine {
  id: string;
  title: string;
  status: SessionStatus;
  points: OverlayPoint[];
  totalCost: number;
  totalTokens: number;
}

/**
 * Per-session cumulative cost/token lines on ONE shared time base.
 *
 * Provenance: `spark` buckets are activity magnitudes, not token counts,
 * so each session's cumulative spark curve is scaled linearly so its
 * final point lands exactly on the row totals (`tokens.total`, `cost`).
 * Shorter sparks are left-padded with zeros (right-aligned: bucket -1 is
 * the latest tick for every session), so mismatched time bases still
 * share one axis. Sessions with an empty (or single-bucket) spark yield
 * exactly one point at t=0 — never a broken chart.
 */
export function overlaySeries(
  sessions: readonly SessionRow[],
): OverlayLine[] {
  const width = Math.max(
    1,
    ...sessions.map((row) => row.spark.length),
  );
  return sessions.map((row) => {
    const buckets = row.spark.length;
    const cum: number[] = new Array<number>(width).fill(0);
    let running = 0;
    for (let t = 0; t < width; t += 1) {
      const src = t - (width - buckets);
      if (src >= 0) running += row.spark[src] ?? 0;
      cum[t] = running;
    }
    const final = cum[width - 1] ?? 0;
    const tokenScale = final > 0 ? row.tokens.total / final : 0;
    const costScale = final > 0 ? row.cost / final : 0;
    // Single-point edge: zero history still renders one honest point.
    const points: OverlayPoint[] =
      width === 1 && final === 0
        ? [{ t: 0, cost: 0, tokens: 0 }]
        : cum.map((c, t) => ({
            t,
            cost: c * costScale,
            tokens: c * tokenScale,
          }));
    return {
      id: row.id,
      title: row.title,
      status: row.status,
      points,
      totalCost: row.cost,
      totalTokens: row.tokens.total,
    };
  });
}

/** One message dot (nonzero activity bucket). */
export interface SwimDot {
  t: number;
  value: number;
}

/** One tool-call bar (bucket magnitude ∝ width). */
export interface SwimBar {
  t: number;
  /** Width fraction 0..1 of this session's own max bucket. */
  width: number;
  value: number;
}

/**
 * One lane on the global timeline. `children` are nested subagent lanes:
 * Snapshot rows carry only a `subagent_count` (no child rows with parent
 * links), so sub-lanes are count-derived placeholders labeled
 * `subagent 1..N of <parent>` — real per-span detail is W3-2's right-rail
 * inspector, not this view (drill-down contract: click → session hash).
 */
export interface SwimLane {
  id: string;
  title: string;
  status: SessionStatus;
  exited: boolean;
  dots: SwimDot[];
  bars: SwimBar[];
  /** Raw spark, rendered as the token-rate area underlay. */
  tokenRate: number[];
  /** Nested subagent placeholder lanes (count = row.subagent_count). */
  children: { id: string; label: string }[];
}

/**
 * All sessions as swimlanes on one time axis.
 *
 * Ordering reuses the fleet contract (`STATUS_RANK`, exited sinks,
 * most-recently-updated first) — imported from `fleetSort`, never forked.
 * Dots mark nonzero buckets; bars carry width ∝ the bucket's share of
 * the session max (width ∝ duration proxy — Snapshot has no per-tool
 * durations, documented here instead of invented).
 */
export function swimlanes(sessions: readonly SessionRow[]): SwimLane[] {
  const ordered = [...sessions].sort((a, b) => {
    const rank = STATUS_RANK[a.status] - STATUS_RANK[b.status];
    if (rank !== 0) return rank;
    const exited = Number(isExited(a)) - Number(isExited(b));
    if (exited !== 0) return exited;
    return a.updated_relative_ms - b.updated_relative_ms;
  });
  return ordered.map((row) => {
    const max = row.spark.length > 0 ? Math.max(...row.spark) : 0;
    const dots: SwimDot[] = [];
    const bars: SwimBar[] = [];
    row.spark.forEach((value, t) => {
      if (value > 0) dots.push({ t, value });
      bars.push({ t, width: max > 0 ? value / max : 0, value });
    });
    const children = Array.from(
      { length: Math.max(0, row.subagent_count) },
      (_, i) => ({
        id: `${row.id}~sub${i + 1}`,
        label: `subagent ${i + 1} of ${row.title}`,
      }),
    );
    return {
      id: row.id,
      title: row.title,
      status: row.status,
      exited: isExited(row),
      dots,
      bars,
      tokenRate: [...row.spark],
      children,
    };
  });
}
