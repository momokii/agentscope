import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup } from "@testing-library/react";

import { downloadJson, exportFilename } from "./download";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("downloadJson", () => {
  it("downloads the exact data as a JSON blob attachment", async () => {
    const created = vi.fn((_blob: Blob) => "blob:mock");
    const revoked: string[] = [];
    vi.stubGlobal("URL", {
      createObjectURL: created,
      revokeObjectURL: (url: string) => void revoked.push(url),
    });
    const clicked: string[] = [];
    const anchor = {
      set href(v: string) {
        clicked.push(v);
      },
      download: "",
      click: vi.fn(),
      remove: vi.fn(),
    };
    const appendChild = vi
      .spyOn(document.body, "appendChild")
      .mockImplementation(((node: Node) => node) as typeof document.body.appendChild);
    const createElement = vi
      .spyOn(document, "createElement")
      .mockReturnValue(anchor as unknown as HTMLElement);
    vi.useFakeTimers();
    try {
      downloadJson("fleet-x.json", { a: 1 });
      expect(created).toHaveBeenCalledTimes(1);
      const blob = created.mock.calls[0]?.[0] as Blob;
      expect(blob.type).toBe("application/json");
      expect(await blob.text()).toBe(JSON.stringify({ a: 1 }, null, 2));
      expect(clicked).toEqual(["blob:mock"]);
      expect(anchor.download).toBe("fleet-x.json");
      expect(revoked).toEqual([]);
      await vi.runAllTimersAsync();
      expect(revoked).toEqual(["blob:mock"]);
    } finally {
      vi.useRealTimers();
      appendChild.mockRestore();
      createElement.mockRestore();
    }
  });
});

describe("exportFilename", () => {
  it("slugifies the prefix and stamps the time", () => {
    const name = exportFilename(
      "Session ses_ABC 123!",
      new Date(2026, 9, 27, 4, 5, 6),
    );
    expect(name).toBe("session-ses-abc-123-20261027-040506.json");
  });

  it("falls back for empty prefixes", () => {
    const name = exportFilename("!!!", new Date(2026, 9, 27, 4, 5, 6));
    expect(name).toBe("export-20261027-040506.json");
  });
});
