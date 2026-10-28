import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import Fleet from "./Fleet";
import { buildFixtureSnapshot, makeProject, makeRow } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderProject({
  projects,
  projectDirectory = "/srv/alpha",
  date,
}: {
  projects?: ReturnType<typeof makeProject>[];
  projectDirectory?: string;
  date?: { preset: "all" | "today" | "24h" | "7d" | "30d"; from: null };
} = {}) {
  const onSelect = vi.fn();
  render(
    <Fleet
      snapshot={buildFixtureSnapshot({
        sessions: [
          makeRow({
            id: "ses_alpha1",
            title: "alpha work",
            directory: "/srv/alpha",
            status: "running",
            updated_relative_ms: 5_000,
          }),
          makeRow({
            id: "ses_beta1",
            title: "beta work",
            directory: "/srv/beta",
            status: "idle",
            updated_relative_ms: 5_000,
          }),
        ],
        ...(projects === undefined ? {} : { projects }),
      })}
      prevSnapshot={null}
      phase="connected"
      attempt={0}
      live={true}
      snapshotAgeMs={4000}
      onToggleLive={() => undefined}
      selectedId={null}
      pins={[]}
      onTogglePin={() => undefined}
      onSelect={onSelect}
      nowMs={NOON}
      projectDirectory={projectDirectory}
      bucket="all"
      {...(date === undefined ? {} : { date })}
    />,
  );
  return { onSelect, container: document };
}

describe("project drill-in", () => {
  it("shows only the project's sessions with a breadcrumb back", () => {
    const { onSelect } = renderProject();
    expect(document.querySelectorAll(".fleet-row")).toHaveLength(1);
    expect(screen.getByText("alpha work")).toBeDefined();
    expect(screen.queryByText("beta work")).toBeNull();
    expect(document.querySelector(".fleet-title")?.textContent).toBe(
      "Sessions (1)",
    );

    const back = screen.getByRole("link", { name: "Back to projects" });
    expect(back.getAttribute("href")).toBe("#/");
    fireEvent.click(screen.getByText("alpha work"));
    expect(onSelect).toHaveBeenCalledWith("ses_alpha1");
  });

  it("derives the project KPI header from member rows under the date filter", () => {
    const alphaProject = {
      ...makeProject({
        directory: "/srv/alpha",
        name: "alpha",
        live_procs: 2,
        running: 1,
        waiting_input: 0,
        sessions: 7,
        tokens_total: 9000,
        cost_total: 4.5,
        last_activity_ms: 5_000,
      }),
      live_panes: 3,
    };
    renderProject({ projects: [alphaProject] });
    const header = screen.getByLabelText("Project alpha");
    expect(header.querySelector(".project-header-name")?.textContent).toBe(
      "alpha",
    );
    // Runtime truth still comes from the frozen array…
    expect(within(header).getByText("/srv/alpha")).toBeDefined();
    expect(within(header).getByText("Live procs")).toBeDefined();
    expect(within(header).getByText("2")).toBeDefined();
    expect(within(header).getByText("Live panes")).toBeDefined();
    expect(within(header).getByText("3")).toBeDefined();
    // …but member counts/totals recompute from the date-filtered rows
    // (one fresh session: 1.5k tokens, $0.01 — not the frozen 9.0k/$4.50).
    expect(within(header).getByText("Cost total")).toBeDefined();
    expect(within(header).getByText("$0.01")).toBeDefined();
    expect(within(header).getByText("1.5k")).toBeDefined();
  });

  it("renders the project insight with distribution and rate from member rows", () => {
    renderProject();
    // One running member row in /srv/alpha: distribution + rate reconcile.
    const section = screen.getByLabelText("Project detail: 1 sessions");
    expect(section).toBeDefined();
    expect(
      screen.getByRole("img", {
        name: /status mix: 1 running, 0 needs input, 0 idle/i,
      }),
    ).toBeDefined();
    expect(
      screen.getByRole("img", { name: /project token rate/i }),
    ).toBeDefined();
  });

  it("scopes the project header and insight to the date filter", () => {
    render(
      <Fleet
        snapshot={buildFixtureSnapshot({
          sessions: [
            makeRow({
              id: "ses_fresh",
              title: "fresh work",
              directory: "/srv/alpha",
              status: "running",
              updated_relative_ms: 5_000,
            }),
            makeRow({
              id: "ses_stale",
              title: "stale work",
              directory: "/srv/alpha",
              status: "idle",
              updated_relative_ms: 10 * 24 * 3_600_000,
            }),
          ],
        })}
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
        projectDirectory="/srv/alpha"
        bucket="all"
        date={{ preset: "24h", from: null }}
      />,
    );
    // Header aggregates + insight + table all drop the 10-day-old row.
    expect(screen.getByLabelText("Project detail: 1 sessions")).toBeDefined();
    expect(screen.getByText("fresh work")).toBeDefined();
    expect(screen.queryByText("stale work")).toBeNull();
  });

  it("shows windowed project tokens and cost under a 24h filter", () => {
    render(
      <Fleet
        snapshot={buildFixtureSnapshot({
          sessions: [
            makeRow({
              id: "ses_fresh",
              title: "fresh work",
              directory: "/srv/alpha",
              status: "running",
              updated_relative_ms: 5_000,
              tokens: { in: 1000, out: 500, total: 1500 },
              cost: 0.5,
            }),
          ],
          session_windows: {
            ses_fresh: {
              tokens_24h: 900,
              tokens_7d: 1200,
              tokens_30d: 1500,
              cost_24h: 0.3,
              cost_7d: 0.4,
              cost_30d: 0.5,
            },
          },
        })}
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
        projectDirectory="/srv/alpha"
        bucket="all"
        date={{ preset: "24h", from: null }}
      />,
    );
    const header = screen.getByLabelText("Project alpha");
    expect(within(header).getByText("Tokens 24h")).toBeDefined();
    expect(within(header).getByText("900")).toBeDefined();
    expect(within(header).getByText("Cost 24h")).toBeDefined();
    expect(within(header).getByText("$0.30")).toBeDefined();
  });

  it("degrades to derived counts and em dashes for live procs and panes", () => {
    renderProject();
    const header = screen.getByLabelText("Project alpha");
    expect(within(header).getByText("Live procs")).toBeDefined();
    expect(within(header).getByText("Live panes")).toBeDefined();
    expect(within(header).getAllByText("—")).toHaveLength(2);
    expect(within(header).getByText("Running")).toBeDefined();
  });
});
