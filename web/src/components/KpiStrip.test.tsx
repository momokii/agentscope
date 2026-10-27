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
