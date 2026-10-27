import { afterEach, describe, expect, it, vi } from "vitest";

import { liveProvider, mapExportPayload } from "./sessionProvider";

afterEach(() => {
  vi.unstubAllGlobals();
});

const WIRE_PAYLOAD = {
  session: {
    info: {
      id: "ses_live",
      slug: "live-slug",
      projectID: "prj",
      directory: "/tmp/live",
      title: "Live title",
      agent: "build",
      model: { id: "m1", providerID: "opencode" },
      cost: 0.25,
      tokens: {
        input: 10,
        output: 2,
        reasoning: 1,
        cache: { read: 3, write: 4 },
      },
      time: { created: 100, updated: 200 },
      parentID: null,
    },
    messages: [
      {
        info: {
          id: "msg_1",
          sessionID: "ses_live",
          role: "assistant",
          time: { created: 110, completed: 140 },
          agent: "build",
          modelID: "m1",
          providerID: "opencode",
          tokens: {
            input: 8,
            output: 2,
            reasoning: 1,
            cache: { read: 1, write: 0 },
            total: 12,
          },
          cost: 0.125,
        },
        parts: [
          {
            type: "text",
            id: "prt_1",
            messageID: "msg_1",
            sessionID: "ses_live",
            text: "live text",
          },
        ],
      },
    ],
  },
  children: [
    {
      info: {
        id: "ses_child",
        slug: "child",
        projectID: "prj",
        directory: "/tmp/live",
        title: "Child",
        agent: "explore",
        model: { id: "m1", providerID: "opencode" },
        cost: 0.01,
        tokens: { input: 1, output: 1, reasoning: 0, cache: { read: 0, write: 0 } },
        time: { created: 150, updated: 160 },
        parentID: "ses_live",
      },
      messages: [],
    },
  ],
};

function stubFetch(status: number, body: unknown): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    json: async () => body,
  } as unknown as Response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("liveProvider", () => {
  it("maps a 200 export payload onto SessionDetailData", async () => {
    const fetchMock = stubFetch(200, WIRE_PAYLOAD);

    const data = await liveProvider.getSessionDetail("ses_live");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/sessions/ses_live/export",
      expect.objectContaining({ credentials: "same-origin" }),
    );
    expect(data).not.toBeNull();
    if (data === null) return;
    expect(data.detail.info.id).toBe("ses_live");
    expect(data.detail.info.model.variant).toBe("");
    expect(data.detail.info.tokens).toEqual({
      input: 10,
      output: 2,
      reasoning: 1,
      cache: { read: 3, write: 4 },
      total: 16,
    });
    expect(data.detail.messages).toHaveLength(1);
    expect(data.detail.messages[0].info.tokens?.total).toBe(12);
    expect(data.detail.messages[0].info.time.completed).toBe(140);
    expect(data.detail.messages[0].parts[0]).toMatchObject({
      type: "text",
      id: "prt_1",
      messageID: "msg_1",
      sessionID: "ses_live",
      text: "live text",
    });
    expect(data.children).toHaveLength(1);
    expect(data.children[0].info.parentID).toBe("ses_live");
  });

  it("returns null on 404", async () => {
    stubFetch(404, { code: "unknown session" });
    expect(await liveProvider.getSessionDetail("ses_nope")).toBeNull();
  });

  it("returns null on a network error", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    expect(await liveProvider.getSessionDetail("ses_live")).toBeNull();
  });

  it("returns null on a non-200 response", async () => {
    stubFetch(500, { code: "internal" });
    expect(await liveProvider.getSessionDetail("ses_live")).toBeNull();
  });

  it("returns null on a malformed payload", async () => {
    stubFetch(200, { session: null, children: [] });
    expect(await liveProvider.getSessionDetail("ses_live")).toBeNull();
  });
});

describe("mapExportPayload", () => {
  it("computes a missing session total including cache reads", () => {
    const data = mapExportPayload(WIRE_PAYLOAD);
    expect(data.detail.info.tokens.total).toBe(16);
  });

  it("skips messages with no role and parts with no type", () => {
    const data = mapExportPayload({
      session: {
        info: { id: "ses_x" },
        messages: [
          { info: { id: "msg_bad" }, parts: [] },
          {
            info: { id: "msg_ok", role: "user", time: { created: 1 } },
            parts: [{ id: "prt_bad" }, { type: "text", id: "prt_ok", text: "kept" }],
          },
        ],
      },
      children: [],
    });
    expect(data.detail.messages).toHaveLength(1);
    expect(data.detail.messages[0].parts).toHaveLength(1);
    expect(data.detail.messages[0].parts[0].id).toBe("prt_ok");
  });
});
