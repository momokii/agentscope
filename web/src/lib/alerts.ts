/**
 * Client-side alert detection (Wave 6): waiting-input flips, long-running
 * turns, and daily budget crosses — computed per applied snapshot, no
 * backend change, read-only posture intact.
 *
 * Pure except the clock (nowMs is a parameter) and the caller-owned
 * tracker: all memory lives in one AlertTracker the App holds in a ref.
 */

import type { Snapshot } from "./sse";

/** Long-running threshold: a turn generating this long earns one nudge. */
export const LONG_RUN_MS = 15 * 60 * 1000;

export type AlertKind = "waiting" | "long-run" | "budget";

export interface AlertEvent {
  kind: AlertKind;
  /** Owning session (absent for fleet-wide budget alerts). */
  sessionId?: string;
  title: string;
  body: string;
}

/**
 * Dedup memory. waitingNotified holds session ids already nudged for the
 * current waiting spell (cleared when the session leaves waiting-input, so
 * a later spell re-arms). longRunSince records when each running session
 * was first seen generating; longRunNotified marks the fired ones.
 * budgetDay is the local YYYY-MM-DD of the last budget alert.
 */
export interface AlertTracker {
  waitingNotified: Set<string>;
  longRunSince: Map<string, number>;
  longRunNotified: Set<string>;
  budgetDay: string | null;
}

export function createAlertTracker(): AlertTracker {
  return {
    waitingNotified: new Set(),
    longRunSince: new Map(),
    longRunNotified: new Set(),
    budgetDay: null,
  };
}

export interface AlertOptions {
  /** Per-turn long-run threshold ms. */
  longRunMs: number;
  /** Daily budget in USD, or null when disabled. */
  budget: number | null;
}

/** Local YYYY-MM-DD for day-bucket comparisons (same wall clock as cost_today's midnight). */
export function localDay(nowMs: number): string {
  const d = new Date(nowMs);
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

function rowById(
  snap: Snapshot | null,
  id: string,
): { status: string } | undefined {
  return snap?.sessions.find((r) => r.id === id);
}

/**
 * Diff one snapshot pair into alert events. Order: waiting flips, then
 * long runs, then budget (at most one budget alert per call — it either
 * crossed today or it didn't).
 */
export function detectAlerts(
  prev: Snapshot | null,
  next: Snapshot,
  nowMs: number,
  tracker: AlertTracker,
  options: AlertOptions,
): AlertEvent[] {
  const out: AlertEvent[] = [];
  if (prev === null) {
    // First snapshot: seed dedup memory silently. Alerts report changes
    // the operator hasn't seen; everything present at load is baseline,
    // not news (same no-flood rule as the activity ring).
    for (const row of next.sessions) {
      if (row.status === "waiting-input") tracker.waitingNotified.add(row.id);
      if (row.status === "running") tracker.longRunSince.set(row.id, nowMs);
    }
    if (
      options.budget !== null &&
      next.kpis.cost_today >= options.budget
    ) {
      tracker.budgetDay = localDay(nowMs);
    }
    return out;
  }

  for (const row of next.sessions) {
    const was = rowById(prev, row.id)?.status;
    if (row.status === "waiting-input") {
      if (was !== "waiting-input" && !tracker.waitingNotified.has(row.id)) {
        tracker.waitingNotified.add(row.id);
        out.push({
          kind: "waiting",
          sessionId: row.id,
          title: `Input needed: ${row.title}`,
          body: `${row.project} · waiting on you — nothing is generating.`,
        });
      }
      tracker.longRunSince.delete(row.id);
      tracker.longRunNotified.delete(row.id);
    } else {
      // Spell over: clear so a later waiting spell re-arms the nudge.
      tracker.waitingNotified.delete(row.id);
      if (row.status === "running") {
        if (!tracker.longRunSince.has(row.id)) {
          tracker.longRunSince.set(row.id, nowMs);
        }
        const since = tracker.longRunSince.get(row.id) ?? nowMs;
        if (
          nowMs - since >= options.longRunMs &&
          !tracker.longRunNotified.has(row.id)
        ) {
          tracker.longRunNotified.add(row.id);
          out.push({
            kind: "long-run",
            sessionId: row.id,
            title: `Long turn: ${row.title}`,
            body: `${row.project} · generating for ${Math.round((nowMs - since) / 60000)}+ min — check whether it's stuck.`,
          });
        }
      } else {
        tracker.longRunSince.delete(row.id);
        tracker.longRunNotified.delete(row.id);
      }
    }
  }
  // Drop memory for sessions that vanished from the fleet.
  const alive = new Set(next.sessions.map((r) => r.id));
  for (const id of [...tracker.waitingNotified]) {
    if (!alive.has(id)) tracker.waitingNotified.delete(id);
  }
  for (const id of [...tracker.longRunSince.keys()]) {
    if (!alive.has(id)) {
      tracker.longRunSince.delete(id);
      tracker.longRunNotified.delete(id);
    }
  }

  if (
    options.budget !== null &&
    Number.isFinite(options.budget) &&
    options.budget > 0 &&
    next.kpis.cost_today >= options.budget
  ) {
    const day = localDay(nowMs);
    if (tracker.budgetDay !== day) {
      tracker.budgetDay = day;
      out.push({
        kind: "budget",
        title: "Daily budget crossed",
        body: `Today's spend hit $${next.kpis.cost_today.toFixed(2)} (budget $${options.budget.toFixed(2)}).`,
      });
    }
  }
  return out;
}
