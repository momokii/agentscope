import { describe, expect, it } from "vitest";

import {
  filterSessions,
  groupFleetRows,
  groupKeyFor,
  sortFleetRows,
} from "./fleetSort";
import { makeRow } from "../testutils/fixture";

/** Fixed local noon: 25h ago is Yesterday, 55h ago is Older, always. */
const NOON = new Date(2026, 9, 24, 12, 0, 0, 0).getTime();

describe("sortFleetRows", () => {
  it("orders running first and exited last", () => {
    const rows = [
      makeRow({ id: "archived", status: "archived" }),
      makeRow({ id: "idle", status: "idle" }),
      makeRow({
        id: "running-exited",
        status: "running",
        pane: { cwd: "", exited: true },
      }),
      makeRow({ id: "error", status: "error" }),
      makeRow({ id: "running", status: "running" }),
      makeRow({ id: "waiting", status: "waiting-input" }),
    ];
    const ids = sortFleetRows(rows).map((row) => row.id);
    expect(ids).toEqual([
      "running",
      "running-exited",
      "waiting",
      "idle",
      "error",
      "archived",
    ]);
  });

  it("breaks same-status ties by most-recent update", () => {
    const rows = [
      makeRow({ id: "old", status: "idle", updated_relative_ms: 5000 }),
      makeRow({ id: "new", status: "idle", updated_relative_ms: 1000 }),
    ];
    expect(sortFleetRows(rows).map((row) => row.id)).toEqual(["new", "old"]);
  });
});

describe("groupKeyFor", () => {
  it("buckets Today / Yesterday / Older off local-midnight boundaries", () => {
    expect(groupKeyFor(5_000, NOON)).toBe("today");
    expect(groupKeyFor(3_600_000, NOON)).toBe("today");
    expect(groupKeyFor(25 * 3_600_000, NOON)).toBe("yesterday");
    expect(groupKeyFor(55 * 3_600_000, NOON)).toBe("older");
  });
});

describe("groupFleetRows", () => {
  it("omits empty groups and preserves row order", () => {
    const rows = [
      makeRow({ id: "a", updated_relative_ms: 1000 }),
      makeRow({ id: "b", updated_relative_ms: 55 * 3_600_000 }),
    ];
    const groups = groupFleetRows(rows, NOON);
    expect(groups.map((group) => group.key)).toEqual(["today", "older"]);
    expect(groups[0]?.rows.map((row) => row.id)).toEqual(["a"]);
  });
});

describe("filterSessions", () => {
  it("matches id, title, project, directory, and model case-insensitively", () => {
    const rows = [
      makeRow({ id: "ses_alpha", title: "Fleet view", project: "dash" }),
      makeRow({
        id: "ses_beta",
        title: "Collector",
        directory: "/srv/collector",
        model: "openai/gpt-5",
      }),
    ];
    expect(filterSessions(rows, "fleet").map((row) => row.id)).toEqual([
      "ses_alpha",
    ]);
    expect(filterSessions(rows, "SES_BETA").map((row) => row.id)).toEqual([
      "ses_beta",
    ]);
    expect(filterSessions(rows, "/srv/col").map((row) => row.id)).toEqual([
      "ses_beta",
    ]);
    expect(filterSessions(rows, "gpt-5").map((row) => row.id)).toEqual([
      "ses_beta",
    ]);
    expect(filterSessions(rows, "   ").length).toBe(2);
    expect(filterSessions(rows, "no-match").length).toBe(0);
  });
});
