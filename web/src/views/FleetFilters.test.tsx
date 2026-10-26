import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";

import Fleet from "./Fleet";
import StatusDistribution from "../components/StatusDistribution";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderFleet(overrides: Partial<Parameters<typeof Fleet>[0]> = {}) {
  const onFilterChange = vi.fn();
  const view = render(
    <Fleet
      snapshot={buildFixtureSnapshot()}
      prevSnapshot={null}
      phase="connected"
      attempt={0}
      live={true}
      snapshotAgeMs={4000}
      onToggleLive={() => undefined}
      selectedId={null}
      pins={[]}
      onTogglePin={() => undefined}
      onSelect={() => undefined}
      nowMs={NOON}
      statusFilter="all"
      liveOnly={false}
      onFilterChange={onFilterChange}
      {...overrides}
    />,
  );
  return { onFilterChange, ...view };
}

function renderedTitles(): string[] {
  return screen
    .getAllByRole("row")
    .filter((row) => row.classList.contains("fleet-row"))
    .map(
      (row) => within(row).getByText(/Implement|Review|Refactor|Fix|spike/).textContent ?? "",
    );
}

describe("Fleet status filters", () => {
  it("renders one chip per status with live counts", () => {
    renderFleet();
    // ALL carries the fleet total; every other chip carries its own count.
    expect(
      screen.getByRole("button", { name: "Show ALL sessions (5)" }),
    ).toBeDefined();
    for (const label of ["RUNNING", "WAITING", "ERROR", "ARCHIVED"]) {
      expect(
        screen.getByRole("button", { name: new RegExp(`Show ${label} sessions \\(1\\)`) }),
      ).toBeDefined();
    }
    expect(
      screen.getByRole("button", { name: /Show only live sessions.*\(2\)/ }),
    ).toBeDefined();
    // The flat IDLE bucket is replaced by the Recency/Stale/Old age chips.
    expect(
      screen.queryByRole("button", { name: /Show IDLE sessions/ }),
    ).toBeNull();
  });

  it("filters rows to the selected status via the controlled prop", () => {
    renderFleet({ statusFilter: "running" });
    expect(renderedTitles()).toEqual(["Implement fleet view"]);
  });

  it("notifies on chip select so App can persist it to the hash", () => {
    const { onFilterChange } = renderFleet();
    fireEvent.click(
      screen.getByRole("button", { name: /Show ARCHIVED sessions/ }),
    );
    expect(onFilterChange).toHaveBeenCalledWith("archived", false);
  });

  it("live-only keeps running plus waiting-input rows", () => {
    renderFleet({ liveOnly: true });
    expect(renderedTitles()).toEqual([
      "Implement fleet view",
      "Review plan defaults",
    ]);
  });

  it("live-only toggle notifies with the flipped flag", () => {
    const { onFilterChange } = renderFleet();
    fireEvent.click(screen.getByRole("button", { name: /Show only live sessions/ }));
    expect(onFilterChange).toHaveBeenCalledWith("all", true);
  });

  it("composes status filter with the search box", () => {
    renderFleet({ statusFilter: "error" });
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "auth" },
    });
    expect(renderedTitles()).toEqual(["Fix auth mount leak"]);
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "fleet view" },
    });
    expect(screen.getByText(/No sessions match/)).toBeDefined();
  });

  it("empty state names the active filter and offers a one-click reset", () => {
    const { onFilterChange } = renderFleet({ statusFilter: "idle" });
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "zzz-no-match" },
    });
    expect(
      screen.getByText("No sessions match “zzz-no-match” · status IDLE."),
    ).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Show all sessions" }));
    expect(onFilterChange).toHaveBeenCalledWith("all", false);
    // Search cleared locally: the controlled status prop still applies, so
    // the idle row returns without waiting on the hash round-trip.
    expect(renderedTitles()).toEqual(["Refactor collector poll loop"]);
    expect(
      (screen.getByLabelText("Filter fleet sessions") as HTMLInputElement).value,
    ).toBe("");
  });
});

describe("Fleet status distribution", () => {
  it("renders proportional segments with glyph, label, and count", () => {
    const { container } = renderFleet();
    const segments = Array.from(container.querySelectorAll(".dist-seg"));
    expect(segments).toHaveLength(5);
    for (const seg of segments) {
      expect((seg as HTMLElement).style.width).toBe("20%");
    }
    const legend = container.querySelector(".dist-legend");
    if (legend === null) throw new Error("missing distribution legend");
    for (const [glyph, label] of [
      ["●", "RUNNING"],
      ["▲", "NEEDS INPUT"],
      ["◌", "IDLE"],
      ["✕", "ERROR"],
      ["▪", "ARCHIVED"],
    ]) {
      expect(within(legend as HTMLElement).getByText(glyph)).toBeDefined();
      expect(within(legend as HTMLElement).getByText(label)).toBeDefined();
    }
    expect(within(legend as HTMLElement).getAllByText("1")).toHaveLength(5);
  });

  it("renders zero-height segments for missing statuses", () => {
    const { container } = render(
      <StatusDistribution
        counts={{ running: 3, "waiting-input": 0, idle: 1, error: 0, archived: 0 }}
        total={4}
      />,
    );
    const segments = Array.from(container.querySelectorAll(".dist-seg"));
    expect((segments[0] as HTMLElement).style.width).toBe("75%");
    expect((segments[1] as HTMLElement).style.width).toBe("0%");
    expect((segments[2] as HTMLElement).style.width).toBe("25%");
  });

  it("renders sensibly for an empty fleet", () => {
    const { container } = renderFleet({
      snapshot: buildFixtureSnapshot({ sessions: [] }),
    });
    expect(
      container.querySelector(".dist-empty")?.textContent,
    ).toBe("No sessions in this fleet.");
    expect(
      container.querySelector(".fleet > .empty-state")?.textContent,
    ).toBe("No sessions in this fleet.");
  });
});
