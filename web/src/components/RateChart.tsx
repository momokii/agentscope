/**
 * RateChart: fleet/project token-rate area (DESIGN-compliant enrichment).
 * Hand-rolled SVG (no chart lib): accent stroke, tonal wash fill, flat
 * baseline, no animation, no gridlines. Peak label + accessible description
 * carry the numbers — the shape is never the only signal.
 */

import { ratePeak } from "../lib/rate";
import { formatCompact } from "../lib/format";

export const RATE_WIDTH = 300;
export const RATE_HEIGHT = 44;

/** Sample point for index i: x spreads the series, y is 0-baselined. */
function ratePoint(values: readonly number[], peak: number, i: number): [number, number] {
  const x =
    values.length <= 1 ? RATE_WIDTH : (i / (values.length - 1)) * RATE_WIDTH;
  const y =
    peak === 0
      ? RATE_HEIGHT - 2
      : RATE_HEIGHT - 2 - (values[i] / peak) * (RATE_HEIGHT - 6);
  return [x, y];
}

/** Line path through the samples (empty → flat midline). */
export function rateLinePath(values: readonly number[]): string {
  if (values.length === 0) {
    const y = RATE_HEIGHT / 2;
    return `M0,${y} L${RATE_WIDTH},${y}`;
  }
  const peak = ratePeak(values);
  return values
    .map((_, i) => {
      const [x, y] = ratePoint(values, peak, i);
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

/** Area path: the line closed along the 0 baseline (empty → flat band). */
export function rateAreaPath(values: readonly number[]): string {
  if (values.length === 0) {
    const y = RATE_HEIGHT / 2;
    return `M0,${y} L${RATE_WIDTH},${y} L${RATE_WIDTH},${RATE_HEIGHT} L0,${RATE_HEIGHT} Z`;
  }
  return `${rateLinePath(values)} L${RATE_WIDTH},${RATE_HEIGHT} L0,${RATE_HEIGHT} Z`;
}

export interface RateChartProps {
  values: readonly number[];
  /** Short label, e.g. "Fleet" or the project name. */
  label: string;
}

export default function RateChart({ values, label }: RateChartProps) {
  const peak = ratePeak(values);
  const title = `${label} token rate (tokens/min per tick): peak ${formatCompact(peak)}`;
  return (
    <figure className="rate-chart" role="img" aria-label={title}>
      <figcaption className="rate-caption mono">
        <span>{label} token rate</span>
        <span className="rate-peak">peak {formatCompact(peak)}/min</span>
      </figcaption>
      <svg
        className="rate-svg"
        viewBox={`0 0 ${RATE_WIDTH} ${RATE_HEIGHT}`}
        preserveAspectRatio="none"
        aria-hidden="true"
      >
        <path className="rate-area" d={rateAreaPath(values)} />
        <path className="rate-line" d={rateLinePath(values)} fill="none" />
      </svg>
    </figure>
  );
}
