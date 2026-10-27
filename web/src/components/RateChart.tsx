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
/** Axis label lane below the plot (ticks are poll ticks, not wall time). */
export const RATE_AXIS_H = 12;

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

/** Dot centers for per-sample tooltips (empty → none). */
export function rateDots(values: readonly number[]): Array<{
  x: number;
  y: number;
  label: string;
}> {
  if (values.length === 0) return [];
  const peak = ratePeak(values);
  return values.map((value, i) => {
    const [x, y] = ratePoint(values, peak, i);
    return {
      x,
      y,
      label: `sample ${i + 1} of ${values.length} · ${formatCompact(value)}/min`,
    };
  });
}

export interface RateChartProps {
  values: readonly number[];
  /** Short label, e.g. "Fleet" or the project name. */
  label: string;
}

export default function RateChart({ values, label }: RateChartProps) {
  const peak = ratePeak(values);
  const title = `${label} token rate (tokens/min per tick): peak ${formatCompact(peak)}`;
  const dots = rateDots(values);
  // Axis labels live in HTML (never in the stretched SVG): each sample is
  // one poll tick, so the axis reads in ticks, honestly — not wall time.
  const axis =
    values.length > 1
      ? `oldest of ${values.length} ticks → latest`
      : "latest tick";
  return (
    <figure className="rate-chart" role="img" aria-label={`${title}; ${axis}`}>
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
        {dots.map((dot, i) => (
          <circle key={i} className="rate-dot" cx={dot.x} cy={dot.y} r={2}>
            <title>{dot.label}</title>
          </circle>
        ))}
      </svg>
      {values.length > 1 && (
        <div className="rate-axis mono" aria-hidden="true">
          <span>−{values.length - 1} ticks</span>
          <span>latest</span>
        </div>
      )}
      <div className="rate-note mono">1 sample ≈ 1 poll tick (default 10s)</div>
    </figure>
  );
}
