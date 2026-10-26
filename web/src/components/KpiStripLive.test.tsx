import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import KpiStrip from "./KpiStrip";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

describe("KpiStrip live runtime cards", () => {
  it("renders live procs and panes when the runtime reports them", () => {
    const { kpis } = buildFixtureSnapshot();
    render(
      <KpiStrip
        kpis={{ ...kpis, live_processes: 7, live_panes: 3 }}
        prevKpis={{ ...kpis, live_processes: 6, live_panes: 3 }}
        phase="connected"
        lastSeq={42}
      />,
    );
    expect(screen.getByText("Live procs")).toBeDefined();
    expect(screen.getByText("Live panes")).toBeDefined();
    expect(screen.getByText("7")).toBeDefined();
    expect(screen.getByText("3")).toBeDefined();
    expect(screen.getByText("+1")).toBeDefined();
  });

  it("degrades to an em dash with a reporting title when absent", () => {
    const { kpis } = buildFixtureSnapshot();
    const { container } = render(
      <KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={42} />,
    );
    const absent = screen.getAllByTitle("runtime not yet reporting");
    expect(absent).toHaveLength(2);
    for (const el of absent) {
      expect(el.textContent).toBe("—");
    }
    expect(container.querySelector(".kpi-strip")?.children).toHaveLength(10);
  });

  it("renders lifetime cost and metrics age with distinct labels", () => {
    const { kpis } = buildFixtureSnapshot();
    render(
      <KpiStrip
        kpis={{ ...kpis, cost_total: 48.2, metrics_age_ms: 12_000 }}
        prevKpis={{ ...kpis, cost_total: 47.0, metrics_age_ms: 20_000 }}
        phase="connected"
        lastSeq={42}
      />,
    );
    expect(screen.getByText("Cost total")).toBeDefined();
    expect(screen.getByText("$48.20")).toBeDefined();
    expect(screen.getByText("+$1.20")).toBeDefined();
    expect(screen.getByText("Metrics age")).toBeDefined();
    expect(screen.getByText("12s")).toBeDefined();
  });
});
