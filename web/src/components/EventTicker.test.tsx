import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import EventTicker from "./EventTicker";
import type { ActivityEvent } from "../lib/activity";
import { ACTIVITY_VISIBLE_CAP } from "../lib/activity";

afterEach(cleanup);

const tick = (i: number): ActivityEvent => ({
  kind: "status",
  sessionId: `ses_${i}`,
  sessionTitle: `work ${i}`,
  project: "alpha",
  harness: i % 2 === 0 ? "pi" : "opencode",
  text: `ses_${i} idle → running`,
  glyph: "●",
  seq: 42,
});

describe("EventTicker", () => {
  it("renders glyph + text + source context per event", () => {
    render(<EventTicker events={[tick(1)]} paused={false} />);
    expect(screen.getByText("seq 42")).toBeDefined();
    expect(screen.getByText("ses_1 idle → running")).toBeDefined();
    // Source context: title (linked), project, harness glyph.
    const link = screen.getByRole("link", { name: "work 1" });
    expect(link.getAttribute("href")).toBe("#/sessions/ses_1");
    expect(screen.getByText("alpha · OC")).toBeDefined();
  });

  it("shows the pi harness glyph for pi-owned events", () => {
    render(<EventTicker events={[tick(2)]} paused={false} />);
    expect(screen.getByText("alpha · π")).toBeDefined();
  });

  it("pages the ring instead of dumping it", () => {
    const events = Array.from({ length: ACTIVITY_VISIBLE_CAP + 5 }, (_, i) =>
      tick(i),
    );
    render(<EventTicker events={events} paused={false} />);
    expect(screen.getByText("1/2")).toBeDefined();
    expect(document.querySelectorAll(".ticker-row")).toHaveLength(
      ACTIVITY_VISIBLE_CAP,
    );
    fireEvent.click(screen.getByRole("button", { name: "Older activity page" }));
    expect(screen.getByText("2/2")).toBeDefined();
    expect(document.querySelectorAll(".ticker-row")).toHaveLength(5);
  });

  it("paused freezes with a PAUSED chip and data attr", () => {
    const { container } = render(
      <EventTicker events={[tick(1)]} paused={true} />,
    );
    expect(screen.getByText("Paused")).toBeDefined();
    expect(container.querySelector('[data-paused="true"]')).not.toBeNull();
  });

  it("empty ring shows empty-state text", () => {
    render(<EventTicker events={[]} paused={false} />);
    expect(screen.getByText(/No activity yet/)).toBeDefined();
  });
});
