import { describe, expect, it } from "vitest";

import { ratePeak, sumSparks } from "./rate";
import { makeRow } from "../testutils/fixture";

describe("sumSparks", () => {
  it("returns empty for no rows or all-empty rings", () => {
    expect(sumSparks([])).toEqual([]);
    expect(sumSparks([makeRow({ spark: [] })])).toEqual([]);
  });

  it("sums element-wise across rows", () => {
    const rows = [
      makeRow({ spark: [1, 2, 3] }),
      makeRow({ spark: [10, 20, 30] }),
    ];
    expect(sumSparks(rows)).toEqual([11, 22, 33]);
  });

  it("right-aligns rings of different lengths (newest last)", () => {
    const rows = [
      makeRow({ spark: [1, 2, 3, 4] }),
      makeRow({ spark: [10, 20] }),
    ];
    expect(sumSparks(rows)).toEqual([1, 2, 13, 24]);
  });

  it("treats non-finite samples as zero instead of poisoning the sum", () => {
    const rows = [makeRow({ spark: [1, Number.NaN, Infinity] })];
    expect(sumSparks(rows)).toEqual([1, 0, 0]);
  });
});

describe("ratePeak", () => {
  it("reports the maximum finite sample, 0 for empty", () => {
    expect(ratePeak([])).toBe(0);
    expect(ratePeak([0, 0])).toBe(0);
    expect(ratePeak([1, 5, 3])).toBe(5);
    expect(ratePeak([2, Number.NaN])).toBe(2);
  });
});
