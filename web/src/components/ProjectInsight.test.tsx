import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import ProjectInsight from "./ProjectInsight";
import RateChart, { rateAreaPath, rateLinePath } from "./RateChart";
import { makeRow } from "../testutils/fixture";

afterEach(cleanup);

describe("rate paths", () => {
  it("draws a flat band for empty series", () => {
    expect(rateLinePath([])).toBe("M0,22 L300,22");
    expect(rateAreaPath([])).toContain("L300,44 L0,44 Z");
  });

  it("baselines the line at zero and peaks at the maximum", () => {
    const line = rateLinePath([0, 50, 100]);
    expect(line.startsWith("M0")).toBe(true);
    expect(line).toContain("300.0,4.0");
  });

  it("closes the area along the baseline", () => {
    expect(rateAreaPath([10, 20])).toMatch(/ L300,44 L0,44 Z$/);
  });
});

describe("RateChart", () => {
  it("labels the peak in text and exposes an accessible name", () => {
    render(<RateChart values={[0, 1000, 500]} label="Fleet" />);
    const figure = screen.getByRole("img", {
      name: /fleet token rate.*peak 1\.0k/i,
    });
    expect(figure).toBeDefined();
    expect(screen.getByText(/peak 1\.0k\/min/)).toBeDefined();
  });

  it("dots every sample with a value tooltip and labels the tick axis", () => {
    const { container } = render(<RateChart values={[10, 20, 30]} label="P" />);
    const titles = Array.from(container.querySelectorAll("circle title")).map(
      (el) => el.textContent,
    );
    expect(titles).toEqual([
      "sample 1 of 3 · 10/min",
      "sample 2 of 3 · 20/min",
      "sample 3 of 3 · 30/min",
    ]);
    expect(screen.getByText("−2 ticks")).toBeDefined();
    expect(screen.getByText("latest")).toBeDefined();
    expect(screen.getByText(/1 sample ≈ 1 poll tick/)).toBeDefined();
  });
});

describe("ProjectInsight", () => {
  it("reconciles distribution, harness split, and rate peak with its rows", () => {
    const rows = [
      makeRow({ status: "running", harness: "pi", spark: [10, 20] }),
      makeRow({ status: "waiting-input", harness: "opencode", spark: [1, 2, 3] }),
      makeRow({ status: "idle", harness: "opencode", spark: [] }),
    ];
    render(<ProjectInsight rows={rows} />);
    const section = screen.getByLabelText("Project detail: 3 sessions");
    expect(section).toBeDefined();
    // Distribution legend carries the exact counts.
    expect(section.textContent).toContain("Running");
    expect(
      screen.getByRole("img", {
        name: /status mix: 1 running, 1 needs input, 1 idle/i,
      }),
    ).toBeDefined();
    // Element-wise spark sum peaks at 20 + 0-padded head: [10, 21, 23].
    expect(
      screen.getByRole("img", { name: /project token rate.*peak 23/i }),
    ).toBeDefined();
  });

  it("renders an empty section without crashing", () => {
    render(<ProjectInsight rows={[]} />);
    expect(screen.getByLabelText("Project detail: 0 sessions")).toBeDefined();
  });
});
