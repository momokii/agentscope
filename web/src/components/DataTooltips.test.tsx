import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import HarnessBadge from "./HarnessBadge";
import StatusBadge from "./StatusBadge";
import KpiStrip from "./KpiStrip";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

/** Every data badge answers what it is and how to read it on hover. */
describe("data tooltips", () => {
  it("status badges explain their state", () => {
    const { container } = render(<StatusBadge status="waiting-input" />);
    expect(container.querySelector(".badge")?.getAttribute("title")).toMatch(
      /heuristic/i,
    );
    const { container: err } = render(<StatusBadge status="error" />);
    expect(err.querySelector(".badge")?.getAttribute("title")).toMatch(
      /newest/i,
    );
    const { container: run } = render(<StatusBadge status="running" />);
    expect(run.querySelector(".badge")?.getAttribute("title")).toMatch(
      /generating/i,
    );
  });

  it("harness badges explain their owner", () => {
    render(<HarnessBadge harness="pi" />);
    expect(
      screen.getByText("PI").closest(".badge")?.getAttribute("title"),
    ).toMatch(/session files/i);
    render(<HarnessBadge harness="opencode" />);
    expect(
      screen.getByText("OPENCODE").closest(".badge")?.getAttribute("title"),
    ).toMatch(/opencode\.db/i);
  });

  it("every KPI label explains its number", () => {
    const { kpis } = buildFixtureSnapshot();
    const { container } = render(
      <KpiStrip kpis={kpis} prevKpis={null} phase="connected" lastSeq={1} />,
    );
    for (const label of [
      "Active",
      "Waiting input",
      "Tokens/hr",
      "Cost today",
      "Errors",
      "Live procs",
      "Live panes",
      "Metrics age",
    ]) {
      const el = Array.from(container.querySelectorAll(".kpi-label")).find(
        (node) => node.textContent === label,
      );
      expect(el, `${label} label renders`).toBeDefined();
      expect(
        el?.getAttribute("title"),
        `${label} needs an explanatory title`,
      ).toMatch(/.{10,}/);
    }
  });
});
