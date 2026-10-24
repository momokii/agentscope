import { describe, expect, it } from "vitest";

import { overlaySeries, swimlanes } from "./compare";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";

describe("overlaySeries", () => {
  it("shares one time base across mismatched spark lengths", () => {
    const short = makeRow({ id: "ses_short", spark: [5] });
    const long = makeRow({
      id: "ses_long",
      spark: [1, 2, 3, 4],
      tokens: { in: 6, out: 4, total: 10 },
      cost: 1,
    });
    const lines = overlaySeries([short, long]);
    expect(lines).toHaveLength(2);
    // Shared base = longest spark (4 buckets), right-aligned.
    for (const line of lines) expect(line.points).toHaveLength(4);
    const longLine = lines.find((l) => l.id === "ses_long");
    expect(longLine).toBeDefined();
    // Final point lands exactly on the row totals (scaled cumulative).
    const last = longLine!.points[longLine!.points.length - 1]!;
    expect(last.tokens).toBeCloseTo(10, 9);
    expect(last.cost).toBeCloseTo(1, 9);
    // Monotone non-decreasing (cumulative).
    const toks = longLine!.points.map((p) => p.tokens);
    expect([...toks].sort((a, b) => a - b)).toEqual(toks);
    // Short spark is zero-padded on the left: first 3 buckets are flat 0.
    const shortLine = lines.find((l) => l.id === "ses_short")!;
    expect(shortLine.points.slice(0, 3).map((p) => p.tokens)).toEqual([
      0, 0, 0,
    ]);
  });

  it("covers the empty-session edge with a single honest point", () => {
    const empty = makeRow({ id: "ses_empty", spark: [] });
    const [line] = overlaySeries([empty]);
    expect(line!.points).toEqual([{ t: 0, cost: 0, tokens: 0 }]);
    expect(line!.totalCost).toBe(empty.cost);
    expect(line!.totalTokens).toBe(empty.tokens.total);
  });

  it("covers the single-point edge without NaN", () => {
    const one = makeRow({
      id: "ses_one",
      spark: [7],
      tokens: { in: 70, out: 30, total: 100 },
      cost: 0.5,
    });
    const [line] = overlaySeries([one]);
    expect(line!.points).toHaveLength(1);
    expect(line!.points[0]).toEqual({ t: 0, cost: 0.5, tokens: 100 });
    for (const p of line!.points) {
      expect(Number.isFinite(p.cost)).toBe(true);
      expect(Number.isFinite(p.tokens)).toBe(true);
    }
  });

  it("returns no lines for zero sessions", () => {
    expect(overlaySeries([])).toEqual([]);
  });
});

describe("swimlanes", () => {
  it("orders running→exited reusing the fleet contract", () => {
    const lanes = swimlanes(buildFixtureSnapshot().sessions);
    expect(lanes.map((l) => l.id)).toEqual([
      "ses_running1",
      "ses_waiting1",
      "ses_idle1",
      "ses_error1",
      "ses_archived1",
    ]);
  });

  it("sinks exited panes within the same status", () => {
    const live = makeRow({ id: "ses_live", status: "idle" });
    const gone = makeRow({
      id: "ses_gone",
      status: "idle",
      pane: { cwd: "", exited: true },
    });
    const lanes = swimlanes([gone, live]);
    expect(lanes.map((l) => l.id)).toEqual(["ses_live", "ses_gone"]);
    expect(lanes[1]!.exited).toBe(true);
  });

  it("nests subagent placeholder lanes under parents", () => {
    const lanes = swimlanes(buildFixtureSnapshot().sessions);
    const parent = lanes.find((l) => l.id === "ses_running1")!;
    expect(parent.children).toHaveLength(2);
    expect(parent.children[0]!.id).toBe("ses_running1~sub1");
    const leaf = lanes.find((l) => l.id === "ses_idle1")!;
    expect(leaf.children).toEqual([]);
  });

  it("maps nonzero buckets to dots and width-proportional bars", () => {
    const [lane] = swimlanes([
      makeRow({ id: "ses_x", spark: [0, 4, 2] }),
    ]);
    expect(lane!.dots.map((d) => d.t)).toEqual([1, 2]);
    expect(lane!.bars).toHaveLength(3);
    expect(lane!.bars[1]!.width).toBeCloseTo(1, 9);
    expect(lane!.bars[2]!.width).toBeCloseTo(0.5, 9);
    expect(lane!.tokenRate).toEqual([0, 4, 2]);
  });

  it("covers a session with no messages (empty spark)", () => {
    const [lane] = swimlanes([makeRow({ id: "ses_quiet", spark: [] })]);
    expect(lane!.dots).toEqual([]);
    expect(lane!.bars).toEqual([]);
    expect(lane!.tokenRate).toEqual([]);
  });

  it("returns no lanes for zero sessions", () => {
    expect(swimlanes([])).toEqual([]);
  });
});
