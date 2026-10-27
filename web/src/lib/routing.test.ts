import { describe, expect, it } from "vitest";

import {
  buildCompareHash,
  buildFleetHash,
  buildSessionHash,
  buildTimelineHash,
  parseHash,
} from "./routing";

describe("hash routing", () => {
  it("parses fleet routes", () => {
    expect(parseHash("#/")).toEqual({ view: "fleet" });
    expect(parseHash("")).toEqual({ view: "fleet" });
    expect(parseHash("#/sessions/")).toEqual({ view: "fleet" });
    expect(parseHash("#/unknown")).toEqual({ view: "fleet" });
  });

  it("parses session routes with decoding", () => {
    expect(parseHash("#/sessions/ses_abc123")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "transcript",
    });
    expect(parseHash("#/sessions/ses%20x")).toEqual({
      view: "session",
      id: "ses x",
      tab: "transcript",
    });
  });

  it("round-trips builders", () => {
    expect(buildFleetHash()).toBe("#/");
    const hash = buildSessionHash("ses_abc123");
    expect(parseHash(hash)).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "transcript",
    });
  });

  it("parses session tab segments, defaulting to transcript", () => {
    expect(parseHash("#/sessions/ses_abc123/turns")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "turns",
    });
    expect(parseHash("#/sessions/ses_abc123/timeline")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "timeline",
    });
    expect(parseHash("#/sessions/ses_abc123/bogus")).toEqual({
      view: "fleet",
    });
    expect(parseHash("#/sessions/ses_abc123/turns/extra")).toEqual({
      view: "fleet",
    });
  });

  it("round-trips session tab builders", () => {
    expect(buildSessionHash("ses_abc123")).toBe("#/sessions/ses_abc123");
    expect(buildSessionHash("ses_abc123", "transcript")).toBe(
      "#/sessions/ses_abc123",
    );
    for (const tab of ["turns", "timeline", "tools", "cost", "raw", "screen"] as const) {
      const hash = buildSessionHash("ses_abc123", tab);
      expect(hash).toBe(`#/sessions/ses_abc123/${tab}`);
      expect(parseHash(hash)).toEqual({
        view: "session",
        id: "ses_abc123",
        tab,
      });
    }
  });

  it("round-trips compare ids through the hash", () => {
    expect(parseHash("#/compare")).toEqual({ view: "compare", ids: [] });
    const hash = buildCompareHash(["ses_a", "ses_b", "ses_c"]);
    expect(hash).toBe("#/compare?ids=ses_a,ses_b,ses_c");
    expect(parseHash(hash)).toEqual({
      view: "compare",
      ids: ["ses_a", "ses_b", "ses_c"],
    });
  });

  it("parses compare ids with decoding, dedupe, and a cap of 4", () => {
    expect(parseHash("#/compare?ids=ses%20x,ses_y")).toEqual({
      view: "compare",
      ids: ["ses x", "ses_y"],
    });
    expect(parseHash("#/compare?ids=a,a,,b")).toEqual({
      view: "compare",
      ids: ["a", "b"],
    });
    expect(parseHash("#/compare?ids=a,b,c,d,e,f")).toEqual({
      view: "compare",
      ids: ["a", "b", "c", "d"],
    });
  });

  it("round-trips the timeline hash", () => {
    expect(buildTimelineHash()).toBe("#/timeline");
    expect(parseHash(buildTimelineHash())).toEqual({ view: "timeline" });
  });

  it("parses additive ?m= raw deep links without breaking tab segments", () => {
    expect(parseHash("#/sessions/ses_abc123/raw?m=msg_1")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "raw",
      messageId: "msg_1",
    });
    expect(parseHash("#/sessions/ses_abc123/raw")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "raw",
    });
    expect(parseHash("#/sessions/ses_abc123/raw?m=")).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "raw",
    });
    expect(parseHash("#/sessions/ses_abc123/turns/extra?m=msg_1")).toEqual({
      view: "fleet",
    });
    expect(parseHash("#/sessions/ses_abc123/bogus?m=msg_1")).toEqual({
      view: "fleet",
    });
  });

  it("round-trips the raw anchor builder", () => {
    expect(buildSessionHash("ses_abc123", "raw", "msg_1")).toBe(
      "#/sessions/ses_abc123/raw?m=msg_1",
    );
    expect(
      parseHash(buildSessionHash("ses_abc123", "raw", "msg_1")),
    ).toEqual({
      view: "session",
      id: "ses_abc123",
      tab: "raw",
      messageId: "msg_1",
    });
    expect(buildSessionHash("ses_abc123", "raw")).toBe(
      "#/sessions/ses_abc123/raw",
    );
  });
});
