/**
 * Token-rate aggregation (Wave 5 enrichment): element-wise sums of the
 * per-session spark rings (`SessionRow.spark`, tokens/min per tick, oldest
 * first, newest last) into one fleet/project series. All inputs are live
 * snapshot data — no sampling, no synthesis.
 */

import type { SessionRow } from "./sse";

/**
 * Sum spark rings across rows, right-aligned (newest sample last). Rings
 * have different lengths (sessions joined at different ticks); missing
 * leading samples read as zero, never as gaps.
 */
export function sumSparks(rows: readonly SessionRow[]): number[] {
  let width = 0;
  for (const row of rows) width = Math.max(width, row.spark.length);
  if (width === 0) return [];
  const out = new Array<number>(width).fill(0);
  for (const row of rows) {
    const offset = width - row.spark.length;
    for (let i = 0; i < row.spark.length; i++) {
      const value = row.spark[i];
      out[offset + i] += Number.isFinite(value) ? value : 0;
    }
  }
  return out;
}

/** Peak of a rate series (0 for empty/all-zero). Labels the chart maximum. */
export function ratePeak(values: readonly number[]): number {
  let peak = 0;
  for (const value of values) {
    if (Number.isFinite(value) && value > peak) peak = value;
  }
  return peak;
}
