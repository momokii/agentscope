import { describe, expect, it } from "vitest";

import type { Snapshot } from "./sse";
import {
  filterProjects,
  hasProjects,
  projectDisplayName,
  projectIsLive,
  projectNameFromDirectory,
  resolveProjectHeader,
  sortProjects,
} from "./projects";
import { buildFixtureSnapshot, makeProject, makeRow } from "../testutils/fixture";

describe("projects helpers", () => {
  it("sorts by live procs, then running, then newest activity", () => {
    const projects = [
      makeProject({
        directory: "/srv/idle",
        live_procs: 0,
        running: 0,
        last_activity_ms: 5_000,
      }),
      makeProject({
        directory: "/srv/busy",
        live_procs: 2,
        running: 1,
        last_activity_ms: 90_000,
      }),
      makeProject({
        directory: "/srv/warm",
        live_procs: 1,
        running: 3,
        last_activity_ms: 40_000,
      }),
      makeProject({
        directory: "/srv/recent",
        live_procs: 1,
        running: 3,
        last_activity_ms: 10_000,
      }),
    ];
    expect(sortProjects(projects).map((project) => project.directory)).toEqual([
      "/srv/busy",
      "/srv/recent",
      "/srv/warm",
      "/srv/idle",
    ]);
  });

  it("filters by display name or full path, case-insensitively", () => {
    const projects = [
      makeProject({ directory: "/srv/alpha", name: "alpha" }),
      makeProject({ directory: "/work/beta-service", name: "beta-service" }),
    ];
    expect(filterProjects(projects, "ALPHA")).toHaveLength(1);
    expect(filterProjects(projects, "/work/")[0]?.directory).toBe(
      "/work/beta-service",
    );
    expect(filterProjects(projects, "   ")).toHaveLength(2);
    expect(filterProjects(projects, "nope")).toHaveLength(0);
  });

  it("derives a missing display name and liveness from its parts", () => {
    expect(projectNameFromDirectory("/srv/alpha/")).toBe("alpha");
    expect(projectDisplayName(makeProject({ directory: "/srv/x", name: undefined }))).toBe(
      "x",
    );
    expect(projectIsLive({ live_procs: 0, running: 1 })).toBe(true);
    expect(projectIsLive({ live_procs: 1, running: 0 })).toBe(true);
    expect(projectIsLive({ live_procs: 0, running: 0 })).toBe(false);
  });

  it("detects presence of the additive projects array", () => {
    expect(hasProjects(buildFixtureSnapshot())).toBe(false);
    expect(hasProjects(buildFixtureSnapshot({ projects: [] }))).toBe(true);
    expect(hasProjects(null)).toBe(false);
  });

  it("prefers the frozen project summary for the drill-in header", () => {
    const snapshot = buildFixtureSnapshot({
      projects: [
        makeProject({
          directory: "/srv/alpha",
          name: "alpha",
          sessions: 9,
          running: 2,
          waiting_input: 1,
          idle: 9,
          tokens_total: 5000,
          cost_total: 3.5,
          live_procs: 2,
          last_activity_ms: 1000,
        }),
      ],
    });
    expect(resolveProjectHeader(snapshot, "/srv/alpha")).toEqual({
      directory: "/srv/alpha",
      name: "alpha",
      sessions: 9,
      running: 2,
      waiting_input: 1,
      idle: 9,
      tokens_total: 5000,
      cost_total: 3.5,
      live_procs: 2,
      last_activity_ms: 1000,
    });
  });

  it("derives counts and cost from rows when projects are absent", () => {
    const snapshot: Snapshot = buildFixtureSnapshot({
      sessions: [
        makeRow({
          directory: "/srv/alpha",
          status: "running",
          tokens: { in: 10, out: 5, total: 15 },
          cost: 0.25,
          updated_relative_ms: 2000,
        }),
        makeRow({
          directory: "/srv/alpha",
          status: "waiting-input",
          tokens: { in: 20, out: 5, total: 25 },
          cost: 0.75,
          updated_relative_ms: 5000,
        }),
        makeRow({ directory: "/srv/beta", status: "idle" }),
      ],
    });
    const header = resolveProjectHeader(snapshot, "/srv/alpha");
    expect(header.name).toBe("alpha");
    expect(header.sessions).toBe(2);
    expect(header.running).toBe(1);
    expect(header.waiting_input).toBe(1);
    expect(header.tokens_total).toBe(40);
    expect(header.cost_total).toBe(1);
    expect(header.live_procs).toBeUndefined();
    expect(header.last_activity_ms).toBe(2000);
  });
});
