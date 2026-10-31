import { describe, expect, it } from "vitest";

import {
  LONG_RUN_MS,
  createAlertTracker,
  detectAlerts,
  localDay,
} from "./alerts";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";
import type { Snapshot } from "./sse";

function snapWith(
  rows: ReturnType<typeof makeRow>[],
  costToday = 0,
): Snapshot {
  return {
    ...buildFixtureSnapshot({ sessions: rows }),
    kpis: { ...buildFixtureSnapshot().kpis, cost_today: costToday },
  };
}

describe("detectAlerts", () => {
  it("stays silent on the first snapshot (baseline, not news)", () => {
    const tracker = createAlertTracker();
    const tick = snapWith(
      [makeRow({ id: "w", status: "waiting-input" })],
      99,
    );
    expect(
      detectAlerts(null, tick, Date.now(), tracker, {
        longRunMs: LONG_RUN_MS,
        budget: 1,
      }),
    ).toEqual([]);
    // ...but the baseline is seeded, so the next identical tick stays quiet.
    expect(
      detectAlerts(tick, tick, Date.now(), tracker, {
        longRunMs: LONG_RUN_MS,
        budget: 1,
      }),
    ).toEqual([]);
  });

  it("fires once per waiting spell and re-arms when it clears", () => {
    const tracker = createAlertTracker();
    const idle = snapWith([makeRow({ id: "s", status: "idle" })]);
    const waiting = snapWith([makeRow({ id: "s", status: "waiting-input" })]);
    const opts = { longRunMs: LONG_RUN_MS, budget: null };
    expect(detectAlerts(idle, idle, 1_000, tracker, opts)).toEqual([]);
    const first = detectAlerts(idle, waiting, 2_000, tracker, opts);
    expect(first).toHaveLength(1);
    expect(first[0]?.kind).toBe("waiting");
    // Still waiting one tick later: no repeat.
    expect(detectAlerts(waiting, waiting, 3_000, tracker, opts)).toEqual([]);
    // Spell ends (idle again): re-arms for the next spell.
    expect(detectAlerts(waiting, idle, 4_000, tracker, opts)).toEqual([]);
    const second = detectAlerts(idle, waiting, 5_000, tracker, opts);
    expect(second).toHaveLength(1);
  });

  it("nudges a turn generating past the threshold, once", () => {
    const tracker = createAlertTracker();
    const running = snapWith([makeRow({ id: "r", status: "running" })]);
    const idle = snapWith([makeRow({ id: "r", status: "idle" })]);
    const opts = { longRunMs: 60_000, budget: null };
    expect(detectAlerts(null, running, 0, tracker, opts)).toEqual([]);
    // 59s in: quiet. 61s: one nudge. Later: quiet.
    expect(detectAlerts(running, running, 59_000, tracker, opts)).toEqual([]);
    const fired = detectAlerts(running, running, 61_000, tracker, opts);
    expect(fired).toHaveLength(1);
    expect(fired[0]?.kind).toBe("long-run");
    expect(detectAlerts(running, running, 500_000, tracker, opts)).toEqual([]);
    // Completing the turn resets the clock for the next run.
    expect(detectAlerts(running, idle, 600_000, tracker, opts)).toEqual([]);
    expect(
      detectAlerts(idle, running, 610_000, tracker, opts),
    ).toEqual([]);
  });

  it("crosses the daily budget once per local day", () => {
    const tracker = createAlertTracker();
    const t0 = new Date(2026, 9, 24, 12, 0, 0).getTime();
    const t1 = t0 + 3_600_000;
    const under = snapWith([], 4.99);
    const over = snapWith([], 5.01);
    const opts = { longRunMs: LONG_RUN_MS, budget: 5 };
    expect(localDay(t0)).toBe("2026-10-24");
    expect(detectAlerts(under, under, t0, tracker, opts)).toEqual([]);
    const fired = detectAlerts(under, over, t1, tracker, opts);
    expect(fired).toHaveLength(1);
    expect(fired[0]?.kind).toBe("budget");
    // Same day, still over: quiet.
    expect(detectAlerts(over, over, t1 + 1000, tracker, opts)).toEqual([]);
  });

  it("ignores a disabled budget", () => {
    const tracker = createAlertTracker();
    const tick = snapWith([], 9999);
    expect(
      detectAlerts(tick, tick, Date.now(), tracker, {
        longRunMs: LONG_RUN_MS,
        budget: null,
      }),
    ).toEqual([]);
  });
});
