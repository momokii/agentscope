import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

afterEach(() => {
  cleanup();
  window.location.hash = "#/";
});

import SessionDetail from "./SessionDetail";
import {
  collectToolRuns,
  CONTEXT_TOKENS,
  retryCount,
  TimelineTab,
} from "./SessionDetailTabs";
import type { DetailExtraProps, ToolRun } from "./SessionDetailTabs";
import { buildTurns, flattenTurns, WINDOW_MAX_ROWS, zeroTokens } from "../lib/transcript";
import { formatUSD } from "../lib/format";
import {
  FIXTURE_MAIN_ID,
  fixtureProvider,
} from "../testutils/sessionDetail.fixture";

function mainData() {
  const data = fixtureProvider.getSessionDetail(FIXTURE_MAIN_ID);
  if (data === null) throw new Error("fixture missing");
  return data;
}

function extraProps(overrides: Partial<DetailExtraProps> = {}): DetailExtraProps {
  const data = mainData();
  const flat = flattenTurns(data.detail, data.children);
  return {
    flat,
    turns: buildTurns(flat),
    runs: collectToolRuns(flat),
    data,
    sessionId: FIXTURE_MAIN_ID,
    messageId: null,
    selectedRun: null,
    paletteOpen: false,
    onSelectRun: () => undefined,
    onCloseInspector: () => undefined,
    ...overrides,
  };
}

function barWidth(container: HTMLElement, runId: string): number {
  const bar = container.querySelector(`[data-testid="tl-bar-${runId}"]`);
  if (bar === null) throw new Error(`missing bar ${runId}`);
  const width = (bar as HTMLElement).style.width;
  return parseFloat(width.replace("%", ""));
}

describe("collectToolRuns", () => {
  it("collects assistant tool calls in time order with parent tokens", () => {
    const data = mainData();
    const runs = collectToolRuns(flattenTurns(data.detail, data.children));
    // a1:2 + a2:1 + ca1:1 + da1:2 + a4:5 = 11
    expect(runs).toHaveLength(11);
    for (let i = 1; i < runs.length; i += 1) {
      expect(runs[i].startMs).toBeGreaterThanOrEqual(runs[i - 1].startMs);
    }
    const bash12 = runs.find((r) => r.runId === "prt_fix_a4bash1");
    expect(bash12?.durationMs).toBe(12_000);
    expect(bash12?.turnTokens.total).toBe(3120);
    expect(retryCount(bash12 as ToolRun, runs)).toBe(1);
  });
});

describe("timeline tab", () => {
  it("renders bars with widths proportional to duration", () => {
    const props = extraProps();
    const { container } = render(<TimelineTab {...props} />);
    const w12 = barWidth(container, "prt_fix_a4bash1");
    const w8 = barWidth(container, "prt_fix_a4bash2");
    expect(w12 / w8).toBeCloseTo(12 / 8, 2);
    expect(container.querySelector("animate")).toBeNull();
  });

  it("nests subagent lanes under parent turns", () => {
    const props = extraProps();
    const { container } = render(<TimelineTab {...props} />);
    expect(container.textContent).toContain("↳ sub · explore");
    expect(container.textContent).toContain("↳ sub · plan");
    expect(container.querySelectorAll(".tl-run-sub").length).toBeGreaterThan(0);
  });

  it("shows an empty state with no runs", () => {
    const { container } = render(<TimelineTab {...extraProps({ runs: [] })} />);
    expect(container.textContent).toContain("No tool calls in this session yet.");
  });

  it("virtualizes a 1500-run stress fixture", () => {
    const runs: ToolRun[] = [];
    for (let i = 0; i < 1500; i += 1) {
      runs.push({
        runId: `run_stress_${i}`,
        callId: `call_stress_${i}`,
        name: "bash",
        status: "completed",
        title: "",
        input: undefined,
        output: "",
        startMs: 1_700_000_000_000 + i * 1000,
        endMs: 1_700_000_000_500 + i * 1000,
        durationMs: 500,
        lane: "main",
        agent: "build",
        sessionId: FIXTURE_MAIN_ID,
        messageId: "msg_stress",
        turnTokens: zeroTokens(),
      });
    }
    const { container } = render(<TimelineTab {...extraProps({ flat: [], turns: [], runs })} />);
    const rendered = container.querySelectorAll(".tl-run").length;
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThanOrEqual(WINDOW_MAX_ROWS);
  });
});

describe("tools tab + inspector (shell)", () => {
  it("renders one row per tool run with top-5 blocks", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="tools" row={null} paletteOpen={false} />,
    );
    expect(container.querySelectorAll(".tools-row[role='row']").length).toBeGreaterThanOrEqual(11);
    const latency = container.querySelector('[aria-label="top latency"]');
    expect(latency?.querySelector(".tools-sum-detail")?.textContent).toBe("12.0s");
    const errors = container.querySelector('[aria-label="top errors"]');
    expect(errors?.textContent).toContain("bash");
  });

  it("opens the right-rail inspector with the clicked run", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="timeline" row={null} paletteOpen={false} />,
    );
    fireEvent.click(screen.getByTestId("tl-row-prt_fix_a4bash1"));
    const rail = screen.getByTestId("inspector-prt_fix_a4bash1");
    expect(rail).toBeDefined();
    expect(rail.textContent).toContain("npm test -- --run");
    expect(rail.textContent).toContain("FAIL src/views/SessionDetail.test.tsx");
    expect(rail.textContent).toContain("attempt 2");
    expect(container.querySelector(".inspector")).not.toBeNull();
    expect(container.querySelector(".detail-split")).not.toBeNull();
  });

  it("Esc closes the inspector and only the inspector", () => {
    render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="tools" row={null} paletteOpen={false} />,
    );
    fireEvent.click(screen.getByTestId("tool-row-prt_fix_a4bash1"));
    expect(screen.queryByTestId("inspector-prt_fix_a4bash1")).not.toBeNull();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("inspector-prt_fix_a4bash1")).toBeNull();
  });

  it("shows an error block for failed runs", () => {
    render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="tools" row={null} paletteOpen={false} />,
    );
    fireEvent.click(screen.getByTestId("tool-row-prt_fix_a2edit"));
    expect(screen.getByTestId("inspector-prt_fix_a2edit").textContent).toContain(
      "oldString not found in content",
    );
  });
});

describe("cost tab", () => {
  it("pins cumulative math to the fixture info blocks", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="cost" row={null} paletteOpen={false} />,
    );
    const expected = formatUSD(0.0214 + 0.0021 + 0.0011);
    expect(container.querySelector("[data-testid='cost-total']")?.textContent).toBe(expected);
    expect(container.querySelector("[data-testid='rollup-total-cost']")?.textContent).toBe(expected);
    expect(screen.getByTestId("cost-context").textContent).toContain("10.2k");
    expect(CONTEXT_TOKENS).toBe(200_000);
  });

  it("donut shares sum to 100% across two models", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="cost" row={null} paletteOpen={false} />,
    );
    const slices = container.querySelectorAll("[data-testid^='donut-pct-']");
    expect(slices.length).toBe(2);
    let sum = 0;
    slices.forEach((el) => {
      sum += parseFloat(el.getAttribute("data-pct") ?? "0");
    });
    expect(sum).toBeCloseTo(100, 1);
    expect(container.textContent).toContain("toy-provider/toy-model-mini");
  });

  it("rolls up one row per session with info-block totals", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="cost" row={null} paletteOpen={false} />,
    );
    expect(container.querySelector("[data-testid='rollup-ses_fixture_main01']")).not.toBeNull();
    expect(container.querySelector("[data-testid='rollup-ses_fixture_sub01']")).not.toBeNull();
    expect(container.querySelector("[data-testid='rollup-ses_fixture_sub02']")).not.toBeNull();
    expect(container.querySelector("[data-testid='rollup-total-tokens']")?.textContent).toBe("10.2k");
  });
});

describe("raw tab", () => {
  it("collapses sections and exposes deep-link anchors plus copy", () => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="raw" row={null} paletteOpen={false} />,
    );
    expect(screen.getByTestId("raw-pre-msg_fix_a1")).toBeDefined();
    fireEvent.click(screen.getByTestId("raw-toggle-msg_fix_a1"));
    expect(screen.queryByTestId("raw-pre-msg_fix_a1")).toBeNull();
    fireEvent.click(screen.getByTestId("raw-toggle-msg_fix_a1"));
    expect(screen.getByTestId("raw-pre-msg_fix_a1")).toBeDefined();
    expect(
      screen.getByTestId("raw-anchor-msg_fix_a1").getAttribute("href"),
    ).toBe(`#/sessions/${FIXTURE_MAIN_ID}/raw?m=msg_fix_a1`);
    fireEvent.click(screen.getByTestId("raw-copy-msg_fix_a1"));
    expect(screen.getByTestId("raw-copy-msg_fix_a1").textContent).toContain("copied");
    expect(container.textContent).toContain("msg_fix_da1");
  });

  it("highlights the ?m= message target", () => {
    render(
      <SessionDetail
        id={FIXTURE_MAIN_ID}
        tab="raw"
        row={null}
        paletteOpen={false}
        messageId="msg_fix_a2"
      />,
    );
    expect(screen.getByTestId("raw-msg-msg_fix_a2").className).toContain("raw-target");
  });
});

describe("data coverage (all tabs read the same fixture)", () => {
  const tabs = ["timeline", "tools", "cost", "raw"] as const;
  const probes: Record<(typeof tabs)[number], string> = {
    timeline: "↳ sub · plan",
    tools: "prt_fix_a4bash1",
    cost: "toy-model-mini",
    raw: "msg_fix_ca1",
  };
  it.each(tabs)("tab %s renders ≥1 element from the shared fixture", (tab) => {
    const { container } = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab={tab} row={null} paletteOpen={false} />,
    );
    if (tab === "tools") {
      expect(container.querySelector(`[data-testid='tool-row-${probes[tab]}']`)).not.toBeNull();
    } else {
      expect(container.textContent).toContain(probes[tab]);
    }
  });
});

describe("tab switching preserves transcript scroll state", () => {
  it("keeps the transcript node (and scrollTop) across tab switches", () => {
    const view = render(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    const scroller = view.container.querySelector(".transcript") as HTMLElement;
    expect(scroller).not.toBeNull();
    scroller.scrollTop = 222;
    view.rerender(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="tools" row={null} paletteOpen={false} />,
    );
    view.rerender(
      <SessionDetail id={FIXTURE_MAIN_ID} tab="transcript" row={null} paletteOpen={false} />,
    );
    const again = view.container.querySelector(".transcript") as HTMLElement;
    expect(again).toBe(scroller);
    expect(again.scrollTop).toBe(222);
    expect(again.textContent).toContain("Add a transcript view with tail follow");
  });
});
