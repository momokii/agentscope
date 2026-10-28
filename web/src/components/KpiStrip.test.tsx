import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import KpiStrip from "./KpiStrip";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

describe("KpiStrip", () => {
  it("renders all six stat values", () => {
    const { kpis } = buildFixtureSnapshot();
    render(<KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={42} />);
    expect(screen.getByText("Active")).toBeDefined();
    expect(screen.getByText("Waiting input")).toBeDefined();
    expect(screen.getByText("Tokens/hr")).toBeDefined();
    expect(screen.getByText("Cost today")).toBeDefined();
    expect(screen.getByText("Errors")).toBeDefined();
    expect(screen.getByText("SSE stream")).toBeDefined();

    // active, waiting-input, and errors fixtures are all 1.
    expect(screen.getAllByText("1")).toHaveLength(3);
    expect(screen.getByText("12.5k")).toBeDefined(); // tokens/hr compact
    expect(screen.getByText("$12.40")).toBeDefined(); // cost today
    expect(screen.getByText("LIVE")).toBeDefined(); // SSE value
    expect(screen.getByText("seq 42")).toBeDefined(); // SSE delta line
  });

  it("renders neutral ±0 deltas without a previous tick", () => {
    const { kpis } = buildFixtureSnapshot();
    render(<KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={42} />);
    expect(screen.getAllByText("±0").length).toBe(5);
  });

  it("renders directional deltas against the previous tick", () => {
    const { kpis } = buildFixtureSnapshot();
    const prev = {
      ...kpis,
      active: 0,
      waiting_input: 0,
      tokens_per_hour: 10000,
      cost_today: 12.0,
      errors: 0,
    };
    render(<KpiStrip kpis={kpis} prevKpis={prev} phase="connected" lastSeq={43} />);
    // active, waiting-input, and errors each rise by exactly 1.
    expect(screen.getAllByText("+1")).toHaveLength(3);
    expect(screen.getByText("+2.5k/h")).toBeDefined(); // tokens/hr up
    expect(screen.getByText("+$0.40")).toBeDefined(); // cost up
  });

  it("ticks metrics age live from the frame age instead of freezing", () => {
    const { kpis } = buildFixtureSnapshot();
    const withAge = { ...kpis, metrics_age_ms: 4000 };
    const { rerender } = render(
      <KpiStrip kpis={withAge} prevKpis={null} phase="connected" lastSeq={42} />,
    );
    expect(screen.getByText("4s")).toBeDefined();
    // 30s after the frame arrived, the same snapshot reads 34s.
    rerender(
      <KpiStrip
        kpis={withAge}
        prevKpis={null}
        phase="connected"
        lastSeq={42}
        frameAgeMs={30000}
      />,
    );
    expect(screen.getByText("34s")).toBeDefined();
  });

  it("switches the cost card to the trailing window with lifetime fallback", () => {
    const { kpis } = buildFixtureSnapshot();
    const windowed = { ...kpis, cost_24h: 1.5, cost_7d: 4.25 };
    const { rerender } = render(
      <KpiStrip kpis={windowed} prevKpis={null} phase="connected" lastSeq={1} costMode="24h" />,
    );
    expect(screen.getByText("Cost 24h")).toBeDefined();
    expect(screen.getByText("$1.50")).toBeDefined();
    // Snapshots that predate windowed metrics fall back to the (also
    // absent) lifetime total — honest unknown, never zero.
    rerender(
      <KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={1} costMode="7d" />,
    );
    expect(screen.getByText("Cost 7d")).toBeDefined();
    expect(screen.getAllByText("—").length).toBeGreaterThan(0);
  });

  it("folds the today preset into the windowed cost card", () => {
    const { kpis } = buildFixtureSnapshot();
    const { container } = render(
      <KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={1} costMode="today" />,
    );
    const labels = Array.from(container.querySelectorAll(".kpi-label")).map(
      (el) => el.textContent,
    );
    expect(labels.filter((t) => t === "Cost today")).toHaveLength(1);
    expect(screen.getByText("$12.40")).toBeDefined();
  });

  it("labels the SSE dot state per connection phase", () => {
    const { kpis } = buildFixtureSnapshot();
    const { rerender } = render(
      <KpiStrip kpis={kpis} prevKpis={null} phase="reconnecting" lastSeq={42} />,
    );
    expect(screen.getByText("RETRY")).toBeDefined();
    expect(screen.getByText("reconnecting…")).toBeDefined();
    rerender(
      <KpiStrip kpis={kpis} prevKpis={null} phase="closed" lastSeq={42} />,
    );
    expect(screen.getByText("OFF")).toBeDefined();
    expect(screen.getByText("stream paused")).toBeDefined();
  });
});
