import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import Fleet from "./Fleet";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function rows(count: number) {
  return Array.from({ length: count }, (_, index) =>
    makeRow({
      id: `ses_${index + 1}`,
      title: `fixture session ${index + 1}`,
      directory: "/srv/alpha",
      updated_relative_ms: 30_000,
    }),
  );
}

function renderFleet(
  overrides: Partial<Parameters<typeof Fleet>[0]> = {},
) {
  const onQueryChange = vi.fn();
  const view = render(
    <Fleet
      snapshot={buildFixtureSnapshot({ sessions: rows(120) })}
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
      onQueryChange={onQueryChange}
      {...overrides}
    />,
  );
  return { onQueryChange, ...view };
}

function renderedRows(): number {
  return document.querySelectorAll(".fleet-row").length;
}

describe("fleet pagination", () => {
  it("renders 50 rows on page 1 with total and page math", () => {
    renderFleet();
    expect(renderedRows()).toBe(50);
    expect(screen.getByText("1–50 of 120")).toBeDefined();
    expect(screen.getByText("Page 1 of 3")).toBeDefined();
    expect(screen.getByLabelText("Previous page")).toHaveProperty(
      "disabled",
      true,
    );
    expect(screen.getByLabelText("Next page")).toHaveProperty(
      "disabled",
      false,
    );
  });

  it("renders the final partial page and disables next", () => {
    renderFleet({ page: 3 });
    expect(renderedRows()).toBe(20);
    expect(screen.getByText("101–120 of 120")).toBeDefined();
    expect(screen.getByText("Page 3 of 3")).toBeDefined();
    expect(screen.getByLabelText("Next page")).toHaveProperty(
      "disabled",
      true,
    );
  });

  it("clamps an out-of-range page to the last page", () => {
    renderFleet({ page: 99 });
    expect(screen.getByText("Page 3 of 3")).toBeDefined();
    expect(renderedRows()).toBe(20);
  });

  it("asks the hash owner for the next page", () => {
    const { onQueryChange } = renderFleet({ page: 1 });
    fireEvent.click(screen.getByLabelText("Next page"));
    expect(onQueryChange).toHaveBeenCalledWith({ page: 2 });
  });

  it("resets to page 1 when search narrows the result set", () => {
    const { onQueryChange } = renderFleet({ page: 2 });
    fireEvent.change(screen.getByLabelText("Filter fleet sessions"), {
      target: { value: "fixture session 77" },
    });
    expect(onQueryChange).toHaveBeenCalledWith({ page: 1 });
    expect(renderedRows()).toBe(1);
  });

  it("resets to page 1 when a bucket chip changes", () => {
    const { onQueryChange } = renderFleet({ page: 2 });
    fireEvent.click(screen.getByRole("button", { name: /^Old sessions \(0\)$/ }));
    expect(onQueryChange).toHaveBeenCalledWith({ bucket: "old", page: 1 });
  });

  it("relaxes the default bucket and resets page on a date preset", () => {
    const { onQueryChange } = renderFleet({
      page: 2,
      bucket: "active-recent",
    });
    fireEvent.click(screen.getByRole("button", { name: "24h" }));
    expect(onQueryChange).toHaveBeenCalledWith({
      bucket: "all",
      since: "24h",
      from: undefined,
      page: 1,
    });
  });

  it("serializes a custom from date and resets page", () => {
    const { onQueryChange } = renderFleet({
      page: 2,
      bucket: "active-recent",
    });
    fireEvent.change(
      screen.getByLabelText(
        "Filter sessions updated on or after this date",
      ),
      { target: { value: "2026-10-01" } },
    );
    expect(onQueryChange).toHaveBeenCalledWith({
      bucket: "all",
      since: undefined,
      from: "2026-10-01",
      page: 1,
    });
  });
});
