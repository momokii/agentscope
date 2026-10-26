import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import Projects from "./Projects";
import { buildProjectHash } from "../lib/routing";
import { buildFixtureSnapshot, makeProject } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function cards(): HTMLElement[] {
  return Array.from(document.querySelectorAll(".project-card"));
}

describe("Projects landing", () => {
  it("ranks projects by live procs, then running, then recency", () => {
    render(
      <Projects
        nowMs={NOON}
        projects={[
          makeProject({
            directory: "/srv/quiet",
            live_procs: 0,
            running: 0,
            last_activity_ms: 1_000,
          }),
          makeProject({
            directory: "/srv/alpha",
            live_procs: 2,
            running: 1,
            last_activity_ms: 90_000,
          }),
          makeProject({
            directory: "/srv/beta",
            live_procs: 0,
            running: 3,
            last_activity_ms: 40_000,
          }),
        ]}
      />,
    );
    expect(cards().map((card) => card.getAttribute("data-directory"))).toEqual([
      "/srv/alpha",
      "/srv/beta",
      "/srv/quiet",
    ]);
  });

  it("marks live projects with glyph + label + process count", () => {
    render(
      <Projects
        nowMs={NOON}
        projects={[
          makeProject({
            directory: "/srv/alpha",
            name: "alpha",
            live_procs: 2,
            running: 1,
            sessions: 9,
            tokens_total: 12_000,
            cost_total: 4.5,
            last_activity_ms: 5_000,
          }),
          makeProject({
            directory: "/srv/beta",
            name: "beta",
            live_procs: 0,
            running: 0,
          }),
        ]}
      />,
    );
    const live = cards()[0] as HTMLElement;
    expect(live.className).toContain("project-card-live");
    expect(live.getAttribute("data-live")).toBe("true");
    expect(within(live).getByText("●")).toBeDefined();
    expect(within(live).getByText("2 procs")).toBeDefined();
    expect(within(live).getByText("LIVE")).toBeDefined();
    expect(within(live).getByText("alpha")).toBeDefined();
    expect(within(live).getByText("/srv/alpha")).toBeDefined();
    expect(within(live).getByText("12.0k")).toBeDefined();
    expect(within(live).getByText("$4.50")).toBeDefined();

    const quiet = cards()[1] as HTMLElement;
    expect(quiet.getAttribute("data-live")).toBe("false");
    expect(within(quiet).getByText("no procs")).toBeDefined();
    expect(within(quiet).getByText("QUIET")).toBeDefined();
  });

  it("links each card to the encoded project drill-in route", () => {
    render(
      <Projects
        projects={[makeProject({ directory: "/srv/alpha service", name: "alpha" })]}
      />,
    );
    expect(cards()[0]?.getAttribute("href")).toBe(
      buildProjectHash("/srv/alpha service"),
    );
  });

  it("filters by name and path and offers a reset on no match", () => {
    render(
      <Projects
        nowMs={NOON}
        projects={[
          makeProject({ directory: "/srv/alpha", name: "alpha" }),
          makeProject({ directory: "/work/beta-service", name: "beta-service" }),
        ]}
      />,
    );
    fireEvent.change(screen.getByLabelText("Filter projects"), {
      target: { value: "beta" },
    });
    expect(cards().map((card) => card.getAttribute("data-directory"))).toEqual([
      "/work/beta-service",
    ]);
    fireEvent.change(screen.getByLabelText("Filter projects"), {
      target: { value: "/srv/" },
    });
    expect(cards().map((card) => card.getAttribute("data-directory"))).toEqual([
      "/srv/alpha",
    ]);
    fireEvent.change(screen.getByLabelText("Filter projects"), {
      target: { value: "no-such-project" },
    });
    expect(screen.getByText(/No projects match/)).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Show all projects" }));
    expect(cards()).toHaveLength(2);
  });

  it("keeps the LIVE toggle and stale state on the landing", () => {
    let toggled = 0;
    render(
      <Projects
        projects={[]}
        live={false}
        stale={true}
        snapshotAgeMs={4000}
        onToggleLive={() => {
          toggled += 1;
        }}
      />,
    );
    expect(
      document.querySelector(".state-chip-paused")?.textContent,
    ).toContain("Paused");
    expect(screen.getByText(/Stale · last update 4s ago/)).toBeDefined();
    fireEvent.click(screen.getByLabelText("Resume live updates"));
    expect(toggled).toBe(1);
  });

  it("renders the fleet KPI strip with distinct cost meanings", () => {
    const { kpis } = buildFixtureSnapshot();
    render(
      <Projects
        projects={[]}
        kpis={{ ...kpis, cost_total: 88.5, metrics_age_ms: 3_000 }}
      />,
    );
    expect(screen.getByText("Cost today")).toBeDefined();
    expect(screen.getByText("Cost total")).toBeDefined();
    expect(screen.getByText("$88.50")).toBeDefined();
    expect(screen.getByText("Metrics age")).toBeDefined();
    expect(screen.getByText("3s")).toBeDefined();
  });

  it("shows an explicit empty state when no projects exist", () => {
    render(<Projects projects={[]} />);
    expect(screen.getByText("No projects yet.")).toBeDefined();
    expect(cards()).toHaveLength(0);
  });

  it("renders em dashes for absent optional project fields", () => {
    render(<Projects projects={[{ directory: "/srv/bare" }]} />);
    const bare = cards()[0] as HTMLElement;
    expect(within(bare).getByText("— procs")).toBeDefined();
    expect(within(bare).getAllByText("—").length).toBeGreaterThanOrEqual(4);
    expect(within(bare).getByText("QUIET")).toBeDefined();
  });
});
