/**
 * Fleet activity derivation (W2-2): PURE diff of consecutive Snapshot ticks.
 *
 * BOUNDARY (binding): a Snapshot carries sessions + KPIs only — it has NO
 * per-session tool-call stream. Polling N sessions per tick
 * (GET /api/sessions/:id/tools) is BANNED this wave: O(N) fan-out per tick
 * would blow the collector budget and duplicate Wave 3's Timeline/Tools tabs,
 * which own the full tool-call stream. Ticker text therefore describes
 * SESSION-LEVEL transitions only (appeared/disappeared, status old→new,
 * waiting-input raised/cleared, error appeared, completed with token delta,
 * top token/cost movers) and must NEVER fabricate tool names.
 */

import type { Harness, SessionRow, Snapshot } from "./sse";
import { formatCompact, formatUSD } from "./format";

/** Ring capacity: newest-first, oldest evicted past this length. */
export const ACTIVITY_RING_CAP = 100;
/** Visible cap in the ticker before "show more" (see EventTicker). */
export const ACTIVITY_VISIBLE_CAP = 30;
/**
 * Mover thresholds: a session is a "top mover" only when its per-tick delta
 * exceeds these. Rationale: per-tick token jitter under ~1k tokens (a single
 * short assistant turn) is noise on a fleet wall; surfacing it would drown
 * real transitions. Cost threshold mirrors ~1k blended tokens in USD.
 */
export const TOKEN_MOVER_DELTA = 1000;
export const COST_MOVER_DELTA = 0.01;
/** Max mover entries per tick per metric (tokens, cost). */
export const MOVER_CAP = 3;

export type ActivityKind =
  | "appeared"
  | "disappeared"
  | "status"
  | "waiting-raised"
  | "waiting-cleared"
  | "error"
  | "completed"
  | "tokens-mover"
  | "cost-mover";

export interface ActivityEvent {
  kind: ActivityKind;
  sessionId: string;
  /** Owning session title (link label + context). */
  sessionTitle: string;
  /** Owning project (directory basename) for fleet-wide context. */
  project: string;
  /** Owning directory (exact match key for view-scoped tickers). */
  directory: string;
  /** Owning harness (opencode/pi) for mixed-fleet context. */
  harness: Harness;
  /** Session-level one-liner, e.g. "ses_abc running → idle · +12.4k tokens". */
  text: string;
  /** Glyph per DESIGN section 6 (● ◌ ▲ ✕ ▪ ◉ +). */
  glyph: string;
  /** Snapshot seq that produced this event (relative-time anchor). */
  seq: number;
}

/** Source context shared by every event push (one row, one owner). */
function sourceOf(row: SessionRow): Pick<ActivityEvent, "sessionTitle" | "project" | "directory" | "harness"> {
  return {
    sessionTitle: row.title,
    project: row.project,
    directory: row.directory,
    harness: row.harness ?? "opencode",
  };
}

/** Harness glyph for activity rows: π for pi, OC for opencode. */
export function harnessGlyph(harness: Harness): string {
  return harness === "pi" ? "π" : "OC";
}

function glyphFor(kind: ActivityKind, status?: string): string {
  switch (kind) {
    case "appeared":
      return "+";
    case "disappeared":
      return "▪";
    case "waiting-raised":
      return "▲";
    case "waiting-cleared":
      return "◌";
    case "error":
      return "✕";
    case "completed":
      return "◌";
    case "tokens-mover":
    case "cost-mover":
      return "◉";
    case "status":
      if (status === "running") return "●";
      if (status === "waiting-input") return "▲";
      if (status === "error") return "✕";
      if (status === "archived") return "▪";
      return "◌";
    default:
      return "◌";
  }
}

/**
 * Derive this tick's activity from consecutive snapshots.
 *
 * - `prev === null` (first tick) returns []: there is no baseline to diff
 *   against, so the ticker starts silent instead of flooding with "appeared".
 * - Order within a tick is derivation order: session transitions in
 *   `next.sessions` order, then top token movers, then top cost movers.
 *   Callers prepend the whole tick newest-first via `prependEvents`.
 */
export function diffSnapshots(
  prev: Snapshot | null,
  next: Snapshot,
): ActivityEvent[] {
  if (prev === null) return [];
  const events: ActivityEvent[] = [];
  const prevById = new Map<string, SessionRow>(
    prev.sessions.map((row) => [row.id, row]),
  );
  const nextById = new Map<string, SessionRow>(
    next.sessions.map((row) => [row.id, row]),
  );

  for (const row of next.sessions) {
    const old = prevById.get(row.id);
    if (!old) {
      events.push({
        kind: "appeared",
        sessionId: row.id,
        ...sourceOf(row),
        text: `${row.id} appeared · ${row.status}`,
        glyph: glyphFor("appeared"),
        seq: next.seq,
      });
      continue;
    }
    if (old.status !== row.status) {
      events.push({
        kind: "status",
        sessionId: row.id,
        ...sourceOf(row),
        text: `${row.id} ${old.status} → ${row.status}`,
        glyph: glyphFor("status", row.status),
        seq: next.seq,
      });
      if (row.status === "waiting-input") {
        events.push({
          kind: "waiting-raised",
          sessionId: row.id,
          ...sourceOf(row),
          text: `${row.id} needs input ▲`,
          glyph: glyphFor("waiting-raised"),
          seq: next.seq,
        });
      }
      if (old.status === "waiting-input") {
        events.push({
          kind: "waiting-cleared",
          sessionId: row.id,
          ...sourceOf(row),
          text: `${row.id} input cleared ◌`,
          glyph: glyphFor("waiting-cleared"),
          seq: next.seq,
        });
      }
      if (row.status === "error") {
        events.push({
          kind: "error",
          sessionId: row.id,
          ...sourceOf(row),
          text: `${row.id} error ✕`,
          glyph: glyphFor("error"),
          seq: next.seq,
        });
      }
      if (
        old.status === "running" &&
        row.status === "idle" &&
        row.tokens.total > 0
      ) {
        const delta = row.tokens.total - old.tokens.total;
        events.push({
          kind: "completed",
          sessionId: row.id,
          ...sourceOf(row),
          text: `${row.id} running → idle · +${formatCompact(delta)} tokens`,
          glyph: glyphFor("completed"),
          seq: next.seq,
        });
      }
    }
  }

  for (const row of prev.sessions) {
    if (!nextById.has(row.id)) {
      events.push({
        kind: "disappeared",
        sessionId: row.id,
        ...sourceOf(row),
        text: `${row.id} disappeared`,
        glyph: glyphFor("disappeared"),
        seq: next.seq,
      });
    }
  }

  const tokenMovers = next.sessions
    .map((row) => {
      const old = prevById.get(row.id);
      const delta = old ? row.tokens.total - old.tokens.total : row.tokens.total;
      return { row, delta };
    })
    .filter((m) => m.delta > TOKEN_MOVER_DELTA)
    .sort((a, b) => b.delta - a.delta)
    .slice(0, MOVER_CAP);
  for (const m of tokenMovers) {
    events.push({
      kind: "tokens-mover",
      sessionId: m.row.id,
      ...sourceOf(m.row),
      text: `${m.row.id} +${formatCompact(m.delta)} tokens`,
      glyph: glyphFor("tokens-mover"),
      seq: next.seq,
    });
  }

  const costMovers = next.sessions
    .map((row) => {
      const old = prevById.get(row.id);
      const delta = old ? row.cost - old.cost : row.cost;
      return { row, delta };
    })
    .filter((m) => m.delta > COST_MOVER_DELTA)
    .sort((a, b) => b.delta - a.delta)
    .slice(0, MOVER_CAP);
  for (const m of costMovers) {
    events.push({
      kind: "cost-mover",
      sessionId: m.row.id,
      ...sourceOf(m.row),
      text: `${m.row.id} +${formatUSD(m.delta)} cost`,
      glyph: glyphFor("cost-mover"),
      seq: next.seq,
    });
  }

  return events;
}

/** Mover kinds refresh per tick (token/cost deltas); everything else is
 * history worth keeping. */
const REFRESH_KINDS: readonly ActivityKind[] = ["tokens-mover", "cost-mover"];

/**
 * Prepend one tick's events onto the ring, newest-first, bounded at
 * ACTIVITY_RING_CAP. Mover events collapse per session: a fresh token/cost
 * delta replaces that session's older one instead of stacking the same row
 * every tick (the spam the ticker was criticized for). Status/appeared/
 * disappeared events always accumulate as history. Pure — returns a new array.
 */
export function prependEvents(
  ring: readonly ActivityEvent[],
  events: readonly ActivityEvent[],
): ActivityEvent[] {
  const refreshed = new Set(
    events
      .filter((e) => (REFRESH_KINDS as readonly string[]).includes(e.kind))
      .map((e) => `${e.kind}\0${e.sessionId}`),
  );
  const kept =
    refreshed.size === 0
      ? ring
      : ring.filter(
          (e) =>
            !(REFRESH_KINDS as readonly string[]).includes(e.kind) ||
            !refreshed.has(`${e.kind}\0${e.sessionId}`),
        );
  return [...events, ...kept].slice(0, ACTIVITY_RING_CAP);
}

/** View scope for the ticker: project drill-ins see their own directory,
 * session views see their own session, everything else sees the fleet. */
export interface TickerScope {
  directory?: string | null;
  sessionId?: string | null;
}

/** Filter the ring to one view's scope (empty scope = whole fleet). Pure. */
export function scopeEvents(
  events: readonly ActivityEvent[],
  scope: TickerScope,
): ActivityEvent[] {
  if (scope.sessionId !== undefined && scope.sessionId !== null) {
    return events.filter((e) => e.sessionId === scope.sessionId);
  }
  if (scope.directory !== undefined && scope.directory !== null) {
    return events.filter((e) => e.directory === scope.directory);
  }
  return [...events];
}
