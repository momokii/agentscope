import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

import Fleet from "./Fleet";
import { buildFixtureSnapshot, makeProject, makeRow } from "../testutils/fixture";

afterEach(cleanup);

const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

function renderProject({
  projects,
  projectDirectory = "/srv/alpha",
}: {
  projects?: ReturnType<typeof makeProject>[];
  projectDirectory?: string;
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

  it("renders the project KPI header from the frozen projects array", () => {
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
    expect(within(header).getByText("/srv/alpha")).toBeDefined();
    expect(within(header).getByText("Live procs")).toBeDefined();
    expect(within(header).getByText("2")).toBeDefined();
    expect(within(header).getByText("Live panes")).toBeDefined();
    expect(within(header).getByText("3")).toBeDefined();
    expect(within(header).getByText("Cost total")).toBeDefined();
    expect(within(header).getByText("$4.50")).toBeDefined();
    expect(within(header).getByText("9.0k")).toBeDefined();
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
