import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import EventTicker from "./EventTicker";
import type { ActivityEvent } from "../lib/activity";
import { ACTIVITY_VISIBLE_CAP } from "../lib/activity";

afterEach(cleanup);

const tick = (i: number): ActivityEvent => ({
  kind: "status",
  sessionId: `ses_${i}`,
  text: `ses_${i} idle → running`,
  glyph: "●",
  seq: 42,
});

describe("EventTicker", () => {
  it("renders time + glyph + text per event", () => {
    render(<EventTicker events={[tick(1)]} paused={false} />);
    expect(screen.getByText("seq 42")).toBeDefined();
    expect(screen.getByText("ses_1 idle → running")).toBeDefined();
  });

  it("caps visible rows with a show-more expander", () => {
    const events = Array.from({ length: ACTIVITY_VISIBLE_CAP + 5 }, (_, i) =>
      tick(i),
    );
    render(<EventTicker events={events} paused={false} />);
    expect(screen.getByText(/show more/)).toBeDefined();
  });

  it("paused freezes with a PAUSED chip and data attr", () => {
    const { container } = render(
      <EventTicker events={[tick(1)]} paused={true} />,
    );
    expect(screen.getByText("Paused")).toBeDefined();
    expect(
      container.querySelector('[data-paused="true"]'),
    ).not.toBeNull();
  });

  it("empty ring shows empty-state text", () => {
    render(<EventTicker events={[]} paused={false} />);
    expect(screen.getByText(/No activity yet/)).toBeDefined();
  });
});
