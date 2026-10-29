import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import ActivityView from "./Activity";
import type { ActivityEvent } from "../lib/activity";

afterEach(cleanup);

function ev(overrides: Partial<ActivityEvent> & { id: string }): ActivityEvent {
  const i = overrides.id;
  return {
    kind: "status",
    sessionId: `ses_${i}`,
    sessionTitle: `work ${i}`,
    project: i === "pi" ? "betta" : "alpha",
    directory: i === "pi" ? "/srv/betta" : "/srv/alpha",
    harness: i === "pi" ? "pi" : "opencode",
    text: `ses_${i} idle → running`,
    glyph: "●",
    seq: 42,
    ...overrides,
  };
}

describe("ActivityView", () => {
  it("renders the log with source context and session links", () => {
    render(<ActivityView events={[ev({ id: "a" }), ev({ id: "pi" })]} />);
    expect(screen.getByText("Activity")).toBeDefined();
    expect(screen.getByRole("link", { name: "work a" })).toBeDefined();
    expect(screen.getByText("alpha · OC")).toBeDefined();
    expect(screen.getByText("betta · π")).toBeDefined();
  });

  it("searches title, project, id, and kind", () => {
    const onQueryChange = vi.fn();
    render(
      <ActivityView
        events={[ev({ id: "a" }), ev({ id: "pi" })]}
        query=""
        onQueryChange={onQueryChange}
      />,
    );
    fireEvent.change(screen.getByLabelText("Filter activity"), {
      target: { value: "betta" },
    });
    expect(onQueryChange).toHaveBeenCalledWith("betta");
  });

  it("filters by harness and pages the ring", () => {
    const events = Array.from({ length: 30 }, (_, i) =>
      ev({ id: `n${i}` }),
    );
    render(<ActivityView events={events} query="work n" page={1} />);
    // 30 rows at 25/page → 2 pages; harness chips slice the ring.
    expect(screen.getByText("Page 1 of 2")).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "PI" }));
    expect(screen.queryByText("Page 1 of 2")).toBeNull();
  });

  it("shows the empty state for an empty or fully filtered ring", () => {
    render(<ActivityView events={[]} />);
    expect(screen.getByText(/No activity yet/)).toBeDefined();
  });

  it("explains its purpose next to the guide", () => {
    render(<ActivityView events={[ev({ id: "a" })]} />);
    expect(screen.getByText("What just changed across the fleet")).toBeDefined();
    expect(
      screen.getByRole("button", { name: "How to read this dashboard" }),
    ).toBeDefined();
  });
});
