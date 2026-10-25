/**
 * Fleet ordering, recency grouping, and search filtering.
 *
 * Pure functions over the frozen Snapshot shape (see ./sse.ts, mirrored from
 * internal/model SessionRow). No rendering, no state — Fleet.tsx owns state
 * and calls these; unit-tested in fleetSort.test.ts.
 */

import type { SessionRow, SessionStatus } from "./sse";

/**
 * Display rank: running first, exited last. ARCHIVED sorts after ERROR
 * (terminal states sink); within the same status, live panes precede exited
 * ones, then most-recently-updated first.
 */
export const STATUS_RANK: Record<SessionStatus, number> = {
  running: 0,
  "waiting-input": 1,
  idle: 2,
  error: 3,
  archived: 4,
};

/** Exited = zellij pane gone (or orphan with no live pane). Sinks in sort. */
export function isExited(row: SessionRow): boolean {
  return row.pane.exited || row.orphan;
}

export function sortFleetRows(rows: readonly SessionRow[]): SessionRow[] {
  return [...rows].sort((a, b) => {
    const rank = STATUS_RANK[a.status] - STATUS_RANK[b.status];
    if (rank !== 0) return rank;
    const exited = Number(isExited(a)) - Number(isExited(b));
    if (exited !== 0) return exited;
    return a.updated_relative_ms - b.updated_relative_ms;
  });
}

export type RecencyGroup = "today" | "yesterday" | "older";

export const RECENCY_LABELS: Record<RecencyGroup, string> = {
  today: "Today",
  yesterday: "Yesterday",
  older: "Older",
};

const DAY_MS = 86_400_000;

/**
 * Calendar-day grouping from the row's relative age: the absolute update
 * instant is `nowMs - updated_relative_ms`; Today/Yesterday compare against
 * local-midnight boundaries (DESIGN section 6 recency rule).
 */
export function groupKeyFor(
  updatedRelativeMs: number,
  nowMs: number,
): RecencyGroup {
  const startOfToday = new Date(nowMs);
  startOfToday.setHours(0, 0, 0, 0);
  const updatedAt = nowMs - Math.max(0, updatedRelativeMs);
  if (updatedAt >= startOfToday.getTime()) return "today";
  if (updatedAt >= startOfToday.getTime() - DAY_MS) return "yesterday";
  return "older";
}

export interface FleetGroup {
  key: RecencyGroup;
  rows: SessionRow[];
}

/** Group pre-sorted rows, preserving order; empty groups are omitted. */
export function groupFleetRows(
  rows: readonly SessionRow[],
  nowMs: number,
): FleetGroup[] {
  const order: RecencyGroup[] = ["today", "yesterday", "older"];
  const buckets: Record<RecencyGroup, SessionRow[]> = {
    today: [],
    yesterday: [],
    older: [],
  };
  for (const row of rows) {
    buckets[groupKeyFor(row.updated_relative_ms, nowMs)].push(row);
  }
  return order
    .map((key) => ({ key, rows: buckets[key] }))
    .filter((group) => group.rows.length > 0);
}

/** Case-insensitive substring match over id/title/project/directory/model. */
export function filterSessions(
  rows: readonly SessionRow[],
  query: string,
): SessionRow[] {
  const needle = query.trim().toLowerCase();
  if (needle === "") return [...rows];
  return rows.filter((row) =>
    [row.id, row.title, row.project, row.directory, row.model].some((field) =>
      field.toLowerCase().includes(needle),
    ),
  );
}

/** Fleet-UX status filter: one session status, or all. */
export type StatusFilter = "all" | SessionStatus;

export const STATUS_FILTERS: readonly StatusFilter[] = [
  "all",
  "running",
  "waiting-input",
  "idle",
  "error",
  "archived",
];

/** Live = needs operator attention now: running or waiting-input. */
export function isLiveStatus(status: SessionStatus): boolean {
  return status === "running" || status === "waiting-input";
}

export interface StatusCounts {
  counts: Record<SessionStatus, number>;
  total: number;
}

/** One pass over the snapshot: per-status counts for the filter chips. */
export function countByStatus(rows: readonly SessionRow[]): StatusCounts {
  const counts: Record<SessionStatus, number> = {
    running: 0,
    "waiting-input": 0,
    idle: 0,
    error: 0,
    archived: 0,
  };
  for (const row of rows) counts[row.status] += 1;
  return { counts, total: rows.length };
}

/**
 * Single-pass fleet filter: search query + status chip + live-only toggle
 * compose (AND). Cheap at 1963 rows — one pass, no per-row allocation.
 */
export function applyFleetFilters(
  rows: readonly SessionRow[],
  query: string,
  status: StatusFilter,
  liveOnly: boolean,
): SessionRow[] {
  const needle = query.trim().toLowerCase();
  return rows.filter((row) => {
    if (status !== "all" && row.status !== status) return false;
    if (liveOnly && !isLiveStatus(row.status)) return false;
    if (needle === "") return true;
    return [row.id, row.title, row.project, row.directory, row.model].some(
      (field) => field.toLowerCase().includes(needle),
    );
  });
}
