import { describe, expect, it } from "vitest";

import {
  ACTIVITY_RING_CAP,
  diffSnapshots,
  prependEvents,
  scopeEvents,
} from "./activity";
import type { ActivityEvent } from "./activity";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";

describe("diffSnapshots", () => {
  it("returns [] on the first tick (no baseline to diff)", () => {
    const next = buildFixtureSnapshot();
    expect(diffSnapshots(null, next)).toEqual([]);
  });

  it("emits appeared/disappeared for session set changes", () => {
    const prev = buildFixtureSnapshot();
    const added = makeRow({ id: "ses_new1", status: "running" });
    const next = buildFixtureSnapshot({
      seq: 43,
      sessions: [...prev.sessions.slice(1), added],
    });
    const events = diffSnapshots(prev, next);
    expect(events.some((e) => e.kind === "appeared" && e.sessionId === "ses_new1")).toBe(true);
    expect(
      events.some(
        (e) => e.kind === "disappeared" && e.sessionId === "ses_running1",
      ),
    ).toBe(true);
  });

  it("emits old→new status transitions with waiting/error satellites", () => {
    const prev = buildFixtureSnapshot();
    const next = buildFixtureSnapshot({
      seq: 43,
      sessions: prev.sessions.map((row) =>
        row.id === "ses_idle1" ? { ...row, status: "waiting-input" as const } : row,
      ),
    });
    const events = diffSnapshots(prev, next);
    const status = events.find((e) => e.kind === "status");
    expect(status?.text).toContain("idle → waiting-input");
    expect(
      events.some(
        (e) => e.kind === "waiting-raised" && e.sessionId === "ses_idle1",
      ),
    ).toBe(true);
  });

  it("emits waiting-cleared when leaving waiting-input", () => {
    const prev = buildFixtureSnapshot();
    const next = buildFixtureSnapshot({
      seq: 43,
      sessions: prev.sessions.map((row) =>
        row.id === "ses_waiting1" ? { ...row, status: "idle" as const } : row,
      ),
    });
    const events = diffSnapshots(prev, next);
    expect(
      events.some(
        (e) => e.kind === "waiting-cleared" && e.sessionId === "ses_waiting1",
      ),
    ).toBe(true);
  });

  it("emits error + completed (running→idle with token delta)", () => {
    const prev = buildFixtureSnapshot();
    const running = prev.sessions.find((r) => r.id === "ses_running1")!;
    const waiting = prev.sessions.find((r) => r.id === "ses_waiting1")!;
    const next = buildFixtureSnapshot({
      seq: 43,
      sessions: prev.sessions.map((row) => {
        if (row.id === "ses_waiting1") return { ...row, status: "error" as const };
        if (row.id === "ses_running1")
          return {
            ...row,
            status: "idle" as const,
            tokens: { in: 9000, out: 3000, total: 24500 },
          };
        return row;
      }),
    });
    void running;
    void waiting;
    const events = diffSnapshots(prev, next);
    expect(
      events.some((e) => e.kind === "error" && e.sessionId === "ses_waiting1"),
    ).toBe(true);
    const completed = events.find((e) => e.kind === "completed");
    expect(completed?.sessionId).toBe("ses_running1");
    expect(completed?.text).toContain("running → idle");
    expect(completed?.text).toContain("tokens");
    // Session-level only: never fabricate tool names.
    for (const e of events) expect(e.text).not.toMatch(/tool/i);
  });

  it("caps movers at 3 and ignores sub-threshold deltas", () => {
    const prev = buildFixtureSnapshot();
    const next = buildFixtureSnapshot({
      seq: 43,
      sessions: prev.sessions.map((row, i) => ({
        ...row,
        tokens: {
          in: row.tokens.in,
          out: row.tokens.out,
          total: row.tokens.total + (i < 4 ? 5000 + i : 10),
        },
      })),
    });
    const events = diffSnapshots(prev, next);
    const movers = events.filter((e) => e.kind === "tokens-mover");
    expect(movers.length).toBeLessThanOrEqual(3);
    expect(movers.length).toBe(3);
  });

  it("prependEvents bounds the ring newest-first at the cap", () => {
    const prev = buildFixtureSnapshot({ seq: 42 });
    const base = buildFixtureSnapshot({ seq: 43 });
    const tick = {
      ...base,
      sessions: base.sessions.map((row) =>
        row.id === "ses_idle1" ? { ...row, status: "running" as const } : row,
      ),
    };
    const events = diffSnapshots(prev, tick);
    expect(events.length).toBeGreaterThan(0);
    let ring = prependEvents([], events);
    expect(ring[0]).toEqual(events[0]);
    const filler = Array.from({ length: ACTIVITY_RING_CAP + 10 }, (_, i) => ({
      kind: "status" as const,
      sessionId: `ses_fill${i}`,
      sessionTitle: `fill ${i}`,
      project: "fill-proj",
      directory: "/srv/fill",
      harness: "opencode" as const,
      text: `ses_fill${i} idle → running`,
      glyph: "●",
      seq: 100 + i,
    }));
    ring = prependEvents(ring, filler);
    expect(ring.length).toBe(ACTIVITY_RING_CAP);
    // Newest tick prepended whole: head is the tick's first event, tail cut.
    // (Mover collapse tested separately below — filler kinds are all status.)
    expect(ring[0]?.sessionId).toBe("ses_fill0");
    expect(ring[ACTIVITY_RING_CAP - 1]?.sessionId).toBe(
      `ses_fill${ACTIVITY_RING_CAP - 1}`,
    );
  });
});

describe("prependEvents mover collapse", () => {
  const mover = (id: string, seq: number): ActivityEvent => ({
    kind: "tokens-mover",
    sessionId: id,
    sessionTitle: id,
    project: "p",
    directory: "/srv/p",
    harness: "opencode",
    text: `${id} +1k tokens`,
    glyph: "+",
    seq,
  });
  const status = (id: string, seq: number): ActivityEvent => ({
    kind: "status",
    sessionId: id,
    sessionTitle: id,
    project: "p",
    directory: "/srv/p",
    harness: "opencode",
    text: `${id} idle → running`,
    glyph: "●",
    seq,
  });

  it("replaces a session's older mover instead of stacking it", () => {
    const ring = prependEvents([], [mover("a", 1), status("b", 1)]);
    const next = prependEvents(ring, [mover("a", 2)]);
    const movers = next.filter(
      (e) => e.kind === "tokens-mover" && e.sessionId === "a",
    );
    expect(movers).toHaveLength(1);
    expect(movers[0]?.seq).toBe(2);
    // Unrelated history survives the collapse.
    expect(next.some((e) => e.sessionId === "b")).toBe(true);
  });
});

describe("scopeEvents", () => {
  const ev = (id: string, dir: string): ActivityEvent => ({
    kind: "status",
    sessionId: id,
    sessionTitle: id,
    project: "p",
    directory: dir,
    harness: "opencode",
    text: id,
    glyph: "●",
    seq: 1,
  });
  const ring = [ev("a", "/x"), ev("b", "/y")];

  it("passes the whole ring without a scope", () => {
    expect(scopeEvents(ring, {})).toHaveLength(2);
  });

  it("filters project views by exact directory", () => {
    expect(scopeEvents(ring, { directory: "/x" }).map((e) => e.sessionId)).toEqual(["a"]);
  });

  it("filters session views by session id", () => {
    expect(scopeEvents(ring, { sessionId: "b" }).map((e) => e.sessionId)).toEqual(["b"]);
  });
});
