import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import GlobalTimeline from "./GlobalTimeline";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderTimeline(
  overrides: Partial<Parameters<typeof GlobalTimeline>[0]> = {},
) {
  const onSelect = vi.fn();
  const view = render(
    <GlobalTimeline
      sessions={buildFixtureSnapshot().sessions}
      selectedId={null}
      onSelect={onSelect}
      nowMs={NOON}
      {...overrides}
    />,
  );
  return { onSelect, ...view };
}

describe("GlobalTimeline", () => {
  it("renders lanes ordered running→exited with glyphs", () => {
    renderTimeline();
    const lanes = screen.getAllByRole("button", { name: /Open session/ });
    expect(lanes.map((b) => b.getAttribute("aria-label"))).toEqual([
      "Open session Implement fleet view",
      "Open session Review plan defaults",
      "Open session Refactor collector poll loop",
      "Open session Fix auth mount leak",
      "Open session Old spike",
    ]);
  });

  it("nests subagent lanes under parents", () => {
    renderTimeline();
    expect(screen.getByText("subagent 1 of Implement fleet view")).toBeDefined();
    expect(screen.getByText("subagent 2 of Implement fleet view")).toBeDefined();
  });

  it("clicking a lane selects the session id", () => {
    const { onSelect } = renderTimeline();
    fireEvent.click(
      screen.getByRole("button", { name: "Open session Review plan defaults" }),
    );
    expect(onSelect).toHaveBeenCalledWith("ses_waiting1");
  });

  it("marks the selected lane", () => {
    renderTimeline({ selectedId: "ses_idle1" });
    const lane = screen.getByRole("button", {
      name: "Open session Refactor collector poll loop",
    });
    expect(lane.getAttribute("aria-current")).toBe("true");
  });

  it("shows a quiet note for sessions with no messages", () => {
    renderTimeline({
      sessions: [makeRow({ id: "ses_quiet", title: "Quiet one", spark: [] })],
    });
    expect(screen.getByText("no activity in window")).toBeDefined();
  });

  it("shows the empty state with zero sessions", () => {
    renderTimeline({ sessions: [] });
    expect(screen.getByText("No sessions in this fleet yet.")).toBeDefined();
  });
});
