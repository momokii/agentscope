import { describe, expect, it, vi } from "vitest";

import { loadPins, savePins, togglePin, PINS_STORAGE_KEY } from "./pins";

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const store = new Map(Object.entries(initial));
  return {
    getItem: (k: string) => (store.has(k) ? store.get(k)! : null),
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
    key: (i: number) => [...store.keys()][i] ?? null,
    get length() {
      return store.size;
    },
  };
}

describe("pins", () => {
  it("round-trips through storage", () => {
    const storage = memoryStorage();
    savePins(storage, ["ses_a", "ses_b"]);
    expect(loadPins(storage)).toEqual(["ses_a", "ses_b"]);
  });

  it("corrupt JSON resets to empty, never crashes", () => {
    const storage = memoryStorage({ [PINS_STORAGE_KEY]: "{not json" });
    expect(loadPins(storage)).toEqual([]);
    const nonArray = memoryStorage({ [PINS_STORAGE_KEY]: '"oops"' });
    expect(loadPins(nonArray)).toEqual([]);
  });

  it("togglePin adds, removes, and caps at 9", () => {
    expect(togglePin([], "ses_a")).toEqual(["ses_a"]);
    expect(togglePin(["ses_a"], "ses_a")).toEqual([]);
    const nine = Array.from({ length: 9 }, (_, i) => `ses_${i}`);
    expect(togglePin(nine, "ses_extra")).toEqual(nine);
  });

  it("savePins truncates to 9", () => {
    const storage = memoryStorage();
    const ten = Array.from({ length: 10 }, (_, i) => `ses_${i}`);
    savePins(storage, ten);
    expect(loadPins(storage).length).toBe(9);
  });

  it("storage setItem throwing never crashes save", () => {
    const storage = memoryStorage();
    vi.spyOn(storage, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => savePins(storage, ["ses_a"])).not.toThrow();
  });
});
