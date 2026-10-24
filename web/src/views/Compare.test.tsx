import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import Compare from "./Compare";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderCompare(
  overrides: Partial<Parameters<typeof Compare>[0]> = {},
) {
  const onChangeIds = vi.fn();
  const onSelect = vi.fn();
  const onPointSelect = vi.fn();
  const view = render(
    <Compare
      sessions={buildFixtureSnapshot().sessions}
      ids={["ses_running1", "ses_waiting1"]}
      onChangeIds={onChangeIds}
      onSelect={onSelect}
      onPointSelect={onPointSelect}
      nowMs={NOON}
      {...overrides}
    />,
  );
  return { onChangeIds, onSelect, onPointSelect, ...view };
}

describe("Compare", () => {
  it("renders KPI cards and overlaid charts for 2+ sessions", () => {
    const { container } = renderCompare();
    const cards = container.querySelectorAll(".compare-card-title");
    expect(Array.from(cards).map((c) => c.textContent)).toEqual([
      "Implement fleet view",
      "Review plan defaults",
    ]);
    expect(screen.getByLabelText("Cumulative cost overlay, 2 sessions")).toBeDefined();
    expect(screen.getByLabelText("Cumulative tokens overlay, 2 sessions")).toBeDefined();
  });

  it("shows a picker prompt — not a broken chart — with 1 session", () => {
    renderCompare({ ids: ["ses_running1"] });
    expect(
      screen.getByText("One session selected — add one more to draw the overlay."),
    ).toBeDefined();
    expect(screen.queryByRole("img", { name: /overlay/ })).toBeNull();
  });

  it("shows the empty state with zero sessions", () => {
    renderCompare({ sessions: [], ids: [] });
    expect(
      screen.getByText("No sessions in this fleet to compare."),
    ).toBeDefined();
  });

  it("toggles picker sessions through onChangeIds", () => {
    const { onChangeIds } = renderCompare({ ids: [] });
    const search = screen.getByLabelText("Pick sessions to compare");
    fireEvent.change(search, { target: { value: "Refactor" } });
    fireEvent.click(screen.getByRole("button", { name: /ses_idle1/ }));
    expect(onChangeIds).toHaveBeenCalledWith(["ses_idle1"]);
  });

  it("clicking a card selects the session id", () => {
    const { onSelect } = renderCompare();
    fireEvent.click(
      screen.getByRole("button", { name: "Open session Implement fleet view" }),
    );
    expect(onSelect).toHaveBeenCalledWith("ses_running1");
  });

  it("clicking a chart point fires the drill-down hook with the right id", () => {
    const { onPointSelect, container } = renderCompare();
    const points = container.querySelectorAll("circle title");
    expect(points.length).toBeGreaterThan(0);
    const first = points[0]!.parentElement!;
    fireEvent.click(first);
    expect(onPointSelect).toHaveBeenCalledWith(
      "ses_running1",
      expect.any(Number),
    );
    expect(onPointSelect.mock.calls[0]![0]).toBe("ses_running1");
  });
});
