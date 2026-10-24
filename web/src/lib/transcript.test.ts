import { describe, expect, it } from "vitest";

import {
  buildTurns,
  computeWindow,
  firstText,
  flattenTurns,
  isNearBottom,
  messageCost,
  messageDurationMs,
  messageTokens,
  rollUpTokens,
  summarizeTurn,
  truncateWithEllipsis,
  zeroTokens,
  TAIL_PIN_THRESHOLD_PX,
  WINDOW_MAX_ROWS,
} from "./transcript";
import type { ExportMessage, ExportSession } from "./transcript";
import {
  FIXTURE_MAIN_ID,
  FIXTURE_SUB2_ID,
  FIXTURE_SUB_ID,
  fixtureProvider,
} from "../testutils/sessionDetail.fixture";

function mainData() {
  const data = fixtureProvider.getSessionDetail(FIXTURE_MAIN_ID);
  if (data === null) throw new Error("fixture missing main session");
  return data;
}

describe("flattenTurns", () => {
  it("interleaves main + subagent turns chronologically with lane attribution", () => {
    const { detail, children } = mainData();
    const flat = flattenTurns(detail, children);
    expect(flat).toHaveLength(11);
    expect(flat.map((f) => f.message.info.id)).toEqual([
      "msg_fix_u1",
      "msg_fix_a1",
      "msg_fix_a2",
      "msg_fix_cu1",
      "msg_fix_ca1",
      "msg_fix_u2",
      "msg_fix_a3",
      "msg_fix_u3",
      "msg_fix_du1",
      "msg_fix_da1",
      "msg_fix_a4",
    ]);
    const lanes = flat.map((f) => f.lane);
    expect(lanes).toEqual([
      "main",
      "main",
      "main",
      "sub",
      "sub",
      "main",
      "main",
      "main",
      "sub",
      "sub",
      "main",
    ]);
    const agents = flat.map((f) => f.agent);
    expect(agents).toEqual([
      "build",
      "build",
      "build",
      "explore",
      "explore",
      "build",
      "build",
      "build",
      "plan",
      "plan",
      "build",
    ]);
  });

  it("is stable on timestamp ties (main first)", () => {
    const { detail } = mainData();
    const twin: ExportSession = {
      info: { ...detail.info, id: "ses_twin", parentID: detail.info.id },
      messages: [
        {
          info: {
            id: "msg_twin",
            sessionID: "ses_twin",
            role: "user",
            time: { created: detail.messages[0].info.time.created },
          },
          parts: [],
        },
      ],
    };
    const flat = flattenTurns(detail, [twin]);
    expect(flat[0].message.info.id).toBe("msg_fix_u1");
    expect(flat[1].message.info.id).toBe("msg_twin");
  });
});

describe("buildTurns + summarizeTurn", () => {
  it("makes one turn per assistant message with the preceding user prompt", () => {
    const { detail, children } = mainData();
    const turns = buildTurns(flattenTurns(detail, children));
    expect(turns.map((t) => t.id)).toEqual([
      "msg_fix_a1",
      "msg_fix_a2",
      "msg_fix_ca1",
      "msg_fix_a3",
      "msg_fix_da1",
      "msg_fix_a4",
    ]);
    expect(turns[0].prompt?.info.id).toBe("msg_fix_u1");
    expect(turns[2].prompt?.info.id).toBe("msg_fix_cu1");
    expect(turns[2].lane).toBe("sub");
    expect(turns[2].agent).toBe("explore");
    expect(turns[4].prompt?.info.id).toBe("msg_fix_du1");
    expect(turns[4].lane).toBe("sub");
    expect(turns[4].agent).toBe("plan");
    expect(turns[5].prompt).toBeNull();
  });

  it("summarizes input, tools, tokens, duration, cost per turn", () => {
    const { detail, children } = mainData();
    const turns = buildTurns(flattenTurns(detail, children));
    const first = summarizeTurn(turns[0]);
    expect(first.inputSummary).toBe(
      "Add a transcript view with tail follow and pinned history for long sessions.",
    );
    expect(first.toolCount).toBe(2);
    expect(first.toolNames).toEqual(["read", "bash"]);
    expect(first.tokens).toEqual({
      input: 1200,
      output: 200,
      reasoning: 100,
      cache: { read: 400, write: 0 },
      total: 1500,
    });
    expect(first.durationMs).toBe(35_000);
    expect(first.cost).toBeCloseTo(0.0042, 10);

    const errorTurn = summarizeTurn(turns[1]);
    expect(errorTurn.tools).toEqual([
      { name: "edit", status: "error" },
    ]);
    expect(errorTurn.durationMs).toBe(22_000);
  });

  it("truncates long prompts with an ellipsis marker", () => {
    const long = `x${"y".repeat(200)}`;
    expect(truncateWithEllipsis(long, 120)).toBe(`${`x${"y".repeat(119)}`}…`);
    expect(truncateWithEllipsis("short", 120)).toBe("short");
  });

  it("falls back to response text, then (no text)", () => {
    const { detail, children } = mainData();
    const turns = buildTurns(flattenTurns(detail, children));
    expect(summarizeTurn(turns[3]).inputSummary).toBe(
      "Also add per-turn cards with token math that link back to the transcript.",
    );
    const bare: ExportMessage = {
      info: {
        id: "m",
        sessionID: "s",
        role: "assistant",
        time: { created: 1 },
      },
      parts: [],
    };
    expect(firstText(bare)).toBe("");
    expect(firstText(null)).toBe("");
    expect(messageTokens(bare)).toEqual(zeroTokens());
    expect(messageCost(bare)).toBe(0);
    expect(messageDurationMs(bare)).toBe(0);
  });
});

describe("export parity (fixture info block)", () => {
  it("assistant token roll-up + cost equal the fixture info block per session", () => {
    for (const id of [FIXTURE_MAIN_ID, FIXTURE_SUB_ID, FIXTURE_SUB2_ID]) {
      const data = fixtureProvider.getSessionDetail(id);
      if (data === null) throw new Error(`fixture missing ${id}`);
      const assistant = data.detail.messages.filter(
        (m) => m.info.role === "assistant",
      );
      expect(assistant.length).toBeGreaterThan(0);
      expect(rollUpTokens(assistant.map(messageTokens))).toEqual(
        data.detail.info.tokens,
      );
      const cost = assistant.reduce((sum, m) => sum + messageCost(m), 0);
      expect(cost).toBeCloseTo(data.detail.info.cost, 10);
    }
  });

  it("message count equals the fixture's own messages", () => {
    const { detail, children } = mainData();
    const flat = flattenTurns(detail, children);
    const childTotal = children.reduce((sum, c) => sum + c.messages.length, 0);
    expect(flat).toHaveLength(detail.messages.length + childTotal);
    for (const child of children) {
      expect(child.info.parentID).toBe(detail.info.id);
    }
  });
});

describe("tail-follow + windowing math", () => {
  it("pins when scrolled more than 24px from the bottom", () => {
    expect(isNearBottom(1000, 900, 100)).toBe(true);
    expect(isNearBottom(1000, 900, 100, TAIL_PIN_THRESHOLD_PX)).toBe(true);
    expect(isNearBottom(1000, 800, 100)).toBe(false);
    expect(isNearBottom(1000, 876, 100)).toBe(true);
    expect(isNearBottom(1000, 875, 100)).toBe(false);
  });

  it("windows bound the DOM for 1500 rows and keep stable anchors", () => {
    const total = 1500;
    const viewport = 600;
    const head = computeWindow(0, total, viewport);
    expect(head.start).toBe(0);
    expect(head.end - head.start).toBeLessThanOrEqual(WINDOW_MAX_ROWS);
    const mid = computeWindow(90_000, total, viewport);
    expect(mid.start).toBeGreaterThan(0);
    expect(mid.end - mid.start).toBeLessThanOrEqual(WINDOW_MAX_ROWS);
    const tail = computeWindow(1_000_000, total, viewport);
    expect(tail.end).toBe(total);
    expect(computeWindow(0, 0, viewport)).toEqual({ start: 0, end: 0 });
  });
});
