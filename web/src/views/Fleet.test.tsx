import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";

import Fleet from "./Fleet";
import { buildFixtureSnapshot } from "../testutils/fixture";

afterEach(cleanup);

/** Fixed local noon keeps Today/Yesterday/Older buckets deterministic. */
const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderFleet(overrides: Partial<Parameters<typeof Fleet>[0]> = {}) {
  const onToggleLive = vi.fn();
  const onTogglePin = vi.fn();
  const onSelect = vi.fn();
  const view = render(
    <Fleet
      snapshot={buildFixtureSnapshot()}
      prevSnapshot={null}
      phase="connected"
      attempt={0}
      live={true}
      snapshotAgeMs={4000}
      onToggleLive={onToggleLive}
      selectedId={null}
      pins={[]}
      onTogglePin={onTogglePin}
      onSelect={onSelect}
      nowMs={NOON}
      {...overrides}
    />,
  );
  return { onToggleLive, onTogglePin, onSelect, ...view };
}

/** Row order in the rendered table body, by session title. */
function renderedTitles(): string[] {
  const rows = screen.getAllByRole("row").filter((row) =>
    row.classList.contains("fleet-row"),
  );
  return rows.map(
    (row) => within(row).getByText(/Implement|Review|Refactor|Fix|spike/).textContent ?? "",
  );
}

describe("Fleet", () => {
  it("renders fixture rows sorted running→exited with glyph+label badges", () => {
    renderFleet();
    expect(renderedTitles()).toEqual([
      "Implement fleet view",
      "Review plan defaults",
      "Refactor collector poll loop",
      "Fix auth mount leak",
      "Old spike",
    ]);

    // Status is glyph + label text, never color alone (scoped to the table:
    // the filter bar and distribution legend repeat the same signals).
    const table = within(document.querySelector(".fleet-table") as HTMLElement);
    expect(table.getByText("●")).toBeDefined();
    expect(table.getByText("RUNNING")).toBeDefined();
    expect(table.getByText("▲")).toBeDefined();
    expect(table.getByText("NEEDS INPUT")).toBeDefined();
    expect(table.getByText("◌")).toBeDefined();
    expect(table.getByText("IDLE")).toBeDefined();
    expect(table.getByText("✕")).toBeDefined();
    expect(table.getByText("ERROR")).toBeDefined();
    expect(table.getByText("▪")).toBeDefined();
    expect(table.getByText("ARCHIVED")).toBeDefined();
  });

  it("keeps waiting-input visually distinct from idle by shape + label", () => {
    renderFleet();
    // Scoped to the table: the legend repeats both labels by design.
    const table = within(document.querySelector(".fleet-table") as HTMLElement);
    // Labels differ (NEEDS INPUT vs IDLE) and glyphs differ (▲ vs ◌):
    // an operator at grayscale can still tell them apart.
    const waitingBadge = table.getByText("NEEDS INPUT").closest(".badge");
    const idleBadge = table.getByText("IDLE").closest(".badge");
    expect(waitingBadge?.textContent).toContain("▲");
    expect(idleBadge?.textContent).toContain("◌");
    expect(waitingBadge?.className).not.toBe(idleBadge?.className);
  });

  it("groups rows Today / Yesterday / Older", () => {
    renderFleet();
    const headers = Array.from(document.querySelectorAll(".group-header")).map(
      (row) => row.textContent,
    );
    expect(headers).toEqual(["Today", "Yesterday", "Older"]);
  });

  it("filters rows by search query", () => {
    renderFleet();
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "fleet view" },
    });
    expect(renderedTitles()).toEqual(["Implement fleet view"]);
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "nothing-matches-this" },
    });
    expect(screen.getByText(/No sessions match/)).toBeDefined();
  });

  it("shows the ↳N subagent expander chip and expands detail", () => {
    renderFleet();
    const expander = screen.getByLabelText("Expand 2 subagents for Implement fleet view");
    expect(expander.textContent).toContain("↳");
    expect(expander.textContent).toContain("2");
    fireEvent.click(expander);
    expect(
      screen.getByText(/2 child sessions.*subagent roll-up/),
    ).toBeDefined();
  });

  it("renders empty spark arrays as a flat placeholder without crashing", () => {
    // ses_idle1 ships spark: [] — assert its row still renders tokens + cost.
    renderFleet();
    const idleRow = screen.getByText("Refactor collector poll loop").closest("tr");
    expect(idleRow?.textContent).toContain("2.8k");
    expect(idleRow?.textContent).toContain("$0.03");
  });

  it("shows STALE chip on a stale snapshot and the reconnecting banner", () => {
    const { container } = renderFleet({
      snapshot: buildFixtureSnapshot({ stale: true }),
      phase: "reconnecting",
      attempt: 3,
    });
    const staleChip = container.querySelector(".state-chip-stale");
    expect(staleChip?.textContent).toContain("Stale");
    expect(staleChip?.textContent).toContain("last update 4s ago");
    expect(screen.getByText(/reconnecting… attempt 3/)).toBeDefined();
  });

  it("shows PAUSED chip when LIVE is off and toggles back on click", () => {
    const { container, onToggleLive } = renderFleet({
      live: false,
      phase: "closed",
    });
    // The LIVE toggle button also reads "Paused" — scope to the state chip.
    const chip = container.querySelector(".state-chip-paused");
    expect(chip?.textContent).toContain("Paused");
    fireEvent.click(screen.getByLabelText("Resume live updates"));
    expect(onToggleLive).toHaveBeenCalledTimes(1);
  });

  it("renders a waiting empty state before the first snapshot", () => {
    renderFleet({ snapshot: null });
    expect(screen.getByText(/Waiting for the first snapshot/)).toBeDefined();
  });

  it("highlights the deep-linked row and selects on click", () => {
    const { onSelect } = renderFleet({ selectedId: "ses_idle1" });
    const row = document.querySelector('[data-session-id="ses_idle1"]');
    expect(row?.getAttribute("data-selected")).toBe("true");
    expect(row?.classList.contains("fleet-row-selected")).toBe(true);
    fireEvent.click(screen.getByText("Implement fleet view"));
    expect(onSelect).toHaveBeenCalledWith("ses_running1");
  });

  it("renders the pinned section with 1–9 badges and pin toggles", () => {
    const { onTogglePin } = renderFleet({ pins: ["ses_idle1", "ses_error1"] });
    const headers = Array.from(document.querySelectorAll(".group-header")).map(
      (row) => row.textContent,
    );
    expect(headers[0]).toBe("Pinned");
    // Pin badges follow pin order: ses_idle1 → 1, ses_error1 → 2.
    const badges = Array.from(document.querySelectorAll(".pin-badge")).map(
      (el) => el.textContent,
    );
    expect(badges).toEqual(["1", "2"]);
    // Pinned rows leave the main groups (no duplicates).
    const idleRows = document.querySelectorAll(
      '[data-session-id="ses_idle1"]',
    );
    expect(idleRows.length).toBe(1);
    fireEvent.click(screen.getByLabelText("Unpin session Fix auth mount leak"));
    expect(onTogglePin).toHaveBeenCalledWith("ses_error1");
    fireEvent.click(
      screen.getByLabelText("Pin session Implement fleet view"),
    );
    expect(onTogglePin).toHaveBeenCalledWith("ses_running1");
  });

  it("offers a manual refresh button beside the live toggle", () => {
    const onRefresh = vi.fn();
    renderFleet({ onRefresh, refreshing: false });
    const button = screen.getByRole("button", { name: "Refresh now" });
    fireEvent.click(button);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("renders the fleet token-rate strip from live snapshot sparks", () => {
    renderFleet();
    expect(
      screen.getByRole("img", { name: /fleet token rate/i }),
    ).toBeDefined();
  });

  it("scopes KPI counts to the active date filter", () => {
    // Fixture: 1 running, 1 waiting, 1 error (25h old). A 24h window
    // drops the error row from Active/Waiting/Errors (global costs stay).
    const { container } = renderFleet({
      date: { preset: "24h", from: null },
    });
    const kpiText = (label: string): string | undefined => {
      const cards = Array.from(container.querySelectorAll(".kpi-card"));
      const card = cards.find((c) => c.textContent?.includes(label));
      return card?.querySelector(".kpi-value")?.textContent ?? undefined;
    };
    expect(kpiText("Active")).toBe("1");
    expect(kpiText("Waiting input")).toBe("1");
    expect(kpiText("Errors")).toBe("0");
    // Throughput/cost/live presence stay global (not date-scoped).
    expect(kpiText("Cost today")).toBe("$12.40");
  });
});
