import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import HistoryView from "./History";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function mockHistory(points = [{ ts: 1000, cost_24h: 1.5, tokens_24h: 150 }]) {
  const fetchMock = vi.fn().mockResolvedValue({
    status: 200,
    ok: true,
    json: async () => ({
      from: 0,
      to: 2000,
      points,
      projects: [
        {
          directory: "/proj/a",
          sessions: 2,
          running: 1,
          cost_24h: 1.5,
          cost_7d: 4,
          cost_30d: 9,
          spark: [{ ts: 1000, cost_24h: 1.5 }],
        },
      ],
    }),
  } as unknown as Response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("HistoryView", () => {
  it("charts recorded samples and lists top projects", async () => {
    const fetchMock = mockHistory();
    render(<HistoryView />);
    expect(await screen.findByText("Fleet cost (trailing 24h)")).toBeDefined();
    expect(screen.getByText("Fleet tokens (trailing 24h)")).toBeDefined();
    expect(screen.getByText("/proj/a")).toBeDefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/history?range=7d",
      expect.objectContaining({ headers: { Accept: "application/json" } }),
    );
  });

  it("switches ranges and re-fetches", async () => {
    const fetchMock = mockHistory();
    render(<HistoryView />);
    await screen.findByText("Fleet cost (trailing 24h)");
    fireEvent.click(screen.getByRole("button", { name: "30d" }));
    expect(await screen.findByText("Fleet cost (trailing 24h)")).toBeDefined();
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/history?range=30d",
      expect.anything(),
    );
  });

  it("renders the unavailable empty state on 503", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ status: 503, ok: false }),
    );
    render(<HistoryView />);
    expect(await screen.findByText(/History is unavailable/)).toBeDefined();
  });

  it("renders the empty state with no samples", async () => {
    mockHistory([]);
    render(<HistoryView />);
    expect(await screen.findByText(/No cost samples in range yet/)).toBeDefined();
  });
});
