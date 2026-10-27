/**
 * Fleet ordering, recency grouping, and search filtering.
 *
 * Pure functions over the frozen Snapshot shape (see ./sse.ts, mirrored from
 * internal/model SessionRow). No rendering, no state — Fleet.tsx owns state
 * and calls these; unit-tested in fleetSort.test.ts.
 */

import type { Harness, SessionRow, SessionStatus } from "./sse";

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

const HOUR_MS = 3_600_000;
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

/**
 * Status chips shown in the bar. IDLE is deliberately absent: the flat idle
 * mass is split into Recent/Stale/Old age chips instead. `status=idle` still
 * parses for old shared links.
 */
export const STATUS_CHIP_FILTERS: readonly StatusFilter[] = STATUS_FILTERS.filter(
  (filter) => filter !== "idle",
);

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

/** Idle-honesty buckets: the old flat idle mass split by session age. */
export type IdleBucket = "active-recent" | "recent" | "stale" | "old" | "all";

export const IDLE_BUCKETS: readonly IdleBucket[] = [
  "active-recent",
  "recent",
  "stale",
  "old",
  "all",
];

export const DEFAULT_IDLE_BUCKET: IdleBucket = "active-recent";

export const IDLE_BUCKET_LABELS: Record<IdleBucket, string> = {
  "active-recent": "Active + recent",
  recent: "Recent",
  stale: "Stale",
  old: "Old",
  all: "Show all",
};

export const IDLE_BUCKET_TITLES: Record<IdleBucket, string> = {
  "active-recent":
    "Running or waiting sessions plus anything updated in the last 24 hours",
  recent: "Sessions updated within the last 24 hours",
  stale: "Sessions last updated between 24 hours and 7 days ago",
  old: "Sessions last updated more than 7 days ago",
  all: "No age filter — may include thousands of old sessions",
};

export type AgeBucket = "recent" | "stale" | "old";

const RECENT_MS = 24 * HOUR_MS;
const STALE_MS = 7 * 24 * HOUR_MS;

/** Boundary rule: recent ≤ 24h, stale 24h–7d, old > 7d. */
export function ageBucketFor(updatedRelativeMs: number): AgeBucket {
  const age = Math.max(0, updatedRelativeMs);
  if (age <= RECENT_MS) return "recent";
  if (age <= STALE_MS) return "stale";
  return "old";
}

export interface IdleBucketCounts {
  "active-recent": number;
  recent: number;
  stale: number;
  old: number;
  all: number;
}

/** One pass over the full snapshot for the idle-bucket chips. */
export function countByIdleBucket(
  rows: readonly SessionRow[],
): IdleBucketCounts {
  const counts: IdleBucketCounts = {
    "active-recent": 0,
    recent: 0,
    stale: 0,
    old: 0,
    all: rows.length,
  };
  for (const row of rows) {
    const bucket = ageBucketFor(row.updated_relative_ms);
    counts[bucket] += 1;
    if (isLiveStatus(row.status) || bucket === "recent") {
      counts["active-recent"] += 1;
    }
  }
  return counts;
}

export function matchesIdleBucket(
  row: SessionRow,
  bucket: IdleBucket,
): boolean {
  if (bucket === "all") return true;
  const ageBucket = ageBucketFor(row.updated_relative_ms);
  if (bucket === "active-recent") {
    return isLiveStatus(row.status) || ageBucket === "recent";
  }
  return ageBucket === bucket;
}

/** Date-filter presets for last-activity filtering. */
export type DatePreset = "all" | "today" | "24h" | "7d" | "30d";

export const DATE_PRESETS: readonly DatePreset[] = [
  "all",
  "today",
  "24h",
  "7d",
  "30d",
];

export const DATE_PRESET_LABELS: Record<DatePreset, string> = {
  all: "All",
  today: "Today",
  "24h": "24h",
  "7d": "7d",
  "30d": "30d",
};

export interface DateFilter {
  preset: DatePreset;
  /** `YYYY-MM-DD` local calendar date; wins over `preset` when set. */
  from?: string | null;
}

export const DEFAULT_DATE_FILTER: DateFilter = { preset: "all", from: null };

function startOfLocalDay(nowMs: number): number {
  const start = new Date(nowMs);
  start.setHours(0, 0, 0, 0);
  return start.getTime();
}

/** Parse `YYYY-MM-DD` as local midnight; invalid input returns null. */
export function parseLocalDate(value: string): number | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (match === null) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(year, month - 1, day);
  if (
    date.getFullYear() !== year ||
    date.getMonth() !== month - 1 ||
    date.getDate() !== day
  ) {
    return null;
  }
  return date.getTime();
}

/**
 * Rows only carry `updated_relative_ms`, so a custom `from` date derives the
 * absolute update instant from the snapshot's own clock (`nowMs`). That is
 * an approximation bounded by one collector tick.
 */
export function matchesDateFilter(
  updatedRelativeMs: number,
  filter: DateFilter,
  nowMs: number,
): boolean {
  const age = Math.max(0, updatedRelativeMs);
  if (filter.from) {
    const fromMs = parseLocalDate(filter.from);
    if (fromMs !== null) return nowMs - age >= fromMs;
  }
  if (filter.preset === "all") return true;
  if (filter.preset === "today") return nowMs - age >= startOfLocalDay(nowMs);
  if (filter.preset === "24h") return age <= RECENT_MS;
  if (filter.preset === "7d") return age <= STALE_MS;
  return age <= 30 * 24 * HOUR_MS;
}

export interface FleetFilterOptions {
  bucket?: IdleBucket;
  date?: DateFilter;
  /** Drill-in: keep only sessions whose directory matches exactly. */
  directory?: string | null;
  /** Harness slice: keep only sessions owned by this agent runtime. */
  harness?: HarnessFilter;
  /** Snapshot clock for the custom `from` date approximation. */
  nowMs?: number;
}

/** Harness slice for the fleet filter (Wave 5). `"all"` keeps both. */
export type HarnessFilter = "all" | Harness;

export const HARNESS_FILTERS: readonly HarnessFilter[] = [
  "all",
  "opencode",
  "pi",
] as const;

export interface HarnessCounts {
  opencode: number;
  pi: number;
}

/** Per-harness counts over a row set (one pass; unknown → opencode). */
export function countByHarness(
  rows: readonly SessionRow[],
): HarnessCounts {
  const counts: HarnessCounts = { opencode: 0, pi: 0 };
  for (const row of rows) {
    if (row.harness === "pi") counts.pi++;
    else counts.opencode++;
  }
  return counts;
}

/**
 * Single-pass fleet filter: search query + status chip + live-only toggle +
 * idle bucket + date window + project directory compose (AND). Cheap at
 * 1963 rows — one pass, no per-row allocation.
 */
export function applyFleetFilters(
  rows: readonly SessionRow[],
  query: string,
  status: StatusFilter,
  liveOnly: boolean,
  options: FleetFilterOptions = {},
): SessionRow[] {
  const needle = query.trim().toLowerCase();
  const bucket = options.bucket ?? "all";
  const date = options.date ?? DEFAULT_DATE_FILTER;
  const directory = options.directory ?? null;
  const harness: HarnessFilter = options.harness ?? "all";
  const nowMs = options.nowMs ?? Date.now();
  return rows.filter((row) => {
    if (directory !== null && row.directory !== directory) return false;
    if (harness !== "all" && (row.harness ?? "opencode") !== harness)
      return false;
    if (status !== "all" && row.status !== status) return false;
    if (liveOnly && !isLiveStatus(row.status)) return false;
    if (!matchesIdleBucket(row, bucket)) return false;
    if (!matchesDateFilter(row.updated_relative_ms, date, nowMs)) return false;
    if (needle === "") return true;
    return [row.id, row.title, row.project, row.directory, row.model].some(
      (field) => field.toLowerCase().includes(needle),
    );
  });
}

export const PAGE_SIZE = 50;

export interface PageSlice<T> {
  rows: T[];
  page: number;
  totalPages: number;
  total: number;
}

/** Clamp a 1-based page to the filtered result set and slice one page. */
export function paginateRows<T>(
  rows: readonly T[],
  page: number,
  pageSize = PAGE_SIZE,
): PageSlice<T> {
  const total = rows.length;
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const requested = Number.isFinite(page) ? Math.trunc(page) : 1;
  const safePage = Math.min(Math.max(1, requested), totalPages);
  const start = (safePage - 1) * pageSize;
  return {
    rows: rows.slice(start, start + pageSize),
    page: safePage,
    totalPages,
    total,
  };
}
