import { describe, expect, it } from "vitest";

import {
  buildFleetHash,
  parseHash,
  parseLiveParam,
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
