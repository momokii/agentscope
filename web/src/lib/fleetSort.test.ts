import { describe, expect, it } from "vitest";

import {
  PAGE_SIZE,
  ageBucketFor,
  applyFleetFilters,
  countByIdleBucket,
  filterSessions,
  groupFleetRows,
  groupKeyFor,
  matchesDateFilter,
  paginateRows,
  sortFleetRows,
} from "./fleetSort";
import { makeRow } from "../testutils/fixture";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

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

describe("idle bucket boundaries", () => {
  it("splits at exactly 24h and 7d", () => {
    expect(ageBucketFor(0)).toBe("recent");
    expect(ageBucketFor(24 * HOUR)).toBe("recent");
    expect(ageBucketFor(24 * HOUR + 1)).toBe("stale");
    expect(ageBucketFor(7 * DAY)).toBe("stale");
    expect(ageBucketFor(7 * DAY + 1)).toBe("old");
  });

  it("counts buckets once and keeps active statuses in Active + recent", () => {
    const rows = [
      makeRow({ status: "running", updated_relative_ms: 50 * DAY }),
      makeRow({ status: "idle", updated_relative_ms: 5_000 }),
      makeRow({ status: "idle", updated_relative_ms: 2 * DAY }),
      makeRow({ status: "error", updated_relative_ms: 30 * DAY }),
    ];
    expect(countByIdleBucket(rows)).toEqual({
      "active-recent": 2,
      recent: 1,
      stale: 1,
      old: 2,
      all: 4,
    });
  });
});

describe("date presets", () => {
  it("filters Today, 24h, 7d and 30d by last activity", () => {
    const now = NOON;
    expect(matchesDateFilter(5_000, { preset: "today" }, now)).toBe(true);
    expect(matchesDateFilter(25 * HOUR, { preset: "today" }, now)).toBe(false);
    expect(matchesDateFilter(24 * HOUR, { preset: "24h" }, now)).toBe(true);
    expect(matchesDateFilter(24 * HOUR + 1, { preset: "24h" }, now)).toBe(false);
    expect(matchesDateFilter(6 * DAY, { preset: "7d" }, now)).toBe(true);
    expect(matchesDateFilter(8 * DAY, { preset: "7d" }, now)).toBe(false);
    expect(matchesDateFilter(29 * DAY, { preset: "30d" }, now)).toBe(true);
    expect(matchesDateFilter(31 * DAY, { preset: "30d" }, now)).toBe(false);
    expect(matchesDateFilter(31 * DAY, { preset: "all" }, now)).toBe(true);
  });

  it("uses a custom from date derived from the snapshot clock", () => {
    const filter = { preset: "all" as const, from: "2026-10-24" };
    expect(matchesDateFilter(5_000, filter, NOON)).toBe(true);
    expect(matchesDateFilter(25 * HOUR, filter, NOON)).toBe(false);
    expect(matchesDateFilter(5_000, { ...filter, from: "not-a-date" }, NOON)).toBe(
      true,
    );
  });
});

describe("paginateRows", () => {
  const rows = Array.from({ length: 120 }, (_, index) => index);

  it("slices 50 rows per page with clamped page math", () => {
    const first = paginateRows(rows, 1);
    expect(first.rows).toHaveLength(PAGE_SIZE);
    expect(first.totalPages).toBe(3);
    expect(first.total).toBe(120);
    const last = paginateRows(rows, 3);
    expect(last.rows).toHaveLength(20);
    expect(last.page).toBe(3);
    expect(paginateRows(rows, 99).page).toBe(3);
    expect(paginateRows(rows, -4).page).toBe(1);
    expect(paginateRows([], 4)).toEqual({
      rows: [],
      page: 1,
      totalPages: 1,
      total: 0,
    });
  });
});

describe("composed fleet filters", () => {
  it("applies directory, bucket and date in one pass", () => {
    const rows = [
      makeRow({
        id: "a",
        directory: "/srv/alpha",
        status: "idle",
        updated_relative_ms: 5_000,
      }),
      makeRow({
        id: "b",
        directory: "/srv/alpha",
        status: "idle",
        updated_relative_ms: 3 * DAY,
      }),
      makeRow({
        id: "c",
        directory: "/srv/beta",
        status: "running",
        updated_relative_ms: 5_000,
      }),
    ];
    expect(
      applyFleetFilters(rows, "", "all", false, {
        directory: "/srv/alpha",
        bucket: "recent",
        nowMs: NOON,
      }).map((row) => row.id),
    ).toEqual(["a"]);
    expect(
      applyFleetFilters(rows, "", "all", false, {
        directory: "/srv/alpha",
        bucket: "stale",
        nowMs: NOON,
      }).map((row) => row.id),
    ).toEqual(["b"]);
    expect(
      applyFleetFilters(rows, "", "all", false, {
        bucket: "active-recent",
        nowMs: NOON,
      }).map((row) => row.id),
    ).toEqual(["a", "c"]);
    expect(
      applyFleetFilters(rows, "", "all", false, {
        date: { preset: "7d" },
        nowMs: NOON,
      }).map((row) => row.id),
    ).toEqual(["a", "b", "c"]);
  });
});
