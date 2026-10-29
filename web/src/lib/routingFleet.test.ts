import { describe, expect, it } from "vitest";

import {
  buildActivityHash,
  buildFleetHash,
  buildFleetQueryHash,
  buildProjectHash,
  parseBucketParam,
  parseFromParam,
  parseHarnessParam,
  parseHash,
  parseLiveParam,
  parsePageParam,
  parseSinceParam,
  parseStatusParam,
} from "./routing";

describe("fleet status routing", () => {
  it("parses status and live params off the fleet path", () => {
    expect(parseHash("#/?status=running")).toEqual({
      view: "fleet",
      status: "running",
    });
    expect(parseHash("#/?status=waiting")).toEqual({
      view: "fleet",
      status: "waiting-input",
    });
    expect(parseHash("#/?status=waiting-input")).toEqual({
      view: "fleet",
      status: "waiting-input",
    });
    expect(parseHash("#/?status=idle")).toEqual({
      view: "fleet",
      status: "idle",
    });
    expect(parseHash("#/?status=error&live=1")).toEqual({
      view: "fleet",
      status: "error",
      liveOnly: true,
    });
    expect(parseHash("#/?live=1")).toEqual({
      view: "fleet",
      liveOnly: true,
    });
  });

  it("falls back to the unfiltered fleet on unknown values", () => {
    expect(parseHash("#/?status=bogus")).toEqual({ view: "fleet" });
    expect(parseHash("#/?status=")).toEqual({ view: "fleet" });
    expect(parseHash("#/?live=maybe")).toEqual({ view: "fleet" });
    expect(parseStatusParam("status=BOGUS")).toBe("all");
    expect(parseLiveParam("live=yes")).toBe(false);
    expect(parseLiveParam("live=true")).toBe(true);
  });

  it("builds shareable hashes and round-trips them", () => {
    expect(buildFleetHash()).toBe("#/");
    expect(buildFleetHash("all", false)).toBe("#/");
    expect(buildFleetHash("running")).toBe("#/?status=running");
    expect(buildFleetHash("all", true)).toBe("#/?live=1");
    expect(buildFleetHash("archived", true)).toBe(
      "#/?status=archived&live=1",
    );
    for (const hash of [
      "#/",
      "#/?status=running",
      "#/?status=waiting-input&live=1",
      "#/?status=idle",
      "#/?live=1",
    ]) {
      const route = parseHash(hash);
      if (route.view !== "fleet") throw new Error(`lost fleet route: ${hash}`);
      expect(parseHash(buildFleetHash(route.status ?? "all", route.liveOnly ?? false))).toEqual(route);
    }
  });
});

describe("project drill-in routing", () => {
  it("parses an encoded directory and its fleet query params", () => {
    const hash =
      "#/projects/%2Fsrv%2Falpha%20service?status=running&live=1&bucket=stale&since=7d&page=3";
    expect(parseHash(hash)).toEqual({
      view: "project",
      directory: "/srv/alpha service",
      status: "running",
      liveOnly: true,
      bucket: "stale",
      since: "7d",
      page: 3,
    });
  });

  it("round-trips the project builder with filters", () => {
    const hash = buildProjectHash("/srv/alpha service", {
      status: "running",
      bucket: "old",
      since: "24h",
      page: 2,
    });
    expect(hash).toBe(
      "#/projects/%2Fsrv%2Falpha%20service?status=running&bucket=old&since=24h&page=2",
    );
    expect(parseHash(hash)).toEqual({
      view: "project",
      directory: "/srv/alpha service",
      status: "running",
      bucket: "old",
      since: "24h",
      page: 2,
    });
  });

  it("falls back to the fleet for an empty project path", () => {
    expect(parseHash("#/projects/")).toEqual({ view: "fleet" });
    expect(parseHash("#/projects/?status=running")).toEqual({
      view: "fleet",
      status: "running",
    });
  });
});

describe("recency + paging params", () => {
  it("parses known values and ignores unknown/default ones", () => {
    expect(parseBucketParam("bucket=recent")).toBe("recent");
    expect(parseBucketParam("bucket=all")).toBe("all");
    expect(parseBucketParam("bucket=bogus")).toBeUndefined();
    expect(parseSinceParam("since=24h")).toBe("24h");
    expect(parseSinceParam("since=today")).toBe("today");
    expect(parseSinceParam("since=all")).toBeUndefined();
    expect(parseSinceParam("since=bogus")).toBeUndefined();
    expect(parseFromParam("from=2026-10-24")).toBe("2026-10-24");
    expect(parseFromParam("from=2026-13-40")).toBeUndefined();
    expect(parsePageParam("page=2")).toBe(2);
    expect(parsePageParam("page=1")).toBeUndefined();
    expect(parsePageParam("page=0")).toBeUndefined();
    expect(parsePageParam("page=nope")).toBeUndefined();
  });

  it("lets from win over since and serializes page only when >1", () => {
    expect(parseHash("#/?since=7d&from=2026-10-01&page=4")).toEqual({
      view: "fleet",
      from: "2026-10-01",
      page: 4,
    });
    expect(
      buildFleetQueryHash({
        bucket: "recent",
        since: "24h",
        from: "2026-10-01",
        page: 1,
      }),
    ).toBe("#/?bucket=recent&from=2026-10-01");
    expect(buildFleetQueryHash({ bucket: "active-recent" })).toBe("#/");
    expect(buildFleetQueryHash({ page: 2 })).toBe("#/?page=2");
  });

  it("parses and builds the activity log route", () => {
    expect(parseHash("#/activity")).toEqual({ view: "activity" });
    expect(parseHash("#/activity?q=ses_x&page=3")).toEqual({
      view: "activity",
      query: "ses_x",
      page: 3,
    });
    expect(buildActivityHash({})).toBe("#/activity");
    expect(buildActivityHash({ query: "wa", page: 2 })).toBe(
      "#/activity?q=wa&page=2",
    );
  });

  it("keeps the legacy fleet builder signature working", () => {
    expect(buildFleetHash("archived", true)).toBe("#/?status=archived&live=1");
    expect(buildFleetHash("all", false, { bucket: "old", page: 2 })).toBe(
      "#/?bucket=old&page=2",
    );
  });
});

describe("harness routing", () => {
  it("parses and builds the harness slice (unknown falls back to all)", () => {
    expect(parseHash("#/?harness=pi")).toEqual({
      view: "fleet",
      harness: "pi",
    });
    expect(parseHash("#/?harness=opencode&status=running")).toEqual({
      view: "fleet",
      harness: "opencode",
      status: "running",
    });
    expect(parseHarnessParam("harness=bogus")).toBe("all");
    expect(parseHarnessParam("")).toBe("all");
    expect(buildFleetQueryHash({ harness: "pi" })).toBe("#/?harness=pi");
    expect(buildFleetQueryHash({ harness: "all" })).toBe("#/");
    expect(buildFleetQueryHash({})).toBe("#/");
  });

  it("round-trips harness on the project path", () => {
    expect(parseHash("#/projects/dir?harness=pi")).toEqual({
      view: "project",
      directory: "dir",
      harness: "pi",
    });
    expect(buildProjectHash("dir", { harness: "pi", page: 2 })).toBe(
      "#/projects/dir?harness=pi&page=2",
    );
  });
});
