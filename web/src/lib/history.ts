/**
 * History API client (V2-B): GET /api/history?range=7d|30d.
 * Mirrors internal/history.History exactly (snake_case wire names).
 * Unknown/absent shapes throw — the view renders its honest empty state.
 */

export type HistoryRange = "7d" | "30d";

export const HISTORY_RANGES: readonly HistoryRange[] = ["7d", "30d"];

export interface HistoryPoint {
  ts: number;
  tokens_24h: number;
  cost_24h: number;
  tokens_7d?: number;
  cost_7d?: number;
  tokens_30d?: number;
  cost_30d?: number;
  active?: number;
  waiting?: number;
  errors?: number;
  live_procs?: number;
  live_panes?: number;
  sessions?: number;
  running?: number;
  directory?: string;
}

export interface HistoryProject {
  directory: string;
  sessions: number;
  running: number;
  cost_24h: number;
  cost_7d: number;
  cost_30d: number;
  spark: HistoryPoint[];
}

export interface HistoryResponse {
  from: number;
  to: number;
  points: HistoryPoint[];
  projects: HistoryProject[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export async function fetchHistory(range: HistoryRange): Promise<HistoryResponse> {
  const response = await fetch(`/api/history?range=${range}`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    if (response.status === 503) throw new HistoryUnavailable();
    throw new Error(`history fetch failed: ${response.status}`);
  }
  const payload: unknown = await response.json();
  if (!isRecord(payload) || !Array.isArray(payload["points"]) || !Array.isArray(payload["projects"])) {
    throw new Error("history: malformed payload");
  }
  return payload as unknown as HistoryResponse;
}

/** History unavailable (503): backend store disabled or unwired. */
export class HistoryUnavailable extends Error {
  constructor() {
    super("history unavailable");
  }
}

/** Short local day label for axis ticks ("Oct 27", "7:42p" for sub-day). */
export function formatDay(ts: number): string {
  const d = new Date(ts * 1000);
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  return `${months[d.getMonth()]} ${d.getDate()}`;
}
