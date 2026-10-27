import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import SessionDetail from "./SessionDetail";
import { FIXTURE_MAIN_ID, fixtureProvider } from "../testutils/sessionDetail.fixture";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

interface FakeHeaders {
  get(name: string): string | null;
}

function screenResponse(
  status: number,
  body: unknown,
  headers: Record<string, string> = {},
): Response {
  const headerMap: FakeHeaders = {
    get: (name: string) => headers[name] ?? null,
  };
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: headerMap,
    text: async () => (typeof body === "string" ? body : JSON.stringify(body)),
    json: async () => body,
  } as unknown as Response;
}

function stubScreen(response: Response): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function renderPane() {
  return render(
    <SessionDetail
      id={FIXTURE_MAIN_ID}
      tab="screen"
      row={null}
      paletteOpen={false}
      provider={fixtureProvider}
    />,
  );
}

describe("Live pane tab", () => {
  it("renders the pane text with the pane id and capture time", async () => {
    const fetchMock = stubScreen(
      screenResponse(200, "pane line 1\npane line 2", {
        "X-Pane-Id": "7",
        "X-Captured-At": "2026-10-27T10:00:00Z",
      }),
    );

    renderPane();

    expect(await screen.findByTestId("pane-text")).toBeDefined();
    expect(screen.getByTestId("pane-text").textContent).toContain("pane line 1");
    expect(screen.getByTestId("pane-caption").textContent).toContain("7");
    expect(screen.getByTestId("pane-caption").textContent).toContain(
      "2026-10-27T10:00:00Z",
    );
    expect(fetchMock).toHaveBeenCalledWith(
      `/api/sessions/${FIXTURE_MAIN_ID}/screen?full=1`,
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("renders the DB-only hint from a 409 body", async () => {
    stubScreen(
      screenResponse(409, {
        error: "no live pane",
        db_only: true,
        hint: "orphan session: DB tabs only",
      }),
    );

    renderPane();

    expect((await screen.findByTestId("pane-db-only")).textContent).toContain(
      "orphan session: DB tabs only",
    );
  });

  it("renders a timeout message on 504", async () => {
    stubScreen(screenResponse(504, { code: "screen timeout" }));

    renderPane();

    expect((await screen.findByTestId("pane-timeout")).textContent).toContain(
      "took too long",
    );
  });

  it("renders an unavailable message on 503", async () => {
    stubScreen(screenResponse(503, { code: "screen unavailable" }));

    renderPane();

    expect((await screen.findByTestId("pane-unavailable")).textContent).toContain(
      "No live pane",
    );
  });

  it("fetches only when the tab is opened", async () => {
    const fetchMock = stubScreen(screenResponse(200, "pane text"));
    const view = render(
      <SessionDetail
        id={FIXTURE_MAIN_ID}
        tab="transcript"
        row={null}
        paletteOpen={false}
        provider={fixtureProvider}
      />,
    );
    await screen.findByText("Add dashboard drill-down");
    expect(fetchMock).not.toHaveBeenCalled();

    view.rerender(
      <SessionDetail
        id={FIXTURE_MAIN_ID}
        tab="screen"
        row={null}
        paletteOpen={false}
        provider={fixtureProvider}
      />,
    );
    await screen.findByTestId("pane-text");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
