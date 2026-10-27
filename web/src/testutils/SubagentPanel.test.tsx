import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";

import SessionDetail from "../views/SessionDetail";
import { makeRow } from "./fixture";
import {
  FIXTURE_MAIN_ID,
  FIXTURE_SUB_ID,
  FIXTURE_SUB2_ID,
  fixtureProvider,
} from "./sessionDetail.fixture";

afterEach(() => {
  cleanup();
  window.location.hash = "#/";
});

const CHILD_ROWS = [
  makeRow({
    id: FIXTURE_SUB_ID,
    title: "Research windowing strategies",
    status: "running",
    agent: "explore",
    model: "toy-provider/toy-model",
    updated_relative_ms: 5_000,
  }),
  makeRow({
    id: FIXTURE_SUB2_ID,
    title: "Plan the cost tab math",
    status: "idle",
    agent: "plan",
    model: "toy-provider/toy-model-mini",
    updated_relative_ms: 90_000,
  }),
];

describe("SubagentPanel", () => {
  it("renders one row per child with its TRUE status, never the parent's", async () => {
    const parentRow = makeRow({ id: FIXTURE_MAIN_ID, status: "running" });
    const { container } = render(
      <SessionDetail
        id={FIXTURE_MAIN_ID}
        tab="transcript"
        row={parentRow}
        paletteOpen={false}
        sessions={CHILD_ROWS}
        provider={fixtureProvider}
      />,
    );
    await waitFor(() => expect(container.querySelector(".subagents")).not.toBeNull());
    const panel = container.querySelector(".subagents");
    if (panel === null) throw new Error("missing subagents panel");
    expect(panel.textContent).toContain("(2)");

    const exploreRow = within(panel as HTMLElement).getByText(
      "Research windowing strategies",
    ).closest("a");
    const planRow = within(panel as HTMLElement).getByText(
      "Plan the cost tab math",
    ).closest("a");
    if (exploreRow === null || planRow === null) {
      throw new Error("missing subagent rows");
    }
    // Finished subagent reads idle even under a running parent.
    expect(exploreRow.textContent).toContain("RUNNING");
    expect(exploreRow.textContent).toContain("●");
    expect(planRow.textContent).toContain("IDLE");
    expect(planRow.textContent).toContain("◌");
    // Agent name, tokens, cost, and relative activity are all present.
    expect(exploreRow.textContent).toContain("explore");
    expect(planRow.textContent).toContain("plan");
    expect(exploreRow.textContent).toContain("5s ago");
    expect(planRow.textContent).toContain("1m ago");
  });

  it("navigates to the child's own drill-down on click", async () => {
    const { container } = render(
      <SessionDetail
        id={FIXTURE_MAIN_ID}
        tab="transcript"
        row={null}
        paletteOpen={false}
        sessions={CHILD_ROWS}
        provider={fixtureProvider}
      />,
    );
    await waitFor(() => expect(container.querySelector(".subagents")).not.toBeNull());
    const panel = container.querySelector(".subagents");
    if (panel === null) throw new Error("missing subagents panel");
    const exploreRow = within(panel as HTMLElement).getByText(
      "Research windowing strategies",
    ).closest("a");
    expect(exploreRow?.getAttribute("href")).toBe(
      `#/sessions/${FIXTURE_SUB_ID}`,
    );
    const planRow = within(panel as HTMLElement).getByText(
      "Plan the cost tab math",
    ).closest("a");
    expect(planRow?.getAttribute("href")).toBe(
      `#/sessions/${FIXTURE_SUB2_ID}`,
    );
  });

  it("shows an explicit empty state with no subagents", async () => {
    render(
      <SessionDetail
        id={FIXTURE_SUB_ID}
        tab="transcript"
        row={null}
        paletteOpen={false}
        sessions={[]}
        provider={fixtureProvider}
      />,
    );
    expect(await screen.findByText("No subagents in this session.")).toBeDefined();
    expect(screen.getByText("(0)")).toBeDefined();
  });
});
