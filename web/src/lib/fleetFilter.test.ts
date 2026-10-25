import { describe, expect, it } from "vitest";

import {
  applyFleetFilters,
  countByStatus,
  isLiveStatus,
} from "./fleetSort";
import { buildFixtureSnapshot, makeRow } from "../testutils/fixture";

describe("countByStatus", () => {
  it("counts each status once over the fixture snapshot", () => {
    const { counts, total } = countByStatus(buildFixtureSnapshot().sessions);
    expect(total).toBe(5);
    expect(counts).toEqual({
      running: 1,
      "waiting-input": 1,
      idle: 1,
      error: 1,
      archived: 1,
    });
  });

  it("handles the empty fleet", () => {
    expect(countByStatus([])).toEqual({
      counts: {
        running: 0,
        "waiting-input": 0,
        idle: 0,
        error: 0,
        archived: 0,
      },
      total: 0,
    });
  });
});

describe("isLiveStatus", () => {
  it("treats only running and waiting-input as live", () => {
    expect(isLiveStatus("running")).toBe(true);
    expect(isLiveStatus("waiting-input")).toBe(true);
    expect(isLiveStatus("idle")).toBe(false);
    expect(isLiveStatus("error")).toBe(false);
    expect(isLiveStatus("archived")).toBe(false);
  });
});

describe("applyFleetFilters", () => {
  it("passes everything through when unfiltered", () => {
    const rows = buildFixtureSnapshot().sessions;
    expect(applyFleetFilters(rows, "", "all", false)).toHaveLength(5);
  });

  it("filters by status", () => {
    const rows = buildFixtureSnapshot().sessions;
    expect(
      applyFleetFilters(rows, "", "running", false).map((row) => row.id),
    ).toEqual(["ses_running1"]);
    expect(
      applyFleetFilters(rows, "", "archived", false).map((row) => row.id),
    ).toEqual(["ses_archived1"]);
  });

  it("live-only keeps running plus waiting-input", () => {
    const rows = buildFixtureSnapshot().sessions;
    expect(
      applyFleetFilters(rows, "", "all", true).map((row) => row.id),
    ).toEqual(["ses_running1", "ses_waiting1"]);
  });

  it("composes query, status, and live-only as AND", () => {
    const rows = [
      makeRow({ id: "a", title: "deploy worker", status: "running" }),
      makeRow({ id: "b", title: "deploy docs", status: "idle" }),
      makeRow({ id: "c", title: "deploy worker retry", status: "error" }),
    ];
    expect(
      applyFleetFilters(rows, "deploy", "all", false).map((row) => row.id),
    ).toEqual(["a", "b", "c"]);
    expect(
      applyFleetFilters(rows, "worker", "running", false).map((row) => row.id),
    ).toEqual(["a"]);
    expect(
      applyFleetFilters(rows, "worker", "all", true).map((row) => row.id),
    ).toEqual(["a"]);
    expect(applyFleetFilters(rows, "worker", "error", true)).toHaveLength(0);
  });
});
